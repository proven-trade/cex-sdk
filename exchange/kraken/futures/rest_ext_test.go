package futures

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	"github.com/proven-trade/cex-sdk/transport"
)

type recordedRequest struct {
	Method string
	Path   string
	Query  url.Values
	Body   string
	Signed bool
}

// newRESTServer는 요청을 기록하고 path별 고정 응답을 돌려준다. 서명이 있는 요청은 Authent를 검증한다.
func newRESTServer(
	t *testing.T,
	secret []byte,
	responses map[string]string,
	headers map[string]string,
) (*httptest.Server, func() []recordedRequest) {
	t.Helper()
	var mu sync.Mutex
	var recorded []recordedRequest
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		body, _ := io.ReadAll(request.Body)
		signed := request.Header.Get("APIKey") != ""
		if signed {
			signingPath := strings.TrimPrefix(request.URL.Path, "/derivatives")
			expected, err := SignAuthent(request.URL.RawQuery, request.Header.Get("Nonce"), signingPath, secret)
			if err != nil || request.Header.Get("APIKey") != "test-api-key" ||
				request.Header.Get("Authent") != expected || len(body) != 0 {
				http.Error(writer, `{"result":"error","error":"authenticationError"}`, http.StatusUnauthorized)
				return
			}
		}
		mu.Lock()
		recorded = append(recorded, recordedRequest{
			Method: request.Method, Path: request.URL.Path, Query: request.URL.Query(),
			Body: string(body), Signed: signed,
		})
		mu.Unlock()
		for key, value := range headers {
			writer.Header().Set(key, value)
		}
		response, ok := responses[request.Method+" "+request.URL.Path]
		if !ok {
			http.Error(writer, `{"result":"error","error":"notFound"}`, http.StatusNotFound)
			return
		}
		_, _ = io.WriteString(writer, response)
	}))
	t.Cleanup(server.Close)
	return server, func() []recordedRequest {
		mu.Lock()
		defer mu.Unlock()
		return append([]recordedRequest(nil), recorded...)
	}
}

func newRESTClient(
	t *testing.T,
	server *httptest.Server,
	secret []byte,
	permissions ...credential.Permission,
) *Client {
	t.Helper()
	provider := &recordingProvider{secret: secret}
	client, _ := newTestClient(
		t, server.URL, &directSender{}, provider, []transport.EgressRouteID{"route-a"}, time.UnixMilli(1_700_000_000_000),
	)
	if len(permissions) > 0 {
		client.credentials.Permissions = permissions
	}
	return client
}

