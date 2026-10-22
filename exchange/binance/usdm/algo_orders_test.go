package usdm

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/transport"
)

func TestAlgoOrderLifecycle(t *testing.T) {
	t.Parallel()

	fixedNow := time.UnixMilli(1_700_000_000_000)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if !verifySignedRequest(request, []byte("secret-key"), fixedNow) {
			http.Error(writer, `{"code":-1022,"msg":"Signature invalid."}`, http.StatusBadRequest)
			return
		}
		query := request.URL.Query()
		switch request.URL.Path {
		case "/fapi/v1/algoOrder":
			switch request.Method {
			case http.MethodPost:
				if query.Get("algoType") != "CONDITIONAL" || query.Get("type") != "TAKE_PROFIT" || query.Get("triggerPrice") != "750" || query.Get("price") != "750" || query.Get("quantity") != "0.01" || query.Get("clientAlgoId") != "tp-1" || query.Get("timeInForce") != "GTC" {
					http.Error(writer, `{"code":-1102,"msg":"bad algo order"}`, http.StatusBadRequest)
					return
				}
				writer.Header().Set("X-MBX-ORDER-COUNT-10S", "1")
				writer.Header().Set("X-MBX-ORDER-COUNT-1M", "1")
				_, _ = io.WriteString(writer, `{"algoId":2146760,"clientAlgoId":"tp-1","algoType":"CONDITIONAL","orderType":"TAKE_PROFIT","symbol":"BNBUSDT","side":"SELL","positionSide":"BOTH","timeInForce":"GTC","quantity":"0.01","algoStatus":"NEW","triggerPrice":"750.000","price":"750.000","workingType":"CONTRACT_PRICE","priceMatch":"NONE","closePosition":false,"priceProtect":false,"reduceOnly":false,"activatePrice":"","callbackRate":"","createTime":1750485492076,"updateTime":1750485492076,"triggerTime":0,"goodTillDate":0}`)
			case http.MethodGet:
				if query.Get("algoId") != "2146760" || query.Has("symbol") {
					http.Error(writer, `{"code":-1102,"msg":"bad algo query"}`, http.StatusBadRequest)
					return
				}
				_, _ = io.WriteString(writer, `{"algoId":2146760,"clientAlgoId":"tp-1","algoStatus":"NEW","symbol":"BNBUSDT"}`)
			case http.MethodDelete:
				if query.Get("clientAlgoId") != "tp-1" || query.Has("algoId") {
					http.Error(writer, `{"code":-1102,"msg":"bad algo cancel"}`, http.StatusBadRequest)
					return
				}
				_, _ = io.WriteString(writer, `{"algoId":2146760,"clientAlgoId":"tp-1","code":"200","msg":"success"}`)
			}
		case "/fapi/v1/openAlgoOrders":
			_, _ = io.WriteString(writer, `[{"algoId":2146760,"clientAlgoId":"tp-1","algoStatus":"NEW","symbol":"BNBUSDT"}]`)
		case "/fapi/v1/allAlgoOrders":
			if query.Get("symbol") != "BNBUSDT" || query.Get("limit") != "10" {
				http.Error(writer, `{"code":-1102,"msg":"bad algo history"}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `[{"algoId":2146759,"clientAlgoId":"sl-1","algoStatus":"FINISHED","symbol":"BNBUSDT"}]`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	sender := &directSender{}
	provider := &recordingProvider{}
	client, limiter := newTestClient(t, server.URL, sender, provider, fixedNow, []transport.EgressRouteID{"route-a", "route-b"})
	placed, err := client.PlaceAlgoOrder(context.Background(), PlaceAlgoOrderRequest{Symbol: "BNBUSDT", Side: SideSell, Type: OrderTypeTakeProfit, TimeInForce: TimeInForceGTC, Quantity: "0.01", Price: "750", TriggerPrice: "750", ClientAlgoID: "tp-1"}, trade.WithEgressRoute("route-b"))
	if err != nil || placed.AlgoID != 2146760 || placed.AlgoStatus != AlgoStatusNew || placed.OrderType != OrderTypeTakeProfit || len(placed.Raw) == 0 {
		t.Fatalf("PlaceAlgoOrder() = %+v, error = %v", placed, err)
	}
	id := int64(2146760)
	queried, err := client.AlgoOrderInfo(context.Background(), AlgoOrderInfoRequest{AlgoID: &id})
	if err != nil || queried.ClientAlgoID != "tp-1" {
		t.Fatalf("AlgoOrderInfo() = %+v, error = %v", queried, err)
	}
	canceled, err := client.CancelAlgoOrder(context.Background(), AlgoOrderInfoRequest{ClientAlgoID: "tp-1"})
	if err != nil || canceled.AlgoID != 2146760 {
		t.Fatalf("CancelAlgoOrder() = %+v, error = %v", canceled, err)
	}
	open, err := client.OpenAlgoOrders(context.Background(), OpenAlgoOrdersRequest{AllSymbols: true})
	if err != nil || len(open) != 1 || len(open[0].Raw) == 0 {
		t.Fatalf("OpenAlgoOrders() = %+v, error = %v", open, err)
	}
	history, err := client.AlgoOrderHistory(context.Background(), AlgoOrderHistoryRequest{Symbol: "BNBUSDT", Limit: 10})
	if err != nil || len(history) != 1 || history[0].AlgoStatus != AlgoStatusFinished {
		t.Fatalf("AlgoOrderHistory() = %+v, error = %v", history, err)
	}
	if routes := sender.snapshot(); len(routes) != 5 || routes[0] != "route-b" || routes[1] != "route-a" {
		t.Fatalf("routes = %v", routes)
	}
	snapshot, err := limiter.Snapshot("binance-usdm:account:futures-main:orders:10seconds")
	if err != nil || snapshot.Used != 1 {
		t.Fatalf("order limiter = %+v, error = %v", snapshot, err)
	}
}

func TestPlaceAlgoOrderValidation(t *testing.T) {
	t.Parallel()

	future := time.Now().Add(time.Hour)
	for name, request := range map[string]PlaceAlgoOrderRequest{
		"missing symbol":           {Side: SideBuy, Type: OrderTypeStopMarket, TriggerPrice: "1", Quantity: "1"},
		"unsupported algo type":    {AlgoType: AlgoType("TWAP"), Symbol: "BTCUSDT", Side: SideBuy, Type: OrderTypeStopMarket, TriggerPrice: "1", Quantity: "1"},
		"plain limit":              {Symbol: "BTCUSDT", Side: SideBuy, Type: OrderTypeLimit, Price: "1", Quantity: "1"},
		"stop without trigger":     {Symbol: "BTCUSDT", Side: SideBuy, Type: OrderTypeStop, Price: "1", Quantity: "1"},
		"stop market no size":      {Symbol: "BTCUSDT", Side: SideBuy, Type: OrderTypeStopMarket, TriggerPrice: "1"},
		"trailing no callback":     {Symbol: "BTCUSDT", Side: SideBuy, Type: OrderTypeTrailingStopMarket, Quantity: "1"},
		"closePosition quantity":   {Symbol: "BTCUSDT", Side: SideSell, Type: OrderTypeStopMarket, TriggerPrice: "1", Quantity: "1", ClosePosition: true},
		"closePosition on stop":    {Symbol: "BTCUSDT", Side: SideSell, Type: OrderTypeStop, TriggerPrice: "1", Price: "1", ClosePosition: true},
		"reduceOnly hedge":         {Symbol: "BTCUSDT", Side: SideSell, PositionSide: PositionSideLong, Type: OrderTypeStopMarket, TriggerPrice: "1", Quantity: "1", ReduceOnly: true},
		"zero trigger":             {Symbol: "BTCUSDT", Side: SideBuy, Type: OrderTypeStopMarket, TriggerPrice: "0", Quantity: "1"},
		"priceMatch with price":    {Symbol: "BTCUSDT", Side: SideBuy, Type: OrderTypeStop, TriggerPrice: "1", Price: "1", PriceMatch: "QUEUE", Quantity: "1"},
		"goodTillDate without GTD": {Symbol: "BTCUSDT", Side: SideBuy, Type: OrderTypeStop, TriggerPrice: "1", Price: "1", Quantity: "1", GoodTillDate: &future},
	} {
		if err := request.validate(); !errors.Is(err, trade.ErrValidation) {
			t.Fatalf("%s: validate() error = %v, want ErrValidation", name, err)
		}
	}
	for name, request := range map[string]PlaceAlgoOrderRequest{
		"stop market close": {Symbol: "BTCUSDT", Side: SideSell, Type: OrderTypeStopMarket, TriggerPrice: "1", ClosePosition: true, WorkingType: WorkingTypeMarkPrice},
		"stop priceMatch":   {Symbol: "BTCUSDT", Side: SideBuy, Type: OrderTypeStop, TriggerPrice: "1", PriceMatch: "QUEUE", Quantity: "1"},
		"trailing":          {Symbol: "BTCUSDT", Side: SideSell, Type: OrderTypeTrailingStopMarket, Quantity: "1", CallbackRate: "1", ActivationPrice: "100"},
		"take profit GTD":   {Symbol: "BTCUSDT", Side: SideSell, Type: OrderTypeTakeProfit, TriggerPrice: "1", Price: "1", Quantity: "1", TimeInForce: TimeInForceGTD, GoodTillDate: &future},
	} {
		if err := request.validate(); err != nil {
			t.Fatalf("%s: validate() error = %v", name, err)
		}
	}
	values := (PlaceAlgoOrderRequest{Symbol: "BTCUSDT", Side: SideSell, Type: OrderTypeStopMarket, TriggerPrice: "1", ClosePosition: true, PriceProtect: true}).values()
	if values.Get("algoType") != "CONDITIONAL" || values.Get("closePosition") != "true" || values.Get("priceProtect") != "true" || values.Has("quantity") || values.Has("stopPrice") {
		t.Fatalf("values() = %v", values)
	}
}

func TestAlgoOrderQueryValidation(t *testing.T) {
	t.Parallel()

	id, zero := int64(1), int64(0)
	start, end := time.UnixMilli(2_000), time.UnixMilli(1_000)
	for name, err := range map[string]error{
		"no identity":       AlgoOrderInfoRequest{}.validate(),
		"both identities":   AlgoOrderInfoRequest{AlgoID: &id, ClientAlgoID: "x"}.validate(),
		"zero algoId":       AlgoOrderInfoRequest{AlgoID: &zero}.validate(),
		"open no scope":     OpenAlgoOrdersRequest{}.validate(),
		"open both scopes":  OpenAlgoOrdersRequest{Symbol: "BTCUSDT", AllSymbols: true}.validate(),
		"history no symbol": AlgoOrderHistoryRequest{}.validate(),
		"history limit":     AlgoOrderHistoryRequest{Symbol: "BTCUSDT", Limit: 1001}.validate(),
		"history range":     AlgoOrderHistoryRequest{Symbol: "BTCUSDT", StartTime: &start, EndTime: &end}.validate(),
	} {
		if !errors.Is(err, trade.ErrValidation) {
			t.Fatalf("%s: error = %v, want ErrValidation", name, err)
		}
	}
}
