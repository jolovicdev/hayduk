package engine

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jolovicdev/hayduk/internal/protocol"
)

// gomsf calls through a nil RPCCaller and panics, so a command that loses its
// connection mid-flight must answer not_connected instead.
func TestModuleExecuteAfterDisconnectIsAnErrorNotACrash(t *testing.T) {
	e := connectedEngine(t, stdFake())
	e.Disconnect()

	_, eb := e.moduleExecute(context.Background(), e.connectedRPC(), "dana",
		protocol.ModuleExecuteParams{Type: "exploit", Name: "windows/smb/a"})
	if eb == nil || eb.Code != protocol.CodeNotConnected {
		t.Fatalf("want not_connected after disconnect, got %+v", eb)
	}
}

func TestCommandAfterDisconnectAnswersNotConnected(t *testing.T) {
	e := connectedEngine(t, stdFake())
	e.Disconnect()

	for _, method := range []string{
		protocol.MethodModuleInfo, protocol.MethodModuleOptions, protocol.MethodPayloads,
		protocol.MethodWorkspaceList,
	} {
		_, eb := e.Exec(context.Background(), "dana", method,
			json.RawMessage(`{"type":"exploit","name":"windows/smb/a"}`))
		if eb == nil || eb.Code != protocol.CodeNotConnected {
			t.Fatalf("%s after disconnect: want not_connected, got %+v", method, eb)
		}
	}
}
