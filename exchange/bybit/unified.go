package bybit

import (
	"context"
	"fmt"
	"math/big"
	"strconv"
	"strings"

	trade "github.com/proven-trade/proven-trade-sdk"
	"github.com/proven-trade/proven-trade-sdk/model"
	"github.com/proven-trade/proven-trade-sdk/unified"
)

// UnifiedSpot은 Bybit V5 native 클라이언트를 공통 Spot 계약으로 변환한다.
type UnifiedSpot struct {
	client *Client
}

var _ unified.SpotClient = (*UnifiedSpot)(nil)

// NewUnifiedSpot은 Bybit Spot 공통 어댑터를 생성한다.
func NewUnifiedSpot(client *Client) (*UnifiedSpot, error) {
	if client == nil {
		return nil, fmt.Errorf("Bybit client is required")
	}
	return &UnifiedSpot{client: client}, nil
}

// Exchange는 Bybit 거래소 식별자를 반환한다.
func (adapter *UnifiedSpot) Exchange() model.ExchangeID {
	return model.ExchangeBybit
}

// Markets는 Bybit Spot 전체 상품 페이지를 공통 형식으로 조회한다.
func (adapter *UnifiedSpot) Markets(
	ctx context.Context,
	options ...trade.RequestOption,
) ([]unified.MarketInfo, error) {
	var markets []unified.MarketInfo
	cursor := ""
	seenCursors := make(map[string]struct{})
	for {
		page, err := adapter.client.Instruments(ctx, InstrumentsRequest{
			Category: CategorySpot, Limit: 1000, Cursor: cursor,
		}, options...)
		if err != nil {
			return nil, err
		}
		for _, instrument := range page.Instruments {
			markets = append(markets, unified.MarketInfo{
				Exchange: model.ExchangeBybit,
				Market: unified.Market{
					Base: instrument.BaseCoin, Quote: instrument.QuoteCoin,
				},
				NativeMarket: instrument.Symbol, Status: instrument.Status, Raw: instrument.Raw,
			})
		}
		if page.NextPageCursor == "" {
			return markets, nil
		}
		if _, exists := seenCursors[page.NextPageCursor]; exists {
			return nil, fmt.Errorf("Bybit instruments returned a repeated cursor")
		}
		seenCursors[page.NextPageCursor] = struct{}{}
		cursor = page.NextPageCursor
	}
}

// Ticker는 공통 마켓의 Bybit 최신 가격을 조회한다.
func (adapter *UnifiedSpot) Ticker(
	ctx context.Context,
	request unified.TickerRequest,
	options ...trade.RequestOption,
) (unified.Ticker, error) {
	if err := request.Validate(); err != nil {
		return unified.Ticker{}, err
	}
	native, err := adapter.client.Tickers(ctx, TickersRequest{
		Category: CategorySpot, Symbol: bybitSpotSymbol(request.Market),
	}, options...)
	if err != nil {
		return unified.Ticker{}, err
	}
	if len(native) != 1 {
		return unified.Ticker{}, fmt.Errorf("Bybit ticker response has %d items, want 1", len(native))
	}
	return unified.Ticker{
		Exchange: model.ExchangeBybit, Market: request.Market,
		NativeMarket: native[0].Symbol, Price: native[0].LastPrice, Raw: native[0].Raw,
	}, nil
}

// OrderBook은 Bybit Spot 호가 스냅샷을 공통 형식으로 조회한다.
func (adapter *UnifiedSpot) OrderBook(
	ctx context.Context,
	request unified.OrderBookRequest,
	options ...trade.RequestOption,
) (unified.OrderBook, error) {
	if err := request.Validate(); err != nil {
		return unified.OrderBook{}, err
	}
	native, err := adapter.client.OrderBook(ctx, OrderBookRequest{
		Category: CategorySpot, Symbol: bybitSpotSymbol(request.Market), Limit: request.Limit,
	}, options...)
	if err != nil {
		return unified.OrderBook{}, err
	}
	bids, err := fromBybitBookLevels(native.Bids)
	if err != nil {
		return unified.OrderBook{}, err
	}
	asks, err := fromBybitBookLevels(native.Asks)
	if err != nil {
		return unified.OrderBook{}, err
	}
	return unified.OrderBook{
		Exchange: model.ExchangeBybit, Market: request.Market,
		NativeMarket: native.Symbol, Bids: bids, Asks: asks,
		Timestamp: native.Timestamp, Raw: native.Raw,
	}, nil
}

