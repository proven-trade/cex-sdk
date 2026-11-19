package archive

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/exchange/binance"
)

const klineHeader = "open_time,open,high,low,close,volume,close_time,quote_volume,count,taker_buy_volume,taker_buy_quote_volume,ignore"

// csvLines는 줄을 개행으로 이어 파일 내용으로 만든다.
func csvLines(lines ...string) string {
	return strings.Join(lines, "\n") + "\n"
}

func readAllKlines(t *testing.T, reader *KlineReader) []binance.Kline {
	t.Helper()
	defer reader.Close()
	var rows []binance.Kline
	for {
		kline, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return rows
		}
		if err != nil {
			t.Fatalf("Next() error = %v", err)
		}
		rows = append(rows, kline)
	}
}

func TestAggTradesReadsSpotDailyFile(t *testing.T) {
	t.Parallel()

	filePath := "/data/spot/daily/aggTrades/ADABNB/ADABNB-aggTrades-2025-01-01.zip"
	server, sender := newArchiveServer(t, map[string]archiveFile{filePath: {csv: csvLines(
		"11073723,0.00120300,8.40000000,15205766,15205766,1735689623270289,True,True",
		"11073725,0.00120400,60.00000000,15205768,15205768,1735689664745776,False,False",
	)}})
	client := newTestClient(t, server.URL, sender, 0)

	reader, err := client.AggTrades(context.Background(), AggTradesRequest{
		Market: MarketSpot, Symbol: "ADABNB", Year: 2025, Month: time.January, Day: 1,
	}, trade.WithEgressRoute("route-b"))
	if err != nil {
		t.Fatalf("AggTrades() error = %v", err)
	}
	defer reader.Close()
	first, err := reader.Next()
	want := AggTrade{
		AggregateID: 11_073_723, Price: "0.00120300", Quantity: "8.40000000",
		FirstTradeID: 15_205_766, LastTradeID: 15_205_766, Time: 1_735_689_623_270,
		BuyerMaker: true, BestMatch: true,
	}
	if err != nil || first != want {
		t.Fatalf("first aggregate trade = %+v, error = %v, want %+v", first, err, want)
	}
	second, err := reader.Next()
	want = AggTrade{
		AggregateID: 11_073_725, Price: "0.00120400", Quantity: "60.00000000",
		FirstTradeID: 15_205_768, LastTradeID: 15_205_768, Time: 1_735_689_664_745,
	}
	if err != nil || second != want {
		t.Fatalf("second aggregate trade = %+v, error = %v, want %+v", second, err, want)
	}
	if _, err := reader.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("third Next() error = %v, want EOF", err)
	}
	routes, paths := sender.snapshot()
	if len(routes) != 2 || routes[0] != "route-b" || routes[1] != "route-b" ||
		paths[0] != filePath+".CHECKSUM" || paths[1] != filePath {
		t.Fatalf("requests = %v %v", routes, paths)
	}
}

func TestAggTradesReadsUSDMDailyFileWithHeaderAndExponents(t *testing.T) {
	t.Parallel()

	filePath := "/data/futures/um/daily/aggTrades/1000SATSUSDT/1000SATSUSDT-aggTrades-2026-11-16.zip"
	server, sender := newArchiveServer(t, map[string]archiveFile{filePath: {csv: csvLines(
		"agg_trade_id,price,quantity,first_trade_id,last_trade_id,transact_time,is_buyer_maker",
		"94005036,7.72E-6,184833.0,407954810,407954810,1794787200036,false",
		"94005037,7.71E-6,5.5738058E7,407954811,407954813,1794787202245,true",
	)}})
	client := newTestClient(t, server.URL, sender, 0)

	reader, err := client.AggTrades(context.Background(), AggTradesRequest{
		Market: MarketUSDM, Symbol: "1000SATSUSDT", Year: 2026, Month: time.November, Day: 16,
	})
	if err != nil {
		t.Fatalf("AggTrades() error = %v", err)
	}
	defer reader.Close()
	first, err := reader.Next()
	want := AggTrade{
		AggregateID: 94_005_036, Price: "0.00000772", Quantity: "184833.0",
		FirstTradeID: 407_954_810, LastTradeID: 407_954_810, Time: 1_794_787_200_036,
	}
	if err != nil || first != want {
		t.Fatalf("first aggregate trade = %+v, error = %v, want %+v", first, err, want)
	}
	second, err := reader.Next()
	want = AggTrade{
		AggregateID: 94_005_037, Price: "0.00000771", Quantity: "55738058",
		FirstTradeID: 407_954_811, LastTradeID: 407_954_813, Time: 1_794_787_202_245, BuyerMaker: true,
	}
	if err != nil || second != want {
		t.Fatalf("second aggregate trade = %+v, error = %v, want %+v", second, err, want)
	}
	if _, err := reader.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("third Next() error = %v, want EOF", err)
	}
	if _, paths := sender.snapshot(); len(paths) != 2 || paths[1] != filePath {
		t.Fatalf("requests = %v", paths)
	}
}

