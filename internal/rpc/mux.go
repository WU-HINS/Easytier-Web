package rpc

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/easytier/easytier-console/internal/logging"
	"github.com/easytier/easytier-console/internal/pb/common"
	errorpb "github.com/easytier/easytier-console/internal/pb/error"
	"github.com/easytier/easytier-console/internal/wire"
)

var muxLog = logging.New("RPC")

const (
	// rpcPayloadBudget mirrors RPC_PACKET_UDP_PAYLOAD_BUDGET. Splitting at the
	// UDP budget keeps every piece inside the TCP/WS MTU as well.
	rpcPayloadBudget = 1300
	// pieceOverhead covers the UDP tunnel header plus peer header and tail
	// reservation, so a piece never exceeds the budget once framed.
	pieceOverhead = 16 + 8 + 8
	// maxPieces mirrors the merger sanity bound.
	maxPieces = 32 * 1024
	// maxMergers, maxMergerBytes and maxInflightBytes bound the memory a single
	// peer can pin with partial packets.
	maxMergers       = 64
	maxMergerBytes   = 4 << 20
	maxInflightBytes = 16 << 20

	defaultCallTimeout = 10 * time.Second
)

// ErrNoHandler is returned when a peer calls a method this side does not serve.
var ErrNoHandler = errors.New("rpc: no handler for method")

// RemoteError wraps an error returned by the peer.
type RemoteError struct {
	Kind    string
	Message string
}

func (e *RemoteError) Error() string {
	if e.Message == "" {
		return "rpc: remote error: " + e.Kind
	}
	return "rpc: remote error: " + e.Kind + ": " + e.Message
}

// Options tunes the multiplexer.
type Options struct {
	// CallTimeout bounds a Call when the caller passes no deadline.
	CallTimeout time.Duration
	// AcceptedCompression is advertised to peers for bodies sent to us.
	AcceptedCompression common.CompressionAlgoPb
	// OnFrame, when set, observes every decoded frame.
	OnFrame func(dir string, packet *common.RpcPacket)
	// Dedupe suppresses a request that just ran, which only a datagram transport
	// can deliver twice. Stream transports leave it off: transaction ids are only
	// unique per peer, so deduplicating there drops legitimate calls whose peer
	// reused an id (round 152).
	Dedupe bool
}

// Mux multiplexes calls over one tunnel in both directions.
type Mux struct {
	tunnel wire.Tunnel
	reg    *Registry
	opts   Options

	nextTID atomic.Int64

	pendingMu sync.Mutex
	pending   map[int64]chan *common.RpcResponse

	mergeMu     sync.Mutex
	mergers     map[int64]*merger
	mergerBytes int
	// completed remembers recently finished transactions so a retransmitted
	// datagram cannot run the handler twice. Transaction ids are only unique per
	// peer, so the key carries the sender and the body fingerprint: a new call
	// that happens to reuse an id is not mistaken for a duplicate (round 148).
	completed     map[completedKey]completedEntry
	completedRing []completedKey

	closeOnce sync.Once
	done      chan struct{}

	retired atomic.Int64
}

// completedHistory bounds how many finished transactions are remembered, so a
// peer cannot grow the dedupe set without limit.
const completedHistory = 1024

// duplicateWindow is how long a retransmission is still recognised as one.
const duplicateWindow = 2 * time.Second

// NewMux binds a multiplexer to a tunnel and registry.
func NewMux(tunnel wire.Tunnel, reg *Registry, opts Options) *Mux {
	if opts.CallTimeout <= 0 {
		opts.CallTimeout = defaultCallTimeout
	}
	if opts.AcceptedCompression == common.CompressionAlgoPb_Invalid {
		opts.AcceptedCompression = common.CompressionAlgoPb_Zstd
	}
	return &Mux{
		completed:     map[completedKey]completedEntry{},
		completedRing: make([]completedKey, 0, completedHistory),
		tunnel:        tunnel,
		reg:           reg,
		opts:          opts,
		pending:       make(map[int64]chan *common.RpcResponse),
		mergers:       make(map[int64]*merger),
		done:          make(chan struct{}),
	}
}

