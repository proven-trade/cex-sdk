package kraken

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// SpotStreamDecimal은 JSON 숫자와 문자열 숫자를 정밀도 손실 없이 보존한다.
type SpotStreamDecimal string

// UnmarshalJSON은 숫자 또는 문자열 표현을 원문 그대로 저장한다.
func (decimal *SpotStreamDecimal) UnmarshalJSON(data []byte) error {
	if decimal == nil {
		return fmt.Errorf("Kraken Spot stream decimal target is nil")
	}
	value := bytes.TrimSpace(data)
	if bytes.Equal(value, []byte("null")) {
		*decimal = ""
		return nil
	}
	if len(value) > 0 && value[0] == '"' {
		var text string
		if err := json.Unmarshal(value, &text); err != nil {
			return fmt.Errorf("decode Kraken Spot stream decimal string: %w", err)
		}
		*decimal = SpotStreamDecimal(text)
		return nil
	}
	var number json.Number
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.UseNumber()
	if err := decoder.Decode(&number); err != nil {
		return fmt.Errorf("decode Kraken Spot stream decimal number: %w", err)
	}
	*decimal = SpotStreamDecimal(number.String())
	return nil
}

// SpotPublicChannel은 Spot WebSocket v2 public channel이다.
type SpotPublicChannel string

const (
	SpotChannelTicker     SpotPublicChannel = "ticker"
	SpotChannelBook       SpotPublicChannel = "book"
	SpotChannelTrade      SpotPublicChannel = "trade"
	SpotChannelOHLC       SpotPublicChannel = "ohlc"
	SpotChannelInstrument SpotPublicChannel = "instrument"
)

// SpotPrivateChannel은 Spot WebSocket v2 account channel이다.
type SpotPrivateChannel string

const (
	SpotChannelExecutions SpotPrivateChannel = "executions"
	SpotChannelBalances   SpotPrivateChannel = "balances"
)

// SpotTickerEventTrigger는 ticker update 발생 기준이다.
type SpotTickerEventTrigger string

const (
	SpotTickerOnTrade SpotTickerEventTrigger = "trades"
	SpotTickerOnBBO   SpotTickerEventTrigger = "bbo"
)

// SpotPublicSubscription은 한 public channel의 구독 파라미터다.
type SpotPublicSubscription struct {
	Channel      SpotPublicChannel
	Symbols      []string
	Depth        int
	Interval     int
	EventTrigger SpotTickerEventTrigger
	Snapshot     *bool
}

// SpotPublicStreamRequest는 한 public 연결의 초기 구독 목록이다.
type SpotPublicStreamRequest struct {
	Subscriptions []SpotPublicSubscription
}

// SpotPrivateSubscription은 한 private channel의 구독 파라미터다.
type SpotPrivateSubscription struct {
	Channel     SpotPrivateChannel
	Snapshot    *bool
	SnapOrders  *bool
	SnapTrades  *bool
	OrderStatus *bool
}

// SpotPrivateStreamRequest는 한 private 연결의 초기 구독 목록이다.
type SpotPrivateStreamRequest struct {
	Subscriptions []SpotPrivateSubscription
}

// SpotStreamMessage는 Spot WebSocket v2 제어 응답, heartbeat 또는 channel 데이터다.
type SpotStreamMessage struct {
	Channel   string
	Type      string
	Method    string
	RequestID int64
	Success   *bool
	Error     string
	TimeIn    string
	TimeOut   string
	Result    json.RawMessage
	Data      json.RawMessage
	Raw       json.RawMessage
}

// DecodeData는 channel data를 지정 타입으로 변환한다.
func (message SpotStreamMessage) DecodeData(target any) error {
	if target == nil {
		return fmt.Errorf("Kraken Spot stream decode target is nil")
	}
	if len(message.Data) == 0 {
		return fmt.Errorf("Kraken Spot stream message does not contain channel data")
	}
	if err := json.Unmarshal(message.Data, target); err != nil {
		return fmt.Errorf("decode Kraken Spot stream data: %w", err)
	}
	return nil
}

// SpotStreamTicker는 ticker snapshot 또는 update다.
type SpotStreamTicker struct {
	Symbol        string            `json:"symbol"`
	Ask           SpotStreamDecimal `json:"ask"`
	AskQuantity   SpotStreamDecimal `json:"ask_qty"`
	Bid           SpotStreamDecimal `json:"bid"`
	BidQuantity   SpotStreamDecimal `json:"bid_qty"`
	Change        SpotStreamDecimal `json:"change"`
	ChangePercent SpotStreamDecimal `json:"change_pct"`
	High          SpotStreamDecimal `json:"high"`
	Last          SpotStreamDecimal `json:"last"`
	Low           SpotStreamDecimal `json:"low"`
	Volume        SpotStreamDecimal `json:"volume"`
	VWAP          SpotStreamDecimal `json:"vwap"`
	Timestamp     string            `json:"timestamp"`
}

// SpotStreamBookLevel은 L2 호가 가격 레벨이다.
type SpotStreamBookLevel struct {
	Price    SpotStreamDecimal `json:"price"`
	Quantity SpotStreamDecimal `json:"qty"`
}

// SpotStreamBook은 L2 호가 snapshot 또는 순차 update다.
type SpotStreamBook struct {
	Symbol    string                `json:"symbol"`
	Bids      []SpotStreamBookLevel `json:"bids"`
	Asks      []SpotStreamBookLevel `json:"asks"`
	Checksum  uint32                `json:"checksum"`
	Timestamp string                `json:"timestamp"`
}

