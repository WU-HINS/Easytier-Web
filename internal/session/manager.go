package session

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/easytier/easytier-console/internal/db/bal"
	"github.com/easytier/easytier-console/internal/logging"
	"github.com/easytier/easytier-console/internal/pb/common"
	"github.com/easytier/easytier-console/internal/pb/web"
	"github.com/easytier/easytier-console/internal/rpc"
	"github.com/easytier/easytier-console/internal/wire"
)

var sessionLog = logging.New("SESSION")

// WebServerService method indexes, taken from declaration order in web.proto.
const (
	webServerHeartbeat  uint32 = 1
	webServerGetFeature uint32 = 2
)

// WebServerKey addresses the service a device calls on the control plane.
var WebServerKey = rpc.ServiceKey{ProtoName: "web", ServiceName: "WebServerService"}

// Policy carries the heartbeat window negotiated with devices.
type Policy struct {
	Interval time.Duration
	Timeout  time.Duration
}

// DefaultPolicy matches the 2.7.0 defaults (3500 ms interval, 15 s timeout).
func DefaultPolicy() Policy {
	return Policy{Interval: 3500 * time.Millisecond, Timeout: 15 * time.Second}
}

// Validate enforces the same bounds the Rust side enforces at startup.
func (p Policy) Validate() error {
	if p.Interval < time.Second || p.Interval > time.Minute {
		return fmt.Errorf("session: heartbeat interval must be within 1s..60s")
	}
	if p.Timeout < 5*time.Second || p.Timeout > 2*time.Minute {
		return fmt.Errorf("session: heartbeat timeout must be within 5s..120s")
	}
	if p.Timeout < p.Interval+5*time.Second {
		return fmt.Errorf("session: heartbeat timeout must exceed the interval by 5s")
	}
	return nil
}

// Session is one connected device.
type Session struct {
	MachineID string
	UserID    int64
	ClientURL string
	// protoName caches the spelling of the device service this peer accepted.
	protoName   string
	serviceName string
	// Transport is "tcp", "ws" or "udp"; it is set once at registration.
	Transport string
	StartedAt time.Time

	mux *rpc.Mux

	mu       sync.Mutex
	lastSeen time.Time
	token    string
	req      *web.HeartbeatRequest
	bound    bool
}

// Snapshot is the read-mostly view the REST layer renders.
type Snapshot struct {
	MachineID string
	UserID    int64
	ClientURL string
	StartedAt time.Time
	LastSeen  time.Time
	Online    bool
	Hostname  string
	Version   string
	Instances []string
	Failed    []string
}

// Manager owns every live session.
type Manager struct {
	store  *bal.Store
	auth   Authenticator
	policy Policy

	mu       sync.RWMutex
	sessions map[string]*Session

	now func() time.Time
}

// NewManager builds a manager with a validated heartbeat policy.
func NewManager(db *bal.Store, authz Authenticator, policy Policy) (*Manager, error) {
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	return &Manager{
		store:    db,
		auth:     authz,
		policy:   policy,
		sessions: make(map[string]*Session),
		now:      time.Now,
	}, nil
}

// sessionKey carries the transport: one device may hold a tcp, a ws and a udp
// tunnel at the same time, and they must not evict each other (round 180).
// transportRank orders the transports a device may hold at once. The console
// pushes one configuration per device, so the registry has to pick a single
// tunnel: tcp first (lowest latency, no upgrade), then ws, then udp.

func transportRank(transport string) int {
	switch transport {
	case "tcp":
		return 3
	case "ws":
		return 2
	case "udp":
		return 1
	default:
		return 0
	}
}
func sessionKey(userID int64, machineID, transport string) string {
	return fmt.Sprintf("%d/%s/%s", userID, machineID, transport)
}

// Serve runs one accepted tunnel until it closes. It blocks, so callers run it
// in a goroutine per connection.
func (m *Manager) Serve(ctx context.Context, tunnel wire.Tunnel) error {
	return m.ServeAs(ctx, tunnel, "")
}

