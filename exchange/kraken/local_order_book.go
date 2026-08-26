package kraken

import (
	"context"
	"fmt"
	"hash/crc32"
	"math/big"
	"sort"
	"strings"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/transport"
)

const defaultLocalOrderBookViewDepth = 20

// SpotLocalOrderBookConfig는 Kraken Spot WebSocket v2 book 로컬 오더북 설정이다.
type SpotLocalOrderBookConfig struct {
	Symbol        string
	Depth         int
	ViewDepth     int
	EgressRouteID transport.EgressRouteID
}

// SpotLocalOrderBookView는 CRC32 checksum이 검증된 시점의 정렬된 상위 호가다.
type SpotLocalOrderBookView struct {
	Symbol            string
	Depth             int
	ViewDepth         int
	Generation        uint64
	SynchronizationID uint64
	GapCount          uint64
	Checksum          uint32
	Timestamp         string
	Bids              []SpotStreamBookLevel
	Asks              []SpotStreamBookLevel
}

// SpotLocalOrderBookHandler는 snapshot 교체와 이후 각 유효 update의 장부를 처리한다.
type SpotLocalOrderBookHandler func(context.Context, SpotLocalOrderBookView) error

// SpotLocalOrderBook은 book snapshot과 update를 결합하고 checksum 이상 시 같은 송신 경로로 재연결한다.
type SpotLocalOrderBook struct {
	symbol    string
	depth     int
	viewDepth int
	routeID   transport.EgressRouteID
}

type spotLocalOrderBookLevel struct {
	priceText    string
	quantityText string
	price        *big.Rat
}

type spotLocalOrderBookState struct {
	bids      map[string]spotLocalOrderBookLevel
	asks      map[string]spotLocalOrderBookLevel
	checksum  uint32
	timestamp string
}

type spotLocalOrderBookProcessor struct {
	book              *SpotLocalOrderBook
	generation        uint64
	synchronizationID uint64
	gapCount          uint64
	state             *spotLocalOrderBookState
}

// NewSpotLocalOrderBook은 특정 symbol·depth의 book channel 전용 로컬 오더북을 생성한다.
func NewSpotLocalOrderBook(config SpotLocalOrderBookConfig) (*SpotLocalOrderBook, error) {
	subscription, err := validateSpotPublicSubscription(SpotPublicSubscription{
		Channel: SpotChannelBook, Symbols: []string{config.Symbol}, Depth: config.Depth,
	})
	if err != nil {
		return nil, err
	}
	config.Depth = effectiveSpotBookDepth(subscription.Depth)
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
		return nil, validationError("Spot local order book view depth must not exceed subscribed depth")
	}
	return &SpotLocalOrderBook{
		symbol: subscription.Symbols[0], depth: config.Depth,
		viewDepth: config.ViewDepth, routeID: config.EgressRouteID,
	}, nil
}

