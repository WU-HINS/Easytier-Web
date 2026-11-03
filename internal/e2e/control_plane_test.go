package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/easytier/easytier-console/internal/auth"
	"github.com/easytier/easytier-console/internal/httpapi"
	apiinstance "github.com/easytier/easytier-console/internal/pb/api/instance"
	"github.com/easytier/easytier-console/internal/pb/common"
	"github.com/easytier/easytier-console/internal/pb/web"
	"github.com/easytier/easytier-console/internal/rpc"
	"github.com/easytier/easytier-console/internal/session"
	"github.com/easytier/easytier-console/internal/settings"
	"github.com/easytier/easytier-console/internal/wire"
)

// peerManageKey is the service a device exposes to the console.
var peerManageKey = rpc.ServiceKey{ProtoName: "api.instance", ServiceName: "PeerManageRpc"}

// TestControlPlaneEndToEnd drives the whole stack the way production does:
// a device opens the control-plane tunnel, heartbeats, the console persists the
// device and then calls back into the device over the same tunnel.
func TestControlPlaneEndToEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	store, err := newTestStore(t, ctx)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	if err := settings.New(store).MarkInitialized(ctx); err != nil {
		t.Fatalf("mark initialized: %v", err)
	}
	password := "correct horse battery staple"
	hash, err := auth.Hash(password)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	user, err := store.CreateUser(ctx, "alice", hash)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	manager, err := session.NewManager(store, &session.LocalAuthenticator{Store: store}, session.DefaultPolicy())
	if err != nil {
		t.Fatalf("manager: %v", err)
	}
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
		_ = manager.Serve(ctx, conn)
	}()

	// --- device side -----------------------------------------------------
	deviceConn, err := wire.DialTCP(ctx, listener.Addr())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	deviceRegistry := rpc.NewRegistry()
	if err := deviceRegistry.Register(peerManageKey, 3, func(ctx context.Context, body []byte) ([]byte, error) {
		return proto.Marshal(&apiinstance.ListRouteResponse{Routes: []*apiinstance.Route{
			{PeerId: 42, Hostname: "peer-42", Cost: 1, Version: "2.7.0"},
		}})
	}); err != nil {
		t.Fatalf("register device method: %v", err)
	}
	deviceMux := rpc.NewMux(deviceConn, deviceRegistry, rpc.Options{})
	go func() { _ = deviceMux.Run(ctx) }()
	defer deviceMux.Close()

	machineID := "0f8fad5b-d9cb-469f-a165-70867728950e"
	heartbeat := &web.HeartbeatRequest{
		UserToken:       "alice",
		MachineId:       mustUUID(t, machineID),
		Hostname:        "laptop",
		EasytierVersion: "2.7.0",
		DeviceOs:        &web.DeviceOsInfo{OsType: "linux"},
	}
	raw, err := deviceMux.Call(ctx, session.WebServerKey, 1, heartbeat)
	if err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	resp := &web.HeartbeatResponse{}
	if err := proto.Unmarshal(raw, resp); err != nil {
		t.Fatalf("decode heartbeat response: %v", err)
	}
	if resp.GetHeartbeatIntervalMs() != 3500 {
		t.Fatalf("interval = %d, want 3500", resp.GetHeartbeatIntervalMs())
	}

	// The registry row is written by the heartbeat, not by the REST layer.
	device, err := store.GetDevice(ctx, user.ID, machineID)
	if err != nil {
		t.Fatalf("device not persisted: %v", err)
	}
	if device.Hostname != "laptop" || device.EasytierVersion != "2.7.0" {
		t.Fatalf("device = %+v", device)
	}

	// --- console side ----------------------------------------------------
	api, err := httpapi.New(httpapi.Config{Store: store, Sessions: manager})
	if err != nil {
		t.Fatalf("httpapi: %v", err)
	}
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 10 * time.Second}

	login, _ := json.Marshal(map[string]string{"username": "alice", "password": password})
	response, err := client.Post(server.URL+"/api/v1/auth/login", "application/json", bytes.NewReader(login))
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("login status = %d: %s", response.StatusCode, body)
	}

	var machines struct {
		Machines []struct {
			MachineID string `json:"machine_id"`
			Hostname  string `json:"hostname"`
			Online    bool   `json:"online"`
		} `json:"machines"`
	}
	getJSON(t, client, server.URL+"/api/v1/machines", &machines)
	if len(machines.Machines) != 1 || machines.Machines[0].MachineID != machineID {
		t.Fatalf("machines = %+v", machines.Machines)
	}
	if !machines.Machines[0].Online {
		t.Fatal("device should be online")
	}

	// proxy-rpc: console -> device -> back, exactly like the 2.7.0 endpoint.
	body, _ := json.Marshal(map[string]any{
		"service_name": "api.instance.PeerManageRpc",
		"method_name":  "list_route",
		"payload":      map[string]any{"instance": map[string]any{"id": map[string]any{}}},
	})
	response, err = client.Post(server.URL+"/api/v1/machines/"+machineID+"/proxy-rpc", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("proxy-rpc: %v", err)
	}
	if response.StatusCode != http.StatusOK {
		payload, _ := io.ReadAll(response.Body)
		t.Fatalf("proxy-rpc status = %d: %s", response.StatusCode, payload)
	}
	var routes struct {
		Routes []struct {
			PeerID   uint32 `json:"peer_id"`
			Hostname string `json:"hostname"`
		} `json:"routes"`
	}
	if err := json.NewDecoder(response.Body).Decode(&routes); err != nil {
		t.Fatalf("decode proxy-rpc: %v", err)
	}
	if len(routes.Routes) != 1 || routes.Routes[0].Hostname != "peer-42" {
		t.Fatalf("routes = %+v", routes.Routes)
	}

	// Blocking the device must reject the next connection attempt.
	if err := store.BlockDevice(ctx, user.ID, machineID, "laptop"); err != nil {
		t.Fatalf("block: %v", err)
	}
	if _, err := (&session.LocalAuthenticator{Store: store}).AuthenticateToken(ctx, "alice", machineID); err == nil {
		t.Fatal("blocked device was authenticated")
	}
}

