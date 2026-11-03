package rpc_test

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/easytier/easytier-console/internal/pb/web"
	"github.com/easytier/easytier-console/internal/rpc"
	"github.com/easytier/easytier-console/internal/wire"
)

var heartbeatKey = rpc.ServiceKey{ProtoName: "web", ServiceName: "WebServerService"}

// newPair returns a server-side and client-side mux wired over a real TCP
// control-plane listener, which keeps the tunnel framing in the loop.
func newPair(t *testing.T) (*rpc.Mux, *rpc.Mux) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)

	listener, err := wire.ListenTCP("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	serverConnCh := make(chan wire.Tunnel, 1)
	go func() {
		conn, err := listener.Accept(ctx)
		if err != nil {
			t.Errorf("accept: %v", err)
			return
		}
		serverConnCh <- conn
	}()

	clientConn, err := wire.DialTCP(ctx, listener.Addr())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = clientConn.Close() })

	var serverConn wire.Tunnel
	select {
	case serverConn = <-serverConnCh:
	case <-ctx.Done():
		t.Fatal("accept timeout")
	}

	serverReg := rpc.NewRegistry()
	if err := serverReg.Register(heartbeatKey, 1, func(ctx context.Context, body []byte) ([]byte, error) {
		req := &web.HeartbeatRequest{}
		if err := proto.Unmarshal(body, req); err != nil {
			return nil, err
		}
		reply := &web.HeartbeatResponse{HeartbeatIntervalMs: proto.Uint32(3500)}
		if req.Hostname != "" {
			reply.HeartbeatTimeoutMs = proto.Uint32(uint32(len(req.Hostname)))
		}
		if len(req.RunningNetworkInstances) > 0 {
			panic("unreachable")
		}
		return proto.Marshal(reply)
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	serverMux := rpc.NewMux(serverConn, serverReg, rpc.Options{})
	clientMux := rpc.NewMux(clientConn, rpc.NewRegistry(), rpc.Options{})
	go func() { _ = serverMux.Run(ctx) }()
	go func() { _ = clientMux.Run(ctx) }()
	t.Cleanup(func() {
		serverMux.Close()
		clientMux.Close()
	})
	return serverMux, clientMux
}

func TestHeartbeatCallRoundTrip(t *testing.T) {
	_, client := newPair(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req := &web.HeartbeatRequest{UserToken: "alice", Hostname: "laptop"}
	raw, err := client.Call(ctx, heartbeatKey, 1, req)
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	resp := &web.HeartbeatResponse{}
	if err := proto.Unmarshal(raw, resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got := resp.GetHeartbeatIntervalMs(); got != 3500 {
		t.Fatalf("interval = %d, want 3500", got)
	}
	if got := resp.GetHeartbeatTimeoutMs(); got != uint32(len("laptop")) {
		t.Fatalf("timeout = %d, want %d", got, len("laptop"))
	}
}

func TestLargeRequestIsSplitAndReassembled(t *testing.T) {
	_, client := newPair(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	hostname := strings.Repeat("x", 4096)
	raw, err := client.Call(ctx, heartbeatKey, 1, &web.HeartbeatRequest{Hostname: hostname})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	resp := &web.HeartbeatResponse{}
	if err := proto.Unmarshal(raw, resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got := resp.GetHeartbeatTimeoutMs(); got != 4096 {
		t.Fatalf("timeout = %d, want 4096 (reassembly failed)", got)
	}
}

func TestUnknownMethodReturnsRemoteError(t *testing.T) {
	_, client := newPair(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.Call(ctx, heartbeatKey, 7, &web.HeartbeatRequest{})
	if err == nil {
		t.Fatal("expected error for unregistered method index")
	}
	remote := &rpc.RemoteError{}
	if !errorsAs(err, &remote) {
		t.Fatalf("error = %v, want RemoteError", err)
	}
	if remote.Kind != "InvalidMethodIndex" {
		t.Fatalf("kind = %q, want InvalidMethodIndex", remote.Kind)
	}
}

func TestFrameStaysWithinMTU(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	listener, err := wire.ListenTCP("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	var maxSeen atomic.Int64
	go func() {
		conn, err := listener.Accept(ctx)
		if err != nil {
			return
		}
		for {
			pkt, err := conn.Recv(ctx)
			if err != nil {
				return
			}
			if n := int64(len(pkt.Payload) + wire.PeerHeaderSize); n > maxSeen.Load() {
				maxSeen.Store(n)
			}
		}
	}()

	clientConn, err := wire.DialTCP(ctx, listener.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer clientConn.Close()
	reg := rpc.NewRegistry()
	_ = reg.Register(heartbeatKey, 1, func(ctx context.Context, body []byte) ([]byte, error) {
		return []byte{}, nil
	})
	mux := rpc.NewMux(clientConn, reg, rpc.Options{CallTimeout: 2 * time.Second})
	go func() { _ = mux.Run(ctx) }()
	defer mux.Close()

	// No peer answers, so the call must time out; we only care about framing.
	_, _ = mux.Call(ctx, heartbeatKey, 1, &web.HeartbeatRequest{Hostname: strings.Repeat("y", 20000)})
	time.Sleep(100 * time.Millisecond)
	observed := int(maxSeen.Load())
	if observed > wire.TCPMTUBytes {
		t.Fatalf("frame of %d bytes exceeds MTU %d", observed, wire.TCPMTUBytes)
	}
	if observed == 0 {
		t.Fatal("no frames observed")
	}
}

func errorsAs(err error, target **rpc.RemoteError) bool {
	var re *rpc.RemoteError
	if e, ok := err.(*rpc.RemoteError); ok {
		re = e
	}
	if re == nil {
		return false
	}
	*target = re
	return true
}

func TestConcurrentCallsDoNotCrossResponses(t *testing.T) {
	_, client := newPair(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	const n = 32
	errCh := make(chan error, n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			hostname := fmt.Sprintf("host-%02d", i)
			raw, err := client.Call(ctx, heartbeatKey, 1, &web.HeartbeatRequest{Hostname: hostname})
			if err != nil {
				errCh <- err
				return
			}
			resp := &web.HeartbeatResponse{}
			if err := proto.Unmarshal(raw, resp); err != nil {
				errCh <- err
				return
			}
			if int(resp.GetHeartbeatTimeoutMs()) != len(hostname) {
				errCh <- fmt.Errorf("crossed response: got %d want %d", resp.GetHeartbeatTimeoutMs(), len(hostname))
				return
			}
			errCh <- nil
		}()
	}
	for i := 0; i < n; i++ {
		if err := <-errCh; err != nil {
			t.Fatalf("concurrent call: %v", err)
		}
	}
	_ = bytes.MinRead
}
