package coinbase

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

const (
	ProductTypeFuture      ProductType = "FUTURE"
	ProductTypeEquity      ProductType = "EQUITY"
	ProductTypeOptionGroup ProductType = "OPTION_GROUP"
	ProductTypeFutureGroup ProductType = "FUTURE_GROUP"
)

// ContractExpiryType은 선물 계약의 만기 구분이다.
type ContractExpiryType string

const (
	ContractExpiryExpiring  ContractExpiryType = "EXPIRING"
	ContractExpiryPerpetual ContractExpiryType = "PERPETUAL"
)

// ProductVenue는 상품이 거래되는 장소다.
type ProductVenue string

const (
	ProductVenueCBE  ProductVenue = "CBE"
	ProductVenueFCM  ProductVenue = "FCM"
	ProductVenueINTX ProductVenue = "INTX"
)

// TransactionSummaryRequest는 수수료 등급·거래량 요약 조회 조건이다.
// 모든 필드가 비어 있으면 모든 상품 유형을 합산한다.
// ContractExpiryType은 ProductType이 FUTURE일 때만 의미가 있다.
type TransactionSummaryRequest struct {
	ProductType        ProductType
	ContractExpiryType ContractExpiryType
	ProductVenue       ProductVenue
}

// VolumeTypesAndRange는 여러 거래량 유형을 합쳐 평가하는 수수료 등급 구간이다.
type VolumeTypesAndRange struct {
	VolumeTypes []string `json:"volume_types"`
	VolFrom     string   `json:"vol_from"`
	VolTo       string   `json:"vol_to"`
}

// FeeTier는 한 수수료 등급의 maker·taker 요율과 구간이다.
type FeeTier struct {
	PricingTier         string                `json:"pricing_tier"`
	TakerFeeRate        string                `json:"taker_fee_rate"`
	MakerFeeRate        string                `json:"maker_fee_rate"`
	AOPFrom             string                `json:"aop_from"`
	AOPTo               string                `json:"aop_to"`
	VolumeTypesAndRange []VolumeTypesAndRange `json:"volume_types_and_range"`
}

// Decimal은 Coinbase가 {"value":"..."} 형태로 보내는 소수다.
type Decimal struct {
	Value string `json:"value"`
}

// GoodsAndServicesTax는 수수료에 적용되는 부가세 정보다.
type GoodsAndServicesTax struct {
	Rate string `json:"rate"`
	Type string `json:"type"`
}

// VolumeBreakdown은 수수료 등급 산정에 반영된 거래량 유형별 합계다.
type VolumeBreakdown struct {
	VolumeType string      `json:"volume_type"`
	Volume     json.Number `json:"volume"`
}

// FeeTierWithoutPromotion은 프로모션을 제외하고 거래량 또는 AOP만으로 산정한 수수료 등급이다.
type FeeTierWithoutPromotion struct {
	PricingTier          string  `json:"pricing_tier"`
	QualificationType    string  `json:"qualification_type"`
	CurrentValue         string  `json:"current_value"`
	NextTierThreshold    string  `json:"next_tier_threshold"`
	CurrentTier          FeeTier `json:"current_tier"`
	NextTier             FeeTier `json:"next_tier"`
	CurrentTierThreshold string  `json:"current_tier_threshold"`
}

// TransactionSummary는 수수료 등급, 30일 거래량, 누적 수수료 요약이다.
// 거래량과 수수료는 USD 기준 JSON 숫자이며 정밀도 손실을 피하려고 원문을 보존한다.
// deribit_venue_data 같은 INTX 파생상품 필드는 Raw에서 읽는다.
type TransactionSummary struct {
	TotalVolume             json.Number             `json:"total_volume"`
	TotalFees               json.Number             `json:"total_fees"`
	FeeTier                 FeeTier                 `json:"fee_tier"`
	MarginRate              Decimal                 `json:"margin_rate"`
	GoodsAndServicesTax     GoodsAndServicesTax     `json:"goods_and_services_tax"`
	AdvancedTradeOnlyVolume json.Number             `json:"advanced_trade_only_volume"`
	AdvancedTradeOnlyFees   json.Number             `json:"advanced_trade_only_fees"`
	CoinbaseProVolume       json.Number             `json:"coinbase_pro_volume"`
	CoinbaseProFees         json.Number             `json:"coinbase_pro_fees"`
	TotalBalance            string                  `json:"total_balance"`
	VolumeBreakdown         []VolumeBreakdown       `json:"volume_breakdown"`
	HasCostPlusCommission   bool                    `json:"has_cost_plus_commission"`
	FeeTierWithoutPromotion FeeTierWithoutPromotion `json:"fee_tier_without_promotion"`
	Raw                     json.RawMessage         `json:"-"`
}

// KeyPermissions는 현재 API key의 권한과 연결된 포트폴리오다.
type KeyPermissions struct {
	CanView       bool            `json:"can_view"`
	CanTrade      bool            `json:"can_trade"`
	CanTransfer   bool            `json:"can_transfer"`
	PortfolioUUID string          `json:"portfolio_uuid"`
	PortfolioType PortfolioType   `json:"portfolio_type"`
	Raw           json.RawMessage `json:"-"`
}

