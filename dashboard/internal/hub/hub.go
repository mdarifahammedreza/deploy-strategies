// Package hub fans one stream of snapshots out to many WebSocket clients.
package hub

import "sync"

// Hub keeps the latest message and a 1-slot mailbox per subscriber.
// A slow browser never blocks the others: it simply skips to the newest snapshot.
type Hub struct {
	mu     sync.Mutex
	subs   map[chan []byte]struct{}
	latest []byte
}

func New() *Hub {
	return &Hub{subs: map[chan []byte]struct{}{}}
}

// Publish stores msg as the latest and delivers it to every subscriber, replacing any unread message.
func (h *Hub) Publish(msg []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.latest = msg
	for ch := range h.subs {
		select {
		case ch <- msg:
		default: // mailbox full: drop the stale message, put the new one
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- msg:
			default:
			}
		}
	}
}

// Subscribe returns a channel of messages and a cancel function. The latest message (if any) arrives immediately.
func (h *Hub) Subscribe() (<-chan []byte, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	ch := make(chan []byte, 1)
	if h.latest != nil {
		ch <- h.latest
	}
	h.subs[ch] = struct{}{}
	cancel := func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		delete(h.subs, ch)
	}
	return ch, cancel
}

// Latest returns the most recently published message (nil before the first one).
func (h *Hub) Latest() []byte {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.latest
}

// Subscribers returns how many clients are connected.
func (h *Hub) Subscribers() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}
