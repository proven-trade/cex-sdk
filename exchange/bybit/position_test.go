package bybit

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"reflect"
	"testing"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
)

var extReadTransferPermissions = []credential.Permission{credential.PermissionRead, credential.PermissionTransfer}

var extAllPermissions = []credential.Permission{
	credential.PermissionRead, credential.PermissionTrade, credential.PermissionTransfer,
}

func TestSetLeverage(t *testing.T) {
	t.Parallel()

	client, recorder, limiter := newExtTestClient(t, extReadTradePermissions, extResult(`{}`))
	ack, err := client.SetLeverage(context.Background(), SetLeverageRequest{
		Category: CategoryLinear, Symbol: "BTCUSDT", BuyLeverage: "6", SellLeverage: "6",
	})
	if err != nil || string(ack.Raw) != "{}" {
		t.Fatalf("SetLeverage() = %+v, error = %v", ack, err)
	}
	call := extSingleCall(t, recorder, http.MethodPost, "/v5/position/set-leverage")
	assertJSONEqual(t, call.Body, `{"category":"linear","symbol":"BTCUSDT","buyLeverage":"6","sellLeverage":"6"}`)
	snapshot, err := limiter.Snapshot(rateLimitEndpointKey("account", "bybit-main", "/v5/position/set-leverage"))
	if err != nil || snapshot.Used != 1 {
		t.Fatalf("set-leverage limiter snapshot = %+v, error = %v", snapshot, err)
	}
}

func TestSetLeverageValidation(t *testing.T) {
	t.Parallel()

	valid := SetLeverageRequest{Category: CategoryLinear, Symbol: "BTCUSDT", BuyLeverage: "6", SellLeverage: "6.5"}
	if err := valid.validate(); err != nil {
		t.Fatalf("valid request error = %v", err)
	}
	tests := []struct {
		name   string
		mutate func(*SetLeverageRequest)
	}{
		{"Spot category", func(r *SetLeverageRequest) { r.Category = CategorySpot }},
		{"missing symbol", func(r *SetLeverageRequest) { r.Symbol = "" }},
		{"zero buy leverage", func(r *SetLeverageRequest) { r.BuyLeverage = "0" }},
		{"missing sell leverage", func(r *SetLeverageRequest) { r.SellLeverage = "" }},
		{"non-numeric leverage", func(r *SetLeverageRequest) { r.BuyLeverage = "x" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := valid
			test.mutate(&request)
			if err := request.validate(); !errors.Is(err, trade.ErrValidation) {
				t.Fatalf("validate() error = %v, want validation error", err)
			}
		})
	}
}

func TestSwitchPositionMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		request  SwitchPositionModeRequest
		wantBody string
	}{
		{
			"Symbol to hedge mode",
			SwitchPositionModeRequest{Category: CategoryLinear, Symbol: "BTCUSDT", Mode: PositionModeHedge},
			`{"category":"linear","symbol":"BTCUSDT","mode":3}`,
		},
		{
			"Settle coin to one-way mode keeps mode zero",
			SwitchPositionModeRequest{Category: CategoryLinear, Coin: "USDT", Mode: PositionModeOneWay},
			`{"category":"linear","coin":"USDT","mode":0}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			client, recorder, _ := newExtTestClient(t, extReadTradePermissions, extResult(`{}`))
			ack, err := client.SwitchPositionMode(context.Background(), test.request)
			if err != nil || string(ack.Raw) != "{}" {
				t.Fatalf("SwitchPositionMode() = %+v, error = %v", ack, err)
			}
			call := extSingleCall(t, recorder, http.MethodPost, "/v5/position/switch-mode")
			assertJSONEqual(t, call.Body, test.wantBody)
		})
	}
}

func TestSwitchPositionModeValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		request SwitchPositionModeRequest
	}{
		{"Spot category", SwitchPositionModeRequest{Category: CategorySpot, Symbol: "BTCUSDT", Mode: PositionModeHedge}},
		{"missing symbol and coin", SwitchPositionModeRequest{Category: CategoryLinear, Mode: PositionModeHedge}},
		{"unsupported mode", SwitchPositionModeRequest{Category: CategoryLinear, Symbol: "BTCUSDT", Mode: 1}},
		{"surrounding whitespace", SwitchPositionModeRequest{Category: CategoryLinear, Symbol: " BTCUSDT", Mode: 3}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.request.validate(); !errors.Is(err, trade.ErrValidation) {
				t.Fatalf("validate() error = %v, want validation error", err)
			}
		})
	}
}

func TestSetTradingStop(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		request  TradingStopRequest
		wantBody string
	}{
		{
			"Partial limit TP/SL",
			TradingStopRequest{
				Category: CategoryLinear, Symbol: "XRPUSDT", TPSLMode: TPSLModePartial,
				TakeProfit: "0.6", StopLoss: "0.2", TPTriggerBy: TriggerByMarkPrice, SLTriggerBy: TriggerByIndexPrice,
				TPOrderType: OrderTypeLimit, SLOrderType: OrderTypeLimit, TPSize: "50", SLSize: "50",
				TPLimitPrice: "0.57", SLLimitPrice: "0.21",
			},
			`{"category":"linear","symbol":"XRPUSDT","takeProfit":"0.6","stopLoss":"0.2",` +
				`"tpTriggerBy":"MarkPrice","slTriggerBy":"IndexPrice","tpslMode":"Partial",` +
				`"tpOrderType":"Limit","slOrderType":"Limit","tpSize":"50","slSize":"50",` +
				`"tpLimitPrice":"0.57","slLimitPrice":"0.21","positionIdx":0}`,
		},
		{
			"Full mode cancels TP/SL and trailing stop",
			TradingStopRequest{
				Category: CategoryLinear, Symbol: "BTCUSDT", TPSLMode: TPSLModeFull, PositionIndex: 1,
				TakeProfit: "0", StopLoss: "0", TrailingStop: "0",
			},
			`{"category":"linear","symbol":"BTCUSDT","tpslMode":"Full","positionIdx":1,` +
				`"takeProfit":"0","stopLoss":"0","trailingStop":"0"}`,
		},
		{
			"Trailing stop with activation price",
			TradingStopRequest{
				Category: CategoryLinear, Symbol: "BTCUSDT", TPSLMode: TPSLModeFull, PositionIndex: 2,
				TrailingStop: "50", ActivePrice: "65000",
			},
			`{"category":"linear","symbol":"BTCUSDT","tpslMode":"Full","positionIdx":2,` +
				`"trailingStop":"50","activePrice":"65000"}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			client, recorder, _ := newExtTestClient(t, extReadTradePermissions, extResult(`{}`))
			ack, err := client.SetTradingStop(context.Background(), test.request)
			if err != nil || string(ack.Raw) != "{}" {
				t.Fatalf("SetTradingStop() = %+v, error = %v", ack, err)
			}
			call := extSingleCall(t, recorder, http.MethodPost, "/v5/position/trading-stop")
			assertJSONEqual(t, call.Body, test.wantBody)
		})
	}
}

