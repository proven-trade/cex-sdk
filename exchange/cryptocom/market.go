package cryptocom

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"

	trade "github.com/proven-trade/proven-trade-sdk"
	commonexchange "github.com/proven-trade/proven-trade-sdk/exchange"
)

const (
	methodGetInstruments = "public/get-instruments"
	methodGetTickers     = "public/get-tickers"
	methodGetBook        = "public/get-book"
	methodGetTrades      = "public/get-trades"
	methodGetCandlestick = "public/get-candlestick"
)

// Instruments는 전체 Crypto.com Exchange v1 상품의 상태와 거래 규칙을 조회한다.
func (client *Client) Instruments(
	ctx context.Context,
	options ...trade.RequestOption,
) (Instruments, error) {
	response, err := client.executePublic(ctx, methodGetInstruments, nil, options...)
	if err != nil {
		return Instruments{}, err
	}
	resultRaw, err := client.decodeResult(response, methodGetInstruments)
	if err != nil {
		return Instruments{}, err
	}
	itemsRaw, err := client.decodeDataItems(response, resultRaw)
	if err != nil {
		return Instruments{}, err
	}
	value := Instruments{Items: make([]Instrument, len(itemsRaw)), Raw: cloneBytes(resultRaw)}
	for index, itemRaw := range itemsRaw {
		if err := json.Unmarshal(itemRaw, &value.Items[index]); err != nil {
			return Instruments{}, client.decodeBodyError(response, err)
		}
		if value.Items[index].Symbol == "" || value.Items[index].InstrumentType == "" {
			return Instruments{}, client.decodeBodyError(
				response, errors.New("Crypto.com instrument identity is missing"),
			)
		}
		value.Items[index].Raw = cloneBytes(itemRaw)
	}
	return value, nil
}

// Ticker는 지정한 Crypto.com Spot 거래쌍의 최근가와 24시간 통계를 조회한다.
func (client *Client) Ticker(
	ctx context.Context,
	instrumentName string,
	options ...trade.RequestOption,
) (Ticker, error) {
	if err := validateInstrumentName(instrumentName); err != nil {
		return Ticker{}, err
	}
	response, err := client.executePublic(
		ctx, methodGetTickers, url.Values{"instrument_name": {instrumentName}}, options...,
	)
	if err != nil {
		return Ticker{}, err
	}
	items, err := client.decodeTickers(response)
	if err != nil {
		return Ticker{}, err
	}
	for _, item := range items {
		if item.InstrumentName == instrumentName {
			return item, nil
		}
	}
	return Ticker{}, client.decodeBodyError(
		response, fmt.Errorf("Crypto.com ticker for %q is missing", instrumentName),
	)
}

// Tickers는 모든 Crypto.com Spot 거래쌍의 ticker를 조회한다.
func (client *Client) Tickers(
	ctx context.Context,
	options ...trade.RequestOption,
) ([]Ticker, error) {
	response, err := client.executePublic(ctx, methodGetTickers, nil, options...)
	if err != nil {
		return nil, err
	}
	return client.decodeTickers(response)
}

func (client *Client) decodeTickers(response commonexchange.Response) ([]Ticker, error) {
	resultRaw, err := client.decodeResult(response, methodGetTickers)
	if err != nil {
		return nil, err
	}
	itemsRaw, err := client.decodeDataItems(response, resultRaw)
	if err != nil {
		return nil, err
	}
	items := make([]Ticker, len(itemsRaw))
	for index, itemRaw := range itemsRaw {
		if err := json.Unmarshal(itemRaw, &items[index]); err != nil {
			return nil, client.decodeBodyError(response, err)
		}
		if items[index].InstrumentName == "" {
			return nil, client.decodeBodyError(
				response, errors.New("Crypto.com ticker instrument is missing"),
			)
		}
		items[index].Raw = cloneBytes(itemRaw)
	}
	return items, nil
}

