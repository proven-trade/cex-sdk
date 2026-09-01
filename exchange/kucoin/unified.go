package kucoin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/model"
	"github.com/proven-trade/cex-sdk/unified"
)

const kucoinUnifiedDefaultLimit = 200

// UnifiedSpot은 KuCoin Classic native 클라이언트를 공통 Spot 계약으로 변환한다.
type UnifiedSpot struct {
	client *Client
}

var _ unified.SpotClient = (*UnifiedSpot)(nil)

// NewUnifiedSpot은 KuCoin Classic Spot 공통 어댑터를 생성한다.
func NewUnifiedSpot(client *Client) (*UnifiedSpot, error) {
	if client == nil {
		return nil, fmt.Errorf("KuCoin client is required")
	}
	return &UnifiedSpot{client: client}, nil
}

// Exchange는 KuCoin 거래소 식별자를 반환한다.
func (adapter *UnifiedSpot) Exchange() model.ExchangeID {
	return model.ExchangeKuCoin
}

// Markets는 KuCoin Spot 거래쌍과 상태를 공통 형식으로 조회한다.
func (adapter *UnifiedSpot) Markets(
	ctx context.Context,
	options ...trade.RequestOption,
) ([]unified.MarketInfo, error) {
	native, err := adapter.client.Symbols(ctx, options...)
	if err != nil {
		return nil, err
	}
	markets := make([]unified.MarketInfo, len(native))
	for index, symbol := range native {
		market := unified.Market{Base: symbol.BaseCurrency, Quote: symbol.QuoteCurrency}
		if err := market.Validate(); err != nil || kucoinSymbol(market) != symbol.Symbol {
			return nil, fmt.Errorf("invalid KuCoin Spot symbol %q", symbol.Symbol)
		}
		status := "disabled"
		if symbol.TradingEnabled {
			status = "trading"
		}
		markets[index] = unified.MarketInfo{
			Exchange: model.ExchangeKuCoin, Market: market,
			NativeMarket: symbol.Symbol, Status: status,
			PriceIncrement: symbol.PriceIncrement, QuantityIncrement: symbol.BaseIncrement,
			QuoteAmountIncrement: symbol.QuoteIncrement,
			MinimumBaseQuantity:  symbol.BaseMinimumSize,
			MinimumQuoteAmount:   symbol.MinimumFunds, Raw: symbol.Raw,
		}
	}
	return markets, nil
}

// Ticker는 공통 마켓의 KuCoin 최신 가격을 조회한다.
func (adapter *UnifiedSpot) Ticker(
	ctx context.Context,
	request unified.TickerRequest,
	options ...trade.RequestOption,
) (unified.Ticker, error) {
	if err := request.Validate(); err != nil {
		return unified.Ticker{}, err
	}
	nativeMarket := kucoinSymbol(request.Market)
	native, err := adapter.client.Ticker(ctx, nativeMarket, options...)
	if err != nil {
		return unified.Ticker{}, err
	}
	return unified.Ticker{
		Exchange: model.ExchangeKuCoin, Market: request.Market,
		NativeMarket: nativeMarket, Price: native.Price, Raw: native.Raw,
	}, nil
}

// OrderBook은 KuCoin 20단계 호가를 공통 깊이로 잘라 반환한다.
func (adapter *UnifiedSpot) OrderBook(
	ctx context.Context,
	request unified.OrderBookRequest,
	options ...trade.RequestOption,
) (unified.OrderBook, error) {
	if err := request.Validate(); err != nil {
		return unified.OrderBook{}, err
	}
	nativeMarket := kucoinSymbol(request.Market)
	native, err := adapter.client.OrderBook(ctx, OrderBookRequest{
		Symbol: nativeMarket, Size: OrderBook20,
	}, options...)
	if err != nil {
		return unified.OrderBook{}, err
	}
	limit := request.Limit
	if limit == 0 {
		limit = 16
	}
	return unified.OrderBook{
		Exchange: model.ExchangeKuCoin, Market: request.Market, NativeMarket: nativeMarket,
		Bids:      fromKuCoinBookLevels(native.Bids, limit),
		Asks:      fromKuCoinBookLevels(native.Asks, limit),
		Timestamp: native.Time, Raw: native.Raw,
	}, nil
}

