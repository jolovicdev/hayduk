package engine

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"time"

	"github.com/jolovicdev/hayduk/internal/protocol"
)

// Operations are the engine's long-running tasks: a version scan, a login
// spray, a Hail Mary. They are owned by the engine rather than the browser
// command that started them, so closing the tab or reloading the page does
// not abort them, every operator sees the progress, and a canceled operation
// actually stops instead of merely hiding its dialog.
//
// One operation per kind runs at a time: two scans or two sprays at once
// would only compete for the same daemon and console slots.
const (
	// operationHistoryCap bounds the finished operations kept in memory for
	// the table. Running operations are always kept.
	operationHistoryCap = 50
)

type operation struct {
	st     protocol.OperationState
	cancel context.CancelFunc
}

// beginOperation registers a running operation and returns its id and
// context. The context is a child of the engine's run context, so a
// disconnect ends the work as well. A nil error body means the operation
// started; otherwise nothing was registered.
func (e *Engine) beginOperation(kind, operator, ws, target string, start, total int) (string, context.Context, *protocol.ErrorBody) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.rpc == nil || e.runCtx == nil {
		return "", nil, notConnected()
	}
	if e.hasRunningOperationLocked(kind) {
		return "", nil, &protocol.ErrorBody{
			Code: protocol.CodeBusy, Message: "a " + kind + " operation is already running",
		}
	}
	if e.operations == nil {
		e.operations = make(map[string]*operation)
	}
	e.opSeq++
	id := "op-" + strconv.FormatUint(e.opSeq, 10)
	ctx, cancel := context.WithCancel(e.runCtx)
	e.operations[id] = &operation{
		st: protocol.OperationState{
			ID: id, Kind: kind, Status: protocol.OpRunning,
			Operator: operator, Workspace: ws, Target: target,
			Done: start, Total: total, StartedAt: time.Now().UTC(),
		},
		cancel: cancel,
	}
	e.pruneOperationsLocked()
	e.bus.send(protocol.OperationsUpdate(e.operationListLocked()))
	return id, ctx, nil
}

// hasRunningOperationLocked reports whether an operation of this kind is
// still running. Callers hold e.mu.
func (e *Engine) hasRunningOperationLocked(kind string) bool {
	for _, op := range e.operations {
		if op.st.Kind == kind && op.st.Status == protocol.OpRunning {
			return true
		}
	}
	return false
}

// hasRunningOperation is hasRunningOperationLocked without the lock, for
// command validation outside the dispatch hold.
func (e *Engine) hasRunningOperation(kind string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.hasRunningOperationLocked(kind)
}

// advanceOperation records progress. done < 0 leaves the counter alone;
// an empty summary keeps the previous one.
func (e *Engine) advanceOperation(id string, done int, summary string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	op := e.operations[id]
	if op == nil || op.st.Status != protocol.OpRunning {
		return
	}
	if done >= 0 {
		op.st.Done = done
	}
	if summary != "" {
		op.st.Summary = summary
	}
	e.bus.send(protocol.OperationsUpdate(e.operationListLocked()))
}

// finishOperation moves a running operation to its terminal state. Calls
// after the operation was canceled by a disconnect are ignored: the
// teardown already recorded why it ended.
func (e *Engine) finishOperation(id, status, summary string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	op := e.operations[id]
	if op == nil || op.st.Status != protocol.OpRunning {
		return
	}
	if status == protocol.OpDone && op.st.Total > 0 {
		op.st.Done = op.st.Total
	}
	op.st.Status = status
	op.st.Summary = summary
	op.st.FinishedAt = time.Now().UTC()
	op.cancel = nil
	e.bus.send(protocol.OperationsUpdate(e.operationListLocked()))
}

// cancelOperation stops a running operation. The worker observes its
// canceled context, stops at the next boundary, and records the canceled
// status itself, so the operation stays visible as running until it really
// stopped.
func (e *Engine) cancelOperation(id string) *protocol.ErrorBody {
	e.mu.Lock()
	op := e.operations[id]
	if op == nil {
		e.mu.Unlock()
		return badParam("no operation " + id)
	}
	if op.st.Status != protocol.OpRunning || op.cancel == nil {
		e.mu.Unlock()
		return badParam("operation " + id + " is not running")
	}
	cancel := op.cancel
	e.mu.Unlock()
	cancel()
	return nil
}

// cancelOperationsLocked marks every running operation canceled because the
// link they were working against is gone. The returned cancels must run
// after e.mu is released. Callers hold e.mu.
func (e *Engine) cancelOperationsLocked() []context.CancelFunc {
	var cancels []context.CancelFunc
	changed := false
	for _, op := range e.operations {
		if op.st.Status != protocol.OpRunning {
			continue
		}
		if op.cancel != nil {
			cancels = append(cancels, op.cancel)
			op.cancel = nil
		}
		op.st.Status = protocol.OpCanceled
		op.st.Summary = "connection ended"
		op.st.FinishedAt = time.Now().UTC()
		changed = true
	}
	if changed {
		e.bus.send(protocol.OperationsUpdate(e.operationListLocked()))
	}
	return cancels
}

// operationListLocked copies the operations newest first. Running
// operations sort above finished ones so an active run is never pushed below
// the fold by history.
func (e *Engine) operationListLocked() []*protocol.OperationState {
	out := make([]*protocol.OperationState, 0, len(e.operations))
	for _, op := range e.operations {
		st := op.st
		out = append(out, &st)
	}
	sort.SliceStable(out, func(i, j int) bool {
		ri := out[i].Status == protocol.OpRunning
		rj := out[j].Status == protocol.OpRunning
		if ri != rj {
			return ri
		}
		return out[i].StartedAt.After(out[j].StartedAt)
	})
	return out
}

// pruneOperationsLocked drops the oldest finished operations beyond the
// history cap. Callers hold e.mu.
func (e *Engine) pruneOperationsLocked() {
	finished := make([]*operation, 0, len(e.operations))
	for _, op := range e.operations {
		if op.st.Status != protocol.OpRunning {
			finished = append(finished, op)
		}
	}
	if len(finished) <= operationHistoryCap {
		return
	}
	sort.Slice(finished, func(i, j int) bool {
		return finished[i].st.StartedAt.Before(finished[j].st.StartedAt)
	})
	for _, op := range finished[:len(finished)-operationHistoryCap] {
		delete(e.operations, op.st.ID)
	}
}

// operationByID returns a copy of one operation, for command responses.
func (e *Engine) operationByID(id string) *protocol.OperationState {
	e.mu.Lock()
	defer e.mu.Unlock()
	op := e.operations[id]
	if op == nil {
		return nil
	}
	st := op.st
	return &st
}

// operationsHandler is the operations.cancel dispatch arm.
func (e *Engine) operationsCancel(params json.RawMessage) (json.RawMessage, *protocol.ErrorBody) {
	var p protocol.OperationRefParams
	if eb := parseParams(params, &p); eb != nil {
		return nil, eb
	}
	if p.ID == "" {
		return nil, badParam("id is required")
	}
	if eb := e.cancelOperation(p.ID); eb != nil {
		return nil, eb
	}
	return mustJSON(e.operationByID(p.ID)), nil
}
