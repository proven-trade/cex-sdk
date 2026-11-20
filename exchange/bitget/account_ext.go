package bitget

import (
	"context"
	"encoding/json"
	"net/http"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
)

// 이 파일의 계정 메서드는 data 필드를 json.RawMessage로 반환한다.
// 공식 문서가 JS 렌더링이라 응답 필드를 SDK 계약으로 고정하지 않는다.

// AccountInfo는 계정 정보를 조회한다. (GET /api/v3/account/info, Read 권한)
func (client *Client) AccountInfo(ctx context.Context, options ...trade.RequestOption) (json.RawMessage, error) {
	return client.readData(ctx, "/api/v3/account/info", nil, options...)
}

// AccountSettings는 계정 모드, 포지션 모드, 레버리지 설정을 조회한다.
// (GET /api/v3/account/settings, Read 권한)
func (client *Client) AccountSettings(ctx context.Context, options ...trade.RequestOption) (json.RawMessage, error) {
	return client.readData(ctx, "/api/v3/account/settings", nil, options...)
}

// SetLeverageRequest는 레버리지 설정 요청이다. Category가 USDT-FUTURES면 Symbol,
// SPOT 마진 대상이면 Coin을 지정한다. 격리 마진 레버리지는 API로 지원되지 않는다.
type SetLeverageRequest struct {
	Category     Category     `json:"category"`
	Symbol       string       `json:"symbol,omitempty"`
	Coin         string       `json:"coin,omitempty"`
	Leverage     string       `json:"leverage"`
	PositionSide PositionSide `json:"posSide,omitempty"`
}

// SetLeverage는 레버리지를 설정한다. (POST /api/v3/account/set-leverage, Trade 권한)
func (client *Client) SetLeverage(
	ctx context.Context,
	request SetLeverageRequest,
	options ...trade.RequestOption,
) (json.RawMessage, error) {
	if err := validateCategory(request.Category); err != nil {
		return nil, err
	}
	if err := validateOptionalSymbol(request.Symbol); err != nil {
		return nil, err
	}
	if err := validateOptionalText("coin", request.Coin); err != nil {
		return nil, err
	}
	if request.Symbol == "" && request.Coin == "" {
		return nil, validationError("symbol or coin is required")
	}
	if err := validatePositiveDecimal("leverage", request.Leverage); err != nil {
		return nil, err
	}
	if request.PositionSide != "" && !request.PositionSide.valid() {
		return nil, validationError("posSide must be long or short")
	}
	return client.mutateRaw(ctx, "/api/v3/account/set-leverage", request, credential.PermissionTrade, options...)
}

// SetHoldMode는 단방향/헤지 포지션 모드를 설정한다. holdMode 값은 Bitget 문서의 값을 그대로 전달한다
// (공식 문서가 JS 렌더링이라 열거값을 SDK에 고정하지 않는다).
// (POST /api/v3/account/set-hold-mode, Trade 권한)
func (client *Client) SetHoldMode(
	ctx context.Context,
	holdMode string,
	options ...trade.RequestOption,
) (json.RawMessage, error) {
	if err := requireText("holdMode", holdMode); err != nil {
		return nil, err
	}
	body := struct {
		HoldMode string `json:"holdMode"`
	}{HoldMode: holdMode}
	return client.mutateRaw(ctx, "/api/v3/account/set-hold-mode", body, credential.PermissionTrade, options...)
}

// AdjustAccountModeRequest는 통합계정의 Basic/Advanced 모드 전환 요청이다.
// TargetUID를 지정하면 마스터 계정이 서브계정의 모드를 전환한다.
type AdjustAccountModeRequest struct {
	Mode      string `json:"mode"`
	TargetUID string `json:"targetUid,omitempty"`
}

