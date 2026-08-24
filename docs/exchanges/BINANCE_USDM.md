# Binance USDⓈ-M Futures 어댑터

## 패키지와 전제조건

USDⓈ-M Futures는 Spot과 base URL, 상품 모델, 주문 제한, 포지션 의미가 달라 `exchange/binance/usdm` 패키지로 분리합니다. 기본 REST 주소는 `https://fapi.binance.com`입니다.

private API는 `credential.Material.APIKey`와 `credential.Material.SecretKey`를 사용합니다. `credential.Descriptor.Exchange`는 Spot과 같은 `model.ExchangeBinance`이며, `AccountID`에는 Binance 계정 또는 제한을 공유하는 계정 식별자를 넣습니다.

자격증명의 `AllowedEgressRouteIDs` 밖 route는 Secret 조회 전에 차단됩니다.

## 지원 범위

| 영역 | 메서드 |
|---|---|
| 연결·시간 | `Ping`, `ServerTime` |
| 계약·시세 | `ExchangeInfo`, `TickerPrice`, `OrderBook`, `RecentTrades`, `Candles` |
| 계정·포지션 | `Account`, `Positions` |
| 주문 | `PlaceOrder`, `OrderInfo`, `CancelOrder`, `OpenOrders`, `OrderHistory` |

계정은 `/fapi/v3/account`, 포지션 위험은 `/fapi/v3/positionRisk`를 사용합니다. 주문 정정, 일괄 주문, 레버리지·마진 모드 변경, 수입 이력, WebSocket은 아직 포함하지 않습니다.

## 인증과 시간

SIGNED 요청은 URL 인코딩한 최종 파라미터에 HMAC SHA-256을 적용합니다. 실행 순서는 다음과 같습니다.

1. 자격증명의 route와 권한을 검사합니다.
2. IP weight와 계정 주문 count 제한을 확보합니다.
3. Secret을 조회합니다.
4. 보정된 현재 시간과 `recvWindow`를 넣습니다.
5. 최종 쿼리에 서명하고 선택한 EIP route로 전송합니다.

기본 `recvWindow`는 5초이며 최대 1분까지 설정할 수 있습니다. `ServerTime`은 요청 왕복 중간 시점을 기준으로 clock offset을 보정합니다.

## 요청 제한

초기 안전값은 현재 공식 기본 규칙을 사용합니다.

| 제한 | 초기값 | 범위 |
|---|---:|---|
| 요청 weight | 2400/분 | EIP route |
| 주문 count | 300/10초 | 계정 |
| 주문 count | 1200/분 | 계정 |

`ExchangeInfo` 응답의 `REQUEST_WEIGHT`와 `ORDERS` 규칙으로 값을 동적으로 갱신합니다. 응답의 `X-MBX-USED-WEIGHT-1M`, `X-MBX-ORDER-COUNT-10S`, `X-MBX-ORDER-COUNT-1M`도 로컬 limiter에 반영합니다.

주문 생성은 IP weight를 소비하지 않지만 계정 주문 count를 소비합니다. 여러 EIP를 사용해도 계정 주문 제한은 늘어나지 않습니다.

## 포지션 모드와 주문 검증

- 단방향 모드는 `PositionSideBoth` 또는 빈 값을 사용합니다.
- 헤지 모드는 `PositionSideLong` 또는 `PositionSideShort`를 명시합니다.
- 헤지 모드 주문에는 `reduceOnly`를 함께 보내지 않습니다.
- `closePosition`은 `STOP_MARKET` 또는 `TAKE_PROFIT_MARKET`에서 사용하며 `quantity`, `reduceOnly`와 함께 보낼 수 없습니다.
- GTD 주문은 현재 시각보다 최소 600초 뒤의 `GoodTillDate`가 필요합니다.
- 가격과 수량은 decimal 문자열로 입력하며 SDK가 자동 반올림하지 않습니다.

지원 주문 종류는 `LIMIT`, `MARKET`, `STOP`, `STOP_MARKET`, `TAKE_PROFIT`, `TAKE_PROFIT_MARKET`, `TRAILING_STOP_MARKET`입니다.

## HTTP 503과 주문 안전성

Binance Futures는 HTTP 503의 문구별 의미가 다릅니다.

| 응답 | SDK 분류 |
|---|---|
| `Unknown error, please check...` | `UNKNOWN_EXECUTION_STATE` |
| `Service Unavailable.` | `EXCHANGE_UNAVAILABLE`, 재시도 가능 |
| `Internal error; unable to process...` | `EXCHANGE_UNAVAILABLE`, 재시도 가능 |
| `-1008` system-level protection | `EXCHANGE_UNAVAILABLE`, 재시도 가능 |

SDK 실행기는 mutation을 자동 재시도하지 않습니다. 특히 불명확한 결과는 사용자 주문 ID나 주문 조회, User Data Stream으로 확인하기 전 다시 생성하면 안 됩니다.

## 공식 기준 문서

- [Binance USDⓈ-M General Info](https://developers.binance.com/en/docs/products/derivatives-trading-usds-futures/general-info)
- [Binance USDⓈ-M Trade REST API](https://developers.binance.com/en/docs/catalog/core-trading-derivatives-trading-usd-s-m-futures/api/rest-api/trade)
- [Binance USDⓈ-M Account REST API](https://developers.binance.com/en/docs/catalog/core-trading-derivatives-trading-usd-s-m-futures/api/rest-api/account)
- [Binance USDⓈ-M Market Data REST API](https://developers.binance.com/en/docs/catalog/core-trading-derivatives-trading-usd-s-m-futures/api/rest-api/market-data)
