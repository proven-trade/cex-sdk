package gateio

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

// PlaceOrder는 Gate.io Spot 지정가 또는 시장가 주문을 생성한다.
// 전송 결과가 불명확하면 자동 재시도하지 않고 UNKNOWN_EXECUTION_STATE로 반환한다.
func (client *Client) PlaceOrder(
	ctx context.Context,
	request PlaceOrderRequest,
	options ...trade.RequestOption,
) (Order, error) {
	if err := request.validate(); err != nil {
		return Order{}, err
	}
	body, err := json.Marshal(request)
	if err != nil {
		return Order{}, validationError("encode Gate.io order: %v", err)
	}
	response, err := client.executePrivate(
		ctx, http.MethodPost, "/spot/orders", nil, body,
		orderLimit(request.CurrencyPair), credential.PermissionTrade,
		commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return Order{}, err
	}
	order, err := client.decodeOrder(response, commonexchange.OperationMutation)
	if err != nil {
		return Order{}, err
	}
	if order.ID == "" {
		return Order{}, client.decodeBodyError(
			response, commonexchange.OperationMutation,
			errors.New("Gate.io order acknowledgement is empty"),
		)
	}
	return order, nil
}

// OrderInfo는 거래소 주문 ID와 거래쌍으로 Spot 주문 한 건을 조회한다.
func (client *Client) OrderInfo(
	ctx context.Context,
	request OrderInfoRequest,
	options ...trade.RequestOption,
) (Order, error) {
	if err := request.validate(); err != nil {
		return Order{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/spot/orders/"+url.PathEscape(request.OrderID),
		url.Values{"currency_pair": {request.CurrencyPair}}, nil,
		privateLimit("order-info"), credential.PermissionRead,
		commonexchange.OperationRead, options...,
	)
	if err != nil {
		return Order{}, err
	}
	return client.decodeOrder(response, commonexchange.OperationRead)
}

// CancelOrder는 거래소 주문 ID와 거래쌍으로 Spot 주문 취소를 접수한다.
// 성공 응답은 취소 접수이며 최종 상태는 주문 조회 또는 private stream으로 확인해야 한다.
func (client *Client) CancelOrder(
	ctx context.Context,
	request CancelOrderRequest,
	options ...trade.RequestOption,
) (Order, error) {
	if err := request.validate(); err != nil {
		return Order{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodDelete, "/spot/orders/"+url.PathEscape(request.OrderID),
		url.Values{"currency_pair": {request.CurrencyPair}}, nil,
		cancelLimit(), credential.PermissionTrade,
		commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return Order{}, err
	}
	order, err := client.decodeOrder(response, commonexchange.OperationMutation)
	if err != nil {
		return Order{}, err
	}
	if order.ID == "" {
		return Order{}, client.decodeBodyError(
			response, commonexchange.OperationMutation,
			errors.New("Gate.io cancel acknowledgement is empty"),
		)
	}
	return order, nil
}

// OpenOrders는 거래쌍별 Gate.io Spot 미체결 주문을 조회한다.
func (client *Client) OpenOrders(
	ctx context.Context,
	request OpenOrdersRequest,
	options ...trade.RequestOption,
) ([]OpenOrderGroup, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/spot/open_orders", request.values(), nil,
		privateLimit("open-orders"), credential.PermissionRead,
		commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	var rawGroups []json.RawMessage
	if _, err := client.decodeData(response, commonexchange.OperationRead, &rawGroups); err != nil {
		return nil, err
	}
	groups := make([]OpenOrderGroup, len(rawGroups))
	for index, raw := range rawGroups {
		var decoded struct {
			CurrencyPair string            `json:"currency_pair"`
			Total        int               `json:"total"`
			Orders       []json.RawMessage `json:"orders"`
		}
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return nil, client.decodeBodyError(response, commonexchange.OperationRead, err)
		}
		groups[index] = OpenOrderGroup{
			CurrencyPair: decoded.CurrencyPair, Total: decoded.Total,
			Orders: make([]Order, len(decoded.Orders)), Raw: cloneBytes(raw),
		}
		for orderIndex, orderRaw := range decoded.Orders {
			if err := json.Unmarshal(orderRaw, &groups[index].Orders[orderIndex]); err != nil {
				return nil, client.decodeBodyError(response, commonexchange.OperationRead, err)
			}
			groups[index].Orders[orderIndex].Raw = cloneBytes(orderRaw)
		}
	}
	return groups, nil
}

// MyTrades는 거래쌍 또는 주문으로 필터링한 Gate.io Spot 체결 이력을 조회한다.
func (client *Client) MyTrades(
	ctx context.Context,
	request MyTradesRequest,
	options ...trade.RequestOption,
) ([]Trade, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/spot/my_trades", request.values(), nil,
		privateLimit("my-trades"), credential.PermissionRead,
		commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	return client.decodeTrades(response)
}

func (client *Client) decodeOrder(
	response commonexchange.Response,
	operation commonexchange.OperationKind,
) (Order, error) {
	var order Order
	data, err := client.decodeData(response, operation, &order)
	if err != nil {
		return Order{}, err
	}
	order.Raw = cloneBytes(data)
	return order, nil
}
