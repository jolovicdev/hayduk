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
