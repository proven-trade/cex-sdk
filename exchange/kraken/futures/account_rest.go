package futures

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

var errMissingTradingEnabled = errors.New("Kraken Futures trading-enabled response is missing tradingEnabled")

// spotWallet은 transfer에서 Kraken Spot 지갑을 가리키는 계정 이름이다.
const spotWallet = "spot"

// UnwindQueueEntry는 열린 포지션의 언와인드 큐 백분위(20, 40, 80, 100)다.
type UnwindQueueEntry struct {
	Symbol     string          `json:"symbol"`
	Percentile int             `json:"percentile"`
	Raw        json.RawMessage `json:"-"`
}

// Notification은 플랫폼 공지 한 건이다.
type Notification struct {
	Type                    string          `json:"type"`
	Priority                string          `json:"priority"`
	EffectiveTime           string          `json:"effectiveTime"`
	Note                    string          `json:"note"`
	ExpectedDowntimeMinutes int             `json:"expectedDowntimeMinutes"`
	Raw                     json.RawMessage `json:"-"`
}

// APIKeyPermissions는 API key의 접근 수준(NO_ACCESS, READ_ONLY, FULL_ACCESS)이다.
type APIKeyPermissions struct {
	General  string `json:"general"`
	Transfer string `json:"transfer"`
}

// APIKeyCheck는 서명된 요청으로 검증한 API key 상세다.
type APIKeyCheck struct {
	APIKey            string            `json:"apiKey"`
	AccountUID        string            `json:"accountUid"`
	IIBAN             string            `json:"iiban"`
	CreatedAt         string            `json:"createdAt"`
	Permissions       APIKeyPermissions `json:"permissions"`
	AllowedCIDRBlock  string            `json:"allowedCidrBlock"`
	AllowedCIDRBlocks []string          `json:"allowedCidrBlocks"`
	Raw               json.RawMessage   `json:"-"`
}

// TransferRequest는 Futures 지갑 사이 또는 Kraken Spot 지갑과의 이체다.
// FromAccount/ToAccount에 "spot"을 쓰면 Spot 이체이며, withdraw 권한 key가 필요하고 비용이 100이다.
type TransferRequest struct {
	FromAccount string
	ToAccount   string
	Unit        string
	Amount      string
}

// SubaccountTransferRequest는 현재 계정과 서브계정 사이 또는 지갑 사이 이체다.
type SubaccountTransferRequest struct {
	FromUser    string
	ToUser      string
	FromAccount string
	ToAccount   string
	Unit        string
	Amount      string
}

// HoldingAccountBalance는 서브계정 holding 지갑 잔고다.
type HoldingAccountBalance struct {
	Currency string  `json:"currency"`
	Amount   Decimal `json:"amount"`
}

// FuturesAccountBalance는 서브계정 단일 담보 margin 계정의 가용 증거금이다.
type FuturesAccountBalance struct {
	Name            string  `json:"name"`
	AvailableMargin Decimal `json:"availableMargin"`
}

// Subaccount는 서브계정 정보와 잔고다. 서버가 추가하는 multi-collateral 필드는 Raw로 제공한다.
type Subaccount struct {
	AccountUID      string                  `json:"accountUid"`
	Email           string                  `json:"email"`
	FullName        string                  `json:"fullName"`
	HoldingAccounts []HoldingAccountBalance `json:"holdingAccounts"`
	FuturesAccounts []FuturesAccountBalance `json:"futuresAccounts"`
	FlexAccount     json.RawMessage         `json:"flexAccount"`
	Raw             json.RawMessage         `json:"-"`
}

// Subaccounts는 마스터 계정 UID와 서브계정 목록이다.
type Subaccounts struct {
	MasterAccountUID string          `json:"masterAccountUid"`
	Subaccounts      []Subaccount    `json:"subaccounts"`
	Raw              json.RawMessage `json:"-"`
}

