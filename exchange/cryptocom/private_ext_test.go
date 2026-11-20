package cryptocom

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	"github.com/proven-trade/cex-sdk/transport"
)

type capturedRequests struct {
	mu        sync.Mutex
	envelopes map[string]privateRequestEnvelope
	calls     map[string]int
}

func (captured *capturedRequests) envelope(method string) privateRequestEnvelope {
	captured.mu.Lock()
	defer captured.mu.Unlock()
	return captured.envelopes[method]
}

func (captured *capturedRequests) callCount(method string) int {
	captured.mu.Lock()
	defer captured.mu.Unlock()
	return captured.calls[method]
}

// newExtServer는 서명을 검증하고 method별 고정 result를 돌려주는 private 테스트 서버다.
func newExtServer(
	t *testing.T,
	now time.Time,
	results map[string]string,
) (*httptest.Server, *capturedRequests) {
	t.Helper()
	captured := &capturedRequests{
		envelopes: make(map[string]privateRequestEnvelope), calls: make(map[string]int),
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		envelope, ok := verifyPrivateRequest(t, request, now)
		if !ok {
			http.Error(writer, `{"id":"0","method":"","code":"40101","message":"UNAUTHORIZED"}`, http.StatusUnauthorized)
			return
		}
		captured.mu.Lock()
		captured.envelopes[envelope.Method] = envelope
		captured.calls[envelope.Method]++
		captured.mu.Unlock()
		result, found := results[envelope.Method]
		if !found {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		if result == "" {
			_, _ = writer.Write([]byte(`{"id":"` + envelope.ID + `","method":"` + envelope.Method + `","code":"0"}`))
			return
		}
		writePrivateSuccess(writer, envelope, result)
	}))
	t.Cleanup(server.Close)
	return server, captured
}

func newExtClient(
	t *testing.T,
	server *httptest.Server,
	now time.Time,
	permissions ...credential.Permission,
) *Client {
	t.Helper()
	client, _ := newPrivateTestClient(
		t, server.URL+"/exchange/v1", &directSender{}, &recordingProvider{},
		[]transport.EgressRouteID{"route-a"}, permissions, now,
	)
	return client
}

func mapParam(t *testing.T, params map[string]any, key string) map[string]any {
	t.Helper()
	value, ok := params[key].(map[string]any)
	if !ok {
		t.Fatalf("params[%q] = %#v, want object", key, params[key])
	}
	return value
}

func listParam(t *testing.T, params map[string]any, key string) []map[string]any {
	t.Helper()
	items, ok := params[key].([]any)
	if !ok {
		t.Fatalf("params[%q] = %#v, want array", key, params[key])
	}
	result := make([]map[string]any, len(items))
	for index, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("params[%q][%d] = %#v, want object", key, index, item)
		}
		result[index] = entry
	}
	return result
}

