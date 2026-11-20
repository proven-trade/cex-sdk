package okx

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

// PositionMode는 계정의 포지션 모드다.
type PositionMode string

const (
	PositionModeLongShort PositionMode = "long_short_mode"
	PositionModeNet       PositionMode = "net_mode"
)

// MarginAdjustType은 격리 포지션 증거금 조정 방향이다.
type MarginAdjustType string

const (
	MarginAdjustAdd    MarginAdjustType = "add"
	MarginAdjustReduce MarginAdjustType = "reduce"
)

// IsolatedMode는 격리 마진 방식이다.
type IsolatedMode string

const (
	IsolatedModeAutomatic        IsolatedMode = "automatic"
	IsolatedModeAutoTransfersCcy IsolatedMode = "auto_transfers_ccy"
)

// IsolatedModeScope는 격리 마진 방식을 적용할 상품군이다.
type IsolatedModeScope string

const (
	IsolatedModeScopeMargin    IsolatedModeScope = "MARGIN"
	IsolatedModeScopeContracts IsolatedModeScope = "CONTRACTS"
)

// AccountConfig는 계정 설정이다.
type AccountConfig struct {
	UID                  string          `json:"uid"`
	MainUID              string          `json:"mainUid"`
	AccountLevel         string          `json:"acctLv"`
	SelfTradeMode        string          `json:"acctStpMode"`
	PositionMode         PositionMode    `json:"posMode"`
	AutoLoan             bool            `json:"autoLoan"`
	GreeksType           string          `json:"greeksType"`
	FeeType              string          `json:"feeType"`
	Level                string          `json:"level"`
	LevelTemp            string          `json:"levelTmp"`
	ContractIsolatedMode string          `json:"ctIsoMode"`
	MarginIsolatedMode   string          `json:"mgnIsoMode"`
	SpotOffsetType       string          `json:"spotOffsetType"`
	StrategyType         string          `json:"stgyType"`
	RoleType             string          `json:"roleType"`
	TraderInstruments    []string        `json:"traderInsts"`
	SpotRoleType         string          `json:"spotRoleType"`
	SpotTraderInsts      []string        `json:"spotTraderInsts"`
	OptionAuth           string          `json:"opAuth"`
	KYCLevel             string          `json:"kycLv"`
	Label                string          `json:"label"`
	IP                   string          `json:"ip"`
	Permission           string          `json:"perm"`
	LiquidationGear      string          `json:"liquidationGear"`
	EnableSpotBorrow     bool            `json:"enableSpotBorrow"`
	SpotBorrowAutoRepay  bool            `json:"spotBorrowAutoRepay"`
	Type                 string          `json:"type"`
	SettleCurrency       string          `json:"settleCcy"`
	SettleCurrencyList   json.RawMessage `json:"settleCcyList"`
	Raw                  json.RawMessage `json:"-"`
}

// SetPositionModeRequest는 포지션 모드 변경 조건이다.
type SetPositionModeRequest struct {
	PositionMode PositionMode `json:"posMode"`
}

// PositionModeResult는 포지션 모드 변경 결과다.
type PositionModeResult struct {
	PositionMode PositionMode    `json:"posMode"`
	Raw          json.RawMessage `json:"-"`
}

// SetLeverageRequest는 레버리지 설정 조건이다.
// InstrumentID 또는 Currency 중 하나가 필요하다. 격리 long/short 모드에서는 PositionSide가 필수다.
type SetLeverageRequest struct {
	InstrumentID string       `json:"instId,omitempty"`
	Currency     string       `json:"ccy,omitempty"`
	Leverage     string       `json:"lever"`
	MarginMode   TradeMode    `json:"mgnMode"`
	PositionSide PositionSide `json:"posSide,omitempty"`
}

// LeverageResult는 레버리지 설정 결과다.
type LeverageResult struct {
	Leverage     string          `json:"lever"`
	MarginMode   TradeMode       `json:"mgnMode"`
	InstrumentID string          `json:"instId"`
	PositionSide PositionSide    `json:"posSide"`
	Raw          json.RawMessage `json:"-"`
}

// LeverageInfoRequest는 레버리지 조회 조건이다. InstrumentIDs는 최대 20개다.
type LeverageInfoRequest struct {
	InstrumentIDs []string
	Currency      string
	MarginMode    TradeMode
}

