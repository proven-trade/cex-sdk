package okx

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	trade "github.com/proven-trade/proven-trade-sdk"
	"github.com/proven-trade/proven-trade-sdk/model"
	"github.com/proven-trade/proven-trade-sdk/unified"
)

// UnifiedSpot은 OKX V5 native 클라이언트를 공통 Spot 계약으로 변환한다.
type UnifiedSpot struct {
	client *Client
}

var _ unified.SpotClient = (*UnifiedSpot)(nil)

// NewUnifiedSpot은 OKX Spot 공통 어댑터를 생성한다.
func NewUnifiedSpot(client *Client) (*UnifiedSpot, error) {
	if client == nil {
		return nil, fmt.Errorf("OKX client is required")
	}
	return &UnifiedSpot{client: client}, nil
}

// Exchange는 OKX 거래소 식별자를 반환한다.
func (adapter *UnifiedSpot) Exchange() model.ExchangeID {
	return model.ExchangeOKX
}

// Markets는 OKX Spot 상품과 상태를 공통 형식으로 조회한다.
func (adapter *UnifiedSpot) Markets(
	ctx context.Context,
	options ...trade.RequestOption,
) ([]unified.MarketInfo, error) {
	native, err := adapter.client.Instruments(ctx, InstrumentsRequest{
		InstrumentType: InstrumentTypeSpot,
	}, options...)
	if err != nil {
		return nil, err
	}
	markets := make([]unified.MarketInfo, len(native))
	for index, instrument := range native {
		markets[index] = unified.MarketInfo{
			Exchange: model.ExchangeOKX,
			Market: unified.Market{
				Base: instrument.BaseCurrency, Quote: instrument.QuoteCurrency,
			},
			NativeMarket: instrument.InstrumentID, Status: instrument.State, Raw: instrument.Raw,
		}
	}
	return markets, nil
}

// Ticker는 공통 마켓의 OKX 최신 가격을 조회한다.
func (adapter *UnifiedSpot) Ticker(
	ctx context.Context,
	request unified.TickerRequest,
	options ...trade.RequestOption,
) (unified.Ticker, error) {
	if err := request.Validate(); err != nil {
		return unified.Ticker{}, err
	}
	nativeMarket := okxSpotInstrumentID(request.Market)
	tickers, err := adapter.client.Tickers(ctx, TickersRequest{
		InstrumentType: InstrumentTypeSpot,
	}, options...)
	if err != nil {
		return unified.Ticker{}, err
	}
	var matched *Ticker
	for index := range tickers {
		if tickers[index].InstrumentID != nativeMarket {
			continue
		}
		if matched != nil {
			return unified.Ticker{}, fmt.Errorf("OKX ticker response contains duplicate %q", nativeMarket)
		}
		matched = &tickers[index]
	}
	if matched == nil {
		return unified.Ticker{}, fmt.Errorf("OKX ticker response does not contain %q", nativeMarket)
	}
	return unified.Ticker{
		Exchange: model.ExchangeOKX, Market: request.Market,
		NativeMarket: matched.InstrumentID, Price: matched.LastPrice, Raw: matched.Raw,
	}, nil
}

// OrderBook은 OKX Spot 호가 스냅샷을 공통 형식으로 조회한다.
func (adapter *UnifiedSpot) OrderBook(
	ctx context.Context,
	request unified.OrderBookRequest,
	options ...trade.RequestOption,
) (unified.OrderBook, error) {
	if err := request.Validate(); err != nil {
		return unified.OrderBook{}, err
	}
	nativeMarket := okxSpotInstrumentID(request.Market)
	native, err := adapter.client.OrderBook(ctx, OrderBookRequest{
		InstrumentID: nativeMarket, Size: request.Limit,
	}, options...)
	if err != nil {
		return unified.OrderBook{}, err
	}
	timestamp, err := parseOKXMilliseconds("order book", native.Timestamp)
	if err != nil {
		return unified.OrderBook{}, err
	}
	return unified.OrderBook{
		Exchange: model.ExchangeOKX, Market: request.Market, NativeMarket: nativeMarket,
		Bids: fromOKXBookLevels(native.Bids), Asks: fromOKXBookLevels(native.Asks),
		Timestamp: timestamp, Raw: native.Raw,
	}, nil
}

// RecentTrades는 OKX Spot 공개 최근 체결을 공통 형식으로 조회한다.
func (adapter *UnifiedSpot) RecentTrades(
	ctx context.Context,
	request unified.RecentTradesRequest,
	options ...trade.RequestOption,
) ([]unified.PublicTrade, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	native, err := adapter.client.RecentTrades(ctx, RecentTradesRequest{
		InstrumentID: okxSpotInstrumentID(request.Market), Limit: request.Limit,
	}, options...)
	if err != nil {
		return nil, err
	}
	trades := make([]unified.PublicTrade, len(native))
	for index, item := range native {
		timestamp, parseErr := parseOKXMilliseconds("trade", item.Timestamp)
		if parseErr != nil {
			return nil, parseErr
		}
		trades[index] = unified.PublicTrade{
			ID: item.TradeID, Price: item.Price, Quantity: item.Quantity,
			Side: toUnifiedOKXSide(item.Side), Timestamp: timestamp,
		}
	}
	return trades, nil
}

