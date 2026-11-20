package bybit

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	trade "github.com/proven-trade/cex-sdk"
)

func TestAccountInfo(t *testing.T) {
	t.Parallel()

	client, recorder, limiter := newExtTestClient(t, extReadPermissions, extResult(`{"marginMode":"REGULAR_MARGIN",`+
		`"updatedTime":"1697078946000","unifiedMarginStatus":4,"dcpStatus":"OFF","timeWindow":10,"smpGroup":0,`+
		`"isMasterTrader":false,"spotHedgingStatus":"OFF"}`))
	info, err := client.AccountInfo(context.Background())
	if err != nil || info.MarginMode != MarginModeRegular || info.UnifiedMarginStatus != 4 ||
		info.DCPStatus != "OFF" || info.TimeWindow != 10 || info.UpdatedTime != "1697078946000" || len(info.Raw) == 0 {
		t.Fatalf("AccountInfo() = %+v, error = %v", info, err)
	}
	call := extSingleCall(t, recorder, http.MethodGet, "/v5/account/info")
	assertQuery(t, call.Query, map[string]string{})
	snapshot, err := limiter.Snapshot(rateLimitEndpointKey("account", "bybit-main", "/v5/account/info"))
	if err != nil || snapshot.Used != 1 {
		t.Fatalf("account info limiter snapshot = %+v, error = %v", snapshot, err)
	}
}

func TestFeeRates(t *testing.T) {
	t.Parallel()

	client, recorder, _ := newExtTestClient(t, extReadPermissions, extResult(`{"list":[`+
		`{"symbol":"ETHUSDT","takerFeeRate":"0.0006","makerFeeRate":"0.0001"}]}`))
	rates, err := client.FeeRates(context.Background(), FeeRatesRequest{Category: CategoryLinear, Symbol: "ETHUSDT"})
	if err != nil || len(rates) != 1 || rates[0].TakerFeeRate != "0.0006" || rates[0].MakerFeeRate != "0.0001" {
		t.Fatalf("FeeRates() = %+v, error = %v", rates, err)
	}
	call := extSingleCall(t, recorder, http.MethodGet, "/v5/account/fee-rate")
	assertQuery(t, call.Query, map[string]string{"category": "linear", "symbol": "ETHUSDT"})
	if _, err := client.FeeRates(context.Background(), FeeRatesRequest{Category: "option"}); !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("option category error = %v, want validation error", err)
	}
}

func TestTransactionLog(t *testing.T) {
	t.Parallel()

	client, recorder, _ := newExtTestClient(t, extReadPermissions, extResult(`{"nextPageCursor":"21963%3A1","list":[{`+
		`"id":"592324_XRPUSDT_161440249321","symbol":"XRPUSDT","side":"Buy","funding":"-0.003676","orderLinkId":"",`+
		`"orderId":"1672128000-8-592324-1-2","fee":"0.00000000","change":"-0.003676","cashFlow":"0",`+
		`"transactionTime":"1672128000000","type":"SETTLEMENT","feeRate":"0.0001","bonusChange":"","size":"100",`+
		`"qty":"100","cashBalance":"5086.55825002","currency":"USDT","category":"linear","tradePrice":"0.3676",`+
		`"tradeId":"534c0003-4bf7-486f-aa02-78cee36825e4"}]}`))
	start := time.UnixMilli(1672041600000)
	end := start.Add(7 * 24 * time.Hour)
	page, err := client.TransactionLog(context.Background(), TransactionLogRequest{
		AccountType: AccountTypeUnified, Category: CategoryLinear, Currency: "USDT", Type: "SETTLEMENT",
		StartTime: &start, EndTime: &end, Limit: 50, Cursor: "c1",
	})
	if err != nil || len(page.Records) != 1 || page.NextPageCursor != "21963%3A1" || len(page.Raw) == 0 {
		t.Fatalf("TransactionLog() = %+v, error = %v", page, err)
	}
	record := page.Records[0]
	if record.Type != "SETTLEMENT" || record.Funding != "-0.003676" || record.Category != CategoryLinear ||
		record.CashBalance != "5086.55825002" || record.TransactionTime != "1672128000000" ||
		record.TradeID != "534c0003-4bf7-486f-aa02-78cee36825e4" || len(record.Raw) == 0 {
		t.Fatalf("record = %+v", record)
	}
	call := extSingleCall(t, recorder, http.MethodGet, "/v5/account/transaction-log")
	assertQuery(t, call.Query, map[string]string{
		"accountType": "UNIFIED", "category": "linear", "currency": "USDT", "type": "SETTLEMENT",
		"startTime": "1672041600000", "endTime": "1672646400000", "limit": "50", "cursor": "c1",
	})

	tooLong := start.Add(8 * 24 * time.Hour)
	for name, request := range map[string]TransactionLogRequest{
		"range over 7 days": {StartTime: &start, EndTime: &tooLong},
		"start after end":   {StartTime: &end, EndTime: &start},
		"limit over 50":     {Limit: 51},
		"invalid account":   {AccountType: "CONTRACT"},
		"option category":   {Category: "option"},
	} {
		if _, err := client.TransactionLog(context.Background(), request); !errors.Is(err, trade.ErrValidation) {
			t.Fatalf("%s: error = %v, want validation error", name, err)
		}
	}
}

