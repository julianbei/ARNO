package events

import "sync"

// Event is a normalized runtime signal emitted by Jade services.
type Event struct {
	Type    string
	Entity  string
	Payload map[string]string
}

// Bus is an in-process pub/sub stream used to decouple core services.
type Bus struct {
	mu          sync.RWMutex
	subscribers []chan Event
}

func NewBus() *Bus {
	return &Bus{}
}

func (b *Bus) Publish(event Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for _, ch := range b.subscribers {
		select {
		case ch <- event:
		default:
			// Slow consumers must not block runtime progress.
		}
	}
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
