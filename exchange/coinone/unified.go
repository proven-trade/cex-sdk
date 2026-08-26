package coinone

import (
	"context"
	"fmt"
	"strings"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/model"
	"github.com/proven-trade/cex-sdk/unified"
)

const coinoneDefaultQuoteCurrency = "KRW"

// UnifiedSpot은 Coinone native 클라이언트를 공통 Spot 계약으로 변환한다.
type UnifiedSpot struct {
	client *Client
}

var _ unified.SpotClient = (*UnifiedSpot)(nil)

// NewUnifiedSpot은 Coinone Spot 공통 어댑터를 생성한다.
func NewUnifiedSpot(client *Client) (*UnifiedSpot, error) {
	if client == nil {
		return nil, fmt.Errorf("Coinone client is required")
	}
	return &UnifiedSpot{client: client}, nil
}

// Exchange는 Coinone 거래소 식별자를 반환한다.
func (adapter *UnifiedSpot) Exchange() model.ExchangeID {
	return model.ExchangeCoinone
}

// Markets는 Coinone 원화 Spot 거래 마켓과 상태를 공통 형식으로 조회한다.
func (adapter *UnifiedSpot) Markets(
	ctx context.Context,
	options ...trade.RequestOption,
) ([]unified.MarketInfo, error) {
	native, err := adapter.client.Markets(ctx, MarketsRequest{
		QuoteCurrency: coinoneDefaultQuoteCurrency,
	}, options...)
	if err != nil {
		return nil, err
	}
	markets := make([]unified.MarketInfo, len(native))
	for index, item := range native {
		market := unified.Market{Base: item.TargetCurrency, Quote: item.QuoteCurrency}
		if err := market.Validate(); err != nil {
			return nil, fmt.Errorf("invalid Coinone market %q: %w", coinoneMarket(market), err)
		}
		markets[index] = unified.MarketInfo{
			Exchange: model.ExchangeCoinone, Market: market,
			NativeMarket: coinoneMarket(market), Status: coinoneMarketStatus(item), Raw: item.Raw,
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
	native, err := adapter.client.Ticker(ctx, TickerRequest{
		QuoteCurrency: request.Market.Quote, TargetCurrency: request.Market.Base,
	}, options...)
	if err != nil {
		return unified.Ticker{}, err
	}
	return unified.Ticker{
		Exchange: model.ExchangeCoinone, Market: request.Market,
		NativeMarket: coinonePair(native.QuoteCurrency, native.TargetCurrency),
		Price:        string(native.Last), Raw: native.Raw,
	}, nil
}

// OrderBook은 Coinone 호가를 공통 형식으로 조회한다.
func (adapter *UnifiedSpot) OrderBook(
	ctx context.Context,
	request unified.OrderBookRequest,
	options ...trade.RequestOption,
) (unified.OrderBook, error) {
	if err := request.Validate(); err != nil {
		return unified.OrderBook{}, err
	}
	native, err := adapter.client.OrderBook(ctx, OrderBookRequest{
		QuoteCurrency: request.Market.Quote, TargetCurrency: request.Market.Base,
		Size: coinoneOrderBookSize(request.Limit),
	}, options...)
	if err != nil {
		return unified.OrderBook{}, err
	}
	bidDepth := limitedCoinoneDepth(len(native.Bids), request.Limit)
	askDepth := limitedCoinoneDepth(len(native.Asks), request.Limit)
	bids := make([]unified.BookLevel, bidDepth)
	asks := make([]unified.BookLevel, askDepth)
	for index, level := range native.Bids[:bidDepth] {
		bids[index] = unified.BookLevel{Price: string(level.Price), Quantity: string(level.Quantity)}
	}
	for index, level := range native.Asks[:askDepth] {
		asks[index] = unified.BookLevel{Price: string(level.Price), Quantity: string(level.Quantity)}
	}
	return unified.OrderBook{
		Exchange: model.ExchangeCoinone, Market: request.Market,
		NativeMarket: coinonePair(native.QuoteCurrency, native.TargetCurrency),
		Bids:         bids, Asks: asks, Timestamp: native.Timestamp, Raw: native.Raw,
	}, nil
}

// RecentTrades는 Coinone 공개 최근 체결을 공통 형식으로 조회한다.
func (adapter *UnifiedSpot) RecentTrades(
	ctx context.Context,
	request unified.RecentTradesRequest,
	options ...trade.RequestOption,
) ([]unified.PublicTrade, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	native, err := adapter.client.RecentTrades(ctx, RecentTradesRequest{
		QuoteCurrency: request.Market.Quote, TargetCurrency: request.Market.Base,
		Size: coinoneRecentTradesSize(request.Limit),
	}, options...)
	if err != nil {
		return nil, err
	}
	depth := limitedCoinoneDepth(len(native), request.Limit)
	trades := make([]unified.PublicTrade, depth)
	for index, item := range native[:depth] {
		side := unified.SideBuy
		if item.IsSellerMaker {
			side = unified.SideSell
		}
		trades[index] = unified.PublicTrade{
			ID: item.ID, Price: string(item.Price), Quantity: string(item.Quantity),
			Side: side, Timestamp: item.Timestamp,
		}
	}
	return trades, nil
}

// Candles는 Coinone Spot OHLCV 캔들을 공통 형식으로 조회한다.
func (adapter *UnifiedSpot) Candles(
	ctx context.Context,
	request unified.CandlesRequest,
	options ...trade.RequestOption,
) ([]unified.Candle, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	page, err := adapter.client.Candles(ctx, CandlesRequest{
		QuoteCurrency: request.Market.Quote, TargetCurrency: request.Market.Base,
		Interval: toCoinoneCandleInterval(request.Interval), Size: request.Limit,
	}, options...)
	if err != nil {
		return nil, err
	}
	candles := make([]unified.Candle, len(page.Chart))
	for index, item := range page.Chart {
		candles[index] = unified.Candle{
			StartTime: item.Timestamp, Open: string(item.Open), High: string(item.High),
			Low: string(item.Low), Close: string(item.Close), Volume: string(item.TargetVolume),
		}
	}
	return candles, nil
}

// Balances는 Coinone 계정 잔고를 공통 형식으로 조회한다.
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
			Asset: balance.Currency, Available: string(balance.Available),
			Locked: string(balance.Limit), Raw: balance.Raw,
		}
	}
	return balances, nil
}

