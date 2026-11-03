package wire

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

// tcpTunnel speaks the length-prefixed stream framing of
// easytier-core/src/tunnel/framed.rs: a little-endian uint32 body length
// (counting the peer header) followed by that many bytes.
type tcpTunnel struct {
	conn   net.Conn
	remote string
	local  string

	writeMu sync.Mutex
	readMu  sync.Mutex
	closed  bool
	closeMu sync.Mutex
}

func newTCPTunnel(conn net.Conn) *tcpTunnel {
	return &tcpTunnel{
		conn:   conn,
		remote: conn.RemoteAddr().String(),
		local:  conn.LocalAddr().String(),
	}
}

func (t *tcpTunnel) RemoteAddr() string { return t.remote }
func (t *tcpTunnel) LocalAddr() string  { return t.local }

func (t *tcpTunnel) Send(ctx context.Context, p *Packet) error {
	body := p.Encode()
	if len(body) > TCPMTUBytes {
		return fmt.Errorf("%w: %d > %d", ErrOversizePacket, len(body), TCPMTUBytes)
	}
	frame := make([]byte, TCPFrameHeaderSize+len(body))
	binary.LittleEndian.PutUint32(frame[0:4], uint32(len(body)))
	copy(frame[TCPFrameHeaderSize:], body)

	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	if deadline, ok := ctx.Deadline(); ok {
		_ = t.conn.SetWriteDeadline(deadline)
	} else {
		_ = t.conn.SetWriteDeadline(time.Time{})
	}
	if _, err := t.conn.Write(frame); err != nil {
		return fmt.Errorf("wire: tcp write: %w", err)
	}
	return nil
}

func (t *tcpTunnel) Recv(ctx context.Context) (*Packet, error) {
	t.readMu.Lock()
	defer t.readMu.Unlock()

	if deadline, ok := ctx.Deadline(); ok {
		_ = t.conn.SetReadDeadline(deadline)
	} else {
		_ = t.conn.SetReadDeadline(time.Time{})
	}
	var hdr [TCPFrameHeaderSize]byte
	if _, err := io.ReadFull(t.conn, hdr[:]); err != nil {
		return nil, fmt.Errorf("wire: tcp read header: %w", err)
	}
	bodyLen := int(binary.LittleEndian.Uint32(hdr[:]))
	if bodyLen < PeerHeaderSize || bodyLen > TCPMTUBytes {
		return nil, fmt.Errorf("%w: tcp body length %d", ErrOversizePacket, bodyLen)
	}
	body := make([]byte, bodyLen)
	if _, err := io.ReadFull(t.conn, body); err != nil {
		return nil, fmt.Errorf("wire: tcp read body: %w", err)
	}
	return DecodePacket(body)
}

func (t *tcpTunnel) Close() error {
	t.closeMu.Lock()
	defer t.closeMu.Unlock()
	if t.closed {
		return nil
	}
	t.closed = true
	return t.conn.Close()
}

type tcpListener struct {
	ln   net.Listener
	addr string
}

// ListenTCP binds a control-plane listener using the length-prefixed framing.
func ListenTCP(addr string) (Listener, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("wire: listen tcp %s: %w", addr, err)
	}
	return &tcpListener{ln: ln, addr: ln.Addr().String()}, nil
}

func (l *tcpListener) Addr() string { return l.addr }

func (l *tcpListener) Accept(ctx context.Context) (Tunnel, error) {
	type accepted struct {
		conn net.Conn
		err  error
	}
	ch := make(chan accepted, 1)
	go func() {
		conn, err := l.ln.Accept()
		ch <- accepted{conn: conn, err: err}
	}()
	select {
	case <-ctx.Done():
		_ = l.ln.Close()
		res := <-ch
		if res.conn != nil {
			_ = res.conn.Close()
		}
		return nil, ctx.Err()
	case res := <-ch:
		if res.err != nil {
			if errors.Is(res.err, net.ErrClosed) {
				return nil, ErrTunnelClosed
			}
			return nil, fmt.Errorf("wire: accept tcp: %w", res.err)
		}
		return newTCPTunnel(res.conn), nil
	}
}

func (l *tcpListener) Close() error { return l.ln.Close() }

// DialTCP connects to a control-plane listener using the same framing.
func DialTCP(ctx context.Context, addr string) (Tunnel, error) {
	dialer := net.Dialer{}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("wire: dial tcp %s: %w", addr, err)
	}
	return newTCPTunnel(conn), nil
}
