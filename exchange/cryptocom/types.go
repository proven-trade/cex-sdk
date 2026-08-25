package cryptocom

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
)

var decimalNumberPattern = regexp.MustCompile(`^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?$`)

// Scalar는 문자열·숫자·null로 달라질 수 있는 Crypto.com 식별자와 시각 원형이다.
type Scalar string

// UnmarshalJSON은 문자열과 숫자를 원형 문자열로 보존하고 null은 빈 값으로 바꾼다.
func (value *Scalar) UnmarshalJSON(data []byte) error {
	text, err := optionalScalarText(data)
	if err != nil {
		return err
	}
	*value = Scalar(text)
	return nil
}

// Decimal은 Crypto.com이 숫자·문자열·null로 반환하는 정밀 값을 보존한다.
type Decimal string

// UnmarshalJSON은 부동소수점 변환 없이 숫자와 문자열을 보존하고 null은 빈 값으로 바꾼다.
func (value *Decimal) UnmarshalJSON(data []byte) error {
	text, err := decimalText(data, true)
	if err != nil {
		return err
	}
	*value = Decimal(text)
	return nil
}

// String은 정밀 값의 원형 문자열을 반환한다.
func (value Decimal) String() string { return string(value) }

// Integer는 JSON 문자열과 숫자로 달라질 수 있는 정수다.
type Integer int64

// UnmarshalJSON은 정수 문자열과 JSON 정수를 int64 범위에서 해석한다.
func (value *Integer) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		*value = 0
		return nil
	}
	text, err := optionalScalarText(data)
	if err != nil {
		return err
	}
	parsed, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return fmt.Errorf("decode Crypto.com integer: %w", err)
	}
	*value = Integer(parsed)
	return nil
}

// Instruments는 상품 목록과 Crypto.com result 원문이다.
type Instruments struct {
	Items []Instrument
	Raw   json.RawMessage
}

// Instrument는 Crypto.com Exchange v1 상품의 거래 규칙과 상태다.
type Instrument struct {
	Symbol            string          `json:"symbol"`
	InstrumentType    string          `json:"inst_type"`
	DisplayName       string          `json:"display_name"`
	BaseCurrency      string          `json:"base_ccy"`
	QuoteCurrency     string          `json:"quote_ccy"`
	QuoteDecimals     Integer         `json:"quote_decimals"`
	QuantityDecimals  Integer         `json:"quantity_decimals"`
	PriceTickSize     Decimal         `json:"price_tick_size"`
	QuantityTickSize  Decimal         `json:"qty_tick_size"`
	MaximumLeverage   Decimal         `json:"max_leverage"`
	Tradable          bool            `json:"tradable"`
	ExpiryTimestamp   Integer         `json:"expiry_timestamp_ms"`
	BetaProduct       bool            `json:"beta_product"`
	UnderlyingSymbol  string          `json:"underlying_symbol"`
	ProductType       string          `json:"product_type"`
	ContractSize      Decimal         `json:"contract_size"`
	MarginBuyEnabled  bool            `json:"margin_buy_enabled"`
	MarginSellEnabled bool            `json:"margin_sell_enabled"`
	Raw               json.RawMessage `json:"-"`
}

// Ticker는 최근가·최우선 호가와 24시간 통계다.
type Ticker struct {
	InstrumentName string          `json:"i"`
	High           Decimal         `json:"h"`
	Low            Decimal         `json:"l"`
	LatestPrice    Decimal         `json:"a"`
	BaseVolume     Decimal         `json:"v"`
	VolumeValueUSD Decimal         `json:"vv"`
	OpenInterest   Decimal         `json:"oi"`
	PriceChange    Decimal         `json:"c"`
	BestBid        Decimal         `json:"b"`
	BestAsk        Decimal         `json:"k"`
	BestBidSize    Decimal         `json:"bs"`
	BestAskSize    Decimal         `json:"ks"`
	Timestamp      Scalar          `json:"t"`
	Raw            json.RawMessage `json:"-"`
}

