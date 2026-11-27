package jsonrpc

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dengyie/mangohub/hub/database"
	"github.com/dengyie/mangohub/hub/database/clients"
	"github.com/dengyie/mangohub/hub/database/models"
	"github.com/dengyie/mangohub/hub/database/tasks"
	"github.com/dengyie/mangohub/hub/internal/config"
	"github.com/dengyie/mangohub/hub/pkg/rpc"
	"github.com/dengyie/mangohub/hub/utils"
	agent_runtime "github.com/dengyie/mangohub/hub/web/agent"
	v2 "github.com/dengyie/mangohub/protocol/v2"

	cache "github.com/patrickmn/go-cache"
)

// pingstats:<uuid>
var pingStatsCache = cache.New(1*time.Minute, 2*time.Minute)

// pingTasksCache fronts GetAllPingTasks for the latest-status hot path: the
// call used to hit the DB on every request (every ~4s per viewer) although
// ping tasks change rarely.
var pingTasksCache = cache.New(5*time.Second, 10*time.Second)

// pingStatsInflight dedups concurrent per-node ping-stat computations: all
// viewers expire their 1-minute cache at roughly the same time and used to
// re-run the same per-node 1-hour records query simultaneously.
var pingStatsInflight = struct {
	mu    sync.Mutex
	calls map[string]*pingStatsCall
}{calls: make(map[string]*pingStatsCall)}

type pingStatsCall struct {
	done   chan struct{}
	result map[string]pingStat
}

// getAllPingTasksCached returns the ping task list from a 5s cache.
func getAllPingTasksCached() []models.PingTask {
	if v, ok := pingTasksCache.Get("tasks"); ok {
		if tasks, ok2 := v.([]models.PingTask); ok2 {
			return tasks
		}
	}
	tasks, err := tasks.GetAllPingTasks()
	if err != nil {
		return nil
	}
	pingTasksCache.Set("tasks", tasks, cache.DefaultExpiration)
	return tasks
}

// getPingStatsSingleflight collapses concurrent computations for the same
// uuid onto one in-flight call.
func getPingStatsSingleflight(uuid string, pingTasks []models.PingTask) map[string]pingStat {
	pingStatsInflight.mu.Lock()
	if call, ok := pingStatsInflight.calls[uuid]; ok {
		pingStatsInflight.mu.Unlock()
		<-call.done
		return call.result
	}
	call := &pingStatsCall{done: make(chan struct{})}
	pingStatsInflight.calls[uuid] = call
	pingStatsInflight.mu.Unlock()

	defer func() {
		pingStatsInflight.mu.Lock()
		delete(pingStatsInflight.calls, uuid)
		pingStatsInflight.mu.Unlock()
		close(call.done)
	}()
	call.result = getPingStatsForNode(uuid, pingTasks)
	return call.result
}

type pingStat struct {
	Name   string  `json:"name"`
	Latest int     `json:"latest"`
	Avg    int     `json:"avg"`
	Tail   float64 `json:"tail"` // (P99-P50)/P50
	Loss   float64 `json:"loss"` // 丢包率 %
	Min    int     `json:"min"`
	Max    int     `json:"max"`
}

