package okx

import (
	"encoding/json"
	"fmt"
)

// InstrumentType은 OKX 상품 유형이다.
type InstrumentType string

const (
	InstrumentTypeSpot InstrumentType = "SPOT"
	InstrumentTypeSwap InstrumentType = "SWAP"
)

// Side는 주문과 체결의 매수 또는 매도 방향이다.
type Side string

const (
	SideBuy  Side = "buy"
	SideSell Side = "sell"
)

// TradeMode는 주문의 현금 또는 마진 모드다.
type TradeMode string

const (
	TradeModeCash     TradeMode = "cash"
	TradeModeCross    TradeMode = "cross"
	TradeModeIsolated TradeMode = "isolated"
)

// PositionSide는 포지션 모드에서 주문과 포지션의 방향이다.
type PositionSide string

const (
	PositionSideNet   PositionSide = "net"
	PositionSideLong  PositionSide = "long"
	PositionSideShort PositionSide = "short"
)

// OrderType은 주문의 가격 및 체결 정책이다.
type OrderType string

const (
	OrderTypeMarket          OrderType = "market"
	OrderTypeLimit           OrderType = "limit"
	OrderTypePostOnly        OrderType = "post_only"
	OrderTypeFOK             OrderType = "fok"
	OrderTypeIOC             OrderType = "ioc"
	OrderTypeOptimalLimitIOC OrderType = "optimal_limit_ioc"
)

// TargetCurrency는 Spot 시장가 주문 수량의 기준 통화다.
type TargetCurrency string

const (
	TargetCurrencyBase  TargetCurrency = "base_ccy"
	TargetCurrencyQuote TargetCurrency = "quote_ccy"
)

// Instrument는 Spot 또는 SWAP 상품 규칙이다.
type Instrument struct {
	InstrumentType    InstrumentType  `json:"instType"`
	InstrumentID      string          `json:"instId"`
	Underlying        string          `json:"uly"`
	InstrumentFamily  string          `json:"instFamily"`
	Category          string          `json:"category"`
	BaseCurrency      string          `json:"baseCcy"`
	QuoteCurrency     string          `json:"quoteCcy"`
	SettleCurrency    string          `json:"settleCcy"`
	ContractValue     string          `json:"ctVal"`
	ContractMultiple  string          `json:"ctMult"`
	ContractValueCcy  string          `json:"ctValCcy"`
	ContractType      string          `json:"ctType"`
	ListTime          string          `json:"listTime"`
	ExpiryTime        string          `json:"expTime"`
	MaximumLeverage   string          `json:"lever"`
	TickSize          string          `json:"tickSz"`
	LotSize           string          `json:"lotSz"`
	MinimumSize       string          `json:"minSz"`
	MaximumLimitSize  string          `json:"maxLmtSz"`
	MaximumMarketSize string          `json:"maxMktSz"`
	State             string          `json:"state"`
	Raw               json.RawMessage `json:"-"`
}

// Ticker는 상품 현재가와 24시간 통계다.
type Ticker struct {
	InstrumentType    InstrumentType  `json:"instType"`
	InstrumentID      string          `json:"instId"`
	LastPrice         string          `json:"last"`
	LastSize          string          `json:"lastSz"`
	AskPrice          string          `json:"askPx"`
	AskSize           string          `json:"askSz"`
	BidPrice          string          `json:"bidPx"`
	BidSize           string          `json:"bidSz"`
	Open24h           string          `json:"open24h"`
	High24h           string          `json:"high24h"`
	Low24h            string          `json:"low24h"`
	VolumeCurrency24h string          `json:"volCcy24h"`
	Volume24h         string          `json:"vol24h"`
	Timestamp         string          `json:"ts"`
	Raw               json.RawMessage `json:"-"`
}

// BookLevel은 OKX 호가 한 단계다.
type BookLevel struct {
	Price                string
	Quantity             string
	LiquidatedOrderCount string
	OrderCount           string
}

// UnmarshalJSON은 위치 기반 OKX 호가 배열을 BookLevel로 변환한다.
func (level *BookLevel) UnmarshalJSON(data []byte) error {
	var fields []string
	if err := json.Unmarshal(data, &fields); err != nil {
		return fmt.Errorf("decode OKX book level: %w", err)
	}
	if len(fields) < 4 {
		return fmt.Errorf("OKX book level has %d fields, want at least 4", len(fields))
	}
	level.Price = fields[0]
	level.Quantity = fields[1]
	level.LiquidatedOrderCount = fields[2]
	level.OrderCount = fields[3]
	return nil
}

// OrderBook은 지정한 상품의 호가 스냅샷이다.
type OrderBook struct {
	Asks               []BookLevel     `json:"asks"`
	Bids               []BookLevel     `json:"bids"`
	Timestamp          string          `json:"ts"`
	Checksum           int64           `json:"checksum"`
	PreviousSequenceID int64           `json:"prevSeqId"`
	SequenceID         int64           `json:"seqId"`
	Raw                json.RawMessage `json:"-"`
}

// PublicTrade는 공개 체결 한 건이다.
type PublicTrade struct {
	InstrumentID string          `json:"instId"`
	TradeID      string          `json:"tradeId"`
	Price        string          `json:"px"`
	Quantity     string          `json:"sz"`
	Side         Side            `json:"side"`
	Timestamp    string          `json:"ts"`
	Raw          json.RawMessage `json:"-"`
}

// Candle은 위치 기반 OHLCV 한 건이다.
type Candle struct {
	Timestamp           string
	Open                string
	High                string
	Low                 string
	Close               string
	Volume              string
	VolumeCurrency      string
	VolumeQuoteCurrency string
	Confirmed           bool
}

