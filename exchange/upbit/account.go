package upbit

import (
	"context"
	"net/http"

	trade "github.com/proven-trade/proven-trade-sdk"
	"github.com/proven-trade/proven-trade-sdk/credential"
	commonexchange "github.com/proven-trade/proven-trade-sdk/exchange"
)

// Accounts는 업비트 계정의 코인별 잔고를 조회한다.
func (client *Client) Accounts(ctx context.Context, options ...trade.RequestOption) ([]Balance, error) {
	response, err := client.executeSigned(
		ctx, http.MethodGet, "/v1/accounts", nil, nil, defaultRate,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeItems[Balance](client, response, commonexchange.OperationRead, nil)
}
