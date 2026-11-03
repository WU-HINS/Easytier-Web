package logging_test

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/easytier/easytier-console/internal/logging"
)

func capture(t *testing.T, level string) *bytes.Buffer {
	t.Helper()
	buffer := &bytes.Buffer{}
	logging.Configure(level, buffer)
	t.Cleanup(func() { logging.Configure("info", os.Stderr) })
	return buffer
}

// TestFormatMatchesTheDocumentedShape pins the log line. The format is an
// interface: operators parse it, so it must not drift silently.
func TestFormatMatchesTheDocumentedShape(t *testing.T) {
	buffer := capture(t, "trace")
	logging.New("HTTP").Sub("Setup").Tag("SUCCESS").Info("console initialized")

	line := strings.TrimSuffix(buffer.String(), "\n")
	fields := strings.SplitN(line, " ", 7)
	if len(fields) != 7 {
		t.Fatalf("the line has %d fields, want 7: %s", len(fields), line)
	}
	if _, err := time.Parse("2006-01-02T15:04:05.000Z", fields[0]); err != nil {
		t.Fatalf("timestamp is not the documented shape: %q (%v)", fields[0], err)
	}
	want := []string{"INFO", "[HTTP]", "[Setup]", "[TestFormatMatchesTheDocumentedShape]"}
	for index, expected := range want {
		if fields[index+1] != expected {
			t.Fatalf("field %d = %q, want %q (line: %s)", index+1, fields[index+1], expected, line)
		}
	}
	if !strings.HasPrefix(fields[5], "[L") || !strings.HasSuffix(fields[5], "]") {
		t.Fatalf("the caller line is missing: %s", fields[5])
	}
	if !strings.HasPrefix(fields[6], "[\"SUCCESS\"] ") {
		t.Fatalf("the tag is missing: %q", fields[6])
	}
	if !strings.HasSuffix(fields[6], "console initialized") {
		t.Fatalf("message = %q", fields[6])
	}
}

func TestNestedSubModulesAndLevels(t *testing.T) {
	buffer := capture(t, "warn")
	logger := logging.New("CENTRAL").Sub("Reconcile").Sub("Plan")
	logger.Info("suppressed")
	logger.Warn("kept")
	logging.New("CENTRAL").Error("also kept")

	lines := strings.Split(strings.TrimSuffix(buffer.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("level filtering is wrong, got %d lines: %v", len(lines), lines)
	}
	if !strings.Contains(lines[0], "[CENTRAL] [Reconcile.Plan]") {
		t.Fatalf("nested sub modules render wrong: %s", lines[0])
	}
	if !strings.Contains(lines[0], " WARN ") || !strings.Contains(lines[1], " ERROR ") {
		t.Fatalf("level names render wrong: %v", lines)
	}
}

func TestExtraDataIsAppended(t *testing.T) {
	buffer := capture(t, "info")
	logging.New("DB").Tag("MIGRATE").Info("applied", 3, "changes")
	if !strings.Contains(buffer.String(), "[\"MIGRATE\"] applied [3 changes]") {
		t.Fatalf("extra data not rendered: %s", buffer.String())
	}
}

// TestDebugLevelActuallyShowsDebugLines covers F-196: EASYTIER_LOG_LEVEL=debug only
// means something if debug lines exist and survive the filter. Before this round
// no call site used Debug at all, so the flag changed nothing.
func TestDebugLevelActuallyShowsDebugLines(t *testing.T) {
	buffer := capture(t, "debug")
	logger := logging.New("TEST")
	logger.Debug("debug line")
	logger.Info("info line")
	text := buffer.String()
	if !strings.Contains(text, "debug line") {
		t.Fatalf("debug level swallowed a debug line: %s", text)
	}
	if !strings.Contains(text, "info line") {
		t.Fatalf("debug level swallowed an info line: %s", text)
	}

	quiet := capture(t, "info")
	logging.New("TEST").Debug("hidden line")
	if strings.Contains(quiet.String(), "hidden line") {
		t.Fatal("info level let a debug line through")
	}
}
