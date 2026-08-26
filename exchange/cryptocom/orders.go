package cryptocom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

const (
	methodCreateOrder      = "private/create-order"
	methodGetOrderDetail   = "private/get-order-detail"
	methodCancelOrder      = "private/cancel-order"
	methodGetOpenOrders    = "private/get-open-orders"
	methodGetOrderHistory  = "private/get-order-history"
	methodGetAccountTrades = "private/get-trades"
)

// PlaceOrder는 Crypto.com Spot 주문을 비동기로 접수한다.
func (client *Client) PlaceOrder(
	ctx context.Context,
	request PlaceOrderRequest,
	options ...trade.RequestOption,
) (OrderReceipt, error) {
	if err := request.validate(); err != nil {
		return OrderReceipt{}, err
	}
	response, requestID, err := client.executePrivate(
		ctx, methodCreateOrder, request.params(), credential.PermissionTrade,
		commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return OrderReceipt{}, err
	}
	resultRaw, err := client.decodePrivateResult(
		response, methodCreateOrder, requestID, commonexchange.OperationMutation, true,
	)
	if err != nil {
		return OrderReceipt{}, err
	}
	var value OrderReceipt
	if err := json.Unmarshal(resultRaw, &value); err != nil {
		return OrderReceipt{}, client.decodePrivateBodyError(
			response, commonexchange.OperationMutation, requestID, err,
		)
	}
	if value.OrderID == "" || value.ClientOrderID != request.ClientOrderID {
		return OrderReceipt{}, client.decodePrivateBodyError(
			response, commonexchange.OperationMutation, requestID,
			errors.New("Crypto.com order receipt identity is inconsistent"),
		)
	}
	value.Raw = cloneBytes(resultRaw)
	return value, nil
}

// OrderInfo는 거래소 주문 ID 또는 사용자 주문 ID로 주문 상세를 조회한다.
func (client *Client) OrderInfo(
	ctx context.Context,
	request OrderInfoRequest,
	options ...trade.RequestOption,
) (Order, error) {
	if err := request.validate(); err != nil {
		return Order{}, err
	}
	response, requestID, err := client.executePrivate(
		ctx, methodGetOrderDetail, request.params(), credential.PermissionRead,
		commonexchange.OperationRead, options...,
	)
	if err != nil {
		return Order{}, err
	}
	resultRaw, err := client.decodePrivateResult(
		response, methodGetOrderDetail, requestID, commonexchange.OperationRead, true,
	)
	if err != nil {
		return Order{}, err
	}
	value, err := client.decodeOrder(response, requestID, resultRaw)
	if err != nil {
		return Order{}, err
	}
	if request.OrderID != "" && string(value.OrderID) != request.OrderID ||
		request.ClientOrderID != "" && value.ClientOrderID != request.ClientOrderID {
		return Order{}, client.decodePrivateBodyError(
			response, commonexchange.OperationRead, requestID,
			errors.New("Crypto.com order detail identity is inconsistent"),
		)
	}
	return value, nil
}

// CancelOrder는 Crypto.com Spot 주문 취소를 비동기로 접수한다.
func (client *Client) CancelOrder(
	ctx context.Context,
	request CancelOrderRequest,
	options ...trade.RequestOption,
) (CancelAcknowledgement, error) {
	if err := request.validate(); err != nil {
		return CancelAcknowledgement{}, err
	}
	response, requestID, err := client.executePrivate(
		ctx, methodCancelOrder, request.params(), credential.PermissionTrade,
		commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return CancelAcknowledgement{}, err
	}
	resultRaw, err := client.decodePrivateResult(
		response, methodCancelOrder, requestID, commonexchange.OperationMutation, false,
	)
	if err != nil {
		return CancelAcknowledgement{}, err
	}
	value := CancelAcknowledgement{
		OrderID: Scalar(request.OrderID), ClientOrderID: request.ClientOrderID,
		Raw: cloneBytes(resultRaw),
	}
	if len(resultRaw) > 0 {
		if err := json.Unmarshal(resultRaw, &value); err != nil {
			return CancelAcknowledgement{}, client.decodePrivateBodyError(
				response, commonexchange.OperationMutation, requestID, err,
			)
		}
		value.Raw = cloneBytes(resultRaw)
	}
	if request.OrderID != "" && value.OrderID != "" && string(value.OrderID) != request.OrderID ||
		request.ClientOrderID != "" && value.ClientOrderID != "" &&
			value.ClientOrderID != request.ClientOrderID {
		return CancelAcknowledgement{}, client.decodePrivateBodyError(
			response, commonexchange.OperationMutation, requestID,
			errors.New("Crypto.com cancel acknowledgement identity is inconsistent"),
		)
	}
	return value, nil
}

