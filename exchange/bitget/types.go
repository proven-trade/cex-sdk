// Package bitget은 Bitget v3 UTA Spot 및 USDT-M Futures REST 어댑터를 제공한다.
package bitget

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Category는 Bitget UTA 상품 분류다.
type Category string

const (
	CategorySpot        Category = "SPOT"
	CategoryUSDTFutures Category = "USDT-FUTURES"
)

// Side는 주문과 체결의 매수 또는 매도 방향이다.
type Side string

const (
	SideBuy  Side = "buy"
	SideSell Side = "sell"
)

// OrderType은 주문의 가격 결정 방식이다.
type OrderType string

const (
	OrderTypeLimit  OrderType = "limit"
	OrderTypeMarket OrderType = "market"
)

// TimeInForce는 주문의 체결 및 만료 정책이다.
type TimeInForce string

const (
	TimeInForceGTC      TimeInForce = "gtc"
	TimeInForceIOC      TimeInForce = "ioc"
	TimeInForceFOK      TimeInForce = "fok"
	TimeInForcePostOnly TimeInForce = "post_only"
	TimeInForceRPI      TimeInForce = "rpi"
)

// PositionSide는 hedge mode에서 주문이 대상으로 삼는 포지션 방향이다.
type PositionSide string

const (
	PositionSideLong  PositionSide = "long"
	PositionSideShort PositionSide = "short"
)

// OrderStatus는 Bitget 주문 상태다.
type OrderStatus string

const (
	OrderStatusLive            OrderStatus = "live"
	OrderStatusNew             OrderStatus = "new"
	OrderStatusPartiallyFilled OrderStatus = "partially_filled"
	OrderStatusFilled          OrderStatus = "filled"
	OrderStatusCancelled       OrderStatus = "cancelled"
)

// Instrument는 Spot 또는 Futures 상품의 거래 규칙이다.
type Instrument struct {
	Symbol                string          `json:"symbol"`
	Category              Category        `json:"category"`
	BaseCoin              string          `json:"baseCoin"`
	QuoteCoin             string          `json:"quoteCoin"`
	Status                string          `json:"status"`
	PricePrecision        string          `json:"pricePrecision"`
	QuantityPrecision     string          `json:"quantityPrecision"`
	QuotePrecision        string          `json:"quotePrecision"`
	PriceMultiplier       string          `json:"priceMultiplier"`
	QuantityMultiplier    string          `json:"sizeMultiplier"`
	MinimumOrderQuantity  string          `json:"minOrderQty"`
	MaximumOrderQuantity  string          `json:"maxOrderQty"`
	MaximumMarketQuantity string          `json:"maxMarketOrderQty"`
	MinimumOrderAmount    string          `json:"minOrderAmount"`
	MaximumLeverage       string          `json:"maxLeverage"`
	MaximumPositionSize   string          `json:"maxPositionSize"`
	MakerFeeRate          string          `json:"makerFeeRate"`
	TakerFeeRate          string          `json:"takerFeeRate"`
	SymbolType            string          `json:"symbolType"`
	LaunchTime            string          `json:"launchTime"`
	Raw                   json.RawMessage `json:"-"`
}

// Ticker는 상품의 현재가와 24시간 시세 정보다.
type Ticker struct {
	Category           Category        `json:"category"`
	Symbol             string          `json:"symbol"`
	LastPrice          string          `json:"lastPrice"`
	OpenPrice24h       string          `json:"openPrice24h"`
	HighPrice24h       string          `json:"highPrice24h"`
	LowPrice24h        string          `json:"lowPrice24h"`
	AskPrice           string          `json:"ask1Price"`
	BidPrice           string          `json:"bid1Price"`
	BidQuantity        string          `json:"bid1Size"`
	AskQuantity        string          `json:"ask1Size"`
	PriceChange24hRate string          `json:"price24hPcnt"`
	Volume24h          string          `json:"volume24h"`
	Turnover24h        string          `json:"turnover24h"`
	IndexPrice         string          `json:"indexPrice"`
	MarkPrice          string          `json:"markPrice"`
	FundingRate        string          `json:"fundingRate"`
	OpenInterest       string          `json:"openInterest"`
	Timestamp          string          `json:"ts"`
	Raw                json.RawMessage `json:"-"`
}

