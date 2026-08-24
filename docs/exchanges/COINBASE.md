# Coinbase Advanced Trade Spot 어댑터

## 범위

첫 번째 범위는 Coinbase Advanced Trade v3의 Spot REST와 WebSocket API다.

- 서버 시간
- 공개 Spot 상품 목록과 단건 상품
- 공개 호가, 최근 체결, 캔들
- 거래 계정 목록과 단건 계정
- 시장가 IOC, 지정가 GTC·SOR IOC·FOK 주문 생성
- 최대 100건 주문 일괄 취소
- 주문 단건과 cursor 기반 주문 목록
- cursor 기반 체결 목록
- 공개 ticker, ticker batch, 체결, 호가, 5분 캔들, 상품 상태 stream
- private user 주문 stream
- 요청별 EIP route 선택

고급 주문, 포트폴리오 자금 이동과 파생상품은 이 단계의 범위가 아니다. 지원하지 않는 주문 설정을 임의의 필드 조합으로 전송하지 않고 명시적으로 거부한다.

## 자격증명

Coinbase App API는 CDP에서 ECDSA 서명 알고리즘으로 생성한 key를 사용해야 한다. Ed25519 key는 이 어댑터에서 지원하지 않는다.

`credential.Material`에는 다음처럼 저장한다.

| 필드 | 값 |
|---|---|
| `APIKey` | `organizations/{org_id}/apiKeys/{key_id}` 형식의 API key name |
| `SecretKey` | P-256 EC private key PEM |
| `Passphrase` | 사용하지 않음 |

PEM의 실제 줄바꿈과 `\n`으로 escape된 줄바꿈을 모두 읽을 수 있다. Secret은 요청을 최종 생성할 때 조회하며, 요청이 끝나면 Provider가 반환한 byte slice를 덮어쓴다. 자격증명에 허용되지 않은 EIP route나 필요한 read/trade 권한은 Secret 조회 전에 거부한다.

## JWT 인증

private REST 요청마다 만료 시간이 2분인 ES256 JWT를 새로 만든다.

- header: `alg=ES256`, `typ=JWT`, `kid={API key name}`, 16-byte random `nonce`
- claims: `sub={API key name}`, `iss=cdp`, `nbf`, `exp`, `uri`
- `uri`: `{대문자 HTTP method} {host}{path}`
- query string과 body는 `uri`에 넣지 않음
- 서명 결과는 JOSE 형식의 64-byte `R || S`

`BaseURL`을 공식 운영 주소가 아닌 프록시로 바꾸면 JWT의 host도 해당 `BaseURL` host가 된다. Coinbase가 직접 검증할 JWT를 중계하는 프록시는 원래 host를 보존하거나 SDK의 URL 설정을 공식 host에 맞춰야 한다.

## 생성 예제

```go
descriptor := &credential.Descriptor{
	AccountID:   "coinbase-main",
	Exchange:    model.ExchangeCoinbase,
	SecretRef:   "secret/coinbase/main",
	Permissions: []credential.Permission{
		credential.PermissionRead,
		credential.PermissionTrade,
	},
	AllowedEgressRouteIDs: []transport.EgressRouteID{"seoul-a", "seoul-b"},
}

client, err := coinbase.New(coinbase.Config{
	Executor:             executor,
	Credentials:          descriptor,
	CredentialProvider:  secretProvider,
	DefaultEgressRouteID: "seoul-a",
})
if err != nil {
	return err
}
```

## 공개 시세

public endpoint에는 인증 정보를 넣지 않고 `Cache-Control: no-cache`를 설정한다. Coinbase 공개 REST의 기본 1초 cache를 사용하고 싶다면 현재 어댑터의 공개 메서드 대신 별도 cache 계층을 두는 방식을 권장한다. 실시간 시세는 후속 WebSocket 어댑터를 사용한다.

```go
book, err := client.OrderBook(
	ctx,
	coinbase.OrderBookRequest{
		ProductID: "BTC-USD",
		Limit:     50,
	},
	trade.WithEgressRoute("seoul-b"),
)
```

최근 공개 체결의 `Limit`은 필수이며 1~1000이다. 캔들은 Unix 초 단위의 시작·종료 시각과 지원 granularity를 전달하고 최대 350개를 요청할 수 있다.

## 주문

첫 범위는 다음 네 설정을 타입으로 분리한다.

