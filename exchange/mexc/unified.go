package mexc

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/model"
	"github.com/proven-trade/cex-sdk/unified"
)

const (
	mexcUnifiedDefaultOrderBookLimit = 16
	mexcUnifiedDefaultTradeLimit     = 100
	mexcUnifiedDefaultCandleLimit    = 200
	mexcUnifiedOpenOrderBatchSize    = 5
)

// UnifiedSpot은 MEXC Spot V3 native 클라이언트를 공통 Spot 계약으로 변환한다.
type UnifiedSpot struct {
	client *Client
}

var _ unified.SpotClient = (*UnifiedSpot)(nil)

// NewUnifiedSpot은 MEXC Spot 공통 어댑터를 생성한다.
func NewUnifiedSpot(client *Client) (*UnifiedSpot, error) {
	if client == nil {
		return nil, fmt.Errorf("MEXC client is required")
	}
	return &UnifiedSpot{client: client}, nil
}

// Exchange는 MEXC 거래소 식별자를 반환한다.
func (adapter *UnifiedSpot) Exchange() model.ExchangeID {
	return model.ExchangeMEXC
}

// Markets는 MEXC Spot 거래쌍과 거래 가능 상태를 공통 형식으로 조회한다.
func (adapter *UnifiedSpot) Markets(
	ctx context.Context,
	options ...trade.RequestOption,
) ([]unified.MarketInfo, error) {
	info, err := adapter.client.ExchangeInfo(ctx, ExchangeInfoRequest{}, options...)
	if err != nil {
		return nil, err
	}
	markets := make([]unified.MarketInfo, len(info.Symbols))
	for index, symbol := range info.Symbols {
		market, parseErr := marketFromMEXCSymbol(symbol)
		if parseErr != nil {
			return nil, parseErr
		}
		priceIncrement, quantityIncrement, minimumBase, minimumQuote, parseErr := mexcSpotRules(symbol.Filters)
		if parseErr != nil {
			return nil, fmt.Errorf("decode MEXC rules for %q: %w", symbol.Symbol, parseErr)
		}
		if quantityIncrement == "" {
			quantityIncrement = symbol.BaseSizePrecision
		}
		markets[index] = unified.MarketInfo{
			Exchange: model.ExchangeMEXC, Market: market, NativeMarket: symbol.Symbol,
			Status:         fromMEXCMarketStatus(symbol),
			PriceIncrement: priceIncrement, QuantityIncrement: quantityIncrement,
			MinimumBaseQuantity: minimumBase, MinimumQuoteAmount: minimumQuote, Raw: symbol.Raw,
		}
	}
	return markets, nil
}

func mexcSpotRules(filters []json.RawMessage) (string, string, string, string, error) {
	var priceIncrement, quantityIncrement, minimumBase, minimumQuote string
	for _, raw := range filters {
		filter := struct {
			Type        string `json:"filterType"`
			TickSize    string `json:"tickSize"`
			StepSize    string `json:"stepSize"`
			MinimumQty  string `json:"minQty"`
			MinimumCost string `json:"minNotional"`
		}{}
		if err := json.Unmarshal(raw, &filter); err != nil {
			return "", "", "", "", err
		}
		switch filter.Type {
		case "PRICE_FILTER":
			priceIncrement = filter.TickSize
		case "LOT_SIZE":
			quantityIncrement, minimumBase = filter.StepSize, filter.MinimumQty
		case "MIN_NOTIONAL", "NOTIONAL":
			minimumQuote = filter.MinimumCost
		}
	}
	return priceIncrement, quantityIncrement, minimumBase, minimumQuote, nil
}

// Ticker는 공통 마켓의 MEXC 최신 가격을 조회한다.
func (adapter *UnifiedSpot) Ticker(
	ctx context.Context,
	request unified.TickerRequest,
	options ...trade.RequestOption,
) (unified.Ticker, error) {
	if err := request.Validate(); err != nil {
		return unified.Ticker{}, err
	}
	nativeMarket := mexcSymbol(request.Market)
	native, err := adapter.client.PriceTicker(ctx, nativeMarket, options...)
	if err != nil {
		return unified.Ticker{}, err
	}
	if native.Symbol != nativeMarket {
		return unified.Ticker{}, fmt.Errorf(
			"MEXC ticker market %q does not match %q", native.Symbol, nativeMarket,
		)
	}
	return unified.Ticker{
		Exchange: model.ExchangeMEXC, Market: request.Market,
		NativeMarket: nativeMarket, Price: native.Price, Raw: native.Raw,
	}, nil
}

