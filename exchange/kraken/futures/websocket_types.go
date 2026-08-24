package futures

import (
	"encoding/json"
	"fmt"
)

// PublicStreamFeed는 Futures public WebSocket feed다.
type PublicStreamFeed string

const (
	PublicFeedTicker     PublicStreamFeed = "ticker"
	PublicFeedTickerLite PublicStreamFeed = "ticker_lite"
	PublicFeedTrade      PublicStreamFeed = "trade"
	PublicFeedBook       PublicStreamFeed = "book"
	PublicFeedHeartbeat  PublicStreamFeed = "heartbeat"
)

// PrivateStreamFeed는 Futures private WebSocket feed다.
type PrivateStreamFeed string

const (
	PrivateFeedBalances          PrivateStreamFeed = "balances"
	PrivateFeedFills             PrivateStreamFeed = "fills"
	PrivateFeedOpenOrders        PrivateStreamFeed = "open_orders"
	PrivateFeedOpenOrdersVerbose PrivateStreamFeed = "open_orders_verbose"
	PrivateFeedOpenPositions     PrivateStreamFeed = "open_positions"
	PrivateFeedAccountLog        PrivateStreamFeed = "account_log"
	PrivateFeedNotifications     PrivateStreamFeed = "notifications_auth"
)

// PublicStreamSubscription은 Futures public feed와 상품 목록이다.
type PublicStreamSubscription struct {
	Feed       PublicStreamFeed
	ProductIDs []string
}

// PublicStreamRequest는 한 public 연결의 초기 구독 목록이다.
type PublicStreamRequest struct {
	Subscriptions []PublicStreamSubscription
}

// PrivateStreamSubscription은 Futures private feed와 선택 상품 목록이다.
type PrivateStreamSubscription struct {
	Feed       PrivateStreamFeed
	ProductIDs []string
}

// PrivateStreamRequest는 한 private 연결의 초기 구독 목록이다.
type PrivateStreamRequest struct {
	Subscriptions []PrivateStreamSubscription
}

// StreamMessage는 Futures WebSocket v1 제어 응답과 feed 데이터를 보존한다.
type StreamMessage struct {
	Event      string
	Feed       string
	Message    string
	ProductID  string
	ProductIDs []string
	Account    string
	Raw        json.RawMessage
}

// Decode는 frame 전체를 지정한 typed feed 구조로 변환한다.
func (message StreamMessage) Decode(target any) error {
	if target == nil {
		return fmt.Errorf("Kraken Futures stream decode target is nil")
	}
	if len(message.Raw) == 0 {
		return fmt.Errorf("Kraken Futures stream message is empty")
	}
	if err := json.Unmarshal(message.Raw, target); err != nil {
		return fmt.Errorf("decode Kraken Futures stream data: %w", err)
	}
	return nil
}

// StreamGreeks는 option ticker와 포지션의 Greeks 값이다.
type StreamGreeks struct {
	ImpliedVolatility Decimal `json:"iv"`
	Delta             Decimal `json:"delta"`
	Theta             Decimal `json:"theta"`
	Gamma             Decimal `json:"gamma"`
	Vega              Decimal `json:"vega"`
	Rho               Decimal `json:"rho"`
}

// StreamTicker는 ticker 또는 ticker_lite snapshot과 update다.
type StreamTicker struct {
	Feed                          string       `json:"feed"`
	Time                          int64        `json:"time"`
	ProductID                     string       `json:"product_id"`
	FundingRate                   Decimal      `json:"funding_rate"`
	FundingRatePrediction         Decimal      `json:"funding_rate_prediction"`
	RelativeFundingRate           Decimal      `json:"relative_funding_rate"`
	RelativeFundingRatePrediction Decimal      `json:"relative_funding_rate_prediction"`
	NextFundingRateTime           int64        `json:"next_funding_rate_time"`
	Bid                           Decimal      `json:"bid"`
	Ask                           Decimal      `json:"ask"`
	BidSize                       Decimal      `json:"bid_size"`
	AskSize                       Decimal      `json:"ask_size"`
	Volume                        Decimal      `json:"volume"`
	DaysToMaturity                int64        `json:"dtm"`
	Leverage                      string       `json:"leverage"`
	Index                         Decimal      `json:"index"`
	Last                          Decimal      `json:"last"`
	Change                        Decimal      `json:"change"`
	Suspended                     bool         `json:"suspended"`
	Tag                           string       `json:"tag"`
	Pair                          string       `json:"pair"`
	OpenInterest                  Decimal      `json:"openInterest"`
	MarkPrice                     Decimal      `json:"markPrice"`
	MaturityTime                  int64        `json:"maturityTime"`
	PostOnly                      bool         `json:"post_only"`
	QuoteVolume                   Decimal      `json:"volumeQuote"`
	Open                          Decimal      `json:"open"`
	High                          Decimal      `json:"high"`
	Low                           Decimal      `json:"low"`
	Premium                       Decimal      `json:"premium"`
	Greeks                        StreamGreeks `json:"greeks"`
}

