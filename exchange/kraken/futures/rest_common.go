package futures

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
	"github.com/proven-trade/cex-sdk/ratelimit"
)

const (
	chartsPrefix  = "/api/charts/v1"
	historyPrefix = "/api/history/v3"
	authPrefix    = "/api/auth/v1"
	// defaultReadCost는 비용표에 없는 private 조회 endpoint에 적용하는 보수적 비용이다.
	defaultReadCost = 2
)

// Ack는 본문 데이터 없이 성공만 알리는 Futures 응답이다.
type Ack struct {
	ServerTime string          `json:"serverTime"`
	Raw        json.RawMessage `json:"-"`
}

// privateRead는 읽기 전용 private 요청을 실행하고 result envelope를 target으로 디코딩한다.
func (client *Client) privateRead(
	ctx context.Context,
	method, path string,
	values url.Values,
	cost int,
	target any,
	options ...trade.RequestOption,
) (commonexchange.Response, error) {
	response, err := client.executePrivate(
		ctx, method, path, values, cost,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return response, err
	}
	if err := client.decodeSuccess(response, commonexchange.OperationRead, target); err != nil {
		return response, err
	}
	return response, nil
}

// privateAck는 mutation 요청을 실행하고 성공 여부만 확인한다. 자동 재시도하지 않는다.
func (client *Client) privateAck(
	ctx context.Context,
	method, path string,
	values url.Values,
	cost int,
	permission credential.Permission,
	options ...trade.RequestOption,
) (Ack, error) {
	response, err := client.executePrivate(
		ctx, method, path, values, cost, permission, commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return Ack{}, err
	}
	var ack Ack
	if err := client.decodeSuccess(response, commonexchange.OperationMutation, &ack); err != nil {
		return Ack{}, err
	}
	ack.Raw = cloneBytes(response.Body)
	return ack, nil
}

// publicRead는 공개 GET 요청을 실행하고 result envelope를 target으로 디코딩한다.
func (client *Client) publicRead(
	ctx context.Context,
	path string,
	query url.Values,
	target any,
	options ...trade.RequestOption,
) (commonexchange.Response, error) {
	response, err := client.executePublic(ctx, path, query, options...)
	if err != nil {
		return response, err
	}
	if err := client.decodeSuccess(response, commonexchange.OperationRead, target); err != nil {
		return response, err
	}
	return response, nil
}

// decodePlainJSON은 result envelope가 없는 응답(Charts)을 디코딩한다.
func (client *Client) decodePlainJSON(
	response commonexchange.Response,
	operation commonexchange.OperationKind,
	target any,
) error {
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return client.decodeError(response, "", "", operation, nil)
	}
	if err := json.Unmarshal(response.Body, target); err != nil {
		return client.decodeBodyError(response, operation, err)
	}
	return nil
}

func setTimeMillis(values url.Values, key string, value *time.Time) {
	if value != nil {
		values.Set(key, strconv.FormatInt(value.UnixMilli(), 10))
	}
}

func setBoolPointer(values url.Values, key string, value *bool) {
	if value != nil {
		values.Set(key, strconv.FormatBool(*value))
	}
}

func setTrue(values url.Values, key string, value bool) {
	if value {
		values.Set(key, "true")
	}
}

// historyRead는 /api/history/v3 계정 이력 endpoint를 별도 토큰 풀 비용으로 조회한다.
func (client *Client) historyRead(
	ctx context.Context,
	path string,
	values url.Values,
	cost int,
	target any,
	options ...trade.RequestOption,
) (commonexchange.Response, error) {
	response, err := client.executePrivateCharged(
		ctx, http.MethodGet, path, values, credential.PermissionRead, commonexchange.OperationRead,
		func(accountID string) ([]ratelimit.Charge, error) {
			return historyRateLimitCharges(client.executor.Limiter(), accountID, cost)
		}, options...,
	)
	if err != nil {
		return response, err
	}
	if err := client.decodeSuccess(response, commonexchange.OperationRead, target); err != nil {
		return response, err
	}
	return response, nil
}