// BookLevel은 가격·수량·해당 가격의 주문 수를 보존한다.
type BookLevel struct {
	Price      Decimal
	Quantity   Decimal
	OrderCount Integer
}

// UnmarshalJSON은 위치 기반 Crypto.com 호가 배열을 검증해 변환한다.
func (level *BookLevel) UnmarshalJSON(data []byte) error {
	var fields []json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return fmt.Errorf("decode Crypto.com book level: %w", err)
	}
	if len(fields) != 3 {
		return fmt.Errorf("Crypto.com book level must contain price, quantity, and order count")
	}
	price, err := decimalText(fields[0], false)
	if err != nil {
		return fmt.Errorf("decode Crypto.com book price: %w", err)
	}
	quantity, err := decimalText(fields[1], false)
	if err != nil {
		return fmt.Errorf("decode Crypto.com book quantity: %w", err)
	}
	var count Integer
	if err := count.UnmarshalJSON(fields[2]); err != nil {
		return fmt.Errorf("decode Crypto.com book order count: %w", err)
	}
	if count < 0 {
		return fmt.Errorf("Crypto.com book order count cannot be negative")
	}
	level.Price = Decimal(price)
	level.Quantity = Decimal(quantity)
	level.OrderCount = count
	return nil
}

// OrderBook은 지정한 깊이의 Crypto.com Spot 호가 snapshot이다.
type OrderBook struct {
	InstrumentName string
	Depth          int
	Timestamp      Scalar
	Bids           []BookLevel
	Asks           []BookLevel
	Raw            json.RawMessage
}

// TradeSide는 공개 체결의 매수·매도 방향이다.
type TradeSide string

const (
	TradeSideBuy  TradeSide = "BUY"
	TradeSideSell TradeSide = "SELL"
)

// Trade는 Crypto.com Spot 공개 체결 한 건이다.
type Trade struct {
	Side                TradeSide       `json:"s"`
	Price               Decimal         `json:"p"`
	Quantity            Decimal         `json:"q"`
	Timestamp           Scalar          `json:"t"`
	NanosecondTimestamp Scalar          `json:"tn"`
	TradeID             Scalar          `json:"d"`
	InstrumentName      string          `json:"i"`
	Raw                 json.RawMessage `json:"-"`
}

// Candle은 Crypto.com Spot OHLCV 한 구간이다.
type Candle struct {
	Open      Decimal         `json:"o"`
	High      Decimal         `json:"h"`
	Low       Decimal         `json:"l"`
	Close     Decimal         `json:"c"`
	Volume    Decimal         `json:"v"`
	Timestamp Scalar          `json:"t"`
	Raw       json.RawMessage `json:"-"`
}

// UserBalance는 계정 위험 요약과 통화별 담보 잔고 목록이다.
type UserBalance struct {
	Accounts []BalanceAccount
	Raw      json.RawMessage
}

