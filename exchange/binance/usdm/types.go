// Package usdm은 Binance USDⓈ-M Futures REST 어댑터를 제공한다.
package usdm

import (
	"encoding/json"
	"fmt"
)

// Side는 주문의 매수 또는 매도 방향이다.
type Side string

const (
	SideBuy  Side = "BUY"
	SideSell Side = "SELL"
)

// PositionSide는 단방향 또는 헤지 모드의 포지션 방향이다.
type PositionSide string

const (
	PositionSideBoth  PositionSide = "BOTH"
	PositionSideLong  PositionSide = "LONG"
	PositionSideShort PositionSide = "SHORT"
)

// OrderType은 Futures 주문 가격 결정 및 발동 방식이다.
type OrderType string

const (
	OrderTypeLimit              OrderType = "LIMIT"
	OrderTypeMarket             OrderType = "MARKET"
	OrderTypeStop               OrderType = "STOP"
	OrderTypeStopMarket         OrderType = "STOP_MARKET"
	OrderTypeTakeProfit         OrderType = "TAKE_PROFIT"
	OrderTypeTakeProfitMarket   OrderType = "TAKE_PROFIT_MARKET"
	OrderTypeTrailingStopMarket OrderType = "TRAILING_STOP_MARKET"
)

// TimeInForce는 주문의 체결 및 만료 정책이다.
type TimeInForce string

const (
	TimeInForceGTC TimeInForce = "GTC"
	TimeInForceIOC TimeInForce = "IOC"
	TimeInForceFOK TimeInForce = "FOK"
	TimeInForceGTD TimeInForce = "GTD"
)

// WorkingType은 조건부 주문의 발동 기준 가격이다.
type WorkingType string

const (
	WorkingTypeContractPrice WorkingType = "CONTRACT_PRICE"
	WorkingTypeMarkPrice     WorkingType = "MARK_PRICE"
)

// OrderStatus는 Futures 주문 처리 상태다.
type OrderStatus string

const (
	OrderStatusNew             OrderStatus = "NEW"
	OrderStatusPartiallyFilled OrderStatus = "PARTIALLY_FILLED"
	OrderStatusFilled          OrderStatus = "FILLED"
	OrderStatusCanceled        OrderStatus = "CANCELED"
	OrderStatusRejected        OrderStatus = "REJECTED"
	OrderStatusExpired         OrderStatus = "EXPIRED"
	OrderStatusExpiredInMatch  OrderStatus = "EXPIRED_IN_MATCH"
)

// RateLimit은 exchangeInfo의 동적 요청 제한 규칙이다.
type RateLimit struct {
	Type        string `json:"rateLimitType"`
	Interval    string `json:"interval"`
	IntervalNum int    `json:"intervalNum"`
	Limit       int    `json:"limit"`
}

// Symbol은 USDⓈ-M 계약의 거래 규칙과 자산 정보다.
type Symbol struct {
	Symbol            string            `json:"symbol"`
	Pair              string            `json:"pair"`
	ContractType      string            `json:"contractType"`
	DeliveryDate      int64             `json:"deliveryDate"`
	OnboardDate       int64             `json:"onboardDate"`
	Status            string            `json:"status"`
	BaseAsset         string            `json:"baseAsset"`
	QuoteAsset        string            `json:"quoteAsset"`
	MarginAsset       string            `json:"marginAsset"`
	PricePrecision    int               `json:"pricePrecision"`
	QuantityPrecision int               `json:"quantityPrecision"`
	OrderTypes        []OrderType       `json:"orderTypes"`
	TimeInForce       []TimeInForce     `json:"timeInForce"`
	Filters           []json.RawMessage `json:"filters"`
}

// ExchangeInfo는 서버 시간, 요청 제한, 계약 규칙을 제공한다.
type ExchangeInfo struct {
	Timezone   string          `json:"timezone"`
	ServerTime int64           `json:"serverTime"`
	RateLimits []RateLimit     `json:"rateLimits"`
	Symbols    []Symbol        `json:"symbols"`
	Raw        json.RawMessage `json:"-"`
}

