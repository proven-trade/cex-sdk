package bithumb

import (
	"context"
	"errors"
	"testing"

	trade "github.com/proven-trade/cex-sdk"
)

const dayCandleSample = `[{"market":"BTC-ETH","candle_date_time_utc":"2024-05-01T00:00:00","candle_date_time_kst":"2024-05-01T09:00:00",` +
	`"opening_price":0.05,"high_price":0.052,"low_price":0.049,"trade_price":0.051,"timestamp":1714608000123,` +
	`"candle_acc_trade_price":1234.5,"candle_acc_trade_volume":24000.25,"prev_closing_price":0.05,` +
	`"change_price":0.001,"change_rate":0.02,"converted_trade_price":3200000}]`

const periodCandleSample = `[{"market":"KRW-BTC","candle_date_time_utc":"2024-04-29T00:00:00","candle_date_time_kst":"2024-04-29T09:00:00",` +
	`"opening_price":90000000,"high_price":95000000,"low_price":88000000,"trade_price":93000000,"timestamp":1714608000123,` +
	`"candle_acc_trade_price":1500000000000,"candle_acc_trade_volume":16500.5,"first_day_of_period":"2024-04-29"}]`

func TestDayWeekAndMonthCandles(t *testing.T) {
	t.Parallel()

	fixture := newExtFixture(t, []extRoute{
		extOK("GET /v1/candles/days", dayCandleSample),
		extOK("GET /v1/candles/weeks", periodCandleSample),
		extOK("GET /v1/candles/months", periodCandleSample),
	}, extReadOnly)
	ctx := context.Background()

	days, err := fixture.client.DayCandles(ctx, DayCandlesRequest{
		Market: "BTC-ETH", To: "2024-05-02T00:00:00", Count: 2, ConvertingPriceUnit: ConvertingPriceUnitKRW,
	})
	if err != nil || len(days) != 1 || days[0].Market != "BTC-ETH" || days[0].ConvertedTradePrice != "3200000" ||
		days[0].PreviousClosingPrice != "0.05" || days[0].ChangeRate != "0.02" ||
		days[0].Timestamp != 1714608000123 || len(days[0].Raw) == 0 {
		t.Fatalf("DayCandles() = %+v, error = %v", days, err)
	}
	weeks, err := fixture.client.WeekCandles(ctx, PeriodCandlesRequest{Market: "KRW-BTC", Count: 1})
	if err != nil || len(weeks) != 1 || weeks[0].FirstDayOfPeriod != "2024-04-29" ||
		weeks[0].AccumulatedTradePrice != "1500000000000" || len(weeks[0].Raw) == 0 {
		t.Fatalf("WeekCandles() = %+v, error = %v", weeks, err)
	}
	months, err := fixture.client.MonthCandles(ctx, PeriodCandlesRequest{Market: "KRW-BTC", To: "2024-05-01 09:00:00"})
	if err != nil || len(months) != 1 || months[0].TradePrice != "93000000" || len(months[0].Raw) == 0 {
		t.Fatalf("MonthCandles() = %+v, error = %v", months, err)
	}

	records := extRequests(t, fixture.server, 3)
	assertExtPublicRequest(
		t, records[0], "/v1/candles/days",
		"market=BTC-ETH&to=2024-05-02T00%3A00%3A00&count=2&convertingPriceUnit=KRW",
	)
	assertExtPublicRequest(t, records[1], "/v1/candles/weeks", "market=KRW-BTC&count=1")
	assertExtPublicRequest(t, records[2], "/v1/candles/months", "market=KRW-BTC&to=2024-05-01+09%3A00%3A00")
	assertExtLimiter(t, fixture.limiter, "bithumb:route:route-a:public-candle:1second", 150, 3)
	if calls, _, _ := fixture.provider.snapshot(); calls != 0 {
		t.Fatalf("credential provider calls = %d, want 0 for public candles", calls)
	}
}

func TestCandleRequestsRejectInvalidInputBeforeSending(t *testing.T) {
	t.Parallel()

	fixture := newExtFixture(t, nil, extReadOnly)
	ctx := context.Background()
	if _, err := fixture.client.DayCandles(ctx, DayCandlesRequest{Market: "KRW-BTC", ConvertingPriceUnit: "USD"}); !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("DayCandles() error = %v, want ErrValidation", err)
	}
	if _, err := fixture.client.WeekCandles(ctx, PeriodCandlesRequest{Market: "krw-btc"}); !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("WeekCandles() error = %v, want ErrValidation", err)
	}
	if _, err := fixture.client.MonthCandles(ctx, PeriodCandlesRequest{Market: "KRW-BTC", Count: 201}); !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("MonthCandles() error = %v, want ErrValidation", err)
	}
	if records := fixture.server.requests(); len(records) != 0 {
		t.Fatalf("invalid candle requests reached the server: %+v", records)
	}

	invalid := []struct {
		name string
		err  error
	}{
		{"negative count", (DayCandlesRequest{Market: "KRW-BTC", Count: -1}).validate()},
		{"count above limit", (DayCandlesRequest{Market: "KRW-BTC", Count: 201}).validate()},
		{"lowercase conversion unit", (DayCandlesRequest{Market: "KRW-BTC", ConvertingPriceUnit: "krw"}).validate()},
		{"to with ampersand", (DayCandlesRequest{Market: "KRW-BTC", To: "2024-01-01&count=9"}).validate()},
		{"reversed market", (PeriodCandlesRequest{Market: "BTCKRW"}).validate()},
		{"period negative count", (PeriodCandlesRequest{Market: "KRW-BTC", Count: -5}).validate()},
	}
	for _, test := range invalid {
		if !errors.Is(test.err, trade.ErrValidation) {
			t.Errorf("%s: error = %v, want ErrValidation", test.name, test.err)
		}
	}
	valid := []error{
		(DayCandlesRequest{Market: "KRW-BTC", Count: 200, ConvertingPriceUnit: "KRW"}).validate(),
		(PeriodCandlesRequest{Market: "KRW-BTC"}).validate(),
	}
	for index, err := range valid {
		if err != nil {
			t.Errorf("valid candle request %d error = %v", index, err)
		}
	}
}