func TestSetTradingStopValidation(t *testing.T) {
	t.Parallel()

	valid := TradingStopRequest{
		Category: CategoryLinear, Symbol: "BTCUSDT", TPSLMode: TPSLModeFull, TakeProfit: "70000", StopLoss: "60000",
	}
	if err := valid.validate(); err != nil {
		t.Fatalf("valid request error = %v", err)
	}
	partial := TradingStopRequest{
		Category: CategoryLinear, Symbol: "BTCUSDT", TPSLMode: TPSLModePartial,
		TakeProfit: "70000", StopLoss: "60000", TPSize: "50", SLSize: "50.0",
	}
	if err := partial.validate(); err != nil {
		t.Fatalf("partial request with equal sizes error = %v", err)
	}

	tests := []struct {
		name   string
		base   TradingStopRequest
		mutate func(*TradingStopRequest)
	}{
		{"Spot category", valid, func(r *TradingStopRequest) { r.Category = CategorySpot }},
		{"missing tpslMode", valid, func(r *TradingStopRequest) { r.TPSLMode = "" }},
		{"position index out of range", valid, func(r *TradingStopRequest) { r.PositionIndex = 3 }},
		{"negative takeProfit", valid, func(r *TradingStopRequest) { r.TakeProfit = "-1" }},
		{"Full with tpSize", valid, func(r *TradingStopRequest) { r.TPSize = "10" }},
		{"Full with limit tpOrderType", valid, func(r *TradingStopRequest) { r.TPOrderType = OrderTypeLimit }},
		{"limit price without limit order type", partial, func(r *TradingStopRequest) { r.TPLimitPrice = "0.5" }},
		{"unequal sizes", partial, func(r *TradingStopRequest) { r.SLSize = "60" }},
		{"zero tpSize", partial, func(r *TradingStopRequest) { r.TPSize = "0" }},
		{"unknown tpTriggerBy", valid, func(r *TradingStopRequest) { r.TPTriggerBy = "Last" }},
		{"unknown tpOrderType", valid, func(r *TradingStopRequest) { r.TPOrderType = "Stop" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := test.base
			test.mutate(&request)
			if err := request.validate(); !errors.Is(err, trade.ErrValidation) {
				t.Fatalf("validate() error = %v, want validation error", err)
			}
		})
	}
}

