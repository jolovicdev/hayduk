package engine

import (
	"context"
	"errors"
	"testing"
)

// The error streak counts consecutive failures; a successful db round trip
// resets it.
func TestSuccessfulRefreshClearsMonitorErrorStreak(t *testing.T) {
	e := connectedEngine(t, stdFake())

	e.mu.Lock()
	mon := e.monitor
	e.mu.Unlock()
	e.monitorError(mon, errors.New("transient blip one"))
	e.monitorError(mon, errors.New("transient blip two"))

	if err := e.refreshDB(context.Background()); err != nil {
		t.Fatalf("refresh failed: %v", err)
	}

	e.mu.Lock()
	streak := e.errStreak
	e.mu.Unlock()
	if streak != 0 {
		t.Fatalf("a successful db round trip must clear the error streak, got %d", streak)
	}
}