// OrderBook은 MEXC Spot 호가 스냅샷을 공통 형식으로 조회한다.
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
		limit = mexcUnifiedDefaultOrderBookLimit
	}
	nativeMarket := mexcSymbol(request.Market)
	native, err := adapter.client.OrderBook(ctx, OrderBookRequest{
		Symbol: nativeMarket, Limit: limit,
	}, options...)
	if err != nil {
		return unified.OrderBook{}, err
	}
	return unified.OrderBook{
		Exchange: model.ExchangeMEXC, Market: request.Market, NativeMarket: nativeMarket,
		Bids: fromMEXCBookLevels(native.Bids), Asks: fromMEXCBookLevels(native.Asks), Raw: native.Raw,
	}, nil
}

// RecentTrades는 MEXC Spot 공개 최근 체결을 공통 형식으로 조회한다.
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
		limit = mexcUnifiedDefaultTradeLimit
	}
	native, err := adapter.client.RecentTrades(ctx, TradesRequest{
		Symbol: mexcSymbol(request.Market), Limit: limit,
	}, options...)
	if err != nil {
		return nil, err
	}
	trades := make([]unified.PublicTrade, len(native))
	for index, item := range native {
		if item.Time < 0 {
			return nil, fmt.Errorf("invalid MEXC trade timestamp %d", item.Time)
		}
		side := unified.SideBuy
		if item.BuyerMaker {
			side = unified.SideSell
		}
		trades[index] = unified.PublicTrade{
			ID: string(item.ID), Price: item.Price, Quantity: item.Quantity,
			Side: side, Timestamp: item.Time,
		}
	}
	return trades, nil
}

// Candles는 MEXC Spot OHLCV 캔들을 공통 형식으로 조회한다.
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
		limit = mexcUnifiedDefaultCandleLimit
	}
	interval := toMEXCCandleInterval(request.Interval)
	nativeLimit := limit
	if request.Interval == unified.Candle3Minutes {
		interval = Candle1Minute
		nativeLimit *= 3
	}
	native, err := adapter.client.Candles(ctx, CandlesRequest{
		Symbol: mexcSymbol(request.Market), Interval: interval, Limit: nativeLimit,
	}, options...)
	if err != nil {
		return nil, err
	}
	candles := make([]unified.Candle, len(native))
	for index, item := range native {
		if item.OpenTime < 0 {
			return nil, fmt.Errorf("invalid MEXC candle timestamp %d", item.OpenTime)
		}
		candles[index] = unified.Candle{
			StartTime: item.OpenTime, Open: item.Open, High: item.High,
			Low: item.Low, Close: item.Close, Volume: item.BaseVolume,
		}
	}
	if request.Interval == unified.Candle3Minutes {
		return unified.AggregateCandles(candles, 3*time.Minute, limit)
	}
	return candles, nil
}

// Balances는 MEXC Spot 자산 잔고를 공통 형식으로 조회한다.
func (adapter *UnifiedSpot) Balances(
	ctx context.Context,
	options ...trade.RequestOption,
) ([]unified.Balance, error) {
	account, err := adapter.client.Account(ctx, options...)
	if err != nil {
		return nil, err
	}
	balances := make([]unified.Balance, len(account.Balances))
	for index, balance := range account.Balances {
		balances[index] = unified.Balance{
			Asset: balance.Asset, Available: balance.Free, Locked: balance.Locked, Raw: balance.Raw,
		}
	}
	return balances, nil
}

// PlaceOrder는 공통 Spot 주문을 MEXC 주문으로 변환해 생성한다.
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
		generated, err := newMEXCClientOrderID()
		if err != nil {
			return unified.Order{}, err
		}
		clientOrderID = generated
	}
	nativeRequest := PlaceOrderRequest{
		ClientOrderID: clientOrderID, Symbol: mexcSymbol(request.Market),
		Side: toMEXCSide(request.Side), Type: OrderTypeMarket,
	}
	if request.Type == unified.OrderTypeLimit {
		nativeRequest.Type = toMEXCLimitOrderType(request.TimeInForce)
		nativeRequest.Quantity = request.Quantity
		nativeRequest.Price = request.Price
	} else if request.Side == unified.SideBuy {
		nativeRequest.QuoteQuantity = request.QuoteAmount
	} else {
		nativeRequest.Quantity = request.Quantity
	}
	reference, err := adapter.client.PlaceOrder(ctx, nativeRequest, options...)
	if err != nil {
		return unified.Order{}, err
	}
	if reference.Symbol != nativeRequest.Symbol {
		return unified.Order{}, fmt.Errorf(
			"MEXC placed order market %q does not match %q", reference.Symbol, nativeRequest.Symbol,
		)
	}
	return unified.Order{
		Exchange: model.ExchangeMEXC, ID: string(reference.OrderID),
		ClientOrderID: clientOrderID, Market: request.Market, NativeMarket: nativeRequest.Symbol,
		Side: request.Side, Type: request.Type, Status: unified.OrderStatusAcknowledged,
		Price: request.Price, Quantity: request.Quantity, QuoteAmount: request.QuoteAmount, Raw: reference.Raw,
	}, nil
}

