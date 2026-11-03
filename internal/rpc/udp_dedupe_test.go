package rpc_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/easytier/easytier-console/internal/pb/common"
	"github.com/easytier/easytier-console/internal/pb/web"
	"github.com/easytier/easytier-console/internal/rpc"
	"github.com/easytier/easytier-console/internal/wire"
)

// TestDuplicateRequestOverUDPServerRunsOnce covers the transport where duplicates
// actually happen: one request delivered twice must run the handler once, and the
// suppression is on because the tunnel declares itself a datagram transport
// (review team, rounds 150 and 158).
func TestDuplicateRequestOverUDPServerRunsOnce(t *testing.T) {
	listener, err := wire.ListenUDP("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := wire.DialUDP(ctx, listener.Addr())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	inner, err := proto.Marshal(&web.HeartbeatRequest{UserToken: "alice", Hostname: "udp"})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	envelope, err := proto.Marshal(&common.RpcRequest{Request: inner})
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	packet := &common.RpcPacket{
		FromPeer:      1,
		ToPeer:        2,
		TransactionId: 424242,
		Descriptor_:   &common.RpcDescriptor{ProtoName: heartbeatKey.ProtoName, ServiceName: heartbeatKey.ServiceName, MethodIndex: 1},
		Body:          envelope,
		IsRequest:     true,
		TotalPieces:   1,
	}
	raw, err := proto.Marshal(packet)
	if err != nil {
		t.Fatalf("marshal packet: %v", err)
	}
	send := func() error {
		return client.Send(ctx, &wire.Packet{Type: wire.PacketTypeRpcReq, Payload: raw})
	}

	// A datagram has to arrive before the listener can hand out a tunnel.
	if err := send(); err != nil {
		t.Fatalf("seed datagram: %v", err)
	}
	serverTunnel, err := listener.Accept(ctx)
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	datagram, ok := serverTunnel.(interface{ IsDatagram() bool })
	if !ok || !datagram.IsDatagram() {
		t.Fatal("the accepted tunnel does not advertise datagram semantics")
	}

	var calls atomic.Int32
	registry := rpc.NewRegistry()
	if err := registry.Register(heartbeatKey, 1, func(ctx context.Context, body []byte) ([]byte, error) {
		calls.Add(1)
		return proto.Marshal(&web.HeartbeatResponse{HeartbeatIntervalMs: proto.Uint32(3500)})
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	serverMux := rpc.NewMux(serverTunnel, registry, rpc.Options{Dedupe: true})
	defer serverMux.Close()
	go func() { _ = serverMux.Run(ctx) }()

	// The retransmission of the same transaction.
	time.Sleep(200 * time.Millisecond)
	if err := send(); err != nil {
		t.Fatalf("retransmitted datagram: %v", err)
	}
	time.Sleep(500 * time.Millisecond)

	if got := calls.Load(); got != 1 {
		t.Fatalf("handler ran %d time(s) for one request delivered twice", got)
	}
}
