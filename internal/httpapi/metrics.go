package httpapi

import (
	"bufio"
	"net"
	"net/http"
	"os"
	"strings"

	"github.com/easytier/easytier-console/internal/metrics"
)

// handleMetrics serves the counters in the Prometheus text format.
//
// The payload is aggregates only - no tenant, network or device identifiers - so a
// scrape cannot learn topology. When EASYTIER_METRICS_TOKEN is set the endpoint
// demands it as a bearer token, which is what you want if the console is reachable
// from anywhere but the scraper.
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if token := strings.TrimSpace(os.Getenv("EASYTIER_METRICS_TOKEN")); token != "" {
		presented := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if presented != token {
			writeError(w, http.StatusUnauthorized, "a metrics token is required")
			return
		}
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(metrics.Registry.Render()))
}

// countingWriter remembers the status code so the middleware can tell a 200 from
// a 502 without inspecting the body.
type countingWriter struct {
	http.ResponseWriter
	status int
}

func (w *countingWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// Hijack lets a websocket upgrade through: the device channel needs the raw
// connection, and a wrapper that hides http.Hijacker turns every upgrade into
// "websocket: bad handshake".
func (w *countingWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	return hijacker.Hijack()
}

// Flush keeps streaming responses working through the wrapper.
func (w *countingWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// countRequests wraps the mux so the failure ratio has a denominator.
func (s *Server) countRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorder := &countingWriter{ResponseWriter: w, status: http.StatusOK}
		metrics.Registry.HTTPRequests.Add(1)
		next.ServeHTTP(recorder, r)
		if recorder.status >= 400 {
			metrics.Registry.HTTPFailures.Add(1)
		}
	})
}
