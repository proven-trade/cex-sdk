package kraken

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

const (
	maxQueryTradeIDs  = 20
	maxQueryLedgerIDs = 20
)

// LedgerType은 원장 조회의 유형 필터다.
type LedgerType string

const (
	LedgerTypeAll        LedgerType = "all"
	LedgerTypeTrade      LedgerType = "trade"
	LedgerTypeDeposit    LedgerType = "deposit"
	LedgerTypeWithdrawal LedgerType = "withdrawal"
	LedgerTypeTransfer   LedgerType = "transfer"
	LedgerTypeMargin     LedgerType = "margin"
	LedgerTypeAdjustment LedgerType = "adjustment"
	LedgerTypeRollover   LedgerType = "rollover"
	LedgerTypeCredit     LedgerType = "credit"
	LedgerTypeSettled    LedgerType = "settled"
	LedgerTypeStaking    LedgerType = "staking"
	LedgerTypeDividend   LedgerType = "dividend"
	LedgerTypeSale       LedgerType = "sale"
	LedgerTypeNFTRebate  LedgerType = "nft_rebate"
)

// pageCursor는 응답의 다음 페이지 cursor다. 더 이상 페이지가 없으면 Next가 비어 있다.
type pageCursor struct {
	Next string `json:"next"`
}

// TradeBalanceRequest는 증거금 거래 잔고 요약 조회 조건이다. Asset을 생략하면 ZUSD 기준이다.
type TradeBalanceRequest struct {
	Asset string
}

// TradeBalance는 담보 잔고, 포지션 평가, 자본과 증거금 수준 요약이다. 금액은 Asset 단위다.
type TradeBalance struct {
	EquivalentBalance   string
	Balance             string
	MarginAmount        string
	UnrealizedPnL       string
	CostBasis           string
	FloatingValuation   string
	Equity              string
	FreeMargin          string
	MarginFreeForOrders string
	MarginLevel         string
	UnexecutedValue     string
	Raw                 json.RawMessage
}

// UnmarshalJSON은 숫자 또는 문자열인 Kraken 금액 필드를 decimal 문자열로 보존한다.
func (balance *TradeBalance) UnmarshalJSON(data []byte) error {
	reader, err := newScalarReader(data)
	if err != nil {
		return err
	}
	reader.read(&balance.EquivalentBalance, "eb")
	reader.read(&balance.Balance, "tb")
	reader.read(&balance.MarginAmount, "m")
	reader.read(&balance.UnrealizedPnL, "n")
	reader.read(&balance.CostBasis, "c")
	reader.read(&balance.FloatingValuation, "v")
	reader.read(&balance.Equity, "e")
	reader.read(&balance.FreeMargin, "mf")
	reader.read(&balance.MarginFreeForOrders, "mfo")
	reader.read(&balance.MarginLevel, "ml")
	reader.read(&balance.UnexecutedValue, "uv")
	if reader.err != nil {
		return reader.err
	}
	balance.Raw = cloneBytes(data)
	return nil
}

// QueryTradesRequest는 체결 ID로 최대 20건을 조회한다.
type QueryTradesRequest struct {
	TransactionIDs []string
	// IncludeTrades가 true이면 포지션과 관련된 체결을 함께 반환한다.
	IncludeTrades bool
	// IncludeLedgers가 true이면 체결과 연결된 원장 ID를 함께 반환한다.
	IncludeLedgers bool
}

// OpenPositionsRequest는 증거금 미결제 포지션 조회 조건이다.
type OpenPositionsRequest struct {
	TransactionIDs []string
	// IncludeCalculations가 true이면 잔여 포지션의 평가액과 미실현 손익(P&L)을 계산해 반환한다.
	IncludeCalculations bool
	// ConsolidateByMarket이 true이면 상품별로 합산한 포지션을 반환한다.
	ConsolidateByMarket bool
}