// PlaceOrder는 공통 Spot 주문을 Coinone 주문으로 변환해 생성한다.
func (adapter *UnifiedSpot) PlaceOrder(
	ctx context.Context,
	request unified.PlaceOrderRequest,
	options ...trade.RequestOption,
) (unified.Order, error) {
	if err := request.Validate(); err != nil {
		return unified.Order{}, err
	}
	nativeRequest := PlaceOrderRequest{
		Side: toCoinoneSide(request.Side), QuoteCurrency: request.Market.Quote,
		TargetCurrency: request.Market.Base, UserOrderID: request.ClientOrderID,
	}
	if request.Type == unified.OrderTypeLimit {
		if request.TimeInForce == unified.TimeInForceIOC || request.TimeInForce == unified.TimeInForceFOK {
			return unified.Order{}, validationError("Coinone unified limit orders do not support %s", request.TimeInForce)
		}
		nativeRequest.Type = OrderTypeLimit
		nativeRequest.Price = request.Price
		nativeRequest.Quantity = request.Quantity
		nativeRequest.PostOnly = request.TimeInForce == unified.TimeInForcePostOnly
	} else {
		nativeRequest.Type = OrderTypeMarket
		if request.Side == unified.SideBuy {
			nativeRequest.Amount = request.QuoteAmount
		} else {
			nativeRequest.Quantity = request.Quantity
		}
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
		Exchange: model.ExchangeCoinone, ID: reference.OrderID,
		ClientOrderID: request.ClientOrderID, Market: request.Market,
		NativeMarket: coinoneMarket(request.Market), Side: request.Side, Type: request.Type,
		Status: unified.OrderStatusNew, Price: request.Price, Quantity: quantity, Raw: reference.Raw,
	}, nil
}

// Order는 Coinone 주문을 거래소 주문 ID 또는 사용자 주문 ID로 조회한다.
func (adapter *UnifiedSpot) Order(
	ctx context.Context,
	request unified.OrderRequest,
	options ...trade.RequestOption,
) (unified.Order, error) {
	if err := request.Validate(); err != nil {
		return unified.Order{}, err
	}
	native, err := adapter.client.OrderInfo(ctx, OrderInfoRequest{
		OrderID: request.OrderID, UserOrderID: request.ClientOrderID,
		QuoteCurrency: request.Market.Quote, TargetCurrency: request.Market.Base,
	}, options...)
	if err != nil {
		return unified.Order{}, err
	}
	return fromCoinoneOrderDetail(native, request.Market), nil
}

// CancelOrder는 Coinone 주문 취소를 접수한다.
func (adapter *UnifiedSpot) CancelOrder(
	ctx context.Context,
	request unified.OrderRequest,
	options ...trade.RequestOption,
) (unified.Order, error) {
	if err := request.Validate(); err != nil {
		return unified.Order{}, err
	}
	native, err := adapter.client.CancelOrder(ctx, CancelOrderRequest{
		OrderID: request.OrderID, UserOrderID: request.ClientOrderID,
		QuoteCurrency: request.Market.Quote, TargetCurrency: request.Market.Base,
	}, options...)
	if err != nil {
		return unified.Order{}, err
	}
	return unified.Order{
		Exchange: model.ExchangeCoinone, ID: native.OrderID,
		ClientOrderID: request.ClientOrderID, Market: request.Market,
		NativeMarket: coinoneMarket(request.Market), Side: toUnifiedCoinoneSide(native.Side),
		Status: unified.OrderStatusCanceled, Price: string(native.Price),
		Quantity: string(native.OriginalQuantity), ExecutedQuantity: string(native.TradedQuantity), Raw: native.Raw,
	}, nil
}

