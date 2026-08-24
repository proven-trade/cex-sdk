package coinbase

import (
	"context"
	"fmt"
	"math"
	"math/big"
	"sort"
	"strings"
	"time"

	trade "github.com/proven-trade/proven-trade-sdk"
	"github.com/proven-trade/proven-trade-sdk/transport"
)

const defaultLocalOrderBookViewDepth = 20

// LocalOrderBookConfig는 Coinbase Advanced Trade level2 로컬 오더북 설정이다.
type LocalOrderBookConfig struct {
	ProductID     string
	ViewDepth     int
	EgressRouteID transport.EgressRouteID
}

// LocalOrderBookLevel은 가격과 그 가격에 남아 있는 전체 수량이다.
type LocalOrderBookLevel struct {
	Price    string
	Quantity string
}

// LocalOrderBookView는 sequence가 검증된 시점의 정렬된 상위 호가다.
type LocalOrderBookView struct {
	ProductID         string
	ViewDepth         int
	Generation        uint64
	SynchronizationID uint64
	GapCount          uint64
	SequenceNumber    int64
	Timestamp         string
	EventTime         string
	Bids              []LocalOrderBookLevel
	Asks              []LocalOrderBookLevel
}

// LocalOrderBookHandler는 snapshot 교체와 이후 각 유효 update의 장부를 처리한다.
type LocalOrderBookHandler func(context.Context, LocalOrderBookView) error

// LocalOrderBook은 level2 snapshot과 update를 결합하고 sequence gap 시 같은 EIP로 재연결한다.
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
	timestamp string
	eventTime string
}

type localOrderBookProcessor struct {
	book              *LocalOrderBook
	generation        uint64
	synchronizationID uint64
	gapCount          uint64
	sequenceNumber    int64
	sequenceSet       bool
	state             *localOrderBookState
}

