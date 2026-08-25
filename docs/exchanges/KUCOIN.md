# KuCoin Spot REST·WebSocket 어댑터

REST와 계정 WebSocket은 KuCoin Classic Spot API와 기본 주소 `https://api.kucoin.com`을 사용합니다. 공개 로컬 오더북은 현재 권장 Pro WebSocket API의 `wss://x-push-spot.kucoin.com`을 사용합니다. 이 패키지는 2026년에 추가된 Unified Trading Account API와 별개이며, `NewUnifiedSpot`은 Classic native API를 프로젝트 공통 Spot 계약으로 변환합니다.

## 전제조건

private API를 사용하려면 `credential.Provider`가 반환하는 `credential.Material`에 다음 값을 넣어야 합니다.

| 필드 | KuCoin 값 |
|---|---|
| `APIKey` | API Key |
| `SecretKey` | API Secret 원문 |
| `Passphrase` | API Passphrase 원문 |

기본 `Config.APIKeyVersion`은 `2`입니다. 버전 2는 Passphrase 원문을 Secret Key로 HMAC-SHA256 서명한 Base64 값을 헤더에 전송합니다. 기존 버전 1 API Key를 사용해야 할 때만 `APIKeyVersion: "1"`을 지정하며, 이 경우 Passphrase 원문을 전송합니다.

`credential.Descriptor.AccountID`에는 KuCoin UID 요청 제한을 공유하는 계정의 안정적인 식별자를 넣어야 합니다. 자격증명의 `AllowedEgressRouteIDs` 밖에 있는 route는 Secret 조회 전에 차단됩니다. API Key의 IP 허용 목록에는 해당 route와 연결된 공인 송신 IP를 등록해야 합니다.

## 지원 범위

| 영역 | 메서드 | API |
|---|---|---|
| 상품 규칙 | `Symbols` | `GET /api/v2/symbols` |
| 현재가 | `Ticker` | `GET /api/v1/market/orderbook/level1` |
| 호가 | `OrderBook` | `GET /api/v1/market/orderbook/level2_20`, `level2_100` |
| 공개 체결 | `RecentTrades` | `GET /api/v1/market/histories` |
| 캔들 | `Candles` | `GET /api/v1/market/candles` |
| 계정 | `Accounts` | `GET /api/v1/accounts` |
| 주문 생성 | `PlaceOrder` | `POST /api/v1/hf/orders` |
| 주문 상세·취소 | `OrderInfo`, `CancelOrder` | `GET`, `DELETE /api/v1/hf/orders/{orderId}`, `/client-order/{clientOid}` |
| 미체결 주문 | `OpenOrders` | `GET /api/v1/hf/orders/active/page` |
| 미체결 거래쌍 | `OpenOrderSymbols` | `GET /api/v1/hf/orders/active/symbols` |
| public 연결 token | `PublicWebSocketToken` | `POST /api/v1/bullet-public` |
| private 연결 token | `PrivateWebSocketToken` | `POST /api/v1/bullet-private` |

가격, 수량, 금액, 수수료는 `float64`로 변환하지 않고 문자열로 보존합니다. 객체와 배열 항목의 `Raw`에는 해당 `data` 원본 JSON을 보존합니다. Classic 캔들 배열은 `time, open, close, high, low, volume, turnover` 순서로 해석하고 최대 1,500개를 최신순으로 반환합니다.

2025년 3월 폐기된 `/api/v1/hf/orders/active`를 사용하지 않습니다. `OpenOrders`는 현재 페이지 API인 `/api/v1/hf/orders/active/page`와 `pageNum`, `pageSize`를 사용합니다.

## 인증과 서명

private 요청은 요청 제한 대기가 끝난 뒤 자격증명을 조회하고 최종 요청을 서명합니다.

