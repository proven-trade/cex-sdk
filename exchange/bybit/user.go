package bybit

import (
	"context"
	"net/url"
	"strings"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
)

// 서브 계정 유형이다.
const (
	// SubMemberTypeNormal은 일반 서브 계정이다.
	SubMemberTypeNormal = 1
	// SubMemberTypeCustodial은 수탁(custodial) 서브 계정이다.
	SubMemberTypeCustodial = 6
)

// CreateSubMemberRequest는 서브 UID 생성 요청이다. 마스터 UID의 API Key로만 호출할 수 있다.
// Username은 영문과 숫자를 모두 포함한 6~16자이고 기존·삭제된 이름과 같을 수 없다.
// Password는 숫자와 대소문자를 포함한 8~30자이며 비우면 보내지 않는다.
// QuickLogin이 true이면 빠른 로그인을 켜고, IsUTA가 true이면 UTA 계정을, false이면 Classic 계정을 만든다.
type CreateSubMemberRequest struct {
	Username   string
	Password   string
	MemberType int
	QuickLogin bool
	IsUTA      bool
	Note       string
}

// SubMember는 서브 UID 정보다.
// Status는 1 정상, 2 로그인 금지, 4 동결이며 AccountMode는 1 Classic, 2 UMA, 3 UTA다.
// 서브 UID 생성 응답에는 AccountMode가 없어 0이다.
type SubMember struct {
	UID         string `json:"uid"`
	Username    string `json:"username"`
	MemberType  int    `json:"memberType"`
	Status      int    `json:"status"`
	AccountMode int    `json:"accountMode"`
	Remark      string `json:"remark"`
}

// SubMembers는 마스터 UID의 서브 UID 목록이다.
type SubMembers struct {
	SubMembers []SubMember `json:"subMembers"`
}

// APIKeyPermissions는 API Key 권한 그룹별 세부 권한이다.
// 생성·수정 요청에서는 비어 있는 그룹을 보내지 않는다.
// 사용 가능한 값은 ContractTrade(Order, Position), Spot(SpotTrade),
// Wallet(AccountTransfer, SubMemberTransferList), Options(OptionsTrade),
// Exchange(ExchangeHistory), CopyTrading(CopyTrading)이다.
// Derivatives는 계정 유형에 따라 Bybit가 자동으로 부여하므로 보내지 않는다.
// 응답에서는 BlockTrade, NFT, Affiliate도 채워진다.
type APIKeyPermissions struct {
	ContractTrade []string `json:"ContractTrade,omitempty"`
	Spot          []string `json:"Spot,omitempty"`
	Wallet        []string `json:"Wallet,omitempty"`
	Options       []string `json:"Options,omitempty"`
	Exchange      []string `json:"Exchange,omitempty"`
	CopyTrading   []string `json:"CopyTrading,omitempty"`
	// Derivatives, BlockTrade, NFT, Affiliate는 응답 전용이며 요청에서는 비워 둔다.
	Derivatives []string `json:"Derivatives,omitempty"`
	BlockTrade  []string `json:"BlockTrade,omitempty"`
	NFT         []string `json:"NFT,omitempty"`
	Affiliate   []string `json:"Affiliate,omitempty"`
}

func (permissions APIKeyPermissions) empty() bool {
	return len(permissions.ContractTrade) == 0 && len(permissions.Spot) == 0 && len(permissions.Wallet) == 0 &&
		len(permissions.Options) == 0 && len(permissions.Exchange) == 0 && len(permissions.CopyTrading) == 0
}

// requestPermissions는 요청에 허용된 그룹만 남긴 복사본을 반환한다.
func (permissions APIKeyPermissions) requestPermissions() APIKeyPermissions {
	return APIKeyPermissions{
		ContractTrade: permissions.ContractTrade, Spot: permissions.Spot, Wallet: permissions.Wallet,
		Options: permissions.Options, Exchange: permissions.Exchange, CopyTrading: permissions.CopyTrading,
	}
}

