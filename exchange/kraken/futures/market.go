package futures

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	trade "github.com/proven-trade/proven-trade-sdk"
	commonexchange "github.com/proven-trade/proven-trade-sdk/exchange"
)

// Instruments는 현재 상장된 Futures 상품 규칙을 조회한다.
func (client *Client) Instruments(
	ctx context.Context,
	request InstrumentsRequest,
	options ...trade.RequestOption,
) ([]Instrument, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePublic(ctx, derivativesPrefix+"instruments", request.values(), options...)
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Instruments []json.RawMessage `json:"instruments"`
	}
	if err := client.decodeSuccess(response, commonexchange.OperationRead, &envelope); err != nil {
		return nil, err
	}
	return decodeRawItems(envelope.Instruments, func(item *Instrument, raw []byte) { item.Raw = raw })
}

// Tickers는 모든 Futures 상품의 현재 시장 요약을 조회한다.
func (client *Client) Tickers(
	ctx context.Context,
	request TickersRequest,
	options ...trade.RequestOption,
) ([]Ticker, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePublic(ctx, derivativesPrefix+"tickers", request.values(), options...)
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Tickers []json.RawMessage `json:"tickers"`
	}
	if err := client.decodeSuccess(response, commonexchange.OperationRead, &envelope); err != nil {
		return nil, err
	}
	return decodeRawItems(envelope.Tickers, func(item *Ticker, raw []byte) { item.Raw = raw })
}

// OrderBook은 단일 Futures 상품의 전체 비누적 호가를 조회한다.
func (client *Client) OrderBook(
	ctx context.Context,
	request OrderBookRequest,
	options ...trade.RequestOption,
) (OrderBook, error) {
	if err := request.validate(); err != nil {
		return OrderBook{}, err
	}
	response, err := client.executePublic(ctx, derivativesPrefix+"orderbook", request.values(), options...)
	if err != nil {
		return OrderBook{}, err
	}
	var envelope struct {
		OrderBook struct {
			Bids [][]json.RawMessage `json:"bids"`
			Asks [][]json.RawMessage `json:"asks"`
		} `json:"orderBook"`
		ServerTime string `json:"serverTime"`
	}
	if err := client.decodeSuccess(response, commonexchange.OperationRead, &envelope); err != nil {
		return OrderBook{}, err
	}
	bids, err := decodeBookLevels(envelope.OrderBook.Bids)
	if err != nil {
		return OrderBook{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	asks, err := decodeBookLevels(envelope.OrderBook.Asks)
	if err != nil {
		return OrderBook{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	return OrderBook{
		Bids: bids, Asks: asks, ServerTime: envelope.ServerTime, Raw: cloneBytes(response.Body),
	}, nil
}

// PublicHistory는 지정 상품의 최근 공개 체결을 최신순으로 조회한다.
func (client *Client) PublicHistory(
	ctx context.Context,
	request PublicHistoryRequest,
	options ...trade.RequestOption,
) (TradeHistory, error) {
	if err := request.validate(); err != nil {
		return TradeHistory{}, err
	}
	response, err := client.executePublic(ctx, derivativesPrefix+"history", request.values(), options...)
	if err != nil {
		return TradeHistory{}, err
	}
	var envelope struct {
		History    []json.RawMessage `json:"history"`
		ServerTime string            `json:"serverTime"`
	}
	if err := client.decodeSuccess(response, commonexchange.OperationRead, &envelope); err != nil {
		return TradeHistory{}, err
	}
	trades, err := decodeRawItems(envelope.History, func(item *PublicTrade, raw []byte) { item.Raw = raw })
	if err != nil {
		return TradeHistory{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	return TradeHistory{Trades: trades, ServerTime: envelope.ServerTime, Raw: cloneBytes(response.Body)}, nil
}

// Candles는 가격 기준과 구간별 OHLCV 차트 데이터를 조회한다.
func (client *Client) Candles(
	ctx context.Context,
	request CandlesRequest,
	options ...trade.RequestOption,
) (CandlePage, error) {
	if err := request.validate(); err != nil {
		return CandlePage{}, err
	}
	path := fmt.Sprintf(
		"/api/charts/v1/%s/%s/%s",
		url.PathEscape(string(request.TickType)),
		url.PathEscape(request.Symbol),
		url.PathEscape(string(request.Resolution)),
	)
	response, err := client.executePublic(ctx, path, request.values(), options...)
	if err != nil {
		return CandlePage{}, err
	}
	var envelope struct {
		Candles     []Candle `json:"candles"`
		MoreCandles bool     `json:"more_candles"`
	}
	if err := client.decodeSuccess(response, commonexchange.OperationRead, &envelope); err != nil {
		return CandlePage{}, err
	}
	return CandlePage{
		Candles: envelope.Candles, MoreCandles: envelope.MoreCandles, Raw: cloneBytes(response.Body),
	}, nil
}

func decodeBookLevels(rows [][]json.RawMessage) ([]BookLevel, error) {
	levels := make([]BookLevel, len(rows))
	for index, row := range rows {
		if len(row) != 2 {
			return nil, fmt.Errorf("Kraken Futures book row %d must contain price and size", index)
		}
		if err := json.Unmarshal(row[0], &levels[index].Price); err != nil {
			return nil, fmt.Errorf("decode Kraken Futures book price %d: %w", index, err)
		}
		if err := json.Unmarshal(row[1], &levels[index].Size); err != nil {
			return nil, fmt.Errorf("decode Kraken Futures book size %d: %w", index, err)
		}
	}
	return levels, nil
}

func decodeRawItems[T any](
	rawItems []json.RawMessage,
	setRaw func(*T, []byte),
) ([]T, error) {
	items := make([]T, len(rawItems))
	for index, raw := range rawItems {
		if err := json.Unmarshal(raw, &items[index]); err != nil {
			return nil, fmt.Errorf("decode Kraken Futures item %d: %w", index, err)
		}
		setRaw(&items[index], cloneBytes(raw))
	}
	return items, nil
}
