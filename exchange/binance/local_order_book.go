package binance

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/big"
	"sort"
	"strings"
	"time"

	trade "github.com/proven-trade/proven-trade-sdk"
	"github.com/proven-trade/proven-trade-sdk/transport"
)

const (
	defaultDepthSnapshotLimit = 5000
	defaultDepthBufferSize    = 4096
	defaultDepthViewSize      = 20
	defaultSnapshotTimeout    = 10 * time.Second
	defaultSnapshotRetry      = 250 * time.Millisecond
)

var (
	// ErrDepthBufferOverflow는 동기화 중 보존 가능한 이벤트 수를 초과했음을 나타낸다.
	ErrDepthBufferOverflow = errors.New("Binance depth event buffer overflow")
)

// LocalOrderBookConfig는 Binance Spot diff depth와 REST snapshot 동기화 설정이다.
type LocalOrderBookConfig struct {
	RESTClient            *Client
	Symbol                string
	EgressRouteID         transport.EgressRouteID
	SnapshotLimit         int
	MaxBufferedEvents     int
	ViewDepth             int
	SnapshotTimeout       time.Duration
	SnapshotRetryInterval time.Duration
}

// LocalOrderBookView는 sequence가 검증된 시점의 정렬된 상위 호가다.
type LocalOrderBookView struct {
	Symbol            string
	Generation        uint64
	SynchronizationID uint64
	GapCount          uint64
	LastUpdateID      int64
	EventTime         int64
	Bids              []BookLevel
	Asks              []BookLevel
}

// LocalOrderBookHandler는 동기화 완료와 이후 각 유효 delta의 장부를 처리한다.
type LocalOrderBookHandler func(context.Context, LocalOrderBookView) error

type depthSnapshotLoader func(context.Context) (OrderBook, error)

// LocalOrderBook은 WebSocket delta를 버퍼링하고 같은 송신 경로의 REST snapshot과 결합한다.
type LocalOrderBook struct {
	symbol                string
	routeID               transport.EgressRouteID
	maxBufferedEvents     int
	viewDepth             int
	snapshotTimeout       time.Duration
	snapshotRetryInterval time.Duration
	loadSnapshot          depthSnapshotLoader
}

type depthInput struct {
	generation uint64
	event      DepthEvent
}

type depthSnapshotResult struct {
	snapshot OrderBook
	err      error
}

type localDepthLevel struct {
	priceText    string
	quantityText string
	price        *big.Rat
}

type localDepthState struct {
	bids         map[string]localDepthLevel
	asks         map[string]localDepthLevel
	lastUpdateID int64
	eventTime    int64
}

// NewLocalOrderBook은 route 고정 snapshot loader와 diff depth 동기화기를 생성한다.
func NewLocalOrderBook(config LocalOrderBookConfig) (*LocalOrderBook, error) {
	if config.RESTClient == nil {
		return nil, fmt.Errorf("Binance local order book REST client is required")
	}
	if err := validateSymbol(config.Symbol); err != nil {
		return nil, err
	}
	config.Symbol = strings.ToUpper(config.Symbol)
	config.EgressRouteID = transport.EgressRouteID(strings.TrimSpace(string(config.EgressRouteID)))
	if config.EgressRouteID == "" {
		return nil, trade.ErrMissingEgressRoute
	}
	if config.SnapshotLimit == 0 {
		config.SnapshotLimit = defaultDepthSnapshotLimit
	}
	if config.SnapshotLimit < 1 || config.SnapshotLimit > 5000 {
		return nil, validationError("local order book snapshot limit must be between 1 and 5000")
	}
	if config.MaxBufferedEvents == 0 {
		config.MaxBufferedEvents = defaultDepthBufferSize
	}
	if config.MaxBufferedEvents < 1 {
		return nil, validationError("local order book event buffer size must be positive")
	}
	if config.ViewDepth == 0 {
		config.ViewDepth = defaultDepthViewSize
		if config.ViewDepth > config.SnapshotLimit {
			config.ViewDepth = config.SnapshotLimit
		}
	}
	if config.ViewDepth < 1 || config.ViewDepth > config.SnapshotLimit {
		return nil, validationError("local order book view depth must not exceed snapshot limit")
	}
	if config.SnapshotTimeout == 0 {
		config.SnapshotTimeout = defaultSnapshotTimeout
	}
	if config.SnapshotRetryInterval == 0 {
		config.SnapshotRetryInterval = defaultSnapshotRetry
	}
	if config.SnapshotTimeout < 0 || config.SnapshotRetryInterval < 0 {
		return nil, validationError("local order book snapshot durations must be positive")
	}
	book := &LocalOrderBook{
		symbol: config.Symbol, routeID: config.EgressRouteID,
		maxBufferedEvents: config.MaxBufferedEvents, viewDepth: config.ViewDepth,
		snapshotTimeout:       config.SnapshotTimeout,
		snapshotRetryInterval: config.SnapshotRetryInterval,
	}
	book.loadSnapshot = func(ctx context.Context) (OrderBook, error) {
		return config.RESTClient.OrderBook(
			ctx, OrderBookRequest{Symbol: config.Symbol, Limit: config.SnapshotLimit},
			trade.WithEgressRoute(config.EgressRouteID),
			trade.WithTimeout(config.SnapshotTimeout),
		)
	}
	return book, nil
}

