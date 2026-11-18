package unified

import (
	"errors"
	"testing"
	"time"

	trade "github.com/proven-trade/cex-sdk"
)

func TestTradeCandleBuilderAggregatesAcrossPages(t *testing.T) {
	t.Parallel()

	builder, err := NewTradeCandleBuilder(time.Minute, TradeOrderAscending)
	if err != nil {
		t.Fatalf("NewTradeCandleBuilder() error = %v", err)
	}
	firstPage := []PublicTrade{
		{Timestamp: 60_000, Price: "100.5", Quantity: "0.1"},
		{Timestamp: 60_000, Price: "101", Quantity: "0.25"},
	}
	secondPage := []PublicTrade{
		{Timestamp: 119_999, Price: "99.75", Quantity: "1"},
		{Timestamp: 119_999, Price: "100.00", Quantity: "0.000001"},
		{Timestamp: 240_000, Price: "102", Quantity: "2"},
	}
	var completed []TradeCandle
	for _, page := range [][]PublicTrade{firstPage, secondPage} {
		for _, item := range page {
			candle, done, addErr := builder.Add(item)
			if addErr != nil {
				t.Fatalf("Add(%+v) error = %v", item, addErr)
			}
			if done {
				completed = append(completed, candle)
			}
		}
	}
	if len(completed) != 1 {
		t.Fatalf("completed candles = %+v, want one", completed)
	}
	first := completed[0]
	if first.StartTime != 60_000 || first.Open != "100.5" || first.High != "101" ||
		first.Low != "99.75" || first.Close != "100.00" || first.Volume != "1.350001" ||
		first.QuoteVolume != "135.05010000" || first.TradeCount != 4 {
		t.Fatalf("first candle = %+v", first)
	}
	last, ok := builder.Flush()
	if !ok || last.StartTime != 240_000 || last.Open != "102" || last.Close != "102" ||
		last.Volume != "2" || last.QuoteVolume != "204" || last.TradeCount != 1 {
		t.Fatalf("Flush() = %+v, %v", last, ok)
	}
	if _, ok := builder.Flush(); ok {
		t.Fatal("second Flush() returned a candle")
	}
}

func TestTradeCandleBuilderMatchesAscendingForDescendingInput(t *testing.T) {
	t.Parallel()

	chronological := []PublicTrade{
		{Timestamp: 0, Price: "10", Quantity: "1"},
		{Timestamp: 0, Price: "12", Quantity: "2"},
		{Timestamp: 30_000, Price: "9", Quantity: "1"},
		{Timestamp: 59_999, Price: "11", Quantity: "1"},
		{Timestamp: 60_000, Price: "11.5", Quantity: "3"},
		{Timestamp: 60_000, Price: "11.25", Quantity: "1"},
	}
	collect := func(order TradeOrder, input []PublicTrade) []TradeCandle {
		builder, err := NewTradeCandleBuilder(time.Minute, order)
		if err != nil {
			t.Fatalf("NewTradeCandleBuilder(%s) error = %v", order, err)
		}
		var candles []TradeCandle
		for _, item := range input {
			candle, done, addErr := builder.Add(item)
			if addErr != nil {
				t.Fatalf("Add(%s, %+v) error = %v", order, item, addErr)
			}
			if done {
				candles = append(candles, candle)
			}
		}
		if candle, ok := builder.Flush(); ok {
			candles = append(candles, candle)
		}
		return candles
	}
	reversed := make([]PublicTrade, len(chronological))
	for index, item := range chronological {
		reversed[len(chronological)-1-index] = item
	}
	ascending := collect(TradeOrderAscending, chronological)
	descending := collect(TradeOrderDescending, reversed)
	if len(ascending) != 2 || len(descending) != 2 ||
		ascending[0] != descending[1] || ascending[1] != descending[0] {
		t.Fatalf("ascending = %+v, descending = %+v", ascending, descending)
	}
	if first := ascending[0]; first.Open != "10" || first.High != "12" || first.Low != "9" ||
		first.Close != "11" || first.Volume != "5" || first.QuoteVolume != "54" {
		t.Fatalf("first candle = %+v", first)
	}
	if second := ascending[1]; second.Open != "11.5" || second.Close != "11.25" {
		t.Fatalf("second candle = %+v", second)
	}
}

func TestTradeCandleBuilderRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	if _, err := NewTradeCandleBuilder(0, TradeOrderAscending); !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("zero interval error = %v", err)
	}
	if _, err := NewTradeCandleBuilder(time.Microsecond, TradeOrderAscending); !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("sub-millisecond interval error = %v", err)
	}
	if _, err := NewTradeCandleBuilder(time.Minute, ""); !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("missing order error = %v", err)
	}
	descending, err := NewTradeCandleBuilder(time.Minute, TradeOrderDescending)
	if err != nil {
		t.Fatalf("NewTradeCandleBuilder(descending) error = %v", err)
	}
	if _, _, err := descending.Add(PublicTrade{Timestamp: 2_000, Price: "1", Quantity: "1"}); err != nil {
		t.Fatalf("descending Add() error = %v", err)
	}
	if _, _, err := descending.Add(PublicTrade{Timestamp: 2_001, Price: "1", Quantity: "1"}); !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("descending forward error = %v", err)
	}
	builder, err := NewTradeCandleBuilder(time.Minute, TradeOrderAscending)
	if err != nil {
		t.Fatalf("NewTradeCandleBuilder() error = %v", err)
	}
	if _, _, err := builder.Add(PublicTrade{Timestamp: 2_000, Price: "1", Quantity: "1"}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if _, _, err := builder.Add(PublicTrade{Timestamp: 1_999, Price: "1", Quantity: "1"}); !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("out-of-order error = %v", err)
	}
	if _, _, err := builder.Add(PublicTrade{Timestamp: 3_000, Price: "1e3", Quantity: "1"}); err == nil {
		t.Fatal("scientific price error = nil")
	}
	if _, _, err := builder.Add(PublicTrade{Timestamp: 3_000, Price: "1", Quantity: "-1"}); err == nil {
		t.Fatal("negative quantity error = nil")
	}
	candle, ok := builder.Flush()
	if !ok || candle.TradeCount != 1 {
		t.Fatalf("rejected trades changed builder state: %+v", candle)
	}
}
