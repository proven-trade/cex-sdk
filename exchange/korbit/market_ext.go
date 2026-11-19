package korbit

import (
	"context"
	"encoding/json"

	trade "github.com/proven-trade/cex-sdk"
)

// CurrencyStatus는 자산 또는 네트워크의 입금·출금 가능 여부다.
type CurrencyStatus string

const (
	CurrencyStatusLaunched CurrencyStatus = "launched"
	CurrencyStatusStopped  CurrencyStatus = "stopped"
)

// CurrencyNetwork는 자산이 지원하는 블록체인 네트워크별 입출금 규칙이다.
type CurrencyNetwork struct {
	Name                string         `json:"name"`
	FullName            string         `json:"fullName"`
	WithdrawalStatus    CurrencyStatus `json:"withdrawalStatus"`
	DepositStatus       CurrencyStatus `json:"depositStatus"`
	ConfirmationCount   int            `json:"confirmationCount"`
	WithdrawalTxFee     string         `json:"withdrawalTxFee"`
	WithdrawalMinAmount string         `json:"withdrawalMinAmount"`
	WithdrawalPrecision int            `json:"withdrawalPrecision"`
	HasSecondaryAddress bool           `json:"hasSecondaryAddr"`
	ContractAddress     string         `json:"contractAddress"`
	AddressExplorerURL  string         `json:"addressExplorerUrl"`
}

// Currency는 자산별 입출금 상태와 네트워크 규칙이다.
// 최상위 WithdrawalStatus·DepositStatus·ConfirmationCount·WithdrawalTxFee는 공식 문서에서 deprecated이므로
// 가상자산은 NetworkList의 같은 이름 필드를 사용해야 한다. 네트워크가 없는 원화(krw)는 최상위 값만 제공한다.
type Currency struct {
	Name                          string            `json:"name"`
	FullName                      string            `json:"fullName"`
	WithdrawalStatus              CurrencyStatus    `json:"withdrawalStatus"`
	DepositStatus                 CurrencyStatus    `json:"depositStatus"`
	ConfirmationCount             string            `json:"confirmationCount"`
	WithdrawalTxFee               string            `json:"withdrawalTxFee"`
	WithdrawalMinAmount           string            `json:"withdrawalMinAmount"`
	WithdrawalMaxAmountPerRequest string            `json:"withdrawalMaxAmountPerRequest"`
	DefaultNetwork                string            `json:"defaultNetwork"`
	NetworkList                   []CurrencyNetwork `json:"networkList"`
	Raw                           json.RawMessage   `json:"-"`
}

// Notice는 거래소 공지사항 한 건이다.
type Notice struct {
	Title     string          `json:"title"`
	CreatedAt int64           `json:"createdAt"`
	UpdatedAt *int64          `json:"updatedAt"`
	URL       string          `json:"url"`
	Raw       json.RawMessage `json:"-"`
}

// MarketAlertDetail은 거래쌍에 걸린 시장경보 한 건이다.
type MarketAlertDetail struct {
	Type        string `json:"type"`
	TypeKorean  string `json:"typeKorean"`
	Level       string `json:"level"`
	LevelKorean string `json:"levelKorean"`
	StartAt     *int64 `json:"startAt"`
	EndAt       *int64 `json:"endAt"`
	Value       string `json:"value"`
}

// MarketAlert는 시장경보가 걸린 거래쌍과 경보 목록이다.
type MarketAlert struct {
	Symbol string              `json:"symbol"`
	Alerts []MarketAlertDetail `json:"alerts"`
	Raw    json.RawMessage     `json:"-"`
}

// Currencies는 자산별 입출금 상태, 네트워크별 수수료와 최소 출금 수량을 조회한다.
func (client *Client) Currencies(
	ctx context.Context,
	options ...trade.RequestOption,
) ([]Currency, error) {
	response, err := client.executePublic(ctx, "/v2/currencies", nil, options...)
	if err != nil {
		return nil, err
	}
	return decodeItems[Currency](client, response, func(item *Currency, raw []byte) { item.Raw = raw })
}

// Notices는 일반 공지와 Open API 공지를 합친 최신 공지사항 최대 20건을 조회한다.
func (client *Client) Notices(
	ctx context.Context,
	options ...trade.RequestOption,
) ([]Notice, error) {
	response, err := client.executePublic(ctx, "/v2/notices", nil, options...)
	if err != nil {
		return nil, err
	}
	return decodeItems[Notice](client, response, func(item *Notice, raw []byte) { item.Raw = raw })
}

// MarketAlerts는 시장경보가 현재 걸려 있는 거래쌍만 조회한다. 경보가 없으면 빈 목록이다.
func (client *Client) MarketAlerts(
	ctx context.Context,
	options ...trade.RequestOption,
) ([]MarketAlert, error) {
	response, err := client.executePublic(ctx, "/v2/marketAlerts", nil, options...)
	if err != nil {
		return nil, err
	}
	return decodeItems[MarketAlert](client, response, func(item *MarketAlert, raw []byte) { item.Raw = raw })
}
