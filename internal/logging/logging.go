// Package logging renders the console log lines in the HiDNS shape:
//
//	2026-11-01T12:00:00.000Z  INFO [HTTP] [Auth.Login] [login] [L42] ["SUCCESS"] user signed in
//
// The module name is bound once per package, sub-modules are chained, and the
// caller function plus line are captured automatically so nobody has to pass
// them by hand.
package logging

import (
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Level is the log severity.
type Level int

const (
	LevelTrace Level = iota
	LevelDebug
	LevelInfo
	LevelWarn
	LevelError
	LevelDisabled
)

func parseLevel(value string) Level {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "trace":
		return LevelTrace
	case "debug":
		return LevelDebug
	case "warn", "warning":
		return LevelWarn
	case "error":
		return LevelError
	case "off", "disabled", "none":
		return LevelDisabled
	default:
		return LevelInfo
	}
}

func (l Level) String() string {
	switch l {
	case LevelTrace:
		return "TRACE"
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return " WARN"
	case LevelError:
		return "ERROR"
	default:
		return "     "
	}
}

// Logger is a bound logger instance.
type Logger struct {
	module string
	sub    string
	tags   []string
	min    Level
	out    io.Writer

	mu *sync.Mutex
}

var (
	globalMu    sync.Mutex
	globalLevel           = LevelInfo
	globalOut   io.Writer = os.Stderr
)

// Configure sets the process wide level and sink (called once at startup).
func Configure(level string, out io.Writer) {
	globalMu.Lock()
	defer globalMu.Unlock()
	globalLevel = parseLevel(level)
	if out != nil {
		globalOut = out
	}
}

func currentLevel() Level {
	globalMu.Lock()
	defer globalMu.Unlock()
	return globalLevel
}

// New binds a logger to a top level module name (HTTP, DB, BAL, DSM, OIDC, ...).
func New(module string) *Logger {
	return &Logger{module: module, mu: &sync.Mutex{}}
}

// Sub returns a child logger; chained calls render as [a.b.c].
func (l *Logger) Sub(name string) *Logger {
	sub := name
	if l.sub != "" {
		sub = l.sub + "." + name
	}
	return &Logger{module: l.module, sub: sub, tags: append([]string(nil), l.tags...), mu: l.mu}
}

// Tag adds a quoted label rendered before the message.
func (l *Logger) Tag(labels ...string) *Logger {
	return &Logger{module: l.module, sub: l.sub, tags: append(append([]string(nil), l.tags...), labels...), mu: l.mu}
}

func (l *Logger) write(level Level, message string, data ...any) {
	if level < currentLevel() {
		return
	}
	function, line := caller()
	var builder strings.Builder
	builder.WriteString(time.Now().UTC().Format("2006-01-02T15:04:05.000Z"))
	builder.WriteString(" ")
	builder.WriteString(level.String())
	builder.WriteString(" [")
	builder.WriteString(l.module)
	builder.WriteString("]")
	if l.sub != "" {
		builder.WriteString(" [")
		builder.WriteString(l.sub)
		builder.WriteString("]")
	}
	builder.WriteString(" [")
	builder.WriteString(function)
	builder.WriteString("] [L")
	builder.WriteString(fmt.Sprintf("%d", line))
	builder.WriteString("]")
	for _, tag := range l.tags {
		builder.WriteString(" [\"")
		builder.WriteString(tag)
		builder.WriteString("\"]")
	}
	builder.WriteString(" ")
	builder.WriteString(message)
	if len(data) > 0 {
		builder.WriteString(" ")
		builder.WriteString(fmt.Sprintf("%v", data))
	}
	builder.WriteString("\n")

	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = globalOut.Write([]byte(builder.String()))
}

// loggingPackage is the import path prefix of the frames this package owns.
const loggingPackage = "github.com/easytier/easytier-console/internal/logging."

func caller() (string, int) {
	// runtime.CallersFrames expands inlined frames, so a one line Info() call
	// still reports the function that actually logged. Counting frames by hand
	// broke as soon as the compiler inlined the wrapper methods.
	pcs := make([]uintptr, 16)
	count := runtime.Callers(2, pcs)
	frames := runtime.CallersFrames(pcs[:count])
	for {
		frame, more := frames.Next()
		if !strings.HasPrefix(frame.Function, loggingPackage) {
			name := frame.Function
			if index := strings.LastIndex(name, "."); index >= 0 {
				name = name[index+1:]
			}
			return name, frame.Line
		}
		if !more {
			break
		}
	}
	return "?", 0
}

func (l *Logger) Trace(message string, data ...any) { l.write(LevelTrace, message, data...) }
func (l *Logger) Debug(message string, data ...any) { l.write(LevelDebug, message, data...) }
func (l *Logger) Info(message string, data ...any)  { l.write(LevelInfo, message, data...) }
func (l *Logger) Warn(message string, data ...any)  { l.write(LevelWarn, message, data...) }
func (l *Logger) Error(message string, data ...any) { l.write(LevelError, message, data...) }
