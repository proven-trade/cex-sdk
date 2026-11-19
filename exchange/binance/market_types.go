package binance

import (
	"encoding/json"
	"fmt"
)

// BookLevel은 호가 한 단계의 가격과 수량이다.
type BookLevel struct {
	Price    string
	Quantity string
}

// UnmarshalJSON은 Binance의 [price, quantity] 배열을 BookLevel로 변환한다.
func (level *BookLevel) UnmarshalJSON(data []byte) error {
	var values []string
	if err := json.Unmarshal(data, &values); err != nil {
		return fmt.Errorf("decode Binance book level: %w", err)
	}
	if len(values) != 2 {
		return fmt.Errorf("Binance book level has %d values, want 2", len(values))
	}
	level.Price = values[0]
	level.Quantity = values[1]
	return nil
}

// OrderBook은 지정 깊이의 현재 호가다.
type OrderBook struct {
	LastUpdateID int64           `json:"lastUpdateId"`
	Bids         []BookLevel     `json:"bids"`
	Asks         []BookLevel     `json:"asks"`
	Raw          json.RawMessage `json:"-"`
}

// PublicTrade는 공개 최근 체결 한 건이다.
type PublicTrade struct {
	ID            int64  `json:"id"`
	Price         string `json:"price"`
	Quantity      string `json:"qty"`
	QuoteQuantity string `json:"quoteQty"`
	Time          int64  `json:"time"`
	BuyerMaker    bool   `json:"isBuyerMaker"`
	BestMatch     bool   `json:"isBestMatch"`
}

// BookTicker는 최우선 매수·매도 가격과 수량이다.
type BookTicker struct {
	Symbol      string          `json:"symbol"`
	BidPrice    string          `json:"bidPrice"`
	BidQuantity string          `json:"bidQty"`
	AskPrice    string          `json:"askPrice"`
	AskQuantity string          `json:"askQty"`
	Raw         json.RawMessage `json:"-"`
}

// KlineInterval은 캔들 구간 문자열이다.
type KlineInterval string

const (
	Kline1Second   KlineInterval = "1s"
	Kline1Minute   KlineInterval = "1m"
	Kline3Minutes  KlineInterval = "3m"
	Kline5Minutes  KlineInterval = "5m"
	Kline15Minutes KlineInterval = "15m"
	Kline30Minutes KlineInterval = "30m"
	Kline1Hour     KlineInterval = "1h"
	Kline2Hours    KlineInterval = "2h"
	Kline4Hours    KlineInterval = "4h"
	Kline6Hours    KlineInterval = "6h"
	Kline8Hours    KlineInterval = "8h"
	Kline12Hours   KlineInterval = "12h"
	Kline1Day      KlineInterval = "1d"
	Kline3Days     KlineInterval = "3d"
	Kline1Week     KlineInterval = "1w"
	Kline1Month    KlineInterval = "1M"
)

// Kline은 Binance Spot OHLCV 캔들이다.
type Kline struct {
	OpenTime                 int64
	Open                     string
	High                     string
	Low                      string
	Close                    string
	Volume                   string
	CloseTime                int64
	QuoteAssetVolume         string
	TradeCount               int64
	TakerBuyBaseAssetVolume  string
	TakerBuyQuoteAssetVolume string
}

// UnmarshalJSON은 Binance의 위치 기반 캔들 배열을 Kline으로 변환한다.
func (kline *Kline) UnmarshalJSON(data []byte) error {
	var fields []json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return fmt.Errorf("decode Binance kline: %w", err)
	}
	if len(fields) < 11 {
		return fmt.Errorf("Binance kline has %d fields, want at least 11", len(fields))
	}
	integerTargets := []struct {
		index  int
		target *int64
	}{{0, &kline.OpenTime}, {6, &kline.CloseTime}, {8, &kline.TradeCount}}
	for _, item := range integerTargets {
		if err := json.Unmarshal(fields[item.index], item.target); err != nil {
			return fmt.Errorf("decode Binance kline field %d: %w", item.index, err)
		}
	}
	stringTargets := []struct {
		index  int
		target *string
	}{
		{1, &kline.Open},
		{2, &kline.High},
		{3, &kline.Low},
		{4, &kline.Close},
		{5, &kline.Volume},
		{7, &kline.QuoteAssetVolume},
		{9, &kline.TakerBuyBaseAssetVolume},
		{10, &kline.TakerBuyQuoteAssetVolume},
	}
	for _, item := range stringTargets {
		if err := json.Unmarshal(fields[item.index], item.target); err != nil {
			return fmt.Errorf("decode Binance kline field %d: %w", item.index, err)
		}
	}
	return nil
}