// Position은 증거금 미결제 포지션 한 건이다.
type Position struct {
	TransactionID string          `json:"-"`
	OrderID       string          `json:"ordertxid"`
	AssetClass    string          `json:"class"`
	Status        string          `json:"posstatus"`
	Pair          string          `json:"pair"`
	Time          float64         `json:"time"`
	Side          Side            `json:"type"`
	OrderType     OrderType       `json:"ordertype"`
	Cost          string          `json:"cost"`
	Fee           string          `json:"fee"`
	Volume        string          `json:"vol"`
	ClosedVolume  string          `json:"vol_closed"`
	Margin        string          `json:"margin"`
	Value         string          `json:"value"`
	Net           string          `json:"net"`
	Terms         string          `json:"terms"`
	RolloverTime  string          `json:"rollovertm"`
	Misc          string          `json:"misc"`
	OrderFlags    string          `json:"oflags"`
	Raw           json.RawMessage `json:"-"`
}

// ConsolidatedPosition은 상품별로 합산한 미결제 포지션이다.
type ConsolidatedPosition struct {
	Pair         string
	AssetClass   string
	Positions    string
	Side         string
	Leverage     string
	Cost         string
	Fee          string
	Volume       string
	ClosedVolume string
	Margin       string
	Value        string
	Net          string
	Raw          json.RawMessage
}

// UnmarshalJSON은 숫자 또는 문자열인 Kraken 수치 필드를 문자열로 보존한다.
func (position *ConsolidatedPosition) UnmarshalJSON(data []byte) error {
	reader, err := newScalarReader(data)
	if err != nil {
		return err
	}
	reader.read(&position.Pair, "pair")
	reader.read(&position.AssetClass, "class")
	reader.read(&position.Positions, "positions")
	reader.read(&position.Side, "type")
	reader.read(&position.Leverage, "leverage")
	reader.read(&position.Cost, "cost")
	reader.read(&position.Fee, "fee")
	reader.read(&position.Volume, "vol")
	reader.read(&position.ClosedVolume, "vol_closed")
	reader.read(&position.Margin, "margin")
	reader.read(&position.Value, "value")
	reader.read(&position.Net, "net")
	if reader.err != nil {
		return reader.err
	}
	position.Raw = cloneBytes(data)
	return nil
}

// OpenPositions는 미결제 포지션 조회 결과다. ConsolidateByMarket 여부에 따라 한쪽만 채워진다.
type OpenPositions struct {
	Positions    []Position
	Consolidated []ConsolidatedPosition
	Raw          json.RawMessage
}

// LedgersRequest는 원장 조회 조건이다. 한 번에 최대 50건이 반환되며 cursor로 다음 페이지를 조회한다.
type LedgersRequest struct {
	// Assets가 비어 있으면 전체 자산을 조회한다.
	Assets []string
	Type   LedgerType
	// Start는 제외, End는 포함하는 Unix 초 경계다.
	Start *time.Time
	End   *time.Time
	// WithCursor가 true이면 cursor 방식 페이지네이션을 쓰며 Count는 0으로 반환된다.
	WithCursor bool
	Cursor     string
	// WithoutCount가 true이면 전체 개수 조회를 생략해 응답이 빨라진다.
	WithoutCount bool
}

// LedgerEntry는 원장 한 건이다.
type LedgerEntry struct {
	ID          string          `json:"-"`
	ReferenceID string          `json:"refid"`
	Time        float64         `json:"time"`
	Type        string          `json:"type"`
	Subtype     string          `json:"subtype"`
	AssetClass  string          `json:"aclass"`
	Asset       string          `json:"asset"`
	Amount      string          `json:"amount"`
	Fee         string          `json:"fee"`
	Balance     string          `json:"balance"`
	AmountToken string          `json:"amount_token"`
	Raw         json.RawMessage `json:"-"`
}

// LedgerPage는 원장 목록과 다음 페이지 cursor다. Entries는 원장 ID 순서다.
type LedgerPage struct {
	Entries    []LedgerEntry
	Count      int
	NextCursor string
	Raw        json.RawMessage
}

