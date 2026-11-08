package wire

import (
	"context"
	"encoding/binary"
	"net"
	"testing"
	"time"
)

// F-254: the client receive loop must skip noise by looping. The first version
// called itself instead, which turns a stream of junk into unbounded recursion -
// and nothing in the suite noticed a revert because every existing test used a
// clean socket.
//
// The test dials a socket this file controls, writes junk and then a real frame
// from that same address (a connected UDP socket drops packets from anyone else),
// and asserts the client surfaces the frame instead of hanging or erroring.
func TestClientRecvSkipsNoiseAndReturnsTheNextFrame(t *testing.T) {
	serverConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer serverConn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, err := DialUDP(ctx, serverConn.LocalAddr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	clientAddr, err := net.ResolveUDPAddr("udp", client.LocalAddr())
	if err != nil {
		t.Fatalf("resolve client address: %v", err)
	}

	// Junk first, in the shapes a real network produces: a runt, a stray byte, and
	// a header-shaped blob that fails its checksum.
	for _, noise := range [][]byte{{0xde, 0xad, 0xbe, 0xef}, {0x00}, {0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}} {
		if _, err := serverConn.WriteToUDP(noise, clientAddr); err != nil {
			t.Fatalf("write noise: %v", err)
		}
	}
	// The datagram frame is a 4 byte length prefix in front of the packet body;
	// sending the body alone would be noise, which is exactly what the previous
	// attempts were measuring.
	body := (&Packet{Type: PacketTypeData, Payload: []byte("after-noise")}).Encode()
	frame := make([]byte, TCPFrameHeaderSize+len(body))
	binary.LittleEndian.PutUint32(frame[0:TCPFrameHeaderSize], uint32(len(body)))
	copy(frame[TCPFrameHeaderSize:], body)
	if _, err := serverConn.WriteToUDP(frame, clientAddr); err != nil {
		t.Fatalf("write frame: %v", err)
	}

	recvCtx, cancelRecv := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelRecv()
	packet, err := client.Recv(recvCtx)
	if err != nil {
		t.Fatalf("recv after noise: %v", err)
	}
	if string(packet.Payload) != "after-noise" {
		t.Fatalf("payload = %q, want the frame that followed the noise", string(packet.Payload))
	}
}
