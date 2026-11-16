package metricstore

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	logger "github.com/dengyie/mangohub/hub/utils/log"

	"github.com/dengyie/mangohub/hub/database/models"
	"github.com/dengyie/mangohub/hub/pkg/metric"
	v2 "github.com/dengyie/mangohub/protocol/v2"
)

type reportTrafficState struct {
	mu sync.Mutex

	reportTrafficValues
}

type reportTrafficValues struct {
	initialized bool
	timestamp   time.Time
	hasUp       bool
	totalUp     int64
	hasDown     bool
	totalDown   int64
}

var reportTrafficStates sync.Map

const (
	reportBatchInterval     = 3 * time.Second
	reportBatchQueueSize    = 4096
	pingBatchMaxRecords     = 512
	reportBatchMaxRecords   = 512
	reportBatchWriteTimeout = 10 * time.Second

	// reportQueueFullLogInterval throttles the queue-full warning: a stalled
	// write pipeline drops one live sample per node per ~3s tick, which would
	// otherwise flood the log while saying nothing new.
	reportQueueFullLogInterval = time.Minute
)

var (
	reportBatcherMu sync.Mutex
	reportBatcher   *reportBatchWorker
)

var (
	ErrReportBatchQueueFull = errors.New("metric report batch queue is full")
	ErrReportBatchStopped   = errors.New("metric report batcher is stopped")
	ErrPingBatchQueueFull   = errors.New("ping batch queue is full")

	// ErrStoreOperationPaused is returned by writeReportBatch while a store
	// maintenance (VACUUM) or store-to-store migration holds the exclusive
	// operation gate. The batcher keeps its pending backlog and retries next
	// tick; run() logs this state as throttled info instead of an error per
	// tick, so a 30-minute migration no longer spams "failed to flush"
	// every 3 seconds.
	ErrStoreOperationPaused = errors.New("metric store operation in progress; report flush paused")
)

// reportPauseLogState throttles the "flush paused by store operation" notice.
var reportPauseLogState struct {
	mu      sync.Mutex
	lastLog time.Time
	resumed bool
}

// logFlushPaused logs the paused state at most once per interval and logs a
// single "resumed" line when the pipeline picks back up.
func logFlushPaused() {
	reportPauseLogState.mu.Lock()
	defer reportPauseLogState.mu.Unlock()
	now := time.Now()
	if reportPauseLogState.lastLog.IsZero() || now.Sub(reportPauseLogState.lastLog) >= reportQueueFullLogInterval {
		logger.Infof("metricstore", "metric report flush paused by store operation (maintenance/migration); backlog retained and retried per tick")
		reportPauseLogState.lastLog = now
		reportPauseLogState.resumed = true
	}
}

func logFlushResumed() {
	reportPauseLogState.mu.Lock()
	defer reportPauseLogState.mu.Unlock()
	if reportPauseLogState.resumed {
		logger.Infof("metricstore", "metric report flush resumed; draining retained backlog")
		reportPauseLogState.resumed = false
		reportPauseLogState.lastLog = time.Time{}
	}
}

// reportQueueFullLogState throttles the queue-full warning to one line per
// interval with a suppressed-count suffix, mirroring the duplicate-rejection
// log throttle in web/api/client.
var reportQueueFullLogState struct {
	mu         sync.Mutex
	lastWarn   time.Time
	suppressed int
}

func logQueueFull(kind string) {
	reportQueueFullLogState.mu.Lock()
	defer reportQueueFullLogState.mu.Unlock()
	now := time.Now()
	if !reportQueueFullLogState.lastWarn.IsZero() && now.Sub(reportQueueFullLogState.lastWarn) < reportQueueFullLogInterval {
		reportQueueFullLogState.suppressed++
		return
	}
	suppressed := reportQueueFullLogState.suppressed
	reportQueueFullLogState.lastWarn = now
	reportQueueFullLogState.suppressed = 0
	if suppressed > 0 {
		logger.Warnf("metricstore", "%s queue is full; dropping live samples until the write pipeline recovers (+%d more dropped within %s)", kind, suppressed, reportQueueFullLogInterval)
		return
	}
	logger.Warnf("metricstore", "%s queue is full; dropping live samples until the write pipeline recovers", kind)
}

