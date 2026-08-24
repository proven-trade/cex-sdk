package korbit

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"testing"
)

func TestSignHMACSHA256Vector(t *testing.T) {
	t.Parallel()

	signature, err := SignHMACSHA256(
		[]byte("secret-key"), "symbol=btc_krw&timestamp=1700000000123",
	)
	if err != nil {
		t.Fatalf("SignHMACSHA256() error = %v", err)
	}
	want := "bf4a6ef9b4341b94c169af5b7c7fa4f4ad6018ab2eac06dd140cf7b1975a56b0"
	if signature != want {
		t.Fatalf("signature = %q, want %q", signature, want)
	}
}

func TestSignED25519UsesPKCS8PEMAndStandardBase64(t *testing.T) {
	t.Parallel()

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519.GenerateKey() error = %v", err)
	}
	encodedKey, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatalf("x509.MarshalPKCS8PrivateKey() error = %v", err)
	}
	privateKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encodedKey})
	input := "recvWindow=5000&timestamp=1700000000123"
	signature, err := SignED25519(privateKeyPEM, input)
	if err != nil {
		t.Fatalf("SignED25519() error = %v", err)
	}
	decoded, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		t.Fatalf("decode signature: %v", err)
	}
	if !ed25519.Verify(publicKey, []byte(input), decoded) {
		t.Fatal("ED25519 signature verification failed")
	}
}
