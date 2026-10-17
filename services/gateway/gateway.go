// The request path: match a route, check the token, check the role, rate-limit, forward.
package main

import (
	"errors"
	"log/slog"
	"net/http"
)

type gateway struct {
	routes   []route
	verifier *verifier
	limiter  *rateLimiter
}

// handler builds the whole gateway as one http.Handler. The liveness and readiness probes are
// served here rather than forwarded: they report on the gateway itself (ADR-005 rule 4).
func (g *gateway) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", g.handleHealth)
	mux.HandleFunc("GET /readyz", g.handleReady)
	mux.HandleFunc("/", g.forward)
	return mux
}

// handleHealth answers the liveness probe: the process is up and serving HTTP.
func (g *gateway) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleReady answers the readiness probe. The gateway is ready when it can verify a token;
// without a key every protected route answers 401, which is a gateway that is running but not
// doing its job, and sending traffic to it would hide that behind a wall of 401s.
func (g *gateway) handleReady(w http.ResponseWriter, r *http.Request) {
	if len(g.verifier.keys) == 0 {
		writeError(w, http.StatusServiceUnavailable, "no verification key configured", nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

// forward is every request that is not a probe.
func (g *gateway) forward(w http.ResponseWriter, r *http.Request) {
	matched, found := match(g.routes, r.URL.Path)
	if !found {
		// Nothing is forwarded by default. A path with no route is not the gateway's to guess
		// at, and a gateway that forwards the unknown is a gateway that exposes the next
		// service someone adds.
		writeError(w, http.StatusNotFound, "no route for "+r.URL.Path, nil)
		return
	}

	// A caller can set any header they like, so an identity header arriving from outside is a
	// forgery attempt by definition. Drop them before anything downstream can read one.
	for name := range r.Header {
		if _, forged := forwardedIdentityHeaders[http.CanonicalHeaderKey(name)]; forged {
			r.Header.Del(name)
		}
	}

	who := caller{}
	if matched.needs != public {
		verified, err := g.verifier.verify(r)
		if err != nil {
			g.rejected(r, matched, "", "unauthenticated", err)
			if errors.Is(err, errNoToken) {
				w.Header().Set("WWW-Authenticate", `Bearer realm="HireAssist"`)
				writeError(w, http.StatusUnauthorized, "this route needs an access token", nil)
				return
			}
			writeError(w, http.StatusUnauthorized, "access token rejected", nil)
			return
		}
		who = verified

		if !matched.needs.allows(who.role) {
			g.rejected(r, matched, who.memberID, "role", nil)
			writeError(w, http.StatusForbidden,
				"this route needs the "+matched.needs.String()+" role", nil)
			return
		}
	}

	// Rate-limited per caller once we know who they are, and per client address before that,
	// so that one noisy member cannot spend everybody's budget and an unauthenticated flood
	// cannot spend one member's.
	if !g.limiter.allow(limitKey(r, who)) {
		writeError(w, http.StatusTooManyRequests, "too many requests; slow down", nil)
		return
	}

	proxyTo(matched.upstream).ServeHTTP(w, r)
}

// forwardedIdentityHeaders are the headers the gateway refuses to pass through from a client.
// No service accepts a role from an unsigned header (ADR-009); these are stripped so that
// nothing downstream can be fooled by one even if it tried to.
var forwardedIdentityHeaders = map[string]struct{}{
	"X-Hireassist-Member-Id": {},
	"X-Hireassist-Role":      {},
}

// rejected records a request the gateway refused.
//
// FR-0.4 requires the rejected attempt to be recorded, and ADR-001 puts that in the access
// audit log the Compliance & Insights Service owns, through recordAuthorisationRejected(). That
// service does not exist yet, so this logs. The call belongs here when it does — the rejection
// is not visible anywhere else, because the request never reaches a service.
func (g *gateway) rejected(r *http.Request, matched route, memberID, why string, cause error) {
	attrs := []any{
		"event", "authorisation_rejected",
		"reason", why,
		"method", r.Method,
		"path", r.URL.Path,
		"needs", matched.needs.String(),
	}
	if memberID != "" {
		attrs = append(attrs, "member_id", memberID)
	}
	if cause != nil {
		attrs = append(attrs, "error", cause.Error())
	}
	slog.Warn("authorisation rejected", attrs...)
}
