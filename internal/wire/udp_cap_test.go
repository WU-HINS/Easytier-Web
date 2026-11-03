package wire

import (
	"context"
	"net"
	"testing"
	"time"
)

// TestUDPListenerStopsAtItsTunnelLimit covers F-240: the datagram port synthesised
// one tunnel per source address with no ceiling, so a source port sweep pinned the
// same number of tunnels and session goroutines. The web socket side has a slot
// limit; this side now has one too (review team, round 150).
func TestUDPListenerStopsAtItsTunnelLimit(t *testing.T) {
	listener, err := ListenUDP("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	udp, err := listener.(*udpListener), error(nil)
	if err != nil {
		t.Fatalf("cast: %v", err)
	}
	udp.limit = 2

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for index := 0; index < 2; index++ {
		client, err := net.Dial("udp", listener.Addr())
		if err != nil {
			t.Fatalf("dial %d: %v", index, err)
		}
		if _, err := client.Write(frameOf(t, &Packet{Type: PacketTypeData, Payload: []byte("hi")})); err != nil {
			t.Fatalf("write %d: %v", index, err)
		}
		if _, err := listener.Accept(ctx); err != nil {
			t.Fatalf("accept %d: %v", index, err)
		}
		client.Close()
	}

	// The third peer must be dropped: no tunnel, nothing to accept.
	extra, err := net.Dial("udp", listener.Addr())
	if err != nil {
		t.Fatalf("dial extra: %v", err)
	}
	defer extra.Close()
	if _, err := extra.Write(frameOf(t, &Packet{Type: PacketTypeData, Payload: []byte("hi")})); err != nil {
		t.Fatalf("write extra: %v", err)
	}
	shortCtx, shortCancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer shortCancel()
	if tunnel, err := listener.Accept(shortCtx); err == nil {
		_ = tunnel.Close()
		t.Fatal("the listener went past its tunnel limit")
	}
}
