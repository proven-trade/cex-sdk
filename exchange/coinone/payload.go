package coinone

import (
	"bytes"
	"encoding/json"
	"fmt"
)

type payloadField struct {
	key   string
	value any
}

type payloadFields []payloadField

func (fields *payloadFields) addString(key, value string) {
	if value != "" {
		*fields = append(*fields, payloadField{key: key, value: value})
	}
}

func (fields *payloadFields) addBool(key string, value bool) {
	*fields = append(*fields, payloadField{key: key, value: value})
}

func (fields *payloadFields) addInt(key string, value int) {
	if value > 0 {
		*fields = append(*fields, payloadField{key: key, value: value})
	}
}

func (fields *payloadFields) addInt64(key string, value int64) {
	if value > 0 {
		*fields = append(*fields, payloadField{key: key, value: value})
	}
}

func (fields *payloadFields) addStrings(key string, values []string) {
	if len(values) > 0 {
		copyValues := append([]string(nil), values...)
		*fields = append(*fields, payloadField{key: key, value: copyValues})
	}
}

func encodePrivatePayload(accessToken []byte, nonce string, fields payloadFields) ([]byte, error) {
	if len(accessToken) == 0 {
		return nil, fmt.Errorf("Coinone access token is required")
	}
	if nonce == "" {
		return nil, fmt.Errorf("Coinone nonce is required")
	}
	allFields := make(payloadFields, 0, len(fields)+2)
	allFields = append(allFields,
		payloadField{key: "access_token", value: string(accessToken)},
		payloadField{key: "nonce", value: nonce},
	)
	allFields = append(allFields, fields...)
	buffer := bytes.NewBufferString("{")
	for index, field := range allFields {
		if index > 0 {
			buffer.WriteByte(',')
		}
		key, err := json.Marshal(field.key)
		if err != nil {
			return nil, fmt.Errorf("encode Coinone payload key: %w", err)
		}
		value, err := json.Marshal(field.value)
		if err != nil {
			return nil, fmt.Errorf("encode Coinone payload value: %w", err)
		}
		buffer.Write(key)
		buffer.WriteByte(':')
		buffer.Write(value)
	}
	buffer.WriteByte('}')
	return buffer.Bytes(), nil
}
