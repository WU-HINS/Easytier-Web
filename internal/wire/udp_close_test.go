package wire

import (
	"context"
	"net"
	"testing"
	"time"
)

// TestClosingTheListenerWithALivePeerReturns covers F-237: Close held the map
// mutex while closing each tunnel, and tunnel.Close runs the detach callback that
// takes the same (non reentrant) mutex, so one live peer was enough to hang
// shutdown for ever. Every existing test closed the tunnel before the listener,
// which is why the deadlock survived (review team, round 150).
func TestClosingTheListenerWithALivePeerReturns(t *testing.T) {
	listener, err := ListenUDP("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	client, err := net.Dial("udp", listener.Addr())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := client.Write(frameOf(t, &Packet{Type: PacketTypeData, Payload: []byte("stay")})); err != nil {
		t.Fatalf("write: %v", err)
	}
	tunnel, err := listener.Accept(ctx)
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	_ = tunnel // deliberately left open: the listener still has it mapped.

	done := make(chan struct{})
	go func() {
		_ = listener.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Close hung with a live peer mapped; the mutex is not reentrant")
	}
}