type reportBatchRequest struct {
	ctx  context.Context
	done chan error
	stop bool
}

type reportBatchWorker struct {
	mu        sync.Mutex
	queue     chan v2.Report
	pingQueue chan models.PingRecord
	requests  chan reportBatchRequest
	done      chan struct{}
	stopping  bool
}

// StartReportBatcher starts the shared report writer. Exact samples are kept in
// the short raw window while the same writes update in-memory minute buckets.
func StartReportBatcher() {
	reportBatcherMu.Lock()
	defer reportBatcherMu.Unlock()
	if reportBatcher != nil {
		return
	}
	worker := &reportBatchWorker{
		queue:     make(chan v2.Report, reportBatchQueueSize),
		pingQueue: make(chan models.PingRecord, reportBatchQueueSize),
		requests:  make(chan reportBatchRequest, 1),
		done:      make(chan struct{}),
	}
	reportBatcher = worker
	go worker.run()
}

// StopReportBatcher stops the report writer after flushing all queued reports.
func StopReportBatcher(ctx context.Context) error {
	reportBatcherMu.Lock()
	worker := reportBatcher
	if worker == nil {
		reportBatcherMu.Unlock()
		return nil
	}
	worker.mu.Lock()
	worker.stopping = true
	worker.mu.Unlock()
	reportBatcherMu.Unlock()

	request := reportBatchRequest{ctx: ctx, done: make(chan error, 1), stop: true}
	select {
	case worker.requests <- request:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-request.done:
		<-worker.done
		reportBatcherMu.Lock()
		if reportBatcher == worker {
			reportBatcher = nil
		}
		reportBatcherMu.Unlock()
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// FlushReportBatch synchronously flushes the current queue. It is useful for
// controlled handoff points and deterministic tests; normal operation uses the
// worker ticker and the active mode's flush interval.
func FlushReportBatch(ctx context.Context) error {
	reportBatcherMu.Lock()
	worker := reportBatcher
	reportBatcherMu.Unlock()
	if worker == nil {
		return nil
	}
	request := reportBatchRequest{ctx: ctx, done: make(chan error, 1)}
	worker.mu.Lock()
	stopping := worker.stopping
	worker.mu.Unlock()
	if stopping {
		return ErrReportBatchStopped
	}
	select {
	case worker.requests <- request:
	case <-worker.done:
		return ErrReportBatchStopped
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-request.done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// WriteReport persists one agent report and adds it to in-memory minute
// summaries using the same server receive time. Traffic deltas remain summable
// after rollup.
func WriteReport(ctx context.Context, report v2.Report) (v2.Report, error) {
	if report.UUID == "" {
		return v2.Report{}, fmt.Errorf("report UUID is required")
	}
	if report.UpdatedAt.IsZero() {
		return v2.Report{}, fmt.Errorf("report receive time is required")
	}
	report.UpdatedAt = report.UpdatedAt.UTC()
	if GetStore() == nil {
		return v2.Report{}, fmt.Errorf("metric store not enabled")
	}

	reportBatcherMu.Lock()
	worker := reportBatcher
	reportBatcherMu.Unlock()
	if worker != nil {
		if err := worker.enqueue(ctx, report); err != nil {
			if errors.Is(err, ErrReportBatchQueueFull) {
				logQueueFull("metric report batch")
			}
			return v2.Report{}, err
		}
		return report, nil
	}

	saved, err := writeReportBatch(ctx, []v2.Report{report})
	if err != nil {
		return v2.Report{}, err
	}
	return saved[0], nil
}

func (w *reportBatchWorker) enqueue(ctx context.Context, report v2.Report) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stopping {
		return ErrReportBatchStopped
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case w.queue <- report:
		return nil
	default:
		return ErrReportBatchQueueFull
	}
}

func (w *reportBatchWorker) run() {
	ticker := time.NewTicker(reportBatchInterval)
	defer ticker.Stop()

	var pending []v2.Report
	var pendingPings []models.PingRecord
	for {
		select {
		case request := <-w.requests:
			pending = append(pending, drainReportQueue(w.queue, reportBatchQueueSize)...)
			pendingPings = append(pendingPings, drainPingQueue(w.pingQueue, reportBatchQueueSize)...)
			err := errors.Join(
				writePendingReports(request.ctx, &pending),
				writePendingPingRecords(request.ctx, &pendingPings),
			)
			if request.stop {
				if err != nil {
					logger.Errorf("metricstore", "failed to flush metric report batch during shutdown: %v", err)
				}
				close(w.done)
				request.done <- err
				return
			}
			request.done <- err
		case <-ticker.C:
			pendingPings = append(pendingPings, drainPingQueue(w.pingQueue, reportBatchQueueSize)...)
			if err := writePendingPingRecords(context.Background(), &pendingPings); err != nil {
				logger.Errorf("metricstore", "failed to flush ping batch: %v", err)
			}
			pending = append(pending, drainReportQueue(w.queue, reportBatchQueueSize)...)
			if err := writePendingReports(context.Background(), &pending); err != nil {
				if errors.Is(err, ErrStoreOperationPaused) {
					logFlushPaused()
				} else {
					logFlushResumed()
					logger.Errorf("metricstore", "failed to flush metric report batch: %v", err)
				}
			} else {
				logFlushResumed()
			}
		}
	}
}

func (w *reportBatchWorker) enqueuePing(ctx context.Context, record models.PingRecord) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stopping {
		return ErrReportBatchStopped
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case w.pingQueue <- record:
		return nil
	default:
		return ErrPingBatchQueueFull
	}
}

func drainReportQueue(queue <-chan v2.Report, limit int) []v2.Report {
	if limit <= 0 {
		return nil
	}
	capacity := len(queue)
	if capacity > limit {
		capacity = limit
	}
	if capacity == 0 {
		return nil
	}
	reports := make([]v2.Report, 0, capacity)
	for len(reports) < limit {
		select {
		case report := <-queue:
			reports = append(reports, report)
		default:
			return reports
		}
	}
	return reports
}

func drainPingQueue(queue <-chan models.PingRecord, limit int) []models.PingRecord {
	if limit <= 0 {
		return nil
	}
	capacity := len(queue)
	if capacity > limit {
		capacity = limit
	}
	if capacity == 0 {
		return nil
	}
	records := make([]models.PingRecord, 0, capacity)
	for len(records) < limit {
		select {
		case record := <-queue:
			records = append(records, record)
		default:
			return records
		}
	}
	return records
}

func writePendingReports(ctx context.Context, pending *[]v2.Report) error {
	if len(*pending) == 0 {
		return nil
	}
	// Cap every chunk like the ping writer does. The backlog from a failed
	// flush is retried on the next tick; without a cap the whole backlog (up
	// to reportBatchQueueSize ≈ 6.6 min of fleet reports) went through ONE
	// WriteBatch under one 10s deadline — a giant transaction that is more
	// likely to blow the deadline again, turning one stall into a repeating
	// failure loop. Smaller chunks also let the flush deadline and the shared
	// locks breathe between chunks.
	batchSize := reportBatchMaxRecords
	for len(*pending) > 0 {
		if batchSize > len(*pending) {
			batchSize = len(*pending)
		}
		writeCtx, cancel := context.WithTimeout(ctx, reportBatchWriteTimeout)
		_, err := writeReportBatch(writeCtx, (*pending)[:batchSize])
		cancel()
		if err != nil {
			return err
		}
		*pending = (*pending)[batchSize:]
	}
	return nil
}

func writePendingPingRecords(ctx context.Context, pending *[]models.PingRecord) error {
	for len(*pending) > 0 {
		batchSize := len(*pending)
		if batchSize > pingBatchMaxRecords {
			batchSize = pingBatchMaxRecords
		}
		writeCtx, cancel := context.WithTimeout(ctx, reportBatchWriteTimeout)
		err := writePingRecords(writeCtx, (*pending)[:batchSize])
		cancel()
		if err != nil {
			return err
		}
		*pending = (*pending)[batchSize:]
	}
	return nil
}

func writeReportBatch(ctx context.Context, reports []v2.Report) ([]v2.Report, error) {
	if len(reports) == 0 {
		return nil, nil
	}
	// Fast shared acquire: while an exclusive operation (maintenance or
	// migration) is waiting or running, shared holders would queue behind it
	// and burn the whole flush deadline. Bailing out immediately keeps the
	// backlog intact and signals the paused state instead.
	if !storeOperations.TryAcquireShared() {
		return nil, ErrStoreOperationPaused
	}
	defer storeOperations.ReleaseShared()

	s := GetStore()
	if s == nil {
		return nil, fmt.Errorf("metric store not enabled")
	}

	prepared := make([]v2.Report, len(reports))
	copy(prepared, reports)
	points := make([]metric.Point, 0, len(reports)*20)
	pendingStates := make(map[*reportTrafficState]reportTrafficValues)
	for i, report := range prepared {
		stateValue, _ := reportTrafficStates.LoadOrStore(report.UUID, &reportTrafficState{})
		state := stateValue.(*reportTrafficState)
		values, ok := pendingStates[state]
		if !ok {
			state.mu.Lock()
			values = state.reportTrafficValues
			state.mu.Unlock()
		}
		if !values.initialized {
			// Keep initialized=false when the restore fails: the state below
			// (hasUp/totalUp from the current report) is a usable baseline for
			// delta computation, but persisting initialized=true would freeze
			// this batch's lost delta forever. Retrying the restore on the
			// next batch re-reads the true previous counter and recovers the
			// traffic delta once the read succeeds.
			restoreFailed := false
			totalUp, hasUp, err := latestReportCounter(ctx, s, MetricNetTotalUp, report.UUID, report.UpdatedAt)
			if err != nil {
				restoreFailed = true
				logger.Errorf("metricstore", "failed to restore previous upload counter for %s: %v", report.UUID, err)
			} else {
				values.totalUp = totalUp
				values.hasUp = hasUp
			}
			totalDown, hasDown, err := latestReportCounter(ctx, s, MetricNetTotalDown, report.UUID, report.UpdatedAt)
			if err != nil {
				restoreFailed = true
				logger.Errorf("metricstore", "failed to restore previous download counter for %s: %v", report.UUID, err)
			} else {
				values.totalDown = totalDown
				values.hasDown = hasDown
			}
			if !restoreFailed {
				values.initialized = true
			}
		}

		if !values.timestamp.IsZero() && !report.UpdatedAt.After(values.timestamp) {
			report.UpdatedAt = values.timestamp.Add(time.Millisecond)
		}
		trafficUp := int64(0)
		if values.hasUp {
			trafficUp = TrafficCounterDelta(report.Network.TotalUp, values.totalUp)
		}
		trafficDown := int64(0)
		if values.hasDown {
			trafficDown = TrafficCounterDelta(report.Network.TotalDown, values.totalDown)
		}
		points = append(points, reportMetricPoints(report, trafficUp, trafficDown)...)
		values.timestamp = report.UpdatedAt
		values.hasUp = true
		values.totalUp = report.Network.TotalUp
		values.hasDown = true
		values.totalDown = report.Network.TotalDown
		pendingStates[state] = values
		prepared[i] = report
	}

	// Persist the per-report traffic state even if the write below fails, so
	// a slow database does not re-issue the previous-counter queries on every
	// batch — except where the restore itself failed: those states keep
	// initialized=false above and retry the lookup next batch.
	for state, values := range pendingStates {
		state.mu.Lock()
		state.reportTrafficValues = values
		state.mu.Unlock()
	}

	if err := s.WriteBatch(ctx, points); err != nil {
		return nil, err
	}
	return prepared, nil
}
