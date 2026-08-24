package coinbase

import (
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"time"

	trade "github.com/proven-trade/proven-trade-sdk"
	"github.com/proven-trade/proven-trade-sdk/model"
	"github.com/proven-trade/proven-trade-sdk/unified"
)

const (
	coinbaseUnifiedDefaultLimit = 200
	coinbaseCandlePageLimit     = 350
)

// UnifiedSpot은 Coinbase Advanced Trade native 클라이언트를 공통 Spot 계약으로 변환한다.
type UnifiedSpot struct {
	client *Client
}

var _ unified.SpotClient = (*UnifiedSpot)(nil)

// NewUnifiedSpot은 Coinbase Advanced Trade Spot 공통 어댑터를 생성한다.
func NewUnifiedSpot(client *Client) (*UnifiedSpot, error) {
	if client == nil {
		return nil, fmt.Errorf("Coinbase client is required")
	}
	return &UnifiedSpot{client: client}, nil
}

// Exchange는 Coinbase 거래소 식별자를 반환한다.
func (adapter *UnifiedSpot) Exchange() model.ExchangeID {
	return model.ExchangeCoinbase
}

// Markets는 Coinbase Spot 상품 전체를 공통 형식으로 조회한다.
func (adapter *UnifiedSpot) Markets(
	ctx context.Context,
	options ...trade.RequestOption,
) ([]unified.MarketInfo, error) {
	const pageLimit = 1000
	request := ProductsRequest{Limit: pageLimit}
	var markets []unified.MarketInfo
	seenProducts := make(map[string]struct{})
	for {
		products, err := adapter.client.Products(ctx, request, options...)
		if err != nil {
			return nil, err
		}
		for _, product := range products {
			if _, exists := seenProducts[product.ProductID]; exists {
				return nil, fmt.Errorf("Coinbase products returned duplicate %q", product.ProductID)
			}
			seenProducts[product.ProductID] = struct{}{}
			market, mappingErr := fromCoinbaseProduct(product)
			if mappingErr != nil {
				return nil, mappingErr
			}
			markets = append(markets, unified.MarketInfo{
				Exchange: model.ExchangeCoinbase, Market: market,
				NativeMarket: product.ProductID, Status: product.Status, Raw: product.Raw,
			})
		}
		if len(products) < pageLimit {
			return markets, nil
		}
		request.Offset += len(products)
	}
}

// Ticker는 공통 마켓의 Coinbase 최신 가격을 조회한다.
func (adapter *UnifiedSpot) Ticker(
	ctx context.Context,
	request unified.TickerRequest,
	options ...trade.RequestOption,
) (unified.Ticker, error) {
	if err := request.Validate(); err != nil {
		return unified.Ticker{}, err
	}
	native, err := adapter.client.Product(ctx, coinbaseProductID(request.Market), options...)
	if err != nil {
		return unified.Ticker{}, err
	}
	return unified.Ticker{
		Exchange: model.ExchangeCoinbase, Market: request.Market,
		NativeMarket: native.ProductID, Price: native.Price, Raw: native.Raw,
	}, nil
}

// OrderBook은 Coinbase Spot 호가 스냅샷을 공통 형식으로 조회한다.
func (adapter *UnifiedSpot) OrderBook(
	ctx context.Context,
	request unified.OrderBookRequest,
	options ...trade.RequestOption,
) (unified.OrderBook, error) {
	if err := request.Validate(); err != nil {
		return unified.OrderBook{}, err
	}
	native, err := adapter.client.OrderBook(ctx, OrderBookRequest{
		ProductID: coinbaseProductID(request.Market), Limit: request.Limit,
	}, options...)
	if err != nil {
		return unified.OrderBook{}, err
	}
	timestamp, err := parseCoinbaseTime("order book", native.PriceBook.Time)
	if err != nil {
		return unified.OrderBook{}, err
	}
	return unified.OrderBook{
		Exchange: model.ExchangeCoinbase, Market: request.Market,
		NativeMarket: native.PriceBook.ProductID,
		Bids:         fromCoinbaseBookLevels(native.PriceBook.Bids),
		Asks:         fromCoinbaseBookLevels(native.PriceBook.Asks),
		Timestamp:    timestamp, Raw: native.Raw,
	}, nil
}

