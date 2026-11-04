// Package legacyview serves the original console's device-first view.
//
// The rewrite models intent as networks with members; the original exposed
// "machine -> instances" endpoints. Rather than let that second model leak into
// the compiler or the session layer, this package translates: it knows the RPC
// method indexes and the request shapes, and nothing else.
package legacyview

import (
	"context"
	"fmt"
	"strings"

	"google.golang.org/protobuf/proto"

	"github.com/easytier/easytier-console/internal/central"
	"github.com/easytier/easytier-console/internal/pb/api/manage"
	"github.com/easytier/easytier-console/internal/pb/common"
	"github.com/easytier/easytier-console/internal/rpc"
)

// Caller reaches a connected device. session.Manager satisfies it.
type Caller interface {
	Call(ctx context.Context, userID int64, machineID string, key rpc.ServiceKey, method uint32, req proto.Message) ([]byte, error)
}

// Service answers the device-first endpoints.
type Service struct {
	Caller Caller
}

// New wires a service to a caller.
func New(caller Caller) *Service { return &Service{Caller: caller} }

// parseUUID turns a string instance id into the protobuf form. Ids are stored as
// UUID text, so a malformed one is a client error, not a panic.
func parseUUID(value string) (*common.UUID, error) {
	trimmed := strings.ReplaceAll(strings.TrimSpace(value), "-", "")
	if len(trimmed) != 32 {
		return nil, fmt.Errorf("legacyview: %q is not a uuid", value)
	}
	var words [4]uint64
	for i := 0; i < 4; i++ {
		var word uint64
		if _, err := fmt.Sscanf(trimmed[i*8:(i+1)*8], "%08x", &word); err != nil {
			return nil, fmt.Errorf("legacyview: %q is not a uuid", value)
		}
		words[i] = word
	}
	return &common.UUID{
		Part1: uint32(words[0] >> 32),
		Part2: uint32(words[0] & 0xffffffff),
		Part3: uint32(words[1] >> 32),
		Part4: uint32(words[1] & 0xffffffff),
	}, nil
}

// CollectNetworkInfo asks the device for the runtime state of its instances.
func (s *Service) CollectNetworkInfo(ctx context.Context, userID int64, machine string, instanceIDs []string) (*manage.CollectNetworkInfoResponse, error) {
	request := &manage.CollectNetworkInfoRequest{}
	for _, id := range instanceIDs {
		uuid, err := parseUUID(id)
		if err != nil {
			return nil, err
		}
		request.InstIds = append(request.InstIds, uuid)
	}
	body, err := s.Caller.Call(ctx, userID, machine, central.WebClientKey, central.MethodCollectNetworkInfo, request)
	if err != nil {
		return nil, err
	}
	response := &manage.CollectNetworkInfoResponse{}
	if err := proto.Unmarshal(body, response); err != nil {
		return nil, fmt.Errorf("legacyview: decode collect info: %w", err)
	}
	return response, nil
}

// ListNetworkInstanceMeta asks the device for instance metadata.
func (s *Service) ListNetworkInstanceMeta(ctx context.Context, userID int64, machine string, instanceIDs []string) (*manage.ListNetworkInstanceMetaResponse, error) {
	request := &manage.ListNetworkInstanceMetaRequest{}
	for _, id := range instanceIDs {
		uuid, err := parseUUID(id)
		if err != nil {
			return nil, err
		}
		request.InstIds = append(request.InstIds, uuid)
	}
	body, err := s.Caller.Call(ctx, userID, machine, central.WebClientKey, central.MethodListNetworkInstanceMeta, request)
	if err != nil {
		return nil, err
	}
	response := &manage.ListNetworkInstanceMetaResponse{}
	if err := proto.Unmarshal(body, response); err != nil {
		return nil, fmt.Errorf("legacyview: decode metas: %w", err)
	}
	return response, nil
}

// GetNetworkInstanceConfig returns the config the device is running.
func (s *Service) GetNetworkInstanceConfig(ctx context.Context, userID int64, machine, instanceID string) (*manage.GetNetworkInstanceConfigResponse, error) {
	uuid, err := parseUUID(instanceID)
	if err != nil {
		return nil, err
	}
	body, err := s.Caller.Call(ctx, userID, machine, central.WebClientKey, central.MethodGetNetworkInstanceCfg,
		&manage.GetNetworkInstanceConfigRequest{InstId: uuid})
	if err != nil {
		return nil, err
	}
	response := &manage.GetNetworkInstanceConfigResponse{}
	if err := proto.Unmarshal(body, response); err != nil {
		return nil, fmt.Errorf("legacyview: decode instance config: %w", err)
	}
	return response, nil
}

// DeleteNetworkInstance stops one instance on the device.
func (s *Service) DeleteNetworkInstance(ctx context.Context, userID int64, machine, instanceID string) error {
	uuid, err := parseUUID(instanceID)
	if err != nil {
		return err
	}
	_, err = s.Caller.Call(ctx, userID, machine, central.WebClientKey, central.MethodDeleteNetworkInstance,
		&manage.DeleteNetworkInstanceRequest{InstIds: []*common.UUID{uuid}})
	return err
}

// RunNetworkInstance starts one instance on the device with the given config.
func (s *Service) RunNetworkInstance(ctx context.Context, userID int64, machine string, cfg *manage.NetworkConfig, overwrite bool) error {
	uuid, err := parseUUID(cfg.GetInstanceId())
	if err != nil {
		return fmt.Errorf("legacyview: the config needs an instance_id: %w", err)
	}
	_, err = s.Caller.Call(ctx, userID, machine, central.WebClientKey, central.MethodRunNetworkInstance,
		&manage.RunNetworkInstanceRequest{InstId: uuid, Config: cfg, Overwrite: overwrite})
	return err
}

// ValidateConfig asks the device whether it would accept this config.
func (s *Service) ValidateConfig(ctx context.Context, userID int64, machine string, cfg *manage.NetworkConfig) (*manage.ValidateConfigResponse, error) {
	body, err := s.Caller.Call(ctx, userID, machine, central.WebClientKey, central.MethodValidateConfig,
		&manage.ValidateConfigRequest{Config: cfg})
	if err != nil {
		return nil, err
	}
	response := &manage.ValidateConfigResponse{}
	if err := proto.Unmarshal(body, response); err != nil {
		return nil, fmt.Errorf("legacyview: decode validate response: %w", err)
	}
	return response, nil
}