// Run은 diff depth stream을 소유하고 snapshot·delta 동기화 장부를 handler에 전달한다.
func (book *LocalOrderBook) Run(
	ctx context.Context,
	market *MarketStream,
	handler LocalOrderBookHandler,
) error {
	if ctx == nil {
		return fmt.Errorf("Binance local order book context is nil")
	}
	if market == nil {
		return fmt.Errorf("Binance local order book market stream is required")
	}
	if handler == nil {
		return fmt.Errorf("Binance local order book handler is required")
	}
	if market.EgressRouteID() != book.routeID {
		return validationError("local order book REST and WebSocket routes must match")
	}
	if !market.hasDiffDepthStream(book.symbol) {
		return validationError("market stream does not contain the required diff depth subscription")
	}
	runContext, cancel := context.WithCancel(ctx)
	defer cancel()
	inputs := make(chan depthInput, book.maxBufferedEvents)
	streamDone := make(chan error, 1)
	go func() {
		streamDone <- market.Run(runContext, func(
			handlerContext context.Context,
			message MarketStreamMessage,
		) error {
			if message.Response != nil {
				if message.Response.Error != nil {
					return fmt.Errorf(
						"Binance depth subscription failed with code %d: %s",
						message.Response.Error.Code,
						message.Response.Error.Message,
					)
				}
				return nil
			}
			if message.EventType != "depthUpdate" {
				return nil
			}
			var event DepthEvent
			if err := message.Decode(&event); err != nil {
				return err
			}
			input := depthInput{generation: market.Generation(), event: event}
			select {
			case inputs <- input:
				return nil
			case <-handlerContext.Done():
				return handlerContext.Err()
			default:
				return ErrDepthBufferOverflow
			}
		})
	}()
	return book.runInputs(runContext, inputs, streamDone, handler)
}

