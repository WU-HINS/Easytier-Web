package legacyview

import (
	"context"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/easytier/easytier-console/internal/rpc"
)

// loggerCaller records what the service sends so the field numbers can be
// checked against api_logger.proto without a device.
type loggerCaller struct {
	key    rpc.ServiceKey
	method uint32
	raw    []byte
	reply  []byte
}

func (c *loggerCaller) Call(context.Context, int64, string, rpc.ServiceKey, uint32, proto.Message) ([]byte, error) {
	return nil, nil
}

func (c *loggerCaller) CallRaw(_ context.Context, _ int64, _ string, key rpc.ServiceKey, method uint32, raw []byte) ([]byte, error) {
	c.key = key
	c.method = method
	c.raw = append([]byte(nil), raw...)
	return c.reply, nil
}

func TestSetLoggerLevelEncodesFieldOne(t *testing.T) {
	caller := &loggerCaller{}
	service := New(caller)
	if err := service.SetLoggerLevel(context.Background(), 1, "machine", 4); err != nil {
		t.Fatalf("SetLoggerLevel: %v", err)
	}
	if caller.key.ServiceName != "LoggerRpc" {
		t.Fatalf("service name = %q", caller.key.ServiceName)
	}
	if caller.method != MethodSetLoggerLevel {
		t.Fatalf("method = %d, want %d", caller.method, MethodSetLoggerLevel)
	}
	// field 1, varint, value 4 (debug)
	want := []byte{0x08, 0x04}
	if len(caller.raw) != len(want) || caller.raw[0] != want[0] || caller.raw[1] != want[1] {
		t.Fatalf("payload = %v, want %v", caller.raw, want)
	}
}

func TestGetLoggerLevelDecodesFieldOne(t *testing.T) {
	caller := &loggerCaller{reply: []byte{0x08, 0x02}}
	service := New(caller)
	level, err := service.GetLoggerLevel(context.Background(), 1, "machine")
	if err != nil {
		t.Fatalf("GetLoggerLevel: %v", err)
	}
	if level != 2 {
		t.Fatalf("level = %d, want 2", level)
	}
	if caller.method != MethodGetLoggerLevel {
		t.Fatalf("method = %d, want %d", caller.method, MethodGetLoggerLevel)
	}
}

func TestLoggerLevelNamesRoundTrip(t *testing.T) {
	for value, name := range LoggerLevels {
		got, ok := LoggerLevelValue(name)
		if !ok || got != value {
			t.Fatalf("LoggerLevelValue(%q) = %d, %v", name, got, ok)
		}
		if LoggerLevelName(value) != name {
			t.Fatalf("LoggerLevelName(%d) = %q", value, LoggerLevelName(value))
		}
	}
	if _, ok := LoggerLevelValue("verbose"); ok {
		t.Fatal("unknown level accepted")
	}
}
