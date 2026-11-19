package usdm

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
	"github.com/proven-trade/cex-sdk/model"
	"github.com/proven-trade/cex-sdk/ratelimit"
	"github.com/proven-trade/cex-sdk/transport"
)

const (
	marketWeightKey = "binance-usdm:route:route-a:request_weight:1minute"
	marketStartMS   = int64(1_700_000_000_000)
	marketEndMS     = int64(1_700_001_800_000)
)

type recordedMarketRequest struct {
	method string
	path   string
	query  string
	header http.Header
}

// marketCase는 엔드포인트 한 건의 기대 요청과 응답, limiter 사용량이다.
// weight는 호출 뒤 요청 weight 규칙의 사용량이고, bucket은 엔드포인트 전용 5분 제한의 이름이다.
type marketCase struct {
	name        string
	path        string
	query       string
	body        string
	weight      int
	bucket      string
	bucketLimit int
	apiKey      bool
	headers     map[string]string
}

// validationCase는 요청 검증 결과 한 건이다.
type validationCase struct {
	name string
	err  error
}

// malformedCase는 해석할 수 없는 응답 본문 한 건이다.
type malformedCase struct {
	name string
	body string
}

// bucketUsage는 limiter 규칙 하나의 기대 사용량이다.
type bucketUsage struct {
	key  string
	used int
}

// weightCase는 캔들 limit과 그 요청 weight다.
type weightCase struct{ limit, weight int }

func newMarketServer(t *testing.T, body string, headers map[string]string) (*httptest.Server, func() []recordedMarketRequest) {
	t.Helper()
	var mu sync.Mutex
	var recorded []recordedMarketRequest
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		mu.Lock()
		recorded = append(recorded, recordedMarketRequest{method: request.Method, path: request.URL.Path, query: request.URL.RawQuery, header: request.Header.Clone()})
		mu.Unlock()
		writer.Header().Set("Content-Type", "application/json")
		for key, value := range headers {
			writer.Header().Set(key, value)
		}
		_, _ = io.WriteString(writer, body)
	}))
	t.Cleanup(server.Close)
	return server, func() []recordedMarketRequest {
		mu.Lock()
		defer mu.Unlock()
		return append([]recordedMarketRequest(nil), recorded...)
	}
}

