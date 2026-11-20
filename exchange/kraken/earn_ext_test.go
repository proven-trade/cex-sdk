package kraken

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
)

const testEarnStrategyID = "ESRFUO3-Q62XD-WIOIL7"

func TestEarnStrategiesSendsFiltersAndDecodesLockTypes(t *testing.T) {
	t.Parallel()

	server := newExtServer(t, map[string]string{
		privatePrefix + "Earn/Strategies": `{"error":[],"result":{"next_cursor":"2","items":[{"id":"ESRFUO3-Q62XD-WIOIL7","asset":"DOT","lock_type":{"type":"instant","payout_frequency":604800},"apr_estimate":{"low":"8.0000","high":"12.0000"},"user_min_allocation":"0.01","allocation_fee":"0.0000","deallocation_fee":0,"auto_compound":{"type":"optional","default":true},"yield_source":{"type":"staking"},"can_allocate":true,"can_deallocate":true,"allocation_restriction_info":["tier"]},{"id":"ESAAAAA-BBBBB-CCCCCC","asset":"ETH","lock_type":{"type":"bonded","bonding_period":0,"bonding_rewards":false,"unbonding_period":86400,"unbonding_period_variable":true,"unbonding_rewards":true,"exit_queue_period":3600,"payout_frequency":432000},"user_cap":"1000","apr_estimate":null,"can_allocate":false,"can_deallocate":true,"allocation_restriction_info":[]}]}}`,
	})
	client, limiter := newExtClient(t, server.URL, credential.PermissionRead)
	page, err := client.EarnStrategies(context.Background(), EarnStrategiesRequest{
		Asset: "DOT", LockTypes: []EarnLockType{EarnLockInstant, EarnLockBonded}, Ascending: true, Limit: 10,
		Cursor: "1",
	})
	if err != nil {
		t.Fatalf("EarnStrategies() error = %v", err)
	}
	if len(page.Strategies) != 2 || page.NextCursor != "2" || len(page.Raw) == 0 {
		t.Fatalf("EarnStrategies() = %+v", page)
	}
	first, second := page.Strategies[0], page.Strategies[1]
	if first.ID != testEarnStrategyID || first.Asset != "DOT" || first.LockType.Type != EarnLockInstant ||
		first.LockType.PayoutFrequency != 604800 || first.APREstimate == nil || first.APREstimate.Low != "8.0000" ||
		first.APREstimate.High != "12.0000" || first.UserMinAllocation != "0.01" ||
		first.AllocationFee != "0.0000" || first.DeallocationFee != "0" ||
		first.AutoCompound.Type != "optional" || !first.AutoCompound.Default ||
		first.YieldSource.Type != "staking" || !first.CanAllocate || !first.CanDeallocate ||
		len(first.AllocationRestrictionInfo) != 1 || first.AllocationRestrictionInfo[0] != "tier" ||
		len(first.Raw) == 0 {
		t.Fatalf("first Earn strategy = %+v", first)
	}
	if second.LockType.Type != EarnLockBonded || second.LockType.UnbondingPeriod != 86400 ||
		!second.LockType.UnbondingPeriodVariable || !second.LockType.UnbondingRewards ||
		second.LockType.ExitQueuePeriod != 3600 || second.UserCap != "1000" || second.APREstimate != nil ||
		second.CanAllocate {
		t.Fatalf("second Earn strategy = %+v", second)
	}
	request := server.last(t)
	body := request.jsonBody(t)
	lockTypes, ok := body["lock_type"].([]any)
	if request.Method != http.MethodPost || request.Path != privatePrefix+"Earn/Strategies" ||
		request.ContentType != jsonContentType || body["asset"] != "DOT" || body["ascending"] != true ||
		fmt.Sprint(body["limit"]) != "10" || body["cursor"] != "1" || !ok || len(lockTypes) != 2 ||
		lockTypes[0] != "instant" || lockTypes[1] != "bonded" {
		t.Fatalf("EarnStrategies request = %+v body = %v", request, body)
	}
	snapshot, err := limiter.Snapshot("kraken:account:kraken-main:private-counter")
	if err != nil || snapshot.Used != 1 {
		t.Fatalf("private limiter snapshot = %+v, error = %v", snapshot, err)
	}
}

