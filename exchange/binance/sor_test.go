package binance

import (
	"context"
	"net/http"
	"testing"
)

func sampleSORRequest() SOROrderRequest {
	return SOROrderRequest{Symbol: "BTCUSDT", Side: SideBuy, Type: OrderTypeLimit, TimeInForce: TimeInForceGTC, Quantity: "0.5", Price: "31000.00", ClientOrderID: "sor-1", ResponseType: NewOrderResponseFull}
}

var sampleSORParams = []string{
	"symbol=BTCUSDT",
	"side=BUY",
	"type=LIMIT",
	"timeInForce=GTC",
	"quantity=0.5",
	"price=31000.00",
	"newClientOrderId=sor-1",
	"newOrderRespType=FULL",
}

func TestSOROrder(t *testing.T) {
	t.Parallel()

	harness := newExtensionHarness(t, http.StatusOK, `{"symbol":"BTCUSDT","orderId":2,"orderListId":-1,"clientOrderId":"sBI1KM6nNtOfj5tccZSKly","transactTime":1689149087774,"price":"31000.00000000","origQty":"0.50000000","executedQty":"0.50000000","origQuoteOrderQty":"0.000000","cummulativeQuoteQty":"14000.00000000","status":"FILLED","timeInForce":"GTC","type":"LIMIT","side":"BUY","workingTime":1689149087774,"fills":[{"matchType":"ONE_PARTY_TRADE_REPORT","price":"28000.00000000","qty":"0.50000000","commission":"0.00000000","commissionAsset":"BTC","tradeId":-1,"allocId":0}],"workingFloor":"SOR","selfTradePreventionMode":"NONE","usedSor":true}`)
	result, err := harness.client.SOROrder(context.Background(), sampleSORRequest())
	if err != nil {
		t.Fatalf("SOROrder() error = %v", err)
	}
	if result.OrderID != 2 || result.Status != OrderStatusFilled || result.CumulativeQuoteQuantity != "14000.00000000" || len(result.Raw) == 0 {
		t.Fatalf("SOROrder() = %+v", result)
	}
	if len(result.Fills) != 1 || result.Fills[0].MatchType != "ONE_PARTY_TRADE_REPORT" || result.Fills[0].TradeID != -1 || result.Fills[0].AllocationID != 0 || result.Fills[0].CommissionAsset != "BTC" {
		t.Fatalf("SOROrder() fills = %+v", result.Fills)
	}
	if result.WorkingFloor != "SOR" || !result.UsedSOR {
		t.Fatalf("SOROrder() = %+v, want a SOR working floor", result)
	}
	harness.expect(t, http.MethodPost, "/api/v3/sor/order", true, 1, 1, sampleSORParams...)
}

func TestSOROrderMarket(t *testing.T) {
	t.Parallel()

	harness := newExtensionHarness(t, http.StatusOK, `{"symbol":"BTCUSDT","orderId":3,"orderListId":-1,"clientOrderId":"x","status":"NEW","type":"MARKET","side":"SELL"}`)
	result, err := harness.client.SOROrder(context.Background(), SOROrderRequest{Symbol: "BTCUSDT", Side: SideSell, Type: OrderTypeMarket, Quantity: "0.25"})
	if err != nil {
		t.Fatalf("SOROrder() error = %v", err)
	}
	if result.OrderID != 3 || result.Type != OrderTypeMarket || result.UsedSOR || len(result.Fills) != 0 {
		t.Fatalf("SOROrder() = %+v", result)
	}
	harness.expect(t, http.MethodPost, "/api/v3/sor/order", true, 1, 1, "symbol=BTCUSDT", "side=SELL", "type=MARKET", "quantity=0.25")
}

