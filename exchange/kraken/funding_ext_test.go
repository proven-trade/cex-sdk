package kraken

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
)

func TestDecimalStringAcceptsScalars(t *testing.T) {
	t.Parallel()

	var values struct {
		Text    DecimalString `json:"text"`
		Number  DecimalString `json:"number"`
		Exact   DecimalString `json:"exact"`
		No      DecimalString `json:"no"`
		Yes     DecimalString `json:"yes"`
		Null    DecimalString `json:"null"`
		Missing DecimalString `json:"missing"`
	}
	if err := json.Unmarshal(
		[]byte(`{"text":"0.0100","number":12,"exact":0.12345678901234567890,"no":false,"yes":true,"null":null}`),
		&values,
	); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if values.Text != "0.0100" || values.Number != "12" || values.Exact != "0.12345678901234567890" ||
		values.No != "" || values.Yes != "true" || values.Null != "" || values.Missing != "" {
		t.Fatalf("DecimalString values = %+v", values)
	}
	if err := json.Unmarshal([]byte(`{"text":[1]}`), &values); err == nil {
		t.Fatal("Unmarshal(array) error = nil, want error")
	}
}

func TestDepositMethodsDecodesLimitFalseAndNumericFees(t *testing.T) {
	t.Parallel()

	server := newExtServer(t, map[string]string{
		privatePrefix + "DepositMethods": `{"error":[],"result":[{"method":"Bitcoin","limit":false,"fee":"0.0000000000","gen-address":true,"minimum":"0.00010000"},{"method":"Bitcoin Lightning","limit":"100.5","fee":0,"fee-percentage":"0.1","address-setup-fee":"true","minimum":"0.00010000"}]}`,
	})
	client, limiter := newExtClient(t, server.URL, credential.PermissionRead)
	methods, err := client.DepositMethods(context.Background(), DepositMethodsRequest{
		Asset: "XBT", AssetClass: AssetClassCurrency, RebaseMultiplier: RebaseMultiplierBase,
	})
	if err != nil {
		t.Fatalf("DepositMethods() error = %v", err)
	}
	if len(methods) != 2 || methods[0].Method != "Bitcoin" || methods[0].Limit != "" ||
		!methods[0].GenAddress || methods[0].Minimum != "0.00010000" || methods[1].Limit != "100.5" ||
		methods[1].Fee != "0" || methods[1].FeePercentage != "0.1" || methods[1].AddressSetupFee != "true" ||
		len(methods[0].Raw) == 0 {
		t.Fatalf("DepositMethods() = %+v", methods)
	}
	request := server.last(t)
	form := request.form(t)
	if request.Method != http.MethodPost || request.Path != privatePrefix+"DepositMethods" ||
		request.ContentType != formContentType || form.Get("asset") != "XBT" ||
		form.Get("aclass") != "currency" || form.Get("rebase_multiplier") != "base" || form.Get("nonce") == "" {
		t.Fatalf("DepositMethods request = %+v form = %v", request, form)
	}
	snapshot, err := limiter.Snapshot("kraken:account:kraken-main:private-counter")
	if err != nil || snapshot.Used != 1 {
		t.Fatalf("private limiter snapshot = %+v, error = %v", snapshot, err)
	}
}

