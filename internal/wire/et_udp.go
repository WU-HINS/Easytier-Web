package wire

import (
	"encoding/binary"
	"errors"
)

// The official EasyTier client does not put our length prefixed frame on the wire
// for udp: it speaks the session protocol of its own socket layer. A datagram is
//
//	conn_id u32 | msg_type u8 | padding u8 | len u16   (little endian)
//
// followed by len bytes of body. Syn carries an eight byte magic the server echoes
// in Sack; Data carries a ZCPacket exactly as the tcp framing does. Without the
// Sack reply the client retries the same Syn forever and never registers (the
// capture that produced this file: 2b05df2c 01 00 0800 + 8 random bytes).
const etUDPHeaderSize = 8

const (
	etUDPSyn  uint8 = 1
	etUDPSack uint8 = 2
	etUDPData uint8 = 3
	etUDPFin  uint8 = 4
)

var errNotEasyTierDatagram = errors.New("wire: not an easy tier datagram")

// easyTierDatagram is one decoded session message.
type easyTierDatagram struct {
	connID  uint32
	msgType uint8
	body    []byte
}

// parseEasyTierDatagram decodes the session header. It is deliberately strict: a
// loose match would swallow our own four byte framing, which shares the socket.
func parseEasyTierDatagram(data []byte) (easyTierDatagram, error) {
	if len(data) < etUDPHeaderSize {
		return easyTierDatagram{}, errNotEasyTierDatagram
	}
	msgType := data[4]
	// The byte after the message type is padding and the clients do fill it: 2.6.4
	// sends 0x69 and 0x61 there, which a padding==0 check rejected outright, so
	// every heartbeat was dropped and the session idled out.
	if msgType < etUDPSyn || msgType > etUDPFin {
		return easyTierDatagram{}, errNotEasyTierDatagram
	}
	length := int(binary.LittleEndian.Uint16(data[6:8]))
	if length != len(data)-etUDPHeaderSize {
		return easyTierDatagram{}, errNotEasyTierDatagram
	}
	return easyTierDatagram{
		connID:  binary.LittleEndian.Uint32(data[0:4]),
		msgType: msgType,
		body:    data[etUDPHeaderSize:],
	}, nil
}

// encodeEasyTierDatagram builds one session message.
func encodeEasyTierDatagram(connID uint32, msgType uint8, body []byte) []byte {
	out := make([]byte, etUDPHeaderSize+len(body))
	binary.LittleEndian.PutUint32(out[0:4], connID)
	out[4] = msgType
	out[5] = 0
	binary.LittleEndian.PutUint16(out[6:8], uint16(len(body)))
	copy(out[etUDPHeaderSize:], body)
	return out
}
