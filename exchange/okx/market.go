package okx

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

// ServerTime은 OKX 서버 시간을 조회하고 로컬 서명 시계 오프셋을 갱신한다.
func (client *Client) ServerTime(ctx context.Context, options ...trade.RequestOption) (time.Time, error) {
	var startedAt time.Time
	response, _, err := client.executePublicWithBuildHook(
		ctx,
		http.MethodGet,
		"/api/v5/public/time",
		nil,
		publicLimit(10, 2*time.Second, ""),
		func() { startedAt = client.now() },
		options...,
	)
	if err != nil {
		return time.Time{}, err
	}
	data, err := client.responseData(response, commonexchange.OperationRead)
	if err != nil {
		return time.Time{}, err
	}
	var items []struct {
		Timestamp string `json:"ts"`
	}
	if err := decodeData(data, &items); err != nil || len(items) == 0 {
		if err == nil {
			err = errors.New("OKX server time response is empty")
		}
		return time.Time{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	serverMillis, err := strconv.ParseInt(items[0].Timestamp, 10, 64)
	if err != nil || serverMillis <= 0 {
		if err == nil {
			err = errors.New("invalid OKX server time")
		}
		return time.Time{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	finishedAt := client.now()
	midpointMillis := startedAt.UnixMilli() + finishedAt.Sub(startedAt).Milliseconds()/2
	client.clockOffsetMillis.Store(serverMillis - midpointMillis)
	return time.UnixMilli(serverMillis), nil
}

// Instruments는 Spot 또는 SWAP 상품 규칙을 조회한다.
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
		"/api/v5/public/instruments",
		request.values(),
		publicLimit(20, 2*time.Second, string(request.InstrumentType)),
		options...,
	)
	if err != nil {
		return nil, err
	}
	data, err := client.responseData(response, commonexchange.OperationRead)
	if err != nil {
		return nil, err
	}
	items, err := decodeItems(data, func(item *Instrument, raw []byte) { item.Raw = raw })
	if err != nil {
		return nil, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	return items, nil
}

// Tickers는 상품 유형의 전체 현재 시세를 조회한다.
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
		"/api/v5/market/tickers",
		request.values(),
		publicLimit(20, 2*time.Second, ""),
		options...,
	)
	if err != nil {
		return nil, err
	}
	data, err := client.responseData(response, commonexchange.OperationRead)
	if err != nil {
		return nil, err
	}
	items, err := decodeItems(data, func(item *Ticker, raw []byte) { item.Raw = raw })
	if err != nil {
		return nil, client.decodeBodyError(response, commonexchange.OperationRead, err)
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
		ctx,
		http.MethodGet,
		"/api/v5/market/books",
		request.values(),
		publicLimit(40, 2*time.Second, ""),
		options...,
	)
	if err != nil {
		return OrderBook{}, err
	}
	data, err := client.responseData(response, commonexchange.OperationRead)
	if err != nil {
		return OrderBook{}, err
	}
	items, err := decodeItems(data, func(item *OrderBook, raw []byte) { item.Raw = raw })
	if err != nil || len(items) == 0 {
		if err == nil {
			err = errors.New("OKX order book response is empty")
		}
		return OrderBook{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	return items[0], nil
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
		"/api/v5/market/trades",
		request.values(),
		publicLimit(100, 2*time.Second, ""),
		options...,
	)
	if err != nil {
		return nil, err
	}
	data, err := client.responseData(response, commonexchange.OperationRead)
	if err != nil {
		return nil, err
	}
	items, err := decodeItems(data, func(item *PublicTrade, raw []byte) { item.Raw = raw })
	if err != nil {
		return nil, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	return items, nil
}

// Candles는 Spot 또는 SWAP OHLCV 캔들을 조회한다.
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
		"/api/v5/market/candles",
		request.values(),
		publicLimit(40, 2*time.Second, ""),
		options...,
	)
	if err != nil {
		return nil, err
	}
	data, err := client.responseData(response, commonexchange.OperationRead)
	if err != nil {
		return nil, err
	}
	items, err := decodeItems[Candle](data, nil)
	if err != nil {
		return nil, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	return items, nil
}