// BookLevel은 호가 한 단계의 가격과 수량이다.
type BookLevel struct {
	Price    string
	Quantity string
}

// UnmarshalJSON은 문자열 또는 숫자로 전달되는 호가 배열을 손실 없는 문자열로 변환한다.
func (level *BookLevel) UnmarshalJSON(data []byte) error {
	var fields []json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return fmt.Errorf("decode Bitget book level: %w", err)
	}
	if len(fields) != 2 {
		return fmt.Errorf("Bitget book level has %d values, want 2", len(fields))
	}
	values := []*string{&level.Price, &level.Quantity}
	for index, target := range values {
		value, err := decimalText(fields[index])
		if err != nil {
			return fmt.Errorf("decode Bitget book level field %d: %w", index, err)
		}
		*target = value
	}
	return nil
}

// OrderBook은 지정한 상품의 호가 스냅샷이다.
type OrderBook struct {
	Asks      []BookLevel     `json:"a"`
	Bids      []BookLevel     `json:"b"`
	Timestamp string          `json:"ts"`
	Raw       json.RawMessage `json:"-"`
}

// PublicFill은 공개 최근 체결 한 건이다.
type PublicFill struct {
	ExecutionID     string `json:"execId"`
	ExecutionLinkID string `json:"execLinkId"`
	Price           string `json:"price"`
	Quantity        string `json:"size"`
	Side            Side   `json:"side"`
	Timestamp       string `json:"ts"`
	RPI             string `json:"isRPI"`
}

// Candle은 OHLCV 캔들 한 건이다.
type Candle struct {
	Timestamp string
	Open      string
	High      string
	Low       string
	Close     string
	Volume    string
	Turnover  string
}

// UnmarshalJSON은 위치 기반 캔들 배열을 Candle로 변환한다.
func (candle *Candle) UnmarshalJSON(data []byte) error {
	var fields []json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return fmt.Errorf("decode Bitget candle: %w", err)
	}
	if len(fields) < 7 {
		return fmt.Errorf("Bitget candle has %d fields, want at least 7", len(fields))
	}
	targets := []*string{
		&candle.Timestamp,
		&candle.Open,
		&candle.High,
		&candle.Low,
		&candle.Close,
		&candle.Volume,
		&candle.Turnover,
	}
	for index, target := range targets {
		value, err := decimalText(fields[index])
		if err != nil {
			return fmt.Errorf("decode Bitget candle field %d: %w", index, err)
		}
		*target = value
	}
	return nil
}

// Asset은 통합 계정의 코인별 자산 상태다.
type Asset struct {
	Coin      string `json:"coin"`
	Equity    string `json:"equity"`
	USDValue  string `json:"usdValue"`
	Balance   string `json:"balance"`
	Available string `json:"available"`
	Debt      string `json:"debt"`
	Locked    string `json:"locked"`
	Bonus     string `json:"bonus"`
}

// AccountAssets는 통합 계정의 총자산과 코인별 잔고다.
type AccountAssets struct {
	AccountEquity       string          `json:"accountEquity"`
	USDTEquity          string          `json:"usdtEquity"`
	BTCEquity           string          `json:"btcEquity"`
	UnrealisedPnL       string          `json:"unrealisedPnl"`
	EffectiveEquity     string          `json:"effEquity"`
	MaintenanceMargin   string          `json:"mmr"`
	InitialMargin       string          `json:"imr"`
	MarginRatio         string          `json:"mgnRatio"`
	PositionMarginRatio string          `json:"positionMgnRatio"`
	PositionValue       string          `json:"positionValue"`
	Leverage            string          `json:"leverage"`
	Assets              []Asset         `json:"assets"`
	Raw                 json.RawMessage `json:"-"`
}

// FeeDetail은 주문에서 누적된 코인별 수수료다.
type FeeDetail struct {
	Coin string `json:"feeCoin"`
	Fee  string `json:"fee"`
}

