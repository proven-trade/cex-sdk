package kraken

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

var (
	assetNamePattern = regexp.MustCompile(`^[0-9A-Za-z._-]+$`)
	// opaqueTokenPattern은 Kraken이 발급한 export, 출금, 배분 등 참조 ID와 cursor에 쓰는 안전한 문자 집합이다.
	opaqueTokenPattern = regexp.MustCompile(`^[0-9A-Za-z._=+/-]+$`)
)

// AssetClass는 Kraken 자산 분류다.
type AssetClass string

const (
	AssetClassCurrency       AssetClass = "currency"
	AssetClassTokenizedAsset AssetClass = "tokenized_asset"
)

// DecimalString은 Kraken이 JSON 문자열 또는 숫자로 내려주는 수치를 정밀도 손실 없이 원문 그대로 보존한다.
// null과 false는 값이 없다는 뜻이므로 빈 문자열이 된다. 예를 들어 입금 한도 limit은 한도가 없으면 false다.
type DecimalString string

// UnmarshalJSON은 JSON 문자열과 숫자를 모두 문자열로 읽는다.
func (value *DecimalString) UnmarshalJSON(data []byte) error {
	switch string(data) {
	case "null", "false":
		*value = ""
		return nil
	case "true":
		*value = "true"
		return nil
	}
	text, err := scalarString(data)
	if err != nil {
		return err
	}
	*value = DecimalString(text)
	return nil
}

// publicResult는 공개 GET 요청을 실행하고 Kraken envelope의 result를 반환한다.
func (client *Client) publicResult(
	ctx context.Context,
	path string,
	query url.Values,
	options ...trade.RequestOption,
) (commonexchange.Response, json.RawMessage, error) {
	response, err := client.executePublic(ctx, path, query, options...)
	if err != nil {
		return commonexchange.Response{}, nil, err
	}
	result, err := client.decodeResult(response, commonexchange.OperationRead)
	if err != nil {
		return commonexchange.Response{}, nil, err
	}
	return response, result, nil
}

// privateResult는 form 본문 private 요청을 실행하고 Kraken envelope의 result를 반환한다.
func (client *Client) privateResult(
	ctx context.Context,
	path string,
	values url.Values,
	permission credential.Permission,
	operation commonexchange.OperationKind,
	limit limitKind,
	pair string,
	options ...trade.RequestOption,
) (commonexchange.Response, json.RawMessage, error) {
	response, err := client.executePrivate(
		ctx, path, values, permission, operation, limit, pair, options...,
	)
	if err != nil {
		return commonexchange.Response{}, nil, err
	}
	result, err := client.decodeResult(response, operation)
	if err != nil {
		return commonexchange.Response{}, nil, err
	}
	return response, result, nil
}

// privateJSONResult는 JSON 본문 private 요청을 실행하고 Kraken envelope의 result를 반환한다.
func (client *Client) privateJSONResult(
	ctx context.Context,
	path string,
	body map[string]any,
	permission credential.Permission,
	operation commonexchange.OperationKind,
	limit limitKind,
	pair string,
	options ...trade.RequestOption,
) (commonexchange.Response, json.RawMessage, error) {
	response, err := client.executePrivateJSON(
		ctx, path, body, permission, operation, limit, pair, options...,
	)
	if err != nil {
		return commonexchange.Response{}, nil, err
	}
	result, err := client.decodeResult(response, operation)
	if err != nil {
		return commonexchange.Response{}, nil, err
	}
	return response, result, nil
}

// decodeResultInto는 result를 target으로 decode하고 실패하면 본문 decode 오류로 분류한다.
func (client *Client) decodeResultInto(
	response commonexchange.Response,
	operation commonexchange.OperationKind,
	result json.RawMessage,
	target any,
) error {
	if err := json.Unmarshal(result, target); err != nil {
		return client.decodeBodyError(response, operation, err)
	}
	return nil
}

// keyedEntry는 Kraken이 ID를 객체 key로 내려주는 항목 하나와 원문이다.
type keyedEntry[T any] struct {
	Key   string
	Value T
	Raw   json.RawMessage
}

// decodeKeyedEntries는 result 객체의 각 항목을 key 순서대로 T로 decode한다.
func decodeKeyedEntries[T any](result json.RawMessage) ([]keyedEntry[T], error) {
	var entries map[string]json.RawMessage
	if err := json.Unmarshal(result, &entries); err != nil {
		return nil, err
	}
	keys := sortedKeys(entries)
	items := make([]keyedEntry[T], len(keys))
	for index, key := range keys {
		items[index].Key = key
		items[index].Raw = cloneBytes(entries[key])
		if err := json.Unmarshal(entries[key], &items[index].Value); err != nil {
			return nil, err
		}
	}
	return items, nil
}

