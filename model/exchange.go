// Package model은 거래소에 종속되지 않는 도메인 타입을 제공한다.
package model

import "strings"

// ExchangeID는 거래소 어댑터를 선택할 때 사용하는 안정적인 식별자다.
type ExchangeID string

const (
	ExchangeBinance  ExchangeID = "binance"
	ExchangeBitget   ExchangeID = "bitget"
	ExchangeUpbit    ExchangeID = "upbit"
	ExchangeBybit    ExchangeID = "bybit"
	ExchangeOKX      ExchangeID = "okx"
	ExchangeCoinbase ExchangeID = "coinbase"
	ExchangeKraken   ExchangeID = "kraken"
)

// Valid는 거래소 식별자가 비어 있지 않은지 반환한다.
// 코어는 이 패키지를 수정하지 않고 외부 어댑터를 추가할 수 있도록
// 기본 제공 목록에 없는 식별자도 의도적으로 허용한다.
func (id ExchangeID) Valid() bool {
	return strings.TrimSpace(string(id)) != ""
}