// StreamBookLevel은 Futures WebSocket L2 호가 가격 레벨이다.
type StreamBookLevel struct {
	Price    Decimal `json:"price"`
	Quantity Decimal `json:"qty"`
}

// StreamBookSnapshot은 전체 L2 호가와 시작 sequence다.
type StreamBookSnapshot struct {
	Feed      string            `json:"feed"`
	ProductID string            `json:"product_id"`
	Sequence  uint64            `json:"seq"`
	Timestamp int64             `json:"timestamp"`
	TickSize  Decimal           `json:"tickSize"`
	Bids      []StreamBookLevel `json:"bids"`
	Asks      []StreamBookLevel `json:"asks"`
}

// StreamBookUpdate는 단일 L2 가격 레벨 변경이다.
type StreamBookUpdate struct {
	Feed      string  `json:"feed"`
	ProductID string  `json:"product_id"`
	Sequence  uint64  `json:"seq"`
	Timestamp int64   `json:"timestamp"`
	Side      string  `json:"side"`
	Price     Decimal `json:"price"`
	Quantity  Decimal `json:"qty"`
}

// StreamTrade는 Futures public 체결 한 건이다.
type StreamTrade struct {
	Feed      string  `json:"feed"`
	ProductID string  `json:"product_id"`
	TradeID   string  `json:"uid"`
	Side      Side    `json:"side"`
	Type      string  `json:"type"`
	Sequence  uint64  `json:"seq"`
	Time      int64   `json:"time"`
	Quantity  Decimal `json:"qty"`
	Price     Decimal `json:"price"`
}

// StreamTradeSnapshot은 최근 public 체결 snapshot이다.
type StreamTradeSnapshot struct {
	Feed      string        `json:"feed"`
	ProductID string        `json:"product_id"`
	Trades    []StreamTrade `json:"trades"`
}

// StreamSingleCollateralWallet은 단일 collateral Futures wallet 상태다.
type StreamSingleCollateralWallet struct {
	Name                string  `json:"name"`
	Pair                string  `json:"pair"`
	Unit                string  `json:"unit"`
	PortfolioValue      Decimal `json:"portfolio_value"`
	Balance             Decimal `json:"balance"`
	MaintenanceMargin   Decimal `json:"maintenance_margin"`
	InitialMargin       Decimal `json:"initial_margin"`
	Available           Decimal `json:"available"`
	UnrealizedFunding   Decimal `json:"unrealized_funding"`
	PNL                 Decimal `json:"pnl"`
	CashValue           Decimal `json:"cash_value"`
	InitialMarginOrders Decimal `json:"initial_margin_with_orders"`
}

// StreamMarginSummary는 multi-collateral isolated 또는 cross margin 요약이다.
type StreamMarginSummary struct {
	InitialMargin              Decimal `json:"initial_margin"`
	InitialMarginWithoutOrders Decimal `json:"initial_margin_without_orders"`
	MaintenanceMargin          Decimal `json:"maintenance_margin"`
	PNL                        Decimal `json:"pnl"`
	UnrealizedFunding          Decimal `json:"unrealized_funding"`
	TotalUnrealized            Decimal `json:"total_unrealized"`
	TotalUnrealizedAsMargin    Decimal `json:"total_unrealized_as_margin"`
	BalanceValue               Decimal `json:"balance_value"`
	CollateralValue            Decimal `json:"collateral_value"`
	PortfolioValue             Decimal `json:"portfolio_value"`
	MarginEquity               Decimal `json:"margin_equity"`
	AvailableMargin            Decimal `json:"available_margin"`
}

