package futures

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

var bizNoPattern = regexp.MustCompile(`^[0-9A-Za-z_-]{1,36}$`)

// PositionMode는 계정 전체 Futures 포지션 모드다. 거래소는 조회에는 정수, 전환에는 문자열로 응답한다.
type PositionMode string

const (
	PositionModeOneWay PositionMode = "0"
	PositionModeHedge  PositionMode = "1"
)

// UnmarshalJSON은 숫자와 문자열 형식의 포지션 모드를 같은 값으로 해석한다.
func (mode *PositionMode) UnmarshalJSON(data []byte) error {
	text := string(bytes.Trim(bytes.TrimSpace(data), `"`))
	if text != string(PositionModeOneWay) && text != string(PositionModeHedge) {
		return validationError("unsupported Futures position mode %q", text)
	}
	*mode = PositionMode(text)
	return nil
}

// PositionDetail은 v2 포지션 조회와 증거금 추가 응답의 상세 포지션이다.
// 증거금 추가 응답에는 일부 필드가 없으므로 비어 있을 수 있다.
type PositionDetail struct {
	ID                   string          `json:"id"`
	Symbol               string          `json:"symbol"`
	AutoDeposit          bool            `json:"autoDeposit"`
	CrossMode            bool            `json:"crossMode"`
	MarginMode           MarginMode      `json:"marginMode"`
	PositionSide         PositionSide    `json:"positionSide"`
	Leverage             OptionalDecimal `json:"leverage"`
	RealLeverage         OptionalDecimal `json:"realLeverage"`
	RiskLimit            int64           `json:"riskLimit"`
	RiskLimitLevel       int64           `json:"riskLimitLevel"`
	MaintenanceMarginReq OptionalDecimal `json:"maintMarginReq"`
	DeleveragePercentage OptionalDecimal `json:"delevPercentage"`
	OpeningTimestamp     int64           `json:"openingTimestamp"`
	CurrentTimestamp     int64           `json:"currentTimestamp"`
	CurrentQuantity      int64           `json:"currentQty"`
	CurrentCost          OptionalDecimal `json:"currentCost"`
	CurrentCommission    OptionalDecimal `json:"currentComm"`
	UnrealisedCost       OptionalDecimal `json:"unrealisedCost"`
	RealisedGrossCost    OptionalDecimal `json:"realisedGrossCost"`
	RealisedCost         OptionalDecimal `json:"realisedCost"`
	Open                 bool            `json:"isOpen"`
	Inverse              bool            `json:"isInverse"`
	MarkPrice            OptionalDecimal `json:"markPrice"`
	MarkValue            OptionalDecimal `json:"markValue"`
	PositionCost         OptionalDecimal `json:"posCost"`
	PositionCross        OptionalDecimal `json:"posCross"`
	PositionInit         OptionalDecimal `json:"posInit"`
	PositionCommission   OptionalDecimal `json:"posComm"`
	PositionLoss         OptionalDecimal `json:"posLoss"`
	PositionMargin       OptionalDecimal `json:"posMargin"`
	PositionMaintenance  OptionalDecimal `json:"posMaint"`
	MaintenanceMargin    OptionalDecimal `json:"maintMargin"`
	RealisedGrossPNL     OptionalDecimal `json:"realisedGrossPnl"`
	RealisedPNL          OptionalDecimal `json:"realisedPnl"`
	UnrealisedPNL        OptionalDecimal `json:"unrealisedPnl"`
	UnrealisedPNLPercent OptionalDecimal `json:"unrealisedPnlPcnt"`
	UnrealisedROEPercent OptionalDecimal `json:"unrealisedRoePcnt"`
	AverageEntryPrice    OptionalDecimal `json:"avgEntryPrice"`
	LiquidationPrice     OptionalDecimal `json:"liquidationPrice"`
	BankruptPrice        OptionalDecimal `json:"bankruptPrice"`
	SettleCurrency       string          `json:"settleCurrency"`
	CumulativeTradeFee   OptionalDecimal `json:"cumulativeTradeFee"`
	CumulativeFundingFee OptionalDecimal `json:"cumulativeFundingFee"`
	CumulativeTax        OptionalDecimal `json:"cumulativeTax"`
	WithdrawPNL          OptionalDecimal `json:"withdrawPnl"`
	Raw                  json.RawMessage `json:"-"`
}

