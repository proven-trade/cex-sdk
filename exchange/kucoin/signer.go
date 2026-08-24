package kucoin

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"
)

// SignHMACSHA256은 KuCoin 문자열을 HMAC SHA-256과 Base64로 서명한다.
func SignHMACSHA256(secret, payload []byte) (string, error) {
	if len(secret) == 0 {
		return "", fmt.Errorf("KuCoin HMAC secret is required")
	}
	mac := hmac.New(sha256.New, secret)
	if _, err := mac.Write(payload); err != nil {
		return "", fmt.Errorf("write KuCoin HMAC payload: %w", err)
	}
	return base64.StdEncoding.EncodeToString(mac.Sum(nil)), nil
}

func signaturePayload(timestamp, method, endpoint string, body []byte) []byte {
	var builder strings.Builder
	builder.Grow(len(timestamp) + len(method) + len(endpoint) + len(body))
	builder.WriteString(timestamp)
	builder.WriteString(strings.ToUpper(method))
	builder.WriteString(endpoint)
	builder.Write(body)
	return []byte(builder.String())
}
