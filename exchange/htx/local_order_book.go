package htx

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strings"

	trade "github.com/proven-trade/proven-trade-sdk"
	"github.com/proven-trade/proven-trade-sdk/transport"
)

const (
	defaultLocalMBPBufferSize = 4096
	defaultLocalMBPViewDepth  = 20
)

var (
	// ErrMBPBufferOverflow는 refresh 정렬 중 보존 가능한 증분 수를 초과했음을 나타낸다.
	ErrMBPBufferOverflow = errors.New("HTX MBP update buffer overflow")
)

// LocalOrderBookConfig는 HTX MBP 증분과 refresh 전체 이미지 동기화 설정이다.
type LocalOrderBookConfig struct {
	Symbol             string
	Depth              StreamMBPDepth
	EgressRouteID      transport.EgressRouteID
	MaxBufferedUpdates int
	ViewDepth          int
}

// LocalOrderBookView는 sequence 연속성을 검증한 시점의 정렬된 상위 호가다.
type LocalOrderBookView struct {
	Symbol            string
	Depth             StreamMBPDepth
	Generation        uint64
	SynchronizationID uint64
	GapCount          uint64
	Sequence          uint64
	Timestamp         int64
	Bids              []BookLevel
	Asks              []BookLevel
}

// LocalOrderBookHandler는 refresh 동기화와 이후 각 유효 증분 장부를 처리한다.
type LocalOrderBookHandler func(context.Context, LocalOrderBookView) error

// LocalOrderBook은 같은 송신 경로의 `/feed` 증분·refresh를 결합해 sequence를 검증한다.
type LocalOrderBook struct {
	symbol             string
	depth              StreamMBPDepth
	routeID            transport.EgressRouteID
	maxBufferedUpdates int
	viewDepth          int
	subscription       StreamSubscription
}

type localMBPInput struct {
	generation uint64
	timestamp  int64
	event      StreamMBPUpdate
}

type localMBPLevel struct {
	price        *big.Rat
	priceText    Decimal
	quantityText Decimal
}

type localMBPState struct {
	sequence  uint64
	timestamp int64
	bids      map[string]localMBPLevel
	asks      map[string]localMBPLevel
}

type localMBPProcessor struct {
	book              *LocalOrderBook
	generation        uint64
	synchronizationID uint64
	gapCount          uint64
	state             *localMBPState
	aligned           bool
	buffer            []localMBPInput
	snapshotID        string
}

// NewLocalOrderBook은 공식 refresh 정렬 절차를 적용할 MBP 로컬 장부를 생성한다.
func NewLocalOrderBook(config LocalOrderBookConfig) (*LocalOrderBook, error) {
	if err := validateSymbol(config.Symbol); err != nil {
		return nil, err
	}
	if !config.Depth.refreshSupported() {
		return nil, validationError("local order book depth must be 5, 20 or 150")
	}
	config.EgressRouteID = transport.EgressRouteID(strings.TrimSpace(string(config.EgressRouteID)))
	if config.EgressRouteID == "" {
		return nil, trade.ErrMissingEgressRoute
	}
	if config.MaxBufferedUpdates == 0 {
		config.MaxBufferedUpdates = defaultLocalMBPBufferSize
	}
	if config.MaxBufferedUpdates < 1 {
		return nil, validationError("local order book buffer size must be positive")
	}
	if config.ViewDepth == 0 {
		config.ViewDepth = defaultLocalMBPViewDepth
		if config.ViewDepth > int(config.Depth) {
			config.ViewDepth = int(config.Depth)
		}
	}
	if config.ViewDepth < 1 || config.ViewDepth > int(config.Depth) {
		return nil, validationError(
			"local order book view depth must be between 1 and %d", config.Depth,
		)
	}
	subscription := StreamSubscription{
		Channel: StreamChannelMBP, Symbol: config.Symbol, MBPDepth: config.Depth,
	}
	return &LocalOrderBook{
		symbol: config.Symbol, depth: config.Depth, routeID: config.EgressRouteID,
		maxBufferedUpdates: config.MaxBufferedUpdates,
		viewDepth:          config.ViewDepth, subscription: subscription,
	}, nil
}

