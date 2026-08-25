// Package futures는 KuCoin Futures REST와 Classic·Pro WebSocket 어댑터를 제공한다.
package futures

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
)

var decimalPattern = regexp.MustCompile(`^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?$`)

// Decimal은 JSON 문자열 또는 숫자의 십진 표현을 정밀도 손실 없이 보존한다.
type Decimal string

// UnmarshalJSON은 문자열과 숫자 형식의 decimal을 동일하게 보존한다.
func (value *Decimal) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return fmt.Errorf("KuCoin Futures decimal value is empty")
	}
	text := string(trimmed)
	if trimmed[0] == '"' {
		if err := json.Unmarshal(trimmed, &text); err != nil {
			return fmt.Errorf("decode KuCoin Futures decimal string: %w", err)
		}
	}
	if !decimalPattern.MatchString(text) {
		return fmt.Errorf("invalid KuCoin Futures decimal %q", text)
	}
	*value = Decimal(text)
	return nil
}

// Contract는 Futures 계약 규칙과 현재 시장 상태다.
type Contract struct {
	Symbol                        string          `json:"symbol"`
	DisplaySymbol                 string          `json:"displaySymbol"`
	RootSymbol                    string          `json:"rootSymbol"`
	Type                          string          `json:"type"`
	BaseCurrency                  string          `json:"baseCurrency"`
	QuoteCurrency                 string          `json:"quoteCurrency"`
	SettleCurrency                string          `json:"settleCurrency"`
	MaximumOrderQuantity          Decimal         `json:"maxOrderQty"`
	MarketMaximumOrderQuantity    Decimal         `json:"marketMaxOrderQty"`
	MaximumPrice                  Decimal         `json:"maxPrice"`
	LotSize                       Decimal         `json:"lotSize"`
	TickSize                      Decimal         `json:"tickSize"`
	IndexPriceTickSize            Decimal         `json:"indexPriceTickSize"`
	Multiplier                    Decimal         `json:"multiplier"`
	InitialMargin                 Decimal         `json:"initialMargin"`
	MaintainMargin                Decimal         `json:"maintainMargin"`
	MakerFeeRate                  Decimal         `json:"makerFeeRate"`
	TakerFeeRate                  Decimal         `json:"takerFeeRate"`
	Inverse                       bool            `json:"isInverse"`
	Quanto                        bool            `json:"isQuanto"`
	Status                        string          `json:"status"`
	FundingFeeRate                Decimal         `json:"fundingFeeRate"`
	FundingRateGranularity        int64           `json:"fundingRateGranularity"`
	CurrentFundingRateGranularity int64           `json:"currentFundingRateGranularity"`
	OpenInterest                  Decimal         `json:"openInterest"`
	MarkPrice                     Decimal         `json:"markPrice"`
	IndexPrice                    Decimal         `json:"indexPrice"`
	LastTradePrice                Decimal         `json:"lastTradePrice"`
	MaximumLeverage               int             `json:"maxLeverage"`
	Raw                           json.RawMessage `json:"-"`
}

// Ticker는 Futures 계약의 최근 체결과 최우선 호가다.
type Ticker struct {
	Sequence    int64           `json:"sequence"`
	Symbol      string          `json:"symbol"`
	Side        Side            `json:"side"`
	Size        int64           `json:"size"`
	TradeID     string          `json:"tradeId"`
	Price       Decimal         `json:"price"`
	BestBid     Decimal         `json:"bestBidPrice"`
	BestBidSize int64           `json:"bestBidSize"`
	BestAsk     Decimal         `json:"bestAskPrice"`
	BestAskSize int64           `json:"bestAskSize"`
	Timestamp   int64           `json:"ts"`
	Raw         json.RawMessage `json:"-"`
}

// BookLevel은 Futures 호가 한 단계의 가격과 계약 수량이다.
type BookLevel struct {
	Price Decimal
	Size  Decimal
}

// UnmarshalJSON은 위치 기반 Futures 호가 배열을 BookLevel로 변환한다.
func (level *BookLevel) UnmarshalJSON(data []byte) error {
	var fields []json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return fmt.Errorf("decode KuCoin Futures book level: %w", err)
	}
	if len(fields) != 2 {
		return fmt.Errorf("KuCoin Futures book level has %d fields, want 2", len(fields))
	}
	if err := json.Unmarshal(fields[0], &level.Price); err != nil {
		return fmt.Errorf("decode KuCoin Futures book price: %w", err)
	}
	if err := json.Unmarshal(fields[1], &level.Size); err != nil {
		return fmt.Errorf("decode KuCoin Futures book size: %w", err)
	}
	return nil
}

