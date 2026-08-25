package mexc

import (
	"context"
	"encoding/json"
	"net/http"

	trade "github.com/proven-trade/proven-trade-sdk"
	"github.com/proven-trade/proven-trade-sdk/credential"
	commonexchange "github.com/proven-trade/proven-trade-sdk/exchange"
)

// SelfSymbols는 현재 API Key로 Spot 거래가 허용된 거래쌍을 조회한다.
func (client *Client) SelfSymbols(
	ctx context.Context,
	options ...trade.RequestOption,
) ([]string, error) {
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/api/v3/selfSymbols", nil,
		privateLimit("self-symbols", 1), "private-read", client.privateReadQuota,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Code Scalar   `json:"code"`
		Data []string `json:"data"`
	}
	if _, err := client.decodeResponse(response, &envelope); err != nil {
		return nil, err
	}
	return envelope.Data, nil
}

// Account는 현재 Spot 계정 권한과 자산별 잔고를 조회한다.
func (client *Client) Account(
	ctx context.Context,
	options ...trade.RequestOption,
) (Account, error) {
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/api/v3/account", nil,
		privateLimit("account", 10), "account", client.accountQuota,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return Account{}, err
	}
	var wire struct {
		CanTrade    bool              `json:"canTrade"`
		CanWithdraw bool              `json:"canWithdraw"`
		CanDeposit  bool              `json:"canDeposit"`
		UpdateTime  Scalar            `json:"updateTime"`
		AccountType string            `json:"accountType"`
		Balances    []json.RawMessage `json:"balances"`
		Permissions []string          `json:"permissions"`
	}
	raw, err := client.decodeResponse(response, &wire)
	if err != nil {
		return Account{}, err
	}
	value := Account{
		CanTrade: wire.CanTrade, CanWithdraw: wire.CanWithdraw, CanDeposit: wire.CanDeposit,
		UpdateTime: wire.UpdateTime, AccountType: wire.AccountType,
		Balances: make([]Balance, len(wire.Balances)), Permissions: wire.Permissions, Raw: raw,
	}
	for index, balanceRaw := range wire.Balances {
		if err := json.Unmarshal(balanceRaw, &value.Balances[index]); err != nil {
			return Account{}, client.decodeBodyError(response, err)
		}
		value.Balances[index].Raw = cloneBytes(balanceRaw)
	}
	return value, nil
}
