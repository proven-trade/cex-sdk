package bitget

import (
	"context"
	"fmt"
	"strconv"

	trade "github.com/proven-trade/proven-trade-sdk"
	"github.com/proven-trade/proven-trade-sdk/model"
	"github.com/proven-trade/proven-trade-sdk/unified"
)

// UnifiedSpot은 Bitget native 클라이언트를 공통 Spot 계약으로 변환한다.
type UnifiedSpot struct {
	client *Client
}

var _ unified.SpotClient = (*UnifiedSpot)(nil)

// NewUnifiedSpot은 Bitget Spot 공통 어댑터를 생성한다.
func NewUnifiedSpot(client *Client) (*UnifiedSpot, error) {
	if client == nil {
		return nil, fmt.Errorf("Bitget client is required")
	}
	return &UnifiedSpot{client: client}, nil
}

// Exchange는 Bitget 거래소 식별자를 반환한다.
func (adapter *UnifiedSpot) Exchange() model.ExchangeID {
	return model.ExchangeBitget
}

// Markets는 Bitget Spot 거래 마켓과 상태를 공통 형식으로 조회한다.
func (adapter *UnifiedSpot) Markets(
	ctx context.Context,
	options ...trade.RequestOption,
) ([]unified.MarketInfo, error) {
	native, err := adapter.client.Instruments(ctx, InstrumentsRequest{Category: CategorySpot}, options...)
	if err != nil {
		return nil, err
	}
	markets := make([]unified.MarketInfo, len(native))
	for index, instrument := range native {
		markets[index] = unified.MarketInfo{
			Exchange:     model.ExchangeBitget,
			Market:       unified.Market{Base: instrument.BaseCoin, Quote: instrument.QuoteCoin},
			NativeMarket: instrument.Symbol, Status: instrument.Status, Raw: instrument.Raw,
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
		Category: CategorySpot, Symbol: bitgetSymbol(request.Market),
	}, options...)
	if err != nil {
		return unified.Ticker{}, err
	}
	if len(tickers) != 1 {
		return unified.Ticker{}, fmt.Errorf("Bitget ticker response has %d items, want 1", len(tickers))
	}
	return unified.Ticker{
		Exchange: model.ExchangeBitget, Market: request.Market,
		NativeMarket: tickers[0].Symbol, Price: tickers[0].LastPrice, Raw: tickers[0].Raw,
	}, nil
}

// OrderBook은 Bitget 호가를 공통 형식으로 조회한다.
func (adapter *UnifiedSpot) OrderBook(
	ctx context.Context,
	request unified.OrderBookRequest,
	options ...trade.RequestOption,
) (unified.OrderBook, error) {
	if err := request.Validate(); err != nil {
		return unified.OrderBook{}, err
	}
	native, err := adapter.client.OrderBook(ctx, OrderBookRequest{
		Category: CategorySpot, Symbol: bitgetSymbol(request.Market), Limit: request.Limit,
	}, options...)
	if err != nil {
		return unified.OrderBook{}, err
	}
	timestamp, err := strconv.ParseInt(native.Timestamp, 10, 64)
	if err != nil && native.Timestamp != "" {
		return unified.OrderBook{}, fmt.Errorf("decode Bitget order book timestamp: %w", err)
	}
	return unified.OrderBook{
		Exchange: model.ExchangeBitget, Market: request.Market,
		NativeMarket: bitgetSymbol(request.Market), Timestamp: timestamp,
		Bids: fromBitgetBookLevels(native.Bids), Asks: fromBitgetBookLevels(native.Asks), Raw: native.Raw,
	}, nil
}

// RecentTrades는 Bitget 공개 최근 체결을 공통 형식으로 조회한다.
func (adapter *UnifiedSpot) RecentTrades(
	ctx context.Context,
	request unified.RecentTradesRequest,
	options ...trade.RequestOption,
) ([]unified.PublicTrade, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	native, err := adapter.client.RecentFills(ctx, RecentFillsRequest{
		Category: CategorySpot, Symbol: bitgetSymbol(request.Market), Limit: request.Limit,
	}, options...)
	if err != nil {
		return nil, err
	}
	trades := make([]unified.PublicTrade, len(native))
	for index, item := range native {
		timestamp, parseErr := strconv.ParseInt(item.Timestamp, 10, 64)
		if parseErr != nil && item.Timestamp != "" {
			return nil, fmt.Errorf("decode Bitget trade timestamp: %w", parseErr)
		}
		trades[index] = unified.PublicTrade{
			ID: item.ExecutionID, Price: item.Price, Quantity: item.Quantity,
			Side: toUnifiedBitgetSide(item.Side), Timestamp: timestamp,
		}
	}
	return trades, nil
}

