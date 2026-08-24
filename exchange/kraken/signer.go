// Package kraken은 Kraken Spot REST API 어댑터를 제공한다.
package kraken

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"fmt"
	"strings"
)

// SignREST는 Kraken private REST 요청의 API-Sign 값을 생성한다.
func SignREST(path, nonce, encodedPayload string, encodedSecret []byte) (string, error) {
	if !strings.HasPrefix(path, "/0/private/") || strings.ContainsAny(path, "?#\r\n") {
		return "", fmt.Errorf("Kraken private request path is invalid")
	}
	if strings.TrimSpace(nonce) == "" {
		return "", fmt.Errorf("Kraken nonce is required")
	}
	secret, err := base64.StdEncoding.DecodeString(string(encodedSecret))
	if err != nil {
		return "", fmt.Errorf("decode Kraken API secret: %w", err)
	}
	if len(secret) == 0 {
		return "", fmt.Errorf("Kraken API secret is required")
	}
	defer clear(secret)
	digest := sha256.Sum256([]byte(nonce + encodedPayload))
	message := make([]byte, 0, len(path)+len(digest))
	message = append(message, path...)
	message = append(message, digest[:]...)
	mac := hmac.New(sha512.New, secret)
	if _, err := mac.Write(message); err != nil {
		return "", fmt.Errorf("write Kraken HMAC payload: %w", err)
	}
	return base64.StdEncoding.EncodeToString(mac.Sum(nil)), nil
}
