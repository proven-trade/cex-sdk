package bithumb

import (
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

func (values parameters) hashString() string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, value.key+"="+value.value)
	}
	return strings.Join(parts, "&")
}

func (values *parameters) add(key, value string) {
	if value != "" {
		*values = append(*values, parameter{key: key, value: value})
	}
}

// addAll은 `uuids[]`처럼 반복되는 배열 key를 값마다 하나씩 순서대로 추가한다.
func (values *parameters) addAll(key string, items []string) {
	for _, item := range items {
		values.add(key, item)
	}
}

func (values *parameters) addInt(key string, value int) {
	if value > 0 {
		values.add(key, strconv.Itoa(value))
	}
}

func (values *parameters) addInt64(key string, value int64) {
	if value > 0 {
		values.add(key, strconv.FormatInt(value, 10))
	}
}

func (values *parameters) addBool(key string, value bool) {
	if value {
		values.add(key, "true")
	}
}