func TestAggTradesReadsUSDMMonthlyFileWithoutHeader(t *testing.T) {
	t.Parallel()

	filePath := "/data/futures/um/monthly/aggTrades/BTCDOMUSDT/BTCDOMUSDT-aggTrades-2021-06.zip"
	server, sender := newArchiveServer(t, map[string]archiveFile{filePath: {csv: csvLines(
		"1,1005.2,0.012,1,1,1624246287727,false",
		"3,1005.7,0.135,3,4,1624246313745,true",
	)}})
	client := newTestClient(t, server.URL, sender, 0)

	reader, err := client.AggTrades(context.Background(), AggTradesRequest{
		Market: MarketUSDM, Symbol: "BTCDOMUSDT", Year: 2021, Month: time.June,
	})
	if err != nil {
		t.Fatalf("AggTrades() error = %v", err)
	}
	defer reader.Close()
	first, err := reader.Next()
	want := AggTrade{
		AggregateID: 1, Price: "1005.2", Quantity: "0.012", FirstTradeID: 1, LastTradeID: 1, Time: 1_624_246_287_727,
	}
	if err != nil || first != want {
		t.Fatalf("first aggregate trade = %+v, error = %v, want %+v", first, err, want)
	}
	second, err := reader.Next()
	want = AggTrade{
		AggregateID: 3, Price: "1005.7", Quantity: "0.135", FirstTradeID: 3, LastTradeID: 4,
		Time: 1_624_246_313_745, BuyerMaker: true,
	}
	if err != nil || second != want {
		t.Fatalf("second aggregate trade = %+v, error = %v, want %+v", second, err, want)
	}
	if _, err := reader.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("third Next() error = %v, want EOF", err)
	}
	if _, paths := sender.snapshot(); len(paths) != 2 || paths[1] != filePath {
		t.Fatalf("requests = %v", paths)
	}
}

func TestTradesReadsSpotMonthlyAndDailyFiles(t *testing.T) {
	t.Parallel()

	monthlyPath := "/data/spot/monthly/trades/ADABNB/ADABNB-trades-2018-04.zip"
	dailyPath := "/data/spot/daily/trades/ADABNB/ADABNB-trades-2025-01-01.zip"
	files := map[string]archiveFile{}
	files[monthlyPath] = archiveFile{csv: csvLines(
		"0,0.01000000,1932.50000000,19.32500000,1523937771564,True,True",
		"1,0.01000000,346.19000000,3.46190000,1523937771582,False,True",
	)}
	files[dailyPath] = archiveFile{csv: csvLines(
		"15205768,0.00120400,60.00000000,0.07224000,1735689664745776,False,True",
	)}
	server, sender := newArchiveServer(t, files)
	client := newTestClient(t, server.URL, sender, 0)
	ctx := context.Background()

	monthly, err := client.Trades(ctx, TradesRequest{
		Market: MarketSpot, Symbol: "ADABNB", Year: 2018, Month: time.April,
	}, trade.WithEgressRoute("route-b"))
	if err != nil {
		t.Fatalf("Trades(monthly) error = %v", err)
	}
	defer monthly.Close()
	first, err := monthly.Next()
	want := Trade{
		ID: 0, Price: "0.01000000", Quantity: "1932.50000000", QuoteQuantity: "19.32500000",
		Time: 1_523_937_771_564, BuyerMaker: true, BestMatch: true,
	}
	if err != nil || first != want {
		t.Fatalf("first trade = %+v, error = %v, want %+v", first, err, want)
	}
	second, err := monthly.Next()
	want = Trade{
		ID: 1, Price: "0.01000000", Quantity: "346.19000000", QuoteQuantity: "3.46190000",
		Time: 1_523_937_771_582, BestMatch: true,
	}
	if err != nil || second != want {
		t.Fatalf("second trade = %+v, error = %v, want %+v", second, err, want)
	}
	if _, err := monthly.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("third Next() error = %v, want EOF", err)
	}

	daily, err := client.Trades(ctx, TradesRequest{
		Market: MarketSpot, Symbol: "ADABNB", Year: 2025, Month: time.January, Day: 1,
	})
	if err != nil {
		t.Fatalf("Trades(daily) error = %v", err)
	}
	defer daily.Close()
	microsecond, err := daily.Next()
	want = Trade{
		ID: 15_205_768, Price: "0.00120400", Quantity: "60.00000000", QuoteQuantity: "0.07224000",
		Time: 1_735_689_664_745, BestMatch: true,
	}
	if err != nil || microsecond != want {
		t.Fatalf("microsecond trade = %+v, error = %v, want %+v", microsecond, err, want)
	}
	routes, paths := sender.snapshot()
	if len(routes) != 4 || routes[0] != "route-b" || routes[1] != "route-b" || routes[2] != "route-a" ||
		paths[0] != monthlyPath+".CHECKSUM" || paths[1] != monthlyPath ||
		paths[2] != dailyPath+".CHECKSUM" || paths[3] != dailyPath {
		t.Fatalf("requests = %v %v", routes, paths)
	}
}

