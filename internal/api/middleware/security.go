// SPDX-License-Identifier: AGPL-3.0-or-later

package middleware

import "net/http"

// SecurityHeaders sets a baseline set of hardening response headers
// appropriate for the controller — both for HTML/SPA responses and
// API responses. Defensive defaults; tighter per-route policies can
// override individual headers later.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()

		// Block MIME-sniffing — browsers must honor the Content-Type
		// the server returns.
		h.Set("X-Content-Type-Options", "nosniff")

		// Don't leak the referring URL to third-party origins.
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")

		// Deny framing — the controller is never embedded in another
		// site; this blocks clickjacking attempts wholesale.
		h.Set("X-Frame-Options", "DENY")

		// HTTPS-only browsers for one year, including subdomains.
		// Safe to send over plain HTTP — browsers ignore it.
		h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")

		// Restrict resource origins to self. 'unsafe-inline' on styles
		// is required by Tailwind's runtime, which injects rules through
		// a <style> tag in JIT mode; we accept that tradeoff for now.
		// A nonce-based CSP is tracked for v1.0.
		h.Set("Content-Security-Policy",
			"default-src 'self'; "+
				"script-src 'self'; "+
				"style-src 'self' 'unsafe-inline'; "+
				"img-src 'self' data:; "+
				"font-src 'self' https://fonts.gstatic.com; "+
				"connect-src 'self'; "+
				"frame-ancestors 'none'; "+
				"base-uri 'self'; "+
				"form-action 'self'")

		next.ServeHTTP(w, r)
	})
}
