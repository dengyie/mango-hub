package monitoring

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"
)

// MinerStat 一次矿工状态快照（SRBMiner-MULTI /api/v2/status 归一化，跨版本兼容）。
// 只取第一个算法的聚合视图；hashrate 单位 H/s，power W，温度 °C。
//
// 注意：没有 SharesStale 字段。SRBMiner /api/v2/status 不暴露独立 stale 计数，
// 硬编码 0 会向 hub 和面板上报一个「看似健康」的假份额，故不产出该字段（缺失=未知）。
type MinerStat struct {
	Algorithm     string  `json:"algorithm"`
	Pool          string  `json:"pool"`
	Wallet        string  `json:"wallet"`
	Hashrate1Min  float64 `json:"hashrate_1min"`
	Hashrate1Hr   float64 `json:"hashrate_1hr"`
	PowerW        float64 `json:"power_w"`
	Temperature   float64 `json:"temperature"`
	FanPercent    float64 `json:"fan_percent"`
	SharesTotal   int64   `json:"shares_total"`
	SharesValid   int64   `json:"shares_valid"`
	SharesInvalid int64   `json:"shares_invalid"`
	HwErrors      int64   `json:"hw_errors"`
	PoolLatency   int64   `json:"pool_latency"`
}

// GovernorStat 是 power_governor 档位快照的线上形态(hub 侧与之同构)。
type GovernorStat struct {
	Tier        string  `json:"tier"` // full | eco | pause
	IdleSec     int64   `json:"idle_sec"`
	GpuPct      float64 `json:"gpu_pct"`
	PausedByGov bool    `json:"paused_by_gov"`
}

// governorState 是 state.json 的原始结构(PowerShell ConvertTo-Json 产出 camelCase)。
type governorState struct {
	Tier        string  `json:"tier"`
	Since       string  `json:"since"`
	IdleSec     int64   `json:"idleSec"`
	GpuPct      float64 `json:"gpuPct"`
	PausedByGov bool    `json:"pausedByGov"`
}

const (
	// governorStateEnv 覆盖 state.json 路径(测试与非默认部署用)。
	governorStateEnv = "AGENT_GOVERNOR_STATE"
	// governorStateDefault 是两机生产部署的固定路径。
	governorStateDefault = `D:\program\srbminer\governor\state.json`
	// governorStaleAfter 超过该时长未更新即视为调节器已停(daemon 每 10s 写一次)。
	governorStaleAfter = 45 * time.Second
	// governorCollectionInterval 档位轮询间隔,与调节器 daemon 的写盘节奏一致。
	governorCollectionInterval = 10 * time.Second
	// governorMissWarnAfter 连续这么多周期读不到有效档位时告警一次(≈1 分钟)。
	governorMissWarnAfter = 6
)

// governorStatePath 返回 state.json 路径;非 Windows 且未显式配置时返回 ""(不采集)。
func governorStatePath() string {
	if p := strings.TrimSpace(os.Getenv(governorStateEnv)); p != "" {
		return p
	}
	if runtime.GOOS == "windows" {
		return governorStateDefault
	}
	return ""
}

