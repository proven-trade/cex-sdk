package cryptocom

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

const maximumParameterDepth = 32

// ParamsString은 Crypto.com 서명 규칙에 따라 객체 key를 정렬하고 배열 순서를 유지해 연결한다.
func ParamsString(params map[string]any) (string, error) {
	var result bytes.Buffer
	if err := appendParameterValue(&result, params, 0); err != nil {
		return "", err
	}
	return result.String(), nil
}

// Sign은 Crypto.com private 요청을 HMAC SHA-256 소문자 hex로 서명한다.
func Sign(
	method string,
	id string,
	apiKey []byte,
	params map[string]any,
	nonce string,
	secret []byte,
) (string, error) {
	if strings.TrimSpace(method) == "" || len(apiKey) == 0 || len(secret) == 0 {
		return "", fmt.Errorf("Crypto.com signing method, API key, and secret are required")
	}
	if err := validateUnsignedIntegerString(id, true); err != nil {
		return "", fmt.Errorf("invalid Crypto.com request ID: %w", err)
	}
	if err := validateUnsignedIntegerString(nonce, false); err != nil {
		return "", fmt.Errorf("invalid Crypto.com nonce: %w", err)
	}
	parameterString, err := ParamsString(params)
	if err != nil {
		return "", err
	}
	payload := make([]byte, 0, len(method)+len(id)+len(apiKey)+len(parameterString)+len(nonce))
	payload = append(payload, method...)
	payload = append(payload, id...)
	payload = append(payload, apiKey...)
	payload = append(payload, parameterString...)
	payload = append(payload, nonce...)
	defer zeroBytes(payload)

	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil)), nil
}

func appendParameterValue(result *bytes.Buffer, value any, depth int) error {
	if depth > maximumParameterDepth {
		return fmt.Errorf("Crypto.com signing parameters exceed maximum nesting depth")
	}
	switch typed := value.(type) {
	case nil:
		result.WriteString("null")
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			result.WriteString(key)
			if err := appendParameterValue(result, typed[key], depth+1); err != nil {
				return err
			}
		}
	case []any:
		for _, item := range typed {
			if err := appendParameterValue(result, item, depth+1); err != nil {
				return err
			}
		}
	case []string:
		for _, item := range typed {
			result.WriteString(item)
		}
	case string:
		result.WriteString(typed)
	case bool:
		result.WriteString(strconv.FormatBool(typed))
	default:
		return fmt.Errorf("unsupported Crypto.com signing parameter type %T", value)
	}
	return nil
}

func validateUnsignedIntegerString(value string, allowZero bool) error {
	if value == "" || strings.TrimSpace(value) != value {
		return fmt.Errorf("integer string is empty or padded")
	}
	parsed, err := strconv.ParseUint(value, 10, 63)
	if err != nil {
		return err
	}
	if !allowZero && parsed == 0 {
		return fmt.Errorf("integer must be positive")
	}
	return nil
}

func zeroBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
