// The route table: every path the gateway forwards, where it goes, and the access it needs.
package main

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

// access is what a caller must present to reach a route.
type access int

const (
	// public needs no token. Only sign-in and refresh are public: a caller cannot have a token
	// before signing in, and a caller whose access token has expired must be able to renew it.
	public access = iota
	// member needs a valid access token, of either role.
	member
	// admin needs a valid access token carrying the Admin role.
	admin
)

func (a access) String() string {
	switch a {
	case public:
		return "public"
	case member:
		return "member"
	default:
		return "admin"
	}
}

// route is one path prefix the gateway forwards.
type route struct {
	prefix   string   // matched against the start of the request path
	upstream *url.URL // the service that owns it
	needs    access
}

// routeTable is every route the gateway serves, in one place so that what is exposed and what
// each exposure costs can be read at a glance. The longest matching prefix wins, so a more
// specific route may sit under a broader one.
//
// Paths are forwarded unchanged: the prefix a caller uses is the path the owning service
// serves. Nothing is rewritten, so a route here can be read against that service's OpenAPI
// document without translating it.
func routeTable(identity, hiring *url.URL) []route {
	return []route{
		// UC-0 (ADR-009): sign-in returns an access token and a refresh token, and a refresh
		// token is exchanged once. Changing a password is public for the same reason refresh
		// is — a member holding a temporary password cannot sign in to get a token, and FR-0.5
		// says they must replace it first. The Identity Service asks for the current password.
		{prefix: "/auth/sign-in", upstream: identity, needs: public},
		{prefix: "/auth/refresh", upstream: identity, needs: public},
		{prefix: "/auth/change-password", upstream: identity, needs: public},

		// UC-6. Admin-only here as well as in the Identity Service, which re-reads the caller's
		// role from its own database before acting (ADR-009) — the gateway's check is the cheap
		// one that keeps most rejected requests off the service entirely.
		{prefix: "/members", upstream: identity, needs: admin},

		// UC-1, UC-2, UC-3. Every hiring operation is open to either role.
		{prefix: "/job-openings", upstream: hiring, needs: member},
	}
}

// match returns the route serving path, and whether one exists. The longest matching prefix
// wins, so adding a narrower route later cannot be shadowed by a broader one already present.
func match(routes []route, path string) (route, bool) {
	best, found := route{}, false
	for _, r := range routes {
		if !strings.HasPrefix(path, r.prefix) {
			continue
		}
		// "/job-openings-archive" must not match the "/job-openings" prefix: a prefix matches
		// only at a path boundary.
		rest := path[len(r.prefix):]
		if rest != "" && !strings.HasPrefix(rest, "/") {
			continue
		}
		if !found || len(r.prefix) > len(best.prefix) {
			best, found = r, true
		}
	}
	return best, found
}

// proxyTo forwards the request to one upstream, unchanged apart from the hop-by-hop headers
// Go's reverse proxy manages and the forwarding headers it adds.
func proxyTo(upstream *url.URL) *httputil.ReverseProxy {
	return &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(upstream)
			pr.Out.Host = pr.In.Host
			pr.SetXForwarded()
			// The access token travels on untouched: a service that acts on the caller's role
			// verifies the same signature the gateway did (ADR-009).
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			// An upstream that is down is the gateway's problem to report, not a 502 with a
			// Go error in the body.
			writeError(w, http.StatusBadGateway, "the service handling this request is unavailable", err)
		},
	}
}
