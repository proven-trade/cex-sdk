package bithumb

import (
	"context"
	"net/http"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

// Accounts는 빗썸 계정의 코인별 잔고를 조회한다.
func (client *Client) Accounts(ctx context.Context, options ...trade.RequestOption) ([]Balance, error) {
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/v1/accounts", nil, nil, rateLimitPrivateOther,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeItems(client, response, commonexchange.OperationRead, func(item *Balance, raw []byte) {
		item.Raw = raw
	})
}