// QueryLedgersRequest는 원장 ID로 최대 20건을 조회한다.
type QueryLedgersRequest struct {
	IDs []string
}

// TradeVolumeRequest는 30일 거래량과 수수료 등급 조회 조건이다.
type TradeVolumeRequest struct {
	// Pairs를 지정하면 상품별 maker·taker 수수료 등급을 함께 반환한다.
	Pairs []string
	// IncludeFeeSchedule이 true이면 상품별 전체 수수료표를 함께 반환한다.
	IncludeFeeSchedule bool
}

// FeeTier는 수수료율과 거래량 등급 정보다. 수수료는 퍼센트 문자열이다.
type FeeTier struct {
	Fee               string `json:"fee"`
	MinFee            string `json:"minfee"`
	MaxFee            string `json:"maxfee"`
	NextFee           string `json:"nextfee"`
	TierVolume        string `json:"tiervolume"`
	TierFuturesVolume string `json:"tierfuturesvolume"`
	NextVolume        string `json:"nextvolume"`
	NextFuturesVolume string `json:"nextfuturesvolume"`
	VolumeOffset      string `json:"volumeoffset"`
}

// TradeVolumeInputs는 수수료 등급 조건에 쓰인 집계 입력값이다.
type TradeVolumeInputs struct {
	SpotVolume30Days    string `json:"domain_spot_volume_30d"`
	FuturesVolume30Days string `json:"domain_futures_volume_30d"`
	AssetsOnPlatform    string `json:"domain_assets_on_platform"`
}

// SubaccountVolume은 서브계정별 거래량이다. 마스터 계정에만 반환된다.
type SubaccountVolume struct {
	IIBAN  string `json:"iiban"`
	Volume string `json:"volume"`
}

// FeeScheduleTier는 수수료표의 등급 하나다.
type FeeScheduleTier struct {
	MakerFee         string `json:"maker_fee"`
	TakerFee         string `json:"taker_fee"`
	MinSpotVolume    string `json:"min_spot_volume"`
	MinFuturesVolume string `json:"min_futures_volume"`
	MinAssets        string `json:"min_assets_on_platform"`
	Active           bool   `json:"active"`
}

// FeeSchedule은 상품 하나의 전체 수수료표다.
type FeeSchedule struct {
	Pair  string            `json:"pair"`
	Class string            `json:"class"`
	Tiers []FeeScheduleTier `json:"tiers"`
}

// TradeVolume은 30일 거래량과 수수료 등급이다. Fees는 taker, FeesMaker는 maker 수수료다.
type TradeVolume struct {
	Currency          string             `json:"currency"`
	AssetClass        string             `json:"asset_class"`
	Volume            string             `json:"volume"`
	Inputs            TradeVolumeInputs  `json:"inputs"`
	Fees              map[string]FeeTier `json:"fees"`
	FeesMaker         map[string]FeeTier `json:"fees_maker"`
	SubaccountVolumes []SubaccountVolume `json:"volume_subaccounts"`
	Schedules         []FeeSchedule      `json:"schedules"`
	Raw               json.RawMessage    `json:"-"`
}

// OrderAmendsRequest는 주문 정정 이력 조회 조건이다.
type OrderAmendsRequest struct {
	OrderID string
}

// OrderAmend는 주문 정정 이력 한 건이다. AmendType은 original, user, restated 중 하나다.
type OrderAmend struct {
	AmendID           string `json:"amend_id"`
	AmendType         string `json:"amend_type"`
	OrderQuantity     string `json:"order_qty"`
	DisplayQuantity   string `json:"display_qty"`
	RemainingQuantity string `json:"remaining_qty"`
	LimitPrice        string `json:"limit_price"`
	TriggerPrice      string `json:"trigger_price"`
	Reason            string `json:"reason"`
	PostOnly          bool   `json:"post_only"`
	Timestamp         int64  `json:"timestamp"`
}