func TestClientTradeExtensions(t *testing.T) {
	t.Parallel()
	now := time.UnixMilli(1_700_000_000_000)
	server, captured := newExtServer(t, now, map[string]string{
		methodAmendOrder:       `{"client_oid":"amend-1","order_id":"18342311"}`,
		methodCancelAllOrders:  "",
		methodCreateOrderList:  `[{"code":"0","index":"0","client_oid":"leg-1","order_id":"5755600460443882762"},{"code":"306","index":1,"client_oid":"leg-2","message":"INSUFFICIENT_AVAILABLE_BALANCE"}]`,
		methodCancelOrderList:  `[{"code":"0","index":"0"},{"code":"212","index":"1","message":"NOT_FOUND"}]`,
		methodGetFeeRate:       `{"spot_tier":"3","deriv_tier":"2","effective_spot_maker_rate_bps":"6.5","effective_spot_taker_rate_bps":"6.9","effective_deriv_maker_rate_bps":"1.1","effective_deriv_taker_rate_bps":"3"}`,
		methodGetInstrumentFee: `{"instrument_name":"BTC_USDT","effective_maker_rate_bps":"6.5","effective_taker_rate_bps":"6.9"}`,
	})
	client := newExtClient(t, server, now, credential.PermissionRead, credential.PermissionTrade)
	ctx := context.Background()

	amended, err := client.AmendOrder(ctx, AmendOrderRequest{
		OrderID: "18342311", NewClientOrderID: "amend-1", NewPrice: "64100", NewQuantity: "0.2",
	})
	params := captured.envelope(methodAmendOrder).Params
	if err != nil || amended.OrderID != "18342311" || amended.ClientOrderID != "amend-1" ||
		stringParam(params, "order_id") != "18342311" || stringParam(params, "new_price") != "64100" ||
		stringParam(params, "new_quantity") != "0.2" || stringParam(params, "client_oid") != "amend-1" ||
		stringParam(params, "orig_client_oid") != "" {
		t.Fatalf("AmendOrder() = %+v, params = %+v, error = %v", amended, params, err)
	}
	if _, err := client.AmendOrder(ctx, AmendOrderRequest{
		OriginalClientOrderID: "strategy-1", NewPrice: "1", NewQuantity: "1",
	}); err != nil {
		// 응답의 order_id가 요청 식별자와 충돌하지 않으면 orig_client_oid만으로도 접수된다.
		t.Fatalf("AmendOrder(orig_client_oid) error = %v", err)
	}
	if got := stringParam(captured.envelope(methodAmendOrder).Params, "orig_client_oid"); got != "strategy-1" {
		t.Fatalf("orig_client_oid = %q", got)
	}

	ack, err := client.CancelAllOrders(ctx, CancelAllOrdersRequest{InstrumentName: "BTC_USDT", Type: CancelAllLimit})
	params = captured.envelope(methodCancelAllOrders).Params
	if err != nil || len(ack.Raw) != 0 || stringParam(params, "instrument_name") != "BTC_USDT" ||
		stringParam(params, "type") != "LIMIT" {
		t.Fatalf("CancelAllOrders() = %+v, params = %+v, error = %v", ack, params, err)
	}

	created, err := client.CreateOrderList(ctx, CreateOrderListRequest{Orders: []PlaceOrderRequest{
		validLimitOrder(),
		{InstrumentName: "ETH_USDT", Side: OrderSideSell, Type: OrderTypeMarket, Quantity: "1", ClientOrderID: "leg-2"},
	}})
	params = captured.envelope(methodCreateOrderList).Params
	orders := listParam(t, params, "order_list")
	if err != nil || len(created) != 2 || created[0].OrderID != "5755600460443882762" ||
		created[0].Code != 0 || created[1].Code != 306 || created[1].Index != 1 ||
		created[1].Message != "INSUFFICIENT_AVAILABLE_BALANCE" ||
		stringParam(params, "contingency_type") != "LIST" || len(orders) != 2 ||
		stringParam(orders[0], "client_oid") != "strategy-1" || stringParam(orders[1], "type") != "MARKET" {
		t.Fatalf("CreateOrderList() = %+v, params = %+v, error = %v", created, params, err)
	}

	canceled, err := client.CancelOrderList(ctx, CancelOrderListRequest{Orders: []CancelOrderListItem{
		{InstrumentName: "BTC_USDT", OrderID: "18342311"}, {OrderID: "18342312", ClientOrderID: "c-2"},
	}})
	params = captured.envelope(methodCancelOrderList).Params
	orders = listParam(t, params, "order_list")
	if err != nil || len(canceled) != 2 || canceled[1].Code != 212 ||
		stringParam(params, "contingency_type") != "LIST" ||
		stringParam(orders[0], "instrument_name") != "BTC_USDT" || stringParam(orders[1], "client_oid") != "c-2" {
		t.Fatalf("CancelOrderList() = %+v, params = %+v, error = %v", canceled, params, err)
	}

	fee, err := client.FeeRate(ctx)
	if err != nil || fee.SpotTier != 3 || fee.DerivativesTier != 2 ||
		fee.EffectiveSpotMakerBps != "6.5" || fee.EffectiveDerivTakerBps != "3" || len(fee.Raw) == 0 {
		t.Fatalf("FeeRate() = %+v, error = %v", fee, err)
	}
	instrumentFee, err := client.InstrumentFeeRate(ctx, InstrumentFeeRateRequest{InstrumentName: "BTC_USDT"})
	if err != nil || instrumentFee.EffectiveMakerBps != "6.5" || instrumentFee.EffectiveTakerBps != "6.9" ||
		stringParam(captured.envelope(methodGetInstrumentFee).Params, "instrument_name") != "BTC_USDT" {
		t.Fatalf("InstrumentFeeRate() = %+v, error = %v", instrumentFee, err)
	}
}

