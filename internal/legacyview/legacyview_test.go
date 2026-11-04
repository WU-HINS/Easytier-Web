package legacyview_test

import (
	"context"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/easytier/easytier-console/internal/central"
	"github.com/easytier/easytier-console/internal/legacyview"
	"github.com/easytier/easytier-console/internal/pb/api/manage"
	"github.com/easytier/easytier-console/internal/rpc"
)

// stubCaller records what the adapter asked the device and answers with a canned
// body, so the test pins the method index and the request shape.
type stubCaller struct {
	method uint32
	key    rpc.ServiceKey
	req    proto.Message
	body   []byte
	err    error
	calls  int
}

func (s *stubCaller) Call(_ context.Context, _ int64, _ string, key rpc.ServiceKey, method uint32, req proto.Message) ([]byte, error) {
	s.calls++
	s.method = method
	s.key = key
	s.req = req
	return s.body, s.err
}

const instanceID = "11111111-2222-3333-4444-555555555555"

// TestAdapterUsesTheClientServiceMethods covers the device-first endpoints: each
// one has to reach the device through WebClientService with the documented method
// index and decode the protobuf answer.
func TestAdapterUsesTheClientServiceMethods(t *testing.T) {
	ctx := context.Background()

	infoBody, err := proto.Marshal(&manage.CollectNetworkInfoResponse{})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	info := &stubCaller{body: infoBody}
	if _, err := legacyview.New(info).CollectNetworkInfo(ctx, 1, "machine", []string{instanceID}); err != nil {
		t.Fatalf("collect: %v", err)
	}
	if info.method != central.MethodCollectNetworkInfo {
		t.Fatalf("collect method = %d, want %d", info.method, central.MethodCollectNetworkInfo)
	}
	if info.key != central.WebClientKey {
		t.Fatalf("collect went to %v, want the client service", info.key)
	}
	if got := info.req.(*manage.CollectNetworkInfoRequest); len(got.InstIds) != 1 {
		t.Fatalf("collect request carried %d ids, want 1", len(got.InstIds))
	}

	metaBody, err := proto.Marshal(&manage.ListNetworkInstanceMetaResponse{})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	meta := &stubCaller{body: metaBody}
	if _, err := legacyview.New(meta).ListNetworkInstanceMeta(ctx, 1, "machine", nil); err != nil {
		t.Fatalf("metas: %v", err)
	}
	if meta.method != central.MethodListNetworkInstanceMeta {
		t.Fatalf("metas method = %d, want %d", meta.method, central.MethodListNetworkInstanceMeta)
	}

	configBody, err := proto.Marshal(&manage.GetNetworkInstanceConfigResponse{})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	config := &stubCaller{body: configBody}
	if _, err := legacyview.New(config).GetNetworkInstanceConfig(ctx, 1, "machine", instanceID); err != nil {
		t.Fatalf("config: %v", err)
	}
	if config.method != central.MethodGetNetworkInstanceCfg {
		t.Fatalf("config method = %d, want %d", config.method, central.MethodGetNetworkInstanceCfg)
	}

	remove := &stubCaller{}
	if err := legacyview.New(remove).DeleteNetworkInstance(ctx, 1, "machine", instanceID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if remove.method != central.MethodDeleteNetworkInstance {
		t.Fatalf("delete method = %d, want %d", remove.method, central.MethodDeleteNetworkInstance)
	}
}

// TestAdapterRejectsAMalformedInstanceID keeps a client error from becoming a
// device call with a zeroed uuid.
func TestAdapterRejectsAMalformedInstanceID(t *testing.T) {
	caller := &stubCaller{}
	if err := legacyview.New(caller).DeleteNetworkInstance(context.Background(), 1, "machine", "not-a-uuid"); err == nil {
		t.Fatal("a malformed id must be rejected")
	}
	if caller.calls != 0 {
		t.Fatalf("the device was called %d times for a malformed id", caller.calls)
	}
}

// TestAdapterStartsAndValidates covers the two write paths: both must carry the
// caller's config to the device, and starting one without an instance id is a
// client error rather than a call with a zeroed uuid.
func TestAdapterStartsAndValidates(t *testing.T) {
	ctx := context.Background()

	run := &stubCaller{}
	cfg := &manage.NetworkConfig{InstanceId: proto.String(instanceID), Hostname: proto.String("node")}
	if err := legacyview.New(run).RunNetworkInstance(ctx, 1, "machine", cfg, true); err != nil {
		t.Fatalf("run: %v", err)
	}
	if run.method != central.MethodRunNetworkInstance {
		t.Fatalf("run method = %d, want %d", run.method, central.MethodRunNetworkInstance)
	}
	if got := run.req.(*manage.RunNetworkInstanceRequest); got.GetConfig().GetHostname() != "node" || !got.GetOverwrite() {
		t.Fatalf("run request lost the config or the overwrite flag: %v", got)
	}

	validateBody, err := proto.Marshal(&manage.ValidateConfigResponse{})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	validate := &stubCaller{body: validateBody}
	if _, err := legacyview.New(validate).ValidateConfig(ctx, 1, "machine", cfg); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if validate.method != central.MethodValidateConfig {
		t.Fatalf("validate method = %d, want %d", validate.method, central.MethodValidateConfig)
	}

	noID := &stubCaller{}
	if err := legacyview.New(noID).RunNetworkInstance(ctx, 1, "machine", &manage.NetworkConfig{}, true); err == nil {
		t.Fatal("a config without an instance id must be rejected")
	}
	if noID.calls != 0 {
		t.Fatalf("the device was called %d times without an instance id", noID.calls)
	}
}
