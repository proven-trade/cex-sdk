package korbit

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

// CreateAccountTransferRequest는 본인의 두 계정 사이 자산 이전 요청이다. AccountSeq의 자산을 ToAccountSeq로 옮기며
// AccountSeq는 API Key의 allowedAccountSeqs에 있어야 한다. Qty는 소수점 이하 8자리까지 허용하고 지수 표기는 쓸 수 없다.
// RequestKey는 사용자 단위 멱등 키다. 응답을 받지 못해도 같은 키로 AccountTransfer를 조회해 결과를 확인할 수 있도록
// 거래소 문서와 달리 SDK에서는 필수로 요구한다.
type CreateAccountTransferRequest struct {
	AccountSeq   int
	ToAccountSeq int
	Currency     string
	Qty          string
	RequestKey   string
}

// AccountTransferRequest는 계정 간 이전 한 건의 조회 조건이다. ID와 RequestKey 중 정확히 하나만 지정한다.
// AccountSeq는 이전의 출금 계정이거나 입금 계정이어야 하며, 그렇지 않으면 존재하지 않는 이전과 구분되지 않는 NOT_FOUND가 반환된다.
type AccountTransferRequest struct {
	AccountSeq int
	ID         int64
	RequestKey string
}

// AccountTransferDirection은 최근 계정 이전 목록에서 조회할 방향이다.
type AccountTransferDirection string

const (
	AccountTransferDirectionOut AccountTransferDirection = "out"
	AccountTransferDirectionIn  AccountTransferDirection = "in"
)

// RecentAccountTransfersRequest는 한 계정의 최근 계정 이전 목록 조회 조건이다. 한 번에 한쪽 방향만 조회하므로
// 양쪽을 모두 보려면 Direction을 바꿔 두 번 호출한다. Limit은 1~100이다.
type RecentAccountTransfersRequest struct {
	AccountSeq int
	Direction  AccountTransferDirection
	Limit      int
}

// AccountTransferStatus는 계정 간 이전 처리 상태다.
type AccountTransferStatus string

const (
	AccountTransferStatusPending AccountTransferStatus = "pending"
	AccountTransferStatusDone    AccountTransferStatus = "done"
	AccountTransferStatusFailed  AccountTransferStatus = "failed"
)

// AccountTransferSource는 계정 간 이전이 제출된 경로다.
type AccountTransferSource string

const (
	AccountTransferSourceAPI AccountTransferSource = "api"
	AccountTransferSourceApp AccountTransferSource = "app"
)

// AccountTransferReceipt는 계정 간 이전 요청 접수 결과다. 접수는 정산이 아니므로 Status는 접수 직후 항상 pending이고,
// 자산은 비동기로 이동한다. 최종 상태는 AccountTransfer로 확인한다.
type AccountTransferReceipt struct {
	ID         int64                 `json:"id"`
	RequestKey string                `json:"requestKey"`
	Status     AccountTransferStatus `json:"status"`
	Raw        json.RawMessage       `json:"-"`
}

// AccountTransfer는 본인 계정 간 자산 이전 한 건이다. RequestKey는 API로 요청하지 않은 이전이면 빈 문자열이고,
// UpdatedAt은 상태가 확정된 시각이며 pending이면 0이다. pending과 알 수 없는 상태는 모두 아직 최종이 아닌 것으로 취급한다.
type AccountTransfer struct {
	ID             int64                 `json:"id"`
	RequestKey     string                `json:"requestKey"`
	Status         AccountTransferStatus `json:"status"`
	Currency       string                `json:"currency"`
	FromAccountSeq int                   `json:"fromAccountSeq"`
	ToAccountSeq   int                   `json:"toAccountSeq"`
	Qty            string                `json:"qty"`
	Source         AccountTransferSource `json:"source"`
	CreatedAt      int64                 `json:"createdAt"`
	UpdatedAt      int64                 `json:"updatedAt"`
	Raw            json.RawMessage       `json:"-"`
}

func (request CreateAccountTransferRequest) validate() error {
	if err := validateRequiredAccountSeq("accountSeq", request.AccountSeq); err != nil {
		return err
	}
	if err := validateRequiredAccountSeq("toAccountSeq", request.ToAccountSeq); err != nil {
		return err
	}
	if request.AccountSeq == request.ToAccountSeq {
		return validationError("toAccountSeq must differ from accountSeq")
	}
	if err := validateCurrency(request.Currency); err != nil {
		return err
	}
	if err := validateTransferQty(request.Qty); err != nil {
		return err
	}
	return validateRequestKey(request.RequestKey)
}

