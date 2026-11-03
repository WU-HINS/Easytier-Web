package wire

import (
	"context"
	"testing"
	"time"
)

// TestDialUDPRoundTrip covers the client half of the datagram transport: a device
// has to be able to dial the console the same way it dials TCP, which is what
// makes an end to end UDP session possible (round 157).
func TestDialUDPRoundTrip(t *testing.T) {
	listener, err := ListenUDP("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, err := DialUDP(ctx, listener.Addr())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	if err := client.Send(ctx, &Packet{Type: PacketTypeData, Payload: []byte("hello")}); err != nil {
		t.Fatalf("send: %v", err)
	}
	server, err := listener.Accept(ctx)
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	defer server.Close()
	received, err := server.Recv(ctx)
	if err != nil {
		t.Fatalf("server recv: %v", err)
	}
	if string(received.Payload) != "hello" {
		t.Fatalf("payload = %q", string(received.Payload))
	}

	if err := server.Send(ctx, &Packet{Type: PacketTypeData, Payload: []byte("world")}); err != nil {
		t.Fatalf("server send: %v", err)
	}
	reply, err := client.Recv(ctx)
	if err != nil {
		t.Fatalf("client recv: %v", err)
	}
	if string(reply.Payload) != "world" {
		t.Fatalf("reply = %q", string(reply.Payload))
	}
	if !client.(interface{ IsDatagram() bool }).IsDatagram() {
		t.Fatal("the dialing side does not advertise datagram semantics")
	}
}
