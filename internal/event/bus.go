package event

import (
	"sync"
)

// Bus is a single-producer, many-consumer fan-out channel bus.
//
// Subscribers come in two flavors:
//   - lossy:   MessageStreamed events may be dropped if the subscriber's
//     buffer is full. All other event types still block-and-send.
//   - strict:  all events block on a full buffer; the publisher waits.
//
// Close is safe to call exactly once; subscribers range until close.
type Bus struct {
	mu     sync.RWMutex
	subs   []*subscription
	closed bool
}

type subscription struct {
	ch    chan Event
	lossy bool
}

// NewBus returns a fresh Bus.
func NewBus() *Bus { return &Bus{} }

// Subscribe returns a channel that receives events. buf is the channel
// buffer size. If lossy is true, MessageStreamed events may be dropped
// under backpressure; all other events block-and-send.
func (b *Bus) Subscribe(buf int, lossy bool) <-chan Event {
	if buf < 1 {
		buf = 1
	}
	sub := &subscription{ch: make(chan Event, buf), lossy: lossy}
	b.mu.Lock()
	b.subs = append(b.subs, sub)
	b.mu.Unlock()
	return sub.ch
}

// Publish delivers ev to every subscriber.
func (b *Bus) Publish(ev Event) {
	b.mu.RLock()
	subs := b.subs
	closed := b.closed
	b.mu.RUnlock()
	if closed {
		return
	}
	_, isStream := ev.(MessageStreamed)
	for _, s := range subs {
		if isStream && s.lossy {
			select {
			case s.ch <- ev:
			default:
				// drop
			}
			continue
		}
		s.ch <- ev
	}
}

// Close closes every subscriber channel. Safe to call once; further
// Publish calls are no-ops.
func (b *Bus) Close() {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.closed = true
	subs := b.subs
	b.subs = nil
	b.mu.Unlock()
	for _, s := range subs {
		close(s.ch)
	}
}