func TestSetAutoAddMargin(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		request  SetAutoAddMarginRequest
		wantBody string
	}{
		{
			"Hedge mode buy side on",
			SetAutoAddMarginRequest{Category: CategoryLinear, Symbol: "BTCUSDT", Enabled: true, PositionIndex: 1},
			`{"category":"linear","symbol":"BTCUSDT","autoAddMargin":1,"positionIdx":1}`,
		},
		{
			"One-way mode off keeps autoAddMargin zero",
			SetAutoAddMarginRequest{Category: CategoryLinear, Symbol: "BTCUSDT"},
			`{"category":"linear","symbol":"BTCUSDT","autoAddMargin":0}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			client, recorder, _ := newExtTestClient(t, extReadTradePermissions, extResult(`{}`))
			ack, err := client.SetAutoAddMargin(context.Background(), test.request)
			if err != nil || string(ack.Raw) != "{}" {
				t.Fatalf("SetAutoAddMargin() = %+v, error = %v", ack, err)
			}
			call := extSingleCall(t, recorder, http.MethodPost, "/v5/position/set-auto-add-margin")
			assertJSONEqual(t, call.Body, test.wantBody)
		})
	}
	for _, request := range []SetAutoAddMarginRequest{
		{Category: CategorySpot, Symbol: "BTCUSDT"},
		{Category: CategoryLinear},
		{Category: CategoryLinear, Symbol: "BTCUSDT", PositionIndex: 3},
		{Category: CategoryLinear, Symbol: "BTCUSDT", PositionIndex: -1},
	} {
		if err := request.validate(); !errors.Is(err, trade.ErrValidation) {
			t.Fatalf("validate(%+v) error = %v, want validation error", request, err)
		}
	}
}

func TestAddMargin(t *testing.T) {
	t.Parallel()

	sample := `{"category":"linear","symbol":"BTCUSDT","positionIdx":1,"riskId":11,"riskLimitValue":"500",` +
		`"size":"0.2","positionValue":"12800","avgPrice":"64000","liqPrice":"55000","bustPrice":"54000",` +
		`"markPrice":"64100","leverage":"5","autoAddMargin":0,"positionStatus":"Normal","positionIM":"2570",` +
		`"positionMM":"60","unrealisedPnl":"20","cumRealisedPnl":"-4.6","stopLoss":"0.00","takeProfit":"0.00",` +
		`"trailingStop":"0.00","createdTime":"1672737740039","updatedTime":"1684234363788"}`
	tests := []struct {
		name     string
		request  AddMarginRequest
		wantBody string
	}{
		{
			"Add margin in hedge mode",
			AddMarginRequest{Category: CategoryLinear, Symbol: "BTCUSDT", Margin: "10", PositionIndex: 1},
			`{"category":"linear","symbol":"BTCUSDT","margin":"10","positionIdx":1}`,
		},
		{
			"Reduce margin in one-way mode",
			AddMarginRequest{Category: CategoryLinear, Symbol: "BTCUSDT", Margin: "-5.5"},
			`{"category":"linear","symbol":"BTCUSDT","margin":"-5.5"}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			client, recorder, _ := newExtTestClient(t, extReadTradePermissions, extResult(sample))
			result, err := client.AddMargin(context.Background(), test.request)
			if err != nil || result.Category != CategoryLinear || result.Symbol != "BTCUSDT" ||
				result.PositionIndex != 1 || result.RiskID != 11 || result.LiquidationPrice != "55000" ||
				result.PositionIM != "2570" || result.AutoAddMargin != 0 || result.PositionStatus != "Normal" ||
				result.CumulativeRealisedPnL != "-4.6" || result.UpdatedTime != "1684234363788" ||
				len(result.Raw) == 0 {
				t.Fatalf("AddMargin() = %+v, error = %v", result, err)
			}
			call := extSingleCall(t, recorder, http.MethodPost, "/v5/position/add-margin")
			assertJSONEqual(t, call.Body, test.wantBody)
		})
	}
}

func TestAddMarginValidation(t *testing.T) {
	t.Parallel()

	for _, margin := range []string{"10", "-5.5", "0.0001"} {
		request := AddMarginRequest{Category: CategoryLinear, Symbol: "BTCUSDT", Margin: margin}
		if err := request.validate(); err != nil {
			t.Fatalf("validate(margin %q) error = %v", margin, err)
		}
	}
	tests := []struct {
		name    string
		request AddMarginRequest
	}{
		{"Spot category", AddMarginRequest{Category: CategorySpot, Symbol: "BTCUSDT", Margin: "10"}},
		{"missing symbol", AddMarginRequest{Category: CategoryLinear, Margin: "10"}},
		{"empty margin", AddMarginRequest{Category: CategoryLinear, Symbol: "BTCUSDT"}},
		{"zero margin", AddMarginRequest{Category: CategoryLinear, Symbol: "BTCUSDT", Margin: "0"}},
		{"negative zero margin", AddMarginRequest{Category: CategoryLinear, Symbol: "BTCUSDT", Margin: "-0"}},
		{"zero decimal margin", AddMarginRequest{Category: CategoryLinear, Symbol: "BTCUSDT", Margin: "0.0000"}},
		{"five decimal places", AddMarginRequest{Category: CategoryLinear, Symbol: "BTCUSDT", Margin: "1.23456"}},
		{"explicit plus sign", AddMarginRequest{Category: CategoryLinear, Symbol: "BTCUSDT", Margin: "+5"}},
		{"non-numeric margin", AddMarginRequest{Category: CategoryLinear, Symbol: "BTCUSDT", Margin: "abc"}},
		{"position index out of range", AddMarginRequest{
			Category: CategoryLinear, Symbol: "BTCUSDT", Margin: "10", PositionIndex: 3,
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.request.validate(); !errors.Is(err, trade.ErrValidation) {
				t.Fatalf("validate() error = %v, want validation error", err)
			}
		})
	}
}