// CreateSubAPIKeyRequest는 서브 UID의 API Key 생성 요청이다. 마스터 UID의 API Key로만 호출할 수 있다.
// ReadOnly가 true이면 읽기 전용이다. IPs를 비우면 IP를 묶지 않으며 이 경우 키는 90일 뒤 만료된다.
// Permissions에는 최소 한 그룹의 권한이 필요하다.
type CreateSubAPIKeyRequest struct {
	SubUID      int64
	Note        string
	ReadOnly    bool
	IPs         []string
	Permissions APIKeyPermissions
}

// UpdateSubAPIKeyRequest는 서브 계정 API Key 수정 요청이다.
// 마스터 키로 호출할 때는 APIKey가 필수이고, 서브 키로 자기 자신을 수정할 때는 비워야 한다.
// ReadOnly, IPs, Permissions가 nil이면 해당 설정을 바꾸지 않는다.
// IPs를 비어 있지 않은 빈 슬라이스로 주면 IP 묶음을 해제("*")한다.
type UpdateSubAPIKeyRequest struct {
	APIKey      string
	ReadOnly    *bool
	IPs         []string
	Permissions *APIKeyPermissions
}

// SubAPIKey는 서브 UID API Key 생성·수정 결과다.
// Secret은 생성 응답에서만 한 번 반환되며 이후 조회할 수 없다. 수정 응답에서는 항상 빈 문자열이다.
// IPs는 수정 응답에서만 채워진다.
type SubAPIKey struct {
	ID          string            `json:"id"`
	Note        string            `json:"note"`
	APIKey      string            `json:"apiKey"`
	ReadOnly    int               `json:"readOnly"`
	Secret      string            `json:"secret"`
	Permissions APIKeyPermissions `json:"permissions"`
	IPs         []string          `json:"ips"`
}

// APIKeyInfo는 호출에 쓴 API Key의 정보다.
type APIKeyInfo struct {
	ID           string            `json:"id"`
	Note         string            `json:"note"`
	APIKey       string            `json:"apiKey"`
	ReadOnly     int               `json:"readOnly"`
	Permissions  APIKeyPermissions `json:"permissions"`
	IPs          []string          `json:"ips"`
	Type         int               `json:"type"`
	DeadlineDay  int               `json:"deadlineDay"`
	ExpiredAt    string            `json:"expiredAt"`
	CreatedAt    string            `json:"createdAt"`
	UTA          int               `json:"uta"`
	UserID       int64             `json:"userID"`
	InviterID    int64             `json:"inviterID"`
	VIPLevel     string            `json:"vipLevel"`
	MktMakerLvl  string            `json:"mktMakerLevel"`
	AffiliateID  int64             `json:"affiliateID"`
	RSAPublicKey string            `json:"rsaPublicKey"`
	IsMaster     bool              `json:"isMaster"`
	ParentUID    string            `json:"parentUid"`
	KYCLevel     string            `json:"kycLevel"`
	KYCRegion    string            `json:"kycRegion"`
}

// CreateSubMember는 서브 UID를 만든다. credential.PermissionTransfer가 필요하다.
// 자동 재시도하지 않는다. 결과가 불명확하면 QuerySubMembers로 생성 여부를 확인해야 한다.
func (client *Client) CreateSubMember(
	ctx context.Context,
	request CreateSubMemberRequest,
	options ...trade.RequestOption,
) (SubMember, error) {
	if err := request.validate(); err != nil {
		return SubMember{}, err
	}
	payload := struct {
		Username   string `json:"username"`
		Password   string `json:"password,omitempty"`
		MemberType int    `json:"memberType"`
		Switch     int    `json:"switch"`
		IsUTA      bool   `json:"isUta"`
		Note       string `json:"note,omitempty"`
	}{
		Username: request.Username, Password: request.Password, MemberType: request.MemberType,
		Switch: userBoolInt(request.QuickLogin), IsUTA: request.IsUTA, Note: request.Note,
	}
	return assetMutate[SubMember](
		ctx, client, "/v5/user/create-sub-member", payload, accountLimit(1), credential.PermissionTransfer, options,
	)
}

