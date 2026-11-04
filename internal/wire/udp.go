package wire

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"sync"
	"time"
)

// A UDP datagram carries the same frame the TCP transport uses: a little endian
// uint32 body length followed by the encoded packet. Keeping one framing means a
// peer can switch transports without a second parser.
//
// UDP has no connections, so the listener keeps one synthetic tunnel per remote
// address and hands it to Accept the first time that address speaks. Idle peers
// are evicted so a flood of source addresses cannot grow the map forever.

const udpIdleTimeout = 2 * time.Minute

// errSkipDatagram marks a datagram that must be dropped rather than returned:
// short frames, absurd lengths and undecodable bodies are noise, not errors
// (review team, rounds 150 and 161).
var errSkipDatagram = errors.New("wire: skip datagram")

// maxUDPTunnels bounds how many peers one datagram port can pin. The web
// socket device channel has a slot limit of 1024; the datagram side used to
// have none, so a source port sweep grew the map and the goroutines with it
// (review team, round 150).
const maxUDPTunnels = 1024
const udpReadBuffer = 2048

type udpTunnel struct {
	conn   *net.UDPConn
	remote *net.UDPAddr
	local  string

	inbound chan *Packet
	closed  chan struct{}
	once    sync.Once
	writeMu sync.Mutex

	lastSeenMu sync.Mutex
	lastSeen   time.Time

	// easyTier is set when the peer opened the session with the upstream header
	// instead of our length prefixed frame; connID is the id it chose.
	easyTier bool
	connID   uint32

	// onClose detaches the tunnel from the listener map. Without it a tunnel
	// closed by the session layer stayed in the map, and every later datagram
	// from that address was dropped into a dead tunnel (round 146).
	onClose func()
}

// IsDatagram lets the RPC layer know duplicates are possible on this transport,
// so it can turn on request deduplication.
func (t *udpTunnel) IsDatagram() bool { return true }

// TransportName lets the session registry keep one tunnel per transport.
func (t *udpTunnel) TransportName() string { return "udp" }

func (t *udpTunnel) RemoteAddr() string { return t.remote.String() }
func (t *udpTunnel) LocalAddr() string  { return t.local }

func (t *udpTunnel) touch() {
	t.lastSeenMu.Lock()
	t.lastSeen = time.Now()
	t.lastSeenMu.Unlock()
}

func (t *udpTunnel) idleFor() time.Duration {
	t.lastSeenMu.Lock()
	defer t.lastSeenMu.Unlock()
	return time.Since(t.lastSeen)
}

func (t *udpTunnel) deliver(p *Packet) {
	select {
	case t.inbound <- p:
	default:
		// A peer that stops reading must not block the reader goroutine.
	}
}

func (t *udpTunnel) Send(ctx context.Context, p *Packet) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	body := p.Encode()
	if len(body) > UDPMTUBytes {
		return fmt.Errorf("%w: %d > %d", ErrOversizePacket, len(body), UDPMTUBytes)
	}
	if t.easyTier {
		frame := encodeEasyTierDatagram(t.connID, etUDPData, body)
		t.writeMu.Lock()
		defer t.writeMu.Unlock()
		if _, err := t.conn.WriteToUDP(frame, t.remote); err != nil {
			return fmt.Errorf("wire: udp write: %w", err)
		}
		return nil
	}
	frame := make([]byte, TCPFrameHeaderSize+len(body))
	binary.LittleEndian.PutUint32(frame[0:4], uint32(len(body)))
	copy(frame[TCPFrameHeaderSize:], body)

	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	if _, err := t.conn.WriteToUDP(frame, t.remote); err != nil {
		return fmt.Errorf("wire: udp write: %w", err)
	}
	return nil
}

func (t *udpTunnel) Recv(ctx context.Context) (*Packet, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-t.closed:
		return nil, ErrTunnelClosed
	case packet := <-t.inbound:
		return packet, nil
	}
}

func (t *udpTunnel) isClosed() bool {
	select {
	case <-t.closed:
		return true
	default:
		return false
	}
}

func (t *udpTunnel) Close() error {
	t.once.Do(func() {
		close(t.closed)
		if t.onClose != nil {
			t.onClose()
		}
	})
	return nil
}

type udpListener struct {
	conn   *net.UDPConn
	addr   string
	accept chan Tunnel
	errs   chan error
	done   chan struct{}
	once   sync.Once

	mu      sync.Mutex
	tunnels map[string]*udpTunnel
	limit   int

	// writeMu serialises replies so a Sack cannot interleave with a Data frame.
	writeMu sync.Mutex
}

// ListenUDP binds a datagram endpoint that speaks the framed control protocol.
func ListenUDP(addr string) (Listener, error) {
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, fmt.Errorf("wire: resolve udp %s: %w", addr, err)
	}
	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return nil, fmt.Errorf("wire: listen udp %s: %w", addr, err)
	}
	l := &udpListener{
		conn:    conn,
		addr:    conn.LocalAddr().String(),
		accept:  make(chan Tunnel, 16),
		errs:    make(chan error, 1),
		done:    make(chan struct{}),
		tunnels: map[string]*udpTunnel{},
	}
	go l.read()
	go l.reap()
	return l, nil
}