func TestTradingRESTMutations(t *testing.T) {
	t.Parallel()

	secret := []byte(base64.StdEncoding.EncodeToString([]byte("test-secret")))
	server, requests := newRESTServer(t, secret, map[string]string{
		"POST /derivatives/api/v3/editorder": `{"result":"success","editStatus":{"status":"edited","orderId":"ORDER-1","receivedTime":"2026-08-25T00:00:00Z","orderEvents":[{"type":"EDIT"}]}}`,
		"POST /derivatives/api/v3/batchorder": `{"result":"success","batchStatus":[` +
			`{"status":"placed","order_tag":"1","order_id":"ORDER-2","orderEvents":[]},` +
			`{"status":"edited","order_id":"ORDER-1","orderEvents":[]},` +
			`{"status":"cancelled","order_id":"ORDER-3","orderEvents":[]},` +
			`{"status":"insufficientAvailableFunds","order_tag":"4","orderEvents":[]}]}`,
		"POST /derivatives/api/v3/cancelallorders":      `{"result":"success","cancelStatus":{"status":"cancelled","cancelOnly":"PI_XBTUSD","receivedTime":"2026-08-25T00:00:00Z","cancelledOrders":[{"order_id":"ORDER-1"},{"order_id":"ORDER-2"}],"orderEvents":[]}}`,
		"POST /derivatives/api/v3/cancelallordersafter": `{"result":"success","status":{"currentTime":"2026-08-25T00:00:00Z","triggerTime":"2026-08-25T00:01:00Z"}}`,
		"GET /derivatives/api/v3/self-trade-strategy":   `{"result":"success","strategy":"REJECT_TAKER"}`,
		"PUT /derivatives/api/v3/self-trade-strategy":   `{"result":"success","strategy":"CANCEL_MAKER_SELF"}`,
		"GET /derivatives/api/v3/leveragepreferences":   `{"result":"success","leveragePreferences":[{"symbol":"PF_XBTUSD","maxLeverage":10,"marginMode":"isolated"}]}`,
		"PUT /derivatives/api/v3/leveragepreferences":   `{"result":"success","serverTime":"2026-08-25T00:00:00Z"}`,
		"GET /derivatives/api/v3/pnlpreferences":        `{"result":"success","accountWidePnlCurrency":"USD","preferences":[{"symbol":"PF_XBTUSD","pnlCurrency":"BTC"}]}`,
		"PUT /derivatives/api/v3/pnlpreferences":        `{"result":"success","serverTime":"2026-08-25T00:00:00Z"}`,
		"DELETE /derivatives/api/v3/pnlpreferences":     `{"result":"success","serverTime":"2026-08-25T00:00:00Z"}`,
	}, nil)
	client := newRESTClient(t, server, secret)
	ctx := context.Background()

	edit, err := client.EditOrder(ctx, EditOrderRequest{
		OrderID: "ORDER-1", Size: "2", LimitPrice: "64000.5", QuantityMode: QuantityModeAbsolute,
	})
	if err != nil || edit.Status != "edited" || edit.OrderID != "ORDER-1" || len(edit.Raw) == 0 {
		t.Fatalf("EditOrder() = %+v, error = %v", edit, err)
	}
	batch, err := client.BatchOrder(ctx, BatchOrderRequest{Instructions: []BatchInstruction{
		{Kind: BatchInstructionSend, OrderTag: "1", OrderType: OrderTypeLimit, Symbol: "PI_XBTUSD",
			Side: SideBuy, Size: "1", LimitPrice: "64000", ClientOrderID: "client-1", ReduceOnly: true},
		{Kind: BatchInstructionEdit, OrderID: "ORDER-1", Size: "3", QuantityMode: QuantityModeRelative},
		{Kind: BatchInstructionCancel, OrderID: "ORDER-3"},
		{Kind: BatchInstructionCancel, ClientOrderID: "client-9"},
	}})
	if err != nil || len(batch) != 4 || batch[0].OrderTag != "1" || batch[3].Status != "insufficientAvailableFunds" {
		t.Fatalf("BatchOrder() = %+v, error = %v", batch, err)
	}
	cancelAll, err := client.CancelAllOrders(ctx, CancelAllOrdersRequest{Symbol: "PI_XBTUSD"})
	if err != nil || cancelAll.Status != "cancelled" || len(cancelAll.CancelledOrders) != 2 ||
		cancelAll.CancelledOrders[1].OrderID != "ORDER-2" {
		t.Fatalf("CancelAllOrders() = %+v, error = %v", cancelAll, err)
	}
	deadMan, err := client.CancelAllOrdersAfter(ctx, CancelAllOrdersAfterRequest{TimeoutSeconds: 60})
	if err != nil || deadMan.TriggerTime != "2026-08-25T00:01:00Z" {
		t.Fatalf("CancelAllOrdersAfter() = %+v, error = %v", deadMan, err)
	}
	strategy, err := client.SelfTradeStrategy(ctx)
	if err != nil || strategy != SelfTradeRejectTaker {
		t.Fatalf("SelfTradeStrategy() = %q, error = %v", strategy, err)
	}
	strategy, err = client.SetSelfTradeStrategy(ctx, SelfTradeCancelMakerSelf)
	if err != nil || strategy != SelfTradeCancelMakerSelf {
		t.Fatalf("SetSelfTradeStrategy() = %q, error = %v", strategy, err)
	}
	leverage, err := client.LeveragePreferences(ctx)
	if err != nil || len(leverage) != 1 || leverage[0].MaxLeverage != "10" || leverage[0].MarginMode != LeverageMarginIsolated {
		t.Fatalf("LeveragePreferences() = %+v, error = %v", leverage, err)
	}
	if _, err := client.SetLeveragePreference(ctx, SetLeveragePreferenceRequest{
		Symbol: "PF_XBTUSD", MaxLeverage: "5", MarginMode: LeverageMarginCross,
	}); err != nil {
		t.Fatalf("SetLeveragePreference() error = %v", err)
	}
	pnl, err := client.PnlPreferences(ctx)
	if err != nil || pnl.AccountWidePnlCurrency != "USD" || len(pnl.Preferences) != 1 || pnl.Preferences[0].PnlCurrency != "BTC" {
		t.Fatalf("PnlPreferences() = %+v, error = %v", pnl, err)
	}
	if _, err := client.SetPnlPreference(ctx, SetPnlPreferenceRequest{Symbol: "PF_XBTUSD", PnlCurrency: "BTC"}); err != nil {
		t.Fatalf("SetPnlPreference() error = %v", err)
	}
	if _, err := client.DeletePnlPreference(ctx, DeletePnlPreferenceRequest{}); err != nil {
		t.Fatalf("DeletePnlPreference() error = %v", err)
	}

	byKey := make(map[string]recordedRequest)
	for _, request := range requests() {
		if !request.Signed {
			t.Fatalf("request %s %s was not signed", request.Method, request.Path)
		}
		byKey[request.Method+" "+request.Path] = request
	}
	edited := byKey["POST /derivatives/api/v3/editorder"].Query
	if edited.Get("orderId") != "ORDER-1" || edited.Get("size") != "2" || edited.Get("limitPrice") != "64000.5" ||
		edited.Get("qtyMode") != "ABSOLUTE" {
		t.Fatalf("editorder query = %v", edited)
	}
	var batchBody struct {
		BatchOrder []map[string]any `json:"batchOrder"`
	}
	if err := json.Unmarshal([]byte(byKey["POST /derivatives/api/v3/batchorder"].Query.Get("json")), &batchBody); err != nil {
		t.Fatalf("batchorder json query is invalid: %v", err)
	}
	if len(batchBody.BatchOrder) != 4 {
		t.Fatalf("batchorder instructions = %v", batchBody.BatchOrder)
	}
	send := batchBody.BatchOrder[0]
	if send["order"] != "send" || send["order_tag"] != "1" || send["orderType"] != "lmt" || send["size"] != float64(1) ||
		send["limitPrice"] != float64(64000) || send["cliOrdId"] != "client-1" || send["reduceOnly"] != true {
		t.Fatalf("batch send = %v", send)
	}
	if edit := batchBody.BatchOrder[1]; edit["order"] != "edit" || edit["order_id"] != "ORDER-1" ||
		edit["size"] != float64(3) || edit["qtyMode"] != "RELATIVE" || edit["symbol"] != nil {
		t.Fatalf("batch edit = %v", edit)
	}
	if cancel := batchBody.BatchOrder[2]; cancel["order"] != "cancel" || cancel["order_id"] != "ORDER-3" || len(cancel) != 2 {
		t.Fatalf("batch cancel = %v", cancel)
	}
	if byKey["POST /derivatives/api/v3/cancelallorders"].Query.Get("symbol") != "PI_XBTUSD" ||
		byKey["POST /derivatives/api/v3/cancelallordersafter"].Query.Get("timeout") != "60" ||
		byKey["PUT /derivatives/api/v3/self-trade-strategy"].Query.Get("strategy") != "CANCEL_MAKER_SELF" {
		t.Fatalf("recorded requests = %+v", byKey)
	}
	leverageQuery := byKey["PUT /derivatives/api/v3/leveragepreferences"].Query
	if leverageQuery.Get("symbol") != "PF_XBTUSD" || leverageQuery.Get("maxLeverage") != "5" ||
		leverageQuery.Get("marginMode") != "cross" {
		t.Fatalf("leveragepreferences query = %v", leverageQuery)
	}
	pnlQuery := byKey["PUT /derivatives/api/v3/pnlpreferences"].Query
	if pnlQuery.Get("symbol") != "PF_XBTUSD" || pnlQuery.Get("pnlPreference") != "BTC" {
		t.Fatalf("pnlpreferences query = %v", pnlQuery)
	}
	if len(byKey["DELETE /derivatives/api/v3/pnlpreferences"].Query) != 0 {
		t.Fatalf("delete pnlpreferences query = %v", byKey["DELETE /derivatives/api/v3/pnlpreferences"].Query)
	}
}

