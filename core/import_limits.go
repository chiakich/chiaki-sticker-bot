package core

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"
)

var (
	importLimiterOnce sync.Once
	importLimiter     *importQueueLimiter
)

type ImportQueueStatus struct {
	Position int
	Waiting  int
	Active   int
	Capacity int
}

type importQueueEntry struct {
	ready   chan struct{}
	notify  func(ImportQueueStatus)
	granted bool
}

type importQueueLimiter struct {
	mu       sync.Mutex
	capacity int
	active   int
	queue    []*importQueueEntry
}

func acquireImportSlot(ctx context.Context, notify ...func(ImportQueueStatus)) (func(), error) {
	importLimiterOnce.Do(func() {
		concurrency := 1
		if value, err := strconv.Atoi(os.Getenv("MSB_IMPORT_CONCURRENCY")); err == nil && value > 0 {
			concurrency = value
		}
		importLimiter = &importQueueLimiter{capacity: concurrency}
	})

	var notifyStatus func(ImportQueueStatus)
	if len(notify) > 0 {
		notifyStatus = notify[0]
	}

	entry := &importQueueEntry{
		ready:  make(chan struct{}),
		notify: notifyStatus,
	}

	if release, ok := importLimiter.tryAcquire(entry); ok {
		return release, nil
	}

	waitTimeout := importQueueTimeout()
	timer := time.NewTimer(waitTimeout)
	defer timer.Stop()

	select {
	case <-entry.ready:
		return importLimiter.release, nil
	case <-timer.C:
		if importLimiter.cancel(entry) {
			return nil, fmt.Errorf("import queue timeout after %s", waitTimeout)
		}
		return importLimiter.release, nil
	case <-ctx.Done():
		if !importLimiter.cancel(entry) {
			return importLimiter.release, nil
		}
		return nil, ctx.Err()
	}
}

func (l *importQueueLimiter) tryAcquire(entry *importQueueEntry) (func(), bool) {
	l.mu.Lock()
	var callbacks []func()
	if l.active < l.capacity && len(l.queue) == 0 {
		l.active++
		callbacks = append(callbacks, entry.callbackLocked(0, l))
		l.mu.Unlock()
		runImportQueueCallbacks(callbacks)
		return l.release, true
	}

	l.queue = append(l.queue, entry)
	callbacks = append(callbacks, l.queueCallbacksLocked()...)
	l.mu.Unlock()
	runImportQueueCallbacks(callbacks)
	return nil, false
}

func (l *importQueueLimiter) release() {
	l.mu.Lock()
	if l.active > 0 {
		l.active--
	}
	callbacks := l.grantQueuedLocked()
	callbacks = append(callbacks, l.queueCallbacksLocked()...)
	l.mu.Unlock()
	runImportQueueCallbacks(callbacks)
}

func (l *importQueueLimiter) cancel(entry *importQueueEntry) bool {
	l.mu.Lock()
	if entry.granted {
		l.mu.Unlock()
		return false
	}
	for i, queued := range l.queue {
		if queued == entry {
			l.queue = append(l.queue[:i], l.queue[i+1:]...)
			callbacks := l.queueCallbacksLocked()
			l.mu.Unlock()
			runImportQueueCallbacks(callbacks)
			return true
		}
	}
	l.mu.Unlock()
	return true
}

func (l *importQueueLimiter) grantQueuedLocked() []func() {
	var callbacks []func()
	for l.active < l.capacity && len(l.queue) > 0 {
		entry := l.queue[0]
		l.queue = l.queue[1:]
		l.active++
		entry.granted = true
		callbacks = append(callbacks, entry.callbackLocked(0, l))
		close(entry.ready)
	}
	return callbacks
}

func (l *importQueueLimiter) queueCallbacksLocked() []func() {
	callbacks := make([]func(), 0, len(l.queue))
	for i, entry := range l.queue {
		callbacks = append(callbacks, entry.callbackLocked(i+1, l))
	}
	return callbacks
}

func (entry *importQueueEntry) callbackLocked(position int, l *importQueueLimiter) func() {
	if entry.notify == nil {
		return nil
	}
	status := ImportQueueStatus{
		Position: position,
		Waiting:  len(l.queue),
		Active:   l.active,
		Capacity: l.capacity,
	}
	return func() {
		entry.notify(status)
	}
}

func runImportQueueCallbacks(callbacks []func()) {
	for _, callback := range callbacks {
		if callback != nil {
			callback()
		}
	}
}

func importQueueTimeout() time.Duration {
	timeout := 10 * time.Minute
	if value, err := strconv.Atoi(os.Getenv("MSB_IMPORT_QUEUE_TIMEOUT_SECONDS")); err == nil && value > 0 {
		timeout = time.Duration(value) * time.Second
	}
	return timeout
}
