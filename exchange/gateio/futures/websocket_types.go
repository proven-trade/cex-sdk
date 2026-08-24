package futures

import (
	"encoding/json"
	"fmt"
)

// StreamChannel은 Gate.io API v4 무기한 Futures WebSocket 채널이다.
type StreamChannel string

const (
	StreamChannelTicker          StreamChannel = "ticker"
	StreamChannelTrades          StreamChannel = "trades"
	StreamChannelCandles         StreamChannel = "candles"
	StreamChannelBookTicker      StreamChannel = "book_ticker"
	StreamChannelOrderBookUpdate StreamChannel = "order_book_update"
	StreamChannelOrders          StreamChannel = "orders"
	StreamChannelUserTrades      StreamChannel = "user_trades"
	StreamChannelBalances        StreamChannel = "balances"
	StreamChannelPositions       StreamChannel = "positions"
)

// StreamUpdateInterval은 증분 호가 통지 주기다.
type StreamUpdateInterval string

const (
	StreamUpdate20Millis  StreamUpdateInterval = "20ms"
	StreamUpdate100Millis StreamUpdateInterval = "100ms"
)

// StreamBookLevel은 WebSocket 호가 한 단계의 가격과 정밀도 보존 계약 수량이다.
type StreamBookLevel struct {
	Price string  `json:"p"`
	Size  Decimal `json:"s"`
}

// StreamSubscription은 채널, 계약과 채널별 선택 값을 정의한다.
type StreamSubscription struct {
	Channel        StreamChannel
	Contract       string
	CandleInterval CandleInterval
	UpdateInterval StreamUpdateInterval
	OrderBookLevel int
}

// StreamRequest는 정산 통화와 연결 직후 복구할 구독 목록이다.
type StreamRequest struct {
	Settlement    Settlement
	Subscriptions []StreamSubscription
}

// StreamError는 구독 요청을 거절한 서버 오류다.
type StreamError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// StreamMessage는 데이터, 구독 응답, pong 또는 시스템 알림 한 건이다.
type StreamMessage struct {
	ID          string
	Time        int64
	TimeMilli   int64
	ChannelName string
	Channel     StreamChannel
	Event       string
	Private     bool
	Error       *StreamError
	Result      json.RawMessage
	Raw         json.RawMessage
}

// Decode는 update 또는 all 이벤트의 result를 지정한 타입으로 변환한다.
func (message StreamMessage) Decode(target any) error {
	if target == nil {
		return fmt.Errorf("Gate.io Futures stream decode target is nil")
	}
	if message.Event != "update" && message.Event != "all" || len(message.Result) == 0 {
		return fmt.Errorf("Gate.io Futures stream message does not contain a data event")
	}
	if err := json.Unmarshal(message.Result, target); err != nil {
		return fmt.Errorf("decode Gate.io Futures stream event: %w", err)
	}
	return nil
}

// StreamTicker는 실시간 무기한 Futures 현재가와 24시간 통계다.
type StreamTicker struct {
	Contract              string `json:"contract"`
	Last                  string `json:"last"`
	ChangePercent         string `json:"change_percentage"`
	FundingRate           string `json:"funding_rate"`
	FundingRateIndicative string `json:"funding_rate_indicative"`
	MarkPrice             string `json:"mark_price"`
	IndexPrice            string `json:"index_price"`
	TotalSize             string `json:"total_size"`
	Volume24Hours         string `json:"volume_24h"`
	Volume24HoursBase     string `json:"volume_24h_base"`
	Volume24HoursQuote    string `json:"volume_24h_quote"`
	Volume24HoursSettle   string `json:"volume_24h_settle"`
	Low24Hours            string `json:"low_24h"`
	High24Hours           string `json:"high_24h"`
}

// StreamTrade는 실시간 공개 무기한 Futures 체결이다.
type StreamTrade struct {
	ID             Identifier `json:"id"`
	CreatedAt      Decimal    `json:"create_time"`
	CreatedAtMilli Decimal    `json:"create_time_ms"`
	Contract       string     `json:"contract"`
	Size           Decimal    `json:"size"`
	Price          string     `json:"price"`
	Internal       bool       `json:"is_internal"`
}

// StreamCandle은 변경된 최신 무기한 Futures 캔들이다.
type StreamCandle struct {
	Timestamp Decimal `json:"t"`
	Volume    Decimal `json:"v"`
	Close     string  `json:"c"`
	High      string  `json:"h"`
	Low       string  `json:"l"`
	Open      string  `json:"o"`
	Name      string  `json:"n"`
	Amount    Decimal `json:"a"`
}

// StreamBookTicker는 실시간 최우선 매수·매도 호가다.
type StreamBookTicker struct {
	Timestamp   int64   `json:"t"`
	UpdateID    int64   `json:"u"`
	Contract    string  `json:"s"`
	BestBid     string  `json:"b"`
	BestBidSize Decimal `json:"B"`
	BestAsk     string  `json:"a"`
	BestAskSize Decimal `json:"A"`
}

