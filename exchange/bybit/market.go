package bybit

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	trade "github.com/proven-trade/proven-trade-sdk"
	commonexchange "github.com/proven-trade/proven-trade-sdk/exchange"
)

// ServerTime은 Bybit 서버 시간을 조회하고 로컬 서명 시계 오프셋을 갱신한다.
func (client *Client) ServerTime(ctx context.Context, options ...trade.RequestOption) (time.Time, error) {
	var startedAt time.Time
	response, _, err := client.executePublicWithBuildHook(
		ctx,
		http.MethodGet,
		"/v5/market/time",
		nil,
		publicLimit(50),
		func() { startedAt = client.now() },
		options...,
	)
	if err != nil {
		return time.Time{}, err
	}
	_, serverMillis, err := client.responseResult(response, commonexchange.OperationRead)
	if err != nil {
		return time.Time{}, err
	}
	if serverMillis <= 0 {
		return time.Time{}, client.decodeBodyError(
			response, commonexchange.OperationRead, errors.New("invalid Bybit server time"),
		)
	}
	finishedAt := client.now()
	midpointMillis := startedAt.UnixMilli() + finishedAt.Sub(startedAt).Milliseconds()/2
	client.clockOffsetMillis.Store(serverMillis - midpointMillis)
	return time.UnixMilli(serverMillis), nil
}

// Instruments는 Spot 또는 Linear 상품 규칙을 cursor 기반으로 조회한다.
func (client *Client) Instruments(
	ctx context.Context,
	request InstrumentsRequest,
	options ...trade.RequestOption,
) (InstrumentPage, error) {
	if err := request.validate(); err != nil {
		return InstrumentPage{}, err
	}
	response, _, err := client.executePublic(
		ctx, http.MethodGet, "/v5/market/instruments-info", request.values(), publicLimit(20), options...,
	)
	if err != nil {
		return InstrumentPage{}, err
	}
	result, _, err := client.responseResult(response, commonexchange.OperationRead)
	if err != nil {
		return InstrumentPage{}, err
	}
	var rawPage struct {
		Category       Category          `json:"category"`
		Items          []json.RawMessage `json:"list"`
		NextPageCursor string            `json:"nextPageCursor"`
	}
	if err := decodeResult(result, &rawPage); err != nil {
		return InstrumentPage{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	page := InstrumentPage{
		Category: rawPage.Category, Instruments: make([]Instrument, len(rawPage.Items)),
		NextPageCursor: rawPage.NextPageCursor, Raw: cloneBytes(result),
	}
	for index, item := range rawPage.Items {
		if err := decodeResult(item, &page.Instruments[index]); err != nil {
			return InstrumentPage{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
		}
		page.Instruments[index].Raw = cloneBytes(item)
	}
	return page, nil
}

// Tickers는 category 전체 또는 조건에 맞는 현재 시세를 조회한다.
func (client *Client) Tickers(
	ctx context.Context,
	request TickersRequest,
	options ...trade.RequestOption,
) ([]Ticker, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, _, err := client.executePublic(
		ctx, http.MethodGet, "/v5/market/tickers", request.values(), publicLimit(20), options...,
	)
	if err != nil {
		return nil, err
	}
	result, _, err := client.responseResult(response, commonexchange.OperationRead)
	if err != nil {
		return nil, err
	}
	var page struct {
		Items []json.RawMessage `json:"list"`
	}
	if err := decodeResult(result, &page); err != nil {
		return nil, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	items := make([]Ticker, len(page.Items))
	for index, item := range page.Items {
		if err := decodeResult(item, &items[index]); err != nil {
			return nil, client.decodeBodyError(response, commonexchange.OperationRead, err)
		}
		items[index].Raw = cloneBytes(item)
	}
	return items, nil
}

// OrderBook은 지정한 상품의 현재 호가 스냅샷을 조회한다.
func (client *Client) OrderBook(
	ctx context.Context,
	request OrderBookRequest,
	options ...trade.RequestOption,
) (OrderBook, error) {
	if err := request.validate(); err != nil {
		return OrderBook{}, err
	}
	response, _, err := client.executePublic(
		ctx, http.MethodGet, "/v5/market/orderbook", request.values(), publicLimit(20), options...,
	)
	if err != nil {
		return OrderBook{}, err
	}
	result, _, err := client.responseResult(response, commonexchange.OperationRead)
	if err != nil {
		return OrderBook{}, err
	}
	var orderBook OrderBook
	if err := decodeResult(result, &orderBook); err != nil {
		return OrderBook{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	orderBook.Raw = cloneBytes(result)
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
		ctx, http.MethodGet, "/v5/market/recent-trade", request.values(), publicLimit(20), options...,
	)
	if err != nil {
		return nil, err
	}
	result, _, err := client.responseResult(response, commonexchange.OperationRead)
	if err != nil {
		return nil, err
	}
	var page struct {
		Items []json.RawMessage `json:"list"`
	}
	if err := decodeResult(result, &page); err != nil {
		return nil, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	items := make([]PublicTrade, len(page.Items))
	for index, item := range page.Items {
		if err := decodeResult(item, &items[index]); err != nil {
			return nil, client.decodeBodyError(response, commonexchange.OperationRead, err)
		}
		items[index].Raw = cloneBytes(item)
	}
	return items, nil
}

// Candles는 Spot 또는 Linear OHLCV 캔들을 조회한다.
func (client *Client) Candles(
	ctx context.Context,
	request CandlesRequest,
	options ...trade.RequestOption,
) ([]Candle, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, _, err := client.executePublic(
		ctx, http.MethodGet, "/v5/market/kline", request.values(), publicLimit(20), options...,
	)
	if err != nil {
		return nil, err
	}
	result, _, err := client.responseResult(response, commonexchange.OperationRead)
	if err != nil {
		return nil, err
	}
	var page struct {
		Items []Candle `json:"list"`
	}
	if err := decodeResult(result, &page); err != nil {
		return nil, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	return page.Items, nil
}
