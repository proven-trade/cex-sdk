package cryptocom

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	trade "github.com/proven-trade/proven-trade-sdk"
	"github.com/proven-trade/proven-trade-sdk/model"
	"github.com/proven-trade/proven-trade-sdk/unified"
)

const (
	cryptoComUnifiedDefaultOrderBookLimit = 16
	cryptoComUnifiedDefaultTradeLimit     = 100
	cryptoComUnifiedDefaultCandleLimit    = 200
	cryptoComSpotInstrumentType           = "CCY_PAIR"
)

// UnifiedSpot은 Crypto.com Exchange v1 native 클라이언트를 공통 Spot 계약으로 변환한다.
type UnifiedSpot struct {
	client *Client
}

var _ unified.SpotClient = (*UnifiedSpot)(nil)

// NewUnifiedSpot은 Crypto.com Spot 공통 어댑터를 생성한다.
func NewUnifiedSpot(client *Client) (*UnifiedSpot, error) {
	if client == nil {
		return nil, fmt.Errorf("Crypto.com client is required")
	}
	return &UnifiedSpot{client: client}, nil
}

// Exchange는 Crypto.com 거래소 식별자를 반환한다.
func (adapter *UnifiedSpot) Exchange() model.ExchangeID {
	return model.ExchangeCryptoCom
}

// Markets는 Crypto.com 통화쌍 상품과 거래 가능 상태를 공통 형식으로 조회한다.
func (adapter *UnifiedSpot) Markets(
	ctx context.Context,
	options ...trade.RequestOption,
) ([]unified.MarketInfo, error) {
	native, err := adapter.client.Instruments(ctx, options...)
	if err != nil {
		return nil, err
	}
	markets := make([]unified.MarketInfo, 0, len(native.Items))
	for _, instrument := range native.Items {
		if instrument.InstrumentType != cryptoComSpotInstrumentType {
			continue
		}
		market, parseErr := marketFromCryptoComInstrument(instrument)
		if parseErr != nil {
			return nil, parseErr
		}
		status := "disabled"
		if instrument.Tradable {
			status = "trading"
		}
		markets = append(markets, unified.MarketInfo{
			Exchange: model.ExchangeCryptoCom, Market: market,
			NativeMarket: instrument.Symbol, Status: status, Raw: instrument.Raw,
		})
	}
	return markets, nil
}

// Ticker는 공통 마켓의 Crypto.com 최신 가격을 조회한다.
func (adapter *UnifiedSpot) Ticker(
	ctx context.Context,
	request unified.TickerRequest,
	options ...trade.RequestOption,
) (unified.Ticker, error) {
	if err := request.Validate(); err != nil {
		return unified.Ticker{}, err
	}
	nativeMarket := cryptoComInstrumentName(request.Market)
	native, err := adapter.client.Ticker(ctx, nativeMarket, options...)
	if err != nil {
		return unified.Ticker{}, err
	}
	if native.InstrumentName != nativeMarket {
		return unified.Ticker{}, fmt.Errorf(
			"Crypto.com ticker market %q does not match %q", native.InstrumentName, nativeMarket,
		)
	}
	return unified.Ticker{
		Exchange: model.ExchangeCryptoCom, Market: request.Market,
		NativeMarket: nativeMarket, Price: native.LatestPrice.String(), Raw: native.Raw,
	}, nil
}

// OrderBook은 Crypto.com Spot 호가 스냅샷을 공통 형식으로 조회한다.
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
		limit = cryptoComUnifiedDefaultOrderBookLimit
	}
	nativeMarket := cryptoComInstrumentName(request.Market)
	native, err := adapter.client.OrderBook(ctx, OrderBookRequest{
		InstrumentName: nativeMarket, Depth: limit,
	}, options...)
	if err != nil {
		return unified.OrderBook{}, err
	}
	timestamp, err := cryptoComMilliseconds(native.Timestamp, "order book timestamp")
	if err != nil {
		return unified.OrderBook{}, err
	}
	return unified.OrderBook{
		Exchange: model.ExchangeCryptoCom, Market: request.Market, NativeMarket: nativeMarket,
		Bids: fromCryptoComBookLevels(native.Bids), Asks: fromCryptoComBookLevels(native.Asks),
		Timestamp: timestamp, Raw: native.Raw,
	}, nil
}

