package htx

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
)

var decimalNumberPattern = regexp.MustCompile(`^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?$`)

// Scalar는 문자열·숫자·null로 달라질 수 있는 HTX 식별자 원형이다.
type Scalar string

// UnmarshalJSON은 문자열과 숫자를 문자열 원형으로 보존하고 null은 빈 값으로 바꾼다.
func (value *Scalar) UnmarshalJSON(data []byte) error {
	text, err := optionalScalarText(data)
	if err != nil {
		return err
	}
	*value = Scalar(text)
	return nil
}

// Decimal은 HTX가 숫자 또는 문자열로 반환하는 정밀 값을 원형 문자열로 보존한다.
type Decimal string

// UnmarshalJSON은 부동소수점 변환 없이 JSON 숫자 또는 문자열을 보존한다.
func (value *Decimal) UnmarshalJSON(data []byte) error {
	text, err := decimalText(data)
	if err != nil {
		return err
	}
	*value = Decimal(text)
	return nil
}

// String은 정밀 값의 원형 문자열을 반환한다.
func (value Decimal) String() string { return string(value) }

// ServerTime은 HTX 서버의 Unix millisecond 시각과 원본 응답이다.
type ServerTime struct {
	Time int64
	Raw  json.RawMessage
}

// MarketSymbols는 Spot 거래쌍 규칙과 증분 응답 메타데이터다.
type MarketSymbols struct {
	Symbols   []MarketSymbol
	UpdatedAt Scalar
	Full      int
	Raw       json.RawMessage
}

// MarketSymbol은 HTX Spot 거래쌍의 상태와 주문 정밀도·한도다.
type MarketSymbol struct {
	Symbol                       string          `json:"symbol"`
	State                        string          `json:"state"`
	BaseCurrency                 string          `json:"bc"`
	QuoteCurrency                string          `json:"qc"`
	PricePrecision               int             `json:"pp"`
	AmountPrecision              int             `json:"ap"`
	ValuePrecision               int             `json:"vp"`
	Partition                    string          `json:"sp"`
	MinimumOrderAmount           Decimal         `json:"minoa"`
	MaximumOrderAmount           Decimal         `json:"maxoa"`
	MinimumOrderValue            Decimal         `json:"minov"`
	LimitMinimumOrderAmount      Decimal         `json:"lominoa"`
	LimitMaximumOrderAmount      Decimal         `json:"lomaxoa"`
	LimitMaximumBuyAmount        Decimal         `json:"lomaxba"`
	LimitMaximumSellAmount       Decimal         `json:"lomaxsa"`
	MarketSellMinimumOrderAmount Decimal         `json:"smminoa"`
	MarketSellMaximumOrderAmount Decimal         `json:"smmaxoa"`
	MarketBuyMaximumOrderValue   Decimal         `json:"bmmaxov"`
	MaximumOrderValue            Decimal         `json:"maxov"`
	BuyLimitMaximumRatio         Decimal         `json:"blmlt"`
	SellLimitMinimumRatio        Decimal         `json:"slmgt"`
	MarketSellMaximumRate        Decimal         `json:"msormlt"`
	MarketBuyMaximumRate         Decimal         `json:"mbormlt"`
	APITrading                   string          `json:"at"`
	Tags                         string          `json:"tags"`
	Raw                          json.RawMessage `json:"-"`
}

// BookLevel은 가격과 해당 가격의 합산 수량이다.
type BookLevel struct {
	Price    Decimal
	Quantity Decimal
}

// UnmarshalJSON은 위치 기반 HTX 호가 배열을 검증해 변환한다.
func (level *BookLevel) UnmarshalJSON(data []byte) error {
	var fields []json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return fmt.Errorf("decode HTX book level: %w", err)
	}
	if len(fields) != 2 {
		return fmt.Errorf("HTX book level has %d fields, want 2", len(fields))
	}
	price, err := decimalText(fields[0])
	if err != nil {
		return fmt.Errorf("decode HTX book price: %w", err)
	}
	quantity, err := decimalText(fields[1])
	if err != nil {
		return fmt.Errorf("decode HTX book quantity: %w", err)
	}
	level.Price = Decimal(price)
	level.Quantity = Decimal(quantity)
	return nil
}

