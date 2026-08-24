package gateio

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"

	trade "github.com/proven-trade/proven-trade-sdk"
	commonexchange "github.com/proven-trade/proven-trade-sdk/exchange"
)

// CurrencyPairs는 Gate.io의 모든 Spot 거래쌍과 주문 규칙을 조회한다.
func (client *Client) CurrencyPairs(
	ctx context.Context,
	options ...trade.RequestOption,
) ([]CurrencyPair, error) {
	response, err := client.executePublic(
		ctx, http.MethodGet, "/spot/currency_pairs", nil,
		publicLimit("currency-pairs"), options...,
	)
	if err != nil {
		return nil, err
	}
	var rawItems []json.RawMessage
	if _, err := client.decodeData(response, commonexchange.OperationRead, &rawItems); err != nil {
		return nil, err
	}
	items := make([]CurrencyPair, len(rawItems))
	for index, raw := range rawItems {
		if err := json.Unmarshal(raw, &items[index]); err != nil {
			return nil, client.decodeBodyError(response, commonexchange.OperationRead, err)
		}
		items[index].Raw = cloneBytes(raw)
	}
	return items, nil
}

// Ticker는 지정한 Spot 거래쌍의 최근가·최우선 호가와 24시간 통계를 조회한다.
func (client *Client) Ticker(
	ctx context.Context,
	currencyPair string,
	options ...trade.RequestOption,
) (Ticker, error) {
	if err := validateCurrencyPair(currencyPair); err != nil {
		return Ticker{}, err
	}
	response, err := client.executePublic(
		ctx, http.MethodGet, "/spot/tickers", url.Values{"currency_pair": {currencyPair}},
		publicLimit("tickers"), options...,
	)
	if err != nil {
		return Ticker{}, err
	}
	var rawItems []json.RawMessage
	if _, err := client.decodeData(response, commonexchange.OperationRead, &rawItems); err != nil {
		return Ticker{}, err
	}
	if len(rawItems) != 1 {
		return Ticker{}, client.decodeBodyError(
			response, commonexchange.OperationRead,
			errors.New("Gate.io ticker response must contain exactly one item"),
		)
	}
	var ticker Ticker
	if err := json.Unmarshal(rawItems[0], &ticker); err != nil {
		return Ticker{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	ticker.Raw = cloneBytes(rawItems[0])
	return ticker, nil
}

// OrderBook은 지정한 Spot 거래쌍의 호가 snapshot을 조회한다.
func (client *Client) OrderBook(
	ctx context.Context,
	request OrderBookRequest,
	options ...trade.RequestOption,
) (OrderBook, error) {
	if err := request.validate(); err != nil {
		return OrderBook{}, err
	}
	response, err := client.executePublic(
		ctx, http.MethodGet, "/spot/order_book", request.values(),
		publicLimit("order-book"), options...,
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
	request TradesRequest,
	options ...trade.RequestOption,
) ([]Trade, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePublic(
		ctx, http.MethodGet, "/spot/trades", request.values(),
		publicLimit("trades"), options...,
	)
	if err != nil {
		return nil, err
	}
	return client.decodeTrades(response)
}

// Candles는 최근 개수 또는 시간 범위에 해당하는 Spot OHLCV 캔들을 조회한다.
func (client *Client) Candles(
	ctx context.Context,
	request CandlesRequest,
	options ...trade.RequestOption,
) ([]Candle, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePublic(
		ctx, http.MethodGet, "/spot/candlesticks", request.values(),
		publicLimit("candlesticks"), options...,
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

func (client *Client) decodeTrades(response commonexchange.Response) ([]Trade, error) {
	var rawItems []json.RawMessage
	if _, err := client.decodeData(response, commonexchange.OperationRead, &rawItems); err != nil {
		return nil, err
	}
	items := make([]Trade, len(rawItems))
	for index, raw := range rawItems {
		if err := json.Unmarshal(raw, &items[index]); err != nil {
			return nil, client.decodeBodyError(response, commonexchange.OperationRead, err)
		}
		items[index].Raw = cloneBytes(raw)
	}
	return items, nil
}
