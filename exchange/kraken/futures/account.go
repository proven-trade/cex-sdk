package futures

import (
	"context"
	"encoding/json"
	"sort"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

// Accounts는 계정의 cash, margin, multi-collateral 지갑을 조회한다.
func (client *Client) Accounts(
	ctx context.Context,
	options ...trade.RequestOption,
) ([]Account, error) {
	response, err := client.executePrivate(
		ctx, "GET", derivativesPrefix+"accounts", nil, 2,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Accounts map[string]json.RawMessage `json:"accounts"`
	}
	if err := client.decodeSuccess(response, commonexchange.OperationRead, &envelope); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(envelope.Accounts))
	for name := range envelope.Accounts {
		names = append(names, name)
	}
	sort.Strings(names)
	accounts := make([]Account, len(names))
	for index, name := range names {
		raw := envelope.Accounts[name]
		if err := json.Unmarshal(raw, &accounts[index]); err != nil {
			return nil, client.decodeBodyError(response, commonexchange.OperationRead, err)
		}
		accounts[index].Name = name
		accounts[index].Raw = cloneBytes(raw)
	}
	return accounts, nil
}

// OpenPositions는 계정의 현재 열린 Futures 포지션을 조회한다.
func (client *Client) OpenPositions(
	ctx context.Context,
	options ...trade.RequestOption,
) ([]Position, error) {
	response, err := client.executePrivate(
		ctx, "GET", derivativesPrefix+"openpositions", nil, 2,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	var envelope struct {
		OpenPositions []json.RawMessage `json:"openPositions"`
	}
	if err := client.decodeSuccess(response, commonexchange.OperationRead, &envelope); err != nil {
		return nil, err
	}
	positions, err := decodeRawItems(
		envelope.OpenPositions, func(item *Position, raw []byte) { item.Raw = raw },
	)
	if err != nil {
		return nil, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	return positions, nil
}
