package binance

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	trade "github.com/proven-trade/cex-sdk"
)

func TestHistoricalTradesIsPublic(t *testing.T) {
	t.Parallel()

	harness := newExtensionHarness(t, http.StatusOK, `[{"id":28457,"price":"4.00000100","qty":"12.00000000","quoteQty":"48.000012","time":1499865549590,"isBuyerMaker":true,"isBestMatch":true}]`)
	fromID := int64(28457)
	trades, err := harness.client.HistoricalTrades(context.Background(), HistoricalTradesRequest{Symbol: "BTCUSDT", FromID: &fromID, Limit: 5})
	if err != nil {
		t.Fatalf("HistoricalTrades() error = %v", err)
	}
	if len(trades) != 1 || trades[0].ID != 28457 || trades[0].Quantity != "12.00000000" || !trades[0].BuyerMaker {
		t.Fatalf("HistoricalTrades() = %+v", trades)
	}
	harness.expect(t, http.MethodGet, "/api/v3/historicalTrades", false, 25, 0, "symbol=BTCUSDT", "fromId=28457", "limit=5")
}

func TestHistoricalBlockTrades(t *testing.T) {
	t.Parallel()

	harness := newExtensionHarness(t, http.StatusOK, `[{"id":582,"price":"0.052","qty":"5838","quoteQty":"303.576","time":1772506983321,"isBuyerMaker":true}]`)
	trades, err := harness.client.HistoricalBlockTrades(context.Background(), HistoricalBlockTradesRequest{Symbol: "BTCUSDT", FromID: 582, Limit: 10})
	if err != nil {
		t.Fatalf("HistoricalBlockTrades() error = %v", err)
	}
	if len(trades) != 1 || trades[0].ID != 582 || trades[0].Price != "0.052" || !trades[0].BuyerMaker {
		t.Fatalf("HistoricalBlockTrades() = %+v", trades)
	}
	harness.expect(t, http.MethodGet, "/api/v3/historicalBlockTrades", false, 25, 0, "symbol=BTCUSDT", "fromId=582", "limit=10")
}

func TestAggregateTrades(t *testing.T) {
	t.Parallel()

	harness := newExtensionHarness(t, http.StatusOK, `[{"a":26129,"p":"0.01633102","q":"4.70443515","f":27781,"l":27781,"T":1498793709153,"m":true,"M":true}]`)
	start := time.UnixMilli(1498793709000)
	end := time.UnixMilli(1498793709153)
	trades, err := harness.client.AggregateTrades(context.Background(), AggregateTradesRequest{Symbol: "BTCUSDT", StartTime: &start, EndTime: &end, Limit: 2})
	if err != nil {
		t.Fatalf("AggregateTrades() error = %v", err)
	}
	if len(trades) != 1 || trades[0].AggregateID != 26129 || trades[0].FirstTradeID != 27781 || trades[0].Time != 1498793709153 || !trades[0].BuyerMaker {
		t.Fatalf("AggregateTrades() = %+v", trades)
	}
	harness.expect(
		t,
		http.MethodGet,
		"/api/v3/aggTrades",
		false,
		4,
		0,
		"symbol=BTCUSDT",
		"startTime=1498793709000",
		"endTime=1498793709153",
		"limit=2",
	)
}

func TestUIKlines(t *testing.T) {
	t.Parallel()

	harness := newExtensionHarness(t, http.StatusOK, `[[1499040000000,"0.01634790","0.80000000","0.01575800","0.01577100","148976.11427815",1499644799999,"2434.19055334",308,"1756.87402397","28.46694368","0"]]`)
	klines, err := harness.client.UIKlines(context.Background(), KlinesRequest{Symbol: "BTCUSDT", Interval: Kline1Minute, TimeZone: "+09:00", Limit: 1})
	if err != nil {
		t.Fatalf("UIKlines() error = %v", err)
	}
	if len(klines) != 1 || klines[0].OpenTime != 1499040000000 || klines[0].Close != "0.01577100" || klines[0].TradeCount != 308 {
		t.Fatalf("UIKlines() = %+v", klines)
	}
	harness.expect(t, http.MethodGet, "/api/v3/uiKlines", false, 2, 0, "symbol=BTCUSDT", "interval=1m", "timeZone=+09:00", "limit=1")
}

