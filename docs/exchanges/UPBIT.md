# Upbit Spot 어댑터

## 전제조건

private API를 사용하려면 `credential.Provider`가 반환하는 `credential.Material`에 다음 값을 넣어야 합니다.

| 필드 | Upbit 값 |
|---|---|
| `APIKey` | Access Key |
| `SecretKey` | Secret Key 원문 |

Secret Key는 Base64 디코딩하지 않고 HS512 서명 키로 직접 사용합니다. `credential.Descriptor.AccountID`에는 개별 Access Key가 아니라 요청 제한을 공유하는 업비트 계정 pocket 식별자를 넣어야 합니다. 같은 pocket의 여러 키가 같은 요청 제한을 공유하기 때문입니다.

자격증명에 설정한 `AllowedEgressRouteIDs` 밖의 route는 Secret 조회 전에 차단됩니다.

## 지원 범위

| 영역 | 메서드 |
|---|---|
| 마켓 | `Markets` |
| 공개 시세 | `Tickers`, `OrderBooks`, `RecentTrades`, `MinuteCandles` |
| 계정 | `Accounts` |
| 주문 | `PlaceOrder`, `OrderInfo`, `CancelOrder`, `OpenOrders`, `ClosedOrders` |

주문 생성은 `limit`, 매수 시장가 `price`, 매도 시장가 `market`을 지원합니다. 업비트 고유 `best` 주문, 주문 가능 정보, 일괄 취소, WebSocket은 아직 포함하지 않습니다.

가격, 수량, 금액은 `float64`로 변환하지 않습니다. private 응답은 거래소가 제공한 문자열을 유지하고 공개 시세의 JSON 숫자는 `Decimal` 문자열 타입으로 손실 없이 보존합니다.

## JWT와 쿼리 해시

인증 요청은 요청 제한 대기가 끝난 뒤 최종 파라미터를 기준으로 서명합니다.

1. 요청마다 UUID v4 nonce를 생성합니다.
2. 파라미터가 있으면 URL 인코딩 전 쿼리 문자열을 SHA-512 해시합니다.
3. `access_key`, `nonce`, `query_hash`, `query_hash_alg=SHA512`를 HS512 JWT payload로 만듭니다.
4. `Authorization: Bearer <JWT>` 헤더로 전송합니다.

반복 파라미터는 `states[]=wait&states[]=watch`처럼 순서와 키를 그대로 유지합니다. POST 주문은 JSON 본문의 필드 순서와 쿼리 해시 입력 순서를 같은 ordered parameter에서 생성합니다.

`NonceSource`는 테스트를 위한 주입 지점입니다. 운영에서는 기본 암호학적 난수 UUID 생성기를 사용해야 하며, 고정 nonce를 설정하면 안 됩니다.

## 요청 제한

SDK는 공개 API를 EIP route별 IP bucket으로, private API를 `AccountID`별 pocket bucket으로 관리합니다.

| 그룹 | 로컬 제한 | 범위 |
|---|---:|---|
| `market`, `ticker`, `orderbook`, `trade`, `candle` | 각 10회/초 | IP route |
| `default` | 30회/초 | 계정 pocket |
| `order` | 12회/초 | 계정 pocket |

2026-08-21 적용된 주문 그룹 상향값 `12회/초`를 반영했습니다. 응답의 `Remaining-Req` 중 `group`과 `sec` 값을 로컬 limiter에 반영합니다. HTTP 429와 418은 `Retry-After`가 있으면 그 기간, 없으면 최소 1초 동안 관련 bucket을 차단합니다.

여러 EIP를 사용해도 pocket 단위 제한은 증가하지 않습니다. route 선택은 API Key IP 허용 목록 준수와 네트워크 격리를 위한 기능이며 거래소 제한 우회 용도가 아닙니다.

## 안전한 주문 실패

주문 생성·취소의 전송 오류, 응답 본문 읽기 실패, HTTP 5xx는 실제 체결 여부를 단정할 수 없습니다. SDK는 자동 재시도하지 않고 `trade.ErrUnknownExecutionState`로 반환합니다.

가능하면 고유한 `Identifier`를 주문에 넣고, 불명확한 결과가 나오면 `OrderInfo`로 최종 상태를 확인해야 합니다. 주문 목록을 전체 마켓으로 조회하려면 실수로 조회 범위를 넓히지 않도록 `AllMarkets: true`를 명시해야 합니다.

## 공식 기준 문서

- [Upbit 인증](https://docs.upbit.com/kr/reference/auth)
- [Upbit 요청 수 제한](https://docs.upbit.com/kr/reference/rate-limits)
- [Upbit 주문 생성](https://docs.upbit.com/kr/reference/new-order)
- [Upbit order 요청 수 제한 상향](https://docs.upbit.com/kr/changelog/order-rate-limit-update)