func TestConfirmPendingMMR(t *testing.T) {
	t.Parallel()

	client, recorder, _ := newExtTestClient(t, extReadTradePermissions, extResult(`{}`))
	ack, err := client.ConfirmPendingMMR(
		context.Background(), ConfirmPendingMMRRequest{Category: CategoryLinear, Symbol: "BTCUSDT"},
	)
	if err != nil || string(ack.Raw) != "{}" {
		t.Fatalf("ConfirmPendingMMR() = %+v, error = %v", ack, err)
	}
	call := extSingleCall(t, recorder, http.MethodPost, "/v5/position/confirm-pending-mmr")
	assertJSONEqual(t, call.Body, `{"category":"linear","symbol":"BTCUSDT"}`)
	for _, request := range []ConfirmPendingMMRRequest{
		{Category: CategorySpot, Symbol: "BTCUSDT"}, {Category: CategoryLinear},
	} {
		if err := request.validate(); !errors.Is(err, trade.ErrValidation) {
			t.Fatalf("validate(%+v) error = %v, want validation error", request, err)
		}
	}
}

func TestSymbolLeverages(t *testing.T) {
	t.Parallel()

	sample := `{"category":"linear","list":[` +
		`{"symbol":"MNTUSDT","leverage":"10","side":"Sell","positionIdx":2},` +
		`{"symbol":"MNTUSDT","leverage":"10","side":"Sell","positionIdx":1}]}`
	client, recorder, _ := newExtTestClient(t, extReadPermissions, extResult(sample))
	leverages, err := client.SymbolLeverages(
		context.Background(), SymbolLeverageRequest{Category: CategoryLinear, Symbol: "MNTUSDT"},
	)
	if err != nil || len(leverages) != 2 || leverages[0].Symbol != "MNTUSDT" || leverages[0].Leverage != "10" ||
		leverages[0].PositionIndex != 2 || leverages[1].PositionIndex != 1 || len(leverages[1].Raw) == 0 {
		t.Fatalf("SymbolLeverages() = %+v, error = %v", leverages, err)
	}
	call := extSingleCall(t, recorder, http.MethodGet, "/v5/position/symbol-info")
	want := url.Values{"category": {"linear"}, "symbol": {"MNTUSDT"}}
	if !reflect.DeepEqual(call.Query, want) || call.Body != "" {
		t.Fatalf("query = %v body = %q, want %v and empty body", call.Query, call.Body, want)
	}
	for _, request := range []SymbolLeverageRequest{
		{Category: CategorySpot}, {Category: CategoryLinear, Symbol: " MNTUSDT"},
	} {
		if err := request.validate(); !errors.Is(err, trade.ErrValidation) {
			t.Fatalf("validate(%+v) error = %v, want validation error", request, err)
		}
	}
}

func TestClosedPnL(t *testing.T) {
	t.Parallel()

	sample := `{"nextPageCursor":"5a373bfe-188d-4913-9c81-d57ab5be8068%3A1672214887231423699","category":"linear","list":[` +
		`{"symbol":"ETHPERP","orderType":"Market","leverage":"3","updatedTime":"1672214887236","side":"Sell",` +
		`"orderId":"5a373bfe-188d-4913-9c81-d57ab5be8068","closedPnl":"-47.4065323","avgEntryPrice":"1194.97516667",` +
		`"qty":"3","cumEntryValue":"3584.9255","createdTime":"1672214887231","orderPrice":"1122.95","closedSize":"3",` +
		`"avgExitPrice":"1180.59833333","execType":"Trade","fillCount":"4","cumExitValue":"3541.795"}]}`
	client, recorder, _ := newExtTestClient(t, extReadPermissions, extResult(sample))
	start := time.UnixMilli(1_672_214_000_000)
	end := start.Add(24 * time.Hour)
	page, err := client.ClosedPnL(context.Background(), ClosedPnLRequest{
		Category: CategoryLinear, Symbol: "ETHPERP", StartTime: &start, EndTime: &end, Limit: 50, Cursor: "cursor-1",
	})
	if err != nil || page.Category != CategoryLinear || len(page.Records) != 1 || len(page.Raw) == 0 ||
		page.NextPageCursor != "5a373bfe-188d-4913-9c81-d57ab5be8068%3A1672214887231423699" {
		t.Fatalf("ClosedPnL() = %+v, error = %v", page, err)
	}
	record := page.Records[0]
	if record.Symbol != "ETHPERP" || record.Side != SideSell || record.OrderType != OrderTypeMarket ||
		record.ClosedPnL != "-47.4065323" || record.Quantity != "3" || record.FillCount != "4" ||
		record.ExecutionType != "Trade" || record.AverageEntryPrice != "1194.97516667" ||
		record.CumulativeExitValue != "3541.795" || record.CreatedTime != "1672214887231" ||
		len(record.Raw) == 0 {
		t.Fatalf("record = %+v", record)
	}
	call := extSingleCall(t, recorder, http.MethodGet, "/v5/position/closed-pnl")
	want := url.Values{
		"category": {"linear"}, "symbol": {"ETHPERP"}, "startTime": {"1672214000000"}, "endTime": {"1672300400000"},
		"limit": {"50"}, "cursor": {"cursor-1"},
	}
	if !reflect.DeepEqual(call.Query, want) || call.Body != "" {
		t.Fatalf("query = %v body = %q, want %v and empty body", call.Query, call.Body, want)
	}
}

