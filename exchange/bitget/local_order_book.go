package bitget

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

const (
	defaultLocalOrderBookViewDepth = 20
	maximumLocalOrderBookViewDepth = 1000
)

// LocalOrderBookConfig는 Bitget Spot books snapshot과 update 동기화 설정이다.
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
	GapCount          uint64
	Sequence          int64
	Timestamp         int64
	MaxDepth          string
	Bids              []BookLevel
	Asks              []BookLevel
}

// LocalOrderBookHandler는 snapshot과 이후 각 유효 update의 장부를 처리한다.
type LocalOrderBookHandler func(context.Context, LocalOrderBookView) error

// LocalOrderBook은 Bitget books snapshot과 pseq·seq update를 결합한다.
type LocalOrderBook struct {
	symbol    string
	routeID   transport.EgressRouteID
	viewDepth int
}

type localOrderBookLevel struct {
	priceText    string
	quantityText string
	price        *big.Rat
}

type localOrderBookState struct {
	bids      map[string]localOrderBookLevel
	asks      map[string]localOrderBookLevel
	sequence  int64
	timestamp int64
	maxDepth  string
	bridged   bool
}

// NewLocalOrderBook은 route 고정 full depth stream용 로컬 오더북을 생성한다.
func NewLocalOrderBook(config LocalOrderBookConfig) (*LocalOrderBook, error) {
	if err := validateSymbol(config.Symbol); err != nil {
		return nil, err
	}
	config.Symbol = strings.ToUpper(config.Symbol)
	config.EgressRouteID = transport.EgressRouteID(strings.TrimSpace(string(config.EgressRouteID)))
	if config.EgressRouteID == "" {
		return nil, trade.ErrMissingEgressRoute
	}
	if config.ViewDepth == 0 {
		config.ViewDepth = defaultLocalOrderBookViewDepth
	}
	if config.ViewDepth < 1 || config.ViewDepth > maximumLocalOrderBookViewDepth {
		return nil, validationError("local order book view depth must be between 1 and 1000")
	}
	return &LocalOrderBook{
		symbol: config.Symbol, routeID: config.EgressRouteID, viewDepth: config.ViewDepth,
	}, nil
}

// Run은 books stream을 소유하고 snapshot·update 장부를 handler에 전달한다.
func (book *LocalOrderBook) Run(
	ctx context.Context,
	public *PublicStream,
	handler LocalOrderBookHandler,
) error {
	if ctx == nil {
		return fmt.Errorf("Bitget local order book context is nil")
	}
	if public == nil {
		return fmt.Errorf("Bitget local order book public stream is required")
	}
	if handler == nil {
		return fmt.Errorf("Bitget local order book handler is required")
	}
	if public.EgressRouteID() != book.routeID {
		return validationError("local order book and WebSocket routes must match")
	}
	if !public.hasSpotBooksArgument(book.symbol) {
		return validationError("public stream does not contain the required Spot books subscription")
	}

	var (
		state              *localOrderBookState
		currentGeneration  uint64
		recoveryGeneration uint64
		synchronizationID  uint64
		gapCount           uint64
	)
	publish := func() error {
		return handler(ctx, state.view(
			book.symbol, currentGeneration, synchronizationID, gapCount, book.viewDepth,
		))
	}
	recoverFromGap := func() error {
		gapCount++
		state = nil
		recoveryGeneration = currentGeneration
		return public.managed.session.Reconnect()
	}

	return public.Run(ctx, func(_ context.Context, message StreamMessage) error {
		if message.Pong {
			return nil
		}
		if message.Event != "" {
			if message.Event == "error" || (message.Code != "" && message.Code != "0") {
				return fmt.Errorf(
					"Bitget books subscription failed with code %s: %s",
					message.Code,
					message.Message,
				)
			}
			return nil
		}
		if message.Argument.InstrumentType != "spot" || message.Argument.Topic != "books" ||
			!strings.EqualFold(message.Argument.Symbol, book.symbol) {
			return nil
		}
		generation := public.Generation()
		if generation == 0 {
			return validationError("local order book WebSocket generation is zero")
		}
		if generation < currentGeneration {
			return nil
		}
		if generation > currentGeneration {
			currentGeneration = generation
			state = nil
			recoveryGeneration = 0
		}
		if recoveryGeneration == generation {
			return nil
		}

		var updates []StreamOrderBook
		if err := message.DecodeData(&updates); err != nil {
			return err
		}
		if len(updates) != 1 {
			return validationError("local order book message must contain exactly one depth item")
		}
		update := updates[0]
		timestamp, err := validateLocalOrderBookUpdate(message.Action, update, message.Timestamp)
		if err != nil {
			return err
		}

		switch message.Action {
		case "snapshot":
			candidate, err := newLocalOrderBookState(update, timestamp)
			if err != nil {
				return err
			}
			state = candidate
			synchronizationID++
			return publish()
		case "update":
			if update.Sequence <= update.PreviousSequence {
				return recoverFromGap()
			}
			if state == nil {
				return recoverFromGap()
			}
			if !state.bridged {
				if update.PreviousSequence > state.sequence || update.Sequence < state.sequence {
					return recoverFromGap()
				}
				if err := state.apply(update, timestamp); err != nil {
					return err
				}
				state.bridged = true
				return publish()
			}
			if update.PreviousSequence == 0 || update.Sequence < state.sequence {
				return recoverFromGap()
			}
			if update.Sequence == state.sequence {
				return nil
			}
			if update.PreviousSequence != state.sequence {
				return recoverFromGap()
			}
			if err := state.apply(update, timestamp); err != nil {
				return err
			}
			return publish()
		default:
			return validationError("local order book action must be snapshot or update")
		}
	})
}