// Candles는 Bitget Spot OHLCV 캔들을 공통 형식으로 조회한다.
func (adapter *UnifiedSpot) Candles(
	ctx context.Context,
	request unified.CandlesRequest,
	options ...trade.RequestOption,
) ([]unified.Candle, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	native, err := adapter.client.Candles(ctx, CandlesRequest{
		Category: CategorySpot, Symbol: bitgetSymbol(request.Market),
		Interval: toBitgetCandleInterval(request.Interval), Limit: request.Limit,
	}, options...)
	if err != nil {
		return nil, err
	}
	candles := make([]unified.Candle, len(native))
	for index, item := range native {
		startTime, parseErr := strconv.ParseInt(item.Timestamp, 10, 64)
		if parseErr != nil {
			return nil, fmt.Errorf("decode Bitget candle timestamp: %w", parseErr)
		}
		candles[index] = unified.Candle{
			StartTime: startTime, Open: item.Open, High: item.High,
			Low: item.Low, Close: item.Close, Volume: item.Volume,
		}
	}
	return candles, nil
}

// Balances는 Bitget 통합 계정의 Spot 사용 가능 잔고를 공통 형식으로 조회한다.
func (adapter *UnifiedSpot) Balances(
	ctx context.Context,
	options ...trade.RequestOption,
) ([]unified.Balance, error) {
	account, err := adapter.client.AccountAssets(ctx, options...)
	if err != nil {
		return nil, err
	}
	balances := make([]unified.Balance, len(account.Assets))
	for index, asset := range account.Assets {
		balances[index] = unified.Balance{
			Asset: asset.Coin, Available: asset.Available, Locked: asset.Locked,
		}
	}
	return balances, nil
}

// PlaceOrder는 공통 Spot 주문을 Bitget 주문으로 변환해 생성한다.
func (adapter *UnifiedSpot) PlaceOrder(
	ctx context.Context,
	request unified.PlaceOrderRequest,
	options ...trade.RequestOption,
) (unified.Order, error) {
	if err := request.Validate(); err != nil {
		return unified.Order{}, err
	}
	quantity := request.Quantity
	if request.Type == unified.OrderTypeMarket && request.Side == unified.SideBuy {
		quantity = request.QuoteAmount
	}
	native, err := adapter.client.PlaceOrder(ctx, PlaceOrderRequest{
		Category: CategorySpot, Symbol: bitgetSymbol(request.Market), Quantity: quantity,
		Price: request.Price, Side: toBitgetSide(request.Side), OrderType: toBitgetOrderType(request.Type),
		TimeInForce: toBitgetTimeInForce(request.TimeInForce), ClientOrderID: request.ClientOrderID,
	}, options...)
	if err != nil {
		return unified.Order{}, err
	}
	return unified.Order{
		Exchange: model.ExchangeBitget, ID: native.OrderID,
		ClientOrderID: native.ClientOrderID, Market: request.Market,
		NativeMarket: bitgetSymbol(request.Market),
		Side:         request.Side, Type: request.Type, Status: unified.OrderStatusNew, Raw: native.Raw,
	}, nil
}

// Order는 Bitget 주문을 단건 조회한다.
func (adapter *UnifiedSpot) Order(
	ctx context.Context,
	request unified.OrderRequest,
	options ...trade.RequestOption,
) (unified.Order, error) {
	if err := request.Validate(); err != nil {
		return unified.Order{}, err
	}
	native, err := adapter.client.OrderInfo(ctx, OrderInfoRequest{
		OrderID: request.OrderID, ClientOrderID: request.ClientOrderID,
	}, options...)
	if err != nil {
		return unified.Order{}, err
	}
	return fromBitgetOrder(native, request.Market), nil
}

