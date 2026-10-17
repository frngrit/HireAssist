// The refresh token as an HttpOnly cookie, for the web application.
//
// ADR-009 asks the web application to keep the refresh token where scripts cannot read it. The
// Identity Service returns it in the response body and leaves where it is kept to the web
// application and the gateway; a script cannot set an HttpOnly cookie, so the gateway does.
//
// It is opt-in per request: a client that sends refreshTokenTransportHeader gets the cookie
// and a body without the token, and every other client — Postman, curl, another service — gets
// the Identity Service's answer untouched. The cookie is scoped to /auth, the only place a
// refresh token is ever presented, and SameSite=Strict, so a page on another site cannot make
// the browser spend it.
package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"
)

const (
	// refreshTokenTransportHeader is how a browser client asks for the cookie.
	refreshTokenTransportHeader = "X-Refresh-Token-Transport"
	cookieTransport             = "cookie"

	refreshCookieName = "hireassist_refresh"
	refreshCookiePath = "/auth"

	// maxAuthBodyBytes bounds what the gateway reads of a sign-in or refresh exchange. Both are
	// a few hundred bytes; anything larger is not one of them.
	maxAuthBodyBytes = 64 << 10
)

// sessionCookies holds the one setting that differs between a deployment and a laptop.
type sessionCookies struct {
	// secure marks the cookie Secure. A deployment serves HTTPS and must leave it on; a
	// browser on plain-HTTP localhost may refuse a Secure cookie, which is the only reason to
	// turn it off.
	secure bool
}

// wantsCookie reports whether this exchange should carry the refresh token in a cookie.
func wantsCookie(r *http.Request) bool {
	if r.Header.Get(refreshTokenTransportHeader) != cookieTransport {
		return false
	}
	return r.URL.Path == "/auth/sign-in" || r.URL.Path == "/auth/refresh"
}

// presentCookie puts the cookie's refresh token into a refresh request that does not carry one
// in its body. It returns false, having answered, when there is no token anywhere: there is no
// session to refresh, and asking the Identity Service would only answer the same.
func (s sessionCookies) presentCookie(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/auth/refresh" {
		return true
	}
	var body struct {
		RefreshToken string `json:"refresh_token"`
	}
	raw, _ := io.ReadAll(io.LimitReader(r.Body, maxAuthBodyBytes))
	r.Body.Close()
	_ = json.Unmarshal(raw, &body)

	if body.RefreshToken == "" {
		cookie, err := r.Cookie(refreshCookieName)
		if err != nil || cookie.Value == "" {
			writeError(w, http.StatusUnauthorized, "no session to refresh; sign in", nil)
			return false
		}
		raw, _ = json.Marshal(map[string]string{"refresh_token": cookie.Value})
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	r.ContentLength = int64(len(raw))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Del("Cookie") // the Identity Service has no use for it
	return true
}

// moveTokenToCookie rewrites the Identity Service's answer: a successful one loses its
// refresh_token field and sets the cookie instead, expiring with the session; a refused refresh
// clears the cookie, because the token in it is now worthless.
func (s sessionCookies) moveTokenToCookie(res *http.Response) error {
	if res.StatusCode == http.StatusUnauthorized && res.Request.URL.Path == "/auth/refresh" {
		res.Header.Add("Set-Cookie", s.cookie("", time.Unix(0, 0)).String())
		return nil
	}
	if res.StatusCode != http.StatusOK {
		return nil
	}

	raw, err := io.ReadAll(io.LimitReader(res.Body, maxAuthBodyBytes))
	res.Body.Close()
	if err != nil {
		return err
	}
	var body map[string]json.RawMessage
	var token string
	var endsAt time.Time
	if json.Unmarshal(raw, &body) == nil &&
		json.Unmarshal(body["refresh_token"], &token) == nil && token != "" {
		_ = json.Unmarshal(body["session_expires_at"], &endsAt)
		delete(body, "refresh_token")
		if rewritten, err := json.Marshal(body); err == nil {
			raw = rewritten
			res.Header.Add("Set-Cookie", s.cookie(token, endsAt).String())
		}
	}
	res.Body = io.NopCloser(bytes.NewReader(raw))
	res.ContentLength = int64(len(raw))
	res.Header.Set("Content-Length", strconv.Itoa(len(raw)))
	return nil
}

// handleSignOut forgets the browser's session: the cookie is cleared. The Identity Service has
// no sign-out, so the refresh token is not revoked — it stays valid until the session's cap,
// but no browser holds it any more.
func (s sessionCookies) handleSignOut(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, s.cookie("", time.Unix(0, 0)))
	w.WriteHeader(http.StatusNoContent)
}

// cookie builds the refresh cookie. An expiry in the past deletes it.
func (s sessionCookies) cookie(value string, expires time.Time) *http.Cookie {
	c := &http.Cookie{
		Name:     refreshCookieName,
		Value:    value,
		Path:     refreshCookiePath,
		HttpOnly: true,
		Secure:   s.secure,
		SameSite: http.SameSiteStrictMode,
	}
	if value == "" {
		c.MaxAge = -1
	} else if !expires.IsZero() {
		c.Expires = expires
	}
	return c
}
