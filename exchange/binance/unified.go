package binance

import (
	"context"
	"fmt"
	"strconv"

	trade "github.com/proven-trade/proven-trade-sdk"
	"github.com/proven-trade/proven-trade-sdk/model"
	"github.com/proven-trade/proven-trade-sdk/unified"
)

// UnifiedSpot은 Binance native 클라이언트를 공통 Spot 계약으로 변환한다.
type UnifiedSpot struct {
	client *Client
}

var _ unified.SpotClient = (*UnifiedSpot)(nil)

// NewUnifiedSpot은 Binance Spot 공통 어댑터를 생성한다.
func NewUnifiedSpot(client *Client) (*UnifiedSpot, error) {
	if client == nil {
		return nil, fmt.Errorf("Binance client is required")
	}
	return &UnifiedSpot{client: client}, nil
}

// Exchange는 Binance 거래소 식별자를 반환한다.
func (adapter *UnifiedSpot) Exchange() model.ExchangeID {
	return model.ExchangeBinance
}

// Markets는 Binance Spot 거래 마켓과 상태를 공통 형식으로 조회한다.
func (adapter *UnifiedSpot) Markets(
	ctx context.Context,
	options ...trade.RequestOption,
) ([]unified.MarketInfo, error) {
	info, err := adapter.client.ExchangeInfo(ctx, ExchangeInfoRequest{}, options...)
	if err != nil {
		return nil, err
	}
	markets := make([]unified.MarketInfo, 0, len(info.Symbols))
	for _, symbol := range info.Symbols {
		if !symbol.SpotTradingAllowed {
			continue
		}
		markets = append(markets, unified.MarketInfo{
			Exchange:     model.ExchangeBinance,
			Market:       unified.Market{Base: symbol.BaseAsset, Quote: symbol.QuoteAsset},
			NativeMarket: symbol.Symbol, Status: symbol.Status,
		})
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
	native, err := adapter.client.TickerPrice(ctx, TickerPriceRequest{Symbol: binanceSymbol(request.Market)}, options...)
	if err != nil {
		return unified.Ticker{}, err
	}
	return unified.Ticker{
		Exchange: model.ExchangeBinance, Market: request.Market,
		NativeMarket: native.Symbol, Price: native.Price, Raw: native.Raw,
	}, nil
}

// OrderBook은 Binance 호가를 공통 형식으로 조회한다.
func (adapter *UnifiedSpot) OrderBook(
	ctx context.Context,
	request unified.OrderBookRequest,
	options ...trade.RequestOption,
) (unified.OrderBook, error) {
	if err := request.Validate(); err != nil {
		return unified.OrderBook{}, err
	}
	native, err := adapter.client.OrderBook(ctx, OrderBookRequest{
		Symbol: binanceSymbol(request.Market), Limit: request.Limit,
	}, options...)
	if err != nil {
		return unified.OrderBook{}, err
	}
	return unified.OrderBook{
		Exchange: model.ExchangeBinance, Market: request.Market,
		NativeMarket: binanceSymbol(request.Market),
		Bids:         fromBinanceBookLevels(native.Bids), Asks: fromBinanceBookLevels(native.Asks), Raw: native.Raw,
	}, nil
}

// RecentTrades는 Binance 공개 최근 체결을 공통 형식으로 조회한다.
func (adapter *UnifiedSpot) RecentTrades(
	ctx context.Context,
	request unified.RecentTradesRequest,
	options ...trade.RequestOption,
) ([]unified.PublicTrade, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	native, err := adapter.client.RecentTrades(ctx, RecentTradesRequest{
		Symbol: binanceSymbol(request.Market), Limit: request.Limit,
	}, options...)
	if err != nil {
		return nil, err
	}
	trades := make([]unified.PublicTrade, len(native))
	for index, item := range native {
		side := unified.SideBuy
		if item.BuyerMaker {
			side = unified.SideSell
		}
		trades[index] = unified.PublicTrade{
			ID: strconv.FormatInt(item.ID, 10), Price: item.Price,
			Quantity: item.Quantity, Side: side, Timestamp: item.Time,
		}
	}
	return trades, nil
}

// Candles는 Binance OHLCV 캔들을 공통 형식으로 조회한다.
func (adapter *UnifiedSpot) Candles(
	ctx context.Context,
	request unified.CandlesRequest,
	options ...trade.RequestOption,
) ([]unified.Candle, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	native, err := adapter.client.Klines(ctx, KlinesRequest{
		Symbol: binanceSymbol(request.Market), Interval: toBinanceCandleInterval(request.Interval), Limit: request.Limit,
	}, options...)
	if err != nil {
		return nil, err
	}
	candles := make([]unified.Candle, len(native))
	for index, item := range native {
		candles[index] = unified.Candle{
			StartTime: item.OpenTime, Open: item.Open, High: item.High,
			Low: item.Low, Close: item.Close, Volume: item.Volume,
		}
	}
	return candles, nil
}

// Balances는 Binance Spot 자산 잔고를 공통 형식으로 조회한다.
func (adapter *UnifiedSpot) Balances(
	ctx context.Context,
	options ...trade.RequestOption,
) ([]unified.Balance, error) {
	account, err := adapter.client.Account(ctx, AccountRequest{}, options...)
	if err != nil {
		return nil, err
	}
	balances := make([]unified.Balance, len(account.Balances))
	for index, balance := range account.Balances {
		balances[index] = unified.Balance{
			Asset: balance.Asset, Available: balance.Free, Locked: balance.Locked,
		}
	}
	return balances, nil
}

// PlaceOrder는 공통 Spot 주문을 Binance 주문으로 변환해 생성한다.
func (adapter *UnifiedSpot) PlaceOrder(
	ctx context.Context,
	request unified.PlaceOrderRequest,
	options ...trade.RequestOption,
) (unified.Order, error) {
	if err := request.Validate(); err != nil {
		return unified.Order{}, err
	}
	nativeRequest := NewOrderRequest{
		Symbol: binanceSymbol(request.Market), Side: toBinanceSide(request.Side),
		ClientOrderID: request.ClientOrderID, ResponseType: NewOrderResponseResult,
	}
	switch request.Type {
	case unified.OrderTypeLimit:
		nativeRequest.Type = OrderTypeLimit
		nativeRequest.Quantity = request.Quantity
		nativeRequest.Price = request.Price
		nativeRequest.TimeInForce = toBinanceTimeInForce(request.TimeInForce)
		if nativeRequest.TimeInForce == "" {
			nativeRequest.TimeInForce = TimeInForceGTC
		}
		if request.TimeInForce == unified.TimeInForcePostOnly {
			nativeRequest.Type = OrderTypeLimitMaker
			nativeRequest.TimeInForce = ""
		}
	case unified.OrderTypeMarket:
		nativeRequest.Type = OrderTypeMarket
		if request.Side == unified.SideBuy {
			nativeRequest.QuoteOrderQuantity = request.QuoteAmount
		} else {
			nativeRequest.Quantity = request.Quantity
		}
	}
	native, err := adapter.client.NewOrder(ctx, nativeRequest, options...)
	if err != nil {
		return unified.Order{}, err
	}
	return fromBinanceOrder(native, request.Market), nil
}

// Order는 Binance 주문을 단건 조회한다.
func (adapter *UnifiedSpot) Order(
	ctx context.Context,
	request unified.OrderRequest,
	options ...trade.RequestOption,
) (unified.Order, error) {
	nativeRequest, err := binanceOrderIdentity(request)
	if err != nil {
		return unified.Order{}, err
	}
	native, err := adapter.client.QueryOrder(ctx, nativeRequest, options...)
	if err != nil {
		return unified.Order{}, err
	}
	return fromBinanceOrder(native, request.Market), nil
}

// CancelOrder는 Binance 주문 취소를 요청한다.
func (adapter *UnifiedSpot) CancelOrder(
	ctx context.Context,
	request unified.OrderRequest,
	options ...trade.RequestOption,
) (unified.Order, error) {
	identity, err := binanceOrderIdentity(request)
	if err != nil {
		return unified.Order{}, err
	}
	native, err := adapter.client.CancelOrder(ctx, CancelOrderRequest{
		Symbol: identity.Symbol, OrderID: identity.OrderID,
		OriginalClientOrderID: identity.OriginalClientOrderID,
	}, options...)
	if err != nil {
		return unified.Order{}, err
	}
	return fromBinanceOrder(native, request.Market), nil
}

// OpenOrders는 Binance 미체결 주문을 단일 또는 전체 마켓에서 조회한다.
func (adapter *UnifiedSpot) OpenOrders(
	ctx context.Context,
	request unified.OpenOrdersRequest,
	options ...trade.RequestOption,
) ([]unified.Order, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	nativeRequest := OpenOrdersRequest{AllSymbols: request.AllMarkets}
	if request.Market != nil {
		nativeRequest.Symbol = binanceSymbol(*request.Market)
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
		}
		orders[index] = fromBinanceOrder(native, market)
	}
	return orders, nil
}

