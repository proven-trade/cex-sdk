package kraken

import (
	"context"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/model"
	"github.com/proven-trade/cex-sdk/unified"
)

const krakenUnifiedDefaultLimit = 200

// UnifiedSpot은 Kraken Spot native 클라이언트를 공통 Spot 계약으로 변환한다.
type UnifiedSpot struct {
	client *Client
}

var _ unified.SpotClient = (*UnifiedSpot)(nil)

// NewUnifiedSpot은 Kraken Spot 공통 어댑터를 생성한다.
func NewUnifiedSpot(client *Client) (*UnifiedSpot, error) {
	if client == nil {
		return nil, fmt.Errorf("Kraken client is required")
	}
	return &UnifiedSpot{client: client}, nil
}

// Exchange는 Kraken 거래소 식별자를 반환한다.
func (adapter *UnifiedSpot) Exchange() model.ExchangeID {
	return model.ExchangeKraken
}

// Markets는 Kraken Spot 상품과 상태를 공통 형식으로 조회한다.
func (adapter *UnifiedSpot) Markets(
	ctx context.Context,
	options ...trade.RequestOption,
) ([]unified.MarketInfo, error) {
	pairs, err := adapter.client.AssetPairs(ctx, AssetPairsRequest{}, options...)
	if err != nil {
		return nil, err
	}
	markets := make([]unified.MarketInfo, 0, len(pairs))
	for _, pair := range pairs {
		if pair.WebSocketName == "" {
			continue
		}
		market, mappingErr := fromKrakenWebSocketPair(pair.WebSocketName)
		if mappingErr != nil {
			return nil, mappingErr
		}
		markets = append(markets, unified.MarketInfo{
			Exchange: model.ExchangeKraken, Market: market,
			NativeMarket: pair.AltName, Status: pair.Status, Raw: pair.Raw,
		})
	}
	return markets, nil
}

// Ticker는 공통 마켓의 Kraken 최신 가격을 조회한다.
func (adapter *UnifiedSpot) Ticker(
	ctx context.Context,
	request unified.TickerRequest,
	options ...trade.RequestOption,
) (unified.Ticker, error) {
	if err := request.Validate(); err != nil {
		return unified.Ticker{}, err
	}
	native, err := adapter.client.Tickers(ctx, TickersRequest{
		Pairs: []string{krakenPair(request.Market)},
	}, options...)
	if err != nil {
		return unified.Ticker{}, err
	}
	if len(native) != 1 {
		return unified.Ticker{}, fmt.Errorf("Kraken ticker response has %d items, want 1", len(native))
	}
	return unified.Ticker{
		Exchange: model.ExchangeKraken, Market: request.Market,
		NativeMarket: native[0].PairID, Price: native[0].LastPrice, Raw: native[0].Raw,
	}, nil
}

// OrderBook은 Kraken Spot 호가 스냅샷을 공통 형식으로 조회한다.
func (adapter *UnifiedSpot) OrderBook(
	ctx context.Context,
	request unified.OrderBookRequest,
	options ...trade.RequestOption,
) (unified.OrderBook, error) {
	if err := request.Validate(); err != nil {
		return unified.OrderBook{}, err
	}
	native, err := adapter.client.OrderBook(ctx, OrderBookRequest{
		Pair: krakenPair(request.Market), Count: request.Limit,
	}, options...)
	if err != nil {
		return unified.OrderBook{}, err
	}
	return unified.OrderBook{
		Exchange: model.ExchangeKraken, Market: request.Market, NativeMarket: native.PairID,
		Bids: fromKrakenBookLevels(native.Bids), Asks: fromKrakenBookLevels(native.Asks),
		Timestamp: latestKrakenBookTimestamp(native) * 1000, Raw: native.Raw,
	}, nil
}

// RecentTrades는 Kraken Spot 공개 최근 체결을 공통 형식으로 조회한다.
func (adapter *UnifiedSpot) RecentTrades(
	ctx context.Context,
	request unified.RecentTradesRequest,
	options ...trade.RequestOption,
) ([]unified.PublicTrade, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	native, err := adapter.client.RecentTrades(ctx, RecentTradesRequest{
		Pair: krakenPair(request.Market), Count: request.Limit,
	}, options...)
	if err != nil {
		return nil, err
	}
	trades := make([]unified.PublicTrade, len(native.Trades))
	for index, item := range native.Trades {
		timestamp, parseErr := krakenSecondsToMillis(item.Time)
		if parseErr != nil {
			return nil, fmt.Errorf("decode Kraken trade timestamp: %w", parseErr)
		}
		trades[index] = unified.PublicTrade{
			ID: strconv.FormatInt(item.TradeID, 10), Price: item.Price, Quantity: item.Volume,
			Side: toUnifiedKrakenSide(item.Side), Timestamp: timestamp,
		}
	}
	return trades, nil
}