func TestClosedPnLValidation(t *testing.T) {
	t.Parallel()

	start := time.UnixMilli(1_672_214_000_000)
	exactWeek := start.Add(7 * 24 * time.Hour)
	overWeek := exactWeek.Add(time.Millisecond)
	before := start.Add(-time.Hour)
	if err := (ClosedPnLRequest{Category: CategoryLinear, StartTime: &start, EndTime: &exactWeek}).validate(); err != nil {
		t.Fatalf("seven day range error = %v", err)
	}
	tests := []struct {
		name    string
		request ClosedPnLRequest
	}{
		{"Spot category", ClosedPnLRequest{Category: CategorySpot}},
		{"range over seven days", ClosedPnLRequest{Category: CategoryLinear, StartTime: &start, EndTime: &overWeek}},
		{"start after end", ClosedPnLRequest{Category: CategoryLinear, StartTime: &start, EndTime: &before}},
		{"limit over maximum", ClosedPnLRequest{Category: CategoryLinear, Limit: 101}},
		{"negative limit", ClosedPnLRequest{Category: CategoryLinear, Limit: -1}},
		{"cursor with whitespace", ClosedPnLRequest{Category: CategoryLinear, Cursor: " cursor"}},
		{"symbol with whitespace", ClosedPnLRequest{Category: CategoryLinear, Symbol: "BTCUSDT "}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.request.validate(); !errors.Is(err, trade.ErrValidation) {
				t.Fatalf("validate() error = %v, want validation error", err)
			}
		})
	}
}

func TestExecutions(t *testing.T) {
	t.Parallel()

	sample := `{"nextPageCursor":"132766%3A2%2C132766%3A2","category":"linear","list":[` +
		`{"symbol":"ETHPERP","orderType":"Market","underlyingPrice":"","orderLinkId":"","side":"Buy","indexPrice":"",` +
		`"orderId":"8c065341-7b52-4ca9-ac2c-37e31ac55c94","stopOrderType":"UNKNOWN","leavesQty":"0",` +
		`"execTime":"1672282722429","feeCurrency":"","isMaker":false,"execFee":"0.071409","feeRate":"0.0006",` +
		`"execId":"e0cbe81d-0f18-5866-9415-cf319b5dab3b","tradeIv":"","blockTradeId":"","markPrice":"1183.54",` +
		`"execPrice":"1190.15","markIv":"","orderQty":"0.1","orderPrice":"1236.9","execValue":"119.015",` +
		`"execType":"Trade","execQty":"0.1","closedSize":"","extraFees":"","seq":4688002127}]}`
	client, recorder, _ := newExtTestClient(t, extReadPermissions, extResult(sample))
	start := time.UnixMilli(1_672_282_000_000)
	end := start.Add(48 * time.Hour)
	page, err := client.Executions(context.Background(), ExecutionsRequest{
		Category: CategoryLinear, Symbol: "ETHPERP", OrderID: "8c065341-7b52-4ca9-ac2c-37e31ac55c94",
		StartTime: &start, EndTime: &end, ExecutionType: "Trade", Limit: 100, Cursor: "cursor-2",
	})
	if err != nil || page.Category != CategoryLinear || len(page.Executions) != 1 || len(page.Raw) == 0 ||
		page.NextPageCursor != "132766%3A2%2C132766%3A2" {
		t.Fatalf("Executions() = %+v, error = %v", page, err)
	}
	execution := page.Executions[0]
	if execution.ExecutionID != "e0cbe81d-0f18-5866-9415-cf319b5dab3b" || execution.Side != SideBuy ||
		execution.OrderType != OrderTypeMarket || execution.ExecutionPrice != "1190.15" ||
		execution.ExecutionQty != "0.1" || execution.ExecutionFee != "0.071409" || execution.FeeRate != "0.0006" ||
		execution.Maker || execution.Sequence != 4688002127 || execution.StopOrderType != "UNKNOWN" ||
		execution.ExecutionTime != "1672282722429" || len(execution.Raw) == 0 {
		t.Fatalf("execution = %+v", execution)
	}
	call := extSingleCall(t, recorder, http.MethodGet, "/v5/execution/list")
	want := url.Values{
		"category": {"linear"}, "symbol": {"ETHPERP"}, "orderId": {"8c065341-7b52-4ca9-ac2c-37e31ac55c94"},
		"startTime": {"1672282000000"}, "endTime": {"1672454800000"}, "execType": {"Trade"},
		"limit": {"100"}, "cursor": {"cursor-2"},
	}
	if !reflect.DeepEqual(call.Query, want) || call.Body != "" {
		t.Fatalf("query = %v body = %q, want %v and empty body", call.Query, call.Body, want)
	}
}