// RecentTrades는 KuCoin 공개 최근 체결을 공통 형식으로 조회한다.
func (adapter *UnifiedSpot) RecentTrades(
	ctx context.Context,
	request unified.RecentTradesRequest,
	options ...trade.RequestOption,
) ([]unified.PublicTrade, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	native, err := adapter.client.RecentTrades(ctx, RecentTradesRequest{
		Symbol: kucoinSymbol(request.Market),
	}, options...)
	if err != nil {
		return nil, err
	}
	limit := request.Limit
	if limit == 0 {
		limit = 100
	}
	depth := limitedKuCoinDepth(len(native), limit)
	trades := make([]unified.PublicTrade, depth)
	for index, item := range native[:depth] {
		if item.Time < 0 {
			return nil, fmt.Errorf("invalid KuCoin trade timestamp %d", item.Time)
		}
		trades[index] = unified.PublicTrade{
			ID: item.Sequence, Price: item.Price, Quantity: item.Size,
			Side: toUnifiedKuCoinSide(item.Side), Timestamp: item.Time / int64(time.Millisecond),
		}
	}
	return trades, nil
}

// Candles는 KuCoin Spot OHLCV 캔들을 공통 형식으로 조회한다.
func (adapter *UnifiedSpot) Candles(
	ctx context.Context,
	request unified.CandlesRequest,
	options ...trade.RequestOption,
) ([]unified.Candle, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	native, err := adapter.client.Candles(ctx, CandlesRequest{
		Symbol: kucoinSymbol(request.Market), Interval: toKuCoinCandleInterval(request.Interval),
	}, options...)
	if err != nil {
		return nil, err
	}
	limit := request.Limit
	if limit == 0 {
		limit = kucoinUnifiedDefaultLimit
	}
	depth := limitedKuCoinDepth(len(native), limit)
	candles := make([]unified.Candle, depth)
	for index, item := range native[:depth] {
		startTime, parseErr := kucoinCandleMilliseconds(item.Timestamp)
		if parseErr != nil {
			return nil, parseErr
		}
		candles[index] = unified.Candle{
			StartTime: startTime, Open: item.Open, High: item.High,
			Low: item.Low, Close: item.Close, Volume: item.Volume,
		}
	}
	return candles, nil
}

// Balances는 KuCoin 거래 계정의 주문 가능·잠금 잔고를 공통 형식으로 조회한다.
func (adapter *UnifiedSpot) Balances(
	ctx context.Context,
	options ...trade.RequestOption,
) ([]unified.Balance, error) {
	native, err := adapter.client.Accounts(ctx, AccountsRequest{Type: AccountTypeTrade}, options...)
	if err != nil {
		return nil, err
	}
	balances := make([]unified.Balance, len(native))
	for index, account := range native {
		balances[index] = unified.Balance{
			Asset: account.Currency, Available: account.Available,
			Locked: account.Holds, Raw: account.Raw,
		}
	}
	return balances, nil
}

// PlaceOrder는 공통 Spot 주문을 KuCoin HF 주문으로 변환해 생성한다.
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
		generated, err := newKuCoinClientOrderID()
		if err != nil {
			return unified.Order{}, err
		}
		clientOrderID = generated
	}
	nativeRequest := PlaceOrderRequest{
		ClientOrderID: clientOrderID, Symbol: kucoinSymbol(request.Market),
		Side: toKuCoinSide(request.Side), Type: OrderTypeMarket,
	}
	if request.Type == unified.OrderTypeLimit {
		nativeRequest.Type = OrderTypeLimit
		nativeRequest.Price = request.Price
		nativeRequest.Size = request.Quantity
		nativeRequest.TimeInForce = toKuCoinTimeInForce(request.TimeInForce)
		if request.TimeInForce == unified.TimeInForcePostOnly {
			nativeRequest.TimeInForce = TimeInForceGTC
			nativeRequest.PostOnly = true
		}
	} else if request.Side == unified.SideBuy {
		nativeRequest.Funds = request.QuoteAmount
	} else {
		nativeRequest.Size = request.Quantity
	}
	reference, err := adapter.client.PlaceOrder(ctx, nativeRequest, options...)
	if err != nil {
		return unified.Order{}, err
	}
	return unified.Order{
		Exchange: model.ExchangeKuCoin, ID: reference.OrderID,
		ClientOrderID: clientOrderID, Market: request.Market,
		NativeMarket: nativeRequest.Symbol, Side: request.Side, Type: request.Type,
		Status: unified.OrderStatusAcknowledged, Price: request.Price,
		Quantity: request.Quantity, QuoteAmount: request.QuoteAmount, Raw: reference.Raw,
	}, nil
}

