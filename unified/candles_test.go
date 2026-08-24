package unified

import (
	"errors"
	"testing"
	"time"

	trade "github.com/proven-trade/proven-trade-sdk"
)

func TestAggregateCandlesUsesExactEpochBuckets(t *testing.T) {
	t.Parallel()

	source := []Candle{
		{StartTime: 300_000, Open: "5", High: "6", Low: "4", Close: "5.5", Volume: "0.003"},
		{StartTime: 180_000, Open: "2", High: "4", Low: "1", Close: "3", Volume: "1.1"},
		{StartTime: 360_000, Open: "7", High: "8", Low: "6", Close: "7.5", Volume: "4"},
		{StartTime: 240_000, Open: "3", High: "5", Low: "2", Close: "4", Volume: "2.20"},
		{StartTime: 240_000, Open: "3", High: "5", Low: "2", Close: "4", Volume: "2.20"},
	}
	candles, err := AggregateCandles(source, 3*time.Minute, 2)
	if err != nil {
		t.Fatalf("AggregateCandles() error = %v", err)
	}
	if len(candles) != 2 || candles[0].StartTime != 360_000 || candles[0].Volume != "4" ||
		candles[1].StartTime != 180_000 || candles[1].Open != "2" || candles[1].High != "6" ||
		candles[1].Low != "1" || candles[1].Close != "5.5" || candles[1].Volume != "3.303" {
		t.Fatalf("AggregateCandles() = %+v", candles)
	}
}

func TestAggregateCandlesRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	if _, err := AggregateCandles(nil, 0, 1); !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("zero interval error = %v", err)
	}
	if _, err := AggregateCandles(nil, time.Minute, 0); !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("zero limit error = %v", err)
	}
	if _, err := AggregateCandles([]Candle{{
		StartTime: 0, Open: "1", High: "invalid", Low: "1", Close: "1", Volume: "1",
	}}, time.Minute, 1); err == nil {
		t.Fatal("invalid decimal error = nil")
	}
}

func TestAddDecimalsPreservesPrecision(t *testing.T) {
	t.Parallel()

	got, err := AddDecimals("100000.00000001", "0.00000009", "2.50")
	if err != nil {
		t.Fatalf("AddDecimals() error = %v", err)
	}
	if got != "100002.50000010" {
		t.Fatalf("AddDecimals() = %q", got)
	}
	if _, err := AddDecimals("1e-8"); err == nil {
		t.Fatal("AddDecimals() error = nil")
	}
}