func TestBorrowHistory(t *testing.T) {
	t.Parallel()

	client, recorder, _ := newExtTestClient(t, extReadPermissions, extResult(`{"nextPageCursor":"2671153%3A1","list":[{`+
		`"borrowAmount":"1.06333265702840778","costExemption":"0","freeBorrowedAmount":"0","createdTime":1697439900204,`+
		`"InterestBearingBorrowSize":"1.06333265702840778","currency":"BTC","unrealisedLoss":"0",`+
		`"hourlyBorrowRate":"0.000001216904","borrowCost":"0.00000129"}]}`))
	page, err := client.BorrowHistory(context.Background(), BorrowHistoryRequest{Currency: "BTC", Limit: 1})
	if err != nil || len(page.Records) != 1 || page.NextPageCursor != "2671153%3A1" {
		t.Fatalf("BorrowHistory() = %+v, error = %v", page, err)
	}
	record := page.Records[0]
	if record.Currency != "BTC" || record.CreatedTime != 1697439900204 || record.BorrowCost != "0.00000129" ||
		record.InterestBearingBorrowSize != "1.06333265702840778" || record.HourlyBorrowRate != "0.000001216904" {
		t.Fatalf("record = %+v", record)
	}
	call := extSingleCall(t, recorder, http.MethodGet, "/v5/account/borrow-history")
	assertQuery(t, call.Query, map[string]string{"currency": "BTC", "limit": "1"})

	start := time.UnixMilli(1697439900204)
	end := start.Add(31 * 24 * time.Hour)
	if _, err := client.BorrowHistory(context.Background(), BorrowHistoryRequest{
		StartTime: &start, EndTime: &end,
	}); !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("range over 30 days error = %v, want validation error", err)
	}
}

func TestCollateralInfos(t *testing.T) {
	t.Parallel()

	client, recorder, _ := newExtTestClient(t, extReadPermissions, extResult(`{"list":[{"availableToBorrow":"3",`+
		`"freeBorrowingAmount":"","freeBorrowAmount":"0","maxBorrowingAmount":"3","hourlyBorrowRate":"0.00000147",`+
		`"borrowUsageRate":"0","collateralSwitch":true,"borrowAmount":"0","borrowable":true,"currency":"BTC",`+
		`"marginCollateral":true,"freeBorrowingLimit":"0","collateralRatio":"0.95"}]}`))
	infos, err := client.CollateralInfos(context.Background(), CollateralInfoRequest{Currency: "BTC"})
	if err != nil || len(infos) != 1 || infos[0].Currency != "BTC" || !infos[0].CollateralSwitch ||
		!infos[0].MarginCollateral || !infos[0].Borrowable || infos[0].CollateralRatio != "0.95" ||
		infos[0].MaxBorrowingAmount != "3" {
		t.Fatalf("CollateralInfos() = %+v, error = %v", infos, err)
	}
	call := extSingleCall(t, recorder, http.MethodGet, "/v5/account/collateral-info")
	assertQuery(t, call.Query, map[string]string{"currency": "BTC"})
}

