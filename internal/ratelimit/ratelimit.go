// Package ratelimit provides a per-IP sliding-window rate limiter for HTTP
// handlers. The real client IP is extracted from the X-Forwarded-For header
// (set by reverse proxies such as Caddy or Nginx) so the limiter works
// correctly behind a proxy.
package ratelimit

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// entry tracks request timestamps for a single IP.
type entry struct {
	times []time.Time
}

// Limiter is a per-IP sliding-window rate limiter.
type Limiter struct {
	mu       sync.Mutex
	entries  map[string]*entry
	limit    int
	window   time.Duration
	cleanup  time.Duration
	stopChan chan struct{}
}

// New creates a Limiter that allows limit requests per window duration.
// A background goroutine purges stale entries every 5 minutes.
func New(limit int, window time.Duration) *Limiter {
	if window <= 0 {
		window = 1 * time.Minute
	}
	cleanup := 5 * time.Minute
	l := &Limiter{
		entries:  make(map[string]*entry),
		limit:    limit,
		window:   window,
		cleanup:  cleanup,
		stopChan: make(chan struct{}),
	}
	go l.loop()
	return l
}

// Stop terminates the background cleanup goroutine.
func (l *Limiter) Stop() {
	close(l.stopChan)
}

// RealIP extracts the real client IP from the request, checking
// X-Forwarded-For first (set by reverse proxies), then falling back to
// RemoteAddr. This ensures rate limiting works correctly behind Caddy,
// Nginx, or any proxy that sets X-Forwarded-For.
func RealIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		parts := strings.SplitN(fwd, ",", 2)
		ip := strings.TrimSpace(parts[0])
		if ip != "" {
			return ip
		}
	}
	if real := r.Header.Get("X-Real-IP"); real != "" {
		return real
	}
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

// Allow returns true if the request is within the rate limit for the real
// client IP extracted from the request.
func (l *Limiter) Allow(r *http.Request) bool {
	ip := RealIP(r)

	now := time.Now()
	windowStart := now.Add(-l.window)

	l.mu.Lock()
	defer l.mu.Unlock()

	e, ok := l.entries[ip]
	if !ok {
		e = &entry{}
		l.entries[ip] = e
	}

	valid := e.times[:0]
	for _, t := range e.times {
		if t.After(windowStart) {
			valid = append(valid, t)
		}
	}
	e.times = valid

	if len(e.times) >= l.limit {
		return false
	}

	e.times = append(e.times, now)
	return true
}

// Middleware returns an HTTP middleware that rejects requests with 429 when
// the rate limit is exceeded. A Retry-After header is included so clients
// know when they can retry.
func (l *Limiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !l.Allow(r) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", fmt.Sprintf("%.0f", l.window.Seconds()))
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"too many requests"}`))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Handler returns an http.Handler that wraps the provided handler with rate
// limiting.
func (l *Limiter) Handler(next http.Handler) http.Handler {
	return l.Middleware(next)
}

func (l *Limiter) loop() {
	ticker := time.NewTicker(l.cleanup)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			l.purge()
		case <-l.stopChan:
			return
		}
	}
}

func (l *Limiter) purge() {
	l.mu.Lock()
	defer l.mu.Unlock()

	cutoff := time.Now().Add(-2 * l.window)
	for ip, e := range l.entries {
		if len(e.times) == 0 || e.times[len(e.times)-1].Before(cutoff) {
			delete(l.entries, ip)
		}
	}
}
