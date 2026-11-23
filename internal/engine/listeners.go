package engine

import (
	"strconv"
	"time"

	"github.com/jolovicdev/hayduk/internal/protocol"
)

// handlerModule is the framework handler every listener launch runs.
const handlerModule = "multi/handler"

// Listener launches are registered by the engine, not the browser, so the
// registry is shared by the team, survives a page reload, and can be
// reconciled with the daemon's job list on reconnect. Only metadata is kept;
// the payload options themselves never leave the launch command.

func listenerOptionString(opts map[string]interface{}, key string) string {
	if opts == nil {
		return ""
	}
	if v, ok := opts[key].(string); ok {
		return v
	}
	return ""
}

func listenerOptionPort(opts map[string]interface{}, key string) int {
	if opts == nil {
		return 0
	}
	switch v := opts[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case string:
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return 0
}

// registerListener records a successful multi/handler launch. Job ids are the
// daemon's; a relaunch under a reused id replaces the old row.
func (e *Engine) registerListener(operator string, jobID int, payload string, opts map[string]interface{}) {
	if jobID <= 0 {
		return
	}
	st := &protocol.ListenerState{
		JobID:     strconv.Itoa(jobID),
		Payload:   payload,
		Lhost:     listenerOptionString(opts, "LHOST"),
		Lport:     listenerOptionPort(opts, "LPORT"),
		Operator:  operator,
		StartedAt: time.Now().UTC(),
	}
	e.mu.Lock()
	replaced := false
	for i, l := range e.listeners {
		if l != nil && l.JobID == st.JobID {
			e.listeners[i] = st
			replaced = true
			break
		}
	}
	if !replaced {
		e.listeners = append(e.listeners, st)
	}
	list := deepSlice(e.listeners)
	var persisted []*protocol.ListenerState
	if e.cfg.Store != nil {
		persisted = deepSlice(e.listeners)
	}
	e.bus.send(protocol.ListenersUpdate(list))
	e.mu.Unlock()
	if persisted != nil {
		_ = e.cfg.Store.SaveListeners(persisted)
	}
}

// removeListenerLocked drops the row for a stopped job. It returns the
// remaining registry for persistence when it changed. Callers hold e.mu.
func (e *Engine) removeListenerLocked(jobID string) (bool, []*protocol.ListenerState) {
	for i, l := range e.listeners {
		if l == nil || l.JobID != jobID {
			continue
		}
		e.listeners = append(e.listeners[:i], e.listeners[i+1:]...)
		if e.cfg.Store != nil {
			return true, deepSlice(e.listeners)
		}
		return true, nil
	}
	return false, nil
}

// seedListenersLocked loads the persisted registry at connect. When the live
// job list was read successfully, entries whose job is gone are pruned; when
// it failed, entries are kept untouched rather than erased by a transient
// error. Callers hold e.mu.
func (e *Engine) seedListenersLocked(liveJobs map[string]string, reconciled bool) []*protocol.ListenerState {
	e.listeners = nil
	if e.cfg.Store == nil {
		return nil
	}
	loaded := e.cfg.Store.LoadListeners()
	kept := make([]*protocol.ListenerState, 0, len(loaded))
	for _, l := range loaded {
		if l == nil {
			continue
		}
		if reconciled {
			if _, live := liveJobs[l.JobID]; !live {
				continue
			}
		}
		kept = append(kept, l)
	}
	e.listeners = kept
	if reconciled {
		return deepSlice(kept)
	}
	return nil
}

// clearListenersLocked empties the in-memory registry without touching the
// store: a disconnect is not a deletion. The next bootstrap reloads and
// reconciles it. Callers hold e.mu.
func (e *Engine) clearListenersLocked() bool {
	had := len(e.listeners) > 0
	e.listeners = nil
	return had
}
