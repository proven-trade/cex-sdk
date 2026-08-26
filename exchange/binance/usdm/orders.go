package usdm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

// PlaceOrder는 USDⓈ-M Futures 주문을 생성한다.
// 불명확한 전송 결과는 UNKNOWN_EXECUTION_STATE로 반환하며 자동 재시도하지 않는다.
func (client *Client) PlaceOrder(ctx context.Context, request PlaceOrderRequest, options ...trade.RequestOption) (Order, error) {
	if err := request.validate(); err != nil {
		return Order{}, err
	}
	response, err := client.executeSigned(ctx, http.MethodPost, "/fapi/v1/order", request.values(), 0, 1, credential.PermissionTrade, commonexchange.OperationMutation, options...)
	if err != nil {
		return Order{}, err
	}
	return client.decodeOrder(response, commonexchange.OperationMutation)
}

// OrderInfo는 USDⓈ-M Futures 주문을 단건 조회한다.
func (client *Client) OrderInfo(ctx context.Context, request OrderInfoRequest, options ...trade.RequestOption) (Order, error) {
	if err := request.validate(); err != nil {
		return Order{}, err
	}
	response, err := client.executeSigned(ctx, http.MethodGet, "/fapi/v1/order", request.values(), 1, 0, credential.PermissionRead, commonexchange.OperationRead, options...)
	if err != nil {
		return Order{}, err
	}
	return client.decodeOrder(response, commonexchange.OperationRead)
}

// CancelOrder는 USDⓈ-M Futures 주문 취소를 요청한다.
func (client *Client) CancelOrder(ctx context.Context, request OrderInfoRequest, options ...trade.RequestOption) (Order, error) {
	if err := request.validate(); err != nil {
		return Order{}, err
	}
	response, err := client.executeSigned(ctx, http.MethodDelete, "/fapi/v1/order", request.values(), 1, 0, credential.PermissionTrade, commonexchange.OperationMutation, options...)
	if err != nil {
		return Order{}, err
	}
	return client.decodeOrder(response, commonexchange.OperationMutation)
}

// OpenOrders는 단일 또는 전체 계약의 미체결 주문을 조회한다.
func (client *Client) OpenOrders(ctx context.Context, request OpenOrdersRequest, options ...trade.RequestOption) ([]Order, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	values := make(url.Values)
	set(values, "symbol", request.Symbol)
	weight := 40
	if !request.AllSymbols {
		weight = 1
	}
	response, err := client.executeSigned(ctx, http.MethodGet, "/fapi/v1/openOrders", values, weight, 0, credential.PermissionRead, commonexchange.OperationRead, options...)
	if err != nil {
		return nil, err
	}
	return client.decodeOrders(response)
}

// OrderHistory는 단일 계약의 주문 이력을 조회한다.
func (client *Client) OrderHistory(ctx context.Context, request OrderHistoryRequest, options ...trade.RequestOption) ([]Order, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executeSigned(ctx, http.MethodGet, "/fapi/v1/allOrders", request.values(), 5, 0, credential.PermissionRead, commonexchange.OperationRead, options...)
	if err != nil {
		return nil, err
	}
	return client.decodeOrders(response)
}

func (client *Client) decodeOrder(response commonexchange.Response, operation commonexchange.OperationKind) (Order, error) {
	var order Order
	if err := client.decode(response, operation, &order); err != nil {
		return Order{}, err
	}
	order.Raw = cloneBytes(response.Body)
	return order, nil
}

func (client *Client) decodeOrders(response commonexchange.Response) ([]Order, error) {
	var rawItems []json.RawMessage
	if err := client.decode(response, commonexchange.OperationRead, &rawItems); err != nil {
		return nil, err
	}
	orders := make([]Order, len(rawItems))
	for index, raw := range rawItems {
		if err := json.Unmarshal(raw, &orders[index]); err != nil {
			return nil, client.decodeError(response, commonexchange.OperationRead, err)
		}
		orders[index].Raw = cloneBytes(raw)
	}
	return orders, nil
}
