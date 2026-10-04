package main

import "testing"

func TestIsLoopbackAddr(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1:6061": true,
		"localhost:6061": true,
		"[::1]:6061":     true,
		"0.0.0.0:6061":   false,
		":6061":          false,
		"10.0.0.5:6061":  false,
		"127.0.0.1":      false,
	}
	for addr, want := range cases {
		if got := isLoopbackAddr(addr); got != want {
			t.Errorf("isLoopbackAddr(%q) = %v, want %v", addr, got, want)
		}
	}
}
