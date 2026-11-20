package bybit

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/transport"
)

// newPublicExtTestClient는 서명 없는 공개 요청을 기록하는 테스트 서버에 연결된 클라이언트를 만든다.
func newPublicExtTestClient(t *testing.T, result string) (*Client, *extRecorder) {
	t.Helper()
	recorder := &extRecorder{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		recorder.record(extCall{
			Method: request.Method, Path: request.URL.Path, Query: request.URL.Query(), Body: string(body),
		})
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, extEnvelope(result))
	}))
	t.Cleanup(server.Close)
	client, _ := newTestClient(
		t, server.URL, &directSender{}, &recordingProvider{},
		[]transport.EgressRouteID{"route-a"}, func() time.Time { return time.UnixMilli(1_700_000_000_000) },
	)
	return client, recorder
}

func assertQuery(t *testing.T, got url.Values, want map[string]string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("query = %v, want %v", got, want)
	}
	for key, value := range want {
		if got.Get(key) != value {
			t.Fatalf("query[%s] = %q, want %q (query = %v)", key, got.Get(key), value, got)
		}
	}
	if got.Has("signature") {
		t.Fatalf("public query must not be signed: %v", got)
	}
}

func TestPriceCandles(t *testing.T) {
	t.Parallel()

	start := time.UnixMilli(1670601600000)
	end := time.UnixMilli(1670608800000)
	tests := []struct {
		name string
		path string
		call func(*Client, CandlesRequest) ([]PriceCandle, error)
	}{
		{"mark", "/v5/market/mark-price-kline", func(c *Client, r CandlesRequest) ([]PriceCandle, error) {
			return c.MarkPriceCandles(context.Background(), r)
		}},
		{"index", "/v5/market/index-price-kline", func(c *Client, r CandlesRequest) ([]PriceCandle, error) {
			return c.IndexPriceCandles(context.Background(), r)
		}},
		{"premium", "/v5/market/premium-index-price-kline", func(c *Client, r CandlesRequest) ([]PriceCandle, error) {
			return c.PremiumIndexPriceCandles(context.Background(), r)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			client, recorder := newPublicExtTestClient(t, `{"symbol":"BTCUSDT","category":"linear","list":[`+
				`["1670608800000","17164.16","17164.16","17121.5","17131.64"],`+
				`["1670605200000","-0.000587","-0.000344","-0.000480","-0.000344"]]}`)
			candles, err := test.call(client, CandlesRequest{
				Category: CategoryLinear, Symbol: "BTCUSDT", Interval: Candle15Minutes,
				StartTime: &start, EndTime: &end, Limit: 2,
			})
			if err != nil || len(candles) != 2 {
				t.Fatalf("candles = %+v, error = %v", candles, err)
			}
			if candles[0] != (PriceCandle{
				StartTime: "1670608800000", Open: "17164.16", High: "17164.16", Low: "17121.5", Close: "17131.64",
			}) || candles[1].Open != "-0.000587" {
				t.Fatalf("decoded candles = %+v", candles)
			}
			call := extSingleCall(t, recorder, http.MethodGet, test.path)
			assertQuery(t, call.Query, map[string]string{
				"category": "linear", "symbol": "BTCUSDT", "interval": "15",
				"start": "1670601600000", "end": "1670608800000", "limit": "2",
			})
		})
	}
}

func TestPriceCandlesValidation(t *testing.T) {
	t.Parallel()

	client, recorder := newPublicExtTestClient(t, `{}`)
	valid := CandlesRequest{Category: CategoryLinear, Symbol: "BTCUSDT", Interval: Candle1Hour}
	tests := map[string]func(*CandlesRequest){
		"spot category":    func(r *CandlesRequest) { r.Category = CategorySpot },
		"missing symbol":   func(r *CandlesRequest) { r.Symbol = "" },
		"invalid interval": func(r *CandlesRequest) { r.Interval = "2" },
		"too large limit":  func(r *CandlesRequest) { r.Limit = 1001 },
	}
	for name, mutate := range tests {
		request := valid
		mutate(&request)
		if _, err := client.MarkPriceCandles(context.Background(), request); !errors.Is(err, trade.ErrValidation) {
			t.Fatalf("%s: error = %v, want validation error", name, err)
		}
	}
	if calls := recorder.snapshot(); len(calls) != 0 {
		t.Fatalf("invalid requests sent calls: %+v", calls)
	}
}