// RecentTrades는 Crypto.com Spot 공개 최근 체결을 공통 형식으로 조회한다.
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
		limit = cryptoComUnifiedDefaultTradeLimit
	}
	native, err := adapter.client.RecentTrades(ctx, TradesRequest{
		InstrumentName: cryptoComInstrumentName(request.Market), Count: limit,
	}, options...)
	if err != nil {
		return nil, err
	}
	trades := make([]unified.PublicTrade, len(native))
	for index, item := range native {
		side, mapErr := fromCryptoComTradeSide(item.Side)
		if mapErr != nil {
			return nil, mapErr
		}
		timestamp, parseErr := cryptoComMilliseconds(item.Timestamp, "trade timestamp")
		if parseErr != nil {
			return nil, parseErr
		}
		trades[index] = unified.PublicTrade{
			ID: string(item.TradeID), Price: item.Price.String(), Quantity: item.Quantity.String(),
			Side: side, Timestamp: timestamp,
		}
	}
	return trades, nil
}

// Candles는 Crypto.com Spot OHLCV 캔들을 공통 형식으로 조회한다.
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
		limit = cryptoComUnifiedDefaultCandleLimit
	}
	timeframe := toCryptoComCandleTimeframe(request.Interval)
	nativeLimit := limit
	if request.Interval == unified.Candle3Minutes {
		timeframe = Candle1Minute
		nativeLimit *= 3
	}
	native, err := adapter.client.Candles(ctx, CandlesRequest{
		InstrumentName: cryptoComInstrumentName(request.Market),
		Timeframe:      timeframe,
		Count:          nativeLimit,
	}, options...)
	if err != nil {
		return nil, err
	}
	candles := make([]unified.Candle, len(native))
	for index, item := range native {
		startTime, parseErr := cryptoComMilliseconds(item.Timestamp, "candle timestamp")
		if parseErr != nil {
			return nil, parseErr
		}
		candles[index] = unified.Candle{
			StartTime: startTime, Open: item.Open.String(), High: item.High.String(),
			Low: item.Low.String(), Close: item.Close.String(), Volume: item.Volume.String(),
		}
	}
	if request.Interval == unified.Candle3Minutes {
		return unified.AggregateCandles(candles, 3*time.Minute, limit)
	}
	return candles, nil
}

// Balances는 Crypto.com 담보 자산의 출금 가능·예약 수량을 공통 형식으로 조회한다.
func (adapter *UnifiedSpot) Balances(
	ctx context.Context,
	options ...trade.RequestOption,
) ([]unified.Balance, error) {
	native, err := adapter.client.Balance(ctx, options...)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{})
	var balances []unified.Balance
	for _, account := range native.Accounts {
		for _, balance := range account.PositionBalances {
			if !cryptoComAssetNameValid(balance.InstrumentName) {
				return nil, fmt.Errorf("invalid Crypto.com balance asset %q", balance.InstrumentName)
			}
			if _, exists := seen[balance.InstrumentName]; exists {
				return nil, fmt.Errorf("Crypto.com balance asset %q is duplicated", balance.InstrumentName)
			}
			seen[balance.InstrumentName] = struct{}{}
			balances = append(balances, unified.Balance{
				Asset: balance.InstrumentName, Available: balance.MaximumWithdrawal.String(),
				Locked: balance.ReservedQuantity.String(), Raw: balance.Raw,
			})
		}
	}
	return balances, nil
}

// PlaceOrder는 공통 Spot 주문을 Crypto.com 주문으로 변환해 비동기로 접수한다.
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
		generated, err := newCryptoComClientOrderID()
		if err != nil {
			return unified.Order{}, err
		}
		clientOrderID = generated
	}
	nativeRequest := PlaceOrderRequest{
		InstrumentName: cryptoComInstrumentName(request.Market),
		Side:           toCryptoComOrderSide(request.Side),
		Type:           OrderTypeMarket,
		ClientOrderID:  clientOrderID,
	}
	if request.Type == unified.OrderTypeLimit {
		nativeRequest.Type = OrderTypeLimit
		nativeRequest.Price = request.Price
		nativeRequest.Quantity = request.Quantity
		nativeRequest.TimeInForce, nativeRequest.PostOnly = toCryptoComTimeInForce(request.TimeInForce)
	} else if request.Side == unified.SideBuy {
		nativeRequest.Notional = request.QuoteAmount
	} else {
		nativeRequest.Quantity = request.Quantity
	}
	receipt, err := adapter.client.PlaceOrder(ctx, nativeRequest, options...)
	if err != nil {
		return unified.Order{}, err
	}
	return unified.Order{
		Exchange: model.ExchangeCryptoCom, ID: string(receipt.OrderID), ClientOrderID: clientOrderID,
		Market: request.Market, NativeMarket: nativeRequest.InstrumentName,
		Side: request.Side, Type: request.Type, Status: unified.OrderStatusNew,
		Price: request.Price, Quantity: request.Quantity, Raw: receipt.Raw,
	}, nil
}

