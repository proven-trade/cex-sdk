# Crypto.com Exchange v1 Spot 구현 계획

## 기준

- 공식 문서: [Crypto.com Exchange API v1](https://exchange-developer.crypto.com/exchange/v1)
- Production REST: `https://api.crypto.com/exchange/v1/{method}`
- UAT REST: `https://uat-api.3ona.co/exchange/v1/{method}`
- Production market WebSocket: `wss://stream.crypto.com/exchange/v1/market`
- Production user WebSocket: `wss://stream.crypto.com/exchange/v1/user`
- UAT market WebSocket: `wss://uat-stream.3ona.co/exchange/v1/market`
- UAT user WebSocket: `wss://uat-stream.3ona.co/exchange/v1/user`
- 초기 상품: Spot

공식 2026년 변경 로그가 유지되는 현행 Exchange v1만 대상으로 한다. 구형 기본 `book.{instrument_name}` 구독과 100ms full snapshot 구독은 이미 폐기됐으므로 구현하지 않는다. Margin·Derivatives 전용 상품 라인은 별도 상품 단계로 확장하며, Spot에서 쓸 수 있는 고급 조건부 주문과 입출금 REST는 아래 「추가 REST API」에 구현되어 있다.

현재 `exchange/cryptocom`의 공개·private REST, 공통 Spot API, public market·private user WebSocket, 로컬 오더북과 mock 자동 테스트가 구현되어 있다. 실제 계정 검증 전이므로 live smoke 상태는 `예정`으로 유지한다.

## 구현 범위

### 공개 REST

- `public/get-instruments`의 상품·거래 가능 상태·가격·수량 tick 규칙 구현 완료
- `public/get-tickers`의 단일·전체 ticker 구현 완료
- `public/get-book`의 최대 50단계 호가 snapshot 구현 완료
- `public/get-trades`의 최근 체결 구현 완료
- `public/get-candlestick`의 공식 timeframe 캔들 구현 완료
- 요청별 `egressRouteId`와 route별 독립 HTTP 연결 풀 구현 완료
- public 메서드별 IP 기준 100회/초 제한과 HTTP 429·`42901` 보정 구현 완료

공식 공통 규격은 숫자 필드를 문자열로 보내도록 요구한다. 가격·수량·금액은 decimal 원문을 보존하고, 식별자·millisecond·nanosecond 시각은 JSON 문자열과 숫자를 모두 안전하게 해석하되 범위를 넘는 값을 `float64`로 변환하지 않는다.

| 영역 | 메서드 | API |
|---|---|---|
| 상품 규칙 | `Instruments` | `GET public/get-instruments` |
| 단일 ticker | `Ticker` | `GET public/get-tickers?instrument_name=...` |
| 전체 ticker | `Tickers` | `GET public/get-tickers` |
| 호가 snapshot | `OrderBook` | `GET public/get-book` |
| 최근 체결 | `RecentTrades` | `GET public/get-trades` |
| 캔들 | `Candles` | `GET public/get-candlestick` |

기본 endpoint는 `DefaultBaseURL`, UAT endpoint는 `DefaultUATBaseURL`로 제공한다. `Ticker`·호가·체결·캔들은 대문자 underscore 형식의 Spot `instrument_name`을 입력받고, 호가 depth는 1~50만 허용한다. 캔들은 공식 `1m`·`5m`·`15m`·`30m`·`1h`·`2h`·`4h`·`12h`·`1D`·`7D`·`14D`·`1M`을 상수로 제공한다.

```go
client, err := cryptocom.New(cryptocom.Config{
	Executor:             executor,
	DefaultEgressRouteID: "seoul-a",
})
if err != nil {
	return err
}

book, err := client.OrderBook(
	ctx,
	cryptocom.OrderBookRequest{InstrumentName: "BTC_USDT", Depth: 50},
	trade.WithEgressRoute("seoul-b"),
)
```

공개 제한은 `(route, method)`별 기본 100회/초 bucket으로 분리하고 더 낮은 값만 설정할 수 있다. HTTP 429의 `Retry-After`는 선택한 route와 메서드 bucket을 차단한다. `40001`, `40101`, `40801`, `42901`, `50001`은 공통 validation·authentication·exchange unavailable·rate limited 오류로 변환하고, 오류의 `original`은 요청 원문이나 민감 값이 포함될 수 있으므로 보존하지 않는다.

### private REST

- `private/user-balance`의 통화별 가용·예약 잔고 구현 완료
- `private/create-order`의 Spot LIMIT·MARKET, GTC·IOC·FOK와 POST_ONLY 구현 완료
- `private/get-order-detail`, `private/cancel-order` 구현 완료
- `private/get-open-orders`, `private/get-order-history`, `private/get-trades` 구현 완료
- API Key IP whitelist와 SDK credential route 허용 목록의 사전 일치 검사 구현 완료
- 전송 불명확 mutation을 `UNKNOWN_EXECUTION_STATE`로 분류하고 자동 재전송 금지 구현 완료

private 요청은 `method + id + api_key + paramsString + nonce`를 Secret Key로 HMAC SHA-256하고 소문자 hex 서명을 만든다. `paramsString`은 객체 key를 재귀적으로 정렬하고 배열 순서를 유지해 연결한다. signer golden vector는 중첩 객체·배열·빈 params·decimal 문자열을 포함하며, 서명 뒤 payload를 변경하지 않는다.

공식 제한 단위를 그대로 분리한다. 주문 생성·취소는 메서드별 API Key 기준 15회/100ms, 주문 상세는 30회/100ms, 체결·주문 이력은 각각 1회/초, 나머지 private 메서드는 각각 3회/100ms다. 허용 route와 `read`·`trade` 권한을 Secret 조회 전에 확인하고 사용한 민감 byte slice는 즉시 덮어쓴다.

주문 응답은 matching engine의 최종 승인이 아니라 비동기 접수다. `client_oid`를 필수 안전 식별자로 사용하고 주문 상세 또는 `user.order`로 `PENDING` 이후의 `ACTIVE`·`FILLED`·`CANCELED`·`REJECTED` 상태를 확인한다. 취소도 접수 응답만으로 최종 취소로 단정하지 않는다.

| 영역 | 메서드 | API |
|---|---|---|
| 계정 잔고 | `Balance` | `POST private/user-balance` |
| 주문 생성 | `PlaceOrder` | `POST private/create-order` |
| 주문 상세 | `OrderInfo` | `POST private/get-order-detail` |
| 주문 취소 | `CancelOrder` | `POST private/cancel-order` |
| 미체결 주문 | `OpenOrders` | `POST private/get-open-orders` |
| 종료 주문 이력 | `OrderHistory` | `POST private/get-order-history` |
| 계정 체결 이력 | `AccountTrades` | `POST private/get-trades` |

```go
client, err := cryptocom.New(cryptocom.Config{
	Executor: executor,
	Credentials: &credential.Descriptor{
		AccountID: "cryptocom-main",
		Exchange: model.ExchangeCryptoCom,
		SecretRef: "secret/cryptocom-main",
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

receipt, err := client.PlaceOrder(
	ctx,
	cryptocom.PlaceOrderRequest{
		InstrumentName: "BTC_USDT",
		Side: cryptocom.OrderSideBuy,
		Type: cryptocom.OrderTypeLimit,
		Price: "50000",
		Quantity: "0.0001",
		ClientOrderID: "strategy-20260825-1",
		PostOnly: true,
	},
	trade.WithEgressRoute("seoul-b"),
)
```

Spot MARKET 매수는 base 수량이 아니라 quote 지출액을 `Notional`로 받고, MARKET 매도와 LIMIT은 base `Quantity`를 받는다. MARKET은 가격·time-in-force·post-only를 거부한다. LIMIT의 POST_ONLY는 `GOOD_TILL_CANCEL` 또는 생략만 허용하며, `ClientOrderID`는 모든 주문에서 36바이트 이내 필수다.

private 본문의 `id`, `nonce`, 주문 ID, 시각, 개수와 decimal은 문자열로 직렬화한다. `ParamsString`과 `Sign`은 중첩 객체를 재귀적으로 정렬하고 배열 순서를 유지하며, 숫자형 params를 거부해 서명값과 전송 JSON이 달라지는 일을 막는다. 요청 제한 대기 뒤에만 Secret을 조회하고 사용한 key·secret과 요청 body byte slice를 덮어쓴다.

주문 생성·취소의 전송 오류, HTTP 5xx·`40801`·`50001`, 성공 응답 손상은 자동 재시도하지 않고 `UNKNOWN_EXECUTION_STATE`로 반환한다. 명시적인 `306`은 잔고 부족, 주문 조회·취소의 `212`는 주문 없음으로 분류한다. HTTP 429와 `Retry-After`는 계정·메서드별 private limiter에도 반영한다.

### 공통 Spot API

- `Base`·`Quote`와 underscore 형식 Spot `instrument_name`의 양방향 변환 구현 완료
- `CCY_PAIR` 상품, ticker, order book, 체결, candle, 잔고와 주문 계약 정규화 구현 완료
- MARKET 매수·매도 수량 의미와 GTC·IOC·FOK·POST_ONLY 변환·검증 구현 완료
- 원본 응답과 미래 필드는 민감 정보를 제외하고 보존
- 공통 적합성 스위트와 모든 요청의 송신 경로 전달 검증 구현 완료

공통 `Balances`는 `position_balances`의 `max_withdrawal_balance`와 `reserved_qty`를 각각 `Available`과 `Locked`로 매핑한다. 공통 3분 캔들은 공식 1분 캔들을 epoch 기준으로 묶고 decimal 문자열을 정수 비례 값으로 더해 부동소수점 정밀도 손실을 피한다.

주문 취소 응답은 비동기 접수이므로 공통 상태를 즉시 `canceled`로 단정하지 않고 `unknown`으로 반환한다. 이후 `Order` 또는 private WebSocket 주문 이벤트로 최종 상태를 확인해야 한다. 사용자 주문 ID가 없으면 36바이트 제한 안에서 `proven-` 접두사의 암호학적 난수를 생성한다.

### WebSocket

- market의 `ticker`, `trade`, `candlestick`, 명시적 `book.{instrument_name}.{depth}` 구독 구현 완료
- user의 `public/auth` 후 `user.order`, `user.trade`, `user.balance` 구독 구현 완료
- `public/heartbeat`의 동일 ID를 `public/respond-heartbeat`로 5초 안에 응답 구현 완료
- 연결 직후 비례 요청 제한을 피하기 위한 공식 권장 1초 준비 구간 구현 완료
- market 100회/초와 user 150회/초 command 제한 구현 완료
- 동적 구독·해지의 실패 응답 rollback 구현 완료
- market·user 재연결마다 같은 송신 경로 유지와 목표 구독 복구 구현 완료

| 공개 채널 | SDK 채널 | 채널 문자열 |
|---|---|---|
| 현재가·최우선 호가 | `StreamChannelTicker` | `ticker.{instrument_name}` |
| 공개 체결 | `StreamChannelTrades` | `trade.{instrument_name}` |
| 캔들 | `StreamChannelCandles` | `candlestick.{timeframe}.{instrument_name}` |
| 호가 | `StreamChannelBook` | `book.{instrument_name}.{10|50}` |

| private 채널 | SDK 채널 | 채널 문자열 |
|---|---|---|
| 주문 | `StreamChannelUserOrders` | `user.order[.{instrument_name}]` |
| 계정 체결 | `StreamChannelUserTrades` | `user.trade[.{instrument_name}]` |
| 계정 잔고 | `StreamChannelUserBalances` | `user.balance` |

`NewStreamClient`는 production market·user endpoint를 기본값으로 사용하고 UAT 상수도 별도로 제공한다. `PublicStream`·`PrivateStream` 생성 때 선택한 `egressRouteId`는 모든 재연결에서 유지된다. 구독 command의 ID·nonce·호가 갱신 간격은 문자열로 직렬화하며, ticker wildcard와 폐기된 기본 깊이 book은 거부한다.

호가 구독은 `SNAPSHOT` 또는 `SNAPSHOT_AND_UPDATE`를 명시해야 한다. full snapshot은 현행 500ms만 허용하고, 증분형은 공식 10·100·500ms 값을 지원한다. 원본 snapshot·delta를 typed event로 전달하며 `LocalOrderBook`이 검증된 로컬 장부 view를 제공한다.

private user 연결은 API Key whitelist route와 `read` 권한을 Secret 조회 전에 검사한다. Secret은 연결·재연결 인증 시점마다 Provider에서 조회하고 서명 직후 민감 byte slice를 덮어쓴다. 인증 성공 뒤 목표 구독을 복구하며 인증 실패는 자동 재연결하지 않는다. 주문 command를 WebSocket으로 보내는 기능은 첫 단계에서 제외하고 REST mutation과 user event 조합을 먼저 안정화한다.

```go
streamClient, err := cryptocom.NewStreamClient(cryptocom.StreamClientConfig{
	Connector: connector,
	Credentials: &credential.Descriptor{
		AccountID: "cryptocom-main",
		Exchange: model.ExchangeCryptoCom,
		SecretRef: "secret/cryptocom-main",
		Permissions: []credential.Permission{credential.PermissionRead},
		AllowedEgressRouteIDs: []transport.EgressRouteID{"seoul-a", "seoul-b"},
	},
	CredentialProvider: provider,
	DefaultEgressRouteID: "seoul-a",
})
if err != nil {
	return err
}

private, err := streamClient.PrivateStream(
	cryptocom.StreamRequest{Subscriptions: []cryptocom.StreamSubscription{
		{Channel: cryptocom.StreamChannelUserOrders, InstrumentName: "BTC_USDT"},
		{Channel: cryptocom.StreamChannelUserBalances},
	}},
	trade.WithEgressRoute("seoul-b"),
)
```

### 로컬 오더북

현행 명시적 10·50단계 `SNAPSHOT_AND_UPDATE`를 사용하도록 구현 완료했다. 최초 `book` full snapshot의 `u`를 기준으로 `book.update`의 `pu`가 직전 `u`와 같은지 검사하고, 수량 0은 해당 가격을 삭제한다. 서버가 큰 delta 대신 full snapshot을 보내면 기존 장부를 원자적으로 교체한다.

`pu` gap, update ID 역행, snapshot 이전 delta 또는 연결 세대 변경이 발생하면 불완전한 view를 공개하지 않고 같은 송신 경로로 재구독해 새 full snapshot부터 복구한다. 공식 REST book에는 WebSocket `u`와 연결할 sequence가 없으므로 임의로 결합하지 않는다. 빈 delta heartbeat도 유효한 `u`·`pu` 연결로 처리한다.

```go
book, err := cryptocom.NewLocalOrderBook(cryptocom.LocalOrderBookConfig{
	InstrumentName: "BTC_USDT",
	Depth: cryptocom.StreamBookDepth10,
	UpdateFrequency: cryptocom.StreamBookUpdate100Milliseconds,
	EgressRouteID: "seoul-b",
})
if err != nil {
	return err
}

public, err := streamClient.PublicStream(
	cryptocom.StreamRequest{Subscriptions: []cryptocom.StreamSubscription{
		book.Subscription(),
	}},
	trade.WithEgressRoute("seoul-b"),
)
if err != nil {
	return err
}

err = book.Run(ctx, public, func(_ context.Context, view cryptocom.LocalOrderBookView) error {
	return consume(view)
})
```

## 추가 REST API

공식 REST 문서에서 확인한 추가 메서드다. 모두 `*Client` 메서드이며 요청별 `trade.RequestOption`(송신 경로)을 받는다. mutation은 자동 재시도하지 않고, 전송 결과가 불명확하면 `UNKNOWN_EXECUTION_STATE`로 반환한다. 주문·취소·정정 응답은 비동기 접수 확인일 뿐이므로 `user.order`·주문 상세로 최종 상태를 확인한다. 요청 제한은 `create-order`·`cancel-order`·`cancel-all-orders`만 15회/100ms 주문 bucket이고 나머지 private 메서드는 메서드별 3회/100ms bucket이다.

### 공개 시세 (권한 불필요, IP 기준 100회/초)

| 메서드 | API |
|---|---|
| `GetValuations` | `GET public/get-valuations` (index·mark·funding 시계열) |
| `DepositInstruments` | `GET public/get-deposit-instruments` |
| `GetRiskParameters` | `GET public/get-risk-parameters` |

### 주문 (권한 `trade`)

| 메서드 | API |
|---|---|
| `AmendOrder` | `POST private/amend-order` (취소 후 재생성, 수량 감소만 큐 우선순위 유지) |
| `CancelAllOrders` | `POST private/cancel-all-orders` (`LIMIT`·`ALL`) |
| `CreateOrderList` | `POST private/create-order-list` (`contingency_type=LIST`, 1~10건, 항목별 `code`) |
| `CancelOrderList` | `POST private/cancel-order-list` (항목별 `code`) |
| `CreateAdvancedOrder` | `POST private/advanced/create-order` (STOP_LOSS·STOP_LIMIT·TAKE_PROFIT·TAKE_PROFIT_LIMIT) |
| `AmendAdvancedOrder` | `POST private/advanced/amend-order` (`new_price` `0`이면 시장가 전환) |
| `CreateOCO` / `CancelOCO` | `POST private/advanced/create-oco` / `cancel-oco` (Spot 전용, 트리거는 `MARK_PRICE`만) |
| `CreateOTO` / `CancelOTO` | `POST private/advanced/create-oto` / `cancel-oto` |
| `CreateOTOCO` / `CancelOTOCO` | `POST private/advanced/create-otoco` / `cancel-otoco` |
| `CancelAdvancedOrder` | `POST private/advanced/cancel-order` (OTO/OTOCO 개별 주문) |
| `CancelAllAdvancedOrders` | `POST private/advanced/cancel-all-orders` |
| `CreateAdvancedOrderList` / `CancelAdvancedOrderList` | `POST private/advanced/create-order-list` / `cancel-order-list` |

생성 요청의 `AdvancedOrder`는 주문 종류별 가격·트리거 조합을 SDK에서 먼저 검증한다. OCO는 LIMIT 1건+트리거 1건, OTO는 LIMIT·MARKET 1건+트리거 1건, OTOCO는 LIMIT·MARKET 1건+손절 1건+익절 1건이어야 한다. 생성 응답은 `list_id`(OCO·OTO·OTOCO)이며 취소는 `list_id`로 한다.

### 주문·수수료 조회 (권한 `read`)

| 메서드 | API |
|---|---|
| `FeeRate` | `POST private/get-fee-rate` |
| `InstrumentFeeRate` | `POST private/get-instrument-fee-rate` |
| `AdvancedOpenOrders` | `POST private/advanced/get-open-orders` |
| `AdvancedOrderInfo` | `POST private/advanced/get-order-detail` |
| `AdvancedOrderHistory` | `POST private/advanced/get-order-history` (history 제한 1회/초가 아니라 기본 3회/100ms bucket) |
| `AdvancedOrderListDetail` | `POST private/advanced/get-order-list-detail` |

### 계정 (권한 `read`)

| 메서드 | API |
|---|---|
| `Positions` | `POST private/get-positions` (파생 포지션, Spot 계정은 빈 목록) |
| `Transactions` | `POST private/get-transactions` (원장 내역, `journal_type` 필터) |
| `SubaccountBalances` | `POST private/get-subaccount-balances` (`UserBalance`와 같은 형식) |

### 지갑

| 메서드 | API | 권한 |
|---|---|---|
| `DepositAddresses` | `POST private/get-deposit-address` | `read` |
| `DepositHistory` | `POST private/get-deposit-history` | `read` |
| `WithdrawalHistory` | `POST private/get-withdrawal-history` | `read` |
| `WithdrawalAddresses` | `POST private/get-withdrawal-addresses` | `read` |
| `CurrencyNetworks` | `POST private/get-currency-networks` | `read` |
| `CreateWithdrawal` | `POST private/create-withdrawal` | `withdraw` |

입출금 이력은 `start_ts`·`end_ts`(ms)와 0부터 시작하는 `page`, 최대 200의 `page_size`를 쓴다. 출금 주소는 거래소에 미리 화이트리스트로 등록해야 하며 `CreateWithdrawal`은 `ClientWithdrawID`로 멱등 확인을 하도록 권장한다. 입금 상태 `0`~`3`, 출금 상태 `0`~`6`은 문자열 상수로 제공한다.

미구현: `private/get-accounts`, `private/create-subaccount-transfer`, `private/user-balance-history`, `private/get-fee-credit-balances`, `private/get-account-settings`·`change-account-settings`, staking REST, 파생 전용 WebSocket과 WS 주문 RPC(이번 REST 범위 밖).

## 구현 순서

1. 공개 REST, 오류 정규화, 요청 제한과 mock 테스트 구현 완료
2. private REST, signer golden vector와 주문 안전 계약 구현 완료
3. 공통 Spot API와 적합성 테스트 구현 완료
4. public market WebSocket과 heartbeat·동적 구독 구현 완료
5. private user WebSocket 인증과 주문·체결·잔고 구독 구현 완료
6. 10·50단계 로컬 오더북과 sequence gap 복구 구현 완료
7. 공통 live smoke CLI 연결 구현 완료, UAT·production 실제 실행 대기

각 단계는 Go formatter, 생성물 검사, 일반·race 테스트, vet, 한글 주석 검사를 통과한 뒤 별도 커밋으로 푸시한다.

## 운영 제약

- API Key는 공식 IP whitelist에 실제 공인 송신 IP를 등록하고 SDK route 허용 목록과 일치시킨다.
- 여러 공인 송신 IP는 키 분리와 가용성을 위해 사용하며 거래소 요청 제한이나 지역 정책을 우회하지 않는다.
- Production과 UAT endpoint·credential은 혼용하지 않는다.
- 지역 제한 우회용 도메인이나 proxy 기능은 제공하지 않는다.
- 서브계정 간 이체와 staking 실행은 이 패키지에 구현하지 않는다. 출금 요청은 `withdraw` 권한을 가진 별도 credential에서만 허용된다.