// ServeAs is Serve with an expected username taken from the upload path. The
// heartbeat token must resolve to that user, otherwise the channel is dropped
// before any state is written.
func (m *Manager) ServeAs(ctx context.Context, tunnel wire.Tunnel, expectedUsername string) error {
	session := &Session{
		ClientURL: tunnel.RemoteAddr(),
		Transport: wire.TransportOf(tunnel),
		StartedAt: m.now(),
	}
	registry := rpc.NewRegistry()
	if err := registry.Register(WebServerKey, webServerHeartbeat, m.heartbeatHandler(session, expectedUsername)); err != nil {
		return err
	}
	// Only a bound session may ask for features. The handler used to be reachable
	// before the first heartbeat, which on the datagram transport turns a spoofed
	// source address into a reflector (review team, round 150).
	if err := registry.Register(WebServerKey, webServerGetFeature, session.boundOnly(getFeatureHandler)); err != nil {
		return err
	}
	// Watch for a peer that keeps the socket open but stops talking (dead
	// device, half open connection, slow loris). Without this the goroutine and
	// the file descriptor live until the process restarts.
	lastFrame := &atomic.Int64{}
	lastFrame.Store(time.Now().UnixNano())
	// A datagram transport can deliver the same request twice; a stream cannot.
	datagram := false
	if capability, ok := tunnel.(interface{ IsDatagram() bool }); ok {
		datagram = capability.IsDatagram()
	}
	session.mux = rpc.NewMux(tunnel, registry, rpc.Options{
		Dedupe:      datagram,
		CallTimeout: m.policy.Timeout,
		OnFrame: func(direction string, _ *common.RpcPacket) {
			if direction == "in" {
				lastFrame.Store(time.Now().UnixNano())
			}
		},
	})
	defer session.mux.Close()
	defer m.forget(session)

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		idleLimit := m.policy.Timeout
		ticker := time.NewTicker(m.policy.Timeout / 2)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				last := time.Unix(0, lastFrame.Load())
				if time.Since(last) > idleLimit {
					sessionLog.Sub("Idle").Tag("CLOSE").Info(
						"idle session closed: " + tunnel.RemoteAddr() + " silent for " + time.Since(last).Round(time.Second).String())
					cancel()
					session.mux.Close()
					return
				}
			}
		}
	}()
	return session.mux.Run(runCtx)
}

// boundOnly gates a handler on the session having seen a valid heartbeat.
//
// The official client calls GetFeature before it says who it is; answering that
// call would make the console a reflector for a spoofed source address on UDP.
// The clients then fall back to the legacy tunnel, which is a load-bearing
// behaviour: allowing the call early made 2.6.4 and 2.7.0 disappear (F-274).
// This is the reason the fallback exists, expressed as code a test can hold.
func (s *Session) boundOnly(handler rpc.Handler) rpc.Handler {
	return func(ctx context.Context, body []byte) ([]byte, error) {
		s.mu.Lock()
		bound := s.bound
		s.mu.Unlock()
		if !bound {
			return nil, errors.New("session: not bound yet")
		}
		return handler(ctx, body)
	}
}

func getFeatureHandler(ctx context.Context, body []byte) ([]byte, error) {
	return proto.Marshal(&web.GetFeatureResponse{SupportEncryption: true})
}