// getPingStatsForNode 计算并缓存节点最近 1 小时 ping 统计
func getPingStatsForNode(uuid string, pingTasks []models.PingTask) map[string]pingStat {
	if uuid == "" {
		return map[string]pingStat{}
	}
	key := fmt.Sprintf("pingstats:%s", uuid)
	if v, ok := pingStatsCache.Get(key); ok {
		if m, ok2 := v.(map[string]pingStat); ok2 {
			return m
		}
	}
	// 筛选属于该节点的任务
	assigned := make([]models.PingTask, 0, 4)
	for _, t := range pingTasks {
		if t.AppliesToClient(uuid) {
			assigned = append(assigned, t)
		}
	}
	if len(assigned) == 0 {
		empty := map[string]pingStat{}
		pingStatsCache.Set(key, empty, cache.DefaultExpiration)
		return empty
	}
	end := time.Now().UTC()
	start := end.Add(-1 * time.Hour)
	recs, err := tasks.GetPingRecords(uuid, -1, start, end)
	if err != nil || len(recs) == 0 {
		empty := map[string]pingStat{}
		pingStatsCache.Set(key, empty, cache.DefaultExpiration)
		return empty
	}
	grouped := make(map[uint][]models.PingRecord)
	for _, r := range recs {
		for _, t := range assigned {
			if r.TaskId == t.Id {
				grouped[r.TaskId] = append(grouped[r.TaskId], r)
				break
			}
		}
	}
	result := make(map[string]pingStat, len(grouped))
	for _, t := range assigned {
		records := grouped[t.Id]
		if len(records) == 0 {
			continue
		}
		latest := -1
		var latestTs time.Time
		values := make([]int, 0, len(records))
		sum := 0
		valid := 0
		total := 0
		lossCount := 0
		minLat := 0
		maxLat := 0
		for _, r := range records {
			total++
			if r.Value < 0 { // 丢包
				lossCount++
				continue
			}
			values = append(values, r.Value)
			sum += r.Value
			valid++
			if minLat == 0 || r.Value < minLat {
				minLat = r.Value
			}
			if r.Value > maxLat {
				maxLat = r.Value
			}
			ts := r.Time
			if latestTs.IsZero() || ts.After(latestTs) {
				latestTs = ts
				latest = r.Value
			}
		}
		avg := 0
		if valid > 0 {
			avg = sum / valid
		}
		p50, p99 := 0, 0
		if len(values) > 0 {
			sort.Ints(values)
			percentile := func(vals []int, pct float64) int {
				if len(vals) == 0 {
					return 0
				}
				if pct <= 0 {
					return vals[0]
				}
				if pct >= 1 {
					return vals[len(vals)-1]
				}
				pos := (float64(len(vals) - 1)) * pct
				lo := int(math.Floor(pos))
				hi := int(math.Ceil(pos))
				if lo == hi {
					return vals[lo]
				}
				frac := pos - float64(lo)
				v := float64(vals[lo]) + (float64(vals[hi])-float64(vals[lo]))*frac
				return int(math.Round(v))
			}
			p50 = percentile(values, 0.50)
			p99 = percentile(values, 0.99)
		}
		tail := 0.0
		if p50 > 0 && p99 >= p50 {
			tail = float64(p99-p50) / float64(p50)
		}
		lossRate := 0.0
		if total > 0 {
			lossRate = float64(lossCount) / float64(total) * 100
		}
		result[fmt.Sprintf("%d", t.Id)] = pingStat{
			Name:   t.Name,
			Latest: latest,
			Avg:    avg,
			Tail:   tail,
			Loss:   lossRate,
			Min:    minLat,
			Max:    maxLat,
		}
	}
	pingStatsCache.Set(key, result, cache.DefaultExpiration)
	return result
}

func init() {
	RegisterWithGroupAndMeta("getNodes", "common",
		func(ctx context.Context, req *rpc.JsonRpcRequest) (any, *rpc.JsonRpcError) {
			return getNodes(ctx, req)
		},
		&rpc.MethodMeta{
			Name:    "getNodes",
			Summary: "Get all nodes",
			Params: []rpc.ParamMeta{
				{
					Name:        "uuid",
					Description: "Specify the UUID of the node",
					Required:    false,
					Type:        "string",
				},
			},
			Returns: "Client | { [uuid]: Client }",
		},
	)
	RegisterWithGroupAndMeta("getNodesLatestStatus", "common",
		func(ctx context.Context, req *rpc.JsonRpcRequest) (any, *rpc.JsonRpcError) {
			return getNodesLatestStatus(ctx, req)
		},
		&rpc.MethodMeta{
			Name:    "getNodesLatestStatus",
			Summary: "Get latest status reports (single node or map).",
			Params: []rpc.ParamMeta{
				{
					Name:        "uuid",
					Description: "Specify the UUID of the node (optional)",
					Required:    false,
					Type:        "string",
				},
				{
					Name:        "uuids",
					Description: "Specify multiple UUIDs (array) to get subset (ignored if uuid provided)",
					Required:    false,
					Type:        "string[]",
				},
			},
			Returns: "Record | { [uuid]: Record }",
		},
	)
	Register("getMe", func(ctx context.Context, req *rpc.JsonRpcRequest) (any, *rpc.JsonRpcError) {
		return getMe(ctx, req)
	})
	Register("getPublicInfo", getPublicInfo)
	Register("getVersion", getVersion)
	Register("getNodeRecentStatus", getNodeRecentStatus)
}

