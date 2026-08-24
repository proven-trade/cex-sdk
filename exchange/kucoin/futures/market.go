package futures

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	trade "github.com/proven-trade/proven-trade-sdk"
	commonexchange "github.com/proven-trade/proven-trade-sdk/exchange"
)

// Contracts는 현재 거래 가능한 모든 KuCoin Futures 계약과 주문 규칙을 조회한다.
func (client *Client) Contracts(
	ctx context.Context,
	options ...trade.RequestOption,
) ([]Contract, error) {
	response, err := client.executePublic(
		ctx, http.MethodGet, "/api/v1/contracts/active", nil, publicLimit(3), options...,
	)
	if err != nil {
		return nil, err
	}
	var rawItems []json.RawMessage
	if _, err := client.decodeData(response, commonexchange.OperationRead, &rawItems); err != nil {
		return nil, err
	}
	items := make([]Contract, len(rawItems))
	for index, raw := range rawItems {
		if err := json.Unmarshal(raw, &items[index]); err != nil {
			return nil, client.decodeBodyError(response, commonexchange.OperationRead, err)
		}
		items[index].Raw = cloneBytes(raw)
	}
	return items, nil
}

// Contract는 지정한 KuCoin Futures 계약의 규칙과 현재 시장 상태를 조회한다.
func (client *Client) Contract(
	ctx context.Context,
	symbol string,
	options ...trade.RequestOption,
) (Contract, error) {
	if err := validateSymbol(symbol); err != nil {
		return Contract{}, err
	}
	response, err := client.executePublic(
		ctx, http.MethodGet, "/api/v1/contracts/"+url.PathEscape(symbol), nil,
		publicLimit(3), options...,
	)
	if err != nil {
		return Contract{}, err
	}
	var contract Contract
	data, err := client.decodeData(response, commonexchange.OperationRead, &contract)
	if err != nil {
		return Contract{}, err
	}
	contract.Raw = cloneBytes(data)
	return contract, nil
}

// Ticker는 지정한 Futures 계약의 최근 체결과 최우선 호가를 조회한다.
func (client *Client) Ticker(
	ctx context.Context,
	symbol string,
	options ...trade.RequestOption,
) (Ticker, error) {
	if err := validateSymbol(symbol); err != nil {
		return Ticker{}, err
	}
	response, err := client.executePublic(
		ctx, http.MethodGet, "/api/v1/ticker", url.Values{"symbol": {symbol}},
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

// OrderBook은 20개 또는 100개 깊이의 Futures 호가 snapshot을 조회한다.
func (client *Client) OrderBook(
	ctx context.Context,
	request OrderBookRequest,
	options ...trade.RequestOption,
) (OrderBook, error) {
	if err := request.validate(); err != nil {
		return OrderBook{}, err
	}
	path := fmt.Sprintf("/api/v1/level2/depth%d", request.Size)
	response, err := client.executePublic(
		ctx, http.MethodGet, path, url.Values{"symbol": {request.Symbol}},
		publicLimit(5), options...,
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

// RecentTrades는 지정한 Futures 계약의 최근 공개 체결을 조회한다.
func (client *Client) RecentTrades(
	ctx context.Context,
	request RecentTradesRequest,
	options ...trade.RequestOption,
) ([]PublicTrade, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePublic(
		ctx, http.MethodGet, "/api/v1/trade/history", url.Values{"symbol": {request.Symbol}},
		publicLimit(5), options...,
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

// Candles는 최대 500개 Futures OHLCV 캔들을 조회한다.
func (client *Client) Candles(
	ctx context.Context,
	request CandlesRequest,
	options ...trade.RequestOption,
) ([]Candle, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePublic(
		ctx, http.MethodGet, "/api/v1/kline/query", request.values(), publicLimit(3), options...,
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
