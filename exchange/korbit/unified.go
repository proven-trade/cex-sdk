package korbit

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/model"
	"github.com/proven-trade/cex-sdk/unified"
)

const (
	korbitUnifiedDefaultLimit = 200
	korbitCandlePageLimit     = 200
)

// UnifiedSpot은 Korbit native 클라이언트를 공통 Spot 계약으로 변환한다.
type UnifiedSpot struct {
	client *Client
}

var _ unified.SpotClient = (*UnifiedSpot)(nil)

// NewUnifiedSpot은 Korbit Spot 공통 어댑터를 생성한다.
func NewUnifiedSpot(client *Client) (*UnifiedSpot, error) {
	if client == nil {
		return nil, fmt.Errorf("Korbit client is required")
	}
	return &UnifiedSpot{client: client}, nil
}

// Exchange는 Korbit 거래소 식별자를 반환한다.
func (adapter *UnifiedSpot) Exchange() model.ExchangeID {
	return model.ExchangeKorbit
}

// Markets는 Korbit Spot 거래 마켓과 상태를 공통 형식으로 조회한다.
func (adapter *UnifiedSpot) Markets(
	ctx context.Context,
	options ...trade.RequestOption,
) ([]unified.MarketInfo, error) {
	native, err := adapter.client.CurrencyPairs(ctx, options...)
	if err != nil {
		return nil, err
	}
	markets := make([]unified.MarketInfo, len(native))
	for index, item := range native {
		market, parseErr := fromKorbitCurrencyPair(item)
		if parseErr != nil {
			return nil, parseErr
		}
		markets[index] = unified.MarketInfo{
			Exchange: model.ExchangeKorbit, Market: market,
			NativeMarket: item.Symbol, Status: item.Status, Raw: item.Raw,
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
	native, err := adapter.client.Tickers(ctx, TickersRequest{
		Symbols: []string{korbitSymbol(request.Market)},
	}, options...)
	if err != nil {
		return unified.Ticker{}, err
	}
	if len(native) != 1 {
		return unified.Ticker{}, fmt.Errorf("Korbit ticker response has %d items, want 1", len(native))
	}
	return unified.Ticker{
		Exchange: model.ExchangeKorbit, Market: request.Market,
		NativeMarket: native[0].Symbol, Price: native[0].Close, Raw: native[0].Raw,
	}, nil
}

// OrderBook은 Korbit 호가를 공통 형식으로 조회한다.
func (adapter *UnifiedSpot) OrderBook(
	ctx context.Context,
	request unified.OrderBookRequest,
	options ...trade.RequestOption,
) (unified.OrderBook, error) {
	if err := request.Validate(); err != nil {
		return unified.OrderBook{}, err
	}
	nativeMarket := korbitSymbol(request.Market)
	native, err := adapter.client.OrderBook(ctx, OrderBookRequest{Symbol: nativeMarket}, options...)
	if err != nil {
		return unified.OrderBook{}, err
	}
	limit := request.Limit
	if limit == 0 {
		limit = 16
	}
	bidDepth := limitedKorbitDepth(len(native.Bids), limit)
	askDepth := limitedKorbitDepth(len(native.Asks), limit)
	bids := make([]unified.BookLevel, bidDepth)
	asks := make([]unified.BookLevel, askDepth)
	for index, level := range native.Bids[:bidDepth] {
		bids[index] = unified.BookLevel{Price: level.Price, Quantity: level.Qty}
	}
	for index, level := range native.Asks[:askDepth] {
		asks[index] = unified.BookLevel{Price: level.Price, Quantity: level.Qty}
	}
	return unified.OrderBook{
		Exchange: model.ExchangeKorbit, Market: request.Market, NativeMarket: nativeMarket,
		Bids: bids, Asks: asks, Timestamp: native.Timestamp, Raw: native.Raw,
	}, nil
}

// RecentTrades는 Korbit 공개 최근 체결을 공통 형식으로 조회한다.
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
		limit = 100
	}
	native, err := adapter.client.RecentTrades(ctx, RecentTradesRequest{
		Symbol: korbitSymbol(request.Market), Limit: limit,
	}, options...)
	if err != nil {
		return nil, err
	}
	trades := make([]unified.PublicTrade, len(native))
	for index, item := range native {
		side := unified.SideSell
		if item.IsBuyerTaker {
			side = unified.SideBuy
		}
		trades[index] = unified.PublicTrade{
			ID: strconv.FormatInt(item.TradeID, 10), Price: item.Price,
			Quantity: item.Qty, Side: side, Timestamp: item.Timestamp,
		}
	}
	return trades, nil
}

