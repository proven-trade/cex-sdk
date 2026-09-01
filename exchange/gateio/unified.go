package gateio

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math"
	"math/big"
	"strings"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/model"
	"github.com/proven-trade/cex-sdk/unified"
)

const (
	gateIOUnifiedDefaultOrderBookLimit = 16
	gateIOUnifiedDefaultTradeLimit     = 100
	gateIOUnifiedDefaultCandleLimit    = 200
	gateIOUnifiedOpenOrderPageSize     = 1000
)

// UnifiedSpot은 Gate.io API v4 native 클라이언트를 공통 Spot 계약으로 변환한다.
type UnifiedSpot struct {
	client *Client
}

var _ unified.SpotClient = (*UnifiedSpot)(nil)

// NewUnifiedSpot은 Gate.io Spot 공통 어댑터를 생성한다.
func NewUnifiedSpot(client *Client) (*UnifiedSpot, error) {
	if client == nil {
		return nil, fmt.Errorf("Gate.io client is required")
	}
	return &UnifiedSpot{client: client}, nil
}

// Exchange는 Gate.io 거래소 식별자를 반환한다.
func (adapter *UnifiedSpot) Exchange() model.ExchangeID {
	return model.ExchangeGateIO
}

// Markets는 Gate.io Spot 거래쌍과 거래 가능 상태를 공통 형식으로 조회한다.
func (adapter *UnifiedSpot) Markets(
	ctx context.Context,
	options ...trade.RequestOption,
) ([]unified.MarketInfo, error) {
	native, err := adapter.client.CurrencyPairs(ctx, options...)
	if err != nil {
		return nil, err
	}
	markets := make([]unified.MarketInfo, len(native))
	for index, pair := range native {
		market := unified.Market{Base: pair.Base, Quote: pair.Quote}
		if err := market.Validate(); err != nil || gateIOSymbol(market) != pair.ID {
			return nil, fmt.Errorf("invalid Gate.io Spot currency pair %q", pair.ID)
		}
		markets[index] = unified.MarketInfo{
			Exchange: model.ExchangeGateIO, Market: market, NativeMarket: pair.ID,
			Status:              fromGateIOTradeStatus(pair.TradeStatus),
			PriceIncrement:      unified.DecimalIncrement(pair.PricePrecision),
			QuantityIncrement:   unified.DecimalIncrement(pair.AmountPrecision),
			MinimumBaseQuantity: gateIOOptionalString(pair.MinimumBaseAmount),
			MinimumQuoteAmount:  gateIOOptionalString(pair.MinimumQuoteAmount), Raw: pair.Raw,
		}
	}
	return markets, nil
}

func gateIOOptionalString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// Ticker는 공통 마켓의 Gate.io 최신 가격을 조회한다.
func (adapter *UnifiedSpot) Ticker(
	ctx context.Context,
	request unified.TickerRequest,
	options ...trade.RequestOption,
) (unified.Ticker, error) {
	if err := request.Validate(); err != nil {
		return unified.Ticker{}, err
	}
	nativeMarket := gateIOSymbol(request.Market)
	native, err := adapter.client.Ticker(ctx, nativeMarket, options...)
	if err != nil {
		return unified.Ticker{}, err
	}
	if native.CurrencyPair != "" && native.CurrencyPair != nativeMarket {
		return unified.Ticker{}, fmt.Errorf(
			"Gate.io ticker market %q does not match %q", native.CurrencyPair, nativeMarket,
		)
	}
	return unified.Ticker{
		Exchange: model.ExchangeGateIO, Market: request.Market,
		NativeMarket: nativeMarket, Price: native.Last, Raw: native.Raw,
	}, nil
}

// OrderBook은 Gate.io Spot 호가 스냅샷을 공통 형식으로 조회한다.
func (adapter *UnifiedSpot) OrderBook(
	ctx context.Context,
	request unified.OrderBookRequest,
	options ...trade.RequestOption,
) (unified.OrderBook, error) {
	if err := request.Validate(); err != nil {
		return unified.OrderBook{}, err
	}
	limit := request.Limit
	if limit == 0 {
		limit = gateIOUnifiedDefaultOrderBookLimit
	}
	nativeMarket := gateIOSymbol(request.Market)
	native, err := adapter.client.OrderBook(ctx, OrderBookRequest{
		CurrencyPair: nativeMarket, Limit: limit,
	}, options...)
	if err != nil {
		return unified.OrderBook{}, err
	}
	if native.Current < 0 {
		return unified.OrderBook{}, fmt.Errorf("invalid Gate.io order book timestamp %d", native.Current)
	}
	return unified.OrderBook{
		Exchange: model.ExchangeGateIO, Market: request.Market, NativeMarket: nativeMarket,
		Bids: fromGateIOBookLevels(native.Bids), Asks: fromGateIOBookLevels(native.Asks),
		Timestamp: native.Current, Raw: native.Raw,
	}, nil
}

