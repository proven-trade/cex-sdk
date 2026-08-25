package mexc

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	trade "github.com/proven-trade/proven-trade-sdk"
	"github.com/proven-trade/proven-trade-sdk/credential"
	commonexchange "github.com/proven-trade/proven-trade-sdk/exchange"
)

// PlaceOrder는 고유한 사용자 주문 ID를 가진 Spot 주문을 생성한다.
func (client *Client) PlaceOrder(
	ctx context.Context,
	request PlaceOrderRequest,
	options ...trade.RequestOption,
) (OrderReference, error) {
	if err := request.validate(); err != nil {
		return OrderReference{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodPost, "/api/v3/order", request.values(),
		privateLimit("order", 1), "order", client.orderQuota,
		credential.PermissionTrade, commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return OrderReference{}, err
	}
	var value OrderReference
	raw, err := client.decodeResponseForOperation(
		response, commonexchange.OperationMutation, &value,
	)
	if err != nil {
		return OrderReference{}, err
	}
	if value.Symbol == "" || value.OrderID == "" {
		return OrderReference{}, client.decodeBodyErrorForOperation(
			response, commonexchange.OperationMutation,
			errors.New("MEXC order response is missing symbol or order ID"),
		)
	}
	if value.ClientOrderID == "" {
		value.ClientOrderID = request.ClientOrderID
	}
	value.Raw = raw
	return value, nil
}

// OrderInfo는 거래소 주문 ID 또는 사용자 주문 ID로 Spot 주문 상태를 조회한다.
func (client *Client) OrderInfo(
	ctx context.Context,
	request OrderInfoRequest,
	options ...trade.RequestOption,
) (Order, error) {
	if err := request.validate(); err != nil {
		return Order{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/api/v3/order", request.values(),
		privateLimit("order-info", 2), "private-read", client.privateReadQuota,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return Order{}, err
	}
	return decodeOrder(client, response, commonexchange.OperationRead)
}

// CancelOrder는 거래소 주문 ID 또는 사용자 주문 ID로 Spot 주문을 취소한다.
func (client *Client) CancelOrder(
	ctx context.Context,
	request CancelOrderRequest,
	options ...trade.RequestOption,
) (Order, error) {
	if err := request.validate(); err != nil {
		return Order{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodDelete, "/api/v3/order", request.values(),
		privateLimit("cancel-order", 1), "cancel", client.cancelQuota,
		credential.PermissionTrade, commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return Order{}, err
	}
	return decodeOrder(client, response, commonexchange.OperationMutation)
}

// OpenOrders는 단일 Spot 거래쌍의 현재 미체결 주문을 조회한다.
func (client *Client) OpenOrders(
	ctx context.Context,
	request OpenOrdersRequest,
	options ...trade.RequestOption,
) ([]Order, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/api/v3/openOrders", request.values(),
		privateLimit("open-orders", 3), "private-read", client.privateReadQuota,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeOrders(client, response)
}

// AllOrders는 최대 7일 범위의 Spot 주문 이력을 조회한다.
func (client *Client) AllOrders(
	ctx context.Context,
	request AllOrdersRequest,
	options ...trade.RequestOption,
) ([]Order, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/api/v3/allOrders", request.values(),
		privateLimit("all-orders", 10), "private-read", client.privateReadQuota,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeOrders(client, response)
}

// MyTrades는 최대 한 달 범위의 계정 Spot 체결을 조회한다.
func (client *Client) MyTrades(
	ctx context.Context,
	request MyTradesRequest,
	options ...trade.RequestOption,
) ([]AccountTrade, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/api/v3/myTrades", request.values(),
		privateLimit("my-trades", 10), "private-read", client.privateReadQuota,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	var rawItems []json.RawMessage
	if _, err := client.decodeResponse(response, &rawItems); err != nil {
		return nil, err
	}
	items := make([]AccountTrade, len(rawItems))
	for index, raw := range rawItems {
		if err := json.Unmarshal(raw, &items[index]); err != nil {
			return nil, client.decodeBodyError(response, err)
		}
		items[index].Raw = cloneBytes(raw)
	}
	return items, nil
}

func decodeOrder(
	client *Client,
	response commonexchange.Response,
	operation commonexchange.OperationKind,
) (Order, error) {
	var value Order
	raw, err := client.decodeResponseForOperation(response, operation, &value)
	if err != nil {
		return Order{}, err
	}
	if value.Symbol == "" || value.OrderID == "" {
		return Order{}, client.decodeBodyErrorForOperation(
			response, operation, errors.New("MEXC order response is missing symbol or order ID"),
		)
	}
	value.Raw = raw
	return value, nil
}

func decodeOrders(client *Client, response commonexchange.Response) ([]Order, error) {
	var rawItems []json.RawMessage
	if _, err := client.decodeResponse(response, &rawItems); err != nil {
		return nil, err
	}
	items := make([]Order, len(rawItems))
	for index, raw := range rawItems {
		if err := json.Unmarshal(raw, &items[index]); err != nil {
			return nil, client.decodeBodyError(response, err)
		}
		if items[index].Symbol == "" || items[index].OrderID == "" {
			return nil, client.decodeBodyError(
				response, errors.New("MEXC order list item is missing symbol or order ID"),
			)
		}
		items[index].Raw = cloneBytes(raw)
	}
	return items, nil
}
