package usdm

import (
	"encoding/json"
	"fmt"
)

// StreamRoute는 Binance가 분리한 public 또는 market WebSocket 진입점이다.
type StreamRoute string

const (
	StreamRoutePublic StreamRoute = "public"
	StreamRouteMarket StreamRoute = "market"
)

// StreamSubscription은 구독 이름과 반드시 사용해야 하는 진입점을 함께 보존한다.
type StreamSubscription struct {
	Route StreamRoute
	Name  string
}

// MarketStreamRequest는 한 연결에서 복구할 같은 진입점의 public 구독 목록이다.
type MarketStreamRequest struct {
	Subscriptions []StreamSubscription
}

// WebSocketError는 Binance WebSocket 구독 제어 오류다.
type WebSocketError struct {
	Code    int    `json:"code"`
	Message string `json:"msg"`
}

// WebSocketResponse는 구독 또는 구독 해제 응답이다.
type WebSocketResponse struct {
	ID     json.RawMessage
	Result json.RawMessage
	Error  *WebSocketError
	Raw    json.RawMessage
}

// MarketStreamMessage는 public·market 이벤트 또는 구독 제어 응답 한 건이다.
type MarketStreamMessage struct {
	Stream    string
	EventType string
	EventTime int64
	Payload   json.RawMessage
	Response  *WebSocketResponse
	Raw       json.RawMessage
}

// Decode는 public·market 이벤트 payload를 지정 타입으로 변환한다.
func (message MarketStreamMessage) Decode(target any) error {
	if target == nil {
		return fmt.Errorf("Binance USD-M market stream decode target is nil")
	}
	if message.Response != nil || len(message.Payload) == 0 {
		return fmt.Errorf("Binance USD-M market stream message does not contain an event")
	}
	if err := json.Unmarshal(message.Payload, target); err != nil {
		return fmt.Errorf("decode Binance USD-M market stream event: %w", err)
	}
	return nil
}

// UserDataStreamMessage는 private 계정 이벤트 한 건이다.
type UserDataStreamMessage struct {
	EventType       string
	EventTime       int64
	TransactionTime int64
	Payload         json.RawMessage
	Raw             json.RawMessage
}

// Decode는 private 이벤트 payload를 지정 타입으로 변환한다.
func (message UserDataStreamMessage) Decode(target any) error {
	if target == nil {
		return fmt.Errorf("Binance USD-M user data stream decode target is nil")
	}
	if len(message.Payload) == 0 {
		return fmt.Errorf("Binance USD-M user data stream message does not contain an event")
	}
	if err := json.Unmarshal(message.Payload, target); err != nil {
		return fmt.Errorf("decode Binance USD-M user data stream event: %w", err)
	}
	return nil
}

// StreamAggregateTrade는 동일 taker 주문 단위로 합친 공개 체결이다.
type StreamAggregateTrade struct {
	EventType    string `json:"e"`
	EventTime    int64  `json:"E"`
	Symbol       string `json:"s"`
	AggregateID  int64  `json:"a"`
	Price        string `json:"p"`
	Quantity     string `json:"q"`
	FirstTradeID int64  `json:"f"`
	LastTradeID  int64  `json:"l"`
	TradeTime    int64  `json:"T"`
	BuyerIsMaker bool   `json:"m"`
	Pair         string `json:"ps"`
	SymbolType   int    `json:"st"`
}

// StreamMarkPrice는 마크가·지수가·펀딩비와 다음 펀딩 시각이다.
type StreamMarkPrice struct {
	EventType       string `json:"e"`
	EventTime       int64  `json:"E"`
	Symbol          string `json:"s"`
	MarkPrice       string `json:"p"`
	IndexPrice      string `json:"i"`
	SettlementPrice string `json:"P"`
	FundingRate     string `json:"r"`
	NextFundingTime int64  `json:"T"`
	Pair            string `json:"ps"`
	SymbolType      int    `json:"st"`
}