// ClosedPosition은 청산 또는 종료된 포지션의 이력 한 건이다.
type ClosedPosition struct {
	CloseID           string          `json:"closeId"`
	UserID            string          `json:"userId"`
	Symbol            string          `json:"symbol"`
	SettleCurrency    string          `json:"settleCurrency"`
	Leverage          OptionalDecimal `json:"leverage"`
	Type              string          `json:"type"`
	PNL               OptionalDecimal `json:"pnl"`
	RealisedGrossCost OptionalDecimal `json:"realisedGrossCost"`
	WithdrawPNL       OptionalDecimal `json:"withdrawPnl"`
	TradeFee          OptionalDecimal `json:"tradeFee"`
	FundingFee        OptionalDecimal `json:"fundingFee"`
	OpenTime          int64           `json:"openTime"`
	CloseTime         int64           `json:"closeTime"`
	OpenPrice         OptionalDecimal `json:"openPrice"`
	ClosePrice        OptionalDecimal `json:"closePrice"`
	MarginMode        MarginMode      `json:"marginMode"`
	PositionSide      PositionSide    `json:"positionSide"`
	Tax               OptionalDecimal `json:"tax"`
	ROE               OptionalDecimal `json:"roe"`
	LiquidAmount      OptionalDecimal `json:"liquidAmount"`
	LiquidPrice       OptionalDecimal `json:"liquidPrice"`
	Side              string          `json:"side"`
	Raw               json.RawMessage `json:"-"`
}

// ClosedPositionPage는 페이지 번호 기반 포지션 종료 이력이다.
type ClosedPositionPage struct {
	CurrentPage int
	PageSize    int
	TotalNumber int
	TotalPages  int
	Positions   []ClosedPosition
	Raw         json.RawMessage
}

// ClosedPositionsRequest는 종료 포지션 이력 조회 조건이다. Limit은 최대 200이고 PageID는 1부터 시작한다.
type ClosedPositionsRequest struct {
	Symbol string
	From   *time.Time
	To     *time.Time
	Limit  int
	PageID int
}

// SymbolMarginMode는 계약 하나의 증거금 모드다.
type SymbolMarginMode struct {
	Symbol     string          `json:"symbol"`
	MarginMode MarginMode      `json:"marginMode"`
	Raw        json.RawMessage `json:"-"`
}

// SwitchMarginModeRequest는 계약 하나의 증거금 모드 전환 조건이다.
type SwitchMarginModeRequest struct {
	Symbol     string     `json:"symbol"`
	MarginMode MarginMode `json:"marginMode"`
}

// BatchSwitchMarginModeRequest는 여러 계약의 증거금 모드를 한 번에 전환하는 조건이다.
type BatchSwitchMarginModeRequest struct {
	MarginMode MarginMode `json:"marginMode"`
	Symbols    []string   `json:"symbols"`
}

// BatchMarginModeError는 일괄 증거금 모드 전환에서 실패한 계약과 사유다.
type BatchMarginModeError struct {
	Code    string `json:"code"`
	Message string `json:"msg"`
	Symbol  string `json:"symbol"`
}

// BatchMarginModeResult는 일괄 증거금 모드 전환의 계약별 결과다.
type BatchMarginModeResult struct {
	MarginModes map[string]MarginMode  `json:"marginMode"`
	Errors      []BatchMarginModeError `json:"errors"`
	Raw         json.RawMessage        `json:"-"`
}

// PositionModeResult는 계정 전체 포지션 모드다.
type PositionModeResult struct {
	PositionMode PositionMode    `json:"positionMode"`
	Raw          json.RawMessage `json:"-"`
}

// MaxOpenSizeRequest는 가격과 레버리지에 따른 최대 진입 수량 조회 조건이다.
type MaxOpenSizeRequest struct {
	Symbol   string
	Price    string
	Leverage int
}

