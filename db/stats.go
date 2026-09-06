package db

import (
	"context"
	"sync"
	"time"
)

type AccessStore interface {
	RecordRedirectAccess(ctx context.Context, id int64, accessedAt time.Time) error
}

type MissStore interface {
	RecordSlugMiss(ctx context.Context, slug string, missedAt time.Time) error
}

type accessRecord struct {
	id   int64
	slug string
	at   time.Time
}

// AccessWriter persists aggregate redirect accesses and valid slug misses on one
// bounded background queue. Tracking never waits for storage; false means the
// event was deliberately dropped because the queue was full or shutdown began.
type AccessWriter struct {
	store   AccessStore
	queue   chan accessRecord
	onError func(error)
	ctx     context.Context
	cancel  context.CancelFunc
	mu      sync.Mutex
	closed  bool
	done    chan struct{}
}

func NewAccessWriter(store AccessStore, capacity int, onError func(error)) *AccessWriter {
	if capacity < 1 {
		capacity = 1
	}
	ctx, cancel := context.WithCancel(context.Background())
	writer := &AccessWriter{store: store, queue: make(chan accessRecord, capacity), onError: onError, ctx: ctx, cancel: cancel, done: make(chan struct{})}
	go writer.run()
	return writer
}

func (writer *AccessWriter) Track(id int64, at time.Time) bool {
	return writer.enqueue(accessRecord{id: id, at: at})
}

func (writer *AccessWriter) TrackMiss(slug string, at time.Time) bool {
	return writer.enqueue(accessRecord{slug: slug, at: at})
}

func (writer *AccessWriter) enqueue(record accessRecord) bool {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if writer.closed {
		return false
	}
	select {
	case writer.queue <- record:
		return true
	default:
		return false
	}
}

func (writer *AccessWriter) Close(ctx context.Context) error {
	writer.mu.Lock()
	if !writer.closed {
		writer.closed = true
		close(writer.queue)
	}
	writer.mu.Unlock()
	select {
	case <-writer.done:
		return nil
	case <-ctx.Done():
		writer.cancel()
		return ctx.Err()
	}
}

func (writer *AccessWriter) run() {
	defer func() {
		writer.cancel()
		close(writer.done)
	}()
	for {
		if writer.ctx.Err() != nil {
			return
		}
		select {
		case <-writer.ctx.Done():
			return
		case record, ok := <-writer.queue:
			if !ok {
				return
			}
			var err error
			if record.slug == "" {
				err = writer.store.RecordRedirectAccess(writer.ctx, record.id, record.at)
			} else if store, ok := writer.store.(MissStore); ok {
				err = store.RecordSlugMiss(writer.ctx, record.slug, record.at)
			}
			if err != nil && writer.ctx.Err() == nil && writer.onError != nil {
				writer.onError(err)
			}
		}
	}
}
