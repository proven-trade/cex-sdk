package coinone

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	trade "github.com/proven-trade/proven-trade-sdk"
	"github.com/proven-trade/proven-trade-sdk/transport"
)

const defaultLocalOrderBookViewDepth = 16

// LocalOrderBookConfig는 코인원 Spot 전체 snapshot 오더북 설정이다.
type LocalOrderBookConfig struct {
	QuoteCurrency  string
	TargetCurrency string
	EgressRouteID  transport.EgressRouteID
	ViewDepth      int
}

// LocalOrderBookView는 source ID가 검증된 최신 전체 호가 snapshot이다.
type LocalOrderBookView struct {
	QuoteCurrency  string
	TargetCurrency string
	Generation     uint64
	SnapshotID     uint64
	Timestamp      int64
	SourceID       string
	Bids           []OrderBookLevel
	Asks           []OrderBookLevel
}

// LocalOrderBookHandler는 검증된 전체 호가 snapshot을 처리한다.
type LocalOrderBookHandler func(context.Context, LocalOrderBookView) error

// LocalOrderBook은 코인원의 전체 호가를 최신 source ID의 best-first view로 변환한다.
type LocalOrderBook struct {
	quoteCurrency  string
	targetCurrency string
	routeID        transport.EgressRouteID
	viewDepth      int
}

type localOrderBookProcessor struct {
	book         *LocalOrderBook
	generation   uint64
	snapshotID   uint64
	latestSource *big.Int
}

// NewLocalOrderBook은 route 고정 ORDERBOOK stream용 snapshot 변환기를 생성한다.
func NewLocalOrderBook(config LocalOrderBookConfig) (*LocalOrderBook, error) {
	config.QuoteCurrency = strings.ToUpper(config.QuoteCurrency)
	config.TargetCurrency = strings.ToUpper(config.TargetCurrency)
	if err := validatePair(config.QuoteCurrency, config.TargetCurrency); err != nil {
		return nil, err
	}
	config.EgressRouteID = transport.EgressRouteID(strings.TrimSpace(string(config.EgressRouteID)))
	if config.EgressRouteID == "" {
		return nil, trade.ErrMissingEgressRoute
	}
	if config.ViewDepth == 0 {
		config.ViewDepth = defaultLocalOrderBookViewDepth
	}
	if config.ViewDepth < 1 || config.ViewDepth > 16 {
		return nil, validationError("local order book view depth must be between 1 and 16")
	}
	return &LocalOrderBook{
		quoteCurrency: config.QuoteCurrency, targetCurrency: config.TargetCurrency,
		routeID: config.EgressRouteID, viewDepth: config.ViewDepth,
	}, nil
}

// Run은 ORDERBOOK stream을 소유하고 새 전체 snapshot만 handler에 전달한다.
func (book *LocalOrderBook) Run(
	ctx context.Context,
	public *PublicStream,
	handler LocalOrderBookHandler,
) error {
	if ctx == nil {
		return fmt.Errorf("Coinone local order book context is nil")
	}
	if public == nil {
		return fmt.Errorf("Coinone local order book public stream is required")
	}
	if handler == nil {
		return fmt.Errorf("Coinone local order book handler is required")
	}
	if public.EgressRouteID() != book.routeID {
		return validationError("local order book and WebSocket routes must match")
	}
	if !public.hasOrderBookSubscription(book.quoteCurrency, book.targetCurrency) {
		return validationError("public stream does not contain the required ORDERBOOK subscription")
	}

	processor := &localOrderBookProcessor{book: book}
	return public.Run(ctx, func(handlerContext context.Context, message StreamMessage) error {
		if message.ResponseType == "ERROR" {
			return fmt.Errorf(
				"Coinone order book subscription failed with code %d: %s",
				message.ErrorCode,
				message.ErrorMessage,
			)
		}
		if message.ResponseType != "DATA" || message.Channel != StreamChannelOrderBook {
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
		view, publish, err := processor.process(event, generation)
		if err != nil || !publish {
			return err
		}
		return handler(handlerContext, view)
	})
}

func (processor *localOrderBookProcessor) process(
	event StreamOrderBook,
	generation uint64,
) (LocalOrderBookView, bool, error) {
	sourceID, err := validateLocalOrderBookSnapshot(processor.book, event)
	if err != nil {
		return LocalOrderBookView{}, false, err
	}
	if processor.generation == generation && processor.latestSource != nil &&
		sourceID.Cmp(processor.latestSource) <= 0 {
		return LocalOrderBookView{}, false, nil
	}
	processor.generation = generation
	processor.latestSource = new(big.Int).Set(sourceID)
	processor.snapshotID++
	return localOrderBookView(event, generation, processor.snapshotID, processor.book.viewDepth), true, nil
}

func validateLocalOrderBookSnapshot(
	book *LocalOrderBook,
	event StreamOrderBook,
) (*big.Int, error) {
	if event.QuoteCurrency != book.quoteCurrency || event.TargetCurrency != book.targetCurrency {
		return nil, validationError("local order book received an unexpected currency pair")
	}
	if event.Timestamp <= 0 {
		return nil, validationError("local order book timestamp is invalid")
	}
	sourceID, ok := new(big.Int).SetString(event.ID, 10)
	if !ok || sourceID.Sign() <= 0 {
		return nil, validationError("local order book source ID must be a positive integer")
	}
	if len(event.Asks) > 16 || len(event.Bids) > 16 || len(event.Asks)+len(event.Bids) == 0 {
		return nil, validationError("local order book must contain at most 16 levels per side")
	}
	if err := validateLocalOrderBookLevels("asks", event.Asks); err != nil {
		return nil, err
	}
	if err := validateLocalOrderBookLevels("bids", event.Bids); err != nil {
		return nil, err
	}
	return sourceID, nil
}

func validateLocalOrderBookLevels(name string, levels []OrderBookLevel) error {
	var previous *big.Rat
	for _, level := range levels {
		price, err := positiveLocalOrderBookDecimal(name+" price", level.Price)
		if err != nil {
			return err
		}
		if _, err := nonnegativeLocalOrderBookDecimal(name+" quantity", level.Quantity); err != nil {
			return err
		}
		if previous != nil && price.Cmp(previous) >= 0 {
			return validationError("local order book %s are not strictly descending", name)
		}
		previous = price
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
	bidDepth := depth
	if len(event.Bids) < bidDepth {
		bidDepth = len(event.Bids)
	}
	askDepth := depth
	if len(event.Asks) < askDepth {
		askDepth = len(event.Asks)
	}
	bids := make([]OrderBookLevel, bidDepth)
	copy(bids, event.Bids[:bidDepth])
	asks := make([]OrderBookLevel, askDepth)
	for index := 0; index < askDepth; index++ {
		asks[index] = event.Asks[len(event.Asks)-1-index]
	}
	return LocalOrderBookView{
		QuoteCurrency: event.QuoteCurrency, TargetCurrency: event.TargetCurrency,
		Generation: generation, SnapshotID: snapshotID, Timestamp: event.Timestamp,
		SourceID: event.ID, Bids: bids, Asks: asks,
	}
}