// StreamCollateralCurrency는 multi-collateral wallet의 통화별 상태다.
type StreamCollateralCurrency struct {
	Quantity         Decimal `json:"quantity"`
	Value            Decimal `json:"value"`
	CollateralValue  Decimal `json:"collateral_value"`
	Available        Decimal `json:"available"`
	Haircut          Decimal `json:"haircut"`
	ConversionSpread Decimal `json:"conversion_spread"`
}

// StreamMultiCollateralWallet은 multi-collateral wallet 전체 상태다.
type StreamMultiCollateralWallet struct {
	StreamMarginSummary
	Currencies map[string]StreamCollateralCurrency `json:"currencies"`
	Isolated   map[string]StreamMarginSummary      `json:"isolated"`
	Cross      map[string]StreamMarginSummary      `json:"cross"`
}

// StreamBalances는 balances snapshot 또는 delta다.
type StreamBalances struct {
	Feed        string                                  `json:"feed"`
	Account     string                                  `json:"account"`
	Timestamp   int64                                   `json:"timestamp"`
	Sequence    uint64                                  `json:"seq"`
	Holding     map[string]Decimal                      `json:"holding"`
	Futures     map[string]StreamSingleCollateralWallet `json:"futures"`
	FlexFutures StreamMultiCollateralWallet             `json:"flex_futures"`
}

// StreamFill은 private Futures 체결 한 건이다.
type StreamFill struct {
	Instrument             string  `json:"instrument"`
	Time                   int64   `json:"time"`
	Price                  Decimal `json:"price"`
	Sequence               uint64  `json:"seq"`
	Buy                    bool    `json:"buy"`
	Quantity               Decimal `json:"qty"`
	RemainingOrderQuantity Decimal `json:"remaining_order_qty"`
	OrderID                string  `json:"order_id"`
	ClientOrderID          string  `json:"cli_ord_id"`
	FillID                 string  `json:"fill_id"`
	FillType               string  `json:"fill_type"`
	FeePaid                Decimal `json:"fee_paid"`
	FeeCurrency            string  `json:"fee_currency"`
	TakerOrderType         string  `json:"taker_order_type"`
	OrderType              string  `json:"order_type"`
}

// StreamFills는 fills snapshot 또는 delta다.
type StreamFills struct {
	Feed    string       `json:"feed"`
	Account string       `json:"account"`
	Fills   []StreamFill `json:"fills"`
}

// StreamTrailingStopOptions는 trailing stop 주문의 거리 설정이다.
type StreamTrailingStopOptions struct {
	MaximumDeviation Decimal `json:"max_deviation"`
	Unit             string  `json:"unit"`
}

// StreamOrder는 open_orders feed의 주문 상태다.
type StreamOrder struct {
	Instrument          string                    `json:"instrument"`
	Time                int64                     `json:"time"`
	LastUpdateTime      int64                     `json:"last_update_time"`
	Quantity            Decimal                   `json:"qty"`
	Filled              Decimal                   `json:"filled"`
	LimitPrice          Decimal                   `json:"limit_price"`
	StopPrice           Decimal                   `json:"stop_price"`
	Type                string                    `json:"type"`
	OrderID             string                    `json:"order_id"`
	ClientOrderID       string                    `json:"cli_ord_id"`
	Direction           int                       `json:"direction"`
	ReduceOnly          bool                      `json:"reduce_only"`
	TriggerSignal       string                    `json:"triggerSignal"`
	TrailingStopOptions StreamTrailingStopOptions `json:"trailing_stop_options"`
}

// StreamOpenOrders는 open_orders snapshot 또는 delta다.
type StreamOpenOrders struct {
	Feed     string        `json:"feed"`
	Account  string        `json:"account"`
	Orders   []StreamOrder `json:"orders"`
	Order    *StreamOrder  `json:"order"`
	OrderID  string        `json:"order_id"`
	Canceled bool          `json:"is_cancel"`
	Reason   string        `json:"reason"`
}

