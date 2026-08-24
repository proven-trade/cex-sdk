# Coinone Spot REST·WebSocket 어댑터

구현 기준은 코인원 API v2·v2.1과 REST 기본 주소 `https://api.coinone.co.kr`입니다.

## 전제조건

private API를 사용하려면 `credential.Provider`가 반환하는 `credential.Material`에 다음 값을 넣어야 합니다.

| 필드 | Coinone 값 |
|---|---|
| `APIKey` | Access Token |
| `SecretKey` | Secret Key 원문 |

`credential.Descriptor.AccountID`에는 요청 제한을 공유하는 코인원 포트폴리오의 안정적인 식별자를 넣어야 합니다. 자격증명의 `AllowedEgressRouteIDs` 밖에 있는 route는 Secret 조회 전에 차단됩니다. 코인원 API Key의 허용 IP에는 실제 route와 연결된 EIP를 등록해야 합니다.

## 지원 범위

| 영역 | 메서드 | API |
|---|---|---|
| 마켓 | `Markets` | `GET /public/v2/markets/{quote_currency}` |
| 공개 시세 | `OrderBook`, `RecentTrades`, `Ticker`, `Candles` | public v2 |
| 계정 | `Accounts` | `POST /v2.1/account/balance/all` |
| 주문 생성 | `PlaceOrder` | `POST /v2.1/order` |
| 주문 상세·취소 | `OrderInfo`, `CancelOrder` | private v2.1 |
| 주문 목록 | `ActiveOrders`, `CompletedOrders` | private v2.1 |
| 공개 stream | `ORDERBOOK`, `TICKER`, `TRADE`, `CHART` | public WebSocket |
| 개인 stream | `MYORDER`, `MYASSET` | private WebSocket v1 |

가격, 수량, 금액, 수수료는 `float64`로 변환하지 않고 `Decimal` 문자열로 보존합니다. 각 항목의 `Raw`에는 해당 응답의 원본 JSON을 보존합니다.

## 인증과 본문 무결성

인증 요청은 요청 제한 대기가 끝난 뒤 최종 본문을 만들고 서명합니다.

1. 요청마다 UUID v4 nonce를 생성합니다.
2. JSON 본문의 첫 필드에 `access_token`, 두 번째 필드에 `nonce`를 넣고 endpoint 필드를 뒤에 직렬화합니다.
3. 전송할 JSON bytes를 표준 Base64로 변환해 `X-COINONE-PAYLOAD`에 넣습니다.
4. Base64 문자열을 Secret Key로 HMAC-SHA512 서명하고 소문자 hex 값을 `X-COINONE-SIGNATURE`에 넣습니다.
5. Base64를 만들 때 사용한 것과 정확히 같은 JSON bytes를 HTTP 본문으로 전송합니다.

최종 JSON의 공백·필드 순서·자료형이 바뀌면 서명이 달라집니다. SDK는 인증 헤더와 본문을 한 번의 build 단계에서 만들며, 생성된 mutable 본문과 Provider가 반환한 자격증명 byte slice는 요청이 끝난 뒤 덮어씁니다. Go 문자열과 네트워크 계층 내부 복사본까지 완전히 지울 수 있다는 보장은 하지 않습니다.

`NonceSource`는 결정적인 테스트를 위한 주입 지점입니다. 운영에서는 기본 암호학적 난수 UUID v4 생성기를 사용해야 합니다.

## 요청 제한과 EIP

SDK 기본 로컬 제한은 공식 안내의 공개 1,200회/분, private 일반 80회/초, private 주문 40회/초입니다.

| bucket | 기본 제한 | 범위 |
|---|---:|---|
| `coinone:route:<route>:public:1minute` | 1,200회/분 | 선택한 EIP route |
| `coinone:account:<account>:private:1second` | 80회/초 | 포트폴리오의 일반 private API |
| `coinone:account:<account>:order:1second` | 40회/초 | 포트폴리오의 주문 API |

private 일반 API와 주문 API는 별도 bucket입니다. 응답의 `Public-Ratelimit-Remaining`, `Private-Ratelimit-Remaining`, `Private-Order-Ratelimit-Remaining`을 읽어 로컬 사용량이 거래소 관측값보다 작지 않게 보정합니다. 제한값은 `Config`에서 더 보수적으로 조정할 수 있습니다.

여러 EIP를 사용해도 포트폴리오 단위 private 제한은 늘어나지 않습니다. route 선택은 API Key IP 허용 목록 준수와 네트워크 격리를 위한 기능이며 거래소 제한 우회 용도가 아닙니다.

## 주문 안전 계약