func TestTradingRESTRejectsBeforeSending(t *testing.T) {
	t.Parallel()

	secret := []byte(base64.StdEncoding.EncodeToString([]byte("test-secret")))
	server, requests := newRESTServer(t, secret, nil, nil)
	client := newRESTClient(t, server, secret)
	ctx := context.Background()

	tooMany := make([]BatchInstruction, 501)
	for index := range tooMany {
		tooMany[index] = BatchInstruction{Kind: BatchInstructionCancel, OrderID: "ORDER-1"}
	}
	cases := []struct {
		name string
		call func() error
	}{
		{"edit without identity", func() error {
			_, err := client.EditOrder(ctx, EditOrderRequest{Size: "1"})
			return err
		}},
		{"edit without change", func() error {
			_, err := client.EditOrder(ctx, EditOrderRequest{OrderID: "ORDER-1"})
			return err
		}},
		{"edit bad quantity mode", func() error {
			_, err := client.EditOrder(ctx, EditOrderRequest{OrderID: "ORDER-1", Size: "1", QuantityMode: "x"})
			return err
		}},
		{"batch empty", func() error {
			_, err := client.BatchOrder(ctx, BatchOrderRequest{})
			return err
		}},
		{"batch too large", func() error {
			_, err := client.BatchOrder(ctx, BatchOrderRequest{Instructions: tooMany})
			return err
		}},
		{"batch send without tag", func() error {
			_, err := client.BatchOrder(ctx, BatchOrderRequest{Instructions: []BatchInstruction{{
				Kind: BatchInstructionSend, OrderType: OrderTypeLimit, Symbol: "PI_XBTUSD",
				Side: SideBuy, Size: "1", LimitPrice: "1",
			}}})
			return err
		}},
		{"batch stop without stop price", func() error {
			_, err := client.BatchOrder(ctx, BatchOrderRequest{Instructions: []BatchInstruction{{
				Kind: BatchInstructionSend, OrderTag: "1", OrderType: OrderTypeStop, Symbol: "PI_XBTUSD",
				Side: SideBuy, Size: "1",
			}}})
			return err
		}},
		{"batch cancel both identities", func() error {
			_, err := client.BatchOrder(ctx, BatchOrderRequest{Instructions: []BatchInstruction{{
				Kind: BatchInstructionCancel, OrderID: "A", ClientOrderID: "B",
			}}})
			return err
		}},
		{"cancel all invalid symbol", func() error {
			_, err := client.CancelAllOrders(ctx, CancelAllOrdersRequest{Symbol: "a b"})
			return err
		}},
		{"self trade strategy invalid", func() error {
			_, err := client.SetSelfTradeStrategy(ctx, "NOPE")
			return err
		}},
		{"leverage invalid mode", func() error {
			_, err := client.SetLeveragePreference(ctx, SetLeveragePreferenceRequest{Symbol: "PF_XBTUSD", MarginMode: "x"})
			return err
		}},
		{"pnl missing currency", func() error {
			_, err := client.SetPnlPreference(ctx, SetPnlPreferenceRequest{})
			return err
		}},
	}
	for _, testCase := range cases {
		if err := testCase.call(); !errors.Is(err, trade.ErrValidation) {
			t.Errorf("%s: error = %v, want validation error", testCase.name, err)
		}
	}
	if sent := requests(); len(sent) != 0 {
		t.Fatalf("invalid requests were sent: %+v", sent)
	}
}

