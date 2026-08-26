package mexc

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/transport"
)

const (
	defaultLocalSnapshotLimit = 5000
	defaultLocalBufferSize    = 4096
	defaultLocalViewDepth     = 20
	defaultLocalSnapshotWait  = 10 * time.Second
	defaultLocalSnapshotRetry = 250 * time.Millisecond
)

var (
	// ErrDepthBufferOverflow는 snapshot 동기화 중 보존 가능한 증분 이벤트 수를 초과했음을 나타낸다.
	ErrDepthBufferOverflow = errors.New("MEXC depth event buffer overflow")
)

// LocalOrderBookConfig는 MEXC Spot diff depth와 REST snapshot 동기화 설정이다.
type LocalOrderBookConfig struct {
	RESTClient            *Client
	Symbol                string
	EgressRouteID         transport.EgressRouteID
	UpdateInterval        StreamUpdateInterval
	SnapshotLimit         int
	MaxBufferedEvents     int
	ViewDepth             int
	SnapshotTimeout       time.Duration
	SnapshotRetryInterval time.Duration
}

// LocalOrderBookView는 version 연속성을 검증한 시점의 정렬된 상위 호가다.
type LocalOrderBookView struct {
	Symbol              string
	Generation          uint64
	SynchronizationID   uint64
	GapCount            uint64
	LastVersion         string
	CreateTime          int64
	SendTime            int64
	LastOrderCreateTime int64
	Bids                []BookLevel
	Asks                []BookLevel
}

// LocalOrderBookHandler는 최초 동기화와 이후 각 유효 증분이 반영된 장부를 처리한다.
type LocalOrderBookHandler func(context.Context, LocalOrderBookView) error

type localSnapshotLoader func(context.Context) (OrderBook, error)

// LocalOrderBook은 Protobuf diff depth를 버퍼링하고 같은 송신 경로의 REST snapshot과 결합한다.
type LocalOrderBook struct {
	symbol                string
	routeID               transport.EgressRouteID
	channel               string
	maxBufferedEvents     int
	viewDepth             int
	snapshotTimeout       time.Duration
	snapshotRetryInterval time.Duration
	loadSnapshot          localSnapshotLoader
}

type localDepthInput struct {
	generation uint64
	channel    string
	symbol     string
	createTime int64
	sendTime   int64
	event      StreamDiffDepth
}

type localSnapshotResult struct {
	snapshot OrderBook
	err      error
}

type localBookLevel struct {
	priceText    string
	quantityText string
	price        *big.Rat
}

type localBookState struct {
	bids                map[string]localBookLevel
	asks                map[string]localBookLevel
	lastVersion         uint64
	createTime          int64
	sendTime            int64
	lastOrderCreateTime int64
}