func TestFundingRateHistory(t *testing.T) {
	t.Parallel()

	client, recorder := newPublicExtTestClient(t, `{"category":"linear","list":[`+
		`{"symbol":"ETHPERP","fundingRate":"0.0001","fundingRateTimestamp":"1672041600000"}]}`)
	end := time.UnixMilli(1672051897447)
	start := time.UnixMilli(1672041600000)
	records, err := client.FundingRateHistory(context.Background(), FundingRateHistoryRequest{
		Category: CategoryLinear, Symbol: "ETHPERP", StartTime: &start, EndTime: &end, Limit: 1,
	})
	if err != nil || len(records) != 1 || records[0].FundingRate != "0.0001" ||
		records[0].FundingRateTimestamp != "1672041600000" || records[0].Symbol != "ETHPERP" || len(records[0].Raw) == 0 {
		t.Fatalf("FundingRateHistory() = %+v, error = %v", records, err)
	}
	call := extSingleCall(t, recorder, http.MethodGet, "/v5/market/funding/history")
	assertQuery(t, call.Query, map[string]string{
		"category": "linear", "symbol": "ETHPERP", "startTime": "1672041600000", "endTime": "1672051897447", "limit": "1",
	})

	if _, err := client.FundingRateHistory(context.Background(), FundingRateHistoryRequest{
		Category: CategoryLinear, Symbol: "ETHPERP", StartTime: &start,
	}); !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("startTime without endTime error = %v, want validation error", err)
	}
	if _, err := client.FundingRateHistory(context.Background(), FundingRateHistoryRequest{
		Category: CategoryLinear, Symbol: "ETHPERP", Limit: 201,
	}); !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("limit 201 error = %v, want validation error", err)
	}
}

func TestOpenInterest(t *testing.T) {
	t.Parallel()

	client, recorder := newPublicExtTestClient(t, `{"symbol":"BTCUSDT","category":"linear","list":[`+
		`{"openInterest":"461134384.00000000","timestamp":"1669571400000"},`+
		`{"openInterest":"461134292.00000000","timestamp":"1669571100000"}],"nextPageCursor":"page-2"}`)
	page, err := client.OpenInterest(context.Background(), OpenInterestRequest{
		Category: CategoryLinear, Symbol: "BTCUSDT", IntervalTime: DataPeriod5Minutes, Limit: 2, Cursor: "page-1",
	})
	if err != nil || page.Category != CategoryLinear || page.Symbol != "BTCUSDT" || len(page.Records) != 2 ||
		page.Records[1].OpenInterest != "461134292.00000000" || page.NextPageCursor != "page-2" || len(page.Raw) == 0 {
		t.Fatalf("OpenInterest() = %+v, error = %v", page, err)
	}
	call := extSingleCall(t, recorder, http.MethodGet, "/v5/market/open-interest")
	assertQuery(t, call.Query, map[string]string{
		"category": "linear", "symbol": "BTCUSDT", "intervalTime": "5min", "limit": "2", "cursor": "page-1",
	})
	if _, err := client.OpenInterest(context.Background(), OpenInterestRequest{
		Category: CategoryLinear, Symbol: "BTCUSDT", IntervalTime: "1min",
	}); !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("invalid interval error = %v, want validation error", err)
	}
}

func TestInsurance(t *testing.T) {
	t.Parallel()

	client, recorder := newPublicExtTestClient(t, `{"updatedTime":"1672012800000","list":[`+
		`{"coin":"ETH","balance":"0.00187332","value":"0"}]}`)
	pool, err := client.Insurance(context.Background(), InsuranceRequest{Coin: "ETH"})
	if err != nil || pool.UpdatedTime != "1672012800000" || len(pool.Balances) != 1 ||
		pool.Balances[0].Coin != "ETH" || pool.Balances[0].Balance != "0.00187332" || len(pool.Raw) == 0 {
		t.Fatalf("Insurance() = %+v, error = %v", pool, err)
	}
	call := extSingleCall(t, recorder, http.MethodGet, "/v5/market/insurance")
	assertQuery(t, call.Query, map[string]string{"coin": "ETH"})
	if _, err := client.Insurance(context.Background(), InsuranceRequest{Coin: " ETH"}); !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("whitespace coin error = %v, want validation error", err)
	}
}

