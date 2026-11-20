package upbit

import (
	"context"
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
	mainPocketUUID = "9ca023a5-851b-4fec-9f0a-48cd83c2eaae"
	subPocketUUID  = "3b11b5f9-1c0c-4d2b-a5b4-f0c6a8d5c001"
)

func TestPocketReadEndpoints(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.Header().Set("Remaining-Req", "group=default; min=1800; sec=29")
		switch request.Method + " " + request.URL.RequestURI() {
		case "GET /v1/pockets":
			verifySignedQuery(t, request, "")
			_, _ = io.WriteString(writer, `[{"uuid":"`+mainPocketUUID+`","name":"메인포켓","type":"main"},{"uuid":"`+subPocketUUID+`","name":"단기 트레이딩 계좌","type":"user_spot_trading"}]`)
		case "GET /v1/pockets/api_keys?uuids[]=" + mainPocketUUID + "&uuids[]=" + subPocketUUID + "&include_expired=true":
			verifySignedQuery(t, request, "uuids[]="+mainPocketUUID+"&uuids[]="+subPocketUUID+"&include_expired=true")
			_, _ = io.WriteString(writer, `[{"uuid":"`+mainPocketUUID+`","keys":[{"access_key":"8f3a2c1d9b7e6a5c4d3f2e1a0b9c8d7e6f5a4b3c","permissions":["deposit","transfer","manage_pockets"],"allowed_ips":["203.0.113.10","198.51.100.25"],"created_at":"2026-05-28T15:17:51+09:00","expired_at":"2027-05-27T15:17:51+09:00"}]},{"uuid":"`+subPocketUUID+`","keys":[]}]`)
		case "GET /v1/pockets/api_keys":
			verifySignedQuery(t, request, "")
			_, _ = io.WriteString(writer, `[]`)
		case "GET /v1/pockets/assets?uuid=" + subPocketUUID:
			verifySignedQuery(t, request, "uuid="+subPocketUUID)
			_, _ = io.WriteString(writer, `[{"currency":"KRW","balance":"1000000.0","locked":"0.0","avg_buy_price":"0","avg_buy_price_modified":false,"unit_currency":"KRW"},{"currency":"BTC","balance":"2.0","locked":"0.5","avg_buy_price":"140000000","avg_buy_price_modified":true,"unit_currency":"KRW"}]`)
		case "GET /v1/pockets/transfers?direction=in&states[]=processing&states[]=done&uuids[]=uuid-1&identifiers[]=client_made_id_123&start_time=2026-05-28T15%3A17%3A51%2B09%3A00&end_time=2026-05-29T15%3A17%3A51%2B09%3A00&currency=XRP&limit=50&order_by=asc":
			verifySignedQuery(t, request, "direction=in&states[]=processing&states[]=done&uuids[]=uuid-1&identifiers[]=client_made_id_123&start_time=2026-05-28T15:17:51+09:00&end_time=2026-05-29T15:17:51+09:00&currency=XRP&limit=50&order_by=asc")
			_, _ = io.WriteString(writer, `[{"from":"`+mainPocketUUID+`","to":"`+subPocketUUID+`","uuid":"uuid-1","identifier":"client_made_id_123","state":"processing","currency":"XRP","amount":"150.00","created_at":"2026-05-29T11:20:10+09:00"},{"from":"`+mainPocketUUID+`","to":"`+subPocketUUID+`","uuid":"uuid-2","identifier":null,"state":"done","currency":"XRP","amount":"20","created_at":"2026-05-28T16:00:00+09:00"}]`)
		case "GET /v1/pockets/transfers":
			verifySignedQuery(t, request, "")
			_, _ = io.WriteString(writer, `[]`)
		case "GET /v1/pockets/universal_transfers?from=" + mainPocketUUID + "&to=" + subPocketUUID + "&states[]=submitted&currency=XRP&limit=100":
			verifySignedQuery(t, request, "from="+mainPocketUUID+"&to="+subPocketUUID+"&states[]=submitted&currency=XRP&limit=100")
			_, _ = io.WriteString(writer, `[{"from":"`+mainPocketUUID+`","to":"`+subPocketUUID+`","uuid":"uuid-3","identifier":"client_made_id_456","state":"submitted","currency":"XRP","amount":"10.00","created_at":"2026-05-29T11:20:10+09:00"}]`)
		case "GET /v1/pockets/universal_transfers":
			verifySignedQuery(t, request, "")
			_, _ = io.WriteString(writer, `[]`)
		default:
			t.Errorf("unexpected request %s %s", request.Method, request.URL.RequestURI())
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client, provider, limiter := newPrivateTestClient(t, server.URL, credential.PermissionRead)
	ctx := context.Background()
	kst := time.FixedZone("KST", 9*60*60)
	start := time.Date(2026, time.May, 28, 15, 17, 51, 0, kst)
	end := start.Add(24 * time.Hour)

	pockets, err := client.Pockets(ctx)
	if err != nil || len(pockets) != 2 || pockets[0].UUID != mainPocketUUID || pockets[0].Name != "메인포켓" ||
		pockets[0].Type != PocketTypeMain || pockets[1].UUID != subPocketUUID || pockets[1].Name != "단기 트레이딩 계좌" ||
		pockets[1].Type != PocketTypeUserSpotTrading || len(pockets[1].Raw) == 0 {
		t.Fatalf("Pockets() = %+v, error = %v", pockets, err)
	}
	keySets, err := client.PocketAPIKeys(ctx, PocketAPIKeysRequest{UUIDs: []string{mainPocketUUID, subPocketUUID}, IncludeExpired: true})
	if err != nil || len(keySets) != 2 || keySets[0].UUID != mainPocketUUID || len(keySets[0].Keys) != 1 ||
		keySets[0].Keys[0].AccessKey != "8f3a2c1d9b7e6a5c4d3f2e1a0b9c8d7e6f5a4b3c" ||
		!slices.Equal(keySets[0].Keys[0].Permissions, []string{"deposit", "transfer", "manage_pockets"}) ||
		!slices.Equal(keySets[0].Keys[0].AllowedIPs, []string{"203.0.113.10", "198.51.100.25"}) ||
		keySets[0].Keys[0].CreatedAt != "2026-05-28T15:17:51+09:00" || keySets[0].Keys[0].ExpiredAt != "2027-05-27T15:17:51+09:00" ||
		len(keySets[0].Raw) == 0 || keySets[1].UUID != subPocketUUID || len(keySets[1].Keys) != 0 {
		t.Fatalf("PocketAPIKeys() = %+v, error = %v", keySets, err)
	}
	noKeys, err := client.PocketAPIKeys(ctx, PocketAPIKeysRequest{})
	if err != nil || len(noKeys) != 0 {
		t.Fatalf("PocketAPIKeys(default) = %+v, error = %v", noKeys, err)
	}
	assets, err := client.PocketAssets(ctx, PocketAssetsRequest{UUID: subPocketUUID})
	if err != nil || len(assets) != 2 || assets[0].Currency != "KRW" || assets[0].Balance != "1000000.0" ||
		assets[1].Currency != "BTC" || assets[1].Balance != "2.0" || assets[1].Locked != "0.5" ||
		assets[1].AverageBuyPrice != "140000000" || !assets[1].AverageBuyPriceModified || assets[1].UnitCurrency != "KRW" {
		t.Fatalf("PocketAssets() = %+v, error = %v", assets, err)
	}
	transfers, err := client.PocketTransfers(ctx, PocketTransfersRequest{
		Direction: TransferDirectionIn,
		PocketTransferQuery: PocketTransferQuery{
			States:      []PocketTransferState{PocketTransferProcessing, PocketTransferDone},
			UUIDs:       []string{"uuid-1"},
			Identifiers: []string{"client_made_id_123"},
			StartTime:   &start, EndTime: &end, Currency: "XRP", Limit: 50, OrderBy: "asc",
		},
	})
	if err != nil || len(transfers) != 2 || transfers[0].UUID != "uuid-1" || transfers[0].Identifier != "client_made_id_123" ||
		transfers[0].From != mainPocketUUID || transfers[0].To != subPocketUUID || transfers[0].State != PocketTransferProcessing ||
		transfers[0].Currency != "XRP" || transfers[0].Amount != "150.00" || transfers[0].CreatedAt != "2026-05-29T11:20:10+09:00" ||
		len(transfers[0].Raw) == 0 || transfers[1].Identifier != "" || transfers[1].State != PocketTransferDone || transfers[1].Amount != "20" {
		t.Fatalf("PocketTransfers() = %+v, error = %v", transfers, err)
	}
	noTransfers, err := client.PocketTransfers(ctx, PocketTransfersRequest{})
	if err != nil || len(noTransfers) != 0 {
		t.Fatalf("PocketTransfers(default) = %+v, error = %v", noTransfers, err)
	}
	universal, err := client.UniversalTransfers(ctx, UniversalTransfersRequest{
		From: mainPocketUUID, To: subPocketUUID,
		PocketTransferQuery: PocketTransferQuery{States: []PocketTransferState{PocketTransferSubmitted}, Currency: "XRP", Limit: 100},
	})
	if err != nil || len(universal) != 1 || universal[0].UUID != "uuid-3" || universal[0].State != PocketTransferSubmitted ||
		universal[0].Identifier != "client_made_id_456" || universal[0].Amount != "10.00" || len(universal[0].Raw) == 0 {
		t.Fatalf("UniversalTransfers() = %+v, error = %v", universal, err)
	}
	noUniversal, err := client.UniversalTransfers(ctx, UniversalTransfersRequest{})
	if err != nil || len(noUniversal) != 0 {
		t.Fatalf("UniversalTransfers(default) = %+v, error = %v", noUniversal, err)
	}
	assertSecretsDestroyed(t, provider, 8)
	assertRateRule(t, limiter, "upbit:account:upbit-pocket:default:1second", 30, time.Second)
}

func TestPocketTransferMutationEndpoints(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.Header().Set("Remaining-Req", "group=default; min=1800; sec=29")
		body, _ := io.ReadAll(request.Body)
		switch request.Method + " " + request.URL.RequestURI() + " " + string(body) {
		case `POST /v1/pockets/transfers {"to":"` + mainPocketUUID + `","currency":"XRP","amount":"10.00","identifier":"unique_transfer_id_20260531_01"}`:
			verifySignedBody(t, request, "to="+mainPocketUUID+"&currency=XRP&amount=10.00&identifier=unique_transfer_id_20260531_01")
			writer.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(writer, `{"uuid":"d6bb6a87-27a9-4c1c-9a54-0e1a54f06eb4","identifier":"unique_transfer_id_20260531_01","from":"`+subPocketUUID+`","to":"`+mainPocketUUID+`","state":"processing","currency":"XRP","amount":"10.00","created_at":"2026-05-28T15:17:51+09:00"}`)
		case `POST /v1/pockets/transfers {"currency":"XRP","amount":"1"}`:
			verifySignedBody(t, request, "currency=XRP&amount=1")
			writer.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(writer, `{"uuid":"d6bb6a87-27a9-4c1c-9a54-0e1a54f06eb5","identifier":null,"from":"`+subPocketUUID+`","to":"`+mainPocketUUID+`","state":"submitted","currency":"XRP","amount":"1","created_at":"2026-05-28T15:18:00+09:00"}`)
		case `POST /v1/pockets/universal_transfers {"from":"` + subPocketUUID + `","to":"` + mainPocketUUID + `","currency":"XRP","amount":"10.00","identifier":"unique_transfer_id_20260531_01"}`:
			verifySignedBody(t, request, "from="+subPocketUUID+"&to="+mainPocketUUID+"&currency=XRP&amount=10.00&identifier=unique_transfer_id_20260531_01")
			writer.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(writer, `{"uuid":"d6bb6a87-27a9-4c1c-9a54-0e1a54f06eb6","identifier":"unique_transfer_id_20260531_01","from":"`+subPocketUUID+`","to":"`+mainPocketUUID+`","state":"processing","currency":"XRP","amount":"10.00","created_at":"2026-05-28T15:17:51+09:00"}`)
		case `POST /v1/pockets/universal_transfers {"to":"` + subPocketUUID + `","currency":"KRW","amount":"1000000"}`:
			verifySignedBody(t, request, "to="+subPocketUUID+"&currency=KRW&amount=1000000")
			writer.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(writer, `{"uuid":"d6bb6a87-27a9-4c1c-9a54-0e1a54f06eb7","identifier":null,"from":"`+mainPocketUUID+`","to":"`+subPocketUUID+`","state":"done","currency":"KRW","amount":"1000000","created_at":"2026-05-28T15:19:00+09:00"}`)
		default:
			t.Errorf("unexpected request %s %s %s", request.Method, request.URL.RequestURI(), body)
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client, provider, limiter := newPrivateTestClient(t, server.URL, credential.PermissionTransfer)
	ctx := context.Background()

	created, err := client.CreatePocketTransfer(ctx, PocketTransferRequest{
		To: mainPocketUUID, Currency: "XRP", Amount: "10.00", Identifier: "unique_transfer_id_20260531_01",
	})
	if err != nil || created.UUID != "d6bb6a87-27a9-4c1c-9a54-0e1a54f06eb4" || created.Identifier != "unique_transfer_id_20260531_01" ||
		created.From != subPocketUUID || created.To != mainPocketUUID || created.State != PocketTransferProcessing ||
		created.Currency != "XRP" || created.Amount != "10.00" || created.CreatedAt != "2026-05-28T15:17:51+09:00" || len(created.Raw) == 0 {
		t.Fatalf("CreatePocketTransfer() = %+v, error = %v", created, err)
	}
	toMain, err := client.CreatePocketTransfer(ctx, PocketTransferRequest{Currency: "XRP", Amount: "1"})
	if err != nil || toMain.Identifier != "" || toMain.State != PocketTransferSubmitted || toMain.To != mainPocketUUID {
		t.Fatalf("CreatePocketTransfer(main) = %+v, error = %v", toMain, err)
	}
	universal, err := client.CreateUniversalTransfer(ctx, UniversalTransferRequest{
		From: subPocketUUID, To: mainPocketUUID, Currency: "XRP", Amount: "10.00", Identifier: "unique_transfer_id_20260531_01",
	})
	if err != nil || universal.UUID != "d6bb6a87-27a9-4c1c-9a54-0e1a54f06eb6" || universal.From != subPocketUUID ||
		universal.To != mainPocketUUID || universal.State != PocketTransferProcessing || len(universal.Raw) == 0 {
		t.Fatalf("CreateUniversalTransfer() = %+v, error = %v", universal, err)
	}
	fromOwn, err := client.CreateUniversalTransfer(ctx, UniversalTransferRequest{To: subPocketUUID, Currency: "KRW", Amount: "1000000"})
	if err != nil || fromOwn.State != PocketTransferDone || fromOwn.From != mainPocketUUID || fromOwn.Amount != "1000000" ||
		fromOwn.Identifier != "" {
		t.Fatalf("CreateUniversalTransfer(own pocket) = %+v, error = %v", fromOwn, err)
	}
	assertSecretsDestroyed(t, provider, 4)
	assertRateRule(t, limiter, "upbit:account:upbit-pocket:default:1second", 30, time.Second)
}

func TestPocketErrorsAndMutationsAreNotRetried(t *testing.T) {
	t.Parallel()

	var posts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.Method + " " + request.URL.Path {
		case "GET /v1/pockets/assets":
			writer.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(writer, `{"error":{"name":"pocket_not_found","message":"포켓을 찾지 못했습니다."}}`)
		case "GET /v1/pockets":
			writer.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(writer, `{"error":{"name":"out_of_scope","message":"권한이 부족합니다."}}`)
		case "POST /v1/pockets/transfers":
			posts.Add(1)
			writer.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(writer, `{"error":{"name":"duplicated_identifier","message":"이미 등록된 identifier입니다."}}`)
		case "POST /v1/pockets/universal_transfers":
			posts.Add(1)
			writer.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(writer, `{"error":{"name":"internal_server_error","message":"temporary"}}`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client, _, _ := newPrivateTestClient(t, server.URL, credential.PermissionRead, credential.PermissionTransfer)
	ctx := context.Background()

	_, err := client.PocketAssets(ctx, PocketAssetsRequest{UUID: "missing"})
	if apiError := requireAPIError(t, err, http.StatusNotFound, "pocket_not_found"); apiError.ExchangeMessage != "포켓을 찾지 못했습니다." {
		t.Fatalf("PocketAssets() error = %+v", apiError)
	}
	_, err = client.Pockets(ctx)
	requireAPIError(t, err, http.StatusForbidden, "out_of_scope")
	if !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("Pockets() error = %v, want ErrAuthorization from out_of_scope", err)
	}
	_, err = client.CreatePocketTransfer(ctx, PocketTransferRequest{Currency: "XRP", Amount: "1", Identifier: "dup"})
	requireAPIError(t, err, http.StatusBadRequest, "duplicated_identifier")
	if !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("CreatePocketTransfer() error = %v, want ErrValidation", err)
	}
	_, err = client.CreateUniversalTransfer(ctx, UniversalTransferRequest{To: mainPocketUUID, Currency: "XRP", Amount: "1"})
	requireAPIError(t, err, http.StatusInternalServerError, "internal_server_error")
	if !errors.Is(err, trade.ErrUnknownExecutionState) {
		t.Fatalf("CreateUniversalTransfer() error = %v, want ErrUnknownExecutionState", err)
	}
	if got := posts.Load(); got != 2 {
		t.Fatalf("POST requests = %d, want 2 without retry", got)
	}
}

func TestPocketMutationMalformedResponseIsUnknownExecutionState(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(writer, `{"uuid":`)
	}))
	defer server.Close()

	client, _, _ := newPrivateTestClient(t, server.URL, credential.PermissionTransfer)
	_, err := client.CreateUniversalTransfer(context.Background(), UniversalTransferRequest{To: mainPocketUUID, Currency: "XRP", Amount: "1"})
	if !errors.Is(err, trade.ErrUnknownExecutionState) {
		t.Fatalf("CreateUniversalTransfer() error = %v, want ErrUnknownExecutionState", err)
	}
}

