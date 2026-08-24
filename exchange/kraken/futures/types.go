package futures

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Decimal은 JSON 숫자의 원문 정밀도를 유지하는 decimal 문자열이다.
type Decimal string

// UnmarshalJSON은 JSON 문자열과 숫자를 동일한 decimal 문자열로 읽는다.
func (decimal *Decimal) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if bytes.Equal(data, []byte("null")) {
		*decimal = ""
		return nil
	}
	if len(data) > 0 && data[0] == '"' {
		var value string
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
		*decimal = Decimal(value)
		return nil
	}
	var number json.Number
	if err := json.Unmarshal(data, &number); err != nil {
		return fmt.Errorf("decode Kraken Futures decimal: %w", err)
	}
	*decimal = Decimal(number.String())
	return nil
}

// Side는 Futures 주문과 체결 방향이다.
type Side string

const (
	SideBuy  Side = "buy"
	SideSell Side = "sell"
)

// OrderType은 첫 Futures 범위가 지원하는 주문 종류다.
type OrderType string

const (
	OrderTypeLimit    OrderType = "lmt"
	OrderTypePostOnly OrderType = "post"
	OrderTypeMarket   OrderType = "mkt"
	OrderTypeIOC      OrderType = "ioc"
	OrderTypeFOK      OrderType = "fok"
)

// ContractType은 Futures 상품 종류다.
type ContractType string

const (
	ContractTypeInverse  ContractType = "futures_inverse"
	ContractTypeVanilla  ContractType = "futures_vanilla"
	ContractTypeFlexible ContractType = "flexible_futures"
)

// MarginLevel은 포지션 규모 구간별 증거금률이다.
type MarginLevel struct {
	Contracts         Decimal `json:"contracts"`
	NonContractUnits  Decimal `json:"numNonContractUnits"`
	InitialMargin     Decimal `json:"initialMargin"`
	MaintenanceMargin Decimal `json:"maintenanceMargin"`
}

// Instrument는 현재 상장된 Futures 상품 규칙이다.
type Instrument struct {
	Symbol                      string          `json:"symbol"`
	Pair                        string          `json:"pair"`
	Base                        string          `json:"base"`
	Quote                       string          `json:"quote"`
	Type                        ContractType    `json:"type"`
	Underlying                  string          `json:"underlying"`
	TickSize                    Decimal         `json:"tickSize"`
	ContractSize                Decimal         `json:"contractSize"`
	Tradeable                   bool            `json:"tradeable"`
	ImpactMidSize               Decimal         `json:"impactMidSize"`
	MaximumPositionSize         Decimal         `json:"maxPositionSize"`
	OpeningDate                 string          `json:"openingDate"`
	LastTradingTime             string          `json:"lastTradingTime"`
	FundingRateCoefficient      Decimal         `json:"fundingRateCoefficient"`
	MaximumRelativeFundingRate  Decimal         `json:"maxRelativeFundingRate"`
	ContractValueTradePrecision int             `json:"contractValueTradePrecision"`
	PostOnly                    bool            `json:"postOnly"`
	Expired                     bool            `json:"isExpired"`
	MarginLevels                []MarginLevel   `json:"marginLevels"`
	RetailMarginLevels          []MarginLevel   `json:"retailMarginLevels"`
	Raw                         json.RawMessage `json:"-"`
}

// Ticker는 Futures 상품의 실시간 시장 요약이다.
type Ticker struct {
	Tag                   string          `json:"tag"`
	Pair                  string          `json:"pair"`
	Symbol                string          `json:"symbol"`
	MarkPrice             Decimal         `json:"markPrice"`
	Bid                   Decimal         `json:"bid"`
	BidSize               Decimal         `json:"bidSize"`
	Ask                   Decimal         `json:"ask"`
	AskSize               Decimal         `json:"askSize"`
	Volume24Hours         Decimal         `json:"vol24h"`
	QuoteVolume           Decimal         `json:"volumeQuote"`
	OpenInterest          Decimal         `json:"openInterest"`
	Open24Hours           Decimal         `json:"open24h"`
	IndexPrice            Decimal         `json:"indexPrice"`
	Last                  Decimal         `json:"last"`
	LastTime              string          `json:"lastTime"`
	LastSize              Decimal         `json:"lastSize"`
	Suspended             bool            `json:"suspended"`
	FundingRate           Decimal         `json:"fundingRate"`
	FundingRatePrediction Decimal         `json:"fundingRatePrediction"`
	PostOnly              bool            `json:"postOnly"`
	Change24Hours         Decimal         `json:"change24h"`
	Raw                   json.RawMessage `json:"-"`
}