func TestAveragePrice(t *testing.T) {
	t.Parallel()

	harness := newExtensionHarness(t, http.StatusOK, `{"mins":5,"price":"9.35751834","closeTime":1694061154503}`)
	price, err := harness.client.AveragePrice(context.Background(), AveragePriceRequest{Symbol: "BNBUSDT"})
	if err != nil {
		t.Fatalf("AveragePrice() error = %v", err)
	}
	if price.Minutes != 5 || price.Price != "9.35751834" || price.CloseTime != 1694061154503 || len(price.Raw) == 0 {
		t.Fatalf("AveragePrice() = %+v", price)
	}
	harness.expect(t, http.MethodGet, "/api/v3/avgPrice", false, 2, 0, "symbol=BNBUSDT")
}

const (
	fullTickerJSON = `{"symbol":"BNBBTC","priceChange":"-94.99999800","priceChangePercent":"-95.960","weightedAvgPrice":"0.29628482","prevClosePrice":"0.10002000","lastPrice":"4.00000200","lastQty":"200.00000000","bidPrice":"4.00000000","bidQty":"100.00000000","askPrice":"4.00000200","askQty":"100.00000000","openPrice":"99.00000000","highPrice":"100.00000000","lowPrice":"0.10000000","volume":"8913.30000000","quoteVolume":"15.30000000","openTime":1499783499040,"closeTime":1499869899040,"firstId":28385,"lastId":28460,"count":76}`
	miniTickerJSON = `{"symbol":"BNBBTC","openPrice":"99.00000000","highPrice":"100.00000000","lowPrice":"0.10000000","lastPrice":"4.00000200","volume":"8913.30000000","quoteVolume":"15.30000000","openTime":1499783499040,"closeTime":1499869899040,"firstId":28385,"lastId":28460,"count":76}`
)

func TestTicker24HourSelections(t *testing.T) {
	t.Parallel()

	t.Run("single symbol returns a one element list", func(t *testing.T) {
		t.Parallel()
		harness := newExtensionHarness(t, http.StatusOK, fullTickerJSON)
		tickers, err := harness.client.Ticker24Hour(context.Background(), Ticker24HourRequest{Symbol: "BNBBTC"})
		if err != nil {
			t.Fatalf("Ticker24Hour() error = %v", err)
		}
		if len(tickers) != 1 || tickers[0].LastPrice != "4.00000200" || tickers[0].TradeCount != 76 || len(tickers[0].Raw) == 0 {
			t.Fatalf("Ticker24Hour() = %+v", tickers)
		}
		harness.expect(t, http.MethodGet, "/api/v3/ticker/24hr", false, 2, 0, "symbol=BNBBTC")
	})
	t.Run("symbols list with mini type", func(t *testing.T) {
		t.Parallel()
		harness := newExtensionHarness(t, http.StatusOK, `[`+miniTickerJSON+`,`+miniTickerJSON+`]`)
		tickers, err := harness.client.Ticker24Hour(context.Background(), Ticker24HourRequest{Symbols: []string{"BTCUSDT", "BNBUSDT"}, Type: TickerTypeMini})
		if err != nil {
			t.Fatalf("Ticker24Hour() error = %v", err)
		}
		if len(tickers) != 2 || tickers[0].OpenPrice != "99.00000000" || tickers[0].PriceChange != "" {
			t.Fatalf("Ticker24Hour() = %+v", tickers)
		}
		harness.expect(t, http.MethodGet, "/api/v3/ticker/24hr", false, 2, 0, `symbols=["BTCUSDT","BNBUSDT"]`, "type=MINI")
	})
	t.Run("all symbols is explicit and costs 80", func(t *testing.T) {
		t.Parallel()
		harness := newExtensionHarness(t, http.StatusOK, `[`+fullTickerJSON+`]`)
		if _, err := harness.client.Ticker24Hour(context.Background(), Ticker24HourRequest{AllSymbols: true}); err != nil {
			t.Fatalf("Ticker24Hour() error = %v", err)
		}
		harness.expect(t, http.MethodGet, "/api/v3/ticker/24hr", false, 80, 0)
	})
}

