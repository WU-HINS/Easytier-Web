package rpc

import (
	"context"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/easytier/easytier-console/internal/pb/common"
	"github.com/easytier/easytier-console/internal/wire"
)

// stubTunnel satisfies wire.Tunnel without doing any IO; the fuzz target only
// exercises the reassembly state machine.
type stubTunnel struct{}

func (stubTunnel) Send(context.Context, *wire.Packet) error   { return nil }
func (stubTunnel) Recv(context.Context) (*wire.Packet, error) { return nil, context.Canceled }
func (stubTunnel) RemoteAddr() string                         { return "stub" }
func (stubTunnel) LocalAddr() string                          { return "stub" }
func (stubTunnel) Close() error                               { return nil }

// FuzzFeedPacket drives the fragment reassembly with arbitrary protobuf bytes.
// The invariants: no panic, and the in-flight reassembly budget stays bounded no
// matter what total_pieces / piece_idx / transaction_id the peer chooses.
func FuzzFeedPacket(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0x08, 0x01})                   // transaction_id = 1
	f.Add([]byte{0x38, 0x02, 0x40, 0x01})       // total_pieces = 2, piece_idx = 1
	f.Add([]byte{0xff, 0xff, 0xff, 0xff, 0x07}) // huge varints

	f.Fuzz(func(t *testing.T, data []byte) {
		mux := NewMux(stubTunnel{}, NewRegistry(), Options{})
		packet := &common.RpcPacket{}
		if err := proto.Unmarshal(data, packet); err != nil {
			return
		}
		// feed must never panic, whatever the field combination is.
		_, _ = mux.feed(packet)
		if count := mux.MergerCount(); count > maxMergers {
			t.Fatalf("mergers = %d, want <= %d", count, maxMergers)
		}
		if mux.mergerBytes > maxInflightBytes {
			t.Fatalf("in-flight bytes = %d, want <= %d", mux.mergerBytes, maxInflightBytes)
		}
	})
}
