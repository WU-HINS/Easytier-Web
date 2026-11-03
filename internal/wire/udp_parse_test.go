package wire

import (
	"encoding/binary"
	"errors"
	"testing"
)

// TestParseDatagramFrameSkipsNoise covers F-252 in the form it can actually be
// tested: a connected UDP socket drops datagrams from other sources, so the
// "ignore the noise and keep reading" rule is verified on the decoder instead of
// on the socket (review team, rounds 160 and 161).
func TestParseDatagramFrameSkipsNoise(t *testing.T) {
	valid := frameOf(t, &Packet{Type: PacketTypeData, Payload: []byte("hello")})

	oversized := make([]byte, TCPFrameHeaderSize+4)
	binary.LittleEndian.PutUint32(oversized, UDPMTUBytes+1)

	tooSmall := make([]byte, TCPFrameHeaderSize+4)
	binary.LittleEndian.PutUint32(tooSmall, PeerHeaderSize-1)

	truncated := make([]byte, TCPFrameHeaderSize+4)
	binary.LittleEndian.PutUint32(truncated, 64)

	cases := []struct {
		name  string
		input []byte
		read  int
	}{
		{"empty", nil, 0},
		{"header only", []byte{1, 2, 3}, 3},
		{"oversized", oversized, len(oversized)},
		{"below peer header", tooSmall, len(tooSmall)},
		{"claims more than arrived", truncated, len(truncated)},
	}
	for _, tc := range cases {
		if _, err := parseDatagramFrame(tc.input, tc.read); !errors.Is(err, errSkipDatagram) {
			t.Fatalf("%s: err = %v, want errSkipDatagram", tc.name, err)
		}
	}

	packet, err := parseDatagramFrame(valid, len(valid))
	if err != nil {
		t.Fatalf("a valid frame was rejected: %v", err)
	}
	if string(packet.Payload) != "hello" {
		t.Fatalf("payload = %q", string(packet.Payload))
	}
}

// TestParseDatagramFrameBoundaries pins the comparisons themselves: an off by one
// in either direction keeps the "obviously broken" samples green, so the exact
// edge is what has to be asserted (review team, round 180).
func TestParseDatagramFrameBoundaries(t *testing.T) {
	valid := frameOf(t, &Packet{Type: PacketTypeData, Payload: []byte("edge")})

	// One byte short of the announced length must be skipped.
	if _, err := parseDatagramFrame(valid, len(valid)-1); !errors.Is(err, errSkipDatagram) {
		t.Fatalf("a frame one byte short: err = %v, want errSkipDatagram", err)
	}

	// Exactly the announced length must be accepted.
	if _, err := parseDatagramFrame(valid, len(valid)); err != nil {
		t.Fatalf("a complete frame was rejected: %v", err)
	}

	// A header that announces an empty body is skipped, not decoded.
	empty := make([]byte, TCPFrameHeaderSize)
	if _, err := parseDatagramFrame(empty, TCPFrameHeaderSize); !errors.Is(err, errSkipDatagram) {
		t.Fatalf("an empty body: err = %v, want errSkipDatagram", err)
	}

	// Exactly the MTU budget is allowed through the length check.
	atMTU := make([]byte, TCPFrameHeaderSize+UDPMTUBytes)
	binary.LittleEndian.PutUint32(atMTU, UDPMTUBytes)
	if _, err := parseDatagramFrame(atMTU, len(atMTU)); errors.Is(err, errSkipDatagram) {
		t.Fatal("a body of exactly UDPMTUBytes was rejected by the length check")
	}
}
