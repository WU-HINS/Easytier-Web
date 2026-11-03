package wire

import (
	"context"
	"net"
	"testing"
	"time"
)

// TestUDPPeerThatChangesSourcePortGetsANewTunnel covers the part of NAT rebinding
// that can be reproduced locally: when a peer's source port changes, the listener
// keys on the remote address, so it must hand out a second tunnel instead of
// dropping the datagram or corrupting the first tunnel's stream.
func TestUDPPeerThatChangesSourcePortGetsANewTunnel(t *testing.T) {
	listener, err := ListenUDP("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	first, err := net.Dial("udp", listener.Addr())
	if err != nil {
		t.Fatalf("dial first: %v", err)
	}
	defer first.Close()
	if _, err := first.Write(frameOf(t, &Packet{Type: PacketTypeData, Payload: []byte("before")})); err != nil {
		t.Fatalf("first write: %v", err)
	}
	tunnelA, err := listener.Accept(ctx)
	if err != nil {
		t.Fatalf("accept A: %v", err)
	}
	defer tunnelA.Close()

	// A new socket is what a rebinding NAT looks like from here.
	second, err := net.Dial("udp", listener.Addr())
	if err != nil {
		t.Fatalf("dial second: %v", err)
	}
	defer second.Close()
	if _, err := second.Write(frameOf(t, &Packet{Type: PacketTypeData, Payload: []byte("after")})); err != nil {
		t.Fatalf("second write: %v", err)
	}
	tunnelB, err := listener.Accept(ctx)
	if err != nil {
		t.Fatalf("a rebinding peer was not given a tunnel: %v", err)
	}
	defer tunnelB.Close()

	if tunnelA.RemoteAddr() == tunnelB.RemoteAddr() {
		t.Fatalf("both tunnels point at %s; the first stream would be corrupted", tunnelA.RemoteAddr())
	}
	packet, err := tunnelB.Recv(ctx)
	if err != nil {
		t.Fatalf("recv on the new tunnel: %v", err)
	}
	if string(packet.Payload) != "after" {
		t.Fatalf("payload = %q", string(packet.Payload))
	}
}