func TestClientTradeExtensionValidationAndPermissions(t *testing.T) {
	t.Parallel()
	now := time.UnixMilli(1_700_000_000_000)
	server, captured := newExtServer(t, now, map[string]string{})
	readOnly := newExtClient(t, server, now, credential.PermissionRead)
	ctx := context.Background()

	_, err := readOnly.AmendOrder(ctx, AmendOrderRequest{
		OrderID: "1", NewPrice: "1", NewQuantity: "1",
	})
	var apiError *trade.APIError
	if !errors.As(err, &apiError) || apiError.Category != trade.ErrorAuthorization {
		t.Fatalf("AmendOrder(read-only) error = %v, want authorization", err)
	}
	if _, err := readOnly.CancelAllOrders(ctx, CancelAllOrdersRequest{}); !errors.As(err, &apiError) ||
		apiError.Category != trade.ErrorAuthorization {
		t.Fatalf("CancelAllOrders(read-only) error = %v, want authorization", err)
	}
	if captured.callCount(methodAmendOrder)+captured.callCount(methodCancelAllOrders) != 0 {
		t.Fatal("unauthorized mutation reached the server")
	}

	trader := newExtClient(t, server, now, credential.PermissionTrade)
	tooMany := make([]PlaceOrderRequest, maximumOrderListSize+1)
	for index := range tooMany {
		tooMany[index] = validLimitOrder()
	}
	invalid := []struct {
		name string
		call func() error
	}{
		{"amend without identity", func() error {
			_, err := trader.AmendOrder(ctx, AmendOrderRequest{NewPrice: "1", NewQuantity: "1"})
			return err
		}},
		{"amend zero quantity", func() error {
			_, err := trader.AmendOrder(ctx, AmendOrderRequest{OrderID: "1", NewPrice: "1", NewQuantity: "0"})
			return err
		}},
		{"cancel all trigger type", func() error {
			_, err := trader.CancelAllOrders(ctx, CancelAllOrdersRequest{Type: CancelAllTrigger})
			return err
		}},
		{"empty order list", func() error {
			_, err := trader.CreateOrderList(ctx, CreateOrderListRequest{})
			return err
		}},
		{"too many orders", func() error {
			_, err := trader.CreateOrderList(ctx, CreateOrderListRequest{Orders: tooMany})
			return err
		}},
		{"cancel list bad id", func() error {
			_, err := trader.CancelOrderList(ctx, CancelOrderListRequest{Orders: []CancelOrderListItem{{OrderID: "abc"}}})
			return err
		}},
		{"instrument fee bad name", func() error {
			_, err := trader.InstrumentFeeRate(ctx, InstrumentFeeRateRequest{InstrumentName: "btc"})
			return err
		}},
	}
	for _, test := range invalid {
		if err := test.call(); err == nil {
			t.Fatalf("%s: error = nil", test.name)
		}
	}
	if len(captured.envelopes) != 0 {
		t.Fatalf("invalid requests reached the server: %+v", captured.envelopes)
	}
}

func TestOrderListMismatchedIndexIsUnknownExecutionState(t *testing.T) {
	t.Parallel()
	now := time.UnixMilli(1_700_000_000_000)
	server, captured := newExtServer(t, now, map[string]string{
		methodCreateOrderList: `[{"code":"0","index":"5","order_id":"1"}]`,
	})
	client := newExtClient(t, server, now, credential.PermissionTrade)
	_, err := client.CreateOrderList(context.Background(), CreateOrderListRequest{
		Orders: []PlaceOrderRequest{validLimitOrder()},
	})
	var apiError *trade.APIError
	if !errors.As(err, &apiError) || apiError.Category != trade.ErrorUnknownExecutionState || apiError.Retryable {
		t.Fatalf("CreateOrderList() error = %v, want unknown execution state", err)
	}
	if captured.callCount(methodCreateOrderList) != 1 {
		t.Fatalf("mutation was sent %d times, want exactly once", captured.callCount(methodCreateOrderList))
	}
}
