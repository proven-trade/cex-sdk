# MEXC Spot V3 REST 어댑터

Go 패키지는 `exchange/mexc`이며 현행 Spot V3 기본 주소 `https://api.mexc.com`을 사용합니다. 공개 시세와 signed 계정·주문 REST를 구현했습니다. 공통 Spot API와 protobuf WebSocket은 후속 단계에서 추가합니다.

MEXC는 별도 sandbox를 제공하지 않으므로 자동 테스트는 로컬 mock 거래소만 사용합니다. 실제 API 호출은 읽기 요청도 production 환경으로 향한다는 점을 운영 절차에서 구분해야 합니다.

## 자격증명

Private API를 사용하려면 `credential.Provider`가 반환하는 `credential.Material`에 다음 값을 넣어야 합니다.

| 필드 | MEXC 값 |
|---|---|
| `APIKey` | Access Key |
| `SecretKey` | Secret Key 원문 |

`credential.Descriptor.AccountID`에는 UID 요청 제한을 공유하는 계정의 안정적인 식별자를 넣어야 합니다. 자격증명의 `AllowedEgressRouteIDs` 밖에 있는 route와 필요한 읽기·거래 권한이 없는 호출은 Secret 조회 전에 차단됩니다. MEXC API Key의 IP 허용 목록에는 사용을 허용할 route에 연결된 EIP를 등록해야 합니다.

```go
client, err := mexc.New(mexc.Config{
	Executor: executor,
	Credentials: &credential.Descriptor{
		AccountID: "mexc-main",
		Exchange:  model.ExchangeMEXC,
		SecretRef: "secret/mexc-main",
		Permissions: []credential.Permission{
			credential.PermissionRead,
			credential.PermissionTrade,
		},
		AllowedEgressRouteIDs: []transport.EgressRouteID{"seoul-a", "seoul-b"},
	},
	CredentialProvider:   provider,
	DefaultEgressRouteID: "seoul-a",
})
if err != nil {
	return err
}
```

## 지원 범위

| 영역 | 메서드 | API | IP weight |
|---|---|---|---:|
| 연결 확인 | `Ping` | `GET /api/v3/ping` | 1 |
| 서버 시각 | `ServerTime` | `GET /api/v3/time` | 1 |
| 기본 API 거래쌍 | `DefaultSymbols` | `GET /api/v3/defaultSymbols` | 1 |
| 전체·단일·복수 거래쌍 규칙 | `ExchangeInfo` | `GET /api/v3/exchangeInfo` | 10 |
| 호가 snapshot | `OrderBook` | `GET /api/v3/depth` | 1 |
| 최근 체결 | `RecentTrades` | `GET /api/v3/trades` | 5 |
| 합산 체결 | `AggregateTrades` | `GET /api/v3/aggTrades` | 1 |
| 캔들 | `Candles` | `GET /api/v3/klines` | 1 |
| 최근 평균가 | `AveragePrice` | `GET /api/v3/avgPrice` | 1 |
| 24시간 통계 | `Ticker24H` | `GET /api/v3/ticker/24hr` | 1 |
| 최근 가격 | `PriceTicker` | `GET /api/v3/ticker/price` | 1 |
| 최우선 호가 | `BookTicker` | `GET /api/v3/ticker/bookTicker` | 1 |
| API Key 허용 거래쌍 | `SelfSymbols` | `GET /api/v3/selfSymbols` | 10 |
| 계정·잔고 | `Account` | `GET /api/v3/account` | 10 |
| 주문 생성 | `PlaceOrder` | `POST /api/v3/order` | 10 |
| 주문 상세 | `OrderInfo` | `GET /api/v3/order` | 10 |
| 주문 취소 | `CancelOrder` | `DELETE /api/v3/order` | 10 |
| 미체결 주문 | `OpenOrders` | `GET /api/v3/openOrders` | 10 |
| 전체 주문 이력 | `AllOrders` | `GET /api/v3/allOrders` | 10 |
| 계정 체결 | `MyTrades` | `GET /api/v3/myTrades` | 10 |