// Candles는 OKX Spot OHLCV 캔들을 공통 형식으로 조회한다.
func (adapter *UnifiedSpot) Candles(
	ctx context.Context,
	request unified.CandlesRequest,
	options ...trade.RequestOption,
) ([]unified.Candle, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	native, err := adapter.client.Candles(ctx, CandlesRequest{
		InstrumentID: okxSpotInstrumentID(request.Market),
		Interval:     toOKXCandleInterval(request.Interval),
		Limit:        request.Limit,
	}, options...)
	if err != nil {
		return nil, err
	}
	candles := make([]unified.Candle, len(native))
	for index, item := range native {
		startTime, parseErr := parseOKXMilliseconds("candle", item.Timestamp)
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

// Balances는 OKX 거래 계정의 통화별 주문 가능 잔고를 공통 형식으로 조회한다.
func (adapter *UnifiedSpot) Balances(
	ctx context.Context,
	options ...trade.RequestOption,
) ([]unified.Balance, error) {
	account, err := adapter.client.Balance(ctx, BalanceRequest{}, options...)
	if err != nil {
		return nil, err
	}
	balances := make([]unified.Balance, len(account.Details))
	for index, detail := range account.Details {
		balances[index] = unified.Balance{
			Asset: detail.Currency, Available: detail.AvailableBalance,
			Locked: detail.FrozenBalance, Raw: account.Raw,
		}
	}
	return balances, nil
}

// PlaceOrder는 공통 Spot 주문을 OKX 주문으로 변환해 생성한다.
func (adapter *UnifiedSpot) PlaceOrder(
	ctx context.Context,
	request unified.PlaceOrderRequest,
	options ...trade.RequestOption,
) (unified.Order, error) {
	if err := request.Validate(); err != nil {
		return unified.Order{}, err
	}
	nativeRequest := PlaceOrderRequest{
		InstrumentType: InstrumentTypeSpot,
		InstrumentID:   okxSpotInstrumentID(request.Market),
		TradeMode:      TradeModeCash,
		ClientOrderID:  request.ClientOrderID,
		Side:           toOKXSide(request.Side),
	}
	if request.Type == unified.OrderTypeLimit {
		nativeRequest.OrderType = toOKXLimitOrderType(request.TimeInForce)
		nativeRequest.Quantity = request.Quantity
		nativeRequest.Price = request.Price
	} else {
		nativeRequest.OrderType = OrderTypeMarket
		if request.Side == unified.SideBuy {
			nativeRequest.Quantity = request.QuoteAmount
			nativeRequest.TargetCurrency = TargetCurrencyQuote
		} else {
			nativeRequest.Quantity = request.Quantity
			nativeRequest.TargetCurrency = TargetCurrencyBase
		}
	}
	reference, err := adapter.client.PlaceOrder(ctx, nativeRequest, options...)
	if err != nil {
		return unified.Order{}, err
	}
	return unified.Order{
		Exchange: model.ExchangeOKX, ID: reference.OrderID,
		ClientOrderID: reference.ClientOrderID, Market: request.Market,
		NativeMarket: nativeRequest.InstrumentID, Side: request.Side, Type: request.Type,
		Status: unified.OrderStatusNew, Price: request.Price,
		Quantity: nativeRequest.Quantity, Raw: reference.Raw,
	}, nil
}

// Order는 OKX Spot 주문을 단건 조회한다.
func (adapter *UnifiedSpot) Order(
	ctx context.Context,
	request unified.OrderRequest,
	options ...trade.RequestOption,
) (unified.Order, error) {
	if err := request.Validate(); err != nil {
		return unified.Order{}, err
	}
	native, err := adapter.client.OrderInfo(ctx, OrderInfoRequest{
		InstrumentID: okxSpotInstrumentID(request.Market),
		OrderID:      request.OrderID, ClientOrderID: request.ClientOrderID,
	}, options...)
	if err != nil {
		return unified.Order{}, err
	}
	return fromOKXOrder(native, request.Market), nil
}

// CancelOrder는 OKX Spot 주문 취소를 요청한다.
func (adapter *UnifiedSpot) CancelOrder(
	ctx context.Context,
	request unified.OrderRequest,
	options ...trade.RequestOption,
) (unified.Order, error) {
	if err := request.Validate(); err != nil {
		return unified.Order{}, err
	}
	nativeMarket := okxSpotInstrumentID(request.Market)
	reference, err := adapter.client.CancelOrder(ctx, CancelOrderRequest{
		InstrumentID: nativeMarket, OrderID: request.OrderID, ClientOrderID: request.ClientOrderID,
	}, options...)
	if err != nil {
		return unified.Order{}, err
	}
	return unified.Order{
		Exchange: model.ExchangeOKX, ID: reference.OrderID,
		ClientOrderID: reference.ClientOrderID, Market: request.Market,
		NativeMarket: nativeMarket, Status: unified.OrderStatusCanceled, Raw: reference.Raw,
	}, nil
}

// OpenOrders는 OKX Spot 미체결 주문을 단일 또는 전체 마켓에서 끝까지 조회한다.
func (adapter *UnifiedSpot) OpenOrders(
	ctx context.Context,
	request unified.OpenOrdersRequest,
	options ...trade.RequestOption,
) ([]unified.Order, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	nativeRequest := OpenOrdersRequest{InstrumentType: InstrumentTypeSpot, Limit: 100}
	requestedMarket := unified.Market{}
	if request.Market != nil {
		requestedMarket = *request.Market
		nativeRequest.InstrumentID = okxSpotInstrumentID(requestedMarket)
	}
	var orders []unified.Order
	seenCursors := make(map[string]struct{})
	for {
		page, err := adapter.client.OpenOrders(ctx, nativeRequest, options...)
		if err != nil {
			return nil, err
		}
		for _, native := range page.Orders {
			market := requestedMarket
			if request.AllMarkets {
				market, err = fromOKXSpotInstrumentID(native.InstrumentID)
				if err != nil {
					return nil, err
				}
			}
			orders = append(orders, fromOKXOrder(native, market))
		}
		if len(page.Orders) < nativeRequest.Limit {
			return orders, nil
		}
		cursor := page.Orders[len(page.Orders)-1].OrderID
		if cursor == "" {
			return nil, fmt.Errorf("OKX open orders cursor is empty")
		}
		if _, exists := seenCursors[cursor]; exists {
			return nil, fmt.Errorf("OKX open orders returned a repeated cursor")
		}
		seenCursors[cursor] = struct{}{}
		nativeRequest.AfterOrderID = cursor
	}
}

func okxSpotInstrumentID(market unified.Market) string {
	return market.Base + "-" + market.Quote
}

func fromOKXSpotInstrumentID(value string) (unified.Market, error) {
	parts := strings.Split(value, "-")
	if len(parts) != 2 {
		return unified.Market{}, fmt.Errorf("invalid OKX Spot instrument ID %q", value)
	}
	market := unified.Market{Base: parts[0], Quote: parts[1]}
	if err := market.Validate(); err != nil {
		return unified.Market{}, fmt.Errorf("invalid OKX Spot instrument ID %q: %w", value, err)
	}
	return market, nil
}

func fromOKXBookLevels(native []BookLevel) []unified.BookLevel {
	levels := make([]unified.BookLevel, len(native))
	for index, level := range native {
		levels[index] = unified.BookLevel{Price: level.Price, Quantity: level.Quantity}
	}
	return levels
}

func parseOKXMilliseconds(kind, value string) (int64, error) {
	timestamp, err := strconv.ParseInt(value, 10, 64)
	if err != nil || timestamp < 0 {
		if err == nil {
			err = fmt.Errorf("timestamp must not be negative")
		}
		return 0, fmt.Errorf("decode OKX %s timestamp: %w", kind, err)
	}
	return timestamp, nil
}

func toOKXCandleInterval(value unified.CandleInterval) CandleInterval {
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

func toOKXSide(side unified.Side) Side {
	if side == unified.SideBuy {
		return SideBuy
	}
	return SideSell
}

func toUnifiedOKXSide(side Side) unified.Side {
	if side == SideBuy {
		return unified.SideBuy
	}
	return unified.SideSell
}

func toOKXLimitOrderType(value unified.TimeInForce) OrderType {
	switch value {
	case unified.TimeInForceIOC:
		return OrderTypeIOC
	case unified.TimeInForceFOK:
		return OrderTypeFOK
	case unified.TimeInForcePostOnly:
		return OrderTypePostOnly
	default:
		return OrderTypeLimit
	}
}

func fromOKXOrder(native Order, market unified.Market) unified.Order {
	return unified.Order{
		Exchange: model.ExchangeOKX, ID: native.OrderID, ClientOrderID: native.ClientOrderID,
		Market: market, NativeMarket: native.InstrumentID,
		Side: toUnifiedOKXSide(native.Side), Type: toUnifiedOKXOrderType(native.OrderType),
		Status: toUnifiedOKXStatus(native.State), Price: native.Price,
		Quantity: native.Quantity, ExecutedQuantity: native.ExecutedQuantity, Raw: native.Raw,
	}
}

func toUnifiedOKXOrderType(orderType OrderType) unified.OrderType {
	if orderType == OrderTypeMarket {
		return unified.OrderTypeMarket
	}
	return unified.OrderTypeLimit
}

func toUnifiedOKXStatus(status string) unified.OrderStatus {
	switch status {
	case "live":
		return unified.OrderStatusNew
	case "partially_filled":
		return unified.OrderStatusPartiallyFilled
	case "filled":
		return unified.OrderStatusFilled
	case "canceled", "mmp_canceled":
		return unified.OrderStatusCanceled
	default:
		return unified.OrderStatusUnknown
	}
}
