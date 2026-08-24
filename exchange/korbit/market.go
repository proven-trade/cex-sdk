package korbit

import (
	"context"
	"encoding/json"

	trade "github.com/proven-trade/proven-trade-sdk"
	commonexchange "github.com/proven-trade/proven-trade-sdk/exchange"
)

// ServerTime은 코빗 서버의 현재 Unix millisecond 시각을 조회한다.
func (client *Client) ServerTime(ctx context.Context, options ...trade.RequestOption) (ServerTime, error) {
	response, err := client.executePublic(ctx, "/v2/time", nil, options...)
	if err != nil {
		return ServerTime{}, err
	}
	var result ServerTime
	raw, err := client.decodeResponse(response, commonexchange.OperationRead, &result)
	if err != nil {
		return ServerTime{}, err
	}
	result.Raw = raw
	return result, nil
}

// Tickers는 일부 또는 전체 거래쌍의 현재가를 조회한다.
func (client *Client) Tickers(
	ctx context.Context,
	request TickersRequest,
	options ...trade.RequestOption,
) ([]Ticker, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePublic(ctx, "/v2/tickers", request.values(), options...)
	if err != nil {
		return nil, err
	}
	return decodeItems[Ticker](client, response, func(item *Ticker, raw []byte) { item.Raw = raw })
}

// OrderBook은 거래쌍의 호가 스냅샷을 조회한다.
func (client *Client) OrderBook(
	ctx context.Context,
	request OrderBookRequest,
	options ...trade.RequestOption,
) (OrderBook, error) {
	if err := request.validate(); err != nil {
		return OrderBook{}, err
	}
	response, err := client.executePublic(ctx, "/v2/orderbook", request.values(), options...)
	if err != nil {
		return OrderBook{}, err
	}
	var result OrderBook
	raw, err := client.decodeResponse(response, commonexchange.OperationRead, &result)
	if err != nil {
		return OrderBook{}, err
	}
	result.Raw = raw
	return result, nil
}

// RecentTrades는 거래쌍의 최근 공개 체결을 조회한다.
func (client *Client) RecentTrades(
	ctx context.Context,
	request RecentTradesRequest,
	options ...trade.RequestOption,
) ([]PublicTrade, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePublic(ctx, "/v2/trades", request.values(), options...)
	if err != nil {
		return nil, err
	}
	return decodeItems[PublicTrade](client, response, func(item *PublicTrade, raw []byte) { item.Raw = raw })
}

// Candles는 거래쌍의 OHLCV 캔들을 조회한다.
func (client *Client) Candles(
	ctx context.Context,
	request CandlesRequest,
	options ...trade.RequestOption,
) ([]Candle, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePublic(ctx, "/v2/candles", request.values(), options...)
	if err != nil {
		return nil, err
	}
	return decodeItems[Candle](client, response, func(item *Candle, raw []byte) { item.Raw = raw })
}

// CurrencyPairs는 코빗이 지원하는 전체 Spot 거래쌍과 주문 금액 범위를 조회한다.
func (client *Client) CurrencyPairs(
	ctx context.Context,
	options ...trade.RequestOption,
) ([]CurrencyPair, error) {
	response, err := client.executePublic(ctx, "/v2/currencyPairs", nil, options...)
	if err != nil {
		return nil, err
	}
	return decodeItems[CurrencyPair](client, response, func(item *CurrencyPair, raw []byte) { item.Raw = raw })
}

// TickSizePolicy는 거래쌍의 가격별 호가 단위 정책을 조회한다.
func (client *Client) TickSizePolicy(
	ctx context.Context,
	request TickSizePolicyRequest,
	options ...trade.RequestOption,
) ([]TickSizePolicy, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePublic(ctx, "/v2/tickSizePolicy", request.values(), options...)
	if err != nil {
		return nil, err
	}
	return decodeItems[TickSizePolicy](client, response, func(item *TickSizePolicy, raw []byte) { item.Raw = raw })
}

func decodeItems[T any](
	client *Client,
	response commonexchange.Response,
	setRaw func(*T, []byte),
) ([]T, error) {
	var rawItems []json.RawMessage
	if _, err := client.decodeResponse(response, commonexchange.OperationRead, &rawItems); err != nil {
		return nil, err
	}
	items := make([]T, len(rawItems))
	for index, rawItem := range rawItems {
		if err := json.Unmarshal(rawItem, &items[index]); err != nil {
			return nil, client.decodeBodyError(response, commonexchange.OperationRead, err)
		}
		if setRaw != nil {
			setRaw(&items[index], cloneBytes(rawItem))
		}
	}
	return items, nil
}
