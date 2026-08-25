package kucoin

import (
	"context"
	"fmt"
	"math/big"
	"sort"
	"strings"

	trade "github.com/proven-trade/proven-trade-sdk"
	"github.com/proven-trade/proven-trade-sdk/transport"
)

const (
	defaultLocalOrderBookViewDepth = 20
	maximumProOrderBookDepth       = 500
)

// LocalOrderBookConfig는 KuCoin Pro Increment Best 500 로컬 오더북 설정이다.
type LocalOrderBookConfig struct {
	Symbol        string
	EgressRouteID transport.EgressRouteID
	ViewDepth     int
}

// LocalOrderBookView는 sequence가 검증된 시점의 정렬된 상위 호가다.
type LocalOrderBookView struct {
	Symbol            string
	Generation        uint64
	SynchronizationID uint64
	Sequence          int64
	MatchingTime      int64
	PublishTime       int64
	GapCount          uint64
	Bids              []BookLevel
	Asks              []BookLevel
}

// LocalOrderBookHandler는 snapshot 교체와 이후 각 유효 delta의 장부를 처리한다.
type LocalOrderBookHandler func(context.Context, LocalOrderBookView) error

// LocalOrderBook은 Pro 상위 500호가 snapshot과 delta를 결합하고 gap 시 같은 송신 경로로 재연결한다.
type LocalOrderBook struct {
	symbol    string
	routeID   transport.EgressRouteID
	viewDepth int
}

type localOrderBookLevel struct {
	price     *big.Rat
	priceText string
	size      string
}

type localOrderBookState struct {
	sequence     int64
	matchingTime int64
	publishTime  int64
	bids         map[string]localOrderBookLevel
	asks         map[string]localOrderBookLevel
}

type localOrderBookProcessor struct {
	book              *LocalOrderBook
	generation        uint64
	synchronizationID uint64
	gapCount          uint64
	state             *localOrderBookState
}

// NewLocalOrderBook은 현재 권장 Pro Increment Best 500 전용 로컬 오더북을 생성한다.
func NewLocalOrderBook(config LocalOrderBookConfig) (*LocalOrderBook, error) {
	config.Symbol = strings.ToUpper(config.Symbol)
	if err := validateSymbol(config.Symbol); err != nil {
		return nil, err
	}
	config.EgressRouteID = transport.EgressRouteID(strings.TrimSpace(string(config.EgressRouteID)))
	if config.EgressRouteID == "" {
		return nil, trade.ErrMissingEgressRoute
	}
	if config.ViewDepth == 0 {
		config.ViewDepth = defaultLocalOrderBookViewDepth
	}
	if config.ViewDepth < 1 || config.ViewDepth > maximumProOrderBookDepth {
		return nil, validationError("local order book view depth must be between 1 and 500")
	}
	return &LocalOrderBook{
		symbol: config.Symbol, routeID: config.EgressRouteID, viewDepth: config.ViewDepth,
	}, nil
}

// Run은 Pro orderbook stream을 소유하고 snapshot·delta 장부를 handler에 전달한다.
func (book *LocalOrderBook) Run(
	ctx context.Context,
	stream *ProOrderBookStream,
	handler LocalOrderBookHandler,
) error {
	if ctx == nil {
		return fmt.Errorf("KuCoin local order book context is nil")
	}
	if stream == nil {
		return fmt.Errorf("KuCoin Pro order book stream is required")
	}
	if handler == nil {
		return fmt.Errorf("KuCoin local order book handler is required")
	}
	if stream.EgressRouteID() != book.routeID {
		return validationError("local order book and WebSocket routes must match")
	}
	if stream.symbol != book.symbol {
		return validationError("local order book and WebSocket symbols must match")
	}

	processor := &localOrderBookProcessor{book: book}
	return stream.Run(ctx, func(handlerContext context.Context, message ProOrderBookMessage) error {
		if message.ErrorCode != "" {
			return fmt.Errorf(
				"KuCoin Pro order book subscription failed with code %s: %s",
				message.ErrorCode,
				message.ErrorMessage,
			)
		}
		if message.Result != "" {
			if message.Result != "true" {
				return fmt.Errorf("KuCoin Pro order book subscription was rejected: %s", message.Result)
			}
			return nil
		}
		if message.ControlType != "" {
			return nil
		}
		generation := stream.Generation()
		if generation == 0 {
			return validationError("local order book WebSocket generation is zero")
		}
		view, publish, reconnect, err := processor.process(message, generation)
		if err != nil {
			return err
		}
		if reconnect {
			return stream.reconnect()
		}
		if !publish {
			return nil
		}
		return handler(handlerContext, view)
	})
}