// MaxOpenSize는 계약의 최대 매수·매도 진입 수량이다.
type MaxOpenSize struct {
	Symbol          string          `json:"symbol"`
	MaxBuyOpenSize  int64           `json:"maxBuyOpenSize"`
	MaxSellOpenSize int64           `json:"maxSellOpenSize"`
	Raw             json.RawMessage `json:"-"`
}

// MaxWithdrawMarginRequest는 격리 포지션에서 인출 가능한 최대 증거금 조회 조건이다.
// PositionSide는 단방향 모드에서 생략하면 BOTH이며 헤지 모드에서는 필수다.
type MaxWithdrawMarginRequest struct {
	Symbol       string
	PositionSide PositionSide
}

// CrossLeverage는 계약의 교차 증거금 레버리지다.
type CrossLeverage struct {
	Symbol   string          `json:"symbol"`
	Leverage Decimal         `json:"leverage"`
	Raw      json.RawMessage `json:"-"`
}

// ChangeCrossLeverageRequest는 계약의 교차 증거금 레버리지 변경 조건이다.
type ChangeCrossLeverageRequest struct {
	Symbol   string `json:"symbol"`
	Leverage string `json:"leverage"`
}

// AddIsolatedMarginRequest는 격리 포지션에 증거금을 추가하는 조건이다.
// BizNo는 같은 요청이 한 번만 처리되도록 사용자가 만드는 36자 이하 고유 ID다.
type AddIsolatedMarginRequest struct {
	Symbol       string
	Margin       string
	BizNo        string
	PositionSide PositionSide
}

// RemoveIsolatedMarginRequest는 격리 포지션에서 증거금을 인출하는 조건이다.
type RemoveIsolatedMarginRequest struct {
	Symbol         string
	WithdrawAmount string
	PositionSide   PositionSide
}

// AutoDepositRequest는 격리 포지션의 증거금 자동 추가 설정이다.
// 단방향 모드는 Status, 헤지 모드는 LongStatus와 ShortStatus를 사용한다. 거래소는 이 기능을 더 이상 권장하지 않는다.
type AutoDepositRequest struct {
	Symbol      string `json:"symbol"`
	Status      *bool  `json:"status,omitempty"`
	LongStatus  *bool  `json:"longStatus,omitempty"`
	ShortStatus *bool  `json:"shortStatus,omitempty"`
}

// CrossRiskLimitRequest는 교차 증거금 리스크 한도 조회 조건이다.
// TotalMargin과 Leverage를 생략하면 거래소가 계정 기준값을 사용한다.
type CrossRiskLimitRequest struct {
	Symbol      string
	TotalMargin string
	Leverage    int
}

// CrossRiskLimit은 교차 증거금 기준의 최대 진입 수량과 증거금율이다.
type CrossRiskLimit struct {
	Symbol       string          `json:"symbol"`
	MaxOpenSize  int64           `json:"maxOpenSize"`
	MaxOpenValue Decimal         `json:"maxOpenValue"`
	TotalMargin  Decimal         `json:"totalMargin"`
	Price        Decimal         `json:"price"`
	Leverage     Decimal         `json:"leverage"`
	MMR          Decimal         `json:"mmr"`
	IMR          Decimal         `json:"imr"`
	Currency     string          `json:"currency"`
	Raw          json.RawMessage `json:"-"`
}

// CrossMarginRequirementRequest는 교차 증거금 필요액 계산 조건이다. Leverage를 생략하면 계약 최대 레버리지를 사용한다.
type CrossMarginRequirementRequest struct {
	Symbol        string `json:"symbol"`
	Leverage      string `json:"leverage,omitempty"`
	PositionValue string `json:"positionValue"`
}

// CrossMarginRequirement는 교차 증거금의 필요 초기·유지 증거금율이다.
type CrossMarginRequirement struct {
	Symbol        string          `json:"symbol"`
	Size          Decimal         `json:"size"`
	PositionValue Decimal         `json:"positionValue"`
	Price         Decimal         `json:"price"`
	IMR           Decimal         `json:"imr"`
	MMR           Decimal         `json:"mmr"`
	Raw           json.RawMessage `json:"-"`
}

