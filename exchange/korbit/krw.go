package korbit

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

// KRWPushRequest는 원화 입금·출금 푸시 요청이다. 거래소가 Digital X 앱으로 알림을 보내고, 사용자가 앱에서 본인 확인을
// 마쳐야 입금·출금이 진행된다. 입출금 API는 메인 계정만 지원하므로 AccountSeq는 0(생략)이나 1만 허용한다.
type KRWPushRequest struct {
	AccountSeq int
	Amount     string
}

// RecentKRWDepositsRequest는 최근 원화 입금 목록 조회 조건이다. Limit은 1~100이고 0이면 거래소 기본값을 쓴다.
// IncludeAll이 true면 입금 수수료·이벤트 보상 같은 부가 내역도 포함하고, false면 일반 원화 입금만 조회한다.
type RecentKRWDepositsRequest struct {
	AccountSeq int
	Limit      int
	IncludeAll bool
}

// RecentKRWWithdrawalsRequest는 최근 원화 출금 목록 조회 조건이다. Limit은 1~100이고 0이면 거래소 기본값을 쓴다.
type RecentKRWWithdrawalsRequest struct {
	AccountSeq int
	Limit      int
}

// KRWPushResult는 원화 푸시 요청이 거래소에 접수됐음을 나타낸다. 사용자의 앱 본인 확인이 끝나기 전에는 입금·출금이 진행되지 않는다.
type KRWPushResult struct {
	Accepted bool
	Raw      json.RawMessage
}

// KRWDepositType은 원화 입금 내역의 종류다.
type KRWDepositType string

const (
	KRWDepositTypeGeneral         KRWDepositType = "general"
	KRWDepositTypeDepositInterest KRWDepositType = "depositInterest"
	KRWDepositTypeMakerIncentive  KRWDepositType = "makerIncentive"
	KRWDepositTypeReward          KRWDepositType = "reward"
	KRWDepositTypeEtc             KRWDepositType = "etc"
)

// KRWDepositStatus는 원화 입금 처리 상태다.
type KRWDepositStatus string

const (
	KRWDepositStatusPending    KRWDepositStatus = "pending"
	KRWDepositStatusProcessing KRWDepositStatus = "processing"
	KRWDepositStatusReviewing  KRWDepositStatus = "reviewing"
	KRWDepositStatusDone       KRWDepositStatus = "done"
	KRWDepositStatusCanceling  KRWDepositStatus = "canceling"
	KRWDepositStatusCanceled   KRWDepositStatus = "canceled"
	KRWDepositStatusFailed     KRWDepositStatus = "failed"
)

// KRWDeposit은 원화 입금 한 건이다. Type은 IncludeAll로 조회할 때만 채워지고, FilledAt은 잔고에 반영된 시각이며
// 기록이 없거나 진행 중이면 nil이다.
type KRWDeposit struct {
	ID        int64            `json:"id"`
	Type      KRWDepositType   `json:"type"`
	Status    KRWDepositStatus `json:"status"`
	Quantity  string           `json:"quantity"`
	CreatedAt int64            `json:"createdAt"`
	FilledAt  *int64           `json:"filledAt"`
	Raw       json.RawMessage  `json:"-"`
}

// KRWWithdrawalStatus는 원화 출금 처리 상태다.
type KRWWithdrawalStatus string

const (
	KRWWithdrawalStatusPending    KRWWithdrawalStatus = "pending"
	KRWWithdrawalStatusReviewing  KRWWithdrawalStatus = "reviewing"
	KRWWithdrawalStatusProcessing KRWWithdrawalStatus = "processing"
	KRWWithdrawalStatusDone       KRWWithdrawalStatus = "done"
	KRWWithdrawalStatusFailed     KRWWithdrawalStatus = "failed"
	KRWWithdrawalStatusCanceled   KRWWithdrawalStatus = "canceled"
)

// KRWWithdrawal은 원화 출금 한 건이다. Quantity는 수수료를 제외한 출금 금액이다.
type KRWWithdrawal struct {
	ID        int64               `json:"id"`
	Quantity  string              `json:"quantity"`
	Fee       string              `json:"fee"`
	Status    KRWWithdrawalStatus `json:"status"`
	CreatedAt int64               `json:"createdAt"`
	Raw       json.RawMessage     `json:"-"`
}

