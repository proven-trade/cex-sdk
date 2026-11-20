package bybit

import (
	"context"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

var assetRequestIDPattern = regexp.MustCompile(`^[A-Za-z0-9]{1,32}$`)

// 전송·조회 한도는 Bybit 문서의 계정 단위 한도를 초당 값으로 환산한 것이다.
// 분당 한도(60/min, 100/min, 300/min)는 초당 1회·1회·5회로 내림해 보수적으로 적용한다.

// CoinInfoRequest는 코인·체인 정보 조회 조건이다. Coin을 비우면 전체 코인을 조회한다.
type CoinInfoRequest struct {
	Coin string
}

// CoinChainInfo는 코인의 체인별 입출금 설정이다.
// WithdrawFee가 비어 있으면 해당 코인은 출금을 지원하지 않는다.
type CoinChainInfo struct {
	Chain                 string `json:"chain"`
	ChainType             string `json:"chainType"`
	Confirmation          string `json:"confirmation"`
	WithdrawFee           string `json:"withdrawFee"`
	DepositMin            string `json:"depositMin"`
	WithdrawMin           string `json:"withdrawMin"`
	MinAccuracy           string `json:"minAccuracy"`
	ChainDeposit          string `json:"chainDeposit"`
	ChainWithdraw         string `json:"chainWithdraw"`
	WithdrawPercentageFee string `json:"withdrawPercentageFee"`
}

// CoinInfoRow는 코인 하나의 정보다.
type CoinInfoRow struct {
	Name         string          `json:"name"`
	Coin         string          `json:"coin"`
	RemainAmount string          `json:"remainAmount"`
	Chains       []CoinChainInfo `json:"chains"`
}

// CoinInfo는 코인 정보 조회 결과다.
type CoinInfo struct {
	Rows []CoinInfoRow `json:"rows"`
}

// InterTransferRequest는 같은 UID의 계정 유형 사이 이체 요청이다.
// TransferID는 호출자가 직접 생성한 UUID이며 멱등 식별자로 쓰인다.
type InterTransferRequest struct {
	TransferID      string `json:"transferId"`
	Coin            string `json:"coin"`
	Amount          string `json:"amount"`
	FromAccountType string `json:"fromAccountType"`
	ToAccountType   string `json:"toAccountType"`
}

// UniversalTransferRequest는 마스터-서브 또는 서브-서브 UID 사이 이체 요청이다.
// 같은 UID 사이에서는 이체할 수 없다.
type UniversalTransferRequest struct {
	TransferID      string `json:"transferId"`
	Coin            string `json:"coin"`
	Amount          string `json:"amount"`
	FromMemberID    int64  `json:"fromMemberId"`
	ToMemberID      int64  `json:"toMemberId"`
	FromAccountType string `json:"fromAccountType"`
	ToAccountType   string `json:"toAccountType"`
}

// TransferResult는 이체 접수 결과다. 최종 상태는 이체 내역 조회로 확인해야 한다.
type TransferResult struct {
	TransferID string `json:"transferId"`
}

// TransferRecordsRequest는 내부·유니버설 이체 내역 조회 조건이다.
// Status는 SUCCESS, FAILED, PENDING 중 하나다. Limit은 1~50이며 0이면 기본값 20이다.
// Bybit는 StartTime·EndTime을 초 단위로 처리한다.
type TransferRecordsRequest struct {
	TransferID string
	Coin       string
	Status     string
	StartTime  *time.Time
	EndTime    *time.Time
	Limit      int
	Cursor     string
}

// InterTransferRecord는 내부 이체 내역 한 건이다.
type InterTransferRecord struct {
	TransferID      string `json:"transferId"`
	Coin            string `json:"coin"`
	Amount          string `json:"amount"`
	FromAccountType string `json:"fromAccountType"`
	ToAccountType   string `json:"toAccountType"`
	Timestamp       string `json:"timestamp"`
	Status          string `json:"status"`
}

// InterTransferPage는 내부 이체 내역 한 페이지다.
type InterTransferPage struct {
	List           []InterTransferRecord `json:"list"`
	NextPageCursor string                `json:"nextPageCursor"`
}

// UniversalTransferRecord는 유니버설 이체 내역 한 건이다.
type UniversalTransferRecord struct {
	TransferID      string `json:"transferId"`
	Coin            string `json:"coin"`
	Amount          string `json:"amount"`
	FromMemberID    string `json:"fromMemberId"`
	ToMemberID      string `json:"toMemberId"`
	FromAccountType string `json:"fromAccountType"`
	ToAccountType   string `json:"toAccountType"`
	Timestamp       string `json:"timestamp"`
	Status          string `json:"status"`
}

// UniversalTransferPage는 유니버설 이체 내역 한 페이지다.
type UniversalTransferPage struct {
	List           []UniversalTransferRecord `json:"list"`
	NextPageCursor string                    `json:"nextPageCursor"`
}

// TransferableCoinsRequest는 두 계정 유형 사이에 이체 가능한 코인 조회 조건이다.
type TransferableCoinsRequest struct {
	FromAccountType string
	ToAccountType   string
}

// TransferableCoins는 이체 가능한 코인 목록이다.
type TransferableCoins struct {
	List []string `json:"list"`
}

// AccountCoinBalanceRequest는 단일 코인 잔고 조회 조건이다.
// 마스터 키로 서브 UID 잔고를 조회하려면 MemberID가, UID 사이 이체 가능 금액을 조회하려면 ToMemberID가 필요하다.
// WithLtvTransferSafeAmount를 쓰려면 ToAccountType이 필요하다.
type AccountCoinBalanceRequest struct {
	MemberID                  string
	ToMemberID                string
	AccountType               string
	ToAccountType             string
	Coin                      string
	WithBonus                 bool
	WithTransferSafeAmount    bool
	WithLtvTransferSafeAmount bool
}

// AssetCoinBalance는 코인 하나의 잔고다. 조회하지 않은 안전 이체 금액은 빈 문자열이다.
type AssetCoinBalance struct {
	Coin                  string `json:"coin"`
	WalletBalance         string `json:"walletBalance"`
	TransferBalance       string `json:"transferBalance"`
	Bonus                 string `json:"bonus"`
	TransferSafeAmount    string `json:"transferSafeAmount"`
	LtvTransferSafeAmount string `json:"ltvTransferSafeAmount"`
}

// AccountCoinBalance는 단일 코인 잔고 조회 결과다.
type AccountCoinBalance struct {
	AccountType string           `json:"accountType"`
	BizType     int              `json:"bizType"`
	AccountID   string           `json:"accountId"`
	MemberID    string           `json:"memberId"`
	Balance     AssetCoinBalance `json:"balance"`
}

// AccountCoinsBalanceRequest는 계정 유형별 코인 잔고 조회 조건이다.
// Coins를 비우면 전체 코인을 조회하지만 AccountType이 UNIFIED이면 1~10개가 필수다.
type AccountCoinsBalanceRequest struct {
	MemberID    string
	AccountType string
	Coins       []string
	WithBonus   bool
}

// AccountCoinsBalance는 계정 유형별 코인 잔고 목록이다.
type AccountCoinsBalance struct {
	AccountType string             `json:"accountType"`
	MemberID    string             `json:"memberId"`
	Balance     []AssetCoinBalance `json:"balance"`
}

// DepositRecordsRequest는 입금 내역 조회 조건이다. Limit은 1~50이며 0이면 기본값 50이다.
type DepositRecordsRequest struct {
	Coin      string
	StartTime *time.Time
	EndTime   *time.Time
	Limit     int
	Cursor    string
}

// SubDepositRecordsRequest는 서브 UID 입금 내역 조회 조건이다.
type SubDepositRecordsRequest struct {
	SubMemberID string
	DepositRecordsRequest
}

// DepositRecord는 온체인 입금 내역 한 건이다.
// Status와 DepositType의 값은 Bybit 문서의 depositStatus와 depositType 정의를 따른다.
type DepositRecord struct {
	Coin              string `json:"coin"`
	Chain             string `json:"chain"`
	Amount            string `json:"amount"`
	TxID              string `json:"txID"`
	Status            int    `json:"status"`
	ToAddress         string `json:"toAddress"`
	Tag               string `json:"tag"`
	DepositFee        string `json:"depositFee"`
	SuccessAt         string `json:"successAt"`
	Confirmations     string `json:"confirmations"`
	TxIndex           string `json:"txIndex"`
	BlockHash         string `json:"blockHash"`
	BatchReleaseLimit string `json:"batchReleaseLimit"`
	DepositType       int    `json:"depositType"`
}

// DepositRecordPage는 입금 내역 한 페이지다.
type DepositRecordPage struct {
	Rows           []DepositRecord `json:"rows"`
	NextPageCursor string          `json:"nextPageCursor"`
}

// InternalDepositRecordsRequest는 Bybit 내부 입금 내역 조회 조건이다.
// 시간 범위를 비우면 Bybit가 최근 30일로 처리한다. Limit은 1~50이며 0이면 기본값 50이다.
type InternalDepositRecordsRequest struct {
	TxID      string
	Coin      string
	StartTime *time.Time
	EndTime   *time.Time
	Limit     int
	Cursor    string
}

// InternalDepositRecord는 내부 입금 내역 한 건이다. Status는 1 처리 중, 2 성공, 3 실패다.
type InternalDepositRecord struct {
	ID          string `json:"id"`
	Type        int    `json:"type"`
	Coin        string `json:"coin"`
	Amount      string `json:"amount"`
	Status      int    `json:"status"`
	Address     string `json:"address"`
	CreatedTime string `json:"createdTime"`
	TxID        string `json:"txID"`
}

// InternalDepositRecordPage는 내부 입금 내역 한 페이지다.
type InternalDepositRecordPage struct {
	Rows           []InternalDepositRecord `json:"rows"`
	NextPageCursor string                  `json:"nextPageCursor"`
}

// DepositAddressRequest는 마스터 계정 입금 주소 조회 조건이다.
// ChainType에는 CoinInfo 응답의 chain 값을 쓴다. 비우면 코인의 모든 체인을 반환한다.
type DepositAddressRequest struct {
	Coin      string
	ChainType string
}

// SubDepositAddressRequest는 서브 UID 입금 주소 조회 조건이다.
type SubDepositAddressRequest struct {
	Coin        string
	ChainType   string
	SubMemberID string
}

// DepositAddressChain은 체인별 입금 주소다.
type DepositAddressChain struct {
	ChainType         string `json:"chainType"`
	AddressDeposit    string `json:"addressDeposit"`
	TagDeposit        string `json:"tagDeposit"`
	Chain             string `json:"chain"`
	BatchReleaseLimit string `json:"batchReleaseLimit"`
}

// DepositAddress는 코인의 입금 주소 조회 결과다.
type DepositAddress struct {
	Coin   string                `json:"coin"`
	Chains []DepositAddressChain `json:"chains"`
}

// WithdrawRecordsRequest는 출금 내역 조회 조건이다.
// WithdrawType은 0 온체인(기본), 1 오프체인, 2 전체이며 nil이면 보내지 않는다.
// Limit은 1~50이며 0이면 기본값 50이다.
type WithdrawRecordsRequest struct {
	WithdrawID   string
	TxID         string
	Coin         string
	WithdrawType *int
	StartTime    *time.Time
	EndTime      *time.Time
	Limit        int
	Cursor       string
}

// WithdrawRecord는 출금 내역 한 건이다.
// Status는 Bybit 문서의 withdrawStatus 문자열(SecurityCheck, Pending, success 등)이다.
type WithdrawRecord struct {
	WithdrawID   string `json:"withdrawId"`
	TxID         string `json:"txID"`
	WithdrawType int    `json:"withdrawType"`
	Coin         string `json:"coin"`
	Chain        string `json:"chain"`
	Amount       string `json:"amount"`
	WithdrawFee  string `json:"withdrawFee"`
	Status       string `json:"status"`
	ToAddress    string `json:"toAddress"`
	Tag          string `json:"tag"`
	CreateTime   string `json:"createTime"`
	UpdateTime   string `json:"updateTime"`
}

// WithdrawRecordPage는 출금 내역 한 페이지다.
type WithdrawRecordPage struct {
	Rows           []WithdrawRecord `json:"rows"`
	NextPageCursor string           `json:"nextPageCursor"`
}

// WithdrawableWallet은 지갑 하나의 출금 가능 금액이다.
type WithdrawableWallet struct {
	Coin               string `json:"coin"`
	WithdrawableAmount string `json:"withdrawableAmount"`
	AvailableBalance   string `json:"availableBalance"`
}

// WithdrawableAmount는 코인의 지갑별 출금 가능 금액이다.
// Wallets의 키는 SPOT, FUND, UTA, EARN이며 코인이 지원하지 않는 지갑은 포함되지 않는다.
type WithdrawableAmount struct {
	LimitAmountUSD string                        `json:"limitAmountUsd"`
	Wallets        map[string]WithdrawableWallet `json:"withdrawableAmount"`
}

// WithdrawRequest는 출금 요청이다.
// ForceChain이 0(기본)이면 내부 주소는 내부 이체, 1이면 온체인 강제, 2이면 Address에 Bybit UID를 쓰며,
// 2가 아니면 Chain이 필수다. 출금 주소는 Bybit 주소록에 미리 등록되어 있어야 한다.
// Timestamp가 0이면 클라이언트 시각(서버 시각 보정 포함)을 쓴다.
// FeeType이 0이면 Amount가 실수령액이고 1이면 수수료가 Amount에서 차감된다.
// RequestID는 멱등 검증용 영문·숫자 1~32자 식별자다. AccountType은 SPOT(기본) 또는 FUND다.
type WithdrawRequest struct {
	Coin        string `json:"coin"`
	Chain       string `json:"chain,omitempty"`
	Address     string `json:"address"`
	Tag         string `json:"tag,omitempty"`
	Amount      string `json:"amount"`
	Timestamp   int64  `json:"timestamp"`
	ForceChain  int    `json:"forceChain,omitempty"`
	AccountType string `json:"accountType,omitempty"`
	FeeType     int    `json:"feeType,omitempty"`
	RequestID   string `json:"requestId,omitempty"`
}

// WithdrawResult는 출금 접수 결과다. 최종 상태는 WithdrawRecords로 확인해야 한다.
type WithdrawResult struct {
	ID string `json:"id"`
}

// CancelWithdrawResult는 출금 취소 결과다. Status는 0 실패, 1 성공이다.
type CancelWithdrawResult struct {
	Status int `json:"status"`
}

// Succeeded는 출금 취소가 성공했는지 반환한다.
func (result CancelWithdrawResult) Succeeded() bool {
	return result.Status == 1
}

// ConvertCoinsRequest는 코인 전환 가능 코인 조회 조건이다.
// AccountType은 eb_convert_funding, eb_convert_uta, eb_convert_spot, eb_convert_contract, eb_convert_inverse 중 하나다.
// Side가 0이면 매도 코인 목록(잔고 포함, Coin 무시), 1이면 매수 코인 목록이며 Coin을 지정하면 그 코인에서 전환 가능한 목록이다.
type ConvertCoinsRequest struct {
	AccountType string
	Coin        string
	Side        *int
}

// ConvertCoin은 전환 가능 코인 한 건이다. 예약 필드는 포함하지 않는다.
type ConvertCoin struct {
	Coin              string `json:"coin"`
	FullName          string `json:"fullName"`
	Icon              string `json:"icon"`
	IconNight         string `json:"iconNight"`
	AccuracyLength    int    `json:"accuracyLength"`
	CoinType          string `json:"coinType"`
	Balance           string `json:"balance"`
	UBalance          string `json:"uBalance"`
	SingleFromMinimum string `json:"singleFromMinLimit"`
	SingleFromMaximum string `json:"singleFromMaxLimit"`
	DisableFrom       bool   `json:"disableFrom"`
	DisableTo         bool   `json:"disableTo"`
}

// ConvertCoins는 전환 가능 코인 목록이다.
type ConvertCoins struct {
	Coins []ConvertCoin `json:"coins"`
}

// ConvertQuoteRequest는 코인 전환 견적 요청이다.
// RequestCoin을 비우면 FromCoin을 쓴다(현재 RequestCoin은 FromCoin과 같아야 한다).
// RequestAmount는 매도할 FromCoin 수량이다. 견적은 약 15초간 유효하다.
type ConvertQuoteRequest struct {
	AccountType   string `json:"accountType"`
	FromCoin      string `json:"fromCoin"`
	ToCoin        string `json:"toCoin"`
	RequestCoin   string `json:"requestCoin"`
	RequestAmount string `json:"requestAmount"`
	FromCoinType  string `json:"fromCoinType,omitempty"`
	ToCoinType    string `json:"toCoinType,omitempty"`
	ParamType     string `json:"paramType,omitempty"`
	ParamValue    string `json:"paramValue,omitempty"`
	RequestID     string `json:"requestId,omitempty"`
}

// ConvertQuote는 코인 전환 견적이다. QuoteTxID로 확정과 결과 조회를 한다.
type ConvertQuote struct {
	QuoteTxID    string `json:"quoteTxId"`
	ExchangeRate string `json:"exchangeRate"`
	FromCoin     string `json:"fromCoin"`
	FromCoinType string `json:"fromCoinType"`
	ToCoin       string `json:"toCoin"`
	ToCoinType   string `json:"toCoinType"`
	FromAmount   string `json:"fromAmount"`
	ToAmount     string `json:"toAmount"`
	ExpiredTime  string `json:"expiredTime"`
	RequestID    string `json:"requestId"`
}

// ConvertConfirmResult는 코인 전환 확정 접수 결과다.
// ExchangeStatus는 init, processing, success, failure 중 하나이며 최종 상태는 ConvertStatus로 확인해야 한다.
type ConvertConfirmResult struct {
	QuoteTxID      string `json:"quoteTxId"`
	ExchangeStatus string `json:"exchangeStatus"`
}

// ConvertStatusRequest는 코인 전환 결과 조회 조건이다.
type ConvertStatusRequest struct {
	QuoteTxID   string
	AccountType string
}

// ConvertRecord는 코인 전환 기록 한 건이다.
type ConvertRecord struct {
	AccountType    string `json:"accountType"`
	ExchangeTxID   string `json:"exchangeTxId"`
	UserID         string `json:"userId"`
	FromCoin       string `json:"fromCoin"`
	FromCoinType   string `json:"fromCoinType"`
	ToCoin         string `json:"toCoin"`
	ToCoinType     string `json:"toCoinType"`
	FromAmount     string `json:"fromAmount"`
	ToAmount       string `json:"toAmount"`
	ExchangeStatus string `json:"exchangeStatus"`
	ConvertRate    string `json:"convertRate"`
	CreatedAt      string `json:"createdAt"`
}

// ConvertHistoryRequest는 코인 전환 기록 조회 조건이다.
// AccountTypes를 비우면 모든 지갑 유형을 반환한다. Index는 1부터 시작하고 Limit은 최대 100이며 0이면 기본값 20이다.
type ConvertHistoryRequest struct {
	AccountTypes []string
	Index        int
	Limit        int
}

// ConvertHistory는 코인 전환 기록 목록이다.
type ConvertHistory struct {
	List []ConvertRecord `json:"list"`
}

// CoinInfo는 코인과 체인별 입출금 설정을 조회한다.
func (client *Client) CoinInfo(
	ctx context.Context,
	request CoinInfoRequest,
	options ...trade.RequestOption,
) (CoinInfo, error) {
	if err := validateOptionalText("coin", request.Coin); err != nil {
		return CoinInfo{}, err
	}
	query := make(url.Values)
	setIfNotEmpty(query, "coin", request.Coin)
	return assetRead[CoinInfo](ctx, client, "/v5/asset/coin/query-info", query, accountLimit(5), options)
}

// CreateInterTransfer는 같은 UID의 계정 유형 사이에서 자산을 이체한다.
// 자동 재시도하지 않으며 결과가 불명확하면 같은 TransferID로 InterTransferRecords에서 확인해야 한다.
func (client *Client) CreateInterTransfer(
	ctx context.Context,
	request InterTransferRequest,
	options ...trade.RequestOption,
) (TransferResult, error) {
	if err := assetValidateTransfer(
		request.TransferID, request.Coin, request.Amount, request.FromAccountType, request.ToAccountType,
	); err != nil {
		return TransferResult{}, err
	}
	return assetMutate[TransferResult](
		ctx, client, "/v5/asset/transfer/inter-transfer", request, accountLimit(1),
		credential.PermissionTransfer, options,
	)
}

// InterTransferRecords는 내부 이체 내역을 cursor 기반으로 조회한다.
func (client *Client) InterTransferRecords(
	ctx context.Context,
	request TransferRecordsRequest,
	options ...trade.RequestOption,
) (InterTransferPage, error) {
	if err := request.validate(); err != nil {
		return InterTransferPage{}, err
	}
	return assetRead[InterTransferPage](
		ctx, client, "/v5/asset/transfer/query-inter-transfer-list", request.values(), accountLimit(1), options,
	)
}

// CreateUniversalTransfer는 마스터-서브 또는 서브-서브 UID 사이에서 자산을 이체한다.
// 자동 재시도하지 않으며 결과가 불명확하면 같은 TransferID로 UniversalTransferRecords에서 확인해야 한다.
func (client *Client) CreateUniversalTransfer(
	ctx context.Context,
	request UniversalTransferRequest,
	options ...trade.RequestOption,
) (TransferResult, error) {
	if err := assetValidateTransfer(
		request.TransferID, request.Coin, request.Amount, request.FromAccountType, request.ToAccountType,
	); err != nil {
		return TransferResult{}, err
	}
	if request.FromMemberID <= 0 || request.ToMemberID <= 0 {
		return TransferResult{}, validationError("fromMemberId and toMemberId must be positive")
	}
	if request.FromMemberID == request.ToMemberID {
		return TransferResult{}, validationError("universal transfer between the same UID is not allowed")
	}
	return assetMutate[TransferResult](
		ctx, client, "/v5/asset/transfer/universal-transfer", request, accountLimit(5),
		credential.PermissionTransfer, options,
	)
}

// UniversalTransferRecords는 유니버설 이체 내역을 cursor 기반으로 조회한다.
func (client *Client) UniversalTransferRecords(
	ctx context.Context,
	request TransferRecordsRequest,
	options ...trade.RequestOption,
) (UniversalTransferPage, error) {
	if err := request.validate(); err != nil {
		return UniversalTransferPage{}, err
	}
	return assetRead[UniversalTransferPage](
		ctx, client, "/v5/asset/transfer/query-universal-transfer-list", request.values(), accountLimit(5), options,
	)
}

// TransferableCoins는 두 계정 유형 사이에 이체 가능한 코인 목록을 조회한다.
func (client *Client) TransferableCoins(
	ctx context.Context,
	request TransferableCoinsRequest,
	options ...trade.RequestOption,
) (TransferableCoins, error) {
	if err := validateRequiredText("fromAccountType", request.FromAccountType); err != nil {
		return TransferableCoins{}, err
	}
	if err := validateRequiredText("toAccountType", request.ToAccountType); err != nil {
		return TransferableCoins{}, err
	}
	query := url.Values{}
	query.Set("fromAccountType", request.FromAccountType)
	query.Set("toAccountType", request.ToAccountType)
	return assetRead[TransferableCoins](
		ctx, client, "/v5/asset/transfer/query-transfer-coin-list", query, accountLimit(1), options,
	)
}

// AccountCoinBalance는 계정 유형 하나의 단일 코인 잔고를 조회한다. 마스터 키는 서브 UID 잔고도 조회할 수 있다.
func (client *Client) AccountCoinBalance(
	ctx context.Context,
	request AccountCoinBalanceRequest,
	options ...trade.RequestOption,
) (AccountCoinBalance, error) {
	if err := request.validate(); err != nil {
		return AccountCoinBalance{}, err
	}
	return assetRead[AccountCoinBalance](
		ctx, client, "/v5/asset/transfer/query-account-coin-balance", request.values(), accountLimit(450), options,
	)
}

// AccountCoinsBalance는 계정 유형 하나의 여러 코인 잔고를 조회한다.
func (client *Client) AccountCoinsBalance(
	ctx context.Context,
	request AccountCoinsBalanceRequest,
	options ...trade.RequestOption,
) (AccountCoinsBalance, error) {
	if err := request.validate(); err != nil {
		return AccountCoinsBalance{}, err
	}
	return assetRead[AccountCoinsBalance](
		ctx, client, "/v5/asset/transfer/query-account-coins-balance", request.values(), accountLimit(5), options,
	)
}

// DepositRecords는 온체인 입금 내역을 cursor 기반으로 조회한다.
func (client *Client) DepositRecords(
	ctx context.Context,
	request DepositRecordsRequest,
	options ...trade.RequestOption,
) (DepositRecordPage, error) {
	if err := request.validate(); err != nil {
		return DepositRecordPage{}, err
	}
	return assetRead[DepositRecordPage](
		ctx, client, "/v5/asset/deposit/query-record", request.values(), accountLimit(1), options,
	)
}

// SubDepositRecords는 서브 UID의 온체인 입금 내역을 cursor 기반으로 조회한다. 마스터 키가 필요하다.
func (client *Client) SubDepositRecords(
	ctx context.Context,
	request SubDepositRecordsRequest,
	options ...trade.RequestOption,
) (DepositRecordPage, error) {
	if err := validateRequiredText("subMemberId", request.SubMemberID); err != nil {
		return DepositRecordPage{}, err
	}
	if err := request.validate(); err != nil {
		return DepositRecordPage{}, err
	}
	query := request.values()
	query.Set("subMemberId", request.SubMemberID)
	return assetRead[DepositRecordPage](
		ctx, client, "/v5/asset/deposit/query-sub-member-record", query, accountLimit(5), options,
	)
}

// InternalDepositRecords는 Bybit 내부 입금 내역을 cursor 기반으로 조회한다.
func (client *Client) InternalDepositRecords(
	ctx context.Context,
	request InternalDepositRecordsRequest,
	options ...trade.RequestOption,
) (InternalDepositRecordPage, error) {
	if err := request.validate(); err != nil {
		return InternalDepositRecordPage{}, err
	}
	return assetRead[InternalDepositRecordPage](
		ctx, client, "/v5/asset/deposit/query-internal-record", request.values(), accountLimit(300), options,
	)
}

// DepositAddress는 마스터 계정의 입금 주소를 조회한다.
func (client *Client) DepositAddress(
	ctx context.Context,
	request DepositAddressRequest,
	options ...trade.RequestOption,
) (DepositAddress, error) {
	if err := validateRequiredText("coin", request.Coin); err != nil {
		return DepositAddress{}, err
	}
	if err := validateOptionalText("chainType", request.ChainType); err != nil {
		return DepositAddress{}, err
	}
	query := url.Values{}
	query.Set("coin", request.Coin)
	setIfNotEmpty(query, "chainType", request.ChainType)
	return assetRead[DepositAddress](ctx, client, "/v5/asset/deposit/query-address", query, accountLimit(5), options)
}

// SubDepositAddress는 서브 UID의 입금 주소를 조회한다. 마스터 키가 필요하다.
func (client *Client) SubDepositAddress(
	ctx context.Context,
	request SubDepositAddressRequest,
	options ...trade.RequestOption,
) (DepositAddress, error) {
	for _, field := range []struct{ name, value string }{
		{"coin", request.Coin}, {"chainType", request.ChainType}, {"subMemberId", request.SubMemberID},
	} {
		if err := validateRequiredText(field.name, field.value); err != nil {
			return DepositAddress{}, err
		}
	}
	query := url.Values{}
	query.Set("coin", request.Coin)
	query.Set("chainType", request.ChainType)
	query.Set("subMemberId", request.SubMemberID)
	return assetRead[DepositAddress](
		ctx, client, "/v5/asset/deposit/query-sub-member-address", query, accountLimit(5), options,
	)
}

// WithdrawRecords는 출금 내역을 cursor 기반으로 조회한다.
func (client *Client) WithdrawRecords(
	ctx context.Context,
	request WithdrawRecordsRequest,
	options ...trade.RequestOption,
) (WithdrawRecordPage, error) {
	if err := request.validate(); err != nil {
		return WithdrawRecordPage{}, err
	}
	return assetRead[WithdrawRecordPage](
		ctx, client, "/v5/asset/withdraw/query-record", request.values(), accountLimit(5), options,
	)
}

// WithdrawableAmount는 코인의 지갑별 출금 가능 금액을 조회한다.
func (client *Client) WithdrawableAmount(
	ctx context.Context,
	coin string,
	options ...trade.RequestOption,
) (WithdrawableAmount, error) {
	if err := validateRequiredText("coin", coin); err != nil {
		return WithdrawableAmount{}, err
	}
	query := url.Values{}
	query.Set("coin", coin)
	return assetRead[WithdrawableAmount](
		ctx, client, "/v5/asset/withdraw/withdrawable-amount", query, accountLimit(300), options,
	)
}

// CreateWithdraw는 출금을 요청한다. credential.PermissionWithdraw가 필요하다.
// 성공 응답은 접수이며 최종 상태는 WithdrawRecords로 확인해야 한다. 자동 재시도하지 않는다.
// 전송 결과가 불명확하면 같은 RequestID로 WithdrawRecords에서 확인해야 한다.
func (client *Client) CreateWithdraw(
	ctx context.Context,
	request WithdrawRequest,
	options ...trade.RequestOption,
) (WithdrawResult, error) {
	if err := request.validate(); err != nil {
		return WithdrawResult{}, err
	}
	if request.Timestamp == 0 {
		request.Timestamp = client.now().UnixMilli() + client.clockOffsetMillis.Load()
	}
	return assetMutate[WithdrawResult](
		ctx, client, "/v5/asset/withdraw/create", request, accountLimit(5), credential.PermissionWithdraw, options,
	)
}

// CancelWithdraw는 처리 전 출금을 취소한다. credential.PermissionWithdraw가 필요하다.
// Status가 1이 아니면 취소되지 않은 것이므로 CancelWithdrawResult.Succeeded로 확인해야 한다.
func (client *Client) CancelWithdraw(
	ctx context.Context,
	withdrawID string,
	options ...trade.RequestOption,
) (CancelWithdrawResult, error) {
	if err := validateRequiredText("id", withdrawID); err != nil {
		return CancelWithdrawResult{}, err
	}
	return assetMutate[CancelWithdrawResult](
		ctx, client, "/v5/asset/withdraw/cancel", struct {
			ID string `json:"id"`
		}{ID: withdrawID}, accountLimit(1), credential.PermissionWithdraw, options,
	)
}

// ConvertCoins는 코인 전환에 쓸 수 있는 코인 목록을 조회한다.
func (client *Client) ConvertCoins(
	ctx context.Context,
	request ConvertCoinsRequest,
	options ...trade.RequestOption,
) (ConvertCoins, error) {
	if err := validateRequiredText("accountType", request.AccountType); err != nil {
		return ConvertCoins{}, err
	}
	if err := validateOptionalText("coin", request.Coin); err != nil {
		return ConvertCoins{}, err
	}
	query := url.Values{}
	query.Set("accountType", request.AccountType)
	setIfNotEmpty(query, "coin", request.Coin)
	if request.Side != nil {
		if *request.Side != 0 && *request.Side != 1 {
			return ConvertCoins{}, validationError("side must be 0 or 1")
		}
		query.Set("side", strconv.Itoa(*request.Side))
	}
	return assetRead[ConvertCoins](ctx, client, "/v5/asset/exchange/query-coin-list", query, accountLimit(30), options)
}

// RequestConvertQuote는 코인 전환 견적을 요청한다. 견적 요청은 자산을 이동하지 않지만 Transfer 권한이 필요하다.
func (client *Client) RequestConvertQuote(
	ctx context.Context,
	request ConvertQuoteRequest,
	options ...trade.RequestOption,
) (ConvertQuote, error) {
	if request.RequestCoin == "" {
		request.RequestCoin = request.FromCoin
	}
	for _, field := range []struct{ name, value string }{
		{"accountType", request.AccountType}, {"fromCoin", request.FromCoin},
		{"toCoin", request.ToCoin}, {"requestCoin", request.RequestCoin},
	} {
		if err := validateRequiredText(field.name, field.value); err != nil {
			return ConvertQuote{}, err
		}
	}
	if err := validatePositiveDecimal("requestAmount", request.RequestAmount); err != nil {
		return ConvertQuote{}, err
	}
	for _, field := range []struct{ name, value string }{
		{"fromCoinType", request.FromCoinType}, {"toCoinType", request.ToCoinType},
		{"paramType", request.ParamType}, {"paramValue", request.ParamValue}, {"requestId", request.RequestID},
	} {
		if err := validateOptionalText(field.name, field.value); err != nil {
			return ConvertQuote{}, err
		}
	}
	if len(request.RequestID) > 36 {
		return ConvertQuote{}, validationError("requestId cannot exceed 36 characters")
	}
	return assetMutate[ConvertQuote](
		ctx, client, "/v5/asset/exchange/quote-apply", request, accountLimit(20),
		credential.PermissionTransfer, options,
	)
}

// ConfirmConvertQuote는 견적을 확정해 코인 전환을 실행한다.
// 응답은 접수이며 최종 상태는 ConvertStatus로 확인해야 한다. 자동 재시도하지 않는다.
func (client *Client) ConfirmConvertQuote(
	ctx context.Context,
	quoteTxID string,
	options ...trade.RequestOption,
) (ConvertConfirmResult, error) {
	if err := validateRequiredText("quoteTxId", quoteTxID); err != nil {
		return ConvertConfirmResult{}, err
	}
	return assetMutate[ConvertConfirmResult](
		ctx, client, "/v5/asset/exchange/convert-execute", struct {
			QuoteTxID string `json:"quoteTxId"`
		}{QuoteTxID: quoteTxID}, accountLimit(20), credential.PermissionTransfer, options,
	)
}

// ConvertStatus는 견적 ID로 코인 전환 결과를 조회한다.
func (client *Client) ConvertStatus(
	ctx context.Context,
	request ConvertStatusRequest,
	options ...trade.RequestOption,
) (ConvertRecord, error) {
	if err := validateRequiredText("quoteTxId", request.QuoteTxID); err != nil {
		return ConvertRecord{}, err
	}
	if err := validateRequiredText("accountType", request.AccountType); err != nil {
		return ConvertRecord{}, err
	}
	query := url.Values{}
	query.Set("quoteTxId", request.QuoteTxID)
	query.Set("accountType", request.AccountType)
	wrapper, err := assetRead[struct {
		Result ConvertRecord `json:"result"`
	}](ctx, client, "/v5/asset/exchange/convert-result-query", query, accountLimit(50), options)
	return wrapper.Result, err
}

// ConvertHistory는 코인 전환 기록을 조회한다.
func (client *Client) ConvertHistory(
	ctx context.Context,
	request ConvertHistoryRequest,
	options ...trade.RequestOption,
) (ConvertHistory, error) {
	if request.Index < 0 {
		return ConvertHistory{}, validationError("index cannot be negative")
	}
	if request.Limit < 0 || request.Limit > 100 {
		return ConvertHistory{}, validationError("limit must be between 1 and 100 or zero for default")
	}
	for _, accountType := range request.AccountTypes {
		if err := validateRequiredText("accountType", accountType); err != nil {
			return ConvertHistory{}, err
		}
	}
	query := url.Values{}
	setIfNotEmpty(query, "accountType", strings.Join(request.AccountTypes, ","))
	setPositiveInt(query, "index", request.Index)
	setPositiveInt(query, "limit", request.Limit)
	return assetRead[ConvertHistory](
		ctx, client, "/v5/asset/exchange/query-convert-history", query, accountLimit(50), options,
	)
}

// assetRead는 서명된 GET 조회를 보내고 result를 T로 디코딩한다.
func assetRead[T any](
	ctx context.Context,
	client *Client,
	path string,
	query url.Values,
	limit endpointLimit,
	options []trade.RequestOption,
) (T, error) {
	var decoded T
	response, result, err := client.executeTradeRead(ctx, path, query, limit, options...)
	if err != nil {
		return decoded, err
	}
	if err := decodeResult(result, &decoded); err != nil {
		var zero T
		return zero, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	return decoded, nil
}

// assetMutate는 서명된 POST 변경 요청을 한 번만 보내고 result를 T로 디코딩한다.
// 응답을 해석하지 못하면 실행 여부를 알 수 없으므로 UNKNOWN_EXECUTION_STATE로 반환한다.
func assetMutate[T any](
	ctx context.Context,
	client *Client,
	path string,
	payload any,
	limit endpointLimit,
	permission credential.Permission,
	options []trade.RequestOption,
) (T, error) {
	var decoded T
	response, result, err := client.executeTradeMutation(ctx, path, payload, limit, permission, options...)
	if err != nil {
		return decoded, err
	}
	if err := decodeResult(result, &decoded); err != nil {
		var zero T
		return zero, client.decodeBodyError(response, commonexchange.OperationMutation, err)
	}
	return decoded, nil
}

func assetValidateTransfer(transferID, coin, amount, fromAccountType, toAccountType string) error {
	for _, field := range []struct{ name, value string }{
		{"transferId", transferID}, {"coin", coin},
		{"fromAccountType", fromAccountType}, {"toAccountType", toAccountType},
	} {
		if err := validateRequiredText(field.name, field.value); err != nil {
			return err
		}
	}
	return validatePositiveDecimal("amount", amount)
}

// assetValidateWindow는 시간 범위의 순서와 페이지 크기·cursor를 검증한다.
func assetValidateWindow(start, end *time.Time, limit int, cursor string) error {
	if start != nil && end != nil && start.After(*end) {
		return validationError("start time cannot be after end time")
	}
	return validatePage(limit, 50, cursor)
}

func assetWindowValues(start, end *time.Time, limit int, cursor string) url.Values {
	values := make(url.Values)
	setTime(values, "startTime", start)
	setTime(values, "endTime", end)
	setPositiveInt(values, "limit", limit)
	setIfNotEmpty(values, "cursor", cursor)
	return values
}

func (request TransferRecordsRequest) validate() error {
	for _, field := range []struct{ name, value string }{
		{"transferId", request.TransferID}, {"coin", request.Coin},
	} {
		if err := validateOptionalText(field.name, field.value); err != nil {
			return err
		}
	}
	switch request.Status {
	case "", "SUCCESS", "FAILED", "PENDING":
	default:
		return validationError("status must be SUCCESS, FAILED or PENDING")
	}
	return assetValidateWindow(request.StartTime, request.EndTime, request.Limit, request.Cursor)
}

func (request TransferRecordsRequest) values() url.Values {
	values := assetWindowValues(request.StartTime, request.EndTime, request.Limit, request.Cursor)
	setIfNotEmpty(values, "transferId", request.TransferID)
	setIfNotEmpty(values, "coin", request.Coin)
	setIfNotEmpty(values, "status", request.Status)
	return values
}

func (request AccountCoinBalanceRequest) validate() error {
	for _, field := range []struct{ name, value string }{
		{"accountType", request.AccountType}, {"coin", request.Coin},
	} {
		if err := validateRequiredText(field.name, field.value); err != nil {
			return err
		}
	}
	for _, field := range []struct{ name, value string }{
		{"memberId", request.MemberID}, {"toMemberId", request.ToMemberID}, {"toAccountType", request.ToAccountType},
	} {
		if err := validateOptionalText(field.name, field.value); err != nil {
			return err
		}
	}
	if request.WithLtvTransferSafeAmount && request.ToAccountType == "" {
		return validationError("toAccountType is required with withLtvTransferSafeAmount")
	}
	return nil
}

func (request AccountCoinBalanceRequest) values() url.Values {
	values := url.Values{}
	values.Set("accountType", request.AccountType)
	values.Set("coin", request.Coin)
	setIfNotEmpty(values, "memberId", request.MemberID)
	setIfNotEmpty(values, "toMemberId", request.ToMemberID)
	setIfNotEmpty(values, "toAccountType", request.ToAccountType)
	assetSetFlag(values, "withBonus", request.WithBonus)
	assetSetFlag(values, "withTransferSafeAmount", request.WithTransferSafeAmount)
	assetSetFlag(values, "withLtvTransferSafeAmount", request.WithLtvTransferSafeAmount)
	return values
}

func (request AccountCoinsBalanceRequest) validate() error {
	if err := validateRequiredText("accountType", request.AccountType); err != nil {
		return err
	}
	if err := validateOptionalText("memberId", request.MemberID); err != nil {
		return err
	}
	for _, coin := range request.Coins {
		if err := validateRequiredText("coin", coin); err != nil {
			return err
		}
	}
	if len(request.Coins) > 10 {
		return validationError("at most 10 coins can be queried per request")
	}
	if request.AccountType == "UNIFIED" && len(request.Coins) == 0 {
		return validationError("coin is required for UNIFIED account type")
	}
	return nil
}

func (request AccountCoinsBalanceRequest) values() url.Values {
	values := url.Values{}
	values.Set("accountType", request.AccountType)
	setIfNotEmpty(values, "memberId", request.MemberID)
	setIfNotEmpty(values, "coin", strings.Join(request.Coins, ","))
	assetSetFlag(values, "withBonus", request.WithBonus)
	return values
}

func assetSetFlag(values url.Values, key string, enabled bool) {
	if enabled {
		values.Set(key, "1")
	}
}

func (request DepositRecordsRequest) validate() error {
	if err := validateOptionalText("coin", request.Coin); err != nil {
		return err
	}
	return assetValidateWindow(request.StartTime, request.EndTime, request.Limit, request.Cursor)
}

func (request DepositRecordsRequest) values() url.Values {
	values := assetWindowValues(request.StartTime, request.EndTime, request.Limit, request.Cursor)
	setIfNotEmpty(values, "coin", request.Coin)
	return values
}

func (request InternalDepositRecordsRequest) validate() error {
	for _, field := range []struct{ name, value string }{{"txID", request.TxID}, {"coin", request.Coin}} {
		if err := validateOptionalText(field.name, field.value); err != nil {
			return err
		}
	}
	return assetValidateWindow(request.StartTime, request.EndTime, request.Limit, request.Cursor)
}

func (request InternalDepositRecordsRequest) values() url.Values {
	values := assetWindowValues(request.StartTime, request.EndTime, request.Limit, request.Cursor)
	setIfNotEmpty(values, "txID", request.TxID)
	setIfNotEmpty(values, "coin", request.Coin)
	return values
}

func (request WithdrawRecordsRequest) validate() error {
	for _, field := range []struct{ name, value string }{
		{"withdrawID", request.WithdrawID}, {"txID", request.TxID}, {"coin", request.Coin},
	} {
		if err := validateOptionalText(field.name, field.value); err != nil {
			return err
		}
	}
	if request.WithdrawType != nil && (*request.WithdrawType < 0 || *request.WithdrawType > 2) {
		return validationError("withdrawType must be 0, 1 or 2")
	}
	return assetValidateWindow(request.StartTime, request.EndTime, request.Limit, request.Cursor)
}

func (request WithdrawRecordsRequest) values() url.Values {
	values := assetWindowValues(request.StartTime, request.EndTime, request.Limit, request.Cursor)
	setIfNotEmpty(values, "withdrawID", request.WithdrawID)
	setIfNotEmpty(values, "txID", request.TxID)
	setIfNotEmpty(values, "coin", request.Coin)
	if request.WithdrawType != nil {
		values.Set("withdrawType", strconv.Itoa(*request.WithdrawType))
	}
	return values
}

func (request WithdrawRequest) validate() error {
	for _, field := range []struct{ name, value string }{{"coin", request.Coin}, {"address", request.Address}} {
		if err := validateRequiredText(field.name, field.value); err != nil {
			return err
		}
	}
	for _, field := range []struct{ name, value string }{{"chain", request.Chain}, {"tag", request.Tag}} {
		if err := validateOptionalText(field.name, field.value); err != nil {
			return err
		}
	}
	if err := validatePositiveDecimal("amount", request.Amount); err != nil {
		return err
	}
	if request.Timestamp < 0 {
		return validationError("timestamp cannot be negative")
	}
	if request.ForceChain < 0 || request.ForceChain > 2 {
		return validationError("forceChain must be 0, 1 or 2")
	}
	if request.ForceChain != 2 && request.Chain == "" {
		return validationError("chain is required unless forceChain is 2")
	}
	if request.AccountType != "" && request.AccountType != "SPOT" && request.AccountType != "FUND" {
		return validationError("accountType must be SPOT or FUND")
	}
	if request.FeeType != 0 && request.FeeType != 1 {
		return validationError("feeType must be 0 or 1")
	}
	if request.RequestID != "" && !assetRequestIDPattern.MatchString(request.RequestID) {
		return validationError("requestId must be 1-32 letters or digits")
	}
	return nil
}
