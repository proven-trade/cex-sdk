package coinone

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	trade "github.com/proven-trade/proven-trade-sdk"
	commonexchange "github.com/proven-trade/proven-trade-sdk/exchange"
)

// Markets는 기준 통화로 거래 가능한 Spot 마켓을 조회한다.
func (client *Client) Markets(
	ctx context.Context,
	request MarketsRequest,
	options ...trade.RequestOption,
) ([]Market, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	path := "/public/v2/markets/" + url.PathEscape(request.QuoteCurrency)
	response, err := client.executePublic(ctx, path, nil, options...)
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Markets []json.RawMessage `json:"markets"`
	}
	if err := client.decodeResponse(response, commonexchange.OperationRead, &envelope); err != nil {
		return nil, err
	}
	return decodeRawItems(client, response, envelope.Markets, func(item *Market, raw []byte) {
		item.Raw = raw
	})
}

// OrderBook은 지정 마켓의 호가 스냅샷을 조회한다.
func (client *Client) OrderBook(
	ctx context.Context,
	request OrderBookRequest,
	options ...trade.RequestOption,
) (OrderBook, error) {
	if err := request.validate(); err != nil {
		return OrderBook{}, err
	}
	path := pairPath("/public/v2/orderbook", request.QuoteCurrency, request.TargetCurrency)
	response, err := client.executePublic(ctx, path, request.values(), options...)
	if err != nil {
		return OrderBook{}, err
	}
	var result OrderBook
	if err := client.decodeResponse(response, commonexchange.OperationRead, &result); err != nil {
		return OrderBook{}, err
	}
	result.Raw = cloneBytes(response.Body)
	return result, nil
}

// RecentTrades는 지정 마켓의 최근 공개 체결을 조회한다.
func (client *Client) RecentTrades(
	ctx context.Context,
	request RecentTradesRequest,
	options ...trade.RequestOption,
) ([]PublicTrade, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	path := pairPath("/public/v2/trades", request.QuoteCurrency, request.TargetCurrency)
	response, err := client.executePublic(ctx, path, request.values(), options...)
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Transactions []json.RawMessage `json:"transactions"`
	}
	if err := client.decodeResponse(response, commonexchange.OperationRead, &envelope); err != nil {
		return nil, err
	}
	return decodeRawItems(client, response, envelope.Transactions, func(item *PublicTrade, raw []byte) {
		item.Raw = raw
	})
}

// Ticker는 지정 마켓의 현재가와 누적 거래 정보를 조회한다.
func (client *Client) Ticker(
	ctx context.Context,
	request TickerRequest,
	options ...trade.RequestOption,
) (Ticker, error) {
	if err := request.validate(); err != nil {
		return Ticker{}, err
	}
	path := pairPath("/public/v2/ticker_new", request.QuoteCurrency, request.TargetCurrency)
	response, err := client.executePublic(ctx, path, request.values(), options...)
	if err != nil {
		return Ticker{}, err
	}
	var envelope struct {
		Tickers []json.RawMessage `json:"tickers"`
	}
	if err := client.decodeResponse(response, commonexchange.OperationRead, &envelope); err != nil {
		return Ticker{}, err
	}
	items, err := decodeRawItems(client, response, envelope.Tickers, func(item *Ticker, raw []byte) {
		item.Raw = raw
	})
	if err != nil {
		return Ticker{}, err
	}
	if len(items) != 1 {
		return Ticker{}, client.decodeBodyError(
			response, commonexchange.OperationRead,
			fmt.Errorf("Coinone individual ticker response contains %d items", len(items)),
		)
	}
	return items[0], nil
}

// Candles는 지정 마켓의 OHLCV 캔들을 조회한다.
func (client *Client) Candles(
	ctx context.Context,
	request CandlesRequest,
	options ...trade.RequestOption,
) (CandlePage, error) {
	if err := request.validate(); err != nil {
		return CandlePage{}, err
	}
	path := pairPath("/public/v2/chart", request.QuoteCurrency, request.TargetCurrency)
	response, err := client.executePublic(ctx, path, request.values(), options...)
	if err != nil {
		return CandlePage{}, err
	}
	var envelope struct {
		IsLast bool              `json:"is_last"`
		Chart  []json.RawMessage `json:"chart"`
	}
	if err := client.decodeResponse(response, commonexchange.OperationRead, &envelope); err != nil {
		return CandlePage{}, err
	}
	items, err := decodeRawItems(client, response, envelope.Chart, func(item *Candle, raw []byte) {
		item.Raw = raw
	})
	if err != nil {
		return CandlePage{}, err
	}
	return CandlePage{IsLast: envelope.IsLast, Chart: items, Raw: cloneBytes(response.Body)}, nil
}

func pairPath(prefix, quoteCurrency, targetCurrency string) string {
	return prefix + "/" + url.PathEscape(quoteCurrency) + "/" + url.PathEscape(targetCurrency)
}

func decodeRawItems[T any](
	client *Client,
	response commonexchange.Response,
	rawItems []json.RawMessage,
	setRaw func(*T, []byte),
) ([]T, error) {
	items := make([]T, len(rawItems))
	for index, rawItem := range rawItems {
		if err := json.Unmarshal(rawItem, &items[index]); err != nil {
			return nil, client.decodeBodyError(
				response, commonexchange.OperationRead,
				fmt.Errorf("decode Coinone response item: %w", err),
			)
		}
		if setRaw != nil {
			setRaw(&items[index], cloneBytes(rawItem))
		}
	}
	return items, nil
}