func TestPocketPermissionBoundaries(t *testing.T) {
	t.Parallel()

	reads := []callCase{
		{"Pockets needs read", func(ctx context.Context, client *Client) error {
			_, err := client.Pockets(ctx)
			return err
		}},
		{"PocketAPIKeys needs read", func(ctx context.Context, client *Client) error {
			_, err := client.PocketAPIKeys(ctx, PocketAPIKeysRequest{})
			return err
		}},
		{"PocketAssets needs read", func(ctx context.Context, client *Client) error {
			_, err := client.PocketAssets(ctx, PocketAssetsRequest{UUID: subPocketUUID})
			return err
		}},
		{"PocketTransfers needs read", func(ctx context.Context, client *Client) error {
			_, err := client.PocketTransfers(ctx, PocketTransfersRequest{})
			return err
		}},
		{"UniversalTransfers needs read", func(ctx context.Context, client *Client) error {
			_, err := client.UniversalTransfers(ctx, UniversalTransfersRequest{})
			return err
		}},
	}
	assertRejectedBeforeSend(
		t, trade.ErrAuthorization, reads,
		credential.PermissionTrade, credential.PermissionTransfer, credential.PermissionWithdraw,
	)

	mutations := []callCase{
		{"CreatePocketTransfer needs transfer", func(ctx context.Context, client *Client) error {
			_, err := client.CreatePocketTransfer(ctx, PocketTransferRequest{Currency: "XRP", Amount: "1"})
			return err
		}},
		{"CreateUniversalTransfer needs transfer", func(ctx context.Context, client *Client) error {
			_, err := client.CreateUniversalTransfer(ctx, UniversalTransferRequest{To: mainPocketUUID, Currency: "XRP", Amount: "1"})
			return err
		}},
	}
	assertRejectedBeforeSend(
		t, trade.ErrAuthorization, mutations,
		credential.PermissionRead, credential.PermissionTrade, credential.PermissionWithdraw,
	)
}