func TestDepositAddressesReadsWithReadPermissionAndCreatesWithTransferPermission(t *testing.T) {
	t.Parallel()

	server := newExtServer(t, map[string]string{
		privatePrefix + "DepositAddresses": `{"error":[],"result":[{"address":"2N9fRkx5JTWXWHmXzZtvhQsufvoYRMq9ExV","expiretm":"0","new":true},{"address":"rLHzPsX3oXdzU2qP17kHCH2G4csZv1rAJh","expiretm":"0","new":true,"tag":"1361101127"},{"address":"krakenkraken","expiretm":"0","memo":"4150096490"}]}`,
	})
	readClient, _ := newExtClient(t, server.URL, credential.PermissionRead)
	addresses, err := readClient.DepositAddresses(context.Background(), DepositAddressesRequest{
		Asset: "XBT", Method: "Bitcoin",
	})
	if err != nil {
		t.Fatalf("DepositAddresses(read) error = %v", err)
	}
	if len(addresses) != 3 || addresses[0].Address != "2N9fRkx5JTWXWHmXzZtvhQsufvoYRMq9ExV" || !addresses[0].New ||
		addresses[1].Tag != "1361101127" || addresses[2].Memo != "4150096490" || addresses[2].ExpireTime != "0" ||
		len(addresses[2].Raw) == 0 {
		t.Fatalf("DepositAddresses() = %+v", addresses)
	}
	if form := server.last(t).form(t); form.Get("asset") != "XBT" || form.Get("method") != "Bitcoin" ||
		form.Get("new") != "" || form.Get("aclass") != "" {
		t.Fatalf("DepositAddresses(read) form = %v", form)
	}

	if _, err := readClient.DepositAddresses(context.Background(), DepositAddressesRequest{
		Asset: "XBT", Method: "Bitcoin", New: true,
	}); !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("DepositAddresses(new, read-only) error = %v, want authorization", err)
	}

	transferClient, _ := newExtClient(t, server.URL, credential.PermissionTransfer)
	if _, err := transferClient.DepositAddresses(context.Background(), DepositAddressesRequest{
		Asset: "XBT", Method: "Bitcoin Lightning", New: true, Amount: "0.001",
	}); err != nil {
		t.Fatalf("DepositAddresses(new) error = %v", err)
	}
	if form := server.last(t).form(t); form.Get("new") != "true" || form.Get("amount") != "0.001" ||
		form.Get("method") != "Bitcoin Lightning" {
		t.Fatalf("DepositAddresses(new) form = %v", form)
	}
	if _, err := transferClient.DepositAddresses(context.Background(), DepositAddressesRequest{
		Asset: "XBT", Method: "Bitcoin",
	}); !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("DepositAddresses(read, transfer-only) error = %v, want authorization", err)
	}
}

func TestDepositAddressesClassifiesMalformedBodyByOperationKind(t *testing.T) {
	t.Parallel()

	server := newExtServer(t, map[string]string{
		privatePrefix + "DepositAddresses": `{"error":[],"result":{"address":"not-a-list"}}`,
	})
	transferClient, _ := newExtClient(t, server.URL, credential.PermissionTransfer)
	_, err := transferClient.DepositAddresses(context.Background(), DepositAddressesRequest{
		Asset: "XBT", Method: "Bitcoin", New: true,
	})
	if !errors.Is(err, trade.ErrUnknownExecutionState) {
		t.Fatalf("DepositAddresses(new, malformed) error = %v, want unknown execution state", err)
	}
	readClient, _ := newExtClient(t, server.URL, credential.PermissionRead)
	_, err = readClient.DepositAddresses(context.Background(), DepositAddressesRequest{
		Asset: "XBT", Method: "Bitcoin",
	})
	if err == nil || errors.Is(err, trade.ErrUnknownExecutionState) {
		t.Fatalf("DepositAddresses(read, malformed) error = %v, want non-unknown error", err)
	}
}

