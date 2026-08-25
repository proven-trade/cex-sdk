package okx

import (
	"context"
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"

	trade "github.com/proven-trade/proven-trade-sdk"
	"github.com/proven-trade/proven-trade-sdk/transport"
)

const defaultLocalOrderBookViewDepth = 20

// LocalOrderBookConfig는 OKX V5 books 계열 로컬 오더북 설정이다.
type LocalOrderBookConfig struct {
	Channel       string
	InstrumentID  string
	ViewDepth     int
	EgressRouteID transport.EgressRouteID
}

// LocalOrderBookView는 sequence가 검증된 시점의 정렬된 상위 호가다.
type LocalOrderBookView struct {
	Channel           string
	InstrumentID      string
	Depth             int
	Generation        uint64
	SynchronizationID uint64
	GapCount          uint64
	SequenceID        int64
	Timestamp         int64
	Bids              []BookLevel
	Asks              []BookLevel
}

// LocalOrderBookHandler는 snapshot 교체와 이후 각 유효 update의 장부를 처리한다.
type LocalOrderBookHandler func(context.Context, LocalOrderBookView) error

// LocalOrderBook은 OKX snapshot과 update를 결합하고 sequence gap 시 같은 송신 경로로 재연결한다.
type LocalOrderBook struct {
	channel      string
	instrumentID string
	depth        int
	viewDepth    int
	routeID      transport.EgressRouteID
	argument     StreamArgument
	incremental  bool
}

type localOrderBookLevel struct {
	key                  string
	priceText            string
	quantityText         string
	liquidatedOrderCount string
	orderCount           string
	price                *big.Rat
}

type localOrderBookState struct {
	bids       map[string]localOrderBookLevel
	asks       map[string]localOrderBookLevel
	sequenceID int64
	timestamp  int64
}

type localOrderBookProcessor struct {
	book              *LocalOrderBook
	generation        uint64
	synchronizationID uint64
	gapCount          uint64
	state             *localOrderBookState
}

// NewLocalOrderBook은 books·books5·bbo-tbt channel 전용 로컬 오더북을 생성한다.
func NewLocalOrderBook(config LocalOrderBookConfig) (*LocalOrderBook, error) {
	depth := 0
	incremental := false
	switch config.Channel {
	case "books":
		depth = 400
		incremental = true
	case "books5":
		depth = 5
	case "bbo-tbt":
		depth = 1
	default:
		return nil, validationError("local order book channel must be books, books5, or bbo-tbt")
	}
	argument, err := PublicStreamArgument(config.Channel, config.InstrumentID)
	if err != nil {
		return nil, err
	}
	config.EgressRouteID = transport.EgressRouteID(strings.TrimSpace(string(config.EgressRouteID)))
	if config.EgressRouteID == "" {
		return nil, trade.ErrMissingEgressRoute
	}
	if config.ViewDepth == 0 {
		config.ViewDepth = defaultLocalOrderBookViewDepth
		if config.ViewDepth > depth {
			config.ViewDepth = depth
		}
	}
	if config.ViewDepth < 1 || config.ViewDepth > depth {
		return nil, validationError("local order book view depth must not exceed channel depth")
	}
	return &LocalOrderBook{
		channel: config.Channel, instrumentID: config.InstrumentID,
		depth: depth, viewDepth: config.ViewDepth, routeID: config.EgressRouteID,
		argument: argument, incremental: incremental,
	}, nil
}

