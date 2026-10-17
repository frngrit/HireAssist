package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// signer stands in for the Identity Service: it is the only thing in these tests that holds a
// private key, which is the arrangement ADR-009 describes.
type signer struct {
	kid string
	key *ecdsa.PrivateKey
}

func newSigner(t *testing.T, kid string) *signer {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return &signer{kid: kid, key: key}
}

// publicKeyPEM is what the Identity Service would hand the gateway as configuration.
func (s *signer) publicKeyPEM(t *testing.T) string {
	t.Helper()
	der, err := x509.MarshalPKIXPublicKey(&s.key.PublicKey)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}

func (s *signer) config(t *testing.T) string {
	t.Helper()
	entries := []publicKey{{Kid: s.kid, PEM: s.publicKeyPEM(t)}}
	encoded, err := json.Marshal(entries)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	return string(encoded)
}

// token signs an access token of the shape ADR-009 describes.
func (s *signer) token(t *testing.T, memberID, role string, expires time.Duration) string {
	t.Helper()
	claims := jwt.MapClaims{
		"sub":  memberID,
		"role": role,
		"iat":  time.Now().Unix(),
		"exp":  time.Now().Add(expires).Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	if s.kid != "" {
		tok.Header["kid"] = s.kid
	}
	signed, err := tok.SignedString(s.key)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return signed
}

// upstream is a stand-in service that records what reached it.
type upstream struct {
	server *httptest.Server
	seen   *http.Request
}

func newUpstream(t *testing.T, body string) *upstream {
	t.Helper()
	u := &upstream{}
	u.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u.seen = r.Clone(r.Context())
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(body))
	}))
	t.Cleanup(u.server.Close)
	return u
}

func (u *upstream) url(t *testing.T) *url.URL {
	t.Helper()
	parsed, err := url.Parse(u.server.URL)
	if err != nil {
		t.Fatalf("parse upstream url: %v", err)
	}
	return parsed
}

// testGateway wires a gateway in front of two stand-in services, with generous rate limits so
// that only the tests about rate limiting hit one.
func testGateway(t *testing.T, keys string, identity, hiring *upstream) http.Handler {
	t.Helper()
	parsed, err := parsePublicKeys(keys)
	if err != nil {
		t.Fatalf("parse keys: %v", err)
	}
	g := &gateway{
		routes:   routeTable(identity.url(t), hiring.url(t)),
		verifier: &verifier{keys: parsed},
		limiter:  newRateLimiter(1000, 1000),
	}
	return g.handler()
}

func do(t *testing.T, h http.Handler, method, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, nil)
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestAValidTokenReachesTheServiceThatOwnsTheRoute(t *testing.T) {
	s := newSigner(t, "2026-10")
	identity, hiring := newUpstream(t, `{"service":"identity"}`), newUpstream(t, `{"service":"hiring"}`)
	h := testGateway(t, s.config(t), identity, hiring)

	w := do(t, h, "GET", "/job-openings", s.token(t, "m-1", roleRecruiter, time.Minute))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body)
	}
	if hiring.seen == nil {
		t.Fatal("the request never reached the Hiring Service")
	}
	if identity.seen != nil {
		t.Error("the request reached the Identity Service as well")
	}
}

func TestTheTokenIsForwardedUnchanged(t *testing.T) {
	// ADR-009: a service that acts on the caller's role verifies the same token the gateway
	// did, so the gateway must not consume or rewrite it.
	s := newSigner(t, "2026-10")
	identity, hiring := newUpstream(t, "{}"), newUpstream(t, "{}")
	h := testGateway(t, s.config(t), identity, hiring)
	token := s.token(t, "m-1", roleRecruiter, time.Minute)

	do(t, h, "GET", "/job-openings", token)

	if got := hiring.seen.Header.Get("Authorization"); got != "Bearer "+token {
		t.Errorf("Authorization reached the service as %q", got)
	}
}

func TestAMissingTokenIsRejectedBeforeTheServiceIsCalled(t *testing.T) {
	s := newSigner(t, "2026-10")
	identity, hiring := newUpstream(t, "{}"), newUpstream(t, "{}")
	h := testGateway(t, s.config(t), identity, hiring)

	w := do(t, h, "GET", "/job-openings", "")

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	if hiring.seen != nil {
		t.Error("an unauthenticated request reached the Hiring Service")
	}
	if got := w.Header().Get("WWW-Authenticate"); got == "" {
		t.Error("a 401 should say how to authenticate")
	}
}

func TestAnExpiredTokenIsRejected(t *testing.T) {
	s := newSigner(t, "2026-10")
	identity, hiring := newUpstream(t, "{}"), newUpstream(t, "{}")
	h := testGateway(t, s.config(t), identity, hiring)

	w := do(t, h, "GET", "/job-openings", s.token(t, "m-1", roleRecruiter, -time.Minute))

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	if hiring.seen != nil {
		t.Error("an expired token reached the Hiring Service")
	}
}