// StreamKlineEvent는 무기한 Futures 캔들 갱신 이벤트다.
type StreamKlineEvent struct {
	EventType string      `json:"e"`
	EventTime int64       `json:"E"`
	Symbol    string      `json:"s"`
	Kline     StreamKline `json:"k"`
}

// StreamKline은 WebSocket 캔들의 현재 상태다.
type StreamKline struct {
	OpenTime            int64          `json:"t"`
	CloseTime           int64          `json:"T"`
	Symbol              string         `json:"s"`
	Interval            CandleInterval `json:"i"`
	FirstTradeID        int64          `json:"f"`
	LastTradeID         int64          `json:"L"`
	Open                string         `json:"o"`
	Close               string         `json:"c"`
	High                string         `json:"h"`
	Low                 string         `json:"l"`
	Volume              string         `json:"v"`
	TradeCount          int64          `json:"n"`
	Closed              bool           `json:"x"`
	QuoteVolume         string         `json:"q"`
	TakerBuyVolume      string         `json:"V"`
	TakerBuyQuoteVolume string         `json:"Q"`
}

// StreamTicker는 무기한 Futures 24시간 rolling 통계다.
type StreamTicker struct {
	EventType          string `json:"e"`
	EventTime          int64  `json:"E"`
	Symbol             string `json:"s"`
	PriceChange        string `json:"p"`
	PriceChangePercent string `json:"P"`
	WeightedAverage    string `json:"w"`
	LastPrice          string `json:"c"`
	LastQuantity       string `json:"Q"`
	OpenPrice          string `json:"o"`
	HighPrice          string `json:"h"`
	LowPrice           string `json:"l"`
	Volume             string `json:"v"`
	QuoteVolume        string `json:"q"`
	OpenTime           int64  `json:"O"`
	CloseTime          int64  `json:"C"`
	FirstTradeID       int64  `json:"F"`
	LastTradeID        int64  `json:"L"`
	TradeCount         int64  `json:"n"`
	Pair               string `json:"ps"`
	SymbolType         int    `json:"st"`
}

// StreamBookTicker는 실시간 최우선 매수·매도 호가다.
type StreamBookTicker struct {
	EventType       string `json:"e"`
	UpdateID        int64  `json:"u"`
	EventTime       int64  `json:"E"`
	TransactionTime int64  `json:"T"`
	Symbol          string `json:"s"`
	Pair            string `json:"ps"`
	BestBidPrice    string `json:"b"`
	BestBidQuantity string `json:"B"`
	BestAskPrice    string `json:"a"`
	BestAskQuantity string `json:"A"`
	SymbolType      int    `json:"st"`
}

// StreamDepth는 이전 sequence 연결 정보가 있는 증분 또는 부분 호가다.
type StreamDepth struct {
	EventType       string      `json:"e"`
	EventTime       int64       `json:"E"`
	TransactionTime int64       `json:"T"`
	Symbol          string      `json:"s"`
	Pair            string      `json:"ps"`
	SymbolType      int         `json:"st"`
	FirstUpdateID   int64       `json:"U"`
	FinalUpdateID   int64       `json:"u"`
	PreviousUpdate  int64       `json:"pu"`
	Bids            []BookLevel `json:"b"`
	Asks            []BookLevel `json:"a"`
}

// StreamAccountUpdate는 잔고 또는 포지션이 실제로 바뀐 private 이벤트다.
type StreamAccountUpdate struct {
	EventType       string            `json:"e"`
	EventTime       int64             `json:"E"`
	TransactionTime int64             `json:"T"`
	Account         StreamAccountData `json:"a"`
}

// StreamAccountData는 변경 이유와 바뀐 자산·포지션만 포함한다.
type StreamAccountData struct {
	Reason    string           `json:"m"`
	Balances  []StreamBalance  `json:"B"`
	Positions []StreamPosition `json:"P"`
}

