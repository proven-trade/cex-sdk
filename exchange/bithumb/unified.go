package bithumb

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

// UnifiedSpot은 Bithumb native 클라이언트를 공통 Spot 계약으로 변환한다.
type UnifiedSpot struct {
	client *Client
}

var _ unified.SpotClient = (*UnifiedSpot)(nil)

// NewUnifiedSpot은 Bithumb Spot 공통 어댑터를 생성한다.
func NewUnifiedSpot(client *Client) (*UnifiedSpot, error) {
	if client == nil {
		return nil, fmt.Errorf("Bithumb client is required")
	}
	return &UnifiedSpot{client: client}, nil
}

// Exchange는 Bithumb 거래소 식별자를 반환한다.
func (adapter *UnifiedSpot) Exchange() model.ExchangeID {
	return model.ExchangeBithumb
}

// Markets는 Bithumb Spot 거래 마켓과 상태를 공통 형식으로 조회한다.
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
		market, parseErr := fromBithumbMarket(item.Market)
		if parseErr != nil {
			return nil, parseErr
		}
		markets[index] = unified.MarketInfo{
			Exchange: model.ExchangeBithumb, Market: market,
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
		Markets: []string{bithumbMarket(request.Market)},
	}, options...)
	if err != nil {
		return unified.Ticker{}, err
	}
	if len(tickers) != 1 {
		return unified.Ticker{}, fmt.Errorf("Bithumb ticker response has %d items, want 1", len(tickers))
	}
	return unified.Ticker{
		Exchange: model.ExchangeBithumb, Market: request.Market,
		NativeMarket: tickers[0].Market, Price: string(tickers[0].TradePrice), Raw: tickers[0].Raw,
	}, nil
}

// OrderBook은 Bithumb 호가를 공통 형식으로 조회한다.
func (adapter *UnifiedSpot) OrderBook(
	ctx context.Context,
	request unified.OrderBookRequest,
	options ...trade.RequestOption,
) (unified.OrderBook, error) {
	if err := request.Validate(); err != nil {
		return unified.OrderBook{}, err
	}
	native, err := adapter.client.OrderBooks(ctx, OrderBooksRequest{
		Markets: []string{bithumbMarket(request.Market)},
	}, options...)
	if err != nil {
		return unified.OrderBook{}, err
	}
	if len(native) != 1 {
		return unified.OrderBook{}, fmt.Errorf("Bithumb order book response has %d items, want 1", len(native))
	}
	depth := len(native[0].OrderBook)
	if request.Limit > 0 && request.Limit < depth {
		depth = request.Limit
	}
	bids := make([]unified.BookLevel, depth)
	asks := make([]unified.BookLevel, depth)
	for index, level := range native[0].OrderBook[:depth] {
		bids[index] = unified.BookLevel{Price: string(level.BidPrice), Quantity: string(level.BidSize)}
		asks[index] = unified.BookLevel{Price: string(level.AskPrice), Quantity: string(level.AskSize)}
	}
	return unified.OrderBook{
		Exchange: model.ExchangeBithumb, Market: request.Market,
		NativeMarket: native[0].Market, Bids: bids, Asks: asks,
		Timestamp: native[0].Timestamp, Raw: native[0].Raw,
	}, nil
}

// RecentTrades는 Bithumb 공개 최근 체결을 공통 형식으로 조회한다.
func (adapter *UnifiedSpot) RecentTrades(
	ctx context.Context,
	request unified.RecentTradesRequest,
	options ...trade.RequestOption,
) ([]unified.PublicTrade, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	native, err := adapter.client.RecentTrades(ctx, RecentTradesRequest{
		Market: bithumbMarket(request.Market), Count: request.Limit,
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

// Candles는 Bithumb Spot 분봉을 공통 형식으로 조회한다.
func (adapter *UnifiedSpot) Candles(
	ctx context.Context,
	request unified.CandlesRequest,
	options ...trade.RequestOption,
) ([]unified.Candle, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	native, err := adapter.client.MinuteCandles(ctx, MinuteCandlesRequest{
		Market: bithumbMarket(request.Market), Unit: toBithumbMinuteUnit(request.Interval), Count: request.Limit,
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
				return nil, fmt.Errorf("decode Bithumb candle start time: %w", parseErr)
			}
			startTime = parsed.UnixMilli()
		}
		candles[index] = unified.Candle{
			StartTime: startTime, Open: string(item.OpeningPrice), High: string(item.HighPrice),
			Low: string(item.LowPrice), Close: string(item.TradePrice),
			Volume: string(item.AccumulatedTradeVolume),
		}
	}
	return candles, nil
}

// Balances는 Bithumb 계정 잔고를 공통 형식으로 조회한다.
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
			Asset: balance.Currency, Available: balance.Balance, Locked: balance.Locked, Raw: balance.Raw,
		}
	}
	return balances, nil
}