// StreamPosition은 private Futures 열린 포지션 상태다.
type StreamPosition struct {
	Instrument              string  `json:"instrument"`
	Balance                 Decimal `json:"balance"`
	EntryPrice              Decimal `json:"entry_price"`
	MarkPrice               Decimal `json:"mark_price"`
	IndexPrice              Decimal `json:"index_price"`
	PNL                     Decimal `json:"pnl"`
	LiquidationThreshold    Decimal `json:"liquidation_threshold"`
	ReturnOnEquity          Decimal `json:"return_on_equity"`
	UnrealizedFunding       Decimal `json:"unrealized_funding"`
	EffectiveLeverage       Decimal `json:"effective_leverage"`
	InitialMargin           Decimal `json:"initial_margin"`
	InitialMarginWithOrders Decimal `json:"initial_margin_with_orders"`
	MaintenanceMargin       Decimal `json:"maintenance_margin"`
	PNLCurrency             string  `json:"pnl_currency"`
	MaximumFixedLeverage    Decimal `json:"max_fixed_leverage"`
	FillTime                string  `json:"fill_time"`
	ImpliedVolatility       Decimal `json:"iv"`
	Delta                   Decimal `json:"delta"`
	Theta                   Decimal `json:"theta"`
	Gamma                   Decimal `json:"gamma"`
	Vega                    Decimal `json:"vega"`
	Rho                     Decimal `json:"rho"`
}

// StreamOpenPositions는 열린 포지션 snapshot이다.
type StreamOpenPositions struct {
	Feed      string           `json:"feed"`
	Account   string           `json:"account"`
	Sequence  uint64           `json:"seq"`
	Timestamp int64            `json:"timestamp"`
	Positions []StreamPosition `json:"positions"`
}

// StreamAccountLogEntry는 wallet과 포지션의 원장 변경 한 건이다.
type StreamAccountLogEntry struct {
	ID                         int64   `json:"id"`
	Date                       string  `json:"date"`
	Asset                      string  `json:"asset"`
	Contract                   string  `json:"contract"`
	Info                       string  `json:"info"`
	BookingID                  string  `json:"booking_uid"`
	MarginAccount              string  `json:"margin_account"`
	OldBalance                 Decimal `json:"old_balance"`
	NewBalance                 Decimal `json:"new_balance"`
	OldAverageEntryPrice       Decimal `json:"old_average_entry_price"`
	NewAverageEntryPrice       Decimal `json:"new_average_entry_price"`
	TradePrice                 Decimal `json:"trade_price"`
	MarkPrice                  Decimal `json:"mark_price"`
	RealizedPNL                Decimal `json:"realized_pnl"`
	Fee                        Decimal `json:"fee"`
	Execution                  string  `json:"execution"`
	Collateral                 string  `json:"collateral"`
	FundingRate                Decimal `json:"funding_rate"`
	RealizedFunding            Decimal `json:"realized_funding"`
	ConversionSpreadPercentage Decimal `json:"conversion_spread_percentage"`
	LiquidationFee             Decimal `json:"liquidation_fee"`
}

// StreamAccountLog는 account_log snapshot 또는 delta다.
type StreamAccountLog struct {
	Feed string                  `json:"feed"`
	Logs []StreamAccountLogEntry `json:"logs"`
}

// StreamNotification은 Futures 운영 알림 한 건이다.
type StreamNotification struct {
	ID                      int64  `json:"id"`
	Type                    string `json:"type"`
	Priority                string `json:"priority"`
	Note                    string `json:"note"`
	EffectiveTime           int64  `json:"effective_time"`
	ExpectedDowntimeMinutes int64  `json:"expected_downtime_minutes"`
}

// StreamNotifications는 notifications_auth snapshot이다.
type StreamNotifications struct {
	Feed          string               `json:"feed"`
	Notifications []StreamNotification `json:"notifications"`
}

// StreamRequestError는 WebSocket 명령이나 인증이 명시적으로 거절된 오류다.
type StreamRequestError struct {
	Event   string
	Feed    string
	Message string
}

// Error는 WebSocket 명령 거절 내용을 반환한다.
func (streamError *StreamRequestError) Error() string {
	return fmt.Sprintf(
		"Kraken Futures stream %s for %s failed: %s",
		streamError.Event, streamError.Feed, streamError.Message,
	)
}
