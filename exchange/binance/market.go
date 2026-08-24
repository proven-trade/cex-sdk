package binance

import (
	"context"
	"net/http"
	"net/url"

	trade "github.com/proven-trade/proven-trade-sdk"
	commonexchange "github.com/proven-trade/proven-trade-sdk/exchange"
)

// OrderBook은 지정한 상품의 현재 호가를 조회한다.
func (client *Client) OrderBook(
	ctx context.Context,
	request OrderBookRequest,
	options ...trade.RequestOption,
) (OrderBook, error) {
	if err := request.validate(); err != nil {
		return OrderBook{}, err
	}
	response, _, err := client.executePublic(
		ctx,
		http.MethodGet,
		"/api/v3/depth",
		request.values(),
		request.weight(),
		options...,
	)
	if err != nil {
		return OrderBook{}, err
	}
	if err := client.ensureSuccess(response, commonexchange.OperationRead); err != nil {
		return OrderBook{}, err
	}
	var orderBook OrderBook
	if err := decodeJSON(response.Body, &orderBook); err != nil {
		return OrderBook{}, err
	}
	orderBook.Raw = cloneBytes(response.Body)
	return orderBook, nil
}

// RecentTrades는 지정한 상품의 최근 공개 체결을 조회한다.
func (client *Client) RecentTrades(
	ctx context.Context,
	request RecentTradesRequest,
	options ...trade.RequestOption,
) ([]PublicTrade, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, _, err := client.executePublic(
		ctx,
		http.MethodGet,
		"/api/v3/trades",
		request.values(),
		25,
		options...,
	)
	if err != nil {
		return nil, err
	}
	if err := client.ensureSuccess(response, commonexchange.OperationRead); err != nil {
		return nil, err
	}
	var trades []PublicTrade
	if err := decodeJSON(response.Body, &trades); err != nil {
		return nil, err
	}
	return trades, nil
}

// BookTicker는 지정한 상품의 최우선 매수·매도 호가를 조회한다.
func (client *Client) BookTicker(
	ctx context.Context,
	request BookTickerRequest,
	options ...trade.RequestOption,
) (BookTicker, error) {
	if err := validateSymbol(request.Symbol); err != nil {
		return BookTicker{}, err
	}
	values := make(url.Values)
	values.Set("symbol", request.Symbol)
	response, _, err := client.executePublic(
		ctx,
		http.MethodGet,
		"/api/v3/ticker/bookTicker",
		values,
		2,
		options...,
	)
	if err != nil {
		return BookTicker{}, err
	}
	if err := client.ensureSuccess(response, commonexchange.OperationRead); err != nil {
		return BookTicker{}, err
	}
	var ticker BookTicker
	if err := decodeJSON(response.Body, &ticker); err != nil {
		return BookTicker{}, err
	}
	ticker.Raw = cloneBytes(response.Body)
	return ticker, nil
}

// Klines는 지정한 상품의 OHLCV 캔들을 조회한다.
func (client *Client) Klines(
	ctx context.Context,
	request KlinesRequest,
	options ...trade.RequestOption,
) ([]Kline, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, _, err := client.executePublic(
		ctx,
		http.MethodGet,
		"/api/v3/klines",
		request.values(),
		2,
		options...,
	)
	if err != nil {
		return nil, err
	}
	if err := client.ensureSuccess(response, commonexchange.OperationRead); err != nil {
		return nil, err
	}
	var klines []Kline
	if err := decodeJSON(response.Body, &klines); err != nil {
		return nil, err
	}
	return klines, nil
}