func TestSetCollateralSwitch(t *testing.T) {
	t.Parallel()

	client, recorder, _ := newExtTestClient(t, extReadTradePermissions, extResult(`{}`))
	ack, err := client.SetCollateralSwitch(context.Background(), SetCollateralSwitchRequest{Coin: "BTC", Enabled: true})
	if err != nil || string(ack.Raw) != "{}" {
		t.Fatalf("SetCollateralSwitch() = %+v, error = %v", ack, err)
	}
	call := extSingleCall(t, recorder, http.MethodPost, "/v5/account/set-collateral-switch")
	assertJSONEqual(t, call.Body, `{"coin":"BTC","collateralSwitch":"ON"}`)

	for name, request := range map[string]SetCollateralSwitchRequest{
		"missing coin":       {Enabled: true},
		"USDT cannot be off": {Coin: "USDT"},
		"USDC cannot be off": {Coin: "USDC"},
	} {
		if _, err := client.SetCollateralSwitch(context.Background(), request); !errors.Is(err, trade.ErrValidation) {
			t.Fatalf("%s: error = %v, want validation error", name, err)
		}
	}
	if len(recorder.snapshot()) != 1 {
		t.Fatalf("invalid requests were sent: %+v", recorder.snapshot())
	}
}

func TestBatchSetCollateralSwitch(t *testing.T) {
	t.Parallel()

	client, recorder, _ := newExtTestClient(t, extReadTradePermissions, extResult(`{"list":[`+
		`{"coin":"MATIC","collateralSwitch":"OFF"},{"coin":"BTC","collateralSwitch":"ON"}]}`))
	result, err := client.BatchSetCollateralSwitch(context.Background(), BatchSetCollateralSwitchRequest{
		Items: []CollateralSwitchItem{{Coin: "MATIC"}, {Coin: "BTC", Enabled: true}},
	})
	if err != nil || len(result.Items) != 2 || result.Items[0] != (CollateralSwitchItem{Coin: "MATIC"}) ||
		result.Items[1] != (CollateralSwitchItem{Coin: "BTC", Enabled: true}) || len(result.Raw) == 0 {
		t.Fatalf("BatchSetCollateralSwitch() = %+v, error = %v", result, err)
	}
	call := extSingleCall(t, recorder, http.MethodPost, "/v5/account/set-collateral-switch-batch")
	assertJSONEqual(t, call.Body, `{"request":[{"coin":"MATIC","collateralSwitch":"OFF"},{"coin":"BTC","collateralSwitch":"ON"}]}`)

	for name, request := range map[string]BatchSetCollateralSwitchRequest{
		"empty":      {},
		"duplicate":  {Items: []CollateralSwitchItem{{Coin: "BTC"}, {Coin: "BTC", Enabled: true}}},
		"USDT off":   {Items: []CollateralSwitchItem{{Coin: "USDT"}}},
		"blank coin": {Items: []CollateralSwitchItem{{Coin: ""}}},
	} {
		if _, err := client.BatchSetCollateralSwitch(context.Background(), request); !errors.Is(err, trade.ErrValidation) {
			t.Fatalf("%s: error = %v, want validation error", name, err)
		}
	}
}

func TestSetMarginMode(t *testing.T) {
	t.Parallel()

	client, recorder, _ := newExtTestClient(t, extReadTradePermissions, extResult(`{"reasons":[]}`))
	ack, err := client.SetMarginMode(context.Background(), SetMarginModeRequest{Mode: MarginModePortfolio})
	if err != nil || string(ack.Raw) != `{"reasons":[]}` {
		t.Fatalf("SetMarginMode() = %+v, error = %v", ack, err)
	}
	call := extSingleCall(t, recorder, http.MethodPost, "/v5/account/set-margin-mode")
	assertJSONEqual(t, call.Body, `{"setMarginMode":"PORTFOLIO_MARGIN"}`)
	if _, err := client.SetMarginMode(context.Background(), SetMarginModeRequest{Mode: "CROSS"}); !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("invalid mode error = %v, want validation error", err)
	}
}

