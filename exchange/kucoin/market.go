package kucoin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	trade "github.com/proven-trade/proven-trade-sdk"
	commonexchange "github.com/proven-trade/proven-trade-sdk/exchange"
)

// Symbols는 모든 KuCoin Spot 거래쌍과 주문 단위 규칙을 조회한다.
func (client *Client) Symbols(
	ctx context.Context,
	options ...trade.RequestOption,
) ([]Symbol, error) {
	response, err := client.executePublic(
		ctx, http.MethodGet, "/api/v2/symbols", nil, publicLimit(4), options...,
	)
	if err != nil {
		return nil, err
	}
	var rawItems []json.RawMessage
	if _, err := client.decodeData(response, commonexchange.OperationRead, &rawItems); err != nil {
		return nil, err
	}
	items := make([]Symbol, len(rawItems))
	for index, raw := range rawItems {
		if err := json.Unmarshal(raw, &items[index]); err != nil {
			return nil, client.decodeBodyError(response, commonexchange.OperationRead, err)
		}
		items[index].Raw = cloneBytes(raw)
	}
	return items, nil
}

// Ticker는 지정한 Spot 거래쌍의 최우선 호가와 최근 체결을 조회한다.
func (client *Client) Ticker(
	ctx context.Context,
	symbol string,
	options ...trade.RequestOption,
) (Ticker, error) {
	if err := validateSymbol(symbol); err != nil {
		return Ticker{}, err
	}
	response, err := client.executePublic(
		ctx, http.MethodGet, "/api/v1/market/orderbook/level1", url.Values{"symbol": {symbol}},
		publicLimit(2), options...,
	)
	if err != nil {
		return Ticker{}, err
	}
	var ticker Ticker
	data, err := client.decodeData(response, commonexchange.OperationRead, &ticker)
	if err != nil {
		return Ticker{}, err
	}
	ticker.Raw = cloneBytes(data)
	return ticker, nil
}

// OrderBook은 20개 또는 100개 깊이의 합산 Spot 호가를 조회한다.
func (client *Client) OrderBook(
	ctx context.Context,
	request OrderBookRequest,
	options ...trade.RequestOption,
) (OrderBook, error) {
	if err := request.validate(); err != nil {
		return OrderBook{}, err
	}
	path := fmt.Sprintf("/api/v1/market/orderbook/level2_%d", request.Size)
	response, err := client.executePublic(
		ctx, http.MethodGet, path, url.Values{"symbol": {request.Symbol}}, publicLimit(2), options...,
	)
	if err != nil {
		return OrderBook{}, err
	}
	var book OrderBook
	data, err := client.decodeData(response, commonexchange.OperationRead, &book)
	if err != nil {
		return OrderBook{}, err
	}
	book.Raw = cloneBytes(data)
	return book, nil
}

// RecentTrades는 지정한 Spot 거래쌍의 최근 공개 체결을 조회한다.
func (client *Client) RecentTrades(
	ctx context.Context,
	request RecentTradesRequest,
	options ...trade.RequestOption,
) ([]PublicTrade, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePublic(
		ctx, http.MethodGet, "/api/v1/market/histories", url.Values{"symbol": {request.Symbol}},
		publicLimit(3), options...,
	)
	if err != nil {
		return nil, err
	}
	var rawItems []json.RawMessage
	if _, err := client.decodeData(response, commonexchange.OperationRead, &rawItems); err != nil {
		return nil, err
	}
	items := make([]PublicTrade, len(rawItems))
	for index, raw := range rawItems {
		if err := json.Unmarshal(raw, &items[index]); err != nil {
			return nil, client.decodeBodyError(response, commonexchange.OperationRead, err)
		}
		items[index].Raw = cloneBytes(raw)
	}
	return items, nil
}

// Candles는 최대 1500개 Spot OHLCV 캔들을 최신순으로 조회한다.
func (client *Client) Candles(
	ctx context.Context,
	request CandlesRequest,
	options ...trade.RequestOption,
) ([]Candle, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePublic(
		ctx, http.MethodGet, "/api/v1/market/candles", request.values(), publicLimit(3), options...,
	)
	if err != nil {
		return nil, err
	}
	var candles []Candle
	if _, err := client.decodeData(response, commonexchange.OperationRead, &candles); err != nil {
		return nil, err
	}
	return candles, nil
}