// LeverageInfo는 상품 또는 통화별 현재 레버리지다.
type LeverageInfo struct {
	InstrumentID string          `json:"instId"`
	Currency     string          `json:"ccy"`
	MarginMode   TradeMode       `json:"mgnMode"`
	PositionSide PositionSide    `json:"posSide"`
	Leverage     string          `json:"lever"`
	Raw          json.RawMessage `json:"-"`
}

// MaxSizeRequest는 최대 주문 가능 수량 조회 조건이다. InstrumentIDs는 최대 5개다.
type MaxSizeRequest struct {
	InstrumentIDs []string
	TradeMode     TradeMode
	Currency      string
	Price         string
	Leverage      string
}

// MaxSize는 상품별 최대 매수·매도 가능 수량이다.
type MaxSize struct {
	InstrumentID string          `json:"instId"`
	Currency     string          `json:"ccy"`
	MaxBuy       string          `json:"maxBuy"`
	MaxSell      string          `json:"maxSell"`
	Raw          json.RawMessage `json:"-"`
}

// MaxAvailSizeRequest는 최대 가용 수량 조회 조건이다.
type MaxAvailSizeRequest struct {
	InstrumentID string
	TradeMode    TradeMode
	Currency     string
	ReduceOnly   bool
	Price        string
}

// MaxAvailSize는 최대 가용 매수·매도 수량이다.
type MaxAvailSize struct {
	InstrumentID string          `json:"instId"`
	AvailBuy     string          `json:"availBuy"`
	AvailSell    string          `json:"availSell"`
	Raw          json.RawMessage `json:"-"`
}

// AdjustMarginRequest는 격리 포지션 증거금 증감 조건이다.
type AdjustMarginRequest struct {
	InstrumentID string           `json:"instId"`
	PositionSide PositionSide     `json:"posSide"`
	Type         MarginAdjustType `json:"type"`
	Amount       string           `json:"amt"`
	Currency     string           `json:"ccy,omitempty"`
}

// AdjustMarginResult는 증거금 조정 결과다.
type AdjustMarginResult struct {
	InstrumentID string           `json:"instId"`
	PositionSide PositionSide     `json:"posSide"`
	Amount       string           `json:"amt"`
	Type         MarginAdjustType `json:"type"`
	Leverage     string           `json:"leverage"`
	Currency     string           `json:"ccy"`
	Raw          json.RawMessage  `json:"-"`
}

// TradeFeeRequest는 거래 수수료율 조회 조건이다.
// Spot은 InstrumentID, SWAP은 InstrumentID 또는 InstrumentFamily를 사용할 수 있다.
type TradeFeeRequest struct {
	InstrumentType   InstrumentType
	InstrumentID     string
	InstrumentFamily string
}

// TradeFee는 거래 수수료율이다. OKX 부호 규칙상 maker/taker는 음수가 수수료 지불이다.
type TradeFee struct {
	Level          string          `json:"level"`
	InstrumentType InstrumentType  `json:"instType"`
	Taker          string          `json:"taker"`
	Maker          string          `json:"maker"`
	TakerU         string          `json:"takerU"`
	MakerU         string          `json:"makerU"`
	TakerUSDC      string          `json:"takerUSDC"`
	MakerUSDC      string          `json:"makerUSDC"`
	Delivery       string          `json:"delivery"`
	Exercise       string          `json:"exercise"`
	RuleType       string          `json:"ruleType"`
	Category       string          `json:"category"`
	Timestamp      string          `json:"ts"`
	Raw            json.RawMessage `json:"-"`
}

// AccountBillsRequest는 계정 원장(bills) 조회 조건이다.
// Bills는 최근 7일, BillsArchive는 최근 3개월을 조회한다.
type AccountBillsRequest struct {
	InstrumentType InstrumentType
	InstrumentID   string
	Currency       string
	MarginMode     TradeMode
	ContractType   string
	Type           string
	SubType        string
	After          string
	Before         string
	BeginTime      *time.Time
	EndTime        *time.Time
	Limit          int
}

