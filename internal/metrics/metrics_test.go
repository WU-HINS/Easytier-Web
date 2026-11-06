package metrics

import (
	"strings"
	"testing"
)

func TestRecordAccumulates(t *testing.T) {
	c := &Counters{}
	c.Record(Sample{Pushed: 2, Skipped: 1})
	c.Record(Sample{Pushed: 1, Failed: 1, CompileErrors: 1, Stopped: 1})
	if c.Cycles.Load() != 2 {
		t.Fatalf("cycles = %d, want 2", c.Cycles.Load())
	}
	if c.Pushed.Load() != 3 || c.Failed.Load() != 1 {
		t.Fatalf("pushed=%d failed=%d", c.Pushed.Load(), c.Failed.Load())
	}
	if c.CompileErrors.Load() != 1 || c.Stopped.Load() != 1 || c.Skipped.Load() != 1 {
		t.Fatalf("compile=%d stopped=%d skipped=%d", c.CompileErrors.Load(), c.Stopped.Load(), c.Skipped.Load())
	}
}

// The ratio is what an operator alerts on, so the two zero cases matter: no
// attempts at all must read as zero rather than NaN, and no failures must read
// as zero rather than 1.
func TestPushFailureRatio(t *testing.T) {
	c := &Counters{}
	if got := c.PushFailureRatio(); got != 0 {
		t.Fatalf("empty ratio = %v, want 0", got)
	}
	c.Record(Sample{Pushed: 9})
	if got := c.PushFailureRatio(); got != 0 {
		t.Fatalf("clean ratio = %v, want 0", got)
	}
	c.Record(Sample{Failed: 1})
	if got := c.PushFailureRatio(); got < 0.09 || got > 0.11 {
		t.Fatalf("ratio = %v, want about 0.1", got)
	}
}

func TestRenderCarriesEveryCounter(t *testing.T) {
	c := &Counters{}
	c.Record(Sample{Pushed: 1})
	out := c.Render()
	for _, want := range []string{
		"easytier_reconcile_cycles_total 1",
		"easytier_reconcile_pushed_total 1",
		"easytier_push_failure_ratio 0.000000",
		"# TYPE easytier_reconcile_pushed_total counter",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("render missing %q", want)
		}
	}
}