func (request KRWPushRequest) validate() error {
	if err := validateMainAccountSeq(request.AccountSeq); err != nil {
		return err
	}
	return validatePositiveDecimal("KRW amount", request.Amount)
}

func (request KRWPushRequest) values() url.Values {
	values := url.Values{"amount": {request.Amount}}
	setAccountSeq(values, request.AccountSeq)
	return values
}

func (request RecentKRWDepositsRequest) validate() error {
	if err := validateMainAccountSeq(request.AccountSeq); err != nil {
		return err
	}
	return validateOptionalLimit("recent KRW deposit", request.Limit, 100)
}

func (request RecentKRWDepositsRequest) values() url.Values {
	values := make(url.Values)
	setAccountSeq(values, request.AccountSeq)
	setPositiveInt(values, "limit", request.Limit)
	if request.IncludeAll {
		values.Set("includeAll", "true")
	}
	return values
}

func (request RecentKRWWithdrawalsRequest) validate() error {
	if err := validateMainAccountSeq(request.AccountSeq); err != nil {
		return err
	}
	return validateOptionalLimit("recent KRW withdrawal", request.Limit, 100)
}

func (request RecentKRWWithdrawalsRequest) values() url.Values {
	values := make(url.Values)
	setAccountSeq(values, request.AccountSeq)
	setPositiveInt(values, "limit", request.Limit)
	return values
}

// SendKRWDepositPush는 원화 입금 요청 알림을 사용자의 Digital X 앱으로 보낸다.
// 이 호출은 credential.PermissionTransfer 권한을 요구하고 자동 재시도하지 않는다.
func (client *Client) SendKRWDepositPush(
	ctx context.Context,
	request KRWPushRequest,
	options ...trade.RequestOption,
) (KRWPushResult, error) {
	return client.sendKRWPush(ctx, "/v2/krw/sendKrwDepositPush", credential.PermissionTransfer, request, options...)
}

// SendKRWWithdrawalPush는 원화 출금 요청 알림을 사용자의 Digital X 앱으로 보낸다.
// 이 호출은 credential.PermissionWithdraw 권한을 요구하고 자동 재시도하지 않는다.
func (client *Client) SendKRWWithdrawalPush(
	ctx context.Context,
	request KRWPushRequest,
	options ...trade.RequestOption,
) (KRWPushResult, error) {
	return client.sendKRWPush(ctx, "/v2/krw/sendKrwWithdrawalPush", credential.PermissionWithdraw, request, options...)
}

func (client *Client) sendKRWPush(
	ctx context.Context,
	path string,
	permission credential.Permission,
	request KRWPushRequest,
	options ...trade.RequestOption,
) (KRWPushResult, error) {
	if err := request.validate(); err != nil {
		return KRWPushResult{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodPost, path, request.values(), rateGroupAsset,
		permission, commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return KRWPushResult{}, err
	}
	if _, err := client.decodeResponse(response, commonexchange.OperationMutation, nil); err != nil {
		return KRWPushResult{}, err
	}
	return KRWPushResult{Accepted: true, Raw: cloneBytes(response.Body)}, nil
}

// RecentKRWDeposits는 최근 원화 입금 목록을 조회한다.
func (client *Client) RecentKRWDeposits(
	ctx context.Context,
	request RecentKRWDepositsRequest,
	options ...trade.RequestOption,
) ([]KRWDeposit, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/v2/krw/recentDeposits", request.values(), rateGroupAsset,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeItems[KRWDeposit](client, response, func(item *KRWDeposit, raw []byte) { item.Raw = raw })
}

// RecentKRWWithdrawals는 최근 원화 출금 목록을 조회한다.
func (client *Client) RecentKRWWithdrawals(
	ctx context.Context,
	request RecentKRWWithdrawalsRequest,
	options ...trade.RequestOption,
) ([]KRWWithdrawal, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/v2/krw/recentWithdrawals", request.values(), rateGroupAsset,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeItems[KRWWithdrawal](client, response, func(item *KRWWithdrawal, raw []byte) { item.Raw = raw })
}
