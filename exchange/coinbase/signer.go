package coinbase

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"strings"
	"time"
)

const jwtLifetime = 2 * time.Minute

// SignRESTJWT는 Coinbase CDP ECDSA key로 요청별 ES256 JWT를 생성한다.
func SignRESTJWT(
	keyName, method, host, path string,
	privateKeyPEM []byte,
	now time.Time,
	random io.Reader,
) (string, error) {
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" || strings.ContainsAny(method, " \t\r\n") {
		return "", fmt.Errorf("Coinbase JWT request method is invalid")
	}
	host = strings.TrimSpace(host)
	if host == "" || strings.ContainsAny(host, " /\t\r\n") {
		return "", fmt.Errorf("Coinbase JWT request host is invalid")
	}
	if !strings.HasPrefix(path, "/") || strings.ContainsAny(path, "?#\r\n") {
		return "", fmt.Errorf("Coinbase JWT request path is invalid")
	}
	return signJWT(keyName, privateKeyPEM, now, random, method+" "+host+path)
}

// SignWebSocketJWT는 Coinbase WebSocket 메시지용 ES256 JWT를 생성한다.
func SignWebSocketJWT(
	keyName string,
	privateKeyPEM []byte,
	now time.Time,
	random io.Reader,
) (string, error) {
	return signJWT(keyName, privateKeyPEM, now, random, "")
}

func signJWT(
	keyName string,
	privateKeyPEM []byte,
	now time.Time,
	random io.Reader,
	uri string,
) (string, error) {
	if strings.TrimSpace(keyName) == "" {
		return "", fmt.Errorf("Coinbase API key name is required")
	}
	key, err := parseECPrivateKey(privateKeyPEM)
	if err != nil {
		return "", err
	}
	defer key.D.SetInt64(0)
	if random == nil {
		random = rand.Reader
	}
	nonceBytes := make([]byte, 16)
	if _, err := io.ReadFull(random, nonceBytes); err != nil {
		return "", fmt.Errorf("generate Coinbase JWT nonce: %w", err)
	}
	header, err := json.Marshal(struct {
		Algorithm string `json:"alg"`
		KeyID     string `json:"kid"`
		Nonce     string `json:"nonce"`
		Type      string `json:"typ"`
	}{
		Algorithm: "ES256", KeyID: keyName, Nonce: hex.EncodeToString(nonceBytes), Type: "JWT",
	})
	if err != nil {
		return "", fmt.Errorf("encode Coinbase JWT header: %w", err)
	}
	issuedAt := now.Unix()
	claims, err := json.Marshal(struct {
		Subject   string `json:"sub"`
		Issuer    string `json:"iss"`
		NotBefore int64  `json:"nbf"`
		ExpiresAt int64  `json:"exp"`
		URI       string `json:"uri,omitempty"`
	}{
		Subject: keyName, Issuer: "cdp", NotBefore: issuedAt,
		ExpiresAt: now.Add(jwtLifetime).Unix(), URI: uri,
	})
	if err != nil {
		return "", fmt.Errorf("encode Coinbase JWT claims: %w", err)
	}
	encodedHeader := base64.RawURLEncoding.EncodeToString(header)
	encodedClaims := base64.RawURLEncoding.EncodeToString(claims)
	signingInput := encodedHeader + "." + encodedClaims
	digest := sha256.Sum256([]byte(signingInput))
	r, s, err := ecdsa.Sign(random, key, digest[:])
	if err != nil {
		return "", fmt.Errorf("sign Coinbase JWT: %w", err)
	}
	signature, err := marshalES256Signature(r, s)
	if err != nil {
		return "", err
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func parseECPrivateKey(privateKeyPEM []byte) (*ecdsa.PrivateKey, error) {
	normalized := cloneBytes(privateKeyPEM)
	if !bytes.Contains(normalized, []byte("\n")) {
		normalized = bytes.ReplaceAll(normalized, []byte(`\n`), []byte("\n"))
	}
	block, _ := pem.Decode(normalized)
	for index := range normalized {
		normalized[index] = 0
	}
	if block == nil {
		return nil, fmt.Errorf("decode Coinbase EC private key PEM")
	}
	defer func() {
		for index := range block.Bytes {
			block.Bytes[index] = 0
		}
	}()
	key, err := x509.ParseECPrivateKey(block.Bytes)
	if err != nil {
		parsed, parseErr := x509.ParsePKCS8PrivateKey(block.Bytes)
		if parseErr != nil {
			return nil, fmt.Errorf("parse Coinbase EC private key: %w", err)
		}
		var ok bool
		key, ok = parsed.(*ecdsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("Coinbase private key must be ECDSA")
		}
	}
	if key.Curve != elliptic.P256() {
		return nil, fmt.Errorf("Coinbase private key must use P-256")
	}
	return key, nil
}

func marshalES256Signature(r, s *big.Int) ([]byte, error) {
	if r == nil || s == nil || r.Sign() <= 0 || s.Sign() <= 0 || r.BitLen() > 256 || s.BitLen() > 256 {
		return nil, fmt.Errorf("Coinbase ES256 signature values are invalid")
	}
	signature := make([]byte, 64)
	r.FillBytes(signature[:32])
	s.FillBytes(signature[32:])
	return signature, nil
}
