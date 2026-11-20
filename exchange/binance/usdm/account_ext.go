package usdm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

// Balance는 Futures 지갑의 자산별 잔고다.
type Balance struct {
	AccountAlias          string          `json:"accountAlias"`
	Asset                 string          `json:"asset"`
	WalletBalance         string          `json:"balance"`
	CrossWalletBalance    string          `json:"crossWalletBalance"`
	CrossUnrealizedProfit string          `json:"crossUnPnl"`
	AvailableBalance      string          `json:"availableBalance"`
	MaximumWithdrawAmount string          `json:"maxWithdrawAmount"`
	MarginAvailable       bool            `json:"marginAvailable"`
	UpdateTime            int64           `json:"updateTime"`
	Raw                   json.RawMessage `json:"-"`
}

func (item *Balance) setRaw(raw json.RawMessage) { item.Raw = raw }

// Balances는 Futures 지갑의 자산별 잔고를 조회한다.
// GET /fapi/v3/balance이며 credential.PermissionRead가 필요하고 IP weight는 5다.
func (client *Client) Balances(ctx context.Context, options ...trade.RequestOption) ([]Balance, error) {
	response, err := client.executeSigned(ctx, http.MethodGet, "/fapi/v3/balance", nil, 5, 0, credential.PermissionRead, commonexchange.OperationRead, options...)
	if err != nil {
		return nil, err
	}
	return decodeArray[Balance](client, response, commonexchange.OperationRead)
}

// AccountConfig는 계정의 수수료 등급과 거래 가능 여부, 포지션·마진 모드 설정이다.
type AccountConfig struct {
	FeeTier           int             `json:"feeTier"`
	CanTrade          bool            `json:"canTrade"`
	CanDeposit        bool            `json:"canDeposit"`
	CanWithdraw       bool            `json:"canWithdraw"`
	DualSidePosition  bool            `json:"dualSidePosition"`
	UpdateTime        int64           `json:"updateTime"`
	MultiAssetsMargin bool            `json:"multiAssetsMargin"`
	TradeGroupID      int64           `json:"tradeGroupId"`
	Raw               json.RawMessage `json:"-"`
}

func (item *AccountConfig) setRaw(raw json.RawMessage) { item.Raw = raw }

// AccountConfig는 계정 설정을 조회한다. UpdateTime은 공식 문서가 무시하라고 밝힌 예약 필드다.
// GET /fapi/v1/accountConfig이며 credential.PermissionRead가 필요하고 IP weight는 5다.
func (client *Client) AccountConfig(ctx context.Context, options ...trade.RequestOption) (AccountConfig, error) {
	response, err := client.executeSigned(ctx, http.MethodGet, "/fapi/v1/accountConfig", nil, 5, 0, credential.PermissionRead, commonexchange.OperationRead, options...)
	if err != nil {
		return AccountConfig{}, err
	}
	return decodeObject[AccountConfig](client, response, commonexchange.OperationRead)
}

// SymbolConfigsRequest는 계약별 설정 조회 범위다. Symbol을 비우면 전체 계약을 조회한다.
type SymbolConfigsRequest struct{ Symbol string }

// SymbolConfig는 계약별 마진 모드와 레버리지 설정이다.
type SymbolConfig struct {
	Symbol           string          `json:"symbol"`
	MarginType       MarginType      `json:"marginType"`
	IsAutoAddMargin  bool            `json:"isAutoAddMargin"`
	Leverage         int             `json:"leverage"`
	MaxNotionalValue string          `json:"maxNotionalValue"`
	Raw              json.RawMessage `json:"-"`
}

func (item *SymbolConfig) setRaw(raw json.RawMessage) { item.Raw = raw }