// Run은 MBP stream을 소유하고 sequence gap 발생 시 refresh를 다시 요청한다.
func (book *LocalOrderBook) Run(
	ctx context.Context,
	stream *MBPStream,
	handler LocalOrderBookHandler,
) error {
	if ctx == nil {
		return fmt.Errorf("HTX local order book context is nil")
	}
	if stream == nil || stream.managed == nil || stream.managed.session == nil {
		return fmt.Errorf("HTX MBP stream is required")
	}
	if handler == nil {
		return fmt.Errorf("HTX local order book handler is required")
	}
	if stream.EgressRouteID() != book.routeID {
		return validationError("local order book and MBP WebSocket routes must match")
	}
	if !book.hasSubscription(stream) {
		return validationError("local order book requires its exact MBP subscription")
	}
	processor := &localMBPProcessor{book: book}
	requestSnapshot := func(handlerContext context.Context) error {
		id, err := stream.RequestSnapshot(handlerContext, book.subscription)
		if err != nil {
			if handlerContext.Err() != nil {
				return handlerContext.Err()
			}
			if reconnectErr := stream.reconnect(); reconnectErr != nil {
				return fmt.Errorf("request HTX MBP refresh: %w", err)
			}
			return nil
		}
		processor.snapshotID = id
		return nil
	}
	return stream.Run(ctx, func(handlerContext context.Context, message StreamMessage) error {
		if message.Error != nil {
			return fmt.Errorf(
				"HTX MBP request failed with code %s: %s",
				message.Error.Code, message.Error.Message,
			)
		}
		if message.Channel != StreamChannelMBP || message.Symbol != book.symbol ||
			message.MBPDepth != book.depth {
			return nil
		}
		generation := stream.Generation()
		if message.Reply != "" {
			var snapshot StreamMBPSnapshot
			if err := message.Decode(&snapshot); err != nil {
				return err
			}
			view, publish, request, err := processor.processSnapshot(
				generation, message.ID, message.Timestamp, snapshot,
			)
			if err != nil {
				return err
			}
			if request {
				return requestSnapshot(handlerContext)
			}
			if publish {
				return handler(handlerContext, view)
			}
			return nil
		}
		if len(message.Tick) == 0 {
			return nil
		}
		var update StreamMBPUpdate
		if err := message.Decode(&update); err != nil {
			return err
		}
		view, publish, request, err := processor.processUpdate(localMBPInput{
			generation: generation, timestamp: message.Timestamp, event: update,
		})
		if err != nil {
			return err
		}
		if request {
			return requestSnapshot(handlerContext)
		}
		if publish {
			return handler(handlerContext, view)
		}
		return nil
	})
}

func (book *LocalOrderBook) hasSubscription(stream *MBPStream) bool {
	key := streamSubscriptionKey(book.subscription)
	stream.managed.stateMu.Lock()
	defer stream.managed.stateMu.Unlock()
	_, exists := stream.managed.subscriptions[key]
	return exists
}

func (processor *localMBPProcessor) processUpdate(
	input localMBPInput,
) (LocalOrderBookView, bool, bool, error) {
	if err := processor.validateUpdate(input); err != nil {
		return LocalOrderBookView{}, false, false, err
	}
	if processor.generation != input.generation {
		processor.generation = input.generation
		processor.state = nil
		processor.aligned = false
		processor.buffer = nil
		processor.snapshotID = ""
	}
	if processor.state == nil {
		processor.buffer = append(processor.buffer, input)
		if len(processor.buffer) > processor.book.maxBufferedUpdates {
			return LocalOrderBookView{}, false, false, ErrMBPBufferOverflow
		}
		return LocalOrderBookView{}, false, processor.snapshotID == "", nil
	}
	if input.event.Sequence <= processor.state.sequence {
		return LocalOrderBookView{}, false, false, nil
	}
	if input.event.PreviousSequence != processor.state.sequence {
		processor.gapCount++
		processor.state = nil
		processor.aligned = false
		processor.buffer = append(processor.buffer[:0], input)
		processor.snapshotID = ""
		return LocalOrderBookView{}, false, true, nil
	}
	if err := processor.state.apply(input); err != nil {
		return LocalOrderBookView{}, false, false, err
	}
	processor.state.prune(int(processor.book.depth))
	if err := processor.state.validateSpread(); err != nil {
		return LocalOrderBookView{}, false, false, err
	}
	if !processor.aligned {
		processor.aligned = true
		processor.synchronizationID++
	}
	return processor.view(), true, false, nil
}

