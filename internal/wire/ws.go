package wire

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	wsReadLimit  = 1 << 20
	wsWriteLimit = 10 * time.Second
)

// wsTunnel maps one WebSocket connection onto a Tunnel. Every binary message
// is exactly one packet (peer header included), matching
// WebSocketPacketSink/WebSocketPacketStream in easytier/src/tunnel/websocket.rs.
type wsTunnel struct {
	conn   *websocket.Conn
	remote string
	local  string

	writeMu sync.Mutex
	readMu  sync.Mutex
}

// TransportName lets the session registry keep one tunnel per transport.
func (t *wsTunnel) TransportName() string { return "ws" }

func (t *wsTunnel) RemoteAddr() string { return t.remote }
func (t *wsTunnel) LocalAddr() string  { return t.local }

func (t *wsTunnel) Send(ctx context.Context, p *Packet) error {
	body := p.Encode()
	if len(body) > wsReadLimit {
		return fmt.Errorf("%w: %d bytes", ErrOversizePacket, len(body))
	}
	deadline := time.Now().Add(wsWriteLimit)
	if d, ok := ctx.Deadline(); ok {
		deadline = d
	}
	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	_ = t.conn.SetWriteDeadline(deadline)
	if err := t.conn.WriteMessage(websocket.BinaryMessage, body); err != nil {
		return fmt.Errorf("wire: ws write: %w", err)
	}
	return nil
}

func (t *wsTunnel) Recv(ctx context.Context) (*Packet, error) {
	deadline := time.Time{}
	if d, ok := ctx.Deadline(); ok {
		deadline = d
	}
	t.readMu.Lock()
	defer t.readMu.Unlock()
	_ = t.conn.SetReadDeadline(deadline)
	msgType, data, err := t.conn.ReadMessage()
	if err != nil {
		return nil, fmt.Errorf("wire: ws read: %w", err)
	}
	if msgType != websocket.BinaryMessage {
		return nil, fmt.Errorf("wire: ws non-binary message type %d", msgType)
	}
	return DecodePacket(data)
}

func (t *wsTunnel) Close() error { return t.conn.Close() }

type wsListener struct {
	ln     net.Listener
	server *http.Server
	addr   string
	accept chan Tunnel
	errs   chan error
	once   sync.Once
}

// ListenWS binds a WebSocket control-plane listener. Any request path is
// accepted; the Rust listener serves a single endpoint per socket.
// ListenWS accepts a trustProxy probe so the device port honours the same
// server.trust_proxy switch as the HTTP side. Before this the header was
// believed unconditionally, which let any unauthenticated peer pick the
// source address recorded in the audit view (review team, r136).
func ListenWS(addr string, trustProxy func() bool) (Listener, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("wire: listen ws %s: %w", addr, err)
	}
	l := &wsListener{
		ln:     ln,
		addr:   ln.Addr().String(),
		accept: make(chan Tunnel, 8),
		errs:   make(chan error, 1),
	}
	upgrader := websocket.Upgrader{
		ReadBufferSize:  4096,
		WriteBufferSize: 4096,
		CheckOrigin:     func(*http.Request) bool { return true },
	}
	mux := http.NewServeMux()
	handler := func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		conn.SetReadLimit(wsReadLimit)
		remote := conn.RemoteAddr().String()
		if trustProxy != nil && trustProxy() {
			if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
				remote = strings.TrimSpace(strings.Split(fwd, ",")[0])
			}
		}
		select {
		case l.accept <- &wsTunnel{conn: conn, remote: remote, local: ln.Addr().String()}:
		default:
			_ = conn.Close()
		}
	}
	mux.HandleFunc("/", handler)
	l.server = &http.Server{Handler: mux}
	go func() {
		if err := l.server.Serve(ln); err != nil && err != http.ErrServerClosed {
			select {
			case l.errs <- err:
			default:
			}
		}
	}()
	return l, nil
}

func (l *wsListener) Addr() string { return l.addr }

func (l *wsListener) Accept(ctx context.Context) (Tunnel, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case err := <-l.errs:
		return nil, fmt.Errorf("wire: ws serve: %w", err)
	case t := <-l.accept:
		return t, nil
	}
}

func (l *wsListener) Close() error {
	var err error
	l.once.Do(func() {
		err = l.server.Close()
	})
	return err
}

// DialWS connects to a WebSocket control plane (used by tests and by the
// console's own egress probes).
func DialWS(ctx context.Context, url string, header http.Header) (Tunnel, error) {
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	conn, resp, err := dialer.DialContext(ctx, url, header)
	if err != nil {
		if resp != nil {
			return nil, fmt.Errorf("wire: ws dial %s: %w (status %s)", url, err, resp.Status)
		}
		return nil, fmt.Errorf("wire: ws dial %s: %w", url, err)
	}
	conn.SetReadLimit(wsReadLimit)
	return &wsTunnel{conn: conn, remote: url, local: conn.LocalAddr().String()}, nil
}

// UpgradeWebSocket turns an inbound HTTP request into a Tunnel. The console
// mounts device upload channels on the same port as the web UI, so the
// listener lives in the HTTP router rather than in a second accept loop.
// The trustProxy probe has to travel with the request: the device channel on the
// web port used to believe X-Forwarded-For from anyone, which is the same hole
// that ListenWS had (review team, r140).
func UpgradeWebSocket(w http.ResponseWriter, r *http.Request, trustProxy func() bool) (Tunnel, error) {
	upgrader := websocket.Upgrader{
		ReadBufferSize:  4096,
		WriteBufferSize: 4096,
		CheckOrigin:     func(*http.Request) bool { return true },
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return nil, fmt.Errorf("wire: ws upgrade: %w", err)
	}
	conn.SetReadLimit(wsReadLimit)
	remote := conn.RemoteAddr().String()
	if trustProxy != nil && trustProxy() {
		if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
			remote = strings.TrimSpace(strings.Split(forwarded, ",")[0])
		}
	}
	return &wsTunnel{conn: conn, remote: remote, local: r.Host}, nil
}