func TestTradesReadsUSDMDailyFileWithHeaderAndExponents(t *testing.T) {
	t.Parallel()

	filePath := "/data/futures/um/daily/trades/1000SATSUSDT/1000SATSUSDT-trades-2026-11-16.zip"
	server, sender := newArchiveServer(t, map[string]archiveFile{filePath: {csv: csvLines(
		"id,price,qty,quote_qty,time,is_buyer_maker",
		"407954810,7.72E-6,184833.0,1.4269108,1794787200036,false",
		"407954813,7.7E-6,4.0,3.08E-5,1794787205000,true",
	)}})
	client := newTestClient(t, server.URL, sender, 0)

	reader, err := client.Trades(context.Background(), TradesRequest{
		Market: MarketUSDM, Symbol: "1000SATSUSDT", Year: 2026, Month: time.November, Day: 16,
	})
	if err != nil {
		t.Fatalf("Trades() error = %v", err)
	}
	defer reader.Close()
	first, err := reader.Next()
	want := Trade{
		ID: 407_954_810, Price: "0.00000772", Quantity: "184833.0", QuoteQuantity: "1.4269108", Time: 1_794_787_200_036,
	}
	if err != nil || first != want {
		t.Fatalf("first trade = %+v, error = %v, want %+v", first, err, want)
	}
	second, err := reader.Next()
	want = Trade{
		ID: 407_954_813, Price: "0.0000077", Quantity: "4.0", QuoteQuantity: "0.0000308",
		Time: 1_794_787_205_000, BuyerMaker: true,
	}
	if err != nil || second != want {
		t.Fatalf("second trade = %+v, error = %v, want %+v", second, err, want)
	}
	if _, err := reader.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("third Next() error = %v, want EOF", err)
	}
	if _, paths := sender.snapshot(); len(paths) != 2 || paths[1] != filePath {
		t.Fatalf("requests = %v", paths)
	}
}

func TestTradesReadsUSDMMonthlyFileWithoutHeader(t *testing.T) {
	t.Parallel()

	filePath := "/data/futures/um/monthly/trades/BTCDOMUSDT/BTCDOMUSDT-trades-2021-06.zip"
	server, sender := newArchiveServer(t, map[string]archiveFile{filePath: {csv: csvLines(
		"1,1005.2,0.012,12.0,1624246287727,false",
		"2,1005.2,0.084,84.4,1624246313745,false",
	)}})
	client := newTestClient(t, server.URL, sender, 0)

	reader, err := client.Trades(context.Background(), TradesRequest{
		Market: MarketUSDM, Symbol: "BTCDOMUSDT", Year: 2021, Month: time.June,
	})
	if err != nil {
		t.Fatalf("Trades() error = %v", err)
	}
	defer reader.Close()
	first, err := reader.Next()
	want := Trade{ID: 1, Price: "1005.2", Quantity: "0.012", QuoteQuantity: "12.0", Time: 1_624_246_287_727}
	if err != nil || first != want {
		t.Fatalf("first trade = %+v, error = %v, want %+v", first, err, want)
	}
	second, err := reader.Next()
	want = Trade{ID: 2, Price: "1005.2", Quantity: "0.084", QuoteQuantity: "84.4", Time: 1_624_246_313_745}
	if err != nil || second != want {
		t.Fatalf("second trade = %+v, error = %v, want %+v", second, err, want)
	}
	if _, err := reader.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("third Next() error = %v, want EOF", err)
	}
}

