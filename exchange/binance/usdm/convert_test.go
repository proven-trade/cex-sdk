package usdm

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	"github.com/proven-trade/cex-sdk/transport"
)

func TestConvertPairs(t *testing.T) {
	t.Parallel()
	const body = `[{"fromAsset":"BTC","toAsset":"USDT","fromAssetMinAmount":"0.0004","fromAssetMaxAmount":"50","toAssetMinAmount":"20","toAssetMaxAmount":"2500000"},{"fromAsset":"BTC","toAsset":"ETH","fromAssetMinAmount":"0.001","fromAssetMaxAmount":"10","toAssetMinAmount":"0.02","toAssetMaxAmount":"300"}]`

	runMarketCase(t, marketCase{name: "convert pairs by both assets", path: "/fapi/v1/convert/exchangeInfo", query: "fromAsset=BTC&toAsset=USDT", weight: 20, body: body}, func(t *testing.T, client *Client) {
		items, err := client.ConvertPairs(context.Background(), ConvertPairsRequest{FromAsset: "BTC", ToAsset: "USDT"})
		if err != nil || len(items) != 2 {
			t.Fatalf("ConvertPairs() = %+v, error = %v", items, err)
		}
		pair := items[0]
		if pair.FromAsset != "BTC" || pair.ToAsset != "USDT" || pair.FromAssetMinAmount != "0.0004" || pair.FromAssetMaxAmount != "50" || pair.ToAssetMinAmount != "20" || pair.ToAssetMaxAmount != "2500000" || len(pair.Raw) == 0 {
			t.Fatalf("ConvertPairs()[0] = %+v", pair)
		}
		if items[1].ToAsset != "ETH" || items[1].ToAssetMaxAmount != "300" {
			t.Fatalf("ConvertPairs()[1] = %+v", items[1])
		}
	})
	runMarketCase(t, marketCase{name: "convert pairs from asset only", path: "/fapi/v1/convert/exchangeInfo", query: "fromAsset=BTC", weight: 20, body: `[]`}, func(t *testing.T, client *Client) {
		items, err := client.ConvertPairs(context.Background(), ConvertPairsRequest{FromAsset: "BTC"})
		if err != nil || items == nil || len(items) != 0 {
			t.Fatalf("ConvertPairs() = %+v, error = %v, want an empty list", items, err)
		}
	})
	runMarketCase(t, marketCase{name: "convert pairs all", path: "/fapi/v1/convert/exchangeInfo", query: "", weight: 20, body: body}, func(t *testing.T, client *Client) {
		items, err := client.ConvertPairs(context.Background(), ConvertPairsRequest{})
		if err != nil || len(items) != 2 {
			t.Fatalf("ConvertPairs() = %+v, error = %v", items, err)
		}
	})
}

