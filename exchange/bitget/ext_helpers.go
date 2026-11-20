package bitget

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

// noRangeLimit는 공식 문서가 조회 구간 상한을 명시하지 않은 endpoint의 검증용 상한이다.
const noRangeLimit = time.Duration(1<<63 - 1)

// Page는 cursor 기반 이력 조회의 공통 조건이다.
type Page struct {
	StartTime *time.Time
	EndTime   *time.Time
	Limit     int
	Cursor    string
}

// apply는 검증 후 page 조건을 query에 기록한다.
func (page Page) apply(values url.Values) error {
	if err := validatePage(page.StartTime, page.EndTime, noRangeLimit, page.Limit, page.Cursor); err != nil {
		return err
	}
	setTime(values, "startTime", page.StartTime)
	setTime(values, "endTime", page.EndTime)
	setPositiveInt(values, "limit", page.Limit)
	setIfNotEmpty(values, "cursor", page.Cursor)
	return nil
}

// queryValues는 key, value 쌍에서 빈 값을 제외한 query를 만든다.
func queryValues(pairs ...string) url.Values {
	values := make(url.Values)
	for index := 0; index+1 < len(pairs); index += 2 {
		setIfNotEmpty(values, pairs[index], pairs[index+1])
	}
	return values
}

func requireText(name, value string) error {
	if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value {
		return validationError("%s is required and cannot have surrounding whitespace", name)
	}
	return nil
}

func validateOptionalText(name, value string) error {
	if value == "" {
		return nil
	}
	return requireText(name, value)
}

func validateOptionalDecimal(name, value string) error {
	if value == "" {
		return nil
	}
	return validatePositiveDecimal(name, value)
}

// publicData는 인증 없는 GET endpoint의 data 필드를 반환한다.
func (client *Client) publicData(
	ctx context.Context,
	path string,
	query url.Values,
	options ...trade.RequestOption,
) (json.RawMessage, error) {
	response, _, err := client.executePublic(ctx, http.MethodGet, path, query, publicLimit(20), options...)
	if err != nil {
		return nil, err
	}
	data, _, err := client.responseData(response, commonexchange.OperationRead)
	return data, err
}

// readData는 Read 권한 GET endpoint의 data 필드를 반환한다.
func (client *Client) readData(
	ctx context.Context,
	path string,
	query url.Values,
	options ...trade.RequestOption,
) (json.RawMessage, error) {
	return client.signedRead(ctx, http.MethodGet, path, query, nil, options...)
}

// signedRead는 Read 권한이 필요한 조회 호출을 수행한다. POST 조회도 재시도 가능한 read로 취급한다.
func (client *Client) signedRead(
	ctx context.Context,
	method, path string,
	query url.Values,
	body []byte,
	options ...trade.RequestOption,
) (json.RawMessage, error) {
	response, _, err := client.executeSigned(
		ctx,
		method,
		path,
		query,
		body,
		accountLimit(10),
		credential.PermissionRead,
		commonexchange.OperationRead,
		options...,
	)
	if err != nil {
		return nil, err
	}
	data, _, err := client.responseData(response, commonexchange.OperationRead)
	return data, err
}

// mutate는 mutation POST를 한 번만 전송한다. body가 nil이면 본문 없이 전송한다.
// 응답 해석을 호출자가 decodeBodyError로 처리할 수 있도록 원본 응답도 반환한다.
func (client *Client) mutate(
	ctx context.Context,
	path string,
	body any,
	permission credential.Permission,
	options ...trade.RequestOption,
) (commonexchange.Response, json.RawMessage, error) {
	var encoded []byte
	if body != nil {
		var err error
		encoded, err = encodeBody(body)
		if err != nil {
			return commonexchange.Response{}, nil, err
		}
	}
	response, _, err := client.executeSigned(
		ctx,
		http.MethodPost,
		path,
		nil,
		encoded,
		accountLimit(10),
		permission,
		commonexchange.OperationMutation,
		options...,
	)
	if err != nil {
		return commonexchange.Response{}, nil, err
	}
	data, _, err := client.responseData(response, commonexchange.OperationMutation)
	if err != nil {
		return commonexchange.Response{}, nil, err
	}
	return response, data, nil
}

// mutateRaw는 mutation POST 후 data 필드를 그대로 반환한다.
func (client *Client) mutateRaw(
	ctx context.Context,
	path string,
	body any,
	permission credential.Permission,
	options ...trade.RequestOption,
) (json.RawMessage, error) {
	_, data, err := client.mutate(ctx, path, body, permission, options...)
	return data, err
}