// AccountBill은 계정 원장 한 건이다.
type AccountBill struct {
	InstrumentType InstrumentType  `json:"instType"`
	BillID         string          `json:"billId"`
	Type           string          `json:"type"`
	SubType        string          `json:"subType"`
	Timestamp      string          `json:"ts"`
	BalanceChange  string          `json:"balChg"`
	PosBalChange   string          `json:"posBalChg"`
	Balance        string          `json:"bal"`
	PosBalance     string          `json:"posBal"`
	Size           string          `json:"sz"`
	Price          string          `json:"px"`
	Currency       string          `json:"ccy"`
	PnL            string          `json:"pnl"`
	Fee            string          `json:"fee"`
	MarginMode     TradeMode       `json:"mgnMode"`
	InstrumentID   string          `json:"instId"`
	OrderID        string          `json:"ordId"`
	ExecType       string          `json:"execType"`
	From           string          `json:"from"`
	To             string          `json:"to"`
	Notes          string          `json:"notes"`
	Interest       string          `json:"interest"`
	Tag            string          `json:"tag"`
	FillTime       string          `json:"fillTime"`
	TradeID        string          `json:"tradeId"`
	ClientOrderID  string          `json:"clOrdId"`
	Raw            json.RawMessage `json:"-"`
}

// AccountBillPage는 원장 목록과 원본 응답이다.
type AccountBillPage struct {
	Bills []AccountBill
	Raw   json.RawMessage
}

// PositionsHistoryRequest는 청산 완료 포지션 이력 조회 조건이다.
type PositionsHistoryRequest struct {
	InstrumentType InstrumentType
	InstrumentID   string
	MarginMode     TradeMode
	CloseType      string
	PositionID     string
	After          string
	Before         string
	Limit          int
}

// PositionHistory는 청산 완료된 포지션 한 건이다.
type PositionHistory struct {
	InstrumentType   InstrumentType  `json:"instType"`
	InstrumentID     string          `json:"instId"`
	MarginMode       TradeMode       `json:"mgnMode"`
	Type             string          `json:"type"`
	CreatedTime      string          `json:"cTime"`
	UpdatedTime      string          `json:"uTime"`
	OpenAveragePrice string          `json:"openAvgPx"`
	CloseAveragePx   string          `json:"closeAvgPx"`
	PositionID       string          `json:"posId"`
	OpenMaxPosition  string          `json:"openMaxPos"`
	CloseTotalPos    string          `json:"closeTotalPos"`
	RealizedPnL      string          `json:"realizedPnl"`
	SettledPnL       string          `json:"settledPnl"`
	PnLRatio         string          `json:"pnlRatio"`
	Fee              string          `json:"fee"`
	FundingFee       string          `json:"fundingFee"`
	LiquidationPen   string          `json:"liqPenalty"`
	PnL              string          `json:"pnl"`
	PositionSide     PositionSide    `json:"posSide"`
	Leverage         string          `json:"lever"`
	Direction        string          `json:"direction"`
	TriggerPrice     string          `json:"triggerPx"`
	Underlying       string          `json:"uly"`
	Currency         string          `json:"ccy"`
	Raw              json.RawMessage `json:"-"`
}

// AccountPositionRiskRequest는 계정·포지션 리스크 조회 조건이다.
type AccountPositionRiskRequest struct {
	InstrumentType InstrumentType
}

// AccountRiskBalance는 리스크 산정에 쓰인 통화별 자산이다.
type AccountRiskBalance struct {
	Currency       string `json:"ccy"`
	Equity         string `json:"eq"`
	DiscountEquity string `json:"disEq"`
}

// AccountRiskPosition은 리스크 산정에 쓰인 포지션이다.
type AccountRiskPosition struct {
	InstrumentType InstrumentType `json:"instType"`
	MarginMode     TradeMode      `json:"mgnMode"`
	PositionID     string         `json:"posId"`
	InstrumentID   string         `json:"instId"`
	Position       string         `json:"pos"`
	BaseBalance    string         `json:"baseBal"`
	QuoteBalance   string         `json:"quoteBal"`
	PositionSide   PositionSide   `json:"posSide"`
	PositionCcy    string         `json:"posCcy"`
	Currency       string         `json:"ccy"`
	NotionalCcy    string         `json:"notionalCcy"`
	NotionalUSD    string         `json:"notionalUsd"`
}

