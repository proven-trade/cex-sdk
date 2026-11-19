package coinbase

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
)

func TestEditOrderPostsBodyAndReturnsSuccess(t *testing.T) {
	t.Parallel()

	harness := newExtHarness(
		t, permissionsOf(credential.PermissionRead, credential.PermissionTrade), http.StatusOK,
		`{"success":true,"errors":[]}`,
	)
	result, err := harness.client.EditOrder(
		context.Background(), EditOrderRequest{OrderID: "order-1", Price: "64100.5", Size: "0.02"},
	)
	if err != nil {
		t.Fatalf("EditOrder() error = %v", err)
	}
	if !result.Success || len(result.Errors) != 0 || len(result.Raw) == 0 {
		t.Fatalf("EditOrder() = %+v", result)
	}
	harness.assertSingle(
		t, http.MethodPost, publicPrefix+"/orders/edit", nil,
		`{"order_id":"order-1","price":"64100.5","size":"0.02"}`,
	)
}

func TestEditOrderSendsOptionalFields(t *testing.T) {
	t.Parallel()

	harness := newExtHarness(
		t, permissionsOf(credential.PermissionTrade), http.StatusOK, `{"success":true}`,
	)
	_, err := harness.client.EditOrder(context.Background(), EditOrderRequest{
		OrderID: "order-1", Price: "64100", Size: "0.02", StopPrice: "63000",
		AverageEntryPrice: "64000", CancelAttachedOrder: true,
	})
	if err != nil {
		t.Fatalf("EditOrder() error = %v", err)
	}
	harness.assertSingle(
		t, http.MethodPost, publicPrefix+"/orders/edit", nil,
		`{"order_id":"order-1","price":"64100","size":"0.02","stop_price":"63000","average_entry_price":"64000","cancel_attached_order":true}`,
	)
}

func TestEditOrderMapsRejectionReasonToError(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		response string
		target   error
		code     string
	}{
		{
			name:     "order not found",
			response: `{"success":false,"errors":[{"edit_failure_reason":"ORDER_NOT_FOUND","preview_failure_reason":"UNKNOWN_PREVIEW_FAILURE_REASON"}]}`,
			target:   trade.ErrOrderNotFound, code: "ORDER_NOT_FOUND",
		},
		{
			name:     "invalid edited size",
			response: `{"success":false,"errors":[{"edit_failure_reason":"INVALID_EDITED_SIZE"}]}`,
			target:   trade.ErrValidation, code: "INVALID_EDITED_SIZE",
		},
		{
			name:     "preview failure reason only",
			response: `{"success":false,"errors":[{"preview_failure_reason":"PREVIEW_INSUFFICIENT_FUND"}]}`,
			target:   trade.ErrInsufficientBalance, code: "PREVIEW_INSUFFICIENT_FUND",
		},
		{
			name:     "no reason",
			response: `{"success":false}`,
			target:   trade.ErrInternal, code: "",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			harness := newExtHarness(
				t, permissionsOf(credential.PermissionTrade), http.StatusOK, testCase.response,
			)
			_, err := harness.client.EditOrder(
				context.Background(), EditOrderRequest{OrderID: "order-1", Price: "64100", Size: "0.02"},
			)
			if !errors.Is(err, testCase.target) {
				t.Fatalf("EditOrder() error = %v, want %v", err, testCase.target)
			}
			var apiError *trade.APIError
			if !errors.As(err, &apiError) || apiError.ExchangeCode != testCase.code || apiError.Retryable {
				t.Fatalf("EditOrder() api error = %+v", apiError)
			}
		})
	}
}

func TestEditOrderServerErrorIsUnknownExecutionStateAndNotRetried(t *testing.T) {
	t.Parallel()

	harness := newExtHarness(
		t, permissionsOf(credential.PermissionTrade), http.StatusInternalServerError,
		`{"error":"INTERNAL","message":"try later"}`,
	)
	_, err := harness.client.EditOrder(
		context.Background(), EditOrderRequest{OrderID: "order-1", Price: "64100", Size: "0.02"},
	)
	if !errors.Is(err, trade.ErrUnknownExecutionState) {
		t.Fatalf("EditOrder(500) error = %v, want unknown execution state", err)
	}
	if count := harness.requestCount(); count != 1 {
		t.Fatalf("request count = %d, want 1 (mutations must not be retried)", count)
	}
}