// BookLevel은 가격과 수량으로 구성된 비누적 호가다.
type BookLevel struct {
	Price Decimal
	Size  Decimal
}

// OrderBook은 단일 Futures 상품의 전체 비누적 호가다.
type OrderBook struct {
	Bids       []BookLevel
	Asks       []BookLevel
	ServerTime string
	Raw        json.RawMessage
}

// PublicTrade는 Futures 공개 체결 한 건이다.
type PublicTrade struct {
	TradeID int64           `json:"trade_id"`
	Price   Decimal         `json:"price"`
	Side    Side            `json:"side"`
	Size    Decimal         `json:"size"`
	Time    string          `json:"time"`
	Type    string          `json:"type"`
	Raw     json.RawMessage `json:"-"`
}

// TradeHistory는 공개 체결 이력과 거래소 서버 시각이다.
type TradeHistory struct {
	Trades     []PublicTrade
	ServerTime string
	Raw        json.RawMessage
}

// CandleTickType은 캔들의 가격 기준이다.
type CandleTickType string

const (
	CandleTickSpot  CandleTickType = "spot"
	CandleTickMark  CandleTickType = "mark"
	CandleTickTrade CandleTickType = "trade"
)

// CandleResolution은 Futures 차트 구간이다.
type CandleResolution string

const (
	Candle1Minute   CandleResolution = "1m"
	Candle5Minutes  CandleResolution = "5m"
	Candle15Minutes CandleResolution = "15m"
	Candle30Minutes CandleResolution = "30m"
	Candle1Hour     CandleResolution = "1h"
	Candle4Hours    CandleResolution = "4h"
	Candle12Hours   CandleResolution = "12h"
	Candle1Day      CandleResolution = "1d"
	Candle1Week     CandleResolution = "1w"
)

// Candle은 Futures OHLCV 한 구간이다.
type Candle struct {
	Time   int64   `json:"time"`
	Open   Decimal `json:"open"`
	High   Decimal `json:"high"`
	Low    Decimal `json:"low"`
	Close  Decimal `json:"close"`
	Volume Decimal `json:"volume"`
}

// CandlePage는 캔들과 다음 페이지 존재 여부다.
type CandlePage struct {
	Candles     []Candle
	MoreCandles bool
	Raw         json.RawMessage
}

// CurrencyBalance는 multi-collateral 계정의 통화별 잔고다.
type CurrencyBalance struct {
	Quantity   Decimal `json:"quantity"`
	Value      Decimal `json:"value"`
	Collateral Decimal `json:"collateral"`
	Available  Decimal `json:"available"`
}

// AccountAuxiliary는 단일 collateral margin 계정의 파생 잔고 요약이다.
type AccountAuxiliary struct {
	AvailableFunds Decimal `json:"af"`
	Funding        Decimal `json:"funding"`
	PNL            Decimal `json:"pnl"`
	PortfolioValue Decimal `json:"pv"`
	USDValue       Decimal `json:"usd"`
}

// MarginRequirements는 단일 collateral margin 계정의 증거금 기준이다.
type MarginRequirements struct {
	Initial     Decimal `json:"im"`
	Liquidation Decimal `json:"lt"`
	Maintenance Decimal `json:"mm"`
	Termination Decimal `json:"tt"`
}

// TriggerEstimates는 증거금 단계별 예상 trigger 가격이다.
type TriggerEstimates struct {
	Initial     Decimal `json:"im"`
	Liquidation Decimal `json:"lt"`
	Maintenance Decimal `json:"mm"`
	Termination Decimal `json:"tt"`
}