// Candles는 Kraken Spot OHLCV를 조회하고 3분봉은 1분봉으로 합성한다.
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
		limit = krakenUnifiedDefaultLimit
	}
	interval := request.Interval
	nativeInterval := toKrakenCandleInterval(interval)
	duration := krakenCandleDuration(interval)
	if interval == unified.Candle3Minutes {
		nativeInterval = Candle1Minute
		duration = 3 * time.Minute
	}
	since := adapter.client.now().Add(-duration * time.Duration(limit)).Unix()
	if since < 0 {
		since = 0
	}
	native, err := adapter.client.Candles(ctx, CandlesRequest{
		Pair: krakenPair(request.Market), Interval: nativeInterval, Since: since,
	}, options...)
	if err != nil {
		return nil, err
	}
	candles, err := fromKrakenCandles(native.Items)
	if err != nil {
		return nil, err
	}
	if interval == unified.Candle3Minutes {
		return unified.AggregateCandles(candles, 3*time.Minute, limit)
	}
	if len(candles) > limit {
		candles = candles[len(candles)-limit:]
	}
	return candles, nil
}

// Balances는 Kraken 확장 잔고로 주문 가능액과 Spot 주문 보류액을 계산한다.
func (adapter *UnifiedSpot) Balances(
	ctx context.Context,
	options ...trade.RequestOption,
) ([]unified.Balance, error) {
	native, err := adapter.client.ExtendedBalance(ctx, options...)
	if err != nil {
		return nil, err
	}
	assets := sortedKeys(native.Details)
	balances := make([]unified.Balance, len(assets))
	for index, asset := range assets {
		detail := native.Details[asset]
		available, calculationErr := krakenAvailableBalance(detail)
		if calculationErr != nil {
			return nil, fmt.Errorf("calculate Kraken available balance for %s: %w", asset, calculationErr)
		}
		balances[index] = unified.Balance{
			Asset: normalizeKrakenAsset(asset), Available: available,
			Locked: zeroIfEmpty(detail.HoldTrade), Raw: detail.Raw,
		}
	}
	return balances, nil
}

// PlaceOrder는 공통 Spot 주문을 Kraken 주문으로 변환해 생성한다.
func (adapter *UnifiedSpot) PlaceOrder(
	ctx context.Context,
	request unified.PlaceOrderRequest,
	options ...trade.RequestOption,
) (unified.Order, error) {
	if err := request.Validate(); err != nil {
		return unified.Order{}, err
	}
	nativeRequest := PlaceOrderRequest{
		Pair: krakenPair(request.Market), Side: toKrakenSide(request.Side),
		OrderType: toKrakenOrderType(request.Type), ClientOrderID: request.ClientOrderID,
	}
	if request.Type == unified.OrderTypeMarket {
		if request.Side == unified.SideBuy {
			nativeRequest.Volume = request.QuoteAmount
			nativeRequest.VolumeInQuote = true
		} else {
			nativeRequest.Volume = request.Quantity
		}
	} else {
		nativeRequest.Volume = request.Quantity
		nativeRequest.Price = request.Price
		switch request.TimeInForce {
		case unified.TimeInForceIOC:
			nativeRequest.TimeInForce = TimeInForceIOC
		case unified.TimeInForceFOK:
			nativeRequest.TimeInForce = TimeInForceFOK
		case unified.TimeInForcePostOnly:
			nativeRequest.TimeInForce = TimeInForceGTC
			nativeRequest.PostOnly = true
		}
	}
	reference, err := adapter.client.PlaceOrder(ctx, nativeRequest, options...)
	if err != nil {
		return unified.Order{}, err
	}
	if len(reference.TransactionIDs) != 1 {
		return unified.Order{}, fmt.Errorf(
			"Kraken order acknowledgement has %d transaction IDs, want 1",
			len(reference.TransactionIDs),
		)
	}
	return unified.Order{
		Exchange: model.ExchangeKraken, ID: reference.TransactionIDs[0],
		ClientOrderID: request.ClientOrderID, Market: request.Market,
		NativeMarket: nativeRequest.Pair, Side: request.Side, Type: request.Type,
		Status: unified.OrderStatusNew, Price: request.Price,
		Quantity: nativeRequest.Volume, Raw: reference.Raw,
	}, nil
}

