package session_test

import (
	"testing"
	"time"

	"github.com/easytier/easytier-console/internal/session"
)

// TestHeartbeatPolicyBounds pins the startup rules for the two heartbeat flags.
// They existed only as three if statements, and the interesting cases are the
// boundaries: an off by one there either rejects a sane configuration or lets a
// device be disconnected for being one second late.
func TestHeartbeatPolicyBounds(t *testing.T) {
	cases := []struct {
		name     string
		interval time.Duration
		timeout  time.Duration
		ok       bool
	}{
		{"default", 3500 * time.Millisecond, 15 * time.Second, true},
		{"fastest interval", time.Second, 6 * time.Second, true},
		{"slowest interval", time.Minute, 65 * time.Second, true},
		{"interval too short", 500 * time.Millisecond, 10 * time.Second, false},
		{"interval too long", 61 * time.Second, 66 * time.Second, false},
		{"timeout too short", time.Second, 4 * time.Second, false},
		{"timeout too long", 10 * time.Second, 121 * time.Second, false},
		{"timeout equals interval plus five", 10 * time.Second, 15 * time.Second, true},
		{"timeout below interval plus five", 10 * time.Second, 14 * time.Second, false},
		{"timeout shorter than interval", 20 * time.Second, 10 * time.Second, false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			policy := session.Policy{Interval: testCase.interval, Timeout: testCase.timeout}
			err := policy.Validate()
			if testCase.ok && err != nil {
				t.Fatalf("Validate() = %v, want it accepted", err)
			}
			if !testCase.ok && err == nil {
				t.Fatalf("Validate() accepted interval %v timeout %v", testCase.interval, testCase.timeout)
			}
		})
	}
}

// TestNewManagerRejectsAnInvalidPolicy keeps the check wired to construction: a
// bad pair of flags must fail at startup, not at the first heartbeat.
func TestNewManagerRejectsAnInvalidPolicy(t *testing.T) {
	if _, err := session.NewManager(nil, nil, session.Policy{
		Interval: 200 * time.Millisecond, Timeout: 10 * time.Second,
	}); err == nil {
		t.Fatal("NewManager accepted an out of range interval")
	}
}
