package bithumb

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
)

func TestSignJWTBuildsValidHS256Token(t *testing.T) {
	t.Parallel()

	query := "market=KRW-BTC&states[]=done&states[]=cancel"
	token, err := SignJWT([]byte("access-key"), []byte("secret-key"), "nonce-1", 1700000000123, query)
	if err != nil {
		t.Fatalf("SignJWT() error = %v", err)
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("JWT part count = %d, want 3", len(parts))
	}
	payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode JWT payload: %v", err)
	}
	var payload struct {
		AccessKey    string `json:"access_key"`
		Nonce        string `json:"nonce"`
		Timestamp    int64  `json:"timestamp"`
		QueryHash    string `json:"query_hash"`
		QueryHashAlg string `json:"query_hash_alg"`
	}
	if err := json.Unmarshal(payloadJSON, &payload); err != nil {
		t.Fatalf("decode JWT payload JSON: %v", err)
	}
	if payload.AccessKey != "access-key" || payload.Nonce != "nonce-1" || payload.Timestamp != 1700000000123 ||
		payload.QueryHash != QueryHash(query) || payload.QueryHashAlg != "SHA512" {
		t.Fatalf("JWT payload = %+v", payload)
	}
	mac := hmac.New(sha256.New, []byte("secret-key"))
	_, _ = mac.Write([]byte(parts[0] + "." + parts[1]))
	wantSignature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if parts[2] != wantSignature {
		t.Fatalf("JWT signature = %q, want %q", parts[2], wantSignature)
	}
}

func TestSignJWTOmitsQueryHashWithoutParameters(t *testing.T) {
	t.Parallel()

	token, err := SignJWT([]byte("access-key"), []byte("secret-key"), "nonce-1", 1700000000123, "")
	if err != nil {
		t.Fatalf("SignJWT() error = %v", err)
	}
	parts := strings.Split(token, ".")
	payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode JWT payload: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(payloadJSON, &payload); err != nil {
		t.Fatalf("decode JWT payload JSON: %v", err)
	}
	if _, exists := payload["query_hash"]; exists {
		t.Fatalf("JWT payload = %v", payload)
	}
}

func TestParametersKeepRawValuesForHash(t *testing.T) {
	t.Parallel()

	values := parameters{
		{key: "market", value: "KRW-BTC"},
		{key: "states[]", value: "done"},
		{key: "states[]", value: "cancel"},
		{key: "next_key", value: "alpha+/="},
	}
	if got, want := values.encoded(), "market=KRW-BTC&states[]=done&states[]=cancel&next_key=alpha%2B%2F%3D"; got != want {
		t.Fatalf("encoded() = %q, want %q", got, want)
	}
	if got, want := values.hashString(), "market=KRW-BTC&states[]=done&states[]=cancel&next_key=alpha+/="; got != want {
		t.Fatalf("hashString() = %q, want %q", got, want)
	}
}

func TestQueryHashUsesSHA512Hex(t *testing.T) {
	t.Parallel()

	digest := sha512.Sum512([]byte("market=KRW-BTC"))
	if got, want := QueryHash("market=KRW-BTC"), hex.EncodeToString(digest[:]); got != want {
		t.Fatalf("QueryHash() = %s, want %s", got, want)
	}
}