func validateLocalOrderBookUpdate(action string, update StreamOrderBook, envelopeTimestamp int64) (int64, error) {
	if action != "snapshot" && action != "update" {
		return 0, validationError("local order book action must be snapshot or update")
	}
	if update.Sequence <= 0 || update.PreviousSequence < 0 {
		return 0, validationError("local order book sequence is invalid")
	}
	for _, fields := range append(append([]BookLevel(nil), update.Bids...), update.Asks...) {
		if _, _, _, err := parseLocalOrderBookLevel(fields); err != nil {
			return 0, err
		}
	}
	if update.MaxDepth != "" {
		maxDepth, err := strconv.Atoi(update.MaxDepth)
		if err != nil || maxDepth < 0 || maxDepth > maximumLocalOrderBookViewDepth {
			return 0, validationError("local order book maximum depth is invalid")
		}
	}
	timestamp := envelopeTimestamp
	if update.Timestamp != "" {
		parsed, err := strconv.ParseInt(update.Timestamp, 10, 64)
		if err != nil || parsed <= 0 {
			return 0, validationError("local order book timestamp is invalid")
		}
		timestamp = parsed
	}
	if timestamp <= 0 {
		return 0, validationError("local order book timestamp is invalid")
	}
	return timestamp, nil
}

func newLocalOrderBookState(update StreamOrderBook, timestamp int64) (*localOrderBookState, error) {
	state := &localOrderBookState{
		bids: make(map[string]localOrderBookLevel, len(update.Bids)),
		asks: make(map[string]localOrderBookLevel, len(update.Asks)),
	}
	if err := state.apply(update, timestamp); err != nil {
		return nil, err
	}
	return state, nil
}

func (state *localOrderBookState) apply(update StreamOrderBook, timestamp int64) error {
	for _, level := range update.Bids {
		if err := applyLocalOrderBookLevel(state.bids, level); err != nil {
			return err
		}
	}
	for _, level := range update.Asks {
		if err := applyLocalOrderBookLevel(state.asks, level); err != nil {
			return err
		}
	}
	state.sequence = update.Sequence
	state.timestamp = timestamp
	if update.MaxDepth != "" {
		state.maxDepth = update.MaxDepth
	}
	return nil
}

func applyLocalOrderBookLevel(levels map[string]localOrderBookLevel, level BookLevel) error {
	key, price, quantity, err := parseLocalOrderBookLevel(level)
	if err != nil {
		return err
	}
	if strings.Trim(quantity, "0.") == "" {
		delete(levels, key)
		return nil
	}
	levels[key] = localOrderBookLevel{
		priceText: level.Price, quantityText: quantity, price: price,
	}
	return nil
}

func parseLocalOrderBookLevel(level BookLevel) (string, *big.Rat, string, error) {
	if !positiveDecimalPattern.MatchString(level.Price) || strings.Trim(level.Price, "0.") == "" ||
		!positiveDecimalPattern.MatchString(level.Quantity) {
		return "", nil, "", validationError("local order book level is invalid")
	}
	price, ok := new(big.Rat).SetString(level.Price)
	if !ok || price.Sign() <= 0 {
		return "", nil, "", validationError("local order book price is invalid")
	}
	return price.RatString(), price, level.Quantity, nil
}

func (state *localOrderBookState) view(
	symbol string,
	generation uint64,
	synchronizationID uint64,
	gapCount uint64,
	depth int,
) LocalOrderBookView {
	return LocalOrderBookView{
		Symbol: symbol, Generation: generation, SynchronizationID: synchronizationID,
		GapCount: gapCount, Sequence: state.sequence, Timestamp: state.timestamp,
		MaxDepth: state.maxDepth, Bids: sortedLocalOrderBookLevels(state.bids, depth, true),
		Asks: sortedLocalOrderBookLevels(state.asks, depth, false),
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
		result[index] = BookLevel{Price: level.priceText, Quantity: level.quantityText}
	}
	return result
}
