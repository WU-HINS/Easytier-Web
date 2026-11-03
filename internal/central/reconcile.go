package central

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/easytier/easytier-console/internal/db/bal"
	"github.com/easytier/easytier-console/internal/logging"
	manage "github.com/easytier/easytier-console/internal/pb/api/manage"
	"github.com/easytier/easytier-console/internal/pb/common"
	"github.com/easytier/easytier-console/internal/rpc"
)

// WebClientKey is the service devices expose for console-driven configuration.
var WebClientKey = rpc.ServiceKey{ProtoName: "api.manage", ServiceName: "WebClientService"}

// Method indexes inside api.manage.WebClientService (declaration order).
const (
	MethodValidateConfig          uint32 = 1
	MethodRunNetworkInstance      uint32 = 2
	MethodRetainNetworkInstance   uint32 = 3
	MethodCollectNetworkInfo      uint32 = 4
	MethodListNetworkInstance     uint32 = 5
	MethodDeleteNetworkInstance   uint32 = 6
	MethodGetNetworkInstanceCfg   uint32 = 7
	MethodListNetworkInstanceMeta uint32 = 8
)

// Caller reaches a connected device. session.Manager implements it.
type Caller interface {
	Call(ctx context.Context, userID int64, machineID string, key rpc.ServiceKey, method uint32, req proto.Message) ([]byte, error)
}

// Reconciler pushes compiled intent to devices on a fixed interval. It keeps
// the 2.7.0 property that the console owns the desired state while each device
// owns its runtime: a push is skipped when the content hash already matches the
// revision recorded for that device.
type Reconciler struct {
	Store    *bal.Store
	Sessions Caller
	Interval time.Duration
	Logf     func(format string, args ...any)
	// MaxInstancesPerUser is the global quota (0 disables enforcement).
	MaxInstancesPerUser func(ctx context.Context, userID int64) int
	// Instances reports what each online device is currently running. When set,
	// instances that the console created but no longer wants are stopped.
	Instances InstanceLister
}

// Report summarises one reconcile pass.
type Report struct {
	Pushed  int
	Skipped int
	Failed  int
	// CompileErrors counts networks whose intent could not be compiled. One
	// broken network must not stop the others (or other tenants) from syncing.
	CompileErrors int
	// Stopped counts instances the console asked a device to shut down because
	// its membership disappeared.
	Stopped int
}

// Run reconciles every tenant until the context is cancelled.
func (r *Reconciler) Run(ctx context.Context) {
	if r.Interval <= 0 {
		r.Interval = 5 * time.Second
	}
	if r.Logf == nil {
		logger := logging.New("CENTRAL").Tag("RECONCILE")
		r.Logf = func(format string, args ...any) {
			logger.Warn(fmt.Sprintf(format, args...))
		}
	}
	ticker := time.NewTicker(r.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := r.ReconcileAll(ctx); err != nil && !errors.Is(err, context.Canceled) {
				r.Logf("reconcile: %v", err)
			}
		}
	}
}

// ReconcileAll walks every tenant that owns a network.
func (r *Reconciler) ReconcileAll(ctx context.Context) (Report, error) {
	users, err := r.Store.TenantsWithNetworks(ctx)
	if err != nil {
		return Report{}, err
	}
	total := Report{}
	for _, userID := range users {
		report, err := r.ReconcileUser(ctx, userID)
		if err != nil {
			// A failing tenant must not stop the loop for everyone else.
			total.Failed++
			if r.Logf != nil {
				r.Logf("reconcile: user %d failed, continuing: %v", userID, err)
			}
			continue
		}
		total.Pushed += report.Pushed
		total.Skipped += report.Skipped
		total.Failed += report.Failed
	}
	// One line per pass with all three counters: "nothing pushed" alone cannot
	// distinguish "already in sync" from "every push failed".
	if total.Pushed+total.Skipped+total.Failed > 0 && r.Logf != nil && total.Pushed+total.Failed > 0 {
		r.Logf("reconcile: pushed=%d skipped=%d failed=%d compile_errors=%d stopped=%d",
			total.Pushed, total.Skipped, total.Failed, total.CompileErrors, total.Stopped)
	}
	return total, nil
}