// AccountPositionRisk는 계정 수준 조정 자산과 포지션별 리스크 입력값이다.
type AccountPositionRisk struct {
	Timestamp      string                `json:"ts"`
	AdjustedEquity string                `json:"adjEq"`
	BalanceData    []AccountRiskBalance  `json:"balData"`
	PositionData   []AccountRiskPosition `json:"posData"`
	Raw            json.RawMessage       `json:"-"`
}

// SetIsolatedModeRequest는 격리 마진 방식 설정 조건이다.
type SetIsolatedModeRequest struct {
	IsolatedMode IsolatedMode      `json:"isoMode"`
	Type         IsolatedModeScope `json:"type"`
}

// IsolatedModeResult는 격리 마진 방식 설정 결과다.
type IsolatedModeResult struct {
	IsolatedMode IsolatedMode    `json:"isoMode"`
	Raw          json.RawMessage `json:"-"`
}

// MaxWithdrawalRequest는 최대 출금 가능액의 통화 필터다. Currencies는 쉼표로 합쳐 전송한다.
type MaxWithdrawalRequest struct {
	Currencies []string
}

// MaxWithdrawal은 통화별 거래 계정 최대 출금 가능액이다.
type MaxWithdrawal struct {
	Currency          string          `json:"ccy"`
	MaxWithdrawal     string          `json:"maxWd"`
	MaxWithdrawalEx   string          `json:"maxWdEx"`
	SpotOffsetMaxWd   string          `json:"spotOffsetMaxWd"`
	SpotOffsetMaxWdEx string          `json:"spotOffsetMaxWdEx"`
	Raw               json.RawMessage `json:"-"`
}

// AccountConfig는 계정 모드, 포지션 모드, 권한 등 계정 설정을 조회한다.
func (client *Client) AccountConfig(
	ctx context.Context,
	options ...trade.RequestOption,
) (AccountConfig, error) {
	response, data, err := client.tradeRead(
		ctx, "/api/v5/account/config", nil, accountLimit(5, 2*time.Second, ""), options...,
	)
	if err != nil {
		return AccountConfig{}, err
	}
	return tradeDecodeOne(
		client, response, data, commonexchange.OperationRead,
		func(item *AccountConfig, raw []byte) { item.Raw = raw },
	)
}

// SetPositionMode는 계정 포지션 모드를 long/short 또는 net으로 변경한다.
func (client *Client) SetPositionMode(
	ctx context.Context,
	request SetPositionModeRequest,
	options ...trade.RequestOption,
) (PositionModeResult, error) {
	if err := request.validate(); err != nil {
		return PositionModeResult{}, err
	}
	return accountMutateOne(
		ctx, client, "/api/v5/account/set-position-mode", request,
		accountLimit(5, 2*time.Second, ""),
		func(item *PositionModeResult, raw []byte) { item.Raw = raw }, options...,
	)
}

// SetLeverage는 상품 또는 통화 단위 레버리지를 설정한다.
func (client *Client) SetLeverage(
	ctx context.Context,
	request SetLeverageRequest,
	options ...trade.RequestOption,
) (LeverageResult, error) {
	if err := request.validate(); err != nil {
		return LeverageResult{}, err
	}
	return accountMutateOne(
		ctx, client, "/api/v5/account/set-leverage", request,
		accountLimit(20, 2*time.Second, ""),
		func(item *LeverageResult, raw []byte) { item.Raw = raw }, options...,
	)
}

// LeverageInfo는 상품 또는 통화의 현재 레버리지를 조회한다.
func (client *Client) LeverageInfo(
	ctx context.Context,
	request LeverageInfoRequest,
	options ...trade.RequestOption,
) ([]LeverageInfo, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	return accountReadItems(
		ctx, client, "/api/v5/account/leverage-info", request.values(),
		accountLimit(20, 2*time.Second, ""),
		func(item *LeverageInfo, raw []byte) { item.Raw = raw }, options...,
	)
}

// MaxSize는 현재 증거금으로 주문 가능한 최대 매수·매도 수량을 조회한다.
func (client *Client) MaxSize(
	ctx context.Context,
	request MaxSizeRequest,
	options ...trade.RequestOption,
) ([]MaxSize, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	return accountReadItems(
		ctx, client, "/api/v5/account/max-size", request.values(),
		accountLimit(20, 2*time.Second, ""),
		func(item *MaxSize, raw []byte) { item.Raw = raw }, options...,
	)
}