func TestDepositStatusDecodesPaginatedAndPlainResults(t *testing.T) {
	t.Parallel()

	server := newExtServer(t, map[string]string{
		privatePrefix + "DepositStatus": `{"error":[],"result":{"deposit":[{"method":"Bitcoin","aclass":"currency","asset":"XXBT","refid":"FTQcuak-V6Za8qrWnhzTx67yYHz8Tg","txid":"6544b41b607d","info":"2Myd4eaAW96ojk38A2uDK4FbioCayvkEgVq","amount":"0.78125000","fee":"0.0000000000","time":1688992722,"status":"Success","status-prop":"return"},{"method":"Ether (Hex)","aclass":"currency","asset":"XETH","refid":"FTQcuak-V6Za8qrPnhsTx47yYLz8Tg","txid":"0x339c","info":"0xca21","amount":"0.1383862742","time":1688992723,"status":"Settled","status-prop":"onhold","originators":["0x70b6","0x5b32"]}],"next_cursor":"cursor-2"}}`,
	})
	client, _ := newExtClient(t, server.URL)
	start := time.Unix(1688000000, 0)
	end := time.Unix(1689000000, 0)
	page, err := client.DepositStatus(context.Background(), FundingStatusRequest{
		Asset: "XBT", Method: "Bitcoin", Start: &start, End: &end, Paginated: true, Limit: 2,
	})
	if err != nil {
		t.Fatalf("DepositStatus() error = %v", err)
	}
	if len(page.Deposits) != 2 || page.NextCursor != "cursor-2" || page.Deposits[0].ReferenceID != "FTQcuak-V6Za8qrWnhzTx67yYHz8Tg" ||
		page.Deposits[0].Amount != "0.78125000" || page.Deposits[0].Time != 1688992722 ||
		page.Deposits[0].Status != "Success" || page.Deposits[0].StatusProperty != "return" ||
		page.Deposits[1].Fee != "" || len(page.Deposits[1].Originators) != 2 || len(page.Deposits[0].Raw) == 0 ||
		len(page.Raw) == 0 {
		t.Fatalf("DepositStatus() = %+v", page)
	}
	form := server.last(t).form(t)
	if form.Get("asset") != "XBT" || form.Get("method") != "Bitcoin" || form.Get("start") != "1688000000" ||
		form.Get("end") != "1689000000" || form.Get("cursor") != "true" || form.Get("limit") != "2" {
		t.Fatalf("DepositStatus form = %v", form)
	}

	plainServer := newExtServer(t, map[string]string{
		privatePrefix + "DepositStatus": `{"error":[],"result":[{"method":"Bitcoin","asset":"XXBT","refid":"R1","amount":"1","time":1,"status":"Pending"}]}`,
	})
	plainClient, _ := newExtClient(t, plainServer.URL)
	plain, err := plainClient.DepositStatus(context.Background(), FundingStatusRequest{Cursor: "cursor-2"})
	if err != nil {
		t.Fatalf("DepositStatus(plain) error = %v", err)
	}
	if len(plain.Deposits) != 1 || plain.NextCursor != "" || plain.Deposits[0].Status != "Pending" {
		t.Fatalf("DepositStatus(plain) = %+v", plain)
	}
	if form := plainServer.last(t).form(t); form.Get("cursor") != "cursor-2" {
		t.Fatalf("DepositStatus(plain) form = %v", form)
	}
}

func TestWithdrawMethodsDecodesFeesAndLimits(t *testing.T) {
	t.Parallel()

	server := newExtServer(t, map[string]string{
		privatePrefix + "WithdrawMethods": `{"error":[],"result":[{"asset":"XXBT","method":"Bitcoin","method_id":"12fca2ad-edae-4d8c-acbb-4a424c1fbdeb","network":"Bitcoin","network_id":"ee9d686d-aeb6-4e61-9d83-448e3a7511f3","minimum":"0.0004","fee":{"aclass":"currency","asset":"XXBT","fee":"0.00001500"},"limits":[{"description":"Daily","limit_type":"amount","limits":{"86400":{"maximum":"10","remaining":"9.5","used":"0.5"}}}]},{"asset":"XXBT","method":"Bitcoin Lightning","method_id":"1ac8da36","network":"Lightning","minimum":0.00001,"fee":{"aclass":"currency","asset":"XXBT","fee":"0","fee_percentage":"0.1"}}]}`,
	})
	client, _ := newExtClient(t, server.URL)
	methods, err := client.WithdrawMethods(context.Background(), WithdrawMethodsRequest{
		Asset: "XBT", Network: "Bitcoin",
	})
	if err != nil {
		t.Fatalf("WithdrawMethods() error = %v", err)
	}
	if len(methods) != 2 || methods[0].MethodID != "12fca2ad-edae-4d8c-acbb-4a424c1fbdeb" ||
		methods[0].NetworkID != "ee9d686d-aeb6-4e61-9d83-448e3a7511f3" || methods[0].Fee.Fee != "0.00001500" ||
		len(methods[0].Limits) != 1 || methods[0].Limits[0].Windows["86400"].Remaining != "9.5" ||
		methods[1].Minimum != "0.00001" || methods[1].Fee.FeePercentage != "0.1" || len(methods[1].Raw) == 0 {
		t.Fatalf("WithdrawMethods() = %+v", methods)
	}
	if form := server.last(t).form(t); form.Get("asset") != "XBT" || form.Get("network") != "Bitcoin" {
		t.Fatalf("WithdrawMethods form = %v", form)
	}
}