func mustUUID(t *testing.T, value string) *common.UUID {
	t.Helper()
	var p1, p2, p3, p4 uint32
	var raw [16]byte
	hexDigits := make([]byte, 0, 32)
	for _, r := range value {
		if r == '-' {
			continue
		}
		hexDigits = append(hexDigits, byte(r))
	}
	decoded := make([]byte, 16)
	for i := 0; i < 16; i++ {
		high := hexValue(hexDigits[i*2])
		low := hexValue(hexDigits[i*2+1])
		decoded[i] = high<<4 | low
	}
	copy(raw[:], decoded)
	p1 = uint32(raw[0])<<24 | uint32(raw[1])<<16 | uint32(raw[2])<<8 | uint32(raw[3])
	p2 = uint32(raw[4])<<24 | uint32(raw[5])<<16 | uint32(raw[6])<<8 | uint32(raw[7])
	p3 = uint32(raw[8])<<24 | uint32(raw[9])<<16 | uint32(raw[10])<<8 | uint32(raw[11])
	p4 = uint32(raw[12])<<24 | uint32(raw[13])<<16 | uint32(raw[14])<<8 | uint32(raw[15])
	return &common.UUID{Part1: p1, Part2: p2, Part3: p3, Part4: p4}
}

func hexValue(b byte) byte {
	switch {
	case b >= '0' && b <= '9':
		return b - '0'
	case b >= 'a' && b <= 'f':
		return b - 'a' + 10
	case b >= 'A' && b <= 'F':
		return b - 'A' + 10
	default:
		return 0
	}
}

func getJSON(t *testing.T, client *http.Client, url string, target any) {
	t.Helper()
	response, err := client.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("GET %s status = %d: %s", url, response.StatusCode, body)
	}
	if err := json.NewDecoder(response.Body).Decode(target); err != nil {
		t.Fatalf("decode %s: %v", url, err)
	}
}