func TestRiskLimits(t *testing.T) {
	t.Parallel()

	client, recorder := newPublicExtTestClient(t, `{"category":"linear","list":[`+
		`{"id":1,"symbol":"BTCUSDT","riskLimitValue":"150","maintenanceMargin":0.5,"initialMargin":"1","isLowestRisk":1,"maxLeverage":"100.00"}]}`)
	tiers, err := client.RiskLimits(context.Background(), RiskLimitRequest{Category: CategoryLinear, Symbol: "BTCUSDT"})
	if err != nil || len(tiers) != 1 || tiers[0].ID != 1 || tiers[0].MaintenanceMargin != "0.5" ||
		tiers[0].InitialMargin != "1" || tiers[0].IsLowestRisk != 1 || tiers[0].MaxLeverage != "100.00" {
		t.Fatalf("RiskLimits() = %+v, error = %v", tiers, err)
	}
	call := extSingleCall(t, recorder, http.MethodGet, "/v5/market/risk-limit")
	assertQuery(t, call.Query, map[string]string{"category": "linear", "symbol": "BTCUSDT"})
	if _, err := client.RiskLimits(context.Background(), RiskLimitRequest{Category: CategorySpot}); !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("spot category error = %v, want validation error", err)
	}
}

func TestDeliveryPrices(t *testing.T) {
	t.Parallel()

	client, recorder := newPublicExtTestClient(t, `{"category":"linear","nextPageCursor":"","list":[`+
		`{"symbol":"BTC-27DEC24","deliveryPrice":"96000.5","deliveryTime":"1735286400000"}]}`)
	page, err := client.DeliveryPrices(context.Background(), DeliveryPriceRequest{
		Category: CategoryLinear, Symbol: "BTC-27DEC24", Limit: 10,
	})
	if err != nil || page.Category != CategoryLinear || len(page.Records) != 1 ||
		page.Records[0].DeliveryPrice != "96000.5" || page.Records[0].DeliveryTime != "1735286400000" {
		t.Fatalf("DeliveryPrices() = %+v, error = %v", page, err)
	}
	call := extSingleCall(t, recorder, http.MethodGet, "/v5/market/delivery-price")
	assertQuery(t, call.Query, map[string]string{"category": "linear", "symbol": "BTC-27DEC24", "limit": "10"})
}

func TestAccountRatios(t *testing.T) {
	t.Parallel()

	client, recorder := newPublicExtTestClient(t, `{"list":[`+
		`{"symbol":"BTCUSDT","buyRatio":"0.5777","sellRatio":"0.4223","timestamp":"1695772800000"}]}`)
	records, err := client.AccountRatios(context.Background(), AccountRatioRequest{
		Category: CategoryLinear, Symbol: "BTCUSDT", Period: DataPeriod1Day, Limit: 1,
	})
	if err != nil || len(records) != 1 || records[0].BuyRatio != "0.5777" ||
		records[0].SellRatio != "0.4223" || records[0].Timestamp != "1695772800000" {
		t.Fatalf("AccountRatios() = %+v, error = %v", records, err)
	}
	call := extSingleCall(t, recorder, http.MethodGet, "/v5/market/account-ratio")
	assertQuery(t, call.Query, map[string]string{
		"category": "linear", "symbol": "BTCUSDT", "period": "1d", "limit": "1",
	})
	if _, err := client.AccountRatios(context.Background(), AccountRatioRequest{
		Category: CategoryLinear, Symbol: "BTCUSDT", Period: DataPeriod1Day, Limit: 501,
	}); !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("limit 501 error = %v, want validation error", err)
	}
}

func TestDecimalStringUnmarshal(t *testing.T) {
	t.Parallel()

	tests := map[string]DecimalString{`0.5`: "0.5", `"0.50"`: "0.50", `null`: "", `1`: "1"}
	for input, want := range tests {
		var got DecimalString
		if err := got.UnmarshalJSON([]byte(input)); err != nil || got != want {
			t.Fatalf("UnmarshalJSON(%s) = %q, error = %v, want %q", input, got, err, want)
		}
	}
}
