// token.go — the access token and the refresh token: what they are made of, and how they are
// minted. ADR-009.
//
// The Identity Service is the only holder of the signing key. Everything else — the gateway,
// and any service that acts on the caller's role — holds the public half as configuration and
// checks a token itself, so no one calls back here to validate one.
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// The role as it travels in a token. The database stores `admin` and `recruiter`; the documents
// and every verifier name the roles Admin and Recruiter (FR-0.3), and the token is where the two
// meet, so the mapping lives in one function.
const (
	claimAdmin     = "Admin"
	claimRecruiter = "Recruiter"
)

// refreshTokenBytes is the size of a refresh token: 256 bits of randomness and no meaning of its
// own. It is never stored — only its hash is (ADR-009), so a copy of the table yields nothing
// that can be presented.
const refreshTokenBytes = 32

// signer mints access tokens. One per process, built at startup from the configured key.
type signer struct {
	key *ecdsa.PrivateKey
	kid string // the key id put in the token header, so a verifier holding two keys knows which
	ttl time.Duration
}

// newSigner reads the signing key from its PEM text. It fails at startup rather than on the
// first sign-in, because a service that cannot sign cannot do its job.
func newSigner(privateKeyPEM, kid string, ttl time.Duration) (*signer, error) {
	key, err := parsePrivateKey(privateKeyPEM)
	if err != nil {
		return nil, err
	}
	if ttl <= 0 {
		return nil, errors.New("access token lifetime must be positive")
	}
	return &signer{key: key, kid: kid, ttl: ttl}, nil
}

// parsePrivateKey accepts either of the two shapes openssl produces: a bare `EC PRIVATE KEY`
// block (SEC 1) or a `PRIVATE KEY` block (PKCS #8).
func parsePrivateKey(text string) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(strings.TrimSpace(text)))
	if block == nil {
		return nil, errors.New("not a PEM block")
	}

	var (
		key *ecdsa.PrivateKey
		err error
	)
	switch block.Type {
	case "EC PRIVATE KEY":
		key, err = x509.ParseECPrivateKey(block.Bytes)
	case "PRIVATE KEY":
		var parsed any
		if parsed, err = x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
			ecKey, ok := parsed.(*ecdsa.PrivateKey)
			if !ok {
				// ES256 is an ECDSA signature, so an RSA key here means the configuration and
				// the verifiers disagree about the algorithm.
				return nil, fmt.Errorf("want an ECDSA private key for ES256, got %T", parsed)
			}
			key = ecKey
		}
	default:
		return nil, fmt.Errorf("unexpected PEM block %q; want EC PRIVATE KEY or PRIVATE KEY", block.Type)
	}
	if err != nil {
		return nil, fmt.Errorf("unreadable private key: %w", err)
	}
	if key.Curve != elliptic.P256() {
		return nil, fmt.Errorf("ES256 needs a P-256 key; this one is %s", key.Curve.Params().Name)
	}
	return key, nil
}

// mint returns a signed access token for a member and when it expires.
func (s *signer) mint(memberID, storedRole string, now time.Time) (string, time.Time, error) {
	role, err := claimRole(storedRole)
	if err != nil {
		return "", time.Time{}, err
	}
	expires := now.Add(s.ttl)

	token := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
		"sub":  memberID,
		"role": role,
		"iat":  jwt.NewNumericDate(now),
		"exp":  jwt.NewNumericDate(expires),
	})
	if s.kid != "" {
		token.Header["kid"] = s.kid
	}
	signed, err := token.SignedString(s.key)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("signing the access token: %w", err)
	}
	return signed, expires, nil
}

// claimRole turns a stored role into the one a verifier expects to read.
func claimRole(stored string) (string, error) {
	switch stored {
	case "admin":
		return claimAdmin, nil
	case "recruiter":
		return claimRecruiter, nil
	}
	return "", fmt.Errorf("member holds an unknown role %q", stored)
}

// newRefreshToken returns a fresh refresh token and the hash that is stored in its place.
func newRefreshToken() (string, []byte, error) {
	raw := make([]byte, refreshTokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, fmt.Errorf("reading random bytes: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	return token, hashRefreshToken(token), nil
}

// hashRefreshToken is what the sessions table holds. SHA-256 rather than bcrypt: the token is
// 256 random bits, so there is no low-entropy secret to slow a guesser down over, and this hash
// is computed on every refresh.
func hashRefreshToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}