// TickerPrice는 계약의 최신 가격이다.
type TickerPrice struct {
	Symbol string          `json:"symbol"`
	Price  string          `json:"price"`
	Time   int64           `json:"time"`
	Raw    json.RawMessage `json:"-"`
}

// BookLevel은 호가 한 단계의 가격과 수량이다.
type BookLevel struct {
	Price    string
	Quantity string
}

// UnmarshalJSON은 위치 기반 호가 배열을 BookLevel로 변환한다.
func (level *BookLevel) UnmarshalJSON(data []byte) error {
	var values []string
	if err := json.Unmarshal(data, &values); err != nil {
		return err
	}
	if len(values) < 2 {
		return fmt.Errorf("Binance USD-M book level has %d fields, want at least 2", len(values))
	}
	level.Price, level.Quantity = values[0], values[1]
	return nil
}

// OrderBook은 USDⓈ-M 계약의 호가 스냅샷이다.
type OrderBook struct {
	LastUpdateID    int64           `json:"lastUpdateId"`
	MessageTime     int64           `json:"E"`
	TransactionTime int64           `json:"T"`
	Bids            []BookLevel     `json:"bids"`
	Asks            []BookLevel     `json:"asks"`
	Raw             json.RawMessage `json:"-"`
}

// PublicTrade는 최근 공개 체결 한 건이다.
type PublicTrade struct {
	ID         int64  `json:"id"`
	Price      string `json:"price"`
	Quantity   string `json:"qty"`
	QuoteValue string `json:"quoteQty"`
	Time       int64  `json:"time"`
	BuyerMaker bool   `json:"isBuyerMaker"`
	RPITrade   bool   `json:"isRPITrade"`
}

// Candle은 USDⓈ-M OHLCV 캔들 한 건이다.
type Candle struct {
	OpenTime         int64
	Open             string
	High             string
	Low              string
	Close            string
	Volume           string
	CloseTime        int64
	QuoteAssetVolume string
	TradeCount       int64
}

// UnmarshalJSON은 위치 기반 캔들 배열을 Candle로 변환한다.
func (candle *Candle) UnmarshalJSON(data []byte) error {
	var fields []json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if len(fields) < 9 {
		return fmt.Errorf("Binance USD-M candle has %d fields, want at least 9", len(fields))
	}
	integerTargets := []struct {
		index  int
		target *int64
	}{{0, &candle.OpenTime}, {6, &candle.CloseTime}, {8, &candle.TradeCount}}
	for _, target := range integerTargets {
		if err := json.Unmarshal(fields[target.index], target.target); err != nil {
			return err
		}
	}
	stringTargets := []struct {
		index  int
		target *string
	}{{1, &candle.Open}, {2, &candle.High}, {3, &candle.Low}, {4, &candle.Close}, {5, &candle.Volume}, {7, &candle.QuoteAssetVolume}}
	for _, target := range stringTargets {
		if err := json.Unmarshal(fields[target.index], target.target); err != nil {
			return err
		}
	}
	return nil
}

// Asset는 Futures 계정의 담보 자산 상태다.
type Asset struct {
	Asset                  string `json:"asset"`
	WalletBalance          string `json:"walletBalance"`
	UnrealizedProfit       string `json:"unrealizedProfit"`
	MarginBalance          string `json:"marginBalance"`
	MaintenanceMargin      string `json:"maintMargin"`
	InitialMargin          string `json:"initialMargin"`
	PositionInitialMargin  string `json:"positionInitialMargin"`
	OpenOrderInitialMargin string `json:"openOrderInitialMargin"`
	CrossWalletBalance     string `json:"crossWalletBalance"`
	CrossUnrealizedProfit  string `json:"crossUnPnl"`
	AvailableBalance       string `json:"availableBalance"`
	MaximumWithdrawAmount  string `json:"maxWithdrawAmount"`
	MarginAvailable        bool   `json:"marginAvailable"`
	UpdateTime             int64  `json:"updateTime"`
}