func TestPriceKlinesReadUSDMFiles(t *testing.T) {
	t.Parallel()

	markPath := "/data/futures/um/monthly/markPriceKlines/BTCUSDT/1h/BTCUSDT-1h-2024-12.zip"
	indexPath := "/data/futures/um/monthly/indexPriceKlines/BTCUSDT/1h/BTCUSDT-1h-2024-12.zip"
	premiumPath := "/data/futures/um/monthly/premiumIndexKlines/BTCUSDT/1h/BTCUSDT-1h-2024-12.zip"
	premiumDailyPath := "/data/futures/um/daily/premiumIndexKlines/BTCUSDT/1m/BTCUSDT-1m-2025-01-01.zip"
	files := map[string]archiveFile{}
	files[markPath] = archiveFile{csv: csvLines(
		klineHeader,
		"1733011200000,96462.18661702,96743.09743262,96316.50000000,96392.91542920,0,1733014799999,0,3600,0,0,0",
		"1733014800000,96392.91117388,96580.20000000,96100,96163.91719858,0,1733018399999,0,3600,0,0,0",
	)}
	files[indexPath] = archiveFile{csv: csvLines(
		klineHeader,
		"1733011200000,96401.97851064,96662.14021277,96266.32936170,96331.51042553,0,1733014799999,0,3600,0,0,0",
	)}
	files[premiumPath] = archiveFile{csv: csvLines(
		klineHeader,
		"1733011200000,0.00075846,0.00112964,0.00036845,0.00074372,0,1733014799999,0,720,0,0,0",
	)}
	files[premiumDailyPath] = archiveFile{csv: csvLines(
		klineHeader,
		"1735689600000,-0.00029091,0.00000412,-0.00031979,0.00000412,0,1735689659999,0,12,0,0,0",
		"1735689660000,-0.00013573,-0.00007668,-0.00031988,-0.00014258,0,1735689719999,0,12,0,0,0",
	)}
	server, sender := newArchiveServer(t, files)
	client := newTestClient(t, server.URL, sender, 0)
	ctx := context.Background()

	mark, err := client.MarkPriceKlines(ctx, MarkPriceKlinesRequest{
		Symbol: "BTCUSDT", Interval: binance.Kline1Hour, Year: 2024, Month: time.December,
	}, trade.WithEgressRoute("route-b"))
	if err != nil {
		t.Fatalf("MarkPriceKlines() error = %v", err)
	}
	rows := readAllKlines(t, mark)
	wantMark := binance.Kline{
		OpenTime: 1_733_011_200_000, Open: "96462.18661702", High: "96743.09743262", Low: "96316.50000000",
		Close: "96392.91542920", Volume: "0", CloseTime: 1_733_014_799_999, QuoteAssetVolume: "0", TradeCount: 3600,
		TakerBuyBaseAssetVolume: "0", TakerBuyQuoteAssetVolume: "0",
	}
	if len(rows) != 2 || rows[0] != wantMark || rows[1].Low != "96100" || rows[1].OpenTime != 1_733_014_800_000 {
		t.Fatalf("mark price klines = %+v", rows)
	}

	index, err := client.IndexPriceKlines(ctx, IndexPriceKlinesRequest{
		Symbol: "BTCUSDT", Interval: binance.Kline1Hour, Year: 2024, Month: time.December,
	})
	if err != nil {
		t.Fatalf("IndexPriceKlines() error = %v", err)
	}
	rows = readAllKlines(t, index)
	wantIndex := binance.Kline{
		OpenTime: 1_733_011_200_000, Open: "96401.97851064", High: "96662.14021277", Low: "96266.32936170",
		Close: "96331.51042553", Volume: "0", CloseTime: 1_733_014_799_999, QuoteAssetVolume: "0", TradeCount: 3600,
		TakerBuyBaseAssetVolume: "0", TakerBuyQuoteAssetVolume: "0",
	}
	if len(rows) != 1 || rows[0] != wantIndex {
		t.Fatalf("index price klines = %+v", rows)
	}

	premium, err := client.PremiumIndexKlines(ctx, PremiumIndexKlinesRequest{
		Symbol: "BTCUSDT", Interval: binance.Kline1Hour, Year: 2024, Month: time.December,
	})
	if err != nil {
		t.Fatalf("PremiumIndexKlines() error = %v", err)
	}
	rows = readAllKlines(t, premium)
	wantPremium := binance.Kline{
		OpenTime: 1_733_011_200_000, Open: "0.00075846", High: "0.00112964", Low: "0.00036845",
		Close: "0.00074372", Volume: "0", CloseTime: 1_733_014_799_999, QuoteAssetVolume: "0", TradeCount: 720,
		TakerBuyBaseAssetVolume: "0", TakerBuyQuoteAssetVolume: "0",
	}
	if len(rows) != 1 || rows[0] != wantPremium {
		t.Fatalf("premium index klines = %+v", rows)
	}

	premiumDaily, err := client.PremiumIndexKlines(ctx, PremiumIndexKlinesRequest{
		Symbol: "BTCUSDT", Interval: binance.Kline1Minute, Year: 2025, Month: time.January, Day: 1,
	})
	if err != nil {
		t.Fatalf("PremiumIndexKlines(daily) error = %v", err)
	}
	rows = readAllKlines(t, premiumDaily)
	wantNegative := binance.Kline{
		OpenTime: 1_735_689_600_000, Open: "-0.00029091", High: "0.00000412", Low: "-0.00031979",
		Close: "0.00000412", Volume: "0", CloseTime: 1_735_689_659_999, QuoteAssetVolume: "0", TradeCount: 12,
		TakerBuyBaseAssetVolume: "0", TakerBuyQuoteAssetVolume: "0",
	}
	if len(rows) != 2 || rows[0] != wantNegative || rows[1].High != "-0.00007668" {
		t.Fatalf("negative premium index klines = %+v", rows)
	}

	routes, paths := sender.snapshot()
	wantPaths := []string{
		markPath + ".CHECKSUM", markPath, indexPath + ".CHECKSUM", indexPath,
		premiumPath + ".CHECKSUM", premiumPath, premiumDailyPath + ".CHECKSUM", premiumDailyPath,
	}
	if len(paths) != len(wantPaths) || len(routes) != len(wantPaths) || routes[0] != "route-b" || routes[2] != "route-a" {
		t.Fatalf("requests = %v %v", routes, paths)
	}
	for index, want := range wantPaths {
		if paths[index] != want {
			t.Fatalf("request %d path = %q, want %q", index, paths[index], want)
		}
	}
}