type merger struct {
	totalPieces uint32
	first       *common.RpcPacket
	desc        *common.RpcDescriptor
	isRequest   bool
	transaction int64
	fromPeer    uint32
	toPeer      uint32
	traceID     int32
	// pieces is keyed by index and grown lazily: an attacker declaring
	// total_pieces=32768 must not be able to allocate 32768 slots per packet.
	pieces     map[uint32][]byte
	bytes      int
	received   int
	lastUpdate time.Time
}

// Run reads frames until the tunnel fails or the context is cancelled.
func (m *Mux) Run(ctx context.Context) error {
	defer m.Close()
	lastSweep := time.Now()
	for {
		// Drop partial reassemblies that stopped arriving; without this the map
		// (and the memory behind it) only ever grows.
		if time.Since(lastSweep) > 5*time.Second {
			m.SweepMergers(10 * time.Second)
			lastSweep = time.Now()
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		pkt, err := m.tunnel.Recv(ctx)
		if err != nil {
			return err
		}
		rpcPacket := &common.RpcPacket{}
		if err := proto.Unmarshal(pkt.Payload, rpcPacket); err != nil {
			// A malformed peer must not take the session down; the Rust server
			// logs and drops in the same place.
			continue
		}
		if rpcPacket.FromPeer == 0 && rpcPacket.ToPeer == 0 {
			rpcPacket.FromPeer = pkt.FromPeer
			rpcPacket.ToPeer = pkt.ToPeer
		}
		if m.opts.OnFrame != nil {
			m.opts.OnFrame("in", rpcPacket)
		}
		complete, err := m.feed(rpcPacket)
		if err != nil || complete == nil {
			continue
		}
		if complete.IsRequest {
			go m.serve(ctx, complete)
			continue
		}
		resp, err := decodeResponse(complete)
		if err != nil {
			continue
		}
		m.pendingMu.Lock()
		ch, ok := m.pending[complete.TransactionId]
		if ok {
			delete(m.pending, complete.TransactionId)
		}
		m.pendingMu.Unlock()
		if ok {
			select {
			case ch <- resp:
			default:
			}
		}
	}
}

func (m *Mux) feed(p *common.RpcPacket) (*common.RpcPacket, error) {
	if p.TotalPieces == 0 && p.PieceIdx == 0 {
		// A whole request in one packet takes the same duplicate check as a
		// reassembled one. It used to return before the check, so a retransmitted
		// datagram ran the handler twice (review team, round 150).
		m.mergeMu.Lock()
		if m.opts.Dedupe && m.isDuplicate(p) {
			m.mergeMu.Unlock()
			return nil, nil
		}
		if m.opts.Dedupe {
			m.remember(p)
		}
		m.mergeMu.Unlock()
		return p, nil
	}
	if p.TotalPieces == 0 || p.TotalPieces > maxPieces {
		return nil, fmt.Errorf("rpc: invalid total_pieces %d", p.TotalPieces)
	}
	m.mergeMu.Lock()
	defer m.mergeMu.Unlock()
	if m.opts.Dedupe && m.isDuplicate(p) {
		// A retransmitted request must not run twice.
		return nil, nil
	}
	entry, ok := m.mergers[p.TransactionId]
	if !ok {
		// Bound the number of concurrent reassemblies and their total size so a
		// peer cannot exhaust memory with partial packets.
		if len(m.mergers) >= maxMergers || m.mergerBytes >= maxInflightBytes {
			return nil, fmt.Errorf("rpc: too many concurrent partial packets")
		}
		entry = &merger{
			totalPieces: p.TotalPieces,
			transaction: p.TransactionId,
			isRequest:   p.IsRequest,
			fromPeer:    p.FromPeer,
			toPeer:      p.ToPeer,
			traceID:     p.TraceId,
			pieces:      make(map[uint32][]byte, 4),
		}
		m.mergers[p.TransactionId] = entry
	}
	if entry.totalPieces != p.TotalPieces || p.PieceIdx >= p.TotalPieces {
		delete(m.mergers, p.TransactionId)
		return nil, fmt.Errorf("rpc: inconsistent piece %d/%d", p.PieceIdx, p.TotalPieces)
	}
	// The first piece carries the descriptor and the compression info that the
	// whole reassembled body needs; keep it as the template, mirroring
	// PacketMerger::try_merge_pieces which clones pieces[0].
	if p.PieceIdx == 0 {
		entry.first = p
	}
	if p.Descriptor_ != nil {
		entry.desc = p.Descriptor_
	}
	if entry.pieces[p.PieceIdx] == nil {
		if entry.bytes+len(p.Body) > maxMergerBytes {
			delete(m.mergers, p.TransactionId)
			m.mergerBytes -= entry.bytes
			return nil, fmt.Errorf("rpc: partial packet exceeds %d bytes", maxMergerBytes)
		}
		entry.pieces[p.PieceIdx] = append([]byte(nil), p.Body...)
		entry.bytes += len(p.Body)
		m.mergerBytes += len(p.Body)
		entry.received++
	}
	entry.lastUpdate = time.Now()
	if entry.received != int(entry.totalPieces) {
		return nil, nil
	}
	delete(m.mergers, p.TransactionId)
	if m.opts.Dedupe {
		m.remember(p)
	}
	m.mergerBytes -= entry.bytes
	body := make([]byte, 0, entry.bytes+1)
	for index := uint32(0); index < entry.totalPieces; index++ {
		body = append(body, entry.pieces[index]...)
	}
	merged := &common.RpcPacket{
		FromPeer:      entry.fromPeer,
		ToPeer:        entry.toPeer,
		TransactionId: entry.transaction,
		Descriptor_:   entry.desc,
		Body:          body,
		IsRequest:     entry.isRequest,
		TotalPieces:   1,
		PieceIdx:      0,
	}
	if entry.first != nil {
		merged.Descriptor_ = entry.first.Descriptor_
		merged.CompressionInfo = entry.first.CompressionInfo
		merged.TraceId = entry.first.TraceId
	}
	return merged, nil
}

// SweepMergers drops stale partial reassemblies. The Rust server keeps
// partials for ten seconds.
func (m *Mux) SweepMergers(maxAge time.Duration) {
	cutoff := time.Now().Add(-maxAge)
	m.mergeMu.Lock()
	defer m.mergeMu.Unlock()
	for tid, entry := range m.mergers {
		if entry.lastUpdate.Before(cutoff) {
			delete(m.mergers, tid)
			m.mergerBytes -= entry.bytes
		}
	}
	if m.mergerBytes < 0 {
		m.mergerBytes = 0
	}
}

// MergerCount reports how many partial reassemblies are in flight. Diagnostics
// only: the bound is enforced in feed and swept from Run.
func (m *Mux) MergerCount() int {
	m.mergeMu.Lock()
	defer m.mergeMu.Unlock()
	return len(m.mergers)
}

// callHandler runs one handler with a recover: a panic in a handler must fail
// that single request, not the whole process. Goroutines do not inherit the
// net/http recovery, so this is the only thing standing between a bad handler
// and the daemon dying.
func callHandler(ctx context.Context, handler Handler, body []byte) (out []byte, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			muxLog.Sub("Serve").Tag("PANIC").Error("handler panicked: " + fmt.Sprint(recovered))
			err = fmt.Errorf("rpc: handler panicked")
			out = nil
		}
	}()
	return handler(ctx, body)
}