func TestEditOrderRequestValidation(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		request EditOrderRequest
	}{
		{name: "missing order ID", request: EditOrderRequest{Price: "1", Size: "1"}},
		{name: "order ID with whitespace", request: EditOrderRequest{OrderID: " o", Price: "1", Size: "1"}},
		{name: "missing price", request: EditOrderRequest{OrderID: "o", Size: "1"}},
		{name: "zero price", request: EditOrderRequest{OrderID: "o", Price: "0", Size: "1"}},
		{name: "non decimal size", request: EditOrderRequest{OrderID: "o", Price: "1", Size: "1e3"}},
		{name: "negative size", request: EditOrderRequest{OrderID: "o", Price: "1", Size: "-1"}},
		{name: "bad stop price", request: EditOrderRequest{OrderID: "o", Price: "1", Size: "1", StopPrice: "abc"}},
		{name: "bad average entry price", request: EditOrderRequest{OrderID: "o", Price: "1", Size: "1", AverageEntryPrice: "0"}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			harness := newExtHarness(
				t, permissionsOf(credential.PermissionRead, credential.PermissionTrade), http.StatusOK, `{"success":true}`,
			)
			if _, err := harness.client.EditOrder(context.Background(), testCase.request); !errors.Is(err, trade.ErrValidation) {
				t.Fatalf("EditOrder() error = %v, want validation", err)
			}
			if _, err := harness.client.PreviewEditOrder(context.Background(), testCase.request); !errors.Is(err, trade.ErrValidation) {
				t.Fatalf("PreviewEditOrder() error = %v, want validation", err)
			}
			if count := harness.requestCount(); count != 0 {
				t.Fatalf("request count = %d, want 0", count)
			}
		})
	}
}

func TestEditOrderRequiresTradePermission(t *testing.T) {
	t.Parallel()

	harness := newExtHarness(
		t, permissionsOf(credential.PermissionRead), http.StatusOK, `{"success":true}`,
	)
	_, err := harness.client.EditOrder(
		context.Background(), EditOrderRequest{OrderID: "order-1", Price: "64100", Size: "0.02"},
	)
	if !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("EditOrder(read only) error = %v, want authorization", err)
	}
	if count := harness.requestCount(); count != 0 {
		t.Fatalf("request count = %d, want 0", count)
	}
}

func TestPreviewEditOrderUsesReadPermissionAndDecodesPreview(t *testing.T) {
	t.Parallel()

	harness := newExtHarness(
		t, permissionsOf(credential.PermissionRead), http.StatusOK,
		`{"errors":[{"edit_failure_reason":"EDIT_REQUEST_EQUAL_TO_ORIGINAL_REQUEST"}],"slippage":"0.0001","order_total":"1282.05","commission_total":"7.69","quote_size":"1282.05","base_size":"0.02","best_bid":"63999","best_ask":"64001","average_filled_price":"64100","order_margin_total":"0","commission_detail_total":{"total_commission":"7.69","client_commission":"7.69"},"pnl_configuration":{"trigger_bracket_pnl":{}}}`,
	)
	preview, err := harness.client.PreviewEditOrder(
		context.Background(), EditOrderRequest{OrderID: "order-1", Price: "64100", Size: "0.02"},
	)
	if err != nil {
		t.Fatalf("PreviewEditOrder() error = %v", err)
	}
	if len(preview.Errors) != 1 || preview.Errors[0].EditFailureReason != "EDIT_REQUEST_EQUAL_TO_ORIGINAL_REQUEST" ||
		preview.OrderTotal != "1282.05" || preview.CommissionTotal != "7.69" || preview.BaseSize != "0.02" ||
		preview.BestBid != "63999" || preview.AverageFilledPrice != "64100" ||
		preview.CommissionDetail.TotalCommission != "7.69" || !strings.Contains(string(preview.Raw), "trigger_bracket_pnl") {
		t.Fatalf("PreviewEditOrder() = %+v", preview)
	}
	harness.assertSingle(
		t, http.MethodPost, publicPrefix+"/orders/edit_preview", nil,
		`{"order_id":"order-1","price":"64100","size":"0.02"}`,
	)
}

