package htx

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"
)

// SignaturePayload는 HTTP 메서드·호스트·경로·정규 쿼리를 HTX 서명 원문으로 결합한다.
func SignaturePayload(method, host, path, query string) []byte {
	return []byte(strings.ToUpper(method) + "\n" + strings.ToLower(host) + "\n" + path + "\n" + query)
}

// SignHMACSHA256Base64는 HTX 서명의 표준 Base64 값을 반환한다.
func SignHMACSHA256Base64(secret, payload []byte) (string, error) {
	if len(secret) == 0 {
		return "", fmt.Errorf("HTX HMAC secret is required")
	}
	mac := hmac.New(sha256.New, secret)
	if _, err := mac.Write(payload); err != nil {
		return "", fmt.Errorf("sign HTX payload: %w", err)
	}
	return base64.StdEncoding.EncodeToString(mac.Sum(nil)), nil
}

func canonicalQuery(values url.Values) string {
	return strings.ReplaceAll(values.Encode(), "+", "%20")
}
