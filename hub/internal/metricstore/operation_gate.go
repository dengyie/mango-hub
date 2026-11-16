package metricstore

import (
	"context"

	"golang.org/x/sync/semaphore"
)

// storeOperationGate keeps an active Store stable while allowing regular
// reads, report writes, and compaction to proceed together. Store replacement
// and maintenance take the whole gate exclusively.
type storeOperationGate struct {
	semaphore *semaphore.Weighted
}

const storeOperationGateWeight int64 = 1024

func newStoreOperationGate() *storeOperationGate {
	return &storeOperationGate{semaphore: semaphore.NewWeighted(storeOperationGateWeight)}
}

func (g *storeOperationGate) Acquire(ctx context.Context) error {
	return g.semaphore.Acquire(ctx, storeOperationGateWeight)
}

func (g *storeOperationGate) TryAcquire() bool {
	return g.semaphore.TryAcquire(storeOperationGateWeight)
}

func (g *storeOperationGate) Release() {
	g.semaphore.Release(storeOperationGateWeight)
}

func (g *storeOperationGate) AcquireShared(ctx context.Context) error {
	return g.semaphore.Acquire(ctx, 1)
}

// TryAcquireShared is the non-blocking variant used by the report flusher:
// while an exclusive operation is waiting or running, shared holders would
// queue behind it and burn their whole deadline, so the flusher prefers to
// bail out and retry next tick.
func (g *storeOperationGate) TryAcquireShared() bool {
	return g.semaphore.TryAcquire(1)
}

func (g *storeOperationGate) ReleaseShared() {
	g.semaphore.Release(1)
}
