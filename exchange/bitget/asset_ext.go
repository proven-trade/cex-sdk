package bitget

import (
	"context"
	"encoding/json"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
)

// 이 파일의 자산·서브계정 메서드는 data 필드를 json.RawMessage로 반환한다.
// 공식 문서가 JS 렌더링이라 응답 필드와 열거값을 SDK 계약으로 고정하지 않고
// 공식 SDK와 Postman 컬렉션의 필드 이름을 그대로 사용한다.

// FundingAssets는 펀딩 계정 잔고를 조회한다. Coin은 선택이다.
// (GET /api/v3/account/funding-assets, Read 권한)
func (client *Client) FundingAssets(
	ctx context.Context,
	coin string,
	options ...trade.RequestOption,
) (json.RawMessage, error) {
	if err := validateOptionalText("coin", coin); err != nil {
		return nil, err
	}
	return client.readData(ctx, "/api/v3/account/funding-assets", queryValues("coin", coin), options...)
}

// TransferableCoins는 계정 유형 사이에서 이체 가능한 코인을 조회한다.
// (GET /api/v3/account/transferable-coins, Read 권한)
func (client *Client) TransferableCoins(
	ctx context.Context,
	fromType, toType string,
	options ...trade.RequestOption,
) (json.RawMessage, error) {
	if err := requireText("fromType", fromType); err != nil {
		return nil, err
	}
	if err := requireText("toType", toType); err != nil {
		return nil, err
	}
	return client.readData(
		ctx, "/api/v3/account/transferable-coins", queryValues("fromType", fromType, "toType", toType), options...,
	)
}

// MaxTransferable은 코인의 최대 이체 가능 수량을 조회한다.
// (GET /api/v3/account/max-transferable, Read 권한)
func (client *Client) MaxTransferable(
	ctx context.Context,
	coin string,
	options ...trade.RequestOption,
) (json.RawMessage, error) {
	if err := requireText("coin", coin); err != nil {
		return nil, err
	}
	return client.readData(ctx, "/api/v3/account/max-transferable", queryValues("coin", coin), options...)
}

// MaxWithdrawal은 코인의 최대 출금 가능 수량을 조회한다.
// (GET /api/v3/account/max-withdrawal, Read 권한)
func (client *Client) MaxWithdrawal(
	ctx context.Context,
	coin string,
	options ...trade.RequestOption,
) (json.RawMessage, error) {
	if err := requireText("coin", coin); err != nil {
		return nil, err
	}
	return client.readData(ctx, "/api/v3/account/max-withdrawal", queryValues("coin", coin), options...)
}

// TransferRequest는 내 계정 유형 사이의 이체 요청이다. FromType과 ToType은 Bitget 문서의 값을 그대로 전달한다.
type TransferRequest struct {
	FromType    string `json:"fromType"`
	ToType      string `json:"toType"`
	Amount      string `json:"amount"`
	Coin        string `json:"coin"`
	Symbol      string `json:"symbol,omitempty"`
	AllowBorrow string `json:"allowBorrow,omitempty"`
}

func validateTransferLeg(fromType, toType, amount, coin string) error {
	if err := requireText("fromType", fromType); err != nil {
		return err
	}
	if err := requireText("toType", toType); err != nil {
		return err
	}
	if err := requireText("coin", coin); err != nil {
		return err
	}
	return validatePositiveDecimal("amount", amount)
}

// Transfer는 내 계정 유형 사이에서 자산을 이체한다. 자동 재시도하지 않는다.
// (POST /api/v3/account/transfer, Transfer 권한)
func (client *Client) Transfer(
	ctx context.Context,
	request TransferRequest,
	options ...trade.RequestOption,
) (json.RawMessage, error) {
	if err := validateTransferLeg(request.FromType, request.ToType, request.Amount, request.Coin); err != nil {
		return nil, err
	}
	if err := validateOptionalText("symbol", request.Symbol); err != nil {
		return nil, err
	}
	return client.mutateRaw(ctx, "/api/v3/account/transfer", request, credential.PermissionTransfer, options...)
}

