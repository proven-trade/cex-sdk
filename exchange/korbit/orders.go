package korbit

import (
	"context"
	"errors"
	"net/http"

	trade "github.com/proven-trade/proven-trade-sdk"
	"github.com/proven-trade/proven-trade-sdk/credential"
	commonexchange "github.com/proven-trade/proven-trade-sdk/exchange"
)

// PlaceOrder는 코빗 Spot 주문을 고유한 clientOrderId와 함께 생성한다.
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
		ctx, http.MethodPost, "/v2/orders", request.values(), rateGroupOrderPlace,
		credential.PermissionTrade, commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return OrderReference{}, err
	}
	var result struct {
		OrderID int64 `json:"orderId"`
	}
	raw, err := client.decodeResponse(response, commonexchange.OperationMutation, &result)
	if err != nil {
		return OrderReference{}, err
	}
	if result.OrderID <= 0 {
		return OrderReference{}, client.decodeBodyError(
			response, commonexchange.OperationMutation, errors.New("Korbit order response has no order ID"),
		)
	}
	return OrderReference{OrderID: result.OrderID, Raw: raw}, nil
}

// OrderInfo는 거래소 주문 ID 또는 clientOrderId로 주문 상세를 조회한다.
func (client *Client) OrderInfo(
	ctx context.Context,
	request OrderInfoRequest,
	options ...trade.RequestOption,
) (Order, error) {
	if err := request.validate(); err != nil {
		return Order{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/v2/orders", request.values(), rateGroupPrivate,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return Order{}, err
	}
	var result Order
	raw, err := client.decodeResponse(response, commonexchange.OperationRead, &result)
	if err != nil {
		return Order{}, err
	}
	if result.OrderID <= 0 {
		return Order{}, client.decodeBodyError(
			response, commonexchange.OperationRead, errors.New("Korbit order detail has no order ID"),
		)
	}
	result.Raw = raw
	return result, nil
}

// CancelOrder는 거래소 주문 ID 또는 clientOrderId로 주문 취소를 요청한다.
func (client *Client) CancelOrder(
	ctx context.Context,
	request CancelOrderRequest,
	options ...trade.RequestOption,
) (CancelResult, error) {
	if err := request.validate(); err != nil {
		return CancelResult{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodDelete, "/v2/orders", request.values(), rateGroupOrderCancel,
		credential.PermissionTrade, commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return CancelResult{}, err
	}
	if _, err := client.decodeResponse(response, commonexchange.OperationMutation, nil); err != nil {
		return CancelResult{}, err
	}
	return CancelResult{Accepted: true, Raw: cloneBytes(response.Body)}, nil
}

// OpenOrders는 거래쌍의 미종료 주문을 조회한다.
func (client *Client) OpenOrders(
	ctx context.Context,
	request OpenOrdersRequest,
	options ...trade.RequestOption,
) ([]Order, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/v2/openOrders", request.values(), rateGroupPrivate,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeItems[Order](client, response, func(item *Order, raw []byte) { item.Raw = raw })
}

// OrderHistory는 거래쌍의 최근 36시간 주문 이력을 조회한다.
func (client *Client) OrderHistory(
	ctx context.Context,
	request OrderHistoryRequest,
	options ...trade.RequestOption,
) ([]Order, error) {
	if err := request.validate(client.now()); err != nil {
		return nil, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/v2/allOrders", request.values(), rateGroupPrivate,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeItems[Order](client, response, func(item *Order, raw []byte) { item.Raw = raw })
}

// MyTrades는 거래쌍의 최근 36시간 계정 체결을 조회한다.
func (client *Client) MyTrades(
	ctx context.Context,
	request MyTradesRequest,
	options ...trade.RequestOption,
) ([]PrivateTrade, error) {
	if err := request.validate(client.now()); err != nil {
		return nil, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/v2/myTrades", request.values(), rateGroupPrivate,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeItems[PrivateTrade](client, response, func(item *PrivateTrade, raw []byte) { item.Raw = raw })
}