func TestWithdrawAddressesFiltersByVerification(t *testing.T) {
	t.Parallel()

	server := newExtServer(t, map[string]string{
		privatePrefix + "WithdrawAddresses": `{"error":[],"result":[{"address":"bc1qxdsh4sdd29h6ldehz0se5c61asq8cgwyjf2y3z","asset":"XBT","method":"Bitcoin","key":"btc-wallet-1","verified":true},{"address":"rLHzPsX3oXdzU2qP17kHCH2G4csZv1rAJh","asset":"XRP","method":"Ripple XRP","key":"xrp","tag":"1361101127","verified":false}]}`,
	})
	client, _ := newExtClient(t, server.URL)
	unverified := false
	addresses, err := client.WithdrawAddresses(context.Background(), WithdrawAddressesRequest{
		Asset: "XRP", Method: "Ripple XRP", Key: "xrp", Verified: &unverified,
	})
	if err != nil {
		t.Fatalf("WithdrawAddresses() error = %v", err)
	}
	if len(addresses) != 2 || addresses[0].Key != "btc-wallet-1" || !addresses[0].Verified ||
		addresses[1].Tag != "1361101127" || addresses[1].Verified || len(addresses[1].Raw) == 0 {
		t.Fatalf("WithdrawAddresses() = %+v", addresses)
	}
	form := server.last(t).form(t)
	if form.Get("asset") != "XRP" || form.Get("method") != "Ripple XRP" || form.Get("key") != "xrp" ||
		form.Get("verified") != "false" {
		t.Fatalf("WithdrawAddresses form = %v", form)
	}

	verified := true
	if _, err := client.WithdrawAddresses(context.Background(), WithdrawAddressesRequest{Verified: &verified}); err != nil {
		t.Fatalf("WithdrawAddresses(verified) error = %v", err)
	}
	if form := server.last(t).form(t); form.Get("verified") != "true" || form.Get("asset") != "" {
		t.Fatalf("WithdrawAddresses(verified) form = %v", form)
	}
	if _, err := client.WithdrawAddresses(context.Background(), WithdrawAddressesRequest{}); err != nil {
		t.Fatalf("WithdrawAddresses(all) error = %v", err)
	}
	if form := server.last(t).form(t); form.Has("verified") {
		t.Fatalf("WithdrawAddresses(all) form = %v", form)
	}
}

func TestWithdrawInfoDecodesFeeEstimate(t *testing.T) {
	t.Parallel()

	server := newExtServer(t, map[string]string{
		privatePrefix + "WithdrawInfo": `{"error":[],"result":{"method":"Bitcoin","limit":"332.00956139","amount":"0.72485000","fee":"0.00020000"}}`,
	})
	client, _ := newExtClient(t, server.URL)
	info, err := client.WithdrawInfo(context.Background(), WithdrawInfoRequest{
		Asset: "XBT", Key: "btc_testnet", Amount: "0.725",
	})
	if err != nil {
		t.Fatalf("WithdrawInfo() error = %v", err)
	}
	if info.Method != "Bitcoin" || info.Limit != "332.00956139" || info.Amount != "0.72485000" ||
		info.Fee != "0.00020000" || len(info.Raw) == 0 {
		t.Fatalf("WithdrawInfo() = %+v", info)
	}
	if form := server.last(t).form(t); form.Get("asset") != "XBT" || form.Get("key") != "btc_testnet" ||
		form.Get("amount") != "0.725" {
		t.Fatalf("WithdrawInfo form = %v", form)
	}
}

