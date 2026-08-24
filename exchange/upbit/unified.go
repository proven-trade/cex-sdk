package upbit

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	trade "github.com/proven-trade/proven-trade-sdk"
	"github.com/proven-trade/proven-trade-sdk/model"
	"github.com/proven-trade/proven-trade-sdk/unified"
)

// UnifiedSpot은 Upbit native 클라이언트를 공통 Spot 계약으로 변환한다.
type UnifiedSpot struct {
	client *Client
}

var _ unified.SpotClient = (*UnifiedSpot)(nil)

// NewUnifiedSpot은 Upbit Spot 공통 어댑터를 생성한다.
func NewUnifiedSpot(client *Client) (*UnifiedSpot, error) {
	if client == nil {
		return nil, fmt.Errorf("Upbit client is required")
	}
	return &UnifiedSpot{client: client}, nil
}

// Exchange는 Upbit 거래소 식별자를 반환한다.
func (adapter *UnifiedSpot) Exchange() model.ExchangeID {
	return model.ExchangeUpbit
}

// Markets는 Upbit Spot 거래 마켓과 상태를 공통 형식으로 조회한다.
func (adapter *UnifiedSpot) Markets(
	ctx context.Context,
	options ...trade.RequestOption,
) ([]unified.MarketInfo, error) {
	native, err := adapter.client.Markets(ctx, MarketsRequest{IncludeDetails: true}, options...)
	if err != nil {
		return nil, err
	}
	markets := make([]unified.MarketInfo, len(native))
	for index, item := range native {
		market, parseErr := fromUpbitMarket(item.Market)
		if parseErr != nil {
			return nil, parseErr
		}
		markets[index] = unified.MarketInfo{
			Exchange: model.ExchangeUpbit, Market: market,
			NativeMarket: item.Market, Status: item.MarketWarning, Raw: item.Raw,
		}
	}
	return markets, nil
}

// Ticker는 공통 마켓의 최신 가격을 조회한다.
func (adapter *UnifiedSpot) Ticker(
	ctx context.Context,
	request unified.TickerRequest,
	options ...trade.RequestOption,
) (unified.Ticker, error) {
	if err := request.Validate(); err != nil {
		return unified.Ticker{}, err
	}
	tickers, err := adapter.client.Tickers(ctx, TickersRequest{
		Markets: []string{upbitMarket(request.Market)},
	}, options...)
	if err != nil {
		return unified.Ticker{}, err
	}
	if len(tickers) != 1 {
		return unified.Ticker{}, fmt.Errorf("Upbit ticker response has %d items, want 1", len(tickers))
	}
	return unified.Ticker{
		Exchange: model.ExchangeUpbit, Market: request.Market,
		NativeMarket: tickers[0].Market, Price: string(tickers[0].TradePrice), Raw: tickers[0].Raw,
	}, nil
}

// OrderBook은 Upbit 호가를 공통 형식으로 조회한다.
func (adapter *UnifiedSpot) OrderBook(
	ctx context.Context,
	request unified.OrderBookRequest,
	options ...trade.RequestOption,
) (unified.OrderBook, error) {
	if err := request.Validate(); err != nil {
		return unified.OrderBook{}, err
	}
	native, err := adapter.client.OrderBooks(ctx, OrderBooksRequest{
		Markets: []string{upbitMarket(request.Market)}, Count: request.Limit,
	}, options...)
	if err != nil {
		return unified.OrderBook{}, err
	}
	if len(native) != 1 {
		return unified.OrderBook{}, fmt.Errorf("Upbit order book response has %d items, want 1", len(native))
	}
	bids := make([]unified.BookLevel, len(native[0].OrderBook))
	asks := make([]unified.BookLevel, len(native[0].OrderBook))
	for index, level := range native[0].OrderBook {
		bids[index] = unified.BookLevel{Price: string(level.BidPrice), Quantity: string(level.BidSize)}
		asks[index] = unified.BookLevel{Price: string(level.AskPrice), Quantity: string(level.AskSize)}
	}
	return unified.OrderBook{
		Exchange: model.ExchangeUpbit, Market: request.Market,
		NativeMarket: native[0].Market, Bids: bids, Asks: asks,
		Timestamp: native[0].Timestamp, Raw: native[0].Raw,
	}, nil
}

