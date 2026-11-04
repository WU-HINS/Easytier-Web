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

// TransportOf names the transport behind a tunnel. A device may hold several
// tunnels at once (web socket, tcp, udp), and the registry keys on this.
func TransportOf(t Tunnel) string {
	if named, ok := t.(interface{ TransportName() string }); ok {
		return named.TransportName()
	}
	return "tunnel"
}

// Listener accepts control-plane connections.
type Listener interface {
	Accept(ctx context.Context) (Tunnel, error)
	Addr() string
	Close() error
}