func (l *udpListener) Addr() string { return l.addr }

func (l *udpListener) read() {
	buffer := make([]byte, udpReadBuffer)
	for {
		read, remote, err := l.conn.ReadFromUDP(buffer)
		if err != nil {
			select {
			case <-l.done:
			default:
				l.errs <- err
			}
			return
		}
		if session, ok := parseEasyTierDatagram(buffer[:read]); ok == nil {
			if debugUDP() {
				log.Printf("wire: et datagram type=%d len=%d body=%d", session.msgType, len(buffer[:read]), len(session.body))
			}
			l.handleEasyTier(session, remote)
			continue
		}
		if debugUDP() && read >= 8 {
			log.Printf("wire: rejected datagram len=%d head=%x", read, buffer[:8])
		}
		packet, err := parseDatagramFrame(buffer, read)
		if errors.Is(err, errSkipDatagram) {
			// One rule, one implementation: the dialing side uses the same
			// decoder, so a change here cannot drift from it (round 181).
			continue
		}
		if err != nil {
			l.errs <- err
			return
		}
		l.deliver(packet, remote, nil)
	}
}

// handleEasyTier answers the upstream session handshake and feeds Data frames to
// the tunnel. Without the Sack echo the client resends the same Syn forever.
func (l *udpListener) handleEasyTier(session easyTierDatagram, remote *net.UDPAddr) {
	switch session.msgType {
	case etUDPSyn:
		reply := encodeEasyTierDatagram(session.connID, etUDPSack, session.body)
		_ = l.writeTo(reply, remote)
		// The handshake alone is enough to accept the peer: a client that sends the
		// Syn and nothing else still has to become a session, otherwise the caller
		// waits for a Data frame that may never come.
		l.deliver(nil, remote, &session)
	case etUDPFin:
		l.forget(remote.String())
	case etUDPData:
		packet, err := DecodePacket(session.body)
		if err != nil {
			// A malformed body must not take the listener down.
			return
		}
		l.deliver(packet, remote, &session)
	}
}

// writeTo sends a datagram back to one peer.
func (l *udpListener) writeTo(data []byte, remote *net.UDPAddr) error {
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	_, err := l.conn.WriteToUDP(data, remote)
	return err
}
func (l *udpListener) deliver(packet *Packet, remote *net.UDPAddr, session *easyTierDatagram) {
	key := remote.String()
	l.mu.Lock()
	tunnel, known := l.tunnels[key]
	if known && tunnel.isClosed() {
		delete(l.tunnels, key)
		known = false
	}
	if !known && l.limit > 0 && len(l.tunnels) >= l.limit {
		// At the ceiling the datagram is dropped rather than served: an open
		// port must not let a source sweep pin unbounded goroutines.
		l.mu.Unlock()
		return
	}
	if !known {
		tunnel = &udpTunnel{
			conn:     l.conn,
			remote:   remote,
			local:    l.addr,
			inbound:  make(chan *Packet, 16),
			closed:   make(chan struct{}),
			lastSeen: time.Now(),
		}
		if session != nil {
			tunnel.easyTier = true
			tunnel.connID = session.connID
		}
		created := tunnel
		tunnel.onClose = func() { l.detach(key, created) }
		l.tunnels[key] = tunnel
	}
	l.mu.Unlock()

	tunnel.touch()
	if !known {
		select {
		case l.accept <- tunnel:
		default:
			// Nobody is accepting: drop the peer rather than queue forever.
			l.forget(key)
			return
		}
	}
	if packet == nil {
		return
	}
	tunnel.deliver(packet)
}

// detach removes the mapping only when it still points at this tunnel, so a
// newer tunnel for the same peer is never evicted by an older one closing.
func (l *udpListener) detach(key string, tunnel *udpTunnel) {
	l.mu.Lock()
	current, ok := l.tunnels[key]
	if ok && current == tunnel {
		delete(l.tunnels, key)
	}
	l.mu.Unlock()
}

func (l *udpListener) forget(key string) {
	l.mu.Lock()
	tunnel, ok := l.tunnels[key]
	delete(l.tunnels, key)
	l.mu.Unlock()
	if ok {
		_ = tunnel.Close()
	}
}

func (l *udpListener) reap() {
	ticker := time.NewTicker(udpIdleTimeout / 2)
	defer ticker.Stop()
	for {
		select {
		case <-l.done:
			return
		case <-ticker.C:
			l.mu.Lock()
			stale := make([]string, 0, len(l.tunnels))
			for key, tunnel := range l.tunnels {
				if tunnel.idleFor() > udpIdleTimeout {
					stale = append(stale, key)
				}
			}
			l.mu.Unlock()
			for _, key := range stale {
				l.forget(key)
			}
		}
	}
}

func (l *udpListener) Accept(ctx context.Context) (Tunnel, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case err := <-l.errs:
		return nil, fmt.Errorf("wire: udp listener: %w", err)
	case tunnel := <-l.accept:
		return tunnel, nil
	case <-l.done:
		return nil, ErrTunnelClosed
	}
}

