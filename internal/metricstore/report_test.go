package metricstore

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/komari-monitor/komari/database/models"
	"github.com/komari-monitor/komari/pkg/metric"
	v1 "github.com/komari-monitor/komari/protocol/v1"
)

func useReportTestStore(t *testing.T, policy *metric.RollupPolicy) *metric.Store {
	t.Helper()
	ctx := context.Background()
	opts := []metric.Option{metric.WithMaxOpenConns(1)}
	if policy != nil {
		opts = append(opts, metric.WithRollupPolicy(*policy))
	}
	dsn := fmt.Sprintf("file:report-%d?mode=memory&cache=shared", time.Now().UnixNano())
	s, err := metric.Open(ctx, metric.SQLite(dsn, opts...))
	if err != nil {
		t.Fatalf("open metric store: %v", err)
	}
	if err := createMetricDefinitions(ctx, s); err != nil {
		_ = s.Close()
		t.Fatalf("create metric definitions: %v", err)
	}

	storeMu.Lock()
	previous := store
	store = s
	storeMu.Unlock()
	clearReportTrafficStates()
	t.Cleanup(func() {
		clearReportTrafficStates()
		storeMu.Lock()
		store = previous
		storeMu.Unlock()
		_ = s.Close()
	})
	return s
}

func TestWriteReportStoresMinuteMetricsAndResetAwareTraffic(t *testing.T) {
	ctx := context.Background()
	policy := defaultRollupPolicy()
	s := useReportTestStore(t, &policy)
	base := time.Now().UTC().Truncate(time.Minute).Add(5 * time.Second)
	now := base.Add(45 * time.Second)

	report := v1.Report{
		UUID:        "node-a",
		UpdatedAt:   base,
		CPU:         v1.CPUReport{Usage: 12.5},
		Ram:         v1.RamReport{Used: 100, Total: 1000},
		Swap:        v1.RamReport{Used: 20, Total: 200},
		Load:        v1.LoadReport{Load1: 0.5},
		Disk:        v1.DiskReport{Used: 300, Total: 3000},
		Network:     v1.NetworkReport{Up: 3, Down: 4, TotalUp: 100, TotalDown: 200},
		Process:     7,
		Connections: v1.ConnectionsReport{TCP: 8, UDP: 9},
		GPU: &v1.GPUDetailReport{
			AverageUsage: 25,
			DetailedInfo: []v1.GPUDeviceInfo{{
				Name: "GPU 0", MemoryUsed: 400, MemoryTotal: 800, Utilization: 30, Temperature: 55,
			}},
		},
	}
	if _, err := WriteReport(ctx, report); err != nil {
		t.Fatalf("write first report: %v", err)
	}

	report.UpdatedAt = base.Add(3 * time.Second)
	report.Network.TotalUp = 150
	report.Network.TotalDown = 260
	if _, err := WriteReport(ctx, report); err != nil {
		t.Fatalf("write second report: %v", err)
	}

	report.UpdatedAt = base.Add(6 * time.Second)
	report.Network.TotalUp = 20
	report.Network.TotalDown = 30
	if _, err := WriteReport(ctx, report); err != nil {
		t.Fatalf("write reset report: %v", err)
	}

	assertMetricValues(t, s, MetricTrafficUp, report.UUID, base.Add(-time.Second), base.Add(time.Minute), []float64{0, 50, 20})
	assertMetricValues(t, s, MetricTrafficDown, report.UUID, base.Add(-time.Second), base.Add(time.Minute), []float64{0, 60, 30})
	assertMetricValues(t, s, MetricNetTotalUp, report.UUID, base.Add(-time.Second), base.Add(time.Minute), []float64{100, 150, 20})
	assertMetricAggregate(t, s, MetricTrafficUp, report.UUID, base.Add(-time.Second), base.Add(time.Minute), metric.AggSum, 70, 3)
	assertMetricAggregate(t, s, MetricTrafficDown, report.UUID, base.Add(-time.Second), base.Add(time.Minute), metric.AggSum, 90, 3)

	gpuPoints, err := s.Query(ctx, metric.Query{
		MetricName: MetricGPUDeviceUsage,
		EntityID:   report.UUID,
		Start:      base.Add(-time.Second),
		End:        base.Add(time.Minute),
		Tags:       map[string]string{"device_index": "0"},
		Order:      metric.OrderAsc,
	})
	if err != nil {
		t.Fatalf("query GPU points: %v", err)
	}
	if len(gpuPoints) != 3 || !gpuPoints[2].Timestamp.Equal(base.Add(6*time.Second)) || gpuPoints[2].Tags["device_name"] != "GPU 0" {
		t.Fatalf("unexpected GPU points: %#v", gpuPoints)
	}

	if _, err := s.Compact(ctx, now); err != nil {
		t.Fatalf("compact reports: %v", err)
	}
	deleteReportTrafficState(report.UUID)
	report.UpdatedAt = now
	report.Network.TotalUp = 35
	report.Network.TotalDown = 50
	if _, err := WriteReport(ctx, report); err != nil {
		t.Fatalf("write after restoring rollup baseline: %v", err)
	}
	assertMetricValues(t, s, MetricTrafficUp, report.UUID, now.Add(-time.Second), now.Add(time.Second), []float64{15})
	assertMetricValues(t, s, MetricTrafficDown, report.UUID, now.Add(-time.Second), now.Add(time.Second), []float64{20})
}

