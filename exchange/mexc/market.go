package mexc

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"

	trade "github.com/proven-trade/proven-trade-sdk"
	commonexchange "github.com/proven-trade/proven-trade-sdk/exchange"
)

// Ping은 MEXC Spot V3 REST 연결 가능 여부를 확인한다.
func (client *Client) Ping(ctx context.Context, options ...trade.RequestOption) error {
	response, err := client.executePublic(
		ctx, "/api/v3/ping", nil, publicLimit("ping", 1), options...,
	)
	if err != nil {
		return err
	}
	var value struct{}
	_, err = client.decodeResponse(response, &value)
	return err
}

// ServerTime은 MEXC 서버의 Unix millisecond 시각을 조회한다.
func (client *Client) ServerTime(
	ctx context.Context,
	options ...trade.RequestOption,
) (ServerTime, error) {
	response, err := client.executePublic(
		ctx, "/api/v3/time", nil, publicLimit("time", 1), options...,
	)
	if err != nil {
		return ServerTime{}, err
	}
	var value ServerTime
	raw, err := client.decodeResponse(response, &value)
	if err != nil {
		return ServerTime{}, err
	}
	if value.Time <= 0 {
		return ServerTime{}, client.decodeBodyError(response, errors.New("MEXC server time is invalid"))
	}
	value.Raw = raw
	return value, nil
}

// DefaultSymbols는 계정 없이 API 사용이 기본 허용된 Spot 거래쌍을 조회한다.
func (client *Client) DefaultSymbols(
	ctx context.Context,
	options ...trade.RequestOption,
) ([]string, error) {
	response, err := client.executePublic(
		ctx, "/api/v3/defaultSymbols", nil, publicLimit("default-symbols", 1), options...,
	)
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Code Scalar   `json:"code"`
		Data []string `json:"data"`
	}
	if _, err := client.decodeResponse(response, &envelope); err != nil {
		return nil, err
	}
	if len(envelope.Data) == 0 {
		return nil, client.decodeBodyError(response, errors.New("MEXC default symbol list is empty"))
	}
	return envelope.Data, nil
}

// ExchangeInfo는 전체·단일·복수 Spot 거래쌍의 거래 규칙을 조회한다.
func (client *Client) ExchangeInfo(
	ctx context.Context,
	request ExchangeInfoRequest,
	options ...trade.RequestOption,
) (ExchangeInfo, error) {
	if err := request.validate(); err != nil {
		return ExchangeInfo{}, err
	}
	response, err := client.executePublic(
		ctx, "/api/v3/exchangeInfo", request.values(), publicLimit("exchange-info", 10),
		options...,
	)
	if err != nil {
		return ExchangeInfo{}, err
	}
	var wire struct {
		Timezone        string            `json:"timezone"`
		ServerTime      int64             `json:"serverTime"`
		RateLimits      []RateLimit       `json:"rateLimits"`
		ExchangeFilters []json.RawMessage `json:"exchangeFilters"`
		Symbols         []json.RawMessage `json:"symbols"`
	}
	raw, err := client.decodeResponse(response, &wire)
	if err != nil {
		return ExchangeInfo{}, err
	}
	value := ExchangeInfo{
		Timezone: wire.Timezone, ServerTime: wire.ServerTime,
		RateLimits: wire.RateLimits, ExchangeFilters: wire.ExchangeFilters,
		Symbols: make([]Symbol, len(wire.Symbols)), Raw: raw,
	}
	for index, symbolRaw := range wire.Symbols {
		if err := json.Unmarshal(symbolRaw, &value.Symbols[index]); err != nil {
			return ExchangeInfo{}, client.decodeBodyError(response, err)
		}
		value.Symbols[index].Raw = cloneBytes(symbolRaw)
	}
	return value, nil
}

// OrderBook은 최대 5000단계의 Spot 호가 snapshot을 조회한다.
func (client *Client) OrderBook(
	ctx context.Context,
	request OrderBookRequest,
	options ...trade.RequestOption,
) (OrderBook, error) {
	if err := request.validate(); err != nil {
		return OrderBook{}, err
	}
	response, err := client.executePublic(
		ctx, "/api/v3/depth", request.values(), publicLimit("depth", 1), options...,
	)
	if err != nil {
		return OrderBook{}, err
	}
	var value OrderBook
	raw, err := client.decodeResponse(response, &value)
	if err != nil {
		return OrderBook{}, err
	}
	value.Raw = raw
	return value, nil
}

// RecentTrades는 최대 1000개의 최근 공개 Spot 체결을 조회한다.
func (client *Client) RecentTrades(
	ctx context.Context,
	request TradesRequest,
	options ...trade.RequestOption,
) ([]PublicTrade, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePublic(
		ctx, "/api/v3/trades", request.values(), publicLimit("trades", 5), options...,
	)
	if err != nil {
		return nil, err
	}
	return decodePublicTrades(client, response)
}

