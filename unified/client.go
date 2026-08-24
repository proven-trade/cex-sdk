package unified

import (
	"context"

	trade "github.com/proven-trade/proven-trade-sdk"
	"github.com/proven-trade/proven-trade-sdk/model"
)

// SpotClient는 지원 거래소가 공통으로 제공하는 Spot 거래 기능이다.
type SpotClient interface {
	Exchange() model.ExchangeID
	Markets(context.Context, ...trade.RequestOption) ([]MarketInfo, error)
	Ticker(context.Context, TickerRequest, ...trade.RequestOption) (Ticker, error)
	OrderBook(context.Context, OrderBookRequest, ...trade.RequestOption) (OrderBook, error)
	RecentTrades(context.Context, RecentTradesRequest, ...trade.RequestOption) ([]PublicTrade, error)
	Candles(context.Context, CandlesRequest, ...trade.RequestOption) ([]Candle, error)
	Balances(context.Context, ...trade.RequestOption) ([]Balance, error)
	PlaceOrder(context.Context, PlaceOrderRequest, ...trade.RequestOption) (Order, error)
	Order(context.Context, OrderRequest, ...trade.RequestOption) (Order, error)
	CancelOrder(context.Context, OrderRequest, ...trade.RequestOption) (Order, error)
	OpenOrders(context.Context, OpenOrdersRequest, ...trade.RequestOption) ([]Order, error)
}
