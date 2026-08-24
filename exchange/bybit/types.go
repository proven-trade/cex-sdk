// Package bybit은 Bybit V5 Spot·Linear REST API 어댑터를 제공한다.
package bybit

import (
	"encoding/json"
	"fmt"
)

// Category는 Bybit V5 상품 분류다.
type Category string

const (
	CategorySpot   Category = "spot"
	CategoryLinear Category = "linear"
)

// Side는 주문과 체결 방향이다.
type Side string

const (
	SideBuy  Side = "Buy"
	SideSell Side = "Sell"
)

// OrderType은 시장가 또는 지정가 주문 종류다.
type OrderType string

const (
	OrderTypeMarket OrderType = "Market"
	OrderTypeLimit  OrderType = "Limit"
)

// TimeInForce는 주문 체결·만료 정책이다.
type TimeInForce string

const (
	TimeInForceGTC      TimeInForce = "GTC"
	TimeInForceIOC      TimeInForce = "IOC"
	TimeInForceFOK      TimeInForce = "FOK"
	TimeInForcePostOnly TimeInForce = "PostOnly"
)

// Instrument는 Spot 또는 Linear 상품 규칙이다.
type Instrument struct {
	Symbol             string          `json:"symbol"`
	ContractType       string          `json:"contractType"`
	Status             string          `json:"status"`
	BaseCoin           string          `json:"baseCoin"`
	QuoteCoin          string          `json:"quoteCoin"`
	SettleCoin         string          `json:"settleCoin"`
	LaunchTime         string          `json:"launchTime"`
	DeliveryTime       string          `json:"deliveryTime"`
	DeliveryFeeRate    string          `json:"deliveryFeeRate"`
	PriceScale         string          `json:"priceScale"`
	UnifiedMarginTrade bool            `json:"unifiedMarginTrade"`
	FundingInterval    int             `json:"fundingInterval"`
	PriceFilter        PriceFilter     `json:"priceFilter"`
	LotSizeFilter      LotSizeFilter   `json:"lotSizeFilter"`
	LeverageFilter     LeverageFilter  `json:"leverageFilter"`
	Raw                json.RawMessage `json:"-"`
}

// PriceFilter는 주문 가격 단위와 범위다.
type PriceFilter struct {
	MinimumPrice string `json:"minPrice"`
	MaximumPrice string `json:"maxPrice"`
	TickSize     string `json:"tickSize"`
}

// LotSizeFilter는 주문 수량과 금액 규칙이다.
type LotSizeFilter struct {
	MaximumOrderQuantity       string `json:"maxOrderQty"`
	MinimumOrderQuantity       string `json:"minOrderQty"`
	QuantityStep               string `json:"qtyStep"`
	MaximumMarketOrderQuantity string `json:"maxMktOrderQty"`
	MinimumNotionalValue       string `json:"minNotionalValue"`
	MinimumOrderAmount         string `json:"minOrderAmt"`
	MaximumOrderAmount         string `json:"maxOrderAmt"`
}

// LeverageFilter는 Linear 레버리지 범위다.
type LeverageFilter struct {
	MinimumLeverage string `json:"minLeverage"`
	MaximumLeverage string `json:"maxLeverage"`
	LeverageStep    string `json:"leverageStep"`
}

// InstrumentPage는 cursor 기반 상품 목록이다.
type InstrumentPage struct {
	Category       Category
	Instruments    []Instrument
	NextPageCursor string
	Raw            json.RawMessage
}

// Ticker는 상품 현재가와 24시간 통계다.
type Ticker struct {
	Symbol             string          `json:"symbol"`
	LastPrice          string          `json:"lastPrice"`
	IndexPrice         string          `json:"indexPrice"`
	MarkPrice          string          `json:"markPrice"`
	PreviousPrice24h   string          `json:"prevPrice24h"`
	PriceChange24hRate string          `json:"price24hPcnt"`
	HighPrice24h       string          `json:"highPrice24h"`
	LowPrice24h        string          `json:"lowPrice24h"`
	PreviousPrice1h    string          `json:"prevPrice1h"`
	Volume24h          string          `json:"volume24h"`
	Turnover24h        string          `json:"turnover24h"`
	FundingRate        string          `json:"fundingRate"`
	NextFundingTime    string          `json:"nextFundingTime"`
	BidPrice           string          `json:"bid1Price"`
	BidQuantity        string          `json:"bid1Size"`
	AskPrice           string          `json:"ask1Price"`
	AskQuantity        string          `json:"ask1Size"`
	Raw                json.RawMessage `json:"-"`
}

// OrderBook은 호가 snapshot이다.
type OrderBook struct {
	Symbol    string          `json:"s"`
	Bids      [][]string      `json:"b"`
	Asks      [][]string      `json:"a"`
	Timestamp int64           `json:"ts"`
	UpdateID  int64           `json:"u"`
	Sequence  int64           `json:"seq"`
	Raw       json.RawMessage `json:"-"`
}

// PublicTrade는 공개 체결 한 건이다.
type PublicTrade struct {
	ExecutionID string          `json:"execId"`
	Symbol      string          `json:"symbol"`
	Price       string          `json:"price"`
	Quantity    string          `json:"size"`
	Side        Side            `json:"side"`
	Timestamp   string          `json:"time"`
	BlockTrade  bool            `json:"isBlockTrade"`
	Raw         json.RawMessage `json:"-"`
}