- `MarketMarketIOC`: `QuoteSize` 또는 `BaseSize` 중 정확히 하나
- `LimitLimitGTC`: `BaseSize`, `LimitPrice`, 선택적 `PostOnly`
- `SORLimitIOC`: `BaseSize`, `LimitPrice`
- `LimitLimitFOK`: `BaseSize`, `LimitPrice`

```go
reference, err := client.PlaceOrder(
	ctx,
	coinbase.PlaceOrderRequest{
		ClientOrderID: "strategy-20260824-1",
		ProductID:     "BTC-USD",
		Side:          coinbase.SideBuy,
		OrderConfiguration: coinbase.OrderConfiguration{
			LimitLimitGTC: &coinbase.LimitGTCConfiguration{
				BaseSize:   "0.001",
				LimitPrice: "60000",
				PostOnly:   true,
			},
		},
	},
	trade.WithEgressRoute("seoul-b"),
)
```

가격과 수량은 `float64`가 아닌 decimal 문자열이다. SDK는 자동 반올림하지 않는다. 네트워크 오류나 5xx로 주문 접수 여부가 불명확하면 mutation을 자동 재시도하지 않고 `UNKNOWN_EXECUTION_STATE`로 반환한다. 같은 `ClientOrderID`로 주문 상태를 조정하기 전에는 새 주문을 전송하지 않아야 한다.

일괄 취소 HTTP 응답이 성공해도 각 `CancelResult.Success`를 확인해야 한다. 취소 접수 후 최종 상태는 `OrderInfo` 또는 후속 user WebSocket으로 확인한다.

## WebSocket

Coinbase Advanced Trade는 market data와 user data 주소가 분리되어 있다.

| 용도 | 운영 주소 |
|---|---|
| 공개 market data | `wss://advanced-trade-ws.coinbase.com` |
| private user data | `wss://advanced-trade-ws-user.coinbase.com` |

public stream은 다음 채널을 지원한다.

- `ticker`, `ticker_batch`: `[]StreamTickerEvent`
- `market_trades`: `[]StreamMarketTradeEvent`
- `level2`: `[]StreamLevel2Event`
- `candles`: `[]StreamCandleEvent`
- `status`: `[]StreamStatusEvent`
- 자동 구독되는 `heartbeats`: `[]StreamHeartbeatEvent`

```go
streams, err := coinbase.NewStreamClient(coinbase.StreamClientConfig{
	Connector:             connector,
	Credentials:           descriptor,
	CredentialProvider:   secretProvider,
	DefaultEgressRouteID: "seoul-a",
})
if err != nil {
	return err
}

public, err := streams.PublicStream(
	coinbase.PublicStreamRequest{
		Subscriptions: []coinbase.StreamSubscription{
			{
				Channel:    coinbase.StreamChannelTicker,
				ProductIDs: []string{"BTC-USD"},
			},
			{
				Channel:    coinbase.StreamChannelLevel2,
				ProductIDs: []string{"BTC-USD"},
			},
		},
	},
	trade.WithEgressRoute("seoul-b"),
)
if err != nil {
	return err
}
defer public.Close()

err = public.Run(ctx, func(ctx context.Context, message coinbase.StreamMessage) error {
	if message.Channel != coinbase.StreamChannelTicker {
		return nil
	}
	var events []coinbase.StreamTickerEvent
	if err := message.DecodeEvents(&events); err != nil {
		return err
	}
	return handleTickers(events)
})
```

서버는 `level2` 구독 데이터를 `channel=l2_data`로 전달하므로 handler에서 raw `Channel`을 분기할 때 이 이름을 사용해야 한다. `SequenceNumber`를 노출하지만 첫 범위에는 로컬 오더북 gap 자동 복구가 포함되지 않는다. snapshot을 적용한 뒤 update의 `NewQuantity`를 해당 가격 레벨의 전체 수량으로 교체하고, 값이 `0`이면 레벨을 제거해야 한다.

public `Subscribe`와 `Unsubscribe`가 성공하면 현재 구독 목록을 갱신한다. 연결이 끊기면 같은 EIP route에서 heartbeat를 포함한 현재 목록을 채널별 메시지로 다시 구독한다. Coinbase는 연결 후 5초 안에 구독 메시지를 요구하며 채널 하나마다 별도 메시지를 보내야 한다.

