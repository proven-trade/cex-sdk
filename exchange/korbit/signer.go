// Package korbit은 코빗 Open API v2 Spot REST 어댑터를 제공한다.
package korbit

import (
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"fmt"
)

// SigningMode는 코빗 API Key에 설정된 서명 방식이다.
type SigningMode string

const (
	SigningModeHMACSHA256 SigningMode = "HMAC-SHA256"
	SigningModeED25519    SigningMode = "ED25519"
)

// SignHMACSHA256은 최종 URL 인코딩 파라미터를 HMAC-SHA256 소문자 hex로 서명한다.
func SignHMACSHA256(secretKey []byte, encodedParameters string) (string, error) {
	if len(secretKey) == 0 {
		return "", fmt.Errorf("Korbit HMAC secret key is required")
	}
	if encodedParameters == "" {
		return "", fmt.Errorf("Korbit signing input is required")
	}
	mac := hmac.New(sha256.New, secretKey)
	_, _ = mac.Write([]byte(encodedParameters))
	digest := mac.Sum(nil)
	defer clear(digest)
	return hex.EncodeToString(digest), nil
}

// SignED25519은 PKCS#8 PEM private key로 서명하고 표준 Base64 값을 반환한다.
func SignED25519(privateKeyPEM []byte, encodedParameters string) (string, error) {
	if len(privateKeyPEM) == 0 {
		return "", fmt.Errorf("Korbit ED25519 private key is required")
	}
	if encodedParameters == "" {
		return "", fmt.Errorf("Korbit signing input is required")
	}
	block, _ := pem.Decode(privateKeyPEM)
	if block == nil {
		return "", fmt.Errorf("decode Korbit ED25519 private key PEM")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("parse Korbit ED25519 private key: %w", err)
	}
	privateKey, ok := parsed.(ed25519.PrivateKey)
	if !ok {
		return "", fmt.Errorf("Korbit private key is not ED25519")
	}
	signature := ed25519.Sign(privateKey, []byte(encodedParameters))
	defer clear(signature)
	return base64.StdEncoding.EncodeToString(signature), nil
}

func signParameters(mode SigningMode, secretKey []byte, encodedParameters string) (string, error) {
	switch mode {
	case SigningModeHMACSHA256:
		return SignHMACSHA256(secretKey, encodedParameters)
	case SigningModeED25519:
		return SignED25519(secretKey, encodedParameters)
	default:
		return "", fmt.Errorf("unsupported Korbit signing mode %q", mode)
	}
}
