package usdm

import (
	"context"
	"net/url"
	"strconv"
	"time"

	trade "github.com/proven-trade/proven-trade-sdk"
	commonexchange "github.com/proven-trade/proven-trade-sdk/exchange"
)

// Ping은 USDⓈ-M REST 연결 상태를 확인한다.
func (client *Client) Ping(ctx context.Context, options ...trade.RequestOption) error {
	response, err := client.executePublic(ctx, "/fapi/v1/ping", nil, 1, options...)
	if err != nil {
		return err
	}
	var payload map[string]any
	return client.decode(response, commonexchange.OperationRead, &payload)
}

// ServerTime은 서버 시간을 조회하고 로컬 서명 시간 오차를 보정한다.
func (client *Client) ServerTime(ctx context.Context, options ...trade.RequestOption) (time.Time, error) {
	started := client.now()
	response, err := client.executePublic(ctx, "/fapi/v1/time", nil, 1, options...)
	if err != nil {
		return time.Time{}, err
	}
	var payload struct {
		ServerTime int64 `json:"serverTime"`
	}
	if err := client.decode(response, commonexchange.OperationRead, &payload); err != nil {
		return time.Time{}, err
	}
	finished := client.now()
	midpoint := started.UnixMilli() + finished.Sub(started).Milliseconds()/2
	client.clockOffsetMillis.Store(payload.ServerTime - midpoint)
	return time.UnixMilli(payload.ServerTime), nil
}

// ExchangeInfo는 USDⓈ-M 계약 규칙과 동적 요청 제한을 조회한다.
func (client *Client) ExchangeInfo(ctx context.Context, options ...trade.RequestOption) (ExchangeInfo, error) {
	response, err := client.executePublic(ctx, "/fapi/v1/exchangeInfo", nil, 1, options...)
	if err != nil {
		return ExchangeInfo{}, err
	}
	var info ExchangeInfo
	if err := client.decode(response, commonexchange.OperationRead, &info); err != nil {
		return ExchangeInfo{}, err
	}
	info.Raw = cloneBytes(response.Body)
	client.limits.update(info.RateLimits)
	return info, nil
}

// TickerPrice는 단일 계약의 최신 가격을 조회한다.
func (client *Client) TickerPrice(ctx context.Context, request TickerPriceRequest, options ...trade.RequestOption) (TickerPrice, error) {
	if err := validateSymbol(request.Symbol); err != nil {
		return TickerPrice{}, err
	}
	response, err := client.executePublic(ctx, "/fapi/v1/ticker/price", url.Values{"symbol": {request.Symbol}}, 1, options...)
	if err != nil {
		return TickerPrice{}, err
	}
	var ticker TickerPrice
	if err := client.decode(response, commonexchange.OperationRead, &ticker); err != nil {
		return TickerPrice{}, err
	}
	ticker.Raw = cloneBytes(response.Body)
	return ticker, nil
}

// OrderBook은 단일 계약의 호가 스냅샷을 조회한다.
func (client *Client) OrderBook(ctx context.Context, request OrderBookRequest, options ...trade.RequestOption) (OrderBook, error) {
	if err := request.validate(); err != nil {
		return OrderBook{}, err
	}
	values := url.Values{"symbol": {request.Symbol}}
	if request.Limit > 0 {
		values.Set("limit", strconv.Itoa(request.Limit))
	}
	response, err := client.executePublic(ctx, "/fapi/v1/depth", values, depthWeight(request.Limit), options...)
	if err != nil {
		return OrderBook{}, err
	}
	var book OrderBook
	if err := client.decode(response, commonexchange.OperationRead, &book); err != nil {
		return OrderBook{}, err
	}
	book.Raw = cloneBytes(response.Body)
	return book, nil
}

// RecentTrades는 단일 계약의 최근 공개 체결을 조회한다.
func (client *Client) RecentTrades(ctx context.Context, request RecentTradesRequest, options ...trade.RequestOption) ([]PublicTrade, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	values := url.Values{"symbol": {request.Symbol}}
	if request.Limit > 0 {
		values.Set("limit", strconv.Itoa(request.Limit))
	}
	response, err := client.executePublic(ctx, "/fapi/v1/trades", values, 5, options...)
	if err != nil {
		return nil, err
	}
	var trades []PublicTrade
	if err := client.decode(response, commonexchange.OperationRead, &trades); err != nil {
		return nil, err
	}
	return trades, nil
}

// Candles는 단일 계약의 OHLCV 캔들을 조회한다.
func (client *Client) Candles(ctx context.Context, request CandlesRequest, options ...trade.RequestOption) ([]Candle, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	values := url.Values{"symbol": {request.Symbol}, "interval": {string(request.Interval)}}
	if request.StartTime != nil {
		values.Set("startTime", strconv.FormatInt(request.StartTime.UnixMilli(), 10))
	}
	if request.EndTime != nil {
		values.Set("endTime", strconv.FormatInt(request.EndTime.UnixMilli(), 10))
	}
	if request.Limit > 0 {
		values.Set("limit", strconv.Itoa(request.Limit))
	}
	response, err := client.executePublic(ctx, "/fapi/v1/klines", values, candleWeight(request.Limit), options...)
	if err != nil {
		return nil, err
	}
	var candles []Candle
	if err := client.decode(response, commonexchange.OperationRead, &candles); err != nil {
		return nil, err
	}
	return candles, nil
}