// SpotStreamTrade는 public 체결 한 건이다.
type SpotStreamTrade struct {
	Symbol    string            `json:"symbol"`
	Side      string            `json:"side"`
	Quantity  SpotStreamDecimal `json:"qty"`
	Price     SpotStreamDecimal `json:"price"`
	OrderType string            `json:"ord_type"`
	TradeID   int64             `json:"trade_id"`
	Timestamp string            `json:"timestamp"`
}

// SpotStreamOHLC는 진행 중인 캔들 snapshot 또는 update다.
type SpotStreamOHLC struct {
	Symbol        string            `json:"symbol"`
	Open          SpotStreamDecimal `json:"open"`
	High          SpotStreamDecimal `json:"high"`
	Low           SpotStreamDecimal `json:"low"`
	Close         SpotStreamDecimal `json:"close"`
	VWAP          SpotStreamDecimal `json:"vwap"`
	Trades        int64             `json:"trades"`
	Volume        SpotStreamDecimal `json:"volume"`
	IntervalBegin string            `json:"interval_begin"`
	Interval      int               `json:"interval"`
}

// SpotStreamAsset은 instrument channel의 자산 규칙이다.
type SpotStreamAsset struct {
	ID               string            `json:"id"`
	Class            string            `json:"class"`
	Status           string            `json:"status"`
	Precision        int               `json:"precision"`
	DisplayPrecision int               `json:"precision_display"`
	Borrowable       bool              `json:"borrowable"`
	CollateralValue  SpotStreamDecimal `json:"collateral_value"`
	MarginRate       SpotStreamDecimal `json:"margin_rate"`
	Multiplier       SpotStreamDecimal `json:"multiplier"`
}

// SpotStreamPair는 instrument channel의 Spot 상품 규칙이다.
type SpotStreamPair struct {
	Symbol            string            `json:"symbol"`
	Base              string            `json:"base"`
	Quote             string            `json:"quote"`
	Status            string            `json:"status"`
	CostMinimum       SpotStreamDecimal `json:"cost_min"`
	CostPrecision     int               `json:"cost_precision"`
	QuantityMinimum   SpotStreamDecimal `json:"qty_min"`
	QuantityPrecision int               `json:"qty_precision"`
	PriceIncrement    SpotStreamDecimal `json:"price_increment"`
	QuantityIncrement SpotStreamDecimal `json:"qty_increment"`
}

// SpotStreamInstrument는 자산과 상품 규칙 snapshot 또는 update다.
type SpotStreamInstrument struct {
	Assets []SpotStreamAsset `json:"assets"`
	Pairs  []SpotStreamPair  `json:"pairs"`
}

// SpotStreamFee는 private execution 체결 수수료다.
type SpotStreamFee struct {
	Asset    string            `json:"asset"`
	Quantity SpotStreamDecimal `json:"qty"`
}

// SpotStreamExecution은 주문 상태와 체결을 결합한 private event다.
type SpotStreamExecution struct {
	ExecutionType string            `json:"exec_type"`
	OrderID       string            `json:"order_id"`
	ClientOrderID string            `json:"cl_ord_id"`
	TradeID       int64             `json:"trade_id"`
	Symbol        string            `json:"symbol"`
	Side          string            `json:"side"`
	OrderType     string            `json:"order_type"`
	OrderStatus   string            `json:"order_status"`
	OrderQuantity SpotStreamDecimal `json:"order_qty"`
	CumulativeQty SpotStreamDecimal `json:"cum_qty"`
	LastQuantity  SpotStreamDecimal `json:"last_qty"`
	LimitPrice    SpotStreamDecimal `json:"limit_price"`
	AveragePrice  SpotStreamDecimal `json:"avg_price"`
	LastPrice     SpotStreamDecimal `json:"last_price"`
	Cost          SpotStreamDecimal `json:"cost"`
	Fees          []SpotStreamFee   `json:"fees"`
	Liquidity     string            `json:"liquidity_ind"`
	Timestamp     string            `json:"timestamp"`
	Reason        string            `json:"reason"`
	Margin        bool              `json:"margin"`
	ReduceOnly    bool              `json:"reduce_only"`
	Sequence      int64             `json:"sequence"`
}

// SpotStreamWallet은 자산별 wallet 잔고다.
type SpotStreamWallet struct {
	Balance SpotStreamDecimal `json:"balance"`
	Type    string            `json:"type"`
	ID      string            `json:"id"`
}

// SpotStreamBalance는 balances snapshot 자산 또는 update ledger event다.
type SpotStreamBalance struct {
	Asset       string             `json:"asset"`
	AssetClass  string             `json:"asset_class"`
	Amount      SpotStreamDecimal  `json:"amount"`
	Balance     SpotStreamDecimal  `json:"balance"`
	Fee         SpotStreamDecimal  `json:"fee"`
	Wallets     []SpotStreamWallet `json:"wallets"`
	LedgerID    string             `json:"ledger_id"`
	ReferenceID string             `json:"ref_id"`
	Timestamp   string             `json:"timestamp"`
	Type        string             `json:"type"`
	Subtype     string             `json:"subtype"`
	Category    string             `json:"category"`
	Sequence    int64              `json:"sequence"`
}

// SpotStreamRequestError는 WebSocket 명령이 명시적으로 거절된 오류다.
type SpotStreamRequestError struct {
	Method    string
	RequestID int64
	Message   string
}

// Error는 WebSocket 명령 거절 내용을 반환한다.
func (streamError *SpotStreamRequestError) Error() string {
	return fmt.Sprintf(
		"Kraken Spot stream %s request %d failed: %s",
		streamError.Method, streamError.RequestID, streamError.Message,
	)
}