func (processor *localOrderBookProcessor) process(
	message ProOrderBookMessage,
	generation uint64,
) (LocalOrderBookView, bool, bool, error) {
	if processor.generation != generation {
		processor.generation = generation
		processor.state = nil
	}
	if err := processor.book.validateEvent(message); err != nil {
		return LocalOrderBookView{}, false, false, err
	}
	if message.UpdateType == "snapshot" {
		state, err := newLocalOrderBookState(message)
		if err != nil {
			return LocalOrderBookView{}, false, false, err
		}
		processor.state = state
		processor.synchronizationID++
		return processor.view(), true, false, nil
	}
	if processor.state == nil {
		return processor.sequenceGap()
	}
	if message.Data.SequenceEnd <= processor.state.sequence {
		return LocalOrderBookView{}, false, false, nil
	}
	if message.Data.SequenceStart > processor.state.sequence+1 {
		return processor.sequenceGap()
	}
	if err := applyLocalOrderBookLevels(processor.state.asks, message.Data.Asks); err != nil {
		return LocalOrderBookView{}, false, false, err
	}
	if err := applyLocalOrderBookLevels(processor.state.bids, message.Data.Bids); err != nil {
		return LocalOrderBookView{}, false, false, err
	}
	pruneLocalOrderBookLevels(processor.state.asks, maximumProOrderBookDepth, false)
	pruneLocalOrderBookLevels(processor.state.bids, maximumProOrderBookDepth, true)
	processor.state.sequence = message.Data.SequenceEnd
	processor.state.matchingTime = message.Data.MatchingTime
	processor.state.publishTime = message.PublishTime
	return processor.view(), true, false, nil
}

func (processor *localOrderBookProcessor) sequenceGap() (
	LocalOrderBookView,
	bool,
	bool,
	error,
) {
	processor.state = nil
	processor.gapCount++
	return LocalOrderBookView{}, false, true, nil
}

func (book *LocalOrderBook) validateEvent(message ProOrderBookMessage) error {
	if !strings.EqualFold(message.Topic, proOrderBookTopic) || message.Depth != proOrderBookDepth ||
		message.Data.Symbol != book.symbol {
		return validationError("local order book received an unexpected Pro event")
	}
	if message.PublishTime <= 0 || message.Data.MatchingTime <= 0 ||
		message.Data.SequenceStart <= 0 || message.Data.SequenceEnd < message.Data.SequenceStart {
		return validationError("local order book sequence or timestamp is invalid")
	}
	if message.UpdateType == "snapshot" {
		if message.Data.SequenceStart != message.Data.SequenceEnd {
			return validationError("local order book snapshot sequence range is invalid")
		}
		if len(message.Data.Asks) > maximumProOrderBookDepth ||
			len(message.Data.Bids) > maximumProOrderBookDepth ||
			len(message.Data.Asks)+len(message.Data.Bids) == 0 {
			return validationError("local order book snapshot must contain at most 500 levels per side")
		}
	} else if message.UpdateType == "delta" {
		if len(message.Data.Asks)+len(message.Data.Bids) == 0 {
			return validationError("local order book delta has no changes")
		}
	} else {
		return validationError("local order book update type is invalid")
	}
	for _, levels := range [][]BookLevel{message.Data.Asks, message.Data.Bids} {
		seenPrices := make(map[string]struct{}, len(levels))
		for _, level := range levels {
			key, _, size, err := parseLocalOrderBookLevel(level)
			if err != nil {
				return err
			}
			if _, exists := seenPrices[key]; exists {
				return validationError("local order book update has a duplicate price")
			}
			seenPrices[key] = struct{}{}
			if message.UpdateType == "snapshot" && size.Sign() == 0 {
				return validationError("local order book snapshot quantity must be positive")
			}
		}
	}
	return nil
}

func newLocalOrderBookState(message ProOrderBookMessage) (*localOrderBookState, error) {
	state := &localOrderBookState{
		sequence: message.Data.SequenceEnd, matchingTime: message.Data.MatchingTime,
		publishTime: message.PublishTime,
		bids:        make(map[string]localOrderBookLevel, len(message.Data.Bids)),
		asks:        make(map[string]localOrderBookLevel, len(message.Data.Asks)),
	}
	if err := applyLocalOrderBookLevels(state.asks, message.Data.Asks); err != nil {
		return nil, err
	}
	if err := applyLocalOrderBookLevels(state.bids, message.Data.Bids); err != nil {
		return nil, err
	}
	return state, nil
}

func applyLocalOrderBookLevels(
	levels map[string]localOrderBookLevel,
	updates []BookLevel,
) error {
	for _, update := range updates {
		key, level, size, err := parseLocalOrderBookLevel(update)
		if err != nil {
			return err
		}
		if size.Sign() == 0 {
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
	size, ok := new(big.Rat).SetString(level.Size)
	if !ok || size.Sign() < 0 {
		return "", localOrderBookLevel{}, nil, validationError(
			"local order book quantity must be a nonnegative decimal",
		)
	}
	return price.RatString(), localOrderBookLevel{
		price: price, priceText: level.Price, size: level.Size,
	}, size, nil
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
		result[index] = BookLevel{Price: level.priceText, Size: level.size}
	}
	return result
}

func (processor *localOrderBookProcessor) view() LocalOrderBookView {
	return LocalOrderBookView{
		Symbol: processor.book.symbol, Generation: processor.generation,
		SynchronizationID: processor.synchronizationID, Sequence: processor.state.sequence,
		MatchingTime: processor.state.matchingTime, PublishTime: processor.state.publishTime,
		GapCount: processor.gapCount,
		Bids:     sortedLocalOrderBookLevels(processor.state.bids, processor.book.viewDepth, true),
		Asks:     sortedLocalOrderBookLevels(processor.state.asks, processor.book.viewDepth, false),
	}
}