func (m *Manager) heartbeatHandler(session *Session, expectedUsername string) rpc.Handler {
	return func(ctx context.Context, body []byte) ([]byte, error) {
		req := &web.HeartbeatRequest{}
		if err := proto.Unmarshal(body, req); err != nil {
			return nil, fmt.Errorf("session: decode heartbeat: %w", err)
		}
		if req.MachineId == nil {
			return nil, fmt.Errorf("session: heartbeat without machine id")
		}
		machineID := uuidString(req.MachineId)

		session.mu.Lock()
		bound := session.bound
		boundToken := session.token
		session.mu.Unlock()

		if !bound {
			userID, err := m.auth.AuthenticateToken(ctx, req.UserToken, machineID)
			if err == nil && expectedUsername != "" && req.UserToken != expectedUsername {
				return nil, fmt.Errorf("session: upload path user %q does not match token", expectedUsername)
			}
			if errors.Is(err, ErrBlocked) {
				return nil, fmt.Errorf("session: device %s is blocked", machineID)
			}
			if err != nil {
				return nil, fmt.Errorf("session: token rejected: %w", err)
			}
			session.mu.Lock()
			session.UserID = userID
			session.MachineID = machineID
			session.token = req.UserToken
			session.bound = true
			session.mu.Unlock()
			m.register(session)
		} else if boundToken != req.UserToken {
			return nil, fmt.Errorf("session: heartbeat identity does not match bound session")
		}

		now := m.now()
		device := bal.Device{
			UserID:          session.UserID,
			MachineID:       machineID,
			Hostname:        req.Hostname,
			EasytierVersion: req.EasytierVersion,
			DeviceOS:        deviceOSJSON(req.DeviceOs),
			ClientURL:       session.ClientURL,
			FirstSeen:       now,
			LastSeen:        now,
		}
		if err := m.store.UpsertDevice(ctx, device); err != nil {
			return nil, err
		}

		session.mu.Lock()
		session.lastSeen = now
		session.req = req
		session.mu.Unlock()

		return proto.Marshal(&web.HeartbeatResponse{
			HeartbeatIntervalMs: proto.Uint32(uint32(m.policy.Interval.Milliseconds())),
			HeartbeatTimeoutMs:  proto.Uint32(uint32(m.policy.Timeout.Milliseconds())),
		})
	}
}

func deviceOSJSON(info *web.DeviceOsInfo) string {
	if info == nil {
		return "{}"
	}
	return fmt.Sprintf(`{"os_type":%q,"version":%q,"distribution":%q}`,
		info.OsType, info.Version, info.Distribution)
}

func (m *Manager) register(session *Session) {
	var replaced *Session
	m.mu.Lock()
	session.mu.Lock()
	key := sessionKey(session.UserID, session.MachineID, session.Transport)
	session.mu.Unlock()
	if existing, ok := m.sessions[key]; ok && existing != session {
		replaced = existing
	}
	m.sessions[key] = session
	m.mu.Unlock()
	// A second tunnel for the same device must not leave the first one running:
	// the registry only remembers one session, so blocking the device would close
	// the new tunnel while the old one kept heartbeating (review team, r136).
	if replaced != nil && replaced.mux != nil {
		replaced.mux.Close()
	}
}

func (m *Manager) forget(session *Session) {
	session.mu.Lock()
	key := sessionKey(session.UserID, session.MachineID, session.Transport)
	session.mu.Unlock()
	if session.UserID == 0 && session.MachineID == "" {
		return
	}
	m.mu.Lock()
	if current, ok := m.sessions[key]; ok && current == session {
		delete(m.sessions, key)
	}
	m.mu.Unlock()
}

// ByMachine resolves a live session.
func (m *Manager) ByMachine(userID int64, machineID string) (*Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var best *Session
	for _, session := range m.sessions {
		if session.UserID != userID || session.MachineID != machineID {
			continue
		}
		if best == nil || transportRank(session.Transport) > transportRank(best.Transport) {
			best = session
		}
	}
	return best, best != nil
}

// Call forwards one RPC into a connected device. This is the primitive behind
// the console's proxy-rpc endpoint.
// serviceKeyVariants lists how the same device service is addressed across client
// versions. The client compares the whole key (domain, proto, service), and the
// generated descriptors moved between releases, so a single spelling cannot serve
// 2.4.x through 2.7.x. The first entry is our native one: it is tried first and
// the winner is remembered per session.
var serviceKeyVariants = []rpc.ServiceKey{
	// The generated descriptors name the service twice, exactly like the client
	// does for WebServerService: proto_name == service_name (seen in the 2.7.0
	// client trace). That spelling is tried first because it is the real one.
	{ProtoName: "WebClientService", ServiceName: "WebClientService"},
	{ProtoName: "api.manage", ServiceName: "WebClientService"},
	{ProtoName: "", ServiceName: "WebClientService"},
	{ProtoName: "api_manage", ServiceName: "WebClientService"},
	{ProtoName: "", ServiceName: "api.manage.WebClientService"},
	{ProtoName: "api.manage", ServiceName: "api.manage.WebClientService"},
	{ProtoName: "api_manage", ServiceName: "api_manage.WebClientService"},
}

