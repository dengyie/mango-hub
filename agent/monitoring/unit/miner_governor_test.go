package monitoring

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// writeGovernorFile 落一个 state.json 并把 AGENT_GOVERNOR_STATE 指向它。
// age > 0 时把 mtime 推回过去,用来覆盖「调节器停写」的过期判定。
func writeGovernorFile(t *testing.T, body string, age time.Duration) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write governor state: %v", err)
	}
	if age > 0 {
		old := time.Now().Add(-age)
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatalf("chtimes: %v", err)
		}
	}
	t.Setenv(governorStateEnv, p)
}

// 真实产物形态:PowerShell 5.1 的 Set-Content -Encoding UTF8 带 BOM,键名是 camelCase。
func TestReadGovernorStateParsesPowerShellJSON(t *testing.T) {
	writeGovernorFile(t, "\ufeff{\n    \"since\":  \"2026-11-27T01:30:15.3209246+08:00\",\n    \"idleSec\":  6,\n    \"gpuPct\":  6.1510001455731853,\n    \"gpuPid\":  8412,\n    \"tier\":  \"eco\",\n    \"pausedByGov\":  false\n}", 0)

	got := readGovernorState()
	if got == nil {
		t.Fatal("readGovernorState() = nil, want snapshot")
	}
	if got.Tier != "eco" || got.IdleSec != 6 || got.PausedByGov {
		t.Fatalf("unexpected snapshot: %+v", got)
	}
	if got.GpuPct < 6.15 || got.GpuPct > 6.16 {
		t.Fatalf("gpu_pct = %v, want ~6.151", got.GpuPct)
	}
}

func TestReadGovernorStateRejectsStaleFile(t *testing.T) {
	writeGovernorFile(t, `{"tier":"pause","idleSec":0,"gpuPct":42,"pausedByGov":true}`, governorStaleAfter+10*time.Second)
	if got := readGovernorState(); got != nil {
		t.Fatalf("stale state must be dropped, got %+v", got)
	}
}

func TestReadGovernorStateMissingFile(t *testing.T) {
	t.Setenv(governorStateEnv, filepath.Join(t.TempDir(), "nope.json"))
	if got := readGovernorState(); got != nil {
		t.Fatalf("missing file must be nil, got %+v", got)
	}
}

// 历史坑:state.json 曾被写成数组(函数返回值污染),应取最后一个含 tier 的元素。
func TestReadGovernorStateArrayForm(t *testing.T) {
	writeGovernorFile(t, `[{"tier":"full"},{"tier":"pause","idleSec":1,"gpuPct":31.5}]`, 0)
	got := readGovernorState()
	if got == nil || got.Tier != "pause" || got.GpuPct != 31.5 {
		t.Fatalf("array form = %+v, want last tier=pause", got)
	}
}

func TestReadGovernorStateRejectsGarbage(t *testing.T) {
	for _, body := range []string{"not json", `{"idleSec":3}`, `{"tier":"   "}`, ""} {
		writeGovernorFile(t, body, 0)
		if got := readGovernorState(); got != nil {
			t.Fatalf("body %q must yield nil, got %+v", body, got)
		}
	}
}

// 非 Windows 且没显式配置路径时不采集(避免在 Linux 节点上瞎读文件);显式配置则优先。
func TestGovernorStatePathNeedsWindowsOrEnv(t *testing.T) {
	t.Setenv(governorStateEnv, "")
	got := governorStatePath()
	if runtime.GOOS == "windows" {
		if got != governorStateDefault {
			t.Fatalf("windows default path = %q", got)
		}
	} else if got != "" {
		t.Fatalf("non-windows without env must be empty, got %q", got)
	}
	t.Setenv(governorStateEnv, "/tmp/custom-state.json")
	if got := governorStatePath(); got != "/tmp/custom-state.json" {
		t.Fatalf("env override not honoured: %q", got)
	}
}

