package htx

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

// Accounts는 현재 API Key가 접근할 수 있는 HTX 계정 목록을 조회한다.
func (client *Client) Accounts(
	ctx context.Context,
	options ...trade.RequestOption,
) ([]Account, error) {
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/v1/account/accounts", nil, nil,
		rateGroupAccount, client.accountQuota, credential.PermissionRead,
		commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	var rawItems []json.RawMessage
	if _, err := client.decodeDataForOperation(
		response, commonexchange.OperationRead, &rawItems,
	); err != nil {
		return nil, err
	}
	items := make([]Account, len(rawItems))
	for index, raw := range rawItems {
		if err := json.Unmarshal(raw, &items[index]); err != nil {
			return nil, client.decodeBodyError(response, err)
		}
		if items[index].ID == "" {
			return nil, client.decodeBodyError(
				response, errors.New("HTX account item is missing an ID"),
			)
		}
		items[index].Raw = cloneBytes(raw)
	}
	return items, nil
}

// AccountBalance는 지정한 HTX 계정의 통화별 사용 가능·동결 잔고를 조회한다.
func (client *Client) AccountBalance(
	ctx context.Context,
	accountID string,
	options ...trade.RequestOption,
) (AccountBalance, error) {
	if !accountIDPattern.MatchString(accountID) {
		return AccountBalance{}, validationError("invalid HTX account ID %q", accountID)
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/v1/account/accounts/"+accountID+"/balance", nil, nil,
		rateGroupAccount, client.accountQuota, credential.PermissionRead,
		commonexchange.OperationRead, options...,
	)
	if err != nil {
		return AccountBalance{}, err
	}
	var wire struct {
		ID       Scalar            `json:"id"`
		Type     string            `json:"type"`
		State    string            `json:"state"`
		Balances []json.RawMessage `json:"list"`
	}
	raw, err := client.decodeDataForOperation(
		response, commonexchange.OperationRead, &wire,
	)
	if err != nil {
		return AccountBalance{}, err
	}
	if wire.ID == "" {
		return AccountBalance{}, client.decodeBodyError(
			response, errors.New("HTX account balance is missing an account ID"),
		)
	}
	value := AccountBalance{
		ID: wire.ID, Type: wire.Type, State: wire.State,
		Balances: make([]Balance, len(wire.Balances)), Raw: raw,
	}
	for index, balanceRaw := range wire.Balances {
		if err := json.Unmarshal(balanceRaw, &value.Balances[index]); err != nil {
			return AccountBalance{}, client.decodeBodyError(response, err)
		}
		value.Balances[index].Raw = cloneBytes(balanceRaw)
	}
	return value, nil
}
