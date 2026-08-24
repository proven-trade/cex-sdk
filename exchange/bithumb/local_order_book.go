package bithumb

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	trade "github.com/proven-trade/proven-trade-sdk"
	"github.com/proven-trade/proven-trade-sdk/transport"
)

const defaultLocalOrderBookViewDepth = 15

// LocalOrderBookConfig는 빗썸 Spot 전체 snapshot 오더북 설정이다.
type LocalOrderBookConfig struct {
	Market        string
	EgressRouteID transport.EgressRouteID
	ViewDepth     int
}

// LocalOrderBookLevel은 한 방향의 가격과 수량이다.
type LocalOrderBookLevel struct {
	Price    Decimal
	Quantity Decimal
}

// LocalOrderBookView는 독립 검증을 통과한 최신 전체 호가 snapshot이다.
type LocalOrderBookView struct {
	Market       string
	Generation   uint64
	SnapshotID   uint64
	Timestamp    int64
	StreamType   string
	TotalAskSize Decimal
	TotalBidSize Decimal
	Level        Decimal
	Bids         []LocalOrderBookLevel
	Asks         []LocalOrderBookLevel
}

// LocalOrderBookHandler는 검증된 전체 호가 snapshot을 처리한다.
type LocalOrderBookHandler func(context.Context, LocalOrderBookView) error

// LocalOrderBook은 빗썸의 완전한 SNAPSHOT·REALTIME 호가를 안전한 view로 변환한다.
type LocalOrderBook struct {
	market    string
	routeID   transport.EgressRouteID
	viewDepth int
}

// NewLocalOrderBook은 route 고정 orderbook stream용 snapshot 변환기를 생성한다.
func NewLocalOrderBook(config LocalOrderBookConfig) (*LocalOrderBook, error) {
	config.Market = strings.ToUpper(config.Market)
	if err := validateMarket(config.Market); err != nil {
		return nil, err
	}
	config.EgressRouteID = transport.EgressRouteID(strings.TrimSpace(string(config.EgressRouteID)))
	if config.EgressRouteID == "" {
		return nil, trade.ErrMissingEgressRoute
	}
	if config.ViewDepth == 0 {
		config.ViewDepth = defaultLocalOrderBookViewDepth
	}
	if config.ViewDepth < 1 || config.ViewDepth > 15 {
		return nil, validationError("local order book view depth must be between 1 and 15")
	}
	return &LocalOrderBook{
		market: config.Market, routeID: config.EgressRouteID, viewDepth: config.ViewDepth,
	}, nil
}

// Run은 orderbook stream을 소유하고 매 전체 snapshot을 검증해 handler에 전달한다.
func (book *LocalOrderBook) Run(
	ctx context.Context,
	public *PublicStream,
	handler LocalOrderBookHandler,
) error {
	if ctx == nil {
		return fmt.Errorf("Bithumb local order book context is nil")
	}
	if public == nil {
		return fmt.Errorf("Bithumb local order book public stream is required")
	}
	if handler == nil {
		return fmt.Errorf("Bithumb local order book handler is required")
	}
	if public.EgressRouteID() != book.routeID {
		return validationError("local order book and WebSocket routes must match")
	}
	if public.managed.request.Format != StreamFormatDefault {
		return validationError("local order book requires the DEFAULT WebSocket format")
	}
	if !public.hasOrderBookSubscription(book.market) {
		return validationError("public stream does not contain the required orderbook subscription")
	}

	var snapshotID uint64
	return public.Run(ctx, func(_ context.Context, message StreamMessage) error {
		if message.Error != nil {
			return fmt.Errorf(
				"Bithumb order book subscription failed with %s: %s",
				message.Error.Name,
				message.Error.Message,
			)
		}
		if message.Status != "" || message.Type != "orderbook" ||
			!strings.EqualFold(message.Code, book.market) {
			return nil
		}
		generation := public.Generation()
		if generation == 0 {
			return validationError("local order book WebSocket generation is zero")
		}
		var event StreamOrderBook
		if err := message.Decode(&event); err != nil {
			return err
		}
		if err := validateLocalOrderBookSnapshot(book.market, event); err != nil {
			return err
		}
		snapshotID++
		return handler(ctx, localOrderBookView(event, generation, snapshotID, book.viewDepth))
	})
}

