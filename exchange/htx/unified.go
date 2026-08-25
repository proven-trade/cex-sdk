package htx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math"
	"strings"
	"time"

	trade "github.com/proven-trade/proven-trade-sdk"
	"github.com/proven-trade/proven-trade-sdk/model"
	"github.com/proven-trade/proven-trade-sdk/unified"
)

const (
	htxUnifiedDefaultOrderBookLimit = 16
	htxUnifiedDefaultTradeLimit     = 100
	htxUnifiedDefaultCandleLimit    = 200
	htxUnifiedOpenOrderPageSize     = 500
)

// UnifiedSpot은 HTX Spot native 클라이언트를 공통 Spot 계약으로 변환한다.
type UnifiedSpot struct {
	client *Client
}

var _ unified.SpotClient = (*UnifiedSpot)(nil)

// NewUnifiedSpot은 HTX Spot 공통 어댑터를 생성한다.
func NewUnifiedSpot(client *Client) (*UnifiedSpot, error) {
	if client == nil {
		return nil, fmt.Errorf("HTX client is required")
	}
	return &UnifiedSpot{client: client}, nil
}

// Exchange는 HTX 거래소 식별자를 반환한다.
func (adapter *UnifiedSpot) Exchange() model.ExchangeID {
	return model.ExchangeHTX
}

// Markets는 HTX Spot 거래쌍과 거래 가능 상태를 공통 형식으로 조회한다.
func (adapter *UnifiedSpot) Markets(
	ctx context.Context,
	options ...trade.RequestOption,
) ([]unified.MarketInfo, error) {
	native, err := adapter.client.MarketSymbols(ctx, MarketSymbolsRequest{}, options...)
	if err != nil {
		return nil, err
	}
	markets := make([]unified.MarketInfo, len(native.Symbols))
	for index, symbol := range native.Symbols {
		market, parseErr := marketFromHTXSymbol(symbol)
		if parseErr != nil {
			return nil, parseErr
		}
		markets[index] = unified.MarketInfo{
			Exchange: model.ExchangeHTX, Market: market, NativeMarket: symbol.Symbol,
			Status: fromHTXMarketStatus(symbol), Raw: symbol.Raw,
		}
	}
	return markets, nil
}

// Ticker는 공통 마켓의 HTX 최신 체결 가격을 조회한다.
func (adapter *UnifiedSpot) Ticker(
	ctx context.Context,
	request unified.TickerRequest,
	options ...trade.RequestOption,
) (unified.Ticker, error) {
	if err := request.Validate(); err != nil {
		return unified.Ticker{}, err
	}
	nativeMarket := htxSymbol(request.Market)
	native, err := adapter.client.Ticker(ctx, nativeMarket, options...)
	if err != nil {
		return unified.Ticker{}, err
	}
	return unified.Ticker{
		Exchange: model.ExchangeHTX, Market: request.Market,
		NativeMarket: nativeMarket, Price: native.Close.String(), Raw: native.Raw,
	}, nil
}

// OrderBook은 HTX Spot 호가 스냅샷을 공통 형식으로 조회한다.
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
		limit = htxUnifiedDefaultOrderBookLimit
	}
	nativeDepth := 20
	if limit <= 5 {
		nativeDepth = 5
	} else if limit <= 10 {
		nativeDepth = 10
	}
	nativeMarket := htxSymbol(request.Market)
	native, err := adapter.client.OrderBook(ctx, OrderBookRequest{
		Symbol: nativeMarket, Depth: nativeDepth, Type: DepthStep0,
	}, options...)
	if err != nil {
		return unified.OrderBook{}, err
	}
	if native.Timestamp < 0 {
		return unified.OrderBook{}, fmt.Errorf("invalid HTX order book timestamp %d", native.Timestamp)
	}
	return unified.OrderBook{
		Exchange: model.ExchangeHTX, Market: request.Market, NativeMarket: nativeMarket,
		Bids: fromHTXBookLevels(native.Bids, limit), Asks: fromHTXBookLevels(native.Asks, limit),
		Timestamp: native.Timestamp, Raw: native.Raw,
	}, nil
}