func TestExecutionsSpotMinimalQuery(t *testing.T) {
	t.Parallel()

	client, recorder, _ := newExtTestClient(
		t, extReadPermissions, extResult(`{"category":"spot","list":[],"nextPageCursor":""}`),
	)
	page, err := client.Executions(context.Background(), ExecutionsRequest{Category: CategorySpot})
	if err != nil || page.Category != CategorySpot || len(page.Executions) != 0 || page.NextPageCursor != "" {
		t.Fatalf("Executions() = %+v, error = %v", page, err)
	}
	call := extSingleCall(t, recorder, http.MethodGet, "/v5/execution/list")
	if want := (url.Values{"category": {"spot"}}); !reflect.DeepEqual(call.Query, want) {
		t.Fatalf("query = %v, want %v", call.Query, want)
	}
}

func TestExecutionsValidation(t *testing.T) {
	t.Parallel()

	start := time.UnixMilli(1_672_282_000_000)
	overWeek := start.Add(7*24*time.Hour + time.Millisecond)
	tests := []struct {
		name    string
		request ExecutionsRequest
	}{
		{"unsupported category", ExecutionsRequest{Category: "option"}},
		{"invalid orderLinkId", ExecutionsRequest{Category: CategoryLinear, OrderLinkID: "bad id!"}},
		{"range over seven days", ExecutionsRequest{Category: CategoryLinear, StartTime: &start, EndTime: &overWeek}},
		{"limit over maximum", ExecutionsRequest{Category: CategoryLinear, Limit: 101}},
		{"execType with whitespace", ExecutionsRequest{Category: CategoryLinear, ExecutionType: " Trade"}},
		{"baseCoin with whitespace", ExecutionsRequest{Category: CategorySpot, BaseCoin: " BTC"}},
		{"orderId with whitespace", ExecutionsRequest{Category: CategorySpot, OrderID: "1 "}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.request.validate(); !errors.Is(err, trade.ErrValidation) {
				t.Fatalf("validate() error = %v, want validation error", err)
			}
		})
	}
}

func TestMovePositions(t *testing.T) {
	t.Parallel()

	client, recorder, _ := newExtTestClient(t, extReadTransferPermissions, extResult(
		`{"blockTradeId":"e9bb926c95f54cf1ba3e315a58b8597b","status":"Processing","rejectParty":""}`,
	))
	result, err := client.MovePositions(context.Background(), MovePositionsRequest{
		FromUID: "100307601", ToUID: "592324",
		Legs: []MovePositionLeg{{Category: CategorySpot, Symbol: "BTCUSDT", Price: "100", Side: SideSell, Quantity: "0.01"}},
	})
	if err != nil || result.BlockTradeID != "e9bb926c95f54cf1ba3e315a58b8597b" ||
		result.Status != "Processing" || result.RejectParty != "" || len(result.Raw) == 0 {
		t.Fatalf("MovePositions() = %+v, error = %v", result, err)
	}
	call := extSingleCall(t, recorder, http.MethodPost, "/v5/position/move-positions")
	assertJSONEqual(t, call.Body, `{"fromUid":"100307601","toUid":"592324","list":[`+
		`{"category":"spot","symbol":"BTCUSDT","price":"100","side":"Sell","qty":"0.01"}]}`)
}

func TestMovePositionsRequiresTransferPermission(t *testing.T) {
	t.Parallel()

	client, recorder, _ := newExtTestClient(t, extReadTradePermissions, extResult(`{}`))
	_, err := client.MovePositions(context.Background(), MovePositionsRequest{
		FromUID: "100307601", ToUID: "592324",
		Legs: []MovePositionLeg{{Category: CategoryLinear, Symbol: "BTCUSDT", Price: "64000", Side: SideBuy, Quantity: "1"}},
	})
	if !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("MovePositions() error = %v, want authorization", err)
	}
	if calls := recorder.snapshot(); len(calls) != 0 {
		t.Fatalf("server received %d requests, want none without Transfer permission", len(calls))
	}
}