func validateLocalOrderBookSnapshot(market string, event StreamOrderBook) error {
	if event.Type != "orderbook" || !strings.EqualFold(event.Code, market) {
		return validationError("local order book received an unexpected event")
	}
	if event.Timestamp <= 0 {
		return validationError("local order book timestamp is invalid")
	}
	if event.StreamType != "SNAPSHOT" && event.StreamType != "REALTIME" {
		return validationError("local order book stream type is invalid")
	}
	if len(event.OrderBook) < 1 || len(event.OrderBook) > 15 {
		return validationError("local order book must contain between 1 and 15 units")
	}
	if _, err := nonnegativeLocalOrderBookDecimal("total ask size", event.TotalAskSize); err != nil {
		return err
	}
	if _, err := nonnegativeLocalOrderBookDecimal("total bid size", event.TotalBidSize); err != nil {
		return err
	}
	if _, err := positiveLocalOrderBookDecimal("orderbook level", event.Level); err != nil {
		return err
	}
	var previousAsk, previousBid *big.Rat
	for _, unit := range event.OrderBook {
		ask, err := positiveLocalOrderBookDecimal("ask price", unit.AskPrice)
		if err != nil {
			return err
		}
		bid, err := positiveLocalOrderBookDecimal("bid price", unit.BidPrice)
		if err != nil {
			return err
		}
		if _, err := nonnegativeLocalOrderBookDecimal("ask size", unit.AskSize); err != nil {
			return err
		}
		if _, err := nonnegativeLocalOrderBookDecimal("bid size", unit.BidSize); err != nil {
			return err
		}
		if previousAsk != nil && ask.Cmp(previousAsk) <= 0 {
			return validationError("local order book asks are not strictly ascending")
		}
		if previousBid != nil && bid.Cmp(previousBid) >= 0 {
			return validationError("local order book bids are not strictly descending")
		}
		previousAsk = ask
		previousBid = bid
	}
	return nil
}

func nonnegativeLocalOrderBookDecimal(name string, value Decimal) (*big.Rat, error) {
	parsed, ok := new(big.Rat).SetString(string(value))
	if !ok || parsed.Sign() < 0 {
		return nil, validationError("%s must be a nonnegative decimal", name)
	}
	return parsed, nil
}

func positiveLocalOrderBookDecimal(name string, value Decimal) (*big.Rat, error) {
	parsed, err := nonnegativeLocalOrderBookDecimal(name, value)
	if err != nil || parsed.Sign() == 0 {
		if err != nil {
			return nil, err
		}
		return nil, validationError("%s must be a positive decimal", name)
	}
	return parsed, nil
}

func localOrderBookView(
	event StreamOrderBook,
	generation uint64,
	snapshotID uint64,
	depth int,
) LocalOrderBookView {
	if len(event.OrderBook) < depth {
		depth = len(event.OrderBook)
	}
	bids := make([]LocalOrderBookLevel, depth)
	asks := make([]LocalOrderBookLevel, depth)
	for index := 0; index < depth; index++ {
		unit := event.OrderBook[index]
		bids[index] = LocalOrderBookLevel{Price: unit.BidPrice, Quantity: unit.BidSize}
		asks[index] = LocalOrderBookLevel{Price: unit.AskPrice, Quantity: unit.AskSize}
	}
	return LocalOrderBookView{
		Market: event.Code, Generation: generation, SnapshotID: snapshotID,
		Timestamp: event.Timestamp, StreamType: event.StreamType,
		TotalAskSize: event.TotalAskSize, TotalBidSize: event.TotalBidSize,
		Level: event.Level, Bids: bids, Asks: asks,
	}
}
