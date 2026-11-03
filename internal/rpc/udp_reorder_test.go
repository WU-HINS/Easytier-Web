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

// TestReorderedPiecesOverUDPStillMerge covers the datagram hazard that survives
// deduplication: UDP may deliver the pieces of one request out of order, so the
// reassembler has to key on the piece index and not on arrival order.
func TestReorderedPiecesOverUDPStillMerge(t *testing.T) {
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

	inner, err := proto.Marshal(&web.HeartbeatRequest{UserToken: "alice", Hostname: "reorder"})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	envelope, err := proto.Marshal(&common.RpcRequest{Request: inner})
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}

	half := len(envelope) / 2
	pieces := [][]byte{envelope[:half], envelope[half:]}
	send := func(index uint32) error {
		packet := &common.RpcPacket{
			FromPeer:      1,
			ToPeer:        2,
			TransactionId: 555001,
			Descriptor_:   &common.RpcDescriptor{ProtoName: heartbeatKey.ProtoName, ServiceName: heartbeatKey.ServiceName, MethodIndex: 1},
			Body:          pieces[index],
			IsRequest:     true,
			TotalPieces:   2,
			PieceIdx:      index,
		}
		raw, err := proto.Marshal(packet)
		if err != nil {
			return err
		}
		return client.Send(ctx, &wire.Packet{Type: wire.PacketTypeRpcReq, Payload: raw})
	}

	// Seed the tunnel, then send the tail piece before the head piece.
	if err := send(1); err != nil {
		t.Fatalf("seed: %v", err)
	}
	serverTunnel, err := listener.Accept(ctx)
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	var calls atomic.Int32
	registry := rpc.NewRegistry()
	if err := registry.Register(heartbeatKey, 1, func(ctx context.Context, body []byte) ([]byte, error) {
		calls.Add(1)
		return proto.Marshal(&web.HeartbeatResponse{HeartbeatIntervalMs: proto.Uint32(1)})
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	serverMux := rpc.NewMux(serverTunnel, registry, rpc.Options{Dedupe: true})
	defer serverMux.Close()
	go func() { _ = serverMux.Run(ctx) }()

	if err := send(0); err != nil {
		t.Fatalf("out of order piece: %v", err)
	}
	time.Sleep(400 * time.Millisecond)
	if got := calls.Load(); got != 1 {
		t.Fatalf("handler ran %d time(s) for a request delivered out of order, want 1", got)
	}
}
