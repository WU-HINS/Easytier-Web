package httpapi

import (
	"net/http"
	"strings"
)

// securityHeaders applies a strict default policy. The client ships a single
// external boot script (no inline script), so script-src can stay 'self'.
// style-src needs 'unsafe-inline' because React sets inline style attributes.
func securityHeaders(secureTransport bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := w.Header()
		header.Set("Content-Security-Policy",
			"default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; "+
				"img-src 'self' data: https:; connect-src 'self'; font-src 'self' data:; "+
				"frame-ancestors 'none'; base-uri 'self'; form-action 'self'; object-src 'none'")
		header.Set("X-Frame-Options", "DENY")
		header.Set("X-Content-Type-Options", "nosniff")
		header.Set("Referrer-Policy", "no-referrer")
		header.Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
		header.Set("Cross-Origin-Opener-Policy", "same-origin")
		// HSTS only where TLS is actually in play: a LAN deployment served over
		// plain http must keep working, and browsers ignore the header there anyway.
		if r.TLS != nil || secureTransport {
			header.Set("Strict-Transport-Security", "max-age=15552000")
		}
		// Authenticated JSON must never be stored by a browser or a proxy: it is
		// tenant data keyed by a cookie, and a shared cache has no way to tell two
		// sessions apart.
		if strings.HasPrefix(r.URL.Path, "/api/") {
			header.Set("Cache-Control", "no-store")
			header.Set("Pragma", "no-cache")
		}
		next.ServeHTTP(w, r)
	})
}