// RecentTrades는 Upbit 공개 최근 체결을 공통 형식으로 조회한다.
func (adapter *UnifiedSpot) RecentTrades(
	ctx context.Context,
	request unified.RecentTradesRequest,
	options ...trade.RequestOption,
) ([]unified.PublicTrade, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	native, err := adapter.client.RecentTrades(ctx, RecentTradesRequest{
		Market: upbitMarket(request.Market), Count: request.Limit,
	}, options...)
	if err != nil {
		return nil, err
	}
	trades := make([]unified.PublicTrade, len(native))
	for index, item := range native {
		side := unified.SideSell
		if item.AskBid == "BID" {
			side = unified.SideBuy
		}
		trades[index] = unified.PublicTrade{
			ID: strconv.FormatInt(item.SequentialID, 10), Price: string(item.TradePrice),
			Quantity: string(item.TradeVolume), Side: side, Timestamp: item.Timestamp,
		}
	}
	return trades, nil
}

// Candles는 Upbit Spot 분봉을 공통 형식으로 조회한다.
func (adapter *UnifiedSpot) Candles(
	ctx context.Context,
	request unified.CandlesRequest,
	options ...trade.RequestOption,
) ([]unified.Candle, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	native, err := adapter.client.MinuteCandles(ctx, MinuteCandlesRequest{
		Market: upbitMarket(request.Market), Unit: toUpbitMinuteUnit(request.Interval), Count: request.Limit,
	}, options...)
	if err != nil {
		return nil, err
	}
	candles := make([]unified.Candle, len(native))
	for index, item := range native {
		startTime := item.Timestamp
		if item.CandleDateTimeUTC != "" {
			parsed, parseErr := time.Parse("2006-01-02T15:04:05", item.CandleDateTimeUTC)
			if parseErr != nil {
				return nil, fmt.Errorf("decode Upbit candle start time: %w", parseErr)
			}
			startTime = parsed.UnixMilli()
		}
		candles[index] = unified.Candle{
			StartTime: startTime, Open: string(item.OpeningPrice), High: string(item.HighPrice),
			Low: string(item.LowPrice), Close: string(item.TradePrice), Volume: string(item.AccumulatedTradeVolume),
		}
	}
	return candles, nil
}

// Balances는 Upbit 계정 잔고를 공통 형식으로 조회한다.
func (adapter *UnifiedSpot) Balances(
	ctx context.Context,
	options ...trade.RequestOption,
) ([]unified.Balance, error) {
	native, err := adapter.client.Accounts(ctx, options...)
	if err != nil {
		return nil, err
	}
	balances := make([]unified.Balance, len(native))
	for index, balance := range native {
		balances[index] = unified.Balance{
			Asset: balance.Currency, Available: balance.Balance, Locked: balance.Locked,
		}
	}
	return balances, nil
}

// PlaceOrder는 공통 Spot 주문을 Upbit 주문으로 변환해 생성한다.
func (adapter *UnifiedSpot) PlaceOrder(
	ctx context.Context,
	request unified.PlaceOrderRequest,
	options ...trade.RequestOption,
) (unified.Order, error) {
	if err := request.Validate(); err != nil {
		return unified.Order{}, err
	}
	nativeRequest := PlaceOrderRequest{
		Market: upbitMarket(request.Market), Side: toUpbitSide(request.Side),
		Volume: request.Quantity, Price: request.Price, Identifier: request.ClientOrderID,
	}
	if request.Type == unified.OrderTypeLimit {
		nativeRequest.OrderType = OrderTypeLimit
		nativeRequest.TimeInForce = toUpbitTimeInForce(request.TimeInForce)
	} else if request.Side == unified.SideBuy {
		nativeRequest.OrderType = OrderTypePrice
		nativeRequest.Price = request.QuoteAmount
		nativeRequest.Volume = ""
	} else {
		nativeRequest.OrderType = OrderTypeMarket
		nativeRequest.Price = ""
	}
	native, err := adapter.client.PlaceOrder(ctx, nativeRequest, options...)
	if err != nil {
		return unified.Order{}, err
	}
	return fromUpbitOrder(native, request.Market), nil
}

// Order는 Upbit 주문을 단건 조회한다.
func (adapter *UnifiedSpot) Order(
	ctx context.Context,
	request unified.OrderRequest,
	options ...trade.RequestOption,
) (unified.Order, error) {
	if err := request.Validate(); err != nil {
		return unified.Order{}, err
	}
	native, err := adapter.client.OrderInfo(ctx, OrderInfoRequest{
		UUID: request.OrderID, Identifier: request.ClientOrderID,
	}, options...)
	if err != nil {
		return unified.Order{}, err
	}
	return fromUpbitOrder(native, request.Market), nil
}

