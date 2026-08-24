package unified

import (
	"fmt"
	"math/big"
	"sort"
	"strings"
	"time"
)

// AddDecimals는 하나 이상의 음이 아닌 decimal 문자열을 정밀도 손실 없이 더한다.
func AddDecimals(values ...string) (string, error) {
	if len(values) == 0 {
		return "", validationError("at least one decimal value is required")
	}
	total := scaledDecimal{integer: big.NewInt(0)}
	for _, value := range values {
		parsed, err := parseScaledDecimal(value)
		if err != nil {
			return "", fmt.Errorf("decode decimal sum value: %w", err)
		}
		total = addScaledDecimals(total, parsed)
	}
	return total.String(), nil
}

// AggregateCandles는 작은 구간의 캔들을 더 큰 epoch 정렬 구간으로 합성한다.
// 입력 순서와 중복 시각에 관계없이 처리하며 결과는 최신 구간부터 반환한다.
func AggregateCandles(source []Candle, interval time.Duration, limit int) ([]Candle, error) {
	if interval <= 0 || interval%time.Millisecond != 0 {
		return nil, validationError("aggregate candle interval must be a positive millisecond duration")
	}
	if limit < 1 {
		return nil, validationError("aggregate candle limit must be positive")
	}
	byStart := make(map[int64]Candle, len(source))
	starts := make([]int64, 0, len(source))
	for _, candle := range source {
		if candle.StartTime < 0 {
			return nil, fmt.Errorf("aggregate candle timestamp must not be negative")
		}
		if _, exists := byStart[candle.StartTime]; exists {
			continue
		}
		byStart[candle.StartTime] = candle
		starts = append(starts, candle.StartTime)
	}
	sort.Slice(starts, func(left, right int) bool { return starts[left] < starts[right] })
	type bucketCandle struct {
		candle Candle
		volume scaledDecimal
	}
	intervalMillis := interval.Milliseconds()
	buckets := make(map[int64]bucketCandle)
	for _, start := range starts {
		item := byStart[start]
		bucketStart := start - start%intervalMillis
		volume, err := parseScaledDecimal(item.Volume)
		if err != nil {
			return nil, fmt.Errorf("decode aggregate candle volume: %w", err)
		}
		current, exists := buckets[bucketStart]
		if !exists {
			if _, err := compareDecimals(item.High, item.Low); err != nil {
				return nil, fmt.Errorf("decode aggregate candle price: %w", err)
			}
			current = bucketCandle{
				candle: Candle{
					StartTime: bucketStart, Open: item.Open, High: item.High,
					Low: item.Low, Close: item.Close,
				},
				volume: volume,
			}
		} else {
			comparison, compareErr := compareDecimals(item.High, current.candle.High)
			if compareErr != nil {
				return nil, fmt.Errorf("decode aggregate candle high: %w", compareErr)
			}
			if comparison > 0 {
				current.candle.High = item.High
			}
			comparison, compareErr = compareDecimals(item.Low, current.candle.Low)
			if compareErr != nil {
				return nil, fmt.Errorf("decode aggregate candle low: %w", compareErr)
			}
			if comparison < 0 {
				current.candle.Low = item.Low
			}
			current.candle.Close = item.Close
			current.volume = addScaledDecimals(current.volume, volume)
		}
		buckets[bucketStart] = current
	}
	bucketStarts := make([]int64, 0, len(buckets))
	for start := range buckets {
		bucketStarts = append(bucketStarts, start)
	}
	sort.Slice(bucketStarts, func(left, right int) bool { return bucketStarts[left] > bucketStarts[right] })
	if len(bucketStarts) > limit {
		bucketStarts = bucketStarts[:limit]
	}
	result := make([]Candle, len(bucketStarts))
	for index, start := range bucketStarts {
		bucket := buckets[start]
		bucket.candle.Volume = bucket.volume.String()
		result[index] = bucket.candle
	}
	return result, nil
}

type scaledDecimal struct {
	integer *big.Int
	scale   int
}

func parseScaledDecimal(value string) (scaledDecimal, error) {
	if !positiveDecimalPattern.MatchString(value) {
		return scaledDecimal{}, fmt.Errorf("invalid decimal %q", value)
	}
	whole, fraction, _ := strings.Cut(value, ".")
	integer, ok := new(big.Int).SetString(whole+fraction, 10)
	if !ok {
		return scaledDecimal{}, fmt.Errorf("invalid decimal %q", value)
	}
	return scaledDecimal{integer: integer, scale: len(fraction)}, nil
}

func compareDecimals(left, right string) (int, error) {
	leftValue, err := parseScaledDecimal(left)
	if err != nil {
		return 0, err
	}
	rightValue, err := parseScaledDecimal(right)
	if err != nil {
		return 0, err
	}
	scale := leftValue.scale
	if rightValue.scale > scale {
		scale = rightValue.scale
	}
	return scaleInteger(leftValue, scale).Cmp(scaleInteger(rightValue, scale)), nil
}

func addScaledDecimals(left, right scaledDecimal) scaledDecimal {
	scale := left.scale
	if right.scale > scale {
		scale = right.scale
	}
	return scaledDecimal{
		integer: new(big.Int).Add(scaleInteger(left, scale), scaleInteger(right, scale)),
		scale:   scale,
	}
}

func scaleInteger(value scaledDecimal, scale int) *big.Int {
	result := new(big.Int).Set(value.integer)
	if difference := scale - value.scale; difference > 0 {
		result.Mul(result, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(difference)), nil))
	}
	return result
}

func (value scaledDecimal) String() string {
	digits := value.integer.String()
	if value.scale == 0 {
		return digits
	}
	if len(digits) <= value.scale {
		digits = strings.Repeat("0", value.scale-len(digits)+1) + digits
	}
	position := len(digits) - value.scale
	return digits[:position] + "." + digits[position:]
}
