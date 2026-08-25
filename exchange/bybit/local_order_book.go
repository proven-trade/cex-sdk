package bybit

import (
	"context"
	"fmt"
	"math"
	"math/big"
	"sort"
	"strings"

	trade "github.com/proven-trade/proven-trade-sdk"
	"github.com/proven-trade/proven-trade-sdk/transport"
)

const defaultLocalOrderBookViewDepth = 20

// LocalOrderBookConfig는 Bybit V5 snapshot·delta 로컬 오더북 설정이다.
type LocalOrderBookConfig struct {
	Category      Category
	Symbol        string
	Depth         int
	ViewDepth     int
	EgressRouteID transport.EgressRouteID
}

// LocalOrderBookView는 update ID가 검증된 시점의 정렬된 상위 호가다.
type LocalOrderBookView struct {
	Category          Category
	Symbol            string
	Depth             int
	Generation        uint64
	SynchronizationID uint64
	GapCount          uint64
	UpdateID          int64
	Sequence          int64
	Timestamp         int64
	MatchingTime      int64
	Bids              [][]string
	Asks              [][]string
}

// LocalOrderBookHandler는 snapshot 교체와 이후 각 유효 delta의 장부를 처리한다.
type LocalOrderBookHandler func(context.Context, LocalOrderBookView) error

// LocalOrderBook은 Bybit snapshot과 delta를 결합하고 sequence 이상 시 같은 송신 경로로 재연결한다.
type LocalOrderBook struct {
	category  Category
	symbol    string
	depth     int
	viewDepth int
	routeID   transport.EgressRouteID
	topic     string
}

type localOrderBookLevel struct {
	priceText    string
	quantityText string
	price        *big.Rat
}

type localOrderBookState struct {
	bids         map[string]localOrderBookLevel
	asks         map[string]localOrderBookLevel
	updateID     int64
	sequence     int64
	timestamp    int64
	matchingTime int64
}

type localOrderBookProcessor struct {
	book              *LocalOrderBook
	generation        uint64
	synchronizationID uint64
	gapCount          uint64
	state             *localOrderBookState
}

// NewLocalOrderBook은 특정 category·symbol·depth topic 전용 로컬 오더북을 생성한다.
func NewLocalOrderBook(config LocalOrderBookConfig) (*LocalOrderBook, error) {
	if err := validateCategory(config.Category); err != nil {
		return nil, err
	}
	config.Symbol = strings.TrimSpace(config.Symbol)
	topic, err := OrderBookStreamTopic(config.Category, config.Symbol, config.Depth)
	if err != nil {
		return nil, err
	}
	config.EgressRouteID = transport.EgressRouteID(strings.TrimSpace(string(config.EgressRouteID)))
	if config.EgressRouteID == "" {
		return nil, trade.ErrMissingEgressRoute
	}
	if config.ViewDepth == 0 {
		config.ViewDepth = defaultLocalOrderBookViewDepth
		if config.ViewDepth > config.Depth {
			config.ViewDepth = config.Depth
		}
	}
	if config.ViewDepth < 1 || config.ViewDepth > config.Depth {
		return nil, validationError("local order book view depth must not exceed subscribed depth")
	}
	return &LocalOrderBook{
		category: config.Category, symbol: config.Symbol, depth: config.Depth,
		viewDepth: config.ViewDepth, routeID: config.EgressRouteID, topic: topic,
	}, nil
}

