package cryptocom

import (
	"context"
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/transport"
)

const defaultCryptoComLocalOrderBookViewDepth = 10

// LocalOrderBookConfig는 Crypto.com snapshot·delta 로컬 오더북 설정이다.
type LocalOrderBookConfig struct {
	InstrumentName  string
	Depth           StreamBookDepth
	UpdateFrequency StreamBookUpdateFrequency
	ViewDepth       int
	EgressRouteID   transport.EgressRouteID
}

// LocalOrderBookView는 u·pu 연속성이 검증된 시점의 정렬된 상위 호가다.
type LocalOrderBookView struct {
	InstrumentName    string
	Depth             StreamBookDepth
	UpdateFrequency   StreamBookUpdateFrequency
	Generation        uint64
	SynchronizationID uint64
	GapCount          uint64
	Sequence          uint64
	Timestamp         int64
	TransactionTime   int64
	Bids              []BookLevel
	Asks              []BookLevel
}

// LocalOrderBookHandler는 snapshot 교체와 이후 각 유효 delta의 장부를 처리한다.
type LocalOrderBookHandler func(context.Context, LocalOrderBookView) error

// LocalOrderBook은 Crypto.com full snapshot과 절대 수량 delta를 결합한다.
type LocalOrderBook struct {
	instrumentName  string
	depth           StreamBookDepth
	updateFrequency StreamBookUpdateFrequency
	viewDepth       int
	routeID         transport.EgressRouteID
	subscription    StreamSubscription
}

type localOrderBookLevel struct {
	priceText    Decimal
	quantityText Decimal
	orderCount   Integer
	price        *big.Rat
}

type localOrderBookState struct {
	bids            map[string]localOrderBookLevel
	asks            map[string]localOrderBookLevel
	sequence        uint64
	timestamp       int64
	transactionTime int64
}

type localOrderBookProcessor struct {
	book               *LocalOrderBook
	generation         uint64
	recoveryGeneration uint64
	synchronizationID  uint64
	gapCount           uint64
	state              *localOrderBookState
}

// NewLocalOrderBook은 명시적 10·50단계 증분 구독 전용 로컬 오더북을 생성한다.
func NewLocalOrderBook(config LocalOrderBookConfig) (*LocalOrderBook, error) {
	if err := validateInstrumentName(config.InstrumentName); err != nil {
		return nil, err
	}
	if !config.Depth.valid() {
		return nil, validationError("local order book depth must be 10 or 50")
	}
	if config.UpdateFrequency == "" {
		config.UpdateFrequency = StreamBookUpdate100Milliseconds
	}
	if !config.UpdateFrequency.valid() {
		return nil, validationError("unsupported local order book update frequency %q", config.UpdateFrequency)
	}
	config.EgressRouteID = transport.EgressRouteID(strings.TrimSpace(string(config.EgressRouteID)))
	if config.EgressRouteID == "" {
		return nil, trade.ErrMissingEgressRoute
	}
	if config.ViewDepth == 0 {
		config.ViewDepth = defaultCryptoComLocalOrderBookViewDepth
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
		Channel: StreamChannelBook, InstrumentName: config.InstrumentName,
		BookDepth: config.Depth, BookSubscriptionType: StreamBookSnapshotAndUpdate,
		BookUpdateFrequency: config.UpdateFrequency,
	}
	return &LocalOrderBook{
		instrumentName: config.InstrumentName, depth: config.Depth,
		updateFrequency: config.UpdateFrequency, viewDepth: config.ViewDepth,
		routeID: config.EgressRouteID, subscription: subscription,
	}, nil
}

// Subscription은 public stream에 넣어야 하는 정확한 증분 호가 구독을 반환한다.
func (book *LocalOrderBook) Subscription() StreamSubscription {
	return book.subscription
}