func TestPocketRequestsAreValidatedBeforeSend(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.May, 28, 15, 17, 51, 0, time.UTC)
	tooLong := start.Add(7*24*time.Hour + time.Second)
	cases := []callCase{
		{"PocketAPIKeys duplicate uuid", func(ctx context.Context, client *Client) error {
			_, err := client.PocketAPIKeys(ctx, PocketAPIKeysRequest{UUIDs: []string{"a", "a"}})
			return err
		}},
		{"PocketAssets missing uuid", func(ctx context.Context, client *Client) error {
			_, err := client.PocketAssets(ctx, PocketAssetsRequest{})
			return err
		}},
		{"CreatePocketTransfer zero amount", func(ctx context.Context, client *Client) error {
			_, err := client.CreatePocketTransfer(ctx, PocketTransferRequest{Currency: "XRP", Amount: "0"})
			return err
		}},
		{"PocketTransfers unsupported direction", func(ctx context.Context, client *Client) error {
			_, err := client.PocketTransfers(ctx, PocketTransfersRequest{Direction: "sideways"})
			return err
		}},
		{"CreateUniversalTransfer missing destination", func(ctx context.Context, client *Client) error {
			_, err := client.CreateUniversalTransfer(ctx, UniversalTransferRequest{Currency: "XRP", Amount: "1"})
			return err
		}},
		{"UniversalTransfers range above seven days", func(ctx context.Context, client *Client) error {
			_, err := client.UniversalTransfers(ctx, UniversalTransfersRequest{
				PocketTransferQuery: PocketTransferQuery{StartTime: &start, EndTime: &tooLong},
			})
			return err
		}},
	}
	assertRejectedBeforeSend(t, trade.ErrValidation, cases, credential.PermissionRead, credential.PermissionTransfer)
}