// Order는 Kraken Spot 주문을 거래소 주문 ID 또는 사용자 주문 ID로 조회한다.
func (adapter *UnifiedSpot) Order(
	ctx context.Context,
	request unified.OrderRequest,
	options ...trade.RequestOption,
) (unified.Order, error) {
	if err := request.Validate(); err != nil {
		return unified.Order{}, err
	}
	var native Order
	var err error
	if request.OrderID != "" {
		orders, queryErr := adapter.client.OrderInfo(ctx, OrderInfoRequest{
			TransactionIDs: []string{request.OrderID},
		}, options...)
		if queryErr != nil {
			return unified.Order{}, queryErr
		}
		if len(orders) != 1 || orders[0].TransactionID != request.OrderID {
			return unified.Order{}, fmt.Errorf("Kraken order response does not match %q", request.OrderID)
		}
		native = orders[0]
	} else {
		native, err = adapter.resolveKrakenOrderByClientID(ctx, request, options...)
		if err != nil {
			return unified.Order{}, err
		}
	}
	return fromKrakenOrder(native, request.Market), nil
}

// CancelOrder는 Kraken Spot 주문 취소를 접수한다.
func (adapter *UnifiedSpot) CancelOrder(
	ctx context.Context,
	request unified.OrderRequest,
	options ...trade.RequestOption,
) (unified.Order, error) {
	if err := request.Validate(); err != nil {
		return unified.Order{}, err
	}
	native, err := adapter.client.CancelOrder(ctx, CancelOrderRequest{
		TransactionID: request.OrderID, ClientOrderID: request.ClientOrderID,
		Pair: krakenPair(request.Market),
	}, options...)
	if err != nil {
		return unified.Order{}, err
	}
	return unified.Order{
		Exchange: model.ExchangeKraken, ID: request.OrderID,
		ClientOrderID: request.ClientOrderID, Market: request.Market,
		NativeMarket: krakenPair(request.Market), Status: unified.OrderStatusCanceled, Raw: native.Raw,
	}, nil
}

// OpenOrders는 Kraken Spot 미체결 주문을 단일 또는 전체 마켓에서 조회한다.
func (adapter *UnifiedSpot) OpenOrders(
	ctx context.Context,
	request unified.OpenOrdersRequest,
	options ...trade.RequestOption,
) ([]unified.Order, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	pairMarkets, err := adapter.krakenPairMarkets(ctx, options...)
	if err != nil {
		return nil, err
	}
	page, err := adapter.client.OpenOrders(ctx, OpenOrdersRequest{}, options...)
	if err != nil {
		return nil, err
	}
	orders := make([]unified.Order, 0, len(page.Orders))
	for _, native := range page.Orders {
		market, exists := pairMarkets[native.Description.Pair]
		if !exists {
			return nil, fmt.Errorf("Kraken order contains unknown pair %q", native.Description.Pair)
		}
		if request.Market != nil && market != *request.Market {
			continue
		}
		orders = append(orders, fromKrakenOrder(native, market))
	}
	return orders, nil
}

func (adapter *UnifiedSpot) resolveKrakenOrderByClientID(
	ctx context.Context,
	request unified.OrderRequest,
	options ...trade.RequestOption,
) (Order, error) {
	var matches []Order
	open, err := adapter.client.OpenOrders(ctx, OpenOrdersRequest{
		ClientOrderID: request.ClientOrderID,
	}, options...)
	if err != nil {
		return Order{}, err
	}
	for _, order := range open.Orders {
		if order.ClientOrderID == request.ClientOrderID && krakenOrderMatchesMarket(order, request.Market) {
			matches = append(matches, order)
		}
	}
	if len(matches) == 0 {
		offset := 0
		for {
			closed, queryErr := adapter.client.ClosedOrders(ctx, ClosedOrdersRequest{
				ClientOrderID: request.ClientOrderID, Offset: offset,
			}, options...)
			if queryErr != nil {
				return Order{}, queryErr
			}
			for _, order := range closed.Orders {
				if order.ClientOrderID == request.ClientOrderID && krakenOrderMatchesMarket(order, request.Market) {
					matches = append(matches, order)
				}
			}
			offset += len(closed.Orders)
			if len(closed.Orders) == 0 || offset >= closed.Count {
				break
			}
		}
	}
	if len(matches) != 1 {
		return Order{}, fmt.Errorf(
			"Kraken client order ID %q matched %d orders, want 1", request.ClientOrderID, len(matches),
		)
	}
	return matches[0], nil
}