// AdjustAccountMode는 계정 마진 모드(Basic/Advanced)를 전환한다. Mode 값은 Bitget 문서의 값을 그대로 전달한다.
// (POST /api/v3/account/adjust-account-mode, Trade 권한)
func (client *Client) AdjustAccountMode(
	ctx context.Context,
	request AdjustAccountModeRequest,
	options ...trade.RequestOption,
) (json.RawMessage, error) {
	if err := requireText("mode", request.Mode); err != nil {
		return nil, err
	}
	if err := validateOptionalText("targetUid", request.TargetUID); err != nil {
		return nil, err
	}
	return client.mutateRaw(ctx, "/api/v3/account/adjust-account-mode", request, credential.PermissionTrade, options...)
}

// SetMarginRequest는 격리 마진 포지션의 증거금 조정 요청이다.
type SetMarginRequest struct {
	Category     Category     `json:"category"`
	Symbol       string       `json:"symbol"`
	PositionSide PositionSide `json:"posSide,omitempty"`
	// Operation은 Bitget 문서의 증거금 조정 값(예: add)을 그대로 전달한다.
	Operation string `json:"operation"`
	Amount    string `json:"amount"`
}

// SetMargin은 격리 마진 포지션의 증거금을 조정한다. (POST /api/v3/account/set-margin, Trade 권한)
func (client *Client) SetMargin(
	ctx context.Context,
	request SetMarginRequest,
	options ...trade.RequestOption,
) (json.RawMessage, error) {
	if request.Category != CategoryUSDTFutures {
		return nil, validationError("set margin currently supports only USDT-FUTURES")
	}
	if err := validateSymbol(request.Symbol); err != nil {
		return nil, err
	}
	if request.PositionSide != "" && !request.PositionSide.valid() {
		return nil, validationError("posSide must be long or short")
	}
	if err := requireText("operation", request.Operation); err != nil {
		return nil, err
	}
	if err := validatePositiveDecimal("amount", request.Amount); err != nil {
		return nil, err
	}
	return client.mutateRaw(ctx, "/api/v3/account/set-margin", request, credential.PermissionTrade, options...)
}

// FeeRate는 상품의 거래 수수료율을 조회한다. (GET /api/v3/account/fee-rate, Read 권한)
func (client *Client) FeeRate(
	ctx context.Context,
	category Category,
	symbol string,
	options ...trade.RequestOption,
) (json.RawMessage, error) {
	if err := validateCategory(category); err != nil {
		return nil, err
	}
	if err := validateSymbol(symbol); err != nil {
		return nil, err
	}
	return client.readData(ctx, "/api/v3/account/fee-rate", categorySymbolValues(category, symbol), options...)
}

// AllFeeRates는 category의 전체 상품 수수료율을 조회한다. Symbol을 지정하면 해당 상품만 반환한다.
// (GET /api/v3/account/all-fee-rate, Read 권한)
func (client *Client) AllFeeRates(
	ctx context.Context,
	category Category,
	symbol string,
	options ...trade.RequestOption,
) (json.RawMessage, error) {
	if err := validateCategory(category); err != nil {
		return nil, err
	}
	if err := validateOptionalSymbol(symbol); err != nil {
		return nil, err
	}
	return client.readData(ctx, "/api/v3/account/all-fee-rate", categorySymbolValues(category, symbol), options...)
}

// FinancialRecordsRequest는 재무(거래 손익·수수료·펀딩 등) 내역 조회 조건이다.
type FinancialRecordsRequest struct {
	Category Category
	Coin     string
	Type     string
	Page
}

// FinancialRecords는 계정 재무 내역(bills)을 cursor 기반으로 조회한다.
// (GET /api/v3/account/financial-records, Read 권한)
func (client *Client) FinancialRecords(
	ctx context.Context,
	request FinancialRecordsRequest,
	options ...trade.RequestOption,
) (json.RawMessage, error) {
	if err := validateCategory(request.Category); err != nil {
		return nil, err
	}
	if err := validateOptionalText("coin", request.Coin); err != nil {
		return nil, err
	}
	if err := validateOptionalText("type", request.Type); err != nil {
		return nil, err
	}
	values := queryValues("category", string(request.Category), "coin", request.Coin, "type", request.Type)
	if err := request.Page.apply(values); err != nil {
		return nil, err
	}
	return client.readData(ctx, "/api/v3/account/financial-records", values, options...)
}