func TestPreviewOrderPostsConfigurationAndDecodesPreview(t *testing.T) {
	t.Parallel()

	harness := newExtHarness(
		t, permissionsOf(credential.PermissionRead), http.StatusOK,
		`{"order_total":"640.5","commission_total":"3.84","errs":["PREVIEW_INSUFFICIENT_FUND"],"warning":["BIG_ORDER"],"quote_size":"640.5","base_size":"0.01","best_bid":"63999","best_ask":"64001","is_max":false,"order_margin_total":"320.25","leverage":"2.0","slippage":"0.0002","preview_id":"preview-1","margin_ratio_data":{"current_margin_ratio":"0.5","projected_margin_ratio":"0.4"},"commission_detail_total":{"total_commission":"3.84"},"est_average_filled_price":"64050","max_loss":"10.00"}`,
	)
	preview, err := harness.client.PreviewOrder(context.Background(), PreviewOrderRequest{
		ProductID: "BTC-USD", Side: SideBuy, Leverage: "2.0", MarginType: MarginTypeCross,
		OrderConfiguration: OrderConfiguration{LimitLimitGTC: &LimitGTCConfiguration{
			BaseSize: "0.01", LimitPrice: "64050",
		}},
	})
	if err != nil {
		t.Fatalf("PreviewOrder() error = %v", err)
	}
	if preview.OrderTotal != "640.5" || preview.CommissionTotal != "3.84" ||
		len(preview.Errs) != 1 || preview.Errs[0] != "PREVIEW_INSUFFICIENT_FUND" ||
		len(preview.Warning) != 1 || preview.Warning[0] != "BIG_ORDER" || preview.IsMax ||
		preview.OrderMarginTotal != "320.25" || preview.Leverage != "2.0" || preview.PreviewID != "preview-1" ||
		preview.MarginRatioData.ProjectedMarginRatio != "0.4" || preview.CommissionDetail.TotalCommission != "3.84" ||
		preview.EstimatedAverageFilledPrice != "64050" || preview.MaxLoss != "10.00" || len(preview.Raw) == 0 {
		t.Fatalf("PreviewOrder() = %+v", preview)
	}
	harness.assertSingle(
		t, http.MethodPost, publicPrefix+"/orders/preview", nil,
		`{"product_id":"BTC-USD","side":"BUY","order_configuration":{"limit_limit_gtc":{"base_size":"0.01","limit_price":"64050"}},"leverage":"2.0","margin_type":"CROSS"}`,
	)
}

func TestPreviewOrderRequestValidation(t *testing.T) {
	t.Parallel()

	valid := OrderConfiguration{MarketMarketIOC: &MarketIOCConfiguration{QuoteSize: "10"}}
	cases := []struct {
		name    string
		request PreviewOrderRequest
	}{
		{name: "missing product", request: PreviewOrderRequest{Side: SideBuy, OrderConfiguration: valid}},
		{name: "bad side", request: PreviewOrderRequest{ProductID: "BTC-USD", Side: "HOLD", OrderConfiguration: valid}},
		{name: "missing configuration", request: PreviewOrderRequest{ProductID: "BTC-USD", Side: SideBuy}},
		{name: "bad leverage", request: PreviewOrderRequest{ProductID: "BTC-USD", Side: SideBuy, OrderConfiguration: valid, Leverage: "0"}},
		{name: "bad margin type", request: PreviewOrderRequest{ProductID: "BTC-USD", Side: SideBuy, OrderConfiguration: valid, MarginType: "PORTFOLIO"}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			harness := newExtHarness(t, permissionsOf(credential.PermissionRead), http.StatusOK, `{}`)
			if _, err := harness.client.PreviewOrder(context.Background(), testCase.request); !errors.Is(err, trade.ErrValidation) {
				t.Fatalf("PreviewOrder() error = %v, want validation", err)
			}
			if count := harness.requestCount(); count != 0 {
				t.Fatalf("request count = %d, want 0", count)
			}
		})
	}
}

func TestClosePositionPostsBodyAndDecodesReference(t *testing.T) {
	t.Parallel()

	harness := newExtHarness(
		t, permissionsOf(credential.PermissionRead, credential.PermissionTrade), http.StatusOK,
		`{"success":true,"success_response":{"order_id":"order-9","product_id":"BIT-28JUL23-CDE","side":"SELL","client_order_id":"close-1"},"order_configuration":{"market_market_ioc":{"base_size":"3"}}}`,
	)
	reference, err := harness.client.ClosePosition(
		context.Background(),
		ClosePositionRequest{ClientOrderID: "close-1", ProductID: "BIT-28JUL23-CDE", Size: "3"},
	)
	if err != nil {
		t.Fatalf("ClosePosition() error = %v", err)
	}
	if reference.OrderID != "order-9" || reference.Side != SideSell || reference.ClientOrderID != "close-1" ||
		len(reference.Raw) == 0 {
		t.Fatalf("ClosePosition() = %+v", reference)
	}
	harness.assertSingle(
		t, http.MethodPost, publicPrefix+"/orders/close_position", nil,
		`{"client_order_id":"close-1","product_id":"BIT-28JUL23-CDE","size":"3"}`,
	)
}