// OrderBook은 HTX Spot 호가 snapshot과 내부 version이다.
type OrderBook struct {
	Timestamp int64
	Version   Scalar
	Bids      []BookLevel
	Asks      []BookLevel
	Raw       json.RawMessage
}

// AggregatedTicker는 단일 Spot 거래쌍의 최근가·최우선 호가와 24시간 통계다.
type AggregatedTicker struct {
	ID        Scalar
	Version   Scalar
	Open      Decimal
	Close     Decimal
	Low       Decimal
	High      Decimal
	Amount    Decimal
	Volume    Decimal
	Count     int64
	Bid       BookLevel
	Ask       BookLevel
	Timestamp int64
	Raw       json.RawMessage
}

// MarketTicker는 전체 거래쌍 ticker 응답의 한 항목이다.
type MarketTicker struct {
	Symbol      string          `json:"symbol"`
	Open        Decimal         `json:"open"`
	Close       Decimal         `json:"close"`
	Low         Decimal         `json:"low"`
	High        Decimal         `json:"high"`
	Amount      Decimal         `json:"amount"`
	Volume      Decimal         `json:"vol"`
	Count       int64           `json:"count"`
	BidPrice    Decimal         `json:"bid"`
	BidQuantity Decimal         `json:"bidSize"`
	AskPrice    Decimal         `json:"ask"`
	AskQuantity Decimal         `json:"askSize"`
	Raw         json.RawMessage `json:"-"`
}

// TradeDirection은 체결을 발생시킨 taker의 매수·매도 방향이다.
type TradeDirection string

const (
	TradeDirectionBuy  TradeDirection = "buy"
	TradeDirectionSell TradeDirection = "sell"
)

// PublicTrade는 공개 Spot 체결 한 건이다.
type PublicTrade struct {
	ID        Scalar          `json:"id"`
	TradeID   Scalar          `json:"trade-id"`
	Timestamp int64           `json:"ts"`
	Amount    Decimal         `json:"amount"`
	Price     Decimal         `json:"price"`
	Direction TradeDirection  `json:"direction"`
	Raw       json.RawMessage `json:"-"`
}

// TradeBatch는 동일한 market tick에 포함된 공개 체결 묶음이다.
type TradeBatch struct {
	ID        Scalar
	Timestamp int64
	Trades    []PublicTrade
	Raw       json.RawMessage
}

// Candle은 HTX Spot OHLCV 캔들 한 개다.
type Candle struct {
	OpenTime    int64           `json:"id"`
	Open        Decimal         `json:"open"`
	Close       Decimal         `json:"close"`
	Low         Decimal         `json:"low"`
	High        Decimal         `json:"high"`
	BaseVolume  Decimal         `json:"amount"`
	QuoteVolume Decimal         `json:"vol"`
	TradeCount  int64           `json:"count"`
	Raw         json.RawMessage `json:"-"`
}

// Account는 현재 사용자의 HTX 계정 한 개다.
type Account struct {
	ID      Scalar          `json:"id"`
	Type    string          `json:"type"`
	Subtype string          `json:"subtype"`
	State   string          `json:"state"`
	Raw     json.RawMessage `json:"-"`
}

// Balance는 통화별 사용 가능·동결 등 한 종류의 잔고다.
type Balance struct {
	Currency string          `json:"currency"`
	Type     string          `json:"type"`
	Balance  Decimal         `json:"balance"`
	Sequence Scalar          `json:"seq-num"`
	Raw      json.RawMessage `json:"-"`
}

// AccountBalance는 HTX 계정과 그 계정의 통화별 잔고 목록이다.
type AccountBalance struct {
	ID       Scalar
	Type     string
	State    string
	Balances []Balance
	Raw      json.RawMessage
}