// Account는 USDⓈ-M Futures 계정 V3 정보다.
type Account struct {
	FeeTier                     int             `json:"feeTier"`
	CanTrade                    bool            `json:"canTrade"`
	CanDeposit                  bool            `json:"canDeposit"`
	CanWithdraw                 bool            `json:"canWithdraw"`
	MultiAssetsMargin           bool            `json:"multiAssetsMargin"`
	TotalInitialMargin          string          `json:"totalInitialMargin"`
	TotalMaintenanceMargin      string          `json:"totalMaintMargin"`
	TotalWalletBalance          string          `json:"totalWalletBalance"`
	TotalUnrealizedProfit       string          `json:"totalUnrealizedProfit"`
	TotalMarginBalance          string          `json:"totalMarginBalance"`
	TotalPositionInitialMargin  string          `json:"totalPositionInitialMargin"`
	TotalOpenOrderInitialMargin string          `json:"totalOpenOrderInitialMargin"`
	TotalCrossWalletBalance     string          `json:"totalCrossWalletBalance"`
	TotalCrossUnrealizedProfit  string          `json:"totalCrossUnPnl"`
	AvailableBalance            string          `json:"availableBalance"`
	MaximumWithdrawAmount       string          `json:"maxWithdrawAmount"`
	Assets                      []Asset         `json:"assets"`
	Positions                   []Position      `json:"positions"`
	Raw                         json.RawMessage `json:"-"`
}

// Position은 계약별 실시간 포지션 위험 정보다.
type Position struct {
	Symbol                 string          `json:"symbol"`
	PositionSide           PositionSide    `json:"positionSide"`
	PositionAmount         string          `json:"positionAmt"`
	EntryPrice             string          `json:"entryPrice"`
	BreakEvenPrice         string          `json:"breakEvenPrice"`
	MarkPrice              string          `json:"markPrice"`
	UnrealizedProfit       string          `json:"unRealizedProfit"`
	LiquidationPrice       string          `json:"liquidationPrice"`
	IsolatedMargin         string          `json:"isolatedMargin"`
	Notional               string          `json:"notional"`
	MarginAsset            string          `json:"marginAsset"`
	IsolatedWallet         string          `json:"isolatedWallet"`
	InitialMargin          string          `json:"initialMargin"`
	MaintenanceMargin      string          `json:"maintMargin"`
	PositionInitialMargin  string          `json:"positionInitialMargin"`
	OpenOrderInitialMargin string          `json:"openOrderInitialMargin"`
	AdlQuantile            int             `json:"adl"`
	BidNotional            string          `json:"bidNotional"`
	AskNotional            string          `json:"askNotional"`
	UpdateTime             int64           `json:"updateTime"`
	Raw                    json.RawMessage `json:"-"`
}

// Order는 USDⓈ-M Futures 주문 상태다.
type Order struct {
	OrderID                 int64           `json:"orderId"`
	Symbol                  string          `json:"symbol"`
	Status                  OrderStatus     `json:"status"`
	ClientOrderID           string          `json:"clientOrderId"`
	Price                   string          `json:"price"`
	AveragePrice            string          `json:"avgPrice"`
	OriginalQuantity        string          `json:"origQty"`
	ExecutedQuantity        string          `json:"executedQty"`
	CumulativeQuoteQuantity string          `json:"cumQuote"`
	TimeInForce             TimeInForce     `json:"timeInForce"`
	Type                    OrderType       `json:"type"`
	OriginalType            OrderType       `json:"origType"`
	Side                    Side            `json:"side"`
	PositionSide            PositionSide    `json:"positionSide"`
	StopPrice               string          `json:"stopPrice"`
	ClosePosition           bool            `json:"closePosition"`
	ReduceOnly              bool            `json:"reduceOnly"`
	WorkingType             WorkingType     `json:"workingType"`
	PriceProtect            bool            `json:"priceProtect"`
	ActivationPrice         string          `json:"activatePrice"`
	PriceRate               string          `json:"priceRate"`
	UpdateTime              int64           `json:"updateTime"`
	Time                    int64           `json:"time"`
	Raw                     json.RawMessage `json:"-"`
}