func TestEarnAllocationsDecodesStatesAndPayout(t *testing.T) {
	t.Parallel()

	server := newExtServer(t, map[string]string{
		privatePrefix + "Earn/Allocations": `{"error":[],"result":{"converted_asset":"EUR","total_allocated":"49.2398","total_rewarded":"0.0675","items":[{"strategy_id":"ESDQCOL-WTZEU-NU55QF","native_asset":"ETH","amount_allocated":{"bonding":{"native":"0.0210000000","converted":"39.0645","allocation_count":2,"allocations":[{"created_at":"2023-07-06T10:52:05Z","expires":"2023-08-19T02:34:05.807Z","native":"0.0010000000","converted":"1.8602"},{"created_at":"2023-08-01T11:25:52Z","expires":"2023-09-06T07:55:52.648Z","native":"0.0200000000","converted":"37.2043"}]},"pending":{"native":"-0.5","converted":"-900"},"total":{"native":"0.0210000000","converted":"39.0645"}},"payout":{"accumulated_reward":{"native":"0.01","converted":"0.02"},"estimated_reward":{"native":"0.03","converted":"0.05"},"period_start":"2023-08-01T00:00:00Z","period_end":"2023-08-08T00:00:00Z"},"total_rewarded":{"native":"0","converted":"0.0000"}},{"strategy_id":"ESEEEEE-FFFFF-GGGGGG","native_asset":"DOT","amount_allocated":{"total":{"native":"0","converted":"0"}},"payout":null,"total_rewarded":{"native":"1","converted":"5"}}]}}`,
	})
	client, _ := newExtClient(t, server.URL)
	page, err := client.EarnAllocations(context.Background(), EarnAllocationsRequest{
		ConvertedAsset: "EUR", HideZeroAllocations: true,
	})
	if err != nil {
		t.Fatalf("EarnAllocations() error = %v", err)
	}
	if page.ConvertedAsset != "EUR" || page.TotalAllocated != "49.2398" || page.TotalRewarded != "0.0675" ||
		len(page.Allocations) != 2 || len(page.Raw) == 0 {
		t.Fatalf("EarnAllocations() = %+v", page)
	}
	first, second := page.Allocations[0], page.Allocations[1]
	if first.StrategyID != "ESDQCOL-WTZEU-NU55QF" || first.NativeAsset != "ETH" ||
		first.AmountAllocated.Bonding == nil || first.AmountAllocated.Bonding.AllocationCount != 2 ||
		len(first.AmountAllocated.Bonding.Allocations) != 2 ||
		first.AmountAllocated.Bonding.Allocations[1].Expires != "2023-09-06T07:55:52.648Z" ||
		first.AmountAllocated.Pending == nil || first.AmountAllocated.Pending.Native != "-0.5" ||
		first.AmountAllocated.Total == nil || first.AmountAllocated.Total.Converted != "39.0645" ||
		first.AmountAllocated.Unbonding != nil || first.AmountAllocated.ExitQueue != nil ||
		first.Payout == nil || first.Payout.EstimatedReward.Converted != "0.05" ||
		first.Payout.PeriodEnd != "2023-08-08T00:00:00Z" || len(first.Raw) == 0 {
		t.Fatalf("first Earn allocation = %+v", first)
	}
	if second.Payout != nil || second.AmountAllocated.Bonding != nil || second.TotalRewarded.Converted != "5" {
		t.Fatalf("second Earn allocation = %+v", second)
	}
	request := server.last(t)
	body := request.jsonBody(t)
	if request.Path != privatePrefix+"Earn/Allocations" || body["converted_asset"] != "EUR" ||
		body["hide_zero_allocations"] != true {
		t.Fatalf("EarnAllocations request = %+v body = %v", request, body)
	}
}

func TestEarnAllocateAndDeallocateUseTransferPermission(t *testing.T) {
	t.Parallel()

	server := newExtServer(t, map[string]string{
		privatePrefix + "Earn/Allocate":   `{"error":[],"result":true}`,
		privatePrefix + "Earn/Deallocate": `{"error":[],"result":true}`,
	})
	client, _ := newExtClient(t, server.URL, credential.PermissionTransfer)
	request := EarnAllocateRequest{StrategyID: testEarnStrategyID, Amount: "4.3"}

	allocated, err := client.EarnAllocate(context.Background(), request)
	if err != nil {
		t.Fatalf("EarnAllocate() error = %v", err)
	}
	if !allocated.Accepted || len(allocated.Raw) == 0 {
		t.Fatalf("EarnAllocate() = %+v", allocated)
	}
	recorded := server.last(t)
	body := recorded.jsonBody(t)
	if recorded.Method != http.MethodPost || recorded.Path != privatePrefix+"Earn/Allocate" ||
		recorded.ContentType != jsonContentType || body["strategy_id"] != testEarnStrategyID ||
		body["amount"] != "4.3" {
		t.Fatalf("EarnAllocate request = %+v body = %v", recorded, body)
	}

	deallocated, err := client.EarnDeallocate(context.Background(), request)
	if err != nil {
		t.Fatalf("EarnDeallocate() error = %v", err)
	}
	if !deallocated.Accepted || server.last(t).Path != privatePrefix+"Earn/Deallocate" {
		t.Fatalf("EarnDeallocate() = %+v path = %s", deallocated, server.last(t).Path)
	}
}