func TestTradingDayTicker(t *testing.T) {
	t.Parallel()

	harness := newExtensionHarness(t, http.StatusOK, `[`+miniTickerJSON+`,`+miniTickerJSON+`]`)
	tickers, err := harness.client.TradingDayTicker(context.Background(), TradingDayTickerRequest{Symbols: []string{"BTCUSDT", "BNBUSDT"}, TimeZone: "0", Type: TickerTypeMini})
	if err != nil {
		t.Fatalf("TradingDayTicker() error = %v", err)
	}
	if len(tickers) != 2 || tickers[1].CloseTime != 1499869899040 {
		t.Fatalf("TradingDayTicker() = %+v", tickers)
	}
	harness.expect(t, http.MethodGet, "/api/v3/ticker/tradingDay", false, 8, 0, `symbols=["BTCUSDT","BNBUSDT"]`, "timeZone=0", "type=MINI")
}

func TestRollingTicker(t *testing.T) {
	t.Parallel()

	harness := newExtensionHarness(t, http.StatusOK, fullTickerJSON)
	tickers, err := harness.client.RollingTicker(context.Background(), RollingTickerRequest{Symbol: "BNBBTC", WindowSize: "1h"})
	if err != nil {
		t.Fatalf("RollingTicker() error = %v", err)
	}
	if len(tickers) != 1 || tickers[0].Symbol != "BNBBTC" || tickers[0].FirstTradeID != 28385 {
		t.Fatalf("RollingTicker() = %+v", tickers)
	}
	harness.expect(t, http.MethodGet, "/api/v3/ticker", false, 4, 0, "symbol=BNBBTC", "windowSize=1h")
}

func TestTickerWeights(t *testing.T) {
	t.Parallel()

	if got := (Ticker24HourRequest{Symbols: make([]string, 20)}).weight(); got != 2 {
		t.Fatalf("24hr weight for 20 symbols = %d, want 2", got)
	}
	if got := (Ticker24HourRequest{Symbols: make([]string, 21)}).weight(); got != 40 {
		t.Fatalf("24hr weight for 21 symbols = %d, want 40", got)
	}
	if got := (Ticker24HourRequest{Symbols: make([]string, 101)}).weight(); got != 80 {
		t.Fatalf("24hr weight for 101 symbols = %d, want 80", got)
	}
	if got := (RollingTickerRequest{Symbols: make([]string, 50)}).weight(); got != 200 {
		t.Fatalf("rolling weight for 50 symbols = %d, want 200", got)
	}
	if got := (RollingTickerRequest{Symbols: make([]string, 51)}).weight(); got != 200 {
		t.Fatalf("rolling weight for 51 symbols = %d, want the 200 cap", got)
	}
	if got := (TradingDayTickerRequest{Symbols: make([]string, 3)}).weight(); got != 12 {
		t.Fatalf("trading day weight for 3 symbols = %d, want 12", got)
	}
}

