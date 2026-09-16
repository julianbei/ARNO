package events

import "sync"

// Event is a normalized runtime signal emitted by Arno services.
type Event struct {
	Type    string
	Entity  string
	Payload map[string]string
}

// Record wraps an event with a monotonically increasing cursor.
type Record struct {
	Cursor int64
	Event  Event
}

// Bus is an in-process pub/sub stream used to decouple core services.
type Bus struct {
	mu          sync.RWMutex
	nextCursor  int64
	history     []Record
	subscribers []chan Event
}

func NewBus() *Bus {
	return &Bus{}
}

func (b *Bus) Publish(event Event) {
	b.mu.Lock()
	b.nextCursor++
	record := Record{Cursor: b.nextCursor, Event: event}
	b.history = append(b.history, record)
	subscribers := append([]chan Event(nil), b.subscribers...)
	b.mu.Unlock()

	for _, ch := range subscribers {
		select {
		case ch <- event:
		default:
			// Slow consumers must not block runtime progress.
		}
	}
}

// Events returns events after the provided cursor, capped by limit.
// It also returns the latest known cursor regardless of result length.
func (b *Bus) Events(after int64, limit int) ([]Record, int64) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	latest := b.nextCursor
	if limit <= 0 {
		limit = 50
	}

	out := make([]Record, 0, limit)
	for _, record := range b.history {
		if record.Cursor <= after {
			continue
		}
		out = append(out, record)
		if len(out) >= limit {
			break
		}
	}

	return out, latest
}

func (b *Bus) Subscribe(buffer int) <-chan Event {
	if buffer <= 0 {
		buffer = 1
	}
	ch := make(chan Event, buffer)

	b.mu.Lock()
	b.subscribers = append(b.subscribers, ch)
	b.mu.Unlock()

	return ch
}
