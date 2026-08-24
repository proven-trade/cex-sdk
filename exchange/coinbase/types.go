// Package coinbase는 Coinbase Advanced Trade Spot API 어댑터를 제공한다.
package coinbase

import "encoding/json"

// ProductType은 Coinbase 상품 분류다.
type ProductType string

const (
	ProductTypeSpot ProductType = "SPOT"
)

// Side는 주문과 체결 방향이다.
type Side string

const (
	SideBuy  Side = "BUY"
	SideSell Side = "SELL"
)

// CandleGranularity는 Advanced Trade 캔들 구간이다.
type CandleGranularity string

const (
	Candle1Minute   CandleGranularity = "ONE_MINUTE"
	Candle5Minutes  CandleGranularity = "FIVE_MINUTE"
	Candle15Minutes CandleGranularity = "FIFTEEN_MINUTE"
	Candle30Minutes CandleGranularity = "THIRTY_MINUTE"
	Candle1Hour     CandleGranularity = "ONE_HOUR"
	Candle2Hours    CandleGranularity = "TWO_HOUR"
	Candle4Hours    CandleGranularity = "FOUR_HOUR"
	Candle6Hours    CandleGranularity = "SIX_HOUR"
	Candle1Day      CandleGranularity = "ONE_DAY"
)

// Amount는 통화 단위가 포함된 decimal 문자열 금액이다.
type Amount struct {
	Value    string `json:"value"`
	Currency string `json:"currency"`
}

// Product는 Spot 상품의 시세와 주문 단위다.
type Product struct {
	ProductID              string          `json:"product_id"`
	Price                  string          `json:"price"`
	PriceChange24Hour      string          `json:"price_percentage_change_24h"`
	Volume24Hour           string          `json:"volume_24h"`
	VolumeChange24Hour     string          `json:"volume_percentage_change_24h"`
	BaseIncrement          string          `json:"base_increment"`
	QuoteIncrement         string          `json:"quote_increment"`
	PriceIncrement         string          `json:"price_increment"`
	QuoteMinimumSize       string          `json:"quote_min_size"`
	QuoteMaximumSize       string          `json:"quote_max_size"`
	BaseMinimumSize        string          `json:"base_min_size"`
	BaseMaximumSize        string          `json:"base_max_size"`
	BaseCurrencyID         string          `json:"base_currency_id"`
	QuoteCurrencyID        string          `json:"quote_currency_id"`
	Status                 string          `json:"status"`
	ProductType            ProductType     `json:"product_type"`
	CancelOnly             bool            `json:"cancel_only"`
	LimitOnly              bool            `json:"limit_only"`
	PostOnly               bool            `json:"post_only"`
	TradingDisabled        bool            `json:"trading_disabled"`
	AuctionMode            bool            `json:"auction_mode"`
	BestBidPrice           string          `json:"best_bid_price"`
	BestAskPrice           string          `json:"best_ask_price"`
	ApproximateQuoteVolume string          `json:"approximate_quote_24h_volume"`
	Raw                    json.RawMessage `json:"-"`
}

// BookLevel은 가격별 합산 호가다.
type BookLevel struct {
	Price string `json:"price"`
	Size  string `json:"size"`
}

// PriceBook은 단일 상품의 호가 스냅샷이다.
type PriceBook struct {
	ProductID string      `json:"product_id"`
	Bids      []BookLevel `json:"bids"`
	Asks      []BookLevel `json:"asks"`
	Time      string      `json:"time"`
}

// OrderBook은 호가와 시장 요약을 함께 담는다.
type OrderBook struct {
	PriceBook      PriceBook       `json:"pricebook"`
	Last           string          `json:"last"`
	MidMarket      string          `json:"mid_market"`
	SpreadBPS      string          `json:"spread_bps"`
	SpreadAbsolute string          `json:"spread_absolute"`
	Raw            json.RawMessage `json:"-"`
}

// MarketTrade는 공개 체결 한 건이다.
type MarketTrade struct {
	TradeID   string          `json:"trade_id"`
	ProductID string          `json:"product_id"`
	Price     string          `json:"price"`
	Size      string          `json:"size"`
	Time      string          `json:"time"`
	Side      Side            `json:"side"`
	Bid       string          `json:"bid"`
	Ask       string          `json:"ask"`
	Exchange  string          `json:"exchange"`
	Raw       json.RawMessage `json:"-"`
}

// MarketTrades는 공개 체결과 최우선 호가다.
type MarketTrades struct {
	Trades  []MarketTrade   `json:"trades"`
	BestBid string          `json:"best_bid"`
	BestAsk string          `json:"best_ask"`
	Raw     json.RawMessage `json:"-"`
}