func TestTradingRESTRejectedStatusAndPermission(t *testing.T) {
	t.Parallel()

	secret := []byte(base64.StdEncoding.EncodeToString([]byte("test-secret")))
	server, requests := newRESTServer(t, secret, map[string]string{
		"POST /derivatives/api/v3/editorder":       `{"result":"success","editStatus":{"status":"orderForEditNotFound","orderEvents":[]}}`,
		"POST /derivatives/api/v3/cancelallorders": `{"result":"success","cancelStatus":{"status":"noOrdersToCancel","cancelOnly":"all","cancelledOrders":[],"orderEvents":[]}}`,
	}, nil)
	client := newRESTClient(t, server, secret)
	ctx := context.Background()

	if _, err := client.EditOrder(ctx, EditOrderRequest{OrderID: "ORDER-X", Size: "1"}); err == nil {
		t.Fatal("EditOrder() accepted a rejected edit status")
	}
	result, err := client.CancelAllOrders(ctx, CancelAllOrdersRequest{})
	if err != nil || result.Status != "noOrdersToCancel" {
		t.Fatalf("CancelAllOrders() = %+v, error = %v", result, err)
	}
	if got := requests(); len(got) != 2 || len(got[1].Query) != 0 {
		t.Fatalf("recorded requests = %+v", got)
	}

	readOnly := newRESTClient(t, server, secret, credential.PermissionRead)
	if _, err := readOnly.CancelAllOrdersAfter(ctx, CancelAllOrdersAfterRequest{TimeoutSeconds: 60}); !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("CancelAllOrdersAfter() without trade permission error = %v", err)
	}
	if got := requests(); len(got) != 2 {
		t.Fatalf("request was sent without permission: %+v", got)
	}
}

func TestAccountRESTEndpoints(t *testing.T) {
	t.Parallel()

	secret := []byte(base64.StdEncoding.EncodeToString([]byte("test-secret")))
	server, requests := newRESTServer(t, secret, map[string]string{
		"GET /derivatives/api/v3/unwindqueue":                      `{"result":"success","queue":[{"symbol":"PF_GMTUSD","percentile":100},{"symbol":"PF_UNIUSD","percentile":80}]}`,
		"GET /derivatives/api/v3/notifications":                    `{"result":"success","notifications":[{"type":"maintenance","priority":"high","effectiveTime":"2026-08-26T00:00:00Z","note":"upgrade","expectedDowntimeMinutes":15}]}`,
		"GET /api/auth/v1/api-keys/v3/check":                       `{"apiKey":"abc","accountUid":"uid-1","iiban":null,"createdAt":"2026-01-01T00:00:00Z","permissions":{"general":"FULL_ACCESS","transfer":"NO_ACCESS"},"allowedCidrBlock":null,"allowedCidrBlocks":["10.0.0.0/8"]}`,
		"POST /derivatives/api/v3/transfer":                        `{"result":"success","serverTime":"2026-08-25T00:00:00Z"}`,
		"POST /derivatives/api/v3/transfer/subaccount":             `{"result":"success","serverTime":"2026-08-25T00:00:00Z"}`,
		"GET /derivatives/api/v3/subaccounts":                      `{"result":"success","masterAccountUid":"master-1","subaccounts":[{"accountUid":"sub-1","email":"a@example.com","fullName":null,"holdingAccounts":[{"currency":"xbt","amount":0.5}],"futuresAccounts":[{"name":"f-xbt:usd","availableMargin":12.5}],"flexAccount":{"currencies":[]}}]}`,
		"GET /derivatives/api/v3/subaccount/sub-1/trading-enabled": `{"tradingEnabled":true}`,
		"PUT /derivatives/api/v3/subaccount/sub-1/trading-enabled": `{"tradingEnabled":false}`,
		"GET /derivatives/api/v3/portfolio-margining/parameters":   `{"result":"success","extremePriceShockMultiplier":1.5,"volShockMultiplicationFactor":2,"volShockExponentFactor":0.5,"optionExpiryTimeShockHours":4,"optionsInitialMarginFactor":0.1}`,
		"POST /derivatives/api/v3/portfolio-margining/simulate":    `{"result":"success","maintenanceMargin":10.5,"initialMargin":20,"pnl":-1.25,"portfolioMarginBreakdown":{},"greeks":{}}`,
	}, nil)
	client := newRESTClient(t, server, secret,
		credential.PermissionRead, credential.PermissionTransfer, credential.PermissionWithdraw)
	ctx := context.Background()

	queue, err := client.UnwindQueue(ctx)
	if err != nil || len(queue) != 2 || queue[0].Percentile != 100 || queue[1].Symbol != "PF_UNIUSD" {
		t.Fatalf("UnwindQueue() = %+v, error = %v", queue, err)
	}
	notifications, err := client.Notifications(ctx)
	if err != nil || len(notifications) != 1 || notifications[0].ExpectedDowntimeMinutes != 15 ||
		notifications[0].Priority != "high" {
		t.Fatalf("Notifications() = %+v, error = %v", notifications, err)
	}
	check, err := client.CheckAPIKey(ctx)
	if err != nil || check.AccountUID != "uid-1" || check.Permissions.General != "FULL_ACCESS" ||
		len(check.AllowedCIDRBlocks) != 1 {
		t.Fatalf("CheckAPIKey() = %+v, error = %v", check, err)
	}
	if _, err := client.Transfer(ctx, TransferRequest{
		FromAccount: "cash", ToAccount: "fi_xbtusd", Unit: "xbt", Amount: "0.1",
	}); err != nil {
		t.Fatalf("Transfer() error = %v", err)
	}
	if _, err := client.Transfer(ctx, TransferRequest{
		FromAccount: "flex", ToAccount: "spot", Unit: "usd", Amount: "10",
	}); err != nil {
		t.Fatalf("Transfer(spot) error = %v", err)
	}
	if _, err := client.SubaccountTransfer(ctx, SubaccountTransferRequest{
		FromUser: "master-1", ToUser: "sub-1", FromAccount: "cash", ToAccount: "cash", Unit: "usd", Amount: "5",
	}); err != nil {
		t.Fatalf("SubaccountTransfer() error = %v", err)
	}
	subaccounts, err := client.ListSubaccounts(ctx)
	if err != nil || subaccounts.MasterAccountUID != "master-1" || len(subaccounts.Subaccounts) != 1 ||
		subaccounts.Subaccounts[0].HoldingAccounts[0].Amount != "0.5" ||
		subaccounts.Subaccounts[0].FuturesAccounts[0].AvailableMargin != "12.5" {
		t.Fatalf("ListSubaccounts() = %+v, error = %v", subaccounts, err)
	}
	enabled, err := client.SubaccountTradingEnabled(ctx, "sub-1")
	if err != nil || !enabled {
		t.Fatalf("SubaccountTradingEnabled() = %v, error = %v", enabled, err)
	}
	enabled, err = client.SetSubaccountTradingEnabled(ctx, "sub-1", false)
	if err != nil || enabled {
		t.Fatalf("SetSubaccountTradingEnabled() = %v, error = %v", enabled, err)
	}
	parameters, err := client.PortfolioMarginingParameters(ctx)
	if err != nil || parameters.ExtremePriceShockMultiplier != "1.5" || len(parameters.Raw) == 0 {
		t.Fatalf("PortfolioMarginingParameters() = %+v, error = %v", parameters, err)
	}
	simulation, err := client.SimulatePortfolio(ctx, []PortfolioPosition{
		{Instrument: "PF_BTCUSD", Size: "1.5", EntryPrice: "64000"},
	})
	if err != nil || simulation.InitialMargin != "20" || simulation.PNL != "-1.25" {
		t.Fatalf("SimulatePortfolio() = %+v, error = %v", simulation, err)
	}

	var transfers []recordedRequest
	for _, request := range requests() {
		if !request.Signed {
			t.Fatalf("request %s %s was not signed", request.Method, request.Path)
		}
		switch request.Path {
		case "/derivatives/api/v3/transfer":
			transfers = append(transfers, request)
		case "/derivatives/api/v3/transfer/subaccount":
			if request.Query.Get("fromUser") != "master-1" || request.Query.Get("toUser") != "sub-1" ||
				request.Query.Get("amount") != "5" {
				t.Fatalf("transfer/subaccount query = %v", request.Query)
			}
		case "/derivatives/api/v3/subaccount/sub-1/trading-enabled":
			if request.Method == http.MethodPut && request.Query.Get("tradingEnabled") != "false" {
				t.Fatalf("trading-enabled query = %v", request.Query)
			}
		case "/derivatives/api/v3/portfolio-margining/simulate":
			var body struct {
				Positions []map[string]any `json:"positions"`
			}
			if err := json.Unmarshal([]byte(request.Query.Get("json")), &body); err != nil || len(body.Positions) != 1 ||
				body.Positions[0]["instrument"] != "PF_BTCUSD" || body.Positions[0]["size"] != 1.5 ||
				body.Positions[0]["entryPrice"] != float64(64000) {
				t.Fatalf("simulate json = %q, error = %v", request.Query.Get("json"), err)
			}
		}
	}
	if len(transfers) != 2 || transfers[0].Query.Get("fromAccount") != "cash" || transfers[1].Query.Get("toAccount") != "spot" {
		t.Fatalf("transfer requests = %+v", transfers)
	}
}