// RecentTrades는 Bybit Spot 공개 최근 체결을 공통 형식으로 조회한다.
func (adapter *UnifiedSpot) RecentTrades(
	ctx context.Context,
	request unified.RecentTradesRequest,
	options ...trade.RequestOption,
) ([]unified.PublicTrade, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	native, err := adapter.client.RecentTrades(ctx, RecentTradesRequest{
		Category: CategorySpot, Symbol: bybitSpotSymbol(request.Market), Limit: request.Limit,
	}, options...)
	if err != nil {
		return nil, err
	}
	trades := make([]unified.PublicTrade, len(native))
	for index, item := range native {
		timestamp, parseErr := strconv.ParseInt(item.Timestamp, 10, 64)
		if parseErr != nil {
			return nil, fmt.Errorf("decode Bybit trade timestamp: %w", parseErr)
		}
		trades[index] = unified.PublicTrade{
			ID: item.ExecutionID, Price: item.Price, Quantity: item.Quantity,
			Side: toUnifiedBybitSide(item.Side), Timestamp: timestamp,
		}
	}
	return trades, nil
}

// Candles는 Bybit Spot OHLCV 캔들을 공통 형식으로 조회한다.
func (adapter *UnifiedSpot) Candles(
	ctx context.Context,
	request unified.CandlesRequest,
	options ...trade.RequestOption,
) ([]unified.Candle, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	native, err := adapter.client.Candles(ctx, CandlesRequest{
		Category: CategorySpot, Symbol: bybitSpotSymbol(request.Market),
		Interval: toBybitCandleInterval(request.Interval), Limit: request.Limit,
	}, options...)
	if err != nil {
		return nil, err
	}
	candles := make([]unified.Candle, len(native))
	for index, item := range native {
		startTime, parseErr := strconv.ParseInt(item.StartTime, 10, 64)
		if parseErr != nil {
			return nil, fmt.Errorf("decode Bybit candle timestamp: %w", parseErr)
		}
		candles[index] = unified.Candle{
			StartTime: startTime, Open: item.Open, High: item.High,
			Low: item.Low, Close: item.Close, Volume: item.Volume,
		}
	}
	return candles, nil
}

// Balances는 Bybit 통합 계정의 코인 잔고를 공통 형식으로 조회한다.
func (adapter *UnifiedSpot) Balances(
	ctx context.Context,
	options ...trade.RequestOption,
) ([]unified.Balance, error) {
	accounts, err := adapter.client.WalletBalance(ctx, WalletBalanceRequest{
		AccountType: AccountTypeUnified,
	}, options...)
	if err != nil {
		return nil, err
	}
	count := 0
	for _, account := range accounts {
		count += len(account.Coins)
	}
	balances := make([]unified.Balance, 0, count)
	for _, account := range accounts {
		for _, coin := range account.Coins {
			available, calculateErr := bybitSpotAvailable(coin)
			if calculateErr != nil {
				return nil, calculateErr
			}
			balances = append(balances, unified.Balance{
				Asset: coin.Coin, Available: available, Locked: coin.Locked,
			})
		}
	}
	return balances, nil
}