// RecentTrades는 Coinbase Spot 공개 최근 체결을 공통 형식으로 조회한다.
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
	native, err := adapter.client.MarketTrades(ctx, MarketTradesRequest{
		ProductID: coinbaseProductID(request.Market), Limit: limit,
	}, options...)
	if err != nil {
		return nil, err
	}
	trades := make([]unified.PublicTrade, len(native.Trades))
	for index, item := range native.Trades {
		timestamp, parseErr := parseCoinbaseTime("trade", item.Time)
		if parseErr != nil {
			return nil, parseErr
		}
		trades[index] = unified.PublicTrade{
			ID: item.TradeID, Price: item.Price, Quantity: item.Size,
			Side: toUnifiedCoinbaseSide(item.Side), Timestamp: timestamp,
		}
	}
	return trades, nil
}

// Candles는 Coinbase Spot OHLCV를 조회하고 3분봉은 1분봉으로 합성한다.
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
		limit = coinbaseUnifiedDefaultLimit
	}
	if request.Interval == unified.Candle3Minutes {
		return adapter.threeMinuteCandles(ctx, request.Market, limit, options...)
	}
	duration := coinbaseCandleDuration(request.Interval)
	end := adapter.client.now().UTC()
	native, err := adapter.client.Candles(ctx, CandlesRequest{
		ProductID: coinbaseProductID(request.Market), Start: end.Add(-duration * time.Duration(limit)),
		End: end, Granularity: toCoinbaseCandleGranularity(request.Interval), Limit: limit,
	}, options...)
	if err != nil {
		return nil, err
	}
	return fromCoinbaseCandles(native)
}

// Balances는 Coinbase 계정 페이지를 끝까지 순회해 공통 잔고로 변환한다.
func (adapter *UnifiedSpot) Balances(
	ctx context.Context,
	options ...trade.RequestOption,
) ([]unified.Balance, error) {
	request := AccountsRequest{Limit: 250}
	var balances []unified.Balance
	seenCursors := make(map[string]struct{})
	for {
		page, err := adapter.client.Accounts(ctx, request, options...)
		if err != nil {
			return nil, err
		}
		for _, account := range page.Accounts {
			balances = append(balances, unified.Balance{
				Asset: account.Currency, Available: account.AvailableBalance.Value,
				Locked: account.Hold.Value, Raw: account.Raw,
			})
		}
		if !page.HasNext {
			return balances, nil
		}
		if page.Cursor == "" {
			return nil, fmt.Errorf("Coinbase accounts cursor is empty while has_next is true")
		}
		if _, exists := seenCursors[page.Cursor]; exists {
			return nil, fmt.Errorf("Coinbase accounts returned a repeated cursor")
		}
		seenCursors[page.Cursor] = struct{}{}
		request.Cursor = page.Cursor
	}
}