// SubTransferRequest는 마스터-서브 또는 서브-서브 계정 이체 요청이다.
type SubTransferRequest struct {
	FromType    string `json:"fromType"`
	ToType      string `json:"toType"`
	Amount      string `json:"amount"`
	Coin        string `json:"coin"`
	FromUserID  string `json:"fromUserId"`
	ToUserID    string `json:"toUserId"`
	ClientOID   string `json:"clientOid,omitempty"`
	AllowBorrow string `json:"allowBorrow,omitempty"`
}

// SubTransfer는 마스터와 서브계정 사이에서 자산을 이체한다. 자동 재시도하지 않는다.
// (POST /api/v3/account/sub-transfer, Transfer 권한)
func (client *Client) SubTransfer(
	ctx context.Context,
	request SubTransferRequest,
	options ...trade.RequestOption,
) (json.RawMessage, error) {
	if err := validateTransferLeg(request.FromType, request.ToType, request.Amount, request.Coin); err != nil {
		return nil, err
	}
	if err := requireText("fromUserId", request.FromUserID); err != nil {
		return nil, err
	}
	if err := requireText("toUserId", request.ToUserID); err != nil {
		return nil, err
	}
	if err := validateOptionalText("clientOid", request.ClientOID); err != nil {
		return nil, err
	}
	return client.mutateRaw(ctx, "/api/v3/account/sub-transfer", request, credential.PermissionTransfer, options...)
}

// SubMasterTransferRequest는 서브계정이 마스터 계정으로 보내는 이체 요청이다.
type SubMasterTransferRequest struct {
	FromType  string `json:"fromType"`
	ToType    string `json:"toType"`
	Amount    string `json:"amount"`
	Coin      string `json:"coin"`
	ClientOID string `json:"clientOid,omitempty"`
}

// SubMasterTransfer는 서브계정에서 마스터 계정으로 자산을 이체한다. 자동 재시도하지 않는다.
// (POST /api/v3/account/sub-master-transfer, Transfer 권한)
func (client *Client) SubMasterTransfer(
	ctx context.Context,
	request SubMasterTransferRequest,
	options ...trade.RequestOption,
) (json.RawMessage, error) {
	if err := validateTransferLeg(request.FromType, request.ToType, request.Amount, request.Coin); err != nil {
		return nil, err
	}
	if err := validateOptionalText("clientOid", request.ClientOID); err != nil {
		return nil, err
	}
	return client.mutateRaw(
		ctx, "/api/v3/account/sub-master-transfer", request, credential.PermissionTransfer, options...,
	)
}

// SubTransferRecordsRequest는 마스터-서브 이체 내역 조회 조건이다. Role은 Bitget 문서의 값을 그대로 전달한다.
type SubTransferRecordsRequest struct {
	SubUID    string
	Role      string
	Coin      string
	ClientOID string
	Page
}

// SubTransferRecords는 마스터-서브 계정 이체 내역을 조회한다.
// (GET /api/v3/account/sub-transfer-record, Read 권한)
func (client *Client) SubTransferRecords(
	ctx context.Context,
	request SubTransferRecordsRequest,
	options ...trade.RequestOption,
) (json.RawMessage, error) {
	for name, value := range map[string]string{
		"subUid": request.SubUID, "role": request.Role, "coin": request.Coin, "clientOid": request.ClientOID,
	} {
		if err := validateOptionalText(name, value); err != nil {
			return nil, err
		}
	}
	values := queryValues(
		"subUid", request.SubUID, "role", request.Role, "coin", request.Coin, "clientOid", request.ClientOID,
	)
	if err := request.Page.apply(values); err != nil {
		return nil, err
	}
	return client.readData(ctx, "/api/v3/account/sub-transfer-record", values, options...)
}

// SubUnifiedAssets는 서브계정 통합계정 자산을 조회한다. SubUID는 선택이다.
// (GET /api/v3/account/sub-unified-assets, Read 권한)
func (client *Client) SubUnifiedAssets(
	ctx context.Context,
	subUID string,
	page Page,
	options ...trade.RequestOption,
) (json.RawMessage, error) {
	if err := validateOptionalText("subUid", subUID); err != nil {
		return nil, err
	}
	values := queryValues("subUid", subUID)
	if page.StartTime != nil || page.EndTime != nil {
		return nil, validationError("sub unified assets does not accept a time range")
	}
	if err := page.apply(values); err != nil {
		return nil, err
	}
	return client.readData(ctx, "/api/v3/account/sub-unified-assets", values, options...)
}

