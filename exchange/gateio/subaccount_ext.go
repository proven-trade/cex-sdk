package gateio

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

// SubAccount는 서브계정 정보다. State는 1 정상, 2 잠금이고 Type은 1 일반, 3 교차마진이다.
type SubAccount struct {
	Remark     string          `json:"remark"`
	LoginName  string          `json:"login_name"`
	Email      string          `json:"email"`
	State      int             `json:"state"`
	Type       int             `json:"type"`
	UserID     int64           `json:"user_id"`
	CreateTime int64           `json:"create_time"`
	Raw        json.RawMessage `json:"-"`
}

func (account *SubAccount) setRaw(raw json.RawMessage) { account.Raw = raw }

// SubAccountsRequest는 서브계정 목록 조회 조건이다. IncludeCrossMargin이 true이면 교차마진
// 서브계정까지 포함하고 false이면 일반 서브계정만 조회한다.
type SubAccountsRequest struct{ IncludeCrossMargin bool }

// SubAccounts는 서브계정 목록을 조회한다. 권한은 Read다.
// GET /sub_accounts
func (client *Client) SubAccounts(
	ctx context.Context,
	request SubAccountsRequest,
	options ...trade.RequestOption,
) ([]SubAccount, error) {
	query := make(url.Values)
	if request.IncludeCrossMargin {
		query.Set("type", "0")
	} else {
		query.Set("type", "1")
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/sub_accounts", query, nil,
		privateLimit("sub-accounts"), credential.PermissionRead,
		commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeList[SubAccount](client, response, commonexchange.OperationRead)
}

// CreateSubAccountRequest는 서브계정 생성 요청이다. Password와 Email을 비우면 메인계정 값을 따른다.
type CreateSubAccountRequest struct {
	LoginName string `json:"login_name"`
	Remark    string `json:"remark,omitempty"`
	Password  string `json:"password,omitempty"`
	Email     string `json:"email,omitempty"`
}

func (request CreateSubAccountRequest) validate() error {
	if strings.TrimSpace(request.LoginName) == "" {
		return validationError("sub-account login name is required")
	}
	return nil
}

// CreateSubAccount는 서브계정을 생성한다. 권한은 Transfer이며 자동 재시도하지 않는다.
// POST /sub_accounts
func (client *Client) CreateSubAccount(
	ctx context.Context,
	request CreateSubAccountRequest,
	options ...trade.RequestOption,
) (SubAccount, error) {
	if err := request.validate(); err != nil {
		return SubAccount{}, err
	}
	body, err := encodeBody(request)
	if err != nil {
		return SubAccount{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodPost, "/sub_accounts", nil, body,
		privateLimit("create-sub-account"), credential.PermissionTransfer,
		commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return SubAccount{}, err
	}
	return decodeObject[SubAccount](client, response, commonexchange.OperationMutation)
}

// SubAccountInfo는 서브계정 한 건을 조회한다. 권한은 Read다.
// GET /sub_accounts/{user_id}
func (client *Client) SubAccountInfo(
	ctx context.Context,
	userID string,
	options ...trade.RequestOption,
) (SubAccount, error) {
	if !numericIDPattern.MatchString(userID) {
		return SubAccount{}, validationError("invalid sub-account user ID")
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/sub_accounts/"+url.PathEscape(userID), nil, nil,
		privateLimit("sub-account"), credential.PermissionRead,
		commonexchange.OperationRead, options...,
	)
	if err != nil {
		return SubAccount{}, err
	}
	return decodeObject[SubAccount](client, response, commonexchange.OperationRead)
}

// SubAccountKeyPermission은 서브계정 API Key의 기능별 권한이다.
// Name은 wallet, spot, futures, delivery, earn, options, account, loan, margin, unified 등이다.
type SubAccountKeyPermission struct {
	Name     string `json:"name"`
	ReadOnly bool   `json:"read_only"`
}

// SubAccountKey는 서브계정 API Key 정보다. 생성 응답에만 Secret이 포함된다.
type SubAccountKey struct {
	UserID      int64                     `json:"user_id"`
	Mode        int                       `json:"mode"`
	Name        string                    `json:"name"`
	Perms       []SubAccountKeyPermission `json:"perms"`
	IPWhitelist []string                  `json:"ip_whitelist"`
	Key         string                    `json:"key"`
	Secret      string                    `json:"secret"`
	State       int                       `json:"state"`
	CreatedAt   int64                     `json:"created_at"`
	UpdatedAt   int64                     `json:"updated_at"`
	LastAccess  int64                     `json:"last_access"`
	Raw         json.RawMessage           `json:"-"`
}

func (key *SubAccountKey) setRaw(raw json.RawMessage) { key.Raw = raw }

func validateUserID(userID string) error {
	if !numericIDPattern.MatchString(userID) {
		return validationError("invalid sub-account user ID")
	}
	return nil
}

func validateAPIKey(key string) error {
	if key == "" || strings.ContainsAny(key, "/?#% ") {
		return validationError("invalid sub-account API key")
	}
	return nil
}

// SubAccountKeys는 서브계정의 API Key 목록을 조회한다. 권한은 Read다.
// GET /sub_accounts/{user_id}/keys
func (client *Client) SubAccountKeys(
	ctx context.Context,
	userID string,
	options ...trade.RequestOption,
) ([]SubAccountKey, error) {
	if err := validateUserID(userID); err != nil {
		return nil, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/sub_accounts/"+url.PathEscape(userID)+"/keys", nil, nil,
		privateLimit("sub-account-keys"), credential.PermissionRead,
		commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeList[SubAccountKey](client, response, commonexchange.OperationRead)
}

// SubAccountKeyRequest는 서브계정 API Key 생성·수정 본문이다. IPWhitelist를 비우면 목록이 지워진다.
type SubAccountKeyRequest struct {
	Name        string                    `json:"name,omitempty"`
	Perms       []SubAccountKeyPermission `json:"perms,omitempty"`
	IPWhitelist []string                  `json:"ip_whitelist,omitempty"`
}

func (request SubAccountKeyRequest) validate() error {
	for _, perm := range request.Perms {
		if strings.TrimSpace(perm.Name) == "" {
			return validationError("API key permission name is required")
		}
	}
	return nil
}

// CreateSubAccountKey는 서브계정 API Key를 생성한다. 권한은 Transfer이며 자동 재시도하지 않는다.
// 응답의 Secret은 이때만 확인할 수 있다.
// POST /sub_accounts/{user_id}/keys
func (client *Client) CreateSubAccountKey(
	ctx context.Context,
	userID string,
	request SubAccountKeyRequest,
	options ...trade.RequestOption,
) (SubAccountKey, error) {
	if err := validateUserID(userID); err != nil {
		return SubAccountKey{}, err
	}
	if err := request.validate(); err != nil {
		return SubAccountKey{}, err
	}
	body, err := encodeBody(request)
	if err != nil {
		return SubAccountKey{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodPost, "/sub_accounts/"+url.PathEscape(userID)+"/keys", nil, body,
		privateLimit("create-sub-account-key"), credential.PermissionTransfer,
		commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return SubAccountKey{}, err
	}
	return decodeObject[SubAccountKey](client, response, commonexchange.OperationMutation)
}

// SubAccountKeyInfo는 서브계정 API Key 한 건을 조회한다. 권한은 Read다.
// GET /sub_accounts/{user_id}/keys/{key}
func (client *Client) SubAccountKeyInfo(
	ctx context.Context,
	userID string,
	key string,
	options ...trade.RequestOption,
) (SubAccountKey, error) {
	if err := validateUserID(userID); err != nil {
		return SubAccountKey{}, err
	}
	if err := validateAPIKey(key); err != nil {
		return SubAccountKey{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet,
		"/sub_accounts/"+url.PathEscape(userID)+"/keys/"+url.PathEscape(key), nil, nil,
		privateLimit("sub-account-key"), credential.PermissionRead,
		commonexchange.OperationRead, options...,
	)
	if err != nil {
		return SubAccountKey{}, err
	}
	return decodeObject[SubAccountKey](client, response, commonexchange.OperationRead)
}

// UpdateSubAccountKey는 서브계정 API Key의 이름·권한·IP 허용 목록을 수정한다.
// 권한은 Transfer이며 자동 재시도하지 않는다. 성공 응답은 본문이 없다.
// PUT /sub_accounts/{user_id}/keys/{key}
func (client *Client) UpdateSubAccountKey(
	ctx context.Context,
	userID string,
	key string,
	request SubAccountKeyRequest,
	options ...trade.RequestOption,
) error {
	if err := validateUserID(userID); err != nil {
		return err
	}
	if err := validateAPIKey(key); err != nil {
		return err
	}
	if err := request.validate(); err != nil {
		return err
	}
	body, err := encodeBody(request)
	if err != nil {
		return err
	}
	response, err := client.executePrivate(
		ctx, http.MethodPut,
		"/sub_accounts/"+url.PathEscape(userID)+"/keys/"+url.PathEscape(key), nil, body,
		privateLimit("update-sub-account-key"), credential.PermissionTransfer,
		commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return err
	}
	return client.decodeEmpty(response, commonexchange.OperationMutation)
}

// DeleteSubAccountKey는 서브계정 API Key를 삭제한다. 권한은 Transfer이며 자동 재시도하지 않는다.
// 성공 응답은 본문이 없다.
// DELETE /sub_accounts/{user_id}/keys/{key}
func (client *Client) DeleteSubAccountKey(
	ctx context.Context,
	userID string,
	key string,
	options ...trade.RequestOption,
) error {
	if err := validateUserID(userID); err != nil {
		return err
	}
	if err := validateAPIKey(key); err != nil {
		return err
	}
	response, err := client.executePrivate(
		ctx, http.MethodDelete,
		"/sub_accounts/"+url.PathEscape(userID)+"/keys/"+url.PathEscape(key), nil, nil,
		privateLimit("delete-sub-account-key"), credential.PermissionTransfer,
		commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return err
	}
	return client.decodeEmpty(response, commonexchange.OperationMutation)
}

// LockSubAccount는 서브계정을 잠근다. 권한은 Transfer이며 자동 재시도하지 않는다.
// 성공 응답은 본문이 없다.
// POST /sub_accounts/{user_id}/lock
func (client *Client) LockSubAccount(
	ctx context.Context,
	userID string,
	options ...trade.RequestOption,
) error {
	return client.setSubAccountLock(ctx, userID, "lock", options...)
}

// UnlockSubAccount는 서브계정 잠금을 해제한다. 권한은 Transfer이며 자동 재시도하지 않는다.
// 성공 응답은 본문이 없다.
// POST /sub_accounts/{user_id}/unlock
func (client *Client) UnlockSubAccount(
	ctx context.Context,
	userID string,
	options ...trade.RequestOption,
) error {
	return client.setSubAccountLock(ctx, userID, "unlock", options...)
}

func (client *Client) setSubAccountLock(
	ctx context.Context,
	userID string,
	action string,
	options ...trade.RequestOption,
) error {
	if err := validateUserID(userID); err != nil {
		return err
	}
	response, err := client.executePrivate(
		ctx, http.MethodPost, "/sub_accounts/"+url.PathEscape(userID)+"/"+action, nil, nil,
		privateLimit(action+"-sub-account"), credential.PermissionTransfer,
		commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return err
	}
	return client.decodeEmpty(response, commonexchange.OperationMutation)
}

// SubUserMode는 서브계정의 통합계정 모드다.
// Mode는 classic, multi_currency, portfolio, single_currency 중 하나다.
type SubUserMode struct {
	UserID    int64           `json:"user_id"`
	IsUnified bool            `json:"is_unified"`
	Mode      string          `json:"mode"`
	Raw       json.RawMessage `json:"-"`
}

func (mode *SubUserMode) setRaw(raw json.RawMessage) { mode.Raw = raw }

// SubAccountModes는 서브계정의 통합계정 모드를 조회한다. 권한은 Read다.
// GET /sub_accounts/unified_mode
func (client *Client) SubAccountModes(
	ctx context.Context,
	options ...trade.RequestOption,
) ([]SubUserMode, error) {
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/sub_accounts/unified_mode", nil, nil,
		privateLimit("sub-account-modes"), credential.PermissionRead,
		commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeList[SubUserMode](client, response, commonexchange.OperationRead)
}

// SubAccountBalance는 서브계정의 통화별 사용 가능·잠금 잔고다.
type SubAccountBalance struct {
	UID       string            `json:"uid"`
	Available map[string]string `json:"available"`
	Locking   map[string]string `json:"locking"`
	Raw       json.RawMessage   `json:"-"`
}

func (balance *SubAccountBalance) setRaw(raw json.RawMessage) { balance.Raw = raw }

// SubAccountScopedBalance는 마진·선물·교차마진 서브계정 잔고다. 계정 종류별로 Available 구조가 다르므로
// 원본 JSON을 그대로 보존한다.
type SubAccountScopedBalance struct {
	UID       string          `json:"uid"`
	Available json.RawMessage `json:"available"`
	Raw       json.RawMessage `json:"-"`
}

func (balance *SubAccountScopedBalance) setRaw(raw json.RawMessage) { balance.Raw = raw }

// SubAccountBalancesRequest는 서브계정 잔고 조회 조건이다. SubUIDs를 비우면 전체 서브계정이다.
type SubAccountBalancesRequest struct{ SubUIDs []string }

func (request SubAccountBalancesRequest) validate() error {
	for _, uid := range request.SubUIDs {
		if !numericIDPattern.MatchString(uid) {
			return validationError("invalid sub-account user ID")
		}
	}
	return nil
}

func (request SubAccountBalancesRequest) values() url.Values {
	values := make(url.Values)
	if len(request.SubUIDs) > 0 {
		values.Set("sub_uid", strings.Join(request.SubUIDs, ","))
	}
	return values
}

// SubAccountBalances는 서브계정의 Spot 등 통합 지갑 잔고를 조회한다. 권한은 Read다.
// GET /wallet/sub_account_balances
func (client *Client) SubAccountBalances(
	ctx context.Context,
	request SubAccountBalancesRequest,
	options ...trade.RequestOption,
) ([]SubAccountBalance, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/wallet/sub_account_balances", request.values(), nil,
		privateLimit("sub-account-balances"), credential.PermissionRead,
		commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeList[SubAccountBalance](client, response, commonexchange.OperationRead)
}

func (client *Client) subAccountScopedBalances(
	ctx context.Context,
	path string,
	endpoint string,
	query url.Values,
	options ...trade.RequestOption,
) ([]SubAccountScopedBalance, error) {
	response, err := client.executePrivate(
		ctx, http.MethodGet, path, query, nil,
		privateLimit(endpoint), credential.PermissionRead,
		commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeList[SubAccountScopedBalance](client, response, commonexchange.OperationRead)
}

// SubAccountMarginBalances는 서브계정의 격리 마진 잔고를 조회한다. 권한은 Read다.
// GET /wallet/sub_account_margin_balances
func (client *Client) SubAccountMarginBalances(
	ctx context.Context,
	request SubAccountBalancesRequest,
	options ...trade.RequestOption,
) ([]SubAccountScopedBalance, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	return client.subAccountScopedBalances(
		ctx, "/wallet/sub_account_margin_balances", "sub-account-margin-balances",
		request.values(), options...,
	)
}

// SubAccountFuturesBalances는 서브계정의 선물 잔고를 조회한다. Settle을 지정하면 해당 정산 통화만 조회한다.
// 권한은 Read다.
// GET /wallet/sub_account_futures_balances
func (client *Client) SubAccountFuturesBalances(
	ctx context.Context,
	request SubAccountBalancesRequest,
	settle string,
	options ...trade.RequestOption,
) ([]SubAccountScopedBalance, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	if err := validateOptionalCurrency(strings.ToUpper(settle)); err != nil {
		return nil, err
	}
	query := request.values()
	if settle != "" {
		query.Set("settle", strings.ToLower(settle))
	}
	return client.subAccountScopedBalances(
		ctx, "/wallet/sub_account_futures_balances", "sub-account-futures-balances",
		query, options...,
	)
}

// SubAccountCrossMarginBalances는 서브계정의 교차마진 잔고를 조회한다. 권한은 Read다.
// GET /wallet/sub_account_cross_margin_balances
func (client *Client) SubAccountCrossMarginBalances(
	ctx context.Context,
	request SubAccountBalancesRequest,
	options ...trade.RequestOption,
) ([]SubAccountScopedBalance, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	return client.subAccountScopedBalances(
		ctx, "/wallet/sub_account_cross_margin_balances", "sub-account-cross-margin-balances",
		request.values(), options...,
	)
}