// CancelOrder는 Upbit 주문 취소를 요청한다.
func (adapter *UnifiedSpot) CancelOrder(
	ctx context.Context,
	request unified.OrderRequest,
	options ...trade.RequestOption,
) (unified.Order, error) {
	if err := request.Validate(); err != nil {
		return unified.Order{}, err
	}
	native, err := adapter.client.CancelOrder(ctx, CancelOrderRequest{
		UUID: request.OrderID, Identifier: request.ClientOrderID,
	}, options...)
	if err != nil {
		return unified.Order{}, err
	}
	return fromUpbitOrder(native, request.Market), nil
}

// OpenOrders는 Upbit 미체결 주문을 단일 또는 전체 마켓에서 조회한다.
func (adapter *UnifiedSpot) OpenOrders(
	ctx context.Context,
	request unified.OpenOrdersRequest,
	options ...trade.RequestOption,
) ([]unified.Order, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	nativeRequest := OpenOrdersRequest{AllMarkets: request.AllMarkets}
	if request.Market != nil {
		nativeRequest.Market = upbitMarket(*request.Market)
	}
	nativeOrders, err := adapter.client.OpenOrders(ctx, nativeRequest, options...)
	if err != nil {
		return nil, err
	}
	orders := make([]unified.Order, len(nativeOrders))
	for index, native := range nativeOrders {
		market := unified.Market{}
		if request.Market != nil {
			market = *request.Market
		} else if parsed, parseErr := fromUpbitMarket(native.Market); parseErr == nil {
			market = parsed
		}
		orders[index] = fromUpbitOrder(native, market)
	}
	return orders, nil
}

func upbitMarket(market unified.Market) string {
	return market.Quote + "-" + market.Base
}

func fromUpbitMarket(native string) (unified.Market, error) {
	quote, base, found := strings.Cut(native, "-")
	market := unified.Market{Base: base, Quote: quote}
	if !found || market.Validate() != nil {
		return unified.Market{}, fmt.Errorf("invalid Upbit market %q", native)
	}
	return market, nil
}

func toUpbitMinuteUnit(value unified.CandleInterval) MinuteUnit {
	switch value {
	case unified.Candle1Minute:
		return Minute1
	case unified.Candle3Minutes:
		return Minute3
	case unified.Candle5Minutes:
		return Minute5
	case unified.Candle15Minutes:
		return Minute15
	case unified.Candle30Minutes:
		return Minute30
	case unified.Candle1Hour:
		return Minute60
	case unified.Candle4Hours:
		return Minute240
	default:
		return 0
	}
}

func toUpbitSide(side unified.Side) Side {
	if side == unified.SideBuy {
		return SideBid
	}
	return SideAsk
}

func toUpbitTimeInForce(value unified.TimeInForce) TimeInForce {
	switch value {
	case unified.TimeInForceIOC:
		return TimeInForceIOC
	case unified.TimeInForceFOK:
		return TimeInForceFOK
	case unified.TimeInForcePostOnly:
		return TimeInForcePostOnly
	default:
		return ""
	}
}

func fromUpbitOrder(native Order, market unified.Market) unified.Order {
	return unified.Order{
		Exchange: model.ExchangeUpbit, ID: native.UUID, ClientOrderID: native.Identifier,
		Market: market, NativeMarket: native.Market,
		Side: toUnifiedUpbitSide(native.Side), Type: toUnifiedUpbitOrderType(native.OrderType),
		Status: toUnifiedUpbitStatus(native), Price: native.Price,
		Quantity: native.Volume, ExecutedQuantity: native.ExecutedVolume, Raw: native.Raw,
	}
}

func toUnifiedUpbitSide(side Side) unified.Side {
	if side == SideBid {
		return unified.SideBuy
	}
	return unified.SideSell
}

func toUnifiedUpbitOrderType(orderType OrderType) unified.OrderType {
	if orderType == OrderTypeLimit {
		return unified.OrderTypeLimit
	}
	return unified.OrderTypeMarket
}

func toUnifiedUpbitStatus(order Order) unified.OrderStatus {
	switch order.State {
	case OrderStateWait, OrderStateWatch:
		if order.ExecutedVolume != "" && strings.Trim(order.ExecutedVolume, "0.") != "" {
			return unified.OrderStatusPartiallyFilled
		}
		return unified.OrderStatusNew
	case OrderStateDone:
		return unified.OrderStatusFilled
	case OrderStateCancel:
		return unified.OrderStatusCanceled
	default:
		return unified.OrderStatusUnknown
	}
}
