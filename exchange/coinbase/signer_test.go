package coinbase

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
	"time"
)

func TestSignRESTJWTProducesVerifiableCoinbaseClaims(t *testing.T) {
	t.Parallel()

	key, secret := newTestECKey(t, elliptic.P256())
	now := time.Unix(1_700_000_000, 0)
	token, err := SignRESTJWT(
		"organizations/org/apiKeys/key", "get", "api.coinbase.com",
		"/api/v3/brokerage/accounts", secret, now, rand.Reader,
	)
	if err != nil {
		t.Fatalf("SignRESTJWT() error = %v", err)
	}
	header, claims := verifyTestJWT(t, token, &key.PublicKey)
	if header.KeyID != "organizations/org/apiKeys/key" || header.Algorithm != "ES256" ||
		header.Type != "JWT" || len(header.Nonce) != 32 {
		t.Fatalf("JWT header = %+v", header)
	}
	if claims.Subject != header.KeyID || claims.Issuer != "cdp" || claims.NotBefore != now.Unix() ||
		claims.ExpiresAt-claims.NotBefore != 120 ||
		claims.URI != "GET api.coinbase.com/api/v3/brokerage/accounts" {
		t.Fatalf("JWT claims = %+v", claims)
	}
}

func TestSignRESTJWTAcceptsPKCS8AndEscapedNewlines(t *testing.T) {
	t.Parallel()

	key, _ := newTestECKey(t, elliptic.P256())
	encoded, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("MarshalPKCS8PrivateKey() error = %v", err)
	}
	secret := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded})
	escaped := []byte(strings.ReplaceAll(string(secret), "\n", `\n`))
	token, err := SignRESTJWT(
		"organizations/org/apiKeys/key", "POST", "api.coinbase.com",
		"/api/v3/brokerage/orders", escaped, time.Unix(1_700_000_000, 0), rand.Reader,
	)
	if err != nil {
		t.Fatalf("SignRESTJWT() error = %v", err)
	}
	verifyTestJWT(t, token, &key.PublicKey)
}

func TestSignWebSocketJWTOmitsRESTURI(t *testing.T) {
	t.Parallel()

	key, secret := newTestECKey(t, elliptic.P256())
	now := time.Unix(1_700_000_000, 0)
	token, err := SignWebSocketJWT(
		"organizations/org/apiKeys/key", secret, now, rand.Reader,
	)
	if err != nil {
		t.Fatalf("SignWebSocketJWT() error = %v", err)
	}
	header, claims := verifyTestJWT(t, token, &key.PublicKey)
	if header.KeyID != claims.Subject || claims.URI != "" || claims.ExpiresAt-claims.NotBefore != 120 {
		t.Fatalf("WebSocket JWT header = %+v, claims = %+v", header, claims)
	}
}

func TestSignRESTJWTRejectsNonP256Key(t *testing.T) {
	t.Parallel()

	_, secret := newTestECKey(t, elliptic.P384())
	if _, err := SignRESTJWT(
		"organizations/org/apiKeys/key", "GET", "api.coinbase.com",
		"/api/v3/brokerage/accounts", secret, time.Now(), rand.Reader,
	); err == nil {
		t.Fatal("SignRESTJWT() error = nil, want unsupported curve error")
	}
}

type testJWTHeader struct {
	Algorithm string `json:"alg"`
	KeyID     string `json:"kid"`
	Nonce     string `json:"nonce"`
	Type      string `json:"typ"`
}

type testJWTClaims struct {
	Subject   string `json:"sub"`
	Issuer    string `json:"iss"`
	NotBefore int64  `json:"nbf"`
	ExpiresAt int64  `json:"exp"`
	URI       string `json:"uri"`
}

func verifyTestJWT(t *testing.T, token string, publicKey *ecdsa.PublicKey) (testJWTHeader, testJWTClaims) {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("JWT has %d parts", len(parts))
	}
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatalf("decode JWT header: %v", err)
	}
	claimBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode JWT claims: %v", err)
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(signature) != 64 {
		t.Fatalf("decode JWT signature: length = %d, error = %v", len(signature), err)
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	r := new(big.Int).SetBytes(signature[:32])
	s := new(big.Int).SetBytes(signature[32:])
	if !ecdsa.Verify(publicKey, digest[:], r, s) {
		t.Fatal("JWT ES256 signature is invalid")
	}
	var header testJWTHeader
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		t.Fatalf("decode JWT header JSON: %v", err)
	}
	var claims testJWTClaims
	if err := json.Unmarshal(claimBytes, &claims); err != nil {
		t.Fatalf("decode JWT claims JSON: %v", err)
	}
	return header, claims
}

func newTestECKey(t *testing.T, curve elliptic.Curve) (*ecdsa.PrivateKey, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(curve, rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	encoded, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("MarshalECPrivateKey() error = %v", err)
	}
	return key, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: encoded})
}