// readGovernorState 读取并校验档位快照。不存在 / 过期 / 解析失败 / 无 tier 一律返回 nil:
// 宁可不上报,也不把陈旧档位当成现状展示。
func readGovernorState() *GovernorStat {
	path := governorStatePath()
	if path == "" {
		return nil
	}
	fi, err := os.Stat(path)
	if err != nil || time.Since(fi.ModTime()) > governorStaleAfter {
		return nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	// PowerShell 5.1 的 Set-Content -Encoding UTF8 会写 BOM,json 解析前必须剥掉。
	text := strings.TrimPrefix(string(raw), "\ufeff")
	var st governorState
	if err := json.Unmarshal([]byte(text), &st); err != nil {
		// 容错: 历史上出现过 state.json 被写成数组(函数返回值污染)→ 取最后一个含 tier 的元素。
		var arr []governorState
		if json.Unmarshal([]byte(text), &arr) != nil {
			return nil
		}
		for i := len(arr) - 1; i >= 0; i-- {
			if strings.TrimSpace(arr[i].Tier) != "" {
				st = arr[i]
				break
			}
		}
	}
	if strings.TrimSpace(st.Tier) == "" {
		return nil
	}
	return &GovernorStat{Tier: st.Tier, IdleSec: st.IdleSec, GpuPct: st.GpuPct, PausedByGov: st.PausedByGov}
}

// ---------------------------------------------------------------------------
// governor 与矿工采集解耦:档位是「本机调节器」的状态,不是「矿工进程」的状态。
// pause 档(让卡)会把 srbminer 服务停掉 → 矿工 API 死亡 → 若档位挂在
// MinerStat 里,Miner() 连败后返回 nil 会把档位一起扔掉,网页恰好在该显示
// 「暂停/让卡」的时刻变成无徽标。故档位单独缓存、每个采集周期无条件刷新、
// 由 Governor() 独立出口上报(protocol Report.governor 顶层字段)。
// ---------------------------------------------------------------------------

var (
	// lastGovernor 最近一次读到的档位;lastGovernorAt 记录其获取时刻。
	// 文件短暂缺失/读失败时保留旧值,Governor() 用 lastGovernorAt 判定是否还新鲜。
	lastGovernor   *GovernorStat
	lastGovernorAt time.Time
	// governorMissStreak 连续未读到有效档位的周期数,用于一次性「路径配错」告警。
	governorMissStreak int
)

// refreshGovernor 刷新档位缓存(仅独立采集器调用):成功读到 → 更新缓存;
// 文件过期/缺失/解析失败 → 保留旧值到其自身时效截止,避免调节器一次瞬时
// 写失败就把档位从页面抹掉。连续多个周期读不到有效档位时打一次性告警:
// 路径配错 / 调节器未部署会让「这台机器没有徽标」变成纯静默失败。
func refreshGovernor() {
	minerMu.Lock()
	defer minerMu.Unlock()
	if g := readGovernorState(); g != nil {
		lastGovernor = g
		lastGovernorAt = time.Now()
		governorMissStreak = 0
		return
	}
	governorMissStreak++
	if governorMissStreak == governorMissWarnAfter {
		log.Printf("[governor] no valid state.json after %d polls at %s — wrong path or governor not deployed?",
			governorMissStreak, governorStatePath())
	}
}

// Governor 返回最近一次有效档位(带自身时效判定),与矿工 API 是否存活无关。
// 未采集过 / 超过 governorStaleAfter 未更新 → nil。
func Governor() *GovernorStat {
	minerMu.Lock()
	defer minerMu.Unlock()
	if lastGovernor == nil {
		return nil
	}
	if time.Since(lastGovernorAt) > governorStaleAfter {
		return nil
	}
	return lastGovernor
}

// StartGovernorCollector 启动档位采集 goroutine。与 StartMinerCollector 相互独立:
// 档位来自本机 state.json,不依赖 --miner-api-url 是否配置。此前只在矿工采集失败/
// 成功分支里顺带刷新,没配矿工 API 的机器上档位就永远读不到——那正是「有调节器却
// 没有徽标」的又一条静默失败路径。
//
// 未配置路径(非 Windows 且无 AGENT_GOVERNOR_STATE)时不启动,Linux 节点零开销。
// 幂等(sync.Once);goroutine 随进程常驻。
func StartGovernorCollector() {
	if governorStatePath() == "" {
		return
	}
	governorCollectorOnce.Do(func() {
		go func() {
			log.Printf("[governor] collector started, polling %s every %s", governorStatePath(), governorCollectionInterval)
			refreshGovernor() // 立即读一次,避免首个周期无徽标
			ticker := time.NewTicker(governorCollectionInterval)
			defer ticker.Stop()
			for range ticker.C {
				refreshGovernor()
			}
		}()
	})
}

// srbStatus 是 SRBMiner /api/v2/status 的响应子集；多余字段忽略。
type srbStatus struct {
	GPUDevices []struct {
		Model           string  `json:"model"`
		Temperature     float64 `json:"temperature"`
		AsicPower       float64 `json:"asic_power"`
		FanSpeedPercent float64 `json:"fan_speed_percent"`
	} `json:"gpu_devices"`
	Algorithms []struct {
		Name string `json:"name"`
		Pool struct {
			Pool    string `json:"pool"`
			Wallet  string `json:"wallet"`
			Latency int64  `json:"latency"`
		} `json:"pool"`
		Shares struct {
			Total    int64 `json:"total"`
			Accepted int64 `json:"accepted"`
			Rejected int64 `json:"rejected"`
		} `json:"shares"`
		// 顶层窗口 "1min"/"1hr"/"6hr" 是裸数值（H/s）；"cpu"/"gpu" 子对象里才是
		// 按设备的 map。用 json.RawMessage 兼容两种形态。
		Hashrate struct {
			OneMin json.RawMessage `json:"1min"`
			OneHr  json.RawMessage `json:"1hr"`
		} `json:"hashrate"`
		GPUComputeErrors map[string]int64 `json:"gpu_compute_errors"`
	} `json:"algorithms"`
}

// ---------------------------------------------------------------------------
// 后台采集者架构
//
// 早期实现里 Miner() 在每次上报（GenerateReport 热路径）同步发起 HTTP GET。
// 单个 4s 超时的请求会把整条上报链路拖住——默认上报间隔 3s < 单次超时 4s，矿工
// API 一旦慢/挂，网络/CPU/内存等全部核心指标都要陪着等满超时。
//
// 根因修复：把矿工采集移到一个独立后台 goroutine（StartMinerCollector），按
// minerCollectionInterval 轮询并写入共享快照；Miner() 只做非阻塞读缓存。上报热
// 路径从此不再有任何网络 IO，矿工 API 的慢/挂只影响矿工字段本身。
// ---------------------------------------------------------------------------

const (
	// minerRequestTimeout 单次矿工 API 请求超时。只发生在后台采集 goroutine，不影响上报热路径。
	minerRequestTimeout = 4 * time.Second
	// minerCollectionInterval 后台采集轮询间隔。份额通常每 15-30 分钟才变化，5s 足够新鲜。
	minerCollectionInterval = 5 * time.Second
	// minerStaleAfter 快照年龄上限：超过该时长仍未成功采集则视为过期。
	minerStaleAfter = 60 * time.Second
	// minerMaxFails 连续失败超过该次后，放弃回报过期快照（等同旧实现的连败阈值）。
	minerMaxFails = 5
)

var (
	minerHTTPClient = &http.Client{Timeout: minerRequestTimeout}
	minerMu         sync.Mutex
	lastMinerStat   *MinerStat
	lastMinerFetch  time.Time
	minerFetchFails int
	// minerCollectorOnce 保证采集 goroutine 只启动一次：重复调用（如配置
	// 重载路径误触发）会叠加 ticker 造成并发写共享快照与请求翻倍。
	minerCollectorOnce sync.Once
	// governorCollectorOnce 同理,保护独立的档位采集 goroutine。
	governorCollectorOnce sync.Once
)

// StartMinerCollector 在后台轮询 SRBMiner API 并维护最新快照。url 为空时不启动（无上报）。
// 应在 agent 初始化阶段调用；幂等（sync.Once），重复调用只生效首次，避免叠加轮询 goroutine。
// goroutine 随进程常驻，进程退出即回收（无需显式 stop）。
func StartMinerCollector(url string) {
	url = strings.TrimSpace(url)
	if url == "" {
		return
	}
	minerCollectorOnce.Do(func() {
		go func() {
			log.Printf("[miner] collector started, polling %s every %s", url, minerCollectionInterval)
			collectMinerOnce(url) // 立即拉一次，避免首个周期在上报前无数据
			ticker := time.NewTicker(minerCollectionInterval)
			defer ticker.Stop()
			for range ticker.C {
				collectMinerOnce(url)
			}
		}()
	})
}

// collectMinerOnce 拉取一次最新状态并写入共享缓存。失败仅更新失败计数并打日志（限流），
// 保留最近一次成功快照；是否返回由 Miner() 按 minerStaleAfter / minerMaxFails 判定。
func collectMinerOnce(url string) {
	stat, err := fetchMinerStat(url)
	minerMu.Lock()
	defer minerMu.Unlock()
	if err != nil {
		minerFetchFails++
		// 每次失败都打日志太吵,只在阈值附近打(首批 + 达到阈值那次)。
		if minerFetchFails <= minerMaxFails || minerFetchFails == minerMaxFails+1 {
			log.Printf("[miner] fetch failed (%d consecutive): %v", minerFetchFails, err)
		}
		return
	}
	minerFetchFails = 0
	lastMinerStat = stat
	lastMinerFetch = time.Now()
}

// Miner returns 最近一次成功采集的矿工快照（只读缓存读，无任何网络 IO，非阻塞）。
// 未采集过、或快照已过期（>minerStaleAfter 或连败>minerMaxFails）时返回 nil。
func Miner() *MinerStat {
	minerMu.Lock()
	defer minerMu.Unlock()
	if lastMinerStat == nil {
		return nil
	}
	if time.Since(lastMinerFetch) > minerStaleAfter || minerFetchFails > minerMaxFails {
		return nil
	}
	return lastMinerStat
}

func fetchMinerStat(rawURL string) (*MinerStat, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, fmt.Errorf("miner api url is empty")
	}
	resp, err := minerHTTPClient.Get(rawURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("miner api status %d", resp.StatusCode)
	}
	var s srbStatus
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return nil, err
	}
	return normalizeSrbStatus(&s)
}

