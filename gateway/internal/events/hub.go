// Package events fans out live office events to SSE subscribers and wakes
// long-polling workers when there may be new work.
package events

import (
	"encoding/json"
	"sync"
	"time"
)

type Event struct {
	Type string    `json:"type"`
	Data any       `json:"data"`
	At   time.Time `json:"at"`
}

type Hub struct {
	mu     sync.Mutex
	subs   map[chan []byte]struct{}
	wakeCh chan struct{}
}

func NewHub() *Hub {
	return &Hub{subs: map[chan []byte]struct{}{}, wakeCh: make(chan struct{})}
}

// Publish sends an event to all subscribers; slow subscribers drop events
// rather than blocking the gateway.
func (h *Hub) Publish(typ string, data any) {
	b, err := json.Marshal(Event{Type: typ, Data: data, At: time.Now().UTC()})
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- b:
		default:
		}
	}
}

func (h *Hub) Subscribe() (<-chan []byte, func()) {
	ch := make(chan []byte, 64)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		delete(h.subs, ch)
		h.mu.Unlock()
	}
}

// Wake releases every goroutine currently waiting on WaitC.
func (h *Hub) Wake() {
	h.mu.Lock()
	close(h.wakeCh)
	h.wakeCh = make(chan struct{})
	h.mu.Unlock()
}

// WaitC returns a channel closed on the next Wake.
func (h *Hub) WaitC() <-chan struct{} {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.wakeCh
}