// Order는 Crypto.com 주문을 거래소 주문 ID 또는 사용자 주문 ID로 조회한다.
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
	return fromCryptoComOrder(native, request.Market)
}

// CancelOrder는 Crypto.com 주문 취소 접수를 최종 상태 미확정 주문으로 반환한다.
func (adapter *UnifiedSpot) CancelOrder(
	ctx context.Context,
	request unified.OrderRequest,
	options ...trade.RequestOption,
) (unified.Order, error) {
	if err := request.Validate(); err != nil {
		return unified.Order{}, err
	}
	native, err := adapter.client.CancelOrder(ctx, CancelOrderRequest{
		OrderID: request.OrderID, ClientOrderID: request.ClientOrderID,
	}, options...)
	if err != nil {
		return unified.Order{}, err
	}
	return unified.Order{
		Exchange: model.ExchangeCryptoCom, ID: string(native.OrderID),
		ClientOrderID: native.ClientOrderID, Market: request.Market,
		NativeMarket: cryptoComInstrumentName(request.Market),
		Status:       unified.OrderStatusUnknown, Raw: native.Raw,
	}, nil
}

// OpenOrders는 Crypto.com의 단일 또는 전체 Spot 미체결 주문을 공통 형식으로 조회한다.
func (adapter *UnifiedSpot) OpenOrders(
	ctx context.Context,
	request unified.OpenOrdersRequest,
	options ...trade.RequestOption,
) ([]unified.Order, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	nativeRequest := OpenOrdersRequest{}
	if request.Market != nil {
		nativeRequest.InstrumentName = cryptoComInstrumentName(*request.Market)
	}
	native, err := adapter.client.OpenOrders(ctx, nativeRequest, options...)
	if err != nil {
		return nil, err
	}
	orders := make([]unified.Order, len(native))
	for index, item := range native {
		market, parseErr := marketFromCryptoComInstrumentName(item.InstrumentName)
		if parseErr != nil {
			return nil, parseErr
		}
		if request.Market != nil && market != *request.Market {
			return nil, fmt.Errorf(
				"Crypto.com open order market %q does not match %q",
				item.InstrumentName, nativeRequest.InstrumentName,
			)
		}
		mapped, mapErr := fromCryptoComOrder(item, market)
		if mapErr != nil {
			return nil, mapErr
		}
		orders[index] = mapped
	}
	return orders, nil
}

func cryptoComInstrumentName(market unified.Market) string {
	return market.Base + "_" + market.Quote
}

func marketFromCryptoComInstrument(instrument Instrument) (unified.Market, error) {
	market := unified.Market{Base: instrument.BaseCurrency, Quote: instrument.QuoteCurrency}
	if err := market.Validate(); err != nil || cryptoComInstrumentName(market) != instrument.Symbol {
		return unified.Market{}, fmt.Errorf("invalid Crypto.com Spot instrument %q", instrument.Symbol)
	}
	return market, nil
}

func marketFromCryptoComInstrumentName(value string) (unified.Market, error) {
	base, quote, found := strings.Cut(value, "_")
	market := unified.Market{Base: base, Quote: quote}
	if !found || market.Validate() != nil || cryptoComInstrumentName(market) != value {
		return unified.Market{}, fmt.Errorf("invalid Crypto.com Spot instrument %q", value)
	}
	return market, nil
}

func cryptoComAssetNameValid(value string) bool {
	if value == "" || len(value) > 20 {
		return false
	}
	for _, character := range value {
		if character < 'A' || character > 'Z' {
			if character < '0' || character > '9' {
				return false
			}
		}
	}
	return true
}

func fromCryptoComBookLevels(native []BookLevel) []unified.BookLevel {
	levels := make([]unified.BookLevel, len(native))
	for index, level := range native {
		levels[index] = unified.BookLevel{
			Price: level.Price.String(), Quantity: level.Quantity.String(),
		}
	}
	return levels
}

