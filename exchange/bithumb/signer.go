package bithumb

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

var jwtHeader = base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))

// QueryHash는 빗썸 인증에 사용하는 SHA-512 쿼리 해시를 반환한다.
func QueryHash(query string) string {
	digest := sha512.Sum512([]byte(query))
	return hex.EncodeToString(digest[:])
}

// SignJWT는 Access Key와 Secret Key로 빗썸 HS256 JWT를 만든다.
func SignJWT(accessKey, secretKey []byte, nonce string, timestamp int64, query string) (string, error) {
	if len(accessKey) == 0 || len(secretKey) == 0 {
		return "", fmt.Errorf("Bithumb access key and secret key are required")
	}
	if nonce == "" {
		return "", fmt.Errorf("Bithumb JWT nonce is required")
	}
	if timestamp <= 0 {
		return "", fmt.Errorf("Bithumb JWT timestamp must be positive")
	}
	payload := struct {
		AccessKey    string `json:"access_key"`
		Nonce        string `json:"nonce"`
		Timestamp    int64  `json:"timestamp"`
		QueryHash    string `json:"query_hash,omitempty"`
		QueryHashAlg string `json:"query_hash_alg,omitempty"`
	}{
		AccessKey: string(accessKey),
		Nonce:     nonce,
		Timestamp: timestamp,
	}
	if query != "" {
		payload.QueryHash = QueryHash(query)
		payload.QueryHashAlg = "SHA512"
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode Bithumb JWT payload: %w", err)
	}
	defer clear(payloadJSON)
	unsigned := jwtHeader + "." + base64.RawURLEncoding.EncodeToString(payloadJSON)
	mac := hmac.New(sha256.New, secretKey)
	_, _ = mac.Write([]byte(unsigned))
	signatureBytes := mac.Sum(nil)
	defer clear(signatureBytes)
	signature := base64.RawURLEncoding.EncodeToString(signatureBytes)
	return unsigned + "." + signature, nil
}

func randomNonce() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate Bithumb JWT nonce: %w", err)
	}
	value[6] = value[6]&0x0f | 0x40
	value[8] = value[8]&0x3f | 0x80
	return fmt.Sprintf(
		"%08x-%04x-%04x-%04x-%012x",
		value[0:4], value[4:6], value[6:8], value[8:10], value[10:16],
	), nil
}
