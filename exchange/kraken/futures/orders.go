package futures

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

// OpenOrders는 계정의 모든 미체결 Futures 주문을 조회한다.
func (client *Client) OpenOrders(
	ctx context.Context,
	options ...trade.RequestOption,
) ([]Order, error) {
	response, err := client.executePrivate(
		ctx, http.MethodGet, derivativesPrefix+"openorders", nil, 2,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	var envelope struct {
		OpenOrders []json.RawMessage `json:"openOrders"`
	}
	if err := client.decodeSuccess(response, commonexchange.OperationRead, &envelope); err != nil {
		return nil, err
	}
	orders, err := decodeRawItems(envelope.OpenOrders, func(item *Order, raw []byte) { item.Raw = raw })
	if err != nil {
		return nil, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	return orders, nil
}

// Fills는 계정의 Futures 체결 이력을 조회한다.
func (client *Client) Fills(
	ctx context.Context,
	request FillsRequest,
	options ...trade.RequestOption,
) ([]Fill, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	cost := 2
	if request.LastFillTime != "" {
		cost = 25
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, derivativesPrefix+"fills", request.values(), cost,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Fills []json.RawMessage `json:"fills"`
	}
	if err := client.decodeSuccess(response, commonexchange.OperationRead, &envelope); err != nil {
		return nil, err
	}
	fills, err := decodeRawItems(envelope.Fills, func(item *Fill, raw []byte) { item.Raw = raw })
	if err != nil {
		return nil, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	return fills, nil
}

// PlaceOrder는 Futures 주문을 생성한다.
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
		ctx, http.MethodPost, derivativesPrefix+"sendorder", request.values(), 10,
		credential.PermissionTrade, commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return OrderReference{}, err
	}
	var envelope struct {
		SendStatus json.RawMessage `json:"sendStatus"`
	}
	if err := client.decodeSuccess(response, commonexchange.OperationMutation, &envelope); err != nil {
		return OrderReference{}, err
	}
	var reference OrderReference
	if err := json.Unmarshal(envelope.SendStatus, &reference); err != nil {
		return OrderReference{}, client.decodeBodyError(response, commonexchange.OperationMutation, err)
	}
	if reference.Status != "placed" {
		return OrderReference{}, client.decodeError(
			response, reference.Status, reference.Status, commonexchange.OperationMutation, nil,
		)
	}
	if reference.OrderID == "" {
		return OrderReference{}, client.decodeBodyError(
			response, commonexchange.OperationMutation,
			errors.New("Kraken Futures order acknowledgement identity is missing"),
		)
	}
	reference.Raw = cloneBytes(envelope.SendStatus)
	return reference, nil
}

// CancelOrder는 거래소 주문 ID 또는 client order ID로 Futures 주문을 취소한다.
// 성공 응답은 취소 접수이며 최종 상태는 주문 조회 또는 private stream으로 확인해야 한다.
func (client *Client) CancelOrder(
	ctx context.Context,
	request CancelOrderRequest,
	options ...trade.RequestOption,
) (CancelResult, error) {
	if err := request.validate(); err != nil {
		return CancelResult{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodPost, derivativesPrefix+"cancelorder", request.values(), 10,
		credential.PermissionTrade, commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return CancelResult{}, err
	}
	var envelope struct {
		CancelStatus json.RawMessage `json:"cancelStatus"`
	}
	if err := client.decodeSuccess(response, commonexchange.OperationMutation, &envelope); err != nil {
		return CancelResult{}, err
	}
	var canceled CancelResult
	if err := json.Unmarshal(envelope.CancelStatus, &canceled); err != nil {
		return CancelResult{}, client.decodeBodyError(response, commonexchange.OperationMutation, err)
	}
	if canceled.Status != "cancelled" {
		return CancelResult{}, client.decodeError(
			response, canceled.Status, canceled.Status, commonexchange.OperationMutation, nil,
		)
	}
	if canceled.OrderID == "" {
		return CancelResult{}, client.decodeBodyError(
			response, commonexchange.OperationMutation,
			errors.New("Kraken Futures cancel acknowledgement identity is missing"),
		)
	}
	canceled.Raw = cloneBytes(envelope.CancelStatus)
	return canceled, nil
}

// OrderStatus는 열려 있거나 최근 5초 안에 변경된 지정 주문의 상태를 조회한다.
func (client *Client) OrderStatus(
	ctx context.Context,
	request OrderStatusRequest,
	options ...trade.RequestOption,
) ([]OrderStatus, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodPost, derivativesPrefix+"orders/status", request.values(), 1,
		credential.PermissionTrade, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Orders []json.RawMessage `json:"orders"`
	}
	if err := client.decodeSuccess(response, commonexchange.OperationRead, &envelope); err != nil {
		return nil, err
	}
	statuses, err := decodeRawItems(
		envelope.Orders, func(item *OrderStatus, raw []byte) { item.Raw = raw },
	)
	if err != nil {
		return nil, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	return statuses, nil
}
