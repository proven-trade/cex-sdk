package upbit

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

type parameter struct {
	key   string
	value string
}

type parameters []parameter

func (values parameters) encoded() string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		key := strings.ReplaceAll(url.QueryEscape(value.key), "%5B%5D", "[]")
		parts = append(parts, key+"="+url.QueryEscape(value.value))
	}
	return strings.Join(parts, "&")
}

func (values parameters) hashString() (string, error) {
	decoded, err := url.PathUnescape(values.encoded())
	if err != nil {
		return "", fmt.Errorf("decode Upbit query hash input: %w", err)
	}
	return decoded, nil
}

func (values *parameters) add(key, value string) {
	if value != "" {
		*values = append(*values, parameter{key: key, value: value})
	}
}

func (values *parameters) addInt(key string, value int) {
	if value > 0 {
		values.add(key, strconv.Itoa(value))
	}
}

func (values *parameters) addBool(key string, value bool) {
	if value {
		values.add(key, "true")
	}
}