// PlaceOrder는 공통 Spot 주문을 Coinbase 주문 설정으로 변환해 생성한다.
func (adapter *UnifiedSpot) PlaceOrder(
	ctx context.Context,
	request unified.PlaceOrderRequest,
	options ...trade.RequestOption,
) (unified.Order, error) {
	if err := request.Validate(); err != nil {
		return unified.Order{}, err
	}
	clientOrderID, err := adapter.coinbaseClientOrderID(request.ClientOrderID)
	if err != nil {
		return unified.Order{}, err
	}
	nativeRequest := PlaceOrderRequest{
		ClientOrderID: clientOrderID, ProductID: coinbaseProductID(request.Market),
		Side: toCoinbaseSide(request.Side),
	}
	if request.Type == unified.OrderTypeMarket {
		configuration := &MarketIOCConfiguration{}
		if request.Side == unified.SideBuy {
			configuration.QuoteSize = request.QuoteAmount
		} else {
			configuration.BaseSize = request.Quantity
		}
		nativeRequest.OrderConfiguration.MarketMarketIOC = configuration
	} else {
		switch request.TimeInForce {
		case unified.TimeInForceIOC:
			nativeRequest.OrderConfiguration.SORLimitIOC = &SORLimitIOCConfiguration{
				BaseSize: request.Quantity, LimitPrice: request.Price,
			}
		case unified.TimeInForceFOK:
			nativeRequest.OrderConfiguration.LimitLimitFOK = &LimitFOKConfiguration{
				BaseSize: request.Quantity, LimitPrice: request.Price,
			}
		default:
			nativeRequest.OrderConfiguration.LimitLimitGTC = &LimitGTCConfiguration{
				BaseSize: request.Quantity, LimitPrice: request.Price,
				PostOnly: request.TimeInForce == unified.TimeInForcePostOnly,
			}
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
		Exchange: model.ExchangeCoinbase, ID: reference.OrderID,
		ClientOrderID: reference.ClientOrderID, Market: request.Market,
		NativeMarket: reference.ProductID, Side: request.Side, Type: request.Type,
		Status: unified.OrderStatusNew, Price: request.Price, Quantity: quantity, Raw: reference.Raw,
	}, nil
}

// Order는 Coinbase Spot 주문을 거래소 주문 ID 또는 사용자 주문 ID로 조회한다.
func (adapter *UnifiedSpot) Order(
	ctx context.Context,
	request unified.OrderRequest,
	options ...trade.RequestOption,
) (unified.Order, error) {
	if err := request.Validate(); err != nil {
		return unified.Order{}, err
	}
	native, err := adapter.resolveCoinbaseOrder(ctx, request, options...)
	if err != nil {
		return unified.Order{}, err
	}
	return fromCoinbaseOrder(native, request.Market), nil
}

// CancelOrder는 Coinbase Spot 주문 취소를 접수한다.
func (adapter *UnifiedSpot) CancelOrder(
	ctx context.Context,
	request unified.OrderRequest,
	options ...trade.RequestOption,
) (unified.Order, error) {
	if err := request.Validate(); err != nil {
		return unified.Order{}, err
	}
	orderID := request.OrderID
	clientOrderID := request.ClientOrderID
	if orderID == "" {
		native, err := adapter.resolveCoinbaseOrder(ctx, request, options...)
		if err != nil {
			return unified.Order{}, err
		}
		orderID = native.OrderID
		clientOrderID = native.ClientOrderID
	}
	results, err := adapter.client.CancelOrders(ctx, CancelOrdersRequest{OrderIDs: []string{orderID}}, options...)
	if err != nil {
		return unified.Order{}, err
	}
	if len(results) != 1 || results[0].OrderID != orderID {
		return unified.Order{}, fmt.Errorf("Coinbase cancel response does not match order %q", orderID)
	}
	if !results[0].Success {
		return unified.Order{}, fmt.Errorf(
			"Coinbase cancel order %q failed: %s", orderID, results[0].FailureReason,
		)
	}
	return unified.Order{
		Exchange: model.ExchangeCoinbase, ID: orderID, ClientOrderID: clientOrderID,
		Market: request.Market, NativeMarket: coinbaseProductID(request.Market),
		Status: unified.OrderStatusCanceled, Raw: results[0].Raw,
	}, nil
}

// OpenOrders는 Coinbase Spot 활성 주문을 단일 또는 전체 마켓에서 끝까지 조회한다.
func (adapter *UnifiedSpot) OpenOrders(
	ctx context.Context,
	request unified.OpenOrdersRequest,
	options ...trade.RequestOption,
) ([]unified.Order, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	nativeRequest := OrdersRequest{
		OrderStatuses: []string{"PENDING", "OPEN", "QUEUED", "CANCEL_QUEUED", "EDIT_QUEUED"},
		Limit:         1000,
	}
	requestedMarket := unified.Market{}
	if request.Market != nil {
		requestedMarket = *request.Market
		nativeRequest.ProductIDs = []string{coinbaseProductID(requestedMarket)}
	}
	var orders []unified.Order
	seenCursors := make(map[string]struct{})
	for {
		page, err := adapter.client.Orders(ctx, nativeRequest, options...)
		if err != nil {
			return nil, err
		}
		for _, native := range page.Orders {
			market := requestedMarket
			if request.AllMarkets {
				market, err = fromCoinbaseProductID(native.ProductID)
				if err != nil {
					return nil, err
				}
			}
			orders = append(orders, fromCoinbaseOrder(native, market))
		}
		if !page.HasNext {
			return orders, nil
		}
		if err := advanceCoinbaseOrderCursor(&nativeRequest, page.Cursor, seenCursors); err != nil {
			return nil, err
		}
	}
}

func (adapter *UnifiedSpot) resolveCoinbaseOrder(
	ctx context.Context,
	request unified.OrderRequest,
	options ...trade.RequestOption,
) (Order, error) {
	if request.OrderID != "" {
		return adapter.client.OrderInfo(ctx, request.OrderID, options...)
	}
	nativeRequest := OrdersRequest{
		ProductIDs: []string{coinbaseProductID(request.Market)}, Limit: 1000,
	}
	seenCursors := make(map[string]struct{})
	for {
		page, err := adapter.client.Orders(ctx, nativeRequest, options...)
		if err != nil {
			return Order{}, err
		}
		for _, native := range page.Orders {
			if native.ClientOrderID == request.ClientOrderID {
				return native, nil
			}
		}
		if !page.HasNext {
			return Order{}, fmt.Errorf("Coinbase order with client ID %q was not found", request.ClientOrderID)
		}
		if err := advanceCoinbaseOrderCursor(&nativeRequest, page.Cursor, seenCursors); err != nil {
			return Order{}, err
		}
	}
}

func advanceCoinbaseOrderCursor(
	request *OrdersRequest,
	cursor string,
	seen map[string]struct{},
) error {
	if cursor == "" {
		return fmt.Errorf("Coinbase orders cursor is empty while has_next is true")
	}
	if _, exists := seen[cursor]; exists {
		return fmt.Errorf("Coinbase orders returned a repeated cursor")
	}
	seen[cursor] = struct{}{}
	request.Cursor = cursor
	return nil
}

func (adapter *UnifiedSpot) coinbaseClientOrderID(value string) (string, error) {
	if value != "" {
		return value, nil
	}
	randomBytes := make([]byte, 16)
	adapter.client.randomMu.Lock()
	_, err := io.ReadFull(adapter.client.random, randomBytes)
	adapter.client.randomMu.Unlock()
	if err != nil {
		return "", fmt.Errorf("generate Coinbase client order ID: %w", err)
	}
	return "proven-" + hex.EncodeToString(randomBytes), nil
}

func (adapter *UnifiedSpot) threeMinuteCandles(
	ctx context.Context,
	market unified.Market,
	limit int,
	options ...trade.RequestOption,
) ([]unified.Candle, error) {
	end := adapter.client.now().UTC()
	start := end.Add(-3 * time.Minute * time.Duration(limit))
	cursor := start
	var native []Candle
	for cursor.Before(end) {
		pageEnd := cursor.Add(coinbaseCandlePageLimit * time.Minute)
		if pageEnd.After(end) {
			pageEnd = end
		}
		pageLimit := int((pageEnd.Sub(cursor) + time.Minute - 1) / time.Minute)
		page, err := adapter.client.Candles(ctx, CandlesRequest{
			ProductID: coinbaseProductID(market), Start: cursor, End: pageEnd,
			Granularity: Candle1Minute, Limit: pageLimit,
		}, options...)
		if err != nil {
			return nil, err
		}
		native = append(native, page...)
		cursor = pageEnd
	}
	return aggregateCoinbaseThreeMinuteCandles(native, limit)
}

func aggregateCoinbaseThreeMinuteCandles(native []Candle, limit int) ([]unified.Candle, error) {
	byStart := make(map[int64]Candle, len(native))
	starts := make([]int64, 0, len(native))
	for _, item := range native {
		start, err := strconv.ParseInt(item.Start, 10, 64)
		if err != nil || start < 0 {
			if err == nil {
				err = fmt.Errorf("timestamp must not be negative")
			}
			return nil, fmt.Errorf("decode Coinbase candle timestamp: %w", err)
		}
		if _, exists := byStart[start]; exists {
			continue
		}
		byStart[start] = item
		starts = append(starts, start)
	}
	sort.Slice(starts, func(left, right int) bool { return starts[left] < starts[right] })
	type bucketCandle struct {
		candle unified.Candle
		volume scaledCoinbaseDecimal
	}
	buckets := make(map[int64]bucketCandle)
	for _, start := range starts {
		item := byStart[start]
		bucketStart := start - start%180
		volume, err := parseScaledCoinbaseDecimal(item.Volume)
		if err != nil {
			return nil, fmt.Errorf("decode Coinbase candle volume: %w", err)
		}
		current, exists := buckets[bucketStart]
		if !exists {
			if _, err := compareCoinbaseDecimals(item.High, item.Low); err != nil {
				return nil, fmt.Errorf("decode Coinbase candle price: %w", err)
			}
			current = bucketCandle{
				candle: unified.Candle{
					StartTime: bucketStart * 1000, Open: item.Open, High: item.High,
					Low: item.Low, Close: item.Close,
				},
				volume: volume,
			}
		} else {
			comparison, compareErr := compareCoinbaseDecimals(item.High, current.candle.High)
			if compareErr != nil {
				return nil, fmt.Errorf("decode Coinbase candle high: %w", compareErr)
			}
			if comparison > 0 {
				current.candle.High = item.High
			}
			comparison, compareErr = compareCoinbaseDecimals(item.Low, current.candle.Low)
			if compareErr != nil {
				return nil, fmt.Errorf("decode Coinbase candle low: %w", compareErr)
			}
			if comparison < 0 {
				current.candle.Low = item.Low
			}
			current.candle.Close = item.Close
			current.volume = addScaledCoinbaseDecimals(current.volume, volume)
		}
		buckets[bucketStart] = current
	}
	bucketStarts := make([]int64, 0, len(buckets))
	for start := range buckets {
		bucketStarts = append(bucketStarts, start)
	}
	sort.Slice(bucketStarts, func(left, right int) bool { return bucketStarts[left] > bucketStarts[right] })
	if len(bucketStarts) > limit {
		bucketStarts = bucketStarts[:limit]
	}
	result := make([]unified.Candle, len(bucketStarts))
	for index, start := range bucketStarts {
		bucket := buckets[start]
		bucket.candle.Volume = bucket.volume.String()
		result[index] = bucket.candle
	}
	return result, nil
}

type scaledCoinbaseDecimal struct {
	integer *big.Int
	scale   int
}

func parseScaledCoinbaseDecimal(value string) (scaledCoinbaseDecimal, error) {
	if !positiveDecimalPattern.MatchString(value) {
		return scaledCoinbaseDecimal{}, fmt.Errorf("invalid decimal %q", value)
	}
	whole, fraction, _ := strings.Cut(value, ".")
	integer, ok := new(big.Int).SetString(whole+fraction, 10)
	if !ok {
		return scaledCoinbaseDecimal{}, fmt.Errorf("invalid decimal %q", value)
	}
	return scaledCoinbaseDecimal{integer: integer, scale: len(fraction)}, nil
}

func compareCoinbaseDecimals(left, right string) (int, error) {
	leftValue, err := parseScaledCoinbaseDecimal(left)
	if err != nil {
		return 0, err
	}
	rightValue, err := parseScaledCoinbaseDecimal(right)
	if err != nil {
		return 0, err
	}
	scale := leftValue.scale
	if rightValue.scale > scale {
		scale = rightValue.scale
	}
	return scaleCoinbaseInteger(leftValue, scale).Cmp(scaleCoinbaseInteger(rightValue, scale)), nil
}

func addScaledCoinbaseDecimals(left, right scaledCoinbaseDecimal) scaledCoinbaseDecimal {
	scale := left.scale
	if right.scale > scale {
		scale = right.scale
	}
	return scaledCoinbaseDecimal{
		integer: new(big.Int).Add(
			scaleCoinbaseInteger(left, scale), scaleCoinbaseInteger(right, scale),
		),
		scale: scale,
	}
}

func scaleCoinbaseInteger(value scaledCoinbaseDecimal, scale int) *big.Int {
	result := new(big.Int).Set(value.integer)
	if difference := scale - value.scale; difference > 0 {
		result.Mul(result, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(difference)), nil))
	}
	return result
}

