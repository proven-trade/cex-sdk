# Korbit Spot REST 어댑터

구현 기준은 코빗 Open API v2와 REST 기본 주소 `https://api.korbit.co.kr`입니다.

## 전제조건

private API를 사용하려면 `credential.Provider`가 반환하는 `credential.Material`에 다음 값을 넣어야 합니다.

| 필드 | HMAC-SHA256 | ED25519 |
|---|---|---|
| `APIKey` | API Key | API Key |
| `SecretKey` | HMAC Secret 원문 | PKCS#8 PEM private key 원문 |

API Key의 서명 방식과 `Config.SigningMode`가 일치해야 합니다. 기본값은 `SigningModeHMACSHA256`이며 ED25519 키에는 `SigningModeED25519`를 지정합니다.

`credential.Descriptor.AccountID`에는 요청 제한을 공유하는 코빗 계정의 안정적인 식별자를 넣어야 합니다. `AccountSeq`는 코빗 하위 계정 번호이며 0이면 파라미터를 생략해 거래소 기본값 1을 사용합니다. 자격증명의 `AllowedEgressRouteIDs` 밖에 있는 route는 Secret 조회 전에 차단됩니다. API Key의 허용 IP에는 해당 route와 연결된 EIP를 등록해야 합니다.

## 지원 범위

| 영역 | 메서드 | API |
|---|---|---|
| 서버 시각 | `ServerTime` | `GET /v2/time` |
| 공개 시세 | `Tickers`, `OrderBook`, `RecentTrades`, `Candles` | public v2 |
| 상품 규칙 | `CurrencyPairs`, `TickSizePolicy` | public v2 |
| 계정 | `Balances` | `GET /v2/balance` |
| 주문 생성 | `PlaceOrder` | `POST /v2/orders` |
| 주문 상세·취소 | `OrderInfo`, `CancelOrder` | `GET`, `DELETE /v2/orders` |
| 주문·체결 목록 | `OpenOrders`, `OrderHistory`, `MyTrades` | private v2 |

가격, 수량, 금액, 수수료는 `float64`로 변환하지 않고 문자열로 보존합니다. 각 조회 항목의 `Raw`에는 해당 `data` 항목의 원본 JSON을 보존합니다. 취소 결과의 `Raw`에는 전체 응답 envelope를 보존합니다.

## 인증과 서명

private 요청은 요청 제한 대기가 끝난 뒤 최종 파라미터를 만들고 서명합니다.

1. endpoint 파라미터에 현재 Unix millisecond `timestamp`와 `recvWindow`를 추가합니다.
2. `signature`를 제외한 전체 파라미터를 Go `url.Values.Encode` 규칙으로 URL 인코딩합니다.
3. 그 문자열을 HMAC-SHA256 소문자 hex 또는 ED25519 표준 Base64로 서명합니다.
4. URL 인코딩한 `signature`를 마지막 파라미터로 추가하고 `X-KAPI-KEY` 헤더를 설정합니다.
5. GET·DELETE는 query string, POST는 같은 문자열의 `application/x-www-form-urlencoded` 본문으로 보냅니다.

서명 원문과 실제 전송 파라미터의 정렬·인코딩이 달라지면 인증에 실패합니다. `ReceiveWindow` 기본값은 5,000ms이며 1~60,000ms만 허용합니다. 운영 인스턴스는 NTP로 시각을 동기화하고 `ServerTime`으로 차이를 관측해야 합니다.

Provider가 반환한 자격증명 byte slice와 생성된 서명 bytes는 요청 후 가능한 범위에서 덮어씁니다. Go 문자열과 네트워크 계층 내부 복사본까지 완전히 지울 수 있다는 보장은 하지 않습니다.

## 요청 제한과 EIP

SDK 기본 로컬 제한은 공개 50회/초, private 일반 50회/초, 주문 생성 30회/초, 주문 취소 30회/초입니다.

| bucket | 기본 제한 | 범위 |
|---|---:|---|
| `korbit:route:<route>:public:1second` | 50회/초 | 선택한 EIP route |
| `korbit:account:<account>:private:1second` | 50회/초 | 계정의 일반 private API |
| `korbit:account:<account>:order-place:1second` | 30회/초 | 계정의 주문 생성 API |
| `korbit:account:<account>:order-cancel:1second` | 30회/초 | 계정의 주문 취소 API |

응답의 `Ratelimit` 헤더에서 `remaining` 값을 읽어 로컬 사용량이 거래소 관측값보다 작지 않게 보정합니다. `429`는 요청 제한 오류로 분류하며 거래소의 `Retry-After` 처리는 공통 실행 계층을 따릅니다. 제한값은 `Config`에서 더 보수적으로 조정할 수 있습니다.

여러 EIP를 사용해도 계정 단위 private 제한은 늘어나지 않습니다. route 선택은 API Key IP 허용 목록 준수와 네트워크 격리를 위한 기능이며 거래소 제한 우회 용도가 아닙니다.

## 주문 안전 계약

- 모든 신규 주문에 `[0-9A-Za-z.:_-]{1,36}` 형식의 고유한 `ClientOrderID`를 요구합니다.
- 지정가는 `Price`와 `Qty`를 사용하며 GTC·IOC·FOK·post-only를 지원합니다.
- 시장가·최유리호가 매수는 총액 `Amount`, 매도는 수량 `Qty`를 사용합니다.
- 최유리호가 주문은 1~5의 `BestNth`와 `TimeInForce`가 필요합니다.
- 가격 보호를 켜면 선택적으로 1~100의 `PriceProtectionPercent`를 지정할 수 있습니다.
- 주문 조회와 취소는 `OrderID` 또는 `ClientOrderID` 중 정확히 하나만 허용합니다.
- 미체결·주문·체결 목록은 최대 1,000건입니다. 주문·체결 이력은 현재 기준 최근 36시간 안의 시간만 허용합니다.
- 전체 현재가 조회는 실수로 조회 범위를 넓히지 않도록 `TickersRequest.AllSymbols: true`를 명시해야 합니다.

주문 생성·취소의 전송 오류, 응답 본문 읽기 실패, 성공 응답 JSON 파싱 실패, 거래소 서버 오류는 실제 처리 여부를 단정할 수 없습니다. SDK는 자동 재시도하지 않고 `trade.ErrUnknownExecutionState`로 반환합니다. 같은 `ClientOrderID`로 무조건 재주문하지 말고 `OrderInfo`를 먼저 조회해 거래소 상태를 확인해야 합니다.

## 오류 처리

SDK는 HTTP 상태와 `{success,data,error}` envelope를 모두 검사합니다. `NO_BALANCE`, 주문 없음·이미 종료됨, `INVALID_USER_STATUS`, `TRY_AGAIN`, `EXCEED_TIME_WINDOW`, HTTP 429와 5xx를 공통 `trade.APIError` 범주로 변환하며 거래소의 symbolic message는 `ExchangeCode`에 보존합니다. 인증 헤더, Secret과 서명 원문은 오류에 포함하지 않습니다.

## 공식 기준

- [Korbit Open API 문서](https://docs.korbit.co.kr/)
- [Korbit Open API LLM 문서](https://docs.korbit.co.kr/llms.txt)

