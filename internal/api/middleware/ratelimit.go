// SPDX-License-Identifier: AGPL-3.0-or-later

package middleware

import (
	"net"
	"net/http"
	"sync"

	"golang.org/x/time/rate"
)

// LoginRateLimiter is a per-IP token-bucket limiter intended to slow
// password-guessing attacks on /v1/auth/login.
//
// Phase scope: in-memory state. Adequate for a single-controller
// deployment. The visitors map grows unbounded for now; a Redis-
// backed replacement lands when Redis becomes a hard dependency.
type LoginRateLimiter struct {
	mu       sync.Mutex
	visitors map[string]*rate.Limiter
	rateLim  rate.Limit
	burst    int
}

// NewLoginRateLimiter returns a middleware allowing burst tokens per
// IP, refilling at perSecond tokens / second.
//
// Default knobs from 0.5: perSecond = 1.0/180.0 (one token every 3
// minutes), burst = 5 (i.e. five attempts then a slow refill).
func NewLoginRateLimiter(perSecond float64, burst int) func(http.Handler) http.Handler {
	l := &LoginRateLimiter{
		visitors: make(map[string]*rate.Limiter),
		rateLim:  rate.Limit(perSecond),
		burst:    burst,
	}
	return l.middleware
}

func (l *LoginRateLimiter) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := extractIP(r)

		l.mu.Lock()
		lim, ok := l.visitors[ip]
		if !ok {
			lim = rate.NewLimiter(l.rateLim, l.burst)
			l.visitors[ip] = lim
		}
		l.mu.Unlock()

		if !lim.Allow() {
			writeJSONStatus(w, http.StatusTooManyRequests, `{"error":"rate_limited"}`)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func extractIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
