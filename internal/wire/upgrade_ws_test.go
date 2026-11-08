package wire

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// R-E, r254: UpgradeWebSocket takes the same trust switch as ListenWS, but nothing
// exercised that path - the handler that serves /@up/ was fixed and left unlocked.
// This drives the upgrade directly and reads back the address the tunnel reports.
func TestUpgradeWebSocketHonoursTheProxySwitch(t *testing.T) {
	for _, trust := range []bool{false, true} {
		trust := trust
		name := "untrusted"
		if trust {
			name = "trusted"
		}
		t.Run(name, func(t *testing.T) {
			var mu sync.Mutex
			remote := ""
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				tunnel, err := UpgradeWebSocket(w, r, func() bool { return trust })
				if err != nil {
					return
				}
				mu.Lock()
				remote = tunnel.RemoteAddr()
				mu.Unlock()
				time.Sleep(100 * time.Millisecond)
				_ = tunnel.Close()
			}))
			defer server.Close()

			header := http.Header{}
			header.Set("X-Forwarded-For", "9.9.9.9")
			url := "ws" + strings.TrimPrefix(server.URL, "http")
			conn, _, err := websocket.DefaultDialer.Dial(url, header)
			if err != nil {
				t.Fatalf("dial: %v", err)
			}
			defer conn.Close()

			deadline := time.Now().Add(2 * time.Second)
			for time.Now().Before(deadline) {
				mu.Lock()
				got := remote
				mu.Unlock()
				if got != "" {
					want := "9.9.9.9"
					if !trust {
						want = "127.0.0.1"
					}
					if !strings.HasPrefix(got, want) {
						t.Fatalf("tunnel address = %q, want it to start with %q (trust=%v)", got, want, trust)
					}
					return
				}
				time.Sleep(20 * time.Millisecond)
			}
			t.Fatal("the upgrade never reported a remote address")
		})
	}
}