// Candles는 Korbit Spot OHLCV를 조회하고 3분봉은 1분봉으로 합성한다.
func (adapter *UnifiedSpot) Candles(
	ctx context.Context,
	request unified.CandlesRequest,
	options ...trade.RequestOption,
) ([]unified.Candle, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	limit := request.Limit
	if limit == 0 {
		limit = korbitUnifiedDefaultLimit
	}
	if request.Interval == unified.Candle3Minutes {
		return adapter.korbitThreeMinuteCandles(ctx, request.Market, limit, options...)
	}
	native, err := adapter.client.Candles(ctx, CandlesRequest{
		Symbol: korbitSymbol(request.Market), Interval: toKorbitCandleInterval(request.Interval), Limit: limit,
	}, options...)
	if err != nil {
		return nil, err
	}
	return fromKorbitCandles(native), nil
}

// Balances는 Korbit 계정 잔고를 공통 형식으로 조회한다.
func (adapter *UnifiedSpot) Balances(
	ctx context.Context,
	options ...trade.RequestOption,
) ([]unified.Balance, error) {
	native, err := adapter.client.Balances(ctx, BalanceRequest{}, options...)
	if err != nil {
		return nil, err
	}
	balances := make([]unified.Balance, len(native))
	for index, balance := range native {
		tradeInUse := balance.TradeInUse
		if tradeInUse == "" {
			tradeInUse = "0"
		}
		withdrawalInUse := balance.WithdrawalInUse
		if withdrawalInUse == "" {
			withdrawalInUse = "0"
		}
		locked, addErr := unified.AddDecimals(tradeInUse, withdrawalInUse)
		if addErr != nil {
			return nil, fmt.Errorf("calculate Korbit locked balance for %q: %w", balance.Currency, addErr)
		}
		balances[index] = unified.Balance{
			Asset: strings.ToUpper(balance.Currency), Available: balance.Available,
			Locked: locked, Raw: balance.Raw,
		}
	}
	return balances, nil
}

// PlaceOrder는 공통 Spot 주문을 Korbit 주문으로 변환해 생성한다.
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
		generated, err := newKorbitClientOrderID()
		if err != nil {
			return unified.Order{}, err
		}
		clientOrderID = generated
	}
	nativeRequest := PlaceOrderRequest{
		Symbol: korbitSymbol(request.Market), Side: toKorbitSide(request.Side),
		OrderType: OrderTypeMarket, ClientOrderID: clientOrderID,
	}
	if request.Type == unified.OrderTypeLimit {
		nativeRequest.OrderType = OrderTypeLimit
		nativeRequest.Price = request.Price
		nativeRequest.Qty = request.Quantity
		nativeRequest.TimeInForce = toKorbitTimeInForce(request.TimeInForce)
	} else if request.Side == unified.SideBuy {
		nativeRequest.Amount = request.QuoteAmount
	} else {
		nativeRequest.Qty = request.Quantity
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
		Exchange: model.ExchangeKorbit, ID: strconv.FormatInt(reference.OrderID, 10),
		ClientOrderID: clientOrderID, Market: request.Market,
		NativeMarket: nativeRequest.Symbol, Side: request.Side, Type: request.Type,
		Status: unified.OrderStatusNew, Price: request.Price, Quantity: quantity, Raw: reference.Raw,
	}, nil
}

