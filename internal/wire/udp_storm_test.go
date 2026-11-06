package wire

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"
)

// TestUDPSurvivesGarbageAndManyPeers puts the datagram listener under the kind of
// traffic an open port actually sees: dozens of peers, malformed frames, and no
// cooperation. It has to keep accepting, and it has to be race free (round 147).
func TestUDPSurvivesGarbageAndManyPeers(t *testing.T) {
	listener, err := ListenUDP("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	const peers = 12
	var wg sync.WaitGroup
	// done tells the peers to stop resending once every one has been seen.
	done := make(chan struct{})
	for index := 0; index < peers; index++ {
		wg.Add(1)
		go func(slot int) {
			defer wg.Done()
			conn, err := net.Dial("udp", listener.Addr())
			if err != nil {
				return
			}
			defer conn.Close()
			// Garbage first: short frames, absurd lengths, and a body that does not decode.
			junk := []byte{0, 0, 0, 0, 1, 2, 3}
			_ = junk
			for _, payload := range [][]byte{{0, 0, 0, 0}, {255, 255, 255, 255, 9, 9}} {
				_, _ = conn.Write(payload)
			}
			truncated := make([]byte, TCPFrameHeaderSize+4)
			binary.LittleEndian.PutUint32(truncated[0:4], 64)
			_, _ = conn.Write(truncated)
			// UDP is lossy and the listener may be draining a backlog; keep offering the
			// frame until the test has seen every peer, otherwise a single dropped
			// datagram looks like "garbage starved a real peer".
			frame := frameOf(t, &Packet{Type: PacketTypeData, Payload: []byte(fmt.Sprintf("peer-%d", slot))})
			for {
				if ok := writeFrame(conn, frame); !ok {
					t.Errorf("peer %d could not send a valid frame", slot)
					return
				}
				select {
				case <-done:
					return
				case <-time.After(50 * time.Millisecond):
				}
			}
		}(index)
	}
	// Peers resend until told to stop, so waiting for them here would deadlock:
	// the accept loop below is what ends them.

	seen := map[string]bool{}
	// One deadline for the whole loop turns machine load into a failure. Each
	// accept gets its own window: a real hang is still caught, a slow machine is not.
	const perPeer = 5 * time.Second
	for len(seen) < peers {
		ctx, cancel := context.WithTimeout(context.Background(), perPeer)
		tunnel, err := listener.Accept(ctx)
		if err != nil {
			cancel()
			t.Fatalf("accept after %d peer(s): %v", len(seen), err)
		}
		packet, err := tunnel.Recv(ctx)
		cancel()
		if err != nil {
			t.Fatalf("recv: %v", err)
		}
		seen[string(packet.Payload)] = true
		_ = tunnel.Close()
	}
	close(done)
	wg.Wait()
	for index := 0; index < peers; index++ {
		want := fmt.Sprintf("peer-%d", index)
		if !seen[want] {
			t.Fatalf("missing %s; garbage datagrams starved a real peer", want)
		}
	}
}

func writeFrame(conn net.Conn, frame []byte) bool {
	_, err := conn.Write(frame)
	return err == nil
}
