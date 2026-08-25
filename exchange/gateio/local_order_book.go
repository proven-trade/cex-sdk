package gateio

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"sort"
	"strings"

	trade "github.com/proven-trade/proven-trade-sdk"
	"github.com/proven-trade/proven-trade-sdk/transport"
)

const defaultLocalOrderBookViewDepth = 20

// LocalOrderBookConfig는 Gate.io Spot V2 로컬 오더북 설정이다.
type LocalOrderBookConfig struct {
	CurrencyPair  string
	Depth         StreamOrderBookDepth
	EgressRouteID transport.EgressRouteID
	ViewDepth     int
}

// LocalOrderBookView는 update ID가 검증된 시점의 정렬된 상위 호가다.
type LocalOrderBookView struct {
	CurrencyPair      string
	Depth             StreamOrderBookDepth
	Generation        uint64
	SynchronizationID uint64
	UpdateID          int64
	Timestamp         int64
	ReceivedAt        int64
	GapCount          uint64
	Bids              []BookLevel
	Asks              []BookLevel
}

// LocalOrderBookHandler는 snapshot 교체와 이후 각 유효 증분 장부를 처리한다.
type LocalOrderBookHandler func(context.Context, LocalOrderBookView) error

// LocalOrderBook은 Spot V2 snapshot과 증분을 결합하고 공백 시 같은 송신 경로로 재연결한다.
type LocalOrderBook struct {
	currencyPair string
	depth        StreamOrderBookDepth
	routeID      transport.EgressRouteID
	viewDepth    int
	streamName   string
}

type localOrderBookLevel struct {
	price      *big.Rat
	priceText  string
	amountText string
}

type localOrderBookState struct {
	updateID   int64
	timestamp  int64
	receivedAt int64
	bids       map[string]localOrderBookLevel
	asks       map[string]localOrderBookLevel
}

type localOrderBookProcessor struct {
	book              *LocalOrderBook
	generation        uint64
	synchronizationID uint64
	gapCount          uint64
	state             *localOrderBookState
}

type localOrderBookSubscriptionResult struct {
	Status string `json:"status"`
}

// NewLocalOrderBook은 Gate.io Spot Order Book V2 전용 로컬 오더북을 생성한다.
func NewLocalOrderBook(config LocalOrderBookConfig) (*LocalOrderBook, error) {
	config.CurrencyPair = strings.ToUpper(config.CurrencyPair)
	if err := validateCurrencyPair(config.CurrencyPair); err != nil {
		return nil, err
	}
	if config.Depth != StreamOrderBookDepth50 && config.Depth != StreamOrderBookDepth400 {
		return nil, validationError("local order book depth must be 50 or 400")
	}
	config.EgressRouteID = transport.EgressRouteID(strings.TrimSpace(string(config.EgressRouteID)))
	if config.EgressRouteID == "" {
		return nil, trade.ErrMissingEgressRoute
	}
	if config.ViewDepth == 0 {
		config.ViewDepth = defaultLocalOrderBookViewDepth
	}
	if config.ViewDepth < 1 || config.ViewDepth > int(config.Depth) {
		return nil, validationError(
			"local order book view depth must be between 1 and %d", config.Depth,
		)
	}
	return &LocalOrderBook{
		currencyPair: config.CurrencyPair, depth: config.Depth,
		routeID: config.EgressRouteID, viewDepth: config.ViewDepth,
		streamName: fmt.Sprintf("ob.%s.%d", config.CurrencyPair, config.Depth),
	}, nil
}