func getNodes(ctx context.Context, req *rpc.JsonRpcRequest) (any, *rpc.JsonRpcError) {
	var params struct {
		UUID string `json:"uuid"`
	}
	req.BindParams(&params)
	cinfo, err := clients.GetAllClientBasicInfo()
	if err != nil {
		return nil, rpc.MakeError(rpc.InternalError, "Failed to get client info", cinfo)
	}
	meta := rpc.MetaFromContext(ctx)

	SendIpAddrToGuest, _ := config.GetAs[bool](config.SendIpAddrToGuestKey)
	if meta.Principal == nil || !meta.Principal.HasRole(rpc.RoleAdmin) {
		// 过滤 Hidden 节点并隐藏敏感字段
		filtered := make([]models.Client, 0, len(cinfo))
		for _, node := range cinfo {
			if node.Hidden { // 非 admin 不显示隐藏节点
				continue
			}
			if SendIpAddrToGuest {
				if node.IPv4 != "" {
					node.IPv4 = strings.Split(node.IPv4, ".")[0] + ".*.*.*"
				}
				if node.IPv6 != "" {
					node.IPv6 = strings.Split(node.IPv6, ":")[0] + ":*:*:*:*:*:*:*"
				}
			} else {
				node.IPv4 = ""
				node.IPv6 = ""
			}

			node.Remark = ""
			node.Version = ""
			node.Token = ""
			clearMinerCapability(&node)
			filtered = append(filtered, node)
		}
		cinfo = filtered
	}
	if params.UUID != "" {
		for _, node := range cinfo {
			if node.UUID == params.UUID {
				return node, nil
			}
		}
		return nil, rpc.MakeError(rpc.InvalidParams, "Node not found", params.UUID)
	}

	// 返回以 uuid 为键的字典（每个 value 自身也包含 uuid 字段）
	nodeMap := make(map[string]models.Client, len(cinfo))
	for _, node := range cinfo {
		nodeMap[node.UUID] = node
	}
	return nodeMap, nil
}

func clearMinerCapability(node *models.Client) {
	node.MinerConfigured = false
	node.MinerControllable = false
}

func gpuUsageFromReport(rep *v2.Report) float32 {
	if rep == nil || rep.GPU == nil {
		return 0
	}
	return float32(rep.GPU.AverageUsage)
}

func getPublicInfo(_ context.Context, _ *rpc.JsonRpcRequest) (any, *rpc.JsonRpcError) {
	info, err := database.GetPublicInfo()
	if err != nil {
		return nil, rpc.MakeError(rpc.InternalError, "Failed to get public info", err.Error())
	}
	return info, nil
}

// governorOfReport 取报告里的自适应功率档位:顶层优先(新 agent),
// 回落到 mining.governor(滚动升级期间的旧 agent)。空档位一律当没有。
func governorOfReport(rep *v2.Report) *v2.GovernorReport {
	if rep == nil {
		return nil
	}
	if g := rep.Governor; g != nil && strings.TrimSpace(g.Tier) != "" {
		return g
	}
	if rep.Mining != nil {
		if g := rep.Mining.Governor; g != nil && strings.TrimSpace(g.Tier) != "" {
			return g
		}
	}
	return nil
}

