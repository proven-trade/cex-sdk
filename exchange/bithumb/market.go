package bithumb

import (
	"context"
	"fmt"
	"strconv"

	trade "github.com/proven-trade/cex-sdk"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

// Markets는 빗썸이 지원하는 전체 마켓을 조회한다.
func (client *Client) Markets(
	ctx context.Context,
	request MarketsRequest,
	options ...trade.RequestOption,
) ([]Market, error) {
	response, err := client.executePublic(ctx, "/v1/market/all", request.parameters(), options...)
	if err != nil {
		return nil, err
	}
	return decodeItems(client, response, commonexchange.OperationRead, func(item *Market, raw []byte) {
		item.Raw = raw
	})
}

// Tickers는 지정한 마켓의 현재가와 누적 거래 정보를 조회한다.
func (client *Client) Tickers(
	ctx context.Context,
	request TickersRequest,
	options ...trade.RequestOption,
) ([]Ticker, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePublic(ctx, "/v1/ticker", request.parameters(), options...)
	if err != nil {
		return nil, err
	}
	return decodeItems(client, response, commonexchange.OperationRead, func(item *Ticker, raw []byte) {
		item.Raw = raw
	})
}

// OrderBooks는 지정한 마켓의 호가 스냅샷을 조회한다.
func (client *Client) OrderBooks(
	ctx context.Context,
	request OrderBooksRequest,
	options ...trade.RequestOption,
) ([]OrderBook, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePublic(ctx, "/v1/orderbook", request.parameters(), options...)
	if err != nil {
		return nil, err
	}
	return decodeItems(client, response, commonexchange.OperationRead, func(item *OrderBook, raw []byte) {
		item.Raw = raw
	})
}

// RecentTrades는 지정한 마켓의 최근 공개 체결을 조회한다.
func (client *Client) RecentTrades(
	ctx context.Context,
	request RecentTradesRequest,
	options ...trade.RequestOption,
) ([]PublicTrade, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePublic(ctx, "/v1/trades/ticks", request.parameters(), options...)
	if err != nil {
		return nil, err
	}
	return decodeItems(client, response, commonexchange.OperationRead, func(item *PublicTrade, raw []byte) {
		item.Raw = raw
	})
}

// MinuteCandles는 지정한 마켓의 분 단위 OHLCV 캔들을 조회한다.
func (client *Client) MinuteCandles(
	ctx context.Context,
	request MinuteCandlesRequest,
	options ...trade.RequestOption,
) ([]Candle, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	path := "/v1/candles/minutes/" + strconv.Itoa(int(request.Unit))
	response, err := client.executePublic(ctx, path, request.parameters(), options...)
	if err != nil {
		return nil, err
	}
	return decodeItems(client, response, commonexchange.OperationRead, func(item *Candle, raw []byte) {
		item.Raw = raw
	})
}

func decodeItems[T any](
	client *Client,
	response commonexchange.Response,
	operation commonexchange.OperationKind,
	setRaw func(*T, []byte),
) ([]T, error) {
	var rawItems []jsonRawMessage
	if err := client.decodeResponse(response, operation, &rawItems); err != nil {
		return nil, err
	}
	items := make([]T, len(rawItems))
	for index, rawItem := range rawItems {
		if err := decodeJSON(rawItem, &items[index]); err != nil {
			return nil, client.decodeBodyError(
				response, operation, fmt.Errorf("decode Bithumb response item: %w", err),
			)
		}
		if setRaw != nil {
			setRaw(&items[index], cloneBytes(rawItem))
		}
	}
	return items, nil
}