// RecentTrades는 Gate.io Spot 공개 최근 체결을 공통 형식으로 조회한다.
func (adapter *UnifiedSpot) RecentTrades(
	ctx context.Context,
	request unified.RecentTradesRequest,
	options ...trade.RequestOption,
) ([]unified.PublicTrade, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	limit := request.Limit
	if limit == 0 {
		limit = gateIOUnifiedDefaultTradeLimit
	}
	nativeMarket := gateIOSymbol(request.Market)
	native, err := adapter.client.RecentTrades(ctx, TradesRequest{
		CurrencyPair: nativeMarket, Limit: limit,
	}, options...)
	if err != nil {
		return nil, err
	}
	trades := make([]unified.PublicTrade, len(native))
	for index, item := range native {
		if item.CurrencyPair != "" && item.CurrencyPair != nativeMarket {
			return nil, fmt.Errorf(
				"Gate.io trades for %q contains %q", nativeMarket, item.CurrencyPair,
			)
		}
		timestamp, parseErr := gateIOTradeMilliseconds(item.CreatedAtMilli, item.CreatedAt)
		if parseErr != nil {
			return nil, parseErr
		}
		side, parseErr := fromGateIOSide(item.Side)
		if parseErr != nil {
			return nil, parseErr
		}
		trades[index] = unified.PublicTrade{
			ID: item.ID, Price: item.Price, Quantity: item.Amount,
			Side: side, Timestamp: timestamp,
		}
	}
	return trades, nil
}

// Candles는 Gate.io Spot OHLCV 캔들을 공통 형식으로 조회한다.
func (adapter *UnifiedSpot) Candles(
	ctx context.Context,
	request unified.CandlesRequest,
	options ...trade.RequestOption,
) ([]unified.Candle, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	interval := toGateIOCandleInterval(request.Interval)
	limit := request.Limit
	if limit == 0 {
		limit = gateIOUnifiedDefaultCandleLimit
	}
	nativeLimit := limit
	if request.Interval == unified.Candle3Minutes {
		nativeLimit *= 3
	}
	native, err := adapter.client.Candles(ctx, CandlesRequest{
		CurrencyPair: gateIOSymbol(request.Market), Interval: interval, Limit: nativeLimit,
	}, options...)
	if err != nil {
		return nil, err
	}
	candles := make([]unified.Candle, len(native))
	for index, item := range native {
		if item.Timestamp < 0 || item.Timestamp > math.MaxInt64/1000 {
			return nil, fmt.Errorf("invalid Gate.io candle timestamp %d", item.Timestamp)
		}
		if item.BaseVolume == "" {
			return nil, fmt.Errorf("Gate.io candle at %d does not contain base volume", item.Timestamp)
		}
		candles[index] = unified.Candle{
			StartTime: item.Timestamp * 1000, Open: item.Open, High: item.High,
			Low: item.Low, Close: item.Close, Volume: item.BaseVolume,
		}
	}
	if request.Interval == unified.Candle3Minutes {
		return unified.AggregateCandles(candles, 3*time.Minute, limit)
	}
	return candles, nil
}

// Balances는 Gate.io Spot 주문 가능·잠금 잔고를 공통 형식으로 조회한다.
func (adapter *UnifiedSpot) Balances(
	ctx context.Context,
	options ...trade.RequestOption,
) ([]unified.Balance, error) {
	native, err := adapter.client.Accounts(ctx, AccountsRequest{}, options...)
	if err != nil {
		return nil, err
	}
	balances := make([]unified.Balance, len(native))
	for index, account := range native {
		balances[index] = unified.Balance{
			Asset: account.Currency, Available: account.Available,
			Locked: account.Locked, Raw: account.Raw,
		}
	}
	return balances, nil
}