func getNodesLatestStatus(ctx context.Context, req *rpc.JsonRpcRequest) (any, *rpc.JsonRpcError) {
	var params struct {
		UUID  string   `json:"uuid"`
		UUIDs []string `json:"uuids"`
	}
	req.BindParams(&params)

	meta := rpc.MetaFromContext(ctx)
	latest := agent_runtime.GetLatestReport()
	onlineUUIDs := agent_runtime.GetAllOnlineUUIDs()
	onlineSet := make(map[string]bool, len(onlineUUIDs))
	for _, uuid := range onlineUUIDs {
		onlineSet[uuid] = true
	}

	// Hidden 过滤
	isAdmin := meta.Principal != nil && meta.Principal.HasRole(rpc.RoleAdmin)
	if !isAdmin {
		cinfo, err := clients.GetAllClientBasicInfo()
		if err != nil {
			return nil, rpc.MakeError(rpc.InternalError, "Failed to get client info", err.Error())
		}
		hidden := make(map[string]bool, len(cinfo))
		for _, c := range cinfo {
			if c.Hidden {
				hidden[c.UUID] = true
			}
		}
		for uuid := range latest {
			if hidden[uuid] {
				delete(latest, uuid)
			}
		}
	}

	// 如果指定 uuid 但找不到，直接返回 not found
	if params.UUID != "" {
		if _, ok := latest[params.UUID]; !ok {
			return nil, rpc.MakeError(rpc.InvalidParams, "Node not found", params.UUID)
		}
	}

	// latest-status 每 4s 被首页轮询一次；卡片 pill 只要算法+算力。
	// 不把钱包/矿池/份额打进热路径（历史详情走 mining_records）。
	// miningGovernorLive 是 power_governor 档位的热路径投影(去掉 since/paused_by_gov 等内部字段)。
	// 它刻意不嵌在「有没有矿工数据」的判断里:pause 档会停掉矿工服务,mining 整体缺失,
	// 档位必须继续可见,否则最该显示「暂停(让卡)」的时刻反而什么都没有。
	type miningGovernorLive struct {
		Tier string `json:"tier"` // full | eco | pause
		// idle_sec / gpu_pct 只给管理员:未认证调用者拿到 idle_sec 等于「主人多久没
		// 碰键盘」,gpu_pct 能反推在跑什么负载,属行为隐私;游客只需要档位本身。
		// 注:tier 本身也是粒度约 2 分钟的「主人在场」信号(eco=有输入,full=空闲
		// ≥120s),公开徽标依赖它——这是接受过的产品取舍,别当漏洞单方面修掉。
		IdleSec int64   `json:"idle_sec,omitempty"`
		GpuPct  float64 `json:"gpu_pct,omitempty"`
	}
	type miningLiveStatus struct {
		// algorithm / hashrate_1min 带 omitempty:只有档位没有矿工(pause 档停服务)时
		// 这个块只承载 governor,不能凭空造出 algorithm:"" + hashrate:0 —— 前端会据此
		// 渲染出一颗假的「Mining · 0 H/s」pill。
		Algorithm    string              `json:"algorithm,omitempty"`
		Hashrate1Min *float64            `json:"hashrate_1min,omitempty"`
		Governor     *miningGovernorLive `json:"governor,omitempty"`
	}
	type recordLike struct {
		Client          string              `json:"client"`
		Time            time.Time           `json:"time"`
		Cpu             float32             `json:"cpu"`
		Gpu             float32             `json:"gpu"`
		GpuCount        int                 `json:"gpu_count,omitempty"`
		GpuAverageUsage float64             `json:"gpu_average_usage,omitempty"`
		GpuDetailedInfo []v2.GPUDeviceInfo  `json:"gpu_detailed_info,omitempty"`
		Ram             int64               `json:"ram"`
		RamTotal        int64               `json:"ram_total"`
		Swap            int64               `json:"swap"`
		SwapTotal       int64               `json:"swap_total"`
		Load            float32             `json:"load"`
		Load5           float32             `json:"load5"`
		Load15          float32             `json:"load15"`
		Temp            float32             `json:"temp"`
		Disk            int64               `json:"disk"`
		DiskTotal       int64               `json:"disk_total"`
		NetIn           int64               `json:"net_in"`
		NetOut          int64               `json:"net_out"`
		NetTotalUp      int64               `json:"net_total_up"`
		NetTotalDown    int64               `json:"net_total_down"`
		Process         int                 `json:"process"`
		Connections     int                 `json:"connections"`
		ConnectionsUdp  int                 `json:"connections_udp"`
		Mining          *miningLiveStatus   `json:"mining,omitempty"`
		Online          bool                `json:"online"`
		Uptime          int64               `json:"uptime"`
		Ping            map[string]pingStat `json:"ping"`
	}

	respMap := make(map[string]recordLike, len(latest))

	// 预取所有 ping 任务（5s 缓存，命中热路径避免每请求查库）
	pingTasks := getAllPingTasksCached()

	appendOne := func(uuid string, rep *v2.Report) {
		if rep == nil {
			return
		}
		stats := getPingStatsSingleflight(uuid, pingTasks)
		rl := recordLike{
			Client:         uuid,
			Time:           rep.UpdatedAt,
			Cpu:            float32(rep.CPU.Usage),
			Gpu:            gpuUsageFromReport(rep),
			Ram:            rep.Ram.Used,
			RamTotal:       rep.Ram.Total,
			Swap:           rep.Swap.Used,
			SwapTotal:      rep.Swap.Total,
			Load:           float32(rep.Load.Load1),
			Load5:          float32(rep.Load.Load5),
			Load15:         float32(rep.Load.Load15),
			Temp:           0,
			Disk:           rep.Disk.Used,
			DiskTotal:      rep.Disk.Total,
			NetIn:          rep.Network.Down,
			NetOut:         rep.Network.Up,
			NetTotalUp:     rep.Network.TotalUp,
			NetTotalDown:   rep.Network.TotalDown,
			Process:        rep.Process,
			Connections:    rep.Connections.TCP + rep.Connections.UDP,
			ConnectionsUdp: rep.Connections.UDP,
			Online:         onlineSet[uuid],
			Uptime:         rep.Uptime,
			Ping:           stats,
		}
		if m := rep.Mining; m != nil {
			hs := m.Hashrate1Min
			rl.Mining = &miningLiveStatus{
				Algorithm:    m.Algorithm,
				Hashrate1Min: &hs,
			}
		}
		// 档位与矿工数据解耦:顶层优先(新 agent),回落到 mining.governor(过渡期旧 agent)。
		// 矿工已停(pause 档)时 mining 为 nil,这里仍要造出只带档位的块,让面板看得见。
		if g := governorOfReport(rep); g != nil {
			if rl.Mining == nil {
				rl.Mining = &miningLiveStatus{}
			}
			live := &miningGovernorLive{Tier: g.Tier}
			if isAdmin {
				live.IdleSec = g.IdleSec
				live.GpuPct = g.GpuPct
			}
			rl.Mining.Governor = live
		}
		if rep.GPU != nil {
			rl.GpuCount = rep.GPU.Count
			rl.GpuAverageUsage = rep.GPU.AverageUsage
			rl.GpuDetailedInfo = rep.GPU.DetailedInfo
		}
		respMap[uuid] = rl
	}

	// 选择逻辑
	if params.UUID != "" { // 单个
		appendOne(params.UUID, latest[params.UUID])
		return respMap[params.UUID], nil
	}
	selected := map[string]bool{}
	if len(params.UUIDs) > 0 {
		for _, id := range params.UUIDs {
			selected[id] = true
		}
		for uuid, rep := range latest {
			if selected[uuid] {
				appendOne(uuid, rep)
			}
		}
		return respMap, nil
	}
	for uuid, rep := range latest {
		appendOne(uuid, rep)
	}
	return respMap, nil
}

