package upbit

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
)

const (
	orderChanceJSON = `{"bid_fee":"0.0005","ask_fee":"0.0005","maker_bid_fee":"0.0005","maker_ask_fee":"0.0004","market":{"id":"KRW-BTC","name":"BTC/KRW","order_types":["limit"],"order_sides":["ask","bid"],"bid_types":["best_fok","best_ioc","limit","limit_fok","limit_ioc","price"],"ask_types":["best_fok","best_ioc","limit","limit_fok","limit_ioc","market"],"bid":{"currency":"KRW","min_total":"5000"},"ask":{"currency":"BTC","min_total":"5000"},"max_total":"1000000000","state":"active"},"bid_account":{"currency":"KRW","balance":"10000","locked":"0","avg_buy_price":"0","avg_buy_price_modified":true,"unit_currency":"KRW"},"ask_account":{"currency":"BTC","balance":"0.001","locked":"0.0005","avg_buy_price":"90000000","avg_buy_price_modified":false,"unit_currency":"KRW"}}`
	batchCancelJSON = `{"success":{"count":2,"orders":[{"uuid":"bbbb8e07-1689-4769-af3e-a117016623f8","market":"KRW-ETH"},{"uuid":"4312ba49-5f1a-4a01-9f3b-2d2bce17267e","market":"KRW-ETH","identifier":"strategy-2"}]},"failed":{"count":1,"orders":[{"uuid":"bdb49a54-de36-4eb4-a963-9c8d4337a9da","market":"BTC-XRP","identifier":"strategy-9"}]}}`
)

func TestOrderChanceEndpoint(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.Header().Set("Remaining-Req", "group=default; min=1800; sec=29")
		if got := request.Method + " " + request.URL.RequestURI(); got != "GET /v1/orders/chance?market=KRW-BTC" {
			t.Errorf("unexpected request %s", got)
			http.NotFound(writer, request)
			return
		}
		verifySignedQuery(t, request, "market=KRW-BTC")
		_, _ = io.WriteString(writer, orderChanceJSON)
	}))
	defer server.Close()

	client, provider, limiter := newPrivateTestClient(t, server.URL, credential.PermissionRead)
	chance, err := client.OrderChance(context.Background(), OrderChanceRequest{Market: "KRW-BTC"})
	if err != nil || chance.BidFee != "0.0005" || chance.MakerAskFee != "0.0004" || chance.Market.ID != "KRW-BTC" ||
		chance.Market.Name != "BTC/KRW" || chance.Market.State != "active" || chance.Market.MaxTotal != "1000000000" ||
		!slices.Equal(chance.Market.OrderSides, []Side{SideAsk, SideBid}) || len(chance.Market.BidTypes) != 6 ||
		len(chance.Market.AskTypes) != 6 || chance.Market.Bid.Currency != "KRW" || chance.Market.Bid.MinTotal != "5000" ||
		chance.Market.Ask.Currency != "BTC" || chance.BidAccount.Currency != "KRW" || chance.BidAccount.Balance != "10000" ||
		!chance.BidAccount.AverageBuyPriceModified || chance.AskAccount.Locked != "0.0005" ||
		chance.AskAccount.AverageBuyPrice != "90000000" || chance.AskAccount.AverageBuyPriceModified || len(chance.Raw) == 0 {
		t.Fatalf("OrderChance() = %+v, error = %v", chance, err)
	}
	assertSecretsDestroyed(t, provider, 1)
	assertRateRule(t, limiter, "upbit:account:upbit-pocket:default:1second", 30, time.Second)
}