user stream은 Spot 주문 snapshot과 update를 `[]StreamUserEvent`로 decode한다. 상품 목록이 비어 있으면 계정 전체 상품이며, 공식 계약상 user 연결의 상품 필터를 바꾸려면 기존 연결을 닫고 새 `UserStream`을 만들어야 한다.

```go
user, err := streams.UserStream(
	coinbase.UserStreamRequest{ProductIDs: []string{"BTC-USD"}},
	trade.WithEgressRoute("seoul-b"),
)
if err != nil {
	return err
}
defer user.Close()
```

user 연결은 `user`와 `heartbeats`를 모두 인증 구독한다. 연결할 때마다 최신 Secret을 조회하며, 각 subscribe 메시지마다 `uri` claim이 없는 새 2분 ES256 JWT를 만든다. 명시적인 JWT 인증 오류를 받으면 같은 key로 무한 재연결하지 않는다.

공개 구독 메시지는 IP당 초당 8건 공식 제한보다 낮도록 기본 150ms 간격으로 직렬화한다. heartbeat는 매초 수신되며 Coinbase가 60~90초 동안 업데이트가 없는 채널을 닫지 않도록 모든 세션에서 자동 구독한다. WebSocket protocol ping도 기본 20초 간격으로 수행한다.

한 WebSocket의 EIP route는 생성 후 바뀌지 않는다. 최초 연결과 모든 재연결은 생성 시 선택한 route의 private IP 전용 HTTP client로 handshake한다.

## 요청 제한

공식 한도와 계정별 상향 한도는 변경될 수 있으므로 SDK 기본값은 공개 route와 private 계정 모두 초당 10건으로 보수적으로 설정한다. `PublicRequestsPerSecond`와 `PrivateRequestsPerSecond`로 운영 계정의 공식 한도에 맞춰 조정할 수 있다. public 설정값은 한 EIP route의 전체 REST 상한으로도 적용되어 public/private 요청을 합산한다.

HTTP 429와 `Retry-After`는 공통 limiter에 반영된다. 여러 EIP route를 쓰더라도 거래소 제한이나 이용 정책을 우회하는 용도로 사용하면 안 된다.

## EIP 선택

모든 메서드는 `trade.RequestOption`을 받는다. 옵션이 없으면 클라이언트 기본 route를 사용하고, `trade.WithEgressRoute`를 지정하면 해당 요청만 선택한 private IP 전용 연결 풀로 보낸다. HTTP keep-alive 풀은 route별로 분리되어 다른 EIP의 기존 연결을 재사용하지 않는다.

## 공식 기준 문서

- [Advanced Trade REST endpoint 목록](https://docs.cdp.coinbase.com/coinbase-app/advanced-trade-apis/rest-api)
- [Coinbase App API key 인증](https://docs.cdp.coinbase.com/coinbase-app/authentication-authorization/api-key-authentication)
- [공개 상품 목록](https://docs.cdp.coinbase.com/api-reference/advanced-trade-api/rest-api/public/list-public-products)
- [공개 호가](https://docs.cdp.coinbase.com/api-reference/advanced-trade-api/rest-api/public/get-public-product-book)
- [공개 체결](https://docs.cdp.coinbase.com/api-reference/advanced-trade-api/rest-api/public/get-public-market-trades)
- [공개 캔들](https://docs.cdp.coinbase.com/api-reference/advanced-trade-api/rest-api/public/get-public-product-candles)
- [주문 생성](https://docs.cdp.coinbase.com/api-reference/advanced-trade-api/rest-api/orders/create-order)
- [주문 목록](https://docs.cdp.coinbase.com/api-reference/advanced-trade-api/rest-api/orders/list-orders)
- [체결 목록](https://docs.cdp.coinbase.com/api-reference/advanced-trade-api/rest-api/orders/list-fills)
- [Advanced Trade WebSocket 개요](https://docs.cdp.coinbase.com/coinbase-app/advanced-trade-apis/websocket/websocket-overview)
- [Advanced Trade WebSocket 채널](https://docs.cdp.coinbase.com/coinbase-app/advanced-trade-apis/websocket/websocket-channels)
- [Advanced Trade WebSocket 인증](https://docs.cdp.coinbase.com/coinbase-app/advanced-trade-apis/websocket/websocket-authentication)
- [Advanced Trade WebSocket 요청 제한](https://docs.cdp.coinbase.com/coinbase-app/advanced-trade-apis/websocket/websocket-rate-limits)