// MaxOpenAvailableRequest는 최대 주문 가능 수량 조회 조건이다.
type MaxOpenAvailableRequest struct {
	Category  Category  `json:"category"`
	Symbol    string    `json:"symbol"`
	OrderType OrderType `json:"orderType"`
	Side      Side      `json:"side"`
	Price     string    `json:"price,omitempty"`
	Size      string    `json:"size,omitempty"`
}

// MaxOpenAvailable은 최대 주문 가능 수량을 조회한다. POST 본문을 쓰지만 상태를 바꾸지 않는 조회이며
// Read 권한을 사용한다. (POST /api/v3/account/max-open-available)
func (client *Client) MaxOpenAvailable(
	ctx context.Context,
	request MaxOpenAvailableRequest,
	options ...trade.RequestOption,
) (json.RawMessage, error) {
	if err := validateCategory(request.Category); err != nil {
		return nil, err
	}
	if err := validateSymbol(request.Symbol); err != nil {
		return nil, err
	}
	if request.OrderType != OrderTypeLimit && request.OrderType != OrderTypeMarket {
		return nil, validationError("orderType must be limit or market")
	}
	if request.Side != SideBuy && request.Side != SideSell {
		return nil, validationError("side must be buy or sell")
	}
	if err := validateOptionalDecimal("price", request.Price); err != nil {
		return nil, err
	}
	if err := validateOptionalDecimal("size", request.Size); err != nil {
		return nil, err
	}
	body, err := encodeBody(request)
	if err != nil {
		return nil, err
	}
	return client.signedRead(ctx, http.MethodPost, "/api/v3/account/max-open-available", nil, body, options...)
}

// AccountOpenInterestLimit은 계정의 미결제약정 한도를 조회한다.
// (GET /api/v3/account/open-interest-limit, Read 권한)
func (client *Client) AccountOpenInterestLimit(
	ctx context.Context,
	request OpenInterestRequest,
	options ...trade.RequestOption,
) (json.RawMessage, error) {
	if err := validateCategory(request.Category); err != nil {
		return nil, err
	}
	if err := validateOptionalSymbol(request.Symbol); err != nil {
		return nil, err
	}
	return client.readData(
		ctx, "/api/v3/account/open-interest-limit",
		categorySymbolValues(request.Category, request.Symbol), options...,
	)
}

// DeltaInfo는 계정 델타 정보를 조회한다. (GET /api/v3/account/delta-info, Read 권한)
func (client *Client) DeltaInfo(ctx context.Context, options ...trade.RequestOption) (json.RawMessage, error) {
	return client.readData(ctx, "/api/v3/account/delta-info", nil, options...)
}

// DeductInfo는 BGB 수수료 차감 설정을 조회한다. (GET /api/v3/account/deduct-info, Read 권한)
func (client *Client) DeductInfo(ctx context.Context, options ...trade.RequestOption) (json.RawMessage, error) {
	return client.readData(ctx, "/api/v3/account/deduct-info", nil, options...)
}

// SwitchDeduct는 BGB 수수료 차감 설정을 바꾼다. deduct 값은 Bitget 문서의 값을 그대로 전달한다.
// (POST /api/v3/account/switch-deduct, Trade 권한)
func (client *Client) SwitchDeduct(
	ctx context.Context,
	deduct string,
	options ...trade.RequestOption,
) (json.RawMessage, error) {
	if err := requireText("deduct", deduct); err != nil {
		return nil, err
	}
	body := struct {
		Deduct string `json:"deduct"`
	}{Deduct: deduct}
	return client.mutateRaw(ctx, "/api/v3/account/switch-deduct", body, credential.PermissionTrade, options...)
}

// SwitchStatus는 클래식 계정의 UTA 전환 상태를 조회한다.
// (GET /api/v3/account/switch-status, Read 권한)
func (client *Client) SwitchStatus(ctx context.Context, options ...trade.RequestOption) (json.RawMessage, error) {
	return client.readData(ctx, "/api/v3/account/switch-status", nil, options...)
}