// OpenOrders는 Coinone 미체결 주문을 단일 또는 전체 마켓에서 조회한다.
func (adapter *UnifiedSpot) OpenOrders(
	ctx context.Context,
	request unified.OpenOrdersRequest,
	options ...trade.RequestOption,
) ([]unified.Order, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	nativeRequest := ActiveOrdersRequest{AllMarkets: request.AllMarkets}
	if request.Market != nil {
		nativeRequest.QuoteCurrency = request.Market.Quote
		nativeRequest.TargetCurrency = request.Market.Base
	}
	native, err := adapter.client.ActiveOrders(ctx, nativeRequest, options...)
	if err != nil {
		return nil, err
	}
	orders := make([]unified.Order, len(native))
	for index, item := range native {
		market := unified.Market{Base: item.TargetCurrency, Quote: item.QuoteCurrency}
		if err := market.Validate(); err != nil {
			return nil, fmt.Errorf("invalid Coinone active order market %q: %w", coinoneMarket(market), err)
		}
		orders[index] = fromCoinoneActiveOrder(item, market)
	}
	return orders, nil
}

func coinoneMarket(market unified.Market) string {
	return coinonePair(market.Quote, market.Base)
}

func coinonePair(quoteCurrency, targetCurrency string) string {
	return quoteCurrency + "-" + targetCurrency
}

func coinoneMarketStatus(market Market) string {
	if market.MaintenanceState == 1 {
		return "maintenance"
	}
	switch market.TradeState {
	case 1:
		return "trading"
	case 2:
		return "sell_only"
	case 3:
		return "buy_only"
	default:
		return "suspended"
	}
}

func coinoneOrderBookSize(limit int) int {
	switch {
	case limit == 0 || limit > 15:
		return 16
	case limit > 10:
		return 15
	case limit > 5:
		return 10
	default:
		return 5
	}
}

func coinoneRecentTradesSize(limit int) int {
	switch {
	case limit == 0 || limit > 50:
		return 100
	case limit > 10:
		return 50
	default:
		return 10
	}
}

func limitedCoinoneDepth(length, limit int) int {
	if limit > 0 && limit < length {
		return limit
	}
	return length
}

func toCoinoneCandleInterval(value unified.CandleInterval) CandleInterval {
	return CandleInterval(value)
}

func toCoinoneSide(side unified.Side) Side {
	if side == unified.SideBuy {
		return SideBuy
	}
	return SideSell
}

func toUnifiedCoinoneSide(side Side) unified.Side {
	if side == SideBuy {
		return unified.SideBuy
	}
	return unified.SideSell
}

func toUnifiedCoinoneOrderType(orderType OrderType) unified.OrderType {
	if orderType == OrderTypeMarket {
		return unified.OrderTypeMarket
	}
	return unified.OrderTypeLimit
}

func fromCoinoneOrderDetail(native OrderDetail, market unified.Market) unified.Order {
	quantity := string(native.OriginalQuantity)
	if native.Type == OrderTypeMarket && native.Side == SideBuy {
		quantity = string(native.OriginalAmount)
	}
	return unified.Order{
		Exchange: model.ExchangeCoinone, ID: native.OrderID, ClientOrderID: native.UserOrderID,
		Market: market, NativeMarket: coinonePair(native.QuoteCurrency, native.TargetCurrency),
		Side: toUnifiedCoinoneSide(native.Side), Type: toUnifiedCoinoneOrderType(native.Type),
		Status: toUnifiedCoinoneStatus(native.Status), Price: string(native.Price),
		Quantity: quantity, ExecutedQuantity: string(native.ExecutedQuantity), Raw: native.Raw,
	}
}

func fromCoinoneActiveOrder(native ActiveOrder, market unified.Market) unified.Order {
	status := unified.OrderStatusNew
	if native.ExecutedQuantity != "" && strings.Trim(string(native.ExecutedQuantity), "0.") != "" {
		status = unified.OrderStatusPartiallyFilled
	}
	return unified.Order{
		Exchange: model.ExchangeCoinone, ID: native.OrderID, ClientOrderID: native.UserOrderID,
		Market: market, NativeMarket: coinonePair(native.QuoteCurrency, native.TargetCurrency),
		Side: toUnifiedCoinoneSide(native.Side), Type: toUnifiedCoinoneOrderType(native.Type),
		Status: status, Price: string(native.Price), Quantity: string(native.OriginalQuantity),
		ExecutedQuantity: string(native.ExecutedQuantity), Raw: native.Raw,
	}
}

func toUnifiedCoinoneStatus(status string) unified.OrderStatus {
	switch status {
	case "LIVE", "NOT_TRIGGERED", "TRIGGERED":
		return unified.OrderStatusNew
	case "PARTIALLY_FILLED":
		return unified.OrderStatusPartiallyFilled
	case "FILLED":
		return unified.OrderStatusFilled
	case "PARTIALLY_CANCELED", "CANCELED", "NOT_TRIGGERED_PARTIALLY_CANCELED",
		"NOT_TRIGGERED_CANCELED", "CANCELED_NO_ORDER", "CANCELED_LIMIT_PRICE_EXCEED",
		"CANCELED_UNDER_PRODUCT_UNIT":
		return unified.OrderStatusCanceled
	default:
		return unified.OrderStatusUnknown
	}
}
