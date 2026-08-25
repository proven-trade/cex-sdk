package futures

import (
	"context"
	"fmt"
	"math/big"
	"sort"
	"strings"

	trade "github.com/proven-trade/proven-trade-sdk"
	"github.com/proven-trade/proven-trade-sdk/transport"
)

const defaultLocalOrderBookViewDepth = 20

// LocalOrderBookConfig는 Kraken Futures WebSocket v1 book 로컬 오더북 설정이다.
type LocalOrderBookConfig struct {
	ProductID     string
	ViewDepth     int
	EgressRouteID transport.EgressRouteID
}

// LocalOrderBookView는 sequence가 검증된 시점의 정렬된 상위 호가다.
type LocalOrderBookView struct {
	ProductID         string
	ViewDepth         int
	Generation        uint64
	SynchronizationID uint64
	GapCount          uint64
	Sequence          uint64
	Timestamp         int64
	Bids              []StreamBookLevel
	Asks              []StreamBookLevel
}

// LocalOrderBookHandler는 snapshot 교체와 이후 각 유효 update의 장부를 처리한다.
type LocalOrderBookHandler func(context.Context, LocalOrderBookView) error

// LocalOrderBook은 book_snapshot과 book update를 결합하고 sequence 이상 시 같은 송신 경로로 재연결한다.
type LocalOrderBook struct {
	productID string
	viewDepth int
	routeID   transport.EgressRouteID
}

type localOrderBookLevel struct {
	priceText    string
	quantityText string
	price        *big.Rat
}

type localOrderBookState struct {
	bids      map[string]localOrderBookLevel
	asks      map[string]localOrderBookLevel
	sequence  uint64
	timestamp int64
}

type localOrderBookProcessor struct {
	book              *LocalOrderBook
	generation        uint64
	synchronizationID uint64
	gapCount          uint64
	state             *localOrderBookState
}

// NewLocalOrderBook은 특정 Futures 상품의 book feed 전용 로컬 오더북을 생성한다.
func NewLocalOrderBook(config LocalOrderBookConfig) (*LocalOrderBook, error) {
	subscription, err := validatePublicStreamSubscription(PublicStreamSubscription{
		Feed: PublicFeedBook, ProductIDs: []string{config.ProductID},
	})
	if err != nil {
		return nil, err
	}
	config.EgressRouteID = transport.EgressRouteID(strings.TrimSpace(string(config.EgressRouteID)))
	if config.EgressRouteID == "" {
		return nil, trade.ErrMissingEgressRoute
	}
	if config.ViewDepth == 0 {
		config.ViewDepth = defaultLocalOrderBookViewDepth
	}
	if config.ViewDepth < 1 {
		return nil, validationError("Futures local order book view depth must be positive")
	}
	return &LocalOrderBook{
		productID: subscription.ProductIDs[0], viewDepth: config.ViewDepth,
		routeID: config.EgressRouteID,
	}, nil
}

// Run은 public stream을 소유하고 sequence가 검증된 snapshot·update 장부를 handler에 전달한다.
func (book *LocalOrderBook) Run(
	ctx context.Context,
	public *PublicStream,
	handler LocalOrderBookHandler,
) error {
	if ctx == nil {
		return fmt.Errorf("Kraken Futures local order book context is nil")
	}
	if public == nil {
		return fmt.Errorf("Kraken Futures local order book public stream is required")
	}
	if handler == nil {
		return fmt.Errorf("Kraken Futures local order book handler is required")
	}
	if public.EgressRouteID() != book.routeID {
		return validationError("Futures local order book and public WebSocket routes must match")
	}
	if !public.hasBookSubscription(book.productID) {
		return validationError("public stream does not contain the required Futures book product")
	}
	processor := &localOrderBookProcessor{book: book}
	return public.Run(ctx, func(handlerContext context.Context, message StreamMessage) error {
		if message.Event != "" {
			return nil
		}
		switch message.Feed {
		case "book_snapshot":
			if message.ProductID != book.productID {
				return nil
			}
			var event StreamBookSnapshot
			if err := message.Decode(&event); err != nil {
				return err
			}
			view, reconnect, err := processor.processSnapshot(public.Generation(), event)
			if err != nil {
				return err
			}
			if reconnect {
				return public.reconnect()
			}
			if view == nil {
				return nil
			}
			return handler(handlerContext, *view)
		case string(PublicFeedBook):
			if message.ProductID != book.productID {
				return nil
			}
			var event StreamBookUpdate
			if err := message.Decode(&event); err != nil {
				return err
			}
			view, reconnect, err := processor.processUpdate(public.Generation(), event)
			if err != nil {
				return err
			}
			if reconnect {
				return public.reconnect()
			}
			if view == nil {
				return nil
			}
			return handler(handlerContext, *view)
		default:
			return nil
		}
	})
}

func (processor *localOrderBookProcessor) processSnapshot(
	generation uint64,
	event StreamBookSnapshot,
) (*LocalOrderBookView, bool, error) {
	stale, err := processor.prepareGeneration(generation)
	if err != nil || stale {
		return nil, false, err
	}
	if err := processor.book.validateSnapshot(event); err != nil {
		return nil, false, err
	}
	state := &localOrderBookState{
		bids:     make(map[string]localOrderBookLevel, len(event.Bids)),
		asks:     make(map[string]localOrderBookLevel, len(event.Asks)),
		sequence: event.Sequence, timestamp: event.Timestamp,
	}
	for _, level := range event.Bids {
		if err := applyLocalOrderBookLevel(state.bids, level, false); err != nil {
			return nil, false, err
		}
	}
	for _, level := range event.Asks {
		if err := applyLocalOrderBookLevel(state.asks, level, false); err != nil {
			return nil, false, err
		}
	}
	processor.state = state
	processor.synchronizationID++
	view := state.view(processor)
	return &view, false, nil
}

