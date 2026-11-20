package usdm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

// ConvertPairsRequest는 Convert 지원 페어 조회 조건이다.
// 두 자산을 모두 비우면 일부 페어만 반환되므로 필요한 쪽을 지정한다.
type ConvertPairsRequest struct {
	FromAsset string
	ToAsset   string
}

// ConvertPair는 Convert로 바꿀 수 있는 페어와 자산별 최소·최대 수량이다.
type ConvertPair struct {
	FromAsset          string          `json:"fromAsset"`
	ToAsset            string          `json:"toAsset"`
	FromAssetMinAmount string          `json:"fromAssetMinAmount"`
	FromAssetMaxAmount string          `json:"fromAssetMaxAmount"`
	ToAssetMinAmount   string          `json:"toAssetMinAmount"`
	ToAssetMaxAmount   string          `json:"toAssetMaxAmount"`
	Raw                json.RawMessage `json:"-"`
}

func (item *ConvertPair) setRaw(raw json.RawMessage) { item.Raw = raw }

func (request ConvertPairsRequest) validate() error {
	if err := validateOptionalAsset("fromAsset", request.FromAsset); err != nil {
		return err
	}
	return validateOptionalAsset("toAsset", request.ToAsset)
}

func (request ConvertPairsRequest) values() url.Values {
	values := make(url.Values)
	set(values, "fromAsset", request.FromAsset)
	set(values, "toAsset", request.ToAsset)
	return values
}

// ConvertPairs는 Futures Convert가 지원하는 토큰 페어와 수량 한도를 조회한다.
// GET /fapi/v1/convert/exchangeInfo이며 서명이 필요 없고 IP weight는 20이다.
func (client *Client) ConvertPairs(ctx context.Context, request ConvertPairsRequest, options ...trade.RequestOption) ([]ConvertPair, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePublic(ctx, "/fapi/v1/convert/exchangeInfo", request.values(), 20, options...)
	if err != nil {
		return nil, err
	}
	return decodeArray[ConvertPair](client, response, commonexchange.OperationRead)
}

// ConvertQuoteRequest는 Convert 견적 요청이다.
// FromAmount는 차감될 수량, ToAmount는 받을 수량이며 둘 중 정확히 하나만 지정한다.
// ValidTime은 견적 유효 시간이며 비우면 서버 기본값인 10s를 쓴다.
type ConvertQuoteRequest struct {
	FromAsset  string
	ToAsset    string
	FromAmount string
	ToAmount   string
	ValidTime  string
}

// ConvertQuote는 제시된 Convert 견적이다. 잔고가 충분할 때만 QuoteID가 채워진다.
// ValidTimestamp는 견적이 만료되는 시각(밀리초)이다.
type ConvertQuote struct {
	QuoteID        string          `json:"quoteId"`
	Ratio          string          `json:"ratio"`
	InverseRatio   string          `json:"inverseRatio"`
	ValidTimestamp int64           `json:"validTimestamp"`
	FromAmount     string          `json:"fromAmount"`
	ToAmount       string          `json:"toAmount"`
	Raw            json.RawMessage `json:"-"`
}

func (item *ConvertQuote) setRaw(raw json.RawMessage) { item.Raw = raw }

func (request ConvertQuoteRequest) validate() error {
	if request.FromAsset == "" || request.ToAsset == "" {
		return validationError("fromAsset and toAsset are required")
	}
	if err := validateOptionalAsset("fromAsset", request.FromAsset); err != nil {
		return err
	}
	if err := validateOptionalAsset("toAsset", request.ToAsset); err != nil {
		return err
	}
	if (request.FromAmount == "") == (request.ToAmount == "") {
		return validationError("exactly one of fromAmount or toAmount is required")
	}
	if err := validatePositiveDecimal("fromAmount", request.FromAmount, false); err != nil {
		return err
	}
	if err := validatePositiveDecimal("toAmount", request.ToAmount, false); err != nil {
		return err
	}
	if strings.TrimSpace(request.ValidTime) != request.ValidTime {
		return validationError("validTime cannot have surrounding whitespace")
	}
	return nil
}

func (request ConvertQuoteRequest) values() url.Values {
	values := url.Values{"fromAsset": {request.FromAsset}, "toAsset": {request.ToAsset}}
	set(values, "fromAmount", request.FromAmount)
	set(values, "toAmount", request.ToAmount)
	set(values, "validTime", request.ValidTime)
	return values
}

// RequestConvertQuote는 Convert 견적을 요청한다. 견적은 구속력이 없으며 AcceptConvertQuote로 수락해야 체결된다.
// POST /fapi/v1/convert/getQuote이며 credential.PermissionTransfer가 필요하고 IP weight는 50이다.
// 공식 서버 한도(시간당 360회, 일 500회)는 SDK가 로컬에서 세지 않는다.
func (client *Client) RequestConvertQuote(ctx context.Context, request ConvertQuoteRequest, options ...trade.RequestOption) (ConvertQuote, error) {
	if err := request.validate(); err != nil {
		return ConvertQuote{}, err
	}
	response, err := client.executeSigned(ctx, http.MethodPost, "/fapi/v1/convert/getQuote", request.values(), 50, 0, credential.PermissionTransfer, commonexchange.OperationMutation, options...)
	if err != nil {
		return ConvertQuote{}, err
	}
	return decodeObject[ConvertQuote](client, response, commonexchange.OperationMutation)
}

