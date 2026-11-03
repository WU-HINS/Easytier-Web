package httpapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	instance "github.com/easytier/easytier-console/internal/pb/api/instance"
	"github.com/easytier/easytier-console/internal/pb/web"
	"github.com/easytier/easytier-console/internal/rpc"
	"github.com/easytier/easytier-console/internal/session"
	"github.com/easytier/easytier-console/internal/wire"
)

// TestRPCProxyGatesUnavailableMethods covers F-058: three white-listed methods
// have no generated messages, so the proxy used to answer 500 from the message
// factory. The capability listing lets the UI hide them instead.
func TestRPCProxyGatesUnavailableMethods(t *testing.T) {
	h := newHarness(t)
	admin := h.client(t)
	h.setupAdmin(t, admin, "root-owner", strongPassword)
	user := h.client(t)
	h.register(t, user, "quinn", strongPassword)

	status, body := h.do(t, user, http.MethodPost, "/api/v1/machines/unknown-machine/proxy-rpc", map[string]any{
		"service_name": "api.config.ConfigRpc",
		"method_name":  "patch_config",
		"payload":      map[string]any{},
	})
	if status != http.StatusBadRequest {
		t.Fatalf("patch_config = %d %s, want 400", status, body)
	}
	if !strings.Contains(string(body), "not available in this build") {
		t.Fatalf("rejection should explain why: %s", body)
	}

	status, body = h.do(t, user, http.MethodGet, "/api/v1/rpc-capabilities", nil)
	if status != http.StatusOK {
		t.Fatalf("capabilities = %d %s", status, body)
	}
	var payload struct {
		Services []struct {
			Name    string "json:\"name\""
			Methods []struct {
				Name      string "json:\"name\""
				Available bool   "json:\"available\""
				Reason    string "json:\"reason\""
			} "json:\"methods\""
		} "json:\"services\""
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode capabilities: %v", err)
	}
	available := map[string]bool{}
	for _, service := range payload.Services {
		for _, method := range service.Methods {
			available[service.Name+"/"+method.Name] = method.Available
		}
	}
	if available["api.config.ConfigRpc/PatchConfig"] {
		t.Fatalf("PatchConfig reported available but its messages are missing")
	}
	if !available["api.instance.PeerManageRpc/ListRoute"] {
		t.Fatalf("ListRoute should be available: its messages exist")
	}
}

// TestProxyRPCListRouteEndToEnd proves the proxy path works for a method whose
// messages exist: this is the endpoint the device operations UI will use.
func TestProxyRPCListRouteEndToEnd(t *testing.T) {
	h := newHarness(t)
	admin := h.client(t)
	h.setupAdmin(t, admin, "root-owner", strongPassword)
	user := h.client(t)
	h.register(t, user, "rex", strongPassword)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	machine := "c0ffee00-0000-0000-0000-000000000001"

	// A device that answers ListRoute with one route.
	listener, err := wire.ListenTCP("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()
	go func() {
		conn, err := listener.Accept(ctx)
		if err != nil {
			return
		}
		_ = h.manager.Serve(ctx, conn)
	}()
	tunnel, err := wire.DialTCP(ctx, listener.Addr())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	var gotInstance string
	registry := rpc.NewRegistry()
	if err := registry.Register(rpc.ServiceKey{ProtoName: "api.instance", ServiceName: "PeerManageRpc"}, 3,
		func(ctx context.Context, body []byte) ([]byte, error) {
			request := &instance.ListRouteRequest{}
			if err := proto.Unmarshal(body, request); err != nil {
				return nil, err
			}
			if id := request.GetInstance().GetId(); id != nil {
				gotInstance = fmt.Sprintf("%08x%08x", id.GetPart1(), id.GetPart2())
			}
			response := &instance.ListRouteResponse{Routes: []*instance.Route{{
				PeerId:   4242,
				Hostname: "peer-a",
			}}}
			return proto.Marshal(response)
		}); err != nil {
		t.Fatalf("register: %v", err)
	}
	deviceMux := rpc.NewMux(tunnel, registry, rpc.Options{})
	go func() { _ = deviceMux.Run(ctx) }()
	defer deviceMux.Close()
	if _, err := deviceMux.Call(ctx, session.WebServerKey, 1, &web.HeartbeatRequest{
		UserToken: "rex",
		MachineId: uuidFromString(t, machine),
		Hostname:  "node",
	}); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	waitFor(t, 5*time.Second, func() bool {
		me, err := h.store.GetUserByName(ctx, "rex")
		if err != nil {
			return false
		}
		return len(h.manager.Snapshots(me.ID)) == 1
	})

	status, body := h.do(t, user, http.MethodPost, "/api/v1/machines/"+machine+"/proxy-rpc", map[string]any{
		"service_name": "api.instance.PeerManageRpc",
		"method_name":  "list_route",
		"payload": map[string]any{
			"instance": map[string]any{
				"id": map[string]any{"part1": uint32(10), "part2": uint32(0), "part3": uint32(0), "part4": uint32(0)},
			},
		},
	})
	if status != http.StatusOK {
		t.Fatalf("list_route = %d %s", status, body)
	}
	var routes struct {
		Routes []map[string]any "json:\"routes\""
	}
	if err := json.Unmarshal(body, &routes); err != nil {
		t.Fatalf("decode routes: %v", err)
	}
	if len(routes.Routes) != 1 {
		t.Fatalf("routes = %d, want 1 (%s)", len(routes.Routes), body)
	}
	if routes.Routes[0]["hostname"] != "peer-a" {
		t.Fatalf("route hostname = %v", routes.Routes[0]["hostname"])
	}
	// The instance id must survive the JSON -> protobuf conversion: the UI sends
	// it split into the four big endian parts the wire format uses.
	if gotInstance != "0000000a00000000" {
		t.Fatalf("device saw instance id %q, want the id from the payload", gotInstance)
	}
}
