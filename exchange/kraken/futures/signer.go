// Package futures는 Kraken Futures REST API 어댑터를 제공한다.
package futures

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"fmt"
	"strings"
)

// SignAuthent는 Kraken Futures private REST 요청의 Authent 값을 생성한다.
func SignAuthent(postData, nonce, endpointPath string, encodedSecret []byte) (string, error) {
	if !strings.HasPrefix(endpointPath, "/api/v3/") || strings.ContainsAny(endpointPath, "?#\r\n") {
		return "", fmt.Errorf("Kraken Futures endpoint path is invalid")
	}
	if strings.TrimSpace(nonce) == "" {
		return "", fmt.Errorf("Kraken Futures nonce is required")
	}
	secret, err := base64.StdEncoding.DecodeString(string(encodedSecret))
	if err != nil {
		return "", fmt.Errorf("decode Kraken Futures API secret: %w", err)
	}
	if len(secret) == 0 {
		return "", fmt.Errorf("Kraken Futures API secret is required")
	}
	defer clear(secret)
	digest := sha256.Sum256([]byte(postData + nonce + endpointPath))
	mac := hmac.New(sha512.New, secret)
	if _, err := mac.Write(digest[:]); err != nil {
		return "", fmt.Errorf("write Kraken Futures HMAC payload: %w", err)
	}
	return base64.StdEncoding.EncodeToString(mac.Sum(nil)), nil
}