func (processor *localOrderBookProcessor) processUpdate(
	generation uint64,
	event StreamBookUpdate,
) (*LocalOrderBookView, bool, error) {
	stale, err := processor.prepareGeneration(generation)
	if err != nil || stale {
		return nil, false, err
	}
	if err := processor.book.validateUpdate(event); err != nil {
		return nil, false, err
	}
	if processor.state == nil {
		return processor.sequenceGap()
	}
	if event.Sequence <= processor.state.sequence {
		return nil, false, nil
	}
	if processor.state.sequence == ^uint64(0) || event.Sequence != processor.state.sequence+1 {
		return processor.sequenceGap()
	}
	levels := processor.state.asks
	if event.Side == string(SideBuy) {
		levels = processor.state.bids
	}
	if err := applyLocalOrderBookLevel(levels, StreamBookLevel{
		Price: event.Price, Quantity: event.Quantity,
	}, true); err != nil {
		return nil, false, err
	}
	processor.state.sequence = event.Sequence
	processor.state.timestamp = event.Timestamp
	view := processor.state.view(processor)
	return &view, false, nil
}

func (processor *localOrderBookProcessor) prepareGeneration(generation uint64) (bool, error) {
	if generation == 0 {
		return false, validationError("Futures local order book WebSocket generation is zero")
	}
	if generation < processor.generation {
		return true, nil
	}
	if processor.generation == 0 || generation > processor.generation {
		processor.generation = generation
		processor.state = nil
	}
	return false, nil
}

func (processor *localOrderBookProcessor) sequenceGap() (*LocalOrderBookView, bool, error) {
	processor.gapCount++
	processor.state = nil
	return nil, true, nil
}

func (book *LocalOrderBook) validateSnapshot(event StreamBookSnapshot) error {
	if event.Feed != "book_snapshot" || event.ProductID != book.productID ||
		event.Sequence == 0 || event.Timestamp <= 0 {
		return validationError("Futures local order book snapshot identity or sequence is invalid")
	}
	for _, level := range append(append([]StreamBookLevel(nil), event.Bids...), event.Asks...) {
		if _, _, err := parseLocalOrderBookLevel(level, false); err != nil {
			return err
		}
	}
	return nil
}

func (book *LocalOrderBook) validateUpdate(event StreamBookUpdate) error {
	if event.Feed != string(PublicFeedBook) || event.ProductID != book.productID ||
		event.Sequence == 0 || event.Timestamp <= 0 ||
		(event.Side != string(SideBuy) && event.Side != string(SideSell)) {
		return validationError("Futures local order book update identity or sequence is invalid")
	}
	_, _, err := parseLocalOrderBookLevel(StreamBookLevel{
		Price: event.Price, Quantity: event.Quantity,
	}, true)
	return err
}

func applyLocalOrderBookLevel(
	levels map[string]localOrderBookLevel,
	value StreamBookLevel,
	allowZero bool,
) error {
	key, level, err := parseLocalOrderBookLevel(value, allowZero)
	if err != nil {
		return err
	}
	if decimalIsZero(level.quantityText) {
		delete(levels, key)
		return nil
	}
	levels[key] = level
	return nil
}

func parseLocalOrderBookLevel(
	value StreamBookLevel,
	allowZero bool,
) (string, localOrderBookLevel, error) {
	priceText := string(value.Price)
	quantityText := string(value.Quantity)
	if !positiveDecimalPattern.MatchString(priceText) || decimalIsZero(priceText) ||
		!positiveDecimalPattern.MatchString(quantityText) ||
		(!allowZero && decimalIsZero(quantityText)) {
		return "", localOrderBookLevel{}, validationError("Futures local order book level is invalid")
	}
	price, ok := new(big.Rat).SetString(priceText)
	if !ok || price.Sign() <= 0 {
		return "", localOrderBookLevel{}, validationError("Futures local order book price is invalid")
	}
	return price.RatString(), localOrderBookLevel{
		priceText: priceText, quantityText: quantityText, price: price,
	}, nil
}

func decimalIsZero(value string) bool {
	return strings.Trim(value, "0.") == ""
}

func sortedLocalOrderBookLevels(
	values map[string]localOrderBookLevel,
	depth int,
	descending bool,
) []StreamBookLevel {
	levels := make([]localOrderBookLevel, 0, len(values))
	for _, level := range values {
		levels = append(levels, level)
	}
	sort.Slice(levels, func(left, right int) bool {
		comparison := levels[left].price.Cmp(levels[right].price)
		if descending {
			return comparison > 0
		}
		return comparison < 0
	})
	if len(levels) > depth {
		levels = levels[:depth]
	}
	result := make([]StreamBookLevel, len(levels))
	for index, level := range levels {
		result[index] = StreamBookLevel{
			Price: Decimal(level.priceText), Quantity: Decimal(level.quantityText),
		}
	}
	return result
}

func (state *localOrderBookState) view(
	processor *localOrderBookProcessor,
) LocalOrderBookView {
	book := processor.book
	return LocalOrderBookView{
		ProductID: book.productID, ViewDepth: book.viewDepth,
		Generation: processor.generation, SynchronizationID: processor.synchronizationID,
		GapCount: processor.gapCount, Sequence: state.sequence, Timestamp: state.timestamp,
		Bids: sortedLocalOrderBookLevels(state.bids, book.viewDepth, true),
		Asks: sortedLocalOrderBookLevels(state.asks, book.viewDepth, false),
	}
}