func TestWithdrawRequiresWithdrawPermissionAndDecodesReference(t *testing.T) {
	t.Parallel()

	server := newExtServer(t, map[string]string{
		privatePrefix + "Withdraw": `{"error":[],"result":{"refid":"FTQcuak-V6Za8qrWnhzTx67yYHz8Tg"}}`,
	})
	request := WithdrawRequest{
		Asset: "XBT", AssetClass: AssetClassCurrency, Key: "btc_2709", Amount: "0.725",
		Address: "bc1kar0ssrr7xf3vy5l6d3lydnwkre5og2zz3f5ldq", MaxFee: "0.001",
	}
	noWithdrawClient, _ := newExtClient(t, server.URL, credential.PermissionRead, credential.PermissionTransfer)
	if _, err := noWithdrawClient.Withdraw(context.Background(), request); !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("Withdraw() error = %v, want authorization", err)
	}
	if server.count() != 0 {
		t.Fatalf("server requests = %d, want 0", server.count())
	}

	client, _ := newExtClient(t, server.URL, credential.PermissionWithdraw)
	reference, err := client.Withdraw(context.Background(), request)
	if err != nil {
		t.Fatalf("Withdraw() error = %v", err)
	}
	if reference.ReferenceID != "FTQcuak-V6Za8qrWnhzTx67yYHz8Tg" || len(reference.Raw) == 0 {
		t.Fatalf("Withdraw() = %+v", reference)
	}
	recorded := server.last(t)
	form := recorded.form(t)
	if recorded.Method != http.MethodPost || recorded.Path != privatePrefix+"Withdraw" ||
		form.Get("asset") != "XBT" || form.Get("aclass") != "currency" || form.Get("key") != "btc_2709" ||
		form.Get("amount") != "0.725" || form.Get("address") != "bc1kar0ssrr7xf3vy5l6d3lydnwkre5og2zz3f5ldq" ||
		form.Get("max_fee") != "0.001" || form.Get("nonce") == "" {
		t.Fatalf("Withdraw request = %+v form = %v", recorded, form)
	}

	emptyServer := newExtServer(t, map[string]string{
		privatePrefix + "Withdraw": `{"error":[],"result":{}}`,
	})
	emptyClient, _ := newExtClient(t, emptyServer.URL, credential.PermissionWithdraw)
	if _, err := emptyClient.Withdraw(context.Background(), request); !errors.Is(err, trade.ErrUnknownExecutionState) {
		t.Fatalf("Withdraw(empty) error = %v, want unknown execution state", err)
	}
}

func TestWithdrawStatusDecodesRecordsAndCursor(t *testing.T) {
	t.Parallel()

	server := newExtServer(t, map[string]string{
		privatePrefix + "WithdrawStatus": `{"error":[],"result":[{"method":"Bitcoin","network":"Bitcoin","aclass":"currency","asset":"XXBT","refid":"FTQcuak-V6Za8qrWnhzTx67yYHz8Tg","txid":"29323ce2","info":"bc1qm32pq","amount":"0.72485000","fee":"0.00020000","time":1688014586,"status":"Pending","key":"btc-wallet-1"},{"method":"Bitcoin","aclass":"currency","asset":"XXBT","refid":"FTQcuak-V6Za8qrPnhsTx47yYLz8Tg","amount":"0.72485000","fee":0.0002,"time":1688015423,"status":"Failure","status-prop":"canceled","key":"btc-wallet-2"}]}`,
	})
	client, _ := newExtClient(t, server.URL)
	page, err := client.WithdrawStatus(context.Background(), FundingStatusRequest{Asset: "XBT", Method: "Bitcoin"})
	if err != nil {
		t.Fatalf("WithdrawStatus() error = %v", err)
	}
	if len(page.Withdrawals) != 2 || page.NextCursor != "" || page.Withdrawals[0].Network != "Bitcoin" ||
		page.Withdrawals[0].Key != "btc-wallet-1" || page.Withdrawals[0].Status != "Pending" ||
		page.Withdrawals[1].StatusProperty != "canceled" || page.Withdrawals[1].Fee != "0.0002" ||
		page.Withdrawals[1].Time != 1688015423 || len(page.Withdrawals[1].Raw) == 0 || len(page.Raw) == 0 {
		t.Fatalf("WithdrawStatus() = %+v", page)
	}
	form := server.last(t).form(t)
	if form.Get("asset") != "XBT" || form.Get("method") != "Bitcoin" || form.Has("cursor") || form.Has("limit") {
		t.Fatalf("WithdrawStatus form = %v", form)
	}

	pagedServer := newExtServer(t, map[string]string{
		privatePrefix + "WithdrawStatus": `{"error":[],"result":{"withdrawals":[{"refid":"R1","status":"Success","time":5}],"next_cursor":"next-page"}}`,
	})
	pagedClient, _ := newExtClient(t, pagedServer.URL)
	paged, err := pagedClient.WithdrawStatus(context.Background(), FundingStatusRequest{Paginated: true})
	if err != nil {
		t.Fatalf("WithdrawStatus(paged) error = %v", err)
	}
	if len(paged.Withdrawals) != 1 || paged.Withdrawals[0].ReferenceID != "R1" || paged.NextCursor != "next-page" {
		t.Fatalf("WithdrawStatus(paged) = %+v", paged)
	}
	if form := pagedServer.last(t).form(t); form.Get("cursor") != "true" {
		t.Fatalf("WithdrawStatus(paged) form = %v", form)
	}
}