// DepositAddress는 코인 입금 주소를 조회한다. Chain과 Size는 선택이다.
// (GET /api/v3/account/deposit-address, Read 권한)
func (client *Client) DepositAddress(
	ctx context.Context,
	coin, chain, size string,
	options ...trade.RequestOption,
) (json.RawMessage, error) {
	if err := requireText("coin", coin); err != nil {
		return nil, err
	}
	if err := validateOptionalText("chain", chain); err != nil {
		return nil, err
	}
	if err := validateOptionalText("size", size); err != nil {
		return nil, err
	}
	return client.readData(
		ctx, "/api/v3/account/deposit-address", queryValues("coin", coin, "chain", chain, "size", size), options...,
	)
}

// SubDepositAddress는 서브계정 코인 입금 주소를 조회한다. Chain과 Size는 선택이다.
// (GET /api/v3/account/sub-deposit-address, Read 권한)
func (client *Client) SubDepositAddress(
	ctx context.Context,
	subUID, coin, chain, size string,
	options ...trade.RequestOption,
) (json.RawMessage, error) {
	if err := requireText("subUid", subUID); err != nil {
		return nil, err
	}
	if err := requireText("coin", coin); err != nil {
		return nil, err
	}
	if err := validateOptionalText("chain", chain); err != nil {
		return nil, err
	}
	if err := validateOptionalText("size", size); err != nil {
		return nil, err
	}
	return client.readData(
		ctx,
		"/api/v3/account/sub-deposit-address",
		queryValues("subUid", subUID, "coin", coin, "chain", chain, "size", size),
		options...,
	)
}

// DepositRecordsRequest는 입금 내역 조회 조건이다.
type DepositRecordsRequest struct {
	Coin    string
	OrderID string
	Page
}

// DepositRecords는 입금 내역을 조회한다. (GET /api/v3/account/deposit-records, Read 권한)
func (client *Client) DepositRecords(
	ctx context.Context,
	request DepositRecordsRequest,
	options ...trade.RequestOption,
) (json.RawMessage, error) {
	if err := validateOptionalText("coin", request.Coin); err != nil {
		return nil, err
	}
	if err := validateOptionalText("orderId", request.OrderID); err != nil {
		return nil, err
	}
	values := queryValues("coin", request.Coin, "orderId", request.OrderID)
	if err := request.Page.apply(values); err != nil {
		return nil, err
	}
	return client.readData(ctx, "/api/v3/account/deposit-records", values, options...)
}

// SubDepositRecordsRequest는 서브계정 입금 내역 조회 조건이다.
type SubDepositRecordsRequest struct {
	SubUID string
	Coin   string
	Page
}

// SubDepositRecords는 서브계정 입금 내역을 조회한다.
// 공식 Go/Java SDK는 GET으로 정의한다(Postman 컬렉션은 POST로 표기).
// (GET /api/v3/account/sub-deposit-records, Read 권한)
func (client *Client) SubDepositRecords(
	ctx context.Context,
	request SubDepositRecordsRequest,
	options ...trade.RequestOption,
) (json.RawMessage, error) {
	if err := requireText("subUid", request.SubUID); err != nil {
		return nil, err
	}
	if err := validateOptionalText("coin", request.Coin); err != nil {
		return nil, err
	}
	values := queryValues("subUid", request.SubUID, "coin", request.Coin)
	if err := request.Page.apply(values); err != nil {
		return nil, err
	}
	return client.readData(ctx, "/api/v3/account/sub-deposit-records", values, options...)
}

