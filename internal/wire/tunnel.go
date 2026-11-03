package wire

import (
	"context"
	"errors"
)

// ErrTunnelClosed is returned by Tunnel methods after Close.
var ErrTunnelClosed = errors.New("wire: tunnel closed")

// Tunnel is one established control-plane connection. Packets are delivered
// whole: framing is the transport's problem, not the caller's.
type Tunnel interface {
	Send(ctx context.Context, p *Packet) error
	Recv(ctx context.Context) (*Packet, error)
	RemoteAddr() string
	LocalAddr() string
	Close() error
}

// Listener accepts control-plane connections.
type Listener interface {
	Accept(ctx context.Context) (Tunnel, error)
	Addr() string
	Close() error
}
