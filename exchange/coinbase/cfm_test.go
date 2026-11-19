package coinbase

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
)

const cfmBalanceSummaryResponse = `{"balance_summary":{` +
	`"futures_buying_power":{"value":"1000.5","currency":"USD"},"total_usd_balance":{"value":"2000","currency":"USD"},` +
	`"cbi_usd_balance":{"value":"1500","currency":"USD"},"cfm_usd_balance":{"value":"500","currency":"USD"},` +
	`"total_open_orders_hold_amount":{"value":"10","currency":"USD"},"unrealized_pnl":{"value":"-5.25","currency":"USD"},` +
	`"daily_realized_pnl":{"value":"2","currency":"USD"},"initial_margin":{"value":"300","currency":"USD"},` +
	`"available_margin":{"value":"1700","currency":"USD"},"liquidation_threshold":{"value":"150","currency":"USD"},` +
	`"liquidation_buffer_amount":{"value":"1550","currency":"USD"},"liquidation_buffer_percentage":"91.1",` +
	`"intraday_margin_window_measure":{"margin_window_type":"FCM_MARGIN_WINDOW_TYPE_INTRADAY",` +
	`"margin_level":"MARGIN_LEVEL_TYPE_BASE","initial_margin":"300","maintenance_margin":"150",` +
	`"liquidation_buffer":"91.1","total_hold":"10","futures_buying_power":"1000.5"},` +
	`"overnight_margin_window_measure":{"margin_window_type":"FCM_MARGIN_WINDOW_TYPE_OVERNIGHT",` +
	`"margin_level":"MARGIN_LEVEL_TYPE_WARNING","initial_margin":"600","maintenance_margin":"300",` +
	`"liquidation_buffer":"80","total_hold":"10","futures_buying_power":"700"},` +
	`"total_pending_transfers_amount":{"value":"0","currency":"USD"},"funding_pnl":{"value":"1.5","currency":"USD"}}}`

const cfmPositionJSON = `{"product_id":"BIT-28JUL23-CDE","expiration_time":"2023-07-28T15:00:00Z",` +
	`"side":"FUTURES_POSITION_SIDE_LONG","number_of_contracts":"2","current_price":"29000",` +
	`"avg_entry_price":"28500","unrealized_pnl":"50","daily_realized_pnl":"3"}`

func TestCFMBalanceSummaryDecodesAmountsAndMarginWindows(t *testing.T) {
	t.Parallel()

	harness := newExtHarness(
		t, permissionsOf(credential.PermissionRead), http.StatusOK, cfmBalanceSummaryResponse,
	)
	summary, err := harness.client.CFMBalanceSummary(context.Background())
	if err != nil {
		t.Fatalf("CFMBalanceSummary() error = %v", err)
	}
	if summary.FuturesBuyingPower.Value != "1000.5" || summary.TotalUSDBalance.Currency != "USD" ||
		summary.CBIUSDBalance.Value != "1500" || summary.CFMUSDBalance.Value != "500" ||
		summary.TotalOpenOrdersHoldAmount.Value != "10" || summary.UnrealizedPnL.Value != "-5.25" ||
		summary.DailyRealizedPnL.Value != "2" || summary.InitialMargin.Value != "300" ||
		summary.AvailableMargin.Value != "1700" || summary.LiquidationThreshold.Value != "150" ||
		summary.LiquidationBufferAmount.Value != "1550" || summary.LiquidationBufferPercentage != "91.1" ||
		summary.TotalPendingTransfersAmount.Value != "0" || summary.FundingPnL.Value != "1.5" {
		t.Fatalf("CFMBalanceSummary() amounts = %+v", summary)
	}
	intraday := summary.IntradayMarginWindowMeasure
	overnight := summary.OvernightMarginWindowMeasure
	if intraday.MarginWindowType != "FCM_MARGIN_WINDOW_TYPE_INTRADAY" || intraday.MarginLevel != "MARGIN_LEVEL_TYPE_BASE" ||
		intraday.InitialMargin != "300" || intraday.MaintenanceMargin != "150" || intraday.LiquidationBuffer != "91.1" ||
		intraday.TotalHold != "10" || intraday.FuturesBuyingPower != "1000.5" ||
		overnight.MarginWindowType != "FCM_MARGIN_WINDOW_TYPE_OVERNIGHT" || overnight.MarginLevel != "MARGIN_LEVEL_TYPE_WARNING" ||
		overnight.InitialMargin != "600" {
		t.Fatalf("CFMBalanceSummary() margin windows = %+v / %+v", intraday, overnight)
	}
	if !strings.Contains(string(summary.Raw), "funding_pnl") {
		t.Fatalf("CFMBalanceSummary() raw = %s", summary.Raw)
	}
	harness.assertSingle(t, http.MethodGet, publicPrefix+"/cfm/balance_summary", nil, "")
}