func TestPriceKlinesRejectNegativesOutsidePremiumIndexPrices(t *testing.T) {
	t.Parallel()

	markPath := "/data/futures/um/monthly/markPriceKlines/BTCUSDT/1h/BTCUSDT-1h-2024-12.zip"
	premiumPath := "/data/futures/um/monthly/premiumIndexKlines/BTCUSDT/1h/BTCUSDT-1h-2024-12.zip"
	files := map[string]archiveFile{}
	files[markPath] = archiveFile{csv: csvLines(
		"1733011200000,-1.5,2,1,1,0,1733014799999,0,3600,0,0,0",
	)}
	files[premiumPath] = archiveFile{csv: csvLines(
		"1733011200000,-1.5,2,-1,1,-3,1733014799999,0,720,0,0,0",
	)}
	server, sender := newArchiveServer(t, files)
	client := newTestClient(t, server.URL, sender, 0)
	ctx := context.Background()

	mark, err := client.MarkPriceKlines(ctx, MarkPriceKlinesRequest{
		Symbol: "BTCUSDT", Interval: binance.Kline1Hour, Year: 2024, Month: time.December,
	})
	if err != nil {
		t.Fatalf("MarkPriceKlines() error = %v", err)
	}
	defer mark.Close()
	if _, err := mark.Next(); err == nil || !strings.Contains(err.Error(), `invalid decimal "-1.5" in column 2`) ||
		!strings.Contains(err.Error(), "line 1") {
		t.Fatalf("mark price negative open error = %v", err)
	}

	premium, err := client.PremiumIndexKlines(ctx, PremiumIndexKlinesRequest{
		Symbol: "BTCUSDT", Interval: binance.Kline1Hour, Year: 2024, Month: time.December,
	})
	if err != nil {
		t.Fatalf("PremiumIndexKlines() error = %v", err)
	}
	defer premium.Close()
	if _, err := premium.Next(); err == nil || !strings.Contains(err.Error(), `invalid decimal "-3" in column 6`) {
		t.Fatalf("premium index negative volume error = %v", err)
	}
}

