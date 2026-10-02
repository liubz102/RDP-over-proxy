// Package testutil holds helpers that tests in several packages share. It is
// imported only by _test.go files.
package testutil

import (
	"io"
	"net"
	"sync"
	"testing"

	"github.com/liubz102/RDP-over-proxy/internal/probe"
)

// Answer is how a fake RDP server responds to a Connection Request.
type Answer int

const (
	// AnswerConfirm sends a Connection Confirm that selects
	// RDPOptions.Selected, then echoes every byte it receives until the
	// client closes, so tunnel tests can check that data flows both ways.
	AnswerConfirm Answer = iota
	// AnswerLegacy sends a Connection Confirm without negotiation data, as a
	// server that only knows standard RDP security does, then echoes.
	AnswerLegacy
	// AnswerFailure sends a negotiation failure with RDPOptions.Failure and
	// closes.
	AnswerFailure
	// AnswerNotRDP sends an HTTP error response and closes.
	AnswerNotRDP
	// AnswerTruncated sends the first half of a Connection Confirm and
	// closes.
	AnswerTruncated
	// AnswerClose closes without answering.
	AnswerClose
	// AnswerNothing keeps the connection open and never answers, until the
	// client closes it.
	AnswerNothing
)

// RDPOptions configure a fake RDP server.
type RDPOptions struct {
	Answer   Answer
	Selected probe.Protocols
	Failure  probe.FailureCode
}

// requestBuffer is how many Connection Requests the server keeps for
// Requests before a further connection waits for the test to read one.
const requestBuffer = 64

// RDPServer is a fake RDP server on 127.0.0.1 with a port of its own. It
// handles each connection's Connection Request as RDPOptions say. It stops,
// closing every connection, when the test ends.
type RDPServer struct {
	// Addr is the host:port to connect to.
	Addr string

	opts     RDPOptions
	ln       net.Listener
	requests chan probe.ConnectionRequest
	done     chan struct{}
	wg       sync.WaitGroup

	mu     sync.Mutex
	conns  map[net.Conn]struct{}
	closed bool
}

// NewRDPServer starts a fake RDP server for the test.
func NewRDPServer(t testing.TB, opts RDPOptions) *RDPServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("fake RDP server: %v", err)
	}
	s := &RDPServer{
		Addr:     ln.Addr().String(),
		opts:     opts,
		ln:       ln,
		requests: make(chan probe.ConnectionRequest, requestBuffer),
		done:     make(chan struct{}),
		conns:    map[net.Conn]struct{}{},
	}
	t.Cleanup(s.close)
	s.wg.Go(s.serve)
	return s
}

// Requests delivers every Connection Request the server receives, in the
// order they arrive. Receiving from it is how a test waits until a client's
// request has reached the server.
func (s *RDPServer) Requests() <-chan probe.ConnectionRequest { return s.requests }

func (s *RDPServer) serve() {
	for {
		c, err := s.ln.Accept()
		if err != nil {
			return // the listener was closed
		}
		if !s.track(c) {
			c.Close()
			return
		}
		s.wg.Go(func() {
			defer s.untrack(c)
			s.handle(c)
		})
	}
}

func (s *RDPServer) handle(c net.Conn) {
	pkt, err := probe.ReadTPKT(c)
	if err != nil {
		return
	}
	req, err := probe.ParseConnectionRequest(pkt)
	if err != nil {
		return
	}
	select {
	case s.requests <- req:
	case <-s.done:
		return
	}
	switch s.opts.Answer {
	case AnswerConfirm:
		if _, err := c.Write(probe.ConnectionConfirm{Negotiated: true, Selected: s.opts.Selected}.Marshal()); err == nil {
			_, _ = io.Copy(c, c)
		}
	case AnswerLegacy:
		if _, err := c.Write(probe.ConnectionConfirm{}.Marshal()); err == nil {
			_, _ = io.Copy(c, c)
		}
	case AnswerFailure:
		_, _ = c.Write(probe.ConnectionConfirm{Failure: s.opts.Failure}.Marshal())
	case AnswerNotRDP:
		_, _ = c.Write([]byte("HTTP/1.1 400 Bad Request\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"))
	case AnswerTruncated:
		cc := probe.ConnectionConfirm{Negotiated: true, Selected: s.opts.Selected}.Marshal()
		_, _ = c.Write(cc[:len(cc)/2])
	case AnswerClose:
	case AnswerNothing:
		_, _ = io.Copy(io.Discard, c)
	}
}

// track registers a connection so close can end it; it refuses once the
// server is closing.
func (s *RDPServer) track(c net.Conn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	s.conns[c] = struct{}{}
	return true
}

func (s *RDPServer) untrack(c net.Conn) {
	c.Close()
	s.mu.Lock()
	delete(s.conns, c)
	s.mu.Unlock()
}

// close stops accepting, ends every open connection and waits until all of
// the server's goroutines have returned.
func (s *RDPServer) close() {
	s.mu.Lock()
	s.closed = true
	for c := range s.conns {
		c.Close()
	}
	s.mu.Unlock()
	close(s.done)
	s.ln.Close()
	s.wg.Wait()
}