func TestWithdrawCancelReportsDefiniteAnswer(t *testing.T) {
	t.Parallel()

	server := newExtServer(t, map[string]string{
		privatePrefix + "WithdrawCancel": `{"error":[],"result":true}`,
	})
	client, _ := newExtClient(t, server.URL, credential.PermissionWithdraw)
	canceled, err := client.WithdrawCancel(context.Background(), WithdrawCancelRequest{
		Asset: "XBT", ReferenceID: "FTQcuak-V6Za8qrWnhzTx67yYHz8Tg",
	})
	if err != nil {
		t.Fatalf("WithdrawCancel() error = %v", err)
	}
	if !canceled.Canceled || len(canceled.Raw) == 0 {
		t.Fatalf("WithdrawCancel() = %+v", canceled)
	}
	if form := server.last(t).form(t); form.Get("asset") != "XBT" ||
		form.Get("refid") != "FTQcuak-V6Za8qrWnhzTx67yYHz8Tg" {
		t.Fatalf("WithdrawCancel form = %v", form)
	}

	declinedServer := newExtServer(t, map[string]string{
		privatePrefix + "WithdrawCancel": `{"error":[],"result":false}`,
	})
	declinedClient, _ := newExtClient(t, declinedServer.URL, credential.PermissionWithdraw)
	declined, err := declinedClient.WithdrawCancel(context.Background(), WithdrawCancelRequest{
		Asset: "XBT", ReferenceID: "FTQcuak-V6Za8qrWnhzTx67yYHz8Tg",
	})
	if err != nil || declined.Canceled {
		t.Fatalf("WithdrawCancel(declined) = %+v, error = %v", declined, err)
	}

	tradeClient, _ := newExtClient(t, server.URL, credential.PermissionTrade, credential.PermissionRead)
	if _, err := tradeClient.WithdrawCancel(context.Background(), WithdrawCancelRequest{
		Asset: "XBT", ReferenceID: "R1",
	}); !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("WithdrawCancel() error = %v, want authorization", err)
	}
}

func TestWalletTransferSendsFixedWalletsWithTransferPermission(t *testing.T) {
	t.Parallel()

	server := newExtServer(t, map[string]string{
		privatePrefix + "WalletTransfer": `{"error":[],"result":{"refid":"FTQcuak-V6Za8qrWnhzTx67yYHz8Tg"}}`,
	})
	client, _ := newExtClient(t, server.URL, credential.PermissionTransfer)
	transfer, err := client.WalletTransfer(context.Background(), WalletTransferRequest{Asset: "XBT", Amount: "2.54"})
	if err != nil {
		t.Fatalf("WalletTransfer() error = %v", err)
	}
	if transfer.ReferenceID != "FTQcuak-V6Za8qrWnhzTx67yYHz8Tg" || len(transfer.Raw) == 0 {
		t.Fatalf("WalletTransfer() = %+v", transfer)
	}
	form := server.last(t).form(t)
	if form.Get("asset") != "XBT" || form.Get("from") != "Spot Wallet" || form.Get("to") != "Futures Wallet" ||
		form.Get("amount") != "2.54" {
		t.Fatalf("WalletTransfer form = %v", form)
	}

	withdrawClient, _ := newExtClient(t, server.URL, credential.PermissionWithdraw)
	if _, err := withdrawClient.WalletTransfer(
		context.Background(), WalletTransferRequest{Asset: "XBT", Amount: "1"},
	); !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("WalletTransfer() error = %v, want authorization", err)
	}
}