// PlaceOrder는 공통 Spot 주문을 Bybit 주문으로 변환해 생성한다.
func (adapter *UnifiedSpot) PlaceOrder(
	ctx context.Context,
	request unified.PlaceOrderRequest,
	options ...trade.RequestOption,
) (unified.Order, error) {
	if err := request.Validate(); err != nil {
		return unified.Order{}, err
	}
	nativeRequest := PlaceOrderRequest{
		Category: CategorySpot, Symbol: bybitSpotSymbol(request.Market),
		Side: toBybitSide(request.Side), OrderType: toBybitOrderType(request.Type),
		OrderLinkID: request.ClientOrderID,
	}
	if request.Type == unified.OrderTypeLimit {
		nativeRequest.Quantity = request.Quantity
		nativeRequest.Price = request.Price
		nativeRequest.TimeInForce = toBybitTimeInForce(request.TimeInForce)
		if nativeRequest.TimeInForce == "" {
			nativeRequest.TimeInForce = TimeInForceGTC
		}
	} else if request.Side == unified.SideBuy {
		nativeRequest.Quantity = request.QuoteAmount
		nativeRequest.MarketUnit = MarketUnitQuoteCoin
	} else {
		nativeRequest.Quantity = request.Quantity
		nativeRequest.MarketUnit = MarketUnitBaseCoin
	}
	reference, err := adapter.client.PlaceOrder(ctx, nativeRequest, options...)
	if err != nil {
		return unified.Order{}, err
	}
	return unified.Order{
		Exchange: model.ExchangeBybit, ID: reference.OrderID,
		ClientOrderID: reference.OrderLinkID, Market: request.Market,
		NativeMarket: nativeRequest.Symbol, Side: request.Side, Type: request.Type,
		Status: unified.OrderStatusNew, Price: request.Price,
		Quantity: nativeRequest.Quantity, Raw: reference.Raw,
	}, nil
}

// Order는 Bybit Spot 주문을 단건 조회한다.
func (adapter *UnifiedSpot) Order(
	ctx context.Context,
	request unified.OrderRequest,
	options ...trade.RequestOption,
) (unified.Order, error) {
	if err := request.Validate(); err != nil {
		return unified.Order{}, err
	}
	native, err := adapter.client.OrderInfo(ctx, OrderInfoRequest{
		Category: CategorySpot, Symbol: bybitSpotSymbol(request.Market),
		OrderID: request.OrderID, OrderLinkID: request.ClientOrderID,
	}, options...)
	if err != nil {
		return unified.Order{}, err
	}
	return fromBybitOrder(native, request.Market), nil
}

// CancelOrder는 Bybit Spot 주문 취소를 요청한다.
func (adapter *UnifiedSpot) CancelOrder(
	ctx context.Context,
	request unified.OrderRequest,
	options ...trade.RequestOption,
) (unified.Order, error) {
	if err := request.Validate(); err != nil {
		return unified.Order{}, err
	}
	reference, err := adapter.client.CancelOrder(ctx, CancelOrderRequest{
		Category: CategorySpot, Symbol: bybitSpotSymbol(request.Market),
		OrderID: request.OrderID, OrderLinkID: request.ClientOrderID,
	}, options...)
	if err != nil {
		return unified.Order{}, err
	}
	return unified.Order{
		Exchange: model.ExchangeBybit, ID: reference.OrderID,
		ClientOrderID: reference.OrderLinkID, Market: request.Market,
		NativeMarket: bybitSpotSymbol(request.Market), Status: unified.OrderStatusCanceled,
		Raw: reference.Raw,
	}, nil
}

// OpenOrders는 Bybit Spot 미체결 주문을 단일 또는 전체 마켓에서 조회한다.
func (adapter *UnifiedSpot) OpenOrders(
	ctx context.Context,
	request unified.OpenOrdersRequest,
	options ...trade.RequestOption,
) ([]unified.Order, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	nativeRequest := OpenOrdersRequest{Category: CategorySpot, Limit: 50}
	market := unified.Market{}
	if request.Market != nil {
		market = *request.Market
		nativeRequest.Symbol = bybitSpotSymbol(market)
	}
	var orders []unified.Order
	seenCursors := make(map[string]struct{})
	for {
		page, err := adapter.client.OpenOrders(ctx, nativeRequest, options...)
		if err != nil {
			return nil, err
		}
		for _, native := range page.Orders {
			orders = append(orders, fromBybitOrder(native, market))
		}
		if page.NextPageCursor == "" {
			return orders, nil
		}
		if _, exists := seenCursors[page.NextPageCursor]; exists {
			return nil, fmt.Errorf("Bybit open orders returned a repeated cursor")
		}
		seenCursors[page.NextPageCursor] = struct{}{}
		nativeRequest.Cursor = page.NextPageCursor
	}
}

