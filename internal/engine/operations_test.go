package engine

import (
	"testing"
	"time"

	"github.com/jolovicdev/hayduk/internal/protocol"
)

func TestOperationLifecycleBroadcastsAndFinishes(t *testing.T) {
	e := connectedEngine(t, stdFake())
	sub := e.Subscribe()
	defer sub.Stop()

	id, ctx, eb := e.beginOperation(protocol.OpScan, "dana", "default", "10.0.0.0/24", 0, 0)
	if eb != nil {
		t.Fatalf("begin: %+v", eb)
	}
	if ctx == nil {
		t.Fatal("no operation context")
	}
	select {
	case msg := <-sub.C():
		upd, ok := msg.(protocol.ResourceUpdate)
		if !ok || upd.Resource != protocol.ResOperations || len(upd.Operations) != 1 {
			t.Fatalf("first broadcast %#v", msg)
		}
		if upd.Operations[0].Status != protocol.OpRunning || upd.Operations[0].Operator != "dana" {
			t.Fatalf("operation %+v", upd.Operations[0])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no operations broadcast")
	}

	e.advanceOperation(id, 3, "halfway")
	op := e.operationByID(id)
	if op == nil || op.Done != 3 || op.Summary != "halfway" {
		t.Fatalf("advanced %+v", op)
	}
	e.finishOperation(id, protocol.OpDone, "finished")
	op = e.operationByID(id)
	if op == nil || op.Status != protocol.OpDone || op.FinishedAt.IsZero() {
		t.Fatalf("finished %+v", op)
	}
	// a late finish must not overwrite the terminal state
	e.finishOperation(id, protocol.OpFailed, "late")
	if op := e.operationByID(id); op.Status != protocol.OpDone {
		t.Fatalf("late finish overwrote: %+v", op)
	}
}

func TestOperationIsCancelable(t *testing.T) {
	e := connectedEngine(t, stdFake())
	id, ctx, eb := e.beginOperation(protocol.OpBrute, "", "default", "10.0.0.5", 0, 10)
	if eb != nil {
		t.Fatalf("begin: %+v", eb)
	}
	if eb := e.cancelOperation(id); eb != nil {
		t.Fatalf("cancel: %+v", eb)
	}
	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("cancel did not cancel the operation context")
	}
	// the worker reports its own terminal state
	e.finishOperation(id, protocol.OpCanceled, "canceled")
	if op := e.operationByID(id); op == nil || op.Status != protocol.OpCanceled {
		t.Fatalf("canceled %+v", op)
	}
	if eb := e.cancelOperation(id); eb == nil {
		t.Fatal("canceling a finished operation must fail")
	}
}

func TestOperationSlotsArePerKind(t *testing.T) {
	e := connectedEngine(t, stdFake())
	if _, _, eb := e.beginOperation(protocol.OpScan, "", "default", "10.0.0.1", 0, 0); eb != nil {
		t.Fatalf("first scan: %+v", eb)
	}
	if _, _, eb := e.beginOperation(protocol.OpScan, "", "default", "10.0.0.2", 0, 0); eb == nil || eb.Code != protocol.CodeBusy {
		t.Fatalf("second scan: got %+v, want busy", eb)
	}
	// another kind still fits
	if _, _, eb := e.beginOperation(protocol.OpBrute, "", "default", "10.0.0.1", 0, 0); eb != nil {
		t.Fatalf("brute alongside scan: %+v", eb)
	}
}

func TestDisconnectCancelsRunningOperations(t *testing.T) {
	e := connectedEngine(t, stdFake())
	id, ctx, eb := e.beginOperation(protocol.OpHailMary, "", "default", "10.0.0.5", 0, 4)
	if eb != nil {
		t.Fatalf("begin: %+v", eb)
	}
	e.Disconnect()
	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("disconnect did not cancel the operation context")
	}
	op := e.operationByID(id)
	if op == nil || op.Status != protocol.OpCanceled || op.Summary != "connection ended" {
		t.Fatalf("after disconnect %+v", op)
	}
	if e.hasRunningOperation(protocol.OpHailMary) {
		t.Fatal("operation still counts as running after disconnect")
	}
}

func TestOperationHistoryIsCapped(t *testing.T) {
	e := connectedEngine(t, stdFake())
	// fill and finish more than the cap
	for i := 0; i < operationHistoryCap+5; i++ {
		id, _, eb := e.beginOperation(protocol.OpScan, "", "default", "10.0.0.1", 0, 1)
		if eb != nil {
			t.Fatalf("begin %d: %+v", i, eb)
		}
		e.finishOperation(id, protocol.OpDone, "done")
	}
	e.mu.Lock()
	kept := len(e.operations)
	e.mu.Unlock()
	if kept > operationHistoryCap+1 {
		t.Fatalf("kept %d operations, want at most %d", kept, operationHistoryCap+1)
	}
}

// A canceled operation's worker must not be able to finish a later run that
// reused its slot; ids are unique, so finish only touches its own record.
func TestCanceledOperationFinishDoesNotTouchNewRun(t *testing.T) {
	e := connectedEngine(t, stdFake())
	first, _, eb := e.beginOperation(protocol.OpBrute, "", "default", "10.0.0.1", 0, 5)
	if eb != nil {
		t.Fatalf("begin: %+v", eb)
	}
	e.cancelOperation(first)
	e.finishOperation(first, protocol.OpCanceled, "canceled")
	second, _, eb := e.beginOperation(protocol.OpBrute, "", "default", "10.0.0.2", 0, 5)
	if eb != nil {
		t.Fatalf("second begin: %+v", eb)
	}
	e.finishOperation(first, protocol.OpDone, "late old finish")
	if op := e.operationByID(second); op == nil || op.Status != protocol.OpRunning {
		t.Fatalf("old finish touched new run: %+v", op)
	}
}

func TestOperationsSnapshotCarriesHistory(t *testing.T) {
	e := connectedEngine(t, stdFake())
	id, _, eb := e.beginOperation(protocol.OpScan, "dana", "default", "10.0.0.1", 0, 0)
	if eb != nil {
		t.Fatalf("begin: %+v", eb)
	}
	e.finishOperation(id, protocol.OpDone, "done")
	st := e.State()
	if len(st.Operations) != 1 || st.Operations[0].ID != id {
		t.Fatalf("snapshot operations %+v", st.Operations)
	}
	// the snapshot is a copy
	st.Operations[0].Summary = "mutated"
	if got := e.State().Operations[0].Summary; got != "done" {
		t.Fatalf("snapshot shared operation state: %q", got)
	}
	// starting after a disconnect must fail through the command path
	e.Disconnect()
	if _, _, eb := e.beginOperation(protocol.OpScan, "", "default", "10.0.0.1", 0, 0); eb == nil {
		t.Fatal("begin after disconnect accepted")
	}
}

func TestOperationsCancelCommand(t *testing.T) {
	e := connectedEngine(t, stdFake())
	id, ctx, eb := e.beginOperation(protocol.OpBrute, "", "default", "10.0.0.5", 0, 5)
	if eb != nil {
		t.Fatalf("begin: %+v", eb)
	}
	raw, eb := e.operationsCancel([]byte(`{"id":"` + id + `"}`))
	if eb != nil {
		t.Fatalf("cancel command: %+v", eb)
	}
	if len(raw) == 0 {
		t.Fatal("cancel command returned no operation")
	}
	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("cancel command did not cancel the context")
	}
}