// PortfolioMarginParameters는 포트폴리오 마진 계산 파라미터다. 필드 구조가 넓어 원문 JSON도 함께 제공한다.
type PortfolioMarginParameters struct {
	ExtremePriceShockMultiplier  Decimal         `json:"extremePriceShockMultiplier"`
	VolShockMultiplicationFactor Decimal         `json:"volShockMultiplicationFactor"`
	VolShockExponentFactor       Decimal         `json:"volShockExponentFactor"`
	OptionExpiryTimeShockHours   Decimal         `json:"optionExpiryTimeShockHours"`
	OptionsInitialMarginFactor   Decimal         `json:"optionsInitialMarginFactor"`
	Raw                          json.RawMessage `json:"-"`
}

// PortfolioPosition은 포트폴리오 시뮬레이션 입력 포지션이다.
type PortfolioPosition struct {
	Instrument string  `json:"instrument"`
	Size       Decimal `json:"size"`
	EntryPrice Decimal `json:"entryPrice"`
}

// PortfolioSimulation은 시뮬레이션된 마진·PnL·그릭스다. 상세 구조는 Raw로 제공한다.
type PortfolioSimulation struct {
	MaintenanceMargin Decimal         `json:"maintenanceMargin"`
	InitialMargin     Decimal         `json:"initialMargin"`
	PNL               Decimal         `json:"pnl"`
	Raw               json.RawMessage `json:"-"`
}