// OrderAmends는 주문 정정 이력이다. Count는 원래 주문을 포함한 전체 거래 수다.
type OrderAmends struct {
	Count  int             `json:"count"`
	Amends []OrderAmend    `json:"amends"`
	Raw    json.RawMessage `json:"-"`
}

// CreditLineAsset은 자산별 신용 한도 정보다.
type CreditLineAsset struct {
	Balance         string
	HoldTrade       string
	Credit          string
	CreditUsed      string
	CollateralValue string
	RolloverFees    string
	ReserveFees     string
	Raw             json.RawMessage
}

// UnmarshalJSON은 숫자 또는 문자열인 Kraken 수치 필드를 문자열로 보존한다.
func (asset *CreditLineAsset) UnmarshalJSON(data []byte) error {
	reader, err := newScalarReader(data)
	if err != nil {
		return err
	}
	reader.read(&asset.Balance, "balance")
	reader.read(&asset.HoldTrade, "hold_trade")
	reader.read(&asset.Credit, "credit")
	reader.read(&asset.CreditUsed, "credit_used")
	reader.read(&asset.CollateralValue, "collateral_value")
	reader.read(&asset.RolloverFees, "rollover_fees")
	reader.read(&asset.ReserveFees, "reserve_fees")
	if reader.err != nil {
		return reader.err
	}
	asset.Raw = cloneBytes(data)
	return nil
}

// CreditLimitsMonitor는 모든 자산을 합산한 신용 한도 요약이다.
type CreditLimitsMonitor struct {
	TotalCreditUSD          string
	TotalCreditUsedUSD      string
	TotalCollateralValueUSD string
	EquityUSD               string
	OngoingBalance          string
	DebtToEquity            string
}

// UnmarshalJSON은 숫자 또는 문자열인 Kraken 수치 필드를 문자열로 보존한다.
func (monitor *CreditLimitsMonitor) UnmarshalJSON(data []byte) error {
	reader, err := newScalarReader(data)
	if err != nil {
		return err
	}
	reader.read(&monitor.TotalCreditUSD, "total_credit_usd")
	reader.read(&monitor.TotalCreditUsedUSD, "total_credit_used_usd")
	reader.read(&monitor.TotalCollateralValueUSD, "total_collateral_value_usd")
	reader.read(&monitor.EquityUSD, "equity_usd")
	reader.read(&monitor.OngoingBalance, "ongoing_balance")
	reader.read(&monitor.DebtToEquity, "debt_to_equity")
	return reader.err
}

// CreditLines는 VIP 신용 한도 상세다. 신용 한도가 없는 계정은 AssetDetails가 비어 있다.
type CreditLines struct {
	AssetDetails  map[string]CreditLineAsset `json:"asset_details"`
	LimitsMonitor CreditLimitsMonitor        `json:"limits_monitor"`
	Raw           json.RawMessage            `json:"-"`
}

// APIKeyInfoRequest는 요청에 쓰인 API Key 정보 조회 조건이다.
type APIKeyInfoRequest struct {
	// OTP는 API Key에 2FA가 설정된 경우에만 필요하다.
	OTP string
}

// APIKeyInfo는 요청에 쓰인 API Key의 이름, 권한, 제한과 사용 시각이다. 시각은 Unix 초 문자열이다.
type APIKeyInfo struct {
	Name         string
	Key          string
	Nonce        string
	NonceWindow  string
	Permissions  []string
	IIBAN        string
	ValidUntil   string
	QueryFrom    string
	QueryTo      string
	CreatedTime  string
	ModifiedTime string
	IPAllowlist  []string
	LastUsed     string
	Raw          json.RawMessage
}