func TestWriteReportSkipsMetricsWithoutAgentData(t *testing.T) {
	ctx := context.Background()
	s := useReportTestStore(t, nil)
	timestamp := time.Now().UTC()
	if _, err := WriteReport(ctx, v1.Report{
		UUID: "node-without-gpu", UpdatedAt: timestamp,
	}); err != nil {
		t.Fatalf("write report: %v", err)
	}
	points, err := s.Query(ctx, metric.Query{
		MetricName: MetricGPU, EntityID: "node-without-gpu",
		Start: timestamp.Add(-time.Second), End: timestamp.Add(time.Second),
	})
	if err != nil {
		t.Fatalf("query GPU metric: %v", err)
	}
	if len(points) != 0 {
		t.Fatalf("GPU metric was written without GPU data: %#v", points)
	}
}

func TestReportBatcherFlushesQueuedReports(t *testing.T) {
	ctx := context.Background()
	s := useReportTestStore(t, nil)
	StartReportBatcher()
	t.Cleanup(func() {
		if err := StopReportBatcher(ctx); err != nil {
			t.Errorf("stop report batcher: %v", err)
		}
	})

	base := time.Now().UTC().Truncate(time.Minute).Add(10 * time.Second)
	first := v1.Report{
		UUID:      "batched-node",
		UpdatedAt: base,
		CPU:       v1.CPUReport{Usage: 10},
		Network:   v1.NetworkReport{TotalUp: 100, TotalDown: 200},
	}
	second := first
	second.UpdatedAt = base.Add(3 * time.Second)
	second.CPU.Usage = 20
	second.Network.TotalUp = 150
	second.Network.TotalDown = 260

	if _, err := WriteReport(ctx, first); err != nil {
		t.Fatalf("queue first report: %v", err)
	}
	if _, err := WriteReport(ctx, second); err != nil {
		t.Fatalf("queue second report: %v", err)
	}
	points, err := s.Query(ctx, metric.Query{
		MetricName: MetricCPU,
		EntityID:   first.UUID,
		Start:      base.Add(-time.Second),
		End:        base.Add(time.Minute),
		Order:      metric.OrderAsc,
	})
	if err != nil {
		t.Fatalf("query before flush: %v", err)
	}
	if len(points) != 0 {
		t.Fatalf("queued reports were written before flush: %#v", points)
	}

	if err := FlushReportBatch(ctx); err != nil {
		t.Fatalf("flush report batch: %v", err)
	}
	assertMetricValues(t, s, MetricCPU, first.UUID, base.Add(-time.Second), base.Add(time.Minute), []float64{10, 20})
	assertMetricValues(t, s, MetricTrafficUp, first.UUID, base.Add(-time.Second), base.Add(time.Minute), []float64{0, 50})
	assertMetricValues(t, s, MetricTrafficDown, first.UUID, base.Add(-time.Second), base.Add(time.Minute), []float64{0, 60})
	assertMetricAggregate(t, s, MetricCPU, first.UUID, base.Add(-time.Second), base.Add(time.Minute), metric.AggAvg, 15, 2)
}