// Run은 public stream을 소유하고 snapshot·update 장부를 handler에 전달한다.
func (book *LocalOrderBook) Run(
	ctx context.Context,
	public *PublicStream,
	handler LocalOrderBookHandler,
) error {
	if ctx == nil {
		return fmt.Errorf("OKX local order book context is nil")
	}
	if public == nil {
		return fmt.Errorf("OKX local order book public stream is required")
	}
	if handler == nil {
		return fmt.Errorf("OKX local order book handler is required")
	}
	if public.EgressRouteID() != book.routeID {
		return validationError("local order book and public WebSocket routes must match")
	}
	if !public.hasArgument(book.argument) {
		return validationError("public stream does not contain the required order book channel")
	}
	processor := &localOrderBookProcessor{book: book}
	return public.Run(ctx, func(handlerContext context.Context, message StreamMessage) error {
		if message.Event != "" {
			if message.Event == "error" {
				return fmt.Errorf(
					"OKX order book subscription failed with code %s: %s",
					message.Code,
					message.Message,
				)
			}
			if message.Event == "notice" && message.Code == "64008" {
				return public.reconnect()
			}
			return nil
		}
		if message.Pong || streamArgumentKey(message.Argument) != streamArgumentKey(book.argument) {
			return nil
		}
		var events []OrderBook
		if err := message.DecodeData(&events); err != nil {
			return err
		}
		if len(events) == 0 {
			return validationError("local order book message data is empty")
		}
		for _, event := range events {
			view, reconnect, err := processor.process(
				public.Generation(), message.Action, event,
			)
			if err != nil {
				return err
			}
			if reconnect {
				return public.reconnect()
			}
			if view != nil {
				if err := handler(handlerContext, *view); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func (processor *localOrderBookProcessor) process(
	generation uint64,
	action string,
	event OrderBook,
) (*LocalOrderBookView, bool, error) {
	if generation == 0 {
		return nil, false, validationError("local order book WebSocket generation is zero")
	}
	if generation < processor.generation {
		return nil, false, nil
	}
	if processor.generation == 0 || generation > processor.generation {
		processor.generation = generation
		processor.state = nil
	}
	if err := processor.book.validateEvent(action, event); err != nil {
		return nil, false, err
	}
	if !processor.book.incremental {
		state, err := newLocalOrderBookState(event, processor.book.depth)
		if err != nil {
			return nil, false, err
		}
		processor.state = state
		processor.synchronizationID++
		view := state.view(processor)
		return &view, false, nil
	}
	if action == "snapshot" {
		state, err := newLocalOrderBookState(event, processor.book.depth)
		if err != nil {
			return nil, false, err
		}
		processor.state = state
		processor.synchronizationID++
		view := state.view(processor)
		return &view, false, nil
	}
	if processor.state == nil || event.PreviousSequenceID != processor.state.sequenceID {
		return processor.sequenceGap()
	}
	if event.SequenceID == event.PreviousSequenceID {
		if len(event.Bids) == 0 && len(event.Asks) == 0 {
			return nil, false, nil
		}
		return processor.sequenceGap()
	}
	if err := processor.state.apply(event, processor.book.depth); err != nil {
		return nil, false, err
	}
	view := processor.state.view(processor)
	return &view, false, nil
}

func (processor *localOrderBookProcessor) sequenceGap() (*LocalOrderBookView, bool, error) {
	processor.gapCount++
	processor.state = nil
	return nil, true, nil
}

func (book *LocalOrderBook) validateEvent(action string, event OrderBook) error {
	if book.incremental {
		if action != "snapshot" && action != "update" {
			return validationError("incremental local order book action is invalid")
		}
		if action == "snapshot" && event.PreviousSequenceID != -1 {
			return validationError("local order book snapshot previous sequence is invalid")
		}
	} else if action != "" && action != "snapshot" {
		return validationError("snapshot local order book action is invalid")
	}
	if event.SequenceID < 0 {
		return validationError("local order book sequence is invalid")
	}
	if action != "update" && (len(event.Bids) > book.depth || len(event.Asks) > book.depth) {
		return validationError("local order book snapshot exceeds channel depth")
	}
	if _, err := localOrderBookTimestamp(event.Timestamp); err != nil {
		return err
	}
	for _, level := range append(append([]BookLevel(nil), event.Bids...), event.Asks...) {
		if _, err := parseLocalOrderBookLevel(level); err != nil {
			return err
		}
	}
	return nil
}

func newLocalOrderBookState(event OrderBook, depth int) (*localOrderBookState, error) {
	state := &localOrderBookState{
		bids: make(map[string]localOrderBookLevel, len(event.Bids)),
		asks: make(map[string]localOrderBookLevel, len(event.Asks)),
	}
	if err := state.apply(event, depth); err != nil {
		return nil, err
	}
	return state, nil
}

func (state *localOrderBookState) apply(event OrderBook, depth int) error {
	for _, level := range event.Bids {
		if err := applyLocalOrderBookLevel(state.bids, level); err != nil {
			return err
		}
	}
	for _, level := range event.Asks {
		if err := applyLocalOrderBookLevel(state.asks, level); err != nil {
			return err
		}
	}
	pruneLocalOrderBookLevels(state.bids, depth, true)
	pruneLocalOrderBookLevels(state.asks, depth, false)
	timestamp, err := localOrderBookTimestamp(event.Timestamp)
	if err != nil {
		return err
	}
	state.sequenceID = event.SequenceID
	state.timestamp = timestamp
	return nil
}

func applyLocalOrderBookLevel(
	levels map[string]localOrderBookLevel,
	level BookLevel,
) error {
	parsed, err := parseLocalOrderBookLevel(level)
	if err != nil {
		return err
	}
	if strings.Trim(level.Quantity, "0.") == "" {
		delete(levels, parsed.key)
		return nil
	}
	levels[parsed.key] = parsed
	return nil
}

func parseLocalOrderBookLevel(level BookLevel) (localOrderBookLevel, error) {
	if !positiveDecimalPattern.MatchString(level.Price) ||
		strings.Trim(level.Price, "0.") == "" ||
		!positiveDecimalPattern.MatchString(level.Quantity) ||
		level.LiquidatedOrderCount != "0" {
		return localOrderBookLevel{}, validationError("local order book level is invalid")
	}
	if _, err := strconv.ParseUint(level.OrderCount, 10, 64); err != nil {
		return localOrderBookLevel{}, validationError("local order book order count is invalid")
	}
	price, ok := new(big.Rat).SetString(level.Price)
	if !ok || price.Sign() <= 0 {
		return localOrderBookLevel{}, validationError("local order book price is invalid")
	}
	return localOrderBookLevel{
		key: price.RatString(), priceText: level.Price, quantityText: level.Quantity,
		liquidatedOrderCount: level.LiquidatedOrderCount,
		orderCount:           level.OrderCount,
		price:                price,
	}, nil
}

func localOrderBookTimestamp(value string) (int64, error) {
	timestamp, err := strconv.ParseInt(value, 10, 64)
	if err != nil || timestamp <= 0 {
		return 0, validationError("local order book timestamp is invalid")
	}
	return timestamp, nil
}

func pruneLocalOrderBookLevels(
	values map[string]localOrderBookLevel,
	depth int,
	descending bool,
) {
	levels := sortedLocalOrderBookLevels(values, len(values), descending)
	for index := depth; index < len(levels); index++ {
		delete(values, levels[index].key)
	}
}

func sortedLocalOrderBookLevels(
	values map[string]localOrderBookLevel,
	depth int,
	descending bool,
) []localOrderBookLevel {
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
	return levels
}

func (state *localOrderBookState) view(
	processor *localOrderBookProcessor,
) LocalOrderBookView {
	book := processor.book
	return LocalOrderBookView{
		Channel: book.channel, InstrumentID: book.instrumentID, Depth: book.depth,
		Generation: processor.generation, SynchronizationID: processor.synchronizationID,
		GapCount: processor.gapCount, SequenceID: state.sequenceID, Timestamp: state.timestamp,
		Bids: localOrderBookViewLevels(state.bids, book.viewDepth, true),
		Asks: localOrderBookViewLevels(state.asks, book.viewDepth, false),
	}
}

func localOrderBookViewLevels(
	values map[string]localOrderBookLevel,
	depth int,
	descending bool,
) []BookLevel {
	levels := sortedLocalOrderBookLevels(values, depth, descending)
	result := make([]BookLevel, len(levels))
	for index, level := range levels {
		result[index] = BookLevel{
			Price: level.priceText, Quantity: level.quantityText,
			LiquidatedOrderCount: level.liquidatedOrderCount, OrderCount: level.orderCount,
		}
	}
	return result
}