// OrderBook은 지정한 Crypto.com Spot 거래쌍의 호가 snapshot을 조회한다.
func (client *Client) OrderBook(
	ctx context.Context,
	request OrderBookRequest,
	options ...trade.RequestOption,
) (OrderBook, error) {
	if err := request.validate(); err != nil {
		return OrderBook{}, err
	}
	response, err := client.executePublic(ctx, methodGetBook, request.values(), options...)
	if err != nil {
		return OrderBook{}, err
	}
	resultRaw, err := client.decodeResult(response, methodGetBook)
	if err != nil {
		return OrderBook{}, err
	}
	var result struct {
		InstrumentName string            `json:"instrument_name"`
		Depth          Integer           `json:"depth"`
		Data           []json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(resultRaw, &result); err != nil {
		return OrderBook{}, client.decodeBodyError(response, err)
	}
	depth, err := intFromInteger(result.Depth, "order book depth")
	if err != nil {
		return OrderBook{}, client.decodeBodyError(response, err)
	}
	if result.InstrumentName != request.InstrumentName || depth != request.Depth || len(result.Data) != 1 {
		return OrderBook{}, client.decodeBodyError(
			response, errors.New("Crypto.com order book metadata is inconsistent"),
		)
	}
	var snapshot struct {
		Bids      []BookLevel `json:"bids"`
		Asks      []BookLevel `json:"asks"`
		Timestamp Scalar      `json:"t"`
	}
	if err := json.Unmarshal(result.Data[0], &snapshot); err != nil {
		return OrderBook{}, client.decodeBodyError(response, err)
	}
	return OrderBook{
		InstrumentName: result.InstrumentName, Depth: depth,
		Timestamp: snapshot.Timestamp, Bids: snapshot.Bids,
		Asks: snapshot.Asks, Raw: cloneBytes(result.Data[0]),
	}, nil
}

// RecentTrades는 지정한 Crypto.com Spot 거래쌍의 최근 공개 체결을 조회한다.
func (client *Client) RecentTrades(
	ctx context.Context,
	request TradesRequest,
	options ...trade.RequestOption,
) ([]Trade, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePublic(ctx, methodGetTrades, request.values(), options...)
	if err != nil {
		return nil, err
	}
	resultRaw, err := client.decodeResult(response, methodGetTrades)
	if err != nil {
		return nil, err
	}
	itemsRaw, err := client.decodeDataItems(response, resultRaw)
	if err != nil {
		return nil, err
	}
	items := make([]Trade, len(itemsRaw))
	for index, itemRaw := range itemsRaw {
		if err := json.Unmarshal(itemRaw, &items[index]); err != nil {
			return nil, client.decodeBodyError(response, err)
		}
		if items[index].InstrumentName != request.InstrumentName ||
			(items[index].Side != TradeSideBuy && items[index].Side != TradeSideSell) {
			return nil, client.decodeBodyError(
				response, errors.New("Crypto.com trade identity or side is invalid"),
			)
		}
		items[index].Raw = cloneBytes(itemRaw)
	}
	return items, nil
}

// Candles는 지정한 Crypto.com Spot 거래쌍의 OHLCV 캔들을 조회한다.
func (client *Client) Candles(
	ctx context.Context,
	request CandlesRequest,
	options ...trade.RequestOption,
) ([]Candle, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePublic(ctx, methodGetCandlestick, request.values(), options...)
	if err != nil {
		return nil, err
	}
	resultRaw, err := client.decodeResult(response, methodGetCandlestick)
	if err != nil {
		return nil, err
	}
	var result struct {
		InstrumentName string          `json:"instrument_name"`
		Interval       string          `json:"interval"`
		Data           json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(resultRaw, &result); err != nil {
		return nil, client.decodeBodyError(response, err)
	}
	if result.InstrumentName != request.InstrumentName || result.Interval != string(request.Timeframe) {
		return nil, client.decodeBodyError(
			response, errors.New("Crypto.com candlestick metadata is inconsistent"),
		)
	}
	dataRaw := bytes.TrimSpace(result.Data)
	if len(dataRaw) == 0 || dataRaw[0] != '[' {
		return nil, client.decodeBodyError(
			response, errors.New("Crypto.com candlestick data is not an array"),
		)
	}
	var itemsRaw []json.RawMessage
	if err := json.Unmarshal(dataRaw, &itemsRaw); err != nil {
		return nil, client.decodeBodyError(response, err)
	}
	items := make([]Candle, len(itemsRaw))
	for index, itemRaw := range itemsRaw {
		if err := json.Unmarshal(itemRaw, &items[index]); err != nil {
			return nil, client.decodeBodyError(response, err)
		}
		items[index].Raw = cloneBytes(itemRaw)
	}
	return items, nil
}

func (client *Client) decodeDataItems(
	response commonexchange.Response,
	resultRaw json.RawMessage,
) ([]json.RawMessage, error) {
	var result struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(resultRaw, &result); err != nil {
		return nil, client.decodeBodyError(response, err)
	}
	trimmed := bytes.TrimSpace(result.Data)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return nil, client.decodeBodyError(
			response, errors.New("Crypto.com response data is not an array"),
		)
	}
	var items []json.RawMessage
	if err := json.Unmarshal(trimmed, &items); err != nil {
		return nil, client.decodeBodyError(response, err)
	}
	return items, nil
}