// UnwindQueue는 열린 포지션의 언와인드 큐 백분위를 조회한다. 비용은 200이다.
// Required permission: credential.PermissionRead.
func (client *Client) UnwindQueue(
	ctx context.Context,
	options ...trade.RequestOption,
) ([]UnwindQueueEntry, error) {
	var envelope struct {
		Queue []json.RawMessage `json:"queue"`
	}
	response, err := client.privateRead(
		ctx, http.MethodGet, derivativesPrefix+"unwindqueue", nil, 200, &envelope, options...,
	)
	if err != nil {
		return nil, err
	}
	queue, err := decodeRawItems(envelope.Queue, func(item *UnwindQueueEntry, raw []byte) { item.Raw = raw })
	if err != nil {
		return nil, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	return queue, nil
}

// Notifications는 플랫폼 공지를 조회한다.
// Required permission: credential.PermissionRead.
func (client *Client) Notifications(
	ctx context.Context,
	options ...trade.RequestOption,
) ([]Notification, error) {
	var envelope struct {
		Notifications []json.RawMessage `json:"notifications"`
	}
	response, err := client.privateRead(
		ctx, http.MethodGet, derivativesPrefix+"notifications", nil, defaultReadCost, &envelope, options...,
	)
	if err != nil {
		return nil, err
	}
	notifications, err := decodeRawItems(
		envelope.Notifications, func(item *Notification, raw []byte) { item.Raw = raw },
	)
	if err != nil {
		return nil, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	return notifications, nil
}

// CheckAPIKey는 서명된 요청으로 API key 접근과 key 상세를 검증한다(/api/auth/v1/api-keys/v3/check).
// Required permission: credential.PermissionRead.
func (client *Client) CheckAPIKey(
	ctx context.Context,
	options ...trade.RequestOption,
) (APIKeyCheck, error) {
	var check APIKeyCheck
	response, err := client.privateRead(
		ctx, http.MethodGet, authPrefix+"/api-keys/v3/check", nil, 1, &check, options...,
	)
	if err != nil {
		return APIKeyCheck{}, err
	}
	check.Raw = cloneBytes(response.Body)
	return check, nil
}

// Transfer는 Futures 지갑 사이 또는 Kraken Spot 지갑과 자산을 이체한다.
// Spot 이체는 비용 100이며 credential.PermissionWithdraw가 필요하고, 그 외에는 credential.PermissionTransfer가 필요하다.
func (client *Client) Transfer(
	ctx context.Context,
	request TransferRequest,
	options ...trade.RequestOption,
) (Ack, error) {
	if err := validateTransferFields(request.FromAccount, request.ToAccount, request.Unit, request.Amount); err != nil {
		return Ack{}, err
	}
	permission, cost := credential.PermissionTransfer, 10
	if strings.EqualFold(request.FromAccount, spotWallet) || strings.EqualFold(request.ToAccount, spotWallet) {
		permission, cost = credential.PermissionWithdraw, 100
	}
	values := url.Values{
		"fromAccount": []string{request.FromAccount}, "toAccount": []string{request.ToAccount},
		"unit": []string{request.Unit}, "amount": []string{request.Amount},
	}
	return client.privateAck(ctx, http.MethodPost, derivativesPrefix+"transfer", values, cost, permission, options...)
}

// SubaccountTransfer는 현재 계정과 서브계정 사이 또는 지갑 사이 자산을 이체한다.
// Required permission: credential.PermissionTransfer.
func (client *Client) SubaccountTransfer(
	ctx context.Context,
	request SubaccountTransferRequest,
	options ...trade.RequestOption,
) (Ack, error) {
	if err := validateTransferFields(request.FromAccount, request.ToAccount, request.Unit, request.Amount); err != nil {
		return Ack{}, err
	}
	if err := validateIdentifier("fromUser", request.FromUser); err != nil {
		return Ack{}, err
	}
	if err := validateIdentifier("toUser", request.ToUser); err != nil {
		return Ack{}, err
	}
	values := url.Values{
		"fromUser": []string{request.FromUser}, "toUser": []string{request.ToUser},
		"fromAccount": []string{request.FromAccount}, "toAccount": []string{request.ToAccount},
		"unit": []string{request.Unit}, "amount": []string{request.Amount},
	}
	return client.privateAck(
		ctx, http.MethodPost, derivativesPrefix+"transfer/subaccount", values, 10,
		credential.PermissionTransfer, options...,
	)
}

// ListSubaccounts는 서브계정 목록, UID와 잔고를 조회한다.
// Required permission: credential.PermissionRead.
func (client *Client) ListSubaccounts(
	ctx context.Context,
	options ...trade.RequestOption,
) (Subaccounts, error) {
	var envelope struct {
		MasterAccountUID string            `json:"masterAccountUid"`
		Subaccounts      []json.RawMessage `json:"subaccounts"`
	}
	response, err := client.privateRead(
		ctx, http.MethodGet, derivativesPrefix+"subaccounts", nil, defaultReadCost, &envelope, options...,
	)
	if err != nil {
		return Subaccounts{}, err
	}
	items, err := decodeRawItems(envelope.Subaccounts, func(item *Subaccount, raw []byte) { item.Raw = raw })
	if err != nil {
		return Subaccounts{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	return Subaccounts{
		MasterAccountUID: envelope.MasterAccountUID, Subaccounts: items, Raw: cloneBytes(response.Body),
	}, nil
}

// SubaccountTradingEnabled는 서브계정의 거래 가능 여부를 조회한다. 비용은 2다.
// Required permission: credential.PermissionTransfer (공식 문서상 읽기 전용이 아닌 일반 API key).
func (client *Client) SubaccountTradingEnabled(
	ctx context.Context,
	subaccountUID string,
	options ...trade.RequestOption,
) (bool, error) {
	if err := validateIdentifier("subaccount UID", subaccountUID); err != nil {
		return false, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, subaccountTradingPath(subaccountUID), nil, 2,
		credential.PermissionTransfer, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return false, err
	}
	return client.decodeTradingEnabled(response, commonexchange.OperationRead)
}

// SetSubaccountTradingEnabled는 서브계정의 거래 가능 여부를 변경하고 저장된 값을 반환한다.
// Required permission: credential.PermissionTransfer.
func (client *Client) SetSubaccountTradingEnabled(
	ctx context.Context,
	subaccountUID string,
	enabled bool,
	options ...trade.RequestOption,
) (bool, error) {
	if err := validateIdentifier("subaccount UID", subaccountUID); err != nil {
		return false, err
	}
	value := "false"
	if enabled {
		value = "true"
	}
	response, err := client.executePrivate(
		ctx, http.MethodPut, subaccountTradingPath(subaccountUID),
		url.Values{"tradingEnabled": []string{value}}, 2,
		credential.PermissionTransfer, commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return false, err
	}
	return client.decodeTradingEnabled(response, commonexchange.OperationMutation)
}

// PortfolioMarginingParameters는 포트폴리오 마진 계산 파라미터를 조회한다.
// 공식 문서상 현재 Kraken pre-prod 환경에서만 제공되므로 Config.BaseURL을 해당 환경으로 지정해야 한다.
// Required permission: credential.PermissionRead.
func (client *Client) PortfolioMarginingParameters(
	ctx context.Context,
	options ...trade.RequestOption,
) (PortfolioMarginParameters, error) {
	var parameters PortfolioMarginParameters
	response, err := client.privateRead(
		ctx, http.MethodGet, derivativesPrefix+"portfolio-margining/parameters", nil, defaultReadCost,
		&parameters, options...,
	)
	if err != nil {
		return PortfolioMarginParameters{}, err
	}
	parameters.Raw = cloneBytes(response.Body)
	return parameters, nil
}

// SimulatePortfolio는 포지션 목록의 포트폴리오 마진, PnL과 그릭스를 계산한다(최대 500개 포지션).
// 공식 문서상 현재 Kraken pre-prod 환경에서만 제공되므로 Config.BaseURL을 해당 환경으로 지정해야 한다.
// Required permission: credential.PermissionRead.
func (client *Client) SimulatePortfolio(
	ctx context.Context,
	positions []PortfolioPosition,
	options ...trade.RequestOption,
) (PortfolioSimulation, error) {
	if len(positions) == 0 || len(positions) > 500 {
		return PortfolioSimulation{}, validationError("portfolio simulation requires between 1 and 500 positions")
	}
	for _, position := range positions {
		if err := validateSymbol(position.Instrument); err != nil {
			return PortfolioSimulation{}, err
		}
		if position.Size == "" || position.EntryPrice == "" {
			return PortfolioSimulation{}, validationError("portfolio position size and entryPrice are required")
		}
	}
	encoded, err := json.Marshal(struct {
		Positions []portfolioWirePosition `json:"positions"`
	}{Positions: portfolioWirePositions(positions)})
	if err != nil {
		return PortfolioSimulation{}, validationError("encode portfolio simulation: %v", err)
	}
	var simulation PortfolioSimulation
	response, err := client.privateRead(
		ctx, http.MethodPost, derivativesPrefix+"portfolio-margining/simulate",
		url.Values{"json": []string{string(encoded)}}, defaultReadCost, &simulation, options...,
	)
	if err != nil {
		return PortfolioSimulation{}, err
	}
	simulation.Raw = cloneBytes(response.Body)
	return simulation, nil
}

type portfolioWirePosition struct {
	Instrument string      `json:"instrument"`
	Size       json.Number `json:"size"`
	EntryPrice json.Number `json:"entryPrice"`
}

func portfolioWirePositions(positions []PortfolioPosition) []portfolioWirePosition {
	wire := make([]portfolioWirePosition, len(positions))
	for index, position := range positions {
		wire[index] = portfolioWirePosition{
			Instrument: position.Instrument, Size: json.Number(position.Size),
			EntryPrice: json.Number(position.EntryPrice),
		}
	}
	return wire
}

func (client *Client) decodeTradingEnabled(
	response commonexchange.Response,
	operation commonexchange.OperationKind,
) (bool, error) {
	var envelope struct {
		TradingEnabled *bool `json:"tradingEnabled"`
	}
	if err := client.decodeSuccess(response, operation, &envelope); err != nil {
		return false, err
	}
	if envelope.TradingEnabled == nil {
		return false, client.decodeBodyError(response, operation, errMissingTradingEnabled)
	}
	return *envelope.TradingEnabled, nil
}

func subaccountTradingPath(subaccountUID string) string {
	return derivativesPrefix + "subaccount/" + url.PathEscape(subaccountUID) + "/trading-enabled"
}

func validateTransferFields(fromAccount, toAccount, unit, amount string) error {
	for _, account := range []struct{ name, value string }{
		{"fromAccount", fromAccount}, {"toAccount", toAccount},
	} {
		if !pathSegmentPattern.MatchString(account.value) {
			return validationError("%s has an invalid format", account.name)
		}
	}
	if err := validateIdentifier("unit", unit); err != nil {
		return err
	}
	return validatePositiveDecimal("amount", amount)
}
