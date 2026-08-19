package api

import "sync"

const (
	// maxGlobalSSEConnections bounds total concurrent /api/v1/stream
	// connections. Each connection holds a long-lived handler goroutine
	// and a broadcaster subscriber channel; without a cap, a client (or
	// a scripted browser tab) opening many connections could exhaust
	// server goroutines/memory since rate limiting only bounds request
	// start rate, not active stream count.
	maxGlobalSSEConnections = 100

	// maxSSEConnectionsPerIP additionally bounds connections from a
	// single client so one misbehaving IP cannot consume the entire
	// global budget.
	maxSSEConnectionsPerIP = 20
)

// sseConnLimiter caps concurrent SSE connections globally and per-IP.
type sseConnLimiter struct {
	mu    sync.Mutex
	total int
	perIP map[string]int
}

func newSSEConnLimiter() *sseConnLimiter {
	return &sseConnLimiter{perIP: make(map[string]int)}
}

// acquire reserves a connection slot for ip, returning false if the
// global or per-IP limit is already reached. On true, the caller must
// call release(ip) exactly once when the connection ends.
func (l *sseConnLimiter) acquire(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.total >= maxGlobalSSEConnections || l.perIP[ip] >= maxSSEConnectionsPerIP {
		return false
	}
	l.total++
	l.perIP[ip]++
	return true
}

func (l *sseConnLimiter) release(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.total--
	l.perIP[ip]--
	if l.perIP[ip] <= 0 {
		delete(l.perIP, ip)
	}
}