func (book *LocalOrderBook) runInputs(
	ctx context.Context,
	inputs <-chan depthInput,
	streamDone <-chan error,
	handler LocalOrderBookHandler,
) error {
	var (
		buffer            []depthInput
		currentGeneration uint64
		state             *localDepthState
		pendingSnapshot   *OrderBook
		synchronizationID uint64
		gapCount          uint64
		fetching          bool
		snapshotResults   = make(chan depthSnapshotResult, 1)
		retryTimer        *time.Timer
		retry             <-chan time.Time
	)
	stopRetry := func() {
		if retryTimer != nil {
			if !retryTimer.Stop() {
				select {
				case <-retryTimer.C:
				default:
				}
			}
			retryTimer = nil
			retry = nil
		}
	}
	defer stopRetry()
	startSnapshot := func() {
		if fetching {
			return
		}
		stopRetry()
		fetching = true
		go func() {
			snapshotContext, cancel := context.WithTimeout(ctx, book.snapshotTimeout)
			snapshot, err := book.loadSnapshot(snapshotContext)
			cancel()
			select {
			case snapshotResults <- depthSnapshotResult{snapshot: snapshot, err: err}:
			case <-ctx.Done():
			}
		}()
	}
	scheduleRetry := func() {
		if retryTimer != nil {
			return
		}
		retryTimer = time.NewTimer(book.snapshotRetryInterval)
		retry = retryTimer.C
	}
	publish := func() error {
		view := state.view(
			book.symbol, currentGeneration, synchronizationID, gapCount, book.viewDepth,
		)
		return handler(ctx, view)
	}
	trySynchronize := func() (bool, error) {
		if pendingSnapshot == nil || len(buffer) == 0 {
			return false, nil
		}
		snapshot := *pendingSnapshot
		firstRelevant := 0
		for firstRelevant < len(buffer) &&
			buffer[firstRelevant].event.FinalUpdateID <= snapshot.LastUpdateID {
			firstRelevant++
		}
		buffer = buffer[firstRelevant:]
		if len(buffer) == 0 {
			return false, nil
		}
		if snapshot.LastUpdateID == math.MaxInt64 {
			return false, validationError("local order book snapshot update ID is invalid")
		}
		target := snapshot.LastUpdateID + 1
		first := buffer[0].event
		if first.FirstUpdateID > target {
			pendingSnapshot = nil
			scheduleRetry()
			return false, nil
		}
		candidate, err := newLocalDepthState(snapshot)
		if err != nil {
			return false, err
		}
		for index, input := range buffer {
			applied, gap, err := candidate.apply(input.event)
			if err != nil {
				return false, err
			}
			if gap {
				gapCount++
				buffer = append([]depthInput(nil), buffer[index:]...)
				pendingSnapshot = nil
				scheduleRetry()
				return false, nil
			}
			if !applied {
				continue
			}
		}
		state = candidate
		pendingSnapshot = nil
		buffer = nil
		synchronizationID++
		return true, nil
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case streamErr := <-streamDone:
			return streamErr
		case <-retry:
			retryTimer = nil
			retry = nil
			startSnapshot()
		case result := <-snapshotResults:
			fetching = false
			if result.err != nil {
				scheduleRetry()
				continue
			}
			if err := validateDepthSnapshot(result.snapshot); err != nil {
				return err
			}
			pendingSnapshot = &result.snapshot
			synchronized, err := trySynchronize()
			if err != nil {
				return err
			}
			if synchronized {
				if err := publish(); err != nil {
					return err
				}
			}
		case input := <-inputs:
			if err := book.validateInput(input); err != nil {
				return err
			}
			if input.generation < currentGeneration {
				continue
			}
			if currentGeneration == 0 || input.generation > currentGeneration {
				currentGeneration = input.generation
				state = nil
				pendingSnapshot = nil
				buffer = buffer[:0]
			}
			if state != nil {
				applied, gap, err := state.apply(input.event)
				if err != nil {
					return err
				}
				if gap {
					gapCount++
					state = nil
					pendingSnapshot = nil
					buffer = append(buffer[:0], input)
					startSnapshot()
					continue
				}
				if applied {
					if err := publish(); err != nil {
						return err
					}
				}
				continue
			}
			buffer = append(buffer, input)
			if len(buffer) > book.maxBufferedEvents {
				return ErrDepthBufferOverflow
			}
			if pendingSnapshot != nil {
				synchronized, err := trySynchronize()
				if err != nil {
					return err
				}
				if synchronized {
					if err := publish(); err != nil {
						return err
					}
				}
			}
			if state == nil && pendingSnapshot == nil && !fetching && retry == nil {
				startSnapshot()
			}
		}
	}
}

func (book *LocalOrderBook) validateInput(input depthInput) error {
	if input.generation == 0 {
		return validationError("local order book WebSocket generation is zero")
	}
	event := input.event
	if event.EventType != "depthUpdate" || event.Symbol != book.symbol {
		return validationError("local order book received an unexpected depth event")
	}
	if event.FirstUpdateID <= 0 || event.FinalUpdateID < event.FirstUpdateID {
		return validationError("local order book depth update IDs are invalid")
	}
	for _, fields := range append(append([][]string(nil), event.Bids...), event.Asks...) {
		if _, _, _, err := parseDepthFields(fields); err != nil {
			return err
		}
	}
	return nil
}