func TestTradeReadersRejectMalformedRows(t *testing.T) {
	t.Parallel()

	aggregateCases := []struct {
		name string
		row  string
		want string
	}{
		{"buyer maker flag", "1,0.1,1,1,1,1735689623270289,maybe,True", `invalid buyer maker flag "maybe"`},
		{"best match flag", "1,0.1,1,1,1,1735689623270289,True,2", `invalid best match flag "2"`},
		{"price", "1,abc,1,1,1,1735689623270289,True,True", `invalid price "abc"`},
		{"exponent range", "1,1E99,1,1,1,1735689623270289,True,True", `invalid price "1E99"`},
		{"negative ID", "-1,0.1,1,1,1,1735689623270289,True,True", `invalid aggregate trade ID "-1"`},
		{"trade time", "1,0.1,1,1,1,12:00,True,True", `invalid trade time "12:00"`},
		{"field count", "1,0.1,1,1,1,1735689623270289,True", "wrong number of fields"},
	}
	tradeCases := []struct {
		name string
		row  string
		want string
	}{
		{"buyer maker flag", "1,0.1,1,0.1,1735689623270289,maybe,True", `invalid buyer maker flag "maybe"`},
		{"quote quantity", "1,0.1,1,-0.1,1735689623270289,True,True", `invalid quote quantity "-0.1"`},
		{"quantity", "1,0.1,1e,0.1,1735689623270289,True,True", `invalid quantity "1e"`},
		{"field count", "1,0.1,1,0.1,1735689623270289,True,True,True", "wrong number of fields"},
	}
	files := map[string]archiveFile{}
	for index, test := range aggregateCases {
		filePath := fmt.Sprintf("/data/spot/daily/aggTrades/BTCUSDT/BTCUSDT-aggTrades-2025-01-%02d.zip", index+1)
		files[filePath] = archiveFile{csv: csvLines(test.row)}
	}
	for index, test := range tradeCases {
		filePath := fmt.Sprintf("/data/spot/daily/trades/BTCUSDT/BTCUSDT-trades-2025-01-%02d.zip", index+1)
		files[filePath] = archiveFile{csv: csvLines(test.row)}
	}
	server, sender := newArchiveServer(t, files)
	client := newTestClient(t, server.URL, sender, 0)
	ctx := context.Background()

	for index, test := range aggregateCases {
		reader, err := client.AggTrades(ctx, AggTradesRequest{
			Market: MarketSpot, Symbol: "BTCUSDT", Year: 2025, Month: time.January, Day: index + 1,
		})
		if err != nil {
			t.Fatalf("%s: AggTrades() error = %v", test.name, err)
		}
		_, err = reader.Next()
		_ = reader.Close()
		if err == nil || !strings.Contains(err.Error(), test.want) || !strings.Contains(err.Error(), "line 1") {
			t.Fatalf("%s: aggregate trade Next() error = %v, want %q on line 1", test.name, err, test.want)
		}
	}
	for index, test := range tradeCases {
		reader, err := client.Trades(ctx, TradesRequest{
			Market: MarketSpot, Symbol: "BTCUSDT", Year: 2025, Month: time.January, Day: index + 1,
		})
		if err != nil {
			t.Fatalf("%s: Trades() error = %v", test.name, err)
		}
		_, err = reader.Next()
		_ = reader.Close()
		if err == nil || !strings.Contains(err.Error(), test.want) || !strings.Contains(err.Error(), "line 1") {
			t.Fatalf("%s: trade Next() error = %v, want %q on line 1", test.name, err, test.want)
		}
	}
}

func TestDatasetFileErrors(t *testing.T) {
	t.Parallel()

	corruptPath := "/data/spot/monthly/trades/BTCUSDT/BTCUSDT-trades-2017-08.zip"
	server, sender := newArchiveServer(t, map[string]archiveFile{corruptPath: {
		csv: csvLines("0,0.1,1,0.1,1502942400000,True,True"), checksum: strings.Repeat("0", 64),
	}})
	client := newTestClient(t, server.URL, sender, 0)
	ctx := context.Background()

	_, err := client.Trades(ctx, TradesRequest{Market: MarketSpot, Symbol: "BTCUSDT", Year: 2017, Month: time.August})
	if !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("Trades() checksum error = %v", err)
	}

	missing := []struct {
		name string
		call func() error
	}{
		{"mark price klines", func() error {
			_, err := client.MarkPriceKlines(ctx, MarkPriceKlinesRequest{
				Symbol: "BTCUSDT", Interval: binance.Kline1Minute, Year: 2024, Month: time.January,
			})
			return err
		}},
		{"index price klines", func() error {
			_, err := client.IndexPriceKlines(ctx, IndexPriceKlinesRequest{
				Symbol: "BTCUSDT", Interval: binance.Kline1Minute, Year: 2024, Month: time.January,
			})
			return err
		}},
		{"premium index klines", func() error {
			_, err := client.PremiumIndexKlines(ctx, PremiumIndexKlinesRequest{
				Symbol: "BTCUSDT", Interval: binance.Kline1Minute, Year: 2024, Month: time.January,
			})
			return err
		}},
		{"aggregate trades", func() error {
			_, err := client.AggTrades(ctx, AggTradesRequest{
				Market: MarketUSDM, Symbol: "BTCUSDT", Year: 2024, Month: time.January,
			})
			return err
		}},
		{"trades", func() error {
			_, err := client.Trades(ctx, TradesRequest{
				Market: MarketUSDM, Symbol: "BTCUSDT", Year: 2024, Month: time.January, Day: 5,
			})
			return err
		}},
	}
	for _, test := range missing {
		if err := test.call(); !errors.Is(err, ErrFileNotFound) || !errors.Is(err, trade.ErrUnsupportedCapability) {
			t.Fatalf("%s missing file error = %v", test.name, err)
		}
	}
}

