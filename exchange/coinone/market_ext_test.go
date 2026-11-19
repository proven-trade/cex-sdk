package coinone

import (
	"context"
	"errors"
	"testing"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
)

func TestRangeUnitsDecodesPriceUnitTable(t *testing.T) {
	t.Parallel()

	server := newExtServer(t, successReply(`"range_price_units":[{"range_min":0,"next_range_min":1,"price_unit":0.0001},{"range_min":1,"next_range_min":10,"price_unit":0.001},{"range_min":1000,"next_range_min":5000,"price_unit":1}]`))
	provider := &recordingProvider{}
	client, limiter := newExtTestClient(t, server.URL, &directSender{}, provider, credential.PermissionRead)

	units, err := client.RangeUnits(context.Background(), RangeUnitsRequest{QuoteCurrency: "KRW", TargetCurrency: "BTC"})
	if err != nil {
		t.Fatalf("RangeUnits() error = %v", err)
	}
	assertPublicRequest(t, server.single(t), "/public/v2/range_units/KRW/BTC", "")
	if len(units) != 3 || units[0].RangeMin != "0" || units[0].NextRangeMin != "1" || units[0].PriceUnit != "0.0001" ||
		units[2].RangeMin != "1000" || units[2].PriceUnit != "1" || len(units[0].Raw) == 0 {
		t.Fatalf("RangeUnits() = %+v", units)
	}
	if calls, _, _ := provider.snapshot(); calls != 0 {
		t.Fatalf("public request resolved credentials %d times", calls)
	}
	assertLimiterUsed(t, limiter, extPublicKey, 1)
	assertLimiterUnknown(t, limiter, extPrivateKey)
}

func TestMarketReturnsSingleMarket(t *testing.T) {
	t.Parallel()

	market := `{"quote_currency":"KRW","target_currency":"BTC","price_unit":"100.0","qty_unit":"0.0001","max_order_amount":"1000000000.0","max_price":"1000000000000.0","max_qty":"100000000.0","min_order_amount":"5000.0","min_price":"1000.0","min_qty":"0.00000001","order_book_units":["0.0","10000.0","100000.0"],"maintenance_status":0,"trade_status":1,"order_types":["limit","market"]}`
	server := newExtServer(
		t,
		successReply(`"server_time":1416895635000,"markets":[`+market+`]`),
		successReply(`"server_time":1416895635000,"markets":[`+market+`,`+market+`]`),
	)
	client, _ := newExtTestClient(t, server.URL, &directSender{}, &recordingProvider{}, credential.PermissionRead)
	request := MarketRequest{QuoteCurrency: "KRW", TargetCurrency: "BTC"}

	result, err := client.Market(context.Background(), request)
	if err != nil {
		t.Fatalf("Market() error = %v", err)
	}
	if result.QuoteCurrency != "KRW" || result.TargetCurrency != "BTC" || result.MinimumAmount != "5000.0" ||
		result.TradeState != 1 || len(result.OrderBookUnits) != 3 || len(result.Raw) == 0 {
		t.Fatalf("Market() = %+v", result)
	}
	_, err = client.Market(context.Background(), request)
	if !errors.Is(err, trade.ErrExchangeUnavailable) {
		t.Fatalf("Market() with two items error = %v, want ErrExchangeUnavailable", err)
	}
	requests := server.received()
	if len(requests) != 2 {
		t.Fatalf("requests = %+v, want two", requests)
	}
	for _, got := range requests {
		assertPublicRequest(t, got, "/public/v2/markets/KRW/BTC", "")
	}
}

func TestTickersAndUTCTickers(t *testing.T) {
	t.Parallel()

	btc := `{"quote_currency":"KRW","target_currency":"BTC","timestamp":1499341142000,"high":"3845000.0","low":"3819000.0","first":"3825000.0","last":"3833000.0","quote_volume":"10000.0","target_volume":"163.3828","best_asks":[{"price":"1200.0","qty":"1.234"}],"best_bids":[{"price":"1000.0","qty":"0.123"}],"id":"1499341142000001","yesterday_last":"3820000.0"}`
	eth := `{"quote_currency":"KRW","target_currency":"ETH","timestamp":1499341142000,"last":"250000.0","id":"1499341142000002"}`
	server := newExtServer(
		t,
		successReply(`"server_time":1416895635000,"tickers":[`+btc+`,`+eth+`]`),
		successReply(`"server_time":1416895635000,"tickers":[`+btc+`,`+eth+`]`),
		successReply(`"server_time":1416895635000,"tickers":[`+btc+`]`),
		successReply(`"server_time":1416895635000,"tickers":[`+btc+`,`+eth+`]`),
	)
	client, limiter := newExtTestClient(t, server.URL, &directSender{}, &recordingProvider{}, credential.PermissionRead)
	ctx := context.Background()

	all, err := client.Tickers(ctx, TickersRequest{QuoteCurrency: "KRW", AdditionalData: true})
	if err != nil || len(all) != 2 || all[0].Last != "3833000.0" || all[0].YesterdayLast != "3820000.0" ||
		all[0].BestAsks[0].Price != "1200.0" || all[1].TargetCurrency != "ETH" || len(all[1].Raw) == 0 {
		t.Fatalf("Tickers() = %+v, error = %v", all, err)
	}
	utcAll, err := client.UTCTickers(ctx, TickersRequest{QuoteCurrency: "KRW"})
	if err != nil || len(utcAll) != 2 || utcAll[0].BestBids[0].Quantity != "0.123" {
		t.Fatalf("UTCTickers() = %+v, error = %v", utcAll, err)
	}
	utcOne, err := client.UTCTicker(ctx, TickerRequest{QuoteCurrency: "KRW", TargetCurrency: "BTC", AdditionalData: true})
	if err != nil || utcOne.TargetCurrency != "BTC" || utcOne.High != "3845000.0" || len(utcOne.Raw) == 0 {
		t.Fatalf("UTCTicker() = %+v, error = %v", utcOne, err)
	}
	if _, err = client.UTCTicker(ctx, TickerRequest{QuoteCurrency: "KRW", TargetCurrency: "BTC"}); !errors.Is(err, trade.ErrExchangeUnavailable) {
		t.Fatalf("UTCTicker() with two items error = %v, want ErrExchangeUnavailable", err)
	}

	requests := server.received()
	if len(requests) != 4 {
		t.Fatalf("requests = %+v, want four", requests)
	}
	assertPublicRequest(t, requests[0], "/public/v2/ticker_new/KRW", "additional_data=true")
	assertPublicRequest(t, requests[1], "/public/v2/ticker_utc_new/KRW", "")
	assertPublicRequest(t, requests[2], "/public/v2/ticker_utc_new/KRW/BTC", "additional_data=true")
	assertPublicRequest(t, requests[3], "/public/v2/ticker_utc_new/KRW/BTC", "")
	assertLimiterUsed(t, limiter, extPublicKey, 4)
}