func TestTickerPricesAndBookTickers(t *testing.T) {
	t.Parallel()

	t.Run("single price returns a one element list", func(t *testing.T) {
		t.Parallel()
		harness := newExtensionHarness(t, http.StatusOK, `{"symbol":"LTCBNB","price":"4.00000200"}`)
		prices, err := harness.client.TickerPrices(context.Background(), TickerPricesRequest{Symbol: "LTCBNB"})
		if err != nil {
			t.Fatalf("TickerPrices() error = %v", err)
		}
		if len(prices) != 1 || prices[0].Price != "4.00000200" || len(prices[0].Raw) == 0 {
			t.Fatalf("TickerPrices() = %+v", prices)
		}
		harness.expect(t, http.MethodGet, "/api/v3/ticker/price", false, 2, 0, "symbol=LTCBNB")
	})
	t.Run("all prices cost 4", func(t *testing.T) {
		t.Parallel()
		harness := newExtensionHarness(t, http.StatusOK, `[{"symbol":"LTCBNB","price":"4.00000200"},{"symbol":"ETHBTC","price":"0.07946600"}]`)
		prices, err := harness.client.TickerPrices(context.Background(), TickerPricesRequest{AllSymbols: true})
		if err != nil {
			t.Fatalf("TickerPrices() error = %v", err)
		}
		if len(prices) != 2 || prices[1].Symbol != "ETHBTC" {
			t.Fatalf("TickerPrices() = %+v", prices)
		}
		harness.expect(t, http.MethodGet, "/api/v3/ticker/price", false, 4, 0)
	})
	t.Run("book tickers for a symbol list", func(t *testing.T) {
		t.Parallel()
		harness := newExtensionHarness(t, http.StatusOK, `[{"symbol":"LTCBNB","bidPrice":"4.00000000","bidQty":"431.00000000","askPrice":"4.00000200","askQty":"9.00000000"}]`)
		tickers, err := harness.client.BookTickers(context.Background(), BookTickersRequest{Symbols: []string{"LTCBNB", "ETHBTC"}})
		if err != nil {
			t.Fatalf("BookTickers() error = %v", err)
		}
		if len(tickers) != 1 || tickers[0].BidQuantity != "431.00000000" || len(tickers[0].Raw) == 0 {
			t.Fatalf("BookTickers() = %+v", tickers)
		}
		harness.expect(t, http.MethodGet, "/api/v3/ticker/bookTicker", false, 4, 0, `symbols=["LTCBNB","ETHBTC"]`)
	})
}

func TestReferencePrice(t *testing.T) {
	t.Parallel()

	t.Run("price set", func(t *testing.T) {
		t.Parallel()
		harness := newExtensionHarness(t, http.StatusOK, `{"symbol":"BAZUSD","referencePrice":"10.00","timestamp":1770736694138}`)
		price, err := harness.client.ReferencePrice(context.Background(), ReferencePriceRequest{Symbol: "BAZUSD"})
		if err != nil {
			t.Fatalf("ReferencePrice() error = %v", err)
		}
		if price.Price == nil || *price.Price != "10.00" || price.Timestamp != 1770736694138 || len(price.Raw) == 0 {
			t.Fatalf("ReferencePrice() = %+v", price)
		}
		harness.expect(t, http.MethodGet, "/api/v3/referencePrice", false, 2, 0, "symbol=BAZUSD")
	})
	t.Run("price not set", func(t *testing.T) {
		t.Parallel()
		harness := newExtensionHarness(t, http.StatusOK, `{"symbol":"BAZUSD","referencePrice":null,"timestamp":1770736694138}`)
		price, err := harness.client.ReferencePrice(context.Background(), ReferencePriceRequest{Symbol: "BAZUSD"})
		if err != nil {
			t.Fatalf("ReferencePrice() error = %v", err)
		}
		if price.Price != nil || price.Timestamp != 1770736694138 {
			t.Fatalf("ReferencePrice() = %+v, want a nil price", price)
		}
	})
	t.Run("never set is a 400 exchange error", func(t *testing.T) {
		t.Parallel()
		harness := newExtensionHarness(t, http.StatusBadRequest, `{"code":-2043,"msg":"This symbol doesn't have a reference price."}`)
		_, err := harness.client.ReferencePrice(context.Background(), ReferencePriceRequest{Symbol: "BAZUSD"})
		var apiError *trade.APIError
		if !errors.As(err, &apiError) || apiError.ExchangeCode != "-2043" || apiError.HTTPStatus != http.StatusBadRequest {
			t.Fatalf("ReferencePrice() error = %v, want APIError -2043", err)
		}
	})
}

