package wire

import (
	"context"
	"encoding/binary"
	"net"
	"testing"
	"time"
)

func frameOf(t *testing.T, packet *Packet) []byte {
	t.Helper()
	body := packet.Encode()
	frame := make([]byte, TCPFrameHeaderSize+len(body))
	binary.LittleEndian.PutUint32(frame[0:4], uint32(len(body)))
	copy(frame[TCPFrameHeaderSize:], body)
	return frame
}

// TestUDPTunnelCarriesFramedPackets covers the transport the console advertised
// but never implemented: the same length prefixed frame as TCP, one synthetic
// tunnel per remote address (round 145).
func TestUDPTunnelCarriesFramedPackets(t *testing.T) {
	listener, err := ListenUDP("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	client, err := net.Dial("udp", listener.Addr())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	outbound := &Packet{Type: PacketTypeData, Payload: []byte("hello")}
	if _, err := client.Write(frameOf(t, outbound)); err != nil {
		t.Fatalf("write: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tunnel, err := listener.Accept(ctx)
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	defer tunnel.Close()

	received, err := tunnel.Recv(ctx)
	if err != nil {
		t.Fatalf("recv: %v", err)
	}
	if string(received.Payload) != "hello" {
		t.Fatalf("payload = %q, want hello", string(received.Payload))
	}

	if err := tunnel.Send(ctx, &Packet{Type: PacketTypeData, Payload: []byte("pong")}); err != nil {
		t.Fatalf("send: %v", err)
	}
	buffer := make([]byte, 2048)
	if err := client.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("deadline: %v", err)
	}
	read, err := client.Read(buffer)
	if err != nil {
		t.Fatalf("client read: %v", err)
	}
	bodyLen := int(binary.LittleEndian.Uint32(buffer[0:TCPFrameHeaderSize]))
	reply, err := DecodePacket(buffer[TCPFrameHeaderSize : TCPFrameHeaderSize+bodyLen])
	if err != nil {
		t.Fatalf("decode reply: %v", err)
	}
	if string(reply.Payload) != "pong" {
		t.Fatalf("reply payload = %q, want pong", string(reply.Payload))
	}
	if read != TCPFrameHeaderSize+bodyLen {
		t.Fatalf("read = %d, want %d", read, TCPFrameHeaderSize+bodyLen)
	}
}

// TestUDPListsOneTunnelPerPeer keeps a chatty peer from being handed to Accept
// twice: the second datagram has to land on the tunnel already accepted.
func TestUDPListsOneTunnelPerPeer(t *testing.T) {
	listener, err := ListenUDP("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	client, err := net.Dial("udp", listener.Addr())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for index := 0; index < 3; index++ {
		if _, err := client.Write(frameOf(t, &Packet{Type: PacketTypeData, Payload: []byte("again")})); err != nil {
			t.Fatalf("write %d: %v", index, err)
		}
	}
	tunnel, err := listener.Accept(ctx)
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	defer tunnel.Close()

	shortCtx, shortCancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer shortCancel()
	if _, err := listener.Accept(shortCtx); err == nil {
		t.Fatal("the same peer was accepted twice")
	}
}

// TestUDPPeerCanReconnectAfterItsTunnelCloses covers the defect a self review of
// the new transport found: a tunnel closed by the session layer stayed in the
// listener map, so every later datagram from that address was dropped into a dead
// tunnel and the peer could never reconnect (round 146).
func TestUDPPeerCanReconnectAfterItsTunnelCloses(t *testing.T) {
	listener, err := ListenUDP("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	client, err := net.Dial("udp", listener.Addr())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := client.Write(frameOf(t, &Packet{Type: PacketTypeData, Payload: []byte("first")})); err != nil {
		t.Fatalf("first write: %v", err)
	}
	first, err := listener.Accept(ctx)
	if err != nil {
		t.Fatalf("first accept: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	if _, err := client.Write(frameOf(t, &Packet{Type: PacketTypeData, Payload: []byte("second")})); err != nil {
		t.Fatalf("second write: %v", err)
	}
	second, err := listener.Accept(ctx)
	if err != nil {
		t.Fatalf("the peer could not reconnect: %v", err)
	}
	defer second.Close()
	packet, err := second.Recv(ctx)
	if err != nil {
		t.Fatalf("receive after reconnect: %v", err)
	}
	if string(packet.Payload) != "second" {
		t.Fatalf("payload = %q", string(packet.Payload))
	}
}
