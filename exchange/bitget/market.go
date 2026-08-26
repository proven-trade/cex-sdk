package bitget

import (
	"context"
	"net/http"

	trade "github.com/proven-trade/cex-sdk"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

// Instruments는 Spot 또는 USDT-M Futures 상품 규칙을 조회한다.
func (client *Client) Instruments(
	ctx context.Context,
	request InstrumentsRequest,
	options ...trade.RequestOption,
) ([]Instrument, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, _, err := client.executePublic(
		ctx,
		http.MethodGet,
		"/api/v3/market/instruments",
		request.values(),
		publicLimit(20),
		options...,
	)
	if err != nil {
		return nil, err
	}
	data, _, err := client.responseData(response, commonexchange.OperationRead)
	if err != nil {
		return nil, err
	}
	items, err := decodeItems(data, func(item *Instrument, raw []byte) { item.Raw = raw })
	if err != nil {
		return nil, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	return items, nil
}

// Tickers는 category 전체 또는 단일 상품의 현재 시세를 조회한다.
func (client *Client) Tickers(
	ctx context.Context,
	request TickersRequest,
	options ...trade.RequestOption,
) ([]Ticker, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, _, err := client.executePublic(
		ctx,
		http.MethodGet,
		"/api/v3/market/tickers",
		request.values(),
		publicLimit(20),
		options...,
	)
	if err != nil {
		return nil, err
	}
	data, _, err := client.responseData(response, commonexchange.OperationRead)
	if err != nil {
		return nil, err
	}
	items, err := decodeItems(data, func(item *Ticker, raw []byte) { item.Raw = raw })
	if err != nil {
		return nil, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	return items, nil
}

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
		"/api/v3/market/orderbook",
		request.values(),
		publicLimit(20),
		options...,
	)
	if err != nil {
		return OrderBook{}, err
	}
	data, _, err := client.responseData(response, commonexchange.OperationRead)
	if err != nil {
		return OrderBook{}, err
	}
	var orderBook OrderBook
	if err := decodeData(data, &orderBook); err != nil {
		return OrderBook{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	orderBook.Raw = cloneBytes(data)
	return orderBook, nil
}

// RecentFills는 지정한 상품의 최근 공개 체결을 조회한다.
func (client *Client) RecentFills(
	ctx context.Context,
	request RecentFillsRequest,
	options ...trade.RequestOption,
) ([]PublicFill, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, _, err := client.executePublic(
		ctx,
		http.MethodGet,
		"/api/v3/market/fills",
		request.values(),
		publicLimit(20),
		options...,
	)
	if err != nil {
		return nil, err
	}
	data, _, err := client.responseData(response, commonexchange.OperationRead)
	if err != nil {
		return nil, err
	}
	items, err := decodeItems[PublicFill](data, nil)
	if err != nil {
		return nil, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	return items, nil
}

// Candles는 Spot 또는 USDT-M Futures OHLCV 캔들을 조회한다.
func (client *Client) Candles(
	ctx context.Context,
	request CandlesRequest,
	options ...trade.RequestOption,
) ([]Candle, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, _, err := client.executePublic(
		ctx,
		http.MethodGet,
		"/api/v3/market/candles",
		request.values(),
		publicLimit(20),
		options...,
	)
	if err != nil {
		return nil, err
	}
	data, _, err := client.responseData(response, commonexchange.OperationRead)
	if err != nil {
		return nil, err
	}
	items, err := decodeItems[Candle](data, nil)
	if err != nil {
		return nil, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	return items, nil
}