func (adapter *UnifiedSpot) krakenPairMarkets(
	ctx context.Context,
	options ...trade.RequestOption,
) (map[string]unified.Market, error) {
	pairs, err := adapter.client.AssetPairs(ctx, AssetPairsRequest{}, options...)
	if err != nil {
		return nil, err
	}
	result := make(map[string]unified.Market, len(pairs)*4)
	for _, pair := range pairs {
		if pair.WebSocketName == "" {
			continue
		}
		market, mappingErr := fromKrakenWebSocketPair(pair.WebSocketName)
		if mappingErr != nil {
			return nil, mappingErr
		}
		for _, alias := range []string{
			pair.ID, pair.AltName, pair.WebSocketName, strings.ReplaceAll(pair.WebSocketName, "/", ""),
		} {
			if alias != "" {
				result[alias] = market
			}
		}
	}
	return result, nil
}

func krakenPair(market unified.Market) string {
	return toKrakenAsset(market.Base) + toKrakenAsset(market.Quote)
}

func toKrakenAsset(asset string) string {
	switch asset {
	case "BTC":
		return "XBT"
	case "DOGE":
		return "XDG"
	default:
		return asset
	}
}

func normalizeKrakenAsset(asset string) string {
	base, suffix, _ := strings.Cut(asset, ".")
	switch base {
	case "XXBT", "XBT":
		base = "BTC"
	case "XXDG", "XDG":
		base = "DOGE"
	case "XETH":
		base = "ETH"
	case "ZUSD":
		base = "USD"
	case "ZEUR":
		base = "EUR"
	case "ZGBP":
		base = "GBP"
	case "ZJPY":
		base = "JPY"
	case "ZCAD":
		base = "CAD"
	case "ZAUD":
		base = "AUD"
	}
	if suffix != "" {
		return base + "." + suffix
	}
	return base
}

func fromKrakenWebSocketPair(value string) (unified.Market, error) {
	parts := strings.Split(value, "/")
	if len(parts) != 2 {
		return unified.Market{}, fmt.Errorf("invalid Kraken WebSocket pair %q", value)
	}
	market := unified.Market{
		Base: normalizeKrakenAsset(parts[0]), Quote: normalizeKrakenAsset(parts[1]),
	}
	if err := market.Validate(); err != nil {
		return unified.Market{}, fmt.Errorf("invalid Kraken WebSocket pair %q: %w", value, err)
	}
	return market, nil
}

func fromKrakenBookLevels(native []BookLevel) []unified.BookLevel {
	levels := make([]unified.BookLevel, len(native))
	for index, level := range native {
		levels[index] = unified.BookLevel{Price: level.Price, Quantity: level.Volume}
	}
	return levels
}

func latestKrakenBookTimestamp(book OrderBook) int64 {
	var latest int64
	for _, side := range [][]BookLevel{book.Bids, book.Asks} {
		for _, level := range side {
			if level.Timestamp > latest {
				latest = level.Timestamp
			}
		}
	}
	return latest
}

func krakenSecondsToMillis(value string) (int64, error) {
	seconds, ok := new(big.Rat).SetString(value)
	if !ok || seconds.Sign() < 0 {
		return 0, fmt.Errorf("invalid non-negative decimal seconds %q", value)
	}
	milliseconds := new(big.Rat).Mul(seconds, big.NewRat(1000, 1))
	integer := new(big.Int).Quo(milliseconds.Num(), milliseconds.Denom())
	if !integer.IsInt64() {
		return 0, fmt.Errorf("timestamp %q is outside int64 milliseconds", value)
	}
	return integer.Int64(), nil
}

func fromKrakenCandles(native []Candle) ([]unified.Candle, error) {
	candles := make([]unified.Candle, len(native))
	for index, item := range native {
		if item.Time < 0 || item.Time > int64(^uint64(0)>>1)/1000 {
			return nil, fmt.Errorf("Kraken candle timestamp is outside the supported range")
		}
		candles[index] = unified.Candle{
			StartTime: item.Time * 1000, Open: item.Open, High: item.High,
			Low: item.Low, Close: item.Close, Volume: item.Volume,
		}
	}
	return candles, nil
}