func (request CreateAccountTransferRequest) values() url.Values {
	values := make(url.Values)
	setRequiredAccountSeq(values, "accountSeq", request.AccountSeq)
	setRequiredAccountSeq(values, "toAccountSeq", request.ToAccountSeq)
	values.Set("currency", request.Currency)
	values.Set("qty", request.Qty)
	values.Set("requestKey", request.RequestKey)
	return values
}

func (request AccountTransferRequest) validate() error {
	if err := validateRequiredAccountSeq("accountSeq", request.AccountSeq); err != nil {
		return err
	}
	if (request.ID == 0) == (request.RequestKey == "") {
		return validationError("exactly one of account transfer ID or request key is required")
	}
	if request.RequestKey != "" {
		return validateRequestKey(request.RequestKey)
	}
	return validatePositiveID("account transfer ID", request.ID)
}

func (request AccountTransferRequest) values() url.Values {
	values := make(url.Values)
	setRequiredAccountSeq(values, "accountSeq", request.AccountSeq)
	if request.ID > 0 {
		values.Set("id", strconv.FormatInt(request.ID, 10))
	}
	setIfNotEmpty(values, "requestKey", request.RequestKey)
	return values
}

func (request RecentAccountTransfersRequest) validate() error {
	if err := validateRequiredAccountSeq("accountSeq", request.AccountSeq); err != nil {
		return err
	}
	if request.Direction != AccountTransferDirectionOut && request.Direction != AccountTransferDirectionIn {
		return validationError("account transfer direction must be out or in")
	}
	return validateRequiredLimit("recent account transfer", request.Limit, 100)
}

func (request RecentAccountTransfersRequest) values() url.Values {
	values := url.Values{"direction": {string(request.Direction)}}
	setRequiredAccountSeq(values, "accountSeq", request.AccountSeq)
	values.Set("limit", strconv.Itoa(request.Limit))
	return values
}

// CreateAccountTransfer는 본인의 한 계정에서 다른 계정으로 자산 이전을 요청한다.
// 이 호출은 credential.PermissionTransfer 권한을 요구하고 자동 재시도하지 않는다. 접수는 정산이 아니므로 반환값의
// Status가 pending이어도 이동은 비동기로 진행되며, 최종 상태는 AccountTransfer로 확인한다.
// UNKNOWN_EXECUTION_STATE나 DUPLICATE_REQUEST_KEY를 받으면 같은 RequestKey로 AccountTransfer를 조회해 접수 여부를
// 확인한 뒤에만 다시 요청한다.
func (client *Client) CreateAccountTransfer(
	ctx context.Context,
	request CreateAccountTransferRequest,
	options ...trade.RequestOption,
) (AccountTransferReceipt, error) {
	if err := request.validate(); err != nil {
		return AccountTransferReceipt{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodPost, "/v2/accountTransfer", request.values(), rateGroupAsset,
		credential.PermissionTransfer, commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return AccountTransferReceipt{}, err
	}
	var result AccountTransferReceipt
	raw, err := client.decodeResponse(response, commonexchange.OperationMutation, &result)
	if err != nil {
		return AccountTransferReceipt{}, err
	}
	if result.ID <= 0 {
		return AccountTransferReceipt{}, client.decodeBodyError(
			response, commonexchange.OperationMutation, errors.New("Korbit account transfer response has no ID"),
		)
	}
	result.Raw = raw
	return result, nil
}

// AccountTransfer는 계정 간 이전 한 건을 ID 또는 RequestKey로 조회한다.
func (client *Client) AccountTransfer(
	ctx context.Context,
	request AccountTransferRequest,
	options ...trade.RequestOption,
) (AccountTransfer, error) {
	if err := request.validate(); err != nil {
		return AccountTransfer{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/v2/accountTransfer", request.values(), rateGroupAsset,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return AccountTransfer{}, err
	}
	var result AccountTransfer
	raw, err := client.decodeResponse(response, commonexchange.OperationRead, &result)
	if err != nil {
		return AccountTransfer{}, err
	}
	if result.ID <= 0 {
		return AccountTransfer{}, client.decodeBodyError(
			response, commonexchange.OperationRead, errors.New("Korbit account transfer has no ID"),
		)
	}
	result.Raw = raw
	return result, nil
}

// RecentAccountTransfers는 한 계정의 최근 계정 이전을 한쪽 방향만 최신순으로 조회한다.
// 방금 제출한 이전은 목록에 아직 없을 수 있으므로 AccountTransfer로 직접 확인한다.
func (client *Client) RecentAccountTransfers(
	ctx context.Context,
	request RecentAccountTransfersRequest,
	options ...trade.RequestOption,
) ([]AccountTransfer, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/v2/recentAccountTransfers", request.values(), rateGroupAsset,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeItems[AccountTransfer](
		client, response, func(item *AccountTransfer, raw []byte) { item.Raw = raw },
	)
}
