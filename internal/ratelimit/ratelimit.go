package ratelimit

import (
	"net"
	"net/http"
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
// A background goroutine purges stale entries every cleanup interval
// (default: 5 minutes).
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

// Allow returns true if the request is within the rate limit.
func (l *Limiter) Allow(r *http.Request) bool {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}

	now := time.Now()
	windowStart := now.Add(-l.window)

	l.mu.Lock()
	defer l.mu.Unlock()

	e, ok := l.entries[ip]
	if !ok {
		e = &entry{}
		l.entries[ip] = e
	}

	// Prune timestamps outside the window.
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
// the rate limit is exceeded.
func (l *Limiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !l.Allow(r) {
			http.Error(w, `{"error":"too many requests"}`, http.StatusTooManyRequests)
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