func TestCFMPositionsDecodesList(t *testing.T) {
	t.Parallel()

	harness := newExtHarness(
		t, permissionsOf(credential.PermissionRead), http.StatusOK,
		`{"positions":[`+cfmPositionJSON+`,{"product_id":"ETH-28JUL23-CDE","side":"FUTURES_POSITION_SIDE_SHORT","number_of_contracts":"1"}]}`,
	)
	positions, err := harness.client.CFMPositions(context.Background())
	if err != nil {
		t.Fatalf("CFMPositions() error = %v", err)
	}
	if len(positions) != 2 || positions[0].ProductID != "BIT-28JUL23-CDE" ||
		positions[0].ExpirationTime != "2023-07-28T15:00:00Z" || positions[0].Side != "FUTURES_POSITION_SIDE_LONG" ||
		positions[0].NumberOfContracts != "2" || positions[0].CurrentPrice != "29000" ||
		positions[0].AverageEntryPrice != "28500" || positions[0].UnrealizedPnL != "50" ||
		positions[0].DailyRealizedPnL != "3" || positions[1].ProductID != "ETH-28JUL23-CDE" ||
		positions[1].Side != "FUTURES_POSITION_SIDE_SHORT" || len(positions[0].Raw) == 0 || len(positions[1].Raw) == 0 {
		t.Fatalf("CFMPositions() = %+v", positions)
	}
	harness.assertSingle(t, http.MethodGet, publicPrefix+"/cfm/positions", nil, "")
}

func TestCFMPositionQueriesProductAndRejectsUnsafeID(t *testing.T) {
	t.Parallel()

	harness := newExtHarness(
		t, permissionsOf(credential.PermissionRead), http.StatusOK, `{"position":`+cfmPositionJSON+`}`,
	)
	for _, productID := range []string{"", " BIT-28JUL23-CDE", "BIT/28JUL23"} {
		if _, err := harness.client.CFMPosition(context.Background(), productID); !errors.Is(err, trade.ErrValidation) {
			t.Fatalf("CFMPosition(%q) error = %v, want validation", productID, err)
		}
	}
	if count := harness.requestCount(); count != 0 {
		t.Fatalf("request count = %d, want 0", count)
	}
	position, err := harness.client.CFMPosition(context.Background(), "BIT-28JUL23-CDE")
	if err != nil {
		t.Fatalf("CFMPosition() error = %v", err)
	}
	if position.ProductID != "BIT-28JUL23-CDE" || position.NumberOfContracts != "2" ||
		position.AverageEntryPrice != "28500" || !strings.Contains(string(position.Raw), "FUTURES_POSITION_SIDE_LONG") {
		t.Fatalf("CFMPosition() = %+v", position)
	}
	harness.assertSingle(t, http.MethodGet, publicPrefix+"/cfm/positions/BIT-28JUL23-CDE", nil, "")
}

