// token_test.go — the access token is the one thing in this service that another service has to
// agree with, so what it contains is tested here rather than only in a running system.
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// devKeyPEM makes a P-256 key in both of the shapes openssl writes.
func devKeyPEM(t *testing.T, pkcs8 bool) (*ecdsa.PrivateKey, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating a key: %v", err)
	}
	var block pem.Block
	if pkcs8 {
		der, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			t.Fatalf("encoding the key: %v", err)
		}
		block = pem.Block{Type: "PRIVATE KEY", Bytes: der}
	} else {
		der, err := x509.MarshalECPrivateKey(key)
		if err != nil {
			t.Fatalf("encoding the key: %v", err)
		}
		block = pem.Block{Type: "EC PRIVATE KEY", Bytes: der}
	}
	return key, string(pem.EncodeToMemory(&block))
}

func TestMintedTokenVerifiesWithThePublicKey(t *testing.T) {
	key, keyPEM := devKeyPEM(t, false)
	s, err := newSigner(keyPEM, "dev-1", 15*time.Minute)
	if err != nil {
		t.Fatalf("newSigner: %v", err)
	}

	now := time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)
	token, expires, err := s.mint("00000000-0000-4000-8000-000000000001", "admin", now)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	if want := now.Add(15 * time.Minute); !expires.Equal(want) {
		t.Errorf("expires at %s, want %s", expires, want)
	}

	claims := jwt.MapClaims{}
	parsed, err := jwt.ParseWithClaims(token, claims, func(*jwt.Token) (any, error) { return &key.PublicKey, nil },
		jwt.WithValidMethods([]string{"ES256"}), jwt.WithExpirationRequired(), jwt.WithTimeFunc(func() time.Time { return now }))
	if err != nil {
		t.Fatalf("a verifier holding the public key could not read the token: %v", err)
	}

	// These three are the contract with every verifier: the gateway reads sub and role, and
	// refuses a token whose algorithm is anything but ES256.
	if got := parsed.Header["alg"]; got != "ES256" {
		t.Errorf("alg is %v, want ES256", got)
	}
	if got := parsed.Header["kid"]; got != "dev-1" {
		t.Errorf("kid is %v, want dev-1", got)
	}
	if got, _ := claims["sub"].(string); got != "00000000-0000-4000-8000-000000000001" {
		t.Errorf("sub is %q", got)
	}
	// The database says `admin`; every verifier expects the role as the requirements name it.
	if got, _ := claims["role"].(string); got != "Admin" {
		t.Errorf("role is %q, want Admin", got)
	}
}

func TestExpiredTokenIsRejected(t *testing.T) {
	key, keyPEM := devKeyPEM(t, true)
	s, _ := newSigner(keyPEM, "", time.Minute)

	now := time.Now().UTC()
	token, _, err := s.mint("m-1", "recruiter", now.Add(-2*time.Minute))
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	_, err = jwt.Parse(token, func(*jwt.Token) (any, error) { return &key.PublicKey, nil },
		jwt.WithValidMethods([]string{"ES256"}), jwt.WithExpirationRequired())
	if err == nil {
		t.Fatal("a token that expired a minute ago was accepted")
	}
}

func TestSignerRefusesWhatCannotSignES256(t *testing.T) {
	cases := map[string]string{
		"not a PEM block": "hello",
		"the public half": func() string {
			key, _ := devKeyPEM(t, false)
			der, _ := x509.MarshalPKIXPublicKey(&key.PublicKey)
			return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
		}(),
		"the wrong curve": func() string {
			key, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
			der, _ := x509.MarshalECPrivateKey(key)
			return string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}))
		}(),
	}
	for name, keyPEM := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := newSigner(keyPEM, "", time.Minute); err == nil {
				t.Error("accepted at startup; it would have failed on the first sign-in instead")
			}
		})
	}
}

func TestClaimRoleRejectsAnUnknownRole(t *testing.T) {
	if _, err := claimRole("owner"); err == nil {
		t.Error("an unknown role produced a token; a verifier would reject it with no explanation")
	}
}

func TestRefreshTokensAreUnrelatedToTheirStoredHash(t *testing.T) {
	first, firstHash, err := newRefreshToken()
	if err != nil {
		t.Fatalf("newRefreshToken: %v", err)
	}
	second, _, _ := newRefreshToken()

	if first == second {
		t.Error("two refresh tokens came out the same")
	}
	if len(firstHash) != 32 {
		t.Errorf("hash is %d bytes, want 32", len(firstHash))
	}
	if string(firstHash) == first {
		t.Error("the stored hash is the token itself")
	}
	// The same token must hash the same way, or no token could ever be exchanged.
	if string(hashRefreshToken(first)) != string(firstHash) {
		t.Error("hashing the same token twice gave two answers")
	}
}
