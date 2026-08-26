package coinbase

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

const publicPrefix = "/api/v3/brokerage"

// ServerTime은 Coinbase 서버 시간을 조회한다.
func (client *Client) ServerTime(ctx context.Context, options ...trade.RequestOption) (time.Time, error) {
	response, err := client.executePublic(ctx, http.MethodGet, publicPrefix+"/time", nil, options...)
	if err != nil {
		return time.Time{}, err
	}
	var result struct {
		ISO          string `json:"iso"`
		EpochSeconds string `json:"epochSeconds"`
		EpochMillis  string `json:"epochMillis"`
	}
	if err := client.decodeSuccess(response, commonexchange.OperationRead, &result); err != nil {
		return time.Time{}, err
	}
	serverTime, err := time.Parse(time.RFC3339Nano, result.ISO)
	if err != nil {
		return time.Time{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	return serverTime, nil
}

// Products는 공개 Spot 상품 목록을 조회한다.
func (client *Client) Products(
	ctx context.Context,
	request ProductsRequest,
	options ...trade.RequestOption,
) ([]Product, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePublic(
		ctx, http.MethodGet, publicPrefix+"/market/products", request.values(), options...,
	)
	if err != nil {
		return nil, err
	}
	var page struct {
		Products []json.RawMessage `json:"products"`
	}
	if err := client.decodeSuccess(response, commonexchange.OperationRead, &page); err != nil {
		return nil, err
	}
	products := make([]Product, len(page.Products))
	for index, raw := range page.Products {
		if err := json.Unmarshal(raw, &products[index]); err != nil {
			return nil, client.decodeBodyError(response, commonexchange.OperationRead, err)
		}
		products[index].Raw = cloneBytes(raw)
	}
	return products, nil
}

// Product는 단일 공개 Spot 상품을 조회한다.
func (client *Client) Product(
	ctx context.Context,
	productID string,
	options ...trade.RequestOption,
) (Product, error) {
	segment, err := escapePathSegment("product ID", productID)
	if err != nil {
		return Product{}, err
	}
	response, err := client.executePublic(
		ctx, http.MethodGet, publicPrefix+"/market/products/"+segment, nil, options...,
	)
	if err != nil {
		return Product{}, err
	}
	var product Product
	if err := client.decodeSuccess(response, commonexchange.OperationRead, &product); err != nil {
		return Product{}, err
	}
	product.Raw = cloneBytes(response.Body)
	return product, nil
}

// OrderBook은 공개 호가 스냅샷을 조회한다.
func (client *Client) OrderBook(
	ctx context.Context,
	request OrderBookRequest,
	options ...trade.RequestOption,
) (OrderBook, error) {
	if err := request.validate(); err != nil {
		return OrderBook{}, err
	}
	response, err := client.executePublic(
		ctx, http.MethodGet, publicPrefix+"/market/product_book", request.values(), options...,
	)
	if err != nil {
		return OrderBook{}, err
	}
	var book OrderBook
	if err := client.decodeSuccess(response, commonexchange.OperationRead, &book); err != nil {
		return OrderBook{}, err
	}
	if book.PriceBook.ProductID == "" {
		return OrderBook{}, client.decodeBodyError(
			response, commonexchange.OperationRead, errors.New("Coinbase order book is empty"),
		)
	}
	book.Raw = cloneBytes(response.Body)
	return book, nil
}

// MarketTrades는 최근 공개 체결을 조회한다.
func (client *Client) MarketTrades(
	ctx context.Context,
	request MarketTradesRequest,
	options ...trade.RequestOption,
) (MarketTrades, error) {
	if err := request.validate(); err != nil {
		return MarketTrades{}, err
	}
	segment, err := escapePathSegment("product ID", request.ProductID)
	if err != nil {
		return MarketTrades{}, err
	}
	response, err := client.executePublic(
		ctx, http.MethodGet, publicPrefix+"/market/products/"+segment+"/ticker",
		request.values(), options...,
	)
	if err != nil {
		return MarketTrades{}, err
	}
	var rawPage struct {
		Trades  []json.RawMessage `json:"trades"`
		BestBid string            `json:"best_bid"`
		BestAsk string            `json:"best_ask"`
	}
	if err := client.decodeSuccess(response, commonexchange.OperationRead, &rawPage); err != nil {
		return MarketTrades{}, err
	}
	result := MarketTrades{
		Trades: make([]MarketTrade, len(rawPage.Trades)), BestBid: rawPage.BestBid,
		BestAsk: rawPage.BestAsk, Raw: cloneBytes(response.Body),
	}
	for index, raw := range rawPage.Trades {
		if err := json.Unmarshal(raw, &result.Trades[index]); err != nil {
			return MarketTrades{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
		}
		result.Trades[index].Raw = cloneBytes(raw)
	}
	return result, nil
}

// Candles는 공개 Spot OHLCV를 조회한다.
func (client *Client) Candles(
	ctx context.Context,
	request CandlesRequest,
	options ...trade.RequestOption,
) ([]Candle, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	segment, err := escapePathSegment("product ID", request.ProductID)
	if err != nil {
		return nil, err
	}
	response, err := client.executePublic(
		ctx, http.MethodGet, publicPrefix+"/market/products/"+segment+"/candles",
		request.values(), options...,
	)
	if err != nil {
		return nil, err
	}
	var page struct {
		Candles []Candle `json:"candles"`
	}
	if err := client.decodeSuccess(response, commonexchange.OperationRead, &page); err != nil {
		return nil, err
	}
	return page.Candles, nil
}