// Order는 Korbit 주문을 거래소 주문 ID 또는 사용자 주문 ID로 조회한다.
func (adapter *UnifiedSpot) Order(
	ctx context.Context,
	request unified.OrderRequest,
	options ...trade.RequestOption,
) (unified.Order, error) {
	if err := request.Validate(); err != nil {
		return unified.Order{}, err
	}
	orderID, err := korbitOrderID(request.OrderID)
	if err != nil {
		return unified.Order{}, err
	}
	native, err := adapter.client.OrderInfo(ctx, OrderInfoRequest{
		Symbol: korbitSymbol(request.Market), OrderID: orderID, ClientOrderID: request.ClientOrderID,
	}, options...)
	if err != nil {
		return unified.Order{}, err
	}
	return fromKorbitOrder(native, request.Market), nil
}

// CancelOrder는 Korbit 주문 취소를 접수한다.
func (adapter *UnifiedSpot) CancelOrder(
	ctx context.Context,
	request unified.OrderRequest,
	options ...trade.RequestOption,
) (unified.Order, error) {
	if err := request.Validate(); err != nil {
		return unified.Order{}, err
	}
	orderID, err := korbitOrderID(request.OrderID)
	if err != nil {
		return unified.Order{}, err
	}
	native, err := adapter.client.CancelOrder(ctx, CancelOrderRequest{
		Symbol: korbitSymbol(request.Market), OrderID: orderID, ClientOrderID: request.ClientOrderID,
	}, options...)
	if err != nil {
		return unified.Order{}, err
	}
	return unified.Order{
		Exchange: model.ExchangeKorbit, ID: request.OrderID,
		ClientOrderID: request.ClientOrderID, Market: request.Market,
		NativeMarket: korbitSymbol(request.Market), Status: unified.OrderStatusCanceled, Raw: native.Raw,
	}, nil
}

// OpenOrders는 Korbit 미체결 주문을 단일 또는 전체 마켓에서 조회한다.
func (adapter *UnifiedSpot) OpenOrders(
	ctx context.Context,
	request unified.OpenOrdersRequest,
	options ...trade.RequestOption,
) ([]unified.Order, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	var markets []unified.Market
	if request.Market != nil {
		markets = []unified.Market{*request.Market}
	} else {
		pairs, err := adapter.client.CurrencyPairs(ctx, options...)
		if err != nil {
			return nil, err
		}
		markets = make([]unified.Market, len(pairs))
		for index, pair := range pairs {
			market, parseErr := fromKorbitCurrencyPair(pair)
			if parseErr != nil {
				return nil, parseErr
			}
			markets[index] = market
		}
	}
	var orders []unified.Order
	for _, market := range markets {
		native, err := adapter.client.OpenOrders(ctx, OpenOrdersRequest{
			Symbol: korbitSymbol(market), Limit: 1000,
		}, options...)
		if err != nil {
			return nil, err
		}
		for _, order := range native {
			orderMarket, parseErr := fromKorbitSymbol(order.Symbol)
			if parseErr != nil {
				return nil, parseErr
			}
			orders = append(orders, fromKorbitOrder(order, orderMarket))
		}
	}
	return orders, nil
}

func (adapter *UnifiedSpot) korbitThreeMinuteCandles(
	ctx context.Context,
	market unified.Market,
	limit int,
	options ...trade.RequestOption,
) ([]unified.Candle, error) {
	end := adapter.client.now().UTC()
	start := end.Add(-3 * time.Minute * time.Duration(limit))
	cursor := start
	var source []unified.Candle
	for cursor.Before(end) {
		pageEnd := cursor.Add(korbitCandlePageLimit * time.Minute)
		if pageEnd.After(end) {
			pageEnd = end
		}
		pageLimit := int((pageEnd.Sub(cursor) + time.Minute - 1) / time.Minute)
		pageStart := cursor
		native, err := adapter.client.Candles(ctx, CandlesRequest{
			Symbol: korbitSymbol(market), Interval: Candle1Minute,
			Start: &pageStart, End: &pageEnd, Limit: pageLimit,
		}, options...)
		if err != nil {
			return nil, err
		}
		source = append(source, fromKorbitCandles(native)...)
		cursor = pageEnd
	}
	return unified.AggregateCandles(source, 3*time.Minute, limit)
}