// Account는 cash, margin, multi-collateral 계정의 공통 필드다.
type Account struct {
	Name                    string
	Type                    string                     `json:"type"`
	Currency                string                     `json:"currency"`
	Auxiliary               AccountAuxiliary           `json:"auxiliary"`
	Balances                map[string]Decimal         `json:"balances"`
	Currencies              map[string]CurrencyBalance `json:"currencies"`
	MarginRequirements      MarginRequirements         `json:"marginRequirements"`
	TriggerEstimates        TriggerEstimates           `json:"triggerEstimates"`
	BalanceValue            Decimal                    `json:"balanceValue"`
	PortfolioValue          Decimal                    `json:"portfolioValue"`
	CollateralValue         Decimal                    `json:"collateralValue"`
	InitialMargin           Decimal                    `json:"initialMargin"`
	InitialMarginWithOrders Decimal                    `json:"initialMarginWithOrders"`
	MaintenanceMargin       Decimal                    `json:"maintenanceMargin"`
	PNL                     Decimal                    `json:"pnl"`
	UnrealizedFunding       Decimal                    `json:"unrealizedFunding"`
	TotalUnrealized         Decimal                    `json:"totalUnrealized"`
	TotalUnrealizedAsMargin Decimal                    `json:"totalUnrealizedAsMargin"`
	MarginEquity            Decimal                    `json:"marginEquity"`
	AvailableMargin         Decimal                    `json:"availableMargin"`
	Raw                     json.RawMessage            `json:"-"`
}

// Position은 현재 열린 Futures 포지션이다.
type Position struct {
	Side                 string          `json:"side"`
	Symbol               string          `json:"symbol"`
	Price                Decimal         `json:"price"`
	Size                 Decimal         `json:"size"`
	UnrealizedPNL        Decimal         `json:"unrealizedPnl"`
	UnrealizedFunding    Decimal         `json:"unrealizedFunding"`
	PNLCurrency          string          `json:"pnlCurrency"`
	MaximumFixedLeverage Decimal         `json:"maxFixedLeverage"`
	Raw                  json.RawMessage `json:"-"`
}

// Order는 Futures 주문의 현재 상태다.
type Order struct {
	OrderID        string          `json:"order_id"`
	ClientOrderID  string          `json:"cliOrdId"`
	Symbol         string          `json:"symbol"`
	Side           Side            `json:"side"`
	OrderType      OrderType       `json:"orderType"`
	LimitPrice     Decimal         `json:"limitPrice"`
	UnfilledSize   Decimal         `json:"unfilledSize"`
	FilledSize     Decimal         `json:"filledSize"`
	ReceivedTime   string          `json:"receivedTime"`
	LastUpdateTime string          `json:"lastUpdateTime"`
	Status         string          `json:"status"`
	ReduceOnly     bool            `json:"reduceOnly"`
	Raw            json.RawMessage `json:"-"`
}

// Fill은 계정의 Futures 체결 한 건이다.
type Fill struct {
	FillID   string          `json:"fill_id"`
	Symbol   string          `json:"symbol"`
	Side     Side            `json:"side"`
	OrderID  string          `json:"order_id"`
	Size     Decimal         `json:"size"`
	Price    Decimal         `json:"price"`
	FillTime string          `json:"fillTime"`
	FillType string          `json:"fillType"`
	Raw      json.RawMessage `json:"-"`
}

// OrderReference는 Futures 주문 접수 결과다.
type OrderReference struct {
	OrderID      string          `json:"order_id"`
	Status       string          `json:"status"`
	ReceivedTime string          `json:"receivedTime"`
	Raw          json.RawMessage `json:"-"`
}

// CancelResult는 Futures 취소 접수 결과다.
type CancelResult struct {
	OrderID      string          `json:"order_id"`
	Status       string          `json:"status"`
	ReceivedTime string          `json:"receivedTime"`
	Raw          json.RawMessage `json:"-"`
}

// OrderStatus는 지정한 주문의 최근 상태다.
type OrderStatus struct {
	Order        json.RawMessage `json:"order"`
	Status       string          `json:"status"`
	UpdateReason string          `json:"updateReason"`
	Error        string          `json:"error"`
	Raw          json.RawMessage `json:"-"`
}