// newPublicTestClient는 자격증명 없이 공개 엔드포인트만 호출하는 클라이언트를 만든다.
func newPublicTestClient(t *testing.T, baseURL string, sender *directSender) *Client {
	t.Helper()
	limiter, _ := ratelimit.New()
	executor, err := commonexchange.NewExecutor(commonexchange.ExecutorConfig{Sender: sender, Limiter: limiter})
	if err != nil {
		t.Fatal(err)
	}
	client, err := New(Config{Executor: executor, DefaultEgressRouteID: "route-a", BaseURL: baseURL, AllowInsecureHTTP: true})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

// runMarketCase는 엔드포인트 한 건을 호출하고 method·path·query, 서명 부재, API Key 헤더,
// secret 조회 횟수와 limiter 사용량을 검증한다. 응답 해석은 run이 맡는다.
func runMarketCase(t *testing.T, tc marketCase, run func(t *testing.T, client *Client)) {
	t.Helper()
	t.Run(tc.name, func(t *testing.T) {
		t.Parallel()
		server, requests := newMarketServer(t, tc.body, tc.headers)
		provider := &recordingProvider{}
		client, limiter := newTestClient(t, server.URL, &directSender{}, provider, time.UnixMilli(marketStartMS), []transport.EgressRouteID{"route-a"})
		run(t, client)

		got := requests()
		if len(got) != 1 {
			t.Fatalf("requests = %+v, want exactly one", got)
		}
		if got[0].method != http.MethodGet || got[0].path != tc.path || got[0].query != tc.query {
			t.Fatalf("request = %s %s?%s, want GET %s?%s", got[0].method, got[0].path, got[0].query, tc.path, tc.query)
		}
		values, err := url.ParseQuery(got[0].query)
		if err != nil {
			t.Fatal(err)
		}
		if values.Has("signature") || values.Has("timestamp") || values.Has("recvWindow") {
			t.Fatalf("market data request must not be signed: %s", got[0].query)
		}
		calls, _, _ := provider.snapshot()
		wantKey, wantCalls := "", 0
		if tc.apiKey {
			wantKey, wantCalls = "api-key", 1
		}
		if got[0].header.Get("X-MBX-APIKEY") != wantKey || calls != wantCalls {
			t.Fatalf("X-MBX-APIKEY = %q, secret resolutions = %d, want %q/%d", got[0].header.Get("X-MBX-APIKEY"), calls, wantKey, wantCalls)
		}
		weight, err := limiter.Snapshot(marketWeightKey)
		switch {
		case errors.Is(err, ratelimit.ErrUnknownRule):
			if tc.weight != 0 {
				t.Fatalf("request weight rule is missing, want used %d", tc.weight)
			}
		case err != nil:
			t.Fatalf("Snapshot(request weight) error = %v", err)
		case weight.Used != tc.weight:
			t.Fatalf("request weight used = %d, want %d", weight.Used, tc.weight)
		}
		if tc.bucket == "" {
			return
		}
		bucket, err := limiter.Snapshot("binance-usdm:route:route-a:" + tc.bucket + ":5minutes")
		if err != nil || bucket.Used != 1 || bucket.Rule.Limit != tc.bucketLimit || bucket.Rule.Window != 5*time.Minute {
			t.Fatalf("bucket %s = %+v, error = %v", tc.bucket, bucket, err)
		}
	})
}

func TestMarketDataTrades(t *testing.T) {
	t.Parallel()
	fromID, historyID := int64(26129), int64(28457)
	start, end := time.UnixMilli(marketStartMS), time.UnixMilli(marketEndMS)

	runMarketCase(t, marketCase{name: "aggTrades from id", path: "/fapi/v1/aggTrades", query: "fromId=26129&limit=50&symbol=BTCUSDT", weight: 20, body: `[{"a":26129,"p":"0.01633102","q":"4.70443515","nq":"100","f":27781,"l":27781,"T":1498793709153,"m":true}]`}, func(t *testing.T, client *Client) {
		items, err := client.AggregateTrades(context.Background(), AggregateTradesRequest{Symbol: "BTCUSDT", FromID: &fromID, Limit: 50})
		if err != nil || len(items) != 1 {
			t.Fatalf("AggregateTrades() = %+v, error = %v", items, err)
		}
		item := items[0]
		if item.AggregateID != 26129 || item.Price != "0.01633102" || item.Quantity != "4.70443515" || item.NormalQuantity != "100" || item.FirstTradeID != 27781 || item.LastTradeID != 27781 || item.Time != 1498793709153 || !item.BuyerMaker || len(item.Raw) == 0 {
			t.Fatalf("AggregateTrades()[0] = %+v", item)
		}
	})
	runMarketCase(t, marketCase{name: "aggTrades time window", path: "/fapi/v1/aggTrades", query: "endTime=1700001800000&startTime=1700000000000&symbol=BTCUSDT", weight: 20, body: `[]`}, func(t *testing.T, client *Client) {
		items, err := client.AggregateTrades(context.Background(), AggregateTradesRequest{Symbol: "BTCUSDT", StartTime: &start, EndTime: &end})
		if err != nil || items == nil || len(items) != 0 {
			t.Fatalf("AggregateTrades() = %+v, error = %v, want an empty list", items, err)
		}
	})
	runMarketCase(t, marketCase{name: "historicalTrades", path: "/fapi/v1/historicalTrades", query: "fromId=28457&limit=100&symbol=BTCUSDT", weight: 200, apiKey: true, body: `[{"id":28457,"price":"4.00000100","qty":"12.00000000","quoteQty":"8000.00","time":1499865549590,"isBuyerMaker":true,"isRPITrade":true}]`}, func(t *testing.T, client *Client) {
		trades, err := client.HistoricalTrades(context.Background(), HistoricalTradesRequest{Symbol: "BTCUSDT", FromID: &historyID, Limit: 100})
		if err != nil || len(trades) != 1 {
			t.Fatalf("HistoricalTrades() = %+v, error = %v", trades, err)
		}
		if trades[0].ID != 28457 || trades[0].Price != "4.00000100" || trades[0].Quantity != "12.00000000" || trades[0].QuoteValue != "8000.00" || trades[0].Time != 1499865549590 || !trades[0].BuyerMaker || !trades[0].RPITrade {
			t.Fatalf("HistoricalTrades()[0] = %+v", trades[0])
		}
	})
}

func TestMarketDataCandles(t *testing.T) {
	t.Parallel()
	start, end := time.UnixMilli(marketStartMS), time.UnixMilli(marketEndMS)

	runMarketCase(t, marketCase{name: "continuousKlines", path: "/fapi/v1/continuousKlines", query: "contractType=PERPETUAL&interval=1s&limit=100&pair=BTCUSDT", weight: 2, body: `[[1607444700000,"18879.99","18900.00","18878.98","18896.13","492.363",1607444759999,"9302145.66080",1874,"385.983","7292402.33267","0"]]`}, func(t *testing.T, client *Client) {
		candles, err := client.ContinuousCandles(context.Background(), ContinuousCandlesRequest{Pair: "BTCUSDT", ContractType: ContractTypePerpetual, Interval: Candle1Second, Limit: 100})
		if err != nil || len(candles) != 1 {
			t.Fatalf("ContinuousCandles() = %+v, error = %v", candles, err)
		}
		candle := candles[0]
		if candle.OpenTime != 1607444700000 || candle.Open != "18879.99" || candle.High != "18900.00" || candle.Low != "18878.98" || candle.Close != "18896.13" || candle.Volume != "492.363" || candle.CloseTime != 1607444759999 || candle.QuoteAssetVolume != "9302145.66080" || candle.TradeCount != 1874 {
			t.Fatalf("ContinuousCandles()[0] = %+v", candle)
		}
	})
	runMarketCase(t, marketCase{name: "indexPriceKlines", path: "/fapi/v1/indexPriceKlines", query: "endTime=1700001800000&interval=1h&limit=10&pair=BTCUSDT&startTime=1700000000000", weight: 1, body: `[[1591256400000,"9653.69440000","9653.69640000","9651.38600000","9651.55200000","0",1591256459999,"0",60,"0","0","0"]]`}, func(t *testing.T, client *Client) {
		candles, err := client.IndexPriceCandles(context.Background(), IndexPriceCandlesRequest{Pair: "BTCUSDT", Interval: Candle1Hour, StartTime: &start, EndTime: &end, Limit: 10})
		if err != nil || len(candles) != 1 || candles[0].Close != "9651.55200000" || candles[0].TradeCount != 60 {
			t.Fatalf("IndexPriceCandles() = %+v, error = %v", candles, err)
		}
	})
	// limit을 생략하면 서버 기본값 500으로 계산하므로 weight는 1이 아니라 5다.
	runMarketCase(t, marketCase{name: "markPriceKlines default limit", path: "/fapi/v1/markPriceKlines", query: "interval=5m&symbol=BTCUSDT", weight: 5, body: `[[1591256460000,"9653.29201333","9654.56401333","9653.07951000","9653.64749667","0",1591256519999,"0",60,"0","0","0"]]`}, func(t *testing.T, client *Client) {
		candles, err := client.MarkPriceCandles(context.Background(), MarkPriceCandlesRequest{Symbol: "BTCUSDT", Interval: Candle5Minutes})
		if err != nil || len(candles) != 1 || candles[0].Open != "9653.29201333" || candles[0].CloseTime != 1591256519999 {
			t.Fatalf("MarkPriceCandles() = %+v, error = %v", candles, err)
		}
	})
	runMarketCase(t, marketCase{name: "premiumIndexKlines", path: "/fapi/v1/premiumIndexKlines", query: "interval=1m&limit=1500&symbol=BTCUSDT", weight: 10, body: `[[1691603820000,"-0.00042931","-0.00023641","-0.00059406","-0.00043659","0",1691603879999,"0",12,"0","0","0"]]`}, func(t *testing.T, client *Client) {
		candles, err := client.PremiumIndexCandles(context.Background(), PremiumIndexCandlesRequest{Symbol: "BTCUSDT", Interval: Candle1Minute, Limit: 1500})
		if err != nil || len(candles) != 1 || candles[0].Low != "-0.00059406" || candles[0].TradeCount != 12 {
			t.Fatalf("PremiumIndexCandles() = %+v, error = %v", candles, err)
		}
	})
}

func TestMarketDataPricesAndFunding(t *testing.T) {
	t.Parallel()
	start, end := time.UnixMilli(marketStartMS), time.UnixMilli(marketEndMS)
	btcBody := `{"symbol":"BTCUSDT","markPrice":"11793.63104562","indexPrice":"11781.80495970","estimatedSettlePrice":"11781.16138815","lastFundingRate":"0.00038246","interestRate":"0.00010000","nextFundingTime":1597392000000,"time":1597370495002}`
	ethBody := `{"symbol":"ETHUSDT","markPrice":"1800.10","indexPrice":"1800.00","estimatedSettlePrice":"1800.05","lastFundingRate":"-0.00010000","interestRate":"0.00010000","nextFundingTime":1597392000000,"time":1597370495002}`

	runMarketCase(t, marketCase{name: "premiumIndex single object", path: "/fapi/v1/premiumIndex", query: "symbol=BTCUSDT", weight: 1, body: btcBody}, func(t *testing.T, client *Client) {
		prices, err := client.MarkPrices(context.Background(), MarkPricesRequest{Symbol: "BTCUSDT"})
		if err != nil || len(prices) != 1 {
			t.Fatalf("MarkPrices() = %+v, error = %v", prices, err)
		}
		price := prices[0]
		if price.Symbol != "BTCUSDT" || price.MarkPrice != "11793.63104562" || price.IndexPrice != "11781.80495970" || price.EstimatedSettlePrice != "11781.16138815" || price.LastFundingRate != "0.00038246" || price.InterestRate != "0.00010000" || price.NextFundingTime != 1597392000000 || price.Time != 1597370495002 || string(price.Raw) != btcBody {
			t.Fatalf("MarkPrices()[0] = %+v", price)
		}
	})
	runMarketCase(t, marketCase{name: "premiumIndex all symbols array", path: "/fapi/v1/premiumIndex", query: "", weight: 10, body: `[` + btcBody + `,` + ethBody + `]`}, func(t *testing.T, client *Client) {
		prices, err := client.MarkPrices(context.Background(), MarkPricesRequest{})
		if err != nil || len(prices) != 2 {
			t.Fatalf("MarkPrices() = %+v, error = %v", prices, err)
		}
		if prices[0].Symbol != "BTCUSDT" || string(prices[0].Raw) != btcBody || prices[1].Symbol != "ETHUSDT" || prices[1].LastFundingRate != "-0.00010000" || string(prices[1].Raw) != ethBody {
			t.Fatalf("MarkPrices() = %+v", prices)
		}
	})
	runMarketCase(t, marketCase{name: "fundingRate", path: "/fapi/v1/fundingRate", query: "endTime=1700001800000&limit=50&startTime=1700000000000&symbol=BTCUSDT", weight: 0, bucket: "funding", bucketLimit: 500, body: `[{"symbol":"BTCUSDT","fundingRate":"-0.03750000","fundingTime":1570608000000,"markPrice":"34287.54619963","rateType":"Regular"}]`}, func(t *testing.T, client *Client) {
		rates, err := client.FundingRateHistory(context.Background(), FundingRateHistoryRequest{Symbol: "BTCUSDT", StartTime: &start, EndTime: &end, Limit: 50})
		if err != nil || len(rates) != 1 {
			t.Fatalf("FundingRateHistory() = %+v, error = %v", rates, err)
		}
		if rates[0].Symbol != "BTCUSDT" || rates[0].FundingRate != "-0.03750000" || rates[0].FundingTime != 1570608000000 || rates[0].MarkPrice != "34287.54619963" || rates[0].RateType != "Regular" || len(rates[0].Raw) == 0 {
			t.Fatalf("FundingRateHistory()[0] = %+v", rates[0])
		}
	})
	runMarketCase(t, marketCase{name: "fundingInfo", path: "/fapi/v1/fundingInfo", query: "", weight: 0, bucket: "funding", bucketLimit: 500, body: `[{"symbol":"BLZUSDT","adjustedFundingRateCap":"0.02500000","adjustedFundingRateFloor":"-0.02500000","fundingIntervalHours":8,"disclaimer":false}]`}, func(t *testing.T, client *Client) {
		infos, err := client.FundingInfo(context.Background())
		if err != nil || len(infos) != 1 {
			t.Fatalf("FundingInfo() = %+v, error = %v", infos, err)
		}
		if infos[0].Symbol != "BLZUSDT" || infos[0].AdjustedFundingRateCap != "0.02500000" || infos[0].AdjustedFundingRateFloor != "-0.02500000" || infos[0].FundingIntervalHours != 8 || len(infos[0].Raw) == 0 {
			t.Fatalf("FundingInfo()[0] = %+v", infos[0])
		}
	})
}

func TestMarketDataTickers(t *testing.T) {
	t.Parallel()
	tickerBody := `{"symbol":"BTCUSDT","priceChange":"-94.99999800","priceChangePercent":"-95.960","weightedAvgPrice":"0.29628482","lastPrice":"4.00000200","lastQty":"200.00000000","openPrice":"99.00000000","highPrice":"100.00000000","lowPrice":"0.10000000","volume":"8913.30000000","quoteVolume":"15.30000000","openTime":1499783499040,"closeTime":1499869899040,"firstId":28385,"lastId":28460,"count":76}`
	bookBody := `{"symbol":"BTCUSDT","bidPrice":"4.00000000","bidQty":"431.00000000","askPrice":"4.00000200","askQty":"9.00000000","time":1589437530011}`

	// 사용 weight 헤더는 limiter에 반영된다. 로컬 차감 1보다 큰 30이 사용량이 된다.
	runMarketCase(t, marketCase{name: "ticker 24hr single", path: "/fapi/v1/ticker/24hr", query: "symbol=BTCUSDT", weight: 30, headers: map[string]string{"X-MBX-USED-WEIGHT-1M": "30"}, body: tickerBody}, func(t *testing.T, client *Client) {
		tickers, err := client.Ticker24hr(context.Background(), Ticker24hrRequest{Symbol: "BTCUSDT"})
		if err != nil || len(tickers) != 1 {
			t.Fatalf("Ticker24hr() = %+v, error = %v", tickers, err)
		}
		ticker := tickers[0]
		if ticker.Symbol != "BTCUSDT" || ticker.PriceChange != "-94.99999800" || ticker.PriceChangePercent != "-95.960" || ticker.WeightedAvgPrice != "0.29628482" || ticker.LastPrice != "4.00000200" || ticker.LastQuantity != "200.00000000" || ticker.OpenPrice != "99.00000000" || ticker.HighPrice != "100.00000000" || ticker.LowPrice != "0.10000000" || ticker.Volume != "8913.30000000" || ticker.QuoteVolume != "15.30000000" || ticker.OpenTime != 1499783499040 || ticker.CloseTime != 1499869899040 || ticker.FirstTradeID != 28385 || ticker.LastTradeID != 28460 || ticker.TradeCount != 76 || len(ticker.Raw) == 0 {
			t.Fatalf("Ticker24hr()[0] = %+v", ticker)
		}
	})
	runMarketCase(t, marketCase{name: "ticker 24hr all symbols", path: "/fapi/v1/ticker/24hr", query: "", weight: 40, body: `[` + tickerBody + `,` + tickerBody + `]`}, func(t *testing.T, client *Client) {
		tickers, err := client.Ticker24hr(context.Background(), Ticker24hrRequest{})
		if err != nil || len(tickers) != 2 || tickers[1].LastPrice != "4.00000200" {
			t.Fatalf("Ticker24hr() = %+v, error = %v", tickers, err)
		}
	})
	// bookTicker의 사용 weight 헤더는 공식 문서가 부정확하다고 밝히므로 900이 아니라 로컬 차감 2가 남아야 한다.
	runMarketCase(t, marketCase{name: "bookTicker single ignores used weight header", path: "/fapi/v1/ticker/bookTicker", query: "symbol=BTCUSDT", weight: 2, headers: map[string]string{"X-MBX-USED-WEIGHT-1M": "900"}, body: bookBody}, func(t *testing.T, client *Client) {
		tickers, err := client.BookTickers(context.Background(), BookTickersRequest{Symbol: "BTCUSDT"})
		if err != nil || len(tickers) != 1 {
			t.Fatalf("BookTickers() = %+v, error = %v", tickers, err)
		}
		if tickers[0].Symbol != "BTCUSDT" || tickers[0].BidPrice != "4.00000000" || tickers[0].BidQuantity != "431.00000000" || tickers[0].AskPrice != "4.00000200" || tickers[0].AskQuantity != "9.00000000" || tickers[0].Time != 1589437530011 || len(tickers[0].Raw) == 0 {
			t.Fatalf("BookTickers()[0] = %+v", tickers[0])
		}
	})
	runMarketCase(t, marketCase{name: "bookTicker all symbols", path: "/fapi/v1/ticker/bookTicker", query: "", weight: 5, body: `[` + bookBody + `]`}, func(t *testing.T, client *Client) {
		tickers, err := client.BookTickers(context.Background(), BookTickersRequest{})
		if err != nil || len(tickers) != 1 {
			t.Fatalf("BookTickers() = %+v, error = %v", tickers, err)
		}
	})
	runMarketCase(t, marketCase{name: "openInterest", path: "/fapi/v1/openInterest", query: "symbol=BTCUSDT", weight: 1, body: `{"openInterest":"10659.509","symbol":"BTCUSDT","time":1589437530011}`}, func(t *testing.T, client *Client) {
		interest, err := client.OpenInterest(context.Background(), OpenInterestRequest{Symbol: "BTCUSDT"})
		if err != nil || interest.OpenInterest != "10659.509" || interest.Symbol != "BTCUSDT" || interest.Time != 1589437530011 || len(interest.Raw) == 0 {
			t.Fatalf("OpenInterest() = %+v, error = %v", interest, err)
		}
	})
	runMarketCase(t, marketCase{name: "rpiDepth", path: "/fapi/v1/rpiDepth", query: "limit=1000&symbol=BTCUSDT", weight: 20, body: `{"lastUpdateId":1027024,"E":1589436922972,"T":1589436922959,"bids":[["4.00000000","431.00000000"]],"asks":[["4.00000200","12.00000000"]]}`}, func(t *testing.T, client *Client) {
		book, err := client.RPIOrderBook(context.Background(), RPIOrderBookRequest{Symbol: "BTCUSDT", Limit: 1000})
		if err != nil || book.LastUpdateID != 1027024 || book.MessageTime != 1589436922972 || book.TransactionTime != 1589436922959 || len(book.Bids) != 1 || book.Bids[0].Price != "4.00000000" || book.Bids[0].Quantity != "431.00000000" || len(book.Asks) != 1 || book.Asks[0].Price != "4.00000200" || book.Asks[0].Quantity != "12.00000000" || len(book.Raw) == 0 {
			t.Fatalf("RPIOrderBook() = %+v, error = %v", book, err)
		}
	})
}

func TestMarketDataStatistics(t *testing.T) {
	t.Parallel()
	start, end := time.UnixMilli(marketStartMS), time.UnixMilli(marketEndMS)
	ratioBody := `[{"symbol":"BTCUSDT","longShortRatio":"1.8105","longAccount":"0.6442","shortAccount":"0.3558","timestamp":1591261042378}]`
	assertRatio := func(t *testing.T, ratios []LongShortRatio, err error) {
		t.Helper()
		if err != nil || len(ratios) != 1 {
			t.Fatalf("ratio = %+v, error = %v", ratios, err)
		}
		if ratios[0].Symbol != "BTCUSDT" || ratios[0].LongShortRatio != "1.8105" || ratios[0].LongAccount != "0.6442" || ratios[0].ShortAccount != "0.3558" || ratios[0].Timestamp != 1591261042378 || len(ratios[0].Raw) == 0 {
			t.Fatalf("ratio[0] = %+v", ratios[0])
		}
	}

	runMarketCase(t, marketCase{name: "openInterestHist", path: "/futures/data/openInterestHist", query: "limit=30&period=5m&symbol=BTCUSDT", weight: 0, bucket: "futures_data", bucketLimit: 1000, body: `[{"symbol":"BTCUSDT","sumOpenInterest":"20403.12345678","sumOpenInterestValue":"176196512.12345678","CMCCirculatingSupply":"165880.538","timestamp":1591261042378}]`}, func(t *testing.T, client *Client) {
		stats, err := client.OpenInterestHistory(context.Background(), MarketStatisticsRequest{Symbol: "BTCUSDT", Period: StatisticsPeriod5Minutes, Limit: 30})
		if err != nil || len(stats) != 1 {
			t.Fatalf("OpenInterestHistory() = %+v, error = %v", stats, err)
		}
		if stats[0].Symbol != "BTCUSDT" || stats[0].SumOpenInterest != "20403.12345678" || stats[0].SumOpenInterestValue != "176196512.12345678" || stats[0].CirculatingSupply != "165880.538" || stats[0].Timestamp != 1591261042378 || len(stats[0].Raw) == 0 {
			t.Fatalf("OpenInterestHistory()[0] = %+v", stats[0])
		}
	})
	runMarketCase(t, marketCase{name: "topLongShortAccountRatio", path: "/futures/data/topLongShortAccountRatio", query: "period=1h&symbol=BTCUSDT", weight: 0, bucket: "futures_data", bucketLimit: 1000, apiKey: true, body: ratioBody}, func(t *testing.T, client *Client) {
		ratios, err := client.TopTraderAccountRatio(context.Background(), MarketStatisticsRequest{Symbol: "BTCUSDT", Period: StatisticsPeriod1Hour})
		assertRatio(t, ratios, err)
	})
	runMarketCase(t, marketCase{name: "topLongShortPositionRatio", path: "/futures/data/topLongShortPositionRatio", query: "period=1d&symbol=BTCUSDT", weight: 0, bucket: "futures_data", bucketLimit: 1000, apiKey: true, body: ratioBody}, func(t *testing.T, client *Client) {
		ratios, err := client.TopTraderPositionRatio(context.Background(), MarketStatisticsRequest{Symbol: "BTCUSDT", Period: StatisticsPeriod1Day})
		assertRatio(t, ratios, err)
	})
	runMarketCase(t, marketCase{name: "globalLongShortAccountRatio", path: "/futures/data/globalLongShortAccountRatio", query: "endTime=1700001800000&period=15m&startTime=1700000000000&symbol=BTCUSDT", weight: 0, bucket: "futures_data", bucketLimit: 1000, body: ratioBody}, func(t *testing.T, client *Client) {
		ratios, err := client.GlobalLongShortAccountRatio(context.Background(), MarketStatisticsRequest{Symbol: "BTCUSDT", Period: StatisticsPeriod15Minutes, StartTime: &start, EndTime: &end})
		assertRatio(t, ratios, err)
	})
	runMarketCase(t, marketCase{name: "takerlongshortRatio", path: "/futures/data/takerlongshortRatio", query: "limit=500&period=4h&symbol=BTCUSDT", weight: 0, bucket: "futures_data", bucketLimit: 1000, body: `[{"buySellRatio":"1.5586","buyVol":"387.3300","sellVol":"248.5030","timestamp":1591261042378}]`}, func(t *testing.T, client *Client) {
		volumes, err := client.TakerBuySellVolumes(context.Background(), MarketStatisticsRequest{Symbol: "BTCUSDT", Period: StatisticsPeriod4Hours, Limit: 500})
		if err != nil || len(volumes) != 1 {
			t.Fatalf("TakerBuySellVolumes() = %+v, error = %v", volumes, err)
		}
		if volumes[0].BuySellRatio != "1.5586" || volumes[0].BuyVolume != "387.3300" || volumes[0].SellVolume != "248.5030" || volumes[0].Timestamp != 1591261042378 || len(volumes[0].Raw) == 0 {
			t.Fatalf("TakerBuySellVolumes()[0] = %+v", volumes[0])
		}
	})
	// basis와 delivery-price는 공식 문서에 weight 0만 적혀 있어 5분 제한을 차감하지 않는다.
	runMarketCase(t, marketCase{name: "basis", path: "/futures/data/basis", query: "contractType=PERPETUAL&limit=30&pair=BTCUSDT&period=5m", weight: 0, body: `[{"indexPrice":"34400.15945055","contractType":"PERPETUAL","basisRate":"0.0004","futuresPrice":"34414.10","annualizedBasisRate":"","basis":"13.94054945","pair":"BTCUSDT","timestamp":1698742800000}]`}, func(t *testing.T, client *Client) {
		basis, err := client.Basis(context.Background(), BasisRequest{Pair: "BTCUSDT", ContractType: ContractTypePerpetual, Period: StatisticsPeriod5Minutes, Limit: 30})
		if err != nil || len(basis) != 1 {
			t.Fatalf("Basis() = %+v, error = %v", basis, err)
		}
		if basis[0].IndexPrice != "34400.15945055" || basis[0].ContractType != "PERPETUAL" || basis[0].BasisRate != "0.0004" || basis[0].FuturesPrice != "34414.10" || basis[0].AnnualizedBasisRate != "" || basis[0].Basis != "13.94054945" || basis[0].Pair != "BTCUSDT" || basis[0].Timestamp != 1698742800000 || len(basis[0].Raw) == 0 {
			t.Fatalf("Basis()[0] = %+v", basis[0])
		}
	})
	runMarketCase(t, marketCase{name: "delivery-price", path: "/futures/data/delivery-price", query: "pair=BTCUSDT", weight: 0, body: `[{"deliveryTime":1695945600000,"deliveryPrice":27103}]`}, func(t *testing.T, client *Client) {
		prices, err := client.DeliveryPrices(context.Background(), DeliveryPricesRequest{Pair: "BTCUSDT"})
		if err != nil || len(prices) != 1 {
			t.Fatalf("DeliveryPrices() = %+v, error = %v", prices, err)
		}
		if prices[0].DeliveryTime != 1695945600000 || prices[0].DeliveryPrice.String() != "27103" || len(prices[0].Raw) == 0 {
			t.Fatalf("DeliveryPrices()[0] = %+v", prices[0])
		}
	})
}

func TestMarketDataIndexAndRisk(t *testing.T) {
	t.Parallel()
	assetBody := `{"symbol":"ADAUSD","time":1635740268004,"index":"1.92957370","bidBuffer":"0.10000000","askBuffer":"0.10000000","bidRate":"1.73661633","askRate":"2.12253107","autoExchangeBidBuffer":"0.05000000","autoExchangeAskBuffer":"0.05000000","autoExchangeBidRate":"1.83309501","autoExchangeAskRate":"2.02605238"}`
	insuranceBody := `{"symbols":["BNBUSDT","BTCUSDT"],"assets":[{"asset":"USDT","marginBalance":"1000000.12345678","updateTime":1745401553408}]}`
	adlBody := `{"symbol":"BTCUSDT","adlRisk":"low","updateTime":1597370495002}`

	runMarketCase(t, marketCase{name: "indexInfo", path: "/fapi/v1/indexInfo", query: "symbol=DEFIUSDT", weight: 1, body: `[{"symbol":"DEFIUSDT","time":1589437530011,"component":"baseAsset","baseAssetList":[{"baseAsset":"BAL","quoteAsset":"USDT","weightInQuantity":"1.04406228","weightInPercentage":"0.02783900"}]}]`}, func(t *testing.T, client *Client) {
		indices, err := client.CompositeIndexInfo(context.Background(), CompositeIndexRequest{Symbol: "DEFIUSDT"})
		if err != nil || len(indices) != 1 || len(indices[0].BaseAssetList) != 1 {
			t.Fatalf("CompositeIndexInfo() = %+v, error = %v", indices, err)
		}
		asset := indices[0].BaseAssetList[0]
		if indices[0].Symbol != "DEFIUSDT" || indices[0].Component != "baseAsset" || indices[0].Time != 1589437530011 || asset.BaseAsset != "BAL" || asset.QuoteAsset != "USDT" || asset.WeightInQuantity != "1.04406228" || asset.WeightInPercentage != "0.02783900" || len(indices[0].Raw) == 0 {
			t.Fatalf("CompositeIndexInfo()[0] = %+v", indices[0])
		}
	})
	runMarketCase(t, marketCase{name: "assetIndex single object", path: "/fapi/v1/assetIndex", query: "symbol=ADAUSD", weight: 1, body: assetBody}, func(t *testing.T, client *Client) {
		indices, err := client.AssetIndices(context.Background(), AssetIndexRequest{Symbol: "ADAUSD"})
		if err != nil || len(indices) != 1 {
			t.Fatalf("AssetIndices() = %+v, error = %v", indices, err)
		}
		if indices[0].Symbol != "ADAUSD" || indices[0].Time != 1635740268004 || indices[0].Index != "1.92957370" || indices[0].BidBuffer != "0.10000000" || indices[0].AskBuffer != "0.10000000" || indices[0].BidRate != "1.73661633" || indices[0].AskRate != "2.12253107" || indices[0].AutoExchangeBidBuffer != "0.05000000" || indices[0].AutoExchangeAskBuffer != "0.05000000" || indices[0].AutoExchangeBidRate != "1.83309501" || indices[0].AutoExchangeAskRate != "2.02605238" || len(indices[0].Raw) == 0 {
			t.Fatalf("AssetIndices()[0] = %+v", indices[0])
		}
	})
	runMarketCase(t, marketCase{name: "assetIndex all array", path: "/fapi/v1/assetIndex", query: "", weight: 10, body: `[` + assetBody + `,` + assetBody + `]`}, func(t *testing.T, client *Client) {
		indices, err := client.AssetIndices(context.Background(), AssetIndexRequest{})
		if err != nil || len(indices) != 2 {
			t.Fatalf("AssetIndices() = %+v, error = %v", indices, err)
		}
	})
	runMarketCase(t, marketCase{name: "constituents", path: "/fapi/v1/constituents", query: "symbol=BTCUSDT", weight: 2, body: `{"symbol":"BTCUSDT","time":1745401553408,"constituents":[{"exchange":"binance","symbol":"BTCUSDT","price":"94057.03000000","weight":"0.51282051"}]}`}, func(t *testing.T, client *Client) {
		result, err := client.IndexConstituents(context.Background(), IndexConstituentsRequest{Symbol: "BTCUSDT"})
		if err != nil || result.Symbol != "BTCUSDT" || result.Time != 1745401553408 || len(result.Constituents) != 1 || len(result.Raw) == 0 {
			t.Fatalf("IndexConstituents() = %+v, error = %v", result, err)
		}
		if constituent := result.Constituents[0]; constituent.Exchange != "binance" || constituent.Symbol != "BTCUSDT" || constituent.Price != "94057.03000000" || constituent.Weight != "0.51282051" {
			t.Fatalf("IndexConstituents().Constituents[0] = %+v", constituent)
		}
	})
	runMarketCase(t, marketCase{name: "insuranceBalance single object", path: "/fapi/v1/insuranceBalance", query: "symbol=BNBUSDT", weight: 1, body: insuranceBody}, func(t *testing.T, client *Client) {
		funds, err := client.InsuranceFundBalance(context.Background(), InsuranceFundRequest{Symbol: "BNBUSDT"})
		if err != nil || len(funds) != 1 || len(funds[0].Symbols) != 2 || funds[0].Symbols[0] != "BNBUSDT" || len(funds[0].Assets) != 1 || len(funds[0].Raw) == 0 {
			t.Fatalf("InsuranceFundBalance() = %+v, error = %v", funds, err)
		}
		if asset := funds[0].Assets[0]; asset.Asset != "USDT" || asset.MarginBalance != "1000000.12345678" || asset.UpdateTime != 1745401553408 {
			t.Fatalf("InsuranceFundBalance().Assets[0] = %+v", asset)
		}
	})
	runMarketCase(t, marketCase{name: "insuranceBalance all array", path: "/fapi/v1/insuranceBalance", query: "", weight: 1, body: `[` + insuranceBody + `,` + insuranceBody + `]`}, func(t *testing.T, client *Client) {
		funds, err := client.InsuranceFundBalance(context.Background(), InsuranceFundRequest{})
		if err != nil || len(funds) != 2 {
			t.Fatalf("InsuranceFundBalance() = %+v, error = %v", funds, err)
		}
	})
	runMarketCase(t, marketCase{name: "symbolAdlRisk single object", path: "/fapi/v1/symbolAdlRisk", query: "symbol=BTCUSDT", weight: 1, body: adlBody}, func(t *testing.T, client *Client) {
		risks, err := client.SymbolAdlRisks(context.Background(), SymbolAdlRiskRequest{Symbol: "BTCUSDT"})
		if err != nil || len(risks) != 1 || risks[0].Symbol != "BTCUSDT" || risks[0].AdlRisk != "low" || risks[0].UpdateTime != 1597370495002 || len(risks[0].Raw) == 0 {
			t.Fatalf("SymbolAdlRisks() = %+v, error = %v", risks, err)
		}
	})
	runMarketCase(t, marketCase{name: "symbolAdlRisk all array", path: "/fapi/v1/symbolAdlRisk", query: "", weight: 1, body: `[` + adlBody + `]`}, func(t *testing.T, client *Client) {
		risks, err := client.SymbolAdlRisks(context.Background(), SymbolAdlRiskRequest{})
		if err != nil || len(risks) != 1 {
			t.Fatalf("SymbolAdlRisks() = %+v, error = %v", risks, err)
		}
	})
	runMarketCase(t, marketCase{name: "tradingSchedule", path: "/fapi/v1/tradingSchedule", query: "", weight: 5, body: `{"updateTime":1761286643918,"marketSchedules":{"EQUITY":{"sessions":[{"startTime":1761177600000,"endTime":1761206400000,"type":"OVERNIGHT"}]},"FX":{"sessions":[{"startTime":1779966000000,"endTime":1780027200000,"type":"NO_TRADING"}]}}}`}, func(t *testing.T, client *Client) {
		schedule, err := client.TradingSchedule(context.Background())
		if err != nil || schedule.UpdateTime != 1761286643918 || len(schedule.MarketSchedules) != 2 || len(schedule.Raw) == 0 {
			t.Fatalf("TradingSchedule() = %+v, error = %v", schedule, err)
		}
		equity := schedule.MarketSchedules["EQUITY"].Sessions
		if len(equity) != 1 || equity[0].StartTime != 1761177600000 || equity[0].EndTime != 1761206400000 || equity[0].Type != "OVERNIGHT" {
			t.Fatalf("TradingSchedule().EQUITY = %+v", equity)
		}
		if fx := schedule.MarketSchedules["FX"].Sessions; len(fx) != 1 || fx[0].Type != "NO_TRADING" {
			t.Fatalf("TradingSchedule().FX = %+v", fx)
		}
	})
}

func TestMarketDataSharedRateLimitBuckets(t *testing.T) {
	t.Parallel()
	server, _ := newMarketServer(t, `[]`, nil)
	sender := &directSender{}
	client, limiter := newTestClient(t, server.URL, sender, &recordingProvider{}, time.UnixMilli(marketStartMS), []transport.EgressRouteID{"route-a", "route-b"})
	ctx := context.Background()
	statistics := MarketStatisticsRequest{Symbol: "BTCUSDT", Period: StatisticsPeriod15Minutes}

	if _, err := client.FundingRateHistory(ctx, FundingRateHistoryRequest{}); err != nil {
		t.Fatalf("FundingRateHistory() error = %v", err)
	}
	if _, err := client.FundingInfo(ctx); err != nil {
		t.Fatalf("FundingInfo() error = %v", err)
	}
	if _, err := client.OpenInterestHistory(ctx, statistics); err != nil {
		t.Fatalf("OpenInterestHistory() error = %v", err)
	}
	if _, err := client.TopTraderAccountRatio(ctx, statistics); err != nil {
		t.Fatalf("TopTraderAccountRatio() error = %v", err)
	}
	if _, err := client.TopTraderPositionRatio(ctx, statistics); err != nil {
		t.Fatalf("TopTraderPositionRatio() error = %v", err)
	}
	if _, err := client.GlobalLongShortAccountRatio(ctx, statistics); err != nil {
		t.Fatalf("GlobalLongShortAccountRatio() error = %v", err)
	}
	if _, err := client.TakerBuySellVolumes(ctx, statistics); err != nil {
		t.Fatalf("TakerBuySellVolumes() error = %v", err)
	}
	if _, err := client.FundingInfo(ctx, trade.WithEgressRoute("route-b")); err != nil {
		t.Fatalf("FundingInfo(route-b) error = %v", err)
	}
	// fundingRate와 fundingInfo는 한 제한을, /futures/data 통계 다섯 건은 다른 한 제한을 route별로 공유한다.
	for _, want := range []bucketUsage{
		{"binance-usdm:route:route-a:funding:5minutes", 2},
		{"binance-usdm:route:route-a:futures_data:5minutes", 5},
		{"binance-usdm:route:route-b:funding:5minutes", 1},
	} {
		snapshot, err := limiter.Snapshot(want.key)
		if err != nil || snapshot.Used != want.used {
			t.Fatalf("Snapshot(%s) = %+v, error = %v, want used %d", want.key, snapshot, err, want.used)
		}
	}
	if _, err := limiter.Snapshot("binance-usdm:route:route-b:futures_data:5minutes"); !errors.Is(err, ratelimit.ErrUnknownRule) {
		t.Fatalf("Snapshot(route-b futures_data) error = %v, want unknown rule", err)
	}
	if routes := sender.snapshot(); len(routes) != 8 || routes[0] != "route-a" || routes[7] != "route-b" {
		t.Fatalf("routes = %v", routes)
	}
}

func TestMarketDataAPIKeyEndpointsRequireCredentials(t *testing.T) {
	t.Parallel()
	server, requests := newMarketServer(t, `[]`, nil)
	client := newPublicTestClient(t, server.URL, &directSender{})
	ctx := context.Background()
	statistics := MarketStatisticsRequest{Symbol: "BTCUSDT", Period: StatisticsPeriod5Minutes}

	if _, err := client.AggregateTrades(ctx, AggregateTradesRequest{Symbol: "BTCUSDT"}); err != nil {
		t.Fatalf("public AggregateTrades() error = %v", err)
	}
	if _, err := client.GlobalLongShortAccountRatio(ctx, statistics); err != nil {
		t.Fatalf("public GlobalLongShortAccountRatio() error = %v", err)
	}
	if _, err := client.HistoricalTrades(ctx, HistoricalTradesRequest{Symbol: "BTCUSDT"}); !errors.Is(err, trade.ErrAuthentication) {
		t.Fatalf("HistoricalTrades() error = %v, want ErrAuthentication", err)
	}
	if _, err := client.TopTraderAccountRatio(ctx, statistics); !errors.Is(err, trade.ErrAuthentication) {
		t.Fatalf("TopTraderAccountRatio() error = %v, want ErrAuthentication", err)
	}
	if _, err := client.TopTraderPositionRatio(ctx, statistics); !errors.Is(err, trade.ErrAuthentication) {
		t.Fatalf("TopTraderPositionRatio() error = %v, want ErrAuthentication", err)
	}
	if got := requests(); len(got) != 2 || got[0].path != "/fapi/v1/aggTrades" || got[1].path != "/futures/data/globalLongShortAccountRatio" {
		t.Fatalf("requests = %+v, want only the two public requests", got)
	}
}

func TestMarketDataAPIKeyRequiresReadPermission(t *testing.T) {
	t.Parallel()
	server, requests := newMarketServer(t, `[]`, nil)
	limiter, _ := ratelimit.New()
	executor, err := commonexchange.NewExecutor(commonexchange.ExecutorConfig{Sender: &directSender{}, Limiter: limiter})
	if err != nil {
		t.Fatal(err)
	}
	provider := &recordingProvider{}
	descriptor := &credential.Descriptor{AccountID: "futures-trade", Exchange: model.ExchangeBinance, SecretRef: "secret/trade", Permissions: []credential.Permission{credential.PermissionTrade}, AllowedEgressRouteIDs: []transport.EgressRouteID{"route-a"}}
	client, err := New(Config{Executor: executor, Credentials: descriptor, CredentialProvider: provider, DefaultEgressRouteID: "route-a", BaseURL: server.URL, AllowInsecureHTTP: true})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	statistics := MarketStatisticsRequest{Symbol: "BTCUSDT", Period: StatisticsPeriod5Minutes}

	if _, err := client.HistoricalTrades(ctx, HistoricalTradesRequest{Symbol: "BTCUSDT"}); !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("HistoricalTrades() error = %v, want ErrAuthorization", err)
	}
	if _, err := client.TopTraderPositionRatio(ctx, statistics); !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("TopTraderPositionRatio() error = %v, want ErrAuthorization", err)
	}
	// 공개 엔드포인트는 권한을 요구하지 않으므로 read 권한이 없는 자격증명으로도 호출된다.
	if _, err := client.TakerBuySellVolumes(ctx, statistics); err != nil {
		t.Fatalf("public TakerBuySellVolumes() error = %v", err)
	}
	calls, _, _ := provider.snapshot()
	if got := requests(); calls != 0 || len(got) != 1 || got[0].header.Get("X-MBX-APIKEY") != "" {
		t.Fatalf("secret resolutions = %d, requests = %+v", calls, got)
	}
}

