// Access tokens: how the gateway verifies one, and what it reads out of it.
//
// ADR-009 — the Identity Service signs; everyone else verifies. The gateway holds only public
// keys, checks the signature and expiry itself, and never calls the Identity Service to check a
// token, per request or at startup. A token it cannot verify is rejected; it is never forwarded
// on the chance that the service behind it might accept it.
package main

import (
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

// The two roles (FR-0.3). Admin holds every Recruiter permission in addition to its own.
const (
	roleAdmin     = "Admin"
	roleRecruiter = "Recruiter"
)

// caller is who the gateway decided the request is from.
type caller struct {
	memberID string
	role     string
}

// verifier holds the public keys the Identity Service's signatures are checked against. More
// than one is how a key is rotated: the new key is added, tokens signed with either are
// accepted, and the old one is removed once nothing holds a token signed with it.
type verifier struct {
	keys map[string]*ecdsa.PublicKey // by key id; "" is the key used when a token carries no kid
}

// publicKey is one entry of the JWT_PUBLIC_KEYS JSON array.
type publicKey struct {
	Kid string `json:"kid"`
	PEM string `json:"pem"`
}

// parsePublicKeys reads the verification material from configuration. It accepts either a JSON
// array of {"kid", "pem"} objects, or a single bare PEM block for the common case of one key.
// An empty value yields no keys, which is a running gateway that rejects every protected route.
func parsePublicKeys(value string) (map[string]*ecdsa.PublicKey, error) {
	keys := map[string]*ecdsa.PublicKey{}
	value = strings.TrimSpace(value)
	if value == "" {
		return keys, nil
	}

	entries := []publicKey{}
	if strings.HasPrefix(value, "-----BEGIN") {
		entries = append(entries, publicKey{PEM: value})
	} else if err := json.Unmarshal([]byte(value), &entries); err != nil {
		return nil, fmt.Errorf("expected a PEM block or a JSON array of {kid, pem}: %w", err)
	}

	for i, entry := range entries {
		key, err := parseECDSAPublicKey(entry.PEM)
		if err != nil {
			return nil, fmt.Errorf("key %d (kid %q): %w", i, entry.Kid, err)
		}
		if _, clash := keys[entry.Kid]; clash {
			return nil, fmt.Errorf("two keys share the key id %q", entry.Kid)
		}
		keys[entry.Kid] = key
	}
	return keys, nil
}

func parseECDSAPublicKey(encoded string) (*ecdsa.PublicKey, error) {
	block, _ := pem.Decode([]byte(strings.TrimSpace(encoded)))
	if block == nil {
		return nil, errors.New("not a PEM block")
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("not a PKIX public key: %w", err)
	}
	key, ok := parsed.(*ecdsa.PublicKey)
	if !ok {
		// ES256 is an ECDSA signature. An RSA key here means the configuration and the signer
		// disagree, which is worth failing at startup for.
		return nil, fmt.Errorf("want an ECDSA public key for ES256, got %T", parsed)
	}
	return key, nil
}

// errNoToken separates "did not present a token" from "presented one that is not valid", so the
// gateway can say which without the caller having to guess.
var errNoToken = errors.New("no access token")

// verify reads the bearer token off the request and returns who it says the caller is.
func (v *verifier) verify(r *http.Request) (caller, error) {
	header := r.Header.Get("Authorization")
	if header == "" {
		return caller{}, errNoToken
	}
	token, found := strings.CutPrefix(header, "Bearer ")
	if !found || strings.TrimSpace(token) == "" {
		return caller{}, errors.New("Authorization header must read: Bearer <token>")
	}

	claims := jwt.MapClaims{}
	_, err := jwt.ParseWithClaims(strings.TrimSpace(token), claims, v.key,
		// ES256 only. Without this a token could name its own algorithm, which is how a
		// verifier gets talked into accepting "none" or an HMAC over the public key.
		jwt.WithValidMethods([]string{"ES256"}),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return caller{}, err
	}

	memberID, _ := claims["sub"].(string)
	role, _ := claims["role"].(string)
	if memberID == "" {
		return caller{}, errors.New("token carries no member id")
	}
	if role != roleAdmin && role != roleRecruiter {
		return caller{}, fmt.Errorf("token carries an unknown role %q", role)
	}
	return caller{memberID: memberID, role: role}, nil
}

// key picks the public key a token names. While more than one key is configured a token must
// name the one it was signed with: during rotation, guessing would mean a token signed by the
// retired key verifying against the new one by luck.
//
// One key configured without a key id means no rotation is in progress, so a token is checked
// against it whether or not it names a kid — the signature still has to hold.
func (v *verifier) key(token *jwt.Token) (any, error) {
	if len(v.keys) == 0 {
		return nil, errors.New("the gateway has no verification key configured")
	}
	kid, _ := token.Header["kid"].(string)
	if key, ok := v.keys[kid]; ok {
		return key, nil
	}
	if only, unnamed := v.keys[""]; unnamed && len(v.keys) == 1 {
		return only, nil
	}
	return nil, fmt.Errorf("no verification key with id %q", kid)
}

// allows reports whether a role satisfies what a route requires. Admin holds every Recruiter
// permission in addition to its own (FR-0.3), so it passes a member route too.
func (a access) allows(role string) bool {
	switch a {
	case public:
		return true
	case member:
		return role == roleAdmin || role == roleRecruiter
	default:
		return role == roleAdmin
	}
}
