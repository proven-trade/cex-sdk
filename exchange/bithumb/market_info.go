package bithumb

import (
	"context"
	"encoding/json"

	trade "github.com/proven-trade/cex-sdk"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

// WarningType은 경보제 유형이다.
type WarningType string

const (
	WarningTypePriceSuddenFluctuation         WarningType = "PRICE_SUDDEN_FLUCTUATION"
	WarningTypePriceDifferenceHigh            WarningType = "PRICE_DIFFERENCE_HIGH"
	WarningTypeSpecificAccountHighTransaction WarningType = "SPECIFIC_ACCOUNT_HIGH_TRANSACTION"
	WarningTypeTradingVolumeSuddenFluctuation WarningType = "TRADING_VOLUME_SUDDEN_FLUCTUATION"
	WarningTypeDepositAmountSuddenFluctuation WarningType = "DEPOSIT_AMOUNT_SUDDEN_FLUCTUATION"
)

// WarningStep은 경보제 단계다.
type WarningStep string

const (
	WarningStepCaution WarningStep = "CAUTION"
	WarningStepWarning WarningStep = "WARNING"
	WarningStepDanger  WarningStep = "DANGER"
)

// FeeCurrencyAll은 입출금 수수료 조회에서 전체 자산을 요청하는 화폐 심볼이다.
const FeeCurrencyAll = "ALL"

// VirtualAssetWarning은 경보제로 지정된 마켓의 경보 유형과 단계다.
// EndDate는 경보 종료 일시(KST, yyyy-MM-dd HH:mm:ss)다.
type VirtualAssetWarning struct {
	Market      string          `json:"market"`
	WarningType WarningType     `json:"warning_type"`
	WarningStep WarningStep     `json:"warning_step"`
	EndDate     string          `json:"end_date"`
	Raw         json.RawMessage `json:"-"`
}

// NoticesRequest는 거래소 공지사항 조회 조건이다. Count가 0이면 거래소 기본값 5건을 쓴다.
type NoticesRequest struct {
	Count int
}

// Notice는 거래소 공지사항 한 건이다. 게시 시각 최신순으로 반환된다.
// PublishedAt과 ModifiedAt은 yyyy-MM-dd HH:mm:ss 형식이다.
type Notice struct {
	Categories  []string        `json:"categories"`
	Title       string          `json:"title"`
	PCURL       string          `json:"pc_url"`
	PublishedAt string          `json:"published_at"`
	ModifiedAt  string          `json:"modified_at"`
	Raw         json.RawMessage `json:"-"`
}

// InOutFeesRequest는 자산별 입출금 수수료 조회 조건이다.
// Currency에 FeeCurrencyAll을 주면 전체 자산의 수수료를 반환한다.
type InOutFeesRequest struct {
	Currency string
}

// InOutFeeNetwork는 네트워크별 입출금 수수료와 최소 수량이다.
// 출금 수수료는 금액 기준(WithdrawFeeQuantity)이거나 정률 기준(WithdrawRate,
// 선택적으로 WithdrawFeeMin·WithdrawFeeMax)이며 적용되지 않는 필드는 빈 값이다.
// 모든 수량은 해당 출금 자산 단위다.
type InOutFeeNetwork struct {
	NetName                 string  `json:"net_name"`
	DepositFeeQuantity      Decimal `json:"deposit_fee_quantity"`
	DepositMinimumQuantity  Decimal `json:"deposit_minimum_quantity"`
	WithdrawFeeQuantity     Decimal `json:"withdraw_fee_quantity"`
	WithdrawRate            Decimal `json:"withdraw_rate"`
	WithdrawFeeMax          Decimal `json:"withdraw_fee_max"`
	WithdrawFeeMin          Decimal `json:"withdraw_fee_min"`
	WithdrawMinimumQuantity Decimal `json:"withdraw_minimum_quantity"`
}

// InOutFee는 자산 하나의 네트워크별 입출금 수수료다.
type InOutFee struct {
	Name     string            `json:"name"`
	Currency string            `json:"currency"`
	Networks []InOutFeeNetwork `json:"networks"`
	Raw      json.RawMessage   `json:"-"`
}

// VirtualAssetWarnings는 경보제(투자주의)로 지정된 마켓을 조회한다.
func (client *Client) VirtualAssetWarnings(
	ctx context.Context,
	options ...trade.RequestOption,
) ([]VirtualAssetWarning, error) {
	response, err := client.executePublic(ctx, "/v1/market/virtual_asset_warning", nil, rateLimitPublicOther, options...)
	if err != nil {
		return nil, err
	}
	return decodeItems(client, response, commonexchange.OperationRead, func(item *VirtualAssetWarning, raw []byte) {
		item.Raw = raw
	})
}

// Notices는 거래소 공지사항을 조회한다. 공식 제한은 IP별 초당 1회로 다른 공개 API와 분리해 관리한다.
func (client *Client) Notices(
	ctx context.Context,
	request NoticesRequest,
	options ...trade.RequestOption,
) ([]Notice, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePublic(ctx, "/v1/notices", request.parameters(), rateLimitPublicNotice, options...)
	if err != nil {
		return nil, err
	}
	return decodeItems(client, response, commonexchange.OperationRead, func(item *Notice, raw []byte) {
		item.Raw = raw
	})
}

// InOutFees는 자산별 입출금 수수료를 조회한다.
func (client *Client) InOutFees(
	ctx context.Context,
	request InOutFeesRequest,
	options ...trade.RequestOption,
) ([]InOutFee, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePublic(ctx, "/v2/fee/inout/"+request.Currency, nil, rateLimitPublicOther, options...)
	if err != nil {
		return nil, err
	}
	return decodeItems(client, response, commonexchange.OperationRead, func(item *InOutFee, raw []byte) {
		item.Raw = raw
	})
}

func (request NoticesRequest) validate() error {
	if request.Count < 0 || request.Count > 20 {
		return validationError("notice count must be between 1 and 20 or zero for default")
	}
	return nil
}

func (request NoticesRequest) parameters() parameters {
	values := parameters{}
	values.addInt("count", request.Count)
	return values
}

func (request InOutFeesRequest) validate() error {
	if !feeCurrencyPattern.MatchString(request.Currency) {
		return validationError("invalid fee currency %q", request.Currency)
	}
	return nil
}