func (value scaledCoinbaseDecimal) String() string {
	digits := value.integer.String()
	if value.scale == 0 {
		return digits
	}
	if len(digits) <= value.scale {
		digits = strings.Repeat("0", value.scale-len(digits)+1) + digits
	}
	position := len(digits) - value.scale
	return digits[:position] + "." + digits[position:]
}

func coinbaseProductID(market unified.Market) string {
	return market.Base + "-" + market.Quote
}

func fromCoinbaseProduct(product Product) (unified.Market, error) {
	market := unified.Market{Base: product.BaseCurrencyID, Quote: product.QuoteCurrencyID}
	if err := market.Validate(); err == nil {
		return market, nil
	}
	return fromCoinbaseProductID(product.ProductID)
}

func fromCoinbaseProductID(value string) (unified.Market, error) {
	parts := strings.Split(value, "-")
	if len(parts) != 2 {
		return unified.Market{}, fmt.Errorf("invalid Coinbase Spot product ID %q", value)
	}
	market := unified.Market{Base: parts[0], Quote: parts[1]}
	if err := market.Validate(); err != nil {
		return unified.Market{}, fmt.Errorf("invalid Coinbase Spot product ID %q: %w", value, err)
	}
	return market, nil
}

func fromCoinbaseBookLevels(native []BookLevel) []unified.BookLevel {
	levels := make([]unified.BookLevel, len(native))
	for index, level := range native {
		levels[index] = unified.BookLevel{Price: level.Price, Quantity: level.Size}
	}
	return levels
}