func TestSubaccountExtensionsRequireTransferPermission(t *testing.T) {
	t.Parallel()

	server := newExtServer(t, map[string]string{
		privatePrefix + "CreateSubaccount": `{"error":[],"result":true}`,
		privatePrefix + "AccountTransfer":  `{"error":[],"result":{"transfer_id":"TOH3AS2-LPCWR8-JDQGEU","status":"complete"}}`,
	})
	client, _ := newExtClient(t, server.URL, credential.PermissionTransfer)

	created, err := client.CreateSubaccount(context.Background(), CreateSubaccountRequest{
		Username: "abc123", Email: "abc123@example.com",
	})
	if err != nil {
		t.Fatalf("CreateSubaccount() error = %v", err)
	}
	if !created.Created || len(created.Raw) == 0 {
		t.Fatalf("CreateSubaccount() = %+v", created)
	}
	if form := server.last(t).form(t); form.Get("username") != "abc123" || form.Get("email") != "abc123@example.com" {
		t.Fatalf("CreateSubaccount form = %v", form)
	}

	transfer, err := client.AccountTransfer(context.Background(), AccountTransferRequest{
		Asset: "XBT", AssetClass: AssetClassCurrency, Amount: "2.54",
		From: "ABCD 1234 EFGH 5678", To: "IJKL 0987 MNOP 6543",
	})
	if err != nil {
		t.Fatalf("AccountTransfer() error = %v", err)
	}
	if transfer.TransferID != "TOH3AS2-LPCWR8-JDQGEU" || transfer.Status != "complete" || len(transfer.Raw) == 0 {
		t.Fatalf("AccountTransfer() = %+v", transfer)
	}
	form := server.last(t).form(t)
	if form.Get("asset") != "XBT" || form.Get("asset_class") != "currency" || form.Get("amount") != "2.54" ||
		form.Get("from") != "ABCD 1234 EFGH 5678" || form.Get("to") != "IJKL 0987 MNOP 6543" {
		t.Fatalf("AccountTransfer form = %v", form)
	}

	readClient, _ := newExtClient(t, server.URL, credential.PermissionRead, credential.PermissionWithdraw)
	requests := server.count()
	if _, err := readClient.CreateSubaccount(context.Background(), CreateSubaccountRequest{
		Username: "abc123", Email: "abc123@example.com",
	}); !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("CreateSubaccount() error = %v, want authorization", err)
	}
	if _, err := readClient.AccountTransfer(context.Background(), AccountTransferRequest{
		Asset: "XBT", Amount: "1", From: "A", To: "B",
	}); !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("AccountTransfer() error = %v, want authorization", err)
	}
	if server.count() != requests {
		t.Fatalf("server requests = %d, want %d", server.count(), requests)
	}
}

func TestSubaccountMutationsRejectUnacknowledgedResults(t *testing.T) {
	t.Parallel()

	server := newExtServer(t, map[string]string{
		privatePrefix + "CreateSubaccount": `{"error":[],"result":false}`,
		privatePrefix + "AccountTransfer":  `{"error":[],"result":{"status":"pending"}}`,
		privatePrefix + "WalletTransfer":   `{"error":[],"result":{}}`,
	})
	client, _ := newExtClient(t, server.URL)
	if _, err := client.CreateSubaccount(context.Background(), CreateSubaccountRequest{
		Username: "abc123", Email: "abc123@example.com",
	}); !errors.Is(err, trade.ErrUnknownExecutionState) {
		t.Fatalf("CreateSubaccount() error = %v, want unknown execution state", err)
	}
	if _, err := client.AccountTransfer(context.Background(), AccountTransferRequest{
		Asset: "XBT", Amount: "1", From: "A", To: "B",
	}); !errors.Is(err, trade.ErrUnknownExecutionState) {
		t.Fatalf("AccountTransfer() error = %v, want unknown execution state", err)
	}
	if _, err := client.WalletTransfer(context.Background(), WalletTransferRequest{
		Asset: "XBT", Amount: "1",
	}); !errors.Is(err, trade.ErrUnknownExecutionState) {
		t.Fatalf("WalletTransfer() error = %v, want unknown execution state", err)
	}
}