func (m *Mux) serve(ctx context.Context, p *common.RpcPacket) {
	now := time.Now()
	resp := &common.RpcResponse{}
	if p.Descriptor_ == nil {
		resp.Error = remoteErr("request without descriptor")
	} else {
		key := ServiceKey{
			DomainName:  p.Descriptor_.DomainName,
			ProtoName:   p.Descriptor_.ProtoName,
			ServiceName: p.Descriptor_.ServiceName,
		}
		handler, ok := m.reg.Lookup(key, p.Descriptor_.MethodIndex)
		switch {
		case !ok:
			resp.Error = &errorpb.Error{ErrorKind: &errorpb.Error_InvalidMethodIndex{
				InvalidMethodIndex: &errorpb.InvalidMethodIndex{
					ServiceName: p.Descriptor_.ServiceName,
					MethodIndex: p.Descriptor_.MethodIndex,
				},
			}}
		default:
			body, err := decodeRequest(p)
			if err != nil {
				resp.Error = remoteErr(err.Error())
			} else if out, err := callHandler(ctx, handler, body); err != nil {
				resp.Error = executeErr(err.Error())
			} else {
				resp.Response = out
			}
		}
	}
	resp.RuntimeUs = uint64(time.Since(now).Microseconds())
	if err := m.sendResponse(ctx, p, resp); err != nil {
		m.Close()
	}
}

