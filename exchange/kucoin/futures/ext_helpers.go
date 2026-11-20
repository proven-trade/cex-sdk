package futures

import (
	"bytes"
	"encoding/json"
	"net/url"
	"regexp"
	"strconv"
	"time"

	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

// indexSymbolPattern은 `.KXBTUSDT`처럼 점으로 시작할 수 있는 Futures 지수 symbol 형식이다.
var indexSymbolPattern = regexp.MustCompile(`^\.?[A-Z0-9]{2,30}$`)

func validateIndexSymbol(symbol string) error {
	if !indexSymbolPattern.MatchString(symbol) {
		return validationError("invalid Futures index symbol %q", symbol)
	}
	return nil
}

// decodeObject는 data 객체 하나를 T로 해석하고 setRaw로 원본 JSON을 보존한다.
func decodeObject[T any](
	client *Client,
	response commonexchange.Response,
	operation commonexchange.OperationKind,
	setRaw func(*T, json.RawMessage),
) (T, error) {
	var value T
	data, err := client.decodeData(response, operation, &value)
	if err != nil {
		var zero T
		return zero, err
	}
	setRaw(&value, cloneBytes(data))
	return value, nil
}

// decodeList는 data 배열의 각 항목을 T로 해석하고 항목별 원본 JSON을 보존한다.
func decodeList[T any](
	client *Client,
	response commonexchange.Response,
	operation commonexchange.OperationKind,
	setRaw func(*T, json.RawMessage),
) ([]T, error) {
	var rawItems []json.RawMessage
	if _, err := client.decodeData(response, operation, &rawItems); err != nil {
		return nil, err
	}
	items := make([]T, len(rawItems))
	for index, raw := range rawItems {
		if err := json.Unmarshal(raw, &items[index]); err != nil {
			return nil, client.decodeBodyError(response, operation, err)
		}
		setRaw(&items[index], cloneBytes(raw))
	}
	return items, nil
}

// decodeRawItems는 이미 envelope에서 꺼낸 배열 항목을 T로 해석하고 항목별 원본 JSON을 보존한다.
func decodeRawItems[T any](
	client *Client,
	response commonexchange.Response,
	operation commonexchange.OperationKind,
	rawItems []json.RawMessage,
	setRaw func(*T, json.RawMessage),
) ([]T, error) {
	items := make([]T, len(rawItems))
	for index, raw := range rawItems {
		if err := json.Unmarshal(raw, &items[index]); err != nil {
			return nil, client.decodeBodyError(response, operation, err)
		}
		setRaw(&items[index], cloneBytes(raw))
	}
	return items, nil
}

// decodeBool은 data가 단일 boolean인 성공 응답을 해석한다.
func decodeBool(
	client *Client,
	response commonexchange.Response,
	operation commonexchange.OperationKind,
) (bool, error) {
	var value bool
	if _, err := client.decodeData(response, operation, &value); err != nil {
		return false, err
	}
	return value, nil
}

// firstJSONByte는 공백을 제외한 JSON 값의 첫 바이트를 반환한다.
func firstJSONByte(data []byte) byte {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return 0
	}
	return trimmed[0]
}

func setMillis(values url.Values, key string, value *time.Time) {
	if value != nil {
		values.Set(key, strconv.FormatInt(value.UnixMilli(), 10))
	}
}

func setBool(values url.Values, key string, value *bool) {
	if value != nil {
		values.Set(key, strconv.FormatBool(*value))
	}
}

func setPositiveInt64(values url.Values, key string, value int64) {
	if value > 0 {
		values.Set(key, strconv.FormatInt(value, 10))
	}
}

func validateTimeRange(name string, start, end *time.Time) error {
	for _, value := range []*time.Time{start, end} {
		if value != nil && value.UnixMilli() <= 0 {
			return validationError("%s time must be after the Unix epoch", name)
		}
	}
	if start != nil && end != nil && !start.Before(*end) {
		return validationError("%s start must be before end", name)
	}
	return nil
}
