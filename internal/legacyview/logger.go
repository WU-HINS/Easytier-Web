package legacyview

import (
	"context"
	"fmt"

	"google.golang.org/protobuf/encoding/protowire"

	"github.com/easytier/easytier-console/internal/rpc"
)

// The logger service has no instance identifier: it sets the level of the
// process that answers, so a device has one level for all of its instances.
// Field numbers come from easytier-proto/proto/api_logger.proto:
//
//	enum LogLevel { DISABLED=0; ERROR=1; WARNING=2; INFO=3; DEBUG=4; TRACE=5; }
//	message SetLoggerConfigRequest { LogLevel level = 1; }
//	message GetLoggerConfigRequest {}
//	message GetLoggerConfigResponse { LogLevel level = 1; }
//
// There is no generated package for it here, which is why the payload is built
// with protowire instead of a struct.
const (
	MethodSetLoggerLevel uint32 = 1
	MethodGetLoggerLevel uint32 = 2
)

// LoggerServiceKey addresses the per process logger service.
var LoggerServiceKey = rpc.ServiceKey{ProtoName: "LoggerRpc", ServiceName: "LoggerRpc"}

// LoggerLevels lists the levels in order, lowest first.
var LoggerLevels = []string{"disabled", "error", "warning", "info", "debug", "trace"}

// LoggerLevelName renders a level number. Unknown values pass through so the UI
// shows what the device claims rather than hiding it.
func LoggerLevelName(level int) string {
	if level >= 0 && level < len(LoggerLevels) {
		return LoggerLevels[level]
	}
	return fmt.Sprintf("level-%d", level)
}

// LoggerLevelValue turns a name into its enum number.
func LoggerLevelValue(name string) (int, bool) {
	for value, candidate := range LoggerLevels {
		if candidate == name {
			return value, true
		}
	}
	return 0, false
}

// GetLoggerLevel reads the level the device currently runs with.
func (s *Service) GetLoggerLevel(ctx context.Context, userID int64, machineID string) (int, error) {
	body, err := s.Caller.CallRaw(ctx, userID, machineID, LoggerServiceKey, MethodGetLoggerLevel, nil)
	if err != nil {
		return 0, err
	}
	if len(body) == 0 {
		return 0, nil
	}
	number, _, n := protowire.ConsumeTag(body)
	if n < 0 {
		return 0, fmt.Errorf("legacyview: decode logger level tag: %w", protowire.ParseError(n))
	}
	if protowire.Number(number) != 1 {
		return 0, nil
	}
	value, m := protowire.ConsumeVarint(body[n:])
	if m < 0 {
		return 0, fmt.Errorf("legacyview: decode logger level: %w", protowire.ParseError(m))
	}
	return int(value), nil
}

// SetLoggerLevel writes the level the device should run with.
func (s *Service) SetLoggerLevel(ctx context.Context, userID int64, machineID string, level int) error {
	payload := protowire.AppendTag(nil, 1, protowire.VarintType)
	payload = protowire.AppendVarint(payload, uint64(level))
	_, err := s.Caller.CallRaw(ctx, userID, machineID, LoggerServiceKey, MethodSetLoggerLevel, payload)
	return err
}
