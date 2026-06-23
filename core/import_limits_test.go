package core

import "testing"

func TestImportQueueLimiterReportsPosition(t *testing.T) {
	limiter := &importQueueLimiter{capacity: 1}

	var firstStatuses []ImportQueueStatus
	first := &importQueueEntry{
		ready: make(chan struct{}),
		notify: func(status ImportQueueStatus) {
			firstStatuses = append(firstStatuses, status)
		},
	}
	release, ok := limiter.tryAcquire(first)
	if !ok {
		t.Fatal("first entry should acquire immediately")
	}
	if got := firstStatuses[len(firstStatuses)-1]; got.Position != 0 || got.Active != 1 || got.Capacity != 1 {
		t.Fatalf("unexpected first status: %+v", got)
	}

	var secondStatuses []ImportQueueStatus
	second := &importQueueEntry{
		ready: make(chan struct{}),
		notify: func(status ImportQueueStatus) {
			secondStatuses = append(secondStatuses, status)
		},
	}
	if _, ok := limiter.tryAcquire(second); ok {
		t.Fatal("second entry should queue")
	}
	if got := secondStatuses[len(secondStatuses)-1]; got.Position != 1 || got.Waiting != 1 {
		t.Fatalf("unexpected second queued status: %+v", got)
	}

	var thirdStatuses []ImportQueueStatus
	third := &importQueueEntry{
		ready: make(chan struct{}),
		notify: func(status ImportQueueStatus) {
			thirdStatuses = append(thirdStatuses, status)
		},
	}
	if _, ok := limiter.tryAcquire(third); ok {
		t.Fatal("third entry should queue")
	}
	if got := thirdStatuses[len(thirdStatuses)-1]; got.Position != 2 || got.Waiting != 2 {
		t.Fatalf("unexpected third queued status: %+v", got)
	}

	release()

	select {
	case <-second.ready:
	default:
		t.Fatal("second entry should be granted after release")
	}
	if got := secondStatuses[len(secondStatuses)-1]; got.Position != 0 || got.Active != 1 {
		t.Fatalf("unexpected second granted status: %+v", got)
	}
	if got := thirdStatuses[len(thirdStatuses)-1]; got.Position != 1 || got.Waiting != 1 {
		t.Fatalf("unexpected third shifted status: %+v", got)
	}
}