func TestTransferPermissionsAndValidation(t *testing.T) {
	t.Parallel()

	secret := []byte(base64.StdEncoding.EncodeToString([]byte("test-secret")))
	server, requests := newRESTServer(t, secret, nil, nil)
	ctx := context.Background()

	transferOnly := newRESTClient(t, server, secret, credential.PermissionRead, credential.PermissionTransfer)
	if _, err := transferOnly.Transfer(ctx, TransferRequest{
		FromAccount: "cash", ToAccount: "spot", Unit: "usd", Amount: "1",
	}); !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("Transfer(spot) without withdraw permission error = %v", err)
	}
	readOnly := newRESTClient(t, server, secret, credential.PermissionRead)
	if _, err := readOnly.SubaccountTransfer(ctx, SubaccountTransferRequest{
		FromUser: "a", ToUser: "b", FromAccount: "cash", ToAccount: "cash", Unit: "usd", Amount: "1",
	}); !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("SubaccountTransfer() without transfer permission error = %v", err)
	}
	for _, request := range []TransferRequest{
		{FromAccount: "cash", ToAccount: "flex", Unit: "usd", Amount: "0"},
		{FromAccount: "cash", ToAccount: "flex", Unit: "usd", Amount: "-1"},
		{FromAccount: "", ToAccount: "flex", Unit: "usd", Amount: "1"},
		{FromAccount: "cash", ToAccount: "flex", Unit: "", Amount: "1"},
	} {
		if _, err := transferOnly.Transfer(ctx, request); !errors.Is(err, trade.ErrValidation) {
			t.Errorf("Transfer(%+v) error = %v, want validation error", request, err)
		}
	}
	if _, err := transferOnly.SimulatePortfolio(ctx, nil); !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("SimulatePortfolio(nil) error = %v", err)
	}
	if _, err := transferOnly.SetSubaccountTradingEnabled(ctx, "bad/uid", true); !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("SetSubaccountTradingEnabled(bad uid) error = %v", err)
	}
	if sent := requests(); len(sent) != 0 {
		t.Fatalf("rejected requests were sent: %+v", sent)
	}
}