// Run은 public stream을 소유하고 V2 snapshot·증분 장부를 handler에 전달한다.
func (book *LocalOrderBook) Run(
	ctx context.Context,
	public *PublicStream,
	handler LocalOrderBookHandler,
) error {
	if ctx == nil {
		return fmt.Errorf("Gate.io local order book context is nil")
	}
	if public == nil {
		return fmt.Errorf("Gate.io public stream is required")
	}
	if handler == nil {
		return fmt.Errorf("Gate.io local order book handler is required")
	}
	if public.EgressRouteID() != book.routeID {
		return validationError("local order book and WebSocket routes must match")
	}
	if !book.hasSubscription(public) {
		return validationError("local order book requires its exact order book V2 subscription")
	}

	processor := &localOrderBookProcessor{book: book}
	return public.Run(ctx, func(handlerContext context.Context, message StreamMessage) error {
		if message.Channel != StreamChannelOrderBookV2 {
			return nil
		}
		if message.Error != nil {
			return fmt.Errorf(
				"Gate.io order book V2 subscription failed with code %d: %s",
				message.Error.Code,
				message.Error.Message,
			)
		}
		if message.Event == "subscribe" {
			var result localOrderBookSubscriptionResult
			if err := json.Unmarshal(message.Result, &result); err != nil {
				return fmt.Errorf("decode Gate.io order book V2 subscription result: %w", err)
			}
			if result.Status != "success" {
				return fmt.Errorf("Gate.io order book V2 subscription was rejected: %s", result.Status)
			}
			return nil
		}
		if message.Event != "update" {
			return nil
		}
		var event StreamOrderBookV2
		if err := message.Decode(&event); err != nil {
			return err
		}
		generation := public.Generation()
		if generation == 0 {
			return validationError("local order book WebSocket generation is zero")
		}
		view, publish, reconnect, err := processor.process(message.TimeMilli, event, generation)
		if err != nil {
			return err
		}
		if reconnect {
			return public.reconnect()
		}
		if !publish {
			return nil
		}
		return handler(handlerContext, view)
	})
}

func (book *LocalOrderBook) hasSubscription(public *PublicStream) bool {
	for _, subscription := range public.managed.snapshotSubscriptions() {
		if subscription.Channel == StreamChannelOrderBookV2 &&
			subscription.CurrencyPair == book.currencyPair &&
			subscription.OrderBookDepth == book.depth {
			return true
		}
	}
	return false
}

func (processor *localOrderBookProcessor) process(
	receivedAt int64,
	event StreamOrderBookV2,
	generation uint64,
) (LocalOrderBookView, bool, bool, error) {
	if processor.generation != generation {
		processor.generation = generation
		processor.state = nil
	}
	if err := processor.book.validateEvent(receivedAt, event); err != nil {
		return LocalOrderBookView{}, false, false, err
	}
	if event.Full {
		state, err := newLocalOrderBookState(receivedAt, event)
		if err != nil {
			return LocalOrderBookView{}, false, false, err
		}
		processor.state = state
		processor.synchronizationID++
		return processor.view(), true, false, nil
	}
	if processor.state == nil || event.FirstUpdateID != processor.state.updateID+1 {
		processor.state = nil
		processor.gapCount++
		return LocalOrderBookView{}, false, true, nil
	}
	if err := applyLocalOrderBookLevels(processor.state.asks, event.Asks); err != nil {
		return LocalOrderBookView{}, false, false, err
	}
	if err := applyLocalOrderBookLevels(processor.state.bids, event.Bids); err != nil {
		return LocalOrderBookView{}, false, false, err
	}
	pruneLocalOrderBookLevels(processor.state.asks, int(processor.book.depth), false)
	pruneLocalOrderBookLevels(processor.state.bids, int(processor.book.depth), true)
	processor.state.updateID = event.LastUpdateID
	processor.state.timestamp = event.Timestamp
	processor.state.receivedAt = receivedAt
	return processor.view(), true, false, nil
}