func TestCurrenciesAndCurrency(t *testing.T) {
	t.Parallel()

	bitcoin := `{"name":"Bitcoin","symbol":"BTC","deposit_status":"normal","withdraw_status":"normal","deposit_confirm_count":2,"max_precision":8,"deposit_fee":"0.0","withdrawal_min_amount":"0.0001","withdrawal_fee":"0.0015"}`
	ripple := `{"name":"Ripple","symbol":"XRP","deposit_status":"suspended","withdraw_status":"normal","deposit_confirm_count":1,"max_precision":6,"deposit_fee":"0.0","withdrawal_min_amount":"20","withdrawal_fee":"1"}`
	server := newExtServer(
		t,
		successReply(`"server_time":1644541746590,"currencies":[`+bitcoin+`,`+ripple+`]`),
		successReply(`"server_time":1644541746590,"currencies":[`+ripple+`]`),
		successReply(`"server_time":1644541746590,"currencies":[]`),
	)
	client, limiter := newExtTestClient(t, server.URL, &directSender{}, &recordingProvider{}, credential.PermissionRead)
	ctx := context.Background()

	currencies, err := client.Currencies(ctx)
	if err != nil || len(currencies) != 2 {
		t.Fatalf("Currencies() = %+v, error = %v", currencies, err)
	}
	first, second := currencies[0], currencies[1]
	if first.Name != "Bitcoin" || first.Symbol != "BTC" || first.DepositStatus != "normal" || first.WithdrawStatus != "normal" ||
		first.DepositConfirmCount != 2 || first.MaxPrecision != 8 || first.DepositFee != "0.0" ||
		first.WithdrawalMinimumAmount != "0.0001" || first.WithdrawalFee != "0.0015" || len(first.Raw) == 0 {
		t.Fatalf("Currencies()[0] = %+v", first)
	}
	if second.Symbol != "XRP" || second.DepositStatus != "suspended" || second.MaxPrecision != 6 || second.WithdrawalFee != "1" {
		t.Fatalf("Currencies()[1] = %+v", second)
	}
	currency, err := client.Currency(ctx, CurrencyRequest{Currency: "XRP"})
	if err != nil || currency.Symbol != "XRP" || currency.WithdrawalMinimumAmount != "20" || len(currency.Raw) == 0 {
		t.Fatalf("Currency() = %+v, error = %v", currency, err)
	}
	if _, err = client.Currency(ctx, CurrencyRequest{Currency: "XRP"}); !errors.Is(err, trade.ErrExchangeUnavailable) {
		t.Fatalf("Currency() with no item error = %v, want ErrExchangeUnavailable", err)
	}

	requests := server.received()
	if len(requests) != 3 {
		t.Fatalf("requests = %+v, want three", requests)
	}
	assertPublicRequest(t, requests[0], "/public/v2/currencies", "")
	assertPublicRequest(t, requests[1], "/public/v2/currencies/XRP", "")
	assertPublicRequest(t, requests[2], "/public/v2/currencies/XRP", "")
	assertLimiterUsed(t, limiter, extPublicKey, 3)
}

func TestMarketExtensionsRejectInvalidRequestsBeforeSending(t *testing.T) {
	t.Parallel()

	server := newExtServer(t)
	client, _ := newExtTestClient(t, server.URL, &directSender{}, &recordingProvider{}, credential.PermissionRead)
	ctx := context.Background()

	_, rangeErr := client.RangeUnits(ctx, RangeUnitsRequest{QuoteCurrency: "KRW", TargetCurrency: "KRW"})
	_, marketErr := client.Market(ctx, MarketRequest{QuoteCurrency: "krw", TargetCurrency: "BTC"})
	_, tickersErr := client.Tickers(ctx, TickersRequest{})
	_, utcTickersErr := client.UTCTickers(ctx, TickersRequest{QuoteCurrency: "KRW/BTC"})
	_, utcTickerErr := client.UTCTicker(ctx, TickerRequest{QuoteCurrency: "KRW"})
	_, currencyErr := client.Currency(ctx, CurrencyRequest{Currency: "btc"})
	for index, err := range []error{rangeErr, marketErr, tickersErr, utcTickersErr, utcTickerErr, currencyErr} {
		if !errors.Is(err, trade.ErrValidation) {
			t.Fatalf("validation error %d = %v, want ErrValidation", index, err)
		}
	}
	if requests := server.received(); len(requests) != 0 {
		t.Fatalf("requests = %+v, want none", requests)
	}
}
