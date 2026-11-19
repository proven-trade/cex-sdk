# Coinone Spot REST·WebSocket 어댑터

구현 기준은 코인원 API v2·v2.1과 REST 기본 주소 `https://api.coinone.co.kr`입니다.

## 전제조건

private API를 사용하려면 `credential.Provider`가 반환하는 `credential.Material`에 다음 값을 넣어야 합니다.

| 필드 | Coinone 값 |
|---|---|
| `APIKey` | Access Token |
| `SecretKey` | Secret Key 원문 |

`credential.Descriptor.AccountID`에는 요청 제한을 공유하는 코인원 포트폴리오의 안정적인 식별자를 넣어야 합니다. 자격증명의 `AllowedEgressRouteIDs` 밖에 있는 route는 Secret 조회 전에 차단됩니다. 코인원 API Key의 허용 IP에는 실제 route와 연결된 공인 송신 IP를 등록해야 합니다.

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

표에 없는 REST API는 [추가 REST API](#추가-rest-api)에 카테고리별로 정리합니다.

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

## 요청 제한과 송신 경로

SDK 기본 로컬 제한은 공식 안내의 공개 1,200회/분, private 일반 80회/초, private 주문 40회/초입니다.

| bucket | 기본 제한 | 범위 |
|---|---:|---|
| `coinone:route:<route>:public:1minute` | 1,200회/분 | 선택한 송신 경로 |
| `coinone:account:<account>:private:1second` | 80회/초 | 포트폴리오의 일반 private API |
| `coinone:account:<account>:order:1second` | 40회/초 | 포트폴리오의 주문 API |

private 일반 API와 주문 API는 별도 bucket입니다. 응답의 `Public-Ratelimit-Remaining`, `Private-Ratelimit-Remaining`, `Private-Order-Ratelimit-Remaining`을 읽어 로컬 사용량이 거래소 관측값보다 작지 않게 보정합니다. 제한값은 `Config`에서 더 보수적으로 조정할 수 있습니다.

여러 공인 송신 IP를 사용해도 포트폴리오 단위 private 제한은 늘어나지 않습니다. route 선택은 API Key IP 허용 목록 준수와 네트워크 격리를 위한 기능이며 거래소 제한 우회 용도가 아닙니다.

## 주문 안전 계약

- `LIMIT`은 `Price`, `Quantity`가 필요하며 `PostOnly`는 `false`도 JSON boolean으로 명시합니다.
- `MARKET BUY`는 총액 `Amount`, `MARKET SELL`은 수량 `Quantity`를 사용합니다. 두 방향 모두 선택적으로 `LimitPrice`를 지정할 수 있습니다.
- `STOP_LIMIT`은 `Price`, `Quantity`, `TriggerPrice`가 필요합니다.
- `UserOrderID`는 소문자 영문자, 숫자, `.`, `_`, `-`만 사용하며 최대 150자입니다. 거래소 전체에서 중복되지 않는 값을 사용해야 합니다.
- 주문 조회와 취소는 `OrderID` 또는 `UserOrderID` 중 정확히 하나만 허용합니다.
- 전체 마켓 주문 목록은 실수로 조회 범위를 넓히지 않도록 `AllMarkets: true`를 명시해야 합니다.
- 종료 주문 조회는 1~100개 크기, 필수 시작·종료 시간, 최대 90일 범위와 미래 시간 금지를 검증합니다.

주문 생성·취소의 전송 오류, 응답 본문 읽기 실패, 성공 응답 JSON 파싱 실패, 거래소 서버 오류는 실제 처리 여부를 단정할 수 없습니다. SDK는 자동 재시도하지 않고 `trade.ErrUnknownExecutionState`로 반환합니다. 고유한 `UserOrderID`를 사용하고 불명확한 결과는 `OrderInfo`로 확인해야 합니다.

## 추가 REST API

`Client`는 아래 REST API도 제공합니다. 모든 메서드는 기존 메서드와 같이 `trade.RequestOption`으로 송신 경로를 선택하고, 가격·수량·금액·수수료는 `Decimal`로, 원본 응답은 각 항목의 `Raw`로 보존합니다. private API는 요청에 필요한 `credential.Permission`을 `Descriptor.Permissions`에서 확인하며, 없으면 Secret 조회와 네트워크 요청 전에 `trade.ErrAuthorization`으로 거절합니다. 읽기 요청은 기존 읽기 재시도 정책을 따르고, 변경 요청인 `CancelAllOrders`와 `Withdraw`는 자동 재시도하지 않습니다.

### 시세·마켓 정보

인증과 권한이 필요 없으며 public bucket을 사용합니다.

| 메서드 | API |
|---|---|
| `RangeUnits` | `GET /public/v2/range_units/{quote_currency}/{target_currency}` |
| `Market` | `GET /public/v2/markets/{quote_currency}/{target_currency}` |
| `Tickers` | `GET /public/v2/ticker_new/{quote_currency}` |
| `UTCTicker` | `GET /public/v2/ticker_utc_new/{quote_currency}/{target_currency}` |
| `UTCTickers` | `GET /public/v2/ticker_utc_new/{quote_currency}` |
| `Currencies` | `GET /public/v2/currencies` |
| `Currency` | `GET /public/v2/currencies/{currency}` |

- `RangeUnits`는 가격 구간별 호가 단위(`RangeMin`, `NextRangeMin`, `PriceUnit`)를 반환합니다. 코인원은 `Market`의 `PriceUnit`을 제거할 예정이라고 안내하므로 호가 단위는 `RangeUnits`로 조회해야 합니다.
- `Market`, `UTCTicker`, `Currency`처럼 개별 조회 응답에 정확히 한 건이 없으면 `trade.ErrExchangeUnavailable`로 반환합니다.
- `Tickers`와 `UTCTickers`는 마켓 전체 종목을 조회합니다. `AdditionalData: true`이면 `yesterday_*` 필드가 함께 오며, UTC 티커에서도 전일 거래대금·거래량(`yesterday_quote_volume`, `yesterday_target_volume`)은 UTC 기준이 아니라 KST 티커와 같은 값입니다.
- `Currencies`와 `Currency`는 입출금 가능 상태(`normal`, `suspended`), 입출금 수수료, 최소 출금 수량(`WithdrawalMinimumAmount`), 출금 가능한 소수점 자릿수(`MaxPrecision`)를 반환합니다.

### 거래

| 메서드 | API | 권한 | 작업 | bucket |
|---|---|---|---|---|
| `CancelAllOrders` | `POST /v2.1/order/cancel/all` | `credential.PermissionTrade` | 변경 | order |

- 통화쌍 하나(`QuoteCurrency`, `TargetCurrency`)에 걸려 있는 모든 주문을 취소합니다.
- 결과의 `CanceledCount`가 `TotalOrderCount`보다 작으면 일부 주문이 남아 있으므로 `ActiveOrders`로 확인해야 합니다.
- 전송 오류, 서버 오류, 성공 응답의 주문 개수 누락은 실제 처리 여부를 단정할 수 없어 `trade.ErrUnknownExecutionState`로 반환합니다. `ActiveOrders`로 상태를 확인하기 전에는 다시 요청하지 않아야 합니다.

### 계정·수수료·주문 리워드

모두 `credential.PermissionRead`가 필요한 읽기 요청이며 일반 private bucket(주문 외 private API 합산 80회/초)을 사용합니다.

| 메서드 | API |
|---|---|
| `Balances` | `POST /v2.1/account/balance` |
| `TradeFees` | `POST /v2.1/account/trade_fee` |
| `TradeFee` | `POST /v2.1/account/trade_fee/{quote_currency}/{target_currency}` |
| `OrderRewardPrograms` | `POST /v2.1/event/order-reward/programs` |
| `OrderRewardHistory` | `POST /v2.1/event/order-reward/history` |

- `Balances`는 지정한 심볼의 잔고만 조회하며 심볼이 하나 이상 필요합니다. 전체 잔고는 `Accounts`로 조회합니다.
- `TradeFees`는 거래 가능한 모든 종목의 maker·taker 수수료율을, `TradeFee`는 통화쌍 하나의 수수료율을 반환합니다.
- `OrderRewardPrograms`는 진행 중인 주문 리워드 종목과 내 점수·기여도·경쟁도를 반환합니다.
- `OrderRewardHistory`의 `From`, `To`는 초 단위 Unix 시각으로 전송하며 비우면 각각 시작 제한 없음과 현재 시점입니다. 코인원은 기간 안에서 최근일부터 역순으로 최대 30일치만 반환합니다.

### 입출금

모두 일반 private bucket을 사용합니다.

| 메서드 | API | 권한 | 작업 |
|---|---|---|---|
| `DepositAddresses` | `POST /v2.1/account/deposit_address` | `credential.PermissionRead` | 읽기 |
| `KRWTransactionHistory` | `POST /v2.1/transaction/krw/history` | `credential.PermissionRead` | 읽기 |
| `CoinTransactionHistory` | `POST /v2.1/transaction/coin/history` | `credential.PermissionRead` | 읽기 |
| `CoinTransactionDetail` | `POST /v2.1/transaction/coin/history/detail` | `credential.PermissionRead` | 읽기 |
| `WithdrawalLimit` | `POST /v2.1/transaction/coin/withdrawal/limit` | `credential.PermissionRead` | 읽기 |
| `WithdrawalAddressBook` | `POST /v2.1/transaction/coin/withdrawal/address_book` | `credential.PermissionRead` | 읽기 |
| `Withdraw` | `POST /v2.1/transaction/coin/withdrawal` | `credential.PermissionWithdraw` | 변경 |

- `DepositAddresses`는 이미 발급된 입금 주소를 조회할 뿐 새 주소를 만들지 않으므로 Read 권한으로 호출합니다. 요청의 `Currencies` 필드를 비우면 전체를 조회하고 입금이 불가능한 가상자산은 응답에서 제외됩니다. `SecondaryAddress`는 Memo·Tag가 없으면 빈 문자열입니다.
- `KRWTransactionHistory`와 `CoinTransactionHistory`의 `Type`을 비우면 입금과 출금을 모두 조회하고 `TransferTypeDeposit`, `TransferTypeWithdrawal`이면 `is_deposit`을 지정합니다. `Size`는 1~100이며 비우면 100건이고 `ToID`는 해당 ID 이전 내역을 조회하는 cursor입니다.
- 입출금 내역의 `From`, `To`는 millisecond로 전송하며 비우면 각각 현재 시점 90일 전과 현재 시점입니다. 90일을 넘는 기간, `From`이 `To`보다 늦거나 같은 기간, 미래 시각은 요청 전에 거절합니다.
- `CoinTransactionDetail`은 출금 응답의 ID로 입출금 한 건의 진행 상태를 조회하며 응답에 정확히 한 건이 없으면 `trade.ErrExchangeUnavailable`로 반환합니다.
- 코인원은 포트폴리오 API Key로 원화·가상자산 입출금 내역, 단건 조회, 출금 한도, 출금 주소 목록, 출금을 지원하지 않습니다. 포트폴리오를 지원하지 않는 API를 뜻하는 error code 315(`The API does not support a portfolio.`)는 SDK가 `trade.ErrAuthorization`으로 분류합니다.
- `Withdraw`는 코인원 홈페이지 > Open API > API 출금주소록에 추가 정보 등록과 추가 채널 인증을 마친 주소로만 출금할 수 있습니다. `Address`와 `SecondaryAddress`는 대소문자를 구분하므로 SDK가 정규화하지 않습니다.
- `Descriptor.Permissions`에 `credential.PermissionWithdraw`를 명시한 자격증명만 `Withdraw`를 호출할 수 있습니다. 전송 오류, 서버 오류, 응답 해석 실패, 응답의 transaction ID 누락은 출금 접수 여부를 단정할 수 없어 `trade.ErrUnknownExecutionState`로 반환하며 자동 재시도하지 않습니다. `CoinTransactionHistory`로 접수 여부를 확인하기 전에는 같은 출금을 다시 요청하지 않아야 합니다.
- `Withdraw` 성공 응답의 `Withdrawal.ID`는 `CoinTransactionDetail`로 진행 상태를 확인할 때 사용합니다. 거래소가 error code로 거절한 출금(예: 154 API 출금 주소 미등록, 3003 일일 출금 한도 초과)은 `ExchangeCode`와 `ExchangeMessage`를 보존한 `trade.APIError`로 반환합니다.

## 오류 처리

코인원은 HTTP 200에서도 `result: "error"`와 0이 아닌 `error_code`를 반환할 수 있습니다. SDK는 HTTP 상태와 JSON envelope를 모두 검사하며 인증, 권한·IP, 잔고 부족, 주문 없음, 요청 제한, 거래소 장애를 공통 `trade.APIError`로 분류합니다. `error_code`와 `error_msg` 원문도 보존하지만 인증 헤더와 서명 원문은 오류에 포함하지 않습니다.

## WebSocket

`StreamClient`는 public `wss://stream.coinone.co.kr`과 private `wss://stream.coinone.co.kr/v1/private`를 분리합니다. `PublicStream`과 `PrivateStream`을 생성할 때 선택한 송신 경로는 해당 세션의 모든 재연결에서도 고정됩니다.

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

### Spot 로컬 오더북 snapshot

코인원 `ORDERBOOK`은 최초 구독 시 마지막 전체 오더북을 한 번 보내고, 이후 변경 때마다 갱신된 전체 asks·bids를 제공합니다. `id`는 숫자 문자열이며 값이 클수록 최신 장부입니다. 연속성을 뜻하는 sequence는 아니므로 임의로 `id+1`을 요구하지 않고, 같은 연결 세대에서 이전 값보다 크지 않은 중복·역행 snapshot만 무시합니다.

```go
public, err := streams.PublicStream(
	coinone.StreamRequest{Subscriptions: []coinone.StreamSubscription{{
		Channel: coinone.StreamChannelOrderBook,
		Topics: []coinone.StreamTopic{{
			QuoteCurrency:  "KRW",
			TargetCurrency: "BTC",
		}},
		Format: coinone.StreamFormatShort,
	}}},
	trade.WithEgressRoute("seoul-b"),
)
if err != nil {
	return err
}
defer public.Close()

book, err := coinone.NewLocalOrderBook(coinone.LocalOrderBookConfig{
	QuoteCurrency:  "KRW",
	TargetCurrency: "BTC",
	EgressRouteID:  "seoul-b",
	ViewDepth:      16,
})
if err != nil {
	return err
}

err = book.Run(ctx, public, func(ctx context.Context, view coinone.LocalOrderBookView) error {
	return consumeBook(view)
})
```

운영 계약은 다음과 같습니다.

- `DEFAULT`와 `SHORT`를 모두 같은 typed snapshot으로 처리합니다.
- 공식 응답의 asks와 bids는 높은 가격부터 내려옵니다. view는 공통 사용성을 위해 bids는 높은 가격부터, asks는 낮은 가격부터인 best-first 순서로 제공합니다.
- 각 방향 최대 16단계를 검증하고 `ViewDepth` 기본값도 16으로 둡니다.
- 로컬 오더북과 WebSocket의 통화쌍·`EgressRouteID`가 다르면 연결 전에 거부합니다.
- `LocalOrderBook.Run`이 대상 구독의 수명주기를 소유하므로 실행 중 해당 `ORDERBOOK` 구독을 제거하지 않아야 합니다.
- 네트워크 재연결 시 같은 송신 경로로 현재 구독을 복구하고 새 세대의 첫 전체 snapshot을 받아들입니다. `SnapshotID`, `Generation`, millisecond `Timestamp`, `SourceID`로 상태를 관측합니다.
- source `id`는 최신성 비교값일 뿐 연속 sequence가 아니므로 gap count를 만들거나 REST snapshot과 섞지 않습니다.

## 공통 Spot API

`NewUnifiedSpot`은 native 클라이언트를 `unified.SpotClient`로 변환합니다. Coinone API가 기준 통화와 대상 통화를 별도 필드로 사용하므로 공통 `BTC/KRW`의 `NativeMarket`은 SDK 표기인 `KRW-BTC`로 합성합니다. 인자 없는 공통 `Markets`는 현재 원화 마켓을 조회합니다.

시장가 매수의 `QuoteAmount`는 `amount`, 시장가 매도의 `Quantity`는 `qty`로 전송합니다. 지정가는 GTC와 post-only를 지원하며 Coinone에 대응 계약이 없는 IOC·FOK는 요청 전에 거절합니다. 미체결 예약가는 공통 계약에 별도 stop 유형이 없으므로 `OrderTypeLimit`로 분류하고 원본 유형은 `Raw`에 보존합니다.

Coinone 주문 상세의 현재 `order` 객체 응답과 이전 평면 응답을 모두 해석합니다. `LIVE`, 부분 체결, 완료, 부분·전체·자동 취소 상태를 공통 주문 상태로 변환합니다.

## 공식 기준

- [Coinone API 문서](https://docs.coinone.co.kr/)
- [Public API 안내](https://docs.coinone.co.kr/docs/about-public-api)
- [요청 횟수 제한 안내](https://docs.coinone.co.kr/docs/ratelimit-%EC%95%88%EB%82%B4)
- [오류 코드](https://docs.coinone.co.kr/docs/error-code)
- [Public WebSocket](https://docs.coinone.co.kr/reference/public-websocket-1)
- [WebSocket 오더북](https://docs.coinone.co.kr/reference/public-websocket-orderbook)
- [Private WebSocket](https://docs.coinone.co.kr/reference/private-websocket-1)
- [포트폴리오 기능 신규 지원](https://docs.coinone.co.kr/changelog/%ED%8F%AC%ED%8A%B8%ED%8F%B4%EB%A6%AC%EC%98%A4-%EA%B8%B0%EB%8A%A5-%EC%8B%A0%EA%B7%9C-%EC%A7%80%EC%9B%90)
- [개별 호가 단위 조회](https://docs.coinone.co.kr/reference/range-unit)
- [개별 종목 정보 조회](https://docs.coinone.co.kr/reference/market)
- [전체 티커 정보 조회](https://docs.coinone.co.kr/reference/tickers)
- [전체 티커 정보 조회 (UTC)](https://docs.coinone.co.kr/reference/utc-tickers)
- [개별 티커 정보 조회 (UTC)](https://docs.coinone.co.kr/reference/utc-ticker)
- [전체 가상자산 정보 조회](https://docs.coinone.co.kr/reference/currencies)
- [개별 가상자산 정보 조회](https://docs.coinone.co.kr/reference/currency)
- [종목 별 전체 주문 취소](https://docs.coinone.co.kr/reference/cancel-orders)
- [특정 자산 잔고 조회](https://docs.coinone.co.kr/reference/find-balance-by-currencies)
- [전체 수수료 조회](https://docs.coinone.co.kr/reference/find-all-trade-fees)
- [개별 종목 수수료 조회](https://docs.coinone.co.kr/reference/find-trade-fee-by-pair)
- [주문 리워드 종목 정보 조회](https://docs.coinone.co.kr/reference/order-reward-programs)
- [주문 리워드 내역 조회](https://docs.coinone.co.kr/reference/order-reward-history)
- [가상자산 입금 주소 조회](https://docs.coinone.co.kr/reference/virtual-asset-deposit-address)
- [원화 입출금 내역 조회](https://docs.coinone.co.kr/reference/krw-transaction-history)
- [가상자산 입출금 내역 조회](https://docs.coinone.co.kr/reference/coin-transaction-history)
- [가상자산 입출금 내역 단건 조회](https://docs.coinone.co.kr/reference/single-coin-transaction-history)
- [가상자산 출금 한도 조회](https://docs.coinone.co.kr/reference/coin-withdrawal-limit)
- [출금 주소 목록 조회](https://docs.coinone.co.kr/reference/coin-withdrawal-address-book)
- [가상자산 출금](https://docs.coinone.co.kr/reference/coin-withdrawal)