func getMe(ctx context.Context, _ *rpc.JsonRpcRequest) (any, *rpc.JsonRpcError) {
	var resp struct {
		TwoFAEnabled bool   `json:"2fa_enabled"`
		LoggedIn     bool   `json:"logged_in"`
		SSOId        string `json:"sso_id"`
		SSOType      string `json:"sso_type"`
		Username     string `json:"username"`
		UUID         string `json:"uuid"`
	}

	meta := rpc.MetaFromContext(ctx)

	switch meta.Principal.Type {
	case rpc.PrincipalUser, rpc.PrincipalAPIKey:
		if meta.User == nil {
			resp.LoggedIn = true
			resp.Username = "api_key"
			return resp, nil
		}
		resp.TwoFAEnabled = meta.User.TwoFactor != ""
		resp.LoggedIn = true
		resp.SSOId = meta.User.SSOID
		resp.SSOType = meta.User.SSOType
		resp.Username = meta.User.Username
		resp.UUID = meta.User.UUID
		return resp, nil
	case rpc.PrincipalAnonymous:
		resp.LoggedIn = false
		return resp, nil
	case rpc.PrincipalAgent:
		resp.LoggedIn = true
		resp.SSOId = "client"
		resp.SSOType = "client"
		resp.Username = "client"
		// Return the node's UUID, not an echo of the raw token; the original
		// inverted error check discarded the lookup result on success and
		// re-transmitted the token instead.
		if uuid, err := clients.GetClientUUIDByToken(meta.ClientToken); err == nil && uuid != "" {
			resp.UUID = uuid
		} else {
			resp.UUID = "client"
		}
		return resp, nil
	default:
		resp.LoggedIn = false
		return resp, nil
	}
}

