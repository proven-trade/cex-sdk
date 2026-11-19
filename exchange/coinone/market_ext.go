package coinone

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	trade "github.com/proven-trade/cex-sdk"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

// RangeUnitsRequest는 종목의 가격 구간별 호가 단위 조회 조건이다.
type RangeUnitsRequest struct {
	QuoteCurrency  string
	TargetCurrency string
}

// MarketRequest는 개별 종목 정보 조회 조건이다.
type MarketRequest struct {
	QuoteCurrency  string
	TargetCurrency string
}

// TickersRequest는 마켓 전체 종목의 현재가 조회 조건이다.
// AdditionalData가 true이면 전일 시세(yesterday_*)를 함께 조회한다.
type TickersRequest struct {
	QuoteCurrency  string
	AdditionalData bool
}

// CurrencyRequest는 개별 가상자산의 입출금 정보 조회 조건이다.
type CurrencyRequest struct {
	Currency string
}

// RangeUnit은 한 가격 구간의 호가 단위다.
// RangeMin부터 다음 구간이 시작되는 NextRangeMin 전까지의 가격은 PriceUnit 단위로 주문한다.
type RangeUnit struct {
	RangeMin     Decimal         `json:"range_min"`
	NextRangeMin Decimal         `json:"next_range_min"`
	PriceUnit    Decimal         `json:"price_unit"`
	Raw          json.RawMessage `json:"-"`
}

// Currency는 가상자산의 입출금 가능 상태, 수수료와 출금 정밀도다.
// DepositStatus와 WithdrawStatus는 normal 또는 suspended이고,
// MaxPrecision은 출금할 수 있는 소수점 아래 자릿수다.
type Currency struct {
	Name                    string          `json:"name"`
	Symbol                  string          `json:"symbol"`
	DepositStatus           string          `json:"deposit_status"`
	WithdrawStatus          string          `json:"withdraw_status"`
	DepositConfirmCount     int             `json:"deposit_confirm_count"`
	MaxPrecision            int             `json:"max_precision"`
	DepositFee              Decimal         `json:"deposit_fee"`
	WithdrawalMinimumAmount Decimal         `json:"withdrawal_min_amount"`
	WithdrawalFee           Decimal         `json:"withdrawal_fee"`
	Raw                     json.RawMessage `json:"-"`
}

func (request RangeUnitsRequest) validate() error {
	return validatePair(request.QuoteCurrency, request.TargetCurrency)
}

func (request MarketRequest) validate() error {
	return validatePair(request.QuoteCurrency, request.TargetCurrency)
}

func (request TickersRequest) validate() error {
	return validateCurrency("quote currency", request.QuoteCurrency)
}

func (request TickersRequest) values() url.Values {
	values := make(url.Values)
	if request.AdditionalData {
		values.Set("additional_data", "true")
	}
	return values
}

func (request CurrencyRequest) validate() error {
	return validateCurrency("currency", request.Currency)
}

// RangeUnits는 종목의 가격 구간별 호가 단위를 조회한다.
func (client *Client) RangeUnits(
	ctx context.Context,
	request RangeUnitsRequest,
	options ...trade.RequestOption,
) ([]RangeUnit, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	path := pairPath("/public/v2/range_units", request.QuoteCurrency, request.TargetCurrency)
	response, err := client.executePublic(ctx, path, nil, options...)
	if err != nil {
		return nil, err
	}
	var envelope struct {
		RangePriceUnits []json.RawMessage `json:"range_price_units"`
	}
	if err := client.decodeResponse(response, commonexchange.OperationRead, &envelope); err != nil {
		return nil, err
	}
	return decodeRawItems(client, response, envelope.RangePriceUnits, func(item *RangeUnit, raw []byte) {
		item.Raw = raw
	})
}

// Market은 지정 종목의 주문 규칙과 거래 상태를 조회한다.
// 코인원이 제거 예정으로 안내한 PriceUnit 대신 가격 구간별 호가 단위는 RangeUnits로 조회한다.
func (client *Client) Market(
	ctx context.Context,
	request MarketRequest,
	options ...trade.RequestOption,
) (Market, error) {
	if err := request.validate(); err != nil {
		return Market{}, err
	}
	path := pairPath("/public/v2/markets", request.QuoteCurrency, request.TargetCurrency)
	response, err := client.executePublic(ctx, path, nil, options...)
	if err != nil {
		return Market{}, err
	}
	var envelope struct {
		Markets []json.RawMessage `json:"markets"`
	}
	if err := client.decodeResponse(response, commonexchange.OperationRead, &envelope); err != nil {
		return Market{}, err
	}
	items, err := decodeRawItems(client, response, envelope.Markets, func(item *Market, raw []byte) {
		item.Raw = raw
	})
	if err != nil {
		return Market{}, err
	}
	return singleItem(client, response, "market", items)
}