// Order는 Spot과 Futures 주문의 공통 원본 필드를 보존한다.
type Order struct {
	OrderID             string          `json:"orderId"`
	ClientOrderID       string          `json:"clientOid"`
	Category            Category        `json:"category"`
	Symbol              string          `json:"symbol"`
	OrderType           OrderType       `json:"orderType"`
	Side                Side            `json:"side"`
	Price               string          `json:"price"`
	Quantity            string          `json:"qty"`
	Amount              string          `json:"amount"`
	ExecutedQuantity    string          `json:"cumExecQty"`
	ExecutedValue       string          `json:"cumExecValue"`
	AveragePrice        string          `json:"avgPrice"`
	TimeInForce         TimeInForce     `json:"timeInForce"`
	Status              OrderStatus     `json:"orderStatus"`
	PositionSide        PositionSide    `json:"posSide"`
	HoldMode            string          `json:"holdMode"`
	TradeSide           string          `json:"tradeSide"`
	ReduceOnly          string          `json:"reduceOnly"`
	MarginMode          string          `json:"marginMode"`
	SelfTradePrevention string          `json:"stpMode"`
	TakeProfit          string          `json:"takeProfit"`
	StopLoss            string          `json:"stopLoss"`
	CancelReason        string          `json:"cancelReason"`
	ExecutionType       string          `json:"execType"`
	CreatedTime         string          `json:"createdTime"`
	UpdatedTime         string          `json:"updatedTime"`
	Fees                []FeeDetail     `json:"feeDetail"`
	Raw                 json.RawMessage `json:"-"`
}

// OrderReference는 주문 생성 또는 취소 접수 결과다.
type OrderReference struct {
	OrderID       string          `json:"orderId"`
	ClientOrderID string          `json:"clientOid"`
	Raw           json.RawMessage `json:"-"`
}

// OrderPage는 cursor 기반 주문 목록 한 페이지다.
type OrderPage struct {
	Orders []Order         `json:"list"`
	Cursor string          `json:"cursor"`
	Raw    json.RawMessage `json:"-"`
}

// Position은 Futures 실시간 포지션 정보다.
type Position struct {
	Category              Category        `json:"category"`
	Symbol                string          `json:"symbol"`
	MarginCoin            string          `json:"marginCoin"`
	PositionSide          PositionSide    `json:"posSide"`
	HoldMode              string          `json:"holdMode"`
	MarginMode            string          `json:"marginMode"`
	PositionBalance       string          `json:"positionBalance"`
	Available             string          `json:"available"`
	Frozen                string          `json:"frozen"`
	Total                 string          `json:"total"`
	Leverage              string          `json:"leverage"`
	CurrentRealisedPnL    string          `json:"curRealisedPnl"`
	AverageOpenPrice      string          `json:"avgPrice"`
	PositionStatus        string          `json:"positionStatus"`
	UnrealisedPnL         string          `json:"unrealisedPnl"`
	LiquidationPrice      string          `json:"liquidationPrice"`
	MaintenanceMarginRate string          `json:"mmr"`
	ProfitRate            string          `json:"profitRate"`
	MarkPrice             string          `json:"markPrice"`
	BreakEvenPrice        string          `json:"breakEvenPrice"`
	TotalFunding          string          `json:"totalFunding"`
	OpenFeeTotal          string          `json:"openFeeTotal"`
	CloseFeeTotal         string          `json:"closeFeeTotal"`
	CashDividend          string          `json:"cashDividend"`
	CreatedTime           string          `json:"createdTime"`
	UpdatedTime           string          `json:"updatedTime"`
	Raw                   json.RawMessage `json:"-"`
}

func decimalText(raw json.RawMessage) (string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return "", fmt.Errorf("decimal value is empty")
	}
	if trimmed[0] == '"' {
		var value string
		if err := json.Unmarshal(trimmed, &value); err != nil {
			return "", err
		}
		return value, nil
	}
	var number json.Number
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.UseNumber()
	if err := decoder.Decode(&number); err != nil {
		return "", err
	}
	return number.String(), nil
}
