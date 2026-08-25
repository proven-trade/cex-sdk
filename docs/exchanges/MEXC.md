# MEXC Spot V3 REST·Protobuf WebSocket·로컬 오더북·공통 어댑터

Go 패키지는 `exchange/mexc`이며 현행 Spot V3 REST 기본 주소 `https://api.mexc.com`과 WebSocket 주소 `wss://wbs-api.mexc.com/ws`를 사용합니다. 공개 시세, signed 계정·주문 REST, public/private Protobuf WebSocket, version 연속성 기반 로컬 오더북과 `unified.SpotClient` 공통 API를 구현했습니다.

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
| 단일·최대 5개 미체결 주문 | `OpenOrders` | `GET /api/v3/openOrders` | 10 |
| 전체 주문 이력 | `AllOrders` | `GET /api/v3/allOrders` | 10 |
| 계정 체결 | `MyTrades` | `GET /api/v3/myTrades` | 10 |

24시간 통계와 ticker 메서드는 이번 단계에서 단일 symbol을 필수로 받습니다. 전체 symbol 조회는 공식 weight가 더 크고 응답도 배열로 바뀌므로, 호출 실수로 IP quota와 메모리를 크게 소비하지 않게 별도 API로 열지 않았습니다.

`OrderBookRequest.Limit`은 생략하거나 1~5000, 체결·캔들 조회의 `Limit`은 생략하거나 1~1000입니다. 합산 체결의 `Start`와 `End`는 함께 지정해야 합니다. 캔들은 `1m`, `5m`, `15m`, `30m`, `60m`, `4h`, `1d`, `1W`, `1M`을 지원하고 시각은 Unix millisecond query로 변환합니다.

Private 표의 weight는 endpoint 본문과 2025년 공식 제한표가 충돌하는 항목에서 더 보수적인 값인 10을 사용합니다. `AllOrders`는 최대 1000건과 7일 범위, `MyTrades`는 최대 100건과 31일 범위를 로컬에서 검증합니다.

## Protobuf WebSocket

MEXC는 구독·구독 해제·PONG 응답은 JSON text frame으로, 실제 시세와 계정 이벤트는 `PushDataV3ApiWrapper` binary Protobuf frame으로 전송합니다. SDK는 `DecodeStreamMessage`에서 frame 종류를 먼저 확인하고 다음 이벤트 포인터 중 해당하는 값 하나를 채웁니다. 숫자로 계산하면 정밀도를 잃을 수 있는 가격·수량·version·식별자는 문자열로 보존합니다.

| 채널 | 생성 함수 | `StreamMessage` 필드 |
|---|---|---|
| 합산 체결 | `AggregateTradesStream` | `AggregateTrades` |
| 캔들 | `CandleStream` | `Candle` |
| 증분 호가 | `DiffDepthStream` | `DiffDepth` |
| 5·10·20단계 완전 호가 | `PartialDepthStream` | `PartialDepth` |
| 최우선 호가 | `BookTickerStream` | `BookTicker` |
| private 잔고 변경 | `AccountStream` | `Account` |
| private 체결 | `AccountDealsStream` | `AccountDeal` |
| private 주문 | `AccountOrdersStream` | `AccountOrder` |

합산 체결·증분 호가·최우선 호가는 `StreamUpdate10Millis` 또는 `StreamUpdate100Millis`를 요구합니다. 캔들은 `Min1`, `Min5`, `Min15`, `Min30`, `Min60`, `Hour4`, `Hour8`, `Day1`, `Week1`, `Month1`에 대응하는 `StreamCandleInterval` 상수를 사용합니다. 한 연결의 구독은 공식 제한인 30개를 넘길 수 없습니다.

```go
streamClient, err := mexc.NewStreamClient(mexc.StreamClientConfig{
	Connector:            connector,
	RESTClient:           client,
	DefaultEgressRouteID: "seoul-a",
})
if err != nil {
	return err
}

trades, err := mexc.AggregateTradesStream(
	"BTCUSDT",
	mexc.StreamUpdate100Millis,
)
if err != nil {
	return err
}

public, err := streamClient.PublicStream(
	mexc.StreamRequest{Subscriptions: []mexc.StreamSubscription{trades}},
	trade.WithEgressRoute("seoul-b"),
)
if err != nil {
	return err
}

err = public.Run(ctx, func(_ context.Context, message mexc.StreamMessage) error {
	if message.AggregateTrades == nil {
		return nil
	}
	for _, deal := range message.AggregateTrades.Deals {
		consume(deal)
	}
	return nil
})
```