func (processor *localMBPProcessor) processSnapshot(
	generation uint64,
	id string,
	timestamp int64,
	snapshot StreamMBPSnapshot,
) (LocalOrderBookView, bool, bool, error) {
	if generation == 0 {
		return LocalOrderBookView{}, false, false,
			validationError("local order book WebSocket generation is zero")
	}
	if generation != processor.generation || processor.snapshotID == "" ||
		id != processor.snapshotID {
		return LocalOrderBookView{}, false, false, nil
	}
	if err := processor.validateSnapshot(snapshot); err != nil {
		return LocalOrderBookView{}, false, false, err
	}
	candidate, err := newLocalMBPState(snapshot, timestamp)
	if err != nil {
		return LocalOrderBookView{}, false, false, err
	}
	first := 0
	for first < len(processor.buffer) &&
		processor.buffer[first].event.Sequence <= snapshot.Sequence {
		first++
	}
	processor.buffer = processor.buffer[first:]
	if len(processor.buffer) == 0 {
		processor.state = candidate
		processor.aligned = false
		processor.snapshotID = ""
		return LocalOrderBookView{}, false, false, nil
	}
	if processor.buffer[0].event.PreviousSequence != snapshot.Sequence {
		processor.snapshotID = ""
		return LocalOrderBookView{}, false, true, nil
	}
	for index, input := range processor.buffer {
		if input.event.PreviousSequence != candidate.sequence {
			processor.gapCount++
			processor.buffer = append([]localMBPInput(nil), processor.buffer[index:]...)
			processor.snapshotID = ""
			return LocalOrderBookView{}, false, true, nil
		}
		if err := candidate.apply(input); err != nil {
			return LocalOrderBookView{}, false, false, err
		}
	}
	candidate.prune(int(processor.book.depth))
	if err := candidate.validateSpread(); err != nil {
		return LocalOrderBookView{}, false, false, err
	}
	processor.state = candidate
	processor.aligned = true
	processor.buffer = nil
	processor.snapshotID = ""
	processor.synchronizationID++
	return processor.view(), true, false, nil
}

func (processor *localMBPProcessor) validateUpdate(input localMBPInput) error {
	if input.generation == 0 {
		return validationError("local order book WebSocket generation is zero")
	}
	if input.timestamp <= 0 || input.event.PreviousSequence == 0 ||
		input.event.Sequence <= input.event.PreviousSequence {
		return validationError("local order book MBP update metadata is invalid")
	}
	if len(input.event.Bids) > int(processor.book.depth) ||
		len(input.event.Asks) > int(processor.book.depth) {
		return validationError("local order book MBP update depth is invalid")
	}
	if err := validateLocalMBPLevels(input.event.Bids, true); err != nil {
		return err
	}
	return validateLocalMBPLevels(input.event.Asks, true)
}

func (processor *localMBPProcessor) validateSnapshot(snapshot StreamMBPSnapshot) error {
	if snapshot.Sequence == 0 || len(snapshot.Bids) > int(processor.book.depth) ||
		len(snapshot.Asks) > int(processor.book.depth) {
		return validationError("local order book MBP snapshot metadata is invalid")
	}
	if err := validateLocalMBPLevels(snapshot.Bids, false); err != nil {
		return err
	}
	return validateLocalMBPLevels(snapshot.Asks, false)
}

func newLocalMBPState(snapshot StreamMBPSnapshot, timestamp int64) (*localMBPState, error) {
	state := &localMBPState{
		sequence: snapshot.Sequence, timestamp: timestamp,
		bids: make(map[string]localMBPLevel, len(snapshot.Bids)),
		asks: make(map[string]localMBPLevel, len(snapshot.Asks)),
	}
	if err := applyLocalMBPLevels(state.bids, snapshot.Bids); err != nil {
		return nil, err
	}
	if err := applyLocalMBPLevels(state.asks, snapshot.Asks); err != nil {
		return nil, err
	}
	if err := state.validateSpread(); err != nil {
		return nil, err
	}
	return state, nil
}

