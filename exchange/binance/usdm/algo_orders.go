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

// PlaceAlgoOrder는 USDⓈ-M Futures Algo 조건부 주문을 생성한다.
// 불명확한 전송 결과는 UNKNOWN_EXECUTION_STATE로 반환하며 자동 재시도하지 않는다.
func (client *Client) PlaceAlgoOrder(ctx context.Context, request PlaceAlgoOrderRequest, options ...trade.RequestOption) (AlgoOrder, error) {
	if err := request.validate(); err != nil {
		return AlgoOrder{}, err
	}
	response, err := client.executeSigned(ctx, http.MethodPost, "/fapi/v1/algoOrder", request.values(), 0, 1, credential.PermissionTrade, commonexchange.OperationMutation, options...)
	if err != nil {
		return AlgoOrder{}, err
	}
	return client.decodeAlgoOrder(response, commonexchange.OperationMutation)
}

// AlgoOrderInfo는 USDⓈ-M Futures Algo 주문을 단건 조회한다.
func (client *Client) AlgoOrderInfo(ctx context.Context, request AlgoOrderInfoRequest, options ...trade.RequestOption) (AlgoOrder, error) {
	if err := request.validate(); err != nil {
		return AlgoOrder{}, err
	}
	response, err := client.executeSigned(ctx, http.MethodGet, "/fapi/v1/algoOrder", request.values(), 1, 0, credential.PermissionRead, commonexchange.OperationRead, options...)
	if err != nil {
		return AlgoOrder{}, err
	}
	return client.decodeAlgoOrder(response, commonexchange.OperationRead)
}

// CancelAlgoOrder는 USDⓈ-M Futures Algo 주문 취소를 요청한다.
// 응답 본문은 취소 대상 식별자만 포함할 수 있으므로 상태 확인은 AlgoOrderInfo로 한다.
func (client *Client) CancelAlgoOrder(ctx context.Context, request AlgoOrderInfoRequest, options ...trade.RequestOption) (AlgoOrder, error) {
	if err := request.validate(); err != nil {
		return AlgoOrder{}, err
	}
	response, err := client.executeSigned(ctx, http.MethodDelete, "/fapi/v1/algoOrder", request.values(), 1, 0, credential.PermissionTrade, commonexchange.OperationMutation, options...)
	if err != nil {
		return AlgoOrder{}, err
	}
	return client.decodeAlgoOrder(response, commonexchange.OperationMutation)
}

// OpenAlgoOrders는 단일 또는 전체 계약의 미체결 Algo 주문을 조회한다.
func (client *Client) OpenAlgoOrders(ctx context.Context, request OpenAlgoOrdersRequest, options ...trade.RequestOption) ([]AlgoOrder, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	values := make(url.Values)
	set(values, "symbol", request.Symbol)
	weight := 40
	if !request.AllSymbols {
		weight = 1
	}
	response, err := client.executeSigned(ctx, http.MethodGet, "/fapi/v1/openAlgoOrders", values, weight, 0, credential.PermissionRead, commonexchange.OperationRead, options...)
	if err != nil {
		return nil, err
	}
	return client.decodeAlgoOrders(response)
}

// AlgoOrderHistory는 단일 계약의 Algo 주문 이력을 조회한다.
func (client *Client) AlgoOrderHistory(ctx context.Context, request AlgoOrderHistoryRequest, options ...trade.RequestOption) ([]AlgoOrder, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executeSigned(ctx, http.MethodGet, "/fapi/v1/allAlgoOrders", request.values(), 5, 0, credential.PermissionRead, commonexchange.OperationRead, options...)
	if err != nil {
		return nil, err
	}
	return client.decodeAlgoOrders(response)
}

func (client *Client) decodeAlgoOrder(response commonexchange.Response, operation commonexchange.OperationKind) (AlgoOrder, error) {
	var order AlgoOrder
	if err := client.decode(response, operation, &order); err != nil {
		return AlgoOrder{}, err
	}
	order.Raw = cloneBytes(response.Body)
	return order, nil
}

func (client *Client) decodeAlgoOrders(response commonexchange.Response) ([]AlgoOrder, error) {
	var rawItems []json.RawMessage
	if err := client.decode(response, commonexchange.OperationRead, &rawItems); err != nil {
		return nil, err
	}
	orders := make([]AlgoOrder, len(rawItems))
	for index, raw := range rawItems {
		if err := json.Unmarshal(raw, &orders[index]); err != nil {
			return nil, client.decodeError(response, commonexchange.OperationRead, err)
		}
		orders[index].Raw = cloneBytes(raw)
	}
	return orders, nil
}