func TestMarketDataAPIKeyRouteRejectedBeforeSecretResolution(t *testing.T) {
	t.Parallel()
	sender := &directSender{}
	provider := &recordingProvider{}
	client, _ := newTestClient(t, "http://127.0.0.1", sender, provider, time.Now(), []transport.EgressRouteID{"route-a"})
	_, err := client.HistoricalTrades(context.Background(), HistoricalTradesRequest{Symbol: "BTCUSDT"}, trade.WithEgressRoute("route-b"))
	if !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("HistoricalTrades() error = %v, want ErrAuthorization", err)
	}
	calls, _, _ := provider.snapshot()
	if calls != 0 || len(sender.snapshot()) != 0 {
		t.Fatalf("calls = %d, routes = %v", calls, sender.snapshot())
	}
}

func TestMarketDataExchangeErrorIsClassified(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(writer, `{"code":-1121,"msg":"Invalid symbol."}`)
	}))
	defer server.Close()
	client, _ := newTestClient(t, server.URL, &directSender{}, &recordingProvider{}, time.UnixMilli(marketStartMS), []transport.EgressRouteID{"route-a"})

	_, err := client.AggregateTrades(context.Background(), AggregateTradesRequest{Symbol: "NOPE"})
	var apiError *trade.APIError
	if !errors.Is(err, trade.ErrValidation) || !errors.As(err, &apiError) || apiError.ExchangeCode != "-1121" || apiError.ExchangeMessage != "Invalid symbol." || apiError.HTTPStatus != http.StatusBadRequest {
		t.Fatalf("AggregateTrades() error = %v, want validation APIError -1121", err)
	}
}