// Tickers는 마켓 전체 종목의 현재가와 24시간 누적 거래 정보를 조회한다.
func (client *Client) Tickers(
	ctx context.Context,
	request TickersRequest,
	options ...trade.RequestOption,
) ([]Ticker, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	path := "/public/v2/ticker_new/" + url.PathEscape(request.QuoteCurrency)
	items, _, err := client.fetchTickers(ctx, path, request.values(), options...)
	return items, err
}

// UTCTicker는 UTC 기준으로 지정 종목의 현재가와 누적 거래 정보를 조회한다.
// AdditionalData가 true일 때 포함되는 전일 거래대금과 거래량은 UTC 기준이 아니라 KST 티커와 같은 값이다.
func (client *Client) UTCTicker(
	ctx context.Context,
	request TickerRequest,
	options ...trade.RequestOption,
) (Ticker, error) {
	if err := request.validate(); err != nil {
		return Ticker{}, err
	}
	path := pairPath("/public/v2/ticker_utc_new", request.QuoteCurrency, request.TargetCurrency)
	items, response, err := client.fetchTickers(ctx, path, request.values(), options...)
	if err != nil {
		return Ticker{}, err
	}
	return singleItem(client, response, "UTC ticker", items)
}

// UTCTickers는 UTC 기준으로 마켓 전체 종목의 현재가와 누적 거래 정보를 조회한다.
// AdditionalData가 true일 때 포함되는 전일 거래대금과 거래량은 UTC 기준이 아니라 KST 티커와 같은 값이다.
func (client *Client) UTCTickers(
	ctx context.Context,
	request TickersRequest,
	options ...trade.RequestOption,
) ([]Ticker, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	path := "/public/v2/ticker_utc_new/" + url.PathEscape(request.QuoteCurrency)
	items, _, err := client.fetchTickers(ctx, path, request.values(), options...)
	return items, err
}

// Currencies는 전체 가상자산의 입출금 가능 여부, 수수료와 최소 출금 수량을 조회한다.
func (client *Client) Currencies(ctx context.Context, options ...trade.RequestOption) ([]Currency, error) {
	items, _, err := client.fetchCurrencies(ctx, "/public/v2/currencies", options...)
	return items, err
}

// Currency는 지정 가상자산의 입출금 가능 여부, 수수료와 최소 출금 수량을 조회한다.
func (client *Client) Currency(
	ctx context.Context,
	request CurrencyRequest,
	options ...trade.RequestOption,
) (Currency, error) {
	if err := request.validate(); err != nil {
		return Currency{}, err
	}
	path := "/public/v2/currencies/" + url.PathEscape(request.Currency)
	items, response, err := client.fetchCurrencies(ctx, path, options...)
	if err != nil {
		return Currency{}, err
	}
	return singleItem(client, response, "currency", items)
}

func (client *Client) fetchTickers(
	ctx context.Context,
	path string,
	query url.Values,
	options ...trade.RequestOption,
) ([]Ticker, commonexchange.Response, error) {
	response, err := client.executePublic(ctx, path, query, options...)
	if err != nil {
		return nil, commonexchange.Response{}, err
	}
	var envelope struct {
		Tickers []json.RawMessage `json:"tickers"`
	}
	if err := client.decodeResponse(response, commonexchange.OperationRead, &envelope); err != nil {
		return nil, commonexchange.Response{}, err
	}
	items, err := decodeRawItems(client, response, envelope.Tickers, func(item *Ticker, raw []byte) {
		item.Raw = raw
	})
	return items, response, err
}

func (client *Client) fetchCurrencies(
	ctx context.Context,
	path string,
	options ...trade.RequestOption,
) ([]Currency, commonexchange.Response, error) {
	response, err := client.executePublic(ctx, path, nil, options...)
	if err != nil {
		return nil, commonexchange.Response{}, err
	}
	var envelope struct {
		Currencies []json.RawMessage `json:"currencies"`
	}
	if err := client.decodeResponse(response, commonexchange.OperationRead, &envelope); err != nil {
		return nil, commonexchange.Response{}, err
	}
	items, err := decodeRawItems(client, response, envelope.Currencies, func(item *Currency, raw []byte) {
		item.Raw = raw
	})
	return items, response, err
}

// singleItem은 개별 조회 응답의 배열에 정확히 한 건만 들어 있는지 확인하고 그 항목을 반환한다.
func singleItem[T any](
	client *Client,
	response commonexchange.Response,
	name string,
	items []T,
) (T, error) {
	if len(items) != 1 {
		var zero T
		return zero, client.decodeBodyError(
			response, commonexchange.OperationRead,
			fmt.Errorf("Coinone individual %s response contains %d items", name, len(items)),
		)
	}
	return items[0], nil
}