// UnmarshalJSON은 공식 문서가 snake_case 스키마와 camelCase 예시를 함께 싣고 있어 두 표기를 모두 받는다.
func (info *APIKeyInfo) UnmarshalJSON(data []byte) error {
	reader, err := newScalarReader(data)
	if err != nil {
		return err
	}
	reader.read(&info.Name, "api_key_name", "apiKeyName")
	reader.read(&info.Key, "api_key", "apiKey")
	reader.read(&info.Nonce, "nonce")
	reader.read(&info.NonceWindow, "nonce_window", "nonceWindow")
	reader.read(&info.IIBAN, "iban")
	reader.read(&info.ValidUntil, "valid_until", "validUntil")
	reader.read(&info.QueryFrom, "query_from", "queryFrom")
	reader.read(&info.QueryTo, "query_to", "queryTo")
	reader.read(&info.CreatedTime, "created_time", "createdTime")
	reader.read(&info.ModifiedTime, "modified_time", "modifiedTime")
	reader.read(&info.LastUsed, "last_used", "lastUsed")
	if reader.err != nil {
		return reader.err
	}
	var lists struct {
		Permissions      []string `json:"permissions"`
		IPAllowlist      []string `json:"ip_allowlist"`
		IPAllowlistCamel []string `json:"ipAllowlist"`
	}
	if err := json.Unmarshal(data, &lists); err != nil {
		return err
	}
	info.Permissions = lists.Permissions
	info.IPAllowlist = lists.IPAllowlist
	if info.IPAllowlist == nil {
		info.IPAllowlist = lists.IPAllowlistCamel
	}
	info.Raw = cloneBytes(data)
	return nil
}

// WalletAccountFlags는 지갑 계정의 생성 주체와 활성 여부다.
type WalletAccountFlags struct {
	UserDefined bool `json:"user_defined"`
	Active      bool `json:"active"`
}

// WalletAccount는 지갑 계정 한 건이다. AccountID는 다른 endpoint의 account_id로 쓰인다.
type WalletAccount struct {
	AccountID string             `json:"account_id"`
	Flags     WalletAccountFlags `json:"flags"`
	Status    string             `json:"status"`
	Type      string             `json:"type"`
	Name      string             `json:"name"`
}

// WalletAccountPage는 지갑 계정 목록과 다음 페이지 cursor다.
type WalletAccountPage struct {
	Accounts   []WalletAccount
	NextCursor string
	Raw        json.RawMessage
}

func (request TradeBalanceRequest) validate() error {
	if request.Asset == "" {
		return nil
	}
	return validateAssetName("asset", request.Asset)
}

func (request TradeBalanceRequest) values() url.Values {
	values := make(url.Values)
	setIfNotEmpty(values, "asset", request.Asset)
	return values
}

func (request QueryTradesRequest) validate() error {
	return validateIDList("trade transaction ID", request.TransactionIDs, 1, maxQueryTradeIDs)
}

func (request QueryTradesRequest) values() url.Values {
	values := url.Values{"txid": {strings.Join(request.TransactionIDs, ",")}}
	setBool(values, "trades", request.IncludeTrades)
	setBool(values, "ledgers", request.IncludeLedgers)
	return values
}

func (request OpenPositionsRequest) validate() error {
	if len(request.TransactionIDs) == 0 {
		return nil
	}
	return validateIDList("position transaction ID", request.TransactionIDs, 1, len(request.TransactionIDs))
}

func (request OpenPositionsRequest) values() url.Values {
	values := make(url.Values)
	if len(request.TransactionIDs) > 0 {
		values.Set("txid", strings.Join(request.TransactionIDs, ","))
	}
	setBool(values, "docalcs", request.IncludeCalculations)
	if request.ConsolidateByMarket {
		values.Set("consolidation", "market")
	}
	return values
}

func (request LedgersRequest) validate() error {
	if err := validateAssetNames("asset", request.Assets, false); err != nil {
		return err
	}
	switch request.Type {
	case "", LedgerTypeAll, LedgerTypeTrade, LedgerTypeDeposit, LedgerTypeWithdrawal, LedgerTypeTransfer,
		LedgerTypeMargin, LedgerTypeAdjustment, LedgerTypeRollover, LedgerTypeCredit, LedgerTypeSettled,
		LedgerTypeStaking, LedgerTypeDividend, LedgerTypeSale, LedgerTypeNFTRebate:
	default:
		return validationError("unsupported ledger type %q", request.Type)
	}
	if err := validateTimeRange(request.Start, request.End); err != nil {
		return err
	}
	if request.Cursor != "" {
		if !request.WithCursor {
			return validationError("ledger cursor requires with-cursor pagination")
		}
		return validateOpaqueToken("ledger cursor", request.Cursor)
	}
	return nil
}

