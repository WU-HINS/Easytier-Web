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
			if ok := writeFrame(conn, frameOf(t, &Packet{Type: PacketTypeData, Payload: []byte(fmt.Sprintf("peer-%d", slot))})); !ok {
				t.Errorf("peer %d could not send a valid frame", slot)
			}
		}(index)
	}
	wg.Wait()

	seen := map[string]bool{}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for len(seen) < peers {
		tunnel, err := listener.Accept(ctx)
		if err != nil {
			t.Fatalf("accept after %d peer(s): %v", len(seen), err)
		}
		packet, err := tunnel.Recv(ctx)
		if err != nil {
			t.Fatalf("recv: %v", err)
		}
		seen[string(packet.Payload)] = true
		_ = tunnel.Close()
	}
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