func TestPocketRequestValidation(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.May, 28, 15, 17, 51, 0, time.UTC)
	weekEnd := start.Add(7 * 24 * time.Hour)
	tooLong := weekEnd.Add(time.Second)
	before := start.Add(-time.Second)

	valid := []depositValidationCase{
		{"api keys default", PocketAPIKeysRequest{}.validate()},
		{"api keys uuids", PocketAPIKeysRequest{UUIDs: []string{mainPocketUUID, subPocketUUID}, IncludeExpired: true}.validate()},
		{"assets", PocketAssetsRequest{UUID: subPocketUUID}.validate()},
		{"transfer to main", PocketTransferRequest{Currency: "XRP", Amount: "10.5"}.validate()},
		{"transfer to pocket with identifier", PocketTransferRequest{To: subPocketUUID, Currency: "KRW", Amount: "1000", Identifier: "a.b_c-1"}.validate()},
		{"universal transfer", UniversalTransferRequest{From: mainPocketUUID, To: subPocketUUID, Currency: "BTC", Amount: "0.001"}.validate()},
		{"transfers default", PocketTransfersRequest{}.validate()},
		{"transfers full", PocketTransfersRequest{
			Direction: TransferDirectionAll,
			PocketTransferQuery: PocketTransferQuery{
				States: []PocketTransferState{PocketTransferSubmitted, PocketTransferFailed}, UUIDs: numberedValues("uuid-", 20),
				StartTime: &start, EndTime: &weekEnd, Currency: "XRP", Limit: 100, OrderBy: "desc",
			},
		}.validate()},
		{"transfers identifiers only", PocketTransfersRequest{Direction: TransferDirectionOut, PocketTransferQuery: PocketTransferQuery{Identifiers: numberedValues("id-", 20)}}.validate()},
		{"transfers start only", PocketTransfersRequest{PocketTransferQuery: PocketTransferQuery{StartTime: &start}}.validate()},
		{"universal transfers default", UniversalTransfersRequest{}.validate()},
		{"universal transfers endpoints", UniversalTransfersRequest{From: mainPocketUUID, To: subPocketUUID, PocketTransferQuery: PocketTransferQuery{EndTime: &weekEnd}}.validate()},
	}
	invalid := []depositValidationCase{
		{"api keys duplicate", PocketAPIKeysRequest{UUIDs: []string{"a", "a"}}.validate()},
		{"api keys whitespace", PocketAPIKeysRequest{UUIDs: []string{"a b"}}.validate()},
		{"assets empty", PocketAssetsRequest{}.validate()},
		{"assets whitespace", PocketAssetsRequest{UUID: " " + subPocketUUID}.validate()},
		{"transfer whitespace destination", PocketTransferRequest{To: "a b", Currency: "XRP", Amount: "1"}.validate()},
		{"transfer lowercase currency", PocketTransferRequest{Currency: "xrp", Amount: "1"}.validate()},
		{"transfer missing currency", PocketTransferRequest{Amount: "1"}.validate()},
		{"transfer zero amount", PocketTransferRequest{Currency: "XRP", Amount: "0.0"}.validate()},
		{"transfer scientific amount", PocketTransferRequest{Currency: "XRP", Amount: "1e3"}.validate()},
		{"transfer negative amount", PocketTransferRequest{Currency: "XRP", Amount: "-1"}.validate()},
		{"transfer missing amount", PocketTransferRequest{Currency: "XRP"}.validate()},
		{"transfer identifier space", PocketTransferRequest{Currency: "XRP", Amount: "1", Identifier: "a b"}.validate()},
		{"transfer identifier too long", PocketTransferRequest{Currency: "XRP", Amount: "1", Identifier: strings.Repeat("a", 65)}.validate()},
		{"universal missing destination", UniversalTransferRequest{Currency: "XRP", Amount: "1"}.validate()},
		{"universal whitespace source", UniversalTransferRequest{From: "a b", To: subPocketUUID, Currency: "XRP", Amount: "1"}.validate()},
		{"universal zero amount", UniversalTransferRequest{To: subPocketUUID, Currency: "XRP", Amount: "0"}.validate()},
		{"universal identifier slash", UniversalTransferRequest{To: subPocketUUID, Currency: "XRP", Amount: "1", Identifier: "a/b"}.validate()},
		{"transfers unsupported direction", PocketTransfersRequest{Direction: "sideways"}.validate()},
		{"transfers unsupported state", PocketTransfersRequest{PocketTransferQuery: PocketTransferQuery{States: []PocketTransferState{"pending"}}}.validate()},
		{"transfers uuids above 20", PocketTransfersRequest{PocketTransferQuery: PocketTransferQuery{UUIDs: numberedValues("uuid-", 21)}}.validate()},
		{"transfers identifiers above 20", PocketTransfersRequest{PocketTransferQuery: PocketTransferQuery{Identifiers: numberedValues("id-", 21)}}.validate()},
		{"transfers duplicate uuid", PocketTransfersRequest{PocketTransferQuery: PocketTransferQuery{UUIDs: []string{"a", "a"}}}.validate()},
		{"transfers start after end", PocketTransfersRequest{PocketTransferQuery: PocketTransferQuery{StartTime: &start, EndTime: &before}}.validate()},
		{"transfers range above seven days", PocketTransfersRequest{PocketTransferQuery: PocketTransferQuery{StartTime: &start, EndTime: &tooLong}}.validate()},
		{"transfers lowercase currency", PocketTransfersRequest{PocketTransferQuery: PocketTransferQuery{Currency: "xrp"}}.validate()},
		{"transfers limit above 100", PocketTransfersRequest{PocketTransferQuery: PocketTransferQuery{Limit: 101}}.validate()},
		{"transfers negative limit", PocketTransfersRequest{PocketTransferQuery: PocketTransferQuery{Limit: -1}}.validate()},
		{"transfers unsupported order", PocketTransfersRequest{PocketTransferQuery: PocketTransferQuery{OrderBy: "up"}}.validate()},
		{"universal transfers whitespace source", UniversalTransfersRequest{From: "a b"}.validate()},
		{"universal transfers whitespace destination", UniversalTransfersRequest{To: "a\tb"}.validate()},
		{"universal transfers range above seven days", UniversalTransfersRequest{PocketTransferQuery: PocketTransferQuery{StartTime: &start, EndTime: &tooLong}}.validate()},
	}
	assertValidationCases(t, valid, invalid)
}
