package bybit

import (
	"encoding/json"
	"fmt"
)

// PublicStreamRequest는 한 public 연결의 상품 분류와 구독 topic 목록이다.
type PublicStreamRequest struct {
	Category Category
	Topics   []string
}

// PrivateStreamRequest는 한 private 연결의 구독 topic 목록이다.
type PrivateStreamRequest struct {
	Topics []string
}

// StreamMessage는 Bybit 제어 응답, heartbeat 또는 topic 데이터 한 건이다.
type StreamMessage struct {
	Topic         string
	Type          string
	Timestamp     int64
	CreationTime  int64
	Operation     string
	RequestID     string
	ConnectionID  string
	Success       *bool
	ReturnMessage string
	Data          json.RawMessage
	Pong          bool
	Raw           json.RawMessage
}

// DecodeData는 topic data를 지정 타입으로 변환한다.
func (message StreamMessage) DecodeData(target any) error {
	if target == nil {
		return fmt.Errorf("Bybit stream decode target is nil")
	}
	if message.Pong || len(message.Data) == 0 {
		return fmt.Errorf("Bybit stream message does not contain topic data")
	}
	if err := json.Unmarshal(message.Data, target); err != nil {
		return fmt.Errorf("decode Bybit stream data: %w", err)
	}
	return nil
}

// StreamTicker는 Spot 또는 Linear ticker topic 데이터다.
type StreamTicker struct {
	Symbol             string `json:"symbol"`
	TickDirection      string `json:"tickDirection"`
	LastPrice          string `json:"lastPrice"`
	PreviousPrice24h   string `json:"prevPrice24h"`
	PriceChange24hRate string `json:"price24hPcnt"`
	HighPrice24h       string `json:"highPrice24h"`
	LowPrice24h        string `json:"lowPrice24h"`
	PreviousPrice1h    string `json:"prevPrice1h"`
	MarkPrice          string `json:"markPrice"`
	IndexPrice         string `json:"indexPrice"`
	OpenInterest       string `json:"openInterest"`
	OpenInterestValue  string `json:"openInterestValue"`
	Volume24h          string `json:"volume24h"`
	Turnover24h        string `json:"turnover24h"`
	NextFundingTime    string `json:"nextFundingTime"`
	FundingRate        string `json:"fundingRate"`
	BidPrice           string `json:"bid1Price"`
	BidQuantity        string `json:"bid1Size"`
	AskPrice           string `json:"ask1Price"`
	AskQuantity        string `json:"ask1Size"`
}

// StreamOrderBook은 orderbook snapshot 또는 delta 데이터다.
type StreamOrderBook struct {
	Symbol       string     `json:"s"`
	Bids         [][]string `json:"b"`
	Asks         [][]string `json:"a"`
	UpdateID     int64      `json:"u"`
	Sequence     int64      `json:"seq"`
	MatchingTime int64      `json:"cts"`
}

// StreamPublicTrade는 publicTrade topic 체결 한 건이다.
type StreamPublicTrade struct {
	Timestamp        int64  `json:"T"`
	Symbol           string `json:"s"`
	Side             Side   `json:"S"`
	Quantity         string `json:"v"`
	Price            string `json:"p"`
	TickDirection    string `json:"L"`
	ExecutionID      string `json:"i"`
	BlockTrade       bool   `json:"BT"`
	RetailPriceTrade bool   `json:"RPI"`
}

// StreamKline은 kline topic의 진행 중이거나 확정된 캔들이다.
type StreamKline struct {
	StartTime int64  `json:"start"`
	EndTime   int64  `json:"end"`
	Interval  string `json:"interval"`
	Open      string `json:"open"`
	Close     string `json:"close"`
	High      string `json:"high"`
	Low       string `json:"low"`
	Volume    string `json:"volume"`
	Turnover  string `json:"turnover"`
	Confirmed bool   `json:"confirm"`
	Timestamp int64  `json:"timestamp"`
}

