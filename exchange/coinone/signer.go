package coinone

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

// SignPayload는 JSON 본문을 Base64로 만들고 Secret Key로 HMAC-SHA512 hex 서명한다.
func SignPayload(secretKey, body []byte) (string, string, error) {
	if len(secretKey) == 0 {
		return "", "", fmt.Errorf("Coinone secret key is required")
	}
	if len(body) == 0 {
		return "", "", fmt.Errorf("Coinone payload body is required")
	}
	encoded := base64.StdEncoding.EncodeToString(body)
	mac := hmac.New(sha512.New, secretKey)
	_, _ = mac.Write([]byte(encoded))
	digest := mac.Sum(nil)
	defer clear(digest)
	return encoded, hex.EncodeToString(digest), nil
}

func randomNonce() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate Coinone nonce: %w", err)
	}
	value[6] = value[6]&0x0f | 0x40
	value[8] = value[8]&0x3f | 0x80
	return fmt.Sprintf(
		"%08x-%04x-%04x-%04x-%012x",
		value[0:4], value[4:6], value[6:8], value[8:10], value[10:16],
	), nil
}