1. 현재 Unix millisecond를 `KC-API-TIMESTAMP`로 사용합니다.
2. `timestamp + 대문자 HTTP method + endpoint와 query + JSON body`를 이어 붙입니다.
3. 이 bytes를 API Secret으로 HMAC-SHA256 서명하고 표준 Base64로 변환해 `KC-API-SIGN`에 넣습니다.
4. API Key, timestamp, 서명한 Passphrase, API Key 버전을 각각 `KC-API-*` 헤더에 넣습니다.
5. POST는 서명한 것과 정확히 같은 공백 없는 JSON bytes를 본문으로 전송합니다. GET·DELETE는 본문 없이 실제 query와 같은 문자열을 서명합니다.

SDK가 지원하는 query 값은 URL 인코딩 전후가 달라지지 않도록 엄격하게 검증합니다. 임의 문자가 포함된 endpoint를 추가할 때는 KuCoin 규칙에 따라 URL 인코딩 전 원문으로 서명하고 전송 URL만 인코딩해야 합니다.

Provider가 반환한 API Key, Secret, Passphrase byte slice는 요청 뒤 가능한 범위에서 덮어씁니다. Go 문자열과 HTTP 계층 내부 복사본까지 완전히 지울 수 있다는 보장은 하지 않습니다.

## 요청 제한과 송신 경로

VIP 0 기준 기본 로컬 quota는 각 30초 구간의 Public 2,000 weight, Spot 4,000 weight, Management 2,000 weight입니다.

| bucket | 기본 제한 | 범위 |
|---|---:|---|
| `kucoin:route:<route>:public:30seconds` | 2,000 weight/30초 | 선택한 송신 경로 |
| `kucoin:account:<account>:spot:30seconds` | 4,000 weight/30초 | KuCoin UID의 Spot 주문 API |
| `kucoin:account:<account>:management:30seconds` | 2,000 weight/30초 | KuCoin UID의 계정 API |

각 endpoint의 공식 weight를 차감합니다. 예를 들어 `Symbols`는 4, `Accounts`는 5, 주문 생성·취소는 각각 1입니다. `gw-ratelimit-limit`과 `gw-ratelimit-remaining`이 로컬 설정과 일치하면 관측 사용량을 반영하고, remaining이 0이면 `gw-ratelimit-reset` millisecond 동안 해당 bucket을 막습니다. `Config`의 quota는 계정 VIP 등급이나 더 보수적인 운영 정책에 맞게 조정할 수 있습니다.

Public 풀은 IP 기준이므로 요청별 송신 경로가 각각 독립된 bucket을 사용합니다. Spot과 Management private 풀은 UID 기준이므로 송신 경로를 바꿔도 quota가 늘어나지 않습니다. 다중 송신 IP는 public 처리량 분산과 API Key IP 허용 목록·장애 격리를 위한 기능이며 private 제한 우회 용도가 아닙니다.

public token 발급은 Public pool에서 10 weight, private token 발급은 Spot pool에서 10 weight를 사용합니다. WebSocket 재연결마다 새 token을 발급하므로 연결 장애가 반복될 때 REST 요청 제한도 함께 소비됩니다.

## Classic WebSocket

`StreamClient`는 연결 전에 REST token을 발급하고 KuCoin이 반환한 `instanceServers` 중 사용 가능한 WebSocket endpoint를 선택합니다. production에서는 `wss`만 허용하며 `AllowInsecureWebSocket`은 로컬 테스트에서만 사용해야 합니다.

| 구분 | `StreamChannel` | KuCoin topic |
|---|---|---|
| public 현재가 | `StreamChannelTicker` | `/market/ticker:{symbol}` |
| public 증분 호가 | `StreamChannelLevel2` | `/market/level2:{symbol}` |
| public 5단계 호가 | `StreamChannelOrderBook5` | `/spotMarket/level2Depth5:{symbol}` |
| public 50단계 호가 | `StreamChannelOrderBook50` | `/spotMarket/level2Depth50:{symbol}` |
| public 캔들 | `StreamChannelCandles` | `/market/candles:{symbol}_{interval}` |
| public 체결 | `StreamChannelTrade` | `/market/match:{symbol}` |
| private 주문 | `StreamChannelOrders` | `/spotMarket/tradeOrdersV2` |
| private 잔고 | `StreamChannelBalance` | `/account/balance` |

