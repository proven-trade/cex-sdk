package coinone

import (
	"context"
	"encoding/json"

	trade "github.com/proven-trade/proven-trade-sdk"
	"github.com/proven-trade/proven-trade-sdk/credential"
	commonexchange "github.com/proven-trade/proven-trade-sdk/exchange"
)

// Accounts는 계정의 통화별 잔고를 조회한다.
func (client *Client) Accounts(ctx context.Context, options ...trade.RequestOption) ([]Balance, error) {
	response, err := client.executePrivate(
		ctx, "/v2.1/account/balance/all", nil, false,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Balances []json.RawMessage `json:"balances"`
	}
	if err := client.decodeResponse(response, commonexchange.OperationRead, &envelope); err != nil {
		return nil, err
	}
	return decodeRawItems(client, response, envelope.Balances, func(item *Balance, raw []byte) {
		item.Raw = raw
	})
}