func TestMarketRESTEndpoints(t *testing.T) {
	t.Parallel()

	secret := []byte(base64.StdEncoding.EncodeToString([]byte("test-secret")))
	server, requests := newRESTServer(t, secret, map[string]string{
		"GET /derivatives/api/v3/instruments/status":           `{"result":"success","instrumentStatus":[{"tradeable":"PF_XBTUSD","experiencingDislocation":true,"priceDislocationDirection":"ABOVE_UPPER_BOUND","experiencingExtremeVolatility":false,"extremeVolatilityInitialMarginMultiplier":1}]}`,
		"GET /derivatives/api/v3/instruments/PF_XBTUSD/status": `{"result":"success","tradeable":"PF_XBTUSD","experiencingDislocation":false,"priceDislocationDirection":null,"experiencingExtremeVolatility":true,"extremeVolatilityInitialMarginMultiplier":2}`,
		"GET /derivatives/api/v3/trading/instruments":          `{"result":"success","instruments":[{"symbol":"PF_XBTUSD","type":"flexible_futures","tradeable":true,"tickSize":0.5}]}`,
		"GET /derivatives/api/v3/historical-funding-rates":     `{"result":"success","rates":[{"timestamp":"2022-06-28T00:00:00.000Z","fundingRate":-8.15861558e-10,"relativeFundingRate":-0.000016898883333333}]}`,
		"GET /api/history/v3/market/PF_XBTUSD/executions":      `{"len":1,"elements":[{"uid":"e1","timestamp":1700000000000,"event":{}}]}`,
		"GET /api/history/v3/market/PF_XBTUSD/price":           `{"len":0,"elements":[]}`,
		"GET /api/history/v3/executions":                       `{"accountUid":"uid-1","len":1,"elements":[{"uid":"e1","timestamp":1700000000000,"event":{}}]}`,
		"GET /api/history/v3/orders":                           `{"accountUid":"uid-1","len":0,"elements":[]}`,
		"GET /api/history/v3/triggers":                         `{"accountUid":"uid-1","len":0,"elements":[]}`,
		"GET /api/history/v3/positions":                        `{"accountUid":"uid-1","len":0,"elements":[]}`,
		"GET /api/history/v3/account-log":                      `{"accountUid":"uid-1","logs":[{"id":7,"booking_uid":"b1","date":"2026-08-25T00:00:00.000Z","asset":"usd","info":"futures trade","contract":"PF_XBTUSD","collateral":null,"execution":"x1","margin_account":"flex","old_balance":100.5,"new_balance":99,"fee":0.01,"funding_rate":null,"realized_pnl":-1.5}]}`,
		"GET /api/charts/v1/":                                  `["spot","mark","trade"]`,
		"GET /api/charts/v1/trade":                             `["PF_XBTUSD"]`,
		"GET /api/charts/v1/trade/PF_XBTUSD":                   `["1m","1h"]`,
		"GET /api/charts/v1/analytics/PF_XBTUSD/open-interest": `{"result":{"timestamp":[1700000000,1700000060],"data":[1.5,"2.5"],"more":false},"errors":[]}`,
		"GET /api/charts/v1/analytics/liquidity-pool":          `{"result":{"timestamp":[1700000000],"data":[{"usd":"1"}],"more":true},"errors":[]}`,
	}, map[string]string{"Next-Continuation-Token": "TOKEN-2"})
	client := newRESTClient(t, server, secret)
	ctx := context.Background()

	statuses, err := client.InstrumentsStatus(ctx, InstrumentStatusRequest{ContractTypes: []ContractType{ContractTypeAll}})
	if err != nil || len(statuses) != 1 || !statuses[0].ExperiencingDislocation ||
		statuses[0].PriceDislocationDirection != "ABOVE_UPPER_BOUND" {
		t.Fatalf("InstrumentsStatus() = %+v, error = %v", statuses, err)
	}
	status, err := client.InstrumentStatus(ctx, "PF_XBTUSD")
	if err != nil || !status.ExperiencingExtremeVolatility || status.ExtremeVolatilityInitialMarginMultiplier != 2 ||
		status.PriceDislocationDirection != "" {
		t.Fatalf("InstrumentStatus() = %+v, error = %v", status, err)
	}
	instruments, err := client.TradingInstruments(ctx, TradingInstrumentsRequest{ContractTypes: []ContractType{ContractTypeFlexible}})
	if err != nil || len(instruments) != 1 || instruments[0].Symbol != "PF_XBTUSD" || instruments[0].TickSize != "0.5" {
		t.Fatalf("TradingInstruments() = %+v, error = %v", instruments, err)
	}
	rates, err := client.HistoricalFundingRates(ctx, HistoricalFundingRatesRequest{Symbol: "PF_XBTUSD"})
	if err != nil || len(rates) != 1 || rates[0].FundingRate != "-8.15861558e-10" ||
		rates[0].RelativeFundingRate != "-0.000016898883333333" {
		t.Fatalf("HistoricalFundingRates() = %+v, error = %v", rates, err)
	}
	since := time.UnixMilli(1_700_000_000_000)
	before := time.UnixMilli(1_700_000_600_000)
	market, err := client.MarketHistory(ctx, MarketHistoryRequest{
		Kind: MarketHistoryExecutions, Tradeable: "PF_XBTUSD",
		Page: HistoryPageRequest{Since: &since, Before: &before, Ascending: true, Count: 100, ContinuationToken: "TOKEN-1"},
	})
	if err != nil || market.Length != 1 || len(market.Elements) != 1 || market.ContinuationToken != "TOKEN-2" {
		t.Fatalf("MarketHistory() = %+v, error = %v", market, err)
	}
	if _, err := client.MarketHistory(ctx, MarketHistoryRequest{Kind: MarketHistoryPrice, Tradeable: "PF_XBTUSD"}); err != nil {
		t.Fatalf("MarketHistory(price) error = %v", err)
	}
	executions, err := client.AccountHistory(ctx, AccountHistoryRequest{Kind: AccountHistoryExecutions, Tradeable: "PF_XBTUSD"})
	if err != nil || executions.AccountUID != "uid-1" || len(executions.Elements) != 1 || executions.ContinuationToken != "TOKEN-2" {
		t.Fatalf("AccountHistory(executions) = %+v, error = %v", executions, err)
	}
	for _, kind := range []AccountHistoryKind{AccountHistoryOrders, AccountHistoryTriggers} {
		if _, err := client.AccountHistory(ctx, AccountHistoryRequest{Kind: kind, Opened: true, Closed: true}); err != nil {
			t.Fatalf("AccountHistory(%s) error = %v", kind, err)
		}
	}
	if _, err := client.AccountHistory(ctx, AccountHistoryRequest{
		Kind: AccountHistoryPositions, Increased: true, NoChange: true, FundingRealization: true,
	}); err != nil {
		t.Fatalf("AccountHistory(positions) error = %v", err)
	}
	logs, err := client.AccountLog(ctx, AccountLogRequest{
		Since: &since, FromID: 5, ToID: 9, Descending: true, Info: []string{"futures trade", "funding rate change"}, Count: 50,
	})
	if err != nil || logs.AccountUID != "uid-1" || len(logs.Logs) != 1 || logs.Logs[0].ID != 7 ||
		logs.Logs[0].OldBalance != "100.5" || logs.Logs[0].RealizedPNL != "-1.5" || logs.Logs[0].FundingRate != "" {
		t.Fatalf("AccountLog() = %+v, error = %v", logs, err)
	}
	tickTypes, err := client.ChartTickTypes(ctx)
	if err != nil || len(tickTypes) != 3 || tickTypes[1] != "mark" {
		t.Fatalf("ChartTickTypes() = %v, error = %v", tickTypes, err)
	}
	markets, err := client.ChartMarkets(ctx, CandleTickTrade)
	if err != nil || len(markets) != 1 || markets[0] != "PF_XBTUSD" {
		t.Fatalf("ChartMarkets() = %v, error = %v", markets, err)
	}
	resolutions, err := client.ChartResolutions(ctx, CandleTickTrade, "PF_XBTUSD")
	if err != nil || len(resolutions) != 2 || resolutions[1] != Candle1Hour {
		t.Fatalf("ChartResolutions() = %v, error = %v", resolutions, err)
	}
	analytics, err := client.MarketAnalytics(ctx, AnalyticsRequest{
		Symbol: "PF_XBTUSD", Type: AnalyticsOpenInterest, Since: since, IntervalSeconds: 3600, To: &before,
	})
	if err != nil || len(analytics.Timestamps) != 2 || analytics.More || !strings.Contains(string(analytics.Data), `"2.5"`) {
		t.Fatalf("MarketAnalytics() = %+v, error = %v", analytics, err)
	}
	pool, err := client.LiquidityPoolAnalytics(ctx, LiquidityPoolRequest{Since: since, IntervalSeconds: 86400})
	if err != nil || !pool.More || len(pool.Timestamps) != 1 {
		t.Fatalf("LiquidityPoolAnalytics() = %+v, error = %v", pool, err)
	}

	byPath := make(map[string]recordedRequest)
	for _, request := range requests() {
		byPath[request.Path] = request
		wantSigned := !strings.HasPrefix(request.Path, "/api/charts/") && !strings.Contains(request.Path, "/market/") &&
			request.Path != "/derivatives/api/v3/instruments/status" &&
			request.Path != "/derivatives/api/v3/instruments/PF_XBTUSD/status" &&
			request.Path != "/derivatives/api/v3/historical-funding-rates"
		if request.Signed != wantSigned {
			t.Fatalf("request %s signed = %v, want %v", request.Path, request.Signed, wantSigned)
		}
	}
	if got := byPath["/derivatives/api/v3/instruments/status"].Query.Get("contractType"); got != "all" {
		t.Fatalf("instruments/status contractType = %q", got)
	}
	if got := byPath["/derivatives/api/v3/historical-funding-rates"].Query.Get("symbol"); got != "PF_XBTUSD" {
		t.Fatalf("historical-funding-rates symbol = %q", got)
	}
	marketQuery := byPath["/api/history/v3/market/PF_XBTUSD/executions"].Query
	if marketQuery.Get("since") != "1700000000000" || marketQuery.Get("before") != "1700000600000" ||
		marketQuery.Get("sort") != "asc" || marketQuery.Get("count") != "100" ||
		marketQuery.Get("continuation_token") != "TOKEN-1" {
		t.Fatalf("market history query = %v", marketQuery)
	}
	if got := byPath["/api/history/v3/executions"].Query.Get("tradeable"); got != "PF_XBTUSD" {
		t.Fatalf("executions tradeable = %q", got)
	}
	positionsQuery := byPath["/api/history/v3/positions"].Query
	if positionsQuery.Get("increased") != "true" || positionsQuery.Get("no_change") != "true" ||
		positionsQuery.Get("funding_realization") != "true" || positionsQuery.Get("opened") != "" {
		t.Fatalf("positions query = %v", positionsQuery)
	}
	logQuery := byPath["/api/history/v3/account-log"].Query
	if logQuery.Get("since") != "1700000000000" || logQuery.Get("from") != "5" || logQuery.Get("to") != "9" ||
		logQuery.Get("sort") != "desc" || logQuery.Get("count") != "50" ||
		strings.Join(logQuery["info"], "|") != "futures trade|funding rate change" {
		t.Fatalf("account-log query = %v", logQuery)
	}
	analyticsQuery := byPath["/api/charts/v1/analytics/PF_XBTUSD/open-interest"].Query
	if analyticsQuery.Get("since") != "1700000000" || analyticsQuery.Get("interval") != "3600" ||
		analyticsQuery.Get("to") != "1700000600" {
		t.Fatalf("analytics query = %v", analyticsQuery)
	}
}

func TestMarketRESTRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	secret := []byte(base64.StdEncoding.EncodeToString([]byte("test-secret")))
	server, requests := newRESTServer(t, secret, nil, nil)
	client := newRESTClient(t, server, secret)
	ctx := context.Background()
	now := time.UnixMilli(1_700_000_000_000)
	earlier := now.Add(-time.Hour)

	cases := []struct {
		name string
		call func() error
	}{
		{"status symbol", func() error { _, err := client.InstrumentStatus(ctx, "a/b"); return err }},
		{"status contract type", func() error {
			_, err := client.InstrumentsStatus(ctx, InstrumentStatusRequest{ContractTypes: []ContractType{"x"}})
			return err
		}},
		{"funding symbol", func() error {
			_, err := client.HistoricalFundingRates(ctx, HistoricalFundingRatesRequest{})
			return err
		}},
		{"market history kind", func() error {
			_, err := client.MarketHistory(ctx, MarketHistoryRequest{Kind: "x", Tradeable: "PF_XBTUSD"})
			return err
		}},
		{"history count", func() error {
			_, err := client.AccountHistory(ctx, AccountHistoryRequest{
				Kind: AccountHistoryOrders, Page: HistoryPageRequest{Count: 1001},
			})
			return err
		}},
		{"history sort", func() error {
			_, err := client.AccountHistory(ctx, AccountHistoryRequest{
				Kind: AccountHistoryOrders, Page: HistoryPageRequest{Ascending: true, Descending: true},
			})
			return err
		}},
		{"history range", func() error {
			_, err := client.AccountHistory(ctx, AccountHistoryRequest{
				Kind: AccountHistoryOrders, Page: HistoryPageRequest{Since: &now, Before: &earlier},
			})
			return err
		}},
		{"account log id range", func() error {
			_, err := client.AccountLog(ctx, AccountLogRequest{FromID: 10, ToID: 5})
			return err
		}},
		{"chart tick type", func() error { _, err := client.ChartMarkets(ctx, "x"); return err }},
		{"analytics type", func() error {
			_, err := client.MarketAnalytics(ctx, AnalyticsRequest{
				Symbol: "PF_XBTUSD", Type: "x", Since: now, IntervalSeconds: 60,
			})
			return err
		}},
		{"analytics interval", func() error {
			_, err := client.MarketAnalytics(ctx, AnalyticsRequest{
				Symbol: "PF_XBTUSD", Type: AnalyticsFunding, Since: now, IntervalSeconds: 61,
			})
			return err
		}},
		{"liquidity pool since", func() error {
			_, err := client.LiquidityPoolAnalytics(ctx, LiquidityPoolRequest{IntervalSeconds: 60})
			return err
		}},
	}
	for _, testCase := range cases {
		if err := testCase.call(); !errors.Is(err, trade.ErrValidation) {
			t.Errorf("%s: error = %v, want validation error", testCase.name, err)
		}
	}
	if sent := requests(); len(sent) != 0 {
		t.Fatalf("invalid requests were sent: %+v", sent)
	}
}