// Candle은 OHLCV 한 구간이다.
type Candle struct {
	Start  string `json:"start"`
	Low    string `json:"low"`
	High   string `json:"high"`
	Open   string `json:"open"`
	Close  string `json:"close"`
	Volume string `json:"volume"`
}

// Account는 거래 가능 통화별 잔고다.
type Account struct {
	UUID              string          `json:"uuid"`
	Name              string          `json:"name"`
	Currency          string          `json:"currency"`
	AvailableBalance  Amount          `json:"available_balance"`
	Hold              Amount          `json:"hold"`
	Default           bool            `json:"default"`
	Active            bool            `json:"active"`
	CreatedAt         string          `json:"created_at"`
	UpdatedAt         string          `json:"updated_at"`
	DeletedAt         string          `json:"deleted_at"`
	Type              string          `json:"type"`
	Ready             bool            `json:"ready"`
	RetailPortfolioID string          `json:"retail_portfolio_id"`
	Platform          string          `json:"platform"`
	Raw               json.RawMessage `json:"-"`
}

// AccountPage는 cursor 기반 잔고 목록이다.
type AccountPage struct {
	Accounts []Account
	HasNext  bool
	Cursor   string
	Size     int
	Raw      json.RawMessage
}

// MarketIOCConfiguration은 시장가 IOC 주문의 기준 금액 또는 수량이다.
type MarketIOCConfiguration struct {
	QuoteSize string `json:"quote_size,omitempty"`
	BaseSize  string `json:"base_size,omitempty"`
}

// LimitGTCConfiguration은 취소 전까지 유효한 지정가 주문이다.
type LimitGTCConfiguration struct {
	BaseSize   string `json:"base_size"`
	LimitPrice string `json:"limit_price"`
	PostOnly   bool   `json:"post_only,omitempty"`
}

// OrderConfiguration은 지원하는 Spot 주문 종류 중 하나를 담는다.
type OrderConfiguration struct {
	MarketMarketIOC *MarketIOCConfiguration `json:"market_market_ioc,omitempty"`
	LimitLimitGTC   *LimitGTCConfiguration  `json:"limit_limit_gtc,omitempty"`
}

// Order는 주문 조회와 user stream에서 공유하는 주문 필드다.
type Order struct {
	OrderID              string             `json:"order_id"`
	ProductID            string             `json:"product_id"`
	UserID               string             `json:"user_id"`
	ClientOrderID        string             `json:"client_order_id"`
	Side                 Side               `json:"side"`
	Status               string             `json:"status"`
	TimeInForce          string             `json:"time_in_force"`
	OrderType            string             `json:"order_type"`
	CreationTime         string             `json:"creation_time"`
	CompletionPercentage string             `json:"completion_percentage"`
	FilledSize           string             `json:"filled_size"`
	AverageFilledPrice   string             `json:"average_filled_price"`
	Fee                  string             `json:"fee"`
	NumberOfFills        string             `json:"number_of_fills"`
	FilledValue          string             `json:"filled_value"`
	PendingCancel        bool               `json:"pending_cancel"`
	Settled              bool               `json:"settled"`
	OrderConfiguration   OrderConfiguration `json:"order_configuration"`
	Raw                  json.RawMessage    `json:"-"`
}

// OrderPage는 cursor 기반 주문 목록이다.
type OrderPage struct {
	Orders   []Order
	Sequence string
	HasNext  bool
	Cursor   string
	Raw      json.RawMessage
}

// OrderReference는 주문 접수 결과다.
type OrderReference struct {
	OrderID       string          `json:"order_id"`
	ProductID     string          `json:"product_id"`
	Side          Side            `json:"side"`
	ClientOrderID string          `json:"client_order_id"`
	Raw           json.RawMessage `json:"-"`
}

// CancelResult는 일괄 취소 항목별 접수 결과다.
type CancelResult struct {
	Success       bool   `json:"success"`
	FailureReason string `json:"failure_reason"`
	OrderID       string `json:"order_id"`
}

// Fill은 계정의 주문 체결 한 건이다.
type Fill struct {
	EntryID            string          `json:"entry_id"`
	TradeID            string          `json:"trade_id"`
	OrderID            string          `json:"order_id"`
	TradeTime          string          `json:"trade_time"`
	TradeType          string          `json:"trade_type"`
	Price              string          `json:"price"`
	Size               string          `json:"size"`
	Commission         string          `json:"commission"`
	ProductID          string          `json:"product_id"`
	SequenceTimestamp  string          `json:"sequence_timestamp"`
	LiquidityIndicator string          `json:"liquidity_indicator"`
	Side               Side            `json:"side"`
	Raw                json.RawMessage `json:"-"`
}

// FillPage는 cursor 기반 체결 목록이다.
type FillPage struct {
	Fills  []Fill
	Cursor string
	Raw    json.RawMessage
}