func TestFundingRequestValidation(t *testing.T) {
	t.Parallel()

	end := time.Unix(100, 0)
	start := time.Unix(200, 0)
	invalid := []error{
		(DepositMethodsRequest{}).validate(),
		(DepositMethodsRequest{Asset: "XBT", AssetClass: "equity"}).validate(),
		(DepositMethodsRequest{Asset: "XBT", RebaseMultiplier: "other"}).validate(),
		(DepositAddressesRequest{Method: "Bitcoin"}).validate(),
		(DepositAddressesRequest{Asset: "XBT"}).validate(),
		(DepositAddressesRequest{Asset: "XBT", Method: "Bitcoin", Amount: "abc"}).validate(),
		(FundingStatusRequest{Asset: "bad asset"}).validate(),
		(FundingStatusRequest{Method: "bad\nmethod"}).validate(),
		(FundingStatusRequest{Start: &start, End: &end}).validate(),
		(FundingStatusRequest{Cursor: "bad cursor"}).validate(),
		(FundingStatusRequest{Limit: -1}).validate(),
		(WithdrawMethodsRequest{Network: "bad\nnetwork"}).validate(),
		(WithdrawAddressesRequest{Key: "bad\nkey"}).validate(),
		(WithdrawInfoRequest{Key: "key", Amount: "1"}).validate(),
		(WithdrawInfoRequest{Asset: "XBT", Amount: "1"}).validate(),
		(WithdrawInfoRequest{Asset: "XBT", Key: "key", Amount: "0"}).validate(),
		(WithdrawRequest{Key: "key", Amount: "1"}).validate(),
		(WithdrawRequest{Asset: "XBT", Amount: "1"}).validate(),
		(WithdrawRequest{Asset: "XBT", Key: "key", Amount: "-1"}).validate(),
		(WithdrawRequest{Asset: "XBT", Key: "key", Amount: "1", MaxFee: "x"}).validate(),
		(WithdrawRequest{Asset: "XBT", Key: "key", Amount: "1", Address: "a\nb"}).validate(),
		(WithdrawCancelRequest{ReferenceID: "R1"}).validate(),
		(WithdrawCancelRequest{Asset: "XBT"}).validate(),
		(WithdrawCancelRequest{Asset: "XBT", ReferenceID: "bad ref"}).validate(),
		(WalletTransferRequest{Amount: "1"}).validate(),
		(WalletTransferRequest{Asset: "XBT"}).validate(),
		(CreateSubaccountRequest{Email: "a@example.com"}).validate(),
		(CreateSubaccountRequest{Username: "abc"}).validate(),
		(AccountTransferRequest{Amount: "1", From: "A", To: "B"}).validate(),
		(AccountTransferRequest{Asset: "XBT", From: "A", To: "B"}).validate(),
		(AccountTransferRequest{Asset: "XBT", Amount: "1", To: "B"}).validate(),
		(AccountTransferRequest{Asset: "XBT", Amount: "1", From: "A"}).validate(),
		(AccountTransferRequest{Asset: "XBT", Amount: "1", From: "A", To: "A"}).validate(),
	}
	for index, err := range invalid {
		if !errors.Is(err, trade.ErrValidation) {
			t.Fatalf("validation error %d = %v, want validation", index, err)
		}
	}
	valid := []error{
		(DepositMethodsRequest{Asset: "XBT"}).validate(),
		(DepositAddressesRequest{Asset: "XBT", Method: "Bitcoin Lightning", Amount: "0.001"}).validate(),
		(FundingStatusRequest{}).validate(),
		(FundingStatusRequest{Asset: "XBT", Cursor: "abc-1", Limit: 10, RebaseMultiplier: RebaseMultiplierRebased}).validate(),
		(WithdrawMethodsRequest{}).validate(),
		(WithdrawAddressesRequest{}).validate(),
		(WithdrawInfoRequest{Asset: "XBT", Key: "key", Amount: "0.725"}).validate(),
		(WithdrawRequest{Asset: "XBT", Key: "key", Amount: "1"}).validate(),
		(WithdrawCancelRequest{Asset: "XBT", ReferenceID: "FTQcuak-V6Za8qrWnhzTx67yYHz8Tg"}).validate(),
		(WalletTransferRequest{Asset: "XBT", Amount: "1"}).validate(),
		(CreateSubaccountRequest{Username: "abc", Email: "a@example.com"}).validate(),
		(AccountTransferRequest{Asset: "XBT", Amount: "1", From: "A", To: "B"}).validate(),
	}
	for index, err := range valid {
		if err != nil {
			t.Fatalf("valid request %d error = %v", index, err)
		}
	}
}