// ReconcileUser compiles and pushes every network of one tenant.
func (r *Reconciler) ReconcileUser(ctx context.Context, userID int64) (Report, error) {
	networks, err := r.Store.ListNetworks(ctx, userID)
	if err != nil {
		return Report{}, err
	}
	devices, err := r.Store.ListDevices(ctx, userID)
	if err != nil {
		return Report{}, err
	}
	byID := make(map[string]bal.Device, len(devices))
	for _, device := range devices {
		byID[device.MachineID] = device
	}
	quota := 0
	if r.MaxInstancesPerUser != nil {
		quota = r.MaxInstancesPerUser(ctx, userID)
	}
	enforceQuota := quota > 0
	budget := quota
	report := Report{}
	// expected[device] holds the instance ids the console wants running; every
	// id the console could ever have created for this tenant is tracked so we
	// never stop an instance that came from the device's own configuration.
	expected := map[string][]*common.UUID{}
	managed := map[string]map[string]bool{}
	for _, network := range networks {
		for _, device := range devices {
			id := InstanceID(network.ID, device.MachineID)
			if managed[device.MachineID] == nil {
				managed[device.MachineID] = map[string]bool{}
			}
			managed[device.MachineID][id] = true
		}
	}
	for _, network := range networks {
		members, err := r.Store.ListMembers(ctx, userID, network.ID)
		if err != nil {
			return report, err
		}
		credentials, err := r.Store.ListCredentials(ctx, userID, network.ID)
		if err != nil {
			return report, err
		}
		plan, err := Compile(network, members, byID, credentials)
		if err != nil {
			report.CompileErrors++
			if r.Logf != nil {
				r.Logf("reconcile: network %s cannot be compiled, skipping it: %v", network.ID, err)
			}
			continue
		}
		for _, member := range plan.Members {
			if uuid := protoUUID(member.InstanceID); uuid != nil {
				expected[member.DeviceID] = append(expected[member.DeviceID], uuid)
			}
			if uuid := protoUUID(member.InstanceID); uuid != nil {
				expected[member.DeviceID] = append(expected[member.DeviceID], uuid)
			}
			if enforceQuota {
				if budget <= 0 {
					report.Failed++
					r.Logf("reconcile: quota %d exhausted for user %d, skipping %s", quota, userID, member.DeviceID)
					continue
				}
				budget--
			}
			revision := PlanRevision(network.ID, member)
			current, err := r.Store.GetManagedConfigRevision(ctx, userID, member.DeviceID)
			if err != nil {
				return report, err
			}
			// The revision row is keyed by device, so it stores one entry per
			// network in a small JSON map. Comparing a single value would make a
			// device in two networks re-push both configs on every pass.
			revisions := DecodeRevisions(current)
			if revisions[network.ID] == revision {
				report.Skipped++
				continue
			}
			request := &manage.RunNetworkInstanceRequest{
				InstId:    protoUUID(member.InstanceID),
				Config:    member.Config,
				Overwrite: true,
				Source:    manage.ConfigSource_ConfigSourceWeb,
			}
			if _, err := r.Sessions.Call(ctx, userID, member.DeviceID, WebClientKey,
				MethodRunNetworkInstance, request); err != nil {
				// An offline device is normal: the next pass retries once it
				// heartbeats again. Anything else is a real push failure.
				report.Failed++
				r.Logf("reconcile: push %s to %s: %v", network.ID, member.DeviceID, err)
				continue
			}
			// No row yet means "never applied": write unconditionally. Otherwise
			// compare-and-swap so a concurrent console write is never lost.
			var expected *string
			if current != "" {
				expected = &current
			}
			revisions[network.ID] = revision
			if err := r.Store.SetManagedConfigRevision(ctx, userID, member.DeviceID, encodeRevisions(revisions), expected); err != nil {
				if errors.Is(err, bal.ErrRevisionConflict) {
					report.Failed++
					continue
				}
				return report, err
			}
			report.Pushed++
		}
	}
	report.Stopped += r.stopOrphans(ctx, userID, expected, managed)
	return report, nil
}