func TestTestOrderEndpoint(t *testing.T) {
	t.Parallel()

	const wantBody = `{"market":"KRW-BTC","side":"bid","volume":"1.0","price":"140000000","ord_type":"limit","time_in_force":"ioc","identifier":"dry-run-1","smp_type":"cancel_maker"}`
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.Header().Set("Remaining-Req", "group=order-test; min=480; sec=7")
		body, _ := io.ReadAll(request.Body)
		if request.Method != http.MethodPost || request.URL.RequestURI() != "/v1/orders/test" || string(body) != wantBody {
			t.Errorf("unexpected request %s %s %s", request.Method, request.URL.RequestURI(), body)
			http.Error(writer, `{"error":{"name":"invalid_parameter","message":"unexpected request"}}`, http.StatusBadRequest)
			return
		}
		verifySignedBody(t, request, "market=KRW-BTC&side=bid&volume=1.0&price=140000000&ord_type=limit&time_in_force=ioc&identifier=dry-run-1&smp_type=cancel_maker")
		writer.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(writer, `{"market":"KRW-BTC","uuid":"cdd92199-2897-4e14-9448-f923320408ad","side":"bid","ord_type":"limit","price":"140000000","state":"wait","created_at":"2026-11-09T15:00:00+09:00","volume":"1.0","remaining_volume":"1.0","reserved_fee":"70000.0","remaining_fee":"70000.0","paid_fee":"0.0","locked":"0.0","executed_volume":"0.0","time_in_force":"ioc","identifier":"dry-run-1","smp_type":"cancel_maker","prevented_volume":"0","prevented_locked":"0","trades_count":0}`)
	}))
	defer server.Close()

	client, provider, limiter := newPrivateTestClient(t, server.URL, credential.PermissionTrade)
	order, err := client.TestOrder(context.Background(), PlaceOrderRequest{
		Market: "KRW-BTC", Side: SideBid, Volume: "1.0", Price: "140000000", OrderType: OrderTypeLimit,
		TimeInForce: TimeInForceIOC, Identifier: "dry-run-1", SMPType: SMPTypeCancelMaker,
	})
	if err != nil || order.UUID != "cdd92199-2897-4e14-9448-f923320408ad" || order.Side != SideBid || order.OrderType != OrderTypeLimit ||
		order.State != OrderStateWait || order.Price != "140000000" || order.Volume != "1.0" || order.ReservedFee != "70000.0" ||
		order.TimeInForce != TimeInForceIOC || order.Identifier != "dry-run-1" || order.SMPType != SMPTypeCancelMaker ||
		order.PreventedVolume != "0" || order.TradesCount != 0 || len(order.Raw) == 0 {
		t.Fatalf("TestOrder() = %+v, error = %v", order, err)
	}
	assertSecretsDestroyed(t, provider, 1)
	assertRateRule(t, limiter, "upbit:account:upbit-pocket:order-test:1second", 8, time.Second)
}

