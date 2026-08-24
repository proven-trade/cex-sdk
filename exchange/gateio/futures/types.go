package futures

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
)

var (
	identifierNumberPattern = regexp.MustCompile(`^[0-9]+$`)
	jsonDecimalPattern      = regexp.MustCompile(`^-?[0-9]+(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?$`)
)

// Identifier는 JSON 문자열 또는 정수로 오는 Gate.io 식별자를 문자열로 보존한다.
type Identifier string

// UnmarshalJSON은 정수 식별자를 부동소수점 변환 없이 문자열로 해석한다.
func (value *Identifier) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return fmt.Errorf("Gate.io Futures identifier is empty")
	}
	text := string(trimmed)
	if trimmed[0] == '"' {
		if err := json.Unmarshal(trimmed, &text); err != nil {
			return fmt.Errorf("decode Gate.io Futures identifier: %w", err)
		}
	}
	if !identifierNumberPattern.MatchString(text) {
		return fmt.Errorf("invalid Gate.io Futures identifier %q", text)
	}
	*value = Identifier(text)
	return nil
}

// Decimal은 JSON 문자열 또는 숫자의 십진 표현을 정밀도 손실 없이 보존한다.
type Decimal string

// UnmarshalJSON은 문자열과 숫자 형식의 decimal을 같은 문자열 형태로 보존한다.
func (value *Decimal) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return fmt.Errorf("Gate.io Futures decimal value is empty")
	}
	text := string(trimmed)
	if trimmed[0] == '"' {
		if err := json.Unmarshal(trimmed, &text); err != nil {
			return fmt.Errorf("decode Gate.io Futures decimal string: %w", err)
		}
	}
	if !jsonDecimalPattern.MatchString(text) {
		return fmt.Errorf("invalid Gate.io Futures decimal %q", text)
	}
	*value = Decimal(text)
	return nil
}

// Settlement는 무기한 Futures 계약의 결제 통화다.
type Settlement string

const (
	SettlementBTC  Settlement = "btc"
	SettlementUSDT Settlement = "usdt"
	SettlementUSD1 Settlement = "usd1"
)

// Contract는 Gate.io 무기한 Futures 계약 규칙과 현재 시장 상태다.
type Contract struct {
	Name                  string          `json:"name"`
	Type                  string          `json:"type"`
	QuantoMultiplier      string          `json:"quanto_multiplier"`
	ReferenceDiscountRate string          `json:"ref_discount_rate"`
	OrderPriceDeviate     string          `json:"order_price_deviate"`
	MaintenanceRate       string          `json:"maintenance_rate"`
	MarkType              string          `json:"mark_type"`
	LastPrice             string          `json:"last_price"`
	MarkPrice             string          `json:"mark_price"`
	IndexPrice            string          `json:"index_price"`
	FundingRateIndicative string          `json:"funding_rate_indicative"`
	MarkPriceRound        string          `json:"mark_price_round"`
	FundingOffset         int             `json:"funding_offset"`
	InDelisting           bool            `json:"in_delisting"`
	RiskLimitBase         string          `json:"risk_limit_base"`
	InterestRate          string          `json:"interest_rate"`
	OrderPriceRound       string          `json:"order_price_round"`
	OrderSizeMinimum      string          `json:"order_size_min"`
	OrderSizeMaximum      string          `json:"order_size_max"`
	EnableDecimal         bool            `json:"enable_decimal"`
	ReferenceRebateRate   string          `json:"ref_rebate_rate"`
	FundingInterval       int64           `json:"funding_interval"`
	RiskLimitStep         string          `json:"risk_limit_step"`
	LeverageMinimum       string          `json:"leverage_min"`
	LeverageMaximum       string          `json:"leverage_max"`
	RiskLimitMaximum      string          `json:"risk_limit_max"`
	MakerFeeRate          string          `json:"maker_fee_rate"`
	TakerFeeRate          string          `json:"taker_fee_rate"`
	FundingRate           string          `json:"funding_rate"`
	NextFundingTime       int64           `json:"funding_next_apply"`
	Raw                   json.RawMessage `json:"-"`
}

// Ticker는 Gate.io 무기한 Futures 계약의 최근 가격과 24시간 통계다.
type Ticker struct {
	Contract              string          `json:"contract"`
	Last                  string          `json:"last"`
	Low24Hours            string          `json:"low_24h"`
	High24Hours           string          `json:"high_24h"`
	ChangePercent         string          `json:"change_percentage"`
	TotalSize             string          `json:"total_size"`
	Volume24Hours         string          `json:"volume_24h"`
	Volume24HoursBase     string          `json:"volume_24h_base"`
	Volume24HoursQuote    string          `json:"volume_24h_quote"`
	Volume24HoursSettle   string          `json:"volume_24h_settle"`
	MarkPrice             string          `json:"mark_price"`
	FundingRate           string          `json:"funding_rate"`
	FundingRateIndicative string          `json:"funding_rate_indicative"`
	IndexPrice            string          `json:"index_price"`
	HighestBid            string          `json:"highest_bid"`
	HighestBidSize        string          `json:"highest_size"`
	LowestAsk             string          `json:"lowest_ask"`
	LowestAskSize         string          `json:"lowest_size"`
	Raw                   json.RawMessage `json:"-"`
}

