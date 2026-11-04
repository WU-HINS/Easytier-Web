package wire_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"net"
	"testing"
	"time"

	"github.com/easytier/easytier-console/internal/wire"
)

// TestEasyTierUDPSessionHandshake covers the datagram session the official client
// speaks on udp: a Syn carrying an eight byte magic, a Sack echo, then Data frames
// wrapped in the same eight byte header (goal round for F-277).
func TestEasyTierUDPSessionHandshake(t *testing.T) {
	listener, err := wire.ListenUDP("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	conn, err := net.Dial("udp", listener.Addr())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	connID := uint32(0x2cdf052b)
	magic := []byte{0x6e, 0x00, 0xd8, 0x91, 0x00, 0x2d, 0xe8, 0x20}
	// The padding byte is not reserved in practice: 2.6.4 fills it with 0x69, and a
	// parser that insisted on zero dropped every heartbeat it sent.
	if _, err := conn.Write(buildDatagramPadded(connID, 1, 0x69, magic)); err != nil {
		t.Fatalf("write syn: %v", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	reply := make([]byte, 2048)
	read, err := conn.Read(reply)
	if err != nil {
		t.Fatalf("no sack came back: %v", err)
	}
	if got := binary.LittleEndian.Uint32(reply[0:4]); got != connID {
		t.Fatalf("sack conn id = %d, want %d", got, connID)
	}
	if reply[4] != 2 {
		t.Fatalf("sack message type = %d, want 2", reply[4])
	}
	if !bytes.Equal(reply[8:read], magic) {
		t.Fatalf("sack must echo the magic, got %x", reply[8:read])
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tunnel, err := listener.Accept(ctx)
	if err != nil {
		t.Fatalf("accept: %v", err)
	}

	want := &wire.Packet{FromPeer: 1, ToPeer: 1, Payload: []byte("hello")}
	if _, err := conn.Write(buildDatagramPadded(connID, 3, 0x61, want.Encode())); err != nil {
		t.Fatalf("write data: %v", err)
	}
	got, err := tunnel.Recv(ctx)
	if err != nil {
		t.Fatalf("recv: %v", err)
	}
	if !bytes.Equal(got.Payload, want.Payload) {
		t.Fatalf("payload = %q, want %q", got.Payload, want.Payload)
	}

	if err := tunnel.Send(ctx, want); err != nil {
		t.Fatalf("send: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	read, err = conn.Read(reply)
	if err != nil {
		t.Fatalf("no data frame came back: %v", err)
	}
	if reply[4] != 3 {
		t.Fatalf("outbound message type = %d, want 3", reply[4])
	}
	if got := int(binary.LittleEndian.Uint16(reply[6:8])); got != read-8 {
		t.Fatalf("outbound length field = %d, want %d", got, read-8)
	}
	decoded, err := wire.DecodePacket(reply[8:read])
	if err != nil {
		t.Fatalf("outbound body is not a packet: %v", err)
	}
	if !bytes.Equal(decoded.Payload, want.Payload) {
		t.Fatalf("outbound payload = %q, want %q", decoded.Payload, want.Payload)
	}
}

// buildDatagram writes the eight byte session header the upstream client uses.
func buildDatagram(connID uint32, msgType uint8, body []byte) []byte {
	return buildDatagramPadded(connID, msgType, 0, body)
}

// buildDatagramPadded sets the padding byte, which real clients do not zero.
func buildDatagramPadded(connID uint32, msgType uint8, padding uint8, body []byte) []byte {
	out := make([]byte, 8+len(body))
	binary.LittleEndian.PutUint32(out[0:4], connID)
	out[4] = msgType
	out[5] = padding
	binary.LittleEndian.PutUint16(out[6:8], uint16(len(body)))
	copy(out[8:], body)
	return out
}