// Order는 KuCoin 주문을 거래소 주문 ID 또는 사용자 주문 ID로 조회한다.
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
		Symbol: kucoinSymbol(request.Market),
	}, options...)
	if err != nil {
		return unified.Order{}, err
	}
	return fromKuCoinOrder(native, request.Market)
}

// CancelOrder는 KuCoin 주문 취소를 접수한다.
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
		Symbol: kucoinSymbol(request.Market),
	}, options...)
	if err != nil {
		return unified.Order{}, err
	}
	orderID := reference.OrderID
	if orderID == "" {
		orderID = request.OrderID
	}
	clientOrderID := reference.ClientOrderID
	if clientOrderID == "" {
		clientOrderID = request.ClientOrderID
	}
	return unified.Order{
		Exchange: model.ExchangeKuCoin, ID: orderID, ClientOrderID: clientOrderID,
		Market: request.Market, NativeMarket: kucoinSymbol(request.Market),
		Status: unified.OrderStatusCancelPending, Raw: reference.Raw,
	}, nil
}

// OpenOrders는 KuCoin 미체결 주문을 단일 또는 전체 마켓에서 끝까지 조회한다.
func (adapter *UnifiedSpot) OpenOrders(
	ctx context.Context,
	request unified.OpenOrdersRequest,
	options ...trade.RequestOption,
) ([]unified.Order, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	var symbols []string
	if request.Market != nil {
		symbols = []string{kucoinSymbol(*request.Market)}
	} else {
		var err error
		symbols, err = adapter.client.OpenOrderSymbols(ctx, options...)
		if err != nil {
			return nil, err
		}
	}
	var orders []unified.Order
	for _, symbol := range symbols {
		if _, err := fromKuCoinSymbol(symbol); err != nil {
			return nil, err
		}
		pageNumber := 1
		for {
			page, err := adapter.client.OpenOrders(ctx, OpenOrdersRequest{
				Symbol: symbol, PageNumber: pageNumber, PageSize: 50,
			}, options...)
			if err != nil {
				return nil, err
			}
			if page.CurrentPage != pageNumber && page.TotalPages > 0 {
				return nil, fmt.Errorf(
					"KuCoin open orders returned page %d, want %d", page.CurrentPage, pageNumber,
				)
			}
			for _, native := range page.Orders {
				if native.Symbol != symbol {
					return nil, fmt.Errorf(
						"KuCoin open orders for %q contains %q", symbol, native.Symbol,
					)
				}
				orderMarket, parseErr := fromKuCoinSymbol(native.Symbol)
				if parseErr != nil {
					return nil, parseErr
				}
				order, parseErr := fromKuCoinOrder(native, orderMarket)
				if parseErr != nil {
					return nil, parseErr
				}
				orders = append(orders, order)
			}
			if page.TotalPages == 0 || pageNumber >= page.TotalPages {
				break
			}
			pageNumber++
		}
	}
	return orders, nil
}

func kucoinSymbol(market unified.Market) string {
	return market.Base + "-" + market.Quote
}

func fromKuCoinSymbol(symbol string) (unified.Market, error) {
	base, quote, found := strings.Cut(symbol, "-")
	market := unified.Market{Base: base, Quote: quote}
	if !found || market.Validate() != nil || kucoinSymbol(market) != symbol {
		return unified.Market{}, fmt.Errorf("invalid KuCoin Spot symbol %q", symbol)
	}
	return market, nil
}

func fromKuCoinBookLevels(native []BookLevel, limit int) []unified.BookLevel {
	depth := limitedKuCoinDepth(len(native), limit)
	levels := make([]unified.BookLevel, depth)
	for index, level := range native[:depth] {
		levels[index] = unified.BookLevel{Price: level.Price, Quantity: level.Size}
	}
	return levels
}

func limitedKuCoinDepth(length, limit int) int {
	if limit < length {
		return limit
	}
	return length
}

