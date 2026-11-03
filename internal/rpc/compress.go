package rpc

import (
	"fmt"

	"github.com/klauspost/compress/zstd"

	"github.com/easytier/easytier-console/internal/pb/common"
)

var zstdEncoder, _ = zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))
var zstdDecoder, _ = zstd.NewReader(nil)

// compress mirrors compress_packet: keep the compressed form only when it is
// actually smaller, and report which algorithm the body now uses.
func compress(algo common.CompressionAlgoPb, raw []byte) ([]byte, common.CompressionAlgoPb, error) {
	if algo != common.CompressionAlgoPb_Zstd || len(raw) == 0 {
		return raw, common.CompressionAlgoPb_None, nil
	}
	encoded := zstdEncoder.EncodeAll(raw, nil)
	if len(encoded) >= len(raw) {
		return raw, common.CompressionAlgoPb_None, nil
	}
	return encoded, common.CompressionAlgoPb_Zstd, nil
}

// decompress mirrors decompress_packet, tolerating peers that leave the
// compression info unset.
func decompress(info *common.RpcCompressionInfo, body []byte) ([]byte, error) {
	if info == nil {
		return body, nil
	}
	switch info.Algo {
	case common.CompressionAlgoPb_Invalid, common.CompressionAlgoPb_None:
		return body, nil
	case common.CompressionAlgoPb_Zstd:
		out, err := zstdDecoder.DecodeAll(body, nil)
		if err != nil {
			return nil, fmt.Errorf("rpc: zstd decode: %w", err)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("rpc: unsupported compression algo %d", info.Algo)
	}
}