// UnmarshalJSON은 isAutoAddMargin이 불리언 또는 "true"·"false" 문자열로 오는 경우를 모두 받는다.
func (item *SymbolConfig) UnmarshalJSON(data []byte) error {
	var payload struct {
		Symbol           string          `json:"symbol"`
		MarginType       MarginType      `json:"marginType"`
		IsAutoAddMargin  json.RawMessage `json:"isAutoAddMargin"`
		Leverage         int             `json:"leverage"`
		MaxNotionalValue string          `json:"maxNotionalValue"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return err
	}
	autoAddMargin, err := parseLooseBool(payload.IsAutoAddMargin)
	if err != nil {
		return err
	}
	item.Symbol, item.MarginType, item.IsAutoAddMargin = payload.Symbol, payload.MarginType, autoAddMargin
	item.Leverage, item.MaxNotionalValue = payload.Leverage, payload.MaxNotionalValue
	return nil
}

// parseLooseBool은 JSON 불리언과 "true"·"false" 문자열을 모두 불리언으로 바꾼다. 비어 있거나 null이면 false다.
func parseLooseBool(raw json.RawMessage) (bool, error) {
	text := strings.Trim(strings.TrimSpace(string(raw)), `"`)
	switch strings.ToLower(text) {
	case "", "null", "false":
		return false, nil
	case "true":
		return true, nil
	default:
		return false, fmt.Errorf("Binance USD-M boolean field has unexpected value %q", text)
	}
}

// SymbolConfigs는 계약별 마진 모드와 레버리지 설정을 조회한다.
// GET /fapi/v1/symbolConfig이며 credential.PermissionRead가 필요하고 IP weight는 5다.
func (client *Client) SymbolConfigs(ctx context.Context, request SymbolConfigsRequest, options ...trade.RequestOption) ([]SymbolConfig, error) {
	if err := validateOptionalSymbol(request.Symbol); err != nil {
		return nil, err
	}
	response, err := client.executeSigned(ctx, http.MethodGet, "/fapi/v1/symbolConfig", optionalSymbolValues(request.Symbol), 5, 0, credential.PermissionRead, commonexchange.OperationRead, options...)
	if err != nil {
		return nil, err
	}
	return decodeArray[SymbolConfig](client, response, commonexchange.OperationRead)
}

// PositionMode는 계정 전체에 적용되는 포지션 모드다. DualSidePosition이 true이면 헤지 모드, false이면 단방향 모드다.
type PositionMode struct {
	DualSidePosition bool            `json:"dualSidePosition"`
	Raw              json.RawMessage `json:"-"`
}

func (item *PositionMode) setRaw(raw json.RawMessage) { item.Raw = raw }

// PositionMode는 현재 포지션 모드를 조회한다.
// GET /fapi/v1/positionSide/dual이며 credential.PermissionRead가 필요하고 IP weight는 30이다.
func (client *Client) PositionMode(ctx context.Context, options ...trade.RequestOption) (PositionMode, error) {
	response, err := client.executeSigned(ctx, http.MethodGet, "/fapi/v1/positionSide/dual", nil, 30, 0, credential.PermissionRead, commonexchange.OperationRead, options...)
	if err != nil {
		return PositionMode{}, err
	}
	return decodeObject[PositionMode](client, response, commonexchange.OperationRead)
}

// MultiAssetsMode는 계정 전체에 적용되는 마진 모드다. MultiAssetsMargin이 true이면 멀티에셋 모드, false이면 단일에셋 모드다.
type MultiAssetsMode struct {
	MultiAssetsMargin bool            `json:"multiAssetsMargin"`
	Raw               json.RawMessage `json:"-"`
}

func (item *MultiAssetsMode) setRaw(raw json.RawMessage) { item.Raw = raw }

// MultiAssetsMode는 현재 멀티에셋 모드 여부를 조회한다.
// GET /fapi/v1/multiAssetsMargin이며 credential.PermissionRead가 필요하고 IP weight는 30이다.
func (client *Client) MultiAssetsMode(ctx context.Context, options ...trade.RequestOption) (MultiAssetsMode, error) {
	response, err := client.executeSigned(ctx, http.MethodGet, "/fapi/v1/multiAssetsMargin", nil, 30, 0, credential.PermissionRead, commonexchange.OperationRead, options...)
	if err != nil {
		return MultiAssetsMode{}, err
	}
	return decodeObject[MultiAssetsMode](client, response, commonexchange.OperationRead)
}

// Income은 손익·펀딩비·수수료 같은 계정 수입 내역 한 건이다.
type Income struct {
	Symbol     string          `json:"symbol"`
	IncomeType string          `json:"incomeType"`
	Income     string          `json:"income"`
	Asset      string          `json:"asset"`
	Info       string          `json:"info"`
	Time       int64           `json:"time"`
	TranID     int64           `json:"tranId"`
	TradeID    string          `json:"tradeId"`
	Raw        json.RawMessage `json:"-"`
}

func (item *Income) setRaw(raw json.RawMessage) { item.Raw = raw }

// IncomeHistoryRequest는 수입 내역 조회 조건이다.
// IncomeType은 REALIZED_PNL, FUNDING_FEE, COMMISSION, TRANSFER처럼 공식 문서의 incomeType 값이며 비우면 모든 종류를 조회한다.
// 시각을 모두 생략하면 최근 7일을 조회하고 최근 3개월 데이터만 있으며, Limit 최대는 1,000, 서버 기본값은 100이다.
// page 파라미터는 공식 문서에서 폐지되어 지원하지 않으며 StartTime·EndTime으로 구간을 옮긴다.
type IncomeHistoryRequest struct {
	Symbol     string
	IncomeType string
	StartTime  *time.Time
	EndTime    *time.Time
	Limit      int
}

func (request IncomeHistoryRequest) validate() error {
	if err := validateOptionalSymbol(request.Symbol); err != nil {
		return err
	}
	if strings.TrimSpace(request.IncomeType) != request.IncomeType {
		return validationError("incomeType cannot have surrounding whitespace")
	}
	if err := validateLimit("income history", request.Limit, 1000); err != nil {
		return err
	}
	return validateTimeRange(request.StartTime, request.EndTime)
}

func (request IncomeHistoryRequest) values() url.Values {
	values := optionalSymbolValues(request.Symbol)
	set(values, "incomeType", request.IncomeType)
	setTimeRange(values, request.StartTime, request.EndTime)
	setLimit(values, request.Limit)
	return values
}

// IncomeHistory는 계정의 수입 내역(실현 손익, 펀딩비, 수수료, 이체 등)을 조회한다.
// GET /fapi/v1/income이며 credential.PermissionRead가 필요하고 IP weight는 30이다.
func (client *Client) IncomeHistory(ctx context.Context, request IncomeHistoryRequest, options ...trade.RequestOption) ([]Income, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executeSigned(ctx, http.MethodGet, "/fapi/v1/income", request.values(), 30, 0, credential.PermissionRead, commonexchange.OperationRead, options...)
	if err != nil {
		return nil, err
	}
	return decodeArray[Income](client, response, commonexchange.OperationRead)
}

// CommissionRateRequest는 수수료율을 조회할 계약이다.
type CommissionRateRequest struct{ Symbol string }

// CommissionRate는 계약별 계정 수수료율이다. 0.0002는 0.02%다.
type CommissionRate struct {
	Symbol              string          `json:"symbol"`
	MakerCommissionRate string          `json:"makerCommissionRate"`
	TakerCommissionRate string          `json:"takerCommissionRate"`
	RPICommissionRate   string          `json:"rpiCommissionRate"`
	Raw                 json.RawMessage `json:"-"`
}

func (item *CommissionRate) setRaw(raw json.RawMessage) { item.Raw = raw }

// CommissionRate는 계약별 maker·taker·RPI 수수료율을 조회한다.
// GET /fapi/v1/commissionRate이며 credential.PermissionRead가 필요하고 IP weight는 20이다.
func (client *Client) CommissionRate(ctx context.Context, request CommissionRateRequest, options ...trade.RequestOption) (CommissionRate, error) {
	if err := validateSymbol(request.Symbol); err != nil {
		return CommissionRate{}, err
	}
	response, err := client.executeSigned(ctx, http.MethodGet, "/fapi/v1/commissionRate", url.Values{"symbol": {request.Symbol}}, 20, 0, credential.PermissionRead, commonexchange.OperationRead, options...)
	if err != nil {
		return CommissionRate{}, err
	}
	return decodeObject[CommissionRate](client, response, commonexchange.OperationRead)
}

// LeverageBracketsRequest는 명목가별 레버리지 구간 조회 범위다. Symbol을 비우면 전체 계약을 조회한다.
type LeverageBracketsRequest struct{ Symbol string }

// LeverageBracketTier는 명목가 구간 하나의 최대 레버리지와 유지 마진율이다.
// 공식 응답이 JSON 숫자로 주는 값은 정밀도를 보존하는 json.Number로 담는다.
type LeverageBracketTier struct {
	Bracket          int         `json:"bracket"`
	InitialLeverage  int         `json:"initialLeverage"`
	NotionalCap      json.Number `json:"notionalCap"`
	NotionalFloor    json.Number `json:"notionalFloor"`
	MaintMarginRatio json.Number `json:"maintMarginRatio"`
	Cum              json.Number `json:"cum"`
}

// LeverageBracket은 계약의 명목가 구간 목록이다. NotionalCoef는 사용자별 구간 배수가 조정된 계약에만 온다.
type LeverageBracket struct {
	Symbol       string                `json:"symbol"`
	NotionalCoef json.Number           `json:"notionalCoef"`
	Brackets     []LeverageBracketTier `json:"brackets"`
	Raw          json.RawMessage       `json:"-"`
}

func (item *LeverageBracket) setRaw(raw json.RawMessage) { item.Raw = raw }

// LeverageBrackets는 계약별 명목가 구간과 구간별 최대 레버리지를 조회한다.
// Symbol을 지정하면 응답이 객체 하나로 오므로 항상 목록으로 반환한다.
// GET /fapi/v1/leverageBracket이며 credential.PermissionRead가 필요하고 IP weight는 1이다.
func (client *Client) LeverageBrackets(ctx context.Context, request LeverageBracketsRequest, options ...trade.RequestOption) ([]LeverageBracket, error) {
	if err := validateOptionalSymbol(request.Symbol); err != nil {
		return nil, err
	}
	response, err := client.executeSigned(ctx, http.MethodGet, "/fapi/v1/leverageBracket", optionalSymbolValues(request.Symbol), 1, 0, credential.PermissionRead, commonexchange.OperationRead, options...)
	if err != nil {
		return nil, err
	}
	return decodeMarketItems[LeverageBracket](client, response)
}

// OrderRateLimits는 계정의 주문 count 제한(ORDERS, 10초·1분)을 조회한다.
// GET /fapi/v1/rateLimit/order이며 credential.PermissionRead가 필요하고 IP weight는 1이다.
func (client *Client) OrderRateLimits(ctx context.Context, options ...trade.RequestOption) ([]RateLimit, error) {
	response, err := client.executeSigned(ctx, http.MethodGet, "/fapi/v1/rateLimit/order", nil, 1, 0, credential.PermissionRead, commonexchange.OperationRead, options...)
	if err != nil {
		return nil, err
	}
	var limits []RateLimit
	if err := client.decode(response, commonexchange.OperationRead, &limits); err != nil {
		return nil, err
	}
	return limits, nil
}

// APITradingStatusRequest는 정량 거래규칙 지표 조회 범위다. Symbol을 비우면 전체 계약과 계정 지표를 조회한다.
type APITradingStatusRequest struct{ Symbol string }

// TradingIndicator는 정량 거래규칙 지표 하나의 현재 값과 발동 값이다.
type TradingIndicator struct {
	Indicator          string      `json:"indicator"`
	Value              json.Number `json:"value"`
	TriggerValue       json.Number `json:"triggerValue"`
	PlannedRecoverTime int64       `json:"plannedRecoverTime"`
	IsLocked           bool        `json:"isLocked"`
}

// APITradingStatus는 정량 거래규칙 위반 지표다. Indicators 키는 계약 심볼이며 계정 단위 지표는 "ACCOUNT" 키로 온다.
type APITradingStatus struct {
	Indicators map[string][]TradingIndicator `json:"indicators"`
	UpdateTime int64                         `json:"updateTime"`
	Raw        json.RawMessage               `json:"-"`
}

func (item *APITradingStatus) setRaw(raw json.RawMessage) { item.Raw = raw }

// APITradingStatus는 정량 거래규칙 위반 지표와 거래 제한 상태를 조회한다.
// GET /fapi/v1/apiTradingStatus이며 credential.PermissionRead가 필요하고 IP weight는 Symbol 지정 1, 생략 10이다.
func (client *Client) APITradingStatus(ctx context.Context, request APITradingStatusRequest, options ...trade.RequestOption) (APITradingStatus, error) {
	if err := validateOptionalSymbol(request.Symbol); err != nil {
		return APITradingStatus{}, err
	}
	response, err := client.executeSigned(ctx, http.MethodGet, "/fapi/v1/apiTradingStatus", optionalSymbolValues(request.Symbol), symbolWeight(request.Symbol, 1, 10), 0, credential.PermissionRead, commonexchange.OperationRead, options...)
	if err != nil {
		return APITradingStatus{}, err
	}
	return decodeObject[APITradingStatus](client, response, commonexchange.OperationRead)
}

// FeeBurnStatus는 BNB 수수료 차감(수수료 할인) 사용 여부다.
type FeeBurnStatus struct {
	FeeBurn bool            `json:"feeBurn"`
	Raw     json.RawMessage `json:"-"`
}

func (item *FeeBurnStatus) setRaw(raw json.RawMessage) { item.Raw = raw }

// FeeBurn은 BNB 수수료 차감 사용 여부를 조회한다.
// GET /fapi/v1/feeBurn이며 credential.PermissionRead가 필요하고 IP weight는 30이다.
func (client *Client) FeeBurn(ctx context.Context, options ...trade.RequestOption) (FeeBurnStatus, error) {
	response, err := client.executeSigned(ctx, http.MethodGet, "/fapi/v1/feeBurn", nil, 30, 0, credential.PermissionRead, commonexchange.OperationRead, options...)
	if err != nil {
		return FeeBurnStatus{}, err
	}
	return decodeObject[FeeBurnStatus](client, response, commonexchange.OperationRead)
}

// ChangeFeeBurnRequest는 BNB 수수료 차감 설정이다. Enabled가 true이면 BNB로 수수료를 차감한다.
type ChangeFeeBurnRequest struct{ Enabled bool }

// ChangeFeeBurn은 모든 계약에 적용되는 BNB 수수료 차감을 켜거나 끈다.
// POST /fapi/v1/feeBurn이며 credential.PermissionTrade가 필요하고 IP weight는 1이다.
func (client *Client) ChangeFeeBurn(ctx context.Context, request ChangeFeeBurnRequest, options ...trade.RequestOption) (AckResult, error) {
	values := url.Values{"feeBurn": {strconv.FormatBool(request.Enabled)}}
	response, err := client.executeSigned(ctx, http.MethodPost, "/fapi/v1/feeBurn", values, 1, 0, credential.PermissionTrade, commonexchange.OperationMutation, options...)
	if err != nil {
		return AckResult{}, err
	}
	return decodeObject[AckResult](client, response, commonexchange.OperationMutation)
}

// DownloadRangeRequest는 이력 다운로드 요청의 조회 구간이다. 두 시각은 필수이고 간격은 1년을 넘을 수 없다.
type DownloadRangeRequest struct {
	StartTime time.Time
	EndTime   time.Time
}

func (request DownloadRangeRequest) validate() error {
	if request.StartTime.IsZero() || request.EndTime.IsZero() {
		return validationError("startTime and endTime are required")
	}
	if request.StartTime.After(request.EndTime) {
		return validationError("startTime cannot be after endTime")
	}
	if request.EndTime.After(request.StartTime.AddDate(1, 0, 0)) {
		return validationError("download range cannot exceed one year")
	}
	return nil
}

func (request DownloadRangeRequest) values() url.Values {
	return url.Values{"startTime": {strconv.FormatInt(request.StartTime.UnixMilli(), 10)}, "endTime": {strconv.FormatInt(request.EndTime.UnixMilli(), 10)}}
}

// DownloadTicket은 이력 다운로드 요청이 접수된 결과다.
// DownloadID로 같은 종류의 DownloadLink 메서드를 호출해 파일 링크를 받고, AverageCostMillis는 최근 30일 평균 생성 소요 시간(밀리초)이다.
type DownloadTicket struct {
	DownloadID        string          `json:"downloadId"`
	AverageCostMillis int64           `json:"avgCostTimestampOfLast30d"`
	Raw               json.RawMessage `json:"-"`
}

func (item *DownloadTicket) setRaw(raw json.RawMessage) { item.Raw = raw }

// DownloadLinkRequest는 다운로드 링크를 받을 요청 식별자다. DownloadID는 이력 다운로드 요청 응답에서 받는다.
type DownloadLinkRequest struct{ DownloadID string }

func (request DownloadLinkRequest) validate() error {
	if request.DownloadID == "" || strings.TrimSpace(request.DownloadID) != request.DownloadID {
		return validationError("downloadId is required without surrounding whitespace")
	}
	return nil
}

func (request DownloadLinkRequest) values() url.Values {
	return url.Values{"downloadId": {request.DownloadID}}
}

// DownloadLink는 이력 다운로드 파일 링크다. Status가 "completed"일 때만 URL이 채워지고 링크는 ExpirationTimestamp 이후 만료된다.
type DownloadLink struct {
	DownloadID          string          `json:"downloadId"`
	Status              string          `json:"status"`
	URL                 string          `json:"url"`
	Notified            bool            `json:"notified"`
	ExpirationTimestamp int64           `json:"expirationTimestamp"`
	Raw                 json.RawMessage `json:"-"`
}

func (item *DownloadLink) setRaw(raw json.RawMessage) { item.Raw = raw }

// requestDownload는 이력 다운로드 요청을 접수한다. 월 요청 한도와 IP weight 1000을 소모하므로
// 서명 GET이지만 OperationMutation으로 실행해 자동 재시도하지 않는다.
func (client *Client) requestDownload(ctx context.Context, path string, request DownloadRangeRequest, options ...trade.RequestOption) (DownloadTicket, error) {
	if err := request.validate(); err != nil {
		return DownloadTicket{}, err
	}
	response, err := client.executeSigned(ctx, http.MethodGet, path, request.values(), 1000, 0, credential.PermissionRead, commonexchange.OperationMutation, options...)
	if err != nil {
		return DownloadTicket{}, err
	}
	return decodeObject[DownloadTicket](client, response, commonexchange.OperationMutation)
}

// downloadLink는 접수된 이력 다운로드 요청의 파일 링크를 조회한다.
func (client *Client) downloadLink(ctx context.Context, path string, request DownloadLinkRequest, options ...trade.RequestOption) (DownloadLink, error) {
	if err := request.validate(); err != nil {
		return DownloadLink{}, err
	}
	response, err := client.executeSigned(ctx, http.MethodGet, path, request.values(), 10, 0, credential.PermissionRead, commonexchange.OperationRead, options...)
	if err != nil {
		return DownloadLink{}, err
	}
	return decodeObject[DownloadLink](client, response, commonexchange.OperationRead)
}

// RequestOrderHistoryDownload는 주문 이력 파일 생성을 요청한다. 구간은 최대 1년이다.
// GET /fapi/v1/order/asyn이며 credential.PermissionRead가 필요하고 IP weight는 1000이다.
// 공식 월 한도는 10회이고 웹 다운로드 페이지와 공유하며, SDK는 월 한도를 로컬에서 세지 않는다.
func (client *Client) RequestOrderHistoryDownload(ctx context.Context, request DownloadRangeRequest, options ...trade.RequestOption) (DownloadTicket, error) {
	return client.requestDownload(ctx, "/fapi/v1/order/asyn", request, options...)
}

// OrderHistoryDownloadLink는 주문 이력 파일의 다운로드 링크를 조회한다. 링크는 7일 동안 유효하다.
// GET /fapi/v1/order/asyn/id이며 credential.PermissionRead가 필요하고 IP weight는 10이다.
func (client *Client) OrderHistoryDownloadLink(ctx context.Context, request DownloadLinkRequest, options ...trade.RequestOption) (DownloadLink, error) {
	return client.downloadLink(ctx, "/fapi/v1/order/asyn/id", request, options...)
}

// RequestTradeHistoryDownload는 체결 이력 파일 생성을 요청한다. 구간은 최대 1년이다.
// GET /fapi/v1/trade/asyn이며 credential.PermissionRead가 필요하고 IP weight는 1000이다.
// 공식 월 한도는 5회이고 웹 다운로드 페이지와 공유하며, SDK는 월 한도를 로컬에서 세지 않는다.
func (client *Client) RequestTradeHistoryDownload(ctx context.Context, request DownloadRangeRequest, options ...trade.RequestOption) (DownloadTicket, error) {
	return client.requestDownload(ctx, "/fapi/v1/trade/asyn", request, options...)
}

// TradeHistoryDownloadLink는 체결 이력 파일의 다운로드 링크를 조회한다. 링크는 7일 동안 유효하다.
// GET /fapi/v1/trade/asyn/id이며 credential.PermissionRead가 필요하고 IP weight는 10이다.
func (client *Client) TradeHistoryDownloadLink(ctx context.Context, request DownloadLinkRequest, options ...trade.RequestOption) (DownloadLink, error) {
	return client.downloadLink(ctx, "/fapi/v1/trade/asyn/id", request, options...)
}

// RequestIncomeHistoryDownload는 거래 내역(수입 이력) 파일 생성을 요청한다. 구간은 최대 1년이다.
// GET /fapi/v1/income/asyn이며 credential.PermissionRead가 필요하고 IP weight는 1000이다.
// 공식 월 한도는 100회이고 웹 다운로드 페이지와 USDⓈ-M·COIN-M 계약이 공유하며, SDK는 월 한도를 로컬에서 세지 않는다.
func (client *Client) RequestIncomeHistoryDownload(ctx context.Context, request DownloadRangeRequest, options ...trade.RequestOption) (DownloadTicket, error) {
	return client.requestDownload(ctx, "/fapi/v1/income/asyn", request, options...)
}

// IncomeHistoryDownloadLink는 거래 내역 파일의 다운로드 링크를 조회한다. 링크는 7일 동안 유효하다.
// GET /fapi/v1/income/asyn/id이며 credential.PermissionRead가 필요하고 IP weight는 10이다.
func (client *Client) IncomeHistoryDownloadLink(ctx context.Context, request DownloadLinkRequest, options ...trade.RequestOption) (DownloadLink, error) {
	return client.downloadLink(ctx, "/fapi/v1/income/asyn/id", request, options...)
}

// ClassicPortfolioMarginAccountRequest는 클래식 포트폴리오 마진 계정 정보를 조회할 자산이다.
type ClassicPortfolioMarginAccountRequest struct{ Asset string }

// ClassicPortfolioMarginAccountInfo는 자산별 이체 가능 수량이다.
// MaxWithdrawAmount는 현물 지갑으로 옮길 수 있는 수량이고 MaxWithdrawAmountUSD는 USD 환산 가상 수량이다.
type ClassicPortfolioMarginAccountInfo struct {
	Asset                string          `json:"asset"`
	MaxWithdrawAmount    string          `json:"maxWithdrawAmount"`
	MaxWithdrawAmountUSD string          `json:"maxWithdrawAmountUSD"`
	Raw                  json.RawMessage `json:"-"`
}

func (item *ClassicPortfolioMarginAccountInfo) setRaw(raw json.RawMessage) { item.Raw = raw }

// ClassicPortfolioMarginAccount는 클래식 포트폴리오 마진 계정의 자산별 이체 가능 수량을 조회한다.
// GET /fapi/v1/pmAccountInfo이며 credential.PermissionRead가 필요하고 IP weight는 5다.
func (client *Client) ClassicPortfolioMarginAccount(ctx context.Context, request ClassicPortfolioMarginAccountRequest, options ...trade.RequestOption) (ClassicPortfolioMarginAccountInfo, error) {
	if request.Asset == "" || strings.TrimSpace(request.Asset) != request.Asset {
		return ClassicPortfolioMarginAccountInfo{}, validationError("asset is required without surrounding whitespace")
	}
	response, err := client.executeSigned(ctx, http.MethodGet, "/fapi/v1/pmAccountInfo", url.Values{"asset": {request.Asset}}, 5, 0, credential.PermissionRead, commonexchange.OperationRead, options...)
	if err != nil {
		return ClassicPortfolioMarginAccountInfo{}, err
	}
	return decodeObject[ClassicPortfolioMarginAccountInfo](client, response, commonexchange.OperationRead)
}