// StreamBalance는 private 계정 자산 변경 후 잔고다.
type StreamBalance struct {
	Asset              string `json:"a"`
	WalletBalance      string `json:"wb"`
	CrossWalletBalance string `json:"cw"`
	BalanceChange      string `json:"bc"`
}

// StreamPosition은 private 이벤트에 포함된 변경 포지션이다.
type StreamPosition struct {
	Symbol              string       `json:"s"`
	PositionAmount      string       `json:"pa"`
	EntryPrice          string       `json:"ep"`
	BreakEvenPrice      string       `json:"bep"`
	AccumulatedRealized string       `json:"cr"`
	UnrealizedProfit    string       `json:"up"`
	MarginType          string       `json:"mt"`
	IsolatedWallet      string       `json:"iw"`
	PositionSide        PositionSide `json:"ps"`
}

// StreamOrderTradeUpdate는 private 주문 상태와 마지막·누적 체결 정보다.
type StreamOrderTradeUpdate struct {
	EventType       string          `json:"e"`
	EventTime       int64           `json:"E"`
	TransactionTime int64           `json:"T"`
	Order           StreamOrderData `json:"o"`
}

// StreamOrderData는 주문과 실행 상태의 전체 핵심 필드다.
type StreamOrderData struct {
	Symbol                   string       `json:"s"`
	ClientOrderID            string       `json:"c"`
	Side                     Side         `json:"S"`
	OrderType                OrderType    `json:"o"`
	TimeInForce              TimeInForce  `json:"f"`
	OriginalQuantity         string       `json:"q"`
	OriginalPrice            string       `json:"p"`
	AveragePrice             string       `json:"ap"`
	StopPrice                string       `json:"sp"`
	ExecutionType            string       `json:"x"`
	OrderStatus              OrderStatus  `json:"X"`
	OrderID                  int64        `json:"i"`
	LastFilledQuantity       string       `json:"l"`
	CumulativeFilledQuantity string       `json:"z"`
	LastFilledPrice          string       `json:"L"`
	CommissionAsset          string       `json:"N"`
	Commission               string       `json:"n"`
	OrderTradeTime           int64        `json:"T"`
	TradeID                  int64        `json:"t"`
	BidNotional              string       `json:"b"`
	AskNotional              string       `json:"a"`
	Maker                    bool         `json:"m"`
	ReduceOnly               bool         `json:"R"`
	WorkingType              WorkingType  `json:"wt"`
	OriginalOrderType        OrderType    `json:"ot"`
	PositionSide             PositionSide `json:"ps"`
	CloseAll                 bool         `json:"cp"`
	ActivationPrice          string       `json:"AP"`
	CallbackRate             string       `json:"cr"`
	RealizedProfit           string       `json:"rp"`
	SelfTradePreventionMode  string       `json:"V"`
	PriceMatchMode           string       `json:"pm"`
	GoodTillDate             int64        `json:"gtd"`
	ExpiryReason             int          `json:"er"`
}

// StreamMarginCall은 위험 자산과 포지션의 마진콜 안내다.
type StreamMarginCall struct {
	EventType          string                     `json:"e"`
	EventTime          int64                      `json:"E"`
	CrossWalletBalance string                     `json:"cw"`
	Positions          []StreamMarginCallPosition `json:"p"`
}

// StreamMarginCallPosition은 마진콜 대상 포지션의 위험 정보다.
type StreamMarginCallPosition struct {
	Symbol            string       `json:"s"`
	PositionSide      PositionSide `json:"ps"`
	PositionAmount    string       `json:"pa"`
	MarginType        string       `json:"mt"`
	IsolatedWallet    string       `json:"iw"`
	MarkPrice         string       `json:"mp"`
	UnrealizedProfit  string       `json:"up"`
	MaintenanceMargin string       `json:"mm"`
}

// StreamListenKeyExpired는 갱신되지 않은 private 접속 키 만료 알림이다.
type StreamListenKeyExpired struct {
	EventType string `json:"e"`
	EventTime int64  `json:"E"`
}