`PublicStream`과 `UserDataStream`은 실행 중 `Subscribe`·`Unsubscribe`를 지원합니다. 성공적으로 보낸 변경은 재연결 복구 목록에 즉시 반영하고, 거래소가 nonzero `code`로 거절하면 해당 변경만 되돌립니다. 연결이 끊기면 같은 EIP route에서 재연결하고 현재 목록을 다시 구독합니다. 데이터가 없는 연결도 유지하도록 기본 20초마다 공식 JSON `PING`을 보내며 PONG은 `StreamMessage.Control`로 전달합니다.

### Private listenKey 수명주기

Private stream은 `RESTClient`의 자격증명을 사용합니다. `UserDataStream` 생성 시 route 허용과 읽기 권한을 Secret 조회 전에 검사하고, `Run`의 최초 연결 직전에 같은 route로 `POST /api/v3/userDataStream`을 호출합니다. listenKey 요청은 공식 계약대로 HMAC Secret 없이 `X-MEXC-APIKEY`만 보내지만 Provider가 반환한 민감 byte slice는 호출 뒤 덮어씁니다.

```go
private, err := streamClient.UserDataStream(
	mexc.StreamRequest{Subscriptions: []mexc.StreamSubscription{
		mexc.AccountStream(),
		mexc.AccountDealsStream(),
		mexc.AccountOrdersStream(),
	}},
	trade.WithEgressRoute("seoul-b"),
)
if err != nil {
	return err
}

err = private.Run(ctx, func(_ context.Context, message mexc.StreamMessage) error {
	if message.AccountOrder != nil {
		consumeOrder(*message.AccountOrder)
	}
	return nil
})
```

SDK는 기본 30분마다 같은 EIP로 `PUT /api/v3/userDataStream?listenKey=...`을 보내 60분 유효 시간을 연장합니다. 갱신 실패 시 기존 키를 버리고 같은 route에서 새 키를 발급해 재연결합니다. 일반 WebSocket 재연결은 아직 유효한 키를 재사용합니다. 로컬 `Close`는 연결만 종료하므로 키를 즉시 무효화해야 하면 `Client.CloseUserDataStream`에 `private.ListenKey()`를 전달해야 합니다. 현재 유효 키 목록은 `Client.UserDataStreams`로 확인할 수 있습니다.

listenKey 생성은 응답 유실 시 서버에서 키가 만들어졌는지 알 수 없는 mutation입니다. 전송 오류·5xx·성공 응답 파싱 실패는 `UNKNOWN_EXECUTION_STATE`로 반환하고 자동 재시도하지 않아 키를 무제한 생성하지 않습니다. MEXC의 24시간 연결 상한은 자동 재연결로 처리하며 실제 계정 운영 smoke는 아직 대기 상태입니다.

## 로컬 오더북

`NewLocalOrderBook`은 먼저 WebSocket diff depth를 수신해 제한된 개수만큼 버퍼링하고, 같은 `EgressRouteID`로 `GET /api/v3/depth` snapshot을 요청합니다. 기본 snapshot은 공식 최대치인 5000단계이며 기본 공개 view는 상위 20단계입니다. `SnapshotLimit`, `MaxBufferedEvents`, `ViewDepth`, snapshot timeout과 재시도 간격은 설정으로 조정할 수 있습니다.

```go
depth, err := mexc.DiffDepthStream("BTCUSDT", mexc.StreamUpdate100Millis)
if err != nil {
	return err
}

public, err := streamClient.PublicStream(
	mexc.StreamRequest{Subscriptions: []mexc.StreamSubscription{depth}},
	trade.WithEgressRoute("seoul-b"),
)
if err != nil {
	return err
}

book, err := mexc.NewLocalOrderBook(mexc.LocalOrderBookConfig{
	RESTClient:      client,
	Symbol:          "BTCUSDT",
	EgressRouteID:   "seoul-b",
	UpdateInterval:  mexc.StreamUpdate100Millis,
	SnapshotLimit:   5000,
	ViewDepth:       20,
})
if err != nil {
	return err
}

err = book.Run(ctx, public, func(_ context.Context, view mexc.LocalOrderBookView) error {
	consumeBook(view)
	return nil
})
```

동기화 순서는 공식 MEXC 절차를 따릅니다.

1. diff depth를 먼저 연결하고 이벤트를 버퍼링합니다.
2. 같은 EIP에서 REST snapshot을 조회합니다.
3. `toVersion < lastUpdateId`인 오래된 이벤트를 버립니다.
4. 첫 관련 이벤트가 `fromVersion <= lastUpdateId <= toVersion`을 만족해야 snapshot과 연결합니다.
5. 이후 모든 이벤트는 `fromVersion == 이전 toVersion + 1`이어야 합니다.
6. 중복·역행·전방 갭이나 WebSocket 재연결을 발견하면 현재 장부를 버리고 같은 route의 새 snapshot으로 재동기화합니다.