func parseCoinbaseTime(kind, value string) (int64, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return 0, fmt.Errorf("decode Coinbase %s timestamp: %w", kind, err)
	}
	return parsed.UnixMilli(), nil
}

func fromCoinbaseCandles(native []Candle) ([]unified.Candle, error) {
	candles := make([]unified.Candle, len(native))
	for index, item := range native {
		start, err := strconv.ParseInt(item.Start, 10, 64)
		if err != nil || start < 0 || start > int64(^uint64(0)>>1)/1000 {
			if err == nil {
				err = fmt.Errorf("timestamp is outside the supported range")
			}
			return nil, fmt.Errorf("decode Coinbase candle timestamp: %w", err)
		}
		candles[index] = unified.Candle{
			StartTime: start * 1000, Open: item.Open, High: item.High,
			Low: item.Low, Close: item.Close, Volume: item.Volume,
		}
	}
	return candles, nil
}

func coinbaseCandleDuration(value unified.CandleInterval) time.Duration {
	switch value {
	case unified.Candle1Minute:
		return time.Minute
	case unified.Candle5Minutes:
		return 5 * time.Minute
	case unified.Candle15Minutes:
		return 15 * time.Minute
	case unified.Candle30Minutes:
		return 30 * time.Minute
	case unified.Candle1Hour:
		return time.Hour
	case unified.Candle4Hours:
		return 4 * time.Hour
	default:
		return 0
	}
}

