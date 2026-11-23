package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jolovicdev/go-msf/v2"
	"github.com/jolovicdev/hayduk/internal/protocol"
)

func TestBuildBrutePlanExpandsAndCaps(t *testing.T) {
	plan, err := buildBrutePlan(protocol.BruteParams{
		Hosts:     []string{"10.0.0.5", "10.0.0.6"},
		Module:    "scanner/ssh/ssh_login",
		Users:     []string{"root", "admin"},
		Passwords: []string{"toor"},
		AddBlank:  true,
		Ports:     map[string]int{"10.0.0.6": 2222},
	})
	if err != nil {
		t.Fatal(err)
	}
	// two users, two passes, two hosts
	if len(plan.attempts) != 8 || plan.capped != 0 {
		t.Fatalf("attempts %d capped %d, want 8/0", len(plan.attempts), plan.capped)
	}
	if plan.attempts[0].port != 0 {
		t.Fatalf("first host borrowed a port: %+v", plan.attempts[0])
	}
	for _, a := range plan.attempts {
		if a.host == "10.0.0.6" && a.port != 2222 {
			t.Fatalf("scanned port missing: %+v", a)
		}
	}
}

func TestBuildBrutePlanUserlessIgnoresUsers(t *testing.T) {
	plan, err := buildBrutePlan(protocol.BruteParams{
		Hosts:     []string{"10.0.0.5"},
		Module:    "scanner/vnc/vnc_login",
		Passwords: []string{"secret"},
		AddBlank:  true,
		Userless:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.attempts) != 2 {
		t.Fatalf("attempts %d, want 2", len(plan.attempts))
	}
	for _, a := range plan.attempts {
		if a.user != "" {
			t.Fatalf("userless scanner got a user: %+v", a)
		}
	}
}

func TestBuildBrutePlanValidatesInput(t *testing.T) {
	base := protocol.BruteParams{
		Hosts: []string{"10.0.0.5"}, Module: "scanner/ssh/ssh_login",
		Users: []string{"root"}, Passwords: []string{"toor"},
	}
	cases := map[string]func(p *protocol.BruteParams){
		"non-login module":          func(p *protocol.BruteParams) { p.Module = "exploit/windows/smb/ms17_010" },
		"no hosts":                  func(p *protocol.BruteParams) { p.Hosts = nil },
		"bad host":                  func(p *protocol.BruteParams) { p.Hosts = []string{"10.0.0.1; id"} },
		"no users":                  func(p *protocol.BruteParams) { p.Users = nil },
		"no passwords":              func(p *protocol.BruteParams) { p.Passwords = nil; p.AddBlank = false },
		"bad port":                  func(p *protocol.BruteParams) { p.Port = 70000 },
		"bad scanned port":          func(p *protocol.BruteParams) { p.Ports = map[string]int{"10.0.0.5": -1} },
		"newline password":          func(p *protocol.BruteParams) { p.Passwords = []string{"a\nb"} },
		"userless without password": func(p *protocol.BruteParams) { p.Userless = true; p.Passwords = nil },
	}
	for name, mutate := range cases {
		p := base
		mutate(&p)
		if _, err := buildBrutePlan(p); err == nil {
			t.Errorf("%s: accepted invalid plan", name)
		}
	}
}

func TestBrutePlanCapsAttempts(t *testing.T) {
	passwords := make([]string, 300)
	for i := range passwords {
		passwords[i] = fmt.Sprintf("pw-%03d", i)
	}
	plan, err := buildBrutePlan(protocol.BruteParams{
		Hosts:     []string{"10.0.0.5"},
		Module:    "scanner/ssh/ssh_login",
		Users:     []string{"root"},
		Passwords: passwords,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.attempts) != bruteAttemptCap || plan.capped != 300-bruteAttemptCap {
		t.Fatalf("attempts %d capped %d", len(plan.attempts), plan.capped)
	}
}

// bruteCommandFake scripts the module loads and executes one login spray
// needs. The returned launches copy is safe to read after the run.
func bruteCommandFake(t *testing.T, delay time.Duration) (*fakeRPC, func() []map[string]interface{}) {
	t.Helper()
	fake := stdFake()
	fake.set(gomsf.ModuleInfo, okModuleInfo())
	fake.set(gomsf.ModuleOptions, func(args ...interface{}) (interface{}, error) {
		return map[string]interface{}{
			"RHOSTS": map[string]interface{}{"type": "string", "required": true, "desc": "target"},
			"RPORT":  map[string]interface{}{"type": "integer", "desc": "port"},
			"USERNAME": map[string]interface{}{
				"type": "string", "desc": "user",
			},
			"PASSWORD": map[string]interface{}{"type": "string", "desc": "pass"},
			"THREADS":  map[string]interface{}{"type": "integer", "desc": "threads"},
		}, nil
	})
	var mu sync.Mutex
	var executed []map[string]interface{}
	fake.set(gomsf.ModuleExecute, func(args ...interface{}) (interface{}, error) {
		if delay > 0 {
			time.Sleep(delay)
		}
		opts := map[string]interface{}{}
		for k, v := range args[2].(map[string]interface{}) {
			opts[k] = v
		}
		mu.Lock()
		executed = append(executed, opts)
		mu.Unlock()
		return map[string]interface{}{"job_id": 0, "uuid": "u"}, nil
	})
	launches := func() []map[string]interface{} {
		mu.Lock()
		defer mu.Unlock()
		return append([]map[string]interface{}(nil), executed...)
	}
	return fake, launches
}

func TestBruteStartRunsPlannedAttempts(t *testing.T) {
	fake, launches := bruteCommandFake(t, 0)
	e := connectedEngine(t, fake)

	raw, eb := e.Exec(context.Background(), "dana", protocol.MethodBruteStart, json.RawMessage(`{
		"hosts":["10.0.0.5"],"module":"scanner/ssh/ssh_login",
		"users":["root"],"passwords":["toor"],
		"ports":{"10.0.0.5":2222},"addBlank":true
	}`))
	if eb != nil {
		t.Fatalf("brute.start: %+v", eb)
	}
	var payload protocol.BrutePayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Planned != 2 || payload.OperationID == "" {
		t.Fatalf("payload %+v", payload)
	}
	waitFor(t, func() bool {
		for _, op := range e.State().Operations {
			if op != nil && op.ID == payload.OperationID {
				return op.Status == protocol.OpDone
			}
		}
		return false
	})

	got := launches()
	if len(got) != 2 {
		t.Fatalf("executed %d attempts, want 2", len(got))
	}
	passwords := map[interface{}]bool{}
	for _, opts := range got {
		if opts["RHOSTS"] != "10.0.0.5" || opts["USERNAME"] != "root" {
			t.Fatalf("attempt options %+v", opts)
		}
		if opts["RPORT"] != 2222 {
			t.Fatalf("scanned port not used: %+v", opts)
		}
		passwords[opts["PASSWORD"]] = true
	}
	if !passwords["toor"] || !passwords[""] {
		t.Fatalf("passwords %+v", passwords)
	}
	for _, op := range e.State().Operations {
		if op != nil && op.ID == payload.OperationID {
			if op.Operator != "dana" || !strings.Contains(op.Summary, "2 attempt") {
				t.Fatalf("operation %+v", op)
			}
		}
	}
}

func TestBruteStartRejectsSecondRun(t *testing.T) {
	fake, _ := bruteCommandFake(t, 120*time.Millisecond)
	e := connectedEngine(t, fake)
	passwords := make([]string, 12)
	for i := range passwords {
		passwords[i] = fmt.Sprintf("pw-%d", i)
	}
	params, _ := json.Marshal(protocol.BruteParams{
		Hosts: []string{"10.0.0.5"}, Module: "scanner/ssh/ssh_login",
		Users: []string{"root"}, Passwords: passwords,
	})
	if _, eb := e.Exec(context.Background(), "", protocol.MethodBruteStart, params); eb != nil {
		t.Fatalf("first brute.start: %+v", eb)
	}
	if _, eb := e.Exec(context.Background(), "", protocol.MethodBruteStart, params); eb == nil || eb.Code != protocol.CodeBusy {
		t.Fatalf("second run: got %+v, want busy", eb)
	}
	// Cancel so the cleanup does not sit through the pacing.
	for _, op := range e.State().Operations {
		if op != nil && op.Kind == protocol.OpBrute && op.Status == protocol.OpRunning {
			if _, eb := e.Exec(context.Background(), "", protocol.MethodOperationsCancel,
				json.RawMessage(`{"id":"`+op.ID+`"}`)); eb != nil {
				t.Fatalf("cancel: %+v", eb)
			}
		}
	}
}

func TestBruteRunCanBeCanceled(t *testing.T) {
	fake, _ := bruteCommandFake(t, 80*time.Millisecond)
	e := connectedEngine(t, fake)
	passwords := make([]string, 20)
	for i := range passwords {
		passwords[i] = fmt.Sprintf("pw-%d", i)
	}
	params, _ := json.Marshal(protocol.BruteParams{
		Hosts: []string{"10.0.0.5"}, Module: "scanner/ssh/ssh_login",
		Users: []string{"root"}, Passwords: passwords,
	})
	raw, eb := e.Exec(context.Background(), "", protocol.MethodBruteStart, params)
	if eb != nil {
		t.Fatalf("brute.start: %+v", eb)
	}
	var payload protocol.BrutePayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if _, eb := e.Exec(context.Background(), "", protocol.MethodOperationsCancel,
		json.RawMessage(`{"id":"`+payload.OperationID+`"}`)); eb != nil {
		t.Fatalf("cancel: %+v", eb)
	}
	waitFor(t, func() bool {
		for _, op := range e.State().Operations {
			if op != nil && op.ID == payload.OperationID {
				return op.Status == protocol.OpCanceled
			}
		}
		return false
	})
}

func TestOperationsCancelUnknownIsBadParams(t *testing.T) {
	e := connectedEngine(t, stdFake())
	if _, eb := e.Exec(context.Background(), "", protocol.MethodOperationsCancel,
		json.RawMessage(`{"id":"op-404"}`)); eb == nil || eb.Code != protocol.CodeBadParams {
		t.Fatalf("got %+v, want bad_params", eb)
	}
}
