package kraken

import "encoding/json"

// Side는 주문과 체결 방향이다.
type Side string

const (
	SideBuy  Side = "buy"
	SideSell Side = "sell"
)

// OrderType은 첫 구현 범위가 지원하는 주문 종류다.
type OrderType string

const (
	OrderTypeMarket OrderType = "market"
	OrderTypeLimit  OrderType = "limit"
)

// CandleInterval은 Kraken OHLC 분 단위 구간이다.
type CandleInterval int

const (
	Candle1Minute   CandleInterval = 1
	Candle5Minutes  CandleInterval = 5
	Candle15Minutes CandleInterval = 15
	Candle30Minutes CandleInterval = 30
	Candle1Hour     CandleInterval = 60
	Candle4Hours    CandleInterval = 240
	Candle1Day      CandleInterval = 1440
	Candle1Week     CandleInterval = 10080
	Candle15Days    CandleInterval = 21600
)

// ServerTime은 Kraken 서버 시각의 두 표현이다.
type ServerTime struct {
	UnixTime int64           `json:"unixtime"`
	RFC1123  string          `json:"rfc1123"`
	Raw      json.RawMessage `json:"-"`
}

// AssetPair는 Spot 상품 식별자와 주문 단위다.
type AssetPair struct {
	ID                string          `json:"-"`
	AltName           string          `json:"altname"`
	WebSocketName     string          `json:"wsname"`
	Base              string          `json:"base"`
	Quote             string          `json:"quote"`
	Lot               string          `json:"lot"`
	CostDecimals      int             `json:"cost_decimals"`
	PairDecimals      int             `json:"pair_decimals"`
	LotDecimals       int             `json:"lot_decimals"`
	LotMultiplier     int             `json:"lot_multiplier"`
	MinimumOrder      string          `json:"ordermin"`
	MinimumCost       string          `json:"costmin"`
	TickSize          string          `json:"tick_size"`
	Status            string          `json:"status"`
	FeeVolumeCurrency string          `json:"fee_volume_currency"`
	MarginCall        int             `json:"margin_call"`
	MarginStop        int             `json:"margin_stop"`
	BuyLeverage       []int           `json:"leverage_buy"`
	SellLeverage      []int           `json:"leverage_sell"`
	Raw               json.RawMessage `json:"-"`
}

// Ticker는 최근 24시간과 당일 Spot 시세 요약이다.
type Ticker struct {
	PairID            string
	AskPrice          string
	AskWholeLotVolume string
	AskLotVolume      string
	BidPrice          string
	BidWholeLotVolume string
	BidLotVolume      string
	LastPrice         string
	LastVolume        string
	VolumeToday       string
	Volume24Hours     string
	VWAPToday         string
	VWAP24Hours       string
	TradesToday       int64
	Trades24Hours     int64
	LowToday          string
	Low24Hours        string
	HighToday         string
	High24Hours       string
	OpenPrice         string
	Raw               json.RawMessage
}

// BookLevel은 가격, 합산 수량, 마지막 변경 시각으로 구성된 호가다.
type BookLevel struct {
	Price     string
	Volume    string
	Timestamp int64
}

// OrderBook은 단일 Spot 상품의 L2 호가 스냅샷이다.
type OrderBook struct {
	PairID string
	Asks   []BookLevel
	Bids   []BookLevel
	Raw    json.RawMessage
}

// PublicTrade는 공개 체결 한 건이다.
type PublicTrade struct {
	Price     string
	Volume    string
	Time      string
	Side      Side
	OrderType OrderType
	Misc      string
	TradeID   int64
}

// RecentTrades는 공개 체결과 다음 조회용 cursor다.
type RecentTrades struct {
	PairID string
	Trades []PublicTrade
	Last   string
	Raw    json.RawMessage
}

// Candle은 OHLCV 한 구간이다.
type Candle struct {
	Time       int64
	Open       string
	High       string
	Low        string
	Close      string
	VWAP       string
	Volume     string
	TradeCount int64
}

// Candles는 OHLCV와 다음 조회 기준 시각이다.
type Candles struct {
	PairID string
	Items  []Candle
	Last   int64
	Raw    json.RawMessage
}

// Balance는 자산별 총 잔고다.
type Balance struct {
	Amounts map[string]string
	Raw     json.RawMessage
}

// OrderDescription은 주문 조건을 사람이 읽을 수 있는 필드로 분리한다.
type OrderDescription struct {
	Pair      string    `json:"pair"`
	Side      Side      `json:"type"`
	OrderType OrderType `json:"ordertype"`
	Price     string    `json:"price"`
	Price2    string    `json:"price2"`
	Leverage  string    `json:"leverage"`
	Order     string    `json:"order"`
	Close     string    `json:"close"`
}

// Order는 조회된 Spot 주문 한 건이다.
type Order struct {
	TransactionID  string           `json:"-"`
	ReferenceID    string           `json:"refid"`
	UserReference  int64            `json:"userref"`
	ClientOrderID  string           `json:"cl_ord_id"`
	Status         string           `json:"status"`
	OpenTime       float64          `json:"opentm"`
	StartTime      float64          `json:"starttm"`
	ExpireTime     float64          `json:"expiretm"`
	CloseTime      float64          `json:"closetm"`
	Description    OrderDescription `json:"descr"`
	Volume         string           `json:"vol"`
	ExecutedVolume string           `json:"vol_exec"`
	Cost           string           `json:"cost"`
	Fee            string           `json:"fee"`
	AveragePrice   string           `json:"price"`
	StopPrice      string           `json:"stopprice"`
	LimitPrice     string           `json:"limitprice"`
	Misc           string           `json:"misc"`
	OrderFlags     string           `json:"oflags"`
	Reason         string           `json:"reason"`
	TradeIDs       []string         `json:"trades"`
	Raw            json.RawMessage  `json:"-"`
}

// OrderPage는 주문 목록과 전체 개수다.
type OrderPage struct {
	Orders []Order
	Count  int
	Raw    json.RawMessage
}

// TradeFill은 계정의 Spot 체결 한 건이다.
type TradeFill struct {
	TransactionID string          `json:"-"`
	OrderID       string          `json:"ordertxid"`
	PositionID    string          `json:"postxid"`
	Pair          string          `json:"pair"`
	Time          float64         `json:"time"`
	Side          Side            `json:"type"`
	OrderType     OrderType       `json:"ordertype"`
	Price         string          `json:"price"`
	Cost          string          `json:"cost"`
	Fee           string          `json:"fee"`
	Volume        string          `json:"vol"`
	Margin        string          `json:"margin"`
	Misc          string          `json:"misc"`
	TradeID       int64           `json:"trade_id"`
	Raw           json.RawMessage `json:"-"`
}

// TradePage는 체결 목록과 전체 개수다.
type TradePage struct {
	Trades []TradeFill
	Count  int
	Raw    json.RawMessage
}

// OrderReference는 신규 주문 접수 결과다.
type OrderReference struct {
	TransactionIDs []string
	Description    OrderDescription
	Raw            json.RawMessage
}

// CancelResult는 취소 접수 건수와 대기 여부다.
type CancelResult struct {
	Count   int             `json:"count"`
	Pending bool            `json:"pending,omitempty"`
	Raw     json.RawMessage `json:"-"`
}