// NewLocalOrderBook은 특정 Spot 상품의 level2 전용 로컬 오더북을 생성한다.
func NewLocalOrderBook(config LocalOrderBookConfig) (*LocalOrderBook, error) {
	subscription, err := validatePublicSubscription(StreamSubscription{
		Channel: StreamChannelLevel2, ProductIDs: []string{config.ProductID},
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
		return nil, validationError("local order book view depth must be positive")
	}
	return &LocalOrderBook{
		productID: subscription.ProductIDs[0], viewDepth: config.ViewDepth,
		routeID: config.EgressRouteID,
	}, nil
}

// Run은 public stream을 소유하고 snapshot·update 장부를 handler에 전달한다.
func (book *LocalOrderBook) Run(
	ctx context.Context,
	public *PublicStream,
	handler LocalOrderBookHandler,
) error {
	if ctx == nil {
		return fmt.Errorf("Coinbase local order book context is nil")
	}
	if public == nil {
		return fmt.Errorf("Coinbase local order book public stream is required")
	}
	if handler == nil {
		return fmt.Errorf("Coinbase local order book handler is required")
	}
	if public.EgressRouteID() != book.routeID {
		return validationError("local order book and public WebSocket routes must match")
	}
	if !public.hasSubscription(StreamChannelLevel2, book.productID) {
		return validationError("public stream does not contain the required level2 product")
	}
	processor := &localOrderBookProcessor{book: book}
	return public.Run(ctx, func(handlerContext context.Context, message StreamMessage) error {
		if message.Type == "error" {
			return fmt.Errorf("Coinbase order book subscription failed: %s", message.Message)
		}
		if message.Channel != StreamChannelLevel2Data {
			return nil
		}
		var events []StreamLevel2Event
		if err := message.DecodeEvents(&events); err != nil {
			return err
		}
		view, reconnect, err := processor.process(
			public.Generation(), message.SequenceNumber, message.Timestamp, events,
		)
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
	})
}

func (processor *localOrderBookProcessor) process(
	generation uint64,
	sequenceNumber int64,
	timestamp string,
	events []StreamLevel2Event,
) (*LocalOrderBookView, bool, error) {
	if generation == 0 {
		return nil, false, validationError("local order book WebSocket generation is zero")
	}
	if generation < processor.generation {
		return nil, false, nil
	}
	if processor.generation == 0 || generation > processor.generation {
		processor.generation = generation
		processor.sequenceSet = false
		processor.state = nil
	}
	targetEvents := processor.targetEvents(events)
	if len(targetEvents) == 0 {
		return nil, false, nil
	}
	if sequenceNumber < 0 {
		return nil, false, validationError("local order book sequence is invalid")
	}
	if processor.sequenceSet {
		if sequenceNumber <= processor.sequenceNumber {
			return nil, false, nil
		}
		if processor.sequenceNumber == math.MaxInt64 || sequenceNumber != processor.sequenceNumber+1 {
			return processor.sequenceGap()
		}
	}
	if _, err := time.Parse(time.RFC3339Nano, timestamp); err != nil {
		return nil, false, validationError("local order book timestamp is invalid")
	}
	changed := false
	for _, event := range targetEvents {
		if err := processor.book.validateEvent(event); err != nil {
			return nil, false, err
		}
		switch event.Type {
		case "snapshot":
			state := &localOrderBookState{
				bids: make(map[string]localOrderBookLevel),
				asks: make(map[string]localOrderBookLevel),
			}
			if err := state.apply(timestamp, event.Updates); err != nil {
				return nil, false, err
			}
			processor.state = state
			processor.synchronizationID++
			changed = true
		case "update":
			if processor.state == nil {
				return processor.sequenceGap()
			}
			if err := processor.state.apply(timestamp, event.Updates); err != nil {
				return nil, false, err
			}
			changed = true
		}
	}
	processor.sequenceNumber = sequenceNumber
	processor.sequenceSet = true
	if !changed {
		return nil, false, nil
	}
	view := processor.state.view(processor)
	return &view, false, nil
}

func (processor *localOrderBookProcessor) targetEvents(
	events []StreamLevel2Event,
) []StreamLevel2Event {
	result := make([]StreamLevel2Event, 0, len(events))
	for _, event := range events {
		if event.ProductID == processor.book.productID {
			result = append(result, event)
		}
	}
	return result
}

func (processor *localOrderBookProcessor) sequenceGap() (*LocalOrderBookView, bool, error) {
	processor.gapCount++
	processor.sequenceSet = false
	processor.state = nil
	return nil, true, nil
}

func (book *LocalOrderBook) validateEvent(event StreamLevel2Event) error {
	if event.ProductID != book.productID || (event.Type != "snapshot" && event.Type != "update") {
		return validationError("local order book event identity or type is invalid")
	}
	for _, update := range event.Updates {
		if _, _, _, err := parseLocalOrderBookUpdate(update); err != nil {
			return err
		}
	}
	return nil
}

func (state *localOrderBookState) apply(
	timestamp string,
	updates []StreamLevel2Update,
) error {
	latestEventTime := state.eventTime
	for _, update := range updates {
		levels, key, level, err := state.updateTarget(update)
		if err != nil {
			return err
		}
		if strings.Trim(level.quantityText, "0.") == "" {
			delete(levels, key)
		} else {
			levels[key] = level
		}
		latestEventTime = update.EventTime
	}
	state.timestamp = timestamp
	state.eventTime = latestEventTime
	return nil
}

func (state *localOrderBookState) updateTarget(
	update StreamLevel2Update,
) (map[string]localOrderBookLevel, string, localOrderBookLevel, error) {
	key, level, side, err := parseLocalOrderBookUpdate(update)
	if err != nil {
		return nil, "", localOrderBookLevel{}, err
	}
	if side == "bid" {
		return state.bids, key, level, nil
	}
	return state.asks, key, level, nil
}

func parseLocalOrderBookUpdate(
	update StreamLevel2Update,
) (string, localOrderBookLevel, string, error) {
	if update.Side != "bid" && update.Side != "offer" {
		return "", localOrderBookLevel{}, "", validationError("local order book side is invalid")
	}
	if !positiveDecimalPattern.MatchString(update.PriceLevel) ||
		strings.Trim(update.PriceLevel, "0.") == "" ||
		!positiveDecimalPattern.MatchString(update.NewQuantity) {
		return "", localOrderBookLevel{}, "", validationError("local order book level is invalid")
	}
	if _, err := time.Parse(time.RFC3339Nano, update.EventTime); err != nil {
		return "", localOrderBookLevel{}, "", validationError("local order book event time is invalid")
	}
	price, ok := new(big.Rat).SetString(update.PriceLevel)
	if !ok || price.Sign() <= 0 {
		return "", localOrderBookLevel{}, "", validationError("local order book price is invalid")
	}
	return price.RatString(), localOrderBookLevel{
		priceText: update.PriceLevel, quantityText: update.NewQuantity, price: price,
	}, update.Side, nil
}

func sortedLocalOrderBookLevels(
	values map[string]localOrderBookLevel,
	depth int,
	descending bool,
) []LocalOrderBookLevel {
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
	result := make([]LocalOrderBookLevel, len(levels))
	for index, level := range levels {
		result[index] = LocalOrderBookLevel{Price: level.priceText, Quantity: level.quantityText}
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
		GapCount: processor.gapCount, SequenceNumber: processor.sequenceNumber,
		Timestamp: state.timestamp, EventTime: state.eventTime,
		Bids: sortedLocalOrderBookLevels(state.bids, book.viewDepth, true),
		Asks: sortedLocalOrderBookLevels(state.asks, book.viewDepth, false),
	}
}
