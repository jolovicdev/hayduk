package engine

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jolovicdev/go-msf/v2"
	"github.com/jolovicdev/hayduk/internal/protocol"
)

const routeTable = "\nIPv4 Active Routing Table\n=========================\n\n" +
	"Subnet             Netmask            Gateway\n" +
	"------             -------            -------\n" +
	"10.99.0.0          255.255.255.0      Session 1\n" +
	"192.168.0.0        255.255.0.0        Session 3\n\n\n"

func TestParseRoutes(t *testing.T) {
	routes := parseRoutes(routeTable)
	if len(routes) != 2 {
		t.Fatalf("got %+v", routes)
	}
	if routes[0].Subnet != "10.99.0.0/24" || routes[0].SessionID != "1" {
		t.Errorf("route 0 %+v", routes[0])
	}
	if routes[1].Subnet != "192.168.0.0/16" || routes[1].SessionID != "3" {
		t.Errorf("route 1 %+v", routes[1])
	}
}

func TestParseRoutesEmptyAndNoise(t *testing.T) {
	if got := parseRoutes("[*] There are currently no routes defined.\nmsf > "); got != nil {
		t.Fatalf("empty table should parse to nil, got %+v", got)
	}
	local := "IPv4 Active Routing Table\nSubnet Netmask Gateway\n10.0.0.0 255.255.255.0 Local\n"
	if got := parseRoutes(local); got != nil {
		t.Fatalf("local gateways are not pivots, got %+v", got)
	}
	banner := "msf v6 banner\nroute\n[*] There are currently no routes defined.\n"
	if got := parseRoutes(banner); got != nil {
		t.Fatalf("banner noise should parse to nil, got %+v", got)
	}
}

func TestMaskToPrefix(t *testing.T) {
	cases := map[string]string{
		"255.255.255.0":   "24",
		"255.255.0.0":     "16",
		"255.0.0.0":       "8",
		"255.255.255.255": "32",
		"0.0.0.0":         "0",
		"255.255.255.252": "30",
		"255.0.255.0":     "",
		"255.255.255.253": "",
		"ffff:ffff::":     "",
	}
	for mask, want := range cases {
		if got := maskToPrefix(mask); got != want {
			t.Errorf("maskToPrefix(%s) = %q, want %q", mask, got, want)
		}
	}
}

func TestRoutePollIgnoresBusyPartialOutput(t *testing.T) {
	fake := stdFake()
	var consoles int32
	fake.set(gomsf.ConsoleCreate, func(args ...interface{}) (interface{}, error) {
		return map[string]interface{}{"id": string(rune('0' + atomic.AddInt32(&consoles, 1) - 1))}, nil
	})
	// first poll sees the full table; the next poll returns a busy half
	// table first (msfrpcd still printing), then the complete one
	var polls int32
	fake.set(gomsf.ConsoleRead, func(args ...interface{}) (interface{}, error) {
		if len(args) > 0 && args[0] == "1" {
			switch atomic.AddInt32(&polls, 1) {
			case 1:
				return map[string]interface{}{"data": routeTable, "prompt": "msf > ", "busy": false}, nil
			case 2:
				half := "IPv4 Active Routing Table\nSubnet Netmask Gateway\n" +
					"10.99.0.0          255.255.255.0      Session 1\n"
				return map[string]interface{}{"data": half, "prompt": "", "busy": true}, nil
			default:
				return map[string]interface{}{"data": routeTable, "prompt": "msf > ", "busy": false}, nil
			}
		}
		return map[string]interface{}{"data": "", "prompt": "msf > ", "busy": false}, nil
	})

	e := New(Config{RPC: fake, SessionInterval: time.Hour, JobInterval: time.Hour,
		OutputInterval: 5 * time.Millisecond, RefreshInterval: time.Hour,
		RouteInterval: 15 * time.Millisecond})
	t.Cleanup(e.Shutdown)

	if err := e.Connect(context.Background(), protocol.ConnectParams{Host: "h", Port: 55553, Password: "p"}); err != nil {
		t.Fatalf("connect: %+v", err)
	}
	waitFor(t, func() bool { return len(e.State().Routes) == 2 })

	// the busy poll and at least one full poll after it must settle without
	// ever logging the half table as removals
	waitFor(t, func() bool { return atomic.LoadInt32(&polls) >= 3 })
	for _, ev := range e.State().Events {
		if ev != nil && strings.Contains(ev.Text, "route removed") {
			t.Fatalf("busy partial table was parsed as removals: %s", ev.Text)
		}
	}
	if got := e.State().Routes; len(got) != 2 {
		t.Fatalf("routes after busy poll: %+v", got)
	}
}