// MaxAvailSize는 주문에 쓸 수 있는 최대 가용 수량을 조회한다.
func (client *Client) MaxAvailSize(
	ctx context.Context,
	request MaxAvailSizeRequest,
	options ...trade.RequestOption,
) ([]MaxAvailSize, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	return accountReadItems(
		ctx, client, "/api/v5/account/max-avail-size", request.values(),
		accountLimit(20, 2*time.Second, ""),
		func(item *MaxAvailSize, raw []byte) { item.Raw = raw }, options...,
	)
}

// AdjustPositionMargin은 격리 포지션의 증거금을 증가 또는 감소한다.
func (client *Client) AdjustPositionMargin(
	ctx context.Context,
	request AdjustMarginRequest,
	options ...trade.RequestOption,
) (AdjustMarginResult, error) {
	if err := request.validate(); err != nil {
		return AdjustMarginResult{}, err
	}
	return accountMutateOne(
		ctx, client, "/api/v5/account/position/margin-balance", request,
		accountLimit(20, 2*time.Second, ""),
		func(item *AdjustMarginResult, raw []byte) { item.Raw = raw }, options...,
	)
}

// TradeFee는 상품 유형별 수수료율을 조회한다.
func (client *Client) TradeFee(
	ctx context.Context,
	request TradeFeeRequest,
	options ...trade.RequestOption,
) (TradeFee, error) {
	if err := request.validate(); err != nil {
		return TradeFee{}, err
	}
	response, data, err := client.tradeRead(
		ctx, "/api/v5/account/trade-fee", request.values(), accountLimit(5, 2*time.Second, ""), options...,
	)
	if err != nil {
		return TradeFee{}, err
	}
	return tradeDecodeOne(
		client, response, data, commonexchange.OperationRead,
		func(item *TradeFee, raw []byte) { item.Raw = raw },
	)
}

// AccountBills는 최근 7일 계정 원장을 조회한다.
func (client *Client) AccountBills(
	ctx context.Context,
	request AccountBillsRequest,
	options ...trade.RequestOption,
) (AccountBillPage, error) {
	return client.accountBills(
		ctx, "/api/v5/account/bills", accountLimit(5, time.Second, ""), request, options...,
	)
}

// AccountBillsArchive는 최근 3개월 계정 원장을 조회한다.
func (client *Client) AccountBillsArchive(
	ctx context.Context,
	request AccountBillsRequest,
	options ...trade.RequestOption,
) (AccountBillPage, error) {
	return client.accountBills(
		ctx, "/api/v5/account/bills-archive", accountLimit(5, 2*time.Second, ""), request, options...,
	)
}

func (client *Client) accountBills(
	ctx context.Context,
	path string,
	limit endpointLimit,
	request AccountBillsRequest,
	options ...trade.RequestOption,
) (AccountBillPage, error) {
	if err := request.validate(); err != nil {
		return AccountBillPage{}, err
	}
	response, data, err := client.tradeRead(ctx, path, request.values(), limit, options...)
	if err != nil {
		return AccountBillPage{}, err
	}
	bills, err := tradeDecodeItems(
		client, response, data, commonexchange.OperationRead,
		func(item *AccountBill, raw []byte) { item.Raw = raw },
	)
	if err != nil {
		return AccountBillPage{}, err
	}
	return AccountBillPage{Bills: bills, Raw: cloneBytes(data)}, nil
}

// PositionsHistory는 청산 완료된 포지션 이력을 조회한다.
func (client *Client) PositionsHistory(
	ctx context.Context,
	request PositionsHistoryRequest,
	options ...trade.RequestOption,
) ([]PositionHistory, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	return accountReadItems(
		ctx, client, "/api/v5/account/positions-history", request.values(),
		accountLimit(10, 2*time.Second, ""),
		func(item *PositionHistory, raw []byte) { item.Raw = raw }, options...,
	)
}

