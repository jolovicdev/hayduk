package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jolovicdev/hayduk/internal/protocol"
)

func TestNormalizeScanTargets(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"192.168.1.10", "192.168.1.10", true},
		{"192.168.1.0/24", "192.168.1.0/24", true},
		{"10.0.0.0/8, 172.16.0.0/12", "10.0.0.0/8 172.16.0.0/12", true},
		{"2001:db8::/32", "2001:db8::/32", true},
		{"vulnbox.local", "vulnbox.local", true},
		{"host-01.example.com 10.0.0.5", "host-01.example.com 10.0.0.5", true},
		{"", "", false},
		{"10.0.0.1; id", "", false},
		{"-Pn 10.0.0.1", "", false},
		{"10.0.0.0/99", "", false},
		{"bad_host", "", false},
		{"-sV", "", false},
		{"a b;c", "", false},
	}
	for _, tc := range cases {
		got, err := normalizeScanTargets(tc.in)
		if tc.ok && err != nil {
			t.Errorf("normalizeScanTargets(%q) failed: %v", tc.in, err)
		}
		if !tc.ok && err == nil {
			t.Errorf("normalizeScanTargets(%q) accepted injection input", tc.in)
		}
		if tc.ok && got != tc.want {
			t.Errorf("normalizeScanTargets(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNormalizeScanPorts(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"", "", true},
		{"22", "22", true},
		{"22,80,443", "22,80,443", true},
		{"8000-8100, 22", "8000-8100,22", true},
		{" 1 - 65535 ", "", false}, // spaces inside a range are not nmap syntax
		{"0", "", false},
		{"65536", "", false},
		{"443-80", "", false},
		{"22; id", "", false},
		{"22,", "", false},
	}
	for _, tc := range cases {
		got, err := normalizeScanPorts(tc.in)
		if tc.ok && err != nil {
			t.Errorf("normalizeScanPorts(%q) failed: %v", tc.in, err)
		}
		if !tc.ok && err == nil {
			t.Errorf("normalizeScanPorts(%q) accepted invalid input", tc.in)
		}
		if tc.ok && got != tc.want {
			t.Errorf("normalizeScanPorts(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestServiceScanCommand(t *testing.T) {
	got := serviceScanCommand("10.0.0.5", "22,80", false)
	want := "db_nmap -sV --version-light -T4 --open -p 22,80 10.0.0.5"
	if got != want {
		t.Fatalf("command = %q, want %q", got, want)
	}
	if !strings.Contains(serviceScanCommand("10.0.0.0/24", "", true), "--top-ports 1000") {
		t.Fatal("empty ports should scan nmap's top 1000")
	}
	if !strings.Contains(serviceScanCommand("10.0.0.0/24", "", true), "-Pn") {
		t.Fatal("skip discovery should add -Pn")
	}
}

func TestServiceScanFailure(t *testing.T) {
	cases := map[string]string{
		"[-] Database not connected":                      "database is not connected",
		"[!] Could not find nmap. Please install nmap.":   "nmap is not installed",
		"sh: 1: nmap: not found":                          "nmap is not installed",
		"[-] Unknown command: db_nmap.":                   "no db_nmap",
		"[*] Nmap done: 1 IP address (1 host up) scanned": "",
	}
	for output, want := range cases {
		got := serviceScanFailure(output)
		if want == "" {
			if got != "" {
				t.Errorf("serviceScanFailure(%q) = %q, want no error", output, got)
			}
			continue
		}
		if !strings.Contains(got, want) {
			t.Errorf("serviceScanFailure(%q) = %q, want %q", output, got, want)
		}
	}
}

func TestOpenPortCount(t *testing.T) {
	out := `
[*] Nmap: 22/tcp   open  ssh     OpenSSH 8.9p1
[*] Nmap: 80/tcp   open  http    Apache httpd 2.4.7
[*] Nmap: 8080/tcp closed http-proxy
`
	if got := openPortCount(out); got != 2 {
		t.Fatalf("openPortCount = %d, want 2", got)
	}
}

func TestServicesScanRejectsInjectionBeforeStarting(t *testing.T) {
	e := connectedEngine(t, stdFake())
	_, eb := e.Exec(context.Background(), "", protocol.MethodServicesScan,
		json.RawMessage(`{"target":"10.0.0.1; touch /tmp/pwned"}`))
	if eb == nil || eb.Code != protocol.CodeBadParams {
		t.Fatalf("want bad_params, got %+v", eb)
	}
	e.mu.Lock()
	running := e.scanRunning
	e.mu.Unlock()
	if running {
		t.Fatal("invalid scan started a worker")
	}
}

func TestServicesScanHonorsSingleScanSlot(t *testing.T) {
	e := connectedEngine(t, stdFake())
	e.mu.Lock()
	e.scanRunning = true
	e.mu.Unlock()
	_, eb := e.Exec(context.Background(), "", protocol.MethodServicesScan,
		json.RawMessage(`{"target":"10.0.0.1"}`))
	if eb == nil || eb.Code != protocol.CodeBusy {
		t.Fatalf("want busy, got %+v", eb)
	}
}
