package httpapi_test

import (
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
)

// TestConcurrentSettingsWritesKeepOneWinner covers the parallel half of F-018: the
// sequential test passes even if the store compared revisions in Go and then wrote,
// because nothing races. Here six writers hold the same revision, so only an atomic
// conditional update lets exactly one of them through.
func TestConcurrentSettingsWritesKeepOneWinner(t *testing.T) {
	h := newHarness(t)
	admin := h.client(t)
	h.setupAdmin(t, admin, "root-owner", strongPassword)

	const writers = 6
	var created, conflicted, other int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for index := 0; index < writers; index++ {
		wg.Add(1)
		go func(slot int) {
			defer wg.Done()
			<-start
			status, body := h.do(t, admin, http.MethodPut, "/api/v1/admin/settings", map[string]any{
				"max_instances_per_user": 10 + slot,
				"expected_revision":      0,
			})
			if status != http.StatusOK && status != http.StatusConflict {
				t.Logf("writer %d got %d %s", slot, status, body)
			}
			switch status {
			case http.StatusOK:
				atomic.AddInt32(&created, 1)
			case http.StatusConflict:
				atomic.AddInt32(&conflicted, 1)
			default:
				atomic.AddInt32(&other, 1)
			}
		}(index)
	}
	close(start)
	wg.Wait()

	if other != 0 {
		t.Fatalf("%d writer(s) got an unexpected status", other)
	}
	if created != 1 {
		t.Fatalf("%d writers won the same revision, want exactly 1", created)
	}
	if conflicted != writers-1 {
		t.Fatalf("conflicts = %d, want %d", conflicted, writers-1)
	}

	status, body := h.do(t, admin, http.MethodGet, "/api/v1/admin/settings", nil)
	if status != http.StatusOK {
		t.Fatalf("read settings = %d %s", status, body)
	}
	var final struct {
		Revision int "json:\"revision\""
	}
	if err := decodeInto(body, &final); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if final.Revision != 1 {
		t.Fatalf("revision = %d after one winning write, want 1", final.Revision)
	}
}
