package bybit

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// SignHMACSHA256은 Bybit V5 HMAC API Key 서명을 소문자 16진수로 생성한다.
func SignHMACSHA256(secret, payload []byte) (string, error) {
	if len(secret) == 0 {
		return "", fmt.Errorf("Bybit HMAC secret is required")
	}
	mac := hmac.New(sha256.New, secret)
	if _, err := mac.Write(payload); err != nil {
		return "", fmt.Errorf("write Bybit HMAC payload: %w", err)
	}
	return hex.EncodeToString(mac.Sum(nil)), nil
}

func signaturePayload(timestamp string, apiKey []byte, receiveWindow int64, content []byte) []byte {
	prefix := timestamp + string(apiKey) + fmt.Sprintf("%d", receiveWindow)
	payload := make([]byte, 0, len(prefix)+len(content))
	payload = append(payload, prefix...)
	payload = append(payload, content...)
	return payload
}
