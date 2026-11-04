// Package rpc reimplements EasyTier's bidirectional RPC multiplexer: the
// layer that turns a tunnel full of packets into correlated request/response
// calls in both directions.
//
// Wire facts this package depends on (see docs/PROTOCOL.md):
//   - RpcPacket is protobuf-marshalled into a tunnel packet of type RpcReq(8)
//     or RpcResp(9).
//   - A request body is common.RpcRequest{request: <method input>}.
//   - A response body is common.RpcResponse{response|error, runtime_us}.
//   - The service is addressed by (proto_name, service_name) plus a
//     1-based method_index taken from declaration order in the .proto file.
package rpc

import (
	"context"
	"fmt"
	"sync"
)

// ServiceKey addresses one registered service. DomainName allows the same
// service to be registered several times, matching ServiceRegistry.
type ServiceKey struct {
	DomainName  string
	ProtoName   string
	ServiceName string
}

func (k ServiceKey) String() string {
	if k.DomainName == "" {
		return k.ProtoName + "." + k.ServiceName
	}
	return k.DomainName + "/" + k.ProtoName + "." + k.ServiceName
}

// Handler executes one method. req is the unmarshalled method input, and the
// returned bytes are the marshalled method output.
type Handler func(ctx context.Context, req []byte) ([]byte, error)

// Registry maps service keys and method indexes onto handlers.
type Registry struct {
	mu       sync.RWMutex
	services map[ServiceKey]map[uint32]Handler
}

// NewRegistry builds an empty registry.
func NewRegistry() *Registry {
	return &Registry{services: make(map[ServiceKey]map[uint32]Handler)}
}

// Register adds one method. Re-registering an existing method replaces it,
// which keeps test setup simple.
func (r *Registry) Register(key ServiceKey, methodIndex uint32, h Handler) error {
	if methodIndex == 0 {
		return fmt.Errorf("rpc: method index must be 1-based for %s", key)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.services[key] == nil {
		r.services[key] = make(map[uint32]Handler)
	}
	r.services[key][methodIndex] = h
	return nil
}

// Lookup resolves a method handler.
func (r *Registry) Lookup(key ServiceKey, methodIndex uint32) (Handler, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if methods, ok := r.services[key]; ok {
		if handler, ok := methods[methodIndex]; ok {
			return handler, true
		}
	}
	// Not every client fills proto_name the same way: the official 2.7.0 client
	// sends an empty one, which made every heartbeat answer InvalidMethodIndex.
	// Matching on the service name keeps the dispatch stable across versions.
	for registered, methods := range r.services {
		if registered.ServiceName != key.ServiceName || registered.DomainName != key.DomainName {
			continue
		}
		if handler, ok := methods[methodIndex]; ok {
			return handler, true
		}
	}
	return nil, false
}

// HasService reports whether any method is registered for the key.
func (r *Registry) HasService(key ServiceKey) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.services[key]
	return ok
}
