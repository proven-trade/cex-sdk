package kraken

import (
	"context"
	"encoding/json"
	"errors"

	trade "github.com/proven-trade/proven-trade-sdk"
	"github.com/proven-trade/proven-trade-sdk/credential"
	commonexchange "github.com/proven-trade/proven-trade-sdk/exchange"
)

// PlaceOrder는 Spot 시장가 또는 지정가 주문을 생성한다.
// 전송 결과가 불명확하면 자동 재시도하지 않고 UNKNOWN_EXECUTION_STATE로 반환한다.
func (client *Client) PlaceOrder(
	ctx context.Context,
	request PlaceOrderRequest,
	options ...trade.RequestOption,
) (OrderReference, error) {
	if err := request.validate(); err != nil {
		return OrderReference{}, err
	}
	response, err := client.executePrivate(
		ctx, privatePrefix+"AddOrder", request.values(), credential.PermissionTrade,
		commonexchange.OperationMutation, limitTrading, request.Pair, options...,
	)
	if err != nil {
		return OrderReference{}, err
	}
	result, err := client.decodeResult(response, commonexchange.OperationMutation)
	if err != nil {
		return OrderReference{}, err
	}
	var value struct {
		Description    OrderDescription `json:"descr"`
		TransactionIDs []string         `json:"txid"`
	}
	if err := json.Unmarshal(result, &value); err != nil {
		return OrderReference{}, client.decodeBodyError(response, commonexchange.OperationMutation, err)
	}
	if !request.ValidateOnly && len(value.TransactionIDs) == 0 {
		return OrderReference{}, client.decodeBodyError(
			response, commonexchange.OperationMutation, errors.New("Kraken order acknowledgement is empty"),
		)
	}
	return OrderReference{
		TransactionIDs: value.TransactionIDs, Description: value.Description, Raw: cloneBytes(result),
	}, nil
}

// CancelOrder는 거래소 주문 ID 또는 client order ID로 Spot 주문을 취소한다.
// 전송 결과가 불명확하면 자동 재시도하지 않고 UNKNOWN_EXECUTION_STATE로 반환한다.
func (client *Client) CancelOrder(
	ctx context.Context,
	request CancelOrderRequest,
	options ...trade.RequestOption,
) (CancelResult, error) {
	if err := request.validate(); err != nil {
		return CancelResult{}, err
	}
	response, err := client.executePrivate(
		ctx, privatePrefix+"CancelOrder", request.values(), credential.PermissionTrade,
		commonexchange.OperationMutation, limitTrading, request.Pair, options...,
	)
	if err != nil {
		return CancelResult{}, err
	}
	result, err := client.decodeResult(response, commonexchange.OperationMutation)
	if err != nil {
		return CancelResult{}, err
	}
	var canceled CancelResult
	if err := json.Unmarshal(result, &canceled); err != nil {
		return CancelResult{}, client.decodeBodyError(response, commonexchange.OperationMutation, err)
	}
	if canceled.Count < 1 && !canceled.Pending {
		return CancelResult{}, client.decodeBodyError(
			response, commonexchange.OperationMutation, errors.New("Kraken cancel acknowledgement is empty"),
		)
	}
	canceled.Raw = cloneBytes(result)
	return canceled, nil
}