// StreamOrder는 private order topic의 주문 변경 데이터다.
type StreamOrder struct {
	Category                   Category    `json:"category"`
	OrderID                    string      `json:"orderId"`
	OrderLinkID                string      `json:"orderLinkId"`
	Symbol                     string      `json:"symbol"`
	Side                       Side        `json:"side"`
	OrderType                  OrderType   `json:"orderType"`
	Price                      string      `json:"price"`
	Quantity                   string      `json:"qty"`
	TimeInForce                TimeInForce `json:"timeInForce"`
	OrderStatus                string      `json:"orderStatus"`
	AveragePrice               string      `json:"avgPrice"`
	CumulativeExecutedQuantity string      `json:"cumExecQty"`
	CumulativeExecutedValue    string      `json:"cumExecValue"`
	LeavesQuantity             string      `json:"leavesQty"`
	LeavesValue                string      `json:"leavesValue"`
	ReduceOnly                 bool        `json:"reduceOnly"`
	PositionIndex              int         `json:"positionIdx"`
	CancelType                 string      `json:"cancelType"`
	RejectReason               string      `json:"rejectReason"`
	CreatedTime                string      `json:"createdTime"`
	UpdatedTime                string      `json:"updatedTime"`
}

// StreamExecution은 private execution topic의 체결 데이터다.
type StreamExecution struct {
	Category       Category  `json:"category"`
	Symbol         string    `json:"symbol"`
	OrderID        string    `json:"orderId"`
	OrderLinkID    string    `json:"orderLinkId"`
	ExecutionID    string    `json:"execId"`
	ExecutionType  string    `json:"execType"`
	ExecutionPrice string    `json:"execPrice"`
	ExecutionQty   string    `json:"execQty"`
	ExecutionValue string    `json:"execValue"`
	ExecutionFee   string    `json:"execFee"`
	ExecutionTime  string    `json:"execTime"`
	Side           Side      `json:"side"`
	OrderType      OrderType `json:"orderType"`
	OrderPrice     string    `json:"orderPrice"`
	OrderQuantity  string    `json:"orderQty"`
	LeavesQuantity string    `json:"leavesQty"`
	Maker          bool      `json:"isMaker"`
	FeeRate        string    `json:"feeRate"`
	ClosedSize     string    `json:"closedSize"`
	Sequence       int64     `json:"seq"`
}

// StreamPosition은 private position topic의 Linear 포지션 변경 데이터다.
type StreamPosition struct {
	Category              Category `json:"category"`
	Symbol                string   `json:"symbol"`
	Side                  string   `json:"side"`
	Size                  string   `json:"size"`
	AveragePrice          string   `json:"entryPrice"`
	PositionValue         string   `json:"positionValue"`
	Leverage              string   `json:"leverage"`
	MarkPrice             string   `json:"markPrice"`
	LiquidationPrice      string   `json:"liqPrice"`
	UnrealisedPnL         string   `json:"unrealisedPnl"`
	CumulativeRealisedPnL string   `json:"cumRealisedPnl"`
	PositionIndex         int      `json:"positionIdx"`
	PositionStatus        string   `json:"positionStatus"`
	CreatedTime           string   `json:"createdTime"`
	UpdatedTime           string   `json:"updatedTime"`
	Sequence              int64    `json:"seq"`
}

// StreamWalletCoin은 private wallet topic의 코인별 자산 데이터다.
type StreamWalletCoin struct {
	Coin                  string `json:"coin"`
	Equity                string `json:"equity"`
	USDValue              string `json:"usdValue"`
	WalletBalance         string `json:"walletBalance"`
	AvailableToWithdraw   string `json:"availableToWithdraw"`
	BorrowAmount          string `json:"borrowAmount"`
	Locked                string `json:"locked"`
	UnrealisedPnL         string `json:"unrealisedPnl"`
	CumulativeRealisedPnL string `json:"cumRealisedPnl"`
}

// StreamWallet은 private wallet topic의 통합 계정 자산 데이터다.
type StreamWallet struct {
	AccountType            string             `json:"accountType"`
	TotalEquity            string             `json:"totalEquity"`
	TotalWalletBalance     string             `json:"totalWalletBalance"`
	TotalAvailableBalance  string             `json:"totalAvailableBalance"`
	TotalPerpetualUPL      string             `json:"totalPerpUPL"`
	TotalInitialMargin     string             `json:"totalInitialMargin"`
	TotalMaintenanceMargin string             `json:"totalMaintenanceMargin"`
	Coins                  []StreamWalletCoin `json:"coin"`
}

// StreamAuthError는 private WebSocket 인증이 명시적으로 거절된 오류다.
type StreamAuthError struct {
	Message string
}

// Error는 인증 거절 메시지를 반환한다.
func (streamError *StreamAuthError) Error() string {
	return fmt.Sprintf("Bybit stream authentication failed: %s", streamError.Message)
}
