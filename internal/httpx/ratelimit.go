package httpx

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type visitor struct {
	tokens   float64
	lastSeen time.Time
	lastFill time.Time
}

type ipRateLimiter struct {
	mu         sync.Mutex
	visitors   map[string]*visitor
	rps        float64
	burst      float64
	trustProxy bool
	lastSweep  time.Time
}

func newIPRateLimiter(rps float64, burst int, trustProxy bool) *ipRateLimiter {
	if rps <= 0 {
		rps = 5
	}
	if burst <= 0 {
		burst = 20
	}
	now := time.Now().UTC()
	return &ipRateLimiter{
		visitors:   make(map[string]*visitor),
		rps:        rps,
		burst:      float64(burst),
		trustProxy: trustProxy,
		lastSweep:  now,
	}
}

func (l *ipRateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if l.allow(r) {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Retry-After", "1")
		http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
	})
}

func (l *ipRateLimiter) allow(r *http.Request) bool {
	key := l.rateLimitKey(r)
	now := time.Now().UTC()

	l.mu.Lock()
	defer l.mu.Unlock()

	if now.Sub(l.lastSweep) >= 10*time.Minute {
		for k, v := range l.visitors {
			if now.Sub(v.lastSeen) > 30*time.Minute {
				delete(l.visitors, k)
			}
		}
		l.lastSweep = now
	}

	v, ok := l.visitors[key]
	if !ok {
		l.visitors[key] = &visitor{tokens: l.burst - 1, lastSeen: now, lastFill: now}
		return true
	}

	elapsed := now.Sub(v.lastFill).Seconds()
	if elapsed > 0 {
		v.tokens += elapsed * l.rps
		if v.tokens > l.burst {
			v.tokens = l.burst
		}
		v.lastFill = now
	}
	v.lastSeen = now

	if v.tokens < 1 {
		return false
	}
	v.tokens -= 1
	return true
}

func (l *ipRateLimiter) rateLimitKey(r *http.Request) string {
	if sub := authSubjectFromRequest(r); sub != "" {
		return "user:" + strings.ToLower(sub)
	}
	return "ip:" + l.clientIP(r)
}

func (l *ipRateLimiter) clientIP(r *http.Request) string {
	if l.trustProxy {
		xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For"))
		if xff != "" {
			parts := strings.Split(xff, ",")
			if len(parts) > 0 {
				if ip := strings.TrimSpace(parts[0]); ip != "" {
					return ip
				}
			}
		}
	}

	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil && host != "" {
		return host
	}
	if r.RemoteAddr != "" {
		return r.RemoteAddr
	}
	return "unknown"
}