// OrderReference는 신규 Spot 주문 접수 결과다.
type OrderReference struct {
	OrderID       Scalar
	ClientOrderID string
	Raw           json.RawMessage
}

// CancelResult는 주문 ID 또는 사용자 주문 ID 취소 접수 결과다.
type CancelResult struct {
	OrderID       Scalar
	ClientOrderID string
	StatusCode    *int
	Raw           json.RawMessage
}

// Order는 HTX Spot 주문 상태와 누적 체결 금액·수량이다.
type Order struct {
	ID               Scalar          `json:"id"`
	AccountID        Scalar          `json:"account-id"`
	ClientOrderID    string          `json:"client-order-id"`
	Symbol           string          `json:"symbol"`
	Amount           Decimal         `json:"amount"`
	Price            Decimal         `json:"price"`
	CreatedAt        int64           `json:"created-at"`
	FinishedAt       int64           `json:"finished-at"`
	CanceledAt       int64           `json:"canceled-at"`
	Type             OrderType       `json:"type"`
	FilledAmount     Decimal         `json:"field-amount"`
	FilledCashAmount Decimal         `json:"field-cash-amount"`
	FilledFees       Decimal         `json:"field-fees"`
	Source           string          `json:"source"`
	State            OrderState      `json:"state"`
	Raw              json.RawMessage `json:"-"`
}

// UnmarshalJSON은 HTX 주문 API마다 다른 field·filled 누적 필드명을 하나로 정규화한다.
func (order *Order) UnmarshalJSON(data []byte) error {
	type orderAlias Order
	wire := struct {
		*orderAlias
		AlternateFilledAmount     Decimal `json:"filled-amount"`
		AlternateFilledCashAmount Decimal `json:"filled-cash-amount"`
		AlternateFilledFees       Decimal `json:"filled-fees"`
	}{orderAlias: (*orderAlias)(order)}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	if order.FilledAmount == "" {
		order.FilledAmount = wire.AlternateFilledAmount
	}
	if order.FilledCashAmount == "" {
		order.FilledCashAmount = wire.AlternateFilledCashAmount
	}
	if order.FilledFees == "" {
		order.FilledFees = wire.AlternateFilledFees
	}
	return nil
}

// MatchResult는 HTX Spot 주문의 체결 한 건과 수수료 정보다.
type MatchResult struct {
	ID                Scalar          `json:"id"`
	Symbol            string          `json:"symbol"`
	OrderID           Scalar          `json:"order-id"`
	MatchID           Scalar          `json:"match-id"`
	TradeID           Scalar          `json:"trade-id"`
	Price             Decimal         `json:"price"`
	CreatedAt         int64           `json:"created-at"`
	Type              OrderType       `json:"type"`
	FilledAmount      Decimal         `json:"filled-amount"`
	FilledFees        Decimal         `json:"filled-fees"`
	FeeCurrency       string          `json:"fee-currency"`
	Source            string          `json:"source"`
	Role              string          `json:"role"`
	FilledPoints      Decimal         `json:"filled-points"`
	FeeDeductCurrency string          `json:"fee-deduct-currency"`
	FeeDeductState    string          `json:"fee-deduct-state"`
	Raw               json.RawMessage `json:"-"`
}

func decimalText(raw json.RawMessage) (string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return "", fmt.Errorf("decimal value is empty")
	}
	if trimmed[0] == '"' {
		var value string
		if err := json.Unmarshal(trimmed, &value); err != nil {
			return "", err
		}
		if value == "" {
			return "", fmt.Errorf("decimal value is empty")
		}
		if !decimalNumberPattern.MatchString(value) {
			return "", fmt.Errorf("decimal string is invalid")
		}
		return value, nil
	}
	if trimmed[0] == '{' || trimmed[0] == '[' || !json.Valid(trimmed) ||
		!decimalNumberPattern.Match(trimmed) {
		return "", fmt.Errorf("decimal value is not scalar")
	}
	return string(trimmed), nil
}

func cloneBytes(value []byte) []byte { return append([]byte(nil), value...) }