func (request LedgersRequest) values() url.Values {
	values := make(url.Values)
	if len(request.Assets) > 0 {
		values.Set("asset", strings.Join(request.Assets, ","))
	}
	setIfNotEmpty(values, "type", string(request.Type))
	setTime(values, "start", request.Start)
	setTime(values, "end", request.End)
	setBool(values, "with_cursor", request.WithCursor)
	setIfNotEmpty(values, "cursor", request.Cursor)
	setBool(values, "without_count", request.WithoutCount)
	return values
}

func (request QueryLedgersRequest) validate() error {
	return validateIDList("ledger ID", request.IDs, 1, maxQueryLedgerIDs)
}

func (request QueryLedgersRequest) values() url.Values {
	return url.Values{"id": {strings.Join(request.IDs, ",")}}
}

func (request TradeVolumeRequest) validate() error {
	return validatePairs(request.Pairs, false)
}

func (request TradeVolumeRequest) values() url.Values {
	values := pairListValues(request.Pairs)
	setBool(values, "fee_schedule", request.IncludeFeeSchedule)
	return values
}

func (request OrderAmendsRequest) validate() error {
	if !transactionIDPattern.MatchString(request.OrderID) {
		return validationError("order ID is required and contains unsupported characters")
	}
	return nil
}

func (request OrderAmendsRequest) values() url.Values {
	return url.Values{"order_id": {request.OrderID}}
}

func (request APIKeyInfoRequest) validate() error {
	if request.OTP == "" {
		return nil
	}
	return validateRequiredText("otp", request.OTP)
}

func (request APIKeyInfoRequest) values() url.Values {
	values := make(url.Values)
	setIfNotEmpty(values, "otp", request.OTP)
	return values
}

// TradeBalance는 담보 잔고, 증거금 포지션 평가, 자본과 증거금 수준 요약을 조회한다.
func (client *Client) TradeBalance(
	ctx context.Context,
	request TradeBalanceRequest,
	options ...trade.RequestOption,
) (TradeBalance, error) {
	if err := request.validate(); err != nil {
		return TradeBalance{}, err
	}
	response, result, err := client.privateResult(
		ctx, privatePrefix+"TradeBalance", request.values(), credential.PermissionRead,
		commonexchange.OperationRead, limitPrivate, "", options...,
	)
	if err != nil {
		return TradeBalance{}, err
	}
	var balance TradeBalance
	if err := client.decodeResultInto(response, commonexchange.OperationRead, result, &balance); err != nil {
		return TradeBalance{}, err
	}
	return balance, nil
}

