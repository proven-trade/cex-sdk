package kucoin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

var (
	idPattern    = regexp.MustCompile(`^[0-9A-Za-z_-]{1,64}$`)
	chainPattern = regexp.MustCompile(`^[0-9A-Za-z_-]{1,40}$`)
)

// DecimalString은 KuCoin이 문자열, 숫자, null로 혼용해 내려주는 십진수를 원문 그대로 보존한다.
// null은 빈 문자열이 된다.
type DecimalString string

// UnmarshalJSON은 JSON 문자열 또는 숫자를 float64 변환 없이 문자열로 보존한다.
func (value *DecimalString) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		*value = ""
		return nil
	}
	text, err := decimalText(trimmed)
	if err != nil {
		return err
	}
	*value = DecimalString(text)
	return nil
}

var errEmptyAcknowledgement = errors.New("KuCoin acknowledgement is empty")

// Acknowledgement는 본문 데이터가 의미 없는 성공 접수 응답이다.
// Raw에는 data 원본이 들어가며 null이면 비어 있다.
type Acknowledgement struct {
	Raw json.RawMessage
}

func (client *Client) readPublic(
	ctx context.Context,
	path string,
	query url.Values,
	limit endpointLimit,
	target any,
	options ...trade.RequestOption,
) (json.RawMessage, error) {
	response, err := client.executePublic(ctx, http.MethodGet, path, query, limit, options...)
	if err != nil {
		return nil, err
	}
	return client.decodeData(response, commonexchange.OperationRead, target)
}

