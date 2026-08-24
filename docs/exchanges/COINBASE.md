# Coinbase Advanced Trade Spot REST 어댑터

## 범위

첫 번째 범위는 Coinbase Advanced Trade v3의 Spot REST API다.

- 서버 시간
- 공개 Spot 상품 목록과 단건 상품
- 공개 호가, 최근 체결, 캔들
- 거래 계정 목록과 단건 계정
- 시장가 IOC, 지정가 GTC 주문 생성
- 최대 100건 주문 일괄 취소
- 주문 단건과 cursor 기반 주문 목록
- cursor 기반 체결 목록
- 요청별 EIP route 선택

고급 주문, 포트폴리오 자금 이동, 파생상품, WebSocket은 이 단계의 범위가 아니다. 지원하지 않는 주문 설정을 임의의 필드 조합으로 전송하지 않고 명시적으로 거부한다.

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

첫 범위는 다음 두 설정을 타입으로 분리한다.

- `MarketMarketIOC`: `QuoteSize` 또는 `BaseSize` 중 정확히 하나
- `LimitLimitGTC`: `BaseSize`, `LimitPrice`, 선택적 `PostOnly`

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
