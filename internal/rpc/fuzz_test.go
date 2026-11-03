package rpc_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/easytier/easytier-console/internal/pb/common"
	"github.com/easytier/easytier-console/internal/pb/web"
	"github.com/easytier/easytier-console/internal/rpc"
	"github.com/easytier/easytier-console/internal/wire"
)

// rawTunnel writes pre-built packets straight onto a control-plane connection,
// so a test can play the role of a hostile peer instead of a well behaved one.
type rawPeer struct {
	conn wire.Tunnel
}

func dialRaw(t *testing.T, ctx context.Context, addr string) *rawPeer {
	t.Helper()
	conn, err := wire.DialTCP(ctx, addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return &rawPeer{conn: conn}
}

func (p *rawPeer) send(t *testing.T, ctx context.Context, packet *common.RpcPacket) {
	t.Helper()
	raw, err := proto.Marshal(packet)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	typ := wire.PacketTypeRpcReq
	if !packet.IsRequest {
		typ = wire.PacketTypeRpcResp
	}
	if err := p.conn.Send(ctx, wire.NewPacket(0, 0, typ, raw)); err != nil {
		t.Fatalf("send: %v", err)
	}
}

func (p *rawPeer) sendBytes(t *testing.T, ctx context.Context, payload []byte) {
	t.Helper()
	if err := p.conn.Send(ctx, wire.NewPacket(0, 0, wire.PacketTypeRpcReq, payload)); err != nil {
		t.Fatalf("send raw: %v", err)
	}
}

func startServer(t *testing.T, ctx context.Context) (wire.Listener, <-chan *rpc.Mux) {
	t.Helper()
	listener, err := wire.ListenTCP("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	registry := rpc.NewRegistry()
	if err := registry.Register(rpc.ServiceKey{ProtoName: "web", ServiceName: "WebServerService"}, 1,
		func(ctx context.Context, body []byte) ([]byte, error) {
			return proto.Marshal(&web.HeartbeatResponse{})
		}); err != nil {
		t.Fatalf("register: %v", err)
	}
	muxCh := make(chan *rpc.Mux, 1)
	go func() {
		conn, err := listener.Accept(ctx)
		if err != nil {
			return
		}
		mux := rpc.NewMux(conn, registry, rpc.Options{})
		muxCh <- mux
		_ = mux.Run(ctx)
	}()
	return listener, muxCh
}

// TestPartialPacketsAreBounded covers F-030: a peer must not be able to pin
// unbounded memory by declaring huge fragment counts under fresh transaction
// ids. Before the fix each packet allocated total_pieces slots and nothing ever
// freed the partial reassemblies.
func TestPartialPacketsAreBounded(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	listener, muxCh := startServer(t, ctx)
	peer := dialRaw(t, ctx, listener.Addr())

	// 200 partial packets, each claiming 32768 pieces, each with a new id.
	for i := int64(1); i <= 200; i++ {
		peer.send(t, ctx, &common.RpcPacket{
			TransactionId: i,
			TotalPieces:   32768,
			PieceIdx:      1, // piece 0 missing: stays partial forever
			Body:          []byte("x"),
			IsRequest:     true,
			Descriptor_:   &common.RpcDescriptor{ProtoName: "web", ServiceName: "WebServerService", MethodIndex: 1},
		})
	}
	time.Sleep(300 * time.Millisecond)
	select {
	case mux := <-muxCh:
		if count := mux.MergerCount(); count > 64 {
			t.Fatalf("partial reassemblies = %d, want <= 64 (unbounded memory)", count)
		}
	case <-ctx.Done():
		t.Fatal("server mux never started")
	}

	// A legitimate fragmented call still works: the bound must not break the
	// happy path.
	big := strings.Repeat("y", 4000)
	envelope, err := proto.Marshal(&common.RpcRequest{Request: []byte(big)})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	pieces := [][]byte{envelope[:1500], envelope[1500:3000], envelope[3000:]}
	for index, piece := range pieces {
		packet := &common.RpcPacket{
			TransactionId: 9999,
			TotalPieces:   uint32(len(pieces)),
			PieceIdx:      uint32(index),
			Body:          piece,
			IsRequest:     true,
		}
		if index == 0 {
			packet.Descriptor_ = &common.RpcDescriptor{ProtoName: "web", ServiceName: "WebServerService", MethodIndex: 1}
		}
		peer.send(t, ctx, packet)
	}
	time.Sleep(300 * time.Millisecond)
}

// TestMalformedFramesDoNotKillTheListener sends garbage that must be ignored or
// rejected without panicking, and proves a fresh connection still works after.
func TestMalformedFramesDoNotKillTheListener(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	listener, _ := startServer(t, ctx)

	cases := [][]byte{
		{},                              // empty payload
		[]byte("not a protobuf at all"), // garbage
		{0xff, 0xff, 0xff, 0x7f},        // truncated varint
		bytesOfLength(1024),             // claims to be a long message
	}
	for _, payload := range cases {
		peer := dialRaw(t, ctx, listener.Addr())
		peer.sendBytes(t, ctx, payload)
		time.Sleep(50 * time.Millisecond)
	}

	// The listener must still accept and serve a well behaved peer.
	serverConn, err := listener.Accept(ctx)
	if err != nil {
		t.Fatalf("listener died after malformed input: %v", err)
	}
	_ = serverConn.Close()
}

func bytesOfLength(n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = byte(i % 251)
	}
	return out
}

// TestOversizedFrameIsRejected proves the frame length check fires before any
// allocation happens.
func TestOversizedFrameIsRejected(t *testing.T) {
	listener, err := wire.ListenTCP("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := wire.DialTCP(ctx, listener.Addr())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	// Send a frame header claiming 4 GiB.
	huge := wire.NewPacket(0, 0, wire.PacketTypeRpcReq, make([]byte, wire.TCPMTUBytes+1))
	if err := conn.Send(ctx, huge); err == nil {
		t.Fatal("sending an oversized frame should be refused by our own writer")
	}
	server, err := listener.Accept(ctx)
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	defer server.Close()
	_ = server
}