func TestReferencePriceCalculation(t *testing.T) {
	t.Parallel()

	t.Run("arithmetic mean", func(t *testing.T) {
		t.Parallel()
		harness := newExtensionHarness(t, http.StatusOK, `{"symbol":"BAZUSD","calculationType":"ARITHMETIC_MEAN","bucketCount":10,"bucketWidthMs":1000}`)
		calculation, err := harness.client.ReferencePriceCalculation(context.Background(), ReferencePriceCalculationRequest{Symbol: "BAZUSD", SymbolStatus: SymbolStatusTrading})
		if err != nil {
			t.Fatalf("ReferencePriceCalculation() error = %v", err)
		}
		if calculation.CalculationType != "ARITHMETIC_MEAN" || calculation.BucketCount != 10 || calculation.BucketWidthMillis != 1000 || len(calculation.Raw) == 0 {
			t.Fatalf("ReferencePriceCalculation() = %+v", calculation)
		}
		harness.expect(t, http.MethodGet, "/api/v3/referencePrice/calculation", false, 2, 0, "symbol=BAZUSD", "symbolStatus=TRADING")
	})
	t.Run("external", func(t *testing.T) {
		t.Parallel()
		harness := newExtensionHarness(t, http.StatusOK, `{"symbol":"BAZUSD","calculationType":"EXTERNAL","externalCalculationId":42}`)
		calculation, err := harness.client.ReferencePriceCalculation(context.Background(), ReferencePriceCalculationRequest{Symbol: "BAZUSD"})
		if err != nil {
			t.Fatalf("ReferencePriceCalculation() error = %v", err)
		}
		if calculation.CalculationType != "EXTERNAL" || calculation.ExternalCalculationID != 42 {
			t.Fatalf("ReferencePriceCalculation() = %+v", calculation)
		}
	})
}

func TestExecutionRules(t *testing.T) {
	t.Parallel()

	const body = `{"symbolRules":[{"symbol":"BAZUSD","rules":[{"ruleType":"PRICE_RANGE","bidLimitMultUp":"1.0001","bidLimitMultDown":"0.9999","askLimitMultUp":"1.0001","askLimitMultDown":"0.9999"}]}]}`
	t.Run("symbols cost 2 each", func(t *testing.T) {
		t.Parallel()
		harness := newExtensionHarness(t, http.StatusOK, body)
		rules, err := harness.client.ExecutionRules(context.Background(), ExecutionRulesRequest{Symbols: []string{"BAZUSD", "BTCUSDT"}})
		if err != nil {
			t.Fatalf("ExecutionRules() error = %v", err)
		}
		if len(rules.SymbolRules) != 1 || rules.SymbolRules[0].Symbol != "BAZUSD" || len(rules.Raw) == 0 {
			t.Fatalf("ExecutionRules() = %+v", rules)
		}
		rule := rules.SymbolRules[0].Rules[0]
		if rule.RuleType != "PRICE_RANGE" || rule.BidLimitMultUp != "1.0001" || rule.AskLimitMultDown != "0.9999" {
			t.Fatalf("ExecutionRules() rule = %+v", rule)
		}
		harness.expect(t, http.MethodGet, "/api/v3/executionRules", false, 4, 0, `symbols=["BAZUSD","BTCUSDT"]`)
	})
	t.Run("no selection costs 40", func(t *testing.T) {
		t.Parallel()
		harness := newExtensionHarness(t, http.StatusOK, body)
		if _, err := harness.client.ExecutionRules(context.Background(), ExecutionRulesRequest{}); err != nil {
			t.Fatalf("ExecutionRules() error = %v", err)
		}
		harness.expect(t, http.MethodGet, "/api/v3/executionRules", false, 40, 0)
	})
	t.Run("symbol status costs 40", func(t *testing.T) {
		t.Parallel()
		harness := newExtensionHarness(t, http.StatusOK, body)
		if _, err := harness.client.ExecutionRules(context.Background(), ExecutionRulesRequest{SymbolStatus: SymbolStatusTrading}); err != nil {
			t.Fatalf("ExecutionRules() error = %v", err)
		}
		harness.expect(t, http.MethodGet, "/api/v3/executionRules", false, 40, 0, "symbolStatus=TRADING")
	})
}

