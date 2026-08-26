package korbit

import (
	"context"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

// Balances는 선택한 하위 계정의 일부 또는 전체 자산 잔고를 조회한다.
func (client *Client) Balances(
	ctx context.Context,
	request BalanceRequest,
	options ...trade.RequestOption,
) ([]Balance, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePrivate(
		ctx, "GET", "/v2/balance", request.values(), rateGroupPrivate,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeItems[Balance](client, response, func(item *Balance, raw []byte) { item.Raw = raw })
}