func TestEarnAllocateRejectsUnacceptedResult(t *testing.T) {
	t.Parallel()

	server := newExtServer(t, map[string]string{
		privatePrefix + "Earn/Allocate":   `{"error":[],"result":false}`,
		privatePrefix + "Earn/Deallocate": `{"error":[],"result":null}`,
	})
	client, _ := newExtClient(t, server.URL)
	request := EarnAllocateRequest{StrategyID: testEarnStrategyID, Amount: "1"}
	if _, err := client.EarnAllocate(context.Background(), request); !errors.Is(err, trade.ErrUnknownExecutionState) {
		t.Fatalf("EarnAllocate(false) error = %v, want unknown execution state", err)
	}
	if _, err := client.EarnDeallocate(context.Background(), request); !errors.Is(err, trade.ErrUnknownExecutionState) {
		t.Fatalf("EarnDeallocate(null) error = %v, want unknown execution state", err)
	}
}

func TestEarnOperationStatusDecodesPendingAndNull(t *testing.T) {
	t.Parallel()

	server := newExtServer(t, map[string]string{
		privatePrefix + "Earn/AllocateStatus":   `{"error":[],"result":{"pending":true}}`,
		privatePrefix + "Earn/DeallocateStatus": `{"error":[],"result":null}`,
	})
	client, _ := newExtClient(t, server.URL, credential.PermissionRead)
	request := EarnOperationStatusRequest{StrategyID: testEarnStrategyID}

	allocate, err := client.EarnAllocateStatus(context.Background(), request)
	if err != nil {
		t.Fatalf("EarnAllocateStatus() error = %v", err)
	}
	if !allocate.Pending || len(allocate.Raw) == 0 {
		t.Fatalf("EarnAllocateStatus() = %+v", allocate)
	}
	recorded := server.last(t)
	if recorded.Path != privatePrefix+"Earn/AllocateStatus" || recorded.jsonBody(t)["strategy_id"] != testEarnStrategyID {
		t.Fatalf("EarnAllocateStatus request = %+v", recorded)
	}

	deallocate, err := client.EarnDeallocateStatus(context.Background(), request)
	if err != nil {
		t.Fatalf("EarnDeallocateStatus() error = %v", err)
	}
	if deallocate.Pending || server.last(t).Path != privatePrefix+"Earn/DeallocateStatus" {
		t.Fatalf("EarnDeallocateStatus() = %+v", deallocate)
	}
}

func TestEarnExtensionsRequirePermissionsAndValidateRequests(t *testing.T) {
	t.Parallel()

	server := newExtServer(t, map[string]string{})
	readClient, _ := newExtClient(t, server.URL, credential.PermissionRead)
	if _, err := readClient.EarnAllocate(context.Background(), EarnAllocateRequest{
		StrategyID: testEarnStrategyID, Amount: "1",
	}); !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("EarnAllocate() error = %v, want authorization", err)
	}
	tradeClient, _ := newExtClient(t, server.URL, credential.PermissionTrade)
	if _, err := tradeClient.EarnAllocations(context.Background(), EarnAllocationsRequest{}); !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("EarnAllocations() error = %v, want authorization", err)
	}
	if server.count() != 0 {
		t.Fatalf("server requests = %d, want 0", server.count())
	}

	invalid := []error{
		(EarnStrategiesRequest{Asset: "bad asset"}).validate(),
		(EarnStrategiesRequest{LockTypes: []EarnLockType{"locked"}}).validate(),
		(EarnStrategiesRequest{Limit: -1}).validate(),
		(EarnStrategiesRequest{Cursor: "bad cursor"}).validate(),
		(EarnAllocationsRequest{ConvertedAsset: "bad asset"}).validate(),
		(EarnAllocateRequest{Amount: "1"}).validate(),
		(EarnAllocateRequest{StrategyID: "bad id", Amount: "1"}).validate(),
		(EarnAllocateRequest{StrategyID: testEarnStrategyID}).validate(),
		(EarnAllocateRequest{StrategyID: testEarnStrategyID, Amount: "0"}).validate(),
		(EarnAllocateRequest{StrategyID: testEarnStrategyID, Amount: "1e3"}).validate(),
		(EarnOperationStatusRequest{}).validate(),
	}
	for index, err := range invalid {
		if !errors.Is(err, trade.ErrValidation) {
			t.Fatalf("validation error %d = %v, want validation", index, err)
		}
	}
	valid := []error{
		(EarnStrategiesRequest{}).validate(),
		(EarnStrategiesRequest{Asset: "DOT", LockTypes: []EarnLockType{EarnLockFlex, EarnLockTimed}}).validate(),
		(EarnAllocationsRequest{}).validate(),
		(EarnOperationStatusRequest{StrategyID: testEarnStrategyID}).validate(),
	}
	for index, err := range valid {
		if err != nil {
			t.Fatalf("valid request %d error = %v", index, err)
		}
	}
}
