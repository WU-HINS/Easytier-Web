package httpapi

import (
	"fmt"
	"net/http"
	"sort"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	"github.com/easytier/easytier-console/internal/db/bal"
	"github.com/easytier/easytier-console/internal/pb/methodindex"
)

// methodAvailability tells whether this build can actually marshal the request
// and response types of a proxied method. The method index is generated from the
// upstream .proto files and lists far more methods than the generated Go
// messages cover, so a method can be "known" and still unusable.
func methodAvailability(service, method string) error {
	entry, ok := lookupMethod(service, method)
	if !ok {
		return fmt.Errorf("unknown rpc %s/%s", service, method)
	}
	if _, err := protoregistry.GlobalFiles.FindDescriptorByName(descriptorName(entry.Input)); err != nil {
		return fmt.Errorf("%s/%s is not available in this build: message %s was not generated", service, method, entry.Input)
	}
	if _, err := protoregistry.GlobalFiles.FindDescriptorByName(descriptorName(entry.Output)); err != nil {
		return fmt.Errorf("%s/%s is not available in this build: message %s was not generated", service, method, entry.Output)
	}
	return nil
}

// rpcCapability is one method as reported to the client, so the UI can hide
// actions that this build cannot perform instead of failing on click.
type rpcCapability struct {
	Name      string "json:\"name\""
	Available bool   "json:\"available\""
	Reason    string "json:\"reason,omitempty\""
}

type rpcServiceCapability struct {
	Name    string          "json:\"name\""
	Methods []rpcCapability "json:\"methods\""
}

func (s *Server) handleRPCCapabilities(w http.ResponseWriter, r *http.Request, user bal.User) {
	services := make([]rpcServiceCapability, 0, len(methodindex.Services))
	names := make([]string, 0, len(methodindex.Services))
	for name := range methodindex.Services {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		methods := make([]rpcCapability, 0, len(methodindex.Services[name]))
		methodNames := make([]string, 0, len(methodindex.Services[name]))
		for methodName := range methodindex.Services[name] {
			methodNames = append(methodNames, methodName)
		}
		sort.Strings(methodNames)
		for _, methodName := range methodNames {
			capability := rpcCapability{Name: methodName, Available: true}
			if err := methodAvailability(name, methodName); err != nil {
				capability.Available = false
				capability.Reason = err.Error()
			}
			methods = append(methods, capability)
		}
		services = append(services, rpcServiceCapability{Name: name, Methods: methods})
	}
	writeJSON(w, http.StatusOK, map[string]any{"services": services})
}

func descriptorName(full string) protoreflect.FullName {
	return protoreflect.FullName(full)
}