// NewLocalOrderBook은 route 고정 snapshot loader와 diff depth 동기화기를 생성한다.
func NewLocalOrderBook(config LocalOrderBookConfig) (*LocalOrderBook, error) {
	if config.RESTClient == nil {
		return nil, fmt.Errorf("MEXC local order book REST client is required")
	}
	if err := validateSymbol(config.Symbol); err != nil {
		return nil, err
	}
	config.EgressRouteID = transport.EgressRouteID(strings.TrimSpace(string(config.EgressRouteID)))
	if config.EgressRouteID == "" {
		return nil, trade.ErrMissingEgressRoute
	}
	if config.UpdateInterval == "" {
		config.UpdateInterval = StreamUpdate100Millis
	}
	if !streamUpdateIntervalValid(config.UpdateInterval) {
		return nil, validationError(
			"unsupported local order book update interval %q", config.UpdateInterval,
		)
	}
	if config.SnapshotLimit == 0 {
		config.SnapshotLimit = defaultLocalSnapshotLimit
	}
	if config.SnapshotLimit < 1 || config.SnapshotLimit > 5000 {
		return nil, validationError("local order book snapshot limit must be between 1 and 5000")
	}
	if config.MaxBufferedEvents == 0 {
		config.MaxBufferedEvents = defaultLocalBufferSize
	}
	if config.MaxBufferedEvents < 1 {
		return nil, validationError("local order book event buffer size must be positive")
	}
	if config.ViewDepth == 0 {
		config.ViewDepth = defaultLocalViewDepth
		if config.ViewDepth > config.SnapshotLimit {
			config.ViewDepth = config.SnapshotLimit
		}
	}
	if config.ViewDepth < 1 || config.ViewDepth > config.SnapshotLimit {
		return nil, validationError("local order book view depth must not exceed snapshot limit")
	}
	if config.SnapshotTimeout == 0 {
		config.SnapshotTimeout = defaultLocalSnapshotWait
	}
	if config.SnapshotRetryInterval == 0 {
		config.SnapshotRetryInterval = defaultLocalSnapshotRetry
	}
	if config.SnapshotTimeout < 0 || config.SnapshotRetryInterval < 0 {
		return nil, validationError("local order book snapshot durations must be positive")
	}
	subscription := StreamSubscription{
		Channel: StreamChannelDiffDepth, Symbol: config.Symbol,
		UpdateInterval: config.UpdateInterval,
	}
	book := &LocalOrderBook{
		symbol: config.Symbol, routeID: config.EgressRouteID,
		channel:           streamSubscriptionName(subscription),
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

// Run은 diff depth stream을 소유하고 snapshot·증분 동기화 장부를 처리기에 전달한다.
func (book *LocalOrderBook) Run(
	ctx context.Context,
	public *PublicStream,
	handler LocalOrderBookHandler,
) error {
	if ctx == nil {
		return fmt.Errorf("MEXC local order book context is nil")
	}
	if public == nil || public.managed == nil || public.managed.session == nil {
		return fmt.Errorf("MEXC local order book public stream is required")
	}
	if handler == nil {
		return fmt.Errorf("MEXC local order book handler is required")
	}
	if public.EgressRouteID() != book.routeID {
		return validationError("local order book REST and WebSocket routes must match")
	}
	if !public.managed.hasSubscription(book.channel) {
		return validationError("public stream does not contain the exact diff depth subscription")
	}
	runContext, cancel := context.WithCancel(ctx)
	inputs := make(chan localDepthInput, book.maxBufferedEvents)
	streamDone := make(chan error, 1)
	streamFinished := make(chan struct{})
	go func() {
		defer close(streamFinished)
		streamDone <- public.Run(runContext, func(
			handlerContext context.Context,
			message StreamMessage,
		) error {
			if message.Control != nil {
				if message.Control.Code != 0 {
					return fmt.Errorf(
						"MEXC depth subscription failed with code %d: %s",
						message.Control.Code, message.Control.Message,
					)
				}
				return nil
			}
			if message.DiffDepth == nil || message.Channel != book.channel {
				return nil
			}
			input := localDepthInput{
				generation: public.Generation(), channel: message.Channel,
				symbol: message.Symbol, createTime: message.CreateTime,
				sendTime: message.SendTime, event: *message.DiffDepth,
			}
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
	err := book.runInputs(runContext, inputs, streamDone, handler)
	cancel()
	<-streamFinished
	return err
}

func (managed *managedStream) hasSubscription(name string) bool {
	managed.stateMu.Lock()
	defer managed.stateMu.Unlock()
	_, exists := managed.subscriptions[name]
	return exists
}

func (book *LocalOrderBook) runInputs(
	ctx context.Context,
	inputs <-chan localDepthInput,
	streamDone <-chan error,
	handler LocalOrderBookHandler,
) error {
	var (
		buffer            []localDepthInput
		currentGeneration uint64
		state             *localBookState
		pendingSnapshot   *OrderBook
		synchronizationID uint64
		gapCount          uint64
		fetching          bool
		snapshotResults   = make(chan localSnapshotResult, 1)
		retryTimer        *time.Timer
		retry             <-chan time.Time
	)
	stopRetry := func() {
		if retryTimer == nil {
			return
		}
		if !retryTimer.Stop() {
			select {
			case <-retryTimer.C:
			default:
			}
		}
		retryTimer = nil
		retry = nil
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
			case snapshotResults <- localSnapshotResult{snapshot: snapshot, err: err}:
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
		return handler(ctx, state.view(
			book.symbol, currentGeneration, synchronizationID, gapCount, book.viewDepth,
		))
	}
	trySynchronize := func() (bool, error) {
		if pendingSnapshot == nil || len(buffer) == 0 {
			return false, nil
		}
		snapshot := *pendingSnapshot
		snapshotVersion := uint64(snapshot.LastUpdateID)
		firstRelevant := 0
		for firstRelevant < len(buffer) {
			toVersion, err := parseLocalVersion(
				buffer[firstRelevant].event.ToVersion, "to version",
			)
			if err != nil {
				return false, err
			}
			if toVersion >= snapshotVersion {
				break
			}
			firstRelevant++
		}
		buffer = buffer[firstRelevant:]
		if len(buffer) == 0 {
			return false, nil
		}
		firstFrom, err := parseLocalVersion(buffer[0].event.FromVersion, "from version")
		if err != nil {
			return false, err
		}
		if firstFrom > snapshotVersion {
			pendingSnapshot = nil
			scheduleRetry()
			return false, nil
		}
		candidate, err := newLocalBookState(snapshot)
		if err != nil {
			return false, err
		}
		for index, input := range buffer {
			var gap bool
			if index == 0 {
				gap, err = candidate.applyBridge(input)
			} else {
				gap, err = candidate.apply(input)
			}
			if err != nil {
				return false, err
			}
			if gap {
				gapCount++
				buffer = append([]localDepthInput(nil), buffer[index:]...)
				pendingSnapshot = nil
				scheduleRetry()
				return false, nil
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
			if err := validateLocalSnapshot(result.snapshot); err != nil {
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
				gap, err := state.apply(input)
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
				if err := publish(); err != nil {
					return err
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

func (book *LocalOrderBook) validateInput(input localDepthInput) error {
	if input.generation == 0 {
		return validationError("local order book WebSocket generation is zero")
	}
	if input.channel != book.channel || input.symbol != book.symbol {
		return validationError("local order book received an unexpected depth event")
	}
	if input.sendTime <= 0 {
		return validationError("local order book depth event time is invalid")
	}
	fromVersion, err := parseLocalVersion(input.event.FromVersion, "from version")
	if err != nil {
		return err
	}
	toVersion, err := parseLocalVersion(input.event.ToVersion, "to version")
	if err != nil {
		return err
	}
	if toVersion < fromVersion {
		return validationError("local order book depth version range is invalid")
	}
	if err := validateLocalLevels(input.event.Bids, true); err != nil {
		return err
	}
	return validateLocalLevels(input.event.Asks, true)
}

func newLocalBookState(snapshot OrderBook) (*localBookState, error) {
	state := &localBookState{
		bids:        make(map[string]localBookLevel, len(snapshot.Bids)),
		asks:        make(map[string]localBookLevel, len(snapshot.Asks)),
		lastVersion: uint64(snapshot.LastUpdateID),
	}
	for _, level := range snapshot.Bids {
		if err := applyLocalBookLevel(state.bids, level); err != nil {
			return nil, err
		}
	}
	for _, level := range snapshot.Asks {
		if err := applyLocalBookLevel(state.asks, level); err != nil {
			return nil, err
		}
	}
	return state, nil
}

func validateLocalSnapshot(snapshot OrderBook) error {
	if snapshot.LastUpdateID <= 0 || snapshot.LastUpdateID == math.MaxInt64 {
		return validationError("local order book snapshot update ID is invalid")
	}
	if err := validateLocalLevels(snapshot.Bids, false); err != nil {
		return err
	}
	return validateLocalLevels(snapshot.Asks, false)
}

func (state *localBookState) applyBridge(input localDepthInput) (bool, error) {
	fromVersion, err := parseLocalVersion(input.event.FromVersion, "from version")
	if err != nil {
		return false, err
	}
	toVersion, err := parseLocalVersion(input.event.ToVersion, "to version")
	if err != nil {
		return false, err
	}
	if fromVersion > state.lastVersion || toVersion < state.lastVersion {
		return true, nil
	}
	return false, state.applyLevels(input, toVersion)
}

func (state *localBookState) apply(input localDepthInput) (bool, error) {
	fromVersion, err := parseLocalVersion(input.event.FromVersion, "from version")
	if err != nil {
		return false, err
	}
	toVersion, err := parseLocalVersion(input.event.ToVersion, "to version")
	if err != nil {
		return false, err
	}
	if state.lastVersion == math.MaxUint64 || fromVersion != state.lastVersion+1 {
		return true, nil
	}
	return false, state.applyLevels(input, toVersion)
}

func (state *localBookState) applyLevels(input localDepthInput, toVersion uint64) error {
	for _, level := range input.event.Bids {
		if err := applyLocalBookLevel(state.bids, level); err != nil {
			return err
		}
	}
	for _, level := range input.event.Asks {
		if err := applyLocalBookLevel(state.asks, level); err != nil {
			return err
		}
	}
	state.lastVersion = toVersion
	state.createTime = input.createTime
	state.sendTime = input.sendTime
	state.lastOrderCreateTime = input.event.LastOrderCreateTime
	return nil
}

func applyLocalBookLevel(levels map[string]localBookLevel, level BookLevel) error {
	key, price, quantity, err := parseLocalBookLevel(level)
	if err != nil {
		return err
	}
	if decimalIsZero(quantity) {
		delete(levels, key)
		return nil
	}
	levels[key] = localBookLevel{
		priceText: level.Price, quantityText: quantity, price: price,
	}
	return nil
}

func validateLocalLevels(levels []BookLevel, allowZero bool) error {
	seen := make(map[string]struct{}, len(levels))
	for _, level := range levels {
		key, _, quantity, err := parseLocalBookLevel(level)
		if err != nil {
			return err
		}
		if !allowZero && decimalIsZero(quantity) {
			return validationError("local order book snapshot quantity is zero")
		}
		if _, exists := seen[key]; exists {
			return validationError("local order book contains a duplicate canonical price")
		}
		seen[key] = struct{}{}
	}
	return nil
}

func parseLocalBookLevel(level BookLevel) (string, *big.Rat, string, error) {
	if !decimalPattern.MatchString(level.Price) || decimalIsZero(level.Price) ||
		!decimalPattern.MatchString(level.Quantity) {
		return "", nil, "", validationError("local order book level is invalid")
	}
	price, ok := new(big.Rat).SetString(level.Price)
	if !ok || price.Sign() <= 0 {
		return "", nil, "", validationError("local order book price is invalid")
	}
	return price.RatString(), price, level.Quantity, nil
}

func parseLocalVersion(value, name string) (uint64, error) {
	if value == "" || strings.Trim(value, "0") == "" {
		return 0, validationError("local order book %s is invalid", name)
	}
	for index := range len(value) {
		if value[index] < '0' || value[index] > '9' {
			return 0, validationError("local order book %s is invalid", name)
		}
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil || parsed == 0 {
		return 0, validationError("local order book %s is invalid", name)
	}
	return parsed, nil
}

func decimalIsZero(value string) bool {
	return strings.Trim(value, "0.") == ""
}

func (state *localBookState) view(
	symbol string,
	generation uint64,
	synchronizationID uint64,
	gapCount uint64,
	depth int,
) LocalOrderBookView {
	return LocalOrderBookView{
		Symbol: symbol, Generation: generation, SynchronizationID: synchronizationID,
		GapCount: gapCount, LastVersion: strconv.FormatUint(state.lastVersion, 10),
		CreateTime: state.createTime, SendTime: state.sendTime,
		LastOrderCreateTime: state.lastOrderCreateTime,
		Bids:                sortedLocalBookLevels(state.bids, depth, true),
		Asks:                sortedLocalBookLevels(state.asks, depth, false),
	}
}

func sortedLocalBookLevels(
	values map[string]localBookLevel,
	depth int,
	descending bool,
) []BookLevel {
	levels := make([]localBookLevel, 0, len(values))
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