// Candle은 위치 기반 OHLCV 한 건이다.
type Candle struct {
	StartTime string
	Open      string
	High      string
	Low       string
	Close     string
	Volume    string
	Turnover  string
}

// UnmarshalJSON은 위치 기반 Bybit 캔들 배열을 Candle로 변환한다.
func (candle *Candle) UnmarshalJSON(data []byte) error {
	var fields []json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return fmt.Errorf("decode Bybit candle: %w", err)
	}
	if len(fields) < 7 {
		return fmt.Errorf("Bybit candle has %d fields, want at least 7", len(fields))
	}
	targets := []*string{
		&candle.StartTime,
		&candle.Open,
		&candle.High,
		&candle.Low,
		&candle.Close,
		&candle.Volume,
		&candle.Turnover,
	}
	for index, target := range targets {
		if err := json.Unmarshal(fields[index], target); err != nil {
			return fmt.Errorf("decode Bybit candle field %d: %w", index, err)
		}
	}
	return nil
}

// WalletCoin은 통합 계정의 코인별 자산 상태다.
type WalletCoin struct {
	Coin                  string `json:"coin"`
	Equity                string `json:"equity"`
	USDValue              string `json:"usdValue"`
	WalletBalance         string `json:"walletBalance"`
	AvailableToWithdraw   string `json:"availableToWithdraw"`
	AvailableToBorrow     string `json:"availableToBorrow"`
	BorrowAmount          string `json:"borrowAmount"`
	SpotBorrow            string `json:"spotBorrow"`
	Locked                string `json:"locked"`
	UnrealisedPnL         string `json:"unrealisedPnl"`
	CumulativeRealisedPnL string `json:"cumRealisedPnl"`
}

// WalletAccount는 통합 계정 지갑 정보다.
type WalletAccount struct {
	AccountType            string          `json:"accountType"`
	TotalEquity            string          `json:"totalEquity"`
	TotalWalletBalance     string          `json:"totalWalletBalance"`
	TotalAvailableBalance  string          `json:"totalAvailableBalance"`
	TotalPerpetualUPL      string          `json:"totalPerpUPL"`
	TotalInitialMargin     string          `json:"totalInitialMargin"`
	TotalMaintenanceMargin string          `json:"totalMaintenanceMargin"`
	Coins                  []WalletCoin    `json:"coin"`
	Raw                    json.RawMessage `json:"-"`
}

// Position은 Linear 포지션 정보다.
type Position struct {
	Symbol                string          `json:"symbol"`
	Side                  string          `json:"side"`
	Size                  string          `json:"size"`
	AveragePrice          string          `json:"avgPrice"`
	PositionValue         string          `json:"positionValue"`
	Leverage              string          `json:"leverage"`
	MarkPrice             string          `json:"markPrice"`
	LiquidationPrice      string          `json:"liqPrice"`
	UnrealisedPnL         string          `json:"unrealisedPnl"`
	CumulativeRealisedPnL string          `json:"cumRealisedPnl"`
	PositionIndex         int             `json:"positionIdx"`
	TradeMode             int             `json:"tradeMode"`
	PositionStatus        string          `json:"positionStatus"`
	UpdatedTime           string          `json:"updatedTime"`
	Raw                   json.RawMessage `json:"-"`
}

// PositionPage는 cursor 기반 Linear 포지션 목록이다.
type PositionPage struct {
	Category       Category
	Positions      []Position
	NextPageCursor string
	Raw            json.RawMessage
}

// Order는 주문 생성·조회·취소·목록의 공통 필드다.
type Order struct {
	OrderID                    string          `json:"orderId"`
	OrderLinkID                string          `json:"orderLinkId"`
	Symbol                     string          `json:"symbol"`
	Side                       Side            `json:"side"`
	OrderType                  OrderType       `json:"orderType"`
	Price                      string          `json:"price"`
	Quantity                   string          `json:"qty"`
	TimeInForce                TimeInForce     `json:"timeInForce"`
	OrderStatus                string          `json:"orderStatus"`
	AveragePrice               string          `json:"avgPrice"`
	CumulativeExecutedQuantity string          `json:"cumExecQty"`
	CumulativeExecutedValue    string          `json:"cumExecValue"`
	CumulativeExecutionFee     string          `json:"cumExecFee"`
	LeavesQuantity             string          `json:"leavesQty"`
	LeavesValue                string          `json:"leavesValue"`
	ReduceOnly                 bool            `json:"reduceOnly"`
	CloseOnTrigger             bool            `json:"closeOnTrigger"`
	PositionIndex              int             `json:"positionIdx"`
	CreatedTime                string          `json:"createdTime"`
	UpdatedTime                string          `json:"updatedTime"`
	Raw                        json.RawMessage `json:"-"`
}

// OrderReference는 주문 생성 또는 취소 접수 결과다.
type OrderReference struct {
	OrderID     string          `json:"orderId"`
	OrderLinkID string          `json:"orderLinkId"`
	Raw         json.RawMessage `json:"-"`
}

// OrderPage는 cursor 기반 주문 목록이다.
type OrderPage struct {
	Category       Category
	Orders         []Order
	NextPageCursor string
	Raw            json.RawMessage
}