// Order는 MEXC 주문을 거래소 주문 ID 또는 사용자 주문 ID로 조회한다.
func (adapter *UnifiedSpot) Order(
	ctx context.Context,
	request unified.OrderRequest,
	options ...trade.RequestOption,
) (unified.Order, error) {
	if err := request.Validate(); err != nil {
		return unified.Order{}, err
	}
	native, err := adapter.client.OrderInfo(ctx, OrderInfoRequest{
		Symbol: mexcSymbol(request.Market), OrderID: request.OrderID,
		ClientOrderID: request.ClientOrderID,
	}, options...)
	if err != nil {
		return unified.Order{}, err
	}
	return fromMEXCOrder(native, request.Market)
}

// CancelOrder는 MEXC 주문 취소 결과를 공통 형식으로 반환한다.
func (adapter *UnifiedSpot) CancelOrder(
	ctx context.Context,
	request unified.OrderRequest,
	options ...trade.RequestOption,
) (unified.Order, error) {
	if err := request.Validate(); err != nil {
		return unified.Order{}, err
	}
	native, err := adapter.client.CancelOrder(ctx, CancelOrderRequest{
		Symbol: mexcSymbol(request.Market), OrderID: request.OrderID,
		ClientOrderID: request.ClientOrderID,
	}, options...)
	if err != nil {
		return unified.Order{}, err
	}
	return fromMEXCOrder(native, request.Market)
}

// OpenOrders는 MEXC 미체결 주문을 단일 또는 API Key 허용 전체 마켓에서 조회한다.
func (adapter *UnifiedSpot) OpenOrders(
	ctx context.Context,
	request unified.OpenOrdersRequest,
	options ...trade.RequestOption,
) ([]unified.Order, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	if request.Market != nil {
		nativeMarket := mexcSymbol(*request.Market)
		native, err := adapter.client.OpenOrders(ctx, OpenOrdersRequest{
			Symbol: nativeMarket,
		}, options...)
		if err != nil {
			return nil, err
		}
		return mapMEXCOrders(native, map[string]unified.Market{nativeMarket: *request.Market})
	}
	return adapter.allOpenOrders(ctx, options...)
}

func (adapter *UnifiedSpot) allOpenOrders(
	ctx context.Context,
	options ...trade.RequestOption,
) ([]unified.Order, error) {
	symbols, err := adapter.client.SelfSymbols(ctx, options...)
	if err != nil {
		return nil, err
	}
	if len(symbols) == 0 {
		return []unified.Order{}, nil
	}
	info, err := adapter.client.ExchangeInfo(ctx, ExchangeInfoRequest{}, options...)
	if err != nil {
		return nil, err
	}
	markets := make(map[string]unified.Market, len(info.Symbols))
	for _, symbol := range info.Symbols {
		market, parseErr := marketFromMEXCSymbol(symbol)
		if parseErr != nil {
			return nil, parseErr
		}
		markets[symbol.Symbol] = market
	}
	seen := make(map[string]struct{}, len(symbols))
	var orders []unified.Order
	for start := 0; start < len(symbols); start += mexcUnifiedOpenOrderBatchSize {
		end := start + mexcUnifiedOpenOrderBatchSize
		if end > len(symbols) {
			end = len(symbols)
		}
		batch := symbols[start:end]
		requested := make(map[string]unified.Market, len(batch))
		for _, symbol := range batch {
			if _, exists := seen[symbol]; exists {
				return nil, fmt.Errorf("MEXC self symbols contains duplicate %q", symbol)
			}
			seen[symbol] = struct{}{}
			market, exists := markets[symbol]
			if !exists {
				return nil, fmt.Errorf("MEXC self symbol %q is missing from exchange info", symbol)
			}
			requested[symbol] = market
		}
		native, queryErr := adapter.client.OpenOrders(ctx, OpenOrdersRequest{
			Symbols: append([]string(nil), batch...),
		}, options...)
		if queryErr != nil {
			return nil, queryErr
		}
		mapped, mapErr := mapMEXCOrders(native, requested)
		if mapErr != nil {
			return nil, mapErr
		}
		orders = append(orders, mapped...)
	}
	return orders, nil
}

func mexcSymbol(market unified.Market) string {
	return market.Base + market.Quote
}

func marketFromMEXCSymbol(symbol Symbol) (unified.Market, error) {
	market := unified.Market{Base: symbol.BaseAsset, Quote: symbol.QuoteAsset}
	if err := market.Validate(); err != nil || mexcSymbol(market) != symbol.Symbol {
		return unified.Market{}, fmt.Errorf("invalid MEXC Spot symbol %q", symbol.Symbol)
	}
	return market, nil
}