// matchingVariant returns the candidate that shares the service name of key, so a
// caller other than the device push keeps its own service.
func matchingVariant(key rpc.ServiceKey) []rpc.ServiceKey {
	out := make([]rpc.ServiceKey, 0, len(serviceKeyVariants))
	for _, variant := range serviceKeyVariants {
		if strings.HasSuffix(variant.ServiceName, "."+key.ServiceName) || variant.ServiceName == key.ServiceName {
			out = append(out, variant)
		}
	}
	return out
}

// wantsAnotherProtoName reports whether the peer refused the service key.
func wantsAnotherProtoName(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "InvalidService")
}

// CallRaw is Call for services whose proto has no generated Go types here: the
// caller supplies the encoded request instead of a message.
func (m *Manager) CallRaw(ctx context.Context, userID int64, machineID string, key rpc.ServiceKey, method uint32, raw []byte) ([]byte, error) {
	session, ok := m.ByMachine(userID, machineID)
	if !ok {
		return nil, bal.ErrNotFound
	}
	session.mu.Lock()
	preferred := session.protoName
	preferredService := session.serviceName
	session.mu.Unlock()
	if preferred != "" {
		key.ProtoName = preferred
	}
	if preferredService != "" {
		key.ServiceName = preferredService
	}
	return session.mux.CallRaw(ctx, key, method, raw)
}

func (m *Manager) Call(ctx context.Context, userID int64, machineID string, key rpc.ServiceKey, method uint32, req proto.Message) ([]byte, error) {
	session, ok := m.ByMachine(userID, machineID)
	if !ok {
		return nil, bal.ErrNotFound
	}
	session.mu.Lock()
	preferred := session.protoName
	preferredService := session.serviceName
	session.mu.Unlock()
	if preferred != "" {
		key.ProtoName = preferred
	}
	if preferredService != "" {
		key.ServiceName = preferredService
	}
	out, err := session.mux.Call(ctx, key, method, req)
	if !wantsAnotherProtoName(err) {
		return out, err
	}
	// The peer knows the service but not under this proto name: try the other
	// spellings once and remember the one it accepted.
	lastErr := err
	for _, candidate := range matchingVariant(key) {
		if candidate == key {
			continue
		}
		out, err = session.mux.Call(ctx, candidate, method, req)
		if err == nil {
			session.mu.Lock()
			session.protoName = candidate.ProtoName
			session.serviceName = candidate.ServiceName
			session.mu.Unlock()
			return out, nil
		}
		lastErr = err
		if !wantsAnotherProtoName(err) {
			return nil, err
		}
	}
	return nil, lastErr
}

// Snapshots renders every live session, optionally filtered by tenant.
func (m *Manager) Snapshots(userID int64) []Snapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Snapshot, 0, len(m.sessions))
	for _, session := range m.sessions {
		session.mu.Lock()
		if userID != 0 && session.UserID != userID {
			session.mu.Unlock()
			continue
		}
		snapshot := Snapshot{
			MachineID: session.MachineID,
			UserID:    session.UserID,
			ClientURL: session.ClientURL,
			StartedAt: session.StartedAt,
			LastSeen:  session.lastSeen,
			Online:    m.now().Sub(session.lastSeen) <= m.policy.Timeout,
		}
		if session.req != nil {
			snapshot.Hostname = session.req.Hostname
			snapshot.Version = session.req.EasytierVersion
			for _, id := range session.req.RunningNetworkInstances {
				snapshot.Instances = append(snapshot.Instances, uuidString(id))
			}
			for _, id := range session.req.FailedNetworkInstances {
				snapshot.Failed = append(snapshot.Failed, uuidString(id))
			}
		}
		session.mu.Unlock()
		out = append(out, snapshot)
	}
	return out
}