- `LIMIT`은 `Price`, `Quantity`가 필요하며 `PostOnly`는 `false`도 JSON boolean으로 명시합니다.
- `MARKET BUY`는 총액 `Amount`, `MARKET SELL`은 수량 `Quantity`를 사용합니다. 두 방향 모두 선택적으로 `LimitPrice`를 지정할 수 있습니다.
- `STOP_LIMIT`은 `Price`, `Quantity`, `TriggerPrice`가 필요합니다.
- `UserOrderID`는 소문자 영문자, 숫자, `.`, `_`, `-`만 사용하며 최대 150자입니다. 거래소 전체에서 중복되지 않는 값을 사용해야 합니다.
- 주문 조회와 취소는 `OrderID` 또는 `UserOrderID` 중 정확히 하나만 허용합니다.
- 전체 마켓 주문 목록은 실수로 조회 범위를 넓히지 않도록 `AllMarkets: true`를 명시해야 합니다.
- 종료 주문 조회는 1~100개 크기, 필수 시작·종료 시간, 최대 90일 범위와 미래 시간 금지를 검증합니다.

주문 생성·취소의 전송 오류, 응답 본문 읽기 실패, 성공 응답 JSON 파싱 실패, 거래소 서버 오류는 실제 처리 여부를 단정할 수 없습니다. SDK는 자동 재시도하지 않고 `trade.ErrUnknownExecutionState`로 반환합니다. 고유한 `UserOrderID`를 사용하고 불명확한 결과는 `OrderInfo`로 확인해야 합니다.

## 오류 처리

코인원은 HTTP 200에서도 `result: "error"`와 0이 아닌 `error_code`를 반환할 수 있습니다. SDK는 HTTP 상태와 JSON envelope를 모두 검사하며 인증, 권한·IP, 잔고 부족, 주문 없음, 요청 제한, 거래소 장애를 공통 `trade.APIError`로 분류합니다. `error_code`와 `error_msg` 원문도 보존하지만 인증 헤더와 서명 원문은 오류에 포함하지 않습니다.

## WebSocket

`StreamClient`는 public `wss://stream.coinone.co.kr`과 private `wss://stream.coinone.co.kr/v1/private`를 분리합니다. `PublicStream`과 `PrivateStream`을 생성할 때 선택한 EIP route는 해당 세션의 모든 재연결에서도 고정됩니다.

private handshake는 매 연결 세대마다 Secret Provider를 다시 조회하고 다음 JSON을 새 nonce와 현재 millisecond timestamp로 생성합니다.

```json
{"access_token":"...","nonce":"...","timestamp":1700000000123}
```

이 JSON bytes를 REST와 같은 Base64 payload와 HMAC-SHA512 hex 서명으로 만들어 `X-COINONE-PAYLOAD`, `X-COINONE-SIGNATURE` 헤더에 넣습니다. 인증 자격증명과 route 허용 관계는 첫 Secret 조회 전에 검사하며 인증 문제를 뜻하는 close code 4280은 같은 잘못된 인증으로 무한 재연결하지 않습니다.

| 구분 | 지원 채널 |
|---|---|
| public | `ORDERBOOK`, `TICKER`, `TRADE`, `CHART` |
| private | `MYORDER`, `MYASSET` |

- `DEFAULT`와 `SHORT` 형식을 모두 지원하며 `StreamMessage.Decode`는 두 형식을 동일한 typed event로 변환합니다.
- `Subscribe`와 `Unsubscribe`로 실행 중 구독을 변경할 수 있고, 성공적으로 전송된 현재 구독은 재연결 때 자동 복구합니다.
- public 채널은 구독 한 건당 거래쌍 하나를 사용합니다. `CHART`는 공식 WebSocket 구간만 허용합니다.
- `MYORDER`는 topic을 생략해 전체 주문을 받거나 여러 거래쌍을 배열로 지정할 수 있습니다. `MYASSET`은 topic을 받지 않습니다.
- WebSocket control ping과 별개로 공식 JSON `{"request_type":"PING"}`을 기본 10분마다 보내고 `PONG`을 기다려 30분 세션 만료를 갱신합니다.
- 코인원 제한은 public IP당 최대 20개, private 계정당 최대 20개 연결입니다. SDK의 개별 세션은 이를 우회하지 않으며 close code 4290을 원본 연결 오류로 전달합니다.
- `CONNECTED`, `SUBSCRIBED`, `UNSUBSCRIBED`, `PONG`, `ERROR`도 `StreamMessage`로 handler에 전달합니다. 오류 응답의 `error_code`와 `message`를 보존합니다.

구독 변경과 heartbeat write는 연결별로 직렬화합니다. 한 세션의 handler도 수신 순서대로 호출되므로 느린 처리는 사용자 애플리케이션에서 별도 bounded queue로 분리해야 합니다.

## 공식 기준

- [Coinone API 문서](https://docs.coinone.co.kr/)
- [Public API 안내](https://docs.coinone.co.kr/docs/about-public-api)
- [요청 횟수 제한 안내](https://docs.coinone.co.kr/docs/ratelimit-%EC%95%88%EB%82%B4)
- [오류 코드](https://docs.coinone.co.kr/docs/error-code)
- [Public WebSocket](https://docs.coinone.co.kr/reference/public-websocket-1)
- [Private WebSocket](https://docs.coinone.co.kr/reference/private-websocket-1)