func fromMEXCMarketStatus(symbol Symbol) string {
	if !symbol.SpotTradingAllowed || symbol.TradeSideType == "4" {
		return "disabled"
	}
	switch symbol.TradeSideType {
	case "2":
		return "buy_only"
	case "3":
		return "sell_only"
	}
	switch symbol.Status {
	case "1", "ENABLED", "TRADING":
		return "trading"
	case "2", "PAUSE", "PAUSED":
		return "paused"
	default:
		return "disabled"
	}
}

func fromMEXCBookLevels(native []BookLevel) []unified.BookLevel {
	levels := make([]unified.BookLevel, len(native))
	for index, level := range native {
		levels[index] = unified.BookLevel{Price: level.Price, Quantity: level.Quantity}
	}
	return levels
}

func toMEXCCandleInterval(value unified.CandleInterval) CandleInterval {
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

func toMEXCSide(side unified.Side) Side {
	if side == unified.SideBuy {
		return SideBuy
	}
	return SideSell
}

func toMEXCLimitOrderType(value unified.TimeInForce) OrderType {
	switch value {
	case unified.TimeInForceIOC:
		return OrderTypeImmediateOrCancel
	case unified.TimeInForceFOK:
		return OrderTypeFillOrKill
	case unified.TimeInForcePostOnly:
		return OrderTypeLimitMaker
	default:
		return OrderTypeLimit
	}
}

func fromMEXCOrder(native Order, market unified.Market) (unified.Order, error) {
	nativeMarket := mexcSymbol(market)
	if native.Symbol != nativeMarket {
		return unified.Order{}, fmt.Errorf(
			"MEXC order market %q does not match %q", native.Symbol, nativeMarket,
		)
	}
	side, err := fromMEXCSide(native.Side)
	if err != nil {
		return unified.Order{}, err
	}
	orderType, err := fromMEXCOrderType(native.Type)
	if err != nil {
		return unified.Order{}, err
	}
	status, err := fromMEXCOrderStatus(native.Status)
	if err != nil {
		return unified.Order{}, err
	}
	clientOrderID := string(native.ClientOrderID)
	if native.OriginalClientOrderID != "" {
		clientOrderID = string(native.OriginalClientOrderID)
	}
	return unified.Order{
		Exchange: model.ExchangeMEXC, ID: string(native.OrderID), ClientOrderID: clientOrderID,
		Market: market, NativeMarket: native.Symbol, Side: side, Type: orderType, Status: status,
		Price: native.Price, Quantity: native.OriginalQuantity,
		QuoteAmount:      native.OriginalQuoteOrderQuantity,
		ExecutedQuantity: native.ExecutedQuantity, Raw: native.Raw,
	}, nil
}

func mapMEXCOrders(
	native []Order,
	markets map[string]unified.Market,
) ([]unified.Order, error) {
	orders := make([]unified.Order, len(native))
	for index, item := range native {
		market, exists := markets[item.Symbol]
		if !exists {
			return nil, fmt.Errorf("MEXC open orders contains unexpected symbol %q", item.Symbol)
		}
		mapped, err := fromMEXCOrder(item, market)
		if err != nil {
			return nil, err
		}
		orders[index] = mapped
	}
	return orders, nil
}

func fromMEXCSide(side Side) (unified.Side, error) {
	switch side {
	case SideBuy:
		return unified.SideBuy, nil
	case SideSell:
		return unified.SideSell, nil
	default:
		return "", fmt.Errorf("unsupported MEXC side %q", side)
	}
}

func fromMEXCOrderType(orderType OrderType) (unified.OrderType, error) {
	switch orderType {
	case OrderTypeMarket:
		return unified.OrderTypeMarket, nil
	case OrderTypeLimit, OrderTypeLimitMaker, OrderTypeImmediateOrCancel, OrderTypeFillOrKill:
		return unified.OrderTypeLimit, nil
	default:
		return "", fmt.Errorf("unsupported MEXC order type %q", orderType)
	}
}

func fromMEXCOrderStatus(status OrderStatus) (unified.OrderStatus, error) {
	switch status {
	case OrderStatusNew:
		return unified.OrderStatusNew, nil
	case OrderStatusPartiallyFilled:
		return unified.OrderStatusPartiallyFilled, nil
	case OrderStatusFilled:
		return unified.OrderStatusFilled, nil
	case OrderStatusCanceled, OrderStatusPartiallyCanceled:
		return unified.OrderStatusCanceled, nil
	default:
		return unified.OrderStatusUnknown, nil
	}
}

func newMEXCClientOrderID() (string, error) {
	value := make([]byte, 12)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate MEXC client order ID: %w", err)
	}
	return "proven-" + hex.EncodeToString(value), nil
}