func (client *Client) readPrivate(
	ctx context.Context,
	path string,
	query url.Values,
	limit endpointLimit,
	target any,
	options ...trade.RequestOption,
) (json.RawMessage, error) {
	response, err := client.executePrivate(
		ctx, http.MethodGet, path, query, nil, limit,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	return client.decodeData(response, commonexchange.OperationRead, target)
}

// executeMutation은 변경 요청을 한 번만 전송하고 응답 envelope를 그대로 반환한다. 자동 재시도하지 않는다.
func (client *Client) executeMutation(
	ctx context.Context,
	method, path string,
	query url.Values,
	body any,
	limit endpointLimit,
	permission credential.Permission,
	options ...trade.RequestOption,
) (commonexchange.Response, error) {
	var encoded []byte
	if body != nil {
		var err error
		encoded, err = json.Marshal(body)
		if err != nil {
			return commonexchange.Response{}, validationError("encode KuCoin request: %v", err)
		}
	}
	return client.executePrivate(
		ctx, method, path, query, encoded, limit,
		permission, commonexchange.OperationMutation, options...,
	)
}

// mutate는 변경 요청 한 번을 보내고 data를 target으로 디코딩한다.
// 응답 envelope는 접수 응답 검증 오류를 만들 때 쓰도록 함께 반환한다.
func (client *Client) mutate(
	ctx context.Context,
	method, path string,
	query url.Values,
	body any,
	limit endpointLimit,
	permission credential.Permission,
	target any,
	options ...trade.RequestOption,
) (commonexchange.Response, json.RawMessage, error) {
	response, err := client.executeMutation(ctx, method, path, query, body, limit, permission, options...)
	if err != nil {
		return commonexchange.Response{}, nil, err
	}
	data, err := client.decodeData(response, commonexchange.OperationMutation, target)
	return response, data, err
}

func (client *Client) acknowledge(
	ctx context.Context,
	method, path string,
	query url.Values,
	body any,
	limit endpointLimit,
	permission credential.Permission,
	options ...trade.RequestOption,
) (Acknowledgement, error) {
	response, err := client.executeMutation(ctx, method, path, query, body, limit, permission, options...)
	if err != nil {
		return Acknowledgement{}, err
	}
	data, err := client.responseData(response, commonexchange.OperationMutation)
	if err != nil {
		return Acknowledgement{}, err
	}
	if isNullData(data) {
		data = nil
	}
	return Acknowledgement{Raw: cloneBytes(data)}, nil
}

// decodeItems는 최상위 배열 data를 항목별 원본과 함께 디코딩한다.
func decodeItems[T any](
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

func validateID(name, value string) error {
	if !idPattern.MatchString(value) {
		return validationError("invalid %s", name)
	}
	return nil
}

func validateOptionalSymbol(symbol string) error {
	if symbol == "" {
		return nil
	}
	return validateSymbol(symbol)
}

func validateOptionalSide(side Side) error {
	if side != "" && side != SideBuy && side != SideSell {
		return validationError("order side must be buy, sell, or empty")
	}
	return nil
}

func validateOptionalOrderType(orderType OrderType) error {
	if orderType != "" && orderType != OrderTypeLimit && orderType != OrderTypeMarket {
		return validationError("order type must be limit, market, or empty")
	}
	return nil
}

func validateCurrency(currency string) error {
	if !currencyPattern.MatchString(currency) {
		return validationError("invalid currency %q", currency)
	}
	return nil
}

func validateOptionalCurrency(currency string) error {
	if currency == "" {
		return nil
	}
	return validateCurrency(currency)
}

func validateOptionalChain(chain string) error {
	if chain != "" && !chainPattern.MatchString(chain) {
		return validationError("invalid chain %q", chain)
	}
	return nil
}

func validateTimeRange(start, end *time.Time) error {
	if start != nil && start.UnixMilli() <= 0 || end != nil && end.UnixMilli() <= 0 {
		return validationError("time range must be after the Unix epoch")
	}
	if start != nil && end != nil && !start.Before(*end) {
		return validationError("time range start must be before end")
	}
	return nil
}

func validatePageSize(name string, value, minimum, maximum int) error {
	if value == 0 {
		return nil
	}
	if value < minimum || value > maximum {
		return validationError("%s must be between %d and %d or zero for default", name, minimum, maximum)
	}
	return nil
}

func validateIDList(name string, values []string, maximum int) error {
	if len(values) > maximum {
		return validationError("%s cannot exceed %d items", name, maximum)
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if err := validateID(name, value); err != nil {
			return err
		}
		if _, exists := seen[value]; exists {
			return validationError("%s contains a duplicate", name)
		}
		seen[value] = struct{}{}
	}
	return nil
}

func setMillis(values url.Values, key string, value *time.Time) {
	if value != nil {
		values.Set(key, strconv.FormatInt(value.UnixMilli(), 10))
	}
}

func setPositiveInt64(values url.Values, key string, value int64) {
	if value > 0 {
		values.Set(key, strconv.FormatInt(value, 10))
	}
}

func joinList(values []string) string {
	return strings.Join(values, ",")
}

// Page는 currentPage 기반 목록 응답이다. Raw에는 data 원본이 들어간다.
type Page[T any] struct {
	CurrentPage int
	PageSize    int
	TotalNumber int
	TotalPages  int
	Items       []T
	Raw         json.RawMessage
}

func decodePage[T any](
	client *Client,
	response commonexchange.Response,
	operation commonexchange.OperationKind,
	setRaw func(*T, json.RawMessage),
) (Page[T], error) {
	var wire struct {
		CurrentPage int               `json:"currentPage"`
		PageSize    int               `json:"pageSize"`
		TotalNumber int               `json:"totalNum"`
		TotalPages  int               `json:"totalPage"`
		Items       []json.RawMessage `json:"items"`
	}
	data, err := client.decodeData(response, operation, &wire)
	if err != nil {
		return Page[T]{}, err
	}
	page := Page[T]{
		CurrentPage: wire.CurrentPage, PageSize: wire.PageSize,
		TotalNumber: wire.TotalNumber, TotalPages: wire.TotalPages,
		Items: make([]T, len(wire.Items)), Raw: cloneBytes(data),
	}
	for index, raw := range wire.Items {
		if err := json.Unmarshal(raw, &page.Items[index]); err != nil {
			return Page[T]{}, client.decodeBodyError(response, operation, err)
		}
		setRaw(&page.Items[index], cloneBytes(raw))
	}
	return page, nil
}

// validatePage는 currentPage/pageSize 공통 조회 조건을 검증한다.
func validatePage(currentPage, pageSize, minimum, maximum int) error {
	if currentPage < 0 {
		return validationError("current page cannot be negative")
	}
	return validatePageSize("page size", pageSize, minimum, maximum)
}
