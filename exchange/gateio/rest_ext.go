package gateio

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"time"

	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

// rawHolder는 원본 JSON을 보존하는 Raw 필드를 가진 응답 타입의 포인터 제약이다.
type rawHolder[T any] interface {
	*T
	setRaw(json.RawMessage)
}

// decodeObject는 JSON 객체 응답을 T로 해석하고 원본 JSON을 Raw에 보존한다.
func decodeObject[T any, P rawHolder[T]](
	client *Client,
	response commonexchange.Response,
	operation commonexchange.OperationKind,
) (T, error) {
	var value T
	data, err := client.decodeData(response, operation, &value)
	if err != nil {
		var zero T
		return zero, err
	}
	P(&value).setRaw(data)
	return value, nil
}

// decodeList는 JSON 배열 응답을 T 목록으로 해석하고 항목별 원본 JSON을 Raw에 보존한다.
func decodeList[T any, P rawHolder[T]](
	client *Client,
	response commonexchange.Response,
	operation commonexchange.OperationKind,
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
		P(&items[index]).setRaw(cloneBytes(raw))
	}
	return items, nil
}

// decodeEmpty는 본문이 없는 성공 응답을 확인한다. 2xx가 아니면 오류 응답으로 해석한다.
func (client *Client) decodeEmpty(
	response commonexchange.Response,
	operation commonexchange.OperationKind,
) error {
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return client.errorResponse(response, operation)
	}
	return nil
}

func encodeBody(value any) ([]byte, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return nil, validationError("encode Gate.io request body: %v", err)
	}
	return body, nil
}

func validateTimeRange(from, to *time.Time) error {
	if from != nil && from.Unix() <= 0 || to != nil && to.Unix() <= 0 {
		return validationError("timestamps must be after the Unix epoch")
	}
	if from != nil && to != nil && !from.Before(*to) {
		return validationError("from must be before to")
	}
	return nil
}

func setUnixTime(values url.Values, key string, value *time.Time) {
	if value != nil {
		values.Set(key, strconv.FormatInt(value.Unix(), 10))
	}
}

func validateOffsetLimit(offset, limit, maxLimit int) error {
	if offset < 0 {
		return validationError("offset cannot be negative")
	}
	if limit < 0 || limit > maxLimit {
		return validationError("limit must be between 1 and %d or zero", maxLimit)
	}
	return nil
}

func validateOptionalCurrency(value string) error {
	if value != "" && !currencyPattern.MatchString(value) {
		return validationError("invalid currency %q", value)
	}
	return nil
}

func validateOptionalSide(side Side) error {
	if side != "" && side != SideBuy && side != SideSell {
		return validationError("order side must be buy or sell")
	}
	return nil
}