// CreateSubAPIKey는 서브 UID의 API Key를 만든다. credential.PermissionTransfer가 필요하다.
// 응답의 Secret은 이 호출에서만 확인할 수 있으므로 호출자가 안전하게 보관해야 한다.
// 자동 재시도하지 않는다.
func (client *Client) CreateSubAPIKey(
	ctx context.Context,
	request CreateSubAPIKeyRequest,
	options ...trade.RequestOption,
) (SubAPIKey, error) {
	if err := request.validate(); err != nil {
		return SubAPIKey{}, err
	}
	payload := struct {
		SubUID      int64             `json:"subuid"`
		Note        string            `json:"note,omitempty"`
		ReadOnly    int               `json:"readOnly"`
		IPs         string            `json:"ips,omitempty"`
		Permissions APIKeyPermissions `json:"permissions"`
	}{
		SubUID: request.SubUID, Note: request.Note, ReadOnly: userBoolInt(request.ReadOnly),
		IPs: strings.Join(request.IPs, ","), Permissions: request.Permissions.requestPermissions(),
	}
	return assetMutate[SubAPIKey](
		ctx, client, "/v5/user/create-sub-api", payload, accountLimit(1), credential.PermissionTransfer, options,
	)
}

// UpdateSubAPIKey는 서브 계정 API Key의 읽기 전용 여부, IP 묶음, 권한을 수정한다.
// credential.PermissionTransfer가 필요하다. 자동 재시도하지 않는다.
func (client *Client) UpdateSubAPIKey(
	ctx context.Context,
	request UpdateSubAPIKeyRequest,
	options ...trade.RequestOption,
) (SubAPIKey, error) {
	if err := request.validate(); err != nil {
		return SubAPIKey{}, err
	}
	payload := struct {
		APIKey      string             `json:"apikey,omitempty"`
		ReadOnly    *int               `json:"readOnly,omitempty"`
		IPs         *string            `json:"ips,omitempty"`
		Permissions *APIKeyPermissions `json:"permissions,omitempty"`
	}{APIKey: request.APIKey}
	if request.ReadOnly != nil {
		value := userBoolInt(*request.ReadOnly)
		payload.ReadOnly = &value
	}
	if request.IPs != nil {
		value := "*"
		if len(request.IPs) > 0 {
			value = strings.Join(request.IPs, ",")
		}
		payload.IPs = &value
	}
	if request.Permissions != nil {
		permissions := request.Permissions.requestPermissions()
		payload.Permissions = &permissions
	}
	return assetMutate[SubAPIKey](
		ctx, client, "/v5/user/update-sub-api", payload, accountLimit(5), credential.PermissionTransfer, options,
	)
}

// DeleteSubAPIKey는 서브 계정 API Key를 삭제한다. 삭제한 키는 즉시 무효가 된다.
// 마스터 키로 호출할 때는 apiKey가 필수이고, 서브 키로 자기 자신을 삭제할 때는 비워야 한다.
// credential.PermissionTransfer가 필요하다. 자동 재시도하지 않는다.
func (client *Client) DeleteSubAPIKey(
	ctx context.Context,
	apiKey string,
	options ...trade.RequestOption,
) (MutationAck, error) {
	if err := validateOptionalText("apikey", apiKey); err != nil {
		return MutationAck{}, err
	}
	payload := struct {
		APIKey string `json:"apikey,omitempty"`
	}{APIKey: apiKey}
	_, result, err := client.executeTradeMutation(
		ctx, "/v5/user/delete-sub-api", payload, accountLimit(5), credential.PermissionTransfer, options...,
	)
	if err != nil {
		return MutationAck{}, err
	}
	return MutationAck{Raw: cloneBytes(result)}, nil
}