// SwitchAccount는 클래식 계정을 UTA로 전환한다. 되돌릴 수 없는 계정 구조 변경이므로 Transfer 권한을 요구한다.
// (POST /api/v3/account/switch, Transfer 권한)
func (client *Client) SwitchAccount(ctx context.Context, options ...trade.RequestOption) (json.RawMessage, error) {
	return client.mutateRaw(ctx, "/api/v3/account/switch", nil, credential.PermissionTransfer, options...)
}

// ConvertRecordsRequest는 전환(convert) 내역 조회 조건이다.
type ConvertRecordsRequest struct {
	FromCoin string
	ToCoin   string
	Page
}

// ConvertRecords는 전환 내역을 조회한다. (GET /api/v3/account/convert-records, Read 권한)
func (client *Client) ConvertRecords(
	ctx context.Context,
	request ConvertRecordsRequest,
	options ...trade.RequestOption,
) (json.RawMessage, error) {
	if err := validateOptionalText("fromCoin", request.FromCoin); err != nil {
		return nil, err
	}
	if err := validateOptionalText("toCoin", request.ToCoin); err != nil {
		return nil, err
	}
	values := queryValues("fromCoin", request.FromCoin, "toCoin", request.ToCoin)
	if err := request.Page.apply(values); err != nil {
		return nil, err
	}
	return client.readData(ctx, "/api/v3/account/convert-records", values, options...)
}

// RepayableCoins는 상환 가능한 부채 코인을 조회한다. (GET /api/v3/account/repayable-coins, Read 권한)
func (client *Client) RepayableCoins(ctx context.Context, options ...trade.RequestOption) (json.RawMessage, error) {
	return client.readData(ctx, "/api/v3/account/repayable-coins", nil, options...)
}

// PaymentCoins는 부채 상환에 사용할 수 있는 코인을 조회한다. (GET /api/v3/account/payment-coins, Read 권한)
func (client *Client) PaymentCoins(ctx context.Context, options ...trade.RequestOption) (json.RawMessage, error) {
	return client.readData(ctx, "/api/v3/account/payment-coins", nil, options...)
}

// RepayRequest는 부채 상환 요청이다.
type RepayRequest struct {
	RepayableCoins []string `json:"repayableCoinList"`
	PaymentCoins   []string `json:"paymentCoinList"`
}

// Repay는 지정한 코인으로 부채를 상환한다. (POST /api/v3/account/repay, Trade 권한)
func (client *Client) Repay(
	ctx context.Context,
	request RepayRequest,
	options ...trade.RequestOption,
) (json.RawMessage, error) {
	if len(request.RepayableCoins) == 0 || len(request.PaymentCoins) == 0 {
		return nil, validationError("repayableCoinList and paymentCoinList are required")
	}
	for _, coin := range append(append([]string(nil), request.RepayableCoins...), request.PaymentCoins...) {
		if err := requireText("coin", coin); err != nil {
			return nil, err
		}
	}
	return client.mutateRaw(ctx, "/api/v3/account/repay", request, credential.PermissionTrade, options...)
}

// HistoryPositions는 포지션 이력을 cursor 기반으로 조회한다.
// (GET /api/v3/position/history-position, Read 권한)
func (client *Client) HistoryPositions(
	ctx context.Context,
	category Category,
	symbol string,
	page Page,
	options ...trade.RequestOption,
) (json.RawMessage, error) {
	if err := validateCategory(category); err != nil {
		return nil, err
	}
	if err := validateOptionalSymbol(symbol); err != nil {
		return nil, err
	}
	values := categorySymbolValues(category, symbol)
	if err := page.apply(values); err != nil {
		return nil, err
	}
	return client.readData(ctx, "/api/v3/position/history-position", values, options...)
}

// ADLRank는 포지션 ADL 순위를 조회한다. (GET /api/v3/position/adlRank, Read 권한)
func (client *Client) ADLRank(ctx context.Context, options ...trade.RequestOption) (json.RawMessage, error) {
	return client.readData(ctx, "/api/v3/position/adlRank", nil, options...)
}