// 回归:调节器进 pause 档的真实动作是停掉 srbminer 服务,矿工 API 随之消失。
// 早期实现把档位挂在 MinerStat 上,矿工一连败就 Miner()=nil,档位被一起扔掉,
// 网页恰好在最该显示「暂停(让卡)」的时刻变成无徽标。档位必须独立存活。
func TestGovernorSurvivesMinerOutage(t *testing.T) {
	resetMinerState(t)

	writeGovernorFile(t, `{"tier":"pause","idleSec":0,"gpuPct":44.2,"pausedByGov":true}`, 0)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(srbV2Sample))
	}))

	collectMinerOnce(srv.URL + "/api/v2/status")
	if got := Governor(); got == nil || got.Tier != "pause" {
		t.Fatalf("miner alive: Governor() = %+v, want pause", got)
	}
	if Miner() == nil {
		t.Fatal("miner alive: Miner() should still return the snapshot")
	}

	// 调节器 pause: nssm stop srbminer -> 端口无监听
	srv.Close()
	for i := 0; i < minerMaxFails+2; i++ {
		collectMinerOnce("http://127.0.0.1:1/api/v2/status")
	}

	if Miner() != nil {
		t.Fatalf("miner down: Miner() = %+v, want nil (mining data really is gone)", Miner())
	}
	got := Governor()
	if got == nil {
		t.Fatal("miner down: Governor() = nil — pause tier vanished from the panel")
	}
	if got.Tier != "pause" || !got.PausedByGov {
		t.Fatalf("miner down: Governor() = %+v, want tier=pause pausedByGov=true", got)
	}
}

// 档位是「本机调节器」的状态而非「矿工」的状态:矿工从未采集成功过(例如服务没起)
// 时,只要 state.json 新鲜,Governor() 也应可用。
func TestGovernorAvailableWithoutAnyMinerSuccess(t *testing.T) {
	resetMinerState(t)
	writeGovernorFile(t, `{"tier":"full","idleSec":300,"gpuPct":0.4}`, 0)

	collectMinerOnce("http://127.0.0.1:1/api/v2/status")

	if Miner() != nil {
		t.Fatalf("Miner() = %+v, want nil", Miner())
	}
	got := Governor()
	if got == nil || got.Tier != "full" || got.IdleSec != 300 {
		t.Fatalf("Governor() = %+v, want full/300", got)
	}
}

// 档位采集必须独立于矿工采集:此前刷新只在 collectMinerOnce 里,而
// StartMinerCollector 在 url 为空时直接 return —— 没配矿工 API 的机器上
// 调节器明明在跑,档位却永远读不到。独立的 refreshGovernor 必须能单独把档位灌进缓存。
func TestGovernorRefreshesWithoutAnyMinerCollector(t *testing.T) {
	resetMinerState(t)
	writeGovernorFile(t, `{"tier":"eco","idleSec":9,"gpuPct":3.5}`, 0)

	// 刻意不调用 StartMinerCollector / collectMinerOnce
	refreshGovernor()

	got := Governor()
	if got == nil || got.Tier != "eco" || got.IdleSec != 9 {
		t.Fatalf("independent refresh: Governor() = %+v, want eco/9", got)
	}
	if Miner() != nil {
		t.Fatalf("Miner() = %+v, want nil (no miner was ever collected)", Miner())
	}
}

// 非 Windows 且未配置 AGENT_GOVERNOR_STATE 时 StartGovernorCollector 不启 goroutine
// (Linux 节点零开销);配置了路径才启动。
func TestStartGovernorCollectorNoopWithoutPath(t *testing.T) {
	resetMinerState(t)
	if runtime.GOOS == "windows" {
		t.Skip("windows always has a default governor path")
	}
	t.Setenv(governorStateEnv, "")
	StartGovernorCollector()
	if lastGovernor != nil {
		t.Fatalf("no path configured must not populate governor, got %+v", lastGovernor)
	}
}

// 瞬时写失败(文件被清空/写一半)不该立刻把档位从页面抹掉,但超过自身时效必须过期。
func TestGovernorKeepsLastValueUntilItsOwnStaleness(t *testing.T) {
	resetMinerState(t)
	writeGovernorFile(t, `{"tier":"eco","idleSec":5,"gpuPct":1}`, 0)
	collectMinerOnce("http://127.0.0.1:1/api/v2/status")
	if got := Governor(); got == nil || got.Tier != "eco" {
		t.Fatalf("baseline Governor() = %+v, want eco", got)
	}

	// 文件被清空:读取失败 -> 保留上一次有效档位
	if err := os.WriteFile(os.Getenv(governorStateEnv), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	collectMinerOnce("http://127.0.0.1:1/api/v2/status")
	if got := Governor(); got == nil || got.Tier != "eco" {
		t.Fatalf("after transient write failure Governor() = %+v, want the previous eco", got)
	}

	// 手动把缓存时刻推老 -> 超过 governorStaleAfter 必须丢弃(调节器真的停了)
	minerMu.Lock()
	lastGovernorAt = time.Now().Add(-governorStaleAfter - time.Second)
	minerMu.Unlock()
	if got := Governor(); got != nil {
		t.Fatalf("expired Governor() = %+v, want nil", got)
	}
}