func TestMutationsRequireTradePermission(t *testing.T) {
	t.Parallel()

	client, recorder, _ := newExtTestClient(t, extReadPermissions, extResult(`{}`))
	if _, err := client.SetMarginMode(context.Background(), SetMarginModeRequest{Mode: MarginModeRegular}); err == nil {
		t.Fatal("SetMarginMode() with read-only credential succeeded")
	}
	if _, err := client.Borrow(context.Background(), BorrowRequest{Coin: "BTC", Amount: "0.01"}); err == nil {
		t.Fatal("Borrow() with read-only credential succeeded")
	}
	if calls := recorder.snapshot(); len(calls) != 0 {
		t.Fatalf("read-only credential sent mutation calls: %+v", calls)
	}
}

func TestBorrow(t *testing.T) {
	t.Parallel()

	client, recorder, _ := newExtTestClient(t, extReadTradePermissions, extResult(`{"coin":"BTC","amount":"0.01"}`))
	result, err := client.Borrow(context.Background(), BorrowRequest{Coin: "BTC", Amount: "0.01"})
	if err != nil || result.Coin != "BTC" || result.Amount != "0.01" || len(result.Raw) == 0 {
		t.Fatalf("Borrow() = %+v, error = %v", result, err)
	}
	call := extSingleCall(t, recorder, http.MethodPost, "/v5/account/borrow")
	assertJSONEqual(t, call.Body, `{"coin":"BTC","amount":"0.01"}`)
	for name, request := range map[string]BorrowRequest{
		"missing coin":   {Amount: "1"},
		"zero amount":    {Coin: "BTC", Amount: "0"},
		"missing amount": {Coin: "BTC"},
	} {
		if _, err := client.Borrow(context.Background(), request); !errors.Is(err, trade.ErrValidation) {
			t.Fatalf("%s: error = %v, want validation error", name, err)
		}
	}
}