func TestMovePositionsValidation(t *testing.T) {
	t.Parallel()

	leg := MovePositionLeg{Category: CategoryLinear, Symbol: "BTCUSDT", Price: "64000", Side: SideBuy, Quantity: "1"}
	valid := MovePositionsRequest{FromUID: "100307601", ToUID: "592324", Legs: []MovePositionLeg{leg}}
	if err := valid.validate(); err != nil {
		t.Fatalf("valid request error = %v", err)
	}
	maxLegs := valid
	maxLegs.Legs = make([]MovePositionLeg, 25)
	for index := range maxLegs.Legs {
		maxLegs.Legs[index] = leg
	}
	if err := maxLegs.validate(); err != nil {
		t.Fatalf("25-leg request error = %v", err)
	}

	legMutation := func(mutate func(*MovePositionLeg)) func(*MovePositionsRequest) {
		return func(r *MovePositionsRequest) {
			changed := leg
			mutate(&changed)
			r.Legs = []MovePositionLeg{changed}
		}
	}
	tests := []struct {
		name   string
		mutate func(*MovePositionsRequest)
	}{
		{"non-numeric fromUid", func(r *MovePositionsRequest) { r.FromUID = "abc" }},
		{"missing toUid", func(r *MovePositionsRequest) { r.ToUID = "" }},
		{"same uid", func(r *MovePositionsRequest) { r.ToUID = r.FromUID }},
		{"no legs", func(r *MovePositionsRequest) { r.Legs = nil }},
		{"too many legs", func(r *MovePositionsRequest) { r.Legs = append(maxLegs.Legs[:25:25], leg) }},
		{"unsupported leg category", legMutation(func(l *MovePositionLeg) { l.Category = "option" })},
		{"missing leg symbol", legMutation(func(l *MovePositionLeg) { l.Symbol = "" })},
		{"zero leg price", legMutation(func(l *MovePositionLeg) { l.Price = "0" })},
		{"invalid leg side", legMutation(func(l *MovePositionLeg) { l.Side = "Hold" })},
		{"missing leg quantity", legMutation(func(l *MovePositionLeg) { l.Quantity = "" })},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := valid
			test.mutate(&request)
			if err := request.validate(); !errors.Is(err, trade.ErrValidation) {
				t.Fatalf("validate() error = %v, want validation error", err)
			}
		})
	}
}

func TestMovePositionHistory(t *testing.T) {
	t.Parallel()

	sample := `{"list":[{"blockTradeId":"1a82e5801af74b67b7ad71ba00a7391a","category":"option",` +
		`"orderId":"8e09c5b8-f651-4cec-968d-52764cac11ec","userId":592324,"symbol":"BTC-14OCT23-27000-C",` +
		`"side":"Buy","price":"6","qty":"0.99","execFee":"0","status":"Filled",` +
		`"execId":"677ad344-6bb4-4ace-baca-128fcffcaca7","resultCode":0,"resultMessage":"",` +
		`"createdAt":1697186522865,"updatedAt":1697186523289,"rejectParty":""}],` +
		`"nextPageCursor":"page_token%3D1241742%26"}`
	start := time.UnixMilli(1_697_186_000_000)
	end := start.Add(72 * time.Hour)
	tests := []struct {
		name      string
		request   MovePositionHistoryRequest
		wantQuery url.Values
	}{
		{
			"All filters",
			MovePositionHistoryRequest{
				Category: CategoryLinear, Symbol: "BTCUSDT", StartTime: &start, EndTime: &end, Status: "Filled",
				BlockTradeID: "1a82e5801af74b67b7ad71ba00a7391a", Limit: 20, Cursor: "page_token%3D1",
			},
			url.Values{
				"category": {"linear"}, "symbol": {"BTCUSDT"}, "startTime": {"1697186000000"},
				"endTime": {"1697445200000"}, "status": {"Filled"},
				"blockTradeId": {"1a82e5801af74b67b7ad71ba00a7391a"}, "limit": {"20"}, "cursor": {"page_token%3D1"},
			},
		},
		{"No filters", MovePositionHistoryRequest{}, url.Values{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			client, recorder, _ := newExtTestClient(t, extReadPermissions, extResult(sample))
			page, err := client.MovePositionHistory(context.Background(), test.request)
			if err != nil || len(page.Records) != 1 || len(page.Raw) == 0 ||
				page.NextPageCursor != "page_token%3D1241742%26" {
				t.Fatalf("MovePositionHistory() = %+v, error = %v", page, err)
			}
			record := page.Records[0]
			if record.BlockTradeID != "1a82e5801af74b67b7ad71ba00a7391a" || record.Category != "option" ||
				record.UserID != 592324 || record.Side != SideBuy || record.Quantity != "0.99" ||
				record.Status != "Filled" || record.ResultCode != 0 || record.CreatedAt != 1697186522865 ||
				record.UpdatedAt != 1697186523289 || len(record.Raw) == 0 {
				t.Fatalf("record = %+v", record)
			}
			call := extSingleCall(t, recorder, http.MethodGet, "/v5/position/move-history")
			if !reflect.DeepEqual(call.Query, test.wantQuery) || call.Body != "" {
				t.Fatalf("query = %v body = %q, want %v and empty body", call.Query, call.Body, test.wantQuery)
			}
		})
	}
}

