// Package okx는 OKX V5 Spot·SWAP REST API 어댑터를 제공한다.
package okx

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

// SignHMACSHA256은 OKX V5 HMAC 서명을 Base64 문자열로 생성한다.
func SignHMACSHA256(secret, payload []byte) (string, error) {
	if len(secret) == 0 {
		return "", fmt.Errorf("OKX HMAC secret is required")
	}
	mac := hmac.New(sha256.New, secret)
	if _, err := mac.Write(payload); err != nil {
		return "", fmt.Errorf("write OKX HMAC payload: %w", err)
	}
	return base64.StdEncoding.EncodeToString(mac.Sum(nil)), nil
}

func signaturePayload(timestamp, method, requestPath string, body []byte) []byte {
	prefix := timestamp + method + requestPath
	payload := make([]byte, 0, len(prefix)+len(body))
	payload = append(payload, prefix...)
	payload = append(payload, body...)
	return payload
}