func bybitSpotSymbol(market unified.Market) string {
	return market.Base + market.Quote
}

func fromBybitBookLevels(native [][]string) ([]unified.BookLevel, error) {
	levels := make([]unified.BookLevel, len(native))
	for index, level := range native {
		if len(level) < 2 {
			return nil, fmt.Errorf("Bybit order book level has %d fields, want at least 2", len(level))
		}
		levels[index] = unified.BookLevel{Price: level[0], Quantity: level[1]}
	}
	return levels, nil
}

func bybitSpotAvailable(coin WalletCoin) (string, error) {
	available, err := subtractBybitDecimals(coin.WalletBalance, coin.SpotBorrow, coin.Locked)
	if err != nil {
		return "", fmt.Errorf("calculate Bybit available balance for %s: %w", coin.Coin, err)
	}
	return available, nil
}

func subtractBybitDecimals(minuend string, subtrahends ...string) (string, error) {
	values := append([]string{minuend}, subtrahends...)
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
		digits := whole + fraction
		integer, ok := new(big.Int).SetString(digits, 10)
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
	result := new(big.Int).Set(integers[0])
	for _, value := range integers[1:] {
		result.Sub(result, value)
	}
	if result.Sign() < 0 {
		return "", fmt.Errorf("available balance is negative")
	}
	digits := result.String()
	if maximumScale == 0 {
		return digits, nil
	}
	if len(digits) <= maximumScale {
		digits = strings.Repeat("0", maximumScale-len(digits)+1) + digits
	}
	position := len(digits) - maximumScale
	return digits[:position] + "." + digits[position:], nil
}

func toBybitCandleInterval(value unified.CandleInterval) CandleInterval {
	switch value {
	case unified.Candle1Minute:
		return Candle1Minute
	case unified.Candle3Minutes:
		return Candle3Minutes
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

func toBybitSide(side unified.Side) Side {
	if side == unified.SideBuy {
		return SideBuy
	}
	return SideSell
}

func toBybitOrderType(orderType unified.OrderType) OrderType {
	if orderType == unified.OrderTypeMarket {
		return OrderTypeMarket
	}
	return OrderTypeLimit
}

func toBybitTimeInForce(value unified.TimeInForce) TimeInForce {
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

func fromBybitOrder(native Order, market unified.Market) unified.Order {
	return unified.Order{
		Exchange: model.ExchangeBybit, ID: native.OrderID, ClientOrderID: native.OrderLinkID,
		Market: market, NativeMarket: native.Symbol,
		Side: toUnifiedBybitSide(native.Side), Type: toUnifiedBybitOrderType(native.OrderType),
		Status: toUnifiedBybitStatus(native.OrderStatus), Price: native.Price,
		Quantity: native.Quantity, ExecutedQuantity: native.CumulativeExecutedQuantity, Raw: native.Raw,
	}
}

func toUnifiedBybitSide(side Side) unified.Side {
	if side == SideBuy {
		return unified.SideBuy
	}
	return unified.SideSell
}

func toUnifiedBybitOrderType(orderType OrderType) unified.OrderType {
	if orderType == OrderTypeMarket {
		return unified.OrderTypeMarket
	}
	return unified.OrderTypeLimit
}

func toUnifiedBybitStatus(status string) unified.OrderStatus {
	switch status {
	case "New", "Untriggered", "Triggered", "Created":
		return unified.OrderStatusNew
	case "PartiallyFilled":
		return unified.OrderStatusPartiallyFilled
	case "Filled":
		return unified.OrderStatusFilled
	case "Cancelled", "PartiallyFilledCanceled", "Deactivated":
		return unified.OrderStatusCanceled
	case "Rejected":
		return unified.OrderStatusRejected
	default:
		return unified.OrderStatusUnknown
	}
}