func TestATokenSignedByAnotherKeyIsRejected(t *testing.T) {
	ours, theirs := newSigner(t, "2026-10"), newSigner(t, "2026-10")
	identity, hiring := newUpstream(t, "{}"), newUpstream(t, "{}")
	h := testGateway(t, ours.config(t), identity, hiring)

	// Same key id, different key: the id is a label, the signature is the check.
	w := do(t, h, "GET", "/job-openings", theirs.token(t, "m-1", roleRecruiter, time.Minute))

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestAnUnsignedTokenIsRejected(t *testing.T) {
	// The alg=none attack: a token that names its own algorithm must not be allowed to pick
	// one the gateway will accept without a signature.
	s := newSigner(t, "2026-10")
	identity, hiring := newUpstream(t, "{}"), newUpstream(t, "{}")
	h := testGateway(t, s.config(t), identity, hiring)

	claims := jwt.MapClaims{"sub": "m-1", "role": roleAdmin, "exp": time.Now().Add(time.Hour).Unix()}
	unsigned, err := jwt.NewWithClaims(jwt.SigningMethodNone, claims).
		SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("build unsigned token: %v", err)
	}

	if w := do(t, h, "GET", "/job-openings", unsigned); w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestARecruiterCannotReachAnAdminRoute(t *testing.T) {
	s := newSigner(t, "2026-10")
	identity, hiring := newUpstream(t, "{}"), newUpstream(t, "{}")
	h := testGateway(t, s.config(t), identity, hiring)

	w := do(t, h, "GET", "/members", s.token(t, "m-1", roleRecruiter, time.Minute))

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
	if identity.seen != nil {
		t.Error("a Recruiter's request reached the Identity Service")
	}
}

func TestAnAdminReachesBothKindsOfRoute(t *testing.T) {
	// FR-0.3: Admin holds every Recruiter permission in addition to its own.
	s := newSigner(t, "2026-10")
	identity, hiring := newUpstream(t, "{}"), newUpstream(t, "{}")
	h := testGateway(t, s.config(t), identity, hiring)
	token := s.token(t, "m-1", roleAdmin, time.Minute)

	if w := do(t, h, "GET", "/members", token); w.Code != http.StatusOK {
		t.Errorf("admin route: status = %d, want 200", w.Code)
	}
	if w := do(t, h, "GET", "/job-openings", token); w.Code != http.StatusOK {
		t.Errorf("member route: status = %d, want 200", w.Code)
	}
}

func TestSignInNeedsNoToken(t *testing.T) {
	// A caller cannot hold a token before signing in, so the route that issues one is public.
	s := newSigner(t, "2026-10")
	identity, hiring := newUpstream(t, "{}"), newUpstream(t, "{}")
	h := testGateway(t, s.config(t), identity, hiring)

	if w := do(t, h, "POST", "/auth/sign-in", ""); w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if identity.seen == nil {
		t.Error("sign-in never reached the Identity Service")
	}
}

func TestAnUnknownPathIsNotForwarded(t *testing.T) {
	s := newSigner(t, "2026-10")
	identity, hiring := newUpstream(t, "{}"), newUpstream(t, "{}")
	h := testGateway(t, s.config(t), identity, hiring)

	w := do(t, h, "GET", "/candidates", s.token(t, "m-1", roleAdmin, time.Minute))

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
	if identity.seen != nil || hiring.seen != nil {
		t.Error("an unrouted path was forwarded to a service")
	}
}

func TestAForgedIdentityHeaderIsStripped(t *testing.T) {
	// ADR-009: no service accepts a role from an unsigned header.
	s := newSigner(t, "2026-10")
	identity, hiring := newUpstream(t, "{}"), newUpstream(t, "{}")
	h := testGateway(t, s.config(t), identity, hiring)

	r := httptest.NewRequest("GET", "/job-openings", nil)
	r.Header.Set("Authorization", "Bearer "+s.token(t, "m-1", roleRecruiter, time.Minute))
	r.Header.Set("X-HireAssist-Role", roleAdmin)
	r.Header.Set("X-HireAssist-Member-Id", "somebody-else")
	h.ServeHTTP(httptest.NewRecorder(), r)

	if got := hiring.seen.Header.Get("X-HireAssist-Role"); got != "" {
		t.Errorf("a forged role header reached the service as %q", got)
	}
	if got := hiring.seen.Header.Get("X-HireAssist-Member-Id"); got != "" {
		t.Errorf("a forged member id header reached the service as %q", got)
	}
}

func TestAnUnreachableServiceIsReportedAsBadGateway(t *testing.T) {
	s := newSigner(t, "2026-10")
	identity, hiring := newUpstream(t, "{}"), newUpstream(t, "{}")
	hiring.server.Close() // the service is down, not missing

	h := testGateway(t, s.config(t), identity, hiring)
	w := do(t, h, "GET", "/job-openings", s.token(t, "m-1", roleRecruiter, time.Minute))

	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", w.Code)
	}
}

func TestReadinessFailsWithoutAVerificationKey(t *testing.T) {
	identity, hiring := newUpstream(t, "{}"), newUpstream(t, "{}")
	h := testGateway(t, "", identity, hiring)

	if w := do(t, h, "GET", "/healthz", ""); w.Code != http.StatusOK {
		t.Errorf("liveness: status = %d, want 200", w.Code)
	}
	if w := do(t, h, "GET", "/readyz", ""); w.Code != http.StatusServiceUnavailable {
		t.Errorf("readiness: status = %d, want 503", w.Code)
	}
}