func TestTestSOROrder(t *testing.T) {
	t.Parallel()

	t.Run("without commission rates", func(t *testing.T) {
		t.Parallel()
		harness := newExtensionHarness(t, http.StatusOK, `{}`)
		result, err := harness.client.TestSOROrder(context.Background(), TestSOROrderRequest{Order: sampleSORRequest()})
		if err != nil {
			t.Fatalf("TestSOROrder() error = %v", err)
		}
		if result.StandardCommissionForOrder != nil || result.Discount != nil {
			t.Fatalf("TestSOROrder() = %+v, want an empty result", result)
		}
		harness.expect(t, http.MethodPost, "/api/v3/sor/order/test", true, 1, 0, sampleSORParams...)
	})
	t.Run("with commission rates", func(t *testing.T) {
		t.Parallel()
		harness := newExtensionHarness(t, http.StatusOK, `{"standardCommissionForOrder":{"maker":"0.00000112","taker":"0.00000114"},"taxCommissionForOrder":{"maker":"0.00000112","taker":"0.00000114"},"discount":{"enabledForAccount":true,"enabledForSymbol":true,"discountAsset":"BNB","discount":"0.25000000"}}`)
		result, err := harness.client.TestSOROrder(context.Background(), TestSOROrderRequest{Order: sampleSORRequest(), ComputeCommissionRates: true})
		if err != nil {
			t.Fatalf("TestSOROrder() error = %v", err)
		}
		if result.StandardCommissionForOrder == nil || result.StandardCommissionForOrder.Taker != "0.00000114" || result.SpecialCommissionForOrder != nil ||
			result.TaxCommissionForOrder == nil || result.Discount == nil || result.Discount.Discount != "0.25000000" {
			t.Fatalf("TestSOROrder() = %+v", result)
		}
		params := append([]string{"computeCommissionRates=true"}, sampleSORParams...)
		harness.expect(t, http.MethodPost, "/api/v3/sor/order/test", true, 20, 0, params...)
	})
}

func TestSORValidation(t *testing.T) {
	t.Parallel()

	harness := newExtensionHarness(t, http.StatusOK, `{}`)
	ctx := context.Background()

	request := sampleSORRequest()
	request.Type = OrderTypeStopLoss
	_, err := harness.client.SOROrder(ctx, request)
	harness.requireValidation(t, "unsupported type", err)

	request = sampleSORRequest()
	request.Quantity = ""
	_, err = harness.client.SOROrder(ctx, request)
	harness.requireValidation(t, "missing quantity", err)

	request = sampleSORRequest()
	request.Price = ""
	_, err = harness.client.SOROrder(ctx, request)
	harness.requireValidation(t, "LIMIT without price", err)

	request = sampleSORRequest()
	request.TimeInForce = ""
	_, err = harness.client.SOROrder(ctx, request)
	harness.requireValidation(t, "LIMIT without timeInForce", err)

	request = SOROrderRequest{Symbol: "BTCUSDT", Side: SideBuy, Type: OrderTypeMarket, Quantity: "1", Price: "1"}
	_, err = harness.client.SOROrder(ctx, request)
	harness.requireValidation(t, "MARKET with price", err)

	request = SOROrderRequest{Symbol: "BTCUSDT", Side: SideBuy, Type: OrderTypeMarket, Quantity: "1", TimeInForce: TimeInForceGTC}
	_, err = harness.client.SOROrder(ctx, request)
	harness.requireValidation(t, "MARKET with timeInForce", err)

	request = sampleSORRequest()
	request.Side = ""
	_, err = harness.client.SOROrder(ctx, request)
	harness.requireValidation(t, "missing side", err)

	request = sampleSORRequest()
	request.ResponseType = "VERBOSE"
	_, err = harness.client.SOROrder(ctx, request)
	harness.requireValidation(t, "unsupported response type", err)

	request = sampleSORRequest()
	request.ClientOrderID = "has a space"
	_, err = harness.client.TestSOROrder(ctx, TestSOROrderRequest{Order: request})
	harness.requireValidation(t, "TestSOROrder invalid client ID", err)
}