func TestPingBatcherFlushesLatencyAndLossTogether(t *testing.T) {
	ctx := context.Background()
	s := useReportTestStore(t, nil)
	StartReportBatcher()
	t.Cleanup(func() {
		if err := StopReportBatcher(ctx); err != nil {
			t.Errorf("stop report batcher: %v", err)
		}
	})

	base := time.Now().UTC().Truncate(time.Second)
	records := []models.PingRecord{
		{Client: "ping-node", TaskId: 7, Time: base, Value: 24},
		{Client: "ping-node", TaskId: 7, Time: base.Add(time.Minute), Value: -1},
		{Client: "ping-node", TaskId: 8, Time: base, Value: 31},
	}
	for _, record := range records {
		if err := WritePingRecord(ctx, record); err != nil {
			t.Fatalf("queue ping record: %v", err)
		}
	}

	for _, name := range []string{MetricPingLatency, MetricPingLoss} {
		points, err := s.Query(ctx, metric.Query{
			MetricName: name,
			EntityID:   "ping-node",
			Start:      base.Add(-time.Second),
			End:        base.Add(2 * time.Minute),
		})
		if err != nil {
			t.Fatalf("query queued %s points: %v", name, err)
		}
		if len(points) != 0 {
			t.Fatalf("queued %s points were written before flush: %#v", name, points)
		}
	}

	if err := FlushReportBatch(ctx); err != nil {
		t.Fatalf("flush ping batch: %v", err)
	}

	latency, err := s.Query(ctx, metric.Query{
		MetricName: MetricPingLatency,
		EntityID:   "ping-node",
		Tags:       map[string]string{"task_id": "7"},
		Start:      base.Add(-time.Second),
		End:        base.Add(2 * time.Minute),
		Order:      metric.OrderAsc,
	})
	if err != nil {
		t.Fatalf("query flushed latency points: %v", err)
	}
	if len(latency) != 2 || latency[0].Value != 24 || latency[1].Value != -1 {
		t.Fatalf("latency points = %#v, want both original samples", latency)
	}

	loss, err := s.Query(ctx, metric.Query{
		MetricName: MetricPingLoss,
		EntityID:   "ping-node",
		Tags:       map[string]string{"task_id": "7"},
		Start:      base.Add(-time.Second),
		End:        base.Add(2 * time.Minute),
		Order:      metric.OrderAsc,
	})
	if err != nil {
		t.Fatalf("query flushed loss points: %v", err)
	}
	if len(loss) != 2 || loss[0].Value != 0 || loss[1].Value != 1 {
		t.Fatalf("loss points = %#v, want success and loss samples", loss)
	}
}