func TestConvertSignedEndpoints(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	transfer := []credential.Permission{credential.PermissionRead, credential.PermissionTransfer}
	read := []credential.Permission{credential.PermissionRead}
	const orderStatusBody = `{"orderId":933256278426274426,"orderStatus":"SUCCESS","fromAsset":"BTC","fromAmount":"0.00054414","toAsset":"USDT","toAmount":"20","ratio":"36755","inverseRatio":"0.00002721","createTime":1623381330472}`

	runSignedCases(t, []signedCase{
		{name: "request quote from amount", method: http.MethodPost, path: "/fapi/v1/convert/getQuote", params: url.Values{"fromAsset": {"USDT"}, "toAsset": {"BTC"}, "fromAmount": {"20"}, "validTime": {"30s"}}, body: `{"quoteId":"12415572564","ratio":"38163.7","inverseRatio":"0.0000262","validTimestamp":1623319461670,"toAmount":"0.00052405","fromAmount":"20"}`, weight: 50, permissions: transfer, run: func(t *testing.T, client *Client) {
			quote, err := client.RequestConvertQuote(ctx, ConvertQuoteRequest{FromAsset: "USDT", ToAsset: "BTC", FromAmount: "20", ValidTime: "30s"})
			if err != nil || quote.QuoteID != "12415572564" || quote.Ratio != "38163.7" || quote.InverseRatio != "0.0000262" || quote.ValidTimestamp != 1623319461670 || quote.FromAmount != "20" || quote.ToAmount != "0.00052405" || len(quote.Raw) == 0 {
				t.Fatalf("RequestConvertQuote() = %+v, error = %v", quote, err)
			}
		}},
		{name: "request quote to amount without valid time", method: http.MethodPost, path: "/fapi/v1/convert/getQuote", params: url.Values{"fromAsset": {"USDT"}, "toAsset": {"BTC"}, "toAmount": {"0.001"}}, body: `{"ratio":"38163.7","inverseRatio":"0.0000262","validTimestamp":1623319461670,"toAmount":"0.001","fromAmount":"38.1637"}`, weight: 50, permissions: transfer, run: func(t *testing.T, client *Client) {
			quote, err := client.RequestConvertQuote(ctx, ConvertQuoteRequest{FromAsset: "USDT", ToAsset: "BTC", ToAmount: "0.001"})
			if err != nil || quote.QuoteID != "" || quote.ToAmount != "0.001" || quote.FromAmount != "38.1637" {
				t.Fatalf("RequestConvertQuote() = %+v, error = %v, want an empty quote id when the balance is insufficient", quote, err)
			}
		}},
		{name: "accept quote numeric order id", method: http.MethodPost, path: "/fapi/v1/convert/acceptQuote", params: url.Values{"quoteId": {"12415572564"}}, body: `{"orderId":933256278426274426,"createTime":1623381330472,"orderStatus":"PROCESS"}`, weight: 200, permissions: transfer, run: func(t *testing.T, client *Client) {
			acceptance, err := client.AcceptConvertQuote(ctx, AcceptConvertQuoteRequest{QuoteID: "12415572564"})
			if err != nil || acceptance.OrderID.String() != "933256278426274426" || acceptance.CreateTime != 1623381330472 || acceptance.OrderStatus != "PROCESS" || len(acceptance.Raw) == 0 {
				t.Fatalf("AcceptConvertQuote() = %+v, error = %v", acceptance, err)
			}
		}},
		{name: "accept quote string order id", method: http.MethodPost, path: "/fapi/v1/convert/acceptQuote", params: url.Values{"quoteId": {"12415572564"}}, body: `{"orderId":"933256278426274426","createTime":1623381330472,"orderStatus":"SUCCESS"}`, weight: 200, permissions: transfer, run: func(t *testing.T, client *Client) {
			acceptance, err := client.AcceptConvertQuote(ctx, AcceptConvertQuoteRequest{QuoteID: "12415572564"})
			if err != nil || acceptance.OrderID.String() != "933256278426274426" || acceptance.OrderStatus != "SUCCESS" {
				t.Fatalf("AcceptConvertQuote() = %+v, error = %v", acceptance, err)
			}
		}},
		{name: "order status by order id", method: http.MethodGet, path: "/fapi/v1/convert/orderStatus", params: url.Values{"orderId": {"933256278426274426"}}, body: orderStatusBody, weight: 50, permissions: read, run: func(t *testing.T, client *Client) {
			order, err := client.ConvertOrderStatus(ctx, ConvertOrderRequest{OrderID: "933256278426274426"})
			if err != nil || order.OrderID.String() != "933256278426274426" || order.OrderStatus != "SUCCESS" || order.FromAsset != "BTC" || order.FromAmount != "0.00054414" || order.ToAsset != "USDT" || order.ToAmount != "20" || order.Ratio != "36755" || order.InverseRatio != "0.00002721" || order.CreateTime != 1623381330472 || len(order.Raw) == 0 {
				t.Fatalf("ConvertOrderStatus() = %+v, error = %v", order, err)
			}
		}},
		{name: "order status by quote id with string order id", method: http.MethodGet, path: "/fapi/v1/convert/orderStatus", params: url.Values{"quoteId": {"12415572564"}}, body: `{"orderId":"933256278426274426","orderStatus":"PROCESS","fromAsset":"BTC","fromAmount":"0.00054414","toAsset":"USDT","toAmount":"20","ratio":"36755","inverseRatio":"0.00002721","createTime":1623381330472}`, weight: 50, permissions: read, run: func(t *testing.T, client *Client) {
			order, err := client.ConvertOrderStatus(ctx, ConvertOrderRequest{QuoteID: "12415572564"})
			if err != nil || order.OrderID.String() != "933256278426274426" || order.OrderStatus != "PROCESS" {
				t.Fatalf("ConvertOrderStatus() = %+v, error = %v", order, err)
			}
		}},
	})
}

