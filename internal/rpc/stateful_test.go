package rpc_test

import (
	"context"
	"math/rand"
	"testing"
	"time"

	"github.com/easytier/easytier-console/internal/pb/common"
)

// TestStatefulPacketSequencesStayBounded covers F-036: the existing fuzz tests
// feed single packets to a fresh mux, so cross packet behaviour (interleaved
// fragments, duplicates, late pieces, out of range indexes) was never exercised.
func TestStatefulPacketSequencesStayBounded(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	listener, muxCh := startServer(t, ctx)
	peer := dialRaw(t, ctx, listener.Addr())
	mux := <-muxCh

	packet := func(tid int64, request bool, total, index uint32) *common.RpcPacket {
		return &common.RpcPacket{
			TransactionId: tid,
			Descriptor_: &common.RpcDescriptor{
				ProtoName:   "web",
				ServiceName: "WebServerService",
				MethodIndex: 1,
			},
			Body:        []byte("payload"),
			IsRequest:   request,
			TotalPieces: total,
			PieceIdx:    index,
		}
	}

	// Deterministic part: a single fragment of a multi piece message must leave a
	// merger behind, otherwise the rest of the test would pass without ever
	// touching the assembly path.
	peer.send(t, ctx, packet(999, true, 4, 0))
	formed := false
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); {
		if mux.MergerCount() > 0 {
			formed = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !formed {
		t.Fatal("a lone fragment did not create a merger; the test would be vacuous")
	}

	// Randomised part: interleaved transactions, duplicate and late pieces,
	// indexes that do not exist, responses for strangers.
	random := rand.New(rand.NewSource(20261101))
	for round := 0; round < 400; round++ {
		peer.send(t, ctx, packet(
			int64(random.Intn(4)),
			random.Intn(3) != 0,
			uint32(random.Intn(5)),
			uint32(random.Intn(7)),
		))
		if random.Intn(8) == 0 {
			mux.SweepMergers(time.Nanosecond)
		}
		if count := mux.MergerCount(); count > 64 {
			t.Fatalf("mergers grew to %d after %d packets", count, round+1)
		}
	}

	// Let the peer drain before judging the leftovers: the sends above are
	// asynchronous, so an immediate sweep would pass for the wrong reason.
	time.Sleep(200 * time.Millisecond)
	deadline := time.Now().Add(5 * time.Second)
	for {
		mux.SweepMergers(0)
		if mux.MergerCount() == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("mergers survived the sweep: %d", mux.MergerCount())
		}
		time.Sleep(20 * time.Millisecond)
	}
}
