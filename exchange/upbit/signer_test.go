package upbit

import (
	"crypto/hmac"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
)

func TestSignJWTBuildsValidHS512Token(t *testing.T) {
	t.Parallel()

	query := "market=KRW-BTC&states[]=wait&states[]=watch"
	token, err := SignJWT([]byte("access-key"), []byte("secret-key"), "nonce-1", query)
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
	var payload map[string]string
	if err := json.Unmarshal(payloadJSON, &payload); err != nil {
		t.Fatalf("decode JWT payload JSON: %v", err)
	}
	if payload["access_key"] != "access-key" || payload["nonce"] != "nonce-1" ||
		payload["query_hash"] != QueryHash(query) || payload["query_hash_alg"] != "SHA512" {
		t.Fatalf("JWT payload = %v", payload)
	}
	mac := hmac.New(sha512.New, []byte("secret-key"))
	_, _ = mac.Write([]byte(parts[0] + "." + parts[1]))
	wantSignature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if parts[2] != wantSignature {
		t.Fatalf("JWT signature = %q, want %q", parts[2], wantSignature)
	}
}

func TestQueryHashKnownVector(t *testing.T) {
	t.Parallel()

	const query = "market=KRW-BTC&side=bid&volume=0.01&price=100.0&ord_type=limit"
	const want = "1db802a392c559d55c99662a20c6911ba9ea31a9f58bf92156af243ca1462b004c6e6b27c934afefbde5ca15d28deb67e90cd619b466c9a3c2fe020ad2bbdd24"
	if got := QueryHash(query); got != want {
		t.Fatalf("QueryHash() = %s, want %s", got, want)
	}
}

func TestParametersPreserveRepeatedKeyOrder(t *testing.T) {
	t.Parallel()

	values := parameters{
		{key: "market", value: "KRW-BTC"},
		{key: "states[]", value: "wait"},
		{key: "states[]", value: "watch"},
		{key: "identifier", value: "alpha beta+1"},
	}
	if got, want := values.encoded(), "market=KRW-BTC&states[]=wait&states[]=watch&identifier=alpha+beta%2B1"; got != want {
		t.Fatalf("encoded() = %q, want %q", got, want)
	}
	got, err := values.hashString()
	if err != nil {
		t.Fatalf("hashString() error = %v", err)
	}
	if want := "market=KRW-BTC&states[]=wait&states[]=watch&identifier=alpha+beta+1"; got != want {
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