func TestDatasetRequestPaths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path func() (string, error)
		want string
	}{
		{
			AggTradesRequest{Market: MarketSpot, Symbol: "BTCUSDT", Year: 2025, Month: time.January}.path,
			"data/spot/monthly/aggTrades/BTCUSDT/BTCUSDT-aggTrades-2025-01.zip",
		},
		{
			AggTradesRequest{Market: MarketSpot, Symbol: "BTCUSDT", Year: 2026, Month: time.November, Day: 16}.path,
			"data/spot/daily/aggTrades/BTCUSDT/BTCUSDT-aggTrades-2026-11-16.zip",
		},
		{
			AggTradesRequest{Market: MarketUSDM, Symbol: "ETHUSDT", Year: 2024, Month: time.February}.path,
			"data/futures/um/monthly/aggTrades/ETHUSDT/ETHUSDT-aggTrades-2024-02.zip",
		},
		{
			TradesRequest{Market: MarketSpot, Symbol: "币安人生USDT", Year: 2026, Month: time.November, Day: 16}.path,
			"data/spot/daily/trades/币安人生USDT/币安人生USDT-trades-2026-11-16.zip",
		},
		{
			TradesRequest{Market: MarketUSDM, Symbol: "BTCUSDT", Year: 2019, Month: time.September}.path,
			"data/futures/um/monthly/trades/BTCUSDT/BTCUSDT-trades-2019-09.zip",
		},
		{
			MarkPriceKlinesRequest{Symbol: "BTCUSDT", Interval: binance.Kline1Month, Year: 2023, Month: time.June}.path,
			"data/futures/um/monthly/markPriceKlines/BTCUSDT/1mo/BTCUSDT-1mo-2023-06.zip",
		},
		{
			MarkPriceKlinesRequest{
				Symbol: "BTCUSDT", Interval: binance.Kline1Minute, Year: 2024, Month: time.February, Day: 29,
			}.path,
			"data/futures/um/daily/markPriceKlines/BTCUSDT/1m/BTCUSDT-1m-2024-02-29.zip",
		},
		{
			IndexPriceKlinesRequest{
				Symbol: "ETHUSDT", Interval: binance.Kline1Day, Year: 2025, Month: time.January, Day: 1,
			}.path,
			"data/futures/um/daily/indexPriceKlines/ETHUSDT/1d/ETHUSDT-1d-2025-01-01.zip",
		},
		{
			PremiumIndexKlinesRequest{Symbol: "BTCUSDT", Interval: binance.Kline3Days, Year: 2023, Month: time.June}.path,
			"data/futures/um/monthly/premiumIndexKlines/BTCUSDT/3d/BTCUSDT-3d-2023-06.zip",
		},
	}
	for _, test := range tests {
		if got, err := test.path(); err != nil || got != test.want {
			t.Fatalf("path() = %q, %v, want %q", got, err, test.want)
		}
	}
}

func TestDatasetRequestValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path func() (string, error)
	}{
		{"agg trades market", AggTradesRequest{Market: "options", Symbol: "BTCUSDT", Year: 2024, Month: 1}.path},
		{"agg trades symbol", AggTradesRequest{Market: MarketSpot, Symbol: "btc-usdt", Year: 2024, Month: 1}.path},
		{"agg trades path symbol", AggTradesRequest{Market: MarketSpot, Symbol: "BTC/../ETH", Year: 2024, Month: 1}.path},
		{"agg trades year", AggTradesRequest{Market: MarketSpot, Symbol: "BTCUSDT", Year: 2016, Month: 1}.path},
		{"agg trades month", AggTradesRequest{Market: MarketSpot, Symbol: "BTCUSDT", Year: 2024, Month: 13}.path},
		{"agg trades day", AggTradesRequest{Market: MarketSpot, Symbol: "BTCUSDT", Year: 2024, Month: 2, Day: 30}.path},
		{"trades market", TradesRequest{Market: "futures/cm", Symbol: "BTCUSDT", Year: 2024, Month: 1}.path},
		{"trades escape", TradesRequest{Market: MarketUSDM, Symbol: "BTC%2FETH", Year: 2024, Month: 1}.path},
		{"trades negative day", TradesRequest{Market: MarketUSDM, Symbol: "BTCUSDT", Year: 2024, Month: 2, Day: -1}.path},
		{"mark symbol", MarkPriceKlinesRequest{Symbol: "", Interval: binance.Kline1Minute, Year: 2024, Month: 1}.path},
		{"mark interval", MarkPriceKlinesRequest{Symbol: "BTCUSDT", Interval: "7m", Year: 2024, Month: 1}.path},
		{"mark 1s", MarkPriceKlinesRequest{Symbol: "BTCUSDT", Interval: binance.Kline1Second, Year: 2024, Month: 1}.path},
		{"index 1s", IndexPriceKlinesRequest{Symbol: "BTCUSDT", Interval: binance.Kline1Second, Year: 2024, Month: 1}.path},
		{"premium month", PremiumIndexKlinesRequest{Symbol: "BTCUSDT", Interval: binance.Kline1Hour, Year: 2024}.path},
		{
			"mark daily 3d",
			MarkPriceKlinesRequest{Symbol: "BTCUSDT", Interval: binance.Kline3Days, Year: 2024, Month: 1, Day: 5}.path,
		},
		{
			"index daily 1w",
			IndexPriceKlinesRequest{Symbol: "BTCUSDT", Interval: binance.Kline1Week, Year: 2024, Month: 1, Day: 8}.path,
		},
		{
			"premium daily 1mo",
			PremiumIndexKlinesRequest{Symbol: "BTCUSDT", Interval: binance.Kline1Month, Year: 2024, Month: 1, Day: 1}.path,
		},
	}
	for _, test := range tests {
		if got, err := test.path(); !errors.Is(err, trade.ErrValidation) {
			t.Fatalf("%s: path() = %q, %v, want ErrValidation", test.name, got, err)
		}
	}
}