// PlaceOrder는 공통 Spot 주문을 Gate.io 주문으로 변환해 생성한다.
func (adapter *UnifiedSpot) PlaceOrder(
	ctx context.Context,
	request unified.PlaceOrderRequest,
	options ...trade.RequestOption,
) (unified.Order, error) {
	if err := request.Validate(); err != nil {
		return unified.Order{}, err
	}
	clientOrderID := request.ClientOrderID
	if clientOrderID == "" {
		generated, err := newGateIOClientOrderID()
		if err != nil {
			return unified.Order{}, err
		}
		clientOrderID = generated
	}
	nativeRequest := PlaceOrderRequest{
		ClientOrderID: clientOrderID, CurrencyPair: gateIOSymbol(request.Market),
		Account: "spot", Side: toGateIOSide(request.Side), Type: OrderTypeMarket,
		TimeInForce: TimeInForceIOC,
	}
	if request.Type == unified.OrderTypeLimit {
		nativeRequest.Type = OrderTypeLimit
		nativeRequest.Amount = request.Quantity
		nativeRequest.Price = request.Price
		nativeRequest.TimeInForce = toGateIOTimeInForce(request.TimeInForce)
		if nativeRequest.TimeInForce == "" {
			nativeRequest.TimeInForce = TimeInForceGTC
		}
	} else if request.Side == unified.SideBuy {
		nativeRequest.Amount = request.QuoteAmount
	} else {
		nativeRequest.Amount = request.Quantity
	}
	native, err := adapter.client.PlaceOrder(ctx, nativeRequest, options...)
	if err != nil {
		return unified.Order{}, err
	}
	return fromGateIOPlacedOrder(native, request, clientOrderID)
}

// Order는 Gate.io 주문을 거래소 주문 ID 또는 사용자 주문 ID로 조회한다.
func (adapter *UnifiedSpot) Order(
	ctx context.Context,
	request unified.OrderRequest,
	options ...trade.RequestOption,
) (unified.Order, error) {
	if err := request.Validate(); err != nil {
		return unified.Order{}, err
	}
	native, err := adapter.client.OrderInfo(ctx, OrderInfoRequest{
		OrderID: gateIOOrderIdentity(request), CurrencyPair: gateIOSymbol(request.Market),
	}, options...)
	if err != nil {
		return unified.Order{}, err
	}
	return fromGateIOOrder(native, request.Market)
}

// CancelOrder는 Gate.io 주문을 거래소 주문 ID 또는 사용자 주문 ID로 취소한다.
func (adapter *UnifiedSpot) CancelOrder(
	ctx context.Context,
	request unified.OrderRequest,
	options ...trade.RequestOption,
) (unified.Order, error) {
	if err := request.Validate(); err != nil {
		return unified.Order{}, err
	}
	native, err := adapter.client.CancelOrder(ctx, CancelOrderRequest{
		OrderID: gateIOOrderIdentity(request), CurrencyPair: gateIOSymbol(request.Market),
	}, options...)
	if err != nil {
		return unified.Order{}, err
	}
	order, err := fromGateIOOrder(native, request.Market)
	if err != nil {
		return unified.Order{}, err
	}
	if order.ID == "" {
		order.ID = request.OrderID
	}
	if order.ClientOrderID == "" {
		order.ClientOrderID = request.ClientOrderID
	}
	return order, nil
}

// OpenOrders는 Gate.io의 거래쌍 그룹형 미체결 주문을 단일 또는 전체 마켓에서 끝까지 조회한다.
func (adapter *UnifiedSpot) OpenOrders(
	ctx context.Context,
	request unified.OpenOrdersRequest,
	options ...trade.RequestOption,
) ([]unified.Order, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	target := ""
	if request.Market != nil {
		target = gateIOSymbol(*request.Market)
	}
	expected := make(map[string]int)
	received := make(map[string]int)
	var orders []unified.Order
	for pageNumber := 1; ; pageNumber++ {
		groups, err := adapter.client.OpenOrders(ctx, OpenOrdersRequest{
			Page: pageNumber, Limit: gateIOUnifiedOpenOrderPageSize,
		}, options...)
		if err != nil {
			return nil, err
		}
		for _, group := range groups {
			if target != "" && group.CurrencyPair != target {
				continue
			}
			market, parseErr := fromGateIOSymbol(group.CurrencyPair)
			if parseErr != nil {
				return nil, parseErr
			}
			if current, exists := expected[group.CurrencyPair]; exists && current != group.Total {
				return nil, fmt.Errorf(
					"Gate.io open order total for %q changed from %d to %d",
					group.CurrencyPair, current, group.Total,
				)
			}
			expected[group.CurrencyPair] = group.Total
			for _, native := range group.Orders {
				order, parseErr := fromGateIOOrder(native, market)
				if parseErr != nil {
					return nil, parseErr
				}
				orders = append(orders, order)
			}
			received[group.CurrencyPair] += len(group.Orders)
			if received[group.CurrencyPair] > group.Total {
				return nil, fmt.Errorf(
					"Gate.io open orders for %q returned %d items, total is %d",
					group.CurrencyPair, received[group.CurrencyPair], group.Total,
				)
			}
		}
		if gateIOOpenOrdersComplete(target, expected, received) {
			return orders, nil
		}
		if pageNumber >= 101 {
			return nil, fmt.Errorf("Gate.io open orders exceed the supported page offset")
		}
	}
}