// planRevision is the content hash recorded per device. It covers everything the
// compiler decided, so an unchanged intent never causes a push.
// stopOrphans asks each online device to keep only the instances the console
// still wants. It only acts when every extra instance is one the console could
// have created: a device may run instances from its own configuration, and
// those must never be killed by us.
func (r *Reconciler) stopOrphans(ctx context.Context, userID int64,
	expected map[string][]*common.UUID, managed map[string]map[string]bool) int {
	if r.Instances == nil {
		return 0
	}
	stopped := 0
	for machine, reported := range r.Instances.OnlineInstances(userID) {
		want := map[string]bool{}
		for _, uuid := range expected[machine] {
			want[uuidString(uuid)] = true
		}
		extras := 0
		foreign := false
		for _, instance := range reported {
			if want[instance] {
				continue
			}
			extras++
			if !managed[machine][instance] {
				foreign = true
			}
		}
		if extras == 0 {
			continue
		}
		if foreign {
			if r.Logf != nil {
				r.Logf("reconcile: %s runs an instance the console did not create, leaving all instances alone", machine)
			}
			continue
		}
		keep := make([]*common.UUID, 0, len(want))
		seen := map[string]bool{}
		for _, uuid := range expected[machine] {
			id := uuidString(uuid)
			if seen[id] || !reportedHas(reported, id) {
				continue
			}
			seen[id] = true
			keep = append(keep, uuid)
		}
		request := &manage.RetainNetworkInstanceRequest{InstIds: keep}
		if _, err := r.Sessions.Call(ctx, userID, machine, WebClientKey, MethodRetainNetworkInstance, request); err != nil {
			if r.Logf != nil {
				r.Logf("reconcile: stop orphaned instances on %s: %v", machine, err)
			}
			continue
		}
		stopped += extras
	}
	return stopped
}

func reportedHas(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

// decodeRevisions reads the per device revision blob. Legacy rows hold a single
// opaque value; those are treated as "no history" so the first pass re-pushes
// once and then settles.
// DecodeRevisions reads the per device revision blob.
func DecodeRevisions(raw string) map[string]string {
	out := map[string]string{}
	if raw == "" || !strings.HasPrefix(raw, "{") {
		return out
	}
	var decoded map[string]string
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return out
	}
	for key, value := range decoded {
		out[key] = value
	}
	return out
}

// encodeRevisions produces a stable blob (sorted keys) so equal state always
// compares equal.
func encodeRevisions(revisions map[string]string) string {
	keys := make([]string, 0, len(revisions))
	for key := range revisions {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var builder strings.Builder
	builder.WriteString("{")
	for index, key := range keys {
		if index > 0 {
			builder.WriteString(",")
		}
		keyJSON, _ := json.Marshal(key)
		valueJSON, _ := json.Marshal(revisions[key])
		builder.Write(keyJSON)
		builder.WriteString(":")
		builder.Write(valueJSON)
	}
	builder.WriteString("}")
	return builder.String()
}

// PlanRevision is the content hash of one member plan. Exported so the HTTP
// layer can tell whether a device already has the current configuration.
func PlanRevision(networkID string, member MemberPlan) string {
	raw, err := proto.Marshal(member.Config)
	if err != nil {
		return ""
	}
	hash := sha256.New()
	hash.Write([]byte(networkID))
	hash.Write([]byte{0})
	hash.Write(raw)
	sum := hash.Sum(nil)
	return "central:" + hex.EncodeToString(sum[:16])
}

// protoUUID renders a canonical uuid string into the four-part wire form.
func uuidString(u *common.UUID) string {
	if u == nil {
		return ""
	}
	raw := make([]byte, 16)
	binary.BigEndian.PutUint32(raw[0:4], u.GetPart1())
	binary.BigEndian.PutUint32(raw[4:8], u.GetPart2())
	binary.BigEndian.PutUint32(raw[8:12], u.GetPart3())
	binary.BigEndian.PutUint32(raw[12:16], u.GetPart4())
	return fmt.Sprintf("%x-%x-%x-%x-%x", raw[0:4], raw[4:6], raw[6:8], raw[8:10], raw[10:16])
}

func protoUUID(value string) *common.UUID {
	cleaned := make([]byte, 0, 32)
	for i := 0; i < len(value); i++ {
		if value[i] == '-' {
			continue
		}
		cleaned = append(cleaned, value[i])
	}
	if len(cleaned) != 32 {
		return nil
	}
	raw := make([]byte, 16)
	if _, err := hex.Decode(raw, cleaned); err != nil {
		return nil
	}
	return &common.UUID{
		Part1: binary.BigEndian.Uint32(raw[0:4]),
		Part2: binary.BigEndian.Uint32(raw[4:8]),
		Part3: binary.BigEndian.Uint32(raw[8:12]),
		Part4: binary.BigEndian.Uint32(raw[12:16]),
	}
}

// SortPlans orders plans deterministically, which keeps logs and tests stable.
func SortPlans(plans []Plan) {
	sort.SliceStable(plans, func(i, j int) bool { return plans[i].NetworkID < plans[j].NetworkID })
}