func TestCFMCurrentMarginWindowSendsProfileAndDecodesWindow(t *testing.T) {
	t.Parallel()

	harness := newExtHarness(
		t, permissionsOf(credential.PermissionRead), http.StatusOK,
		`{"margin_window":{"margin_window_type":"FCM_MARGIN_WINDOW_TYPE_INTRADAY","end_time":"2023-07-28T20:00:00Z"},`+
			`"is_intraday_margin_killswitch_enabled":true,"is_intraday_margin_enrollment_killswitch_enabled":false}`,
	)
	window, err := harness.client.CFMCurrentMarginWindow(context.Background(), CFMCurrentMarginWindowRequest{
		MarginProfileType: MarginProfileRetailIntraday1,
	})
	if err != nil {
		t.Fatalf("CFMCurrentMarginWindow() error = %v", err)
	}
	if window.MarginWindow.MarginWindowType != "FCM_MARGIN_WINDOW_TYPE_INTRADAY" ||
		window.MarginWindow.EndTime != "2023-07-28T20:00:00Z" || !window.IsIntradayMarginKillswitchEnabled ||
		window.IsIntradayMarginEnrollmentKillswitchEnabled || len(window.Raw) == 0 {
		t.Fatalf("CFMCurrentMarginWindow() = %+v", window)
	}
	harness.assertSingle(
		t, http.MethodGet, publicPrefix+"/cfm/intraday/current_margin_window",
		url.Values{"margin_profile_type": {"MARGIN_PROFILE_TYPE_RETAIL_INTRADAY_MARGIN_1"}}, "",
	)
}

func TestCFMCurrentMarginWindowOmitsEmptyProfileAndRejectsUnknown(t *testing.T) {
	t.Parallel()

	harness := newExtHarness(
		t, permissionsOf(credential.PermissionRead), http.StatusOK, `{"margin_window":{}}`,
	)
	if _, err := harness.client.CFMCurrentMarginWindow(
		context.Background(), CFMCurrentMarginWindowRequest{MarginProfileType: "MARGIN_PROFILE_TYPE_OTHER"},
	); !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("CFMCurrentMarginWindow(unknown profile) error = %v, want validation", err)
	}
	if count := harness.requestCount(); count != 0 {
		t.Fatalf("request count = %d, want 0", count)
	}
	if _, err := harness.client.CFMCurrentMarginWindow(
		context.Background(), CFMCurrentMarginWindowRequest{},
	); err != nil {
		t.Fatalf("CFMCurrentMarginWindow() error = %v", err)
	}
	harness.assertSingle(t, http.MethodGet, publicPrefix+"/cfm/intraday/current_margin_window", nil, "")
}

func TestCFMIntradayMarginSettingDecodesSetting(t *testing.T) {
	t.Parallel()

	harness := newExtHarness(
		t, permissionsOf(credential.PermissionRead), http.StatusOK,
		`{"setting":"INTRADAY_MARGIN_SETTING_INTRADAY"}`,
	)
	setting, err := harness.client.CFMIntradayMarginSetting(context.Background())
	if err != nil {
		t.Fatalf("CFMIntradayMarginSetting() error = %v", err)
	}
	if setting != IntradayMarginSettingIntraday {
		t.Fatalf("CFMIntradayMarginSetting() = %q", setting)
	}
	harness.assertSingle(t, http.MethodGet, publicPrefix+"/cfm/intraday/margin_setting", nil, "")
}

func TestSetCFMIntradayMarginSettingPostsSettingWithTradePermission(t *testing.T) {
	t.Parallel()

	harness := newExtHarness(t, permissionsOf(credential.PermissionTrade), http.StatusOK, `{}`)
	err := harness.client.SetCFMIntradayMarginSetting(
		context.Background(), SetCFMIntradayMarginSettingRequest{Setting: IntradayMarginSettingStandard},
	)
	if err != nil {
		t.Fatalf("SetCFMIntradayMarginSetting() error = %v", err)
	}
	harness.assertSingle(
		t, http.MethodPost, publicPrefix+"/cfm/intraday/margin_setting", nil,
		`{"setting":"INTRADAY_MARGIN_SETTING_STANDARD"}`,
	)
}