func toCryptoComCandleTimeframe(value unified.CandleInterval) CandleTimeframe {
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

func toCryptoComOrderSide(value unified.Side) OrderSide {
	if value == unified.SideBuy {
		return OrderSideBuy
	}
	return OrderSideSell
}

func fromCryptoComOrderSide(value OrderSide) (unified.Side, error) {
	switch value {
	case OrderSideBuy:
		return unified.SideBuy, nil
	case OrderSideSell:
		return unified.SideSell, nil
	default:
		return "", fmt.Errorf("unsupported Crypto.com order side %q", value)
	}
}

func fromCryptoComTradeSide(value TradeSide) (unified.Side, error) {
	switch value {
	case TradeSideBuy:
		return unified.SideBuy, nil
	case TradeSideSell:
		return unified.SideSell, nil
	default:
		return "", fmt.Errorf("unsupported Crypto.com trade side %q", value)
	}
}

func toCryptoComTimeInForce(value unified.TimeInForce) (TimeInForce, bool) {
	switch value {
	case unified.TimeInForceIOC:
		return TimeInForceImmediateOrCancel, false
	case unified.TimeInForceFOK:
		return TimeInForceFillOrKill, false
	case unified.TimeInForcePostOnly:
		return TimeInForceGoodTillCancel, true
	case unified.TimeInForceGTC:
		return TimeInForceGoodTillCancel, false
	default:
		return "", false
	}
}

func fromCryptoComOrder(native Order, market unified.Market) (unified.Order, error) {
	nativeMarket := cryptoComInstrumentName(market)
	if native.InstrumentName != nativeMarket {
		return unified.Order{}, fmt.Errorf(
			"Crypto.com order market %q does not match %q", native.InstrumentName, nativeMarket,
		)
	}
	side, err := fromCryptoComOrderSide(native.Side)
	if err != nil {
		return unified.Order{}, err
	}
	orderType, err := fromCryptoComOrderType(native.Type)
	if err != nil {
		return unified.Order{}, err
	}
	status, err := fromCryptoComOrderStatus(native.Status, native.CumulativeQuantity)
	if err != nil {
		return unified.Order{}, err
	}
	return unified.Order{
		Exchange: model.ExchangeCryptoCom, ID: string(native.OrderID),
		ClientOrderID: native.ClientOrderID, Market: market, NativeMarket: native.InstrumentName,
		Side: side, Type: orderType, Status: status, Price: native.LimitPrice.String(),
		Quantity: native.Quantity.String(), ExecutedQuantity: native.CumulativeQuantity.String(),
		Raw: native.Raw,
	}, nil
}

func fromCryptoComOrderType(value OrderType) (unified.OrderType, error) {
	switch value {
	case OrderTypeLimit:
		return unified.OrderTypeLimit, nil
	case OrderTypeMarket:
		return unified.OrderTypeMarket, nil
	default:
		return "", fmt.Errorf("unsupported Crypto.com order type %q", value)
	}
}

func fromCryptoComOrderStatus(
	value OrderStatus,
	executedQuantity Decimal,
) (unified.OrderStatus, error) {
	switch value {
	case OrderStatusPending, OrderStatusNew:
		return unified.OrderStatusNew, nil
	case OrderStatusActive:
		positive, err := cryptoComDecimalPositive(executedQuantity)
		if err != nil {
			return "", err
		}
		if positive {
			return unified.OrderStatusPartiallyFilled, nil
		}
		return unified.OrderStatusNew, nil
	case OrderStatusFilled:
		return unified.OrderStatusFilled, nil
	case OrderStatusCanceled:
		return unified.OrderStatusCanceled, nil
	case OrderStatusExpired:
		return unified.OrderStatusExpired, nil
	case OrderStatusRejected:
		return unified.OrderStatusRejected, nil
	default:
		return unified.OrderStatusUnknown, nil
	}
}

func cryptoComDecimalPositive(value Decimal) (bool, error) {
	text := string(value)
	if text == "" {
		return false, nil
	}
	if strings.HasPrefix(text, "-") {
		return false, fmt.Errorf("Crypto.com executed quantity %q cannot be negative", text)
	}
	mantissa, _, _ := strings.Cut(strings.ToLower(text), "e")
	for _, character := range mantissa {
		if character >= '1' && character <= '9' {
			return true, nil
		}
	}
	return false, nil
}

func cryptoComMilliseconds(value Scalar, name string) (int64, error) {
	parsed, err := strconv.ParseInt(string(value), 10, 64)
	if err != nil || parsed < 0 {
		return 0, fmt.Errorf("invalid Crypto.com %s %q", name, value)
	}
	return parsed, nil
}

func newCryptoComClientOrderID() (string, error) {
	value := make([]byte, 12)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate Crypto.com client order ID: %w", err)
	}
	return "proven-" + hex.EncodeToString(value), nil
}