// AggregateTrade는 같은 taker 주문과 가격으로 합쳐진 공개 체결 한 건이다.
type AggregateTrade struct {
	AggregateID  int64  `json:"a"`
	Price        string `json:"p"`
	Quantity     string `json:"q"`
	FirstTradeID int64  `json:"f"`
	LastTradeID  int64  `json:"l"`
	Time         int64  `json:"T"`
	BuyerMaker   bool   `json:"m"`
	BestMatch    bool   `json:"M"`
}

// BlockTrade는 블록 체결 한 건이다. 일반 체결과 달리 isBestMatch가 없다.
type BlockTrade struct {
	ID            int64  `json:"id"`
	Price         string `json:"price"`
	Quantity      string `json:"qty"`
	QuoteQuantity string `json:"quoteQty"`
	Time          int64  `json:"time"`
	BuyerMaker    bool   `json:"isBuyerMaker"`
}

// AveragePrice는 상품의 현재 평균가다.
type AveragePrice struct {
	Minutes   int             `json:"mins"`
	Price     string          `json:"price"`
	CloseTime int64           `json:"closeTime"`
	Raw       json.RawMessage `json:"-"`
}

// TickerType은 ticker 응답의 상세 수준이다.
type TickerType string

const (
	TickerTypeFull TickerType = "FULL"
	TickerTypeMini TickerType = "MINI"
)

// SymbolStatus는 상품 거래 상태 필터 값이다.
type SymbolStatus string

const (
	SymbolStatusTrading    SymbolStatus = "TRADING"
	SymbolStatusHalt       SymbolStatus = "HALT"
	SymbolStatusBreak      SymbolStatus = "BREAK"
	SymbolStatusCancelOnly SymbolStatus = "CANCEL_ONLY"
)

// TickerStatistics는 24시간, 거래일, rolling window 가격 변동 통계다.
// MINI 응답에 없는 필드는 빈 값으로 남고 원본은 Raw에 보존한다.
type TickerStatistics struct {
	Symbol               string          `json:"symbol"`
	PriceChange          string          `json:"priceChange"`
	PriceChangePercent   string          `json:"priceChangePercent"`
	WeightedAveragePrice string          `json:"weightedAvgPrice"`
	PreviousClosePrice   string          `json:"prevClosePrice"`
	LastPrice            string          `json:"lastPrice"`
	LastQuantity         string          `json:"lastQty"`
	BidPrice             string          `json:"bidPrice"`
	BidQuantity          string          `json:"bidQty"`
	AskPrice             string          `json:"askPrice"`
	AskQuantity          string          `json:"askQty"`
	OpenPrice            string          `json:"openPrice"`
	HighPrice            string          `json:"highPrice"`
	LowPrice             string          `json:"lowPrice"`
	Volume               string          `json:"volume"`
	QuoteVolume          string          `json:"quoteVolume"`
	OpenTime             int64           `json:"openTime"`
	CloseTime            int64           `json:"closeTime"`
	FirstTradeID         int64           `json:"firstId"`
	LastTradeID          int64           `json:"lastId"`
	TradeCount           int64           `json:"count"`
	Raw                  json.RawMessage `json:"-"`
}

// ReferencePrice는 상품의 기준가격이다. 설정된 기준가격이 없으면 Price가 nil이다.
type ReferencePrice struct {
	Symbol    string          `json:"symbol"`
	Price     *string         `json:"referencePrice"`
	Timestamp int64           `json:"timestamp"`
	Raw       json.RawMessage `json:"-"`
}

// ReferencePriceCalculation은 기준가격 산출 방식이다.
// 산출 방식에 따라 BucketCount·BucketWidthMillis 또는 ExternalCalculationID만 채워진다.
type ReferencePriceCalculation struct {
	Symbol                string          `json:"symbol"`
	CalculationType       string          `json:"calculationType"`
	BucketCount           int             `json:"bucketCount"`
	BucketWidthMillis     int64           `json:"bucketWidthMs"`
	ExternalCalculationID int64           `json:"externalCalculationId"`
	Raw                   json.RawMessage `json:"-"`
}

// ExecutionRule은 상품에 적용되는 실행 규칙 한 건이다. 현재 PRICE_RANGE만 정의되어 있다.
type ExecutionRule struct {
	RuleType         string `json:"ruleType"`
	BidLimitMultUp   string `json:"bidLimitMultUp"`
	BidLimitMultDown string `json:"bidLimitMultDown"`
	AskLimitMultUp   string `json:"askLimitMultUp"`
	AskLimitMultDown string `json:"askLimitMultDown"`
}

// SymbolExecutionRules는 상품별 실행 규칙 목록이다.
type SymbolExecutionRules struct {
	Symbol string          `json:"symbol"`
	Rules  []ExecutionRule `json:"rules"`
}

// ExecutionRules는 executionRules 조회 응답이다.
type ExecutionRules struct {
	SymbolRules []SymbolExecutionRules `json:"symbolRules"`
	Raw         json.RawMessage        `json:"-"`
}
