package cryptocom

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

// 파생·지수 이름(BTCUSD-PERP, BTCUSD-INDEX)과 Spot 거래쌍을 모두 허용하는 상품 이름 형식이다.
var symbolPattern = regexp.MustCompile(`^[A-Z0-9][A-Z0-9_-]{0,39}$`)

var currencyPattern = regexp.MustCompile(`^[A-Z0-9]{1,20}$`)

var networkIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)

// Acknowledgement는 result 본문이 없는 비동기 요청 접수 응답이다.
type Acknowledgement struct {
	Raw json.RawMessage
}

// callPrivate는 private 요청을 실행하고 envelope 검증을 마친 result 원문을 반환한다.
func (client *Client) callPrivate(
	ctx context.Context,
	method string,
	params map[string]any,
	permission credential.Permission,
	operation commonexchange.OperationKind,
	resultRequired bool,
	options ...trade.RequestOption,
) (commonexchange.Response, string, json.RawMessage, error) {
	response, requestID, err := client.executePrivate(
		ctx, method, params, permission, operation, options...,
	)
	if err != nil {
		return commonexchange.Response{}, "", nil, err
	}
	resultRaw, err := client.decodePrivateResult(
		response, method, requestID, operation, resultRequired,
	)
	if err != nil {
		return commonexchange.Response{}, "", nil, err
	}
	return response, requestID, resultRaw, nil
}

// decodePrivateArray는 result 자체가 배열인 응답을 항목별 원문으로 나눈다.
func (client *Client) decodePrivateArray(
	response commonexchange.Response,
	requestID string,
	operation commonexchange.OperationKind,
	resultRaw json.RawMessage,
) ([]json.RawMessage, error) {
	trimmed := bytes.TrimSpace(resultRaw)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return nil, client.decodePrivateBodyError(
			response, operation, requestID, errors.New("Crypto.com private result is not an array"),
		)
	}
	var items []json.RawMessage
	if err := json.Unmarshal(trimmed, &items); err != nil {
		return nil, client.decodePrivateBodyError(response, operation, requestID, err)
	}
	return items, nil
}

// decodePublicItems는 result가 배열이거나 data 배열을 가진 공개 응답을 모두 받는다.
func (client *Client) decodePublicItems(
	response commonexchange.Response,
	resultRaw json.RawMessage,
) ([]json.RawMessage, error) {
	trimmed := bytes.TrimSpace(resultRaw)
	if len(trimmed) > 0 && trimmed[0] == '[' {
		var items []json.RawMessage
		if err := json.Unmarshal(trimmed, &items); err != nil {
			return nil, client.decodeBodyError(response, err)
		}
		return items, nil
	}
	return client.decodeDataItems(response, resultRaw)
}

// decodeItems는 항목 원문을 T로 해석하고 setRaw로 원본을 보존한다.
func decodeItems[T any](
	items []json.RawMessage,
	setRaw func(*T, json.RawMessage),
) ([]T, error) {
	values := make([]T, len(items))
	for index, itemRaw := range items {
		if err := json.Unmarshal(itemRaw, &values[index]); err != nil {
			return nil, err
		}
		setRaw(&values[index], cloneBytes(itemRaw))
	}
	return values, nil
}

// validateSymbol은 Spot·파생·지수 상품 이름을 검사한다.
func validateSymbol(value string) error {
	if !symbolPattern.MatchString(value) {
		return validationError("invalid Crypto.com instrument name %q", value)
	}
	return nil
}

func validateOptionalSymbol(value string) error {
	if value == "" {
		return nil
	}
	return validateSymbol(value)
}

// validateNumericID는 int64 범위의 양수 십진 ID 문자열을 검사한다.
func validateNumericID(name, value string) error {
	if !orderIDPattern.MatchString(value) {
		return validationError("invalid Crypto.com %s %q", name, value)
	}
	parsed, err := strconv.ParseUint(value, 10, 63)
	if err != nil || parsed == 0 {
		return validationError("Crypto.com %s must be a positive int64", name)
	}
	return nil
}

func validateNonNegativeDecimal(name, value string) error {
	if !positiveDecimalPattern.MatchString(value) {
		return validationError("%s must be a non-negative decimal string", name)
	}
	number, ok := new(big.Rat).SetString(value)
	if !ok || number.Sign() < 0 {
		return validationError("%s must not be negative", name)
	}
	return nil
}

// validateOptionalText는 비어 있거나 제어 문자 없는 길이 제한 문자열인지 검사한다.
func validateOptionalText(name, value string, maximumBytes int) error {
	if value == "" {
		return nil
	}
	if len(value) > maximumBytes || !utf8.ValidString(value) || strings.TrimSpace(value) != value {
		return validationError("Crypto.com %s must contain 1 to %d unpadded bytes", name, maximumBytes)
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return validationError("Crypto.com %s cannot contain control characters", name)
		}
	}
	return nil
}

// setPageParams는 입출금 이력의 시간 범위와 0부터 시작하는 page 조건을 params에 넣는다.
func setPageParams(params map[string]any, start, end *time.Time, pageSize, page int) {
	if start != nil {
		params["start_ts"] = strconv.FormatInt(start.UnixMilli(), 10)
	}
	if end != nil {
		params["end_ts"] = strconv.FormatInt(end.UnixMilli(), 10)
	}
	if pageSize > 0 {
		params["page_size"] = strconv.Itoa(pageSize)
	}
	if page > 0 {
		params["page"] = strconv.Itoa(page)
	}
}
