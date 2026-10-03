package testutil

import (
	"net"
	"testing"
)

// FreePort returns a TCP port on 127.0.0.1 that was free a moment ago.
//
// Use it only for a server that cannot listen on port 0 and report the port
// it got, such as an Xray inbound. Another program could take the port in
// between; tests accept that small race. This is one of the exceptions to
// the event-driven rule registered in CLAUDE.md.
func FreePort(t testing.TB) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find a free port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port
}
