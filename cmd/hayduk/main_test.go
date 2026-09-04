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
		{"", true},          // ":8787" binds every interface
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