24시간 통계와 ticker 메서드는 이번 단계에서 단일 symbol을 필수로 받습니다. 전체 symbol 조회는 공식 weight가 더 크고 응답도 배열로 바뀌므로, 호출 실수로 IP quota와 메모리를 크게 소비하지 않게 별도 API로 열지 않았습니다.

`OrderBookRequest.Limit`은 생략하거나 1~5000, 체결·캔들 조회의 `Limit`은 생략하거나 1~1000입니다. 합산 체결의 `Start`와 `End`는 함께 지정해야 합니다. 캔들은 `1m`, `5m`, `15m`, `30m`, `60m`, `4h`, `1d`, `1W`, `1M`을 지원하고 시각은 Unix millisecond query로 변환합니다.

Private 표의 weight는 endpoint 본문과 2025년 공식 제한표가 충돌하는 항목에서 더 보수적인 값인 10을 사용합니다. `AllOrders`는 최대 1000건과 7일 범위, `MyTrades`는 최대 100건과 31일 범위를 로컬에서 검증합니다.

## 인증과 서명

Private 요청은 limiter 대기가 끝난 뒤 자격증명을 조회하고 다음 순서로 최종 요청을 만듭니다.

1. 실제 query에 `recvWindow`와 현재 Unix millisecond `timestamp`를 추가합니다.
2. URL 인코딩과 정렬을 마친 query 전체를 `totalParams`로 사용합니다.
3. Secret Key로 HMAC-SHA256 서명하고 소문자 16진수로 변환합니다.
4. `signature` query와 `X-MEXC-APIKEY` 헤더를 추가합니다.

`Config.ReceiveWindow` 기본값은 5초이며 1ms 이상 60초 이하만 허용합니다. Provider가 반환한 API Key와 Secret byte slice는 요청 뒤 가능한 범위에서 덮어씁니다. Go 문자열과 HTTP 계층 내부 복사본까지 완전히 지울 수 있다는 보장은 하지 않습니다.

## 생성과 요청별 EIP 선택

```go
client, err := mexc.New(mexc.Config{
	Executor:             executor,
	DefaultEgressRouteID: "seoul-a",
})
if err != nil {
	return err
}

book, err := client.OrderBook(
	ctx,
	mexc.OrderBookRequest{Symbol: "BTCUSDT", Limit: 1000},
	trade.WithEgressRoute("seoul-b"),
)
```

요청 옵션을 생략하면 `DefaultEgressRouteID`를 사용하고 `trade.WithEgressRoute`를 지정하면 해당 요청만 다른 route로 보냅니다. 실제 public IP 선택은 공통 전송 계층이 route에 연결된 secondary private IPv4로 소켓을 bind하여 수행합니다. Public 제한은 route마다 분리되고 private 요청은 선택한 route 제한과 계정 제한을 함께 차감합니다.

## 요청 제한과 차단 응답

공식 Spot V3 문서는 IP와 UID 제한이 독립적이며 각 endpoint가 500 weight/10초 제한을 가진다고 설명합니다. SDK는 다음 bucket을 원자적으로 함께 차감합니다.

| bucket | 기본 제한 | 범위 |
|---|---:|---|
| `mexc:route:<route>:public:<endpoint>:10seconds` | 500 weight/10초 | 선택한 EIP route의 공개 endpoint |
| `mexc:route:<route>:private:<endpoint>:10seconds` | 500 weight/10초 | 선택한 EIP route의 private endpoint |
| `mexc:account:<account>:private:<endpoint>:10seconds` | 500 weight/10초 | UID의 private endpoint |
| `mexc:account:<account>:order:1second` | 5회/초 | UID의 주문 생성 |
| `mexc:account:<account>:cancel:1second` | 50회/초 | UID의 주문 취소 |
| `mexc:account:<account>:private-read:1second` | 50회/초 | UID의 허용 거래쌍·주문·체결 조회 |
| `mexc:account:<account>:account:1second` | 2회/초 | UID의 계정·잔고 조회 |