func TestAccountLogCostBuckets(t *testing.T) {
	t.Parallel()

	for count, want := range map[int]int{0: 3, 1: 1, 25: 1, 26: 2, 50: 2, 51: 3, 1000: 3, 1001: 6, 5000: 6, 5001: 10, 10000: 10} {
		if got := accountLogCost(count); got != want {
			t.Errorf("accountLogCost(%d) = %d, want %d", count, got, want)
		}
	}
}

func TestBatchOrderChargesNinePlusInstructionCount(t *testing.T) {
	t.Parallel()

	secret := []byte(base64.StdEncoding.EncodeToString([]byte("test-secret")))
	server, _ := newRESTServer(t, secret, map[string]string{
		"POST /derivatives/api/v3/batchorder": `{"result":"success","batchStatus":[]}`,
	}, nil)
	provider := &recordingProvider{secret: secret}
	client, limiter := newTestClient(
		t, server.URL, &directSender{}, provider, []transport.EgressRouteID{"route-a"}, time.UnixMilli(1_700_000_000_000),
	)
	if _, err := client.BatchOrder(context.Background(), BatchOrderRequest{Instructions: []BatchInstruction{
		{Kind: BatchInstructionCancel, OrderID: "A"}, {Kind: BatchInstructionCancel, OrderID: "B"},
	}}); err != nil {
		t.Fatalf("BatchOrder() error = %v", err)
	}
	snapshot, err := limiter.Snapshot("kraken-futures:account:kraken-main:derivatives:10seconds")
	if err != nil || snapshot.Used != 11 {
		t.Fatalf("limiter snapshot = %+v, error = %v", snapshot, err)
	}
}
