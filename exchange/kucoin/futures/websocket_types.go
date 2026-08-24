package futures

import (
	"encoding/json"
	"fmt"
	"strings"
)

// StreamChannel은 KuCoin Classic Futures WebSocket 채널이다.
type StreamChannel string

const (
	StreamChannelTicker      StreamChannel = "ticker"
	StreamChannelLevel2      StreamChannel = "level2"
	StreamChannelOrderBook5  StreamChannel = "orderbook5"
	StreamChannelOrderBook50 StreamChannel = "orderbook50"
	StreamChannelCandles     StreamChannel = "candles"
	StreamChannelTrade       StreamChannel = "trade"
	StreamChannelOrders      StreamChannel = "orders"
	StreamChannelBalance     StreamChannel = "balance"
	StreamChannelPositions   StreamChannel = "positions"
)

// StreamCandleInterval은 Futures WebSocket 캔들 구간이다.
type StreamCandleInterval string

const (
	StreamCandle1Minute   StreamCandleInterval = "1min"
	StreamCandle3Minutes  StreamCandleInterval = "3min"
	StreamCandle5Minutes  StreamCandleInterval = "5min"
	StreamCandle15Minutes StreamCandleInterval = "15min"
	StreamCandle30Minutes StreamCandleInterval = "30min"
	StreamCandle1Hour     StreamCandleInterval = "1hour"
	StreamCandle2Hours    StreamCandleInterval = "2hour"
	StreamCandle4Hours    StreamCandleInterval = "4hour"
	StreamCandle8Hours    StreamCandleInterval = "8hour"
	StreamCandle12Hours   StreamCandleInterval = "12hour"
	StreamCandle1Day      StreamCandleInterval = "1day"
	StreamCandle1Week     StreamCandleInterval = "1week"
	StreamCandle1Month    StreamCandleInterval = "1month"
)

// StreamSubscription은 채널, 선택적인 계약 symbol과 캔들 구간을 정의한다.
type StreamSubscription struct {
	Channel  StreamChannel
	Symbol   string
	Interval StreamCandleInterval
}

// StreamRequest는 연결 직후 복구할 Futures 구독 목록이다.
type StreamRequest struct {
	Subscriptions []StreamSubscription
}

// StreamMessage는 데이터, 연결 상태, 구독 응답, pong 또는 오류 한 건이다.
type StreamMessage struct {
	ID           string
	Type         string
	Topic        string
	Subject      string
	Channel      StreamChannel
	Private      bool
	UserID       string
	Sequence     int64
	ErrorCode    string
	ErrorMessage string
	Data         json.RawMessage
	Raw          json.RawMessage
}

// Decode는 message 이벤트의 data를 지정한 타입으로 변환한다.
func (message StreamMessage) Decode(target any) error {
	if target == nil {
		return fmt.Errorf("KuCoin Futures stream decode target is nil")
	}
	if message.Type != "message" || len(message.Data) == 0 {
		return fmt.Errorf("KuCoin Futures stream message does not contain a data event")
	}
	if err := json.Unmarshal(message.Data, target); err != nil {
		return fmt.Errorf("decode KuCoin Futures stream event: %w", err)
	}
	return nil
}

// StreamTicker는 Ticker V2의 실시간 최우선 호가다.
type StreamTicker struct {
	Symbol      string  `json:"symbol"`
	Sequence    int64   `json:"sequence"`
	BestBidSize Decimal `json:"bestBidSize"`
	BestBid     Decimal `json:"bestBidPrice"`
	BestAsk     Decimal `json:"bestAskPrice"`
	BestAskSize Decimal `json:"bestAskSize"`
	Timestamp   int64   `json:"ts"`
}

// StreamLevel2Change는 증분 호가 한 단계의 가격, 방향과 새 계약 수량이다.
type StreamLevel2Change struct {
	Price Decimal
	Side  Side
	Size  Decimal
}

// StreamLevel2는 연속 sequence가 있는 Futures 증분 호가 변경이다.
type StreamLevel2 struct {
	Sequence  int64
	Change    StreamLevel2Change
	Timestamp int64
}