// Run은 public stream을 소유하고 snapshot·delta 장부를 handler에 전달한다.
func (book *LocalOrderBook) Run(
	ctx context.Context,
	public *PublicStream,
	handler LocalOrderBookHandler,
) error {
	if ctx == nil {
		return fmt.Errorf("Bybit local order book context is nil")
	}
	if public == nil {
		return fmt.Errorf("Bybit local order book public stream is required")
	}
	if handler == nil {
		return fmt.Errorf("Bybit local order book handler is required")
	}
	if public.EgressRouteID() != book.routeID {
		return validationError("local order book and public WebSocket routes must match")
	}
	if !public.hasTopic(book.category, book.topic) {
		return validationError("public stream does not contain the required order book topic")
	}
	processor := &localOrderBookProcessor{book: book}
	return public.Run(ctx, func(handlerContext context.Context, message StreamMessage) error {
		if message.Operation != "" {
			if message.Success != nil && !*message.Success {
				return fmt.Errorf(
					"Bybit order book subscription failed: %s",
					message.ReturnMessage,
				)
			}
			return nil
		}
		if message.Pong || message.Topic != book.topic {
			return nil
		}
		var event StreamOrderBook
		if err := message.DecodeData(&event); err != nil {
			return err
		}
		view, reconnect, err := processor.process(
			public.Generation(), message.Type, message.Timestamp, event,
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
	messageType string,
	timestamp int64,
	event StreamOrderBook,
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
	if err := processor.book.validateEvent(messageType, event); err != nil {
		return nil, false, err
	}
	switch messageType {
	case "snapshot":
		state, err := newLocalOrderBookState(timestamp, event, processor.book.depth)
		if err != nil {
			return nil, false, err
		}
		processor.state = state
		processor.synchronizationID++
		view := state.view(processor)
		return &view, false, nil
	case "delta":
		if processor.state == nil || event.UpdateID == 1 {
			return processor.sequenceGap()
		}
		state := processor.state
		if event.UpdateID == state.updateID {
			return nil, false, nil
		}
		if state.updateID == math.MaxInt64 || event.UpdateID != state.updateID+1 ||
			event.Sequence <= state.sequence {
			return processor.sequenceGap()
		}
		if err := state.apply(timestamp, event, processor.book.depth); err != nil {
			return nil, false, err
		}
		view := state.view(processor)
		return &view, false, nil
	default:
		return nil, false, validationError("local order book message type is invalid")
	}
}

func (processor *localOrderBookProcessor) sequenceGap() (*LocalOrderBookView, bool, error) {
	processor.gapCount++
	processor.state = nil
	return nil, true, nil
}

func (book *LocalOrderBook) validateEvent(messageType string, event StreamOrderBook) error {
	if messageType != "snapshot" && messageType != "delta" {
		return validationError("local order book message type is invalid")
	}
	if event.Symbol != book.symbol || event.UpdateID <= 0 || event.Sequence <= 0 {
		return validationError("local order book event identity or sequence is invalid")
	}
	if messageType == "delta" && book.depth == 1 {
		return validationError("level 1 order book must contain snapshots only")
	}
	if messageType == "snapshot" &&
		(len(event.Bids) > book.depth || len(event.Asks) > book.depth) {
		return validationError("local order book snapshot exceeds subscribed depth")
	}
	for _, fields := range append(append([][]string(nil), event.Bids...), event.Asks...) {
		if _, _, _, err := parseLocalOrderBookLevel(fields); err != nil {
			return err
		}
	}
	return nil
}

func newLocalOrderBookState(
	timestamp int64,
	event StreamOrderBook,
	depth int,
) (*localOrderBookState, error) {
	state := &localOrderBookState{
		bids: make(map[string]localOrderBookLevel, len(event.Bids)),
		asks: make(map[string]localOrderBookLevel, len(event.Asks)),
	}
	if err := state.apply(timestamp, event, depth); err != nil {
		return nil, err
	}
	return state, nil
}

func (state *localOrderBookState) apply(
	timestamp int64,
	event StreamOrderBook,
	depth int,
) error {
	for _, fields := range event.Bids {
		if err := applyLocalOrderBookLevel(state.bids, fields); err != nil {
			return err
		}
	}
	for _, fields := range event.Asks {
		if err := applyLocalOrderBookLevel(state.asks, fields); err != nil {
			return err
		}
	}
	pruneLocalOrderBookLevels(state.bids, depth, true)
	pruneLocalOrderBookLevels(state.asks, depth, false)
	state.updateID = event.UpdateID
	state.sequence = event.Sequence
	state.timestamp = timestamp
	state.matchingTime = event.MatchingTime
	return nil
}

func applyLocalOrderBookLevel(
	levels map[string]localOrderBookLevel,
	fields []string,
) error {
	key, price, quantity, err := parseLocalOrderBookLevel(fields)
	if err != nil {
		return err
	}
	if strings.Trim(quantity, "0.") == "" {
		delete(levels, key)
		return nil
	}
	levels[key] = localOrderBookLevel{
		priceText: fields[0], quantityText: quantity, price: price,
	}
	return nil
}

func parseLocalOrderBookLevel(fields []string) (string, *big.Rat, string, error) {
	if len(fields) != 2 || !positiveDecimalPattern.MatchString(fields[0]) ||
		strings.Trim(fields[0], "0.") == "" ||
		!positiveDecimalPattern.MatchString(fields[1]) {
		return "", nil, "", validationError("local order book level is invalid")
	}
	price, ok := new(big.Rat).SetString(fields[0])
	if !ok || price.Sign() <= 0 {
		return "", nil, "", validationError("local order book price is invalid")
	}
	return price.RatString(), price, fields[1], nil
}

func pruneLocalOrderBookLevels(
	values map[string]localOrderBookLevel,
	depth int,
	descending bool,
) {
	levels := sortedLocalOrderBookLevels(values, len(values), descending)
	for index := depth; index < len(levels); index++ {
		price, _ := new(big.Rat).SetString(levels[index][0])
		delete(values, price.RatString())
	}
}

func sortedLocalOrderBookLevels(
	values map[string]localOrderBookLevel,
	depth int,
	descending bool,
) [][]string {
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
	result := make([][]string, len(levels))
	for index, level := range levels {
		result[index] = []string{level.priceText, level.quantityText}
	}
	return result
}

func (state *localOrderBookState) view(
	processor *localOrderBookProcessor,
) LocalOrderBookView {
	book := processor.book
	return LocalOrderBookView{
		Category: book.category, Symbol: book.symbol, Depth: book.depth,
		Generation: processor.generation, SynchronizationID: processor.synchronizationID,
		GapCount: processor.gapCount, UpdateID: state.updateID, Sequence: state.sequence,
		Timestamp: state.timestamp, MatchingTime: state.matchingTime,
		Bids: sortedLocalOrderBookLevels(state.bids, book.viewDepth, true),
		Asks: sortedLocalOrderBookLevels(state.asks, book.viewDepth, false),
	}
}
