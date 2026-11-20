package bitget

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
	"github.com/proven-trade/cex-sdk/model"
	"github.com/proven-trade/cex-sdk/ratelimit"
	"github.com/proven-trade/cex-sdk/transport"
)

type seenRequest struct {
	method string
	path   string
	query  string
	body   string
}

// newExtClient는 지정 권한을 가진 클라이언트와 요청 기록 서버를 만든다.
func newExtClient(t *testing.T, permissions ...credential.Permission) (*Client, *[]seenRequest) {
	t.Helper()
	fixedNow := time.UnixMilli(1_700_000_000_000)
	var seen []seenRequest
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		request.Body = io.NopCloser(bytes.NewReader(body))
		seen = append(seen, seenRequest{request.Method, request.URL.Path, request.URL.RawQuery, string(body)})
		if request.Header.Get("ACCESS-KEY") != "" && !verifySignedRequest(t, request, []byte("test-secret"), fixedNow) {
			http.Error(writer, `{"code":"40009","msg":"sign signature error"}`, http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/api/v3/market/time":
			_, _ = io.WriteString(writer, `{"code":"00000","msg":"success","requestTime":1700000000000,"data":{"serverTime":"1700000000123"}}`)
		case "/api/v3/trade/modify-order", "/api/v3/trade/place-strategy-order", "/api/v3/trade/modify-strategy-order":
			_, _ = io.WriteString(writer, `{"code":"00000","msg":"success","requestTime":1700000000000,"data":{"orderId":"77","clientOid":"c-77"}}`)
		case "/api/v3/trade/place-batch":
			_, _ = io.WriteString(writer, `{"code":"00000","msg":"success","requestTime":1700000000000,"data":[{"orderId":"1","clientOid":"a","code":"00000","msg":"success"},{"orderId":"","clientOid":"b","code":"43012","msg":"insufficient"}]}`)
		case "/api/v3/account/fail":
			http.Error(writer, `{"code":"43012","msg":"insufficient balance"}`, http.StatusBadRequest)
		default:
			_, _ = io.WriteString(writer, `{"code":"00000","msg":"success","requestTime":1700000000000,"data":{"ok":true}}`)
		}
	}))
	t.Cleanup(server.Close)

	limiter, err := ratelimit.New()
	if err != nil {
		t.Fatalf("ratelimit.New() error = %v", err)
	}
	executor, err := commonexchange.NewExecutor(commonexchange.ExecutorConfig{Sender: &directSender{}, Limiter: limiter})
	if err != nil {
		t.Fatalf("NewExecutor() error = %v", err)
	}
	client, err := New(Config{
		Executor: executor,
		Credentials: &credential.Descriptor{
			AccountID: "bitget-main", Exchange: model.ExchangeBitget, SecretRef: "secret/bitget/main",
			Permissions:           permissions,
			AllowedEgressRouteIDs: []transport.EgressRouteID{"route-a"},
		},
		CredentialProvider:   &recordingProvider{},
		DefaultEgressRouteID: "route-a",
		BaseURL:              server.URL,
		AllowInsecureHTTP:    true,
		Now:                  func() time.Time { return fixedNow },
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return client, &seen
}

func TestExtPublicMarketEndpoints(t *testing.T) {
	t.Parallel()
	client, seen := newExtClient(t, credential.PermissionRead)
	ctx := context.Background()

	cases := []struct {
		name  string
		call  func() error
		path  string
		query string
	}{
		{"time", func() error { _, err := client.ServerTime(ctx); return err }, "/api/v3/market/time", ""},
		{"rpi", func() error {
			_, err := client.RPIOrderBook(ctx, OrderBookRequest{Category: CategoryUSDTFutures, Symbol: "BTCUSDT", Limit: 5})
			return err
		}, "/api/v3/market/rpi-orderbook", "category=USDT-FUTURES&limit=5&symbol=BTCUSDT"},
		{"open interest", func() error {
			_, err := client.OpenInterest(ctx, OpenInterestRequest{Category: CategoryUSDTFutures, Symbol: "BTCUSDT"})
			return err
		}, "/api/v3/market/open-interest", "category=USDT-FUTURES&symbol=BTCUSDT"},
		{"current fund rate", func() error { _, err := client.CurrentFundingRate(ctx, "BTCUSDT"); return err },
			"/api/v3/market/current-fund-rate", "symbol=BTCUSDT"},
		{"history fund rate", func() error {
			_, err := client.FundingRateHistory(ctx, FundingRateHistoryRequest{
				Category: CategoryUSDTFutures, Symbol: "BTCUSDT", Cursor: "c1", Limit: 20,
			})
			return err
		}, "/api/v3/market/history-fund-rate", "category=USDT-FUTURES&cursor=c1&limit=20&symbol=BTCUSDT"},
		{"position tier", func() error {
			_, err := client.PositionTier(ctx, PositionTierRequest{Category: CategoryUSDTFutures, Symbol: "BTCUSDT"})
			return err
		}, "/api/v3/market/position-tier", "category=USDT-FUTURES&symbol=BTCUSDT"},
		{"oi limit", func() error {
			_, err := client.OpenInterestLimit(ctx, OpenInterestRequest{Category: CategoryUSDTFutures})
			return err
		}, "/api/v3/market/oi-limit", "category=USDT-FUTURES"},
		{"risk reserve", func() error {
			_, err := client.RiskReserve(ctx, RiskReserveRequest{Category: CategoryUSDTFutures, Symbol: "BTCUSDT", MarginCoin: "USDT"})
			return err
		}, "/api/v3/market/risk-reserve", "category=USDT-FUTURES&marginCoin=USDT&symbol=BTCUSDT"},
		{"risk reserve all", func() error {
			_, err := client.RiskReserveAll(ctx, RiskReserveRequest{Category: CategoryUSDTFutures})
			return err
		}, "/api/v3/market/risk-reserve-all", "category=USDT-FUTURES"},
		{"risk reserve hour", func() error {
			_, err := client.RiskReserveHourly(ctx, RiskReserveRequest{Category: CategoryUSDTFutures})
			return err
		}, "/api/v3/market/risk-reserve-hour", "category=USDT-FUTURES"},
		{"discount rate", func() error { _, err := client.DiscountRate(ctx); return err }, "/api/v3/market/discount-rate", ""},
		{"index components", func() error { _, err := client.IndexComponents(ctx, "BTCUSDT"); return err },
			"/api/v3/market/index-components", "symbol=BTCUSDT"},
		{"proof of reserves", func() error { _, err := client.ProofOfReserves(ctx); return err },
			"/api/v3/market/proof-of-reserves", ""},
		{"por", func() error { _, err := client.ProofOfReservesSummary(ctx); return err }, "/api/v3/market/por", ""},
	}
	for _, tc := range cases {
		before := len(*seen)
		if err := tc.call(); err != nil {
			t.Fatalf("%s error = %v", tc.name, err)
		}
		if len(*seen) != before+1 {
			t.Fatalf("%s sent %d requests", tc.name, len(*seen)-before)
		}
		got := (*seen)[before]
		if got.method != http.MethodGet || got.path != tc.path || got.query != tc.query {
			t.Errorf("%s request = %+v, want GET %s?%s", tc.name, got, tc.path, tc.query)
		}
	}

	data, err := client.ServerTime(ctx)
	if err != nil || string(data) != `{"serverTime":"1700000000123"}` {
		t.Fatalf("ServerTime() = %s, error = %v", data, err)
	}
}

func TestExtValidationRejectsBeforeSending(t *testing.T) {
	t.Parallel()
	client, seen := newExtClient(t, credential.PermissionRead, credential.PermissionTrade,
		credential.PermissionTransfer, credential.PermissionWithdraw)
	ctx := context.Background()
	longRange := time.UnixMilli(1_700_000_000_000)
	earlier := longRange.Add(-time.Hour)

	checks := map[string]func() error{
		"category":      func() error { _, err := client.OpenInterest(ctx, OpenInterestRequest{Category: "MARGIN"}); return err },
		"symbol":        func() error { _, err := client.CurrentFundingRate(ctx, ""); return err },
		"batch empty":   func() error { _, err := client.PlaceBatch(ctx, nil); return err },
		"batch too big": func() error { _, err := client.CancelBatch(ctx, make([]BatchCancelItem, 21)); return err },
		"batch tpsl": func() error {
			_, err := client.PlaceBatch(ctx, []PlaceOrderRequest{{
				Category: CategorySpot, Symbol: "BTCUSDT", Quantity: "1", Price: "1", Side: SideBuy,
				OrderType: OrderTypeLimit, StrategyPrices: StrategyPrices{TakeProfit: "2"},
			}})
			return err
		},
		"cancel both ids": func() error {
			_, err := client.CancelBatch(ctx, []BatchCancelItem{{OrderID: "1", ClientOrderID: "a", Category: CategorySpot}})
			return err
		},
		"modify nothing": func() error {
			_, err := client.ModifyOrder(ctx, ModifyOrderRequest{Category: CategorySpot, Symbol: "BTCUSDT", OrderID: "1"})
			return err
		},
		"modify duplicate": func() error {
			item := ModifyOrderRequest{Category: CategorySpot, Symbol: "BTCUSDT", OrderID: "1", Price: "2"}
			_, err := client.BatchModifyOrder(ctx, []ModifyOrderRequest{item, item})
			return err
		},
		"countdown negative": func() error { _, err := client.CountdownCancelAll(ctx, -1); return err },
		"close spot": func() error {
			_, err := client.ClosePositions(ctx, ClosePositionsRequest{Category: CategorySpot})
			return err
		},
		"leverage": func() error {
			_, err := client.SetLeverage(ctx, SetLeverageRequest{Category: CategoryUSDTFutures, Symbol: "BTCUSDT", Leverage: "0"})
			return err
		},
		"page range": func() error {
			_, err := client.FinancialRecords(ctx, FinancialRecordsRequest{
				Category: CategorySpot, Page: Page{StartTime: &longRange, EndTime: &earlier},
			})
			return err
		},
		"transfer amount": func() error {
			_, err := client.Transfer(ctx, TransferRequest{FromType: "a", ToType: "b", Coin: "USDT", Amount: "-1"})
			return err
		},
		"withdraw address": func() error {
			_, err := client.Withdraw(ctx, WithdrawalRequest{Coin: "USDT", TransferType: "on_chain", Size: "1"})
			return err
		},
		"strategy no tpsl": func() error {
			_, err := client.PlaceStrategyOrder(ctx, PlaceStrategyOrderRequest{
				Category: CategoryUSDTFutures, Symbol: "BTCUSDT", Type: "tpsl",
			})
			return err
		},
	}
	for name, check := range checks {
		if err := check(); !errors.Is(err, trade.ErrValidation) {
			t.Errorf("%s error = %v, want validation error", name, err)
		}
	}
	if len(*seen) != 0 {
		t.Fatalf("validation failures sent %d requests: %+v", len(*seen), *seen)
	}
}

func TestExtTradeMutationsBodyAndPermission(t *testing.T) {
	t.Parallel()
	client, seen := newExtClient(t, credential.PermissionRead, credential.PermissionTrade)
	ctx := context.Background()

	reference, err := client.ModifyOrder(ctx, ModifyOrderRequest{
		Category: CategoryUSDTFutures, Symbol: "BTCUSDT", OrderID: "77", Quantity: "0.02", Price: "64100", AutoCancel: "no",
	})
	if err != nil || reference.OrderID != "77" || reference.ClientOrderID != "c-77" || len(reference.Raw) == 0 {
		t.Fatalf("ModifyOrder() = %+v, error = %v", reference, err)
	}
	if _, err := client.CancelSymbolOrders(ctx, CancelSymbolOrdersRequest{Category: CategoryUSDTFutures, Symbol: "BTCUSDT"}); err != nil {
		t.Fatalf("CancelSymbolOrders() error = %v", err)
	}
	if _, err := client.CountdownCancelAll(ctx, 10); err != nil {
		t.Fatalf("CountdownCancelAll() error = %v", err)
	}
	if _, err := client.ClosePositions(ctx, ClosePositionsRequest{
		Category: CategoryUSDTFutures, Symbol: "BTCUSDT", PositionSide: PositionSideLong,
	}); err != nil {
		t.Fatalf("ClosePositions() error = %v", err)
	}
	if _, err := client.CancelBatch(ctx, []BatchCancelItem{{OrderID: "1", Category: CategorySpot, Symbol: "BTCUSDT"}}); err != nil {
		t.Fatalf("CancelBatch() error = %v", err)
	}
	if _, err := client.BatchModifyOrder(ctx, []ModifyOrderRequest{
		{Category: CategorySpot, Symbol: "BTCUSDT", ClientOrderID: "x", Price: "1"},
	}); err != nil {
		t.Fatalf("BatchModifyOrder() error = %v", err)
	}
	strategy, err := client.PlaceStrategyOrder(ctx, PlaceStrategyOrderRequest{
		Category: CategoryUSDTFutures, Symbol: "BTCUSDT", Type: "tpsl", PositionSide: PositionSideLong,
		StrategyPrices: StrategyPrices{TakeProfit: "70000", StopLoss: "60000", TakeProfitTrigger: "mark"},
	})
	if err != nil || strategy.OrderID != "77" {
		t.Fatalf("PlaceStrategyOrder() = %+v, error = %v", strategy, err)
	}
	if _, err := client.ModifyStrategyOrder(ctx, ModifyStrategyOrderRequest{
		OrderID: "77", StrategyPrices: StrategyPrices{StopLoss: "61000"},
	}); err != nil {
		t.Fatalf("ModifyStrategyOrder() error = %v", err)
	}
	if _, err := client.CancelStrategyOrder(ctx, CancelStrategyOrderRequest{ClientOrderID: "c-77"}); err != nil {
		t.Fatalf("CancelStrategyOrder() error = %v", err)
	}
	if _, err := client.SetLeverage(ctx, SetLeverageRequest{
		Category: CategoryUSDTFutures, Symbol: "BTCUSDT", Leverage: "10", PositionSide: PositionSideLong,
	}); err != nil {
		t.Fatalf("SetLeverage() error = %v", err)
	}
	if _, err := client.SetHoldMode(ctx, "hedge_mode"); err != nil {
		t.Fatalf("SetHoldMode() error = %v", err)
	}
	if _, err := client.AdjustAccountMode(ctx, AdjustAccountModeRequest{Mode: "advanced"}); err != nil {
		t.Fatalf("AdjustAccountMode() error = %v", err)
	}
	if _, err := client.SwitchDeduct(ctx, "on"); err != nil {
		t.Fatalf("SwitchDeduct() error = %v", err)
	}
	if _, err := client.Repay(ctx, RepayRequest{RepayableCoins: []string{"USDT"}, PaymentCoins: []string{"BTC"}}); err != nil {
		t.Fatalf("Repay() error = %v", err)
	}

	want := []seenRequest{
		{http.MethodPost, "/api/v3/trade/modify-order", "", `{"category":"USDT-FUTURES","symbol":"BTCUSDT","orderId":"77","qty":"0.02","price":"64100","autoCancel":"no"}`},
		{http.MethodPost, "/api/v3/trade/cancel-symbol-order", "", `{"category":"USDT-FUTURES","symbol":"BTCUSDT"}`},
		{http.MethodPost, "/api/v3/trade/countdown-cancel-all", "", `{"countdown":"10"}`},
		{http.MethodPost, "/api/v3/trade/close-positions", "", `{"category":"USDT-FUTURES","symbol":"BTCUSDT","posSide":"long"}`},
		{http.MethodPost, "/api/v3/trade/cancel-batch", "", `[{"orderId":"1","category":"SPOT","symbol":"BTCUSDT"}]`},
		{http.MethodPost, "/api/v3/trade/batch-modify-order", "", `[{"category":"SPOT","symbol":"BTCUSDT","clientOid":"x","price":"1"}]`},
		{http.MethodPost, "/api/v3/trade/place-strategy-order", "", `{"category":"USDT-FUTURES","symbol":"BTCUSDT","type":"tpsl","posSide":"long","takeProfit":"70000","stopLoss":"60000","tpTriggerBy":"mark"}`},
		{http.MethodPost, "/api/v3/trade/modify-strategy-order", "", `{"orderId":"77","stopLoss":"61000"}`},
		{http.MethodPost, "/api/v3/trade/cancel-strategy-order", "", `{"clientOid":"c-77"}`},
		{http.MethodPost, "/api/v3/account/set-leverage", "", `{"category":"USDT-FUTURES","symbol":"BTCUSDT","leverage":"10","posSide":"long"}`},
		{http.MethodPost, "/api/v3/account/set-hold-mode", "", `{"holdMode":"hedge_mode"}`},
		{http.MethodPost, "/api/v3/account/adjust-account-mode", "", `{"mode":"advanced"}`},
		{http.MethodPost, "/api/v3/account/switch-deduct", "", `{"deduct":"on"}`},
		{http.MethodPost, "/api/v3/account/repay", "", `{"repayableCoinList":["USDT"],"paymentCoinList":["BTC"]}`},
	}
	if len(*seen) != len(want) {
		t.Fatalf("recorded %d requests, want %d: %+v", len(*seen), len(want), *seen)
	}
	for index, expected := range want {
		if (*seen)[index] != expected {
			t.Errorf("request[%d] = %+v, want %+v", index, (*seen)[index], expected)
		}
	}

	// Trade 권한만 있는 키로는 Transfer/Withdraw 호출이 요청 전에 거부된다.
	before := len(*seen)
	if _, err := client.Transfer(ctx, TransferRequest{FromType: "uta", ToType: "spot", Coin: "USDT", Amount: "1"}); !isAuthorizationError(err) {
		t.Fatalf("Transfer() error = %v, want authorization error", err)
	}
	if _, err := client.Withdraw(ctx, WithdrawalRequest{
		Coin: "USDT", TransferType: "on_chain", Address: "addr", Size: "1",
	}); !isAuthorizationError(err) {
		t.Fatalf("Withdraw() error = %v, want authorization error", err)
	}
	if len(*seen) != before {
		t.Fatalf("unauthorized calls sent requests: %+v", (*seen)[before:])
	}
}

func isAuthorizationError(err error) bool {
	var apiError *trade.APIError
	return errors.As(err, &apiError) && apiError.Category == trade.ErrorAuthorization
}

func TestExtPlaceBatchDecodeAndPlaceOrderTPSL(t *testing.T) {
	t.Parallel()
	client, seen := newExtClient(t, credential.PermissionRead, credential.PermissionTrade)
	ctx := context.Background()

	data, err := client.PlaceBatch(ctx, []PlaceOrderRequest{
		{Category: CategorySpot, Symbol: "BTCUSDT", Quantity: "1", Price: "10", Side: SideBuy, OrderType: OrderTypeLimit, ClientOrderID: "a"},
		{Category: CategorySpot, Symbol: "ETHUSDT", Quantity: "1", Price: "10", Side: SideSell, OrderType: OrderTypeLimit, ClientOrderID: "b"},
	})
	if err != nil {
		t.Fatalf("PlaceBatch() error = %v", err)
	}
	wantBody := `[{"category":"SPOT","symbol":"BTCUSDT","qty":"1","price":"10","side":"buy","orderType":"limit","clientOid":"a"},` +
		`{"category":"SPOT","symbol":"ETHUSDT","qty":"1","price":"10","side":"sell","orderType":"limit","clientOid":"b"}]`
	if got := (*seen)[0]; got.path != "/api/v3/trade/place-batch" || got.body != wantBody {
		t.Fatalf("PlaceBatch request = %+v, want body %s", got, wantBody)
	}
	items, err := decodeItems[BatchResultForTest](data, nil)
	if err != nil || len(items) != 2 || items[0].Code != "00000" || items[1].Code != "43012" || items[1].ClientOrderID != "b" {
		t.Fatalf("batch result = %+v, error = %v", items, err)
	}

	// 단건 PlaceOrder는 TP/SL을 본문에 포함한다.
	if _, err := client.PlaceOrder(ctx, PlaceOrderRequest{
		Category: CategoryUSDTFutures, Symbol: "BTCUSDT", Quantity: "0.01", Price: "64000", Side: SideBuy,
		OrderType: OrderTypeLimit, StrategyPrices: StrategyPrices{TakeProfit: "70000", StopLoss: "60000", StopLossType: "market"},
	}); err != nil {
		t.Fatalf("PlaceOrder() error = %v", err)
	}
	last := (*seen)[len(*seen)-1]
	wantTPSL := `{"category":"USDT-FUTURES","symbol":"BTCUSDT","qty":"0.01","price":"64000","side":"buy","orderType":"limit","takeProfit":"70000","stopLoss":"60000","slOrderType":"market"}`
	if last.body != wantTPSL {
		t.Fatalf("PlaceOrder body = %s, want %s", last.body, wantTPSL)
	}
}

// BatchResultForTest는 일괄 응답 항목 decode 예시다.
type BatchResultForTest struct {
	OrderID       string `json:"orderId"`
	ClientOrderID string `json:"clientOid"`
	Code          string `json:"code"`
}

func TestExtReadEndpointsQueryAndPermission(t *testing.T) {
	t.Parallel()
	client, seen := newExtClient(t, credential.PermissionRead)
	ctx := context.Background()
	start := time.UnixMilli(1_700_000_000_000)
	end := start.Add(time.Hour)
	page := Page{StartTime: &start, EndTime: &end, Limit: 50, Cursor: "cur"}
	const pageQuery = "cursor=cur&endTime=1700003600000&limit=50&startTime=1700000000000"

	cases := []struct {
		name   string
		call   func() error
		method string
		path   string
		query  string
		body   string
	}{
		{"fills", func() error {
			_, err := client.Fills(ctx, FillsRequest{Category: CategorySpot, OrderID: "9", Page: page})
			return err
		}, "GET", "/api/v3/trade/fills", "category=SPOT&cursor=cur&endTime=1700003600000&limit=50&orderId=9&startTime=1700000000000", ""},
		{"unfilled strategy", func() error {
			_, err := client.UnfilledStrategyOrders(ctx, UnfilledStrategyOrdersRequest{Category: CategoryUSDTFutures, Type: "tpsl"})
			return err
		}, "GET", "/api/v3/trade/unfilled-strategy-orders", "category=USDT-FUTURES&type=tpsl", ""},
		{"history strategy", func() error {
			_, err := client.HistoryStrategyOrders(ctx, HistoryStrategyOrdersRequest{Category: CategoryUSDTFutures, Page: page})
			return err
		}, "GET", "/api/v3/trade/history-strategy-orders", "category=USDT-FUTURES&" + pageQuery, ""},
		{"account info", func() error { _, err := client.AccountInfo(ctx); return err }, "GET", "/api/v3/account/info", "", ""},
		{"settings", func() error { _, err := client.AccountSettings(ctx); return err }, "GET", "/api/v3/account/settings", "", ""},
		{"fee rate", func() error { _, err := client.FeeRate(ctx, CategorySpot, "BTCUSDT"); return err },
			"GET", "/api/v3/account/fee-rate", "category=SPOT&symbol=BTCUSDT", ""},
		{"all fee rate", func() error { _, err := client.AllFeeRates(ctx, CategoryUSDTFutures, ""); return err },
			"GET", "/api/v3/account/all-fee-rate", "category=USDT-FUTURES", ""},
		{"bills", func() error {
			_, err := client.FinancialRecords(ctx, FinancialRecordsRequest{Category: CategorySpot, Coin: "USDT", Page: page})
			return err
		}, "GET", "/api/v3/account/financial-records", "category=SPOT&coin=USDT&" + pageQuery, ""},
		{"max open available", func() error {
			_, err := client.MaxOpenAvailable(ctx, MaxOpenAvailableRequest{
				Category: CategoryUSDTFutures, Symbol: "BTCUSDT", OrderType: OrderTypeMarket, Side: SideBuy,
			})
			return err
		}, "POST", "/api/v3/account/max-open-available", "",
			`{"category":"USDT-FUTURES","symbol":"BTCUSDT","orderType":"market","side":"buy"}`},
		{"history position", func() error {
			_, err := client.HistoryPositions(ctx, CategoryUSDTFutures, "BTCUSDT", page)
			return err
		}, "GET", "/api/v3/position/history-position", "category=USDT-FUTURES&" + pageQuery + "&symbol=BTCUSDT", ""},
		{"adl rank", func() error { _, err := client.ADLRank(ctx); return err }, "GET", "/api/v3/position/adlRank", "", ""},
		{"funding assets", func() error { _, err := client.FundingAssets(ctx, "USDT"); return err },
			"GET", "/api/v3/account/funding-assets", "coin=USDT", ""},
		{"transferable coins", func() error { _, err := client.TransferableCoins(ctx, "uta", "spot"); return err },
			"GET", "/api/v3/account/transferable-coins", "fromType=uta&toType=spot", ""},
		{"deposit address", func() error { _, err := client.DepositAddress(ctx, "USDT", "trc20", ""); return err },
			"GET", "/api/v3/account/deposit-address", "chain=trc20&coin=USDT", ""},
		{"deposit records", func() error {
			_, err := client.DepositRecords(ctx, DepositRecordsRequest{Coin: "USDT", Page: page})
			return err
		}, "GET", "/api/v3/account/deposit-records", "coin=USDT&" + pageQuery, ""},
		{"withdrawal records", func() error {
			_, err := client.WithdrawalRecords(ctx, WithdrawalRecordsRequest{ClientOID: "w1"})
			return err
		}, "GET", "/api/v3/account/withdrawal-records", "clientOid=w1", ""},
		{"sub list", func() error { _, err := client.SubAccounts(ctx, Page{Limit: 10}); return err },
			"GET", "/api/v3/user/sub-list", "limit=10", ""},
		{"sub api keys", func() error { _, err := client.SubAPIKeys(ctx, "123", Page{}); return err },
			"GET", "/api/v3/user/sub-api-list", "subUid=123", ""},
	}
	for index, tc := range cases {
		if err := tc.call(); err != nil {
			t.Fatalf("%s error = %v", tc.name, err)
		}
		got := (*seen)[index]
		want := seenRequest{tc.method, tc.path, tc.query, tc.body}
		if got != want {
			t.Errorf("%s request = %+v, want %+v", tc.name, got, want)
		}
	}

	// Read 권한만 있으면 mutation은 요청 전에 거부된다.
	before := len(*seen)
	if _, err := client.SetLeverage(ctx, SetLeverageRequest{
		Category: CategoryUSDTFutures, Symbol: "BTCUSDT", Leverage: "5",
	}); !isAuthorizationError(err) {
		t.Fatalf("SetLeverage() error = %v, want authorization error", err)
	}
	if len(*seen) != before {
		t.Fatalf("unauthorized mutation sent requests")
	}
}

func TestExtTransferAndWithdrawUsePermissionsAndBodies(t *testing.T) {
	t.Parallel()
	client, seen := newExtClient(t, credential.PermissionRead, credential.PermissionTransfer, credential.PermissionWithdraw)
	ctx := context.Background()

	if _, err := client.Transfer(ctx, TransferRequest{FromType: "uta", ToType: "spot", Amount: "1.5", Coin: "USDT"}); err != nil {
		t.Fatalf("Transfer() error = %v", err)
	}
	if _, err := client.SubTransfer(ctx, SubTransferRequest{
		FromType: "uta", ToType: "uta", Amount: "2", Coin: "USDT", FromUserID: "1", ToUserID: "2", ClientOID: "t1",
	}); err != nil {
		t.Fatalf("SubTransfer() error = %v", err)
	}
	if _, err := client.SubMasterTransfer(ctx, SubMasterTransferRequest{FromType: "uta", ToType: "uta", Amount: "3", Coin: "USDT"}); err != nil {
		t.Fatalf("SubMasterTransfer() error = %v", err)
	}
	if _, err := client.SetDepositAccount(ctx, "USDT", "uta"); err != nil {
		t.Fatalf("SetDepositAccount() error = %v", err)
	}
	if _, err := client.Withdraw(ctx, WithdrawalRequest{
		Coin: "USDT", Chain: "trc20", TransferType: "on_chain", Address: "Taddr", Size: "10", ClientOID: "w1",
	}); err != nil {
		t.Fatalf("Withdraw() error = %v", err)
	}
	if _, err := client.CancelWithdrawal(ctx, "555"); err != nil {
		t.Fatalf("CancelWithdrawal() error = %v", err)
	}
	if _, err := client.CreateSubAccount(ctx, CreateSubAccountRequest{Username: "sub1", Note: "n"}); err != nil {
		t.Fatalf("CreateSubAccount() error = %v", err)
	}
	if _, err := client.FreezeSubAccount(ctx, "9", "freeze"); err != nil {
		t.Fatalf("FreezeSubAccount() error = %v", err)
	}
	if _, err := client.DeleteSubAPIKey(ctx, "key-1"); err != nil {
		t.Fatalf("DeleteSubAPIKey() error = %v", err)
	}
	if _, err := client.CreateSubAPIKey(ctx, CreateSubAPIKeyRequest{
		SubUID: "9", Passphrase: "pass", Permissions: []byte(`["read"]`),
	}); err != nil {
		t.Fatalf("CreateSubAPIKey() error = %v", err)
	}

	want := []seenRequest{
		{http.MethodPost, "/api/v3/account/transfer", "", `{"fromType":"uta","toType":"spot","amount":"1.5","coin":"USDT"}`},
		{http.MethodPost, "/api/v3/account/sub-transfer", "", `{"fromType":"uta","toType":"uta","amount":"2","coin":"USDT","fromUserId":"1","toUserId":"2","clientOid":"t1"}`},
		{http.MethodPost, "/api/v3/account/sub-master-transfer", "", `{"fromType":"uta","toType":"uta","amount":"3","coin":"USDT"}`},
		{http.MethodPost, "/api/v3/account/deposit-account", "", `{"coin":"USDT","accountType":"uta"}`},
		{http.MethodPost, "/api/v3/account/withdrawal", "", `{"coin":"USDT","chain":"trc20","transferType":"on_chain","address":"Taddr","size":"10","clientOid":"w1"}`},
		{http.MethodPost, "/api/v3/account/cancel-withdrawal", "", `{"orderId":"555"}`},
		{http.MethodPost, "/api/v3/user/create-sub", "", `{"username":"sub1","note":"n"}`},
		{http.MethodPost, "/api/v3/user/freeze-sub", "", `{"subUid":"9","operation":"freeze"}`},
		{http.MethodPost, "/api/v3/user/delete-sub-api", "", `{"apikey":"key-1"}`},
		{http.MethodPost, "/api/v3/user/create-sub-api", "", `{"subUid":"9","passphrase":"pass","permissions":["read"]}`},
	}
	if len(*seen) != len(want) {
		t.Fatalf("recorded %d requests, want %d", len(*seen), len(want))
	}
	for index, expected := range want {
		if (*seen)[index] != expected {
			t.Errorf("request[%d] = %+v, want %+v", index, (*seen)[index], expected)
		}
	}

	// Withdraw 권한만 있는 키는 Trade mutation을 보낼 수 없다.
	before := len(*seen)
	if _, err := client.CancelSymbolOrders(ctx, CancelSymbolOrdersRequest{Category: CategorySpot}); !isAuthorizationError(err) {
		t.Fatalf("CancelSymbolOrders() error = %v, want authorization error", err)
	}
	if len(*seen) != before {
		t.Fatalf("unauthorized mutation sent requests")
	}
}
