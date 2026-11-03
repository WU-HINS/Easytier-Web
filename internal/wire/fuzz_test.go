package wire_test

import (
	"bytes"
	"testing"

	"github.com/easytier/easytier-console/internal/wire"
)

// FuzzDecodePacket feeds arbitrary bytes to the tunnel packet decoder. It must
// never panic, and anything it accepts must survive a re-encode: the decoder is
// the first thing a hostile peer reaches.
func FuzzDecodePacket(f *testing.F) {
	f.Add([]byte{})
	f.Add(make([]byte, 15))
	f.Add(make([]byte, 16))
	f.Add(bytes.Repeat([]byte{0xff}, 64))
	f.Add(wire.NewPacket(1, 2, wire.PacketTypeRpcReq, []byte("hello")).Encode())

	f.Fuzz(func(t *testing.T, data []byte) {
		packet, err := wire.DecodePacket(data)
		if err != nil {
			return
		}
		encoded := packet.Encode()
		again, err := wire.DecodePacket(encoded)
		if err != nil {
			t.Fatalf("re-encode produced an undecodable packet: %v", err)
		}
		if !bytes.Equal(packet.Payload, again.Payload) {
			t.Fatalf("payload changed across round trip")
		}
		if len(encoded) != wire.PeerHeaderSize+len(packet.Payload) {
			t.Fatalf("encoded size %d does not match header+payload", len(encoded))
		}
	})
}