func binanceOrderIdentity(request unified.OrderRequest) (QueryOrderRequest, error) {
	if err := request.Validate(); err != nil {
		return QueryOrderRequest{}, err
	}
	native := QueryOrderRequest{Symbol: binanceSymbol(request.Market), OriginalClientOrderID: request.ClientOrderID}
	if request.OrderID != "" {
		orderID, err := strconv.ParseInt(request.OrderID, 10, 64)
		if err != nil || orderID <= 0 {
			return QueryOrderRequest{}, fmt.Errorf("%w: Binance order ID must be a positive integer", trade.ErrValidation)
		}
		native.OrderID = &orderID
	}
	return native, nil
}

func binanceSymbol(market unified.Market) string {
	return market.Base + market.Quote
}

func fromBinanceBookLevels(native []BookLevel) []unified.BookLevel {
	levels := make([]unified.BookLevel, len(native))
	for index, level := range native {
		levels[index] = unified.BookLevel{Price: level.Price, Quantity: level.Quantity}
	}
	return levels
}

func toBinanceCandleInterval(value unified.CandleInterval) KlineInterval {
	return KlineInterval(value)
}

func toBinanceSide(side unified.Side) Side {
	if side == unified.SideBuy {
		return SideBuy
	}
	return SideSell
}

func toBinanceTimeInForce(value unified.TimeInForce) TimeInForce {
	switch value {
	case unified.TimeInForceIOC:
		return TimeInForceIOC
	case unified.TimeInForceFOK:
		return TimeInForceFOK
	case unified.TimeInForceGTC:
		return TimeInForceGTC
	default:
		return ""
	}
}