// OpenOrders는 선택한 거래쌍 또는 전체 Crypto.com Spot 미체결 주문을 조회한다.
func (client *Client) OpenOrders(
	ctx context.Context,
	request OpenOrdersRequest,
	options ...trade.RequestOption,
) ([]Order, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	return client.orderList(
		ctx, methodGetOpenOrders, request.params(), request.InstrumentName, options...,
	)
}

// OrderHistory는 선택한 거래쌍 또는 전체 Crypto.com Spot 종료 주문 이력을 조회한다.
func (client *Client) OrderHistory(
	ctx context.Context,
	request OrderHistoryRequest,
	options ...trade.RequestOption,
) ([]Order, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	return client.orderList(
		ctx, methodGetOrderHistory, request.params(), request.InstrumentName, options...,
	)
}

func (client *Client) orderList(
	ctx context.Context,
	method string,
	params map[string]any,
	instrumentName string,
	options ...trade.RequestOption,
) ([]Order, error) {
	response, requestID, err := client.executePrivate(
		ctx, method, params, credential.PermissionRead,
		commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	resultRaw, err := client.decodePrivateResult(
		response, method, requestID, commonexchange.OperationRead, true,
	)
	if err != nil {
		return nil, err
	}
	itemsRaw, err := client.decodePrivateDataItems(
		response, resultRaw, commonexchange.OperationRead, requestID,
	)
	if err != nil {
		return nil, err
	}
	items := make([]Order, len(itemsRaw))
	for index, itemRaw := range itemsRaw {
		item, err := client.decodeOrder(response, requestID, itemRaw)
		if err != nil {
			return nil, err
		}
		if instrumentName != "" && item.InstrumentName != instrumentName {
			return nil, client.decodePrivateBodyError(
				response, commonexchange.OperationRead, requestID,
				fmt.Errorf("unexpected Crypto.com order instrument %q", item.InstrumentName),
			)
		}
		items[index] = item
	}
	return items, nil
}

// AccountTrades는 선택한 거래쌍 또는 전체 Crypto.com Spot 계정 체결 이력을 조회한다.
func (client *Client) AccountTrades(
	ctx context.Context,
	request AccountTradesRequest,
	options ...trade.RequestOption,
) ([]AccountTrade, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, requestID, err := client.executePrivate(
		ctx, methodGetAccountTrades, request.params(), credential.PermissionRead,
		commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	resultRaw, err := client.decodePrivateResult(
		response, methodGetAccountTrades, requestID, commonexchange.OperationRead, true,
	)
	if err != nil {
		return nil, err
	}
	itemsRaw, err := client.decodePrivateDataItems(
		response, resultRaw, commonexchange.OperationRead, requestID,
	)
	if err != nil {
		return nil, err
	}
	items := make([]AccountTrade, len(itemsRaw))
	for index, itemRaw := range itemsRaw {
		if err := json.Unmarshal(itemRaw, &items[index]); err != nil {
			return nil, client.decodePrivateBodyError(
				response, commonexchange.OperationRead, requestID, err,
			)
		}
		if items[index].OrderID == "" || items[index].TradeID == "" ||
			items[index].InstrumentName == "" ||
			(items[index].Side != OrderSideBuy && items[index].Side != OrderSideSell) {
			return nil, client.decodePrivateBodyError(
				response, commonexchange.OperationRead, requestID,
				errors.New("Crypto.com account trade identity or side is invalid"),
			)
		}
		if request.InstrumentName != "" && items[index].InstrumentName != request.InstrumentName {
			return nil, client.decodePrivateBodyError(
				response, commonexchange.OperationRead, requestID,
				errors.New("Crypto.com account trade instrument is inconsistent"),
			)
		}
		items[index].Raw = cloneBytes(itemRaw)
	}
	return items, nil
}

func (client *Client) decodeOrder(
	response commonexchange.Response,
	requestID string,
	raw json.RawMessage,
) (Order, error) {
	var value Order
	if err := json.Unmarshal(raw, &value); err != nil {
		return Order{}, client.decodePrivateBodyError(
			response, commonexchange.OperationRead, requestID, err,
		)
	}
	if value.OrderID == "" || value.InstrumentName == "" ||
		(value.Side != OrderSideBuy && value.Side != OrderSideSell) {
		return Order{}, client.decodePrivateBodyError(
			response, commonexchange.OperationRead, requestID,
			errors.New("Crypto.com order identity or side is invalid"),
		)
	}
	value.Raw = cloneBytes(raw)
	return value, nil
}
