package archive

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
	"github.com/proven-trade/cex-sdk/ratelimit"
	"github.com/proven-trade/cex-sdk/transport"
	"github.com/proven-trade/cex-sdk/unified"
)

type recordingSender struct {
	mu     sync.Mutex
	routes []transport.EgressRouteID
	paths  []string
}

func (sender *recordingSender) Do(
	ctx context.Context,
	routeID transport.EgressRouteID,
	request *http.Request,
) (*http.Response, error) {
	sender.mu.Lock()
	sender.routes = append(sender.routes, routeID)
	sender.paths = append(sender.paths, request.URL.Path)
	sender.mu.Unlock()
	return http.DefaultClient.Do(request.WithContext(ctx))
}

func TestTradeCandlesReadsLegacyDescendingSpotDump(t *testing.T) {
	t.Parallel()

	// 2018년 이전 형식: 쉼표 뒤 공백, 최신 체결부터 기록, 1 매수 2 매도.
	dump := strings.Join([]string{
		"1514764865.000000, 103, 13001.000000, 0.500000, 2",
		"1514764860.250000, 102, 13010.500000, 0.100000, 1",
		"1514764859.999000, 101, 12990.000000, 1.000000, 2",
		"1514764800.000000, 100, 13000.000000, 0.250000, 1",
	}, "\n") + "\n"
	server, sender := newArchiveServer(t, map[string]string{
		"/spot/deals/201801/BTC_USDT-201801.csv.gz": dump,
	})
	client := newTestClient(t, server.URL, sender)

	candles, err := client.TradeCandles(
		context.Background(),
		TradesRequest{Market: MarketSpot, Symbol: "BTC_USDT", Year: 2018, Month: time.January},
		time.Minute,
		trade.WithEgressRoute("route-b"),
	)
	if err != nil {
		t.Fatalf("TradeCandles() error = %v", err)
	}
	want := []unified.TradeCandle{
		{
			Candle: unified.Candle{
				StartTime: 1_514_764_800_000, Open: "13000.000000", High: "13000.000000",
				Low: "12990.000000", Close: "12990.000000", Volume: "1.250000",
			},
			QuoteVolume: "16240.000000000000", TradeCount: 2,
		},
		{
			Candle: unified.Candle{
				StartTime: 1_514_764_860_000, Open: "13010.500000", High: "13010.500000",
				Low: "13001.000000", Close: "13001.000000", Volume: "0.600000",
			},
			QuoteVolume: "7801.550000000000", TradeCount: 2,
		},
	}
	if len(candles) != len(want) || candles[0] != want[0] || candles[1] != want[1] {
		t.Fatalf("TradeCandles() = %+v, want %+v", candles, want)
	}
	if routes := sender.snapshotRoutes(); len(routes) != 1 || routes[0] != "route-b" {
		t.Fatalf("sender routes = %v", routes)
	}
}

func TestFuturesTradesAndCandlesReaders(t *testing.T) {
	t.Parallel()

	server, sender := newArchiveServer(t, map[string]string{
		"/futures_usdt/trades/202610/BTC_USDT-202610.csv.gz": "1790812800.577279,844545519,83578.6,-3\n" +
			"1790812800.935000,844545522,83575.7,3791\n",
		"/futures_usdt/candlesticks_1m/202610/BTC_USDT-202610.csv.gz": "1790812800,638485,83535.9,83618.3,83535.9,83578.7\n",
	})
	client := newTestClient(t, server.URL, sender)

	trades, err := client.Trades(context.Background(), TradesRequest{
		Market: MarketFuturesUSDT, Symbol: "BTC_USDT", Year: 2026, Month: time.October,
	})
	if err != nil {
		t.Fatalf("Trades() error = %v", err)
	}
	defer trades.Close()
	sell, err := trades.Next()
	if err != nil || sell.Side != unified.SideSell || sell.Amount != "3" ||
		sell.Timestamp != 1_790_812_800_577 || sell.Time != "1790812800.577279" {
		t.Fatalf("first futures trade = %+v, error = %v", sell, err)
	}
	buy, err := trades.Next()
	if err != nil || buy.Side != unified.SideBuy || buy.Amount != "3791" || buy.ID != "844545522" {
		t.Fatalf("second futures trade = %+v, error = %v", buy, err)
	}
	if _, err := trades.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("third Next() error = %v, want EOF", err)
	}

	candles, err := client.Candles(context.Background(), CandlesRequest{
		Market: MarketFuturesUSDT, Symbol: "BTC_USDT", Interval: Candle1Minute,
		Year: 2026, Month: time.October,
	})
	if err != nil {
		t.Fatalf("Candles() error = %v", err)
	}
	defer candles.Close()
	candle, err := candles.Next()
	want := Candle{
		StartTime: 1_790_812_800_000, Open: "83578.7", High: "83618.3",
		Low: "83535.9", Close: "83535.9", Volume: "638485",
	}
	if err != nil || candle != want {
		t.Fatalf("Candles().Next() = %+v, error = %v, want %+v", candle, err, want)
	}
}