// SetDepositAccount는 코인의 입금 대상 계정 유형을 설정한다.
// (POST /api/v3/account/deposit-account, Transfer 권한)
func (client *Client) SetDepositAccount(
	ctx context.Context,
	coin, accountType string,
	options ...trade.RequestOption,
) (json.RawMessage, error) {
	if err := requireText("coin", coin); err != nil {
		return nil, err
	}
	if err := requireText("accountType", accountType); err != nil {
		return nil, err
	}
	body := struct {
		Coin        string `json:"coin"`
		AccountType string `json:"accountType"`
	}{Coin: coin, AccountType: accountType}
	return client.mutateRaw(ctx, "/api/v3/account/deposit-account", body, credential.PermissionTransfer, options...)
}

// WithdrawalRequest는 출금 요청이다. 필드 이름과 의미는 Bitget 문서를 따른다.
type WithdrawalRequest struct {
	Coin         string `json:"coin"`
	Chain        string `json:"chain,omitempty"`
	TransferType string `json:"transferType"`
	Address      string `json:"address"`
	InnerToType  string `json:"innerToType,omitempty"`
	AreaCode     string `json:"areaCode,omitempty"`
	Tag          string `json:"tag,omitempty"`
	Size         string `json:"size"`
	Remark       string `json:"remark,omitempty"`
	ClientOID    string `json:"clientOid,omitempty"`
	MemberCode   string `json:"memberCode,omitempty"`
	IdentityType string `json:"identityType,omitempty"`
	CompanyName  string `json:"companyName,omitempty"`
	FirstName    string `json:"firstName,omitempty"`
	LastName     string `json:"lastName,omitempty"`
}

// Withdraw는 출금을 요청한다. 거래소 밖으로 자산을 보내므로 Withdraw 권한을 요구하며 자동 재시도하지 않는다.
// 전송 결과가 불명확하면 UNKNOWN_EXECUTION_STATE로 반환되므로 WithdrawalRecords로 ClientOID를 확인해야 한다.
// (POST /api/v3/account/withdrawal, Withdraw 권한)
func (client *Client) Withdraw(
	ctx context.Context,
	request WithdrawalRequest,
	options ...trade.RequestOption,
) (json.RawMessage, error) {
	if err := requireText("coin", request.Coin); err != nil {
		return nil, err
	}
	if err := requireText("transferType", request.TransferType); err != nil {
		return nil, err
	}
	if err := requireText("address", request.Address); err != nil {
		return nil, err
	}
	if err := validatePositiveDecimal("size", request.Size); err != nil {
		return nil, err
	}
	if request.ClientOID != "" && !clientOrderIDPattern.MatchString(request.ClientOID) {
		return nil, validationError("clientOid has an invalid format")
	}
	return client.mutateRaw(ctx, "/api/v3/account/withdrawal", request, credential.PermissionWithdraw, options...)
}

// CancelWithdrawal은 처리 전인 출금을 취소한다.
// (POST /api/v3/account/cancel-withdrawal, Withdraw 권한)
func (client *Client) CancelWithdrawal(
	ctx context.Context,
	orderID string,
	options ...trade.RequestOption,
) (json.RawMessage, error) {
	if err := requireText("orderId", orderID); err != nil {
		return nil, err
	}
	body := struct {
		OrderID string `json:"orderId"`
	}{OrderID: orderID}
	return client.mutateRaw(ctx, "/api/v3/account/cancel-withdrawal", body, credential.PermissionWithdraw, options...)
}

// WithdrawalRecordsRequest는 출금 내역 조회 조건이다.
type WithdrawalRecordsRequest struct {
	Coin      string
	OrderID   string
	ClientOID string
	Page
}

// WithdrawalRecords는 출금 내역을 조회한다. (GET /api/v3/account/withdrawal-records, Read 권한)
func (client *Client) WithdrawalRecords(
	ctx context.Context,
	request WithdrawalRecordsRequest,
	options ...trade.RequestOption,
) (json.RawMessage, error) {
	for name, value := range map[string]string{
		"coin": request.Coin, "orderId": request.OrderID, "clientOid": request.ClientOID,
	} {
		if err := validateOptionalText(name, value); err != nil {
			return nil, err
		}
	}
	values := queryValues("coin", request.Coin, "orderId", request.OrderID, "clientOid", request.ClientOID)
	if err := request.Page.apply(values); err != nil {
		return nil, err
	}
	return client.readData(ctx, "/api/v3/account/withdrawal-records", values, options...)
}

