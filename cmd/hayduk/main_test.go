package main

import "testing"

// Team mode must catch every bind that can only serve this machine, including
// loopback by hostname: "localhost" is not a literal IP.
func TestLoopbackOnly(t *testing.T) {
	cases := []struct {
		host string
		want bool
	}{
		{"127.0.0.1", true},
		{"127.255.0.1", true},
		{"::1", true},
		{"::1%lo", true}, // scoped loopback: ParseIP rejects it, Listen binds it
		{"fe80::1%eth0", false},
		{"localhost", true},
		{"", false}, // wildcard bind: every interface
		{"0.0.0.0", false},
		{"::", false},
		{"192.168.1.10", false},
	}
	for _, c := range cases {
		if got := loopbackOnly(c.host); got != c.want {
			t.Errorf("loopbackOnly(%q) = %v, want %v", c.host, got, c.want)
		}
	}
}

// Team mode must also refuse wildcard binds: the advertised link carries the
// listen host, and [::] or 0.0.0.0 is not an address a remote operator can
// open.
func TestWildcardHost(t *testing.T) {
	cases := []struct {
		host string
		want bool
	}{
		{"", true}, // ":8787" binds every interface
		{"0.0.0.0", true},
		{"::", true},
		{"::ffff:0.0.0.0", true}, // mapped unspecified
		{"127.0.0.1", false},
		{"::1", false},
		{"192.168.1.10", false},
		{"localhost", false}, // a name, not an address; Listen resolves it
	}
	for _, c := range cases {
		if got := wildcardHost(c.host); got != c.want {
			t.Errorf("wildcardHost(%q) = %v, want %v", c.host, got, c.want)
		}
	}
}

func TestParseOperatorTokens(t *testing.T) {
	if m, err := parseOperatorTokens(nil, false); err != nil || m != nil {
		t.Fatalf("empty specs: got (%v, %v), want (nil, nil)", m, err)
	}
	if _, err := parseOperatorTokens([]string{"alice=0123456789abcdef"}, false); err == nil {
		t.Fatal("accepted --operator without --team")
	}
	if _, err := parseOperatorTokens([]string{"alice=short"}, true); err == nil {
		t.Fatal("accepted token shorter than 16 chars")
	}
	if _, err := parseOperatorTokens([]string{"alice=0123456789abcdef", "alice=fedcba9876543210"}, true); err == nil {
		t.Fatal("accepted duplicate operator name")
	}
	if _, err := parseOperatorTokens([]string{"alice=0123456789abcdef", "bob=0123456789abcdef"}, true); err == nil {
		t.Fatal("accepted duplicate token")
	}
	got, err := parseOperatorTokens([]string{"alice=0123456789abcdef", "bob=fedcba9876543210"}, true)
	if err != nil {
		t.Fatalf("valid specs failed: %v", err)
	}
	if got["0123456789abcdef"] != "alice" || got["fedcba9876543210"] != "bob" {
		t.Fatalf("unexpected mapping: %+v", got)
	}
}