// PaymentMethod는 계정에 연결된 결제수단이다.
type PaymentMethod struct {
	ID            string          `json:"id"`
	Type          string          `json:"type"`
	Name          string          `json:"name"`
	Currency      string          `json:"currency"`
	Verified      bool            `json:"verified"`
	AllowBuy      bool            `json:"allow_buy"`
	AllowSell     bool            `json:"allow_sell"`
	AllowDeposit  bool            `json:"allow_deposit"`
	AllowWithdraw bool            `json:"allow_withdraw"`
	CreatedAt     string          `json:"created_at"`
	UpdatedAt     string          `json:"updated_at"`
	Raw           json.RawMessage `json:"-"`
}

func (request TransactionSummaryRequest) validate() error {
	switch request.ProductType {
	case "", ProductTypeSpot, ProductTypeFuture, ProductTypeEquity, ProductTypeOptionGroup, ProductTypeFutureGroup:
	default:
		return validationError("unsupported product type %q", request.ProductType)
	}
	switch request.ContractExpiryType {
	case "", ContractExpiryExpiring, ContractExpiryPerpetual:
	default:
		return validationError("unsupported contract expiry type %q", request.ContractExpiryType)
	}
	switch request.ProductVenue {
	case "", ProductVenueCBE, ProductVenueFCM, ProductVenueINTX:
	default:
		return validationError("unsupported product venue %q", request.ProductVenue)
	}
	return nil
}

func (request TransactionSummaryRequest) values() url.Values {
	values := make(url.Values)
	setIfNotEmpty(values, "product_type", string(request.ProductType))
	setIfNotEmpty(values, "contract_expiry_type", string(request.ContractExpiryType))
	setIfNotEmpty(values, "product_venue", string(request.ProductVenue))
	return values
}

// TransactionSummary는 수수료 등급, 거래량, 누적 수수료 요약을 조회한다.
func (client *Client) TransactionSummary(
	ctx context.Context,
	request TransactionSummaryRequest,
	options ...trade.RequestOption,
) (TransactionSummary, error) {
	if err := request.validate(); err != nil {
		return TransactionSummary{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, publicPrefix+"/transaction_summary", request.values(), nil,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return TransactionSummary{}, err
	}
	var summary TransactionSummary
	if err := client.decodeSuccess(response, commonexchange.OperationRead, &summary); err != nil {
		return TransactionSummary{}, err
	}
	summary.Raw = cloneBytes(response.Body)
	return summary, nil
}

// KeyPermissions는 요청에 사용한 API key의 view·trade·transfer 권한을 조회한다.
func (client *Client) KeyPermissions(
	ctx context.Context,
	options ...trade.RequestOption,
) (KeyPermissions, error) {
	response, err := client.executePrivate(
		ctx, http.MethodGet, publicPrefix+"/key_permissions", nil, nil,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return KeyPermissions{}, err
	}
	var permissions KeyPermissions
	if err := client.decodeSuccess(response, commonexchange.OperationRead, &permissions); err != nil {
		return KeyPermissions{}, err
	}
	permissions.Raw = cloneBytes(response.Body)
	return permissions, nil
}

// PaymentMethods는 계정에 연결된 결제수단 목록을 조회한다.
func (client *Client) PaymentMethods(
	ctx context.Context,
	options ...trade.RequestOption,
) ([]PaymentMethod, error) {
	response, err := client.executePrivate(
		ctx, http.MethodGet, publicPrefix+"/payment_methods", nil, nil,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	var page struct {
		PaymentMethods []json.RawMessage `json:"payment_methods"`
	}
	if err := client.decodeSuccess(response, commonexchange.OperationRead, &page); err != nil {
		return nil, err
	}
	methods := make([]PaymentMethod, len(page.PaymentMethods))
	for index, raw := range page.PaymentMethods {
		if err := json.Unmarshal(raw, &methods[index]); err != nil {
			return nil, client.decodeBodyError(response, commonexchange.OperationRead, err)
		}
		methods[index].Raw = cloneBytes(raw)
	}
	return methods, nil
}

// PaymentMethod는 결제수단 ID로 단일 결제수단을 조회한다.
func (client *Client) PaymentMethod(
	ctx context.Context,
	paymentMethodID string,
	options ...trade.RequestOption,
) (PaymentMethod, error) {
	segment, err := escapePathSegment("payment method ID", paymentMethodID)
	if err != nil {
		return PaymentMethod{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, publicPrefix+"/payment_methods/"+segment, nil, nil,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return PaymentMethod{}, err
	}
	var envelope struct {
		PaymentMethod json.RawMessage `json:"payment_method"`
	}
	if err := client.decodeSuccess(response, commonexchange.OperationRead, &envelope); err != nil {
		return PaymentMethod{}, err
	}
	var method PaymentMethod
	if err := json.Unmarshal(envelope.PaymentMethod, &method); err != nil {
		return PaymentMethod{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	method.Raw = cloneBytes(envelope.PaymentMethod)
	return method, nil
}