// UnmarshalJSON은 쉼표 구분 Futures 호가 변경을 구조체로 변환한다.
func (value *StreamLevel2) UnmarshalJSON(data []byte) error {
	var wire struct {
		Sequence  int64  `json:"sequence"`
		Change    string `json:"change"`
		Timestamp int64  `json:"timestamp"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return fmt.Errorf("decode KuCoin Futures level2 change: %w", err)
	}
	fields := strings.Split(wire.Change, ",")
	if len(fields) != 3 || !decimalPattern.MatchString(fields[0]) || !decimalPattern.MatchString(fields[2]) {
		return fmt.Errorf("invalid KuCoin Futures level2 change %q", wire.Change)
	}
	side := Side(fields[1])
	if side != SideBuy && side != SideSell {
		return fmt.Errorf("invalid KuCoin Futures level2 side %q", fields[1])
	}
	value.Sequence = wire.Sequence
	value.Change = StreamLevel2Change{Price: Decimal(fields[0]), Side: side, Size: Decimal(fields[2])}
	value.Timestamp = wire.Timestamp
	return nil
}

// StreamOrderBook은 5단계 또는 50단계 실시간 Futures 호가 snapshot이다.
type StreamOrderBook struct {
	Asks             []BookLevel `json:"asks"`
	Bids             []BookLevel `json:"bids"`
	Sequence         int64       `json:"sequence"`
	Timestamp        int64       `json:"timestamp"`
	ReceiveTimestamp int64       `json:"ts"`
}

// StreamCandleValue는 위치 기반 Futures WebSocket 캔들 값이다.
type StreamCandleValue struct {
	Timestamp int64
	Open      Decimal
	Close     Decimal
	High      Decimal
	Low       Decimal
	Volume    Decimal
	Turnover  Decimal
}

// UnmarshalJSON은 Futures WebSocket 캔들 배열을 구조체로 변환한다.
func (value *StreamCandleValue) UnmarshalJSON(data []byte) error {
	var fields []json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return fmt.Errorf("decode KuCoin Futures stream candle: %w", err)
	}
	if len(fields) != 7 {
		return fmt.Errorf("KuCoin Futures stream candle has %d fields, want 7", len(fields))
	}
	timestampText, err := decimalFromRaw(fields[0])
	if err != nil || strings.ContainsAny(string(timestampText), ".eE") {
		return fmt.Errorf("decode KuCoin Futures stream candle timestamp")
	}
	var timestamp int64
	if _, err := fmt.Sscan(string(timestampText), &timestamp); err != nil {
		return fmt.Errorf("decode KuCoin Futures stream candle timestamp: %w", err)
	}
	values := []*Decimal{&value.Open, &value.Close, &value.High, &value.Low, &value.Volume, &value.Turnover}
	for index, target := range values {
		parsed, err := decimalFromRaw(fields[index+1])
		if err != nil {
			return fmt.Errorf("decode KuCoin Futures stream candle field %d: %w", index+1, err)
		}
		*target = parsed
	}
	value.Timestamp = timestamp
	return nil
}

// StreamCandle은 변경된 최신 Futures 캔들과 matching engine 시각이다.
type StreamCandle struct {
	Symbol  string            `json:"symbol"`
	Candles StreamCandleValue `json:"candles"`
	Time    int64             `json:"time"`
	Latest  bool              `json:"S"`
}

// StreamTrade는 실시간 공개 Futures 체결 한 건이다.
type StreamTrade struct {
	Symbol       string  `json:"symbol"`
	Sequence     int64   `json:"sequence"`
	Side         Side    `json:"side"`
	Size         Decimal `json:"size"`
	Price        Decimal `json:"price"`
	TakerOrderID string  `json:"takerOrderId"`
	MakerOrderID string  `json:"makerOrderId"`
	TradeID      string  `json:"tradeId"`
	Timestamp    int64   `json:"ts"`
}

// StreamOrder는 private Futures 주문 생성·변경·체결·종료 이벤트다.
type StreamOrder struct {
	Symbol          string       `json:"symbol"`
	OrderType       OrderType    `json:"orderType"`
	TradeType       string       `json:"tradeType"`
	Side            Side         `json:"side"`
	CanceledSize    Decimal      `json:"canceledSize"`
	AllCanceledSize Decimal      `json:"allCanceledSize"`
	OrderID         string       `json:"orderId"`
	ClientOrderID   string       `json:"clientOid"`
	PositionSide    PositionSide `json:"positionSide"`
	Liquidity       string       `json:"liquidity"`
	MarginMode      MarginMode   `json:"marginMode"`
	EventType       string       `json:"type"`
	FeeType         string       `json:"feeType"`
	OrderTime       int64        `json:"orderTime"`
	Size            Decimal      `json:"size"`
	OldSize         Decimal      `json:"oldSize"`
	FilledSize      Decimal      `json:"filledSize"`
	Price           Decimal      `json:"price"`
	MatchPrice      Decimal      `json:"matchPrice"`
	MatchSize       Decimal      `json:"matchSize"`
	RemainSize      Decimal      `json:"remainSize"`
	TradeID         string       `json:"tradeId"`
	Status          string       `json:"status"`
	Timestamp       int64        `json:"ts"`
}

// StreamBalance는 walletBalance.change와 기존 잔고 이벤트를 함께 수용한다.
type StreamBalance struct {
	CrossPositionMargin      Decimal `json:"crossPosMargin"`
	IsolatedOrderMargin      Decimal `json:"isolatedOrderMargin"`
	HoldBalance              Decimal `json:"holdBalance"`
	Equity                   Decimal `json:"equity"`
	Version                  Decimal `json:"version"`
	AvailableBalance         Decimal `json:"availableBalance"`
	IsolatedPositionMargin   Decimal `json:"isolatedPosMargin"`
	MaximumWithdrawAmount    Decimal `json:"maxWithdrawAmount"`
	WalletBalance            Decimal `json:"walletBalance"`
	IsolatedFundingFeeMargin Decimal `json:"isolatedFundingFeeMargin"`
	CrossUnrealisedPNL       Decimal `json:"crossUnPnl"`
	TotalCrossMargin         Decimal `json:"totalCrossMargin"`
	Currency                 string  `json:"currency"`
	IsolatedUnrealisedPNL    Decimal `json:"isolatedUnPnl"`
	CrossOrderMargin         Decimal `json:"crossOrderMargin"`
	OrderMargin              Decimal `json:"orderMargin"`
	WithdrawalHold           Decimal `json:"withdrawHold"`
	Timestamp                Decimal `json:"timestamp"`
}

// StreamPosition은 private Futures 포지션 변경 이벤트의 주요 필드다.
type StreamPosition struct {
	Symbol                       string       `json:"symbol"`
	MaintenanceMarginRequirement Decimal      `json:"maintMarginReq"`
	RiskLimit                    Decimal      `json:"riskLimit"`
	RealLeverage                 Decimal      `json:"realLeverage"`
	CrossMode                    bool         `json:"crossMode"`
	DeleveragingPercentage       Decimal      `json:"delevPercentage"`
	OpeningTimestamp             int64        `json:"openingTimestamp"`
	AutoDeposit                  bool         `json:"autoDeposit"`
	CurrentTimestamp             int64        `json:"currentTimestamp"`
	CurrentQuantity              int64        `json:"currentQty"`
	CurrentCost                  Decimal      `json:"currentCost"`
	CurrentCommission            Decimal      `json:"currentComm"`
	UnrealisedCost               Decimal      `json:"unrealisedCost"`
	RealisedCost                 Decimal      `json:"realisedCost"`
	Open                         bool         `json:"isOpen"`
	MarkPrice                    Decimal      `json:"markPrice"`
	MarkValue                    Decimal      `json:"markValue"`
	PositionCost                 Decimal      `json:"posCost"`
	PositionMargin               Decimal      `json:"posMargin"`
	MaintenanceMargin            Decimal      `json:"maintMargin"`
	AverageEntryPrice            Decimal      `json:"avgEntryPrice"`
	LiquidationPrice             Decimal      `json:"liquidationPrice"`
	BankruptPrice                Decimal      `json:"bankruptPrice"`
	SettleCurrency               string       `json:"settleCurrency"`
	ChangeReason                 string       `json:"changeReason"`
	RealisedPNL                  Decimal      `json:"realisedPnl"`
	UnrealisedPNL                Decimal      `json:"unrealisedPnl"`
	Leverage                     Decimal      `json:"leverage"`
	MarginMode                   MarginMode   `json:"marginMode"`
	PositionSide                 PositionSide `json:"positionSide"`
	FundingQuantity              int64        `json:"qty"`
	FundingTime                  int64        `json:"fundingTime"`
	FundingFee                   Decimal      `json:"fundingFee"`
	FundingRate                  Decimal      `json:"fundingRate"`
	Timestamp                    int64        `json:"ts"`
	Message                      string       `json:"msg"`
	Success                      bool         `json:"success"`
	RiskLimitLevel               int          `json:"riskLimitLevel"`
}

func (interval StreamCandleInterval) valid() bool {
	switch interval {
	case StreamCandle1Minute, StreamCandle3Minutes, StreamCandle5Minutes,
		StreamCandle15Minutes, StreamCandle30Minutes, StreamCandle1Hour,
		StreamCandle2Hours, StreamCandle4Hours, StreamCandle8Hours,
		StreamCandle12Hours, StreamCandle1Day, StreamCandle1Week, StreamCandle1Month:
		return true
	default:
		return false
	}
}

func decimalFromRaw(raw json.RawMessage) (Decimal, error) {
	var value Decimal
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", err
	}
	return value, nil
}
