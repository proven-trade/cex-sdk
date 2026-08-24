package kraken

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"

	trade "github.com/proven-trade/proven-trade-sdk"
	commonexchange "github.com/proven-trade/proven-trade-sdk/exchange"
)

// ServerTime은 Kraken 서버 시간을 조회한다.
func (client *Client) ServerTime(
	ctx context.Context,
	options ...trade.RequestOption,
) (ServerTime, error) {
	response, err := client.executePublic(ctx, publicPrefix+"Time", nil, options...)
	if err != nil {
		return ServerTime{}, err
	}
	result, err := client.decodeResult(response, commonexchange.OperationRead)
	if err != nil {
		return ServerTime{}, err
	}
	var serverTime ServerTime
	if err := json.Unmarshal(result, &serverTime); err != nil {
		return ServerTime{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	if serverTime.UnixTime <= 0 {
		return ServerTime{}, client.decodeBodyError(
			response, commonexchange.OperationRead, errors.New("Kraken server time is empty"),
		)
	}
	serverTime.Raw = cloneBytes(result)
	return serverTime, nil
}

// AssetPairs는 Spot 상품 정보와 주문 단위를 조회한다.
func (client *Client) AssetPairs(
	ctx context.Context,
	request AssetPairsRequest,
	options ...trade.RequestOption,
) ([]AssetPair, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePublic(
		ctx, publicPrefix+"AssetPairs", request.values(), options...,
	)
	if err != nil {
		return nil, err
	}
	result, err := client.decodeResult(response, commonexchange.OperationRead)
	if err != nil {
		return nil, err
	}
	var entries map[string]json.RawMessage
	if err := json.Unmarshal(result, &entries); err != nil {
		return nil, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	keys := sortedKeys(entries)
	pairs := make([]AssetPair, len(keys))
	for index, key := range keys {
		if err := json.Unmarshal(entries[key], &pairs[index]); err != nil {
			return nil, client.decodeBodyError(response, commonexchange.OperationRead, err)
		}
		pairs[index].ID = key
		pairs[index].Raw = cloneBytes(entries[key])
	}
	return pairs, nil
}

// Tickers는 전체 또는 지정한 Spot 상품의 ticker를 조회한다.
func (client *Client) Tickers(
	ctx context.Context,
	request TickersRequest,
	options ...trade.RequestOption,
) ([]Ticker, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePublic(ctx, publicPrefix+"Ticker", request.values(), options...)
	if err != nil {
		return nil, err
	}
	result, err := client.decodeResult(response, commonexchange.OperationRead)
	if err != nil {
		return nil, err
	}
	var entries map[string]json.RawMessage
	if err := json.Unmarshal(result, &entries); err != nil {
		return nil, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	keys := sortedKeys(entries)
	tickers := make([]Ticker, len(keys))
	for index, key := range keys {
		ticker, parseErr := parseTicker(key, entries[key])
		if parseErr != nil {
			return nil, client.decodeBodyError(response, commonexchange.OperationRead, parseErr)
		}
		tickers[index] = ticker
	}
	return tickers, nil
}

// OrderBook은 단일 Spot 상품의 L2 호가 스냅샷을 조회한다.
func (client *Client) OrderBook(
	ctx context.Context,
	request OrderBookRequest,
	options ...trade.RequestOption,
) (OrderBook, error) {
	if err := request.validate(); err != nil {
		return OrderBook{}, err
	}
	response, err := client.executePublic(ctx, publicPrefix+"Depth", request.values(), options...)
	if err != nil {
		return OrderBook{}, err
	}
	result, err := client.decodeResult(response, commonexchange.OperationRead)
	if err != nil {
		return OrderBook{}, err
	}
	pairID, raw, err := singleDynamicEntry(result, nil)
	if err != nil {
		return OrderBook{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	var bookData struct {
		Asks [][]json.RawMessage `json:"asks"`
		Bids [][]json.RawMessage `json:"bids"`
	}
	if err := json.Unmarshal(raw, &bookData); err != nil {
		return OrderBook{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	book := OrderBook{PairID: pairID, Raw: cloneBytes(raw)}
	book.Asks, err = parseBookLevels(bookData.Asks)
	if err == nil {
		book.Bids, err = parseBookLevels(bookData.Bids)
	}
	if err != nil {
		return OrderBook{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	return book, nil
}

// RecentTrades는 단일 Spot 상품의 최근 공개 체결을 조회한다.
func (client *Client) RecentTrades(
	ctx context.Context,
	request RecentTradesRequest,
	options ...trade.RequestOption,
) (RecentTrades, error) {
	if err := request.validate(); err != nil {
		return RecentTrades{}, err
	}
	response, err := client.executePublic(ctx, publicPrefix+"Trades", request.values(), options...)
	if err != nil {
		return RecentTrades{}, err
	}
	result, err := client.decodeResult(response, commonexchange.OperationRead)
	if err != nil {
		return RecentTrades{}, err
	}
	var entries map[string]json.RawMessage
	if err := json.Unmarshal(result, &entries); err != nil {
		return RecentTrades{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	lastRaw, exists := entries["last"]
	if !exists {
		return RecentTrades{}, client.decodeBodyError(
			response, commonexchange.OperationRead, errors.New("Kraken recent trades cursor is missing"),
		)
	}
	delete(entries, "last")
	pairID, tradesRaw, err := singleMapEntry(entries)
	if err != nil {
		return RecentTrades{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	var tuples [][]json.RawMessage
	if err := json.Unmarshal(tradesRaw, &tuples); err != nil {
		return RecentTrades{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	trades := make([]PublicTrade, len(tuples))
	for index, tuple := range tuples {
		tradeValue, parseErr := parsePublicTrade(tuple)
		if parseErr != nil {
			return RecentTrades{}, client.decodeBodyError(response, commonexchange.OperationRead, parseErr)
		}
		trades[index] = tradeValue
	}
	last, err := scalarString(lastRaw)
	if err != nil {
		return RecentTrades{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	return RecentTrades{PairID: pairID, Trades: trades, Last: last, Raw: cloneBytes(result)}, nil
}

// Candles는 단일 Spot 상품의 OHLCV를 조회한다.
func (client *Client) Candles(
	ctx context.Context,
	request CandlesRequest,
	options ...trade.RequestOption,
) (Candles, error) {
	if err := request.validate(); err != nil {
		return Candles{}, err
	}
	response, err := client.executePublic(ctx, publicPrefix+"OHLC", request.values(), options...)
	if err != nil {
		return Candles{}, err
	}
	result, err := client.decodeResult(response, commonexchange.OperationRead)
	if err != nil {
		return Candles{}, err
	}
	var entries map[string]json.RawMessage
	if err := json.Unmarshal(result, &entries); err != nil {
		return Candles{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	lastRaw, exists := entries["last"]
	if !exists {
		return Candles{}, client.decodeBodyError(
			response, commonexchange.OperationRead, errors.New("Kraken candle cursor is missing"),
		)
	}
	delete(entries, "last")
	pairID, candlesRaw, err := singleMapEntry(entries)
	if err != nil {
		return Candles{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	var tuples [][]json.RawMessage
	if err := json.Unmarshal(candlesRaw, &tuples); err != nil {
		return Candles{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	items := make([]Candle, len(tuples))
	for index, tuple := range tuples {
		candle, parseErr := parseCandle(tuple)
		if parseErr != nil {
			return Candles{}, client.decodeBodyError(response, commonexchange.OperationRead, parseErr)
		}
		items[index] = candle
	}
	last, err := scalarInt64(lastRaw)
	if err != nil {
		return Candles{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	return Candles{PairID: pairID, Items: items, Last: last, Raw: cloneBytes(result)}, nil
}

func parseTicker(pairID string, raw json.RawMessage) (Ticker, error) {
	var value struct {
		Ask    []json.RawMessage `json:"a"`
		Bid    []json.RawMessage `json:"b"`
		Close  []json.RawMessage `json:"c"`
		Volume []json.RawMessage `json:"v"`
		VWAP   []json.RawMessage `json:"p"`
		Trades []json.RawMessage `json:"t"`
		Low    []json.RawMessage `json:"l"`
		High   []json.RawMessage `json:"h"`
		Open   json.RawMessage   `json:"o"`
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		return Ticker{}, err
	}
	groups := [][]json.RawMessage{
		value.Ask, value.Bid, value.Close, value.Volume, value.VWAP, value.Trades, value.Low, value.High,
	}
	wants := []int{3, 3, 2, 2, 2, 2, 2, 2}
	for index, group := range groups {
		if len(group) < wants[index] {
			return Ticker{}, fmt.Errorf("Kraken ticker tuple %d is too short", index)
		}
	}
	stringsAt := func(group []json.RawMessage, indexes ...int) ([]string, error) {
		values := make([]string, len(indexes))
		for index, tupleIndex := range indexes {
			value, err := scalarString(group[tupleIndex])
			if err != nil {
				return nil, err
			}
			values[index] = value
		}
		return values, nil
	}
	ask, err := stringsAt(value.Ask, 0, 1, 2)
	if err != nil {
		return Ticker{}, err
	}
	bid, err := stringsAt(value.Bid, 0, 1, 2)
	if err != nil {
		return Ticker{}, err
	}
	closeValues, err := stringsAt(value.Close, 0, 1)
	if err != nil {
		return Ticker{}, err
	}
	volume, err := stringsAt(value.Volume, 0, 1)
	if err != nil {
		return Ticker{}, err
	}
	vwap, err := stringsAt(value.VWAP, 0, 1)
	if err != nil {
		return Ticker{}, err
	}
	low, err := stringsAt(value.Low, 0, 1)
	if err != nil {
		return Ticker{}, err
	}
	high, err := stringsAt(value.High, 0, 1)
	if err != nil {
		return Ticker{}, err
	}
	tradesToday, err := scalarInt64(value.Trades[0])
	if err != nil {
		return Ticker{}, err
	}
	trades24Hours, err := scalarInt64(value.Trades[1])
	if err != nil {
		return Ticker{}, err
	}
	open, err := scalarString(value.Open)
	if err != nil {
		return Ticker{}, err
	}
	return Ticker{
		PairID: pairID, AskPrice: ask[0], AskWholeLotVolume: ask[1], AskLotVolume: ask[2],
		BidPrice: bid[0], BidWholeLotVolume: bid[1], BidLotVolume: bid[2],
		LastPrice: closeValues[0], LastVolume: closeValues[1],
		VolumeToday: volume[0], Volume24Hours: volume[1], VWAPToday: vwap[0], VWAP24Hours: vwap[1],
		TradesToday: tradesToday, Trades24Hours: trades24Hours,
		LowToday: low[0], Low24Hours: low[1], HighToday: high[0], High24Hours: high[1],
		OpenPrice: open, Raw: cloneBytes(raw),
	}, nil
}

func parseBookLevels(tuples [][]json.RawMessage) ([]BookLevel, error) {
	levels := make([]BookLevel, len(tuples))
	for index, tuple := range tuples {
		if len(tuple) < 3 {
			return nil, fmt.Errorf("Kraken order book tuple is too short")
		}
		price, err := scalarString(tuple[0])
		if err != nil {
			return nil, err
		}
		volume, err := scalarString(tuple[1])
		if err != nil {
			return nil, err
		}
		timestamp, err := scalarInt64(tuple[2])
		if err != nil {
			return nil, err
		}
		levels[index] = BookLevel{Price: price, Volume: volume, Timestamp: timestamp}
	}
	return levels, nil
}

func parsePublicTrade(tuple []json.RawMessage) (PublicTrade, error) {
	if len(tuple) < 6 {
		return PublicTrade{}, fmt.Errorf("Kraken public trade tuple is too short")
	}
	price, err := scalarString(tuple[0])
	if err != nil {
		return PublicTrade{}, err
	}
	volume, err := scalarString(tuple[1])
	if err != nil {
		return PublicTrade{}, err
	}
	timeValue, err := scalarString(tuple[2])
	if err != nil {
		return PublicTrade{}, err
	}
	side, err := scalarString(tuple[3])
	if err != nil {
		return PublicTrade{}, err
	}
	orderType, err := scalarString(tuple[4])
	if err != nil {
		return PublicTrade{}, err
	}
	misc, err := scalarString(tuple[5])
	if err != nil {
		return PublicTrade{}, err
	}
	var tradeID int64
	if len(tuple) > 6 {
		tradeID, err = scalarInt64(tuple[6])
		if err != nil {
			return PublicTrade{}, err
		}
	}
	return PublicTrade{
		Price: price, Volume: volume, Time: timeValue, Side: Side(side),
		OrderType: OrderType(orderType), Misc: misc, TradeID: tradeID,
	}, nil
}

func parseCandle(tuple []json.RawMessage) (Candle, error) {
	if len(tuple) < 8 {
		return Candle{}, fmt.Errorf("Kraken candle tuple is too short")
	}
	timestamp, err := scalarInt64(tuple[0])
	if err != nil {
		return Candle{}, err
	}
	values := make([]string, 6)
	for index := range values {
		values[index], err = scalarString(tuple[index+1])
		if err != nil {
			return Candle{}, err
		}
	}
	tradeCount, err := scalarInt64(tuple[7])
	if err != nil {
		return Candle{}, err
	}
	return Candle{
		Time: timestamp, Open: values[0], High: values[1], Low: values[2], Close: values[3],
		VWAP: values[4], Volume: values[5], TradeCount: tradeCount,
	}, nil
}

func singleDynamicEntry(result json.RawMessage, excluded map[string]struct{}) (string, json.RawMessage, error) {
	var entries map[string]json.RawMessage
	if err := json.Unmarshal(result, &entries); err != nil {
		return "", nil, err
	}
	for key := range excluded {
		delete(entries, key)
	}
	return singleMapEntry(entries)
}

func singleMapEntry(entries map[string]json.RawMessage) (string, json.RawMessage, error) {
	if len(entries) != 1 {
		return "", nil, fmt.Errorf("Kraken response contains %d dynamic entries, want 1", len(entries))
	}
	for key, value := range entries {
		return key, value, nil
	}
	return "", nil, errors.New("Kraken response is empty")
}

func scalarString(raw json.RawMessage) (string, error) {
	var value string
	if err := json.Unmarshal(raw, &value); err == nil {
		return value, nil
	}
	var number json.Number
	if err := json.Unmarshal(raw, &number); err != nil {
		return "", err
	}
	return number.String(), nil
}

func scalarInt64(raw json.RawMessage) (int64, error) {
	value, err := scalarString(raw)
	if err != nil {
		return 0, err
	}
	if integer, err := strconv.ParseInt(value, 10, 64); err == nil {
		return integer, nil
	}
	decimal, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, err
	}
	return int64(decimal), nil
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
