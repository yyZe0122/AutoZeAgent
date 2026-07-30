// Package modelstream is an in-process fan-out for provider streaming events
// destined for local Gateway clients (TUI typewriter). It is not durable;
// recovery still uses agent_run_records (ADR-031).
package modelstream

import (
	"sync"
	"sync/atomic"

	"autozeagent.local/autozeagent/pkg/providerapi"
)

// Envelope wraps a StreamEvent with routing ids for multi-subscriber UIs.
type Envelope struct {
	Seq       uint64                  `json:"seq"`
	SessionID string                  `json:"session_id,omitempty"`
	TaskID    string                  `json:"task_id,omitempty"`
	RunID     string                  `json:"run_id,omitempty"`
	Event     providerapi.StreamEvent `json:"event"`
}

// Hub is a process-local pub/sub for model stream envelopes.
type Hub struct {
	mu      sync.RWMutex
	seq     atomic.Uint64
	subs    map[uint64]*subscription
	nextSub uint64
}

type subscription struct {
	id        uint64
	sessionID string
	runID     string
	ch        chan Envelope
}

func NewHub() *Hub {
	return &Hub{subs: make(map[uint64]*subscription)}
}

// Publish sends an envelope to matching subscribers (non-blocking drop if full).
func (h *Hub) Publish(sessionID, taskID, runID string, event providerapi.StreamEvent) {
	if h == nil {
		return
	}
	env := Envelope{
		Seq:       h.seq.Add(1),
		SessionID: sessionID,
		TaskID:    taskID,
		RunID:     runID,
		Event:     event,
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, sub := range h.subs {
		if sub.sessionID != "" && sessionID != "" && sub.sessionID != sessionID {
			continue
		}
		if sub.runID != "" && runID != "" && sub.runID != runID {
			continue
		}
		select {
		case sub.ch <- env:
		default:
			// Drop if subscriber is slow; TUI will reconcile via transcript.
		}
	}
}

// Handler returns a StreamHandler that publishes each event for the given run.
func (h *Hub) Handler(sessionID, taskID, runID string) providerapi.StreamHandler {
	if h == nil {
		return nil
	}
	return func(event providerapi.StreamEvent) error {
		h.Publish(sessionID, taskID, runID, event)
		return nil
	}
}

// Subscribe receives envelopes. Empty sessionID/runID matches all.
// Call the returned cancel to unsubscribe.
func (h *Hub) Subscribe(sessionID, runID string, buffer int) (<-chan Envelope, func()) {
	if h == nil {
		ch := make(chan Envelope)
		close(ch)
		return ch, func() {}
	}
	if buffer < 16 {
		buffer = 64
	}
	h.mu.Lock()
	h.nextSub++
	id := h.nextSub
	sub := &subscription{
		id: id, sessionID: sessionID, runID: runID,
		ch: make(chan Envelope, buffer),
	}
	h.subs[id] = sub
	h.mu.Unlock()
	cancel := func() {
		h.mu.Lock()
		if cur, ok := h.subs[id]; ok {
			delete(h.subs, id)
			close(cur.ch)
		}
		h.mu.Unlock()
	}
	return sub.ch, cancel
}