// UnmarshalJSON은 위치 기반 OKX 캔들 배열을 Candle로 변환한다.
func (candle *Candle) UnmarshalJSON(data []byte) error {
	var fields []string
	if err := json.Unmarshal(data, &fields); err != nil {
		return fmt.Errorf("decode OKX candle: %w", err)
	}
	if len(fields) < 9 {
		return fmt.Errorf("OKX candle has %d fields, want at least 9", len(fields))
	}
	candle.Timestamp = fields[0]
	candle.Open = fields[1]
	candle.High = fields[2]
	candle.Low = fields[3]
	candle.Close = fields[4]
	candle.Volume = fields[5]
	candle.VolumeCurrency = fields[6]
	candle.VolumeQuoteCurrency = fields[7]
	candle.Confirmed = fields[8] == "1"
	return nil
}

// BalanceDetail은 거래 계정의 통화별 잔고다.
type BalanceDetail struct {
	Currency          string `json:"ccy"`
	Equity            string `json:"eq"`
	CashBalance       string `json:"cashBal"`
	UpdateTime        string `json:"uTime"`
	IsolatedEquity    string `json:"isoEq"`
	AvailableEquity   string `json:"availEq"`
	DiscountEquity    string `json:"disEq"`
	AvailableBalance  string `json:"availBal"`
	FrozenBalance     string `json:"frozenBal"`
	OrderFrozen       string `json:"ordFrozen"`
	Liability         string `json:"liab"`
	UnrealisedPnL     string `json:"upl"`
	CrossLiability    string `json:"crossLiab"`
	IsolatedLiability string `json:"isoLiab"`
	MarginRatio       string `json:"mgnRatio"`
	Interest          string `json:"interest"`
}

// Balance는 거래 계정의 총자산과 통화별 잔고다.
type Balance struct {
	TotalEquity    string          `json:"totalEq"`
	IsolatedEquity string          `json:"isoEq"`
	AdjustedEquity string          `json:"adjEq"`
	OrderFrozen    string          `json:"ordFroz"`
	MarginRatio    string          `json:"mgnRatio"`
	NotionalUSD    string          `json:"notionalUsd"`
	UpdateTime     string          `json:"uTime"`
	Details        []BalanceDetail `json:"details"`
	Raw            json.RawMessage `json:"-"`
}

// Position은 SWAP 포지션 정보다.
type Position struct {
	InstrumentType     InstrumentType  `json:"instType"`
	InstrumentID       string          `json:"instId"`
	MarginMode         TradeMode       `json:"mgnMode"`
	PositionID         string          `json:"posId"`
	PositionSide       PositionSide    `json:"posSide"`
	Position           string          `json:"pos"`
	PositionCcy        string          `json:"posCcy"`
	AvailablePosition  string          `json:"availPos"`
	AveragePrice       string          `json:"avgPx"`
	UnrealisedPnL      string          `json:"upl"`
	UnrealisedPnLRatio string          `json:"uplRatio"`
	Leverage           string          `json:"lever"`
	LiquidationPrice   string          `json:"liqPx"`
	MarkPrice          string          `json:"markPx"`
	InitialMargin      string          `json:"imr"`
	Margin             string          `json:"margin"`
	MaintenanceMargin  string          `json:"mmr"`
	NotionalUSD        string          `json:"notionalUsd"`
	ADL                string          `json:"adl"`
	CreatedTime        string          `json:"cTime"`
	UpdatedTime        string          `json:"uTime"`
	Raw                json.RawMessage `json:"-"`
}

// Order는 Spot과 SWAP 주문의 공통 원본 필드다.
type Order struct {
	InstrumentType   InstrumentType  `json:"instType"`
	InstrumentID     string          `json:"instId"`
	OrderID          string          `json:"ordId"`
	ClientOrderID    string          `json:"clOrdId"`
	Tag              string          `json:"tag"`
	Price            string          `json:"px"`
	Quantity         string          `json:"sz"`
	OrderType        OrderType       `json:"ordType"`
	Side             Side            `json:"side"`
	PositionSide     PositionSide    `json:"posSide"`
	TradeMode        TradeMode       `json:"tdMode"`
	ExecutedQuantity string          `json:"accFillSz"`
	FillPrice        string          `json:"fillPx"`
	FillQuantity     string          `json:"fillSz"`
	AveragePrice     string          `json:"avgPx"`
	State            string          `json:"state"`
	FeeCurrency      string          `json:"feeCcy"`
	Fee              string          `json:"fee"`
	PnL              string          `json:"pnl"`
	TargetCurrency   TargetCurrency  `json:"tgtCcy"`
	ReduceOnly       string          `json:"reduceOnly"`
	CancelSource     string          `json:"cancelSource"`
	CreatedTime      string          `json:"cTime"`
	UpdatedTime      string          `json:"uTime"`
	Raw              json.RawMessage `json:"-"`
}

// OrderReference는 주문 생성 또는 취소 접수 결과다.
type OrderReference struct {
	OrderID       string          `json:"ordId"`
	ClientOrderID string          `json:"clOrdId"`
	Tag           string          `json:"tag"`
	Timestamp     string          `json:"ts"`
	StatusCode    string          `json:"sCode"`
	StatusMessage string          `json:"sMsg"`
	Raw           json.RawMessage `json:"-"`
}

// OrderPage는 주문 목록과 원본 응답이다.
type OrderPage struct {
	Orders []Order
	Raw    json.RawMessage
}
