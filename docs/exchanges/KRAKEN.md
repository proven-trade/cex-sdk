# Kraken Spot REST 어댑터

## 범위

첫 번째 범위는 Kraken Spot REST API다.

- 서버 시간
- Spot 상품 정보와 주문 단위
- 전체 또는 선택 상품 ticker
- L2 호가, 최근 공개 체결, OHLCV
- 계정의 자산별 총 잔고
- 시장가·지정가 주문 생성
- 최대 50건 주문 조회와 단건 취소
- 미체결·종료 주문 목록과 계정 체결 이력
- 요청별 EIP route 선택

마진 주문, 조건부 주문, 주문 수정, batch 주문, 자금 이동, Futures와 WebSocket은 후속 범위다. 지원하지 않는 주문 종류는 전송 전에 검증 오류로 거부한다.

## 자격증명

`credential.Material`에는 다음 값을 저장한다.

| 필드 | 값 |
|---|---|
| `APIKey` | Kraken public API key 원문 |
| `SecretKey` | Kraken이 발급한 Base64 secret 원문 |
| `Passphrase` | 사용하지 않음 |

조회 API key에는 `read`, 주문 API key에는 `trade` 상위 권한을 선언한다. 실제 Kraken key에도 호출 endpoint에 대응하는 funds·orders 권한을 부여해야 한다. API key에 API 2FA를 켜면 private 요청마다 유효한 `otp`가 필요하므로 현재 범위에서는 지원하지 않는다.

```go
descriptor := &credential.Descriptor{
	AccountID:  "kraken-main",
	Exchange:   model.ExchangeKraken,
	SecretRef:  "secret/kraken/main",
	Permissions: []credential.Permission{
		credential.PermissionRead,
		credential.PermissionTrade,
	},
	AllowedEgressRouteIDs: []transport.EgressRouteID{"seoul-a", "seoul-b"},
}
```

private 요청 body는 `application/x-www-form-urlencoded`로 직렬화한다. `API-Sign`은 URI path와 `SHA256(nonce + POST data)`를 결합한 뒤 Base64 decode한 secret으로 HMAC-SHA-512를 계산하고 다시 Base64로 인코딩한다.

## 클라이언트

```go
client, err := kraken.New(kraken.Config{
	Executor:             executor,
	Credentials:          descriptor,
	CredentialProvider:   secretProvider,
	DefaultEgressRouteID: "seoul-a",
})
if err != nil {
	return err
}

tickers, err := client.Tickers(
	ctx,
	kraken.TickersRequest{Pairs: []string{"XBTUSD", "ETHUSD"}},
	trade.WithEgressRoute("seoul-b"),
)
```

Kraken 응답의 상품 key는 요청 alias와 다를 수 있다. 예를 들어 `XBTUSD` 요청 결과가 `XXBTZUSD` key로 반환될 수 있으므로 `Ticker.PairID`, `OrderBook.PairID`, `RecentTrades.PairID`, `Candles.PairID`에 실제 응답 key를 보존한다.

## 공개 시세

| 메서드 | endpoint | 주의사항 |
|---|---|---|
| `ServerTime` | `GET /0/public/Time` | Unix 초와 RFC 1123 원문 반환 |
| `AssetPairs` | `GET /0/public/AssetPairs` | 상품 ID, alias, 주문 최소값, tick size 반환 |
| `Tickers` | `GET /0/public/Ticker` | 상품 목록을 비우면 전체 ticker 반환 |
| `OrderBook` | `GET /0/public/Depth` | 단일 상품, 최대 500개 L2 가격 레벨 |
| `RecentTrades` | `GET /0/public/Trades` | `Last`를 다음 `Since`에 전달 |
| `Candles` | `GET /0/public/OHLC` | 공식 계약상 최대 최근 720개 반환 |

OHLC 응답의 마지막 항목은 아직 끝나지 않은 현재 구간이며 `Since` 값과 관계없이 항상 포함된다. 확정 봉만 필요한 전략은 현재 시각과 interval을 비교해 마지막 항목을 제외해야 한다.

## 주문과 계정

```go
reference, err := client.PlaceOrder(
	ctx,
	kraken.PlaceOrderRequest{
		Pair:          "XBTUSD",
		Side:          kraken.SideBuy,
		OrderType:     kraken.OrderTypeLimit,
		Volume:        "0.01",
		Price:         "64000",
		ClientOrderID: "strategy-1",
		PostOnly:      true,
	},
	trade.WithEgressRoute("seoul-b"),
)
```

