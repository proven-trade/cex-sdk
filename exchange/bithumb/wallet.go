package bithumb

import (
	"context"
	"encoding/json"
	"net/http"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

// WalletStatus는 자산·네트워크별 입출금 가능 상태와 블록 동기화 상태다.
// WalletState는 working, withdraw_only, deposit_only, paused 중 하나이고
// BlockState는 normal, delayed, inactive 중 하나다.
type WalletStatus struct {
	Currency            string          `json:"currency"`
	WalletState         string          `json:"wallet_state"`
	BlockState          string          `json:"block_state"`
	BlockHeight         int64           `json:"block_height"`
	BlockUpdatedAt      string          `json:"block_updated_at"`
	BlockElapsedMinutes int             `json:"block_elapsed_minutes"`
	NetType             string          `json:"net_type"`
	NetworkName         string          `json:"network_name"`
	Raw                 json.RawMessage `json:"-"`
}

// APIKey는 계정에 발급된 API Key의 식별자와 만료 일시다. Secret Key는 포함하지 않는다.
type APIKey struct {
	AccessKey string          `json:"access_key"`
	ExpireAt  string          `json:"expire_at"`
	Raw       json.RawMessage `json:"-"`
}

// WalletStatuses는 자산·네트워크별 입출금 현황과 블록 상태를 조회한다.
func (client *Client) WalletStatuses(
	ctx context.Context,
	options ...trade.RequestOption,
) ([]WalletStatus, error) {
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/v1/status/wallet", nil, nil, rateLimitPrivateOther,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeItems(client, response, commonexchange.OperationRead, func(item *WalletStatus, raw []byte) {
		item.Raw = raw
	})
}

// APIKeys는 계정에 발급된 API Key 목록과 만료 일시를 조회한다.
// 키를 만들거나 지우지 않는 읽기 전용 조회이므로 읽기 권한으로 호출한다.
func (client *Client) APIKeys(
	ctx context.Context,
	options ...trade.RequestOption,
) ([]APIKey, error) {
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/v1/api_keys", nil, nil, rateLimitPrivateOther,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeItems(client, response, commonexchange.OperationRead, func(item *APIKey, raw []byte) {
		item.Raw = raw
	})
}