func gateIOSymbol(market unified.Market) string {
	return market.Base + "_" + market.Quote
}

func fromGateIOSymbol(symbol string) (unified.Market, error) {
	base, quote, found := strings.Cut(symbol, "_")
	market := unified.Market{Base: base, Quote: quote}
	if !found || market.Validate() != nil || gateIOSymbol(market) != symbol {
		return unified.Market{}, fmt.Errorf("invalid Gate.io Spot currency pair %q", symbol)
	}
	return market, nil
}

func fromGateIOTradeStatus(status string) string {
	switch status {
	case "tradable":
		return "trading"
	case "buyable":
		return "buy_only"
	case "sellable":
		return "sell_only"
	default:
		return "disabled"
	}
}

func fromGateIOBookLevels(native []BookLevel) []unified.BookLevel {
	levels := make([]unified.BookLevel, len(native))
	for index, level := range native {
		levels[index] = unified.BookLevel{Price: level.Price, Quantity: level.Amount}
	}
	return levels
}

func gateIOTradeMilliseconds(milliseconds, seconds string) (int64, error) {
	if milliseconds != "" {
		return gateIODecimalTimestamp(milliseconds, 1, "millisecond")
	}
	return gateIODecimalTimestamp(seconds, 1000, "second")
}

func gateIODecimalTimestamp(value string, multiplier int64, unit string) (int64, error) {
	if !positiveDecimal.MatchString(value) {
		return 0, fmt.Errorf("invalid Gate.io %s timestamp %q", unit, value)
	}
	timestamp, ok := new(big.Rat).SetString(value)
	if !ok {
		return 0, fmt.Errorf("invalid Gate.io %s timestamp %q", unit, value)
	}
	timestamp.Mul(timestamp, big.NewRat(multiplier, 1))
	integer := new(big.Int).Quo(timestamp.Num(), timestamp.Denom())
	if !integer.IsInt64() {
		return 0, fmt.Errorf("Gate.io %s timestamp %q is outside int64", unit, value)
	}
	return integer.Int64(), nil
}

func fromGateIOSide(side Side) (unified.Side, error) {
	switch side {
	case SideBuy:
		return unified.SideBuy, nil
	case SideSell:
		return unified.SideSell, nil
	default:
		return "", fmt.Errorf("unsupported Gate.io side %q", side)
	}
}

func toGateIOSide(side unified.Side) Side {
	if side == unified.SideBuy {
		return SideBuy
	}
	return SideSell
}

func toGateIOCandleInterval(value unified.CandleInterval) CandleInterval {
	switch value {
	case unified.Candle1Minute, unified.Candle3Minutes:
		return Candle1Minute
	case unified.Candle5Minutes:
		return Candle5Minutes
	case unified.Candle15Minutes:
		return Candle15Minutes
	case unified.Candle30Minutes:
		return Candle30Minutes
	case unified.Candle1Hour:
		return Candle1Hour
	case unified.Candle4Hours:
		return Candle4Hours
	default:
		return ""
	}
}

func toGateIOTimeInForce(value unified.TimeInForce) TimeInForce {
	switch value {
	case unified.TimeInForceGTC:
		return TimeInForceGTC
	case unified.TimeInForceIOC:
		return TimeInForceIOC
	case unified.TimeInForceFOK:
		return TimeInForceFOK
	case unified.TimeInForcePostOnly:
		return TimeInForcePOC
	default:
		return ""
	}
}