// AccountPositionRisk는 계정 조정 자산과 포지션 리스크 입력값을 조회한다.
func (client *Client) AccountPositionRisk(
	ctx context.Context,
	request AccountPositionRiskRequest,
	options ...trade.RequestOption,
) (AccountPositionRisk, error) {
	if err := request.validate(); err != nil {
		return AccountPositionRisk{}, err
	}
	response, data, err := client.tradeRead(
		ctx, "/api/v5/account/account-position-risk", request.values(),
		accountLimit(10, 2*time.Second, ""), options...,
	)
	if err != nil {
		return AccountPositionRisk{}, err
	}
	return tradeDecodeOne(
		client, response, data, commonexchange.OperationRead,
		func(item *AccountPositionRisk, raw []byte) { item.Raw = raw },
	)
}

// SetIsolatedMode는 격리 마진 자동 이체 방식을 설정한다.
func (client *Client) SetIsolatedMode(
	ctx context.Context,
	request SetIsolatedModeRequest,
	options ...trade.RequestOption,
) (IsolatedModeResult, error) {
	if err := request.validate(); err != nil {
		return IsolatedModeResult{}, err
	}
	return accountMutateOne(
		ctx, client, "/api/v5/account/set-isolated-mode", request,
		accountLimit(5, 2*time.Second, ""),
		func(item *IsolatedModeResult, raw []byte) { item.Raw = raw }, options...,
	)
}

// MaxWithdrawal은 거래 계정에서 펀딩 계정으로 이체 가능한 통화별 최대 금액을 조회한다.
func (client *Client) MaxWithdrawal(
	ctx context.Context,
	request MaxWithdrawalRequest,
	options ...trade.RequestOption,
) ([]MaxWithdrawal, error) {
	if err := (BalanceRequest{Currencies: request.Currencies}).validate(); err != nil {
		return nil, err
	}
	return accountReadItems(
		ctx, client, "/api/v5/account/max-withdrawal",
		(BalanceRequest{Currencies: request.Currencies}).values(),
		accountLimit(20, 2*time.Second, ""),
		func(item *MaxWithdrawal, raw []byte) { item.Raw = raw }, options...,
	)
}

// accountReadItems는 서명된 GET 응답 data 배열을 항목 목록으로 decode한다.
func accountReadItems[T any](
	ctx context.Context,
	client *Client,
	path string,
	query url.Values,
	limit endpointLimit,
	setRaw func(*T, []byte),
	options ...trade.RequestOption,
) ([]T, error) {
	response, data, err := client.tradeRead(ctx, path, query, limit, options...)
	if err != nil {
		return nil, err
	}
	return tradeDecodeItems(client, response, data, commonexchange.OperationRead, setRaw)
}

// accountMutateOne은 서명된 POST를 Trade 권한으로 실행하고 첫 항목을 decode한다.
// 자동 재시도하지 않는다.
func accountMutateOne[T any](
	ctx context.Context,
	client *Client,
	path string,
	request any,
	limit endpointLimit,
	setRaw func(*T, []byte),
	options ...trade.RequestOption,
) (T, error) {
	var zero T
	body, err := encodeBody(request)
	if err != nil {
		return zero, err
	}
	response, data, err := client.tradeMutate(ctx, path, body, limit, options...)
	if err != nil {
		return zero, err
	}
	return tradeDecodeOne(client, response, data, commonexchange.OperationMutation, setRaw)
}

func (request SetPositionModeRequest) validate() error {
	if request.PositionMode != PositionModeLongShort && request.PositionMode != PositionModeNet {
		return validationError("posMode must be long_short_mode or net_mode")
	}
	return nil
}

func (request SetLeverageRequest) validate() error {
	if err := validateOptionalText("instId", request.InstrumentID); err != nil {
		return err
	}
	if err := validateOptionalText("ccy", request.Currency); err != nil {
		return err
	}
	if (request.InstrumentID == "") == (request.Currency == "") {
		return validationError("exactly one of instId or ccy is required")
	}
	if err := validatePositiveDecimal("lever", request.Leverage); err != nil {
		return err
	}
	if err := validateMarginMode(request.MarginMode); err != nil {
		return err
	}
	if request.PositionSide != "" && !request.PositionSide.valid() {
		return validationError("unsupported position side %q", request.PositionSide)
	}
	return nil
}

