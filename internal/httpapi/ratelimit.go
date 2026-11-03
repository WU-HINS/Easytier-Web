package httpapi

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// loginLimiter throttles failed credential attempts per client address and
// username. It is deliberately in memory: a console is a single process, and a
// restart clearing the window is acceptable for brute force defence.
type loginLimiter struct {
	mu       sync.Mutex
	attempts map[string]*attemptWindow
	limit    int
	window   time.Duration
	now      func() time.Time
	// trustProxy decides whether X-Forwarded-For may override the peer address.
	// Trusting it unconditionally lets an attacker rotate the header and never
	// hit the throttle.
	trustProxy func(*http.Request) bool
	lastSweep  time.Time
}

type attemptWindow struct {
	count   int
	resetAt time.Time
}

func newLoginLimiter(limit int, window time.Duration, trustProxy func(*http.Request) bool) *loginLimiter {
	return &loginLimiter{
		attempts:   map[string]*attemptWindow{},
		limit:      limit,
		window:     window,
		now:        time.Now,
		trustProxy: trustProxy,
	}
}

func clientAddress(r *http.Request, trustProxy bool) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if !trustProxy {
		return host
	}
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		return strings.TrimSpace(strings.Split(forwarded, ",")[0])
	}
	return host
}

const authIPLimit = 30

// keys returns the buckets an attempt counts against: the specific account and
// the client address. The address bucket stops a caller from rotating usernames
// to bypass the per-account limit (each attempt costs an argon2 computation).
func (l *loginLimiter) keys(r *http.Request, username string) []string {
	trust := false
	if l.trustProxy != nil {
		trust = l.trustProxy(r)
	}
	address := clientAddress(r, trust)
	return []string{
		address + "|" + strings.ToLower(strings.TrimSpace(username)),
		address + "|*",
	}
}

func (l *loginLimiter) limitFor(key string) int {
	if strings.HasSuffix(key, "|*") {
		return authIPLimit
	}
	return l.limit
}

// allow reports whether an attempt may proceed, and how long to wait when not.
func (l *loginLimiter) allow(r *http.Request, username string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	// Expired windows are dropped before every decision, otherwise distinct
	// (address, username) pairs accumulate for ever: sweep used to be dead code.
	if now.Sub(l.lastSweep) >= l.window {
		l.lastSweep = now
		l.sweepLocked(now)
	}
	wait := time.Duration(0)
	for _, key := range l.keys(r, username) {
		entry, ok := l.attempts[key]
		if !ok || now.After(entry.resetAt) {
			l.attempts[key] = &attemptWindow{count: 0, resetAt: now.Add(l.window)}
			continue
		}
		if entry.count >= l.limitFor(key) {
			remaining := entry.resetAt.Sub(now)
			if remaining > wait {
				wait = remaining
			}
		}
	}
	return wait == 0, wait
}

// fail records a failed attempt; success clears the window.
func (l *loginLimiter) fail(r *http.Request, username string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, key := range l.keys(r, username) {
		entry, ok := l.attempts[key]
		if !ok {
			entry = &attemptWindow{resetAt: l.now().Add(l.window)}
			l.attempts[key] = entry
		}
		entry.count++
	}
}

func (l *loginLimiter) reset(r *http.Request, username string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, key := range l.keys(r, username) {
		delete(l.attempts, key)
	}
}

// sweep drops expired windows so the map cannot grow without bound.
func (l *loginLimiter) sweep() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sweepLocked(l.now())
}

// sweepLocked drops expired windows; callers hold the mutex.
func (l *loginLimiter) sweepLocked(now time.Time) {
	for key, entry := range l.attempts {
		if now.After(entry.resetAt) {
			delete(l.attempts, key)
		}
	}
}