// WithdrawAddressesRequest는 출금 주소록 조회 조건이다. Type은 Bitget 문서의 값을 그대로 전달한다.
type WithdrawAddressesRequest struct {
	Coin string
	Type string
	Page
}

// WithdrawAddresses는 출금 주소록을 조회한다. (GET /api/v3/account/withdraw-address, Read 권한)
func (client *Client) WithdrawAddresses(
	ctx context.Context,
	request WithdrawAddressesRequest,
	options ...trade.RequestOption,
) (json.RawMessage, error) {
	if err := validateOptionalText("coin", request.Coin); err != nil {
		return nil, err
	}
	if err := validateOptionalText("type", request.Type); err != nil {
		return nil, err
	}
	values := queryValues("coin", request.Coin, "type", request.Type)
	if request.StartTime != nil || request.EndTime != nil {
		return nil, validationError("withdraw address list does not accept a time range")
	}
	if err := request.Page.apply(values); err != nil {
		return nil, err
	}
	return client.readData(ctx, "/api/v3/account/withdraw-address", values, options...)
}

// CreateSubAccountRequest는 서브계정 생성 요청이다. AccountMode는 Bitget 문서의 값을 그대로 전달한다.
type CreateSubAccountRequest struct {
	Username    string `json:"username"`
	AccountMode string `json:"accountMode,omitempty"`
	Note        string `json:"note,omitempty"`
}

// CreateSubAccount는 서브계정을 생성한다. (POST /api/v3/user/create-sub, Transfer 권한)
func (client *Client) CreateSubAccount(
	ctx context.Context,
	request CreateSubAccountRequest,
	options ...trade.RequestOption,
) (json.RawMessage, error) {
	if err := requireText("username", request.Username); err != nil {
		return nil, err
	}
	if err := validateOptionalText("accountMode", request.AccountMode); err != nil {
		return nil, err
	}
	return client.mutateRaw(ctx, "/api/v3/user/create-sub", request, credential.PermissionTransfer, options...)
}

// ModifySubAccount는 서브계정을 수정한다. body 필드는 Bitget 문서를 따르며 최소 하나의 필드가 필요하다.
// (POST /api/v3/user/modify-sub, Transfer 권한)
func (client *Client) ModifySubAccount(
	ctx context.Context,
	subUID string,
	fields map[string]string,
	options ...trade.RequestOption,
) (json.RawMessage, error) {
	if err := requireText("subUid", subUID); err != nil {
		return nil, err
	}
	if len(fields) == 0 {
		return nil, validationError("modify sub account requires at least one field")
	}
	body := make(map[string]string, len(fields)+1)
	for key, value := range fields {
		body[key] = value
	}
	body["subUid"] = subUID
	return client.mutateRaw(ctx, "/api/v3/user/modify-sub", body, credential.PermissionTransfer, options...)
}

// FreezeSubAccount는 서브계정을 동결하거나 해제한다. operation 값은 Bitget 문서의 값을 그대로 전달한다.
// (POST /api/v3/user/freeze-sub, Transfer 권한)
func (client *Client) FreezeSubAccount(
	ctx context.Context,
	subUID, operation string,
	options ...trade.RequestOption,
) (json.RawMessage, error) {
	if err := requireText("subUid", subUID); err != nil {
		return nil, err
	}
	if err := requireText("operation", operation); err != nil {
		return nil, err
	}
	body := struct {
		SubUID    string `json:"subUid"`
		Operation string `json:"operation"`
	}{SubUID: subUID, Operation: operation}
	return client.mutateRaw(ctx, "/api/v3/user/freeze-sub", body, credential.PermissionTransfer, options...)
}

// SubAccounts는 서브계정 목록을 cursor 기반으로 조회한다. (GET /api/v3/user/sub-list, Read 권한)
func (client *Client) SubAccounts(
	ctx context.Context,
	page Page,
	options ...trade.RequestOption,
) (json.RawMessage, error) {
	values := queryValues()
	if page.StartTime != nil || page.EndTime != nil {
		return nil, validationError("sub account list does not accept a time range")
	}
	if err := page.apply(values); err != nil {
		return nil, err
	}
	return client.readData(ctx, "/api/v3/user/sub-list", values, options...)
}

