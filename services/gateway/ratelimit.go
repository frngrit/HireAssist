// Rate limiting, which ADR-001 gives the gateway along with routing and the session check.
package main

import (
	"net"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// rateLimiter is one token bucket per caller. Buckets are kept in memory, which is the right
// scale for one deployment per customer company (ADR-006) but means two gateway replicas would
// each allow the full rate. Moving the counter to a shared store is a decision for when there
// is a second replica, not before.
type rateLimiter struct {
	perSecond rate.Limit
	burst     int

	mu      sync.Mutex
	buckets map[string]*bucket
}

type bucket struct {
	limiter *rate.Limiter
	lastUse time.Time
}

// idleBucketTTL is how long a caller's bucket outlives their last request. Without this the map
// grows for the lifetime of the process, one entry per address that ever connected.
const idleBucketTTL = 10 * time.Minute

func newRateLimiter(perSecond float64, burst int) *rateLimiter {
	return &rateLimiter{
		perSecond: rate.Limit(perSecond),
		burst:     burst,
		buckets:   map[string]*bucket{},
	}
}

// allow takes one token for key, reporting whether there was one to take.
func (l *rateLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	b, known := l.buckets[key]
	if !known {
		// lastUse is set before the sweep: a bucket created at the zero time looks idle, and
		// the sweep would delete the one we just made.
		b = &bucket{limiter: rate.NewLimiter(l.perSecond, l.burst), lastUse: now}
		l.evictIdle(now)
		l.buckets[key] = b
	}
	b.lastUse = now
	return b.limiter.Allow()
}

// evictIdle drops buckets nobody has used recently. It runs when a new one is created, which is
// the only moment the map can grow, so there is no sweeper goroutine to start or stop.
func (l *rateLimiter) evictIdle(now time.Time) {
	for key, b := range l.buckets {
		if now.Sub(b.lastUse) > idleBucketTTL {
			delete(l.buckets, key)
		}
	}
}

// limitKey is the member once the token has been verified, and the client address before that.
func limitKey(r *http.Request, who caller) string {
	if who.memberID != "" {
		return "member:" + who.memberID
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return "addr:" + host
}
