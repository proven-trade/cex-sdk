package upbit

import (
	"context"
	"encoding/json"
	"net/http"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

// WalletState는 통화의 입출금 서비스 상태다.
type WalletState string

const (
	WalletStateWorking      WalletState = "working"
	WalletStateWithdrawOnly WalletState = "withdraw_only"
	WalletStateDepositOnly  WalletState = "deposit_only"
	WalletStatePaused       WalletState = "paused"
	WalletStateUnsupported  WalletState = "unsupported"
)

// WalletStatus는 통화와 네트워크별 입출금 서비스 상태다.
// 블록 정보는 지갑 또는 거래소 상태에 따라 비어 있을 수 있다.
type WalletStatus struct {
	Currency            string          `json:"currency"`
	WalletState         WalletState     `json:"wallet_state"`
	BlockState          string          `json:"block_state"`
	BlockHeight         int64           `json:"block_height"`
	BlockUpdatedAt      string          `json:"block_updated_at"`
	BlockElapsedMinutes int             `json:"block_elapsed_minutes"`
	NetType             string          `json:"net_type"`
	NetworkName         string          `json:"network_name"`
	Raw                 json.RawMessage `json:"-"`
}

// APIKey는 계정에 발급된 API Key의 Access Key와 만료 일시(KST)다.
type APIKey struct {
	AccessKey string          `json:"access_key"`
	ExpireAt  string          `json:"expire_at"`
	Raw       json.RawMessage `json:"-"`
}

// WalletStatuses는 전체 통화의 입출금 서비스 상태를 조회한다.
// 실시간 상태를 보장하지 않으며 반영이 수 분 지연될 수 있으므로 참고용으로만 써야 한다.
// 인증은 필요하지만 별도 API Key 권한은 필요하지 않다.
func (client *Client) WalletStatuses(ctx context.Context, options ...trade.RequestOption) ([]WalletStatus, error) {
	response, err := client.executeSigned(
		ctx, http.MethodGet, "/v1/status/wallet", nil, nil, defaultRate,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeItems(client, response, commonexchange.OperationRead, func(item *WalletStatus, raw []byte) {
		item.Raw = raw
	})
}

// APIKeys는 계정의 모든 API Key 목록과 각 Key의 만료 일시를 조회한다.
// 인증은 필요하지만 별도 API Key 권한은 필요하지 않다.
func (client *Client) APIKeys(ctx context.Context, options ...trade.RequestOption) ([]APIKey, error) {
	response, err := client.executeSigned(
		ctx, http.MethodGet, "/v1/api_keys", nil, nil, defaultRate,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeItems(client, response, commonexchange.OperationRead, func(item *APIKey, raw []byte) {
		item.Raw = raw
	})
}
