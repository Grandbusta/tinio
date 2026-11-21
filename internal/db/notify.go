package db

import "sync"

// hub is what's said about the database inside this one process: what
// Postgres's LISTEN and NOTIFY did on the platform. A listener that falls
// behind misses what it had no room for, and its channel is closed, so it
// knows to catch up from the database.
type hub struct {
	mu    sync.Mutex
	about map[string]map[chan string]bool
}

// Listen hears what's notified on channel until stop. The channel is closed
// if the listener falls behind by more than room.
func (h *hub) Listen(channel string, room int) (heard <-chan string, stop func()) {
	ch := make(chan string, room)
	h.mu.Lock()
	if h.about == nil {
		h.about = map[string]map[chan string]bool{}
	}
	if h.about[channel] == nil {
		h.about[channel] = map[chan string]bool{}
	}
	h.about[channel][ch] = true
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if h.about[channel][ch] {
			delete(h.about[channel], ch)
			close(ch)
		}
	}
}

// Notify tells channel's listeners payload; never blocks.
func (h *hub) Notify(channel, payload string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.about[channel] {
		select {
		case ch <- payload:
		default:
			delete(h.about[channel], ch)
			close(ch)
		}
	}
}