// RiskLimitLevel은 격리 증거금 리스크 한도의 한 단계다.
type RiskLimitLevel struct {
	Symbol         string          `json:"symbol"`
	Level          int             `json:"level"`
	MaxRiskLimit   int64           `json:"maxRiskLimit"`
	MinRiskLimit   int64           `json:"minRiskLimit"`
	MaxLeverage    int             `json:"maxLeverage"`
	InitialMargin  Decimal         `json:"initialMargin"`
	MaintainMargin Decimal         `json:"maintainMargin"`
	Raw            json.RawMessage `json:"-"`
}

// ChangeRiskLimitLevelRequest는 격리 포지션의 리스크 한도 단계 변경 조건이다.
type ChangeRiskLimitLevelRequest struct {
	Symbol string `json:"symbol"`
	Level  int    `json:"level"`
}

// PositionDetails는 계약 하나의 v2 포지션 상세를 조회한다. 헤지 모드에서는 방향별로 여러 건이 반환된다.
func (client *Client) PositionDetails(
	ctx context.Context,
	symbol string,
	options ...trade.RequestOption,
) ([]PositionDetail, error) {
	if err := validateSymbol(symbol); err != nil {
		return nil, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/api/v2/position", url.Values{"symbol": {symbol}}, nil,
		futuresLimit(2), credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeList(client, response, commonexchange.OperationRead,
		func(value *PositionDetail, raw json.RawMessage) { value.Raw = raw })
}

// ClosedPositions는 종료된 Futures 포지션 이력을 페이지 번호 기반으로 조회한다.
func (client *Client) ClosedPositions(
	ctx context.Context,
	request ClosedPositionsRequest,
	options ...trade.RequestOption,
) (ClosedPositionPage, error) {
	if err := request.validate(); err != nil {
		return ClosedPositionPage{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/api/v1/history-positions", request.values(), nil,
		futuresLimit(2), credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return ClosedPositionPage{}, err
	}
	var rawPage struct {
		CurrentPage int               `json:"currentPage"`
		PageSize    int               `json:"pageSize"`
		TotalNumber int               `json:"totalNum"`
		TotalPages  int               `json:"totalPage"`
		Items       []json.RawMessage `json:"items"`
	}
	data, err := client.decodeData(response, commonexchange.OperationRead, &rawPage)
	if err != nil {
		return ClosedPositionPage{}, err
	}
	positions, err := decodeRawItems(client, response, commonexchange.OperationRead, rawPage.Items,
		func(value *ClosedPosition, raw json.RawMessage) { value.Raw = raw })
	if err != nil {
		return ClosedPositionPage{}, err
	}
	return ClosedPositionPage{
		CurrentPage: rawPage.CurrentPage, PageSize: rawPage.PageSize,
		TotalNumber: rawPage.TotalNumber, TotalPages: rawPage.TotalPages,
		Positions: positions, Raw: cloneBytes(data),
	}, nil
}

// MarginMode는 계약 하나의 현재 증거금 모드를 조회한다.
func (client *Client) MarginMode(
	ctx context.Context,
	symbol string,
	options ...trade.RequestOption,
) (SymbolMarginMode, error) {
	if err := validateSymbol(symbol); err != nil {
		return SymbolMarginMode{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/api/v2/position/getMarginMode", url.Values{"symbol": {symbol}}, nil,
		futuresLimit(2), credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return SymbolMarginMode{}, err
	}
	return decodeObject(client, response, commonexchange.OperationRead,
		func(value *SymbolMarginMode, raw json.RawMessage) { value.Raw = raw })
}

// SwitchMarginMode는 계약 하나의 증거금 모드를 전환한다. 열린 주문이나 포지션이 있으면 거래소가 거절한다.
func (client *Client) SwitchMarginMode(
	ctx context.Context,
	request SwitchMarginModeRequest,
	options ...trade.RequestOption,
) (SymbolMarginMode, error) {
	if err := validateSymbol(request.Symbol); err != nil {
		return SymbolMarginMode{}, err
	}
	if err := validateMarginMode(request.MarginMode); err != nil {
		return SymbolMarginMode{}, err
	}
	response, err := client.postJSON(
		ctx, "/api/v2/position/changeMarginMode", request, 2, options...,
	)
	if err != nil {
		return SymbolMarginMode{}, err
	}
	return decodeObject(client, response, commonexchange.OperationMutation,
		func(value *SymbolMarginMode, raw json.RawMessage) { value.Raw = raw })
}

// BatchSwitchMarginMode는 여러 계약의 증거금 모드를 한 번에 전환한다. 실패한 계약은 결과의 Errors에 담긴다.
func (client *Client) BatchSwitchMarginMode(
	ctx context.Context,
	request BatchSwitchMarginModeRequest,
	options ...trade.RequestOption,
) (BatchMarginModeResult, error) {
	if err := validateMarginMode(request.MarginMode); err != nil {
		return BatchMarginModeResult{}, err
	}
	if len(request.Symbols) == 0 {
		return BatchMarginModeResult{}, validationError("at least one symbol is required")
	}
	for _, symbol := range request.Symbols {
		if err := validateSymbol(symbol); err != nil {
			return BatchMarginModeResult{}, err
		}
	}
	response, err := client.postJSON(
		ctx, "/api/v2/position/batchChangeMarginMode", request, 2, options...,
	)
	if err != nil {
		return BatchMarginModeResult{}, err
	}
	return decodeObject(client, response, commonexchange.OperationMutation,
		func(value *BatchMarginModeResult, raw json.RawMessage) { value.Raw = raw })
}

// GetPositionMode는 계정 전체의 단방향·헤지 포지션 모드를 조회한다.
func (client *Client) GetPositionMode(
	ctx context.Context,
	options ...trade.RequestOption,
) (PositionModeResult, error) {
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/api/v2/position/getPositionMode", nil, nil,
		futuresLimit(2), credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return PositionModeResult{}, err
	}
	return decodeObject(client, response, commonexchange.OperationRead,
		func(value *PositionModeResult, raw json.RawMessage) { value.Raw = raw })
}

// SwitchPositionMode는 계정 전체의 단방향·헤지 포지션 모드를 전환한다.
func (client *Client) SwitchPositionMode(
	ctx context.Context,
	mode PositionMode,
	options ...trade.RequestOption,
) (PositionModeResult, error) {
	if mode != PositionModeOneWay && mode != PositionModeHedge {
		return PositionModeResult{}, validationError("position mode must be 0 or 1")
	}
	response, err := client.postJSON(
		ctx, "/api/v2/position/switchPositionMode",
		struct {
			PositionMode PositionMode `json:"positionMode"`
		}{mode}, 2, options...,
	)
	if err != nil {
		return PositionModeResult{}, err
	}
	return decodeObject(client, response, commonexchange.OperationMutation,
		func(value *PositionModeResult, raw json.RawMessage) { value.Raw = raw })
}

// MaxOpenSize는 지정한 가격과 레버리지에서 최대 진입 가능 수량을 조회한다.
func (client *Client) MaxOpenSize(
	ctx context.Context,
	request MaxOpenSizeRequest,
	options ...trade.RequestOption,
) (MaxOpenSize, error) {
	if err := validateSymbol(request.Symbol); err != nil {
		return MaxOpenSize{}, err
	}
	if err := validatePositiveDecimal("price", request.Price); err != nil {
		return MaxOpenSize{}, err
	}
	if request.Leverage < 1 || request.Leverage > 1_000 {
		return MaxOpenSize{}, validationError("leverage must be between 1 and 1000")
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/api/v2/getMaxOpenSize",
		url.Values{
			"symbol": {request.Symbol}, "price": {request.Price},
			"leverage": {strconv.Itoa(request.Leverage)},
		}, nil,
		futuresLimit(2), credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return MaxOpenSize{}, err
	}
	return decodeObject(client, response, commonexchange.OperationRead,
		func(value *MaxOpenSize, raw json.RawMessage) { value.Raw = raw })
}

// MaxWithdrawMargin은 격리 포지션에서 인출 가능한 최대 증거금을 조회한다.
func (client *Client) MaxWithdrawMargin(
	ctx context.Context,
	request MaxWithdrawMarginRequest,
	options ...trade.RequestOption,
) (Decimal, error) {
	if err := validateSymbol(request.Symbol); err != nil {
		return "", err
	}
	if err := validateOptionalPositionSide(request.PositionSide); err != nil {
		return "", err
	}
	values := url.Values{"symbol": {request.Symbol}}
	setIfNotEmpty(values, "positionSide", string(request.PositionSide))
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/api/v1/margin/maxWithdrawMargin", values, nil,
		futuresLimit(10), credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return "", err
	}
	var amount Decimal
	if _, err := client.decodeData(response, commonexchange.OperationRead, &amount); err != nil {
		return "", err
	}
	return amount, nil
}

// CrossLeverage는 계약의 교차 증거금 레버리지를 조회한다.
func (client *Client) CrossLeverage(
	ctx context.Context,
	symbol string,
	options ...trade.RequestOption,
) (CrossLeverage, error) {
	if err := validateSymbol(symbol); err != nil {
		return CrossLeverage{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/api/v2/getCrossUserLeverage", url.Values{"symbol": {symbol}}, nil,
		futuresLimit(2), credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return CrossLeverage{}, err
	}
	return decodeObject(client, response, commonexchange.OperationRead,
		func(value *CrossLeverage, raw json.RawMessage) { value.Raw = raw })
}

// ChangeCrossLeverage는 계약의 교차 증거금 레버리지를 변경한다. 성공하면 true를 반환한다.
func (client *Client) ChangeCrossLeverage(
	ctx context.Context,
	request ChangeCrossLeverageRequest,
	options ...trade.RequestOption,
) (bool, error) {
	if err := validateSymbol(request.Symbol); err != nil {
		return false, err
	}
	if err := validatePositiveDecimal("leverage", request.Leverage); err != nil {
		return false, err
	}
	response, err := client.postJSON(ctx, "/api/v2/changeCrossUserLeverage", request, 2, options...)
	if err != nil {
		return false, err
	}
	return decodeBool(client, response, commonexchange.OperationMutation)
}

// AddIsolatedMargin은 격리 포지션에 증거금을 추가하고 갱신된 포지션을 반환한다.
func (client *Client) AddIsolatedMargin(
	ctx context.Context,
	request AddIsolatedMarginRequest,
	options ...trade.RequestOption,
) (PositionDetail, error) {
	if err := validateSymbol(request.Symbol); err != nil {
		return PositionDetail{}, err
	}
	if err := validatePositiveDecimal("margin", request.Margin); err != nil {
		return PositionDetail{}, err
	}
	if !bizNoPattern.MatchString(request.BizNo) {
		return PositionDetail{}, validationError("biz number is required and must match [0-9A-Za-z_-]{1,36}")
	}
	if err := validateOptionalPositionSide(request.PositionSide); err != nil {
		return PositionDetail{}, err
	}
	body := struct {
		Symbol       string       `json:"symbol"`
		Margin       json.Number  `json:"margin"`
		BizNo        string       `json:"bizNo"`
		PositionSide PositionSide `json:"positionSide,omitempty"`
	}{request.Symbol, json.Number(request.Margin), request.BizNo, request.PositionSide}
	response, err := client.postJSON(ctx, "/api/v1/position/margin/deposit-margin", body, 4, options...)
	if err != nil {
		return PositionDetail{}, err
	}
	return decodeObject(client, response, commonexchange.OperationMutation,
		func(value *PositionDetail, raw json.RawMessage) { value.Raw = raw })
}

// RemoveIsolatedMargin은 격리 포지션에서 증거금을 인출하고 인출된 금액을 반환한다.
func (client *Client) RemoveIsolatedMargin(
	ctx context.Context,
	request RemoveIsolatedMarginRequest,
	options ...trade.RequestOption,
) (Decimal, error) {
	if err := validateSymbol(request.Symbol); err != nil {
		return "", err
	}
	if err := validatePositiveDecimal("withdraw amount", request.WithdrawAmount); err != nil {
		return "", err
	}
	if err := validateOptionalPositionSide(request.PositionSide); err != nil {
		return "", err
	}
	body := struct {
		Symbol         string       `json:"symbol"`
		WithdrawAmount string       `json:"withdrawAmount"`
		PositionSide   PositionSide `json:"positionSide,omitempty"`
	}{request.Symbol, request.WithdrawAmount, request.PositionSide}
	response, err := client.postJSON(ctx, "/api/v1/margin/withdrawMargin", body, 10, options...)
	if err != nil {
		return "", err
	}
	var amount Decimal
	if _, err := client.decodeData(response, commonexchange.OperationMutation, &amount); err != nil {
		return "", err
	}
	return amount, nil
}

// SetAutoDeposit는 격리 포지션의 증거금 자동 추가를 설정한다. 성공하면 true를 반환한다.
// KuCoin은 격리 증거금과 자동 추가 조합을 더 이상 권장하지 않고 교차 증거금 사용을 안내한다.
func (client *Client) SetAutoDeposit(
	ctx context.Context,
	request AutoDepositRequest,
	options ...trade.RequestOption,
) (bool, error) {
	if err := validateSymbol(request.Symbol); err != nil {
		return false, err
	}
	if request.Status == nil && request.LongStatus == nil && request.ShortStatus == nil {
		return false, validationError("status, long status, or short status is required")
	}
	if request.Status != nil && (request.LongStatus != nil || request.ShortStatus != nil) {
		return false, validationError("status cannot be combined with long or short status")
	}
	response, err := client.postJSON(
		ctx, "/api/v1/position/margin/auto-deposit-status", request, 4, options...,
	)
	if err != nil {
		return false, err
	}
	return decodeBool(client, response, commonexchange.OperationMutation)
}

// CrossRiskLimits는 교차 증거금 기준 최대 진입 수량과 증거금율을 조회한다.
func (client *Client) CrossRiskLimits(
	ctx context.Context,
	request CrossRiskLimitRequest,
	options ...trade.RequestOption,
) ([]CrossRiskLimit, error) {
	if err := validateSymbol(request.Symbol); err != nil {
		return nil, err
	}
	if request.TotalMargin != "" {
		if err := validatePositiveDecimal("total margin", request.TotalMargin); err != nil {
			return nil, err
		}
	}
	if request.Leverage < 0 || request.Leverage > 1_000 {
		return nil, validationError("leverage must be between 1 and 1000 or zero")
	}
	values := url.Values{"symbol": {request.Symbol}}
	setIfNotEmpty(values, "totalMargin", request.TotalMargin)
	setPositiveInt(values, "leverage", request.Leverage)
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/api/v2/batchGetCrossOrderLimit", values, nil,
		futuresLimit(2), credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeList(client, response, commonexchange.OperationRead,
		func(value *CrossRiskLimit, raw json.RawMessage) { value.Raw = raw })
}

// CrossMarginRequirements는 포지션 가치에 필요한 교차 증거금율을 계산한다.
// 거래소 문서의 응답 형식은 객체와 배열이 섞여 있어 두 형식을 모두 목록으로 반환한다.
func (client *Client) CrossMarginRequirements(
	ctx context.Context,
	request CrossMarginRequirementRequest,
	options ...trade.RequestOption,
) ([]CrossMarginRequirement, error) {
	if err := validateSymbol(request.Symbol); err != nil {
		return nil, err
	}
	if request.Leverage != "" {
		if err := validatePositiveDecimal("leverage", request.Leverage); err != nil {
			return nil, err
		}
	}
	if err := validatePositiveDecimal("position value", request.PositionValue); err != nil {
		return nil, err
	}
	body, err := json.Marshal(request)
	if err != nil {
		return nil, validationError("encode KuCoin Futures margin requirement: %v", err)
	}
	// 계산 전용 POST이므로 상태를 바꾸지 않는 읽기 작업으로 취급하되 키에는 Futures(Trade) 권한이 필요하다.
	response, err := client.executePrivate(
		ctx, http.MethodPost, "/api/v2/getCrossModeMarginRequirement", nil, body,
		futuresLimit(3), credential.PermissionTrade, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	data, err := client.responseData(response, commonexchange.OperationRead)
	if err != nil {
		return nil, err
	}
	if firstJSONByte(data) == '[' {
		var rawItems []json.RawMessage
		if err := json.Unmarshal(data, &rawItems); err != nil {
			return nil, client.decodeBodyError(response, commonexchange.OperationRead, err)
		}
		return decodeRawItems(client, response, commonexchange.OperationRead, rawItems,
			func(value *CrossMarginRequirement, raw json.RawMessage) { value.Raw = raw })
	}
	var item CrossMarginRequirement
	if err := json.Unmarshal(data, &item); err != nil {
		return nil, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	item.Raw = cloneBytes(data)
	return []CrossMarginRequirement{item}, nil
}

// RiskLimitLevels는 격리 증거금 계약의 리스크 한도 단계 목록을 조회한다.
func (client *Client) RiskLimitLevels(
	ctx context.Context,
	symbol string,
	options ...trade.RequestOption,
) ([]RiskLimitLevel, error) {
	if err := validateSymbol(symbol); err != nil {
		return nil, err
	}
	response, err := client.executePublic(
		ctx, http.MethodGet, "/api/v1/contracts/risk-limit/"+url.PathEscape(symbol), nil,
		publicLimit(5), options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeList(client, response, commonexchange.OperationRead,
		func(value *RiskLimitLevel, raw json.RawMessage) { value.Raw = raw })
}

// ChangeRiskLimitLevel은 격리 포지션의 리스크 한도 단계를 변경한다. 성공하면 true를 반환한다.
func (client *Client) ChangeRiskLimitLevel(
	ctx context.Context,
	request ChangeRiskLimitLevelRequest,
	options ...trade.RequestOption,
) (bool, error) {
	if err := validateSymbol(request.Symbol); err != nil {
		return false, err
	}
	if request.Level < 1 {
		return false, validationError("risk limit level must be positive")
	}
	response, err := client.postJSON(
		ctx, "/api/v1/position/risk-limit-level/change", request, 5, options...,
	)
	if err != nil {
		return false, err
	}
	return decodeBool(client, response, commonexchange.OperationMutation)
}

// postJSON은 직렬화한 JSON 본문으로 Trade 권한이 필요한 POST 변경 요청을 한 번만 보낸다.
func (client *Client) postJSON(
	ctx context.Context,
	path string,
	payload any,
	weight int,
	options ...trade.RequestOption,
) (commonexchange.Response, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return commonexchange.Response{}, validationError("encode KuCoin Futures request: %v", err)
	}
	return client.executePrivate(
		ctx, http.MethodPost, path, nil, body,
		futuresLimit(weight), credential.PermissionTrade, commonexchange.OperationMutation, options...,
	)
}

func validateMarginMode(mode MarginMode) error {
	if mode != MarginModeIsolated && mode != MarginModeCross {
		return validationError("margin mode must be ISOLATED or CROSS")
	}
	return nil
}

func validateOptionalPositionSide(side PositionSide) error {
	if side != "" && side != PositionSideBoth && side != PositionSideLong && side != PositionSideShort {
		return validationError("position side must be BOTH, LONG, SHORT, or empty")
	}
	return nil
}

func (request ClosedPositionsRequest) validate() error {
	if request.Symbol != "" {
		if err := validateSymbol(request.Symbol); err != nil {
			return err
		}
	}
	if err := validateTimeRange("closed positions", request.From, request.To); err != nil {
		return err
	}
	if request.Limit < 0 || request.Limit > 200 {
		return validationError("limit must be between 1 and 200 or zero for default")
	}
	if request.PageID < 0 {
		return validationError("page ID cannot be negative")
	}
	return nil
}

func (request ClosedPositionsRequest) values() url.Values {
	values := make(url.Values)
	setIfNotEmpty(values, "symbol", request.Symbol)
	setMillis(values, "from", request.From)
	setMillis(values, "to", request.To)
	setPositiveInt(values, "limit", request.Limit)
	setPositiveInt(values, "pageId", request.PageID)
	return values
}
