package gateio

import (
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"strings"
)

// PayloadHash는 요청 본문의 SHA-512 해시를 소문자 16진수로 반환한다.
func PayloadHash(body []byte) string {
	sum := sha512.Sum512(body)
	return hex.EncodeToString(sum[:])
}

func signaturePayload(method, path, rawQuery, bodyHash, timestamp string) []byte {
	var builder strings.Builder
	builder.Grow(len(method) + len(path) + len(rawQuery) + len(bodyHash) + len(timestamp) + 4)
	builder.WriteString(strings.ToUpper(method))
	builder.WriteByte('\n')
	builder.WriteString(path)
	builder.WriteByte('\n')
	builder.WriteString(rawQuery)
	builder.WriteByte('\n')
	builder.WriteString(bodyHash)
	builder.WriteByte('\n')
	builder.WriteString(timestamp)
	return []byte(builder.String())
}

// SignHMACSHA512는 Gate.io 서명 원문을 HMAC-SHA-512 소문자 16진수로 서명한다.
func SignHMACSHA512(secret, payload []byte) (string, error) {
	if len(secret) == 0 {
		return "", errors.New("Gate.io HMAC secret is empty")
	}
	mac := hmac.New(sha512.New, secret)
	if _, err := mac.Write(payload); err != nil {
		return "", err
	}
	return hex.EncodeToString(mac.Sum(nil)), nil
}
