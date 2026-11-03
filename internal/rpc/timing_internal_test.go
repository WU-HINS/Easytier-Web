package rpc

import (
	"testing"
	"time"

	"github.com/easytier/easytier-console/internal/pb/common"
)

func TestFeedWorstCaseTiming(t *testing.T) {
	cases := []struct {
		name   string
		packet *common.RpcPacket
	}{
		{"max pieces, tiny body", &common.RpcPacket{TransactionId: 1, TotalPieces: 32768, PieceIdx: 1, Body: []byte("x"), IsRequest: true}},
		{"max pieces, 8KB body", &common.RpcPacket{TransactionId: 2, TotalPieces: 32768, PieceIdx: 1, Body: make([]byte, 8192), IsRequest: true}},
		{"single piece large", &common.RpcPacket{TransactionId: 3, TotalPieces: 1, PieceIdx: 0, Body: make([]byte, 1<<20), IsRequest: true}},
		{"huge idx", &common.RpcPacket{TransactionId: 4, TotalPieces: 32768, PieceIdx: 32767, Body: []byte("x"), IsRequest: true}},
	}
	for _, testCase := range cases {
		mux := NewMux(stubTunnel{}, NewRegistry(), Options{})
		start := time.Now()
		_, _ = mux.feed(testCase.packet)
		elapsed := time.Since(start)
		t.Logf("%-26s %v", testCase.name, elapsed)
		if elapsed > 200*time.Millisecond {
			t.Fatalf("%s took %v, that is a slow path an attacker can repeat", testCase.name, elapsed)
		}
	}
}
