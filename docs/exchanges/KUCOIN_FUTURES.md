# KuCoin Classic Futures REST 어댑터

구현 기준은 KuCoin Classic Futures REST API와 기본 주소 `https://api-futures.kucoin.com`입니다. Go 패키지는 `exchange/kucoin/futures`이며, Spot용 `exchange/kucoin`과 독립된 native API를 제공합니다.

## 전제조건

private API를 사용하려면 `credential.Provider`가 반환하는 `credential.Material`에 다음 값을 넣어야 합니다.

| 필드 | KuCoin 값 |
|---|---|
| `APIKey` | API Key |
| `SecretKey` | API Secret 원문 |
| `Passphrase` | API Passphrase 원문 |

기본 `Config.APIKeyVersion`은 `2`입니다. 버전 2는 Passphrase 원문을 Secret Key로 HMAC-SHA256 서명한 Base64 값을 헤더에 전송합니다. 기존 버전 1 API Key를 사용해야 할 때만 `APIKeyVersion: "1"`을 지정하며, 이 경우 Passphrase 원문을 전송합니다.

`credential.Descriptor.AccountID`에는 KuCoin UID 요청 제한을 공유하는 계정의 안정적인 식별자를 넣어야 합니다. 자격증명의 `AllowedEgressRouteIDs` 밖에 있는 route는 Secret 조회 전에 차단됩니다. API Key의 IP 허용 목록에는 해당 route와 연결된 EIP를 등록해야 합니다.

## 지원 범위

| 영역 | 메서드 | API |
|---|---|---|
| 전체·단일 계약 | `Contracts`, `Contract` | `GET /api/v1/contracts/active`, `/api/v1/contracts/{symbol}` |
| 현재가 | `Ticker` | `GET /api/v1/ticker` |
| 부분 호가 | `OrderBook` | `GET /api/v1/level2/depth20`, `depth100` |
| 공개 체결 | `RecentTrades` | `GET /api/v1/trade/history` |
| 캔들 | `Candles` | `GET /api/v1/kline/query` |
| 계정 요약 | `AccountOverview` | `GET /api/v1/account-overview` |
| 열린 포지션 | `Positions` | `GET /api/v1/positions` |
| 주문 생성 | `PlaceOrder` | `POST /api/v1/orders` |
| 주문 상세·취소 | `OrderInfo`, `CancelOrder` | `GET`, `DELETE /api/v1/orders/{orderId}` 또는 취소용 `/client-order/{clientOid}` |
| 미체결 주문 | `OpenOrders` | `GET /api/v1/orders?status=active` |
| 체결 이력 | `Fills` | `GET /api/v1/fills` |

가격, 금액, 비율은 `float64`로 변환하지 않고 `Decimal` 문자열로 보존합니다. 객체와 배열 항목의 `Raw`에는 해당 `data` 원본 JSON을 보존합니다. 캔들은 `timestamp, open, high, low, close, volume, turnover` 순서이며, `From`과 `To`를 지정할 때 최대 500개 구간을 허용합니다.

## 생성 예시

```go
client, err := futures.New(futures.Config{
	Executor:             executor,
	Credentials:          descriptor,
	CredentialProvider:   provider,
	DefaultEgressRouteID: "seoul-a",
})
if err != nil {
	return err
}

ticker, err := client.Ticker(
	ctx,
	"XBTUSDTM",
	trade.WithEgressRoute("seoul-b"),
)
```

요청별 `trade.WithEgressRoute`를 생략하면 `DefaultEgressRouteID`를 사용합니다. public 요청은 route별 IP 제한을 사용하고, private 요청은 선택한 route가 자격증명 허용 목록에 포함되는지 확인한 뒤 서명합니다.

## 인증과 서명

private 요청은 요청 제한 대기가 끝난 뒤 자격증명을 조회하고 최종 요청을 서명합니다.

1. 현재 Unix millisecond를 `KC-API-TIMESTAMP`로 사용합니다.
2. `timestamp + 대문자 HTTP method + endpoint와 query + JSON body`를 이어 붙입니다.
3. 이 bytes를 API Secret으로 HMAC-SHA256 서명하고 표준 Base64로 변환해 `KC-API-SIGN`에 넣습니다.
4. API Key, timestamp, 서명한 Passphrase, API Key 버전을 각각 `KC-API-*` 헤더에 넣습니다.
5. POST는 서명한 것과 정확히 같은 공백 없는 JSON bytes를 본문으로 전송합니다. GET·DELETE는 본문 없이 실제 query와 같은 문자열을 서명합니다.