// RecentTrades는 HTX Spot 공개 최근 체결 묶음을 공통 체결 목록으로 펼친다.
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
		limit = htxUnifiedDefaultTradeLimit
	}
	native, err := adapter.client.RecentTrades(ctx, TradesRequest{
		Symbol: htxSymbol(request.Market), Size: limit,
	}, options...)
	if err != nil {
		return nil, err
	}
	trades := make([]unified.PublicTrade, 0, limit)
	for _, batch := range native {
		for _, item := range batch.Trades {
			if item.Timestamp < 0 {
				return nil, fmt.Errorf("invalid HTX trade timestamp %d", item.Timestamp)
			}
			side, parseErr := fromHTXTradeDirection(item.Direction)
			if parseErr != nil {
				return nil, parseErr
			}
			tradeID := string(item.TradeID)
			if tradeID == "" {
				tradeID = string(item.ID)
			}
			trades = append(trades, unified.PublicTrade{
				ID: tradeID, Price: item.Price.String(), Quantity: item.Amount.String(),
				Side: side, Timestamp: item.Timestamp,
			})
			if len(trades) == limit {
				return trades, nil
			}
		}
	}
	return trades, nil
}

// Candles는 HTX Spot OHLCV를 조회하고 3분봉은 1분봉으로 합성한다.
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
		limit = htxUnifiedDefaultCandleLimit
	}
	nativeLimit := limit
	interval := toHTXCandleInterval(request.Interval)
	if request.Interval == unified.Candle3Minutes {
		interval = Candle1Minute
		nativeLimit *= 3
	}
	native, err := adapter.client.Candles(ctx, CandlesRequest{
		Symbol: htxSymbol(request.Market), Interval: interval, Size: nativeLimit,
	}, options...)
	if err != nil {
		return nil, err
	}
	candles := make([]unified.Candle, len(native))
	for index, item := range native {
		if item.OpenTime < 0 || item.OpenTime > math.MaxInt64/1000 {
			return nil, fmt.Errorf("invalid HTX candle timestamp %d", item.OpenTime)
		}
		candles[index] = unified.Candle{
			StartTime: item.OpenTime * 1000, Open: item.Open.String(), High: item.High.String(),
			Low: item.Low.String(), Close: item.Close.String(), Volume: item.BaseVolume.String(),
		}
	}
	if request.Interval == unified.Candle3Minutes {
		return unified.AggregateCandles(candles, 3*time.Minute, limit)
	}
	return candles, nil
}

// Balances는 현재 working Spot 계정의 주문 가능·잠금 잔고를 공통 형식으로 조회한다.
func (adapter *UnifiedSpot) Balances(
	ctx context.Context,
	options ...trade.RequestOption,
) ([]unified.Balance, error) {
	accountID, err := adapter.spotAccountID(ctx, options...)
	if err != nil {
		return nil, err
	}
	native, err := adapter.client.AccountBalance(ctx, accountID, options...)
	if err != nil {
		return nil, err
	}
	type balanceIndex struct {
		index int
	}
	indexes := make(map[string]balanceIndex)
	balances := make([]unified.Balance, 0, len(native.Balances))
	for _, balance := range native.Balances {
		switch balance.Type {
		case "trade", "frozen", "lock", "bank":
		case "loan", "interest":
			continue
		default:
			return nil, fmt.Errorf("unsupported HTX balance type %q", balance.Type)
		}
		asset := strings.ToUpper(balance.Currency)
		if asset == "" || strings.ToLower(asset) != balance.Currency {
			return nil, fmt.Errorf("invalid HTX balance currency %q", balance.Currency)
		}
		position, exists := indexes[asset]
		if !exists {
			position.index = len(balances)
			indexes[asset] = position
			balances = append(balances, unified.Balance{
				Asset: asset, Available: "0", Locked: "0", Raw: native.Raw,
			})
		}
		current := &balances[position.index]
		switch balance.Type {
		case "trade":
			current.Available, err = unified.AddDecimals(current.Available, balance.Balance.String())
		case "frozen", "lock", "bank":
			current.Locked, err = unified.AddDecimals(current.Locked, balance.Balance.String())
		}
		if err != nil {
			return nil, fmt.Errorf("map HTX %s balance: %w", asset, err)
		}
	}
	return balances, nil
}

