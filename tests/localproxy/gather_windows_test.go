package localproxy_test

import (
	. "github.com/liubz102/RDP-over-proxy/internal/localproxy"

	"bufio"
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/liubz102/RDP-over-proxy/tests/testutil"
)

func TestMain(m *testing.M) {
	testutil.RunHelper()
	os.Exit(m.Run())
}

// A stand-in proxy program under a proxy core's file name is found on this
// computer and answers as SOCKS5. Gather only reads; the only port probed is
// the stand-in's.
func TestGatherFindsAProxyProgram(t *testing.T) {
	cmd := testutil.HelperCommandNamed(t, testutil.HelperSocks, "xray.exe")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	var port int
	if line, err := bufio.NewReader(stdout).ReadString('\n'); err != nil {
		t.Fatalf("the helper did not start: %v", err)
	} else if _, err := fmt.Sscanf(line, "ready %d", &port); err != nil {
		t.Fatalf("the helper said %q", line)
	}

	f, err := Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	var found *Candidate
	for _, c := range Candidates(f) {
		if c.Port == port && c.Source == SourceProgram {
			found = &c
		}
	}
	// Named after its file: none of the processes that started it is a
	// known app.
	if found == nil || found.Name != "xray" || len(found.Hosts) != 1 || found.Hosts[0] != "127.0.0.1" {
		t.Fatalf("the stand-in on port %d is not a candidate as expected: %+v", port, found)
	}
	got, err := Probe(context.Background(), found.Hosts, found.Port)
	if err != nil || !got.Socks5 || got.Password {
		t.Fatalf("Probe = %+v, %v", got, err)
	}

	stdin.Close()
	if err := cmd.Wait(); err != nil {
		t.Fatalf("the helper: %v", err)
	}
}