// QueryTrades는 체결 ID로 최대 20건의 계정 체결 정보를 조회한다. 결과는 체결 ID 순서다.
func (client *Client) QueryTrades(
	ctx context.Context,
	request QueryTradesRequest,
	options ...trade.RequestOption,
) ([]TradeFill, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, result, err := client.privateResult(
		ctx, privatePrefix+"QueryTrades", request.values(), credential.PermissionRead,
		commonexchange.OperationRead, limitPrivateHistory, "", options...,
	)
	if err != nil {
		return nil, err
	}
	entries, err := decodeKeyedEntries[TradeFill](result)
	if err != nil {
		return nil, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	trades := make([]TradeFill, len(entries))
	for index, entry := range entries {
		trades[index] = entry.Value
		trades[index].TransactionID = entry.Key
		trades[index].Raw = entry.Raw
	}
	return trades, nil
}

// OpenPositions는 증거금 미결제 포지션을 조회한다.
func (client *Client) OpenPositions(
	ctx context.Context,
	request OpenPositionsRequest,
	options ...trade.RequestOption,
) (OpenPositions, error) {
	if err := request.validate(); err != nil {
		return OpenPositions{}, err
	}
	response, result, err := client.privateResult(
		ctx, privatePrefix+"OpenPositions", request.values(), credential.PermissionRead,
		commonexchange.OperationRead, limitPrivate, "", options...,
	)
	if err != nil {
		return OpenPositions{}, err
	}
	positions := OpenPositions{Raw: cloneBytes(result)}
	if trimmed := bytes.TrimSpace(result); len(trimmed) > 0 && trimmed[0] == '[' {
		if err := client.decodeResultInto(
			response, commonexchange.OperationRead, result, &positions.Consolidated,
		); err != nil {
			return OpenPositions{}, err
		}
		return positions, nil
	}
	entries, err := decodeKeyedEntries[Position](result)
	if err != nil {
		return OpenPositions{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	positions.Positions = make([]Position, len(entries))
	for index, entry := range entries {
		positions.Positions[index] = entry.Value
		positions.Positions[index].TransactionID = entry.Key
		positions.Positions[index].Raw = entry.Raw
	}
	return positions, nil
}

// Ledgers는 계정 원장을 조회한다. 한 페이지에 최대 50건이 반환된다.
func (client *Client) Ledgers(
	ctx context.Context,
	request LedgersRequest,
	options ...trade.RequestOption,
) (LedgerPage, error) {
	if err := request.validate(); err != nil {
		return LedgerPage{}, err
	}
	response, result, err := client.privateResult(
		ctx, privatePrefix+"Ledgers", request.values(), credential.PermissionRead,
		commonexchange.OperationRead, limitPrivateHistory, "", options...,
	)
	if err != nil {
		return LedgerPage{}, err
	}
	var page struct {
		Ledger json.RawMessage `json:"ledger"`
		Count  int             `json:"count"`
		Cursor pageCursor      `json:"cursor"`
	}
	if err := client.decodeResultInto(response, commonexchange.OperationRead, result, &page); err != nil {
		return LedgerPage{}, err
	}
	entries, err := decodeLedgerEntries(page.Ledger)
	if err != nil {
		return LedgerPage{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	return LedgerPage{
		Entries: entries, Count: page.Count, NextCursor: page.Cursor.Next, Raw: cloneBytes(result),
	}, nil
}

// QueryLedgers는 원장 ID로 최대 20건의 원장을 조회한다. 결과는 원장 ID 순서다.
func (client *Client) QueryLedgers(
	ctx context.Context,
	request QueryLedgersRequest,
	options ...trade.RequestOption,
) ([]LedgerEntry, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, result, err := client.privateResult(
		ctx, privatePrefix+"QueryLedgers", request.values(), credential.PermissionRead,
		commonexchange.OperationRead, limitPrivateHistory, "", options...,
	)
	if err != nil {
		return nil, err
	}
	entries, err := decodeLedgerEntries(result)
	if err != nil {
		return nil, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	return entries, nil
}

// TradeVolume은 30일 거래량과 수수료 등급을 조회한다.
func (client *Client) TradeVolume(
	ctx context.Context,
	request TradeVolumeRequest,
	options ...trade.RequestOption,
) (TradeVolume, error) {
	if err := request.validate(); err != nil {
		return TradeVolume{}, err
	}
	response, result, err := client.privateResult(
		ctx, privatePrefix+"TradeVolume", request.values(), credential.PermissionRead,
		commonexchange.OperationRead, limitPrivate, "", options...,
	)
	if err != nil {
		return TradeVolume{}, err
	}
	var volume TradeVolume
	if err := client.decodeResultInto(response, commonexchange.OperationRead, result, &volume); err != nil {
		return TradeVolume{}, err
	}
	volume.Raw = cloneBytes(result)
	return volume, nil
}

// OrderAmends는 주문의 정정 이력을 조회한다.
func (client *Client) OrderAmends(
	ctx context.Context,
	request OrderAmendsRequest,
	options ...trade.RequestOption,
) (OrderAmends, error) {
	if err := request.validate(); err != nil {
		return OrderAmends{}, err
	}
	response, result, err := client.privateResult(
		ctx, privatePrefix+"OrderAmends", request.values(), credential.PermissionRead,
		commonexchange.OperationRead, limitPrivate, "", options...,
	)
	if err != nil {
		return OrderAmends{}, err
	}
	var amends OrderAmends
	if err := client.decodeResultInto(response, commonexchange.OperationRead, result, &amends); err != nil {
		return OrderAmends{}, err
	}
	amends.Raw = cloneBytes(result)
	return amends, nil
}

// CreditLines는 VIP 신용 한도 상세를 조회한다. 신용 한도가 없으면 빈 결과를 반환한다.
func (client *Client) CreditLines(
	ctx context.Context,
	options ...trade.RequestOption,
) (CreditLines, error) {
	response, err := client.executePrivate(
		ctx, privatePrefix+"CreditLines", nil, credential.PermissionRead,
		commonexchange.OperationRead, limitPrivate, "", options...,
	)
	if err != nil {
		return CreditLines{}, err
	}
	result, err := client.decodeNullableResult(response, commonexchange.OperationRead)
	if err != nil {
		return CreditLines{}, err
	}
	if result == nil {
		return CreditLines{}, nil
	}
	var lines CreditLines
	if err := client.decodeResultInto(response, commonexchange.OperationRead, result, &lines); err != nil {
		return CreditLines{}, err
	}
	lines.Raw = cloneBytes(result)
	return lines, nil
}

// APIKeyInfo는 요청에 쓰인 API Key의 이름, 권한, 제한과 사용 시각을 조회한다.
func (client *Client) APIKeyInfo(
	ctx context.Context,
	request APIKeyInfoRequest,
	options ...trade.RequestOption,
) (APIKeyInfo, error) {
	if err := request.validate(); err != nil {
		return APIKeyInfo{}, err
	}
	response, result, err := client.privateResult(
		ctx, privatePrefix+"GetApiKeyInfo", request.values(), credential.PermissionRead,
		commonexchange.OperationRead, limitPrivate, "", options...,
	)
	if err != nil {
		return APIKeyInfo{}, err
	}
	var info APIKeyInfo
	if err := client.decodeResultInto(response, commonexchange.OperationRead, result, &info); err != nil {
		return APIKeyInfo{}, err
	}
	return info, nil
}

// ListWalletAccounts는 인증된 사용자의 지갑 계정 목록을 조회한다.
func (client *Client) ListWalletAccounts(
	ctx context.Context,
	options ...trade.RequestOption,
) (WalletAccountPage, error) {
	response, result, err := client.privateResult(
		ctx, privatePrefix+"ListWalletAccounts", nil, credential.PermissionRead,
		commonexchange.OperationRead, limitPrivate, "", options...,
	)
	if err != nil {
		return WalletAccountPage{}, err
	}
	var page struct {
		Accounts []WalletAccount `json:"accounts"`
		Cursor   pageCursor      `json:"cursor"`
	}
	if err := client.decodeResultInto(response, commonexchange.OperationRead, result, &page); err != nil {
		return WalletAccountPage{}, err
	}
	return WalletAccountPage{
		Accounts: page.Accounts, NextCursor: page.Cursor.Next, Raw: cloneBytes(result),
	}, nil
}

func decodeLedgerEntries(raw json.RawMessage) ([]LedgerEntry, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	entries, err := decodeKeyedEntries[LedgerEntry](raw)
	if err != nil {
		return nil, err
	}
	ledgers := make([]LedgerEntry, len(entries))
	for index, entry := range entries {
		ledgers[index] = entry.Value
		ledgers[index].ID = entry.Key
		ledgers[index].Raw = entry.Raw
	}
	return ledgers, nil
}
