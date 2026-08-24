package korbit

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	trade "github.com/proven-trade/proven-trade-sdk"
	"github.com/proven-trade/proven-trade-sdk/transport"
)

const defaultLocalOrderBookViewDepth = 30

// LocalOrderBookConfig는 코빗 Spot 전체 snapshot 오더북 설정이다.
type LocalOrderBookConfig struct {
	Symbol        string
	Level         string
	EgressRouteID transport.EgressRouteID
	ViewDepth     int
}

// LocalOrderBookView는 독립 검증을 통과한 최신 전체 호가 snapshot이다.
type LocalOrderBookView struct {
	Symbol            string
	Level             string
	Generation        uint64
	SnapshotID        uint64
	EnvelopeTimestamp int64
	Timestamp         int64
	Snapshot          bool
	Bids              []OrderBookLevel
	Asks              []OrderBookLevel
}

// LocalOrderBookHandler는 검증된 전체 호가 snapshot을 처리한다.
type LocalOrderBookHandler func(context.Context, LocalOrderBookView) error

// LocalOrderBook은 코빗의 SNAPSHOT·실시간 전체 호가를 best-first view로 변환한다.
type LocalOrderBook struct {
	symbol    string
	level     string
	routeID   transport.EgressRouteID
	viewDepth int
}

type localOrderBookProcessor struct {
	book            *LocalOrderBook
	generation      uint64
	snapshotID      uint64
	latestTimestamp int64
}

// NewLocalOrderBook은 route와 호가 묶음 단위가 고정된 snapshot 변환기를 생성한다.
func NewLocalOrderBook(config LocalOrderBookConfig) (*LocalOrderBook, error) {
	config.Symbol = strings.ToLower(config.Symbol)
	if err := validateSymbol(config.Symbol); err != nil {
		return nil, err
	}
	if err := validateOptionalPositiveDecimal("local order book level", config.Level); err != nil {
		return nil, err
	}
	config.EgressRouteID = transport.EgressRouteID(strings.TrimSpace(string(config.EgressRouteID)))
	if config.EgressRouteID == "" {
		return nil, trade.ErrMissingEgressRoute
	}
	if config.ViewDepth == 0 {
		config.ViewDepth = defaultLocalOrderBookViewDepth
	}
	if config.ViewDepth < 1 || config.ViewDepth > 30 {
		return nil, validationError("local order book view depth must be between 1 and 30")
	}
	return &LocalOrderBook{
		symbol: config.Symbol, level: config.Level,
		routeID: config.EgressRouteID, viewDepth: config.ViewDepth,
	}, nil
}

// Run은 orderbook stream을 소유하고 최신 전체 snapshot을 handler에 전달한다.
func (book *LocalOrderBook) Run(
	ctx context.Context,
	public *PublicStream,
	handler LocalOrderBookHandler,
) error {
	if ctx == nil {
		return fmt.Errorf("Korbit local order book context is nil")
	}
	if public == nil {
		return fmt.Errorf("Korbit local order book public stream is required")
	}
	if handler == nil {
		return fmt.Errorf("Korbit local order book handler is required")
	}
	if public.EgressRouteID() != book.routeID {
		return validationError("local order book and WebSocket routes must match")
	}
	if !public.hasOrderBookSubscription(book.symbol, book.level) {
		return validationError("public stream must contain exactly one matching orderbook subscription")
	}

	processor := &localOrderBookProcessor{book: book}
	return public.Run(ctx, func(handlerContext context.Context, message StreamMessage) error {
		if message.Status == "fail" || message.Status == "error" {
			return fmt.Errorf(
				"Korbit order book subscription failed with code %s: %s",
				message.Code,
				message.ErrorMessage,
			)
		}
		if message.Status != "" || message.Channel != StreamChannelOrderBook ||
			message.Symbol != book.symbol {
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
		snapshot := message.Snapshot != nil && *message.Snapshot
		view, publish, err := processor.process(message.Timestamp, event, snapshot, generation)
		if err != nil || !publish {
			return err
		}
		return handler(handlerContext, view)
	})
}

func (processor *localOrderBookProcessor) process(
	envelopeTimestamp int64,
	event StreamOrderBook,
	snapshot bool,
	generation uint64,
) (LocalOrderBookView, bool, error) {
	if err := validateLocalOrderBookSnapshot(envelopeTimestamp, event); err != nil {
		return LocalOrderBookView{}, false, err
	}
	if processor.generation == generation && event.Timestamp < processor.latestTimestamp {
		return LocalOrderBookView{}, false, nil
	}
	processor.generation = generation
	processor.latestTimestamp = event.Timestamp
	processor.snapshotID++
	return localOrderBookView(
		processor.book, envelopeTimestamp, event, snapshot, generation, processor.snapshotID,
	), true, nil
}

func validateLocalOrderBookSnapshot(envelopeTimestamp int64, event StreamOrderBook) error {
	if envelopeTimestamp <= 0 || event.Timestamp <= 0 {
		return validationError("local order book timestamp is invalid")
	}
	if len(event.Asks) > 30 || len(event.Bids) > 30 || len(event.Asks)+len(event.Bids) == 0 {
		return validationError("local order book must contain at most 30 levels per side")
	}
	if err := validateLocalOrderBookLevels("asks", event.Asks, false); err != nil {
		return err
	}
	return validateLocalOrderBookLevels("bids", event.Bids, true)
}

func validateLocalOrderBookLevels(name string, levels []OrderBookLevel, descending bool) error {
	var previous *big.Rat
	for _, level := range levels {
		price, err := positiveLocalOrderBookDecimal(name+" price", level.Price)
		if err != nil {
			return err
		}
		if _, err := nonnegativeLocalOrderBookDecimal(name+" quantity", level.Qty); err != nil {
			return err
		}
		if level.Amount != "" {
			if _, err := nonnegativeLocalOrderBookDecimal(name+" amount", level.Amount); err != nil {
				return err
			}
		}
		if previous != nil && (descending && price.Cmp(previous) >= 0 ||
			!descending && price.Cmp(previous) <= 0) {
			return validationError("local order book %s are not strictly sorted", name)
		}
		previous = price
	}
	return nil
}

func nonnegativeLocalOrderBookDecimal(name, value string) (*big.Rat, error) {
	parsed, ok := new(big.Rat).SetString(value)
	if !ok || parsed.Sign() < 0 {
		return nil, validationError("%s must be a nonnegative decimal", name)
	}
	return parsed, nil
}

func positiveLocalOrderBookDecimal(name, value string) (*big.Rat, error) {
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
	book *LocalOrderBook,
	envelopeTimestamp int64,
	event StreamOrderBook,
	snapshot bool,
	generation uint64,
	snapshotID uint64,
) LocalOrderBookView {
	bidDepth := book.viewDepth
	if len(event.Bids) < bidDepth {
		bidDepth = len(event.Bids)
	}
	askDepth := book.viewDepth
	if len(event.Asks) < askDepth {
		askDepth = len(event.Asks)
	}
	bids := make([]OrderBookLevel, bidDepth)
	copy(bids, event.Bids[:bidDepth])
	asks := make([]OrderBookLevel, askDepth)
	copy(asks, event.Asks[:askDepth])
	return LocalOrderBookView{
		Symbol: book.symbol, Level: book.level, Generation: generation,
		SnapshotID: snapshotID, EnvelopeTimestamp: envelopeTimestamp,
		Timestamp: event.Timestamp, Snapshot: snapshot, Bids: bids, Asks: asks,
	}
}