func newLocalDepthState(snapshot OrderBook) (*localDepthState, error) {
	state := &localDepthState{
		bids:         make(map[string]localDepthLevel, len(snapshot.Bids)),
		asks:         make(map[string]localDepthLevel, len(snapshot.Asks)),
		lastUpdateID: snapshot.LastUpdateID,
	}
	for _, level := range snapshot.Bids {
		if err := applyLocalLevel(state.bids, []string{level.Price, level.Quantity}); err != nil {
			return nil, err
		}
	}
	for _, level := range snapshot.Asks {
		if err := applyLocalLevel(state.asks, []string{level.Price, level.Quantity}); err != nil {
			return nil, err
		}
	}
	return state, nil
}

func validateDepthSnapshot(snapshot OrderBook) error {
	if snapshot.LastUpdateID <= 0 || snapshot.LastUpdateID == math.MaxInt64 {
		return validationError("local order book snapshot update ID is invalid")
	}
	for _, level := range snapshot.Bids {
		if _, _, _, err := parseDepthFields([]string{level.Price, level.Quantity}); err != nil {
			return err
		}
	}
	for _, level := range snapshot.Asks {
		if _, _, _, err := parseDepthFields([]string{level.Price, level.Quantity}); err != nil {
			return err
		}
	}
	return nil
}

func (state *localDepthState) apply(event DepthEvent) (bool, bool, error) {
	if event.FinalUpdateID <= state.lastUpdateID {
		return false, false, nil
	}
	if event.FirstUpdateID > state.lastUpdateID+1 {
		return false, true, nil
	}
	for _, fields := range event.Bids {
		if err := applyLocalLevel(state.bids, fields); err != nil {
			return false, false, err
		}
	}
	for _, fields := range event.Asks {
		if err := applyLocalLevel(state.asks, fields); err != nil {
			return false, false, err
		}
	}
	state.lastUpdateID = event.FinalUpdateID
	state.eventTime = event.EventTime
	return true, false, nil
}

func applyLocalLevel(levels map[string]localDepthLevel, fields []string) error {
	key, price, quantity, err := parseDepthFields(fields)
	if err != nil {
		return err
	}
	if strings.Trim(quantity, "0.") == "" {
		delete(levels, key)
		return nil
	}
	levels[key] = localDepthLevel{
		priceText: fields[0], quantityText: quantity, price: price,
	}
	return nil
}

func parseDepthFields(fields []string) (string, *big.Rat, string, error) {
	if len(fields) != 2 || !positiveDecimalPattern.MatchString(fields[0]) ||
		strings.Trim(fields[0], "0.") == "" || !positiveDecimalPattern.MatchString(fields[1]) {
		return "", nil, "", validationError("local order book level is invalid")
	}
	price, ok := new(big.Rat).SetString(fields[0])
	if !ok || price.Sign() <= 0 {
		return "", nil, "", validationError("local order book price is invalid")
	}
	return price.RatString(), price, fields[1], nil
}

func (state *localDepthState) view(
	symbol string,
	generation uint64,
	synchronizationID uint64,
	gapCount uint64,
	depth int,
) LocalOrderBookView {
	return LocalOrderBookView{
		Symbol: symbol, Generation: generation, SynchronizationID: synchronizationID,
		GapCount: gapCount, LastUpdateID: state.lastUpdateID, EventTime: state.eventTime,
		Bids: sortedLocalLevels(state.bids, depth, true),
		Asks: sortedLocalLevels(state.asks, depth, false),
	}
}

func sortedLocalLevels(
	values map[string]localDepthLevel,
	depth int,
	descending bool,
) []BookLevel {
	levels := make([]localDepthLevel, 0, len(values))
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
		result[index] = BookLevel{Price: level.priceText, Quantity: level.quantityText}
	}
	return result
}