// PlaceOrder는 공통 Spot 주문을 Bithumb 주문으로 변환해 생성한다.
func (adapter *UnifiedSpot) PlaceOrder(
	ctx context.Context,
	request unified.PlaceOrderRequest,
	options ...trade.RequestOption,
) (unified.Order, error) {
	if err := request.Validate(); err != nil {
		return unified.Order{}, err
	}
	nativeRequest := PlaceOrderRequest{
		Market: bithumbMarket(request.Market), Side: toBithumbSide(request.Side),
		Volume: request.Quantity, Price: request.Price, ClientOrderID: request.ClientOrderID,
	}
	if request.Type == unified.OrderTypeLimit {
		nativeRequest.OrderType = OrderTypeLimit
		nativeRequest.TimeInForce = toBithumbTimeInForce(request.TimeInForce)
	} else if request.Side == unified.SideBuy {
		nativeRequest.OrderType = OrderTypePrice
		nativeRequest.Price = request.QuoteAmount
		nativeRequest.Volume = ""
	} else {
		nativeRequest.OrderType = OrderTypeMarket
		nativeRequest.Price = ""
	}
	reference, err := adapter.client.PlaceOrder(ctx, nativeRequest, options...)
	if err != nil {
		return unified.Order{}, err
	}
	quantity := request.Quantity
	if request.Type == unified.OrderTypeMarket && request.Side == unified.SideBuy {
		quantity = request.QuoteAmount
	}
	return unified.Order{
		Exchange: model.ExchangeBithumb, ID: reference.OrderID,
		ClientOrderID: reference.ClientOrderID, Market: request.Market,
		NativeMarket: reference.Market, Side: request.Side, Type: request.Type,
		Status: unified.OrderStatusNew, Price: request.Price, Quantity: quantity, Raw: reference.Raw,
	}, nil
}

// Order는 Bithumb 주문을 거래소 주문 ID 또는 사용자 주문 ID로 조회한다.
func (adapter *UnifiedSpot) Order(
	ctx context.Context,
	request unified.OrderRequest,
	options ...trade.RequestOption,
) (unified.Order, error) {
	if err := request.Validate(); err != nil {
		return unified.Order{}, err
	}
	native, err := adapter.client.OrderInfo(ctx, OrderInfoRequest{
		UUID: request.OrderID, ClientOrderID: request.ClientOrderID,
	}, options...)
	if err != nil {
		return unified.Order{}, err
	}
	return fromBithumbOrderDetail(native, request.Market), nil
}

// CancelOrder는 Bithumb 주문 취소를 접수한다.
func (adapter *UnifiedSpot) CancelOrder(
	ctx context.Context,
	request unified.OrderRequest,
	options ...trade.RequestOption,
) (unified.Order, error) {
	if err := request.Validate(); err != nil {
		return unified.Order{}, err
	}
	reference, err := adapter.client.CancelOrder(ctx, CancelOrderRequest{
		OrderID: request.OrderID, ClientOrderID: request.ClientOrderID,
	}, options...)
	if err != nil {
		return unified.Order{}, err
	}
	return unified.Order{
		Exchange: model.ExchangeBithumb, ID: reference.OrderID,
		ClientOrderID: reference.ClientOrderID, Market: request.Market,
		NativeMarket: bithumbMarket(request.Market), Status: unified.OrderStatusCanceled, Raw: reference.Raw,
	}, nil
}

