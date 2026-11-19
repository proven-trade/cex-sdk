package bithumb

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

const (
	// minTWAPDuration과 maxTWAPDuration은 TWAP 주문이 진행되는 시간의 공식 범위(300초~43200초)다.
	minTWAPDuration = 5 * time.Minute
	maxTWAPDuration = 12 * time.Hour
)

// TWAPState는 TWAP 주문 상태다. 조회 조건의 기본값은 TWAPStateProgress다.
type TWAPState string

const (
	TWAPStateProgress TWAPState = "progress"
	TWAPStateDone     TWAPState = "done"
	TWAPStateCancel   TWAPState = "cancel"
)

// PlaceTWAPOrderRequest는 TWAP 주문 요청이다.
// Side가 ask이면 Volume, bid이면 Price가 필수이며 지정한 값은 양의 소수여야 한다.
// Duration은 주문이 진행되는 시간으로 300초 이상 43200초 이하의 초 단위 정수여야 하고,
// Frequency는 분할 주문 간격으로 15초, 20초, 30초, 60초, 120초 중 하나여야 한다.
type PlaceTWAPOrderRequest struct {
	Market    string
	Side      Side
	Volume    string
	Price     string
	Duration  time.Duration
	Frequency time.Duration
}

// TWAPOrdersRequest는 TWAP 주문 내역 조회 조건이다.
// State를 비우면 거래소 기본값인 진행 중(progress) 주문을 조회한다.
type TWAPOrdersRequest struct {
	Market  string
	State   TWAPState
	UUIDs   []string
	Limit   int
	OrderBy string
	NextKey string
}

// CancelTWAPOrderRequest는 TWAP 주문 취소 조건이다.
type CancelTWAPOrderRequest struct {
	AlgoOrderID string
}

// TWAPOrderReference는 TWAP 주문 요청과 취소 응답의 TWAP 주문 ID다.
type TWAPOrderReference struct {
	AlgoOrderID string          `json:"algo_order_id"`
	Raw         json.RawMessage `json:"-"`
}

// TWAPOrder는 TWAP 주문 내역 한 건이다. UUID는 주문 요청의 AlgoOrderID와 같다.
// FinishedAt은 완료 주문에, CanceledAt과 CancelType은 취소 주문에만 채워진다.
type TWAPOrder struct {
	UUID                string          `json:"uuid"`
	Side                Side            `json:"side"`
	Price               Decimal         `json:"price"`
	State               TWAPState       `json:"state"`
	Market              string          `json:"market"`
	CreatedAt           string          `json:"created_at"`
	Volume              Decimal         `json:"volume"`
	FinishedAt          string          `json:"finished_at"`
	CanceledAt          string          `json:"canceled_at"`
	CancelType          string          `json:"cancel_type"`
	TotalOrderCount     int             `json:"total_order_count"`
	TotalTradesCount    int             `json:"total_trades_count"`
	ProgressCount       int             `json:"progress_count"`
	TotalExecutedAmount Decimal         `json:"total_executed_amount"`
	TotalExecutedVolume Decimal         `json:"total_executed_volume"`
	AvgTradePrice       Decimal         `json:"avg_trade_price"`
	WalletID            string          `json:"wallet_id"`
	Raw                 json.RawMessage `json:"-"`
}

// TWAPOrderPage는 TWAP 주문 내역과 다음 페이지 cursor를 보존한다.
type TWAPOrderPage struct {
	Orders  []TWAPOrder     `json:"orders"`
	HasNext bool            `json:"has_next"`
	NextKey string          `json:"next_key"`
	Raw     json.RawMessage `json:"-"`
}

// PlaceTWAPOrder는 TWAP 주문을 요청한다. 거래소는 Duration 동안 Frequency 간격으로 주문을 나눠 제출한다.
// 요청 제한은 단건 주문 요청 분류를 따른다. 전송 결과가 불명확하면 자동 재시도하지 않고
// UNKNOWN_EXECUTION_STATE로 반환하므로 TWAPOrders로 생성 여부를 확인해야 한다.
func (client *Client) PlaceTWAPOrder(
	ctx context.Context,
	request PlaceTWAPOrderRequest,
	options ...trade.RequestOption,
) (TWAPOrderReference, error) {
	if err := request.validate(); err != nil {
		return TWAPOrderReference{}, err
	}
	params := request.parameters()
	body, err := encodeBody(params)
	if err != nil {
		return TWAPOrderReference{}, validationError("encode TWAP order body: %v", err)
	}
	response, err := client.executePrivate(
		ctx, http.MethodPost, "/v1/twap", params, body, rateLimitOrderCreate,
		credential.PermissionTrade, commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return TWAPOrderReference{}, err
	}
	var result TWAPOrderReference
	if err := client.decodeResponse(response, commonexchange.OperationMutation, &result); err != nil {
		return TWAPOrderReference{}, err
	}
	result.Raw = cloneBytes(response.Body)
	return result, nil
}