// An autoroute job ending means the route table just changed; the poller
// must re-poll then instead of waiting out its full interval.
func TestAutorouteJobEndKicksRoutePoll(t *testing.T) {
	fake := stdFake()
	var consoles int32
	fake.set(gomsf.ConsoleCreate, func(args ...interface{}) (interface{}, error) {
		return map[string]interface{}{"id": string(rune('0' + atomic.AddInt32(&consoles, 1) - 1))}, nil
	})
	var table atomic.Value
	table.Store("[*] There are currently no routes defined.\n")
	var routeWrites int32
	fake.set(gomsf.ConsoleWrite, func(args ...interface{}) (interface{}, error) {
		if len(args) > 0 && args[0] == "1" {
			atomic.AddInt32(&routeWrites, 1)
		}
		return map[string]interface{}{"wrote": true}, nil
	})
	fake.set(gomsf.ConsoleRead, func(args ...interface{}) (interface{}, error) {
		if len(args) > 0 && args[0] == "1" {
			return map[string]interface{}{"data": table.Load().(string), "prompt": "msf > ", "busy": false}, nil
		}
		return map[string]interface{}{"data": "", "prompt": "msf > ", "busy": false}, nil
	})
	var jobs atomic.Value
	jobs.Store(map[string]interface{}{})
	fake.set(gomsf.JobList, func(args ...interface{}) (interface{}, error) {
		return jobs.Load().(map[string]interface{}), nil
	})

	// the route ticker would never fire; only a kick can surface the change
	e := New(Config{RPC: fake, SessionInterval: time.Hour, JobInterval: 5 * time.Millisecond,
		OutputInterval: 5 * time.Millisecond, RefreshInterval: time.Hour,
		RouteInterval: time.Hour})
	t.Cleanup(e.Shutdown)
	sub := e.Subscribe()
	defer sub.Stop()

	if err := e.Connect(context.Background(), protocol.ConnectParams{Host: "h", Port: 55553, Password: "p"}); err != nil {
		t.Fatalf("connect: %+v", err)
	}
	waitFor(t, func() bool { return atomic.LoadInt32(&routeWrites) >= 1 }) // bootstrap poll done

	jobs.Store(map[string]interface{}{"7": "Post: multi/manage/autoroute"})
	waitFor(t, func() bool { return e.State().Jobs["7"] != nil }) // job started, then ends
	table.Store(routeTable)
	jobs.Store(map[string]interface{}{})

	waitFor(t, func() bool { return len(e.State().Routes) == 2 })
	waitEvent(t, sub, "route added: 10.99.0.0/24 through session 1")
	if got := atomic.LoadInt32(&routeWrites); got < 2 {
		t.Fatalf("autoroute job end must re-poll routes, got %d route console writes", got)
	}
}

func TestRoutePollTracksChanges(t *testing.T) {
	fake := stdFake()
	var consoles int32
	fake.set(gomsf.ConsoleCreate, func(args ...interface{}) (interface{}, error) {
		return map[string]interface{}{"id": string(rune('0' + atomic.AddInt32(&consoles, 1) - 1))}, nil
	})
	var table atomic.Value
	table.Store(routeTable)
	fake.set(gomsf.ConsoleRead, func(args ...interface{}) (interface{}, error) {
		if len(args) > 0 && args[0] == "1" { // route console, not the operator's
			return map[string]interface{}{"data": table.Load().(string), "prompt": "msf > ", "busy": false}, nil
		}
		return map[string]interface{}{"data": "", "prompt": "msf > ", "busy": false}, nil
	})

	e := New(Config{RPC: fake, SessionInterval: time.Hour, JobInterval: time.Hour,
		OutputInterval: 5 * time.Millisecond, RefreshInterval: time.Hour,
		RouteInterval: 15 * time.Millisecond})
	t.Cleanup(e.Shutdown)
	sub := e.Subscribe()
	defer sub.Stop()

	if err := e.Connect(context.Background(), protocol.ConnectParams{Host: "h", Port: 55553, Password: "p"}); err != nil {
		t.Fatalf("connect: %+v", err)
	}

	waitFor(t, func() bool {
		routes := e.State().Routes
		return len(routes) == 2 && routes[0].Subnet == "10.99.0.0/24"
	})
	waitEvent(t, sub, "route added: 10.99.0.0/24 through session 1")

	table.Store("[*] There are currently no routes defined.\n")
	waitFor(t, func() bool { return len(e.State().Routes) == 0 })
	waitEvent(t, sub, "route removed: 192.168.0.0/16 through session 3")

	e.Disconnect()
	if got := e.State().Routes; len(got) != 0 {
		t.Fatalf("routes must clear on disconnect, got %+v", got)
	}
}