func korbitSymbol(market unified.Market) string {
	return strings.ToLower(market.Base) + "_" + strings.ToLower(market.Quote)
}

func fromKorbitCurrencyPair(pair CurrencyPair) (unified.Market, error) {
	market := unified.Market{
		Base: strings.ToUpper(pair.BaseCurrency), Quote: strings.ToUpper(pair.QuoteCurrency),
	}
	if err := market.Validate(); err == nil {
		return market, nil
	}
	return fromKorbitSymbol(pair.Symbol)
}

func fromKorbitSymbol(symbol string) (unified.Market, error) {
	base, quote, found := strings.Cut(symbol, "_")
	market := unified.Market{Base: strings.ToUpper(base), Quote: strings.ToUpper(quote)}
	if !found || market.Validate() != nil {
		return unified.Market{}, fmt.Errorf("invalid Korbit symbol %q", symbol)
	}
	return market, nil
}

func limitedKorbitDepth(length, limit int) int {
	if limit < length {
		return limit
	}
	return length
}

func toKorbitCandleInterval(value unified.CandleInterval) CandleInterval {
	switch value {
	case unified.Candle1Minute:
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

func fromKorbitCandles(native []Candle) []unified.Candle {
	candles := make([]unified.Candle, len(native))
	for index, item := range native {
		candles[index] = unified.Candle{
			StartTime: item.Timestamp, Open: item.Open, High: item.High,
			Low: item.Low, Close: item.Close, Volume: item.Volume,
		}
	}
	return candles
}

func toKorbitSide(side unified.Side) Side {
	if side == unified.SideBuy {
		return SideBuy
	}
	return SideSell
}

func toKorbitTimeInForce(value unified.TimeInForce) TimeInForce {
	switch value {
	case unified.TimeInForceGTC:
		return TimeInForceGTC
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

func fromKorbitOrder(native Order, market unified.Market) unified.Order {
	quantity := native.Qty
	if native.OrderType != OrderTypeLimit && native.Side == SideBuy {
		quantity = native.Amount
	}
	return unified.Order{
		Exchange: model.ExchangeKorbit, ID: strconv.FormatInt(native.OrderID, 10),
		ClientOrderID: native.ClientOrderID, Market: market, NativeMarket: native.Symbol,
		Side: toUnifiedKorbitSide(native.Side), Type: toUnifiedKorbitOrderType(native.OrderType),
		Status: toUnifiedKorbitStatus(native.Status), Price: native.Price,
		Quantity: quantity, ExecutedQuantity: native.FilledQty, Raw: native.Raw,
	}
}

func toUnifiedKorbitSide(side Side) unified.Side {
	if side == SideBuy {
		return unified.SideBuy
	}
	return unified.SideSell
}

func toUnifiedKorbitOrderType(orderType OrderType) unified.OrderType {
	if orderType == OrderTypeLimit {
		return unified.OrderTypeLimit
	}
	return unified.OrderTypeMarket
}

func toUnifiedKorbitStatus(status OrderStatus) unified.OrderStatus {
	switch status {
	case OrderStatusPending, OrderStatusOpen:
		return unified.OrderStatusNew
	case OrderStatusPartiallyFilled:
		return unified.OrderStatusPartiallyFilled
	case OrderStatusFilled:
		return unified.OrderStatusFilled
	case OrderStatusCanceled, OrderStatusPartiallyFilledCanceled:
		return unified.OrderStatusCanceled
	case OrderStatusExpired:
		return unified.OrderStatusExpired
	default:
		return unified.OrderStatusUnknown
	}
}

func korbitOrderID(value string) (int64, error) {
	if value == "" {
		return 0, nil
	}
	orderID, err := strconv.ParseInt(value, 10, 64)
	if err != nil || orderID <= 0 {
		return 0, validationError("Korbit order ID must be a positive integer")
	}
	return orderID, nil
}

func newKorbitClientOrderID() (string, error) {
	randomBytes := make([]byte, 14)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", fmt.Errorf("generate Korbit client order ID: %w", err)
	}
	return "proven-" + hex.EncodeToString(randomBytes), nil
}