// FreezeSubMember는 서브 UID를 동결(frozen=true)하거나 동결 해제(frozen=false)한다.
// 마스터 UID의 API Key로만 호출할 수 있으며 credential.PermissionTransfer가 필요하다. 자동 재시도하지 않는다.
func (client *Client) FreezeSubMember(
	ctx context.Context,
	subUID int64,
	frozen bool,
	options ...trade.RequestOption,
) (MutationAck, error) {
	if subUID <= 0 {
		return MutationAck{}, validationError("subuid must be positive")
	}
	payload := struct {
		SubUID int64 `json:"subuid"`
		Frozen int   `json:"frozen"`
	}{SubUID: subUID, Frozen: userBoolInt(frozen)}
	_, result, err := client.executeTradeMutation(
		ctx, "/v5/user/frozen-sub-member", payload, accountLimit(5), credential.PermissionTransfer, options...,
	)
	if err != nil {
		return MutationAck{}, err
	}
	return MutationAck{Raw: cloneBytes(result)}, nil
}

// QuerySubMembers는 마스터 UID의 서브 UID를 최대 1만 건까지 조회한다. 마스터 UID의 API Key로만 호출할 수 있다.
func (client *Client) QuerySubMembers(
	ctx context.Context,
	options ...trade.RequestOption,
) (SubMembers, error) {
	return assetRead[SubMembers](ctx, client, "/v5/user/query-sub-members", url.Values{}, accountLimit(10), options)
}

// QueryAPIKey는 요청에 쓴 API Key의 권한, IP 묶음, 만료일 같은 정보를 조회한다.
func (client *Client) QueryAPIKey(
	ctx context.Context,
	options ...trade.RequestOption,
) (APIKeyInfo, error) {
	return assetRead[APIKeyInfo](ctx, client, "/v5/user/query-api", url.Values{}, accountLimit(10), options)
}

func userBoolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func (request CreateSubMemberRequest) validate() error {
	if err := validateRequiredText("username", request.Username); err != nil {
		return err
	}
	if length := len(request.Username); length < 6 || length > 16 {
		return validationError("username must be 6-16 characters")
	}
	if request.Password != "" {
		if length := len(request.Password); length < 8 || length > 30 {
			return validationError("password must be 8-30 characters")
		}
	}
	if request.MemberType != SubMemberTypeNormal && request.MemberType != SubMemberTypeCustodial {
		return validationError("memberType must be 1 (normal) or 6 (custodial)")
	}
	return validateOptionalText("note", request.Note)
}

func (request CreateSubAPIKeyRequest) validate() error {
	if request.SubUID <= 0 {
		return validationError("subuid must be positive")
	}
	if err := validateOptionalText("note", request.Note); err != nil {
		return err
	}
	if err := validateUserIPs(request.IPs); err != nil {
		return err
	}
	if request.Permissions.empty() {
		return validationError("at least one permission group is required")
	}
	return nil
}

func (request UpdateSubAPIKeyRequest) validate() error {
	if err := validateOptionalText("apikey", request.APIKey); err != nil {
		return err
	}
	if err := validateUserIPs(request.IPs); err != nil {
		return err
	}
	if request.ReadOnly == nil && request.IPs == nil && request.Permissions == nil {
		return validationError("at least one of readOnly, ips or permissions is required")
	}
	if request.Permissions != nil && request.Permissions.empty() {
		return validationError("permissions cannot be empty; pass nil to keep the current permissions")
	}
	return nil
}

func validateUserIPs(ips []string) error {
	for _, ip := range ips {
		if err := validateRequiredText("ip", ip); err != nil {
			return err
		}
		if strings.Contains(ip, ",") {
			return validationError("each ip must be a single address")
		}
	}
	return nil
}
