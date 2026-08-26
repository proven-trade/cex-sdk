// Package support는 거래소와 상품별 구현·검증 상태를 조회하는 기능을 제공한다.
package support

import "github.com/proven-trade/cex-sdk/model"

// Status는 기능의 구현 또는 검증 진행 상태다.
type Status string

const (
	StatusImplemented   Status = "implemented"
	StatusPlanned       Status = "planned"
	StatusPending       Status = "pending"
	StatusNotApplicable Status = "not_applicable"
)

// ProductID는 거래소 안에서 상품군을 식별하는 안정적인 값이다.
type ProductID string

const (
	ProductSpot            ProductID = "spot"
	ProductUSDMFutures     ProductID = "usdm_futures"
	ProductUSDTMFutures    ProductID = "usdtm_futures"
	ProductLinearPerpetual ProductID = "linear_perpetual"
	ProductSwap            ProductID = "swap"
	ProductFutures         ProductID = "futures"
)

// ProductSupport는 거래소 상품군 하나의 API와 검증 지원 상태다.
type ProductSupport struct {
	Exchange         model.ExchangeID
	DisplayName      string
	Tier             string
	Product          ProductID
	ProductName      string
	REST             Status
	WebSocketPublic  Status
	WebSocketPrivate Status
	Unified          Status
	AutomatedTests   Status
	LiveReadSmoke    Status
	LiveTradeSmoke   Status
	Docs             []string
}

// All은 전체 지원 목록의 복사본을 반환한다.
func All() []ProductSupport {
	result := make([]ProductSupport, len(catalogData))
	copy(result, catalogData)
	for index := range result {
		result[index].Docs = append([]string(nil), result[index].Docs...)
	}
	return result
}

// Lookup은 거래소와 상품군이 일치하는 지원 정보를 반환한다.
func Lookup(exchange model.ExchangeID, product ProductID) (ProductSupport, bool) {
	for _, entry := range catalogData {
		if entry.Exchange == exchange && entry.Product == product {
			entry.Docs = append([]string(nil), entry.Docs...)
			return entry, true
		}
	}
	return ProductSupport{}, false
}

// Implemented는 해당 상태의 코드 구현이 완료됐는지 반환한다.
func (status Status) Implemented() bool {
	return status == StatusImplemented
}

// OperationallyVerified는 읽기와 거래 live smoke가 모두 끝났는지 반환한다.
func (entry ProductSupport) OperationallyVerified() bool {
	return entry.LiveReadSmoke == StatusImplemented && entry.LiveTradeSmoke == StatusImplemented
}