func fromGateIOPlacedOrder(
	native Order,
	request unified.PlaceOrderRequest,
	clientOrderID string,
) (unified.Order, error) {
	nativeMarket := gateIOSymbol(request.Market)
	if native.CurrencyPair != "" && native.CurrencyPair != nativeMarket {
		return unified.Order{}, fmt.Errorf(
			"Gate.io order market %q does not match %q", native.CurrencyPair, nativeMarket,
		)
	}
	if native.ID == "" {
		return unified.Order{}, fmt.Errorf("Gate.io order acknowledgement does not contain an order ID")
	}
	if native.ClientOrderID != "" && native.ClientOrderID != clientOrderID {
		return unified.Order{}, fmt.Errorf(
			"Gate.io order client ID %q does not match %q", native.ClientOrderID, clientOrderID,
		)
	}
	status := unified.OrderStatusAcknowledged
	if native.Status != "" {
		var err error
		status, err = toUnifiedGateIOOrderStatus(native)
		if err != nil {
			return unified.Order{}, err
		}
	}
	executed := native.FilledAmount
	return unified.Order{
		Exchange: model.ExchangeGateIO, ID: native.ID, ClientOrderID: clientOrderID,
		Market: request.Market, NativeMarket: nativeMarket, Side: request.Side,
		Type: request.Type, Status: status, Price: request.Price,
		Quantity: request.Quantity, QuoteAmount: request.QuoteAmount,
		ExecutedQuantity: executed, Raw: native.Raw,
	}, nil
}

func fromGateIOOrder(native Order, expectedMarket unified.Market) (unified.Order, error) {
	nativeMarket := gateIOSymbol(expectedMarket)
	if native.CurrencyPair != nativeMarket {
		return unified.Order{}, fmt.Errorf(
			"Gate.io order market %q does not match %q", native.CurrencyPair, nativeMarket,
		)
	}
	side, err := fromGateIOSide(native.Side)
	if err != nil {
		return unified.Order{}, err
	}
	orderType, err := fromGateIOOrderType(native.Type)
	if err != nil {
		return unified.Order{}, err
	}
	status, err := toUnifiedGateIOOrderStatus(native)
	if err != nil {
		return unified.Order{}, err
	}
	quantity, quoteAmount := native.Amount, ""
	if orderType == unified.OrderTypeMarket && side == unified.SideBuy {
		quantity, quoteAmount = "", native.Amount
	}
	return unified.Order{
		Exchange: model.ExchangeGateIO, ID: native.ID, ClientOrderID: native.ClientOrderID,
		Market: expectedMarket, NativeMarket: native.CurrencyPair, Side: side,
		Type: orderType, Status: status, Price: native.Price,
		Quantity: quantity, QuoteAmount: quoteAmount,
		ExecutedQuantity: native.FilledAmount, Raw: native.Raw,
	}, nil
}

func fromGateIOOrderType(orderType OrderType) (unified.OrderType, error) {
	switch orderType {
	case OrderTypeLimit:
		return unified.OrderTypeLimit, nil
	case OrderTypeMarket:
		return unified.OrderTypeMarket, nil
	default:
		return "", fmt.Errorf("unsupported Gate.io order type %q", orderType)
	}
}

func toUnifiedGateIOOrderStatus(native Order) (unified.OrderStatus, error) {
	switch native.Status {
	case "open":
		filled, err := gateIODecimalPositive(native.FilledAmount)
		if err != nil {
			return unified.OrderStatusUnknown, fmt.Errorf("decode Gate.io filled amount: %w", err)
		}
		if filled {
			return unified.OrderStatusPartiallyFilled, nil
		}
		return unified.OrderStatusNew, nil
	case "closed":
		return unified.OrderStatusFilled, nil
	case "cancelled":
		return unified.OrderStatusCanceled, nil
	default:
		return unified.OrderStatusUnknown, nil
	}
}

func gateIODecimalPositive(value string) (bool, error) {
	if value == "" {
		return false, nil
	}
	if !positiveDecimal.MatchString(value) {
		return false, fmt.Errorf("invalid decimal %q", value)
	}
	return strings.Trim(value, "0.") != "", nil
}

func gateIOOrderIdentity(request unified.OrderRequest) string {
	if request.OrderID != "" {
		return request.OrderID
	}
	return request.ClientOrderID
}

func gateIOOpenOrdersComplete(target string, expected, received map[string]int) bool {
	if target != "" {
		total, exists := expected[target]
		return !exists || received[target] >= total
	}
	for market, total := range expected {
		if received[market] < total {
			return false
		}
	}
	return true
}

func newGateIOClientOrderID() (string, error) {
	randomBytes := make([]byte, 10)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", fmt.Errorf("generate Gate.io client order ID: %w", err)
	}
	return "t-proven-" + hex.EncodeToString(randomBytes), nil
}