// BalanceAccount는 계정 전체 잔고·증거금 요약과 통화별 잔고다.
type BalanceAccount struct {
	InstrumentName             string            `json:"instrument_name"`
	TotalAvailableBalance      Decimal           `json:"total_available_balance"`
	TotalMarginBalance         Decimal           `json:"total_margin_balance"`
	TotalInitialMargin         Decimal           `json:"total_initial_margin"`
	TotalMaintenanceMargin     Decimal           `json:"total_maintenance_margin"`
	TotalPositionCost          Decimal           `json:"total_position_cost"`
	TotalCashBalance           Decimal           `json:"total_cash_balance"`
	TotalCollateralValue       Decimal           `json:"total_collateral_value"`
	TotalSessionUnrealizedPnL  Decimal           `json:"total_session_unrealized_pnl"`
	TotalSessionRealizedPnL    Decimal           `json:"total_session_realized_pnl"`
	IsLiquidating              bool              `json:"is_liquidating"`
	TotalEffectiveLeverage     Decimal           `json:"total_effective_leverage"`
	PositionLimit              Decimal           `json:"position_limit"`
	UsedPositionLimit          Decimal           `json:"used_position_limit"`
	TotalIsolatedCashBalance   Decimal           `json:"total_isolated_cash_balance"`
	TotalPositionInitialMargin Decimal           `json:"total_position_im"`
	TotalHaircut               Decimal           `json:"total_haircut"`
	TotalRiskExposure          Decimal           `json:"total_risk_exposure"`
	TotalBorrow                Decimal           `json:"total_borrow"`
	MarginScore                Decimal           `json:"margin_score"`
	HasRisk                    bool              `json:"has_risk"`
	Terminatable               bool              `json:"terminatable"`
	PositionBalances           []PositionBalance `json:"position_balances"`
	IsolatedPositions          []BalanceAccount  `json:"isolated_positions"`
	IsolationID                string            `json:"isolation_id"`
	Leverage                   Decimal           `json:"leverage"`
	IsolationType              string            `json:"isolation_type"`
	Raw                        json.RawMessage   `json:"-"`
}

// PositionBalance는 한 통화의 수량·가치·예약 수량과 담보 상태다.
type PositionBalance struct {
	InstrumentName     string          `json:"instrument_name"`
	Quantity           Decimal         `json:"quantity"`
	MarketValue        Decimal         `json:"market_value"`
	CollateralEligible bool            `json:"collateral_eligible"`
	Haircut            Decimal         `json:"haircut"`
	CollateralAmount   Decimal         `json:"collateral_amount"`
	MaximumWithdrawal  Decimal         `json:"max_withdrawal_balance"`
	ReservedQuantity   Decimal         `json:"reserved_qty"`
	HourlyInterestRate Decimal         `json:"hourly_interest_rate"`
	Raw                json.RawMessage `json:"-"`
}

// OrderReceipt는 비동기 주문 접수 식별자와 원본 응답이다.
type OrderReceipt struct {
	OrderID       Scalar          `json:"order_id"`
	ClientOrderID string          `json:"client_oid"`
	Raw           json.RawMessage `json:"-"`
}

// CancelAcknowledgement는 비동기 취소 접수 식별자와 원본 응답이다.
type CancelAcknowledgement struct {
	OrderID       Scalar          `json:"order_id"`
	ClientOrderID string          `json:"client_oid"`
	Raw           json.RawMessage `json:"-"`
}

// OrderStatus는 Crypto.com 주문 수명주기 상태다.
type OrderStatus string

const (
	OrderStatusPending  OrderStatus = "PENDING"
	OrderStatusNew      OrderStatus = "NEW"
	OrderStatusActive   OrderStatus = "ACTIVE"
	OrderStatusFilled   OrderStatus = "FILLED"
	OrderStatusCanceled OrderStatus = "CANCELED"
	OrderStatusExpired  OrderStatus = "EXPIRED"
	OrderStatusRejected OrderStatus = "REJECTED"
)