첫 범위의 `PlaceOrder`는 Spot 시장가와 지정가만 지원한다. `ClientOrderID`는 Kraken 계약에 맞춰 최대 18자의 안전한 문자열 또는 UUID를 허용한다. `ValidateOnly`는 검증만 수행하며 이때 빈 transaction ID 목록도 정상 결과다.

`CancelOrder`는 취소 제한을 계정+상품 단위로 안전하게 적용하기 위해 `Pair`를 요구한다. `TransactionID`와 `ClientOrderID` 중 정확히 하나를 지정하며 각각 Kraken의 `txid`와 `cl_ord_id`로 전송한다. 취소 접수 뒤 최종 상태는 `OrderInfo`, `OpenOrders`, `ClosedOrders`에서 확인한다.

주문 생성과 취소의 전송 오류, timeout, 읽을 수 없는 성공 응답은 자동 재시도하지 않고 `UNKNOWN_EXECUTION_STATE`로 반환한다. `ClientOrderID`와 주문 조회를 이용해 실제 처리 여부를 먼저 확인해야 한다.

## nonce

private 요청은 limiter 대기가 끝난 다음 Secret을 조회하고 서명한다. 한 `Client`에서 생성하는 millisecond nonce는 시계가 멈추거나 뒤로 이동하고 여러 goroutine이 동시에 호출해도 원자적으로 항상 증가한다.

Kraken nonce는 API key 단위다. 같은 API key를 여러 프로세스나 여러 `Client`가 동시에 공유하면 각 로컬 증가 규칙만으로 전체 순서를 보장할 수 없다. 운영에서는 API key 하나당 장수명 `Client` 하나를 사용하거나 외부에서 공유하는 단조 nonce 정책을 적용해야 한다.

## 요청 제한

기본 공개 제한은 route, 즉 EIP별 초당 1건이다. 공식 정책상 `Trades`와 `OHLC`는 IP+상품별 제한이고 나머지는 IP별 제한이지만, 첫 범위는 모든 공개 호출을 EIP별 초당 1건으로 더 보수적으로 직렬화한다. `PublicRequestsPerSecond`로 조정할 수 있다.

private account endpoint는 API key counter를 근사해 기본 40초 구간에서 20 point를 허용한다. 일반 조회는 1 point, `ClosedOrders`와 `TradesHistory`는 공식 증가량에 맞춰 4 point다. 실제 정책은 연속 감소 counter이므로 로컬 fixed window는 보수적 근사다. `PrivateCounterLimit`과 `PrivateCounterWindow`로 계정 등급에 맞춰 조정할 수 있다.

주문 생성·취소는 private account counter와 분리하고 계정+상품별 기본 초당 1건으로 제한한다. Kraken의 실제 trading limiter는 주문이 장부에 머문 시간과 상호작용에 따른 point 방식이므로 로컬 제한이 거래소 제한을 대체하지 않는다. `TradingRequestsPerSecond`는 운영 전략에 맞춰 조정하되 `EOrder:Rate limit exceeded`를 관측해야 한다.

HTTP 429·418의 `Retry-After`는 공통 limiter에 반영한다. HTTP 200이어도 `error` 배열이 비어 있지 않으면 실패이며 인증, 권한, 제한, 잔고 부족, 주문 없음, 거래소 장애 범주로 정규화한다.

## 다중 EIP

public 요청 제한은 선택한 route별로 분리된다. private account와 주문 제한은 EIP를 바꿔도 계정 규칙을 우회하지 않도록 account ID와 상품을 기준으로 공유한다. Secret을 조회하기 전에 API key의 route 허용 목록을 검사한다.

## 공식 문서

- [Spot REST 인증](https://docs.kraken.com/exchange/guides/rest/authentication)
- [서버 시간](https://docs.kraken.com/api-reference/market-data/get-server-time)
- [거래 가능 상품](https://docs.kraken.com/api-reference/market-data/get-tradable-asset-pairs)
- [Ticker](https://docs.kraken.com/api-reference/market-data/get-ticker-information)
- [L2 호가](https://docs.kraken.com/api-reference/market-data/get-order-book)
- [최근 체결](https://docs.kraken.com/api-reference/market-data/get-recent-trades)
- [OHLC](https://docs.kraken.com/api-reference/market-data/get-ohlc-data)
- [주문 생성](https://docs.kraken.com/api-reference/trading/add-order)
- [주문 취소](https://docs.kraken.com/api-reference/trading/cancel-order)
- [Spot API 요청 제한](https://support.kraken.com/articles/206548367-what-are-the-api-rate-limits-)