func (state *localMBPState) apply(input localMBPInput) error {
	if err := applyLocalMBPLevels(state.bids, input.event.Bids); err != nil {
		return err
	}
	if err := applyLocalMBPLevels(state.asks, input.event.Asks); err != nil {
		return err
	}
	state.sequence = input.event.Sequence
	state.timestamp = input.timestamp
	return nil
}

func applyLocalMBPLevels(values map[string]localMBPLevel, levels []BookLevel) error {
	for _, level := range levels {
		key, value, quantity, err := parseLocalMBPLevel(level)
		if err != nil {
			return err
		}
		if quantity.Sign() == 0 {
			delete(values, key)
			continue
		}
		values[key] = value
	}
	return nil
}

func validateLocalMBPLevels(levels []BookLevel, allowZero bool) error {
	seen := make(map[string]struct{}, len(levels))
	for _, level := range levels {
		key, _, quantity, err := parseLocalMBPLevel(level)
		if err != nil {
			return err
		}
		if !allowZero && quantity.Sign() == 0 {
			return validationError("local order book snapshot quantity must be positive")
		}
		if _, exists := seen[key]; exists {
			return validationError("local order book update has a duplicate canonical price")
		}
		seen[key] = struct{}{}
	}
	return nil
}

func parseLocalMBPLevel(
	level BookLevel,
) (string, localMBPLevel, *big.Rat, error) {
	price, ok := new(big.Rat).SetString(string(level.Price))
	if !ok || price.Sign() <= 0 {
		return "", localMBPLevel{}, nil,
			validationError("local order book price must be a positive decimal")
	}
	quantity, ok := new(big.Rat).SetString(string(level.Quantity))
	if !ok || quantity.Sign() < 0 {
		return "", localMBPLevel{}, nil,
			validationError("local order book quantity must be a nonnegative decimal")
	}
	return price.RatString(), localMBPLevel{
		price: price, priceText: level.Price, quantityText: level.Quantity,
	}, quantity, nil
}

func (state *localMBPState) prune(depth int) {
	pruneLocalMBPLevels(state.bids, depth, true)
	pruneLocalMBPLevels(state.asks, depth, false)
}

func pruneLocalMBPLevels(values map[string]localMBPLevel, depth int, descending bool) {
	if len(values) <= depth {
		return
	}
	levels := sortedLocalMBPValues(values, len(values), descending)
	for _, level := range levels[depth:] {
		delete(values, level.price.RatString())
	}
}

func (state *localMBPState) validateSpread() error {
	if len(state.bids) == 0 || len(state.asks) == 0 {
		return nil
	}
	bids := sortedLocalMBPValues(state.bids, 1, true)
	asks := sortedLocalMBPValues(state.asks, 1, false)
	if bids[0].price.Cmp(asks[0].price) >= 0 {
		return validationError("local order book is crossed")
	}
	return nil
}

func (processor *localMBPProcessor) view() LocalOrderBookView {
	return LocalOrderBookView{
		Symbol: processor.book.symbol, Depth: processor.book.depth,
		Generation: processor.generation, SynchronizationID: processor.synchronizationID,
		GapCount: processor.gapCount, Sequence: processor.state.sequence,
		Timestamp: processor.state.timestamp,
		Bids:      sortedLocalMBPLevels(processor.state.bids, processor.book.viewDepth, true),
		Asks:      sortedLocalMBPLevels(processor.state.asks, processor.book.viewDepth, false),
	}
}

func sortedLocalMBPValues(
	values map[string]localMBPLevel,
	depth int,
	descending bool,
) []localMBPLevel {
	levels := make([]localMBPLevel, 0, len(values))
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

func sortedLocalMBPLevels(
	values map[string]localMBPLevel,
	depth int,
	descending bool,
) []BookLevel {
	levels := sortedLocalMBPValues(values, depth, descending)
	result := make([]BookLevel, len(levels))
	for index, level := range levels {
		result[index] = BookLevel{Price: level.priceText, Quantity: level.quantityText}
	}
	return result
}