func decodeRequest(p *common.RpcPacket) ([]byte, error) {
	body, err := decompress(p.CompressionInfo, p.Body)
	if err != nil {
		return nil, err
	}
	req := &common.RpcRequest{}
	if err := proto.Unmarshal(body, req); err != nil {
		return nil, fmt.Errorf("rpc: decode RpcRequest: %w", err)
	}
	return req.Request, nil
}

func decodeResponse(p *common.RpcPacket) (*common.RpcResponse, error) {
	body, err := decompress(p.CompressionInfo, p.Body)
	if err != nil {
		return nil, err
	}
	resp := &common.RpcResponse{}
	if err := proto.Unmarshal(body, resp); err != nil {
		return nil, fmt.Errorf("rpc: decode RpcResponse: %w", err)
	}
	return resp, nil
}

func (m *Mux) sendResponse(ctx context.Context, req *common.RpcPacket, resp *common.RpcResponse) error {
	raw, err := proto.Marshal(resp)
	if err != nil {
		return fmt.Errorf("rpc: encode RpcResponse: %w", err)
	}
	algo := common.CompressionAlgoPb_None
	if req.CompressionInfo != nil {
		algo = req.CompressionInfo.AcceptedAlgo
	}
	body, algo, err := compress(algo, raw)
	if err != nil {
		return err
	}
	pieces := splitBody(body, func(idx int) int {
		return baseLen(req.FromPeer, req.ToPeer, req.Descriptor_, false, idx, algo)
	})
	total := uint32(len(pieces))
	for idx, piece := range pieces {
		packet := &common.RpcPacket{
			FromPeer:      req.ToPeer,
			ToPeer:        req.FromPeer,
			TransactionId: req.TransactionId,
			Body:          piece,
			IsRequest:     false,
			TotalPieces:   total,
			PieceIdx:      uint32(idx),
			TraceId:       req.TraceId,
		}
		if idx == 0 {
			packet.Descriptor_ = req.Descriptor_
			packet.CompressionInfo = &common.RpcCompressionInfo{
				Algo:         algo,
				AcceptedAlgo: m.opts.AcceptedCompression,
			}
		}
		if err := m.send(ctx, packet); err != nil {
			return err
		}
	}
	return nil
}