// OrderBook은 20개 또는 100개 깊이의 Futures 호가 snapshot이다.
type OrderBook struct {
	Sequence  int64           `json:"sequence"`
	Symbol    string          `json:"symbol"`
	Bids      []BookLevel     `json:"bids"`
	Asks      []BookLevel     `json:"asks"`
	Timestamp int64           `json:"ts"`
	Raw       json.RawMessage `json:"-"`
}

// PublicTrade는 공개 Futures 체결 한 건이다.
type PublicTrade struct {
	Sequence     int64           `json:"sequence"`
	ContractID   int64           `json:"contractId"`
	TradeID      string          `json:"tradeId"`
	MakerOrderID string          `json:"makerOrderId"`
	TakerOrderID string          `json:"takerOrderId"`
	Timestamp    int64           `json:"ts"`
	Size         int64           `json:"size"`
	Price        Decimal         `json:"price"`
	Side         Side            `json:"side"`
	Raw          json.RawMessage `json:"-"`
}

// Candle은 Futures OHLCV와 거래대금 한 구간이다.
type Candle struct {
	Timestamp int64
	Open      Decimal
	High      Decimal
	Low       Decimal
	Close     Decimal
	Volume    Decimal
	Turnover  Decimal
	Raw       json.RawMessage
}

// UnmarshalJSON은 위치 기반 Futures 캔들 배열을 Candle로 변환한다.
func (candle *Candle) UnmarshalJSON(data []byte) error {
	var fields []json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return fmt.Errorf("decode KuCoin Futures candle: %w", err)
	}
	if len(fields) < 7 {
		return fmt.Errorf("KuCoin Futures candle has %d fields, want at least 7", len(fields))
	}
	if err := json.Unmarshal(fields[0], &candle.Timestamp); err != nil {
		return fmt.Errorf("decode KuCoin Futures candle timestamp: %w", err)
	}
	values := []*Decimal{
		&candle.Open, &candle.High, &candle.Low, &candle.Close,
		&candle.Volume, &candle.Turnover,
	}
	for index, target := range values {
		if err := json.Unmarshal(fields[index+1], target); err != nil {
			return fmt.Errorf("decode KuCoin Futures candle field %d: %w", index+1, err)
		}
	}
	candle.Raw = cloneBytes(data)
	return nil
}

// AccountOverview는 결제 통화별 Futures 자산과 증거금 요약이다.
type AccountOverview struct {
	AccountEquity         Decimal         `json:"accountEquity"`
	UnrealisedPNL         Decimal         `json:"unrealisedPNL"`
	MarginBalance         Decimal         `json:"marginBalance"`
	PositionMargin        Decimal         `json:"positionMargin"`
	OrderMargin           Decimal         `json:"orderMargin"`
	FrozenFunds           Decimal         `json:"frozenFunds"`
	AvailableBalance      Decimal         `json:"availableBalance"`
	AvailableMargin       Decimal         `json:"availableMargin"`
	Currency              string          `json:"currency"`
	RiskRatio             Decimal         `json:"riskRatio"`
	MaximumWithdrawAmount Decimal         `json:"maxWithdrawAmount"`
	Raw                   json.RawMessage `json:"-"`
}

// Position은 계약별 열린 Futures 포지션과 손익·증거금 정보다.
type Position struct {
	ID                string          `json:"id"`
	Symbol            string          `json:"symbol"`
	AutoDeposit       bool            `json:"autoDeposit"`
	CrossMode         bool            `json:"crossMode"`
	CurrentQuantity   int64           `json:"currentQty"`
	OpeningTimestamp  int64           `json:"openingTimestamp"`
	CurrentTimestamp  int64           `json:"currentTimestamp"`
	MarkPrice         Decimal         `json:"markPrice"`
	PositionMargin    Decimal         `json:"posMargin"`
	MaintenanceMargin Decimal         `json:"maintMargin"`
	RealisedPNL       Decimal         `json:"realisedPnl"`
	UnrealisedPNL     Decimal         `json:"unrealisedPnl"`
	AverageEntryPrice Decimal         `json:"avgEntryPrice"`
	LiquidationPrice  Decimal         `json:"liquidationPrice"`
	BankruptPrice     Decimal         `json:"bankruptPrice"`
	SettleCurrency    string          `json:"settleCurrency"`
	Inverse           bool            `json:"isInverse"`
	Open              bool            `json:"isOpen"`
	MarginMode        MarginMode      `json:"marginMode"`
	PositionSide      PositionSide    `json:"positionSide"`
	Leverage          Decimal         `json:"leverage"`
	Raw               json.RawMessage `json:"-"`
}

// Side는 Futures 주문과 체결의 매수 또는 매도 방향이다.
type Side string