// SweepStale closes sessions that stopped heartbeating, mirroring the Rust
// manager which drops sessions after the heartbeat timeout.
func (m *Manager) SweepStale() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	removed := 0
	for key, session := range m.sessions {
		session.mu.Lock()
		stale := m.now().Sub(session.lastSeen) > m.policy.Timeout
		session.mu.Unlock()
		if stale {
			session.mux.Close()
			delete(m.sessions, key)
			removed++
		}
	}
	return removed
}

// OnlineInstances reports the instances each online machine of a tenant claims,
// keyed by machine id. The console uses it to enforce the instance quota on the
// report path, not only on the push path.
func (m *Manager) OnlineInstances(userID int64) map[string][]string {
	usage := map[string][]string{}
	for _, snapshot := range m.Snapshots(userID) {
		if !snapshot.Online || len(snapshot.Instances) == 0 {
			continue
		}
		usage[snapshot.MachineID] = append([]string(nil), snapshot.Instances...)
	}
	return usage
}

// DisconnectUser tears down every live session of a tenant and returns how many
// existed. Deleting an account calls this after the row is gone so a device that
// reconnected in the check-then-delete window cannot keep a live session
// pointing at a deleted user.
func (m *Manager) DisconnectUser(userID int64) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	stopped := 0
	for key, session := range m.sessions {
		session.mu.Lock()
		owner := session.UserID
		session.mu.Unlock()
		if owner != userID {
			continue
		}
		session.mux.Close()
		delete(m.sessions, key)
		stopped++
	}
	return stopped
}

// ConnectedUsers lists the distinct tenants with a live session.
func (m *Manager) ConnectedUsers() []int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	seen := map[int64]bool{}
	out := make([]int64, 0, len(m.sessions))
	for _, session := range m.sessions {
		session.mu.Lock()
		owner := session.UserID
		session.mu.Unlock()
		if owner == 0 || seen[owner] {
			continue
		}
		seen[owner] = true
		out = append(out, owner)
	}
	return out
}

// Count reports how many sessions are live.
func (m *Manager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

// UUIDString renders a common.UUID for callers outside this package.
func UUIDString(u *common.UUID) string { return uuidString(u) }

// uuidString renders common.UUID. The wire form is four uint32 parts holding
// the uuid bytes big-endian (easytier-proto/src/common.rs), not a byte buffer.
func uuidString(u *common.UUID) string {
	if u == nil {
		return ""
	}
	var raw [16]byte
	binary.BigEndian.PutUint32(raw[0:4], u.Part1)
	binary.BigEndian.PutUint32(raw[4:8], u.Part2)
	binary.BigEndian.PutUint32(raw[8:12], u.Part3)
	binary.BigEndian.PutUint32(raw[12:16], u.Part4)
	return fmt.Sprintf("%x-%x-%x-%x-%x", raw[0:4], raw[4:6], raw[6:8], raw[8:10], raw[10:16])
}

// uuidFromString is the inverse of uuidString.
func uuidFromString(value string) (*common.UUID, error) {
	cleaned := strings.ReplaceAll(value, "-", "")
	raw, err := hex.DecodeString(cleaned)
	if err != nil || len(raw) != 16 {
		return nil, fmt.Errorf("session: invalid uuid %q", value)
	}
	return &common.UUID{
		Part1: binary.BigEndian.Uint32(raw[0:4]),
		Part2: binary.BigEndian.Uint32(raw[4:8]),
		Part3: binary.BigEndian.Uint32(raw[8:12]),
		Part4: binary.BigEndian.Uint32(raw[12:16]),
	}, nil
}

// DisconnectDevice closes the live session of one machine. Blocking a device has
// to take effect immediately: otherwise an already connected device keeps its
// tunnel until it happens to reconnect, which can be days.
func (m *Manager) DisconnectDevice(userID int64, machineID string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	stopped := 0
	for key, session := range m.sessions {
		session.mu.Lock()
		matches := session.UserID == userID && session.MachineID == machineID
		session.mu.Unlock()
		if !matches {
			continue
		}
		session.mux.Close()
		delete(m.sessions, key)
		stopped++
	}
	return stopped
}