func TestClosePositionOmitsSizeToCloseEverything(t *testing.T) {
	t.Parallel()

	harness := newExtHarness(
		t, permissionsOf(credential.PermissionTrade), http.StatusOK,
		`{"success":true,"success_response":{"order_id":"order-9","product_id":"BIT-28JUL23-CDE","side":"SELL","client_order_id":"close-2"}}`,
	)
	if _, err := harness.client.ClosePosition(
		context.Background(), ClosePositionRequest{ClientOrderID: "close-2", ProductID: "BIT-28JUL23-CDE"},
	); err != nil {
		t.Fatalf("ClosePosition() error = %v", err)
	}
	harness.assertSingle(
		t, http.MethodPost, publicPrefix+"/orders/close_position", nil,
		`{"client_order_id":"close-2","product_id":"BIT-28JUL23-CDE"}`,
	)
}

func TestClosePositionClassifiesFailures(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		status   int
		response string
		target   error
	}{
		{
			name: "insufficient funds", status: http.StatusOK, target: trade.ErrInsufficientBalance,
			response: `{"success":false,"error_response":{"error":"INSUFFICIENT_FUND","message":"insufficient balance","new_order_failure_reason":"INSUFFICIENT_FUND"}}`,
		},
		{
			name: "server error", status: http.StatusInternalServerError, target: trade.ErrUnknownExecutionState,
			response: `{"error":"INTERNAL","message":"try later"}`,
		},
		{
			name: "empty acknowledgement", status: http.StatusOK, target: trade.ErrUnknownExecutionState,
			response: `{"success":true,"success_response":{}}`,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			harness := newExtHarness(
				t, permissionsOf(credential.PermissionTrade), testCase.status, testCase.response,
			)
			_, err := harness.client.ClosePosition(
				context.Background(), ClosePositionRequest{ClientOrderID: "close-3", ProductID: "BIT-28JUL23-CDE"},
			)
			if !errors.Is(err, testCase.target) {
				t.Fatalf("ClosePosition() error = %v, want %v", err, testCase.target)
			}
			if count := harness.requestCount(); count != 1 {
				t.Fatalf("request count = %d, want 1 (mutations must not be retried)", count)
			}
		})
	}
}

func TestClosePositionRequestValidation(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		request ClosePositionRequest
	}{
		{name: "missing client order ID", request: ClosePositionRequest{ProductID: "BIT-28JUL23-CDE"}},
		{name: "client order ID too long", request: ClosePositionRequest{ClientOrderID: strings.Repeat("a", 65), ProductID: "BIT-28JUL23-CDE"}},
		{name: "missing product", request: ClosePositionRequest{ClientOrderID: "close-1"}},
		{name: "bad size", request: ClosePositionRequest{ClientOrderID: "close-1", ProductID: "BIT-28JUL23-CDE", Size: "all"}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			harness := newExtHarness(t, permissionsOf(credential.PermissionTrade), http.StatusOK, `{"success":true}`)
			if _, err := harness.client.ClosePosition(context.Background(), testCase.request); !errors.Is(err, trade.ErrValidation) {
				t.Fatalf("ClosePosition() error = %v, want validation", err)
			}
			if count := harness.requestCount(); count != 0 {
				t.Fatalf("request count = %d, want 0", count)
			}
		})
	}
}

func TestClosePositionRequiresTradePermission(t *testing.T) {
	t.Parallel()

	harness := newExtHarness(t, permissionsOf(credential.PermissionRead), http.StatusOK, `{"success":true}`)
	_, err := harness.client.ClosePosition(
		context.Background(), ClosePositionRequest{ClientOrderID: "close-4", ProductID: "BIT-28JUL23-CDE"},
	)
	if !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("ClosePosition(read only) error = %v, want authorization", err)
	}
	if count := harness.requestCount(); count != 0 {
		t.Fatalf("request count = %d, want 0", count)
	}
}
