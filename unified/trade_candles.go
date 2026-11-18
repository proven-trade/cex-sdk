package unified

import (
	"fmt"
	"math/big"
	"time"
)

// TradeCandle은 공개 체결로 합성한 OHLCV 한 구간이다.
// QuoteVolume은 체결별 가격×수량의 정확한 합이고 TradeCount는 반영한 체결 수다.
type TradeCandle struct {
	Candle
	QuoteVolume string
	TradeCount  int64
}

// TradeOrder는 합성기에 넣는 체결의 시간 방향이다.
type TradeOrder string

const (
	// TradeOrderAscending은 오래된 체결부터 들어오는 입력이다.
	TradeOrderAscending TradeOrder = "ascending"
	// TradeOrderDescending은 최신 체결부터 들어오는 입력이다.
	// 같은 시각의 체결도 실제 체결의 역순이어야 한다.
	TradeOrderDescending TradeOrder = "descending"
)

// TradeCandleBuilder는 시간 순서로 들어오는 공개 체결을 epoch 정렬 캔들로 합성한다.
// 체결 이력 API나 대용량 덤프처럼 전체를 메모리에 올릴 수 없는 입력을 페이지 경계와
// 관계없이 이어서 처리하도록 진행 중인 구간 하나만 보관한다. 체결이 없는 구간은 만들지 않는다.
// 내림차순 입력이면 확정 캔들도 최신 구간부터 반환한다.
type TradeCandleBuilder struct {
	intervalMillis int64
	descending     bool
	lastTimestamp  int64
	started        bool
	active         bool
	current        tradeCandleState
}

type tradeCandleState struct {
	startTime   int64
	open        string
	high        scaledDecimal
	highText    string
	low         scaledDecimal
	lowText     string
	close       string
	volume      scaledDecimal
	quoteVolume scaledDecimal
	tradeCount  int64
}

// NewTradeCandleBuilder는 양의 millisecond 구간과 입력 방향을 사용하는 합성기를 생성한다.
func NewTradeCandleBuilder(interval time.Duration, order TradeOrder) (*TradeCandleBuilder, error) {
	if interval <= 0 || interval%time.Millisecond != 0 {
		return nil, validationError("trade candle interval must be a positive millisecond duration")
	}
	if order != TradeOrderAscending && order != TradeOrderDescending {
		return nil, validationError("unsupported trade order %q", order)
	}
	return &TradeCandleBuilder{
		intervalMillis: interval.Milliseconds(), descending: order == TradeOrderDescending,
	}, nil
}

// Add는 체결 한 건을 반영한다. 체결이 새 구간에 속하면 직전 구간을 확정해 반환한다.
// 같은 시각의 체결은 입력 방향에 따라 시가·종가에 반영하며 시각이 역행하면 오류를 반환한다.
func (builder *TradeCandleBuilder) Add(trade PublicTrade) (TradeCandle, bool, error) {
	if trade.Timestamp < 0 {
		return TradeCandle{}, false, validationError("trade timestamp must not be negative")
	}
	if builder.started && !builder.descending && trade.Timestamp < builder.lastTimestamp {
		return TradeCandle{}, false, validationError(
			"trade timestamp %d is earlier than previous trade %d", trade.Timestamp, builder.lastTimestamp,
		)
	}
	if builder.started && builder.descending && trade.Timestamp > builder.lastTimestamp {
		return TradeCandle{}, false, validationError(
			"trade timestamp %d is later than previous trade %d", trade.Timestamp, builder.lastTimestamp,
		)
	}
	price, err := parseScaledDecimal(trade.Price)
	if err != nil {
		return TradeCandle{}, false, fmt.Errorf("decode trade price: %w", err)
	}
	quantity, err := parseScaledDecimal(trade.Quantity)
	if err != nil {
		return TradeCandle{}, false, fmt.Errorf("decode trade quantity: %w", err)
	}
	builder.started = true
	builder.lastTimestamp = trade.Timestamp

	bucketStart := trade.Timestamp - trade.Timestamp%builder.intervalMillis
	notional := multiplyScaledDecimals(price, quantity)
	if builder.active && builder.current.startTime == bucketStart {
		state := &builder.current
		if compareScaledDecimals(price, state.high) > 0 {
			state.high, state.highText = price, trade.Price
		}
		if compareScaledDecimals(price, state.low) < 0 {
			state.low, state.lowText = price, trade.Price
		}
		if builder.descending {
			state.open = trade.Price
		} else {
			state.close = trade.Price
		}
		state.volume = addScaledDecimals(state.volume, quantity)
		state.quoteVolume = addScaledDecimals(state.quoteVolume, notional)
		state.tradeCount++
		return TradeCandle{}, false, nil
	}

	completed, hasCompleted := builder.Flush()
	builder.active = true
	builder.current = tradeCandleState{
		startTime: bucketStart, open: trade.Price,
		high: price, highText: trade.Price, low: price, lowText: trade.Price,
		close: trade.Price, volume: quantity, quoteVolume: notional, tradeCount: 1,
	}
	return completed, hasCompleted, nil
}

// Flush는 진행 중인 구간을 확정해 반환하고 비운다. 입력이 끝났을 때 마지막 구간을 얻는 데 사용한다.
// 마지막 구간은 이후 체결이 더 있을 수 있으므로 원천 데이터의 끝이 확실할 때만 완성봉으로 취급해야 한다.
func (builder *TradeCandleBuilder) Flush() (TradeCandle, bool) {
	if !builder.active {
		return TradeCandle{}, false
	}
	state := builder.current
	builder.active = false
	builder.current = tradeCandleState{}
	return TradeCandle{
		Candle: Candle{
			StartTime: state.startTime, Open: state.open, High: state.highText,
			Low: state.lowText, Close: state.close, Volume: state.volume.String(),
		},
		QuoteVolume: state.quoteVolume.String(),
		TradeCount:  state.tradeCount,
	}, true
}

func multiplyScaledDecimals(left, right scaledDecimal) scaledDecimal {
	return scaledDecimal{
		integer: new(big.Int).Mul(left.integer, right.integer),
		scale:   left.scale + right.scale,
	}
}

func compareScaledDecimals(left, right scaledDecimal) int {
	scale := left.scale
	if right.scale > scale {
		scale = right.scale
	}
	return scaleInteger(left, scale).Cmp(scaleInteger(right, scale))
}