// PlaceOrder는 공통 Spot 주문을 HTX 주문으로 변환해 생성한다.
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
		generated, err := newHTXClientOrderID()
		if err != nil {
			return unified.Order{}, err
		}
		clientOrderID = generated
	}
	accountID, err := adapter.spotAccountID(ctx, options...)
	if err != nil {
		return unified.Order{}, err
	}
	nativeRequest := PlaceOrderRequest{
		AccountID: accountID, ClientOrderID: clientOrderID,
		Symbol: htxSymbol(request.Market), Side: toHTXSide(request.Side),
		Kind: OrderKindMarket,
	}
	if request.Type == unified.OrderTypeLimit {
		nativeRequest.Kind = toHTXOrderKind(request.TimeInForce)
		nativeRequest.Amount = request.Quantity
		nativeRequest.Price = request.Price
	} else if request.Side == unified.SideBuy {
		nativeRequest.Amount = request.QuoteAmount
	} else {
		nativeRequest.Amount = request.Quantity
	}
	reference, err := adapter.client.PlaceOrder(ctx, nativeRequest, options...)
	if err != nil {
		return unified.Order{}, err
	}
	return unified.Order{
		Exchange: model.ExchangeHTX, ID: string(reference.OrderID), ClientOrderID: clientOrderID,
		Market: request.Market, NativeMarket: nativeRequest.Symbol,
		Side: request.Side, Type: request.Type, Status: unified.OrderStatusNew,
		Price: request.Price, Quantity: request.Quantity, Raw: reference.Raw,
	}, nil
}

// Order는 HTX 주문을 거래소 주문 ID 또는 사용자 주문 ID로 조회한다.
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
	return fromHTXOrder(native, request.Market)
}

// CancelOrder는 HTX 주문 취소 접수 결과를 공통 형식으로 반환한다.
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
		Symbol: htxSymbol(request.Market),
	}, options...)
	if err != nil {
		return unified.Order{}, err
	}
	orderID := string(reference.OrderID)
	if orderID == "" {
		orderID = request.OrderID
	}
	clientOrderID := reference.ClientOrderID
	if clientOrderID == "" {
		clientOrderID = request.ClientOrderID
	}
	return unified.Order{
		Exchange: model.ExchangeHTX, ID: orderID, ClientOrderID: clientOrderID,
		Market: request.Market, NativeMarket: htxSymbol(request.Market),
		Status: unified.OrderStatusCanceled, Raw: reference.Raw,
	}, nil
}

// OpenOrders는 HTX 미체결 주문을 단일 또는 전체 Spot 마켓에서 끝까지 조회한다.
func (adapter *UnifiedSpot) OpenOrders(
	ctx context.Context,
	request unified.OpenOrdersRequest,
	options ...trade.RequestOption,
) ([]unified.Order, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	markets := make(map[string]unified.Market)
	if request.Market != nil {
		markets[htxSymbol(*request.Market)] = *request.Market
	} else {
		items, err := adapter.Markets(ctx, options...)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			markets[item.NativeMarket] = item.Market
		}
	}
	accountID, err := adapter.spotAccountID(ctx, options...)
	if err != nil {
		return nil, err
	}
	nativeRequest := OpenOrdersRequest{AccountID: accountID, Size: htxUnifiedOpenOrderPageSize}
	if request.Market != nil {
		nativeRequest.Symbol = htxSymbol(*request.Market)
	}
	var orders []unified.Order
	seenCursors := make(map[string]struct{})
	for page := 0; page < 100; page++ {
		native, err := adapter.client.OpenOrders(ctx, nativeRequest, options...)
		if err != nil {
			return nil, err
		}
		mapped, err := mapHTXOrders(native, markets)
		if err != nil {
			return nil, err
		}
		orders = append(orders, mapped...)
		if len(native) < htxUnifiedOpenOrderPageSize {
			return orders, nil
		}
		cursor := string(native[len(native)-1].ID)
		if cursor == "" {
			return nil, fmt.Errorf("HTX open order page is missing a cursor")
		}
		if _, exists := seenCursors[cursor]; exists {
			return nil, fmt.Errorf("HTX open order cursor %q repeated", cursor)
		}
		seenCursors[cursor] = struct{}{}
		nativeRequest.From = cursor
		nativeRequest.Direction = QueryDirectionNext
	}
	return nil, fmt.Errorf("HTX open orders exceed the supported page limit")
}

func (adapter *UnifiedSpot) spotAccountID(
	ctx context.Context,
	options ...trade.RequestOption,
) (string, error) {
	accounts, err := adapter.client.Accounts(ctx, options...)
	if err != nil {
		return "", err
	}
	accountID := ""
	for _, account := range accounts {
		if account.Type != "spot" || account.State != "working" {
			continue
		}
		if accountID != "" {
			return "", fmt.Errorf("HTX returned multiple working Spot accounts")
		}
		accountID = string(account.ID)
	}
	if accountID == "" {
		return "", fmt.Errorf("HTX working Spot account was not found")
	}
	return accountID, nil
}

func htxSymbol(market unified.Market) string {
	return strings.ToLower(market.Base + market.Quote)
}