// Run은 public stream을 소유하고 sequence gap 발생 시 같은 송신 경로로 재연결한다.
func (book *LocalOrderBook) Run(
	ctx context.Context,
	public *PublicStream,
	handler LocalOrderBookHandler,
) error {
	if ctx == nil {
		return fmt.Errorf("Crypto.com local order book context is nil")
	}
	if public == nil || public.managed == nil || public.managed.session == nil {
		return fmt.Errorf("Crypto.com local order book public stream is required")
	}
	if handler == nil {
		return fmt.Errorf("Crypto.com local order book handler is required")
	}
	if public.EgressRouteID() != book.routeID {
		return validationError("local order book and public WebSocket routes must match")
	}
	if !book.hasSubscription(public) {
		return validationError("public stream does not contain the exact incremental book subscription")
	}
	processor := &localOrderBookProcessor{book: book}
	return public.Run(ctx, func(handlerContext context.Context, message StreamMessage) error {
		if message.Error != nil {
			return fmt.Errorf(
				"Crypto.com order book subscription failed with code %s: %s",
				message.Error.Code, message.Error.Message,
			)
		}
		if message.Heartbeat || message.Channel != StreamChannelBook ||
			message.InstrumentName != book.instrumentName || message.Depth != int(book.depth) {
			return nil
		}
		var events []StreamBookEvent
		if err := message.Decode(&events); err != nil {
			return err
		}
		if len(events) == 0 {
			return validationError("local order book message data is empty")
		}
		for _, event := range events {
			view, reconnect, err := processor.process(public.Generation(), event)
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

func (book *LocalOrderBook) hasSubscription(public *PublicStream) bool {
	key := cryptoComStreamSubscriptionKey(book.subscription)
	public.managed.stateMu.Lock()
	defer public.managed.stateMu.Unlock()
	subscription, exists := public.managed.subscriptions[key]
	return exists && subscription == book.subscription
}

func (processor *localOrderBookProcessor) process(
	generation uint64,
	event StreamBookEvent,
) (*LocalOrderBookView, bool, error) {
	if generation == 0 {
		return nil, false, validationError("local order book WebSocket generation is zero")
	}
	if generation < processor.generation {
		return nil, false, nil
	}
	if processor.generation == 0 || generation > processor.generation {
		processor.generation = generation
		processor.recoveryGeneration = 0
		processor.state = nil
	}
	if processor.recoveryGeneration == generation {
		return nil, false, nil
	}
	metadata, err := processor.book.validateEvent(event)
	if err != nil {
		return nil, false, err
	}
	if event.Update == nil {
		state, err := newCryptoComLocalOrderBookState(event, metadata, int(processor.book.depth))
		if err != nil {
			return nil, false, err
		}
		processor.state = state
		processor.synchronizationID++
		view := state.view(processor)
		return &view, false, nil
	}
	if processor.state == nil || metadata.previousSequence != processor.state.sequence ||
		metadata.sequence <= processor.state.sequence {
		return processor.sequenceGap()
	}
	if err := processor.state.applyUpdate(
		event, metadata, int(processor.book.depth),
	); err != nil {
		return nil, false, err
	}
	view := processor.state.view(processor)
	return &view, false, nil
}

func (processor *localOrderBookProcessor) sequenceGap() (*LocalOrderBookView, bool, error) {
	processor.gapCount++
	processor.recoveryGeneration = processor.generation
	processor.state = nil
	return nil, true, nil
}

type cryptoComLocalOrderBookMetadata struct {
	sequence         uint64
	previousSequence uint64
	timestamp        int64
	transactionTime  int64
}

func (book *LocalOrderBook) validateEvent(
	event StreamBookEvent,
) (cryptoComLocalOrderBookMetadata, error) {
	sequence, err := cryptoComLocalPositiveUint(event.Sequence, "sequence")
	if err != nil {
		return cryptoComLocalOrderBookMetadata{}, err
	}
	transactionTime, err := cryptoComLocalPositiveInt(event.TransactionTime, "transaction time")
	if err != nil {
		return cryptoComLocalOrderBookMetadata{}, err
	}
	metadata := cryptoComLocalOrderBookMetadata{
		sequence: sequence, transactionTime: transactionTime,
	}
	if event.Timestamp != "" {
		metadata.timestamp, err = cryptoComLocalPositiveInt(event.Timestamp, "timestamp")
		if err != nil {
			return cryptoComLocalOrderBookMetadata{}, err
		}
	}
	if event.Update == nil {
		if metadata.timestamp == 0 || event.PreviousSequence != "" && event.PreviousSequence != "0" {
			return cryptoComLocalOrderBookMetadata{},
				validationError("local order book snapshot metadata is invalid")
		}
		if len(event.Bids) > int(book.depth) || len(event.Asks) > int(book.depth) {
			return cryptoComLocalOrderBookMetadata{},
				validationError("local order book snapshot exceeds subscribed depth")
		}
		if err := validateCryptoComLocalOrderBookLevels(event.Bids, false); err != nil {
			return cryptoComLocalOrderBookMetadata{}, err
		}
		if err := validateCryptoComLocalOrderBookLevels(event.Asks, false); err != nil {
			return cryptoComLocalOrderBookMetadata{}, err
		}
		return metadata, nil
	}
	if len(event.Bids) != 0 || len(event.Asks) != 0 {
		return cryptoComLocalOrderBookMetadata{},
			validationError("local order book delta contains snapshot levels")
	}
	metadata.previousSequence, err = cryptoComLocalPositiveUint(
		event.PreviousSequence, "previous sequence",
	)
	if err != nil {
		return cryptoComLocalOrderBookMetadata{},
			validationError("local order book delta sequence is invalid")
	}
	if len(event.Update.Bids) > int(book.depth) || len(event.Update.Asks) > int(book.depth) {
		return cryptoComLocalOrderBookMetadata{},
			validationError("local order book delta exceeds subscribed depth")
	}
	if err := validateCryptoComLocalOrderBookLevels(event.Update.Bids, true); err != nil {
		return cryptoComLocalOrderBookMetadata{}, err
	}
	if err := validateCryptoComLocalOrderBookLevels(event.Update.Asks, true); err != nil {
		return cryptoComLocalOrderBookMetadata{}, err
	}
	return metadata, nil
}

func newCryptoComLocalOrderBookState(
	event StreamBookEvent,
	metadata cryptoComLocalOrderBookMetadata,
	depth int,
) (*localOrderBookState, error) {
	state := &localOrderBookState{
		bids: make(map[string]localOrderBookLevel, len(event.Bids)),
		asks: make(map[string]localOrderBookLevel, len(event.Asks)),
	}
	if err := applyCryptoComLocalOrderBookLevels(state.bids, event.Bids); err != nil {
		return nil, err
	}
	if err := applyCryptoComLocalOrderBookLevels(state.asks, event.Asks); err != nil {
		return nil, err
	}
	state.sequence = metadata.sequence
	state.timestamp = metadata.timestamp
	state.transactionTime = metadata.transactionTime
	state.prune(depth)
	if err := state.validateSpread(); err != nil {
		return nil, err
	}
	return state, nil
}

func (state *localOrderBookState) applyUpdate(
	event StreamBookEvent,
	metadata cryptoComLocalOrderBookMetadata,
	depth int,
) error {
	if err := applyCryptoComLocalOrderBookLevels(state.bids, event.Update.Bids); err != nil {
		return err
	}
	if err := applyCryptoComLocalOrderBookLevels(state.asks, event.Update.Asks); err != nil {
		return err
	}
	state.prune(depth)
	if err := state.validateSpread(); err != nil {
		return err
	}
	state.sequence = metadata.sequence
	if metadata.timestamp != 0 {
		state.timestamp = metadata.timestamp
	}
	state.transactionTime = metadata.transactionTime
	return nil
}

func validateCryptoComLocalOrderBookLevels(levels []BookLevel, allowZero bool) error {
	seen := make(map[string]struct{}, len(levels))
	for _, level := range levels {
		key, _, quantity, err := parseCryptoComLocalOrderBookLevel(level)
		if err != nil {
			return err
		}
		if !allowZero && quantity.Sign() == 0 {
			return validationError("local order book snapshot quantity must be positive")
		}
		if quantity.Sign() == 0 && level.OrderCount != 0 ||
			quantity.Sign() > 0 && level.OrderCount <= 0 {
			return validationError("local order book level order count is invalid")
		}
		if _, exists := seen[key]; exists {
			return validationError("local order book event has a duplicate canonical price")
		}
		seen[key] = struct{}{}
	}
	return nil
}

func applyCryptoComLocalOrderBookLevels(
	levels map[string]localOrderBookLevel,
	updates []BookLevel,
) error {
	for _, level := range updates {
		key, parsed, quantity, err := parseCryptoComLocalOrderBookLevel(level)
		if err != nil {
			return err
		}
		if quantity.Sign() == 0 {
			delete(levels, key)
			continue
		}
		levels[key] = parsed
	}
	return nil
}

func parseCryptoComLocalOrderBookLevel(
	level BookLevel,
) (string, localOrderBookLevel, *big.Rat, error) {
	price, ok := new(big.Rat).SetString(string(level.Price))
	if !ok || price.Sign() <= 0 {
		return "", localOrderBookLevel{}, nil,
			validationError("local order book price must be a positive decimal")
	}
	quantity, ok := new(big.Rat).SetString(string(level.Quantity))
	if !ok || quantity.Sign() < 0 {
		return "", localOrderBookLevel{}, nil,
			validationError("local order book quantity must be a nonnegative decimal")
	}
	if level.OrderCount < 0 {
		return "", localOrderBookLevel{}, nil,
			validationError("local order book order count cannot be negative")
	}
	return price.RatString(), localOrderBookLevel{
		priceText: level.Price, quantityText: level.Quantity,
		orderCount: level.OrderCount, price: price,
	}, quantity, nil
}

func (state *localOrderBookState) prune(depth int) {
	pruneCryptoComLocalOrderBookLevels(state.bids, depth, true)
	pruneCryptoComLocalOrderBookLevels(state.asks, depth, false)
}

func pruneCryptoComLocalOrderBookLevels(
	values map[string]localOrderBookLevel,
	depth int,
	descending bool,
) {
	levels := sortedCryptoComLocalOrderBookLevels(values, len(values), descending)
	for index := depth; index < len(levels); index++ {
		delete(values, levels[index].price.RatString())
	}
}

func sortedCryptoComLocalOrderBookLevels(
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

func (state *localOrderBookState) validateSpread() error {
	if len(state.bids) == 0 || len(state.asks) == 0 {
		return nil
	}
	bestBid := sortedCryptoComLocalOrderBookLevels(state.bids, 1, true)[0]
	bestAsk := sortedCryptoComLocalOrderBookLevels(state.asks, 1, false)[0]
	if bestBid.price.Cmp(bestAsk.price) >= 0 {
		return validationError("local order book is crossed or locked")
	}
	return nil
}

func (state *localOrderBookState) view(
	processor *localOrderBookProcessor,
) LocalOrderBookView {
	book := processor.book
	return LocalOrderBookView{
		InstrumentName: book.instrumentName, Depth: book.depth,
		UpdateFrequency: book.updateFrequency, Generation: processor.generation,
		SynchronizationID: processor.synchronizationID, GapCount: processor.gapCount,
		Sequence: state.sequence, Timestamp: state.timestamp,
		TransactionTime: state.transactionTime,
		Bids:            localCryptoComOrderBookViewLevels(state.bids, book.viewDepth, true),
		Asks:            localCryptoComOrderBookViewLevels(state.asks, book.viewDepth, false),
	}
}

func localCryptoComOrderBookViewLevels(
	values map[string]localOrderBookLevel,
	depth int,
	descending bool,
) []BookLevel {
	levels := sortedCryptoComLocalOrderBookLevels(values, depth, descending)
	result := make([]BookLevel, len(levels))
	for index, level := range levels {
		result[index] = BookLevel{
			Price: level.priceText, Quantity: level.quantityText,
			OrderCount: level.orderCount,
		}
	}
	return result
}

func cryptoComLocalPositiveUint(value Scalar, field string) (uint64, error) {
	parsed, err := strconv.ParseUint(string(value), 10, 64)
	if err != nil || parsed == 0 {
		return 0, validationError("local order book %s is invalid", field)
	}
	return parsed, nil
}

func cryptoComLocalPositiveInt(value Scalar, field string) (int64, error) {
	parsed, err := strconv.ParseInt(string(value), 10, 64)
	if err != nil || parsed <= 0 {
		return 0, validationError("local order book %s is invalid", field)
	}
	return parsed, nil
}