public 구독은 거래쌍을 요구하고 캔들만 `CandleInterval`을 함께 지정합니다. private 주문·잔고 구독은 거래쌍이나 캔들 구간을 받지 않습니다. 연결당 구독은 최대 300개로 제한하고 subscribe·unsubscribe 명령을 최소 100ms 간격으로 전송합니다. 실행 중 `Subscribe`와 `Unsubscribe`로 구독을 바꿀 수 있으며, 오류 응답을 받은 변경은 로컬 복구 목록에서 되돌립니다.

```go
streamClient, err := kucoin.NewStreamClient(kucoin.StreamClientConfig{
	Connector:            connector,
	RESTClient:           restClient,
	DefaultEgressRouteID: "seoul-a",
})
if err != nil {
	return err
}

public, err := streamClient.PublicStream(
	kucoin.StreamRequest{Subscriptions: []kucoin.StreamSubscription{
		{Channel: kucoin.StreamChannelTicker, Symbol: "BTC-USDT"},
		{Channel: kucoin.StreamChannelCandles, Symbol: "BTC-USDT", Interval: kucoin.Candle1Minute},
	}},
	trade.WithEgressRoute("seoul-b"),
)
if err != nil {
	return err
}
defer public.Close()

return public.Run(ctx, func(_ context.Context, message kucoin.StreamMessage) error {
	if message.Channel != kucoin.StreamChannelTicker {
		return nil
	}
	var ticker kucoin.StreamTicker
	return message.Decode(&ticker)
})
```

token REST 요청과 WebSocket handshake는 모두 세션에서 선택한 같은 송신 경로를 사용합니다. 연결된 세션은 route를 바꾸지 않으며 재연결 시에도 같은 route에서 token과 `connectId`를 새로 만들고 현재 구독을 복구합니다. private 세션은 token 발급 전에 자격증명의 route 허용 목록과 읽기 권한을 검사하므로 허용되지 않은 route에서는 Secret을 조회하지 않습니다.

기본 heartbeat는 15초마다 `{type:"ping"}`을 보내고 9초 안에 `{type:"pong"}`을 기다립니다. 이 값은 token 응답의 `pingInterval`보다 짧고 `pingTimeout`보다 길지 않아야 합니다. 서버가 더 엄격한 값을 반환하면 연결을 시작하지 않고 설정 오류를 반환합니다.

Classic `StreamChannelLevel2`는 `sequenceStart`와 `sequenceEnd`가 포함된 원본 증분 feed지만 KuCoin이 2026년 7월 15일 deprecated 처리했습니다. 기존 사용자와 원본 이벤트 decode 호환을 위해 남겨 두되 새 로컬 오더북에는 사용하지 않습니다.

token과 `WebSocketToken.Raw`에는 짧은 수명의 접속 자격이 포함됩니다. 로그, 메트릭 label, 오류 문자열에 저장하지 않아야 합니다. public feed도 재연결 구간에는 이벤트 유실이 가능하며 private 주문·잔고는 연결 복구 뒤 REST 조회로 최종 상태를 재조정해야 합니다.

## Pro 로컬 오더북과 같은 송신 경로 복구

`ProOrderBookStream`은 token이 필요 없는 Pro public endpoint에 연결하고 Spot `obu` 채널의 `increment@10ms`를 구독합니다. 첫 `snapshot`과 이어지는 `delta`는 최대 500호가를 제공하며, delta 수량은 증감량이 아니라 해당 가격의 새 절대 수량입니다. 수량 `0`은 가격 레벨 삭제를 뜻합니다.