func (l *udpListener) Close() error {
	var err error
	l.once.Do(func() {
		close(l.done)
		err = l.conn.Close()
		// Collect under the lock, close outside it: tunnel.Close runs the onClose
		// callback, which takes this same (non reentrant) mutex to detach itself.
		// Closing inside the loop deadlocked as soon as one peer was still mapped
		// (found by the review team in round 150).
		l.mu.Lock()
		pending := make([]*udpTunnel, 0, len(l.tunnels))
		for key, tunnel := range l.tunnels {
			delete(l.tunnels, key)
			pending = append(pending, tunnel)
		}
		l.mu.Unlock()
		for _, tunnel := range pending {
			_ = tunnel.Close()
		}
	})
	if errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}

// udpClientTunnel is the dialing side: one connected socket, the same framing.
type udpClientTunnel struct {
	conn    *net.UDPConn
	remote  string
	local   string
	once    sync.Once
	closed  chan struct{}
	writeMu sync.Mutex
	// readMu serialises Recv: it changes the socket read deadline, which is
	// connection state, so two readers would clear each other's timeout and
	// steal each other's datagrams (review team, round 160).
	readMu sync.Mutex
}

// DialUDP opens a datagram control channel to addr.
func DialUDP(ctx context.Context, addr string) (Tunnel, error) {
	remote, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, fmt.Errorf("wire: resolve udp %s: %w", addr, err)
	}
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "udp", remote.String())
	if err != nil {
		return nil, fmt.Errorf("wire: dial udp %s: %w", addr, err)
	}
	udpConn, ok := conn.(*net.UDPConn)
	if !ok {
		_ = conn.Close()
		return nil, fmt.Errorf("wire: udp dial returned %T", conn)
	}
	return &udpClientTunnel{
		conn:   udpConn,
		remote: udpConn.RemoteAddr().String(),
		local:  udpConn.LocalAddr().String(),
		closed: make(chan struct{}),
	}, nil
}

func (t *udpClientTunnel) IsDatagram() bool { return true }

func (t *udpClientTunnel) TransportName() string { return "udp" }

func (t *udpClientTunnel) RemoteAddr() string { return t.remote }
func (t *udpClientTunnel) LocalAddr() string  { return t.local }

func (t *udpClientTunnel) Send(ctx context.Context, p *Packet) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	body := p.Encode()
	if len(body) > UDPMTUBytes {
		return fmt.Errorf("%w: %d > %d", ErrOversizePacket, len(body), UDPMTUBytes)
	}
	frame := make([]byte, TCPFrameHeaderSize+len(body))
	binary.LittleEndian.PutUint32(frame[0:4], uint32(len(body)))
	copy(frame[TCPFrameHeaderSize:], body)
	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	if _, err := t.conn.Write(frame); err != nil {
		return fmt.Errorf("wire: udp write: %w", err)
	}
	return nil
}

func (t *udpClientTunnel) Recv(ctx context.Context) (*Packet, error) {
	t.readMu.Lock()
	defer t.readMu.Unlock()
	buffer := make([]byte, udpReadBuffer)
	for {
		if deadline, ok := ctx.Deadline(); ok {
			_ = t.conn.SetReadDeadline(deadline)
		} else {
			_ = t.conn.SetReadDeadline(time.Time{})
		}
		read, err := t.conn.Read(buffer)
		if err != nil {
			select {
			case <-t.closed:
				return nil, ErrTunnelClosed
			default:
			}
			return nil, fmt.Errorf("wire: udp read: %w", err)
		}
		packet, err := parseDatagramFrame(buffer, read)
		if errors.Is(err, errSkipDatagram) {
			// Noise is skipped by looping, never by recursing: Recv holds readMu,
			// which is not reentrant, so a recursive call deadlocked the reader
			// on the first malformed datagram (review team, round 180).
			continue
		}
		return packet, err
	}
}

// parseDatagramFrame decodes one datagram, or reports errSkipDatagram when the
// frame is noise that the reader should ignore.
func parseDatagramFrame(buffer []byte, read int) (*Packet, error) {
	if read < TCPFrameHeaderSize {
		return nil, errSkipDatagram
	}
	bodyLen := int(binary.LittleEndian.Uint32(buffer[0:TCPFrameHeaderSize]))
	if bodyLen < PeerHeaderSize || bodyLen > UDPMTUBytes || TCPFrameHeaderSize+bodyLen > read {
		return nil, errSkipDatagram
	}
	packet, err := DecodePacket(buffer[TCPFrameHeaderSize : TCPFrameHeaderSize+bodyLen])
	if err != nil {
		return nil, errSkipDatagram
	}
	return packet, nil
}

func (t *udpClientTunnel) Close() error {
	var err error
	t.once.Do(func() {
		close(t.closed)
		err = t.conn.Close()
	})
	if errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}

// debugUDP turns on per-datagram logging while chasing a client that never gets
// past the handshake.
func debugUDP() bool { return os.Getenv("EASYTIER_UDP_DEBUG") != "" }