// BookLevel은 Gate.io 무기한 Futures 호가 한 단계의 가격과 계약 수량이다.
type BookLevel struct {
	Price string `json:"p"`
	Size  string `json:"s"`
}

// OrderBook은 Gate.io 무기한 Futures 호가와 snapshot 식별자·시각이다.
type OrderBook struct {
	ID        int64           `json:"id"`
	Current   Decimal         `json:"current"`
	UpdatedAt Decimal         `json:"update"`
	Asks      []BookLevel     `json:"asks"`
	Bids      []BookLevel     `json:"bids"`
	Raw       json.RawMessage `json:"-"`
}

// PublicTrade는 Gate.io 무기한 Futures 공개 체결 한 건이다.
type PublicTrade struct {
	ID             Identifier      `json:"id"`
	CreatedAt      Decimal         `json:"create_time"`
	CreatedAtMilli Decimal         `json:"create_time_ms"`
	Contract       string          `json:"contract"`
	Size           string          `json:"size"`
	Price          string          `json:"price"`
	Raw            json.RawMessage `json:"-"`
}

// CandleInterval은 Gate.io 무기한 Futures 캔들 구간이다.
type CandleInterval string

const (
	Candle10Seconds CandleInterval = "10s"
	Candle1Minute   CandleInterval = "1m"
	Candle5Minutes  CandleInterval = "5m"
	Candle15Minutes CandleInterval = "15m"
	Candle30Minutes CandleInterval = "30m"
	Candle1Hour     CandleInterval = "1h"
	Candle4Hours    CandleInterval = "4h"
	Candle8Hours    CandleInterval = "8h"
	Candle1Day      CandleInterval = "1d"
	Candle7Days     CandleInterval = "7d"
	Candle1Week     CandleInterval = "1w"
)

// Candle은 Gate.io 무기한 Futures OHLCV 한 구간이다.
type Candle struct {
	Timestamp   Decimal         `json:"t"`
	Volume      string          `json:"v"`
	Close       string          `json:"c"`
	High        string          `json:"h"`
	Low         string          `json:"l"`
	Open        string          `json:"o"`
	QuoteVolume string          `json:"sum"`
	Raw         json.RawMessage `json:"-"`
}

// Account는 결제 통화별 Gate.io 무기한 Futures 자산과 증거금 요약이다.
type Account struct {
	User                   int64           `json:"user"`
	Currency               string          `json:"currency"`
	Total                  string          `json:"total"`
	UnrealisedPNL          string          `json:"unrealised_pnl"`
	PositionMargin         string          `json:"position_margin"`
	OrderMargin            string          `json:"order_margin"`
	Available              string          `json:"available"`
	Point                  string          `json:"point"`
	Bonus                  string          `json:"bonus"`
	InDualMode             bool            `json:"in_dual_mode"`
	PositionMode           string          `json:"position_mode"`
	EnableDualPlus         bool            `json:"enable_dual_plus"`
	MarginMode             int             `json:"margin_mode"`
	PositionInitialMargin  string          `json:"position_initial_margin"`
	MaintenanceMargin      string          `json:"maintenance_margin"`
	CrossOrderMargin       string          `json:"cross_order_margin"`
	CrossInitialMargin     string          `json:"cross_initial_margin"`
	CrossMaintenanceMargin string          `json:"cross_maintenance_margin"`
	CrossUnrealisedPNL     string          `json:"cross_unrealised_pnl"`
	CrossAvailable         string          `json:"cross_available"`
	CrossMarginBalance     string          `json:"cross_margin_balance"`
	Raw                    json.RawMessage `json:"-"`
}

// PositionMode는 Gate.io 포지션 보유 방향이다.
type PositionMode string

const (
	PositionModeSingle    PositionMode = "single"
	PositionModeDualLong  PositionMode = "dual_long"
	PositionModeDualShort PositionMode = "dual_short"
)

// PositionMarginMode는 Gate.io 포지션의 격리 또는 교차 증거금 방식이다.
type PositionMarginMode string

const (
	PositionMarginModeIsolated PositionMarginMode = "isolated"
	PositionMarginModeCross    PositionMarginMode = "cross"
)