// normalizeSrbStatus 把 SRBMiner v2 状态归一化为 MinerStat。
// hashrate 窗口 "1min"/"1hr" 官方 3.6.3 为裸数值（旧版可能为对象），两种形态都兼容。
// 本产品面向单算法矿机（如 XEL）：只归一化 Algorithms[0]，多算法矿机会静默只报第一个；
// 若日后需要再扩展为按算法分桶上报。
func normalizeSrbStatus(s *srbStatus) (*MinerStat, error) {
	if len(s.Algorithms) == 0 {
		return nil, fmt.Errorf("miner api returned no algorithms")
	}
	alg := s.Algorithms[0]

	oneMin := rawMessageHash(alg.Hashrate.OneMin)
	oneHr := rawMessageHash(alg.Hashrate.OneHr)
	if oneMin == 0 && oneHr == 0 {
		return nil, fmt.Errorf("miner api returned no hashrate")
	}

	var hwErr int64
	for _, v := range alg.GPUComputeErrors {
		hwErr += v
	}

	// GPU 聚合：取第一块核心矿卡（多卡场景温度/功耗取主卡，避免均值失真）。
	var temp, power, fan float64
	if len(s.GPUDevices) > 0 {
		temp = s.GPUDevices[0].Temperature
		power = s.GPUDevices[0].AsicPower
		fan = s.GPUDevices[0].FanSpeedPercent
	}

	return &MinerStat{
		Algorithm:     alg.Name,
		Pool:          alg.Pool.Pool,
		Wallet:        alg.Pool.Wallet,
		Hashrate1Min:  oneMin,
		Hashrate1Hr:   oneHr,
		PowerW:        power,
		Temperature:   temp,
		FanPercent:    fan,
		SharesTotal:   alg.Shares.Total,
		SharesValid:   alg.Shares.Accepted,
		SharesInvalid: alg.Shares.Rejected,
		HwErrors:      hwErr,
		PoolLatency:   alg.Pool.Latency,
	}, nil
}

// rawMessageHash 从 hashrate 窗口值提取聚合 H/s：裸数值直接取；
// 对象形态优先 "total" 键，其次任意非零值。
func rawMessageHash(raw json.RawMessage) float64 {
	if len(raw) == 0 {
		return 0
	}
	var num float64
	if err := json.Unmarshal(raw, &num); err == nil {
		return num
	}
	var m map[string]float64
	if err := json.Unmarshal(raw, &m); err != nil {
		return 0
	}
	if v, ok := m["total"]; ok && v > 0 {
		return v
	}
	for _, v := range m {
		if v > 0 {
			return v
		}
	}
	return 0
}
