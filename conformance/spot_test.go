package conformance

import (
	"context"
	"encoding/json"
	"testing"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/model"
	"github.com/proven-trade/cex-sdk/unified"
)

type fixtureSpotClient struct {
	exchange model.ExchangeID
	market   unified.MarketInfo
	ticker   unified.Ticker
	book     unified.OrderBook
	trades   []unified.PublicTrade
	candles  []unified.Candle
	balances []unified.Balance
	placed   unified.Order
	order    unified.Order
	canceled unified.Order
	open     []unified.Order
}

func (client *fixtureSpotClient) Exchange() model.ExchangeID { return client.exchange }
func (client *fixtureSpotClient) Markets(context.Context, ...trade.RequestOption) ([]unified.MarketInfo, error) {
	return []unified.MarketInfo{client.market}, nil
}
func (client *fixtureSpotClient) Ticker(context.Context, unified.TickerRequest, ...trade.RequestOption) (unified.Ticker, error) {
	return client.ticker, nil
}
func (client *fixtureSpotClient) OrderBook(context.Context, unified.OrderBookRequest, ...trade.RequestOption) (unified.OrderBook, error) {
	return client.book, nil
}
func (client *fixtureSpotClient) RecentTrades(context.Context, unified.RecentTradesRequest, ...trade.RequestOption) ([]unified.PublicTrade, error) {
	return client.trades, nil
}
func (client *fixtureSpotClient) Candles(context.Context, unified.CandlesRequest, ...trade.RequestOption) ([]unified.Candle, error) {
	return client.candles, nil
}
func (client *fixtureSpotClient) Balances(context.Context, ...trade.RequestOption) ([]unified.Balance, error) {
	return client.balances, nil
}
func (client *fixtureSpotClient) PlaceOrder(context.Context, unified.PlaceOrderRequest, ...trade.RequestOption) (unified.Order, error) {
	return client.placed, nil
}
func (client *fixtureSpotClient) Order(context.Context, unified.OrderRequest, ...trade.RequestOption) (unified.Order, error) {
	return client.order, nil
}
func (client *fixtureSpotClient) CancelOrder(context.Context, unified.OrderRequest, ...trade.RequestOption) (unified.Order, error) {
	return client.canceled, nil
}
func (client *fixtureSpotClient) OpenOrders(context.Context, unified.OpenOrdersRequest, ...trade.RequestOption) ([]unified.Order, error) {
	return client.open, nil
}

func TestSpotSuitesCoverEverySpotClientMethod(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`{"fixture":true}`)
	market := unified.Market{Base: "BTC", Quote: "USDT"}
	marketInfo := unified.MarketInfo{
		Exchange: model.ExchangeBinance, Market: market, NativeMarket: "BTCUSDT", Status: "TRADING",
		PriceIncrement: "0.1", QuantityIncrement: "0.001", QuoteAmountIncrement: "0.01",
		MinimumBaseQuantity: "0.001", MinimumQuoteAmount: "10", Raw: raw,
	}
	book := unified.OrderBook{
		Exchange: model.ExchangeBinance, Market: market, NativeMarket: "BTCUSDT",
		Bids: []unified.BookLevel{{Price: "64000", Quantity: "1"}},
		Asks: []unified.BookLevel{{Price: "64001", Quantity: "2"}}, Timestamp: 1, Raw: raw,
	}
	trades := []unified.PublicTrade{{ID: "trade-1", Price: "64000", Quantity: "0.1", Side: unified.SideBuy, Timestamp: 1}}
	candles := []unified.Candle{{StartTime: 1, Open: "1", High: "2", Low: "1", Close: "2", Volume: "3"}}
	placed := unified.Order{
		Exchange: model.ExchangeBinance, ID: "order-1", ClientOrderID: "client-1",
		Market: market, NativeMarket: "BTCUSDT", Side: unified.SideBuy, Type: unified.OrderTypeMarket,
		Status: unified.OrderStatusAcknowledged, QuoteAmount: "100", Raw: raw,
	}
	current := placed
	current.Status = unified.OrderStatusNew
	canceled := placed
	canceled.Status = unified.OrderStatusCancelPending
	client := &fixtureSpotClient{
		exchange: model.ExchangeBinance, market: marketInfo,
		ticker: unified.Ticker{Exchange: model.ExchangeBinance, Market: market, NativeMarket: "BTCUSDT", Price: "64000", Raw: raw},
		book:   book, trades: trades, candles: candles,
		balances: []unified.Balance{{Asset: "USDT", Available: "100", Raw: raw}},
		placed:   placed, order: current, canceled: canceled, open: []unified.Order{current},
	}
	RunSpotReadSuite(t, SpotReadScenario{
		Client: client, Exchange: model.ExchangeBinance, Market: market,
		Price: "64000", NativeMarket: "BTCUSDT", BalanceAsset: "USDT", BalanceAvailable: "100",
	})
	RunSpotOrderSuite(t, SpotOrderScenario{
		Client: client, Exchange: model.ExchangeBinance,
		Request: unified.PlaceOrderRequest{
			Market: market, Side: unified.SideBuy, Type: unified.OrderTypeMarket,
			QuoteAmount: "100", ClientOrderID: "client-1",
		},
		OrderID: "order-1", ClientOrderID: "client-1", NativeMarket: "BTCUSDT",
		Status: unified.OrderStatusAcknowledged,
	})
	RunSpotMarketDataSuite(t, SpotMarketDataScenario{
		Client: client, Exchange: model.ExchangeBinance, Market: market,
		MarketInfo: marketInfo, OrderBook: book, Trades: trades, Candles: candles,
		BookLimit: 1, TradeLimit: 1, CandleLimit: 1, Interval: unified.Candle1Minute,
	})
	RunSpotLifecycleSuite(t, SpotLifecycleScenario{
		Client:            client,
		OrderRequest:      unified.OrderRequest{Market: market, OrderID: "order-1"},
		CancelRequest:     unified.OrderRequest{Market: market, OrderID: "order-1"},
		OpenOrdersRequest: unified.OpenOrdersRequest{Market: &market},
		Order:             current, CanceledOrder: canceled, OpenOrders: []unified.Order{current},
	})
}
