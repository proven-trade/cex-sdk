package coinone

import (
	"context"
	"encoding/json"
	"errors"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

// CancelAllOrdersRequest는 한 종목에 걸려 있는 전체 주문의 취소 조건이다.
type CancelAllOrdersRequest struct {
	QuoteCurrency  string
	TargetCurrency string
}

// CancelAllResult는 종목별 전체 주문 취소 결과다.
// CanceledCount가 TotalOrderCount보다 작으면 일부 주문이 취소되지 않았으므로
// ActiveOrders로 남은 주문을 확인해야 한다.
type CancelAllResult struct {
	CanceledCount   int             `json:"canceled_count"`
	TotalOrderCount int             `json:"total_order_count"`
	Raw             json.RawMessage `json:"-"`
}

func (request CancelAllOrdersRequest) validate() error {
	return validatePair(request.QuoteCurrency, request.TargetCurrency)
}

func (request CancelAllOrdersRequest) fields() payloadFields {
	return pairFields(request.QuoteCurrency, request.TargetCurrency)
}

// CancelAllOrders는 지정한 종목에 걸려 있는 전체 주문을 취소한다. Trade 권한이 필요하다.
// 주문 API 요청 제한을 사용하며 자동 재시도하지 않는다.
// 전송 결과가 불명확하면 UNKNOWN_EXECUTION_STATE로 반환하므로 ActiveOrders로 상태를 확인해야 한다.
func (client *Client) CancelAllOrders(
	ctx context.Context,
	request CancelAllOrdersRequest,
	options ...trade.RequestOption,
) (CancelAllResult, error) {
	if err := request.validate(); err != nil {
		return CancelAllResult{}, err
	}
	response, err := client.executePrivate(
		ctx, "/v2.1/order/cancel/all", request.fields(), true,
		credential.PermissionTrade, commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return CancelAllResult{}, err
	}
	var envelope struct {
		CanceledCount   *int `json:"canceled_count"`
		TotalOrderCount *int `json:"total_order_count"`
	}
	if err := client.decodeResponse(response, commonexchange.OperationMutation, &envelope); err != nil {
		return CancelAllResult{}, err
	}
	if envelope.CanceledCount == nil || envelope.TotalOrderCount == nil {
		return CancelAllResult{}, client.decodeBodyError(
			response, commonexchange.OperationMutation, errors.New("Coinone cancel all response has no order counts"),
		)
	}
	result := CancelAllResult{
		CanceledCount:   *envelope.CanceledCount,
		TotalOrderCount: *envelope.TotalOrderCount,
		Raw:             cloneBytes(response.Body),
	}
	return result, nil
}