// CancelOrder는 Bitget 주문 취소를 요청한다.
func (adapter *UnifiedSpot) CancelOrder(
	ctx context.Context,
	request unified.OrderRequest,
	options ...trade.RequestOption,
) (unified.Order, error) {
	if err := request.Validate(); err != nil {
		return unified.Order{}, err
	}
	native, err := adapter.client.CancelOrder(ctx, CancelOrderRequest{
		OrderID: request.OrderID, ClientOrderID: request.ClientOrderID, Category: CategorySpot,
	}, options...)
	if err != nil {
		return unified.Order{}, err
	}
	return unified.Order{
		Exchange: model.ExchangeBitget, ID: native.OrderID,
		ClientOrderID: native.ClientOrderID, Market: request.Market,
		NativeMarket: bitgetSymbol(request.Market),
		Status:       unified.OrderStatusCanceled, Raw: native.Raw,
	}, nil
}

// OpenOrders는 Bitget Spot 미체결 주문을 단일 또는 전체 마켓에서 조회한다.
func (adapter *UnifiedSpot) OpenOrders(
	ctx context.Context,
	request unified.OpenOrdersRequest,
	options ...trade.RequestOption,
) ([]unified.Order, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	nativeRequest := OpenOrdersRequest{Category: CategorySpot}
	if request.Market != nil {
		nativeRequest.Symbol = bitgetSymbol(*request.Market)
	}
	nativeOrders, err := adapter.client.OpenOrders(ctx, nativeRequest, options...)
	if err != nil {
		return nil, err
	}
	orders := make([]unified.Order, len(nativeOrders.Orders))
	for index, native := range nativeOrders.Orders {
		market := unified.Market{}
		if request.Market != nil {
			market = *request.Market
		}
		orders[index] = fromBitgetOrder(native, market)
	}
	return orders, nil
}

func bitgetSymbol(market unified.Market) string {
	return market.Base + market.Quote
}

func fromBitgetBookLevels(native []BookLevel) []unified.BookLevel {
	levels := make([]unified.BookLevel, len(native))
	for index, level := range native {
		levels[index] = unified.BookLevel{Price: level.Price, Quantity: level.Quantity}
	}
	return levels
}

func toBitgetCandleInterval(value unified.CandleInterval) CandleInterval {
	switch value {
	case unified.Candle1Hour:
		return Candle1Hour
	case unified.Candle4Hours:
		return Candle4Hours
	default:
		return CandleInterval(value)
	}
}

func toBitgetSide(side unified.Side) Side {
	if side == unified.SideBuy {
		return SideBuy
	}
	return SideSell
}

func toBitgetOrderType(orderType unified.OrderType) OrderType {
	if orderType == unified.OrderTypeMarket {
		return OrderTypeMarket
	}
	return OrderTypeLimit
}

func toBitgetTimeInForce(value unified.TimeInForce) TimeInForce {
	switch value {
	case unified.TimeInForceIOC:
		return TimeInForceIOC
	case unified.TimeInForceFOK:
		return TimeInForceFOK
	case unified.TimeInForcePostOnly:
		return TimeInForcePostOnly
	case unified.TimeInForceGTC:
		return TimeInForceGTC
	default:
		return ""
	}
}

func fromBitgetOrder(native Order, market unified.Market) unified.Order {
	return unified.Order{
		Exchange: model.ExchangeBitget, ID: native.OrderID, ClientOrderID: native.ClientOrderID,
		Market: market, NativeMarket: native.Symbol,
		Side: toUnifiedBitgetSide(native.Side), Type: toUnifiedBitgetOrderType(native.OrderType),
		Status: toUnifiedBitgetStatus(native.Status), Price: native.Price,
		Quantity: native.Quantity, ExecutedQuantity: native.ExecutedQuantity, Raw: native.Raw,
	}
}

func toUnifiedBitgetSide(side Side) unified.Side {
	if side == SideBuy {
		return unified.SideBuy
	}
	return unified.SideSell
}

func toUnifiedBitgetOrderType(orderType OrderType) unified.OrderType {
	if orderType == OrderTypeMarket {
		return unified.OrderTypeMarket
	}
	return unified.OrderTypeLimit
}

func toUnifiedBitgetStatus(status OrderStatus) unified.OrderStatus {
	switch status {
	case OrderStatusLive, OrderStatusNew:
		return unified.OrderStatusNew
	case OrderStatusPartiallyFilled:
		return unified.OrderStatusPartiallyFilled
	case OrderStatusFilled:
		return unified.OrderStatusFilled
	case OrderStatusCancelled:
		return unified.OrderStatusCanceled
	default:
		return unified.OrderStatusUnknown
	}
}