func TestOrdersByIDsEndpoint(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.Header().Set("Remaining-Req", "group=default; min=1800; sec=29")
		switch request.Method + " " + request.URL.RequestURI() {
		case "GET /v1/orders/uuids?market=KRW-BTC&uuids[]=5d303952-8be9-41e6-915b-121a90026248&uuids[]=3b67e543-8ad3-48d0-8451-0dad315cae73&order_by=asc":
			verifySignedQuery(t, request, "market=KRW-BTC&uuids[]=5d303952-8be9-41e6-915b-121a90026248&uuids[]=3b67e543-8ad3-48d0-8451-0dad315cae73&order_by=asc")
			_, _ = io.WriteString(writer, `[{"uuid":"5d303952-8be9-41e6-915b-121a90026248","side":"bid","ord_type":"limit","price":"155772000","state":"wait","market":"KRW-BTC","created_at":"2025-08-09T16:52:49+09:00","volume":"0.0001","remaining_volume":"0.0001","smp_type":"none","prevented_volume":"0","reserved_fee":"7.7886","remaining_fee":"7.7886","paid_fee":"0","locked":"15584.9886","prevented_locked":"0","executed_volume":"0","trades_count":0},{"uuid":"3b67e543-8ad3-48d0-8451-0dad315cae73","side":"ask","ord_type":"limit","price":"156000000","state":"done","market":"KRW-BTC","created_at":"2025-08-09T16:44:00+09:00","volume":"0.0005","remaining_volume":"0","smp_type":"none","prevented_volume":"0","reserved_fee":"0","remaining_fee":"0","paid_fee":"39","locked":"0","prevented_locked":"0","executed_volume":"0.0005","trades_count":1,"trades":[{"market":"KRW-BTC","uuid":"a1b2c3d4-0000-4000-8000-000000000001","price":"156000000","volume":"0.0005","funds":"78000","side":"ask","created_at":"2025-08-09T16:44:01+09:00"}]}]`)
		case "GET /v1/orders/uuids?identifiers[]=strategy-1&identifiers[]=strategy-2":
			verifySignedQuery(t, request, "identifiers[]=strategy-1&identifiers[]=strategy-2")
			_, _ = io.WriteString(writer, `[{"uuid":"o-1","side":"bid","ord_type":"limit","price":"1000","state":"cancel","market":"KRW-ETH","identifier":"strategy-1"}]`)
		default:
			t.Errorf("unexpected request %s %s", request.Method, request.URL.RequestURI())
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client, provider, limiter := newPrivateTestClient(t, server.URL, credential.PermissionRead)
	ctx := context.Background()

	byUUID, err := client.OrdersByIDs(ctx, OrdersByIDsRequest{
		Market: "KRW-BTC", OrderBy: "asc",
		UUIDs: []string{"5d303952-8be9-41e6-915b-121a90026248", "3b67e543-8ad3-48d0-8451-0dad315cae73"},
	})
	if err != nil || len(byUUID) != 2 || byUUID[0].State != OrderStateWait || byUUID[0].Locked != "15584.9886" ||
		byUUID[0].SMPType != SMPTypeNone || len(byUUID[0].Trades) != 0 || len(byUUID[0].Raw) == 0 ||
		byUUID[1].State != OrderStateDone || byUUID[1].ExecutedVolume != "0.0005" || byUUID[1].TradesCount != 1 ||
		len(byUUID[1].Trades) != 1 || byUUID[1].Trades[0].Funds != "78000" || byUUID[1].Trades[0].Side != SideAsk {
		t.Fatalf("OrdersByIDs(uuids) = %+v, error = %v", byUUID, err)
	}
	byIdentifier, err := client.OrdersByIDs(ctx, OrdersByIDsRequest{Identifiers: []string{"strategy-1", "strategy-2"}})
	if err != nil || len(byIdentifier) != 1 || byIdentifier[0].Identifier != "strategy-1" || byIdentifier[0].State != OrderStateCancel {
		t.Fatalf("OrdersByIDs(identifiers) = %+v, error = %v", byIdentifier, err)
	}
	assertSecretsDestroyed(t, provider, 2)
	assertRateRule(t, limiter, "upbit:account:upbit-pocket:default:1second", 30, time.Second)
}

func TestCancelOrdersByIDsEndpoint(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.Header().Set("Remaining-Req", "group=default; min=1800; sec=29")
		switch request.Method + " " + request.URL.RequestURI() {
		case "DELETE /v1/orders/uuids?uuids[]=bbbb8e07-1689-4769-af3e-a117016623f8&uuids[]=4312ba49-5f1a-4a01-9f3b-2d2bce17267e&uuids[]=bdb49a54-de36-4eb4-a963-9c8d4337a9da":
			verifySignedQuery(t, request, "uuids[]=bbbb8e07-1689-4769-af3e-a117016623f8&uuids[]=4312ba49-5f1a-4a01-9f3b-2d2bce17267e&uuids[]=bdb49a54-de36-4eb4-a963-9c8d4337a9da")
			_, _ = io.WriteString(writer, batchCancelJSON)
		case "DELETE /v1/orders/uuids?identifiers[]=strategy-1":
			verifySignedQuery(t, request, "identifiers[]=strategy-1")
			_, _ = io.WriteString(writer, `{"success":{"count":1,"orders":[{"uuid":"o-1","market":"KRW-ETH","identifier":"strategy-1"}]},"failed":{"count":0,"orders":[]}}`)
		default:
			t.Errorf("unexpected request %s %s", request.Method, request.URL.RequestURI())
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client, provider, limiter := newPrivateTestClient(t, server.URL, credential.PermissionTrade)
	ctx := context.Background()

	partial, err := client.CancelOrdersByIDs(ctx, CancelOrdersByIDsRequest{UUIDs: []string{
		"bbbb8e07-1689-4769-af3e-a117016623f8", "4312ba49-5f1a-4a01-9f3b-2d2bce17267e", "bdb49a54-de36-4eb4-a963-9c8d4337a9da",
	}})
	if err != nil || partial.Success.Count != 2 || len(partial.Success.Orders) != 2 ||
		partial.Success.Orders[0].UUID != "bbbb8e07-1689-4769-af3e-a117016623f8" || partial.Success.Orders[0].Identifier != "" ||
		partial.Success.Orders[1].Identifier != "strategy-2" || partial.Failed.Count != 1 || len(partial.Failed.Orders) != 1 ||
		partial.Failed.Orders[0].Market != "BTC-XRP" || partial.Failed.Orders[0].Identifier != "strategy-9" || len(partial.Raw) == 0 {
		t.Fatalf("CancelOrdersByIDs(uuids) = %+v, error = %v", partial, err)
	}
	byIdentifier, err := client.CancelOrdersByIDs(ctx, CancelOrdersByIDsRequest{Identifiers: []string{"strategy-1"}})
	if err != nil || byIdentifier.Success.Count != 1 || byIdentifier.Success.Orders[0].Identifier != "strategy-1" ||
		byIdentifier.Failed.Count != 0 || len(byIdentifier.Failed.Orders) != 0 {
		t.Fatalf("CancelOrdersByIDs(identifiers) = %+v, error = %v", byIdentifier, err)
	}
	assertSecretsDestroyed(t, provider, 2)
	assertRateRule(t, limiter, "upbit:account:upbit-pocket:default:1second", 30, time.Second)
}

func TestCancelOpenOrdersEndpoint(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		request  CancelOpenOrdersRequest
		wantURI  string
		wantHash string
	}{
		{
			name: "pairs with exclusions",
			request: CancelOpenOrdersRequest{
				Pairs: []string{"KRW-BTC", "KRW-ETH"}, ExcludedPairs: []string{"KRW-XRP"},
				CancelSide: CancelSideBid, Count: 50, OrderBy: "asc",
			},
			wantURI:  "/v1/orders/open?cancel_side=bid&count=50&order_by=asc&pairs=KRW-BTC%2CKRW-ETH&excluded_pairs=KRW-XRP",
			wantHash: "cancel_side=bid&count=50&order_by=asc&pairs=KRW-BTC,KRW-ETH&excluded_pairs=KRW-XRP",
		},
		{
			name:     "quote currencies",
			request:  CancelOpenOrdersRequest{QuoteCurrencies: []string{"KRW", "BTC"}},
			wantURI:  "/v1/orders/open?quote_currencies=KRW%2CBTC",
			wantHash: "quote_currencies=KRW,BTC",
		},
		{
			name:    "all markets",
			request: CancelOpenOrdersRequest{AllMarkets: true},
			wantURI: "/v1/orders/open",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writer.Header().Set("Content-Type", "application/json")
				if got := request.Method + " " + request.URL.RequestURI(); got != "DELETE "+test.wantURI {
					t.Errorf("request = %s, want DELETE %s", got, test.wantURI)
					http.NotFound(writer, request)
					return
				}
				verifySignedQuery(t, request, test.wantHash)
				_, _ = io.WriteString(writer, batchCancelJSON)
			}))
			defer server.Close()

			// 전체 취소는 2초에 한 번만 호출할 수 있으므로 사례마다 클라이언트를 새로 만든다.
			client, provider, limiter := newPrivateTestClient(t, server.URL, credential.PermissionTrade)
			result, err := client.CancelOpenOrders(context.Background(), test.request)
			if err != nil || result.Success.Count != 2 || len(result.Success.Orders) != 2 ||
				result.Success.Orders[1].UUID != "4312ba49-5f1a-4a01-9f3b-2d2bce17267e" || result.Failed.Count != 1 ||
				result.Failed.Orders[0].Market != "BTC-XRP" || len(result.Raw) == 0 {
				t.Fatalf("CancelOpenOrders() = %+v, error = %v", result, err)
			}
			assertSecretsDestroyed(t, provider, 1)
			assertRateRule(t, limiter, "upbit:account:upbit-pocket:order-cancel-all:2seconds", 1, 2*time.Second)
		})
	}
}