func (book *LocalOrderBook) validateEvent(receivedAt int64, event StreamOrderBookV2) error {
	if receivedAt <= 0 || event.Timestamp <= 0 || event.LastUpdateID <= 0 ||
		event.StreamName != book.streamName {
		return validationError("local order book received invalid V2 metadata")
	}
	if event.Full {
		if event.FirstUpdateID != 0 {
			return validationError("local order book snapshot has a first update ID")
		}
		if len(event.Asks) > int(book.depth) || len(event.Bids) > int(book.depth) ||
			len(event.Asks)+len(event.Bids) == 0 {
			return validationError("local order book snapshot depth is invalid")
		}
	} else if event.FirstUpdateID <= 0 || event.LastUpdateID < event.FirstUpdateID {
		return validationError("local order book incremental update range is invalid")
	}
	for _, levels := range [][]BookLevel{event.Asks, event.Bids} {
		seenPrices := make(map[string]struct{}, len(levels))
		for _, level := range levels {
			key, _, amount, err := parseLocalOrderBookLevel(level)
			if err != nil {
				return err
			}
			if _, exists := seenPrices[key]; exists {
				return validationError("local order book update has a duplicate price")
			}
			seenPrices[key] = struct{}{}
			if event.Full && amount.Sign() == 0 {
				return validationError("local order book snapshot amount must be positive")
			}
		}
	}
	return nil
}

func newLocalOrderBookState(
	receivedAt int64,
	event StreamOrderBookV2,
) (*localOrderBookState, error) {
	state := &localOrderBookState{
		updateID: event.LastUpdateID, timestamp: event.Timestamp, receivedAt: receivedAt,
		bids: make(map[string]localOrderBookLevel, len(event.Bids)),
		asks: make(map[string]localOrderBookLevel, len(event.Asks)),
	}
	if err := applyLocalOrderBookLevels(state.asks, event.Asks); err != nil {
		return nil, err
	}
	if err := applyLocalOrderBookLevels(state.bids, event.Bids); err != nil {
		return nil, err
	}
	return state, nil
}

func applyLocalOrderBookLevels(
	levels map[string]localOrderBookLevel,
	updates []BookLevel,
) error {
	for _, update := range updates {
		key, level, amount, err := parseLocalOrderBookLevel(update)
		if err != nil {
			return err
		}
		if amount.Sign() == 0 {
			delete(levels, key)
			continue
		}
		levels[key] = level
	}
	return nil
}

func parseLocalOrderBookLevel(
	level BookLevel,
) (string, localOrderBookLevel, *big.Rat, error) {
	price, ok := new(big.Rat).SetString(level.Price)
	if !ok || price.Sign() <= 0 {
		return "", localOrderBookLevel{}, nil, validationError(
			"local order book price must be a positive decimal",
		)
	}
	amount, ok := new(big.Rat).SetString(level.Amount)
	if !ok || amount.Sign() < 0 {
		return "", localOrderBookLevel{}, nil, validationError(
			"local order book amount must be a nonnegative decimal",
		)
	}
	return price.RatString(), localOrderBookLevel{
		price: price, priceText: level.Price, amountText: level.Amount,
	}, amount, nil
}

func pruneLocalOrderBookLevels(
	values map[string]localOrderBookLevel,
	depth int,
	descending bool,
) {
	if len(values) <= depth {
		return
	}
	levels := sortedLocalOrderBookLevels(values, len(values), descending)
	for _, level := range levels[depth:] {
		price, _ := new(big.Rat).SetString(level.Price)
		delete(values, price.RatString())
	}
}

func sortedLocalOrderBookLevels(
	values map[string]localOrderBookLevel,
	depth int,
	descending bool,
) []BookLevel {
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
	result := make([]BookLevel, len(levels))
	for index, level := range levels {
		result[index] = BookLevel{Price: level.priceText, Amount: level.amountText}
	}
	return result
}

func (processor *localOrderBookProcessor) view() LocalOrderBookView {
	return LocalOrderBookView{
		CurrencyPair: processor.book.currencyPair, Depth: processor.book.depth,
		Generation: processor.generation, SynchronizationID: processor.synchronizationID,
		UpdateID: processor.state.updateID, Timestamp: processor.state.timestamp,
		ReceivedAt: processor.state.receivedAt, GapCount: processor.gapCount,
		Bids: sortedLocalOrderBookLevels(
			processor.state.bids, processor.book.viewDepth, true,
		),
		Asks: sortedLocalOrderBookLevels(
			processor.state.asks, processor.book.viewDepth, false,
		),
	}
}
