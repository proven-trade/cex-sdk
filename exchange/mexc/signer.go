package mexc

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// SignHMACSHA256은 MEXC totalParams 서명의 lowercase hex 값을 반환한다.
func SignHMACSHA256(secret, payload []byte) (string, error) {
	if len(secret) == 0 {
		return "", fmt.Errorf("MEXC HMAC secret is required")
	}
	mac := hmac.New(sha256.New, secret)
	if _, err := mac.Write(payload); err != nil {
		return "", fmt.Errorf("sign MEXC payload: %w", err)
	}
	return hex.EncodeToString(mac.Sum(nil)), nil
}