호가 수량은 delta가 아니라 해당 가격의 절대 수량이며 0이면 가격 단계를 삭제합니다. 가격은 `big.Rat`로 비교해 `100`과 `100.0`을 같은 단계로 취급하지만 공개 view에는 마지막으로 받은 문자열 정밀도를 보존합니다. 잘못된 decimal, 중복 canonical 가격, 잘못된 version·전송 시각은 장부를 공개하지 않고 validation 오류로 종료합니다. 동기화 버퍼가 `MaxBufferedEvents`를 넘으면 `ErrDepthBufferOverflow`를 반환합니다.

REST snapshot 단계 수에는 상한이 있으므로 최초 snapshot 밖에서 수량이 바뀌지 않은 가격은 증분 stream에 나타나지 않을 수 있습니다. 따라서 제한된 snapshot으로 만든 로컬 장부는 실제 전체 장부와 일부 다를 수 있으며, 운영에서는 기본 5000단계를 유지하고 필요한 상위 view만 잘라 쓰는 구성을 권장합니다.

`Run`은 REST와 WebSocket route가 다르거나 `UpdateInterval`까지 정확히 일치하는 diff depth 구독이 없으면 네트워크를 사용하기 전에 거절합니다. `SynchronizationID`는 성공한 동기화마다 증가하고 `GapCount`는 감지한 version 불연속 수를 보존합니다. `LastVersion`, wrapper 생성·전송 시각과 마지막 주문 생성 시각은 각 view에 함께 전달되며, 거래소가 선택 시각을 생략하면 해당 값은 0입니다.

## 공통 Spot API

`NewUnifiedSpot`은 마켓, 최근가, 호가, 공개 체결, 캔들, 잔고, 주문 생성·조회·취소·미체결 목록을 `unified.SpotClient`로 제공합니다. 공통 `BTC/USDT`는 MEXC `BTCUSDT`로 변환하며 모든 요청 옵션을 native 호출까지 전달합니다. 구분자 없는 응답 심볼은 임의로 분해하지 않고 `ExchangeInfo`의 base/quote와 원문 심볼이 정확히 일치하는지 검증합니다.

공통 3분봉은 같은 EIP에서 1분봉을 요청한 뒤 epoch 기준 3개씩 합성하며 OHLC와 기준 통화 거래량의 decimal 문자열 정밀도를 유지합니다. 지정가 GTC·IOC·FOK·post-only는 각각 `LIMIT`·`IMMEDIATE_OR_CANCEL`·`FILL_OR_KILL`·`LIMIT_MAKER`로 변환합니다. 공통 주문에서 `ClientOrderID`를 생략하면 `proven-`과 암호학적 난수 hex로 구성된 31자 ID를 생성합니다.

전체 미체결은 `SelfSymbols`의 API Key 허용 거래쌍을 `ExchangeInfo`와 대조한 뒤 최대 5개씩 묶어 조회합니다. 중복·알 수 없는 심볼이나 요청 묶음 밖의 주문이 응답되면 결과를 반환하지 않습니다. 허용 거래쌍이 많으면 여러 private 요청과 UID quota를 소비하므로 호출자가 `AllMarkets: true`를 명시한 경우에만 실행됩니다.

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

자동 테스트는 HMAC 서명과 실제 query 일치, 요청별 route 선택, route·권한 사전 검사, Secret 덮어쓰기, IP·UID 요청 제한, 주문 검증, 원본 JSON 보존, 오류 분류와 mutation 불명확 상태를 검증합니다. WebSocket 테스트는 공식 Protobuf field 번호별 공개·private 이벤트 해석, 잘못된 wire type·UTF-8 거절, JSON 제어 응답, listenKey 수명주기, JSON PING, 구독 rollback, 동일 EIP 재연결과 race 안전성을 검증합니다. 로컬 오더북 테스트는 공식 snapshot bridge 경계, 엄격한 다음 version, 중복·역행·전방 갭, snapshot 실패·불일치 재시도, 재연결 세대, 버퍼 상한과 동일 EIP REST·WebSocket 통합을 검증합니다. 공통 적합성 테스트는 마켓·시세·잔고·주문 변환, 3분봉 합성, 전체 미체결 5개 묶음과 EIP 전달을 검증합니다. 공통 live smoke CLI 연결은 구현됐으며 실제 MEXC 계정과 지정 EIP를 이용한 읽기·주문 smoke는 아직 대기 상태입니다.

## 공식 기준

- [MEXC Spot V3 API 문서](https://mexcdevelop.github.io/apidocs/spot_v3_en/)
- [MEXC WebSocket Protobuf 정의](https://github.com/mexcdevelop/websocket-proto)
- [MEXC API 안내](https://www.mexc.com/mexc-api)
- [MEXC 2025 요청 제한표](https://www.mexc.com/en-GB/announcements/article/term-definitions-17827791529303)