func marketFromHTXSymbol(symbol MarketSymbol) (unified.Market, error) {
	market := unified.Market{
		Base: strings.ToUpper(symbol.BaseCurrency), Quote: strings.ToUpper(symbol.QuoteCurrency),
	}
	if err := market.Validate(); err != nil || htxSymbol(market) != symbol.Symbol {
		return unified.Market{}, fmt.Errorf("invalid HTX Spot symbol %q", symbol.Symbol)
	}
	return market, nil
}

func fromHTXMarketStatus(symbol MarketSymbol) string {
	if symbol.State == "online" && symbol.APITrading == "enabled" {
		return "trading"
	}
	return "disabled"
}

func fromHTXBookLevels(native []BookLevel, limit int) []unified.BookLevel {
	if len(native) > limit {
		native = native[:limit]
	}
	levels := make([]unified.BookLevel, len(native))
	for index, level := range native {
		levels[index] = unified.BookLevel{
			Price: level.Price.String(), Quantity: level.Quantity.String(),
		}
	}
	return levels
}

func fromHTXTradeDirection(direction TradeDirection) (unified.Side, error) {
	switch direction {
	case TradeDirectionBuy:
		return unified.SideBuy, nil
	case TradeDirectionSell:
		return unified.SideSell, nil
	default:
		return "", fmt.Errorf("unsupported HTX trade direction %q", direction)
	}
}

func toHTXCandleInterval(value unified.CandleInterval) CandleInterval {
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

func toHTXSide(side unified.Side) Side {
	if side == unified.SideBuy {
		return SideBuy
	}
	return SideSell
}

func toHTXOrderKind(value unified.TimeInForce) OrderKind {
	switch value {
	case unified.TimeInForceIOC:
		return OrderKindIOC
	case unified.TimeInForceFOK:
		return OrderKindLimitFOK
	case unified.TimeInForcePostOnly:
		return OrderKindLimitMaker
	default:
		return OrderKindLimit
	}
}

func fromHTXOrder(native Order, market unified.Market) (unified.Order, error) {
	nativeMarket := htxSymbol(market)
	if native.Symbol != nativeMarket {
		return unified.Order{}, fmt.Errorf(
			"HTX order market %q does not match %q", native.Symbol, nativeMarket,
		)
	}
	side, orderType, err := fromHTXOrderType(native.Type)
	if err != nil {
		return unified.Order{}, err
	}
	return unified.Order{
		Exchange: model.ExchangeHTX, ID: string(native.ID), ClientOrderID: native.ClientOrderID,
		Market: market, NativeMarket: native.Symbol, Side: side, Type: orderType,
		Status: fromHTXOrderStatus(native.State), Price: native.Price.String(),
		Quantity: native.Amount.String(), ExecutedQuantity: native.FilledAmount.String(), Raw: native.Raw,
	}, nil
}

func mapHTXOrders(
	native []Order,
	markets map[string]unified.Market,
) ([]unified.Order, error) {
	orders := make([]unified.Order, len(native))
	for index, item := range native {
		market, exists := markets[item.Symbol]
		if !exists {
			return nil, fmt.Errorf("HTX open orders contains unexpected symbol %q", item.Symbol)
		}
		mapped, err := fromHTXOrder(item, market)
		if err != nil {
			return nil, err
		}
		orders[index] = mapped
	}
	return orders, nil
}

func fromHTXOrderType(orderType OrderType) (unified.Side, unified.OrderType, error) {
	side := unified.SideBuy
	if strings.HasPrefix(string(orderType), "sell-") {
		side = unified.SideSell
	} else if !strings.HasPrefix(string(orderType), "buy-") {
		return "", "", fmt.Errorf("unsupported HTX order type %q", orderType)
	}
	if orderType == OrderTypeBuyMarket || orderType == OrderTypeSellMarket {
		return side, unified.OrderTypeMarket, nil
	}
	if !orderType.valid() {
		return "", "", fmt.Errorf("unsupported HTX order type %q", orderType)
	}
	return side, unified.OrderTypeLimit, nil
}

func fromHTXOrderStatus(status OrderState) unified.OrderStatus {
	switch status {
	case OrderStateCreated, OrderStateSubmitted:
		return unified.OrderStatusNew
	case OrderStatePartialFilled:
		return unified.OrderStatusPartiallyFilled
	case OrderStateFilled:
		return unified.OrderStatusFilled
	case OrderStatePartialCanceled, OrderStateCanceled:
		return unified.OrderStatusCanceled
	default:
		return unified.OrderStatusUnknown
	}
}

func newHTXClientOrderID() (string, error) {
	random := make([]byte, 12)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate HTX client order ID: %w", err)
	}
	return "proven-" + hex.EncodeToString(random), nil
}