func toCoinbaseCandleGranularity(value unified.CandleInterval) CandleGranularity {
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

func toCoinbaseSide(side unified.Side) Side {
	if side == unified.SideBuy {
		return SideBuy
	}
	return SideSell
}

func toUnifiedCoinbaseSide(side Side) unified.Side {
	if side == SideBuy {
		return unified.SideBuy
	}
	return unified.SideSell
}

func fromCoinbaseOrder(native Order, market unified.Market) unified.Order {
	price, quantity, orderType := coinbaseOrderValues(native)
	return unified.Order{
		Exchange: model.ExchangeCoinbase, ID: native.OrderID, ClientOrderID: native.ClientOrderID,
		Market: market, NativeMarket: native.ProductID, Side: toUnifiedCoinbaseSide(native.Side),
		Type: orderType, Status: toUnifiedCoinbaseStatus(native.Status, native.FilledSize),
		Price: price, Quantity: quantity, ExecutedQuantity: native.FilledSize, Raw: native.Raw,
	}
}

func coinbaseOrderValues(native Order) (string, string, unified.OrderType) {
	configuration := native.OrderConfiguration
	switch {
	case configuration.MarketMarketIOC != nil:
		quantity := configuration.MarketMarketIOC.BaseSize
		if quantity == "" {
			quantity = configuration.MarketMarketIOC.QuoteSize
		}
		return "", quantity, unified.OrderTypeMarket
	case configuration.SORLimitIOC != nil:
		return configuration.SORLimitIOC.LimitPrice, configuration.SORLimitIOC.BaseSize, unified.OrderTypeLimit
	case configuration.LimitLimitFOK != nil:
		return configuration.LimitLimitFOK.LimitPrice, configuration.LimitLimitFOK.BaseSize, unified.OrderTypeLimit
	case configuration.LimitLimitGTC != nil:
		return configuration.LimitLimitGTC.LimitPrice, configuration.LimitLimitGTC.BaseSize, unified.OrderTypeLimit
	case native.OrderType == "MARKET":
		return "", "", unified.OrderTypeMarket
	default:
		return "", "", unified.OrderTypeLimit
	}
}

func toUnifiedCoinbaseStatus(status, filledSize string) unified.OrderStatus {
	switch status {
	case "PENDING", "QUEUED", "CANCEL_QUEUED", "EDIT_QUEUED":
		return unified.OrderStatusNew
	case "OPEN":
		if isPositiveDecimal(filledSize) {
			return unified.OrderStatusPartiallyFilled
		}
		return unified.OrderStatusNew
	case "FILLED":
		return unified.OrderStatusFilled
	case "CANCELLED":
		return unified.OrderStatusCanceled
	case "EXPIRED":
		return unified.OrderStatusExpired
	case "FAILED":
		return unified.OrderStatusRejected
	default:
		return unified.OrderStatusUnknown
	}
}
