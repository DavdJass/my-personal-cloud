package ratelimit

import (
	"net/http"
	"testing"
	"time"
)

func TestAllow(t *testing.T) {
	l := New(3, 1*time.Minute)
	defer l.Stop()

	req, _ := http.NewRequest("GET", "/", nil)

	// First 3 requests should be allowed.
	for i := 0; i < 3; i++ {
		if !l.Allow(req) {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}

	// 4th request should be blocked.
	if l.Allow(req) {
		t.Fatal("4th request should be blocked")
	}
}

func TestAllowDifferentIPs(t *testing.T) {
	l := New(2, 1*time.Minute)
	defer l.Stop()

	req1, _ := http.NewRequest("GET", "/", nil)
	req1.RemoteAddr = "192.168.1.1:1234"
	req2, _ := http.NewRequest("GET", "/", nil)
	req2.RemoteAddr = "192.168.1.2:5678"

	// Each IP should get 2 requests.
	if !l.Allow(req1) { t.Fatal("req1 #1 should be allowed") }
	if !l.Allow(req1) { t.Fatal("req1 #2 should be allowed") }
	if l.Allow(req1) { t.Fatal("req1 #3 should be blocked") }

	if !l.Allow(req2) { t.Fatal("req2 #1 should be allowed") }
	if !l.Allow(req2) { t.Fatal("req2 #2 should be allowed") }
	if l.Allow(req2) { t.Fatal("req2 #3 should be blocked") }
}

func TestWindowExpiry(t *testing.T) {
	l := New(1, 50*time.Millisecond)
	defer l.Stop()

	req, _ := http.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:9999"

	if !l.Allow(req) { t.Fatal("first request should be allowed") }
	if l.Allow(req) { t.Fatal("second request should be blocked") }

	// Wait for window to expire.
	time.Sleep(60 * time.Millisecond)

	if !l.Allow(req) { t.Fatal("request after window expiry should be allowed") }
}
