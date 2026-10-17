// auth.go — signing in, refreshing, and replacing a temporary password (UC-0).
//
// The shape is ADR-009's: a fifteen-minute signed access token that every verifier checks
// itself, and a refresh token that lives here, where it can be deleted.
package main

import (
	"errors"
	"net/http"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// Session lifetimes. Both are configuration in ADR-009's sense — the decision is the shape, not
// the numbers — and both are read from the environment at startup.
const (
	defaultAccessTokenTTL  = 15 * time.Minute
	defaultSessionLifetime = 8 * time.Hour
)

// AuthAPI holds what the sign-in handlers need.
type AuthAPI struct {
	store           *Store
	signer          *signer
	sessionLifetime time.Duration
	clock           func() time.Time
}

// tokenResponse is what sign-in and refresh both return.
//
// The refresh token is in the body, not a cookie: who holds it and where is a decision for the
// web application and the gateway (ADR-009), and a cookie here would make it for them.
type tokenResponse struct {
	AccessToken      string    `json:"access_token"`
	TokenType        string    `json:"token_type"` // always "Bearer"
	ExpiresIn        int       `json:"expires_in"` // seconds until the access token expires
	RefreshToken     string    `json:"refresh_token"`
	SessionExpiresAt time.Time `json:"session_expires_at"` // the cap, however often it is refreshed
	Member           Member    `json:"member"`
}

type signInRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// POST /auth/sign-in — authenticateMember(): check the password, open a session, issue the
// first pair of tokens (FR-0.1).
func (a *AuthAPI) signIn(w http.ResponseWriter, r *http.Request) {
	var req signInRequest
	if !readJSON(w, r, &req) {
		return
	}
	email, problem := normaliseEmail(req.Email)
	if problem != "" || req.Password == "" {
		// Which of the two was wrong is not said: an error that distinguishes them turns this
		// endpoint into a way to find out who has an account here.
		writeError(w, http.StatusUnauthorized, ErrInvalidCredentials.Error(), nil)
		return
	}

	creds, err := a.store.FindCredentials(r.Context(), email)
	if errors.Is(err, ErrNotFound) {
		// Hash anyway, so an unknown email does not answer faster than a wrong password.
		_ = bcrypt.CompareHashAndPassword(decoyHash(), []byte(req.Password))
		writeError(w, http.StatusUnauthorized, ErrInvalidCredentials.Error(), nil)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error", err)
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(creds.passwordHash), []byte(req.Password)) != nil {
		writeError(w, http.StatusUnauthorized, ErrInvalidCredentials.Error(), nil)
		return
	}
	// FR-0.5: a password an Admin typed is not the member's password until the member has
	// replaced it, so it cannot open a session.
	if creds.member.TemporaryPassword {
		writeError(w, http.StatusForbidden, ErrPasswordChangeRequired.Error(), nil)
		return
	}

	now := a.clock()
	refreshToken, refreshHash, err := newRefreshToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not issue a refresh token", err)
		return
	}
	endsAt := now.Add(a.sessionLifetime)
	if err := a.store.StartSession(r.Context(), creds.member.ID, endsAt, refreshHash); err != nil {
		writeError(w, http.StatusInternalServerError, "database error", err)
		return
	}
	a.writeTokens(w, creds.member, refreshToken, endsAt, now)
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// POST /auth/refresh — exchange a refresh token for the next pair. The old one is retired in
// the same transaction, so presenting it again is the signal that a copy exists.
func (a *AuthAPI) refresh(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if !readJSON(w, r, &req) {
		return
	}
	if req.RefreshToken == "" {
		writeError(w, http.StatusBadRequest, "refresh_token is required", nil)
		return
	}

	now := a.clock()
	nextToken, nextHash, err := newRefreshToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not issue a refresh token", err)
		return
	}

	rotated, err := a.store.RotateRefreshToken(r.Context(), hashRefreshToken(req.RefreshToken), now, nextHash)
	switch {
	case errors.Is(err, ErrInvalidRefresh), errors.Is(err, ErrSessionEnded), errors.Is(err, ErrTokenReused):
		writeError(w, http.StatusUnauthorized, err.Error(), nil)
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, "database error", err)
		return
	}

	// The role comes back from the members table on every refresh, so a re-roled member gets
	// their new role here rather than only at the next sign-in.
	member, err := a.store.Get(r.Context(), rotated.MemberID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error", err)
		return
	}
	a.writeTokens(w, member, nextToken, rotated.SessionEndsAt, now)
}

type changePasswordRequest struct {
	Email           string `json:"email"`
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// POST /auth/change-password — changePassword(): the member replaces their own password, which
// is how a temporary one is cleared (FR-0.5). It takes the credentials rather than a token,
// because a member with a temporary password cannot sign in to get one.
//
// Every session of that member ends: a password is changed because the old one may be known.
func (a *AuthAPI) changePassword(w http.ResponseWriter, r *http.Request) {
	var req changePasswordRequest
	if !readJSON(w, r, &req) {
		return
	}
	email, emailProblem := normaliseEmail(req.Email)
	problems := nonEmpty(emailProblem, checkPassword(req.NewPassword))
	if len(problems) > 0 {
		writeInvalid(w, problems)
		return
	}
	if req.NewPassword == req.CurrentPassword {
		writeInvalid(w, []string{"the new password must differ from the current one"})
		return
	}

	creds, err := a.store.FindCredentials(r.Context(), email)
	if errors.Is(err, ErrNotFound) {
		_ = bcrypt.CompareHashAndPassword(decoyHash(), []byte(req.CurrentPassword))
		writeError(w, http.StatusUnauthorized, ErrInvalidCredentials.Error(), nil)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error", err)
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(creds.passwordHash), []byte(req.CurrentPassword)) != nil {
		writeError(w, http.StatusUnauthorized, ErrInvalidCredentials.Error(), nil)
		return
	}

	hash, err := hashPassword(req.NewPassword)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not hash the password", err)
		return
	}
	member, err := a.store.ReplaceOwnPassword(r.Context(), creds.member.ID, hash)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, member)
}

// writeTokens mints the access token and answers with the pair.
func (a *AuthAPI) writeTokens(w http.ResponseWriter, member Member, refreshToken string, endsAt, now time.Time) {
	accessToken, expiresAt, err := a.signer.mint(member.ID, member.Role, now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not issue an access token", err)
		return
	}
	writeJSON(w, http.StatusOK, tokenResponse{
		AccessToken:      accessToken,
		TokenType:        "Bearer",
		ExpiresIn:        int(expiresAt.Sub(now).Seconds()),
		RefreshToken:     refreshToken,
		SessionExpiresAt: endsAt.UTC(),
		Member:           member,
	})
}

// decoyHash is a bcrypt hash of a value nobody holds, compared against when the email is
// unknown so that the answer takes the same few hundred milliseconds either way.
var decoyHash = sync.OnceValue(func() []byte {
	hash, err := bcrypt.GenerateFromPassword([]byte("no member has this password"), bcryptCost)
	if err != nil {
		return []byte("$2a$12$invalid")
	}
	return hash
})
