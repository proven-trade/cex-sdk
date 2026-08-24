# Bybit V5 Spot·Linear REST 어댑터

## 범위

`exchange/bybit` 패키지는 Bybit V5의 다음 상품과 REST 기능을 제공합니다.

| Category | 공개 시세 | 계정 | 포지션 | 주문 |
|---|---:|---:|---:|---:|
| `spot` | 지원 | Unified 잔고 | 해당 없음 | 지원 |
| `linear` | 지원 | Unified 잔고 | 지원 | 지원 |

공개 시세는 상품 정보, ticker, 호가, 최근 체결, 캔들을 포함합니다. 주문은 생성, 단건 조회, 취소, 미체결 목록, 이력을 포함합니다. Inverse, Option, 자산 이동, 일괄 주문, 조건부 주문 전용 필드는 아직 범위에 포함하지 않습니다.

## 클라이언트 생성

운영 기본 endpoint는 `https://api.bybit.com`입니다. `Testnet: true`를 지정하면 별도 `BaseURL`이 없는 경우 `https://api-testnet.bybit.com`을 사용합니다.

```go
client, err := bybit.New(bybit.Config{
	Executor:             executor,
	Credentials:          descriptor,
	CredentialProvider:   secretProvider,
	DefaultEgressRouteID: "seoul-a",
	ReceiveWindow:        5 * time.Second,
})
if err != nil {
	return err
}

order, err := client.PlaceOrder(
	ctx,
	bybit.PlaceOrderRequest{
		Category:     bybit.CategoryLinear,
		Symbol:       "BTCUSDT",
		Side:         bybit.SideBuy,
		OrderType:    bybit.OrderTypeLimit,
		Quantity:     "0.001",
		Price:        "60000",
		TimeInForce:  bybit.TimeInForceGTC,
		OrderLinkID:  "strategy-a-0001",
	},
	trade.WithEgressRoute("seoul-b"),
)
```

자격증명에 `seoul-b`가 허용되어 있지 않으면 Secret Provider를 호출하거나 HTTP 요청을 보내기 전에 실패합니다.

## 인증과 시간 보정

HMAC API Key는 `credential.Material`의 다음 값을 사용합니다.

| 필드 | Bybit 값 |
|---|---|
| `APIKey` | API Key |
| `SecretKey` | HMAC Secret Key |

GET 요청은 최종 정렬 query string, POST 요청은 실제 전송할 JSON 바이트를 아래 원문 뒤에 붙여 HMAC SHA-256 소문자 16진수 서명을 만듭니다.

```text
timestamp + apiKey + receiveWindow + queryStringOrJSONBody
```

요청에는 `X-BAPI-API-KEY`, `X-BAPI-TIMESTAMP`, `X-BAPI-RECV-WINDOW`, `X-BAPI-SIGN` 헤더가 추가됩니다. 서명은 요청 제한 대기가 끝난 다음 생성합니다.

`ServerTime`은 요청 왕복 시간의 중간값과 응답의 서버 시간을 비교해 클라이언트 서명 시계 오프셋을 갱신합니다. 운영 환경에서도 NTP 또는 chrony 동기화가 전제입니다.

## 요청 제한

모든 요청은 route별 `600/IP/5초` bucket을 차감합니다. 추가로 공개 endpoint는 route별 보수적 endpoint bucket, private endpoint는 계정별 endpoint bucket을 사용합니다.

- 공개 조회: 기본 `20/route/초`, 서버 시간 `50/route/초`
- private 조회: `20/account/초`
- 주문 생성·취소: `10/account/초`

서버가 `X-Bapi-Limit`과 `X-Bapi-Limit-Status`를 반환하고 해당 endpoint의 설정 한도와 일치하면 로컬 사용량에 반영합니다. 계정 bucket은 EIP route를 바꿔도 공유되므로 다중 EIP를 UID 제한 우회 수단으로 사용하지 않습니다. VIP 또는 Pro 등급에서 실제 한도가 다르면 해당 등급을 반영하는 설정 확장이 필요합니다.

## 안전한 주문 실패

주문 생성과 취소는 mutation으로 처리합니다. 전송 타임아웃, 연결 단절, 읽을 수 없는 응답, HTTP 5xx, Bybit `10000`·`10016` 응답처럼 실행 여부가 불명확한 결과는 `trade.ErrUnknownExecutionState`로 반환하며 자동 재시도하지 않습니다.

가능하면 고유한 `OrderLinkID`를 지정하고 `OrderInfo` 또는 향후 private order stream으로 최종 상태를 확인해야 합니다. 성공한 생성·취소 응답도 접수 결과이므로 최종 주문 상태를 뜻하지 않습니다.

## 요청별 EIP 선택

모든 메서드는 마지막 인자로 `trade.RequestOption`을 받습니다. 옵션이 없으면 클라이언트 기본 route를 사용하고, `trade.WithEgressRoute`를 지정하면 해당 요청만 다른 private IP 전용 연결 풀로 보냅니다.

```go
book, err := client.OrderBook(
	ctx,
	bybit.OrderBookRequest{
		Category: bybit.CategorySpot,
		Symbol:   "BTCUSDT",
		Limit:    50,
	},
	trade.WithEgressRoute("seoul-b"),
)
```

## 공식 기준 문서

- [Bybit V5 Integration Guidance](https://bybit-exchange.github.io/docs/v5/guide)
- [Bybit V5 Rate Limit](https://bybit-exchange.github.io/docs/v5/rate-limit)
- [Bybit V5 Market](https://bybit-exchange.github.io/docs/v5/market/instrument)
- [Bybit V5 Place Order](https://bybit-exchange.github.io/docs/v5/order/create-order)
- [Bybit V5 Wallet Balance](https://bybit-exchange.github.io/docs/v5/account/wallet-balance)