func TestReportBatchKeepsEverySample(t *testing.T) {
	ctx := context.Background()
	s := useReportTestStore(t, nil)
	base := time.Now().UTC().Truncate(time.Second)
	pending := []v1.Report{
		{UUID: "node-a", UpdatedAt: base, CPU: v1.CPUReport{Usage: 10}, Network: v1.NetworkReport{TotalUp: 100}},
		{UUID: "node-a", UpdatedAt: base, CPU: v1.CPUReport{Usage: 20}, Network: v1.NetworkReport{TotalUp: 150}},
		{UUID: "node-b", UpdatedAt: base, CPU: v1.CPUReport{Usage: 30}, Network: v1.NetworkReport{TotalUp: 200}},
		{UUID: "node-b", UpdatedAt: base.Add(time.Second), CPU: v1.CPUReport{Usage: 40}, Network: v1.NetworkReport{TotalUp: 260}},
	}

	if err := writePendingReports(ctx, &pending); err != nil {
		t.Fatalf("write report batch: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("pending reports = %d, want 0", len(pending))
	}
	assertMetricValues(t, s, MetricCPU, "node-a", base.Add(-time.Second), base.Add(time.Minute), []float64{10, 20})
	assertMetricValues(t, s, MetricCPU, "node-b", base.Add(-time.Second), base.Add(time.Minute), []float64{30, 40})
}

func TestReportQueueFullReturnsError(t *testing.T) {
	ctx := context.Background()
	useReportTestStore(t, nil)
	worker := &reportBatchWorker{
		queue:    make(chan v1.Report, 1),
		requests: make(chan reportBatchRequest, 1),
		done:     make(chan struct{}),
	}
	worker.queue <- v1.Report{UUID: "already-queued"}
	reportBatcherMu.Lock()
	reportBatcher = worker
	reportBatcherMu.Unlock()
	t.Cleanup(func() {
		reportBatcherMu.Lock()
		if reportBatcher == worker {
			reportBatcher = nil
		}
		reportBatcherMu.Unlock()
	})

	report := v1.Report{
		UUID:      "realtime-node",
		UpdatedAt: time.Now().UTC(),
	}
	_, err := WriteReport(ctx, report)
	if !errors.Is(err, ErrReportBatchQueueFull) {
		t.Fatalf("queue-full error = %v, want %v", err, ErrReportBatchQueueFull)
	}
}

func TestRecordReconstructionUsesMetricSpecificAggregation(t *testing.T) {
	ctx := context.Background()
	s := useReportTestStore(t, nil)
	base := time.Now().UTC().Truncate(time.Minute)
	entityID := "node-aggregation"
	points := []metric.Point{
		{MetricName: MetricCPU, EntityID: entityID, Timestamp: base.Add(time.Second), Value: 10},
		{MetricName: MetricCPU, EntityID: entityID, Timestamp: base.Add(2 * time.Second), Value: 30},
		{MetricName: MetricNetTotalUp, EntityID: entityID, Timestamp: base.Add(time.Second), Value: 100},
		{MetricName: MetricNetTotalUp, EntityID: entityID, Timestamp: base.Add(2 * time.Second), Value: 200},
		{MetricName: MetricTrafficUp, EntityID: entityID, Timestamp: base.Add(time.Second), Value: 10},
		{MetricName: MetricTrafficUp, EntityID: entityID, Timestamp: base.Add(2 * time.Second), Value: 20},
	}
	if err := s.WriteBatch(ctx, points); err != nil {
		t.Fatalf("write points: %v", err)
	}

	records, err := GetRecordsByClientAndTime(ctx, entityID, base, base.Add(time.Hour))
	if err != nil {
		t.Fatalf("reconstruct records: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("records = %#v, want one bucket", records)
	}
	if records[0].Cpu != 20 || records[0].NetTotalUp != 200 || records[0].TrafficUp != 30 {
		t.Fatalf("unexpected aggregation result: %#v", records[0])
	}
}

func TestTrafficCounterDelta(t *testing.T) {
	tests := []struct {
		name     string
		current  int64
		previous int64
		want     int64
	}{
		{name: "previous zero", current: 120, previous: 0, want: 120},
		{name: "monotonic counter", current: 250, previous: 200, want: 50},
		{name: "unchanged counter", current: 100, previous: 100, want: 0},
		{name: "counter reset", current: 15, previous: 250, want: 15},
		{name: "negative current", current: -1, previous: 100, want: 0},
		{name: "negative previous", current: 15, previous: -1, want: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := TrafficCounterDelta(test.current, test.previous); got != test.want {
				t.Fatalf("TrafficCounterDelta(%d, %d) = %d, want %d", test.current, test.previous, got, test.want)
			}
		})
	}
}

func TestWriteReportNormalizesReceiveTimeToUTC(t *testing.T) {
	ctx := context.Background()
	s := useReportTestStore(t, nil)
	local := time.FixedZone("UTC+8", 8*60*60)
	receiveTime := time.Now().In(local).Add(-10 * time.Second)
	report := v1.Report{
		UUID:      "utc-report",
		UpdatedAt: receiveTime,
		CPU:       v1.CPUReport{Usage: 10},
		Network:   v1.NetworkReport{TotalUp: 1, TotalDown: 2},
	}

	saved, err := WriteReport(ctx, report)
	if err != nil {
		t.Fatalf("write report: %v", err)
	}
	if !saved.UpdatedAt.Equal(receiveTime) || saved.UpdatedAt.Location() != time.UTC {
		t.Fatalf("saved receive time = %s (%s), want UTC", saved.UpdatedAt, saved.UpdatedAt.Location())
	}
	points, err := s.Query(ctx, metric.Query{
		MetricName: MetricCPU,
		EntityID:   report.UUID,
		Start:      receiveTime.Add(-time.Nanosecond),
		End:        receiveTime.Add(time.Nanosecond),
	})
	if err != nil {
		t.Fatalf("query stored point: %v", err)
	}
	if len(points) != 1 || points[0].Timestamp.Location() != time.UTC || points[0].Timestamp.UnixMilli() != receiveTime.UnixMilli() {
		t.Fatalf("stored points = %#v, want one UTC millisecond point", points)
	}
}

func assertMetricValues(t *testing.T, s *metric.Store, metricName, entityID string, start, end time.Time, want []float64) {
	t.Helper()
	points, err := s.Query(context.Background(), metric.Query{
		MetricName: metricName,
		EntityID:   entityID,
		Start:      start,
		End:        end,
		Order:      metric.OrderAsc,
	})
	if err != nil {
		t.Fatalf("query %s: %v", metricName, err)
	}
	if len(points) != len(want) {
		t.Fatalf("%s point count = %d, want %d: %#v", metricName, len(points), len(want), points)
	}
	for i := range want {
		if points[i].Value != want[i] {
			t.Fatalf("%s point %d = %v, want %v", metricName, i, points[i].Value, want[i])
		}
	}
}

func assertMetricAggregate(t *testing.T, s *metric.Store, metricName, entityID string, start, end time.Time, aggregation metric.Aggregation, want float64, wantCount int) {
	t.Helper()
	points, err := s.Series(context.Background(), metric.AggregateQuery{
		Query:       metric.Query{MetricName: metricName, EntityID: entityID, Start: start, End: end},
		Aggregation: aggregation, Interval: time.Minute, PreserveSeries: true,
	}, end)
	if err != nil {
		t.Fatalf("aggregate %s: %v", metricName, err)
	}
	if len(points) != 1 || points[0].Value != want || points[0].Count != wantCount {
		t.Fatalf("aggregate %s = %#v, want value=%v count=%d", metricName, points, want, wantCount)
	}
}

func TestWriteReportStoresMiningMetricsAndHistory(t *testing.T) {
	ctx := context.Background()
	policy := defaultRollupPolicy()
	s := useReportTestStore(t, &policy)
	base := time.Now().UTC().Truncate(time.Second)

	report := v1.Report{
		UUID:      "rig-a",
		UpdatedAt: base,
		Mining: &v1.MiningReport{
			Algorithm:    "pearlhash",
			Pool:         "prl-eu.kryptex.network:7048",
			Wallet:       "krxXGNKMD4/home-win",
			Hashrate1Min: 62403052604616.36,
			PowerW:       149,
			Temperature:  74,
			FanPercent:    86,
			SharesValid:  64,
			SharesInvalid: 1,
			HwErrors:     0,
			PoolLatency:  232,
		},
	}
	if _, err := WriteReport(ctx, report); err != nil {
		t.Fatalf("write mining report: %v", err)
	}
	if err := FlushReportBatch(ctx); err != nil {
		t.Fatalf("flush mining report batch: %v", err)
	}

	points, err := s.Query(ctx, metric.Query{
		MetricName: MetricMiningHashrate,
		EntityID:   report.UUID,
		Start:      base.Add(-time.Second),
		End:        base.Add(time.Second),
		Order:      metric.OrderAsc,
	})
	if err != nil {
		t.Fatalf("query mining hashrate: %v", err)
	}
	if len(points) != 1 || points[0].Value != 62403052604616.36 {
		t.Fatalf("unexpected mining hashrate points: %#v", points)
	}
	if points[0].Tags["rig"] != "krxXGNKMD4/home-win" || points[0].Tags["algorithm"] != "pearlhash" {
		t.Fatalf("unexpected mining tags: %#v", points[0].Tags)
	}

	records, err := GetMiningRecordsByClientAndTime(ctx, report.UUID, base.Add(-time.Minute), base.Add(time.Minute))
	if err != nil {
		t.Fatalf("query mining history: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("mining records = %d, want 1", len(records))
	}
	rec := records[0]
	if rec.Rig != "krxXGNKMD4/home-win" || rec.Algorithm != "pearlhash" || rec.Pool != "prl-eu.kryptex.network:7048" {
		t.Fatalf("mining record identity tags lost: %#v", rec)
	}
	if rec.Hashrate != 62403052604616.36 || rec.Power != 149 || rec.Temperature != 74 ||
		rec.Fan != 86 || rec.SharesValid != 64 || rec.SharesInvalid != 1 || rec.PoolLatency != 232 {
		t.Fatalf("unexpected mining record: %#v", rec)
	}
}

// 多桶场景：history 记录经 recordMap 合并后 map 迭代顺序随机，返回前必须按
// rig+时间排序（前端 records[length-1] 取 latest、XAxis 按数组顺序绘图）。
// 每个 rig 每个时刻只有一份快照（agent 一节点一上报），钱包切换后 rig 随时间交错。
func TestGetMiningRecordsSortedByRigAndTime(t *testing.T) {
	ctx := context.Background()
	policy := defaultRollupPolicy()
	useReportTestStore(t, &policy)
	// 桶边界按 interval 对齐（bucketStartMillis），base 对齐到分钟让时间戳落在可预期的桶
	base := time.Now().UTC().Truncate(time.Minute)

	// rig-b 先起挖，之后切到 rig-a：两条 series 的时间桶交错
	writes := []struct {
		ts  time.Time
		rig string
	}{
		{base.Add(1 * time.Minute), "rig-b"},
		{base.Add(2 * time.Minute), "rig-b"},
		{base.Add(3 * time.Minute), "rig-a"},
		{base.Add(4 * time.Minute), "rig-a"},
	}
	for _, w := range writes {
		report := v1.Report{
			UUID:      "mining-node",
			UpdatedAt: w.ts,
			Mining: &v1.MiningReport{
				Algorithm:    "pearlhash",
				Pool:         "prl-eu.kryptex.network:7048",
				Wallet:       w.rig,
				Hashrate1Min: 1e12,
			},
		}
		if _, err := WriteReport(ctx, report); err != nil {
			t.Fatalf("write report %s@%d: %v", w.rig, w.ts.Unix(), err)
		}
	}
	if err := FlushReportBatch(ctx); err != nil {
		t.Fatalf("flush: %v", err)
	}

	// 乱序与否取决于 map 迭代顺序：多次独立查询重复断言锁死排序
	for attempt := 0; attempt < 20; attempt++ {
		records, err := GetMiningRecordsByClientAndTime(ctx, "mining-node", base, base.Add(5*time.Minute))
		if err != nil {
			t.Fatalf("query mining history: %v", err)
		}
		if len(records) != 4 {
			t.Fatalf("mining records = %d, want 4", len(records))
		}
		for i := 1; i < len(records); i++ {
			prev, cur := records[i-1], records[i]
			if prev.Rig != cur.Rig && prev.Rig >= cur.Rig {
				t.Fatalf("records not sorted by rig: %q after %q", cur.Rig, prev.Rig)
			}
			if prev.Rig == cur.Rig && !prev.Time.Before(cur.Time) {
				t.Fatalf("records not sorted by time within rig %q: %v after %v", cur.Rig, cur.Time, prev.Time)
			}
		}
		// rig 按字典序分组（rig-a 在前），组内按时间升序（排序语义的两层都必须成立）
		if records[0].Rig != "rig-a" || !records[0].Time.Equal(base.Add(3*time.Minute)) {
			t.Fatalf("first record = %s@%v, want rig-a@%v", records[0].Rig, records[0].Time, base.Add(3*time.Minute))
		}
		last := records[len(records)-1]
		if last.Rig != "rig-b" || !last.Time.Equal(base.Add(2*time.Minute)) {
			t.Fatalf("last record = %s@%v, want rig-b@%v", last.Rig, last.Time, base.Add(2*time.Minute))
		}
	}
}

// 同款排序回归覆盖 GPU 历史路径（按 device_index+时间；每个设备每桶一条，
// 多设备 series 交错时 map 迭代顺序同样随机）。
func TestGetGPURecordsSortedByDeviceAndTime(t *testing.T) {
	ctx := context.Background()
	policy := defaultRollupPolicy()
	useReportTestStore(t, &policy)
	// 桶边界按 interval 对齐（bucketStartMillis），base 对齐到分钟让时间戳落在可预期的桶
	base := time.Now().UTC().Truncate(time.Minute)

	for _, ts := range []time.Time{
		base.Add(1 * time.Minute),
		base.Add(2 * time.Minute),
		base.Add(3 * time.Minute),
	} {
		report := v1.Report{
			UUID:      "gpu-node",
			UpdatedAt: ts,
			GPU: &v1.GPUDetailReport{
				AverageUsage: 15,
				DetailedInfo: []v1.GPUDeviceInfo{
					{Utilization: 10, Temperature: 60, MemoryUsed: 1024, MemoryTotal: 8192, Name: "GPU B"},
					{Utilization: 20, Temperature: 50, MemoryUsed: 2048, MemoryTotal: 8192, Name: "GPU A"},
				},
			},
		}
		if _, err := WriteReport(ctx, report); err != nil {
			t.Fatalf("write gpu report: %v", err)
		}
	}
	if err := FlushReportBatch(ctx); err != nil {
		t.Fatalf("flush: %v", err)
	}

	for attempt := 0; attempt < 20; attempt++ {
		records, err := GetGPURecordsByClientAndTime(ctx, "gpu-node", base, base.Add(4*time.Minute))
		if err != nil {
			t.Fatalf("query gpu history: %v", err)
		}
		if len(records) != 6 {
			t.Fatalf("gpu records = %d, want 6 (2 devices x 3 buckets)", len(records))
		}
		for i := 1; i < len(records); i++ {
			prev, cur := records[i-1], records[i]
			if prev.DeviceIndex != cur.DeviceIndex && prev.DeviceIndex >= cur.DeviceIndex {
				t.Fatalf("records not sorted by device index: %d after %d", cur.DeviceIndex, prev.DeviceIndex)
			}
			if prev.DeviceIndex == cur.DeviceIndex && !prev.Time.Before(cur.Time) {
				t.Fatalf("records not sorted by time within device %d: %v after %v", cur.DeviceIndex, cur.Time, prev.Time)
			}
		}
	}
}

func TestGetGPURecordsPreservesDeviceTagsAcrossRollups(t *testing.T) {
	ctx := context.Background()
	policy := defaultRollupPolicy()
	useReportTestStore(t, &policy)
	base := time.Now().UTC().Truncate(time.Second)

	report := v1.Report{
		UUID:      "gpu-node",
		UpdatedAt: base,
		GPU: &v1.GPUDetailReport{
			Count:        1,
			AverageUsage: 42,
			DetailedInfo: []v1.GPUDeviceInfo{{
				Name:        "NVIDIA GeForce RTX 3070",
				MemoryTotal: 8589934592,
				MemoryUsed:  2147483648,
				Utilization: 96,
				Temperature: 71,
			}},
		},
	}
	if _, err := WriteReport(ctx, report); err != nil {
		t.Fatalf("write gpu report: %v", err)
	}
	if err := FlushReportBatch(ctx); err != nil {
		t.Fatalf("flush gpu report batch: %v", err)
	}

	records, err := GetGPURecordsByClientAndTime(ctx, report.UUID, base.Add(-time.Minute), base.Add(time.Minute))
	if err != nil {
		t.Fatalf("query gpu history: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("gpu records = %d, want 1", len(records))
	}
	rec := records[0]
	if rec.DeviceIndex != 0 || rec.DeviceName != "NVIDIA GeForce RTX 3070" {
		t.Fatalf("gpu record identity tags lost: %#v", rec)
	}
	if rec.Utilization != 96 || rec.Temperature != 71 || rec.MemUsed != 2147483648 || rec.MemTotal != 8589934592 {
		t.Fatalf("unexpected gpu record: %#v", rec)
	}
}