// OpenOrders는 Bithumb 미체결 주문을 단일 또는 전체 마켓에서 끝까지 조회한다.
func (adapter *UnifiedSpot) OpenOrders(
	ctx context.Context,
	request unified.OpenOrdersRequest,
	options ...trade.RequestOption,
) ([]unified.Order, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	nativeRequest := PendingOrdersRequest{AllMarkets: request.AllMarkets, Limit: 100}
	requestedMarket := unified.Market{}
	if request.Market != nil {
		requestedMarket = *request.Market
		nativeRequest.Market = bithumbMarket(requestedMarket)
	}
	var orders []unified.Order
	seenCursors := make(map[string]struct{})
	for {
		page, err := adapter.client.PendingOrders(ctx, nativeRequest, options...)
		if err != nil {
			return nil, err
		}
		for _, native := range page.Data {
			market := requestedMarket
			if request.AllMarkets {
				market, err = fromBithumbMarket(native.Market)
				if err != nil {
					return nil, err
				}
			}
			orders = append(orders, fromBithumbOrderSummary(native, market))
		}
		if !page.HasNext {
			return orders, nil
		}
		if page.NextKey == "" {
			return nil, fmt.Errorf("Bithumb open orders cursor is empty")
		}
		if _, exists := seenCursors[page.NextKey]; exists {
			return nil, fmt.Errorf("Bithumb open orders returned a repeated cursor")
		}
		seenCursors[page.NextKey] = struct{}{}
		nativeRequest.NextKey = page.NextKey
	}
}

func bithumbMarket(market unified.Market) string {
	return market.Quote + "-" + market.Base
}

func fromBithumbMarket(native string) (unified.Market, error) {
	quote, base, found := strings.Cut(native, "-")
	market := unified.Market{Base: base, Quote: quote}
	if !found || market.Validate() != nil {
		return unified.Market{}, fmt.Errorf("invalid Bithumb market %q", native)
	}
	return market, nil
}

func toBithumbMinuteUnit(value unified.CandleInterval) MinuteUnit {
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

func toBithumbSide(side unified.Side) Side {
	if side == unified.SideBuy {
		return SideBid
	}
	return SideAsk
}

func toBithumbTimeInForce(value unified.TimeInForce) TimeInForce {
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

func fromBithumbOrderDetail(native OrderDetail, market unified.Market) unified.Order {
	return unified.Order{
		Exchange: model.ExchangeBithumb, ID: native.UUID, ClientOrderID: native.ClientOrderID,
		Market: market, NativeMarket: native.Market,
		Side: toUnifiedBithumbSide(native.Side), Type: toUnifiedBithumbOrderType(native.OrderType),
		Status: toUnifiedBithumbStatus(native.State, native.ExecutedVolume), Price: native.Price,
		Quantity: native.Volume, ExecutedQuantity: native.ExecutedVolume, Raw: native.Raw,
	}
}

func fromBithumbOrderSummary(native OrderSummary, market unified.Market) unified.Order {
	return unified.Order{
		Exchange: model.ExchangeBithumb, ID: native.OrderID, ClientOrderID: native.ClientOrderID,
		Market: market, NativeMarket: native.Market,
		Side: toUnifiedBithumbSide(native.Side), Type: toUnifiedBithumbOrderType(native.OrderType),
		Status: toUnifiedBithumbStatus(native.State, native.ExecutedVolume), Price: native.Price,
		Quantity: native.Volume, ExecutedQuantity: native.ExecutedVolume, Raw: native.Raw,
	}
}

func toUnifiedBithumbSide(side Side) unified.Side {
	if side == SideBid {
		return unified.SideBuy
	}
	return unified.SideSell
}

func toUnifiedBithumbOrderType(orderType OrderType) unified.OrderType {
	if orderType == OrderTypeLimit {
		return unified.OrderTypeLimit
	}
	return unified.OrderTypeMarket
}

func toUnifiedBithumbStatus(state OrderState, executedVolume string) unified.OrderStatus {
	switch state {
	case OrderStateWait, OrderStateWatch:
		if executedVolume != "" && strings.Trim(executedVolume, "0.") != "" {
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