func TestCancelAndNewOrderEndpoint(t *testing.T) {
	t.Parallel()

	const (
		replacedBody = `{"prev_order_uuid":"9ca023a5-851b-4fec-9f0a-48cd83c2eaae","new_ord_type":"limit","new_volume":"remain_only","new_price":"1000","new_identifier":"replace-1","new_time_in_force":"ioc","new_smp_type":"none"}`
		filledBody   = `{"prev_order_identifier":"old-1","new_ord_type":"best","new_price":"50000","new_time_in_force":"fok"}`
	)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.Header().Set("Remaining-Req", "group=order; min=720; sec=11")
		body, _ := io.ReadAll(request.Body)
		switch request.Method + " " + request.URL.RequestURI() + " " + string(body) {
		case "POST /v1/orders/cancel_and_new " + replacedBody:
			verifySignedBody(t, request, "prev_order_uuid=9ca023a5-851b-4fec-9f0a-48cd83c2eaae&new_ord_type=limit&new_volume=remain_only&new_price=1000&new_identifier=replace-1&new_time_in_force=ioc&new_smp_type=none")
			writer.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(writer, `{"uuid":"ad217e24-ed02-469c-9b30-c08dbbda6908","side":"bid","ord_type":"limit","price":"100000000","state":"wait","market":"KRW-BTC","created_at":"2025-07-04T15:00:00+09:00","volume":"1","remaining_volume":"1","executed_volume":"0.0","reserved_fee":"70000.0","remaining_fee":"70000.0","paid_fee":"0.0","locked":"100070000.0","smp_type":"none","prevented_volume":"0","prevented_locked":"0","trades_count":0,"new_order_uuid":"4b07aa31-4747-485c-8bce-ac5495e4a639","new_order_identifier":"replace-1"}`)
		case "POST /v1/orders/cancel_and_new " + filledBody:
			verifySignedBody(t, request, "prev_order_identifier=old-1&new_ord_type=best&new_price=50000&new_time_in_force=fok")
			writer.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(writer, `{"uuid":"c0ffee00-0000-4000-8000-000000000002","side":"bid","ord_type":"limit","price":"49000","state":"done","market":"KRW-ETH","created_at":"2025-07-04T15:00:00+09:00","volume":"1","remaining_volume":"0","executed_volume":"1","identifier":"old-1","smp_type":"none","prevented_volume":"0","prevented_locked":"0","trades_count":1}`)
		default:
			t.Errorf("unexpected request %s %s %s", request.Method, request.URL.RequestURI(), body)
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client, provider, limiter := newPrivateTestClient(t, server.URL, credential.PermissionTrade)
	ctx := context.Background()

	replaced, err := client.CancelAndNewOrder(ctx, CancelAndNewOrderRequest{
		PrevOrderUUID: "9ca023a5-851b-4fec-9f0a-48cd83c2eaae", NewOrderType: OrderTypeLimit,
		NewVolume: NewVolumeRemainOnly, NewPrice: "1000", NewIdentifier: "replace-1",
		NewTimeInForce: TimeInForceIOC, NewSMPType: SMPTypeNone,
	})
	if err != nil || replaced.CanceledOrder.UUID != "ad217e24-ed02-469c-9b30-c08dbbda6908" || replaced.CanceledOrder.State != OrderStateWait ||
		replaced.CanceledOrder.Market != "KRW-BTC" || replaced.CanceledOrder.Price != "100000000" || replaced.CanceledOrder.Locked != "100070000.0" ||
		replaced.NewOrderUUID != "4b07aa31-4747-485c-8bce-ac5495e4a639" || replaced.NewOrderIdentifier != "replace-1" ||
		!strings.Contains(string(replaced.Raw), `"new_order_uuid":"4b07aa31-4747-485c-8bce-ac5495e4a639"`) {
		t.Fatalf("CancelAndNewOrder(uuid) = %+v, error = %v", replaced, err)
	}
	filled, err := client.CancelAndNewOrder(ctx, CancelAndNewOrderRequest{
		PrevOrderIdentifier: "old-1", NewOrderType: OrderTypeBest, NewPrice: "50000", NewTimeInForce: TimeInForceFOK,
	})
	if err != nil || filled.CanceledOrder.State != OrderStateDone || filled.CanceledOrder.Identifier != "old-1" ||
		filled.NewOrderUUID != "" || filled.NewOrderIdentifier != "" || len(filled.Raw) == 0 {
		t.Fatalf("CancelAndNewOrder(identifier) = %+v, error = %v", filled, err)
	}
	assertSecretsDestroyed(t, provider, 2)
	assertRateRule(t, limiter, "upbit:account:upbit-pocket:order:1second", 12, time.Second)
}

func TestCancelAndNewOrderResultRejectsNonObject(t *testing.T) {
	t.Parallel()

	var result CancelAndNewOrderResult
	if err := json.Unmarshal([]byte(`[]`), &result); err == nil {
		t.Fatalf("Unmarshal(array) = %+v, want error", result)
	}
	if err := json.Unmarshal([]byte(`{"new_order_uuid":7}`), &result); err == nil {
		t.Fatalf("Unmarshal(numeric new_order_uuid) = %+v, want error", result)
	}
}

func TestOrderExtErrorsAndMutationsAreNotRetried(t *testing.T) {
	t.Parallel()

	var mutations atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.Method + " " + request.URL.Path {
		case "POST /v1/orders/test":
			mutations.Add(1)
			writer.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(writer, `{"error":{"name":"internal_server_error","message":"temporary"}}`)
		case "POST /v1/orders/cancel_and_new":
			mutations.Add(1)
			writer.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(writer, `{"error":{"name":"order_not_found","message":"주문을 찾지 못했습니다."}}`)
		case "DELETE /v1/orders/uuids":
			mutations.Add(1)
			writer.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(writer, `{"error":{"name":"service_unavailable","message":"temporary"}}`)
		case "DELETE /v1/orders/open":
			mutations.Add(1)
			writer.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(writer, `{"error":{"name":"invalid_query_payload","message":"JWT의 query_hash가 일치하지 않습니다."}}`)
		case "GET /v1/orders/chance":
			writer.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(writer, `{"error":{"name":"invalid_parameter","message":"잘못된 파라미터"}}`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client, _, _ := newPrivateTestClient(t, server.URL, credential.PermissionRead, credential.PermissionTrade)
	ctx := context.Background()

	_, err := client.TestOrder(ctx, PlaceOrderRequest{
		Market: "KRW-BTC", Side: SideBid, Volume: "1", Price: "1000", OrderType: OrderTypeLimit,
	})
	requireAPIError(t, err, http.StatusInternalServerError, "internal_server_error")
	if !errors.Is(err, trade.ErrUnknownExecutionState) {
		t.Fatalf("TestOrder() error = %v, want ErrUnknownExecutionState", err)
	}
	_, err = client.CancelAndNewOrder(ctx, CancelAndNewOrderRequest{
		PrevOrderUUID: "gone", NewOrderType: OrderTypeLimit, NewVolume: "1", NewPrice: "1000",
	})
	requireAPIError(t, err, http.StatusNotFound, "order_not_found")
	if !errors.Is(err, trade.ErrOrderNotFound) {
		t.Fatalf("CancelAndNewOrder() error = %v, want ErrOrderNotFound", err)
	}
	_, err = client.CancelOrdersByIDs(ctx, CancelOrdersByIDsRequest{UUIDs: []string{"a"}})
	requireAPIError(t, err, http.StatusServiceUnavailable, "service_unavailable")
	if !errors.Is(err, trade.ErrUnknownExecutionState) {
		t.Fatalf("CancelOrdersByIDs() error = %v, want ErrUnknownExecutionState", err)
	}
	_, err = client.CancelOpenOrders(ctx, CancelOpenOrdersRequest{AllMarkets: true})
	requireAPIError(t, err, http.StatusBadRequest, "invalid_query_payload")
	if !errors.Is(err, trade.ErrAuthentication) {
		t.Fatalf("CancelOpenOrders() error = %v, want ErrAuthentication", err)
	}
	if got := mutations.Load(); got != 4 {
		t.Fatalf("mutation requests = %d, want 4 without retry", got)
	}
	_, err = client.OrderChance(ctx, OrderChanceRequest{Market: "KRW-BTC"})
	requireAPIError(t, err, http.StatusBadRequest, "invalid_parameter")
}

func TestOrderExtPermissionBoundaries(t *testing.T) {
	t.Parallel()

	mutations := []callCase{
		{"TestOrder needs trade", func(ctx context.Context, client *Client) error {
			_, err := client.TestOrder(ctx, PlaceOrderRequest{
				Market: "KRW-BTC", Side: SideBid, Volume: "1", Price: "1000", OrderType: OrderTypeLimit,
			})
			return err
		}},
		{"CancelOrdersByIDs needs trade", func(ctx context.Context, client *Client) error {
			_, err := client.CancelOrdersByIDs(ctx, CancelOrdersByIDsRequest{UUIDs: []string{"a"}})
			return err
		}},
		{"CancelOpenOrders needs trade", func(ctx context.Context, client *Client) error {
			_, err := client.CancelOpenOrders(ctx, CancelOpenOrdersRequest{AllMarkets: true})
			return err
		}},
		{"CancelAndNewOrder needs trade", func(ctx context.Context, client *Client) error {
			_, err := client.CancelAndNewOrder(ctx, CancelAndNewOrderRequest{
				PrevOrderUUID: "a", NewOrderType: OrderTypeLimit, NewVolume: "1", NewPrice: "1000",
			})
			return err
		}},
	}
	assertRejectedBeforeSend(
		t, trade.ErrAuthorization, mutations,
		credential.PermissionRead, credential.PermissionTransfer, credential.PermissionWithdraw,
	)

	reads := []callCase{
		{"OrderChance needs read", func(ctx context.Context, client *Client) error {
			_, err := client.OrderChance(ctx, OrderChanceRequest{Market: "KRW-BTC"})
			return err
		}},
		{"OrdersByIDs needs read", func(ctx context.Context, client *Client) error {
			_, err := client.OrdersByIDs(ctx, OrdersByIDsRequest{UUIDs: []string{"a"}})
			return err
		}},
	}
	assertRejectedBeforeSend(t, trade.ErrAuthorization, reads, credential.PermissionTrade)
}

func TestOrderExtRequestsAreValidatedBeforeSend(t *testing.T) {
	t.Parallel()

	cases := []callCase{
		{"OrderChance missing market", func(ctx context.Context, client *Client) error {
			_, err := client.OrderChance(ctx, OrderChanceRequest{})
			return err
		}},
		{"TestOrder zero volume", func(ctx context.Context, client *Client) error {
			_, err := client.TestOrder(ctx, PlaceOrderRequest{
				Market: "KRW-BTC", Side: SideBid, Volume: "0", Price: "1000", OrderType: OrderTypeLimit,
			})
			return err
		}},
		{"OrdersByIDs without ids", func(ctx context.Context, client *Client) error {
			_, err := client.OrdersByIDs(ctx, OrdersByIDsRequest{Market: "KRW-BTC"})
			return err
		}},
		{"OrdersByIDs uuids and identifiers", func(ctx context.Context, client *Client) error {
			_, err := client.OrdersByIDs(ctx, OrdersByIDsRequest{UUIDs: []string{"a"}, Identifiers: []string{"b"}})
			return err
		}},
		{"CancelOrdersByIDs above 20", func(ctx context.Context, client *Client) error {
			_, err := client.CancelOrdersByIDs(ctx, CancelOrdersByIDsRequest{UUIDs: numberedValues("uuid-", 21)})
			return err
		}},
		{"CancelOpenOrders without scope", func(ctx context.Context, client *Client) error {
			_, err := client.CancelOpenOrders(ctx, CancelOpenOrdersRequest{})
			return err
		}},
		{"CancelOpenOrders count above 300", func(ctx context.Context, client *Client) error {
			_, err := client.CancelOpenOrders(ctx, CancelOpenOrdersRequest{AllMarkets: true, Count: 301})
			return err
		}},
		{"CancelAndNewOrder without previous order", func(ctx context.Context, client *Client) error {
			_, err := client.CancelAndNewOrder(ctx, CancelAndNewOrderRequest{NewOrderType: OrderTypeLimit, NewVolume: "1", NewPrice: "1000"})
			return err
		}},
		{"CancelAndNewOrder limit without price", func(ctx context.Context, client *Client) error {
			_, err := client.CancelAndNewOrder(ctx, CancelAndNewOrderRequest{PrevOrderUUID: "a", NewOrderType: OrderTypeLimit, NewVolume: "1"})
			return err
		}},
	}
	assertRejectedBeforeSend(t, trade.ErrValidation, cases, credential.PermissionRead, credential.PermissionTrade)
}

func TestOrderExtRequestValidation(t *testing.T) {
	t.Parallel()

	valid := []depositValidationCase{
		{"chance", OrderChanceRequest{Market: "KRW-BTC"}.validate()},
		{"by uuids", OrdersByIDsRequest{UUIDs: numberedValues("uuid-", 100)}.validate()},
		{"by identifiers with market", OrdersByIDsRequest{Market: "KRW-BTC", Identifiers: []string{"a"}, OrderBy: "desc"}.validate()},
		{"cancel by uuids maximum", CancelOrdersByIDsRequest{UUIDs: numberedValues("uuid-", 20)}.validate()},
		{"cancel by identifiers", CancelOrdersByIDsRequest{Identifiers: []string{"a", "b"}}.validate()},
		{"cancel open all markets", CancelOpenOrdersRequest{AllMarkets: true}.validate()},
		{"cancel open pairs", CancelOpenOrdersRequest{Pairs: numberedPairs(20), ExcludedPairs: []string{"KRW-XRP"}, CancelSide: CancelSideAsk, Count: 300, OrderBy: "asc"}.validate()},
		{"cancel open quote currencies", CancelOpenOrdersRequest{QuoteCurrencies: []string{"KRW"}, ExcludedPairs: []string{"KRW-XRP"}, CancelSide: CancelSideAll}.validate()},
		{"new limit", CancelAndNewOrderRequest{PrevOrderUUID: "a", NewOrderType: OrderTypeLimit, NewVolume: "1.5", NewPrice: "1000"}.validate()},
		{"new limit remain only", CancelAndNewOrderRequest{PrevOrderIdentifier: "a", NewOrderType: OrderTypeLimit, NewVolume: NewVolumeRemainOnly, NewPrice: "1000", NewIdentifier: "b"}.validate()},
		{"new limit post only none", CancelAndNewOrderRequest{PrevOrderUUID: "a", NewOrderType: OrderTypeLimit, NewVolume: "1", NewPrice: "1000", NewTimeInForce: TimeInForcePostOnly, NewSMPType: SMPTypeNone}.validate()},
		{"new price", CancelAndNewOrderRequest{PrevOrderUUID: "a", NewOrderType: OrderTypePrice, NewPrice: "5000"}.validate()},
		{"new market", CancelAndNewOrderRequest{PrevOrderUUID: "a", NewOrderType: OrderTypeMarket, NewVolume: "0.5", NewSMPType: SMPTypeReduce}.validate()},
		{"new best bid", CancelAndNewOrderRequest{PrevOrderUUID: "a", NewOrderType: OrderTypeBest, NewPrice: "10000", NewTimeInForce: TimeInForceIOC}.validate()},
		{"new best ask remain only", CancelAndNewOrderRequest{PrevOrderUUID: "a", NewOrderType: OrderTypeBest, NewVolume: NewVolumeRemainOnly, NewTimeInForce: TimeInForceFOK}.validate()},
	}
	invalid := []depositValidationCase{
		{"chance empty market", OrderChanceRequest{}.validate()},
		{"chance malformed market", OrderChanceRequest{Market: "BTC"}.validate()},
		{"by ids neither", OrdersByIDsRequest{Market: "KRW-BTC"}.validate()},
		{"by ids both", OrdersByIDsRequest{UUIDs: []string{"a"}, Identifiers: []string{"b"}}.validate()},
		{"by ids above 100", OrdersByIDsRequest{UUIDs: numberedValues("uuid-", 101)}.validate()},
		{"by ids duplicate", OrdersByIDsRequest{Identifiers: []string{"a", "a"}}.validate()},
		{"by ids whitespace", OrdersByIDsRequest{UUIDs: []string{"a b"}}.validate()},
		{"by ids malformed market", OrdersByIDsRequest{Market: "KRW BTC", UUIDs: []string{"a"}}.validate()},
		{"by ids unsupported order", OrdersByIDsRequest{UUIDs: []string{"a"}, OrderBy: "up"}.validate()},
		{"cancel by ids neither", CancelOrdersByIDsRequest{}.validate()},
		{"cancel by ids both", CancelOrdersByIDsRequest{UUIDs: []string{"a"}, Identifiers: []string{"b"}}.validate()},
		{"cancel by ids above 20", CancelOrdersByIDsRequest{Identifiers: numberedValues("id-", 21)}.validate()},
		{"cancel open without scope", CancelOpenOrdersRequest{}.validate()},
		{"cancel open pairs and quote currencies", CancelOpenOrdersRequest{Pairs: []string{"KRW-BTC"}, QuoteCurrencies: []string{"KRW"}}.validate()},
		{"cancel open pairs and all markets", CancelOpenOrdersRequest{Pairs: []string{"KRW-BTC"}, AllMarkets: true}.validate()},
		{"cancel open quote currencies and all markets", CancelOpenOrdersRequest{QuoteCurrencies: []string{"KRW"}, AllMarkets: true}.validate()},
		{"cancel open lowercase quote currency", CancelOpenOrdersRequest{QuoteCurrencies: []string{"krw"}}.validate()},
		{"cancel open pairs above 20", CancelOpenOrdersRequest{Pairs: numberedPairs(21)}.validate()},
		{"cancel open excluded pairs above 20", CancelOpenOrdersRequest{AllMarkets: true, ExcludedPairs: numberedPairs(21)}.validate()},
		{"cancel open duplicate pair", CancelOpenOrdersRequest{Pairs: []string{"KRW-BTC", "KRW-BTC"}}.validate()},
		{"cancel open malformed pair", CancelOpenOrdersRequest{Pairs: []string{"KRWBTC"}}.validate()},
		{"cancel open unsupported side", CancelOpenOrdersRequest{AllMarkets: true, CancelSide: "buy"}.validate()},
		{"cancel open count above 300", CancelOpenOrdersRequest{AllMarkets: true, Count: 301}.validate()},
		{"cancel open negative count", CancelOpenOrdersRequest{AllMarkets: true, Count: -1}.validate()},
		{"cancel open unsupported order", CancelOpenOrdersRequest{AllMarkets: true, OrderBy: "up"}.validate()},
		{"new neither previous", CancelAndNewOrderRequest{NewOrderType: OrderTypeLimit, NewVolume: "1", NewPrice: "1000"}.validate()},
		{"new both previous", CancelAndNewOrderRequest{PrevOrderUUID: "a", PrevOrderIdentifier: "b", NewOrderType: OrderTypeLimit, NewVolume: "1", NewPrice: "1000"}.validate()},
		{"new limit without price", CancelAndNewOrderRequest{PrevOrderUUID: "a", NewOrderType: OrderTypeLimit, NewVolume: "1"}.validate()},
		{"new limit zero volume", CancelAndNewOrderRequest{PrevOrderUUID: "a", NewOrderType: OrderTypeLimit, NewVolume: "0", NewPrice: "1000"}.validate()},
		{"new price with volume", CancelAndNewOrderRequest{PrevOrderUUID: "a", NewOrderType: OrderTypePrice, NewVolume: "1", NewPrice: "5000"}.validate()},
		{"new market with price", CancelAndNewOrderRequest{PrevOrderUUID: "a", NewOrderType: OrderTypeMarket, NewVolume: "1", NewPrice: "5000"}.validate()},
		{"new best without time in force", CancelAndNewOrderRequest{PrevOrderUUID: "a", NewOrderType: OrderTypeBest, NewPrice: "10000"}.validate()},
		{"new best with post only", CancelAndNewOrderRequest{PrevOrderUUID: "a", NewOrderType: OrderTypeBest, NewPrice: "10000", NewTimeInForce: TimeInForcePostOnly}.validate()},
		{"new best with price and volume", CancelAndNewOrderRequest{PrevOrderUUID: "a", NewOrderType: OrderTypeBest, NewPrice: "10000", NewVolume: "1", NewTimeInForce: TimeInForceIOC}.validate()},
		{"new best with neither", CancelAndNewOrderRequest{PrevOrderUUID: "a", NewOrderType: OrderTypeBest, NewTimeInForce: TimeInForceIOC}.validate()},
		{"new unsupported type", CancelAndNewOrderRequest{PrevOrderUUID: "a", NewOrderType: "stop", NewVolume: "1", NewPrice: "1000"}.validate()},
		{"new time in force on market", CancelAndNewOrderRequest{PrevOrderUUID: "a", NewOrderType: OrderTypeMarket, NewVolume: "1", NewTimeInForce: TimeInForceIOC}.validate()},
		{"new unsupported time in force", CancelAndNewOrderRequest{PrevOrderUUID: "a", NewOrderType: OrderTypeLimit, NewVolume: "1", NewPrice: "1000", NewTimeInForce: "gtc"}.validate()},
		{"new unsupported smp", CancelAndNewOrderRequest{PrevOrderUUID: "a", NewOrderType: OrderTypeLimit, NewVolume: "1", NewPrice: "1000", NewSMPType: "cancel_both"}.validate()},
		{"new post only with smp", CancelAndNewOrderRequest{PrevOrderUUID: "a", NewOrderType: OrderTypeLimit, NewVolume: "1", NewPrice: "1000", NewTimeInForce: TimeInForcePostOnly, NewSMPType: SMPTypeReduce}.validate()},
		{"new identifier whitespace", CancelAndNewOrderRequest{PrevOrderUUID: "a", NewOrderType: OrderTypeLimit, NewVolume: "1", NewPrice: "1000", NewIdentifier: " b"}.validate()},
		{"new identifier too long", CancelAndNewOrderRequest{PrevOrderUUID: "a", NewOrderType: OrderTypeLimit, NewVolume: "1", NewPrice: "1000", NewIdentifier: strings.Repeat("b", 65)}.validate()},
		{"new identifier equals previous", CancelAndNewOrderRequest{PrevOrderIdentifier: "same", NewOrderType: OrderTypeLimit, NewVolume: "1", NewPrice: "1000", NewIdentifier: "same"}.validate()},
	}
	assertValidationCases(t, valid, invalid)
}