// StreamOrderBookUpdate는 sequence 범위가 있는 절대 수량 기반 증분 호가다.
type StreamOrderBookUpdate struct {
	Timestamp     int64             `json:"t"`
	Contract      string            `json:"s"`
	FirstUpdateID int64             `json:"U"`
	LastUpdateID  int64             `json:"u"`
	Bids          []StreamBookLevel `json:"b"`
	Asks          []StreamBookLevel `json:"a"`
}

// StreamOrder는 private 무기한 Futures 주문 생성·변경·체결·종료 이벤트다.
type StreamOrder struct {
	ID                   Identifier          `json:"id"`
	User                 json.RawMessage     `json:"user"`
	Contract             string              `json:"contract"`
	CreatedAt            Decimal             `json:"create_time"`
	CreatedAtMilli       Decimal             `json:"create_time_ms"`
	FinishedAt           Decimal             `json:"finish_time"`
	FinishedAtMilli      Decimal             `json:"finish_time_ms"`
	Size                 Decimal             `json:"size"`
	Iceberg              Decimal             `json:"iceberg"`
	Left                 Decimal             `json:"left"`
	Price                Decimal             `json:"price"`
	FillPrice            Decimal             `json:"fill_price"`
	MakerFeeRate         Decimal             `json:"mkfr"`
	TakerFeeRate         Decimal             `json:"tkfr"`
	ReferenceFeeRate     Decimal             `json:"refr"`
	ReferenceUser        json.RawMessage     `json:"refu"`
	TimeInForce          TimeInForce         `json:"tif"`
	ReduceOnly           bool                `json:"is_reduce_only"`
	Close                bool                `json:"is_close"`
	Liquidation          bool                `json:"is_liq"`
	Status               string              `json:"status"`
	FinishAs             string              `json:"finish_as"`
	ClientOrderID        string              `json:"text"`
	SelfTradePrevention  SelfTradePrevention `json:"stp_act"`
	PositionMarginMode   PositionMarginMode  `json:"pos_margin_mode"`
	MarketOrderSlipRatio Decimal             `json:"market_order_slip_ratio"`
}

// StreamUserTrade는 private 무기한 Futures 계정 체결이다.
type StreamUserTrade struct {
	ID             Identifier `json:"id"`
	CreatedAt      Decimal    `json:"create_time"`
	CreatedAtMilli Decimal    `json:"create_time_ms"`
	Contract       string     `json:"contract"`
	OrderID        Identifier `json:"order_id"`
	Size           Decimal    `json:"size"`
	Price          string     `json:"price"`
	Role           string     `json:"role"`
	ClientOrderID  string     `json:"text"`
	Fee            Decimal    `json:"fee"`
	PointFee       Decimal    `json:"point_fee"`
	CloseSize      Decimal    `json:"close_size"`
	TradeValue     Decimal    `json:"trade_value"`
}

// StreamBalance는 private 무기한 Futures 자산 변경과 변경 후 잔고다.
type StreamBalance struct {
	Balance   Decimal         `json:"balance"`
	Change    Decimal         `json:"change"`
	Text      string          `json:"text"`
	Timestamp Decimal         `json:"time"`
	TimeMilli Decimal         `json:"time_ms"`
	Type      string          `json:"type"`
	User      json.RawMessage `json:"user"`
	Currency  string          `json:"currency"`
}

// StreamPosition은 private 무기한 Futures 포지션 변경 이벤트다.
type StreamPosition struct {
	Contract           string             `json:"contract"`
	CrossLeverageLimit Decimal            `json:"cross_leverage_limit"`
	EntryPrice         Decimal            `json:"entry_price"`
	HistoryPNL         Decimal            `json:"history_pnl"`
	HistoryPoint       Decimal            `json:"history_point"`
	LastClosePNL       Decimal            `json:"last_close_pnl"`
	Leverage           Decimal            `json:"leverage"`
	MaximumLeverage    Decimal            `json:"leverage_max"`
	LiquidationPrice   Decimal            `json:"liq_price"`
	MaintenanceRate    Decimal            `json:"maintenance_rate"`
	Margin             Decimal            `json:"margin"`
	Mode               PositionMode       `json:"mode"`
	PositionMarginMode PositionMarginMode `json:"pos_margin_mode"`
	RealisedPNL        Decimal            `json:"realised_pnl"`
	RealisedPoint      Decimal            `json:"realised_point"`
	RiskLimit          Decimal            `json:"risk_limit"`
	Size               Decimal            `json:"size"`
	Timestamp          Decimal            `json:"time"`
	TimeMilli          Decimal            `json:"time_ms"`
	User               json.RawMessage    `json:"user"`
	UpdateID           int64              `json:"update_id"`
}
