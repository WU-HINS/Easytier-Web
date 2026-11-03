package rpc

import (
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/easytier/easytier-console/internal/pb/common"
)

// TestCompletedTransactionIsNotMergedTwice covers F-235: the reassembler deduped
// by piece index while an entry was alive, but a single piece request deleted the
// entry on completion, so a retransmitted datagram created a fresh entry and the
// handler ran a second time. A datagram transport makes that everyday traffic.
func TestCompletedTransactionIsNotMergedTwice(t *testing.T) {
	mux := NewMux(stubTunnel{}, NewRegistry(), Options{Dedupe: true})
	payload := []byte("heartbeat")
	packet := &common.RpcPacket{
		FromPeer:      1,
		ToPeer:        2,
		TransactionId: 42,
		Body:          payload,
		IsRequest:     true,
		TotalPieces:   1,
		PieceIdx:      0,
	}

	first, err := mux.feed(packet)
	if err != nil {
		t.Fatalf("first feed: %v", err)
	}
	if first == nil {
		t.Fatal("the first request was not delivered")
	}

	// Same datagram again: a retransmission, not a new call.
	second, err := mux.feed(packet)
	if err != nil {
		t.Fatalf("second feed: %v", err)
	}
	if second != nil {
		t.Fatal("a retransmitted request was delivered a second time")
	}

	// A different transaction must still get through.
	other := proto.Clone(packet).(*common.RpcPacket)
	other.TransactionId = 43
	third, err := mux.feed(other)
	if err != nil {
		t.Fatalf("third feed: %v", err)
	}
	if third == nil {
		t.Fatal("a new transaction was dropped along with the duplicate")
	}
}

// TestWholeRequestInOnePacketIsAlsoDeduped covers the second half of F-235: the
// single packet path returned before the duplicate check existed, so the most
// common request shape (heartbeat) was never deduped at all.
func TestWholeRequestInOnePacketIsAlsoDeduped(t *testing.T) {
	mux := NewMux(stubTunnel{}, NewRegistry(), Options{Dedupe: true})
	packet := &common.RpcPacket{
		FromPeer:      7,
		ToPeer:        8,
		TransactionId: 900,
		Body:          []byte("whole"),
		IsRequest:     true,
	}
	first, err := mux.feed(packet)
	if err != nil || first == nil {
		t.Fatalf("first feed = %v, %v", first, err)
	}
	second, err := mux.feed(packet)
	if err != nil {
		t.Fatalf("second feed: %v", err)
	}
	if second != nil {
		t.Fatal("a retransmitted single packet request was delivered twice")
	}
}

// TestMultipartRetransmissionIsAlsoDeduped covers the rest of F-244: a multipart
// request was remembered with the length of its last piece while a retransmission
// starts again at piece zero, so the lengths never matched and the merged request
// ran twice.
func TestMultipartRetransmissionIsAlsoDeduped(t *testing.T) {
	mux := NewMux(stubTunnel{}, NewRegistry(), Options{Dedupe: true})
	build := func(index uint32) *common.RpcPacket {
		return &common.RpcPacket{
			FromPeer:      3,
			ToPeer:        4,
			TransactionId: 77,
			Body:          []byte{byte(index), 0xaa},
			IsRequest:     true,
			TotalPieces:   2,
			PieceIdx:      index,
		}
	}
	if packet, err := mux.feed(build(0)); err != nil || packet != nil {
		t.Fatalf("piece 0 = %v, %v", packet, err)
	}
	merged, err := mux.feed(build(1))
	if err != nil {
		t.Fatalf("piece 1: %v", err)
	}
	if merged == nil {
		t.Fatal("the request was never delivered")
	}
	// Retransmission of the same two pieces.
	if packet, err := mux.feed(build(0)); err != nil || packet != nil {
		t.Fatalf("retransmitted piece 0 = %v, %v", packet, err)
	}
	again, err := mux.feed(build(1))
	if err != nil {
		t.Fatalf("retransmitted piece 1: %v", err)
	}
	if again != nil {
		t.Fatal("a retransmitted multipart request was delivered twice")
	}
}