func TestMarketDataValidation(t *testing.T) {
	t.Parallel()

	harness := newExtensionHarness(t, http.StatusOK, `{}`)
	ctx := context.Background()
	_, err := harness.client.ExecutionRules(ctx, ExecutionRulesRequest{Symbol: "BTCUSDT", SymbolStatus: SymbolStatusTrading})
	harness.requireValidation(t, "ExecutionRules mixed selection", err)
	_, err = harness.client.HistoricalTrades(ctx, HistoricalTradesRequest{})
	harness.requireValidation(t, "HistoricalTrades without symbol", err)
	_, err = harness.client.HistoricalTrades(ctx, HistoricalTradesRequest{Symbol: "BTCUSDT", Limit: 1001})
	harness.requireValidation(t, "HistoricalTrades limit", err)
	_, err = harness.client.HistoricalBlockTrades(ctx, HistoricalBlockTradesRequest{Symbol: "BTCUSDT", FromID: -1})
	harness.requireValidation(t, "HistoricalBlockTrades fromId", err)
	_, err = harness.client.AggregateTrades(ctx, AggregateTradesRequest{Symbol: "BTCUSDT", Limit: 1001})
	harness.requireValidation(t, "AggregateTrades limit", err)
	_, err = harness.client.UIKlines(ctx, KlinesRequest{Symbol: "BTCUSDT", Interval: "2m"})
	harness.requireValidation(t, "UIKlines interval", err)
	_, err = harness.client.AveragePrice(ctx, AveragePriceRequest{})
	harness.requireValidation(t, "AveragePrice without symbol", err)
	_, err = harness.client.Ticker24Hour(ctx, Ticker24HourRequest{})
	harness.requireValidation(t, "Ticker24Hour without selection", err)
	_, err = harness.client.Ticker24Hour(ctx, Ticker24HourRequest{Symbol: "BTCUSDT", AllSymbols: true})
	harness.requireValidation(t, "Ticker24Hour mixed selection", err)
	_, err = harness.client.TradingDayTicker(ctx, TradingDayTickerRequest{Symbols: []string{"BTCUSDT", "BTCUSDT"}})
	harness.requireValidation(t, "TradingDayTicker duplicate symbols", err)
	_, err = harness.client.RollingTicker(ctx, RollingTickerRequest{Symbol: "BTCUSDT", WindowSize: "60m"})
	harness.requireValidation(t, "RollingTicker window", err)
	_, err = harness.client.RollingTicker(ctx, RollingTickerRequest{Symbol: "BTCUSDT", WindowSize: "8d"})
	harness.requireValidation(t, "RollingTicker days window", err)
	_, err = harness.client.TickerPrices(ctx, TickerPricesRequest{})
	harness.requireValidation(t, "TickerPrices without selection", err)
	_, err = harness.client.BookTickers(ctx, BookTickersRequest{Symbol: "BTCUSDT", Symbols: []string{"ETHBTC"}})
	harness.requireValidation(t, "BookTickers mixed selection", err)
	_, err = harness.client.ReferencePrice(ctx, ReferencePriceRequest{})
	harness.requireValidation(t, "ReferencePrice without symbol", err)
	_, err = harness.client.ReferencePriceCalculation(ctx, ReferencePriceCalculationRequest{Symbol: "BAZUSD", SymbolStatus: "OPEN"})
	harness.requireValidation(t, "ReferencePriceCalculation status", err)
}