// TWAPOrders는 TWAP 주문 내역과 다음 페이지 cursor를 조회한다.
func (client *Client) TWAPOrders(
	ctx context.Context,
	request TWAPOrdersRequest,
	options ...trade.RequestOption,
) (TWAPOrderPage, error) {
	if err := request.validate(); err != nil {
		return TWAPOrderPage{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/v1/twap", request.parameters(), nil, rateLimitPrivateOther,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return TWAPOrderPage{}, err
	}
	var envelope struct {
		Orders  []json.RawMessage `json:"orders"`
		HasNext bool              `json:"has_next"`
		NextKey string            `json:"next_key"`
	}
	if err := client.decodeResponse(response, commonexchange.OperationRead, &envelope); err != nil {
		return TWAPOrderPage{}, err
	}
	orders, err := decodeRawItems(envelope.Orders, func(item *TWAPOrder, raw []byte) {
		item.Raw = raw
	})
	if err != nil {
		return TWAPOrderPage{}, client.decodeBodyError(
			response, commonexchange.OperationRead, fmt.Errorf("decode Bithumb TWAP order item: %w", err),
		)
	}
	return TWAPOrderPage{
		Orders: orders, HasNext: envelope.HasNext, NextKey: envelope.NextKey, Raw: cloneBytes(response.Body),
	}, nil
}

// CancelTWAPOrder는 TWAP 주문 취소를 요청한다. 요청 제한은 단건 주문 취소 접수 분류를 따른다.
// 전송 결과가 불명확하면 자동 재시도하지 않고 UNKNOWN_EXECUTION_STATE로 반환한다.
func (client *Client) CancelTWAPOrder(
	ctx context.Context,
	request CancelTWAPOrderRequest,
	options ...trade.RequestOption,
) (TWAPOrderReference, error) {
	if err := request.validate(); err != nil {
		return TWAPOrderReference{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodDelete, "/v1/twap", request.parameters(), nil, rateLimitOrderCancel,
		credential.PermissionTrade, commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return TWAPOrderReference{}, err
	}
	var result TWAPOrderReference
	if err := client.decodeResponse(response, commonexchange.OperationMutation, &result); err != nil {
		return TWAPOrderReference{}, err
	}
	result.Raw = cloneBytes(response.Body)
	return result, nil
}

func (request PlaceTWAPOrderRequest) validate() error {
	if err := validateMarket(request.Market); err != nil {
		return err
	}
	if request.Side != SideAsk && request.Side != SideBid {
		return validationError("side must be ask or bid")
	}
	if request.Side == SideAsk || request.Volume != "" {
		if err := validatePositiveDecimal("volume", request.Volume); err != nil {
			return err
		}
	}
	if request.Side == SideBid || request.Price != "" {
		if err := validatePositiveDecimal("price", request.Price); err != nil {
			return err
		}
	}
	if request.Duration%time.Second != 0 || request.Duration < minTWAPDuration || request.Duration > maxTWAPDuration {
		return validationError("duration must be a whole number of seconds between 300 and 43200")
	}
	switch request.Frequency {
	case 15 * time.Second, 20 * time.Second, 30 * time.Second, time.Minute, 2 * time.Minute:
		return nil
	default:
		return validationError("frequency must be 15, 20, 30, 60, or 120 seconds")
	}
}

func (request PlaceTWAPOrderRequest) parameters() parameters {
	values := parameters{}
	values.add("market", request.Market)
	values.add("side", string(request.Side))
	values.add("volume", request.Volume)
	values.add("price", request.Price)
	values.add("duration", strconv.FormatInt(int64(request.Duration/time.Second), 10))
	values.add("frequency", strconv.FormatInt(int64(request.Frequency/time.Second), 10))
	return values
}

func (request TWAPOrdersRequest) validate() error {
	if request.Market != "" {
		if err := validateMarket(request.Market); err != nil {
			return err
		}
	}
	switch request.State {
	case "", TWAPStateProgress, TWAPStateDone, TWAPStateCancel:
	default:
		return validationError("TWAP state must be progress, done, or cancel")
	}
	if err := validateIDList("uuids", request.UUIDs, 0, nil); err != nil {
		return err
	}
	if request.Limit < 0 || request.Limit > 100 {
		return validationError("TWAP order limit must be between 1 and 100 or zero for default")
	}
	if err := validateOrderBy(request.OrderBy); err != nil {
		return err
	}
	return validateParameterValue("nextKey", request.NextKey)
}

func (request TWAPOrdersRequest) parameters() parameters {
	values := parameters{}
	values.add("market", request.Market)
	values.add("state", string(request.State))
	values.addAll("uuids[]", request.UUIDs)
	values.addInt("limit", request.Limit)
	values.add("order_by", request.OrderBy)
	values.add("next_key", request.NextKey)
	return values
}

func (request CancelTWAPOrderRequest) validate() error {
	return validateIdentifier("algoOrderID", request.AlgoOrderID)
}

func (request CancelTWAPOrderRequest) parameters() parameters {
	return parameters{{key: "algo_order_id", value: request.AlgoOrderID}}
}