// AggregateTrades는 최대 1000개의 합산 공개 Spot 체결을 조회한다.
func (client *Client) AggregateTrades(
	ctx context.Context,
	request AggregateTradesRequest,
	options ...trade.RequestOption,
) ([]AggregateTrade, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePublic(
		ctx, "/api/v3/aggTrades", request.values(), publicLimit("aggregate-trades", 1),
		options...,
	)
	if err != nil {
		return nil, err
	}
	var rawItems []json.RawMessage
	if _, err := client.decodeResponse(response, &rawItems); err != nil {
		return nil, err
	}
	items := make([]AggregateTrade, len(rawItems))
	for index, raw := range rawItems {
		if err := json.Unmarshal(raw, &items[index]); err != nil {
			return nil, client.decodeBodyError(response, err)
		}
		items[index].Raw = cloneBytes(raw)
	}
	return items, nil
}

// Candles는 최대 1000개의 Spot OHLCV 캔들을 조회한다.
func (client *Client) Candles(
	ctx context.Context,
	request CandlesRequest,
	options ...trade.RequestOption,
) ([]Candle, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePublic(
		ctx, "/api/v3/klines", request.values(), publicLimit("klines", 1), options...,
	)
	if err != nil {
		return nil, err
	}
	var items []Candle
	if _, err := client.decodeResponse(response, &items); err != nil {
		return nil, err
	}
	return items, nil
}

// AveragePrice는 지정한 Spot 거래쌍의 최근 분 단위 평균가를 조회한다.
func (client *Client) AveragePrice(
	ctx context.Context,
	symbol string,
	options ...trade.RequestOption,
) (AveragePrice, error) {
	if err := validateSymbol(symbol); err != nil {
		return AveragePrice{}, err
	}
	response, err := client.executePublic(
		ctx, "/api/v3/avgPrice", url.Values{"symbol": {symbol}},
		publicLimit("average-price", 1), options...,
	)
	if err != nil {
		return AveragePrice{}, err
	}
	var value AveragePrice
	raw, err := client.decodeResponse(response, &value)
	if err != nil {
		return AveragePrice{}, err
	}
	value.Raw = raw
	return value, nil
}

// Ticker24H는 지정한 Spot 거래쌍의 24시간 가격·거래량 통계를 조회한다.
func (client *Client) Ticker24H(
	ctx context.Context,
	symbol string,
	options ...trade.RequestOption,
) (Ticker24H, error) {
	if err := validateSymbol(symbol); err != nil {
		return Ticker24H{}, err
	}
	response, err := client.executePublic(
		ctx, "/api/v3/ticker/24hr", url.Values{"symbol": {symbol}},
		publicLimit("ticker-24h", 1), options...,
	)
	if err != nil {
		return Ticker24H{}, err
	}
	var value Ticker24H
	raw, err := client.decodeResponse(response, &value)
	if err != nil {
		return Ticker24H{}, err
	}
	value.Raw = raw
	return value, nil
}

// PriceTicker는 지정한 Spot 거래쌍의 최근 가격을 조회한다.
func (client *Client) PriceTicker(
	ctx context.Context,
	symbol string,
	options ...trade.RequestOption,
) (PriceTicker, error) {
	if err := validateSymbol(symbol); err != nil {
		return PriceTicker{}, err
	}
	response, err := client.executePublic(
		ctx, "/api/v3/ticker/price", url.Values{"symbol": {symbol}},
		publicLimit("price-ticker", 1), options...,
	)
	if err != nil {
		return PriceTicker{}, err
	}
	var value PriceTicker
	raw, err := client.decodeResponse(response, &value)
	if err != nil {
		return PriceTicker{}, err
	}
	value.Raw = raw
	return value, nil
}

// BookTicker는 지정한 Spot 거래쌍의 최우선 매수·매도 호가를 조회한다.
func (client *Client) BookTicker(
	ctx context.Context,
	symbol string,
	options ...trade.RequestOption,
) (BookTicker, error) {
	if err := validateSymbol(symbol); err != nil {
		return BookTicker{}, err
	}
	response, err := client.executePublic(
		ctx, "/api/v3/ticker/bookTicker", url.Values{"symbol": {symbol}},
		publicLimit("book-ticker", 1), options...,
	)
	if err != nil {
		return BookTicker{}, err
	}
	var value BookTicker
	raw, err := client.decodeResponse(response, &value)
	if err != nil {
		return BookTicker{}, err
	}
	value.Raw = raw
	return value, nil
}

func decodePublicTrades(
	client *Client,
	response commonexchange.Response,
) ([]PublicTrade, error) {
	var rawItems []json.RawMessage
	if _, err := client.decodeResponse(response, &rawItems); err != nil {
		return nil, err
	}
	items := make([]PublicTrade, len(rawItems))
	for index, raw := range rawItems {
		if err := json.Unmarshal(raw, &items[index]); err != nil {
			return nil, client.decodeBodyError(response, err)
		}
		items[index].Raw = cloneBytes(raw)
	}
	return items, nil
}