SDK의 symbol, currency, 주문 ID는 URL 인코딩 전후가 달라지지 않는 제한된 문자만 허용합니다. Provider가 반환한 API Key, Secret, Passphrase byte slice는 요청 뒤 가능한 범위에서 덮어씁니다. Go 문자열과 HTTP 계층 내부 복사본까지 완전히 지울 수 있다는 보장은 하지 않습니다.

## 요청 제한과 EIP

VIP 0 기준 기본 로컬 quota는 각 30초 구간의 Public 2,000 weight와 Futures 2,000 weight입니다.

| bucket | 기본 제한 | 범위 |
|---|---:|---|
| `kucoin-futures:route:<route>:public:30seconds` | 2,000 weight/30초 | 선택한 EIP route |
| `kucoin-futures:account:<account>:futures:30seconds` | 2,000 weight/30초 | KuCoin UID의 Futures API |

각 endpoint의 공식 weight를 차감합니다. `gw-ratelimit-limit`과 `gw-ratelimit-remaining`이 로컬 설정과 일치하면 관측 사용량을 반영하고, remaining이 0이면 `gw-ratelimit-reset` millisecond 동안 해당 bucket을 막습니다. `Config`의 quota는 계정 VIP 등급이나 더 보수적인 운영 정책에 맞게 조정할 수 있습니다.

Public 풀은 IP 기준이므로 요청별 EIP가 각각 독립된 bucket을 사용합니다. Futures private 풀은 UID 기준이므로 EIP를 바꿔도 quota가 늘어나지 않습니다. 다중 EIP는 public 처리량 분산과 API Key IP 허용 목록·장애 격리를 위한 기능이며 private 제한 우회 용도가 아닙니다.

## 주문 안전 계약

- `ClientOrderID`는 모든 주문에서 필수이며 `[0-9A-Za-z_-]{1,40}` 형식으로 검증합니다.
- 1차 범위는 양의 정수 계약 수량 `Size`를 사용하는 지정가·시장가 주문입니다.
- 지정가는 양의 `Price`를 요구하고 GTC·IOC를 지원합니다. IOC와 post-only는 함께 사용할 수 없습니다.
- 시장가는 가격, TIF, post-only를 허용하지 않습니다.
- `MarginMode`는 `ISOLATED` 또는 `CROSS`이며 `PositionSide`는 생략하거나 `BOTH`, `LONG`, `SHORT`를 사용합니다.
- 레버리지는 생략하거나 1~1,000 범위에서 지정하며 실제 허용값은 계약 규칙과 계정 설정을 따라야 합니다.
- 취소는 거래소 주문 ID와 사용자 주문 ID 중 정확히 하나를 받습니다.
- 조건부 주문, 전체 포지션 종료, `qty`·`valueQty`, RPI 주문은 1차 범위에서 제외합니다.

주문 생성·취소의 전송 오류, 응답 본문 읽기 실패, 성공 응답 JSON 파싱 실패, 거래소 `500000`과 처리 여부를 확정할 수 없는 일부 오류는 실제 처리 여부를 단정할 수 없습니다. SDK는 자동 재시도하지 않고 `trade.ErrUnknownExecutionState`로 반환합니다. 불명확한 주문 생성 결과는 고유한 `ClientOrderID`를 기준으로 거래소 상태를 확인한 뒤 처리해야 합니다.

## 오류 처리

KuCoin은 HTTP 200에서도 `code`가 `200000`이 아닌 논리 오류를 반환할 수 있습니다. SDK는 HTTP 상태와 JSON envelope를 모두 검사하고 인증·서명, 권한·IP 허용 목록, 잔고 부족, 요청 제한, 주문 없음, 거래소 장애를 공통 `trade.APIError`로 변환합니다. 원본 오류 code와 message, 요청 ID를 보존하되 인증 헤더와 서명 원문은 오류에 포함하지 않습니다.

## 공식 기준

- [KuCoin Authentication](https://www.kucoin.com/docs-new/authentication)
- [KuCoin Rate Limit](https://www.kucoin.com/docs-new/rate-limit)
- [KuCoin Futures All Symbols](https://www.kucoin.com/docs-new/rest/futures-trading/market-data/get-all-symbols)
- [KuCoin Futures Account](https://www.kucoin.com/docs-new/rest/account-info/account-funding/get-account-futures)
- [KuCoin Futures Position List](https://www.kucoin.com/docs-new/rest/futures-trading/positions/get-position-list)
- [KuCoin Futures Add Order](https://www.kucoin.com/docs-new/rest/futures-trading/orders/add-order)
- [KuCoin Futures Error Codes](https://www.kucoin.com/docs-new/error-code/futures)
- [KuCoin Change Log](https://www.kucoin.com/docs-new/change-log)