func fromBinanceOrder(native Order, market unified.Market) unified.Order {
	clientOrderID := native.ClientOrderID
	if native.OriginalClientOrderID != "" {
		clientOrderID = native.OriginalClientOrderID
	}
	return unified.Order{
		Exchange: model.ExchangeBinance, ID: strconv.FormatInt(native.OrderID, 10),
		ClientOrderID: clientOrderID, Market: market, NativeMarket: native.Symbol,
		Side: toUnifiedBinanceSide(native.Side), Type: toUnifiedBinanceOrderType(native.Type),
		Status: toUnifiedBinanceStatus(native.Status), Price: native.Price,
		Quantity: native.OriginalQuantity, ExecutedQuantity: native.ExecutedQuantity, Raw: native.Raw,
	}
}

func toUnifiedBinanceSide(side Side) unified.Side {
	if side == SideBuy {
		return unified.SideBuy
	}
	return unified.SideSell
}

func toUnifiedBinanceOrderType(orderType OrderType) unified.OrderType {
	if orderType == OrderTypeMarket {
		return unified.OrderTypeMarket
	}
	return unified.OrderTypeLimit
}

func toUnifiedBinanceStatus(status OrderStatus) unified.OrderStatus {
	switch status {
	case OrderStatusNew, OrderStatusPendingNew, OrderStatusPendingCancel:
		return unified.OrderStatusNew
	case OrderStatusPartiallyFilled:
		return unified.OrderStatusPartiallyFilled
	case OrderStatusFilled:
		return unified.OrderStatusFilled
	case OrderStatusCanceled:
		return unified.OrderStatusCanceled
	case OrderStatusRejected:
		return unified.OrderStatusRejected
	case OrderStatusExpired, OrderStatusExpiredInMatch:
		return unified.OrderStatusExpired
	default:
		return unified.OrderStatusUnknown
	}
}