func TestDatasetValidationErrorsDoNotReachNetwork(t *testing.T) {
	t.Parallel()

	server, sender := newArchiveServer(t, nil)
	client := newTestClient(t, server.URL, sender, 0)
	ctx := context.Background()
	calls := []struct {
		name string
		call func() error
	}{
		{"mark price klines", func() error {
			_, err := client.MarkPriceKlines(ctx, MarkPriceKlinesRequest{
				Symbol: "BTCUSDT", Interval: binance.Kline1Second, Year: 2024, Month: time.January,
			})
			return err
		}},
		{"index price klines", func() error {
			_, err := client.IndexPriceKlines(ctx, IndexPriceKlinesRequest{
				Symbol: "BTCUSDT", Interval: binance.Kline1Week, Year: 2024, Month: time.January, Day: 8,
			})
			return err
		}},
		{"premium index klines", func() error {
			_, err := client.PremiumIndexKlines(ctx, PremiumIndexKlinesRequest{
				Symbol: "btcusdt", Interval: binance.Kline1Hour, Year: 2024, Month: time.January,
			})
			return err
		}},
		{"aggregate trades", func() error {
			_, err := client.AggTrades(ctx, AggTradesRequest{
				Market: "options", Symbol: "BTCUSDT", Year: 2024, Month: time.January,
			})
			return err
		}},
		{"trades", func() error {
			_, err := client.Trades(ctx, TradesRequest{
				Market: MarketSpot, Symbol: "BTCUSDT", Year: 2024, Month: time.February, Day: 30,
			})
			return err
		}},
	}
	for _, test := range calls {
		if err := test.call(); !errors.Is(err, trade.ErrValidation) {
			t.Fatalf("%s error = %v, want ErrValidation", test.name, err)
		}
	}
	if _, paths := sender.snapshot(); len(paths) != 0 {
		t.Fatalf("validation errors reached the network: %v", paths)
	}
}

func TestPlainDecimal(t *testing.T) {
	t.Parallel()

	valid := map[string]string{
		"93548.8":     "93548.8",
		"184833.0":    "184833.0",
		"0.00120300":  "0.00120300",
		"7.72E-6":     "0.00000772",
		"1.785E-4":    "0.0001785",
		"3.08e-5":     "0.0000308",
		"1.0E-5":      "0.000010",
		"5.5738058E7": "55738058",
		"1.5E7":       "15000000",
		"2E+3":        "2000",
		"1.25E1":      "12.5",
		"100E-2":      "1.00",
		"0.5E1":       "5",
		"0E5":         "0",
	}
	for value, want := range valid {
		if got, err := plainDecimal("price", value); err != nil || got != want {
			t.Fatalf("plainDecimal(%q) = %q, %v, want %q", value, got, err, want)
		}
	}
	for _, value := range []string{"", "abc", "1e", "E5", "-1", ".5", "1.", "1E99", "1E-99", "1.5E+", "1 E5", "0x10"} {
		if got, err := plainDecimal("price", value); err == nil {
			t.Fatalf("plainDecimal(%q) = %q, error = nil", value, got)
		}
	}
}
