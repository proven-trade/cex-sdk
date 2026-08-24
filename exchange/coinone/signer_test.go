package coinone

import (
	"encoding/base64"
	"encoding/json"
	"regexp"
	"testing"
)

func TestSignPayloadVector(t *testing.T) {
	t.Parallel()

	body := []byte(`{"access_token":"access-key","nonce":"123e4567-e89b-42d3-a456-426614174000","side":"BUY"}`)
	payload, signature, err := SignPayload([]byte("secret-key"), body)
	if err != nil {
		t.Fatalf("SignPayload() error = %v", err)
	}
	if want := "eyJhY2Nlc3NfdG9rZW4iOiJhY2Nlc3Mta2V5Iiwibm9uY2UiOiIxMjNlNDU2Ny1lODliLTQyZDMtYTQ1Ni00MjY2MTQxNzQwMDAiLCJzaWRlIjoiQlVZIn0="; payload != want {
		t.Fatalf("payload = %q, want %q", payload, want)
	}
	if want := "7f1bd5e812545548a1609fcbab9b463e7020f555c039999b8d98e6c690e4054adbb123fdb9732c36b5082e1862b01bd98507109e37abdc905e88081849e2a927"; signature != want {
		t.Fatalf("signature = %q, want %q", signature, want)
	}
}

func TestEncodePrivatePayloadPreservesOrderAndTypes(t *testing.T) {
	t.Parallel()

	fields := payloadFields{}
	fields.addString("side", "BUY")
	fields.addBool("post_only", false)
	fields.addInt("size", 10)
	fields.addStrings("order_type", []string{"LIMIT", "STOP_LIMIT"})
	body, err := encodePrivatePayload([]byte("token"), "nonce", fields)
	if err != nil {
		t.Fatalf("encodePrivatePayload() error = %v", err)
	}
	want := `{"access_token":"token","nonce":"nonce","side":"BUY","post_only":false,"size":10,"order_type":["LIMIT","STOP_LIMIT"]}`
	if string(body) != want {
		t.Fatalf("body = %s, want %s", body, want)
	}
	encoded, _, err := SignPayload([]byte("secret"), body)
	if err != nil {
		t.Fatalf("SignPayload() error = %v", err)
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || string(decoded) != want {
		t.Fatalf("decoded payload = %s, error = %v", decoded, err)
	}
	var object map[string]any
	if err := json.Unmarshal(body, &object); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if object["post_only"] != false || object["size"] != float64(10) {
		t.Fatalf("payload types = %#v", object)
	}
}

func TestRandomNonceIsUUIDVersion4(t *testing.T) {
	t.Parallel()

	pattern := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	first, err := randomNonce()
	if err != nil {
		t.Fatalf("randomNonce() error = %v", err)
	}
	second, err := randomNonce()
	if err != nil {
		t.Fatalf("randomNonce() error = %v", err)
	}
	if !pattern.MatchString(first) || !pattern.MatchString(second) || first == second {
		t.Fatalf("nonces = %q, %q", first, second)
	}
}
