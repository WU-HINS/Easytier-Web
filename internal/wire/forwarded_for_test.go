package wire

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// TestForwardedForIsOnlyBelievedWhenTheProxyIsTrusted covers F-216: the device
// port used to take X-Forwarded-For from anyone, so an unauthenticated peer could
// choose the source address shown in the audit view. The switch now matches the
// HTTP side (found by the review team in r136).
func TestForwardedForIsOnlyBelievedWhenTheProxyIsTrusted(t *testing.T) {
	for _, trust := range []bool{false, true} {
		trust := trust
		name := "untrusted"
		if trust {
			name = "trusted"
		}
		t.Run(name, func(t *testing.T) {
			listener, err := ListenWS("127.0.0.1:0", func() bool { return trust })
			if err != nil {
				t.Fatalf("listen: %v", err)
			}
			defer listener.Close()

			url := "ws://" + listener.Addr() + "/"
			header := http.Header{}
			header.Set("X-Forwarded-For", "9.9.9.9")
			conn, _, err := websocket.DefaultDialer.Dial(url, header)
			if err != nil {
				t.Fatalf("dial: %v", err)
			}
			defer conn.Close()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			tunnel, err := listener.Accept(ctx)
			if err != nil {
				t.Fatalf("accept: %v", err)
			}
			defer tunnel.Close()
			remote := tunnel.RemoteAddr()
			if trust && remote != "9.9.9.9" {
				t.Fatalf("trusted proxy: remote = %q, want the forwarded address", remote)
			}
			if !trust && strings.Contains(remote, "9.9.9.9") {
				t.Fatalf("untrusted peer chose its own address: remote = %q", remote)
			}
		})
	}
}
