package bitget

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"
)

// SignHMACSHA256은 Bitget pre-hash 문자열을 HMAC SHA-256과 Base64로 서명한다.
func SignHMACSHA256(secret, preHash []byte) (string, error) {
	if len(secret) == 0 {
		return "", fmt.Errorf("Bitget HMAC secret is required")
	}
	mac := hmac.New(sha256.New, secret)
	if _, err := mac.Write(preHash); err != nil {
		return "", fmt.Errorf("write Bitget HMAC payload: %w", err)
	}
	return base64.StdEncoding.EncodeToString(mac.Sum(nil)), nil
}

func signaturePayload(timestamp, method, path, query string, body []byte) []byte {
	var builder strings.Builder
	builder.Grow(len(timestamp) + len(method) + len(path) + len(query) + len(body) + 1)
	builder.WriteString(timestamp)
	builder.WriteString(strings.ToUpper(method))
	builder.WriteString(path)
	if query != "" {
		builder.WriteByte('?')
		builder.WriteString(query)
	}
	builder.Write(body)
	return []byte(builder.String())
}