func TestMovePositionHistoryValidation(t *testing.T) {
	t.Parallel()

	start := time.UnixMilli(1_697_186_000_000)
	overWeek := start.Add(7*24*time.Hour + time.Millisecond)
	for _, request := range []MovePositionHistoryRequest{
		{}, {Category: CategorySpot, Status: "Rejected"}, {Category: CategoryLinear, Status: "Processing", Limit: 200},
	} {
		if err := request.validate(); err != nil {
			t.Fatalf("validate(%+v) error = %v", request, err)
		}
	}
	tests := []struct {
		name    string
		request MovePositionHistoryRequest
	}{
		{"unsupported category", MovePositionHistoryRequest{Category: "option"}},
		{"unknown status", MovePositionHistoryRequest{Status: "Open"}},
		{"limit over maximum", MovePositionHistoryRequest{Limit: 201}},
		{"range over seven days", MovePositionHistoryRequest{StartTime: &start, EndTime: &overWeek}},
		{"blockTradeId with whitespace", MovePositionHistoryRequest{BlockTradeID: " id"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.request.validate(); !errors.Is(err, trade.ErrValidation) {
				t.Fatalf("validate() error = %v, want validation error", err)
			}
		})
	}
}

// positionMutationCases는 포지션 변경 메서드를 유효한 요청으로 호출한다.
func positionMutationCases() []extMutationCase {
	return []extMutationCase{
		{"SetLeverage", func(client *Client) error {
			_, err := client.SetLeverage(context.Background(), SetLeverageRequest{
				Category: CategoryLinear, Symbol: "BTCUSDT", BuyLeverage: "5", SellLeverage: "5",
			})
			return err
		}},
		{"SwitchPositionMode", func(client *Client) error {
			_, err := client.SwitchPositionMode(context.Background(), SwitchPositionModeRequest{
				Category: CategoryLinear, Symbol: "BTCUSDT", Mode: PositionModeHedge,
			})
			return err
		}},
		{"SetTradingStop", func(client *Client) error {
			_, err := client.SetTradingStop(context.Background(), TradingStopRequest{
				Category: CategoryLinear, Symbol: "BTCUSDT", TPSLMode: TPSLModeFull, TakeProfit: "70000",
			})
			return err
		}},
		{"SetAutoAddMargin", func(client *Client) error {
			_, err := client.SetAutoAddMargin(context.Background(), SetAutoAddMarginRequest{
				Category: CategoryLinear, Symbol: "BTCUSDT", Enabled: true,
			})
			return err
		}},
		{"AddMargin", func(client *Client) error {
			_, err := client.AddMargin(context.Background(), AddMarginRequest{
				Category: CategoryLinear, Symbol: "BTCUSDT", Margin: "10",
			})
			return err
		}},
		{"ConfirmPendingMMR", func(client *Client) error {
			_, err := client.ConfirmPendingMMR(
				context.Background(), ConfirmPendingMMRRequest{Category: CategoryLinear, Symbol: "BTCUSDT"},
			)
			return err
		}},
		{"MovePositions", func(client *Client) error {
			_, err := client.MovePositions(context.Background(), MovePositionsRequest{
				FromUID: "100307601", ToUID: "592324",
				Legs: []MovePositionLeg{
					{Category: CategoryLinear, Symbol: "BTCUSDT", Price: "64000", Side: SideBuy, Quantity: "1"},
				},
			})
			return err
		}},
	}
}

func TestPositionMutationsAreNotRetried(t *testing.T) {
	t.Parallel()

	for _, test := range positionMutationCases() {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			client, recorder, _ := newExtTestClient(t, extAllPermissions, extServerError)
			if err := test.call(client); !errors.Is(err, trade.ErrUnknownExecutionState) {
				t.Fatalf("error = %v, want unknown execution state", err)
			}
			if calls := recorder.snapshot(); len(calls) != 1 {
				t.Fatalf("server received %d requests, want exactly one without retry", len(calls))
			}
		})
	}
}

func TestPositionMutationsRequireTradePermission(t *testing.T) {
	t.Parallel()

	for _, test := range positionMutationCases() {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			client, recorder, _ := newExtTestClient(t, extReadPermissions, extResult(`{}`))
			if err := test.call(client); !errors.Is(err, trade.ErrAuthorization) {
				t.Fatalf("error = %v, want authorization", err)
			}
			if calls := recorder.snapshot(); len(calls) != 0 {
				t.Fatalf("server received %d requests, want none without Trade or Transfer permission", len(calls))
			}
		})
	}
}