func toKuCoinCandleInterval(value unified.CandleInterval) CandleInterval {
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

func kucoinCandleMilliseconds(value string) (int64, error) {
	seconds, err := strconv.ParseInt(value, 10, 64)
	if err != nil || seconds < 0 || seconds > math.MaxInt64/1_000 {
		if err == nil {
			err = fmt.Errorf("timestamp is outside millisecond range")
		}
		return 0, fmt.Errorf("decode KuCoin candle timestamp %q: %w", value, err)
	}
	return seconds * 1_000, nil
}

func toKuCoinSide(side unified.Side) Side {
	if side == unified.SideBuy {
		return SideBuy
	}
	return SideSell
}

func toUnifiedKuCoinSide(side Side) unified.Side {
	if side == SideBuy {
		return unified.SideBuy
	}
	return unified.SideSell
}

func toKuCoinTimeInForce(value unified.TimeInForce) TimeInForce {
	switch value {
	case unified.TimeInForceGTC:
		return TimeInForceGTC
	case unified.TimeInForceIOC:
		return TimeInForceIOC
	case unified.TimeInForceFOK:
		return TimeInForceFOK
	default:
		return ""
	}
}

func fromKuCoinOrder(native Order, market unified.Market) (unified.Order, error) {
	if native.Symbol != kucoinSymbol(market) {
		return unified.Order{}, fmt.Errorf(
			"KuCoin order market %q does not match %q", native.Symbol, kucoinSymbol(market),
		)
	}
	status, err := toUnifiedKuCoinOrderStatus(native)
	if err != nil {
		return unified.Order{}, err
	}
	orderType := unified.OrderTypeLimit
	quantity := native.Size
	quoteAmount := ""
	if native.Type == OrderTypeMarket {
		orderType = unified.OrderTypeMarket
		if native.Side == SideBuy {
			quantity, quoteAmount = "", native.Funds
		}
	}
	return unified.Order{
		Exchange: model.ExchangeKuCoin, ID: native.ID, ClientOrderID: native.ClientOrderID,
		Market: market, NativeMarket: native.Symbol, Side: toUnifiedKuCoinSide(native.Side),
		Type: orderType, Status: status, Price: native.Price,
		Quantity: quantity, QuoteAmount: quoteAmount,
		ExecutedQuantity: native.DealSize, Raw: native.Raw,
	}, nil
}

func toUnifiedKuCoinOrderStatus(native Order) (unified.OrderStatus, error) {
	filled, err := kucoinDecimalPositive(native.DealSize)
	if err != nil {
		return unified.OrderStatusUnknown, fmt.Errorf("decode KuCoin order filled size: %w", err)
	}
	if native.Active {
		if filled {
			return unified.OrderStatusPartiallyFilled, nil
		}
		return unified.OrderStatusNew, nil
	}
	if native.CancelExists {
		return unified.OrderStatusCanceled, nil
	}
	equal, err := kucoinDecimalsEqual(native.DealSize, native.Size)
	if err != nil {
		return unified.OrderStatusUnknown, fmt.Errorf("decode KuCoin order size: %w", err)
	}
	if filled && equal {
		return unified.OrderStatusFilled, nil
	}
	return unified.OrderStatusUnknown, nil
}

func kucoinDecimalPositive(value string) (bool, error) {
	if value == "" {
		return false, nil
	}
	if !positiveDecimalRegex.MatchString(value) {
		return false, fmt.Errorf("invalid decimal %q", value)
	}
	return strings.Trim(value, "0.") != "", nil
}

func kucoinDecimalsEqual(left, right string) (bool, error) {
	if left == "" || right == "" {
		return false, nil
	}
	if !positiveDecimalRegex.MatchString(left) || !positiveDecimalRegex.MatchString(right) {
		return false, fmt.Errorf("invalid decimals %q and %q", left, right)
	}
	leftValue, leftOK := new(big.Rat).SetString(left)
	rightValue, rightOK := new(big.Rat).SetString(right)
	if !leftOK || !rightOK {
		return false, fmt.Errorf("invalid decimals %q and %q", left, right)
	}
	return leftValue.Cmp(rightValue) == 0, nil
}

func newKuCoinClientOrderID() (string, error) {
	randomBytes := make([]byte, 14)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", fmt.Errorf("generate KuCoin client order ID: %w", err)
	}
	return "proven-" + hex.EncodeToString(randomBytes), nil
}