```go
streamClient, err := kucoin.NewStreamClient(kucoin.StreamClientConfig{
	Connector:            connector,
	RESTClient:           restClient,
	DefaultEgressRouteID: "seoul-a",
})
if err != nil {
	return err
}

orderBookStream, err := streamClient.ProOrderBookStream(
	"BTC-USDT",
	trade.WithEgressRoute("seoul-b"),
)
if err != nil {
	return err
}
defer orderBookStream.Close()

orderBook, err := kucoin.NewLocalOrderBook(kucoin.LocalOrderBookConfig{
	Symbol:        "BTC-USDT",
	EgressRouteID: "seoul-b",
	ViewDepth:     20,
})
if err != nil {
	return err
}

return orderBook.Run(ctx, orderBookStream, func(
	_ context.Context,
	view kucoin.LocalOrderBookView,
) error {
	return consume(view)
})
```

snapshot은 `O == C`를 만족해야 합니다. 현재 적용한 마지막 sequence가 `oldC`일 때 새 delta는 `O <= oldC + 1`이고 `C > oldC`여야 하며, `C <= oldC`인 오래된 이벤트는 무시합니다. `O > oldC + 1`인 공백이나 새 연결에서 snapshot보다 먼저 온 delta를 발견하면 장부를 버리고 선택한 같은 송신 경로로 재연결해 새 snapshot부터 복구합니다.

`SynchronizationID`는 적용한 snapshot 횟수, `GapCount`는 복구를 유발한 sequence 공백 횟수, `Generation`은 WebSocket 연결 세대입니다. `ViewDepth` 기본값은 20이고 최대 500이며, 내부 장부도 Pro 채널 계약에 맞춰 매수·매도 각각 최우선 500호가로 제한합니다. 로컬 오더북과 stream의 symbol 또는 송신 경로가 다르면 네트워크 연결 전에 거부합니다.

Pro public 연결은 REST token을 발급하지 않으므로 token 요청 제한을 소비하지 않습니다. 연결과 재연결은 세션 생성 시 선택한 route에 고정되며 `ProPublicWebSocketURL`을 바꾸는 설정은 테스트나 KuCoin이 공지한 공식 대체 endpoint 적용에만 사용해야 합니다.

## 공통 Spot API

`NewUnifiedSpot`으로 생성한 어댑터는 `unified.SpotClient`의 마켓, 현재가, 호가, 최근 체결, 캔들, 잔고, 주문 생성·조회·취소·미체결 목록 계약을 모두 구현합니다.

- 공통 `BASE/QUOTE` 마켓은 KuCoin의 `BASE-QUOTE` symbol로 변환합니다.
- 공통 호가 깊이는 최대 16이므로 KuCoin 20단계 snapshot을 받은 뒤 요청 깊이만 남깁니다.
- 공개 체결 nanosecond 시각과 캔들 second 시각은 공통 Unix millisecond로 변환합니다.
- 잔고는 주문에 사용되는 `trade` 계정만 조회하고 `available`과 `holds`를 각각 사용 가능·잠금 수량으로 매핑합니다.
- 시장가 매수의 공통 `QuoteAmount`는 KuCoin `funds`, 시장가 매도의 `Quantity`는 `size`로 변환합니다.
- 공통 post-only 지정가는 KuCoin GTC와 `postOnly=true` 조합으로 전송합니다.
- 공통 주문에 `ClientOrderID`가 없으면 `[0-9A-Za-z_-]{1,40}` 범위 안에서 `proven-` 접두사의 무작위 ID를 생성합니다.
- 거래소 주문 ID와 사용자 주문 ID 중 하나로 주문 조회·취소가 가능합니다.
- 전체 마켓 미체결 조회는 전체 상품을 순회하지 않고 `OpenOrderSymbols`로 대상 symbol을 얻은 뒤 각 페이지를 끝까지 조회합니다.

공통 주문 상태는 `isActive`, `cancelExist`, `size`, `dealSize`를 함께 사용합니다. 활성 주문의 체결 수량이 0이면 `new`, 0보다 크면 `partially_filled`이며, 비활성 취소 주문은 `canceled`, 전체 수량이 체결된 주문은 `filled`로 변환합니다. 거래소 응답만으로 확정할 수 없는 상태는 `unknown`으로 남깁니다.