func TestArchiveErrors(t *testing.T) {
	t.Parallel()

	server, sender := newArchiveServer(t, map[string]string{
		"/spot/deals/202401/BTC_USDT-202401.csv.gz": "1704067200.1,1,42000,0.1,1\n1704067200.2,2,42000,1e-3,1\n",
	})
	client := newTestClient(t, server.URL, sender)
	ctx := context.Background()

	_, err := client.Candles(ctx, CandlesRequest{
		Market: MarketSpot, Symbol: "BTC_USDT", Interval: Candle1Minute, Year: 2026, Month: time.October,
	})
	if !errors.Is(err, ErrFileNotFound) || !errors.Is(err, trade.ErrUnsupportedCapability) {
		t.Fatalf("missing file error = %v", err)
	}

	_, err = client.TradeCandles(ctx, TradesRequest{
		Market: MarketSpot, Symbol: "BTC_USDT", Year: 2024, Month: time.January,
	}, time.Minute)
	if err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("scientific amount error = %v", err)
	}

	validationCases := map[string]error{
		"symbol": func() error {
			_, err := client.Trades(ctx, TradesRequest{Market: MarketSpot, Symbol: "btc-usdt", Year: 2024, Month: 1})
			return err
		}(),
		"market": func() error {
			_, err := client.Trades(ctx, TradesRequest{Market: "options", Symbol: "BTC_USDT", Year: 2024, Month: 1})
			return err
		}(),
		"month": func() error {
			_, err := client.Trades(ctx, TradesRequest{Market: MarketSpot, Symbol: "BTC_USDT", Year: 2024, Month: 13})
			return err
		}(),
		"candle interval": func() error {
			_, err := client.Candles(ctx, CandlesRequest{
				Market: MarketSpot, Symbol: "BTC_USDT", Interval: "15m", Year: 2024, Month: 1,
			})
			return err
		}(),
		"trade candle interval": func() error {
			_, err := client.TradeCandles(ctx, TradesRequest{
				Market: MarketSpot, Symbol: "BTC_USDT", Year: 2024, Month: 1,
			}, 7*time.Hour)
			return err
		}(),
	}
	for name, err := range validationCases {
		if !errors.Is(err, trade.ErrValidation) {
			t.Fatalf("%s error = %v, want ErrValidation", name, err)
		}
	}
	if paths := sender.snapshotPaths(); len(paths) != 2 {
		t.Fatalf("validation errors reached the network: %v", paths)
	}
}

func newArchiveServer(t *testing.T, files map[string]string) (*httptest.Server, *recordingSender) {
	t.Helper()
	compressed := make(map[string][]byte, len(files))
	for path, content := range files {
		var buffer bytes.Buffer
		writer := gzip.NewWriter(&buffer)
		if _, err := writer.Write([]byte(content)); err != nil {
			t.Fatalf("gzip write error = %v", err)
		}
		if err := writer.Close(); err != nil {
			t.Fatalf("gzip close error = %v", err)
		}
		compressed[path] = buffer.Bytes()
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, exists := compressed[request.URL.Path]
		if !exists {
			http.Error(writer, "<Error><Code>NoSuchKey</Code></Error>", http.StatusNotFound)
			return
		}
		writer.Header().Set("Content-Type", "application/octet-stream")
		_, _ = writer.Write(body)
	}))
	t.Cleanup(server.Close)
	return server, &recordingSender{}
}

func newTestClient(t *testing.T, baseURL string, sender commonexchange.Sender) *Client {
	t.Helper()
	limiter, err := ratelimit.New()
	if err != nil {
		t.Fatalf("ratelimit.New() error = %v", err)
	}
	executor, err := commonexchange.NewExecutor(commonexchange.ExecutorConfig{
		Sender: sender, Limiter: limiter, MaxResponseBodyBytes: 16,
	})
	if err != nil {
		t.Fatalf("NewExecutor() error = %v", err)
	}
	client, err := New(Config{
		Executor: executor, DefaultEgressRouteID: "route-a",
		BaseURL: baseURL, AllowInsecureHTTP: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return client
}

func (sender *recordingSender) snapshotRoutes() []transport.EgressRouteID {
	sender.mu.Lock()
	defer sender.mu.Unlock()
	return append([]transport.EgressRouteID(nil), sender.routes...)
}

func (sender *recordingSender) snapshotPaths() []string {
	sender.mu.Lock()
	defer sender.mu.Unlock()
	return append([]string(nil), sender.paths...)
}
