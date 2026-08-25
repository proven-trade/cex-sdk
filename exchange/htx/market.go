package htx

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"

	trade "github.com/proven-trade/proven-trade-sdk"
	commonexchange "github.com/proven-trade/proven-trade-sdk/exchange"
)

// ServerTime은 HTX 서버의 Unix millisecond 시각을 조회한다.
func (client *Client) ServerTime(
	ctx context.Context,
	options ...trade.RequestOption,
) (ServerTime, error) {
	response, err := client.executePublic(
		ctx, "/v1/common/timestamp", nil, "timestamp", options...,
	)
	if err != nil {
		return ServerTime{}, err
	}
	var envelope struct {
		Data int64 `json:"data"`
	}
	raw, err := client.decodeResponse(response, &envelope)
	if err != nil {
		return ServerTime{}, err
	}
	if envelope.Data <= 0 {
		return ServerTime{}, client.decodeBodyError(
			response, errors.New("HTX server time is invalid"),
		)
	}
	return ServerTime{Time: envelope.Data, Raw: raw}, nil
}

// MarketSymbols는 HTX Spot 거래쌍의 상태와 주문 정밀도·한도를 조회한다.
func (client *Client) MarketSymbols(
	ctx context.Context,
	request MarketSymbolsRequest,
	options ...trade.RequestOption,
) (MarketSymbols, error) {
	if err := request.validate(); err != nil {
		return MarketSymbols{}, err
	}
	response, err := client.executePublic(
		ctx, "/v1/settings/common/market-symbols", request.values(),
		"market-symbols", options...,
	)
	if err != nil {
		return MarketSymbols{}, err
	}
	var envelope struct {
		Data []json.RawMessage `json:"data"`
		TS   Scalar            `json:"ts"`
		Full int               `json:"full"`
	}
	raw, err := client.decodeResponse(response, &envelope)
	if err != nil {
		return MarketSymbols{}, err
	}
	value := MarketSymbols{
		Symbols:   make([]MarketSymbol, len(envelope.Data)),
		UpdatedAt: envelope.TS, Full: envelope.Full, Raw: raw,
	}
	for index, itemRaw := range envelope.Data {
		if err := json.Unmarshal(itemRaw, &value.Symbols[index]); err != nil {
			return MarketSymbols{}, client.decodeBodyError(response, err)
		}
		value.Symbols[index].Raw = cloneBytes(itemRaw)
	}
	return value, nil
}

// Ticker는 지정한 Spot 거래쌍의 최근가·최우선 호가와 24시간 통계를 조회한다.
func (client *Client) Ticker(
	ctx context.Context,
	symbol string,
	options ...trade.RequestOption,
) (AggregatedTicker, error) {
	if err := validateSymbol(symbol); err != nil {
		return AggregatedTicker{}, err
	}
	response, err := client.executePublic(
		ctx, "/market/detail/merged", url.Values{"symbol": {symbol}},
		"ticker", options...,
	)
	if err != nil {
		return AggregatedTicker{}, err
	}
	var envelope struct {
		Timestamp int64           `json:"ts"`
		Tick      json.RawMessage `json:"tick"`
	}
	if _, err := client.decodeResponse(response, &envelope); err != nil {
		return AggregatedTicker{}, err
	}
	var wire struct {
		ID      Scalar    `json:"id"`
		Version Scalar    `json:"version"`
		Open    Decimal   `json:"open"`
		Close   Decimal   `json:"close"`
		Low     Decimal   `json:"low"`
		High    Decimal   `json:"high"`
		Amount  Decimal   `json:"amount"`
		Volume  Decimal   `json:"vol"`
		Count   int64     `json:"count"`
		Bid     BookLevel `json:"bid"`
		Ask     BookLevel `json:"ask"`
	}
	if err := json.Unmarshal(envelope.Tick, &wire); err != nil {
		return AggregatedTicker{}, client.decodeBodyError(response, err)
	}
	return AggregatedTicker{
		ID: wire.ID, Version: wire.Version, Open: wire.Open, Close: wire.Close,
		Low: wire.Low, High: wire.High, Amount: wire.Amount, Volume: wire.Volume,
		Count: wire.Count, Bid: wire.Bid, Ask: wire.Ask,
		Timestamp: envelope.Timestamp, Raw: cloneBytes(envelope.Tick),
	}, nil
}