## 주문 안전 계약

- `ClientOrderID`는 모든 주문에서 필수이며 `[0-9A-Za-z_-]{1,40}` 형식으로 검증합니다.
- 지정가는 `Price`와 `Size`를 사용하며 GTC·GTT·IOC·FOK를 지원합니다.
- post-only는 GTC 또는 거래소 기본 TIF에서만 허용합니다. `CancelAfter`는 GTT에서만 허용합니다.
- 시장가 매수는 기준 통화 수량 `Size` 또는 견적 통화 총액 `Funds` 중 하나를 사용하고, 시장가 매도는 기준 통화 수량 `Size`를 사용합니다.
- 주문 상세와 취소는 `OrderID`와 `Symbol`을 함께 요구합니다.
- 부분 호가는 공식 고정 깊이인 20 또는 100만 허용합니다.

주문 생성·취소의 전송 오류, 응답 본문 읽기 실패, 성공 응답 JSON 파싱 실패, 거래소 `500000`·`230005` 오류는 실제 처리 여부를 단정할 수 없습니다. SDK는 자동 재시도하지 않고 `trade.ErrUnknownExecutionState`로 반환합니다. 불명확한 주문 생성 결과는 고유한 `ClientOrderID`를 기준으로 거래소 상태를 확인한 뒤 처리해야 합니다.

## 오류 처리

KuCoin은 HTTP 200에서도 `code`가 `200000`이 아닌 논리 오류를 반환할 수 있습니다. SDK는 HTTP 상태와 JSON envelope를 모두 검사하고 인증·서명, 권한·IP 허용 목록, 잔고 부족, 요청 제한, 거래소 장애를 공통 `trade.APIError`로 변환합니다. 원본 오류 code와 message, 요청 ID를 보존하되 인증 헤더와 서명 원문은 오류에 포함하지 않습니다.

## 공식 기준

- [KuCoin Authentication](https://www.kucoin.com/docs-new/authentication)
- [KuCoin Rate Limit](https://www.kucoin.com/docs-new/rate-limit)
- [KuCoin Spot Market Data](https://www.kucoin.com/docs-new/rest/spot-trading/market-data/get-all-symbols)
- [KuCoin Add Order](https://www.kucoin.com/docs-new/rest/spot-trading/orders/add-order)
- [KuCoin Open Orders By Page](https://www.kucoin.com/docs-new/rest/spot-trading/orders/get-open-orders-by-page)
- [KuCoin Get Order By ClientOid](https://www.kucoin.com/docs-new/rest/spot-trading/orders/get-order-by-clientoid)
- [KuCoin Cancel Order By ClientOid](https://www.kucoin.com/docs-new/rest/spot-trading/orders/cancel-order-by-clientoid)
- [KuCoin Symbols With Open Orders](https://www.kucoin.com/docs-new/rest/spot-trading/orders/get-symbols-with-open-order)
- [KuCoin Public WebSocket Token](https://www.kucoin.com/docs-new/websocket-api/base-info/get-public-token-spot-margin)
- [KuCoin Private WebSocket Token](https://www.kucoin.com/docs-new/websocket-api/base-info/get-private-token-spot-margin)
- [KuCoin WebSocket Ticker](https://www.kucoin.com/docs-new/3470063w0)
- [KuCoin Pro Increment Best 500 Order Book](https://www.kucoin.com/docs-new/3470221w0)
- [KuCoin Pro WebSocket Introduction](https://www.kucoin.com/docs-new/websocket-api/base-info/introduction-uta)
- [KuCoin Classic WebSocket Level2](https://www.kucoin.com/docs-new/3470068w0)
- [KuCoin WebSocket Order V2](https://www.kucoin.com/docs-new/3470073w0)
- [KuCoin WebSocket Balance](https://www.kucoin.com/docs-new/3470075w0)
- [KuCoin Spot Error Codes](https://www.kucoin.com/docs-new/error-code/spot)
- [KuCoin Change Log](https://www.kucoin.com/docs-new/change-log)