func getVersion(_ context.Context, _ *rpc.JsonRpcRequest) (any, *rpc.JsonRpcError) {
	return struct {
		Version string `json:"version"`
		Hash    string `json:"hash"`
	}{
		Version: utils.CurrentVersion,
		Hash:    utils.VersionHash,
	}, nil
}

func getNodeRecentStatus(ctx context.Context, req *rpc.JsonRpcRequest) (any, *rpc.JsonRpcError) {
	var params struct {
		UUID string `json:"uuid"`
	}
	req.BindParams(&params)
	if params.UUID == "" {
		return nil, rpc.MakeError(rpc.InvalidParams, "UUID is required", params)
	}
	meta := rpc.MetaFromContext(ctx)
	// 登录状态检查
	isLogin := false
	if meta.Principal != nil && meta.Principal.HasRole(rpc.RoleAdmin) {
		isLogin = true
	}

	// 仅在未登录时需要 Hidden 信息做过滤
	if !isLogin && clients.IsHiddenClient(params.UUID) {
		return nil, rpc.MakeError(rpc.InvalidParams, "UUID is required", params) //防止未登录用户获取隐藏客户端数据
	}

	reports := agent_runtime.GetRecentReports(params.UUID)

	// 扁平化为 { count, records: [] }
	type flatRecord struct {
		Client         string    `json:"client"`
		Time           time.Time `json:"time"`
		Cpu            float32   `json:"cpu"`
		Gpu            float32   `json:"gpu"`
		Ram            int64     `json:"ram"`
		RamTotal       int64     `json:"ram_total"`
		Swap           int64     `json:"swap"`
		SwapTotal      int64     `json:"swap_total"`
		Load           float32   `json:"load"`
		Temp           float32   `json:"temp"`
		Disk           int64     `json:"disk"`
		DiskTotal      int64     `json:"disk_total"`
		NetIn          int64     `json:"net_in"`
		NetOut         int64     `json:"net_out"`
		NetTotalUp     int64     `json:"net_total_up"`
		NetTotalDown   int64     `json:"net_total_down"`
		Process        int       `json:"process"`
		Connections    int       `json:"connections"`
		ConnectionsUdp int       `json:"connections_udp"`
	}

	resp := struct {
		Count   int          `json:"count"`
		Records []flatRecord `json:"records"`
	}{
		Count:   0,
		Records: []flatRecord{},
	}

	if len(reports) == 0 {
		return resp, nil
	}

	resp.Records = make([]flatRecord, 0, len(reports))
	for _, r := range reports {
		fr := flatRecord{
			Client:         params.UUID,
			Time:           r.UpdatedAt,
			Cpu:            float32(r.CPU.Usage),
			Gpu:            gpuUsageFromReport(&r),
			Ram:            r.Ram.Used,
			RamTotal:       r.Ram.Total,
			Swap:           r.Swap.Used,
			SwapTotal:      r.Swap.Total,
			Load:           float32(r.Load.Load1),
			Temp:           0,
			Disk:           r.Disk.Used,
			DiskTotal:      r.Disk.Total,
			NetIn:          r.Network.Down,
			NetOut:         r.Network.Up,
			NetTotalUp:     r.Network.TotalUp,
			NetTotalDown:   r.Network.TotalDown,
			Process:        r.Process,
			Connections:    r.Connections.TCP + r.Connections.UDP,
			ConnectionsUdp: r.Connections.UDP,
		}
		resp.Records = append(resp.Records, fr)
	}
	resp.Count = len(resp.Records)
	return resp, nil
}