func TestSetCFMIntradayMarginSettingValidationPermissionAndNoRetry(t *testing.T) {
	t.Parallel()

	readOnly := newExtHarness(
		t, permissionsOf(credential.PermissionRead, credential.PermissionTransfer), http.StatusOK, `{}`,
	)
	for name, request := range map[string]SetCFMIntradayMarginSettingRequest{
		"empty setting":   {},
		"unknown setting": {Setting: "INTRADAY_MARGIN_SETTING_UNKNOWN"},
	} {
		if err := readOnly.client.SetCFMIntradayMarginSetting(context.Background(), request); !errors.Is(err, trade.ErrValidation) {
			t.Fatalf("SetCFMIntradayMarginSetting(%s) error = %v, want validation", name, err)
		}
	}
	err := readOnly.client.SetCFMIntradayMarginSetting(
		context.Background(), SetCFMIntradayMarginSettingRequest{Setting: IntradayMarginSettingIntraday},
	)
	if !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("SetCFMIntradayMarginSetting(no trade) error = %v, want authorization", err)
	}
	if count := readOnly.requestCount(); count != 0 {
		t.Fatalf("request count = %d, want 0", count)
	}

	failing := newExtHarness(
		t, permissionsOf(credential.PermissionTrade), http.StatusInternalServerError,
		`{"error":"INTERNAL","message":"try later"}`,
	)
	err = failing.client.SetCFMIntradayMarginSetting(
		context.Background(), SetCFMIntradayMarginSettingRequest{Setting: IntradayMarginSettingIntraday},
	)
	if !errors.Is(err, trade.ErrUnknownExecutionState) {
		t.Fatalf("SetCFMIntradayMarginSetting(500) error = %v, want unknown execution state", err)
	}
	if count := failing.requestCount(); count != 1 {
		t.Fatalf("request count = %d, want 1 (mutations must not be retried)", count)
	}
}

func TestCFMSweepsDecodesList(t *testing.T) {
	t.Parallel()

	harness := newExtHarness(
		t, permissionsOf(credential.PermissionRead), http.StatusOK,
		`{"sweeps":[{"id":"sweep-1","requested_amount":{"value":"100","currency":"USD"},"should_sweep_all":false,`+
			`"status":"SWEEP_STATUS_PENDING","scheduled_time":"2023-07-28T20:00:00Z"},`+
			`{"id":"sweep-2","requested_amount":{"value":"0","currency":"USD"},"should_sweep_all":true,"status":"SWEEP_STATUS_PROCESSING"}]}`,
	)
	sweeps, err := harness.client.CFMSweeps(context.Background())
	if err != nil {
		t.Fatalf("CFMSweeps() error = %v", err)
	}
	if len(sweeps) != 2 || sweeps[0].ID != "sweep-1" || sweeps[0].RequestedAmount.Value != "100" ||
		sweeps[0].RequestedAmount.Currency != "USD" || sweeps[0].ShouldSweepAll ||
		sweeps[0].Status != "SWEEP_STATUS_PENDING" || sweeps[0].ScheduledTime != "2023-07-28T20:00:00Z" ||
		sweeps[1].ID != "sweep-2" || !sweeps[1].ShouldSweepAll || sweeps[1].Status != "SWEEP_STATUS_PROCESSING" ||
		len(sweeps[0].Raw) == 0 || len(sweeps[1].Raw) == 0 {
		t.Fatalf("CFMSweeps() = %+v", sweeps)
	}
	harness.assertSingle(t, http.MethodGet, publicPrefix+"/cfm/sweeps", nil, "")
}