func TestConvertPermissionBoundary(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	server, requests := newMarketServer(t, `{}`, nil)
	provider := &recordingProvider{}
	// trade 권한은 Convert 변경 요청을 허용하지 않고, transfer 권한은 조회를 허용하지 않는다.
	tradeClient, _ := newPermissionTestClient(t, server.URL, provider, time.UnixMilli(marketStartMS), credential.PermissionRead, credential.PermissionTrade)
	transferOnly, _ := newPermissionTestClient(t, server.URL, provider, time.UnixMilli(marketStartMS), credential.PermissionTransfer)
	for _, tc := range []struct {
		name string
		call func() error
	}{
		{"request quote with trade permission", func() error {
			_, err := tradeClient.RequestConvertQuote(ctx, ConvertQuoteRequest{FromAsset: "USDT", ToAsset: "BTC", FromAmount: "20"})
			return err
		}},
		{"accept quote with trade permission", func() error {
			_, err := tradeClient.AcceptConvertQuote(ctx, AcceptConvertQuoteRequest{QuoteID: "12415572564"})
			return err
		}},
		{"order status without read permission", func() error {
			_, err := transferOnly.ConvertOrderStatus(ctx, ConvertOrderRequest{OrderID: "1"})
			return err
		}},
	} {
		err := tc.call()
		if !errors.Is(err, trade.ErrAuthorization) || !errors.Is(err, credential.ErrPermissionNotAllowed) {
			t.Errorf("%s: error = %v, want ErrAuthorization from the missing permission", tc.name, err)
		}
	}
	if calls, _, _ := provider.snapshot(); calls != 0 || len(requests()) != 0 {
		t.Fatalf("secret resolutions = %d, requests = %+v, want none", calls, requests())
	}
}

func TestConvertMutationsAreNotRetried(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		call func(client *Client) error
	}{
		{"request quote", func(client *Client) error {
			_, err := client.RequestConvertQuote(context.Background(), ConvertQuoteRequest{FromAsset: "USDT", ToAsset: "BTC", FromAmount: "20"})
			return err
		}},
		{"accept quote", func(client *Client) error {
			_, err := client.AcceptConvertQuote(context.Background(), AcceptConvertQuoteRequest{QuoteID: "12415572564"})
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var attempts atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				attempts.Add(1)
				writer.Header().Set("Content-Type", "application/json")
				writer.WriteHeader(http.StatusServiceUnavailable)
				_, _ = io.WriteString(writer, `{"code":-1007,"msg":"Timeout waiting for response from backend server. Send status unknown; execution status unknown."}`)
			}))
			t.Cleanup(server.Close)
			client, _ := newPermissionTestClient(t, server.URL, &recordingProvider{}, time.UnixMilli(marketStartMS), credential.PermissionRead, credential.PermissionTransfer)
			err := tc.call(client)
			var apiError *trade.APIError
			if !errors.As(err, &apiError) || apiError.Category != trade.ErrorUnknownExecutionState || apiError.Retryable || apiError.ExchangeCode != "-1007" {
				t.Fatalf("error = %v, want unknown execution state", err)
			}
			if attempts.Load() != 1 {
				t.Fatalf("attempts = %d, want exactly one", attempts.Load())
			}
		})
	}
}