func (request LeverageInfoRequest) validate() error {
	if len(request.InstrumentIDs) > 20 {
		return validationError("leverage info accepts at most 20 instruments")
	}
	if err := validateCSVItems("instId", request.InstrumentIDs); err != nil {
		return err
	}
	if err := validateOptionalText("ccy", request.Currency); err != nil {
		return err
	}
	if (len(request.InstrumentIDs) == 0) == (request.Currency == "") {
		return validationError("exactly one of instId or ccy is required")
	}
	return validateMarginMode(request.MarginMode)
}

func (request LeverageInfoRequest) values() url.Values {
	values := make(url.Values)
	if len(request.InstrumentIDs) > 0 {
		values.Set("instId", strings.Join(request.InstrumentIDs, ","))
	}
	setIfNotEmpty(values, "ccy", request.Currency)
	values.Set("mgnMode", string(request.MarginMode))
	return values
}

func validateTradeModeAny(mode TradeMode) error {
	if mode != TradeModeCash && mode != TradeModeCross && mode != TradeModeIsolated {
		return validationError("trade mode must be cash, cross or isolated")
	}
	return nil
}

func (request MaxSizeRequest) validate() error {
	if len(request.InstrumentIDs) == 0 || len(request.InstrumentIDs) > 5 {
		return validationError("max size requires between 1 and 5 instruments")
	}
	if err := validateCSVItems("instId", request.InstrumentIDs); err != nil {
		return err
	}
	if err := validateTradeModeAny(request.TradeMode); err != nil {
		return err
	}
	if err := validateOptionalText("ccy", request.Currency); err != nil {
		return err
	}
	if request.Price != "" {
		if err := validatePositiveDecimal("px", request.Price); err != nil {
			return err
		}
	}
	if request.Leverage != "" {
		return validatePositiveDecimal("leverage", request.Leverage)
	}
	return nil
}

func (request MaxSizeRequest) values() url.Values {
	values := make(url.Values)
	values.Set("instId", strings.Join(request.InstrumentIDs, ","))
	values.Set("tdMode", string(request.TradeMode))
	setIfNotEmpty(values, "ccy", request.Currency)
	setIfNotEmpty(values, "px", request.Price)
	setIfNotEmpty(values, "leverage", request.Leverage)
	return values
}

func (request MaxAvailSizeRequest) validate() error {
	if err := validateRequiredText("instId", request.InstrumentID); err != nil {
		return err
	}
	if err := validateTradeModeAny(request.TradeMode); err != nil {
		return err
	}
	if err := validateOptionalText("ccy", request.Currency); err != nil {
		return err
	}
	if request.Price != "" {
		return validatePositiveDecimal("px", request.Price)
	}
	return nil
}

func (request MaxAvailSizeRequest) values() url.Values {
	values := make(url.Values)
	values.Set("instId", request.InstrumentID)
	values.Set("tdMode", string(request.TradeMode))
	setIfNotEmpty(values, "ccy", request.Currency)
	if request.ReduceOnly {
		values.Set("reduceOnly", "true")
	}
	setIfNotEmpty(values, "px", request.Price)
	return values
}

func (request AdjustMarginRequest) validate() error {
	if err := validateRequiredText("instId", request.InstrumentID); err != nil {
		return err
	}
	if !request.PositionSide.valid() {
		return validationError("posSide must be long, short or net")
	}
	if request.Type != MarginAdjustAdd && request.Type != MarginAdjustReduce {
		return validationError("type must be add or reduce")
	}
	if err := validatePositiveDecimal("amt", request.Amount); err != nil {
		return err
	}
	return validateOptionalText("ccy", request.Currency)
}

func (request TradeFeeRequest) validate() error {
	if err := validateRequiredText("instType", string(request.InstrumentType)); err != nil {
		return err
	}
	if err := validateOptionalText("instId", request.InstrumentID); err != nil {
		return err
	}
	if err := validateOptionalText("instFamily", request.InstrumentFamily); err != nil {
		return err
	}
	if request.InstrumentID != "" && request.InstrumentFamily != "" {
		return validationError("instId and instFamily cannot be combined")
	}
	return nil
}

func (request TradeFeeRequest) values() url.Values {
	values := instrumentTypeValues(request.InstrumentType)
	setIfNotEmpty(values, "instId", request.InstrumentID)
	setIfNotEmpty(values, "instFamily", request.InstrumentFamily)
	return values
}