// CreateSubAPIKeyRequest는 서브계정 API Key 생성 요청이다.
type CreateSubAPIKeyRequest struct {
	SubUID     string `json:"subUid"`
	Note       string `json:"note,omitempty"`
	Type       string `json:"type,omitempty"`
	Passphrase string `json:"passphrase"`
	// Permissions와 IPs는 Bitget 문서 형식의 JSON 값을 그대로 전달한다(형식이 문서에서 확인되지 않음).
	Permissions json.RawMessage `json:"permissions,omitempty"`
	IPs         json.RawMessage `json:"ips,omitempty"`
}

// CreateSubAPIKey는 서브계정 API Key를 생성한다. 응답에 Secret이 포함될 수 있으므로 호출자가 보관·폐기해야 한다.
// (POST /api/v3/user/create-sub-api, Transfer 권한)
func (client *Client) CreateSubAPIKey(
	ctx context.Context,
	request CreateSubAPIKeyRequest,
	options ...trade.RequestOption,
) (json.RawMessage, error) {
	if err := requireText("subUid", request.SubUID); err != nil {
		return nil, err
	}
	if err := requireText("passphrase", request.Passphrase); err != nil {
		return nil, err
	}
	return client.mutateRaw(ctx, "/api/v3/user/create-sub-api", request, credential.PermissionTransfer, options...)
}

// UpdateSubAPIKeyRequest는 서브계정 API Key 수정 요청이다.
type UpdateSubAPIKeyRequest struct {
	APIKey     string `json:"apikey"`
	Passphrase string `json:"passphrase"`
	Type       string `json:"type,omitempty"`
	// Permissions와 IPs는 Bitget 문서 형식의 JSON 값을 그대로 전달한다(형식이 문서에서 확인되지 않음).
	Permissions json.RawMessage `json:"permissions,omitempty"`
	IPs         json.RawMessage `json:"ips,omitempty"`
}

// UpdateSubAPIKey는 서브계정 API Key 권한과 IP 화이트리스트를 수정한다.
// (POST /api/v3/user/update-sub-api, Transfer 권한)
func (client *Client) UpdateSubAPIKey(
	ctx context.Context,
	request UpdateSubAPIKeyRequest,
	options ...trade.RequestOption,
) (json.RawMessage, error) {
	if err := requireText("apikey", request.APIKey); err != nil {
		return nil, err
	}
	if err := requireText("passphrase", request.Passphrase); err != nil {
		return nil, err
	}
	return client.mutateRaw(ctx, "/api/v3/user/update-sub-api", request, credential.PermissionTransfer, options...)
}

// DeleteSubAPIKey는 서브계정 API Key를 삭제한다. (POST /api/v3/user/delete-sub-api, Transfer 권한)
func (client *Client) DeleteSubAPIKey(
	ctx context.Context,
	apiKey string,
	options ...trade.RequestOption,
) (json.RawMessage, error) {
	if err := requireText("apikey", apiKey); err != nil {
		return nil, err
	}
	body := struct {
		APIKey string `json:"apikey"`
	}{APIKey: apiKey}
	return client.mutateRaw(ctx, "/api/v3/user/delete-sub-api", body, credential.PermissionTransfer, options...)
}

// SubAPIKeys는 서브계정 API Key 목록을 조회한다. (GET /api/v3/user/sub-api-list, Read 권한)
func (client *Client) SubAPIKeys(
	ctx context.Context,
	subUID string,
	page Page,
	options ...trade.RequestOption,
) (json.RawMessage, error) {
	if err := requireText("subUid", subUID); err != nil {
		return nil, err
	}
	values := queryValues("subUid", subUID)
	if page.StartTime != nil || page.EndTime != nil {
		return nil, validationError("sub API key list does not accept a time range")
	}
	if err := page.apply(values); err != nil {
		return nil, err
	}
	return client.readData(ctx, "/api/v3/user/sub-api-list", values, options...)
}
