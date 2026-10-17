package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestTheLongestMatchingPrefixWins(t *testing.T) {
	identity, _ := url.Parse("http://identity")
	hiring, _ := url.Parse("http://hiring")
	routes := []route{
		{prefix: "/job-openings", upstream: hiring, needs: member},
		{prefix: "/job-openings/archive", upstream: identity, needs: admin},
	}

	got, found := match(routes, "/job-openings/archive/7")
	if !found {
		t.Fatal("no route matched")
	}
	if got.prefix != "/job-openings/archive" {
		t.Errorf("matched %q, want the more specific route", got.prefix)
	}
}

func TestAPrefixMatchesOnlyAtAPathBoundary(t *testing.T) {
	hiring, _ := url.Parse("http://hiring")
	routes := []route{{prefix: "/job-openings", upstream: hiring, needs: member}}

	if _, found := match(routes, "/job-openings-archive"); found {
		t.Error("/job-openings-archive matched the /job-openings route")
	}
	if _, found := match(routes, "/job-openings/7"); !found {
		t.Error("/job-openings/7 did not match the /job-openings route")
	}
	if _, found := match(routes, "/job-openings"); !found {
		t.Error("the prefix itself did not match")
	}
}

func TestAccessAllows(t *testing.T) {
	for _, c := range []struct {
		needs access
		role  string
		want  bool
	}{
		{public, "", true},
		{member, roleRecruiter, true},
		{member, roleAdmin, true}, // FR-0.3: Admin holds every Recruiter permission
		{admin, roleAdmin, true},
		{admin, roleRecruiter, false},
		{member, "Auditor", false},
	} {
		if got := c.needs.allows(c.role); got != c.want {
			t.Errorf("%s.allows(%q) = %v, want %v", c.needs, c.role, got, c.want)
		}
	}
}

func TestOneKeyNeedsNoKeyID(t *testing.T) {
	// The common case: a single key, handed over as a bare PEM block.
	s := newSigner(t, "")
	keys, err := parsePublicKeys(s.publicKeyPEM(t))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("got %d keys, want 1", len(keys))
	}
}

func TestDuringRotationATokenMustNameItsKey(t *testing.T) {
	// Two keys configured and a token that names neither: accepting it would mean a token
	// signed by the retired key verifying against the new one by luck.
	first, second := newSigner(t, "old"), newSigner(t, "new")
	keys, err := parsePublicKeys(`[` +
		`{"kid":"old","pem":` + quote(first.publicKeyPEM(t)) + `},` +
		`{"kid":"new","pem":` + quote(second.publicKeyPEM(t)) + `}]`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	v := &verifier{keys: keys}

	unnamed := newSigner(t, "")
	unnamed.key = first.key // signed by a configured key, but the token carries no kid
	if _, err := v.verify(request(t, unnamed.token(t, "m-1", roleAdmin, time.Minute))); err == nil {
		t.Error("a token with no key id was accepted while two keys were configured")
	}

	// Both named keys still verify, which is what makes a rotation possible at all.
	for _, s := range []*signer{first, second} {
		if _, err := v.verify(request(t, s.token(t, "m-1", roleAdmin, time.Minute))); err != nil {
			t.Errorf("kid %q: %v", s.kid, err)
		}
	}
}

func TestANonECDSAKeyIsRejectedAtStartup(t *testing.T) {
	// ES256 is an ECDSA signature. An RSA key here means the signer and the configuration
	// disagree, and the gateway should say so before it serves a request.
	const rsaPEM = `-----BEGIN PUBLIC KEY-----
MFwwDQYJKoZIhvcNAQEBBQADSwAwSAJBAKj34GkxFhD90vcNLYLInFEX6Ppy1tPf
9Cnzj4p4WGeKLs1Pt8QuKUpRKfFLfRYC9AIKjbJTWit+CqvjWYzvQwECAwEAAQ==
-----END PUBLIC KEY-----`
	if _, err := parsePublicKeys(rsaPEM); err == nil {
		t.Fatal("an RSA key was accepted for ES256")
	}
}

func TestRateLimitingIsPerCaller(t *testing.T) {
	l := newRateLimiter(1, 2) // two at once, then one a second

	if !l.allow("member:a") || !l.allow("member:a") {
		t.Fatal("the burst was not allowed")
	}
	if l.allow("member:a") {
		t.Error("a third request was allowed inside the same second")
	}
	if !l.allow("member:b") {
		t.Error("one caller's burst spent another caller's budget")
	}
}

// quote renders s as a JSON string so a PEM block can be embedded in a config literal.
func quote(s string) string {
	encoded, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

// request builds a request carrying token, for the verifier tests.
func request(t *testing.T, token string) *http.Request {
	t.Helper()
	r := httptest.NewRequest("GET", "/job-openings", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	return r
}