func TestConvertMalformedResponses(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, tc := range []malformedCase{
		{"not json", `not-json`},
		{"array instead of object", `[]`},
		{"non numeric order id", `{"orderId":"abc","orderStatus":"SUCCESS"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server, _ := newMarketServer(t, tc.body, nil)
			client, _ := newPermissionTestClient(t, server.URL, &recordingProvider{}, time.UnixMilli(marketStartMS), credential.PermissionRead, credential.PermissionTransfer)
			if order, err := client.ConvertOrderStatus(ctx, ConvertOrderRequest{OrderID: "1"}); err == nil {
				t.Errorf("ConvertOrderStatus() = %+v, error = nil", order)
			}
		})
	}

	server, _ := newMarketServer(t, `{"unexpected":`, nil)
	client, _ := newTestClient(t, server.URL, &directSender{}, &recordingProvider{}, time.UnixMilli(marketStartMS), []transport.EgressRouteID{"route-a"})
	if items, err := client.ConvertPairs(ctx, ConvertPairsRequest{}); err == nil {
		t.Errorf("ConvertPairs() = %+v, error = nil for a truncated body", items)
	}
}

func TestConvertValidation(t *testing.T) {
	t.Parallel()
	for _, tc := range []validationCase{
		{"pairs padded from asset", ConvertPairsRequest{FromAsset: "BTC "}.validate()},
		{"pairs padded to asset", ConvertPairsRequest{ToAsset: " USDT"}.validate()},
		{"quote missing from asset", ConvertQuoteRequest{ToAsset: "BTC", FromAmount: "1"}.validate()},
		{"quote missing to asset", ConvertQuoteRequest{FromAsset: "USDT", FromAmount: "1"}.validate()},
		{"quote padded asset", ConvertQuoteRequest{FromAsset: "USDT ", ToAsset: "BTC", FromAmount: "1"}.validate()},
		{"quote no amount", ConvertQuoteRequest{FromAsset: "USDT", ToAsset: "BTC"}.validate()},
		{"quote both amounts", ConvertQuoteRequest{FromAsset: "USDT", ToAsset: "BTC", FromAmount: "1", ToAmount: "1"}.validate()},
		{"quote zero amount", ConvertQuoteRequest{FromAsset: "USDT", ToAsset: "BTC", FromAmount: "0"}.validate()},
		{"quote non decimal amount", ConvertQuoteRequest{FromAsset: "USDT", ToAsset: "BTC", ToAmount: "abc"}.validate()},
		{"quote padded valid time", ConvertQuoteRequest{FromAsset: "USDT", ToAsset: "BTC", FromAmount: "1", ValidTime: "10s "}.validate()},
		{"accept empty quote id", AcceptConvertQuoteRequest{}.validate()},
		{"accept padded quote id", AcceptConvertQuoteRequest{QuoteID: " 1"}.validate()},
		{"status neither id", ConvertOrderRequest{}.validate()},
		{"status both ids", ConvertOrderRequest{OrderID: "1", QuoteID: "2"}.validate()},
		{"status padded order id", ConvertOrderRequest{OrderID: "1 "}.validate()},
		{"status padded quote id", ConvertOrderRequest{QuoteID: " 2"}.validate()},
	} {
		if !errors.Is(tc.err, trade.ErrValidation) {
			t.Errorf("%s: error = %v, want ErrValidation", tc.name, tc.err)
		}
	}
	for _, tc := range []validationCase{
		{"pairs empty", ConvertPairsRequest{}.validate()},
		{"pairs both assets", ConvertPairsRequest{FromAsset: "BTC", ToAsset: "USDT"}.validate()},
		{"quote from amount", ConvertQuoteRequest{FromAsset: "USDT", ToAsset: "BTC", FromAmount: "20", ValidTime: "30s"}.validate()},
		{"quote to amount", ConvertQuoteRequest{FromAsset: "USDT", ToAsset: "BTC", ToAmount: "0.001"}.validate()},
		{"accept", AcceptConvertQuoteRequest{QuoteID: "12415572564"}.validate()},
		{"status by order id", ConvertOrderRequest{OrderID: "933256278426274426"}.validate()},
		{"status by quote id", ConvertOrderRequest{QuoteID: "12415572564"}.validate()},
	} {
		if tc.err != nil {
			t.Errorf("%s: error = %v, want nil", tc.name, tc.err)
		}
	}

	ctx := context.Background()
	sender := &directSender{}
	provider := &recordingProvider{}
	client, _ := newTestClient(t, "http://127.0.0.1", sender, provider, time.UnixMilli(marketStartMS), []transport.EgressRouteID{"route-a"})
	for _, tc := range []struct {
		name string
		call func() error
	}{
		{"convert pairs padded", func() error {
			_, err := client.ConvertPairs(ctx, ConvertPairsRequest{FromAsset: " BTC"})
			return err
		}},
		{"request quote both amounts", func() error {
			_, err := client.RequestConvertQuote(ctx, ConvertQuoteRequest{FromAsset: "USDT", ToAsset: "BTC", FromAmount: "1", ToAmount: "1"})
			return err
		}},
		{"accept quote empty", func() error {
			_, err := client.AcceptConvertQuote(ctx, AcceptConvertQuoteRequest{})
			return err
		}},
		{"order status empty", func() error {
			_, err := client.ConvertOrderStatus(ctx, ConvertOrderRequest{})
			return err
		}},
	} {
		if err := tc.call(); !errors.Is(err, trade.ErrValidation) {
			t.Errorf("%s: error = %v, want ErrValidation", tc.name, err)
		}
	}
	if calls, _, _ := provider.snapshot(); calls != 0 || len(sender.snapshot()) != 0 {
		t.Fatalf("secret resolutions = %d, routes = %v, want none", calls, sender.snapshot())
	}
}
