package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// identityStub answers sign-in and refresh like the Identity Service, and records the body of
// the last request that reached it.
type identityStub struct {
	*upstream
	lastBody string
	status   int
}

func newIdentityStub(t *testing.T) *identityStub {
	t.Helper()
	stub := &identityStub{upstream: &upstream{}, status: http.StatusOK}
	stub.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		stub.lastBody = string(raw)
		stub.seen = r.Clone(r.Context())
		if stub.status != http.StatusOK {
			w.WriteHeader(stub.status)
			w.Write([]byte(`{"error":"refresh token is not valid"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"access_token":"at","token_type":"Bearer","expires_in":900,` +
			`"refresh_token":"rt-next","session_expires_at":"2026-10-17T18:00:00Z","member":{"id":"m1"}}`))
	}))
	t.Cleanup(stub.server.Close)
	return stub
}

func cookieGateway(t *testing.T, identity *identityStub) http.Handler {
	t.Helper()
	g := &gateway{
		routes:   routeTable(identity.url(t), newUpstream(t, "{}").url(t)),
		verifier: &verifier{},
		limiter:  newRateLimiter(1000, 1000),
		cookies:  sessionCookies{secure: true},
	}
	return g.handler()
}

func post(h http.Handler, path, body string, cookieMode bool, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if cookieMode {
		r.Header.Set(refreshTokenTransportHeader, cookieTransport)
	}
	for _, c := range cookies {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func refreshCookie(t *testing.T, w *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range w.Result().Cookies() {
		if c.Name == refreshCookieName {
			return c
		}
	}
	return nil
}

func TestABrowserSignInGetsTheRefreshTokenAsAnHttpOnlyCookie(t *testing.T) {
	identity := newIdentityStub(t)
	w := post(cookieGateway(t, identity), "/auth/sign-in", `{"email":"a","password":"b"}`, true)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if _, present := body["refresh_token"]; present {
		t.Error("the refresh token is still in the body, where a script can read it")
	}
	if body["access_token"] != "at" {
		t.Errorf("access_token = %v, want the Identity Service's", body["access_token"])
	}

	c := refreshCookie(t, w)
	if c == nil {
		t.Fatal("no refresh cookie was set")
	}
	if c.Value != "rt-next" || !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteStrictMode || c.Path != "/auth" {
		t.Errorf("cookie = %+v, want rt-next, HttpOnly, Secure, SameSite=Strict, Path=/auth", c)
	}
	if want := time.Date(2026, 10, 17, 18, 0, 0, 0, time.UTC); !c.Expires.Equal(want) {
		t.Errorf("cookie expires %v, want the session's cap %v", c.Expires, want)
	}
}

func TestAClientThatDoesNotAskKeepsTheTokenInTheBody(t *testing.T) {
	// Postman, curl and the demo scripts rely on the Identity Service's answer as it is.
	identity := newIdentityStub(t)
	w := post(cookieGateway(t, identity), "/auth/sign-in", `{}`, false)

	if !strings.Contains(w.Body.String(), `"refresh_token":"rt-next"`) {
		t.Errorf("body = %s, want the refresh token left in it", w.Body.String())
	}
	if refreshCookie(t, w) != nil {
		t.Error("a cookie was set for a client that did not ask for one")
	}
}

func TestARefreshPresentsTheCookiesTokenToTheIdentityService(t *testing.T) {
	identity := newIdentityStub(t)
	w := post(cookieGateway(t, identity), "/auth/refresh", "", true,
		&http.Cookie{Name: refreshCookieName, Value: "rt-current"})

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if identity.lastBody != `{"refresh_token":"rt-current"}` {
		t.Errorf("the Identity Service received %q, want the cookie's token", identity.lastBody)
	}
	if identity.seen.Header.Get("Cookie") != "" {
		t.Error("the cookie was forwarded to the Identity Service")
	}
	if c := refreshCookie(t, w); c == nil || c.Value != "rt-next" {
		t.Errorf("cookie = %+v, want it rotated to rt-next", c)
	}
}

func TestARefreshWithNoCookieIsAnsweredWithoutAskingTheIdentityService(t *testing.T) {
	identity := newIdentityStub(t)
	w := post(cookieGateway(t, identity), "/auth/refresh", "", true)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	if identity.seen != nil {
		t.Error("the Identity Service was asked to refresh nothing")
	}
}

func TestARefusedRefreshClearsTheCookie(t *testing.T) {
	identity := newIdentityStub(t)
	identity.status = http.StatusUnauthorized
	w := post(cookieGateway(t, identity), "/auth/refresh", "", true,
		&http.Cookie{Name: refreshCookieName, Value: "rt-used"})

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	if c := refreshCookie(t, w); c == nil || c.MaxAge >= 0 {
		t.Errorf("cookie = %+v, want it deleted", c)
	}
}

func TestSignOutClearsTheCookie(t *testing.T) {
	w := post(cookieGateway(t, newIdentityStub(t)), "/auth/sign-out", "", false)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", w.Code)
	}
	if c := refreshCookie(t, w); c == nil || c.MaxAge >= 0 || !c.HttpOnly {
		t.Errorf("cookie = %+v, want it deleted", c)
	}
}
