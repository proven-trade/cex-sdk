package archive

import (
	"context"
	"errors"
	"io"
	"slices"
	"strconv"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/unified"
)

// TradeCandles는 한 달치 체결 덤프 전체를 interval 캔들로 합성해 시간 오름차순으로 반환한다.
// Spot 1분·5분 캔들 덤프는 2024년 이후 게시가 끊겼으므로 체결 덤프로 같은 구간을 만든다.
// 월 경계가 구간 경계와 일치해야 하므로 interval은 하루를 나누어떨어지게 해야 한다.
// 파일의 기록 방향은 체결 ID로 판별하며 체결이 없는 구간은 만들지 않는다.
func (client *Client) TradeCandles(
	ctx context.Context,
	request TradesRequest,
	interval time.Duration,
	options ...trade.RequestOption,
) ([]unified.TradeCandle, error) {
	if interval <= 0 || interval%time.Millisecond != 0 || (24*time.Hour)%interval != 0 {
		return nil, validationError("trade candle interval must evenly divide one day")
	}
	if _, err := request.path(); err != nil {
		return nil, err
	}
	reader, err := client.Trades(ctx, request, options...)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	first, err := reader.Next()
	if errors.Is(err, io.EOF) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	pending := []Trade{first}
	order := unified.TradeOrderAscending
	for {
		item, nextErr := reader.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			return nil, nextErr
		}
		pending = append(pending, item)
		if item.ID != first.ID {
			if tradeIDLess(item.ID, first.ID) {
				order = unified.TradeOrderDescending
			}
			break
		}
	}

	builder, err := unified.NewTradeCandleBuilder(interval, order)
	if err != nil {
		return nil, err
	}
	var candles []unified.TradeCandle
	add := func(item Trade) error {
		candle, completed, addErr := builder.Add(item.Unified())
		if addErr != nil {
			return reader.stream.recordError(addErr)
		}
		if completed {
			candles = append(candles, candle)
		}
		return nil
	}
	for _, item := range pending {
		if err := add(item); err != nil {
			return nil, err
		}
	}
	for {
		item, nextErr := reader.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			return nil, nextErr
		}
		if err := add(item); err != nil {
			return nil, err
		}
	}
	if candle, ok := builder.Flush(); ok {
		candles = append(candles, candle)
	}
	if order == unified.TradeOrderDescending {
		slices.Reverse(candles)
	}
	return candles, nil
}

func tradeIDLess(left, right string) bool {
	leftValue, _ := strconv.ParseUint(left, 10, 64)
	rightValue, _ := strconv.ParseUint(right, 10, 64)
	return leftValue < rightValue
}