func TestMarketDataMalformedResponse(t *testing.T) {
	t.Parallel()
	for _, tc := range []malformedCase{{"not json", `not-json`}, {"non object item", `[1]`}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server, _ := newMarketServer(t, tc.body, nil)
			client, _ := newTestClient(t, server.URL, &directSender{}, &recordingProvider{}, time.UnixMilli(marketStartMS), []transport.EgressRouteID{"route-a"})
			prices, err := client.MarkPrices(context.Background(), MarkPricesRequest{Symbol: "BTCUSDT"})
			if !errors.Is(err, trade.ErrExchangeUnavailable) || prices != nil {
				t.Fatalf("MarkPrices() = %+v, error = %v, want ErrExchangeUnavailable", prices, err)
			}
		})
	}
}

func TestMarketDataRequestValidation(t *testing.T) {
	t.Parallel()

	one, negative := int64(1), int64(-1)
	begin := time.UnixMilli(1_000_000)
	earlier := begin.Add(-time.Minute)
	later := begin.Add(time.Minute)
	halfHour := begin.Add(30 * time.Minute)
	hourLater := begin.Add(time.Hour)

	for _, tc := range []validationCase{
		{"aggTrades no symbol", AggregateTradesRequest{}.validate()},
		{"aggTrades whitespace symbol", AggregateTradesRequest{Symbol: " BTCUSDT"}.validate()},
		{"aggTrades negative fromId", AggregateTradesRequest{Symbol: "BTCUSDT", FromID: &negative}.validate()},
		{"aggTrades fromId with startTime", AggregateTradesRequest{Symbol: "BTCUSDT", FromID: &one, StartTime: &begin}.validate()},
		{"aggTrades fromId with endTime", AggregateTradesRequest{Symbol: "BTCUSDT", FromID: &one, EndTime: &later}.validate()},
		{"aggTrades limit", AggregateTradesRequest{Symbol: "BTCUSDT", Limit: 1001}.validate()},
		{"aggTrades negative limit", AggregateTradesRequest{Symbol: "BTCUSDT", Limit: -1}.validate()},
		{"aggTrades start after end", AggregateTradesRequest{Symbol: "BTCUSDT", StartTime: &later, EndTime: &begin}.validate()},
		{"aggTrades window one hour", AggregateTradesRequest{Symbol: "BTCUSDT", StartTime: &begin, EndTime: &hourLater}.validate()},
		{"historicalTrades no symbol", HistoricalTradesRequest{}.validate()},
		{"historicalTrades limit", HistoricalTradesRequest{Symbol: "BTCUSDT", Limit: 501}.validate()},
		{"historicalTrades negative fromId", HistoricalTradesRequest{Symbol: "BTCUSDT", FromID: &negative}.validate()},
		{"continuous no pair", ContinuousCandlesRequest{ContractType: ContractTypePerpetual, Interval: Candle1Minute}.validate()},
		{"continuous contract type", ContinuousCandlesRequest{Pair: "BTCUSDT", ContractType: ContractType("SPOT"), Interval: Candle1Minute}.validate()},
		{"continuous no contract type", ContinuousCandlesRequest{Pair: "BTCUSDT", Interval: Candle1Minute}.validate()},
		{"continuous interval", ContinuousCandlesRequest{Pair: "BTCUSDT", ContractType: ContractTypePerpetual, Interval: CandleInterval("7m")}.validate()},
		{"continuous limit", ContinuousCandlesRequest{Pair: "BTCUSDT", ContractType: ContractTypePerpetual, Interval: Candle1Minute, Limit: 1501}.validate()},
		{"index no pair", IndexPriceCandlesRequest{Interval: Candle1Minute}.validate()},
		{"index second interval", IndexPriceCandlesRequest{Pair: "BTCUSDT", Interval: Candle1Second}.validate()},
		{"mark no symbol", MarkPriceCandlesRequest{Interval: Candle1Minute}.validate()},
		{"mark second interval", MarkPriceCandlesRequest{Symbol: "BTCUSDT", Interval: Candle1Second}.validate()},
		{"premium range", PremiumIndexCandlesRequest{Symbol: "BTCUSDT", Interval: Candle1Minute, StartTime: &later, EndTime: &earlier}.validate()},
		{"premium second interval", PremiumIndexCandlesRequest{Symbol: "BTCUSDT", Interval: Candle1Second}.validate()},
		{"markPrices whitespace", MarkPricesRequest{Symbol: "BTCUSDT "}.validate()},
		{"funding limit", FundingRateHistoryRequest{Limit: 1001}.validate()},
		{"funding range", FundingRateHistoryRequest{StartTime: &later, EndTime: &begin}.validate()},
		{"ticker whitespace", Ticker24hrRequest{Symbol: " "}.validate()},
		{"bookTicker whitespace", BookTickersRequest{Symbol: "BTCUSDT\n"}.validate()},
		{"openInterest no symbol", OpenInterestRequest{}.validate()},
		{"statistics no symbol", MarketStatisticsRequest{Period: StatisticsPeriod5Minutes}.validate()},
		{"statistics no period", MarketStatisticsRequest{Symbol: "BTCUSDT"}.validate()},
		{"statistics unsupported period", MarketStatisticsRequest{Symbol: "BTCUSDT", Period: StatisticsPeriod("1w")}.validate()},
		{"statistics limit", MarketStatisticsRequest{Symbol: "BTCUSDT", Period: StatisticsPeriod5Minutes, Limit: 501}.validate()},
		{"statistics range", MarketStatisticsRequest{Symbol: "BTCUSDT", Period: StatisticsPeriod5Minutes, StartTime: &later, EndTime: &begin}.validate()},
		{"basis no pair", BasisRequest{ContractType: ContractTypePerpetual, Period: StatisticsPeriod5Minutes}.validate()},
		{"basis tradfi contract", BasisRequest{Pair: "BTCUSDT", ContractType: ContractTypeTradFiPerpetual, Period: StatisticsPeriod5Minutes}.validate()},
		{"basis no contract type", BasisRequest{Pair: "BTCUSDT", Period: StatisticsPeriod5Minutes}.validate()},
		{"basis no period", BasisRequest{Pair: "BTCUSDT", ContractType: ContractTypePerpetual}.validate()},
		{"basis limit", BasisRequest{Pair: "BTCUSDT", ContractType: ContractTypePerpetual, Period: StatisticsPeriod5Minutes, Limit: 501}.validate()},
		{"deliveryPrice no pair", DeliveryPricesRequest{}.validate()},
		{"indexInfo whitespace", CompositeIndexRequest{Symbol: " DEFIUSDT"}.validate()},
		{"assetIndex whitespace", AssetIndexRequest{Symbol: "ADAUSD "}.validate()},
		{"constituents no symbol", IndexConstituentsRequest{}.validate()},
		{"insurance whitespace", InsuranceFundRequest{Symbol: " BNBUSDT"}.validate()},
		{"adlRisk whitespace", SymbolAdlRiskRequest{Symbol: "BTCUSDT "}.validate()},
		{"rpiDepth no symbol", RPIOrderBookRequest{}.validate()},
		{"rpiDepth limit", RPIOrderBookRequest{Symbol: "BTCUSDT", Limit: 500}.validate()},
	} {
		if !errors.Is(tc.err, trade.ErrValidation) {
			t.Errorf("%s: error = %v, want ErrValidation", tc.name, tc.err)
		}
	}

	for _, tc := range []validationCase{
		{"aggTrades fromId", AggregateTradesRequest{Symbol: "BTCUSDT", FromID: &one, Limit: 1000}.validate()},
		{"aggTrades half hour", AggregateTradesRequest{Symbol: "BTCUSDT", StartTime: &begin, EndTime: &halfHour}.validate()},
		{"historicalTrades max", HistoricalTradesRequest{Symbol: "BTCUSDT", FromID: &one, Limit: 500}.validate()},
		{"continuous 1s tradfi", ContinuousCandlesRequest{Pair: "BTCUSDT", ContractType: ContractTypeTradFiPerpetual, Interval: Candle1Second, Limit: 1500}.validate()},
		{"index monthly", IndexPriceCandlesRequest{Pair: "BTCUSDT", Interval: Candle1Month, StartTime: &begin, EndTime: &later}.validate()},
		{"mark weekly", MarkPriceCandlesRequest{Symbol: "BTCUSDT", Interval: Candle1Week}.validate()},
		{"premium minute", PremiumIndexCandlesRequest{Symbol: "BTCUSDT", Interval: Candle1Minute}.validate()},
		{"markPrices all", MarkPricesRequest{}.validate()},
		{"funding all", FundingRateHistoryRequest{Limit: 1000}.validate()},
		{"ticker all", Ticker24hrRequest{}.validate()},
		{"bookTicker all", BookTickersRequest{}.validate()},
		{"openInterest", OpenInterestRequest{Symbol: "BTCUSDT"}.validate()},
		{"statistics daily", MarketStatisticsRequest{Symbol: "BTCUSDT", Period: StatisticsPeriod1Day, Limit: 500}.validate()},
		{"basis next quarter", BasisRequest{Pair: "BTCUSDT", ContractType: ContractTypeNextQuarter, Period: StatisticsPeriod12Hours}.validate()},
		{"deliveryPrice", DeliveryPricesRequest{Pair: "BTCUSDT"}.validate()},
		{"indexInfo all", CompositeIndexRequest{}.validate()},
		{"assetIndex all", AssetIndexRequest{}.validate()},
		{"constituents", IndexConstituentsRequest{Symbol: "BTCUSDT"}.validate()},
		{"insurance all", InsuranceFundRequest{}.validate()},
		{"adlRisk all", SymbolAdlRiskRequest{}.validate()},
		{"rpiDepth default limit", RPIOrderBookRequest{Symbol: "BTCUSDT"}.validate()},
		{"rpiDepth explicit limit", RPIOrderBookRequest{Symbol: "BTCUSDT", Limit: 1000}.validate()},
	} {
		if tc.err != nil {
			t.Errorf("%s: validate() error = %v", tc.name, tc.err)
		}
	}
}

func TestKlineWeight(t *testing.T) {
	t.Parallel()
	// limit을 생략하면 서버 기본값 500의 weight 5를 적용한다.
	for _, tc := range []weightCase{{0, 5}, {1, 1}, {99, 1}, {100, 2}, {499, 2}, {500, 5}, {1000, 5}, {1001, 10}, {1500, 10}} {
		if got := klineWeight(tc.limit); got != tc.weight {
			t.Errorf("klineWeight(%d) = %d, want %d", tc.limit, got, tc.weight)
		}
	}
}
