package upbit

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	"github.com/proven-trade/cex-sdk/transport"
)

func TestExtendedMarketEndpoints(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.Header.Get("Authorization") != "" {
			t.Errorf("public request %s carries an Authorization header", request.URL.RequestURI())
		}
		switch request.Method + " " + request.URL.RequestURI() {
		case "GET /v1/candles/seconds?market=KRW-BTC&to=2025-07-01T08%3A59%3A59%2B09%3A00&count=2":
			if got := request.URL.Query().Get("to"); got != "2025-07-01T08:59:59+09:00" {
				t.Errorf("decoded to = %q, want 2025-07-01T08:59:59+09:00", got)
			}
			_, _ = io.WriteString(writer, `[{"market":"KRW-BTC","candle_date_time_utc":"2025-06-30T23:59:59","candle_date_time_kst":"2025-07-01T08:59:59","opening_price":145794000,"high_price":145794000,"low_price":145759000,"trade_price":145759000,"timestamp":1751327999833,"candle_acc_trade_price":185042,"candle_acc_trade_volume":0.0013},{"market":"KRW-BTC","candle_date_time_utc":"2025-06-30T23:59:58","candle_date_time_kst":"2025-07-01T08:59:58","opening_price":145800000,"high_price":145800000,"low_price":145794000,"trade_price":145794000,"timestamp":1751327998901,"candle_acc_trade_price":9000000.5,"candle_acc_trade_volume":0.06173}]`)
		case "GET /v1/candles/days?market=BTC-ETH&count=1&converting_price_unit=KRW":
			_, _ = io.WriteString(writer, `[{"market":"BTC-ETH","candle_date_time_utc":"2025-06-30T00:00:00","candle_date_time_kst":"2025-06-30T09:00:00","opening_price":0.0245,"high_price":0.0251,"low_price":0.0243,"trade_price":0.025,"timestamp":1751327999833,"candle_acc_trade_price":152.38,"candle_acc_trade_volume":6120.5,"prev_closing_price":0.0245,"change_price":0.0005,"change_rate":0.0204081633,"converted_trade_price":3710000.5}]`)
		case "GET /v1/candles/days?market=KRW-BTC":
			_, _ = io.WriteString(writer, `[{"market":"KRW-BTC","candle_date_time_utc":"2025-06-30T00:00:00","candle_date_time_kst":"2025-06-30T09:00:00","opening_price":147996000,"high_price":148480000,"low_price":145740000,"trade_price":145759000,"timestamp":1751327999833,"candle_acc_trade_price":138812096716.42776,"candle_acc_trade_volume":944.35761221,"prev_closing_price":147996000,"change_price":-2237000,"change_rate":-0.0151152734}]`)
		case "GET /v1/candles/weeks?market=KRW-BTC&count=1":
			_, _ = io.WriteString(writer, `[{"market":"KRW-BTC","candle_date_time_utc":"2025-06-30T00:00:00","candle_date_time_kst":"2025-06-30T09:00:00","opening_price":147996000,"high_price":149755000,"low_price":144265000,"trade_price":148336000,"timestamp":1751605274309,"candle_acc_trade_price":677950313447.2709,"candle_acc_trade_volume":4603.1776237,"first_day_of_period":"2025-06-30"}]`)
		case "GET /v1/candles/months?market=KRW-BTC&count=2":
			_, _ = io.WriteString(writer, `[{"market":"KRW-BTC","candle_date_time_utc":"2025-07-01T00:00:00","candle_date_time_kst":"2025-07-01T09:00:00","opening_price":145759000,"high_price":149755000,"low_price":145000000,"trade_price":148336000,"timestamp":1751605274309,"candle_acc_trade_price":98000000000.1,"candle_acc_trade_volume":660.5,"first_day_of_period":"2025-07-01"},{"market":"KRW-BTC","candle_date_time_utc":"2025-06-01T00:00:00","candle_date_time_kst":"2025-06-01T09:00:00","opening_price":142000000,"high_price":150000000,"low_price":141000000,"trade_price":145759000,"timestamp":1751327999833,"candle_acc_trade_price":3500000000000.7,"candle_acc_trade_volume":24000.25,"first_day_of_period":"2025-06-01"}]`)
		case "GET /v1/candles/years?market=KRW-BTC":
			_, _ = io.WriteString(writer, `[{"market":"KRW-BTC","candle_date_time_utc":"2025-01-01T00:00:00","candle_date_time_kst":"2025-01-01T09:00:00","opening_price":139394000,"high_price":163325000,"low_price":111659000,"trade_price":148348000,"timestamp":1751605431315,"candle_acc_trade_price":58039408928064.63,"candle_acc_trade_volume":416778.44227734,"first_day_of_period":"2025-01-01"}]`)
		case "GET /v1/ticker/all?quote_currencies=KRW%2CBTC":
			_, _ = io.WriteString(writer, `[{"market":"KRW-BTC","trade_date":"20250704","trade_time":"051400","trade_date_kst":"20250704","trade_time_kst":"141400","trade_timestamp":1751606040365,"opening_price":148737000,"high_price":149360000,"low_price":148288000,"trade_price":148601000,"prev_closing_price":148737000,"change":"FALL","change_price":136000,"change_rate":0.0009143656,"signed_change_price":-136000,"signed_change_rate":-0.0009143656,"trade_volume":0.00016823,"acc_trade_price":31615925234.05438,"acc_trade_price_24h":178448329314.96686,"acc_trade_volume":212.38911576,"acc_trade_volume_24h":1198.26954807,"highest_52_week_price":163325000,"highest_52_week_date":"2025-01-20","lowest_52_week_price":72100000,"lowest_52_week_date":"2024-08-05","timestamp":1751606040395},{"market":"BTC-ETH","trade_date":"20250704","trade_time":"051400","trade_date_kst":"20250704","trade_time_kst":"141400","trade_timestamp":1751606040111,"opening_price":0.02465,"high_price":0.02501,"low_price":0.0244,"trade_price":0.0248,"prev_closing_price":0.02465,"change":"RISE","change_price":0.00015,"change_rate":0.0060851927,"signed_change_price":0.00015,"signed_change_rate":0.0060851927,"trade_volume":1.5,"acc_trade_price":120.5,"acc_trade_price_24h":410.25,"acc_trade_volume":4890.2,"acc_trade_volume_24h":16500.8,"highest_52_week_price":0.0335,"highest_52_week_date":"2025-02-01","lowest_52_week_price":0.0182,"lowest_52_week_date":"2025-04-09","timestamp":1751606040222}]`)
		case "GET /v1/orderbook/instruments?markets=KRW-BTC%2CKRW-ETH":
			_, _ = io.WriteString(writer, `[{"market":"KRW-BTC","quote_currency":"KRW","tick_size":"1000","supported_levels":["0","10000","100000","1000000","10000000","100000000"]},{"market":"KRW-ETH","quote_currency":"KRW","tick_size":1000,"supported_levels":[0,10000,100000,1000000]}]`)
		default:
			t.Errorf("unexpected request %s %s", request.Method, request.URL.RequestURI())
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	sender := &directSender{}
	client, limiter := newPublicTestClient(t, server.URL, sender)
	ctx := context.Background()

	seconds, err := client.SecondCandles(ctx, CandlesRequest{Market: "KRW-BTC", To: "2025-07-01T08:59:59+09:00", Count: 2})
	if err != nil || len(seconds) != 2 || seconds[0].Market != "KRW-BTC" || seconds[0].CandleDateTimeKST != "2025-07-01T08:59:59" ||
		seconds[0].OpeningPrice != "145794000" || seconds[0].TradePrice != "145759000" || seconds[0].Timestamp != 1751327999833 ||
		seconds[0].AccumulatedTradeVolume != "0.0013" || seconds[1].AccumulatedTradePrice != "9000000.5" ||
		seconds[0].Unit != 0 || len(seconds[0].Raw) == 0 {
		t.Fatalf("SecondCandles() = %+v, error = %v", seconds, err)
	}
	converted, err := client.DayCandles(ctx, DayCandlesRequest{Market: "BTC-ETH", Count: 1, ConvertingPriceUnit: "KRW"})
	if err != nil || len(converted) != 1 || converted[0].TradePrice != "0.025" || converted[0].ConvertedTradePrice != "3710000.5" ||
		converted[0].PreviousClosingPrice != "0.0245" || converted[0].ChangeRate != "0.0204081633" || len(converted[0].Raw) == 0 {
		t.Fatalf("DayCandles(converting) = %+v, error = %v", converted, err)
	}
	days, err := client.DayCandles(ctx, DayCandlesRequest{Market: "KRW-BTC"})
	if err != nil || len(days) != 1 || days[0].CandleDateTimeUTC != "2025-06-30T00:00:00" || days[0].ChangePrice != "-2237000" ||
		days[0].ChangeRate != "-0.0151152734" || days[0].AccumulatedTradePrice != "138812096716.42776" ||
		days[0].ConvertedTradePrice != "" {
		t.Fatalf("DayCandles() = %+v, error = %v", days, err)
	}
	weeks, err := client.WeekCandles(ctx, CandlesRequest{Market: "KRW-BTC", Count: 1}, trade.WithEgressRoute("route-b"))
	if err != nil || len(weeks) != 1 || weeks[0].FirstDayOfPeriod != "2025-06-30" || weeks[0].HighPrice != "149755000" ||
		weeks[0].AccumulatedTradeVolume != "4603.1776237" || len(weeks[0].Raw) == 0 {
		t.Fatalf("WeekCandles() = %+v, error = %v", weeks, err)
	}
	months, err := client.MonthCandles(ctx, CandlesRequest{Market: "KRW-BTC", Count: 2})
	if err != nil || len(months) != 2 || months[0].FirstDayOfPeriod != "2025-07-01" || months[1].FirstDayOfPeriod != "2025-06-01" ||
		months[1].AccumulatedTradeVolume != "24000.25" {
		t.Fatalf("MonthCandles() = %+v, error = %v", months, err)
	}
	years, err := client.YearCandles(ctx, CandlesRequest{Market: "KRW-BTC"})
	if err != nil || len(years) != 1 || years[0].FirstDayOfPeriod != "2025-01-01" || years[0].LowPrice != "111659000" ||
		years[0].AccumulatedTradePrice != "58039408928064.63" {
		t.Fatalf("YearCandles() = %+v, error = %v", years, err)
	}
	tickers, err := client.QuoteTickers(ctx, QuoteTickersRequest{QuoteCurrencies: []string{"KRW", "BTC"}})
	if err != nil || len(tickers) != 2 || tickers[0].Market != "KRW-BTC" || tickers[0].TradePrice != "148601000" ||
		tickers[0].Change != "FALL" || tickers[0].SignedChangePrice != "-136000" || tickers[0].TradeTimestamp != 1751606040365 ||
		tickers[0].AccumulatedVolume != "1198.26954807" || tickers[1].Market != "BTC-ETH" || tickers[1].TradePrice != "0.0248" ||
		len(tickers[1].Raw) == 0 || !strings.Contains(string(tickers[0].Raw), `"acc_trade_volume":212.38911576`) {
		t.Fatalf("QuoteTickers() = %+v, error = %v", tickers, err)
	}
	instruments, err := client.OrderBookInstruments(ctx, OrderBookInstrumentsRequest{Markets: []string{"KRW-BTC", "KRW-ETH"}})
	if err != nil || len(instruments) != 2 || instruments[0].Market != "KRW-BTC" || instruments[0].QuoteCurrency != "KRW" ||
		instruments[0].TickSize != "1000" || len(instruments[0].SupportedLevels) != 6 || instruments[0].SupportedLevels[5] != "100000000" ||
		instruments[1].TickSize != "1000" || len(instruments[1].SupportedLevels) != 4 || instruments[1].SupportedLevels[3] != "1000000" ||
		len(instruments[0].Raw) == 0 {
		t.Fatalf("OrderBookInstruments() = %+v, error = %v", instruments, err)
	}

	wantRoutes := []transport.EgressRouteID{
		"route-a", "route-a", "route-a", "route-b", "route-a", "route-a", "route-a", "route-a",
	}
	if routes := sender.snapshot(); !slices.Equal(routes, wantRoutes) {
		t.Fatalf("sender routes = %v, want %v", routes, wantRoutes)
	}
	for _, key := range []string{
		"upbit:route:route-a:candle:1second",
		"upbit:route:route-b:candle:1second",
		"upbit:route:route-a:ticker:1second",
		"upbit:route:route-a:orderbook:1second",
	} {
		assertRateRule(t, limiter, key, DefaultPublicRequestsPerSecond, time.Second)
	}
}

func TestExtendedMarketErrorResponses(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/v1/ticker/all":
			writer.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(writer, `{"error":{"name":400,"message":"Invalid parameter. Check the given value!"}}`)
		case "/v1/orderbook/instruments":
			writer.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(writer, `{"error":{"name":"too_many_requests","message":"Too many API requests."}}`)
		case "/v1/candles/days":
			writer.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(writer, `{"error":{"name":"internal_server_error","message":"temporary"}}`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client, _ := newPublicTestClient(t, server.URL, &directSender{})
	ctx := context.Background()

	_, err := client.QuoteTickers(ctx, QuoteTickersRequest{QuoteCurrencies: []string{"KRW"}})
	apiError := requireAPIError(t, err, http.StatusBadRequest, "400")
	if apiError.ExchangeMessage != "Invalid parameter. Check the given value!" || apiError.Retryable {
		t.Fatalf("QuoteTickers() error = %+v, want non-retryable message from exchange", apiError)
	}
	_, err = client.OrderBookInstruments(ctx, OrderBookInstrumentsRequest{Markets: []string{"KRW-BTC"}})
	if apiError = requireAPIError(t, err, http.StatusTooManyRequests, "too_many_requests"); !apiError.Retryable {
		t.Fatalf("OrderBookInstruments() error = %+v, want retryable rate limit", apiError)
	}
	_, err = client.DayCandles(ctx, DayCandlesRequest{Market: "KRW-BTC"})
	if apiError = requireAPIError(t, err, http.StatusInternalServerError, "internal_server_error"); !apiError.Retryable {
		t.Fatalf("DayCandles() error = %+v, want retryable exchange unavailable", apiError)
	}
}

func TestExtendedMarketRequestsAreValidatedBeforeSend(t *testing.T) {
	t.Parallel()

	cases := []callCase{
		{"SecondCandles missing market", func(ctx context.Context, client *Client) error {
			_, err := client.SecondCandles(ctx, CandlesRequest{Count: 1})
			return err
		}},
		{"SecondCandles count above 200", func(ctx context.Context, client *Client) error {
			_, err := client.SecondCandles(ctx, CandlesRequest{Market: "KRW-BTC", Count: 201})
			return err
		}},
		{"DayCandles lowercase converting unit", func(ctx context.Context, client *Client) error {
			_, err := client.DayCandles(ctx, DayCandlesRequest{Market: "KRW-BTC", ConvertingPriceUnit: "krw"})
			return err
		}},
		{"DayCandles negative count", func(ctx context.Context, client *Client) error {
			_, err := client.DayCandles(ctx, DayCandlesRequest{Market: "KRW-BTC", Count: -1})
			return err
		}},
		{"WeekCandles market without quote separator", func(ctx context.Context, client *Client) error {
			_, err := client.WeekCandles(ctx, CandlesRequest{Market: "KRWBTC"})
			return err
		}},
		{"MonthCandles market list", func(ctx context.Context, client *Client) error {
			_, err := client.MonthCandles(ctx, CandlesRequest{Market: "KRW-BTC,KRW-ETH"})
			return err
		}},
		{"YearCandles padded market", func(ctx context.Context, client *Client) error {
			_, err := client.YearCandles(ctx, CandlesRequest{Market: " KRW-BTC"})
			return err
		}},
		{"QuoteTickers empty", func(ctx context.Context, client *Client) error {
			_, err := client.QuoteTickers(ctx, QuoteTickersRequest{})
			return err
		}},
		{"QuoteTickers lowercase", func(ctx context.Context, client *Client) error {
			_, err := client.QuoteTickers(ctx, QuoteTickersRequest{QuoteCurrencies: []string{"krw"}})
			return err
		}},
		{"QuoteTickers duplicate", func(ctx context.Context, client *Client) error {
			_, err := client.QuoteTickers(ctx, QuoteTickersRequest{QuoteCurrencies: []string{"KRW", "KRW"}})
			return err
		}},
		{"OrderBookInstruments empty", func(ctx context.Context, client *Client) error {
			_, err := client.OrderBookInstruments(ctx, OrderBookInstrumentsRequest{})
			return err
		}},
		{"OrderBookInstruments duplicate", func(ctx context.Context, client *Client) error {
			_, err := client.OrderBookInstruments(ctx, OrderBookInstrumentsRequest{Markets: []string{"KRW-BTC", "KRW-BTC"}})
			return err
		}},
		{"OrderBookInstruments malformed", func(ctx context.Context, client *Client) error {
			_, err := client.OrderBookInstruments(ctx, OrderBookInstrumentsRequest{Markets: []string{"BTC"}})
			return err
		}},
	}
	assertRejectedBeforeSend(t, trade.ErrValidation, cases, credential.PermissionRead)
}

func TestExtendedMarketRequestValidation(t *testing.T) {
	t.Parallel()

	tooManyMarkets := make([]string, 101)
	for index := range tooManyMarkets {
		tooManyMarkets[index] = "KRW-C" + strconv.Itoa(index)
	}
	valid := []depositValidationCase{
		{"candles default count", CandlesRequest{Market: "KRW-BTC"}.validate()},
		{"candles maximum count", CandlesRequest{Market: "KRW-BTC", To: "2025-07-01T00:00:00Z", Count: 200}.validate()},
		{"day converting unit", DayCandlesRequest{Market: "BTC-ETH", Count: 1, ConvertingPriceUnit: "KRW"}.validate()},
		{"quote tickers", QuoteTickersRequest{QuoteCurrencies: []string{"KRW", "BTC", "USDT"}}.validate()},
		{"instruments", OrderBookInstrumentsRequest{Markets: []string{"KRW-BTC", "BTC-ETH"}}.validate()},
	}
	invalid := []depositValidationCase{
		{"candles empty market", CandlesRequest{}.validate()},
		{"candles count 201", CandlesRequest{Market: "KRW-BTC", Count: 201}.validate()},
		{"day converting unit lowercase", DayCandlesRequest{Market: "KRW-BTC", ConvertingPriceUnit: "krw"}.validate()},
		{"day converting unit too long", DayCandlesRequest{Market: "KRW-BTC", ConvertingPriceUnit: strings.Repeat("K", 21)}.validate()},
		{"quote tickers nil", QuoteTickersRequest{}.validate()},
		{"quote tickers empty item", QuoteTickersRequest{QuoteCurrencies: []string{""}}.validate()},
		{"instruments nil", OrderBookInstrumentsRequest{}.validate()},
		{"instruments above 100", OrderBookInstrumentsRequest{Markets: tooManyMarkets}.validate()},
	}
	assertValidationCases(t, valid, invalid)

	if got := (CandlesRequest{Market: "KRW-BTC", To: "2025-07-01T00:00:00Z", Count: 3}).parameters().encoded(); got != "market=KRW-BTC&to=2025-07-01T00%3A00%3A00Z&count=3" {
		t.Fatalf("CandlesRequest.parameters() = %q", got)
	}
	if got := (DayCandlesRequest{Market: "KRW-BTC", ConvertingPriceUnit: "KRW"}).parameters().encoded(); got != "market=KRW-BTC&converting_price_unit=KRW" {
		t.Fatalf("DayCandlesRequest.parameters() = %q", got)
	}
}