// Call invokes a remote method and waits for its response.
func (m *Mux) Call(ctx context.Context, key ServiceKey, methodIndex uint32, req proto.Message) ([]byte, error) {
	raw, err := proto.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("rpc: encode request: %w", err)
	}
	envelope, err := proto.Marshal(&common.RpcRequest{Request: raw})
	if err != nil {
		return nil, fmt.Errorf("rpc: encode RpcRequest: %w", err)
	}
	body, algo, err := compress(common.CompressionAlgoPb_Zstd, envelope)
	if err != nil {
		return nil, err
	}

	desc := &common.RpcDescriptor{
		ProtoName:   key.ProtoName,
		ServiceName: key.ServiceName,
		MethodIndex: methodIndex,
		DomainName:  key.DomainName,
	}
	tid := m.nextTID.Add(1)
	ch := make(chan *common.RpcResponse, 1)
	m.pendingMu.Lock()
	m.pending[tid] = ch
	m.pendingMu.Unlock()
	cleanup := func() {
		m.pendingMu.Lock()
		delete(m.pending, tid)
		m.pendingMu.Unlock()
	}

	pieces := splitBody(body, func(idx int) int {
		return baseLen(0, 0, desc, true, idx, algo)
	})
	total := uint32(len(pieces))
	for idx, piece := range pieces {
		packet := &common.RpcPacket{
			TransactionId: tid,
			Body:          piece,
			IsRequest:     true,
			TotalPieces:   total,
			PieceIdx:      uint32(idx),
		}
		if idx == 0 {
			packet.Descriptor_ = desc
			packet.CompressionInfo = &common.RpcCompressionInfo{
				Algo:         algo,
				AcceptedAlgo: m.opts.AcceptedCompression,
			}
		}
		if err := m.send(ctx, packet); err != nil {
			cleanup()
			return nil, err
		}
	}

	timeout := m.opts.CallTimeout
	if deadline, ok := ctx.Deadline(); ok {
		timeout = time.Until(deadline)
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		cleanup()
		return nil, ctx.Err()
	case <-m.done:
		cleanup()
		return nil, wire.ErrTunnelClosed
	case <-timer.C:
		cleanup()
		return nil, fmt.Errorf("rpc: call %s#%d timed out after %s", key, methodIndex, timeout)
	case resp, ok := <-ch:
		if !ok {
			return nil, wire.ErrTunnelClosed
		}
		if resp.Error != nil {
			return nil, toRemoteError(resp.Error)
		}
		return resp.Response, nil
	}
}

func (m *Mux) send(ctx context.Context, packet *common.RpcPacket) error {
	raw, err := proto.Marshal(packet)
	if err != nil {
		return fmt.Errorf("rpc: encode RpcPacket: %w", err)
	}
	if m.opts.OnFrame != nil {
		m.opts.OnFrame("out", packet)
	}
	typ := wire.PacketTypeRpcReq
	if !packet.IsRequest {
		typ = wire.PacketTypeRpcResp
	}
	return m.tunnel.Send(ctx, wire.NewPacket(packet.FromPeer, packet.ToPeer, typ, raw))
}

// Close tears the tunnel down and fails outstanding calls.
func (m *Mux) Close() {
	m.closeOnce.Do(func() {
		close(m.done)
		_ = m.tunnel.Close()
		m.pendingMu.Lock()
		for tid, ch := range m.pending {
			delete(m.pending, tid)
			close(ch)
		}
		m.pendingMu.Unlock()
	})
}

// Done is closed when the multiplexer is torn down.
func (m *Mux) Done() <-chan struct{} { return m.done }

func remoteErr(message string) *errorpb.Error {
	return &errorpb.Error{ErrorKind: &errorpb.Error_OtherError{
		OtherError: &errorpb.OtherError{ErrorMessage: message},
	}}
}

func executeErr(message string) *errorpb.Error {
	return &errorpb.Error{ErrorKind: &errorpb.Error_ExecuteError{
		ExecuteError: &errorpb.ExecuteError{ErrorMessage: message},
	}}
}

