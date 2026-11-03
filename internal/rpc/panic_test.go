package rpc_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/easytier/easytier-console/internal/pb/web"
	"github.com/easytier/easytier-console/internal/rpc"
	"github.com/easytier/easytier-console/internal/wire"
)

// TestHandlerPanicDoesNotKillTheProcess covers F-074: handlers run in their own
// goroutine, so without a recover a single panic would take the whole daemon
// down. The peer must get an error response and the mux must keep serving.
func TestHandlerPanicDoesNotKillTheProcess(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	listener, err := wire.ListenTCP("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	registry := rpc.NewRegistry()
	key := rpc.ServiceKey{ProtoName: "web", ServiceName: "WebServerService"}
	if err := registry.Register(key, 1, func(ctx context.Context, body []byte) ([]byte, error) {
		panic("handler exploded")
	}); err != nil {
		t.Fatalf("register panicking handler: %v", err)
	}
	if err := registry.Register(key, 2, func(ctx context.Context, body []byte) ([]byte, error) {
		return proto.Marshal(&web.HeartbeatResponse{})
	}); err != nil {
		t.Fatalf("register healthy handler: %v", err)
	}

	go func() {
		conn, err := listener.Accept(ctx)
		if err != nil {
			return
		}
		mux := rpc.NewMux(conn, registry, rpc.Options{})
		_ = mux.Run(ctx)
	}()

	tunnel, err := wire.DialTCP(ctx, listener.Addr())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	mux := rpc.NewMux(tunnel, rpc.NewRegistry(), rpc.Options{CallTimeout: 2 * time.Second})
	go func() { _ = mux.Run(ctx) }()
	defer mux.Close()

	_, err = mux.Call(ctx, key, 1, &web.HeartbeatRequest{UserToken: "x"})
	if err == nil {
		t.Fatalf("panicking handler reported success")
	}
	if !strings.Contains(err.Error(), "panicked") {
		t.Fatalf("error = %v, want it to say the handler panicked", err)
	}

	// The process is still alive and the mux still serves the next call.
	if _, err := mux.Call(ctx, key, 2, &web.HeartbeatRequest{UserToken: "x"}); err != nil {
		t.Fatalf("mux stopped serving after a handler panic: %v", err)
	}
}