// Order는 Crypto.com Spot 주문의 현재 상태와 누적 체결 정보다.
type Order struct {
	AccountID             string          `json:"account_id"`
	OrderID               Scalar          `json:"order_id"`
	ClientOrderID         string          `json:"client_oid"`
	Type                  OrderType       `json:"order_type"`
	TimeInForce           TimeInForce     `json:"time_in_force"`
	Side                  OrderSide       `json:"side"`
	ExecutionInstructions []string        `json:"exec_inst"`
	Quantity              Decimal         `json:"quantity"`
	LimitPrice            Decimal         `json:"limit_price"`
	OrderValue            Decimal         `json:"order_value"`
	MakerFeeRate          Decimal         `json:"maker_fee_rate"`
	TakerFeeRate          Decimal         `json:"taker_fee_rate"`
	AveragePrice          Decimal         `json:"avg_price"`
	CumulativeQuantity    Decimal         `json:"cumulative_quantity"`
	CumulativeValue       Decimal         `json:"cumulative_value"`
	CumulativeFee         Decimal         `json:"cumulative_fee"`
	Status                OrderStatus     `json:"status"`
	UpdateUserID          string          `json:"update_user_id"`
	OrderDate             string          `json:"order_date"`
	CreateTime            Scalar          `json:"create_time"`
	CreateTimeNS          Scalar          `json:"create_time_ns"`
	UpdateTime            Scalar          `json:"update_time"`
	InstrumentName        string          `json:"instrument_name"`
	FeeInstrumentName     string          `json:"fee_instrument_name"`
	IsolationID           string          `json:"isolation_id"`
	IsolationType         string          `json:"isolation_type"`
	Leverage              Decimal         `json:"leverage"`
	IsolatedMarginAmount  Decimal         `json:"isolated_margin_amount"`
	Raw                   json.RawMessage `json:"-"`
}

// TakerSide는 계정 체결이 maker인지 taker인지 나타낸다.
type TakerSide string

const (
	TakerSideMaker TakerSide = "MAKER"
	TakerSideTaker TakerSide = "TAKER"
)

// AccountTrade는 계정 주문에서 발생한 체결과 수수료 정보다.
type AccountTrade struct {
	AccountID         string          `json:"account_id"`
	EventDate         string          `json:"event_date"`
	JournalType       string          `json:"journal_type"`
	TradedQuantity    Decimal         `json:"traded_quantity"`
	TradedPrice       Decimal         `json:"traded_price"`
	Fees              Decimal         `json:"fees"`
	FeeCredits        Decimal         `json:"fee_credits"`
	OrderID           Scalar          `json:"order_id"`
	TradeID           Scalar          `json:"trade_id"`
	TradeMatchID      Scalar          `json:"trade_match_id"`
	ClientOrderID     string          `json:"client_oid"`
	TakerSide         TakerSide       `json:"taker_side"`
	Side              OrderSide       `json:"side"`
	InstrumentName    string          `json:"instrument_name"`
	FeeInstrumentName string          `json:"fee_instrument_name"`
	CreateTime        Scalar          `json:"create_time"`
	CreateTimeNS      Scalar          `json:"create_time_ns"`
	MatchCount        Integer         `json:"match_count"`
	MatchIndex        Integer         `json:"match_index"`
	IsolationID       string          `json:"isolation_id"`
	IsolationType     string          `json:"isolation_type"`
	TransactionTimeNS Scalar          `json:"transact_time_ns"`
	Raw               json.RawMessage `json:"-"`
}

func optionalScalarText(raw json.RawMessage) (string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return "", nil
	}
	if trimmed[0] == '"' {
		var value string
		if err := json.Unmarshal(trimmed, &value); err != nil {
			return "", err
		}
		return value, nil
	}
	if trimmed[0] == '{' || trimmed[0] == '[' || !json.Valid(trimmed) ||
		bytes.Equal(trimmed, []byte("true")) || bytes.Equal(trimmed, []byte("false")) {
		return "", fmt.Errorf("value is not a string or number")
	}
	return string(trimmed), nil
}

func decimalText(raw json.RawMessage, nullable bool) (string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		if nullable {
			return "", nil
		}
		return "", fmt.Errorf("decimal value is missing")
	}
	text, err := optionalScalarText(raw)
	if err != nil {
		return "", err
	}
	if text == "" {
		return "", fmt.Errorf("decimal value is empty")
	}
	if !decimalNumberPattern.MatchString(text) {
		return "", fmt.Errorf("invalid decimal %q", text)
	}
	return text, nil
}

func intFromInteger(value Integer, name string) (int, error) {
	converted := int(value)
	if int64(converted) != int64(value) {
		return 0, fmt.Errorf("Crypto.com %s exceeds platform integer range", name)
	}
	return converted, nil
}