`Config.EndpointQuota` 기본값은 500이며 private 초당 제한은 `OrderQuota`, `CancelQuota`, `PrivateReadQuota`, `AccountQuota`로 더 낮출 수 있습니다. 2025년 제한표와 endpoint 본문의 계정 조회 제한이 다르므로 계정 조회에는 더 보수적인 2회/초를 적용합니다. 서버가 HTTP 418 또는 429와 `Retry-After`를 반환하면 요청이 차감한 route·계정 bucket을 지정된 기간 동안 차단합니다. 429 이후 계속 호출하면 IP ban 기간이 길어질 수 있으므로 호출자가 별도 우회 재시도를 추가하면 안 됩니다.

EIP를 바꿔도 UID bucket은 공유됩니다. 다중 EIP 기능은 정상적인 public 트래픽 분산, API Key 허용 IP 일치, 장애 격리를 위한 기능이며 MEXC의 제한이나 이용 정책을 우회하는 용도로 사용하면 안 됩니다.

## 주문 안전 계약

- `ClientOrderID`는 필수이며 영문자, 숫자, 밑줄, 하이픈으로 구성된 1~32자만 허용합니다.
- `LIMIT`, `LIMIT_MAKER`, `IMMEDIATE_OR_CANCEL`, `FILL_OR_KILL`은 가격과 기준 통화 수량을 요구합니다.
- 시장가 매수는 `QuoteQuantity`만, 시장가 매도는 `Quantity`만 받으며 가격을 허용하지 않습니다.
- SDK는 주문 수량과 가격을 자동 반올림하지 않습니다. `ExchangeInfo`의 거래쌍별 정밀도와 최소·최대 값을 적용한 양의 십진수 문자열을 전달해야 합니다.
- 주문 상세와 취소는 거래쌍과 거래소 주문 ID 또는 사용자 주문 ID 중 정확히 하나를 요구합니다.

주문 생성·취소의 전송 오류, 응답 본문 읽기 실패, 성공 응답 JSON 파싱 실패, 필수 주문 식별자 누락, HTTP 5xx는 실제 처리 여부를 단정할 수 없습니다. SDK는 자동 재시도하지 않고 `trade.ErrUnknownExecutionState`를 반환합니다. 주문 생성 결과가 불명확하면 같은 `ClientOrderID`로 재주문하지 말고 `OrderInfo`로 먼저 상태를 확인해야 합니다.

## 응답과 오류 계약

가격·수량·거래량은 `float64`로 바꾸지 않고 문자열로 보존합니다. 위치 기반 호가와 캔들은 필드 수를 검증하며 각 객체의 `Raw`에는 해당 JSON 원본을 보존합니다. 거래 ID처럼 응답에 숫자·문자열·`null`이 혼재하는 값은 `Scalar`로 원형을 유지합니다.

`DefaultSymbols`와 `SelfSymbols`는 공식 문서 예시의 성공 code `200`과 production에서 사용하는 `0`을 모두 허용합니다. 다른 nonzero code, HTTP 오류, JSON 파싱 실패는 `trade.APIError`로 변환합니다. 인증·권한·잔고·주문 없음·요청 제한·거래소 장애 코드를 공통 category로 분류하고 MEXC 원본 code·message와 요청 ID를 함께 보존합니다.

자동 테스트는 HMAC 서명과 실제 query 일치, 요청별 route 선택, route·권한 사전 검사, Secret 덮어쓰기, IP·UID 요청 제한, 주문 검증, 원본 JSON 보존, 오류 분류와 mutation 불명확 상태를 검증합니다. 실제 MEXC 계정과 지정 EIP를 이용한 읽기·주문 smoke는 아직 대기 상태입니다.

## 공식 기준

- [MEXC Spot V3 API 문서](https://mexcdevelop.github.io/apidocs/spot_v3_en/)
- [MEXC API 안내](https://www.mexc.com/mexc-api)
- [MEXC 2025 요청 제한표](https://www.mexc.com/en-GB/announcements/article/term-definitions-17827791529303)
