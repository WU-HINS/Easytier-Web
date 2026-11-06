// Package metrics holds the few counters an operator needs to answer "is the
// control plane actually pushing configurations, and how often does it fail".
//
// The numbers are aggregates only: no tenant, network or device identifiers ever
// reach the endpoint, so exposing it cannot leak topology.
package metrics

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
)

// Counters is the set the reconcile loop maintains.
type Counters struct {
	Cycles        atomic.Uint64
	Pushed        atomic.Uint64
	Skipped       atomic.Uint64
	Failed        atomic.Uint64
	CompileErrors atomic.Uint64
	Stopped       atomic.Uint64
	HTTPRequests  atomic.Uint64
	HTTPFailures  atomic.Uint64
}

// Registry is the process wide set of counters.
var Registry = &Counters{}

// Sample is one reconcile cycle outcome, as the loop already logs it.
type Sample struct {
	Pushed        int
	Skipped       int
	Failed        int
	CompileErrors int
	Stopped       int
}

// Record adds one cycle to the counters.
func (c *Counters) Record(s Sample) {
	c.Cycles.Add(1)
	if s.Pushed > 0 {
		c.Pushed.Add(uint64(s.Pushed))
	}
	if s.Skipped > 0 {
		c.Skipped.Add(uint64(s.Skipped))
	}
	if s.Failed > 0 {
		c.Failed.Add(uint64(s.Failed))
	}
	if s.CompileErrors > 0 {
		c.CompileErrors.Add(uint64(s.CompileErrors))
	}
	if s.Stopped > 0 {
		c.Stopped.Add(uint64(s.Stopped))
	}
}

// PushFailureRatio is failures over attempts. A ratio is what an operator alerts
// on; the raw counters are there to explain it.
func (c *Counters) PushFailureRatio() float64 {
	pushed := c.Pushed.Load()
	failed := c.Failed.Load()
	total := pushed + failed
	if total == 0 {
		return 0
	}
	return float64(failed) / float64(total)
}

var help = map[string]string{
	"easytier_reconcile_cycles_total":         "Reconcile passes completed.",
	"easytier_reconcile_pushed_total":         "Configurations pushed to devices.",
	"easytier_reconcile_skipped_total":        "Members skipped (quota or disabled).",
	"easytier_reconcile_failed_total":         "Pushes the device refused.",
	"easytier_reconcile_compile_errors_total": "Networks that could not be compiled.",
	"easytier_reconcile_stopped_total":        "Instances stopped because the member was removed.",
	"easytier_http_requests_total":            "HTTP requests served.",
	"easytier_http_failures_total":            "HTTP requests answered with 4xx or 5xx.",
}

// Render writes the Prometheus text format. The order is stable so the output is
// diffable, which matters when a human reads it during an incident.
func (c *Counters) Render() string {
	values := map[string]uint64{
		"easytier_reconcile_cycles_total":         c.Cycles.Load(),
		"easytier_reconcile_pushed_total":         c.Pushed.Load(),
		"easytier_reconcile_skipped_total":        c.Skipped.Load(),
		"easytier_reconcile_failed_total":         c.Failed.Load(),
		"easytier_reconcile_compile_errors_total": c.CompileErrors.Load(),
		"easytier_reconcile_stopped_total":        c.Stopped.Load(),
		"easytier_http_requests_total":            c.HTTPRequests.Load(),
		"easytier_http_failures_total":            c.HTTPFailures.Load(),
	}
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)

	var out strings.Builder
	for _, name := range names {
		fmt.Fprintln(&out, "# HELP "+name+" "+help[name])
		fmt.Fprintln(&out, "# TYPE "+name+" counter")
		fmt.Fprintln(&out, name+" "+strconv.FormatUint(values[name], 10))
	}
	fmt.Fprintln(&out, "# HELP easytier_push_failure_ratio Failures over attempts.")
	fmt.Fprintln(&out, "# TYPE easytier_push_failure_ratio gauge")
	fmt.Fprintln(&out, "easytier_push_failure_ratio "+strconv.FormatFloat(c.PushFailureRatio(), 'f', 6, 64))
	return out.String()
}

// Reset is used by tests; production code never clears counters.
func (c *Counters) Reset() {
	c.Cycles.Store(0)
	c.Pushed.Store(0)
	c.Skipped.Store(0)
	c.Failed.Store(0)
	c.CompileErrors.Store(0)
	c.Stopped.Store(0)
	c.HTTPRequests.Store(0)
	c.HTTPFailures.Store(0)
}
