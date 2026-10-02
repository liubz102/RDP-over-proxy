package probe

import (
	"context"
	"fmt"
	"net"
	"time"
)

// Requested is what Check offers the server: the protocols a current mstsc
// offers, so the server picks what it would pick for mstsc.
const Requested = ProtocolSSL | ProtocolHybrid | ProtocolHybridEx

// ContextDialer opens connections, possibly through a proxy. *net.Dialer is
// one; so is the route to a target (package route, M3).
type ContextDialer interface {
	DialContext(ctx context.Context, network, address string) (net.Conn, error)
}

// Result is what a successful check learned.
type Result struct {
	// Elapsed runs from starting to connect until the Connection Confirm
	// arrived: how long the route and the server took to answer.
	Elapsed time.Duration
	Confirm ConnectionConfirm
}

// Check connects to address through d, sends a Connection Request and reads
// the server's Connection Confirm. Any well-formed confirm counts as success,
// including a negotiation failure: it proves the route works and an RDP
// server is listening, and mstsc can often still connect (for example by
// falling back to standard RDP security).
//
// Check waits as long as the route and the server take; there is no timeout.
// Cancel ctx to stop it, and Check returns ctx's error.
func Check(ctx context.Context, d ContextDialer, address string) (Result, error) {
	req, err := ConnectionRequest{Negotiate: true, Protocols: Requested}.Marshal()
	if err != nil {
		return Result{}, err
	}
	start := time.Now()
	conn, err := d.DialContext(ctx, "tcp", address)
	if err != nil {
		return Result{}, canceled(ctx, fmt.Errorf("connect: %w", err))
	}
	defer conn.Close()
	// Cancelling ctx closes the connection, which ends a blocked write or
	// read at once.
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()

	if _, err := conn.Write(req); err != nil {
		return Result{}, canceled(ctx, fmt.Errorf("send the connection request: %w", err))
	}
	pkt, err := ReadTPKT(conn)
	elapsed := time.Since(start)
	if err != nil {
		return Result{}, canceled(ctx, err)
	}
	cc, err := ParseConnectionConfirm(pkt)
	if err != nil {
		return Result{}, err
	}
	return Result{Elapsed: elapsed, Confirm: cc}, nil
}

// canceled reports ctx's error in place of err once ctx has ended: the
// failure was then caused by the cancellation closing the connection.
func canceled(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	return err
}
