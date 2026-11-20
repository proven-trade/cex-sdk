package htx

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	"github.com/proven-trade/cex-sdk/transport"
)

func TestClientAccountExtensions(t *testing.T) {
	t.Parallel()
	fixedNow := time.Date(2026, time.August, 25, 3, 4, 5, 0, time.UTC)
	start := fixedNow.Add(-30 * time.Minute)
	end := fixedNow
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if !verifyHTXSignedRequest(t, request, []byte("test-secret"), fixedNow) {
			http.Error(writer, `{"status":"error","err-code":"api-signature-not-valid"}`, http.StatusUnauthorized)
			return
		}
		query := request.URL.Query()
		switch {
		case request.URL.Path == "/v2/account/valuation" && request.Method == http.MethodGet:
			if query.Get("accountType") != "1" || query.Get("valuationCurrency") != "BTC" {
				extBadRequest(writer)
				return
			}
			_, _ = io.WriteString(writer, `{"code":200,"data":{"updated":{"success":true,"time":1629916724000},"todayProfitRate":"0.0123","totalBalance":"0.5","todayProfit":"0.001","profitAccountBalanceList":[{"distributionType":"1","balance":0.4,"success":true,"accountBalance":"0.4"},{"distributionType":"2","balance":0,"success":false,"accountBalance":"0"}]},"success":true}`)
		case request.URL.Path == "/v2/account/asset-valuation" && request.Method == http.MethodGet:
			if query.Get("accountType") != "spot" || query.Get("valuationCurrency") != "USD" ||
				query.Get("subUid") != "123456" {
				extBadRequest(writer)
				return
			}
			_, _ = io.WriteString(writer, `{"code":200,"data":{"balance":"34.75","timestamp":1594901254363},"ok":true}`)
		case request.URL.Path == "/v1/account/history" && request.Method == http.MethodGet:
			if query.Get("account-id") != "5260185" || query.Get("currency") != "usdt" ||
				query.Get("transact-types") != "trade,transfer" || query.Get("sort") != "desc" ||
				query.Get("size") != "100" || query.Get("from-id") != "123" ||
				query.Get("start-time") != "1787625245000" || query.Get("end-time") != "1787627045000" {
				extBadRequest(writer)
				return
			}
			_, _ = io.WriteString(writer, `{"status":"ok","data":[{"account-id":5260185,"currency":"usdt","record-id":89375706443,"transact-amt":"-0.5","transact-type":"trade","avail-balance":"10.5","acct-balance":"10.5","transact-time":1629882096557}],"next-id":89375706442}`)
		case request.URL.Path == "/v2/account/ledger" && request.Method == http.MethodGet:
			if query.Get("accountId") != "5260185" || query.Get("currency") != "usdt" ||
				query.Get("limit") != "10" || query.Get("sort") != "asc" ||
				query.Get("startTime") != "1787625245000" || query.Get("endTime") != "1787627045000" {
				extBadRequest(writer)
				return
			}
			_, _ = io.WriteString(writer, `{"code":200,"message":"success","data":[{"accountId":5260185,"currency":"usdt","transactAmt":1.0,"transactType":"transfer","transferType":"margin-transfer-out","transactId":0,"transactTime":1629882331066,"transferer":28483123,"transferee":13496526}],"nextId":1624316679}`)
		case request.URL.Path == "/v1/account/transfer" && request.Method == http.MethodPost:
			body, ok := extReadJSONBody(request)
			if !ok || body["from-user"] != float64(28390) || body["from-account-type"] != "spot" ||
				body["from-account"] != float64(1) || body["to-user"] != float64(28390) ||
				body["to-account-type"] != "margin" || body["to-account"] != float64(2) ||
				body["currency"] != "usdt" || body["amount"] != "10" {
				extBadRequest(writer)
				return
			}
			_, _ = io.WriteString(writer, `{"status":"ok","data":{"transact-id":220521190,"transact-time":1590662591832}}`)
		case request.URL.Path == "/v1/futures/transfer" && request.Method == http.MethodPost:
			body, ok := extReadJSONBody(request)
			if !ok || body["currency"] != "btc" || body["amount"] != 0.001 ||
				body["type"] != "pro-to-futures" {
				extBadRequest(writer)
				return
			}
			_, _ = io.WriteString(writer, `{"status":"ok","data":12345}`)
		case request.URL.Path == "/v2/point/account" && request.Method == http.MethodGet:
			if query.Get("subUid") != "123456" {
				extBadRequest(writer)
				return
			}
			_, _ = io.WriteString(writer, `{"code":200,"data":{"accountId":"14403739","groupIds":[{"groupId":0,"expiryDate":0,"remainAmt":"0.5"},{"groupId":26,"expiryDate":1594396800000,"remainAmt":"1"}],"acctBalance":"1.5","accountStatus":"working"},"success":true}`)
		case request.URL.Path == "/v2/point/transfer" && request.Method == http.MethodPost:
			body, ok := extReadJSONBody(request)
			if !ok || body["fromUid"] != "125753978" || body["toUid"] != "128654510" ||
				body["groupId"] != float64(0) || body["amount"] != "0.1" {
				extBadRequest(writer)
				return
			}
			_, _ = io.WriteString(writer, `{"code":200,"data":{"transactId":"74","transactTime":1594370136458},"success":true}`)
		case request.URL.Path == "/v2/user/uid" && request.Method == http.MethodGet:
			_, _ = io.WriteString(writer, `{"code":200,"data":63628520,"ok":true}`)
		case request.URL.Path == "/v2/user/api-key" && request.Method == http.MethodGet:
			if query.Get("uid") != "63628520" || query.Get("accessKey") != "ak-1" {
				extBadRequest(writer)
				return
			}
			_, _ = io.WriteString(writer, `{"code":200,"message":"success","data":[{"accessKey":"ak-1","status":"normal","note":"bot","permission":"readOnly,trade","ipAddresses":"1.1.1.1","validDays":-1,"createTime":1591348751000,"updateTime":1591348751000}],"ok":true}`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client, _ := newPrivateTestClient(
		t, server.URL, &directSender{}, &recordingProvider{},
		[]transport.EgressRouteID{"route-a"},
		[]credential.Permission{credential.PermissionRead, credential.PermissionTransfer}, fixedNow,
	)
	ctx := context.Background()

	valuation, err := client.AccountValuation(ctx, AccountValuationRequest{AccountType: "1", ValuationCurrency: "BTC"})
	if err != nil || valuation.TotalBalance != "0.5" || !valuation.UpdatedToday ||
		valuation.UpdatedAt != 1_629_916_724_000 || len(valuation.Balances) != 2 ||
		valuation.Balances[0].DistributionType != "1" || valuation.Balances[1].Success ||
		len(valuation.Balances[0].Raw) == 0 || len(valuation.Raw) == 0 {
		t.Fatalf("AccountValuation() = %+v, error = %v", valuation, err)
	}
	asset, err := client.AssetValuation(ctx, AssetValuationRequest{
		AccountType: AssetAccountSpot, ValuationCurrency: "USD", SubUID: "123456",
	})
	if err != nil || asset.Balance != "34.75" || asset.Timestamp != 1_594_901_254_363 {
		t.Fatalf("AssetValuation() = %+v, error = %v", asset, err)
	}
	history, err := client.AccountHistory(ctx, AccountHistoryRequest{
		AccountID: "5260185", Currency: "usdt",
		TransactTypes: []TransactType{TransactTypeTrade, TransactTypeTransfer},
		Start:         &start, End: &end, Sort: SortDescending, Size: 100, FromID: "123",
	})
	if err != nil || len(history.Records) != 1 || history.NextID != "89375706442" ||
		history.Records[0].TransactAmount != "-0.5" || history.Records[0].RecordID != "89375706443" {
		t.Fatalf("AccountHistory() = %+v, error = %v", history, err)
	}
	ledger, err := client.AccountLedger(ctx, AccountLedgerRequest{
		AccountID: "5260185", Currency: "usdt", Start: &start, End: &end,
		Sort: SortAscending, Limit: 10,
	})
	if err != nil || len(ledger.Records) != 1 || ledger.NextID != "1624316679" ||
		ledger.Records[0].TransferType != "margin-transfer-out" ||
		ledger.Records[0].Transferee != "13496526" {
		t.Fatalf("AccountLedger() = %+v, error = %v", ledger, err)
	}
	transfer, err := client.AccountTransfer(ctx, AccountTransferRequest{
		FromUserID: "28390", FromAccountType: TransferAccountSpot, FromAccountID: "1",
		ToUserID: "28390", ToAccountType: TransferAccountMargin, ToAccountID: "2",
		Currency: "usdt", Amount: "10",
	})
	if err != nil || transfer.TransactID != "220521190" || transfer.TransactTime != 1_590_662_591_832 {
		t.Fatalf("AccountTransfer() = %+v, error = %v", transfer, err)
	}
	futures, err := client.FuturesTransfer(ctx, FuturesTransferRequest{
		Currency: "btc", Amount: "0.001", Type: FuturesTransferToFutures,
	})
	if err != nil || futures.TransferID != "12345" {
		t.Fatalf("FuturesTransfer() = %+v, error = %v", futures, err)
	}
	points, err := client.PointAccount(ctx, PointAccountRequest{SubUID: "123456"})
	if err != nil || points.AccountID != "14403739" || points.Balance != "1.5" ||
		len(points.Groups) != 2 || points.Groups[1].GroupID != "26" ||
		points.Groups[1].ExpiryDate != 1_594_396_800_000 {
		t.Fatalf("PointAccount() = %+v, error = %v", points, err)
	}
	pointTransfer, err := client.PointTransfer(ctx, PointTransferRequest{
		FromUID: "125753978", ToUID: "128654510", Amount: "0.1",
	})
	if err != nil || pointTransfer.TransactID != "74" {
		t.Fatalf("PointTransfer() = %+v, error = %v", pointTransfer, err)
	}
	uid, err := client.UID(ctx)
	if err != nil || uid.UID != "63628520" {
		t.Fatalf("UID() = %+v, error = %v", uid, err)
	}
	keys, err := client.APIKeys(ctx, APIKeysRequest{UID: "63628520", AccessKey: "ak-1"})
	if err != nil || len(keys) != 1 || keys[0].AccessKey != "ak-1" || keys[0].ValidDays != -1 ||
		len(keys[0].Raw) == 0 {
		t.Fatalf("APIKeys() = %+v, error = %v", keys, err)
	}
}

func TestAccountExtensionsRequireTransferPermission(t *testing.T) {
	t.Parallel()
	fixedNow := time.Date(2026, time.August, 25, 3, 4, 5, 0, time.UTC)
	sender := &directSender{}
	provider := &recordingProvider{}
	client, _ := newPrivateTestClient(
		t, "http://127.0.0.1", sender, provider,
		[]transport.EgressRouteID{"route-a"},
		[]credential.Permission{credential.PermissionRead, credential.PermissionTrade}, fixedNow,
	)
	ctx := context.Background()
	_, accountErr := client.AccountTransfer(ctx, AccountTransferRequest{
		FromUserID: "1", FromAccountType: TransferAccountSpot, FromAccountID: "1",
		ToUserID: "1", ToAccountType: TransferAccountMargin, ToAccountID: "2",
		Currency: "usdt", Amount: "1",
	})
	_, futuresErr := client.FuturesTransfer(ctx, FuturesTransferRequest{
		Currency: "btc", Amount: "1", Type: FuturesTransferToSpot,
	})
	_, pointErr := client.PointTransfer(ctx, PointTransferRequest{FromUID: "1", ToUID: "2", Amount: "1"})
	for name, err := range map[string]error{"account": accountErr, "futures": futuresErr, "point": pointErr} {
		if !errors.Is(err, trade.ErrAuthorization) {
			t.Fatalf("%s transfer error = %v, want authorization", name, err)
		}
	}
}

func TestAccountExtensionValidation(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, time.August, 25, 0, 0, 0, 0, time.UTC)
	tooLate := start.Add(2 * time.Hour)
	invalid := map[string]error{
		"valuation currency":   (AccountValuationRequest{ValuationCurrency: "btc"}).validate(),
		"asset account type":   (AssetValuationRequest{AccountType: "futures"}).validate(),
		"asset sub uid":        (AssetValuationRequest{AccountType: AssetAccountSpot, SubUID: "x"}).validate(),
		"history account":      (AccountHistoryRequest{AccountID: "x"}).validate(),
		"history type":         (AccountHistoryRequest{AccountID: "1", TransactTypes: []TransactType{"gift"}}).validate(),
		"history window":       (AccountHistoryRequest{AccountID: "1", Start: &start, End: &tooLate}).validate(),
		"history size":         (AccountHistoryRequest{AccountID: "1", Size: 501}).validate(),
		"ledger limit":         (AccountLedgerRequest{AccountID: "1", Limit: 501}).validate(),
		"futures type":         (FuturesTransferRequest{Currency: "btc", Amount: "1", Type: "x"}).validate(),
		"futures amount":       (FuturesTransferRequest{Currency: "btc", Amount: "01", Type: FuturesTransferToSpot}).validate(),
		"point same user":      (PointTransferRequest{FromUID: "1", ToUID: "1", Amount: "1"}).validate(),
		"point negative group": (PointTransferRequest{FromUID: "1", ToUID: "2", GroupID: -1, Amount: "1"}).validate(),
		"api key uid":          (APIKeysRequest{UID: "x"}).validate(),
	}
	for name, err := range invalid {
		if !errors.Is(err, trade.ErrValidation) {
			t.Fatalf("%s validation error = %v", name, err)
		}
	}
	if _, err := (AccountTransferRequest{
		FromUserID: "1", FromAccountType: TransferAccountSpot, FromAccountID: "1",
		ToUserID: "1", ToAccountType: TransferAccountSpot, ToAccountID: "1",
		Currency: "usdt", Amount: "1",
	}).wire(); !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("same-account transfer error = %v", err)
	}
}
