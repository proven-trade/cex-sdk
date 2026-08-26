package coinbase

import (
	"context"
	"encoding/json"
	"net/http"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

// Accounts는 cursor 기반 Spot 잔고 목록을 조회한다.
func (client *Client) Accounts(
	ctx context.Context,
	request AccountsRequest,
	options ...trade.RequestOption,
) (AccountPage, error) {
	if err := request.validate(); err != nil {
		return AccountPage{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, publicPrefix+"/accounts", request.values(), nil,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return AccountPage{}, err
	}
	var rawPage struct {
		Accounts []json.RawMessage `json:"accounts"`
		HasNext  bool              `json:"has_next"`
		Cursor   string            `json:"cursor"`
		Size     int               `json:"size"`
	}
	if err := client.decodeSuccess(response, commonexchange.OperationRead, &rawPage); err != nil {
		return AccountPage{}, err
	}
	page := AccountPage{
		Accounts: make([]Account, len(rawPage.Accounts)), HasNext: rawPage.HasNext,
		Cursor: rawPage.Cursor, Size: rawPage.Size, Raw: cloneBytes(response.Body),
	}
	for index, raw := range rawPage.Accounts {
		if err := json.Unmarshal(raw, &page.Accounts[index]); err != nil {
			return AccountPage{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
		}
		page.Accounts[index].Raw = cloneBytes(raw)
	}
	return page, nil
}

// Account는 UUID로 단일 Spot 잔고를 조회한다.
func (client *Client) Account(
	ctx context.Context,
	accountUUID string,
	options ...trade.RequestOption,
) (Account, error) {
	segment, err := escapePathSegment("account UUID", accountUUID)
	if err != nil {
		return Account{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, publicPrefix+"/accounts/"+segment, nil, nil,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return Account{}, err
	}
	var envelope struct {
		Account json.RawMessage `json:"account"`
	}
	if err := client.decodeSuccess(response, commonexchange.OperationRead, &envelope); err != nil {
		return Account{}, err
	}
	var account Account
	if err := json.Unmarshal(envelope.Account, &account); err != nil {
		return Account{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	account.Raw = cloneBytes(envelope.Account)
	return account, nil
}