// Tickers는 모든 Spot 거래쌍의 ticker를 조회한다.
func (client *Client) Tickers(
	ctx context.Context,
	options ...trade.RequestOption,
) ([]MarketTicker, error) {
	response, err := client.executePublic(
		ctx, "/market/tickers", nil, "tickers", options...,
	)
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Data []json.RawMessage `json:"data"`
	}
	if _, err := client.decodeResponse(response, &envelope); err != nil {
		return nil, err
	}
	items := make([]MarketTicker, len(envelope.Data))
	for index, itemRaw := range envelope.Data {
		if err := json.Unmarshal(itemRaw, &items[index]); err != nil {
			return nil, client.decodeBodyError(response, err)
		}
		items[index].Raw = cloneBytes(itemRaw)
	}
	return items, nil
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
		ctx, "/market/depth", request.values(), "order-book", options...,
	)
	if err != nil {
		return OrderBook{}, err
	}
	var envelope struct {
		Tick json.RawMessage `json:"tick"`
	}
	if _, err := client.decodeResponse(response, &envelope); err != nil {
		return OrderBook{}, err
	}
	var wire struct {
		Timestamp int64       `json:"ts"`
		Version   Scalar      `json:"version"`
		Bids      []BookLevel `json:"bids"`
		Asks      []BookLevel `json:"asks"`
	}
	if err := json.Unmarshal(envelope.Tick, &wire); err != nil {
		return OrderBook{}, client.decodeBodyError(response, err)
	}
	return OrderBook{
		Timestamp: wire.Timestamp, Version: wire.Version,
		Bids: wire.Bids, Asks: wire.Asks, Raw: cloneBytes(envelope.Tick),
	}, nil
}

// LatestTrade는 지정한 Spot 거래쌍의 가장 최근 체결 묶음을 조회한다.
func (client *Client) LatestTrade(
	ctx context.Context,
	symbol string,
	options ...trade.RequestOption,
) (TradeBatch, error) {
	if err := validateSymbol(symbol); err != nil {
		return TradeBatch{}, err
	}
	response, err := client.executePublic(
		ctx, "/market/trade", url.Values{"symbol": {symbol}},
		"latest-trade", options...,
	)
	if err != nil {
		return TradeBatch{}, err
	}
	var envelope struct {
		Tick json.RawMessage `json:"tick"`
	}
	if _, err := client.decodeResponse(response, &envelope); err != nil {
		return TradeBatch{}, err
	}
	return client.decodeTradeBatch(response, envelope.Tick)
}

// RecentTrades는 지정한 Spot 거래쌍의 최근 체결 묶음을 조회한다.
func (client *Client) RecentTrades(
	ctx context.Context,
	request TradesRequest,
	options ...trade.RequestOption,
) ([]TradeBatch, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePublic(
		ctx, "/market/history/trade", request.values(), "recent-trades", options...,
	)
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Data []json.RawMessage `json:"data"`
	}
	if _, err := client.decodeResponse(response, &envelope); err != nil {
		return nil, err
	}
	items := make([]TradeBatch, len(envelope.Data))
	for index, itemRaw := range envelope.Data {
		item, err := client.decodeTradeBatch(response, itemRaw)
		if err != nil {
			return nil, err
		}
		items[index] = item
	}
	return items, nil
}

// Candles는 지정한 Spot 거래쌍의 최근 OHLCV 캔들을 조회한다.
func (client *Client) Candles(
	ctx context.Context,
	request CandlesRequest,
	options ...trade.RequestOption,
) ([]Candle, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePublic(
		ctx, "/market/history/kline", request.values(), "candles", options...,
	)
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Data []json.RawMessage `json:"data"`
	}
	if _, err := client.decodeResponse(response, &envelope); err != nil {
		return nil, err
	}
	items := make([]Candle, len(envelope.Data))
	for index, itemRaw := range envelope.Data {
		if err := json.Unmarshal(itemRaw, &items[index]); err != nil {
			return nil, client.decodeBodyError(response, err)
		}
		items[index].Raw = cloneBytes(itemRaw)
	}
	return items, nil
}

func (client *Client) decodeTradeBatch(
	response commonexchange.Response,
	raw json.RawMessage,
) (TradeBatch, error) {
	var wire struct {
		ID        Scalar            `json:"id"`
		Timestamp int64             `json:"ts"`
		Data      []json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return TradeBatch{}, client.decodeBodyError(response, err)
	}
	value := TradeBatch{
		ID: wire.ID, Timestamp: wire.Timestamp,
		Trades: make([]PublicTrade, len(wire.Data)), Raw: cloneBytes(raw),
	}
	for index, itemRaw := range wire.Data {
		if err := json.Unmarshal(itemRaw, &value.Trades[index]); err != nil {
			return TradeBatch{}, client.decodeBodyError(response, err)
		}
		value.Trades[index].Raw = cloneBytes(itemRaw)
	}
	return value, nil
}