// AcceptConvertQuoteRequest는 수락할 Convert 견적 식별자다.
type AcceptConvertQuoteRequest struct{ QuoteID string }

// ConvertAcceptance는 수락된 Convert 주문의 접수 결과다.
// OrderID는 숫자 또는 숫자 문자열로 오며 정밀도를 보존하기 위해 json.Number로 담는다.
// 최종 결과는 ConvertOrderStatus로 확인한다.
type ConvertAcceptance struct {
	OrderID     json.Number     `json:"orderId"`
	CreateTime  int64           `json:"createTime"`
	OrderStatus string          `json:"orderStatus"`
	Raw         json.RawMessage `json:"-"`
}

func (item *ConvertAcceptance) setRaw(raw json.RawMessage) { item.Raw = raw }

func (request AcceptConvertQuoteRequest) validate() error {
	if request.QuoteID == "" || strings.TrimSpace(request.QuoteID) != request.QuoteID {
		return validationError("quoteId is required without surrounding whitespace")
	}
	return nil
}

func (request AcceptConvertQuoteRequest) values() url.Values {
	return url.Values{"quoteId": {request.QuoteID}}
}

// AcceptConvertQuote는 받은 Convert 견적을 수락해 변환 주문을 만든다.
// POST /fapi/v1/convert/acceptQuote이며 credential.PermissionTransfer가 필요하고 IP weight는 200이다.
// 불명확한 전송 결과는 UNKNOWN_EXECUTION_STATE로 반환하며 자동 재시도하지 않으므로 ConvertOrderStatus로 먼저 확인한다.
func (client *Client) AcceptConvertQuote(ctx context.Context, request AcceptConvertQuoteRequest, options ...trade.RequestOption) (ConvertAcceptance, error) {
	if err := request.validate(); err != nil {
		return ConvertAcceptance{}, err
	}
	response, err := client.executeSigned(ctx, http.MethodPost, "/fapi/v1/convert/acceptQuote", request.values(), 200, 0, credential.PermissionTransfer, commonexchange.OperationMutation, options...)
	if err != nil {
		return ConvertAcceptance{}, err
	}
	return decodeObject[ConvertAcceptance](client, response, commonexchange.OperationMutation)
}

// ConvertOrderRequest는 Convert 주문 상태를 조회할 식별자다. OrderID 또는 QuoteID 중 정확히 하나만 지정한다.
type ConvertOrderRequest struct {
	OrderID string
	QuoteID string
}

// ConvertOrder는 Convert 주문의 상태와 체결 내용이다.
// OrderID는 숫자 또는 숫자 문자열로 오며 정밀도를 보존하기 위해 json.Number로 담는다.
type ConvertOrder struct {
	OrderID      json.Number     `json:"orderId"`
	OrderStatus  string          `json:"orderStatus"`
	FromAsset    string          `json:"fromAsset"`
	FromAmount   string          `json:"fromAmount"`
	ToAsset      string          `json:"toAsset"`
	ToAmount     string          `json:"toAmount"`
	Ratio        string          `json:"ratio"`
	InverseRatio string          `json:"inverseRatio"`
	CreateTime   int64           `json:"createTime"`
	Raw          json.RawMessage `json:"-"`
}

func (item *ConvertOrder) setRaw(raw json.RawMessage) { item.Raw = raw }

func (request ConvertOrderRequest) validate() error {
	if (request.OrderID == "") == (request.QuoteID == "") {
		return validationError("exactly one of orderId or quoteId is required")
	}
	if strings.TrimSpace(request.OrderID) != request.OrderID || strings.TrimSpace(request.QuoteID) != request.QuoteID {
		return validationError("orderId and quoteId cannot have surrounding whitespace")
	}
	return nil
}

func (request ConvertOrderRequest) values() url.Values {
	values := make(url.Values)
	set(values, "orderId", request.OrderID)
	set(values, "quoteId", request.QuoteID)
	return values
}

// ConvertOrderStatus는 Convert 주문의 처리 상태를 조회한다.
// GET /fapi/v1/convert/orderStatus이며 credential.PermissionRead가 필요하고 IP weight는 50이다.
func (client *Client) ConvertOrderStatus(ctx context.Context, request ConvertOrderRequest, options ...trade.RequestOption) (ConvertOrder, error) {
	if err := request.validate(); err != nil {
		return ConvertOrder{}, err
	}
	response, err := client.executeSigned(ctx, http.MethodGet, "/fapi/v1/convert/orderStatus", request.values(), 50, 0, credential.PermissionRead, commonexchange.OperationRead, options...)
	if err != nil {
		return ConvertOrder{}, err
	}
	return decodeObject[ConvertOrder](client, response, commonexchange.OperationRead)
}

func validateOptionalAsset(name, value string) error {
	if strings.TrimSpace(value) != value {
		return validationError("%s cannot have surrounding whitespace", name)
	}
	return nil
}
