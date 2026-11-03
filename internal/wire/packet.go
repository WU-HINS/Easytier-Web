// Package wire implements EasyTier's transport framing: the 16-byte peer
// manager header that every tunnel packet carries, and the two tunnelling
// transports the web control plane accepts (TCP length-prefixed frames and
// WebSocket binary messages).
//
// Fidelity note: every layout in this file mirrors a #[repr(C, packed)] struct
// in easytier-core/src/packet/mod.rs. Field order, byte order and the
// forward_counter default are part of the wire contract and must not be
// "cleaned up".
package wire

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	// PeerHeaderSize is sizeof(PeerManagerHeader).
	PeerHeaderSize = 16
	// TCPFrameHeaderSize is sizeof(TCPTunnelHeader).
	TCPFrameHeaderSize = 4
	// TCPMTUBytes bounds a single TCP/WS frame. It matches TCP_MTU_BYTES.
	TCPMTUBytes = 2000
	// UDPMTUBytes is the payload budget RPC splitting targets.
	UDPMTUBytes = 1300
	// MaxPayloadSize is a safety bound for reassembly buffers.
	MaxPayloadSize = 8 << 20
)

// PacketType values are the discriminant of PacketType in packet/mod.rs.
type PacketType uint8

const (
	PacketTypeInvalid            PacketType = 0
	PacketTypeData               PacketType = 1
	PacketTypeHandShake          PacketType = 2
	PacketTypePing               PacketType = 4
	PacketTypePong               PacketType = 5
	PacketTypeRpcReq             PacketType = 8
	PacketTypeRpcResp            PacketType = 9
	PacketTypeForeignNetwork     PacketType = 10
	PacketTypeNoiseHandshakeMsg1 PacketType = 13
	PacketTypeNoiseHandshakeMsg2 PacketType = 14
	PacketTypeNoiseHandshakeMsg3 PacketType = 15
	PacketTypeRelayHandshake     PacketType = 20
	PacketTypeRelayHandshakeAck  PacketType = 21
)

func (t PacketType) String() string {
	switch t {
	case PacketTypeData:
		return "Data"
	case PacketTypeHandShake:
		return "HandShake"
	case PacketTypePing:
		return "Ping"
	case PacketTypePong:
		return "Pong"
	case PacketTypeRpcReq:
		return "RpcReq"
	case PacketTypeRpcResp:
		return "RpcResp"
	case PacketTypeNoiseHandshakeMsg1:
		return "NoiseHandshakeMsg1"
	default:
		return fmt.Sprintf("PacketType(%d)", uint8(t))
	}
}

// NoiseMagic prefixes the first payload of a secure (Noise) web tunnel.
const NoiseMagic = "ET_WEB_NOISE_V1:"

var (
	// ErrShortPacket means a buffer is smaller than the framing requires.
	ErrShortPacket = errors.New("wire: short packet")
	// ErrOversizePacket means a frame exceeded the accepted MTU.
	ErrOversizePacket = errors.New("wire: oversize packet")
)

// Packet is one EasyTier tunnel packet: peer manager header plus opaque body.
type Packet struct {
	FromPeer       uint32
	ToPeer         uint32
	Type           PacketType
	Flags          uint8
	ForwardCounter uint8
	Reserved       uint8
	Payload        []byte
}

// NewPacket builds a packet the way ZCPacket::new_with_payload followed by
// fill_peer_manager_hdr does: flags zero, forward_counter one.
func NewPacket(fromPeer, toPeer uint32, typ PacketType, payload []byte) *Packet {
	return &Packet{
		FromPeer:       fromPeer,
		ToPeer:         toPeer,
		Type:           typ,
		ForwardCounter: 1,
		Payload:        payload,
	}
}

// Encode serialises header and payload into a single buffer.
func (p *Packet) Encode() []byte {
	buf := make([]byte, PeerHeaderSize+len(p.Payload))
	binary.LittleEndian.PutUint32(buf[0:4], p.FromPeer)
	binary.LittleEndian.PutUint32(buf[4:8], p.ToPeer)
	buf[8] = byte(p.Type)
	buf[9] = p.Flags
	buf[10] = p.ForwardCounter
	buf[11] = p.Reserved
	binary.LittleEndian.PutUint32(buf[12:16], uint32(len(p.Payload)))
	copy(buf[PeerHeaderSize:], p.Payload)
	return buf
}

// DecodePacket parses header and payload. The header's own length field is
// advisory only: the enclosing transport frame is authoritative, matching
// FramedReader which never cross-checks the two.
func DecodePacket(buf []byte) (*Packet, error) {
	if len(buf) < PeerHeaderSize {
		return nil, fmt.Errorf("%w: need %d bytes, got %d", ErrShortPacket, PeerHeaderSize, len(buf))
	}
	payload := make([]byte, len(buf)-PeerHeaderSize)
	copy(payload, buf[PeerHeaderSize:])
	return &Packet{
		FromPeer:       binary.LittleEndian.Uint32(buf[0:4]),
		ToPeer:         binary.LittleEndian.Uint32(buf[4:8]),
		Type:           PacketType(buf[8]),
		Flags:          buf[9],
		ForwardCounter: buf[10],
		Reserved:       buf[11],
		Payload:        payload,
	}, nil
}