// OrderInfo는 거래소 주문 ID로 최대 50개 주문을 조회한다.
func (client *Client) OrderInfo(
	ctx context.Context,
	request OrderInfoRequest,
	options ...trade.RequestOption,
) ([]Order, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePrivate(
		ctx, privatePrefix+"QueryOrders", request.values(), credential.PermissionRead,
		commonexchange.OperationRead, limitPrivate, "", options...,
	)
	if err != nil {
		return nil, err
	}
	result, err := client.decodeResult(response, commonexchange.OperationRead)
	if err != nil {
		return nil, err
	}
	orders, err := decodeOrderMap(result)
	if err != nil {
		return nil, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	return orders, nil
}

// OpenOrders는 계정의 미체결 Spot 주문을 조회한다.
func (client *Client) OpenOrders(
	ctx context.Context,
	request OpenOrdersRequest,
	options ...trade.RequestOption,
) (OrderPage, error) {
	if err := request.validate(); err != nil {
		return OrderPage{}, err
	}
	response, err := client.executePrivate(
		ctx, privatePrefix+"OpenOrders", request.values(), credential.PermissionRead,
		commonexchange.OperationRead, limitPrivate, "", options...,
	)
	if err != nil {
		return OrderPage{}, err
	}
	result, err := client.decodeResult(response, commonexchange.OperationRead)
	if err != nil {
		return OrderPage{}, err
	}
	var page struct {
		Open json.RawMessage `json:"open"`
	}
	if err := json.Unmarshal(result, &page); err != nil {
		return OrderPage{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	orders, err := decodeOrderMap(page.Open)
	if err != nil {
		return OrderPage{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	return OrderPage{Orders: orders, Count: len(orders), Raw: cloneBytes(result)}, nil
}

// ClosedOrders는 계정의 종료된 Spot 주문을 조회한다.
func (client *Client) ClosedOrders(
	ctx context.Context,
	request ClosedOrdersRequest,
	options ...trade.RequestOption,
) (OrderPage, error) {
	if err := request.validate(); err != nil {
		return OrderPage{}, err
	}
	response, err := client.executePrivate(
		ctx, privatePrefix+"ClosedOrders", request.values(), credential.PermissionRead,
		commonexchange.OperationRead, limitPrivateHistory, "", options...,
	)
	if err != nil {
		return OrderPage{}, err
	}
	result, err := client.decodeResult(response, commonexchange.OperationRead)
	if err != nil {
		return OrderPage{}, err
	}
	var page struct {
		Closed json.RawMessage `json:"closed"`
		Count  int             `json:"count"`
	}
	if err := json.Unmarshal(result, &page); err != nil {
		return OrderPage{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	orders, err := decodeOrderMap(page.Closed)
	if err != nil {
		return OrderPage{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	return OrderPage{Orders: orders, Count: page.Count, Raw: cloneBytes(result)}, nil
}

// TradesHistory는 계정의 Spot 체결 이력을 조회한다.
func (client *Client) TradesHistory(
	ctx context.Context,
	request TradesHistoryRequest,
	options ...trade.RequestOption,
) (TradePage, error) {
	if err := request.validate(); err != nil {
		return TradePage{}, err
	}
	response, err := client.executePrivate(
		ctx, privatePrefix+"TradesHistory", request.values(), credential.PermissionRead,
		commonexchange.OperationRead, limitPrivateHistory, "", options...,
	)
	if err != nil {
		return TradePage{}, err
	}
	result, err := client.decodeResult(response, commonexchange.OperationRead)
	if err != nil {
		return TradePage{}, err
	}
	var page struct {
		Trades map[string]json.RawMessage `json:"trades"`
		Count  int                        `json:"count"`
	}
	if err := json.Unmarshal(result, &page); err != nil {
		return TradePage{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	keys := sortedKeys(page.Trades)
	trades := make([]TradeFill, len(keys))
	for index, key := range keys {
		if err := json.Unmarshal(page.Trades[key], &trades[index]); err != nil {
			return TradePage{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
		}
		trades[index].TransactionID = key
		trades[index].Raw = cloneBytes(page.Trades[key])
	}
	return TradePage{Trades: trades, Count: page.Count, Raw: cloneBytes(result)}, nil
}

func decodeOrderMap(raw json.RawMessage) ([]Order, error) {
	var entries map[string]json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, err
	}
	keys := sortedKeys(entries)
	orders := make([]Order, len(keys))
	for index, key := range keys {
		if err := json.Unmarshal(entries[key], &orders[index]); err != nil {
			return nil, err
		}
		orders[index].TransactionID = key
		orders[index].Raw = cloneBytes(entries[key])
	}
	return orders, nil
}