// decodeNullableResult는 성공 응답의 result가 null이면 nil을 반환하고, 그 외에는 decodeResult와 같은 규칙을 적용한다.
func (client *Client) decodeNullableResult(
	response commonexchange.Response,
	operation commonexchange.OperationKind,
) (json.RawMessage, error) {
	var envelope struct {
		Errors []string        `json:"error"`
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(response.Body, &envelope); err == nil && len(envelope.Errors) == 0 &&
		response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices &&
		(len(envelope.Result) == 0 || string(envelope.Result) == "null") {
		return nil, nil
	}
	return client.decodeResult(response, operation)
}

// scalarReader는 JSON 객체의 숫자 또는 문자열 필드를 정밀도 손실 없이 문자열로 읽는다.
// 처음 발생한 오류를 보관하고 이후 read를 무시하므로 호출자는 마지막에 err만 확인한다.
type scalarReader struct {
	fields map[string]json.RawMessage
	err    error
}

func newScalarReader(data []byte) (*scalarReader, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	return &scalarReader{fields: fields}, nil
}

// read는 names 중 먼저 존재하는 key의 값을 target에 채운다. null과 부재는 건너뛴다.
func (reader *scalarReader) read(target *string, names ...string) {
	if reader.err != nil {
		return
	}
	for _, name := range names {
		raw, exists := reader.fields[name]
		if !exists || string(raw) == "null" {
			continue
		}
		value, err := scalarString(raw)
		if err != nil {
			reader.err = err
			return
		}
		*target = value
		return
	}
}

// validateIDList는 중복 없는 거래소 ID 목록의 개수와 문자 집합을 확인한다.
func validateIDList(name string, ids []string, minimum, maximum int) error {
	if len(ids) < minimum || len(ids) > maximum {
		return validationError("%s count must be between %d and %d", name, minimum, maximum)
	}
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if !transactionIDPattern.MatchString(id) {
			return validationError("%s contains unsupported characters", name)
		}
		if _, exists := seen[id]; exists {
			return validationError("duplicate %s %q", name, id)
		}
		seen[id] = struct{}{}
	}
	return nil
}

func validateAssetNames(name string, assets []string, required bool) error {
	if required && len(assets) == 0 {
		return validationError("at least one %s is required", name)
	}
	seen := make(map[string]struct{}, len(assets))
	for _, asset := range assets {
		if !assetNamePattern.MatchString(asset) {
			return validationError("%s contains unsupported characters", name)
		}
		if _, exists := seen[asset]; exists {
			return validationError("duplicate %s %q", name, asset)
		}
		seen[asset] = struct{}{}
	}
	return nil
}

func validateAssetName(name, value string) error {
	if !assetNamePattern.MatchString(value) {
		return validationError("%s is required and contains unsupported characters", name)
	}
	return nil
}

func validateOpaqueToken(name, value string) error {
	if !opaqueTokenPattern.MatchString(value) {
		return validationError("%s is required and contains unsupported characters", name)
	}
	return nil
}

func validateAssetClass(value AssetClass) error {
	if value != "" && value != AssetClassCurrency && value != AssetClassTokenizedAsset {
		return validationError("unsupported asset class %q", value)
	}
	return nil
}

// validateDecimalAmount는 양수 decimal 문자열인지 확인한다.
func validateDecimalAmount(name, value string) error {
	if !isPositiveDecimal(value) {
		return validationError("%s must be a positive decimal string", name)
	}
	return nil
}

func validateOptionalDecimal(name, value string) error {
	if value == "" {
		return nil
	}
	return validateDecimalAmount(name, value)
}

// validateDeadline은 RFC3339 deadline 문자열을 확인한다.
func validateDeadline(value string) error {
	if value == "" {
		return nil
	}
	if _, err := time.Parse(time.RFC3339, value); err != nil {
		return validationError("deadline must be an RFC3339 timestamp")
	}
	return nil
}

func setAssetClass(values url.Values, key string, value AssetClass) {
	if value != "" {
		values.Set(key, string(value))
	}
}

func setRFC3339Nano(values url.Values, key string, value *time.Time) {
	if value != nil {
		values.Set(key, value.UTC().Format(time.RFC3339Nano))
	}
}