func toRemoteError(e *errorpb.Error) error {
	switch kind := e.ErrorKind.(type) {
	case *errorpb.Error_Timeout:
		return &RemoteError{Kind: "Timeout", Message: kind.Timeout.ErrorMessage}
	case *errorpb.Error_ExecuteError:
		return &RemoteError{Kind: "ExecuteError", Message: kind.ExecuteError.ErrorMessage}
	case *errorpb.Error_InvalidService:
		return &RemoteError{Kind: "InvalidService", Message: kind.InvalidService.ServiceName}
	case *errorpb.Error_InvalidMethodIndex:
		return &RemoteError{Kind: "InvalidMethodIndex", Message: fmt.Sprintf("%s#%d", kind.InvalidMethodIndex.ServiceName, kind.InvalidMethodIndex.MethodIndex)}
	case *errorpb.Error_MalformatRpcPacket:
		return &RemoteError{Kind: "MalformatRpcPacket", Message: kind.MalformatRpcPacket.ErrorMessage}
	case *errorpb.Error_OtherError:
		return &RemoteError{Kind: "OtherError", Message: kind.OtherError.ErrorMessage}
	default:
		return &RemoteError{Kind: "Unknown"}
	}
}

// baseLen estimates the encoded packet size with an empty body, which is what
// the Rust splitter uses to size pieces against the UDP budget.
func baseLen(from, to uint32, desc *common.RpcDescriptor, isReq bool, idx int, algo common.CompressionAlgoPb) int {
	probe := &common.RpcPacket{
		FromPeer:      from,
		ToPeer:        to,
		TransactionId: 1 << 40,
		IsRequest:     isReq,
		TotalPieces:   maxPieces,
		PieceIdx:      uint32(idx),
		TraceId:       1,
	}
	if idx == 0 {
		probe.Descriptor_ = desc
		probe.CompressionInfo = &common.RpcCompressionInfo{Algo: algo, AcceptedAlgo: common.CompressionAlgoPb_None}
	}
	raw, err := proto.Marshal(probe)
	if err != nil {
		return rpcPayloadBudget
	}
	return len(raw)
}

func splitBody(body []byte, base func(idx int) int) [][]byte {
	if len(body) == 0 {
		return [][]byte{{}}
	}
	maxEncoded := rpcPayloadBudget - pieceOverhead
	pieces := make([][]byte, 0, 1)
	offset := 0
	for offset < len(body) {
		room := maxEncoded - base(len(pieces))
		if room <= 3 {
			room = 1
		}
		end := offset + room
		if end > len(body) {
			end = len(body)
		}
		pieces = append(pieces, body[offset:end])
		offset = end
	}
	return pieces
}

// completedKey identifies one finished transaction on one peer.
type completedKey struct {
	fromPeer    int64
	transaction int64
}

type completedEntry struct {
	at time.Time
}

// remember records a finished transaction and evicts the oldest entry once the
// history is full. Callers hold mergeMu.
func (m *Mux) remember(p *common.RpcPacket) {
	key := completedKey{fromPeer: int64(p.FromPeer), transaction: p.TransactionId}
	if _, seen := m.completed[key]; !seen {
		m.completedRing = append(m.completedRing, key)
	}
	m.completed[key] = completedEntry{at: time.Now()}
	if len(m.completedRing) > completedHistory {
		oldest := m.completedRing[0]
		m.completedRing = m.completedRing[1:]
		delete(m.completed, oldest)
	}
}

// isDuplicate reports whether this packet repeats one that just ran. Duplicates
// arrive within milliseconds; a later call that reuses an id is a new request.
func (m *Mux) isDuplicate(p *common.RpcPacket) bool {
	key := completedKey{fromPeer: int64(p.FromPeer), transaction: p.TransactionId}
	entry, seen := m.completed[key]
	if !seen {
		return false
	}
	// Size is deliberately not compared: a multipart request is remembered with
	// the merged length, while a retransmission arrives piece by piece, so the
	// lengths never match (review team, round 150).
	return time.Since(entry.at) < duplicateWindow
}
