package futures

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	trade "github.com/proven-trade/proven-trade-sdk"
	commonexchange "github.com/proven-trade/proven-trade-sdk/exchange"
)

// Contracts는 결제 통화별 Gate.io 무기한 Futures 계약과 주문 규칙 한 페이지를 조회한다.
func (client *Client) Contracts(
	ctx context.Context,
	request ContractsRequest,
	options ...trade.RequestOption,
) ([]Contract, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePublic(
		ctx, http.MethodGet, futuresPath(request.Settlement, "/contracts"), request.values(),
		publicLimit("contracts"), options...,
	)
	if err != nil {
		return nil, err
	}
	return client.decodeContracts(response)
}

// Contract는 지정한 Gate.io 무기한 Futures 계약 규칙과 시장 상태를 조회한다.
func (client *Client) Contract(
	ctx context.Context,
	settlement Settlement,
	contract string,
	options ...trade.RequestOption,
) (Contract, error) {
	if err := validateSettlementContract(settlement, contract); err != nil {
		return Contract{}, err
	}
	response, err := client.executePublic(
		ctx, http.MethodGet, futuresPath(settlement, "/contracts/"+url.PathEscape(contract)), nil,
		publicLimit("contract"), options...,
	)
	if err != nil {
		return Contract{}, err
	}
	var item Contract
	data, err := client.decodeData(response, commonexchange.OperationRead, &item)
	if err != nil {
		return Contract{}, err
	}
	item.Raw = cloneBytes(data)
	return item, nil
}

// Tickers는 전체 또는 지정 계약의 Gate.io 무기한 Futures 거래 통계를 조회한다.
func (client *Client) Tickers(
	ctx context.Context,
	request TickersRequest,
	options ...trade.RequestOption,
) ([]Ticker, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePublic(
		ctx, http.MethodGet, futuresPath(request.Settlement, "/tickers"), request.values(),
		publicLimit("tickers"), options...,
	)
	if err != nil {
		return nil, err
	}
	var rawItems []json.RawMessage
	if _, err := client.decodeData(response, commonexchange.OperationRead, &rawItems); err != nil {
		return nil, err
	}
	items := make([]Ticker, len(rawItems))
	for index, raw := range rawItems {
		if err := json.Unmarshal(raw, &items[index]); err != nil {
			return nil, client.decodeBodyError(response, commonexchange.OperationRead, err)
		}
		items[index].Raw = cloneBytes(raw)
	}
	return items, nil
}

// OrderBook은 지정한 Gate.io 무기한 Futures 계약의 호가 snapshot을 조회한다.
func (client *Client) OrderBook(
	ctx context.Context,
	request OrderBookRequest,
	options ...trade.RequestOption,
) (OrderBook, error) {
	if err := request.validate(); err != nil {
		return OrderBook{}, err
	}
	response, err := client.executePublic(
		ctx, http.MethodGet, futuresPath(request.Settlement, "/order_book"), request.values(),
		publicLimit("order-book"), options...,
	)
	if err != nil {
		return OrderBook{}, err
	}
	var book OrderBook
	data, err := client.decodeData(response, commonexchange.OperationRead, &book)
	if err != nil {
		return OrderBook{}, err
	}
	book.Raw = cloneBytes(data)
	return book, nil
}

// RecentTrades는 지정한 Gate.io 무기한 Futures 계약의 공개 체결을 조회한다.
func (client *Client) RecentTrades(
	ctx context.Context,
	request TradesRequest,
	options ...trade.RequestOption,
) ([]PublicTrade, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePublic(
		ctx, http.MethodGet, futuresPath(request.Settlement, "/trades"), request.values(),
		publicLimit("trades"), options...,
	)
	if err != nil {
		return nil, err
	}
	var rawItems []json.RawMessage
	if _, err := client.decodeData(response, commonexchange.OperationRead, &rawItems); err != nil {
		return nil, err
	}
	items := make([]PublicTrade, len(rawItems))
	for index, raw := range rawItems {
		if err := json.Unmarshal(raw, &items[index]); err != nil {
			return nil, client.decodeBodyError(response, commonexchange.OperationRead, err)
		}
		items[index].Raw = cloneBytes(raw)
	}
	return items, nil
}

// Candles는 최근 개수 또는 시간 범위에 해당하는 Gate.io 무기한 Futures OHLCV를 조회한다.
func (client *Client) Candles(
	ctx context.Context,
	request CandlesRequest,
	options ...trade.RequestOption,
) ([]Candle, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePublic(
		ctx, http.MethodGet, futuresPath(request.Settlement, "/candlesticks"), request.values(),
		publicLimit("candlesticks"), options...,
	)
	if err != nil {
		return nil, err
	}
	var rawItems []json.RawMessage
	if _, err := client.decodeData(response, commonexchange.OperationRead, &rawItems); err != nil {
		return nil, err
	}
	items := make([]Candle, len(rawItems))
	for index, raw := range rawItems {
		if err := json.Unmarshal(raw, &items[index]); err != nil {
			return nil, client.decodeBodyError(response, commonexchange.OperationRead, err)
		}
		items[index].Raw = cloneBytes(raw)
	}
	return items, nil
}

func (client *Client) decodeContracts(response commonexchange.Response) ([]Contract, error) {
	var rawItems []json.RawMessage
	if _, err := client.decodeData(response, commonexchange.OperationRead, &rawItems); err != nil {
		return nil, err
	}
	items := make([]Contract, len(rawItems))
	for index, raw := range rawItems {
		if err := json.Unmarshal(raw, &items[index]); err != nil {
			return nil, client.decodeBodyError(response, commonexchange.OperationRead, err)
		}
		items[index].Raw = cloneBytes(raw)
	}
	return items, nil
}

func futuresPath(settlement Settlement, suffix string) string {
	return fmt.Sprintf("/futures/%s%s", settlement, suffix)
}
