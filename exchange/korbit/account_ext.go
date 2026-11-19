package korbit

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

// TradingFeePolicyRequest는 거래쌍별 수수료율 조회 조건이다. Symbols를 비우면 전체 거래쌍을 조회한다.
type TradingFeePolicyRequest struct {
	AccountSeq int
	Symbols    []string
}

// TradingFeePolicy는 거래쌍별 수수료 부과 통화와 수수료율이다. 수수료율은 소수 문자열이다.
type TradingFeePolicy struct {
	Symbol          string          `json:"symbol"`
	BuyFeeCurrency  string          `json:"buyFeeCurrency"`
	SellFeeCurrency string          `json:"sellFeeCurrency"`
	MaxFeeRate      string          `json:"maxFeeRate"`
	TakerFeeRate    string          `json:"takerFeeRate"`
	MakerFeeRate    string          `json:"makerFeeRate"`
	Raw             json.RawMessage `json:"-"`
}

// APIKeyType은 API Key의 서명 방식이다.
type APIKeyType string

const (
	APIKeyTypeHMACSHA256 APIKeyType = "hmac-sha256"
	APIKeyTypeED25519    APIKeyType = "ed25519"
)

// APIKeyStatus는 API Key의 활성 상태다.
type APIKeyStatus string

const (
	APIKeyStatusActivated   APIKeyStatus = "activated"
	APIKeyStatusDeactivated APIKeyStatus = "deactivated"
)

// APIKeyPermission은 거래소가 API Key에 부여하는 권한 이름이다.
// SDK의 credential.Permission과 별개이며 새 값이 추가돼도 문자열로 보존한다.
type APIKeyPermission string

const (
	APIKeyPermissionReadBalances          APIKeyPermission = "readBalances"
	APIKeyPermissionReadOrders            APIKeyPermission = "readOrders"
	APIKeyPermissionWriteOrders           APIKeyPermission = "writeOrders"
	APIKeyPermissionReadDeposits          APIKeyPermission = "readDeposits"
	APIKeyPermissionWriteDeposits         APIKeyPermission = "writeDeposits"
	APIKeyPermissionReadWithdrawals       APIKeyPermission = "readWithdrawals"
	APIKeyPermissionWriteWithdrawals      APIKeyPermission = "writeWithdrawals"
	APIKeyPermissionReadAccountTransfers  APIKeyPermission = "readAccountTransfers"
	APIKeyPermissionWriteAccountTransfers APIKeyPermission = "writeAccountTransfers"
)

// APIKeyInfo는 현재 요청에 쓴 API Key의 서명 방식, 권한, 허용 IP와 접근 가능한 계정이다.
// Whitelist는 쉼표로 구분한 허용 IP 목록이고, Expiration·CreatedAt은 Unix millisecond다.
type APIKeyInfo struct {
	APIKey             string             `json:"apiKey"`
	UserUUID           string             `json:"userUuid"`
	Type               APIKeyType         `json:"type"`
	PublicKey          string             `json:"publicKey"`
	Permissions        []APIKeyPermission `json:"permissions"`
	Whitelist          string             `json:"whitelist"`
	Expiration         int64              `json:"expiration"`
	Status             APIKeyStatus       `json:"status"`
	Label              string             `json:"label"`
	AllowedAccountSeqs []int              `json:"allowedAccountSeqs"`
	CreatedAt          int64              `json:"createdAt"`
	Raw                json.RawMessage    `json:"-"`
}

// Account는 사용자의 계정 한 건이다. AccountSeq 1은 메인 계정이고 나머지는 하위 계정이다.
type Account struct {
	AccountSeq int             `json:"accountSeq"`
	Name       string          `json:"name"`
	Raw        json.RawMessage `json:"-"`
}

func (request TradingFeePolicyRequest) validate() error {
	if err := validateAccountSeq(request.AccountSeq); err != nil {
		return err
	}
	return validateSymbols(request.Symbols)
}

func (request TradingFeePolicyRequest) values() url.Values {
	values := make(url.Values)
	setAccountSeq(values, request.AccountSeq)
	if len(request.Symbols) > 0 {
		values.Set("symbol", strings.Join(request.Symbols, ","))
	}
	return values
}

// TradingFeePolicy는 계정에 적용되는 거래쌍별 수수료율을 조회한다.
func (client *Client) TradingFeePolicy(
	ctx context.Context,
	request TradingFeePolicyRequest,
	options ...trade.RequestOption,
) ([]TradingFeePolicy, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/v2/tradingFeePolicy", request.values(), rateGroupPrivate,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeItems[TradingFeePolicy](client, response, func(item *TradingFeePolicy, raw []byte) { item.Raw = raw })
}

// CurrentKeyInfo는 요청에 사용한 API Key의 권한, 허용 IP, 접근 가능한 계정을 조회한다.
func (client *Client) CurrentKeyInfo(
	ctx context.Context,
	options ...trade.RequestOption,
) (APIKeyInfo, error) {
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/v2/currentKeyInfo", nil, rateGroupPrivate,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return APIKeyInfo{}, err
	}
	var result APIKeyInfo
	raw, err := client.decodeResponse(response, commonexchange.OperationRead, &result)
	if err != nil {
		return APIKeyInfo{}, err
	}
	if result.APIKey == "" {
		return APIKeyInfo{}, client.decodeBodyError(
			response, commonexchange.OperationRead, errors.New("Korbit API key info has no API key"),
		)
	}
	result.Raw = raw
	return result, nil
}

// Accounts는 사용자의 메인·하위 계정을 accountSeq 오름차순으로 조회한다.
func (client *Client) Accounts(
	ctx context.Context,
	options ...trade.RequestOption,
) ([]Account, error) {
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/v2/accounts", nil, rateGroupPrivate,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeItems[Account](client, response, func(item *Account, raw []byte) { item.Raw = raw })
}