func krakenCandleDuration(value unified.CandleInterval) time.Duration {
	switch value {
	case unified.Candle1Minute:
		return time.Minute
	case unified.Candle3Minutes:
		return 3 * time.Minute
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

func toKrakenCandleInterval(value unified.CandleInterval) CandleInterval {
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
		return 0
	}
}

func toKrakenSide(side unified.Side) Side {
	if side == unified.SideBuy {
		return SideBuy
	}
	return SideSell
}

func toUnifiedKrakenSide(side Side) unified.Side {
	if side == SideBuy {
		return unified.SideBuy
	}
	return unified.SideSell
}

func toKrakenOrderType(orderType unified.OrderType) OrderType {
	if orderType == unified.OrderTypeMarket {
		return OrderTypeMarket
	}
	return OrderTypeLimit
}

func fromKrakenOrder(native Order, market unified.Market) unified.Order {
	nativeMarket := native.Description.Pair
	if nativeMarket == "" {
		nativeMarket = krakenPair(market)
	}
	return unified.Order{
		Exchange: model.ExchangeKraken, ID: native.TransactionID, ClientOrderID: native.ClientOrderID,
		Market: market, NativeMarket: nativeMarket,
		Side:   toUnifiedKrakenSide(native.Description.Side),
		Type:   toUnifiedKrakenOrderType(native.Description.OrderType),
		Status: toUnifiedKrakenStatus(native.Status, native.ExecutedVolume),
		Price:  native.Description.Price, Quantity: native.Volume,
		ExecutedQuantity: native.ExecutedVolume, Raw: native.Raw,
	}
}

func toUnifiedKrakenOrderType(orderType OrderType) unified.OrderType {
	if orderType == OrderTypeMarket {
		return unified.OrderTypeMarket
	}
	return unified.OrderTypeLimit
}

func toUnifiedKrakenStatus(status, executedVolume string) unified.OrderStatus {
	switch status {
	case "pending":
		return unified.OrderStatusNew
	case "open":
		if isPositiveDecimal(executedVolume) {
			return unified.OrderStatusPartiallyFilled
		}
		return unified.OrderStatusNew
	case "closed":
		return unified.OrderStatusFilled
	case "canceled", "cancelled":
		return unified.OrderStatusCanceled
	case "expired":
		return unified.OrderStatusExpired
	default:
		return unified.OrderStatusUnknown
	}
}

func krakenOrderMatchesMarket(order Order, market unified.Market) bool {
	pair := order.Description.Pair
	for _, alias := range []string{
		krakenPair(market),
		toKrakenAsset(market.Base) + "/" + toKrakenAsset(market.Quote),
		market.Base + market.Quote,
		market.Base + "/" + market.Quote,
	} {
		if pair == alias {
			return true
		}
	}
	return false
}

func krakenAvailableBalance(detail ExtendedBalanceDetail) (string, error) {
	values := []string{detail.Balance, detail.Credit, detail.CreditUsed, detail.HoldTrade}
	integers := make([]*big.Int, len(values))
	scales := make([]int, len(values))
	maximumScale := 0
	for index, value := range values {
		if value == "" {
			value = "0"
		}
		if !positiveDecimalPattern.MatchString(value) {
			return "", fmt.Errorf("invalid decimal %q", value)
		}
		whole, fraction, _ := strings.Cut(value, ".")
		integer, ok := new(big.Int).SetString(whole+fraction, 10)
		if !ok {
			return "", fmt.Errorf("invalid decimal %q", value)
		}
		integers[index] = integer
		scales[index] = len(fraction)
		if scales[index] > maximumScale {
			maximumScale = scales[index]
		}
	}
	for index := range integers {
		if difference := maximumScale - scales[index]; difference > 0 {
			integers[index].Mul(integers[index], new(big.Int).Exp(
				big.NewInt(10), big.NewInt(int64(difference)), nil,
			))
		}
	}
	result := new(big.Int).Add(integers[0], integers[1])
	result.Sub(result, integers[2])
	result.Sub(result, integers[3])
	if result.Sign() < 0 {
		return "", fmt.Errorf("available balance is negative")
	}
	return formatKrakenDecimal(result, maximumScale), nil
}

func formatKrakenDecimal(value *big.Int, scale int) string {
	digits := value.String()
	if scale == 0 {
		return digits
	}
	if len(digits) <= scale {
		digits = strings.Repeat("0", scale-len(digits)+1) + digits
	}
	position := len(digits) - scale
	return digits[:position] + "." + digits[position:]
}

func zeroIfEmpty(value string) string {
	if value == "" {
		return "0"
	}
	return value
}