func TestRepay(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		path     string
		call     func(*Client, RepayRequest) (RepayResult, error)
		request  RepayRequest
		wantBody string
	}{
		{
			"repay coin and amount", "/v5/account/repay",
			func(c *Client, r RepayRequest) (RepayResult, error) { return c.Repay(context.Background(), r) },
			RepayRequest{Coin: "BTC", Amount: "0.01", RepaymentType: RepaymentTypeFlexible},
			`{"coin":"BTC","amount":"0.01","repaymentType":"FLEXIBLE"}`,
		},
		{
			"repay all liabilities", "/v5/account/repay",
			func(c *Client, r RepayRequest) (RepayResult, error) { return c.Repay(context.Background(), r) },
			RepayRequest{RepaymentType: RepaymentTypeAll},
			`{"repaymentType":"ALL"}`,
		},
		{
			"repay without conversion", "/v5/account/no-convert-repay",
			func(c *Client, r RepayRequest) (RepayResult, error) {
				return c.RepayWithoutConversion(context.Background(), r)
			},
			RepayRequest{Coin: "BTC", Amount: "0.01"},
			`{"coin":"BTC","amount":"0.01"}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			client, recorder, _ := newExtTestClient(t, extReadTradePermissions, extResult(`{"resultStatus":"P"}`))
			result, err := test.call(client, test.request)
			if err != nil || result.ResultStatus != "P" || len(result.Raw) == 0 {
				t.Fatalf("repay = %+v, error = %v", result, err)
			}
			call := extSingleCall(t, recorder, http.MethodPost, test.path)
			assertJSONEqual(t, call.Body, test.wantBody)
		})
	}
}

func TestRepayValidation(t *testing.T) {
	t.Parallel()

	client, recorder, _ := newExtTestClient(t, extReadTradePermissions, extResult(`{}`))
	for name, call := range map[string]func() error{
		"amount without coin": func() error {
			_, err := client.Repay(context.Background(), RepayRequest{Amount: "1", RepaymentType: RepaymentTypeAll})
			return err
		},
		"all liabilities requires ALL": func() error {
			_, err := client.Repay(context.Background(), RepayRequest{})
			return err
		},
		"invalid repayment type": func() error {
			_, err := client.Repay(context.Background(), RepayRequest{Coin: "BTC", RepaymentType: "ANY"})
			return err
		},
		"invalid amount": func() error {
			_, err := client.Repay(context.Background(), RepayRequest{Coin: "BTC", Amount: "-1"})
			return err
		},
		"no-convert requires coin": func() error {
			_, err := client.RepayWithoutConversion(context.Background(), RepayRequest{RepaymentType: RepaymentTypeAll})
			return err
		},
	} {
		if err := call(); !errors.Is(err, trade.ErrValidation) {
			t.Fatalf("%s: error = %v, want validation error", name, err)
		}
	}
	if calls := recorder.snapshot(); len(calls) != 0 {
		t.Fatalf("invalid requests sent calls: %+v", calls)
	}
}

func TestRepayLiability(t *testing.T) {
	t.Parallel()

	client, recorder, _ := newExtTestClient(t, extReadTradePermissions, extResult(`{"list":[`+
		`{"coin":"BTC","repaymentQty":"0.10549670"},{"coin":"ETH","repaymentQty":"2.27768114"}]}`))
	result, err := client.RepayLiability(context.Background(), RepayLiabilityRequest{Coin: "USDT"})
	if err != nil || len(result.Items) != 2 || result.Items[1].Coin != "ETH" ||
		result.Items[0].RepaymentQty != "0.10549670" || len(result.Raw) == 0 || len(result.Items[0].Raw) == 0 {
		t.Fatalf("RepayLiability() = %+v, error = %v", result, err)
	}
	call := extSingleCall(t, recorder, http.MethodPost, "/v5/account/quick-repayment")
	assertJSONEqual(t, call.Body, `{"coin":"USDT"}`)
}

func TestPayInfo(t *testing.T) {
	t.Parallel()

	client, recorder, _ := newExtTestClient(t, extReadPermissions, extResult(`{"collateralInfo":{"collateralList":[`+
		`{"availableValue":"13038.36","coinScale":4,"borrowSize":"0","spotHedgeAmount":"0","assetFrozen":"0",`+
		`"availableSize":"13041.90","coin":"USDT"}]},"borrowInfo":{"borrowSize":"0.1000683","assetFrozen":"0",`+
		`"borrowValue":"9.17","coin":"SOL","availableBalance":"0"}}`))
	info, err := client.PayInfo(context.Background(), PayInfoRequest{Coin: "SOL"})
	if err != nil || len(info.Collateral) != 1 || info.Collateral[0].Coin != "USDT" ||
		info.Collateral[0].CoinScale != 4 || info.Collateral[0].AvailableSize != "13041.90" ||
		info.Borrow.Coin != "SOL" || info.Borrow.BorrowValue != "9.17" || len(info.Raw) == 0 {
		t.Fatalf("PayInfo() = %+v, error = %v", info, err)
	}
	call := extSingleCall(t, recorder, http.MethodGet, "/v5/account/pay-info")
	assertQuery(t, call.Query, map[string]string{"coin": "SOL"})
}

func TestDCPInfosAndSMPGroup(t *testing.T) {
	t.Parallel()

	client, recorder, _ := newExtTestClient(t, extReadPermissions, extResult(`{"dcpInfos":[`+
		`{"product":"SPOT","dcpStatus":"ON","timeWindow":"10"},{"product":"DERIVATIVES","dcpStatus":"ON","timeWindow":"30"}]}`))
	infos, err := client.DCPInfos(context.Background())
	if err != nil || len(infos) != 2 || infos[1].Product != "DERIVATIVES" || infos[1].TimeWindow != "30" ||
		infos[0].DCPStatus != "ON" || len(infos[0].Raw) == 0 {
		t.Fatalf("DCPInfos() = %+v, error = %v", infos, err)
	}
	extSingleCall(t, recorder, http.MethodGet, "/v5/account/query-dcp-info")

	smpClient, smpRecorder, _ := newExtTestClient(t, extReadPermissions, extResult(`{"smpGroup":7}`))
	group, err := smpClient.SMPGroup(context.Background())
	if err != nil || group.ID != 7 || len(group.Raw) == 0 {
		t.Fatalf("SMPGroup() = %+v, error = %v", group, err)
	}
	extSingleCall(t, smpRecorder, http.MethodGet, "/v5/account/smp-group")
}

func TestUserSettingConfigAndSetDeltaMode(t *testing.T) {
	t.Parallel()

	client, recorder, _ := newExtTestClient(t, extReadTradePermissions, extResult(`{"lpaSpot":false,"lpaPerp":true,`+
		`"deltaEnable":true,"smsef":false,"fmsef":true,"smpType":2}`))
	config, err := client.UserSettingConfig(context.Background())
	if err != nil || config.LimitPriceActionSpot || !config.LimitPriceActionPerp || !config.DeltaEnabled ||
		!config.FuturesMNTFeeDeduct || config.SMPType != 2 || len(config.Raw) == 0 {
		t.Fatalf("UserSettingConfig() = %+v, error = %v", config, err)
	}
	call := extSingleCall(t, recorder, http.MethodGet, "/v5/account/user-setting-config")
	assertQuery(t, call.Query, map[string]string{})

	deltaClient, deltaRecorder, _ := newExtTestClient(t, extReadTradePermissions, extResult(`{}`))
	if _, err := deltaClient.SetDeltaMode(context.Background(), SetDeltaModeRequest{Enabled: true}); err != nil {
		t.Fatalf("SetDeltaMode(true) error = %v", err)
	}
	call = extSingleCall(t, deltaRecorder, http.MethodPost, "/v5/account/set-delta-mode")
	assertJSONEqual(t, call.Body, `{"deltaEnable":"1"}`)
}

func TestSpotTradeAnalysis(t *testing.T) {
	t.Parallel()

	client, recorder, _ := newExtTestClient(t, extReadPermissions, extResult(`{"symbolRnl":"1.5","netExecQty":"0.2",`+
		`"sumExecValue":"100","sumBuyExecValue":"60","sumBuyExecQty":"0.3","sumSellExecFee":"0.01","settleCoin":"USDT",`+
		`"baseCoin":"ETH","maxMarginVersion":0,"sumPriceList":[{"day":"2026-03-10","sumBuyExecValue":"60",`+
		`"sumSellExecValue":"40","sumExecValue":"100"}]}`))
	start := time.UnixMilli(1773100000000)
	analysis, err := client.SpotTradeAnalysis(context.Background(), SpotTradeAnalysisRequest{
		Symbol: "ETHUSDT", StartTime: &start,
	})
	if err != nil || analysis.SymbolRealisedPnL != "1.5" || analysis.NetExecQuantity != "0.2" ||
		analysis.BaseCoin != "ETH" || len(analysis.DailyPrices) != 1 || analysis.DailyPrices[0].Day != "2026-03-10" ||
		analysis.DailyPrices[0].SumSellExecValue != "40" || len(analysis.Raw) == 0 {
		t.Fatalf("SpotTradeAnalysis() = %+v, error = %v", analysis, err)
	}
	call := extSingleCall(t, recorder, http.MethodGet, "/v5/account/trade-info-for-analysis")
	assertQuery(t, call.Query, map[string]string{"symbol": "ETHUSDT", "startTime": "1773100000000"})
	if _, err := client.SpotTradeAnalysis(context.Background(), SpotTradeAnalysisRequest{}); !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("missing symbol error = %v, want validation error", err)
	}
}