// Position은 계약별 Gate.io 무기한 Futures 포지션과 손익·증거금 정보다.
type Position struct {
	User                   int64              `json:"user"`
	Contract               string             `json:"contract"`
	Size                   string             `json:"size"`
	HedgeStatus            string             `json:"hedge_status"`
	HedgedSize             string             `json:"hedged_size"`
	UnhedgedSize           string             `json:"unhedged_size"`
	Leverage               string             `json:"leverage"`
	RiskLimit              string             `json:"risk_limit"`
	MaximumLeverage        string             `json:"leverage_max"`
	MaintenanceRate        string             `json:"maintenance_rate"`
	Value                  string             `json:"value"`
	Margin                 string             `json:"margin"`
	EntryPrice             string             `json:"entry_price"`
	LiquidationPrice       string             `json:"liq_price"`
	MarkPrice              string             `json:"mark_price"`
	InitialMargin          string             `json:"initial_margin"`
	MaintenanceMargin      string             `json:"maintenance_margin"`
	UnrealisedPNL          string             `json:"unrealised_pnl"`
	RealisedPNL            string             `json:"realised_pnl"`
	ADLRanking             int                `json:"adl_ranking"`
	PendingOrders          int                `json:"pending_orders"`
	Mode                   PositionMode       `json:"mode"`
	UpdatedAt              int64              `json:"update_time"`
	UpdateID               int64              `json:"update_id"`
	CrossLeverageLimit     string             `json:"cross_leverage_limit"`
	RiskLimitTable         string             `json:"risk_limit_table"`
	AverageMaintenanceRate string             `json:"average_maintenance_rate"`
	PositionMarginMode     PositionMarginMode `json:"pos_margin_mode"`
	EffectiveLeverage      string             `json:"lever"`
	Raw                    json.RawMessage    `json:"-"`
}

// OrderType은 Gate.io 무기한 Futures 주문 가격 결정 방식이다.
type OrderType string

const (
	OrderTypeLimit  OrderType = "limit"
	OrderTypeMarket OrderType = "market"
)

// TimeInForce는 Gate.io 무기한 Futures 주문 체결 정책이다.
type TimeInForce string

const (
	TimeInForceGTC TimeInForce = "gtc"
	TimeInForceIOC TimeInForce = "ioc"
	TimeInForcePOC TimeInForce = "poc"
	TimeInForceFOK TimeInForce = "fok"
)

// AutoSize는 양방향 모드에서 전량 청산할 포지션 방향이다.
type AutoSize string

const (
	AutoSizeCloseLong  AutoSize = "close_long"
	AutoSizeCloseShort AutoSize = "close_short"
)

// SelfTradePrevention은 자기 거래 방지 시 취소할 주문 범위다.
type SelfTradePrevention string

const (
	SelfTradePreventionNone       SelfTradePrevention = "-"
	SelfTradePreventionCancelOld  SelfTradePrevention = "co"
	SelfTradePreventionCancelNew  SelfTradePrevention = "cn"
	SelfTradePreventionCancelBoth SelfTradePrevention = "cb"
)

// Order는 Gate.io 무기한 Futures 주문 상태와 누적 체결 정보다.
type Order struct {
	ID                     Identifier          `json:"id"`
	User                   int64               `json:"user"`
	CreatedAt              Decimal             `json:"create_time"`
	UpdatedAt              Decimal             `json:"update_time"`
	FinishedAt             Decimal             `json:"finish_time"`
	FinishAs               string              `json:"finish_as"`
	Status                 string              `json:"status"`
	Contract               string              `json:"contract"`
	Size                   string              `json:"size"`
	Iceberg                string              `json:"iceberg"`
	Left                   string              `json:"left"`
	Price                  string              `json:"price"`
	FillPrice              string              `json:"fill_price"`
	MakerFeeRate           string              `json:"mkfr"`
	TakerFeeRate           string              `json:"tkfr"`
	TimeInForce            TimeInForce         `json:"tif"`
	ReduceOnly             bool                `json:"is_reduce_only"`
	Close                  bool                `json:"is_close"`
	Liquidation            bool                `json:"is_liq"`
	ClientOrderID          string              `json:"text"`
	SelfTradePrevention    SelfTradePrevention `json:"stp_act"`
	OrderValue             string              `json:"order_value"`
	TradeValue             string              `json:"trade_value"`
	MarketOrderSlipRatio   string              `json:"market_order_slip_ratio"`
	PositionMarginMode     PositionMarginMode  `json:"pos_margin_mode"`
	TakeProfitTriggerPrice string              `json:"tpsl_tp_trigger_price"`
	StopLossTriggerPrice   string              `json:"tpsl_sl_trigger_price"`
	Raw                    json.RawMessage     `json:"-"`
}

// MyTrade는 Gate.io 무기한 Futures 계정 체결 한 건이다.
type MyTrade struct {
	ID            Identifier      `json:"id"`
	CreatedAt     Decimal         `json:"create_time"`
	Contract      string          `json:"contract"`
	OrderID       string          `json:"order_id"`
	Size          string          `json:"size"`
	Price         string          `json:"price"`
	ClientOrderID string          `json:"text"`
	Fee           string          `json:"fee"`
	PointFee      string          `json:"point_fee"`
	Role          string          `json:"role"`
	CloseSize     string          `json:"close_size"`
	TradeValue    string          `json:"trade_value"`
	Raw           json.RawMessage `json:"-"`
}

func cloneBytes(value []byte) []byte {
	return append([]byte(nil), value...)
}