const (
	SideBuy  Side = "buy"
	SideSell Side = "sell"
)

// OrderType은 Futures 주문의 가격 결정 방식이다.
type OrderType string

const (
	OrderTypeLimit  OrderType = "limit"
	OrderTypeMarket OrderType = "market"
)

// TimeInForce는 Futures 지정가 주문의 체결 정책이다.
type TimeInForce string

const (
	TimeInForceGTC TimeInForce = "GTC"
	TimeInForceIOC TimeInForce = "IOC"
)

// MarginMode는 교차 또는 격리 증거금 모드다.
type MarginMode string

const (
	MarginModeIsolated MarginMode = "ISOLATED"
	MarginModeCross    MarginMode = "CROSS"
)

// PositionSide는 단방향 또는 헤지 모드의 포지션 방향이다.
type PositionSide string

const (
	PositionSideBoth  PositionSide = "BOTH"
	PositionSideLong  PositionSide = "LONG"
	PositionSideShort PositionSide = "SHORT"
)

// Order는 Futures 주문 상태와 누적 체결 정보다.
type Order struct {
	ID               string          `json:"id"`
	Symbol           string          `json:"symbol"`
	Type             OrderType       `json:"type"`
	Side             Side            `json:"side"`
	Price            Decimal         `json:"price"`
	Size             int64           `json:"size"`
	Value            Decimal         `json:"value"`
	DealValue        Decimal         `json:"dealValue"`
	DealSize         int64           `json:"dealSize"`
	TimeInForce      TimeInForce     `json:"timeInForce"`
	PostOnly         bool            `json:"postOnly"`
	Leverage         Decimal         `json:"leverage"`
	CloseOrder       bool            `json:"closeOrder"`
	ClientOrderID    string          `json:"clientOid"`
	Active           bool            `json:"isActive"`
	CancelExists     bool            `json:"cancelExist"`
	CreatedAt        int64           `json:"createdAt"`
	UpdatedAt        int64           `json:"updatedAt"`
	OrderTime        int64           `json:"orderTime"`
	SettleCurrency   string          `json:"settleCurrency"`
	MarginMode       MarginMode      `json:"marginMode"`
	PositionSide     PositionSide    `json:"positionSide"`
	AverageDealPrice Decimal         `json:"avgDealPrice"`
	FilledSize       int64           `json:"filledSize"`
	FilledValue      Decimal         `json:"filledValue"`
	Status           string          `json:"status"`
	ReduceOnly       bool            `json:"reduceOnly"`
	Raw              json.RawMessage `json:"-"`
}

// OrderReference는 Futures 주문 생성 접수 결과다.
type OrderReference struct {
	OrderID       string          `json:"orderId"`
	ClientOrderID string          `json:"clientOid"`
	Raw           json.RawMessage `json:"-"`
}

// CancelResult는 취소가 접수된 Futures 주문 ID 목록이다.
type CancelResult struct {
	CancelledOrderIDs []string        `json:"cancelledOrderIds"`
	ClientOrderID     string          `json:"clientOid"`
	Raw               json.RawMessage `json:"-"`
}

// OrderPage는 페이지 번호 기반 Futures 주문 목록이다.
type OrderPage struct {
	CurrentPage int
	PageSize    int
	TotalNumber int
	TotalPages  int
	Orders      []Order
	Raw         json.RawMessage
}

// Fill은 계정의 Futures 체결 한 건이다.
type Fill struct {
	Symbol         string          `json:"symbol"`
	TradeID        string          `json:"tradeId"`
	OrderID        string          `json:"orderId"`
	Side           Side            `json:"side"`
	Liquidity      string          `json:"liquidity"`
	Price          Decimal         `json:"price"`
	Size           int64           `json:"size"`
	Value          Decimal         `json:"value"`
	Fee            Decimal         `json:"fee"`
	FeeRate        Decimal         `json:"feeRate"`
	FeeCurrency    string          `json:"feeCurrency"`
	MarginMode     MarginMode      `json:"marginMode"`
	PositionSide   PositionSide    `json:"positionSide"`
	OrderType      OrderType       `json:"orderType"`
	TradeType      string          `json:"tradeType"`
	TradeTimestamp int64           `json:"tradeTime"`
	CreatedAt      int64           `json:"createdAt"`
	Raw            json.RawMessage `json:"-"`
}

// FillPage는 페이지 번호 기반 Futures 체결 목록이다.
type FillPage struct {
	CurrentPage int
	PageSize    int
	TotalNumber int
	TotalPages  int
	Fills       []Fill
	Raw         json.RawMessage
}

func cloneBytes(value []byte) []byte {
	return append([]byte(nil), value...)
}