func (request AccountBillsRequest) validate() error {
	for name, value := range map[string]string{
		"instType": string(request.InstrumentType), "instId": request.InstrumentID,
		"ccy": request.Currency, "ctType": request.ContractType, "type": request.Type,
		"subType": request.SubType,
	} {
		if err := validateOptionalText(name, value); err != nil {
			return err
		}
	}
	if request.MarginMode != "" && request.MarginMode != TradeModeCross && request.MarginMode != TradeModeIsolated {
		return validationError("margin mode must be cross or isolated")
	}
	if request.BeginTime != nil && request.EndTime != nil && request.BeginTime.After(*request.EndTime) {
		return validationError("bills begin time cannot be after end time")
	}
	return validateOrderPage(request.After, request.Before, request.Limit)
}

func (request AccountBillsRequest) values() url.Values {
	values := make(url.Values)
	setIfNotEmpty(values, "instType", string(request.InstrumentType))
	setIfNotEmpty(values, "instId", request.InstrumentID)
	setIfNotEmpty(values, "ccy", request.Currency)
	setIfNotEmpty(values, "mgnMode", string(request.MarginMode))
	setIfNotEmpty(values, "ctType", request.ContractType)
	setIfNotEmpty(values, "type", request.Type)
	setIfNotEmpty(values, "subType", request.SubType)
	setIfNotEmpty(values, "after", request.After)
	setIfNotEmpty(values, "before", request.Before)
	setTime(values, "begin", request.BeginTime)
	setTime(values, "end", request.EndTime)
	setPositiveInt(values, "limit", request.Limit)
	return values
}

func (request PositionsHistoryRequest) validate() error {
	for name, value := range map[string]string{
		"instType": string(request.InstrumentType), "instId": request.InstrumentID,
		"type": request.CloseType, "posId": request.PositionID,
	} {
		if err := validateOptionalText(name, value); err != nil {
			return err
		}
	}
	if request.MarginMode != "" && request.MarginMode != TradeModeCross && request.MarginMode != TradeModeIsolated {
		return validationError("margin mode must be cross or isolated")
	}
	return validateOrderPage(request.After, request.Before, request.Limit)
}

func (request PositionsHistoryRequest) values() url.Values {
	values := make(url.Values)
	setIfNotEmpty(values, "instType", string(request.InstrumentType))
	setIfNotEmpty(values, "instId", request.InstrumentID)
	setIfNotEmpty(values, "mgnMode", string(request.MarginMode))
	setIfNotEmpty(values, "type", request.CloseType)
	setIfNotEmpty(values, "posId", request.PositionID)
	setIfNotEmpty(values, "after", request.After)
	setIfNotEmpty(values, "before", request.Before)
	setPositiveInt(values, "limit", request.Limit)
	return values
}

func (request AccountPositionRiskRequest) validate() error {
	return validateOptionalText("instType", string(request.InstrumentType))
}

func (request AccountPositionRiskRequest) values() url.Values {
	values := make(url.Values)
	setIfNotEmpty(values, "instType", string(request.InstrumentType))
	return values
}

func (request SetIsolatedModeRequest) validate() error {
	if request.IsolatedMode != IsolatedModeAutomatic && request.IsolatedMode != IsolatedModeAutoTransfersCcy {
		return validationError("isoMode must be automatic or auto_transfers_ccy")
	}
	if request.Type != IsolatedModeScopeMargin && request.Type != IsolatedModeScopeContracts {
		return validationError("type must be MARGIN or CONTRACTS")
	}
	if request.IsolatedMode == IsolatedModeAutoTransfersCcy && request.Type != IsolatedModeScopeMargin {
		return validationError("auto_transfers_ccy is applicable only to MARGIN")
	}
	return nil
}

func validateCSVItems(name string, values []string) error {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if err := validateRequiredText(name, value); err != nil {
			return err
		}
		if strings.Contains(value, ",") {
			return validationError("%s cannot contain a comma", name)
		}
		if _, exists := seen[value]; exists {
			return validationError("duplicate %s %q", name, value)
		}
		seen[value] = struct{}{}
	}
	return nil
}