// Run은 public stream을 소유하고 checksum이 검증된 snapshot·update 장부를 handler에 전달한다.
func (book *SpotLocalOrderBook) Run(
	ctx context.Context,
	public *SpotPublicStream,
	handler SpotLocalOrderBookHandler,
) error {
	if ctx == nil {
		return fmt.Errorf("Kraken Spot local order book context is nil")
	}
	if public == nil {
		return fmt.Errorf("Kraken Spot local order book public stream is required")
	}
	if handler == nil {
		return fmt.Errorf("Kraken Spot local order book handler is required")
	}
	if public.EgressRouteID() != book.routeID {
		return validationError("Spot local order book and public WebSocket routes must match")
	}
	if !public.hasBookSubscription(book.symbol, book.depth) {
		return validationError("public stream does not contain the required Spot book subscription")
	}
	processor := &spotLocalOrderBookProcessor{book: book}
	return public.Run(ctx, func(handlerContext context.Context, message SpotStreamMessage) error {
		if message.Channel != string(SpotChannelBook) {
			return nil
		}
		var events []SpotStreamBook
		if err := message.DecodeData(&events); err != nil {
			return err
		}
		if len(events) == 0 {
			return validationError("Spot local order book message data is empty")
		}
		for _, event := range events {
			if event.Symbol != book.symbol {
				continue
			}
			view, reconnect, err := processor.process(
				public.Generation(), message.Type, event,
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

func (processor *spotLocalOrderBookProcessor) process(
	generation uint64,
	messageType string,
	event SpotStreamBook,
) (*SpotLocalOrderBookView, bool, error) {
	if generation == 0 {
		return nil, false, validationError("Spot local order book WebSocket generation is zero")
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
		state := &spotLocalOrderBookState{
			bids: make(map[string]spotLocalOrderBookLevel, len(event.Bids)),
			asks: make(map[string]spotLocalOrderBookLevel, len(event.Asks)),
		}
		if err := state.apply(event, processor.book.depth); err != nil {
			return nil, false, err
		}
		if state.checksum != event.Checksum {
			return processor.checksumGap()
		}
		processor.state = state
		processor.synchronizationID++
		view := state.view(processor)
		return &view, false, nil
	case "update":
		if processor.state == nil {
			return processor.checksumGap()
		}
		if err := processor.state.apply(event, processor.book.depth); err != nil {
			return nil, false, err
		}
		if processor.state.checksum != event.Checksum {
			return processor.checksumGap()
		}
		view := processor.state.view(processor)
		return &view, false, nil
	default:
		return nil, false, validationError("Spot local order book message type is invalid")
	}
}

func (processor *spotLocalOrderBookProcessor) checksumGap() (*SpotLocalOrderBookView, bool, error) {
	processor.gapCount++
	processor.state = nil
	return nil, true, nil
}

func (book *SpotLocalOrderBook) validateEvent(messageType string, event SpotStreamBook) error {
	if messageType != "snapshot" && messageType != "update" {
		return validationError("Spot local order book message type is invalid")
	}
	if event.Symbol != book.symbol {
		return validationError("Spot local order book symbol is invalid")
	}
	if messageType == "snapshot" &&
		(len(event.Bids) > book.depth || len(event.Asks) > book.depth) {
		return validationError("Spot local order book snapshot exceeds subscribed depth")
	}
	if _, err := time.Parse(time.RFC3339Nano, event.Timestamp); err != nil {
		return validationError("Spot local order book timestamp is invalid")
	}
	for _, level := range append(append([]SpotStreamBookLevel(nil), event.Bids...), event.Asks...) {
		if _, _, err := parseSpotLocalOrderBookLevel(level); err != nil {
			return err
		}
	}
	return nil
}

func (state *spotLocalOrderBookState) apply(event SpotStreamBook, depth int) error {
	for _, level := range event.Bids {
		if err := applySpotLocalOrderBookLevel(state.bids, level); err != nil {
			return err
		}
	}
	for _, level := range event.Asks {
		if err := applySpotLocalOrderBookLevel(state.asks, level); err != nil {
			return err
		}
	}
	pruneSpotLocalOrderBookLevels(state.bids, depth, true)
	pruneSpotLocalOrderBookLevels(state.asks, depth, false)
	state.checksum = spotLocalOrderBookChecksum(state.asks, state.bids)
	state.timestamp = event.Timestamp
	return nil
}

func applySpotLocalOrderBookLevel(
	levels map[string]spotLocalOrderBookLevel,
	value SpotStreamBookLevel,
) error {
	key, level, err := parseSpotLocalOrderBookLevel(value)
	if err != nil {
		return err
	}
	if decimalIsZero(level.quantityText) {
		delete(levels, key)
		return nil
	}
	levels[key] = level
	return nil
}

func parseSpotLocalOrderBookLevel(
	value SpotStreamBookLevel,
) (string, spotLocalOrderBookLevel, error) {
	priceText := string(value.Price)
	quantityText := string(value.Quantity)
	if !positiveDecimalPattern.MatchString(priceText) || decimalIsZero(priceText) ||
		!positiveDecimalPattern.MatchString(quantityText) {
		return "", spotLocalOrderBookLevel{}, validationError("Spot local order book level is invalid")
	}
	price, ok := new(big.Rat).SetString(priceText)
	if !ok || price.Sign() <= 0 {
		return "", spotLocalOrderBookLevel{}, validationError("Spot local order book price is invalid")
	}
	return price.RatString(), spotLocalOrderBookLevel{
		priceText: priceText, quantityText: quantityText, price: price,
	}, nil
}

func decimalIsZero(value string) bool {
	return strings.Trim(value, "0.") == ""
}

func pruneSpotLocalOrderBookLevels(
	values map[string]spotLocalOrderBookLevel,
	depth int,
	descending bool,
) {
	levels := sortedSpotLocalOrderBookValues(values, descending)
	for index := depth; index < len(levels); index++ {
		delete(values, levels[index].price.RatString())
	}
}

func sortedSpotLocalOrderBookValues(
	values map[string]spotLocalOrderBookLevel,
	descending bool,
) []spotLocalOrderBookLevel {
	levels := make([]spotLocalOrderBookLevel, 0, len(values))
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
	return levels
}

func sortedSpotLocalOrderBookLevels(
	values map[string]spotLocalOrderBookLevel,
	depth int,
	descending bool,
) []SpotStreamBookLevel {
	levels := sortedSpotLocalOrderBookValues(values, descending)
	if len(levels) > depth {
		levels = levels[:depth]
	}
	result := make([]SpotStreamBookLevel, len(levels))
	for index, level := range levels {
		result[index] = SpotStreamBookLevel{
			Price:    SpotStreamDecimal(level.priceText),
			Quantity: SpotStreamDecimal(level.quantityText),
		}
	}
	return result
}

func spotLocalOrderBookChecksum(
	asks map[string]spotLocalOrderBookLevel,
	bids map[string]spotLocalOrderBookLevel,
) uint32 {
	var payload strings.Builder
	appendSpotLocalOrderBookChecksumLevels(&payload, asks, false)
	appendSpotLocalOrderBookChecksumLevels(&payload, bids, true)
	return crc32.ChecksumIEEE([]byte(payload.String()))
}

func appendSpotLocalOrderBookChecksumLevels(
	payload *strings.Builder,
	values map[string]spotLocalOrderBookLevel,
	descending bool,
) {
	levels := sortedSpotLocalOrderBookValues(values, descending)
	if len(levels) > 10 {
		levels = levels[:10]
	}
	for _, level := range levels {
		payload.WriteString(spotLocalOrderBookChecksumDecimal(level.priceText))
		payload.WriteString(spotLocalOrderBookChecksumDecimal(level.quantityText))
	}
}

func spotLocalOrderBookChecksumDecimal(value string) string {
	return strings.TrimLeft(strings.ReplaceAll(value, ".", ""), "0")
}

func (state *spotLocalOrderBookState) view(
	processor *spotLocalOrderBookProcessor,
) SpotLocalOrderBookView {
	book := processor.book
	return SpotLocalOrderBookView{
		Symbol: book.symbol, Depth: book.depth, ViewDepth: book.viewDepth,
		Generation: processor.generation, SynchronizationID: processor.synchronizationID,
		GapCount: processor.gapCount, Checksum: state.checksum, Timestamp: state.timestamp,
		Bids: sortedSpotLocalOrderBookLevels(state.bids, book.viewDepth, true),
		Asks: sortedSpotLocalOrderBookLevels(state.asks, book.viewDepth, false),
	}
}