func TestScheduleCFMSweepPostsAmountAndDecodesResult(t *testing.T) {
	t.Parallel()

	harness := newExtHarness(
		t, permissionsOf(credential.PermissionRead, credential.PermissionTransfer), http.StatusOK, `{"success":true}`,
	)
	result, err := harness.client.ScheduleCFMSweep(context.Background(), ScheduleCFMSweepRequest{USDAmount: "100.5"})
	if err != nil {
		t.Fatalf("ScheduleCFMSweep() error = %v", err)
	}
	if !result.Success || len(result.Raw) == 0 {
		t.Fatalf("ScheduleCFMSweep() = %+v", result)
	}
	harness.assertSingle(t, http.MethodPost, publicPrefix+"/cfm/sweeps/schedule", nil, `{"usd_amount":"100.5"}`)
}

func TestScheduleCFMSweepWithoutAmountSweepsEverythingAndReportsRejection(t *testing.T) {
	t.Parallel()

	harness := newExtHarness(t, permissionsOf(credential.PermissionTransfer), http.StatusOK, `{"success":false}`)
	result, err := harness.client.ScheduleCFMSweep(context.Background(), ScheduleCFMSweepRequest{})
	if err != nil {
		t.Fatalf("ScheduleCFMSweep() error = %v", err)
	}
	if result.Success {
		t.Fatalf("ScheduleCFMSweep() = %+v, want Success=false", result)
	}
	harness.assertSingle(t, http.MethodPost, publicPrefix+"/cfm/sweeps/schedule", nil, `{}`)
}

func TestCancelCFMSweepSendsDeleteWithoutBody(t *testing.T) {
	t.Parallel()

	harness := newExtHarness(t, permissionsOf(credential.PermissionTransfer), http.StatusOK, `{"success":true}`)
	result, err := harness.client.CancelCFMSweep(context.Background())
	if err != nil {
		t.Fatalf("CancelCFMSweep() error = %v", err)
	}
	if !result.Success || len(result.Raw) == 0 {
		t.Fatalf("CancelCFMSweep() = %+v", result)
	}
	harness.assertSingle(t, http.MethodDelete, publicPrefix+"/cfm/sweeps", nil, "")
}

func TestCFMSweepMutationsValidationPermissionAndNoRetry(t *testing.T) {
	t.Parallel()

	denied := newExtHarness(
		t, permissionsOf(credential.PermissionRead, credential.PermissionTrade), http.StatusOK, `{"success":true}`,
	)
	for _, amount := range []string{"0", "-1", " 5", "1e3"} {
		if _, err := denied.client.ScheduleCFMSweep(
			context.Background(), ScheduleCFMSweepRequest{USDAmount: amount},
		); !errors.Is(err, trade.ErrValidation) {
			t.Fatalf("ScheduleCFMSweep(%q) error = %v, want validation", amount, err)
		}
	}
	if _, err := denied.client.ScheduleCFMSweep(
		context.Background(), ScheduleCFMSweepRequest{USDAmount: "10"},
	); !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("ScheduleCFMSweep(no transfer) error = %v, want authorization", err)
	}
	if _, err := denied.client.CancelCFMSweep(context.Background()); !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("CancelCFMSweep(no transfer) error = %v, want authorization", err)
	}
	if count := denied.requestCount(); count != 0 {
		t.Fatalf("request count = %d, want 0", count)
	}

	failing := newExtHarness(
		t, permissionsOf(credential.PermissionTransfer), http.StatusServiceUnavailable,
		`{"error":"UNAVAILABLE","message":"try later"}`,
	)
	if _, err := failing.client.CancelCFMSweep(context.Background()); !errors.Is(err, trade.ErrUnknownExecutionState) {
		t.Fatalf("CancelCFMSweep(503) error = %v, want unknown execution state", err)
	}
	if count := failing.requestCount(); count != 1 {
		t.Fatalf("request count = %d, want 1 (mutations must not be retried)", count)
	}
}
