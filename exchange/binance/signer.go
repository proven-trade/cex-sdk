package binance

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// SignHMACSHA256은 Binance HMAC API Key용 서명을 16진수 문자열로 생성한다.
func SignHMACSHA256(secret, payload []byte) (string, error) {
	if len(secret) == 0 {
		return "", fmt.Errorf("Binance HMAC secret key is empty")
	}
	hash := hmac.New(sha256.New, secret)
	if _, err := hash.Write(payload); err != nil {
		return "", fmt.Errorf("write Binance HMAC payload: %w", err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
