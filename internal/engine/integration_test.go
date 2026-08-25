//go:build integration

package engine

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jolovicdev/hayduk/internal/protocol"
)

func integrationEnv(t *testing.T) protocol.ConnectParams {
	t.Helper()
	if os.Getenv("RUN_MSF_INTEGRATION") != "1" {
		t.Skip("RUN_MSF_INTEGRATION=1 not set")
	}
	return protocol.ConnectParams{
		Host:     envOr("MSF_HOST", "127.0.0.1"),
		Port:     55553,
		SSL:      false,
		Username: envOr("MSF_USERNAME", "msf"),
		Password: envOr("MSF_PASSWORD", "testpass123"),
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func TestIntegrationConnectAndBootstrap(t *testing.T) {
	e := New(Config{})
	t.Cleanup(e.Shutdown)
	sub := e.Subscribe()
	defer sub.Stop()

	if err := e.Connect(context.Background(), integrationEnv(t)); err != nil {
		t.Fatalf("connect: %+v", err)
	}

	st := e.State()
	if st.Connection.Status != "connected" || st.Connection.MSFVersion == "" {
		t.Fatalf("connection %+v", st.Connection)
	}
	if st.Connection.Workspace != "default" {
		t.Fatalf("workspace %q", st.Connection.Workspace)
	}
	if st.Modules == nil || len(st.Modules.Exploits) < 500 {
		t.Fatalf("exploits %d", len(st.Modules.Exploits))
	}
	if st.Console == nil {
		t.Fatal("console missing")
	}
}

func TestIntegrationConsoleRoundtrip(t *testing.T) {
	e := New(Config{})
	t.Cleanup(e.Shutdown)
	sub := e.Subscribe()
	defer sub.Stop()
	if err := e.Connect(context.Background(), integrationEnv(t)); err != nil {
		t.Fatalf("connect: %+v", err)
	}

	if _, err := e.Exec(context.Background(), "", protocol.MethodConsoleWrite,
		json.RawMessage(`{"command":"version\n"}`)); err != nil {
		t.Fatal(err)
	}

	deadline := time.After(30 * time.Second)
	for {
		if strings.Contains(e.State().Console.Output, "Framework:") {
			return
		}
		select {
		case <-deadline:
			t.Fatal("version output never arrived")
		case <-time.After(100 * time.Millisecond):
		}
	}
}
