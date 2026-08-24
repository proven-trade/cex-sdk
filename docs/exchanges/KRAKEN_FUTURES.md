# Kraken Futures REST·WebSocket v1 어댑터

## 범위

`exchange/kraken/futures`는 Kraken Derivatives REST v3, Futures Charts v1과 WebSocket v1의 첫 번째 범위를 제공한다.

- Futures 상품 규칙과 전체 ticker
- 단일 상품의 전체 비누적 L2 호가
- 최근 100건 공개 체결과 OHLCV 캔들
- cash, margin, multi-collateral 지갑
- 열린 포지션, 미체결 주문, 계정 체결
- 시장가, 지정가, post-only, IOC, FOK 주문 생성
- 주문 취소와 최근 주문 상태 조회
- WebSocket ticker, ticker lite, L2 호가, public 체결
- WebSocket 잔고, 체결, 미체결 주문, 포지션, 계정 원장, 운영 알림
- 요청별 EIP route 선택

조건부 주문, 주문 수정, 일괄 주문, 레버리지 설정과 자금 이동은 후속 범위다. 첫 범위 밖의 주문 종류, 잘못된 수량·가격과 지원하지 않는 WebSocket 구독 조합은 전송 전에 검증 오류로 거부한다.

## 자격증명

`credential.Material`에는 다음 값을 저장한다.

| 필드 | 값 |
|---|---|
| `APIKey` | Kraken Futures API key 원문 |
| `SecretKey` | Kraken Futures가 발급한 Base64 secret 원문 |
| `Passphrase` | 사용하지 않음 |

조회 key에는 `read`, 주문 key에는 `trade` 상위 권한을 선언한다. 실제 Kraken API key의 General 권한도 조회에는 Read Only 이상, 주문에는 Full Access로 설정해야 한다.

```go
descriptor := &credential.Descriptor{
	AccountID:  "kraken-futures-main",
	Exchange:   model.ExchangeKraken,
	SecretRef:  "secret/kraken/futures/main",
	Permissions: []credential.Permission{
		credential.PermissionRead,
		credential.PermissionTrade,
	},
	AllowedEgressRouteIDs: []transport.EgressRouteID{"seoul-a", "seoul-b"},
}
```

private 요청은 파라미터를 URL query로 직렬화하고 body를 비워서 전송한다. `Authent`는 URL 인코딩된 query, `Nonce`, `/api/v3/...` endpoint path를 결합한 SHA-256 digest를 Base64 decode한 secret으로 HMAC-SHA-512 처리한 뒤 Base64로 인코딩한다. 서명 후 query를 수정하지 않는다.

## 클라이언트

```go
client, err := krakenfutures.New(krakenfutures.Config{
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
	krakenfutures.TickersRequest{Symbols: []string{"PI_XBTUSD", "PF_ETHUSD"}},
	trade.WithEgressRoute("seoul-b"),
)
```

모든 가격, 수량, 잔고는 JSON 숫자와 문자열 어느 형식으로 와도 `Decimal` 문자열로 보존한다. 이를 통해 `float64` 변환에서 생기는 주문 정밀도 손실을 피한다.

## 공개 시세

| 메서드 | endpoint | 주의사항 |
|---|---|---|
| `Instruments` | `GET /derivatives/api/v3/instruments` | 상품 규칙과 증거금 구간 반환 |
| `Tickers` | `GET /derivatives/api/v3/tickers` | 전체 상품의 현재 시장 요약 반환 |
| `OrderBook` | `GET /derivatives/api/v3/orderbook` | 전체 비누적 호가 반환 |
| `PublicHistory` | `GET /derivatives/api/v3/history` | 최신순 최대 100건, `lastTime`으로 이전 구간 조회 |
| `Candles` | `GET /api/charts/v1/{tickType}/{symbol}/{resolution}` | `from`과 `to`는 epoch 초, candle `time`은 epoch 밀리초 |

캔들의 `tickType`은 `spot`, `mark`, `trade`이고 지원 구간은 1분부터 1주까지다. 응답의 `MoreCandles`가 참이면 시간 범위를 이동해 다음 데이터를 조회한다.

## 계정과 주문

```go
reference, err := client.PlaceOrder(
	ctx,
	krakenfutures.PlaceOrderRequest{
		OrderType:     krakenfutures.OrderTypeLimit,
		Symbol:        "PI_XBTUSD",
		Side:          krakenfutures.SideBuy,
		Size:          "1",
		LimitPrice:    "64000",
		ClientOrderID: "strategy-1",
		ReduceOnly:    true,
	},
	trade.WithEgressRoute("seoul-b"),
)
```

`CancelOrder`는 `order_id`와 `cliOrdId` 중 정확히 하나를 받는다. 주문 생성과 취소의 `processBefore`를 지정하면 거래소가 그 시각을 지난 요청을 처리하지 않게 할 수 있다.

`OrderStatus`는 최대 100개의 거래소 주문 ID와 client order ID를 조회한다. 이 endpoint는 공식 계약상 General Full Access가 필요하므로 SDK에서도 `trade` 상위 권한을 요구한다. 열려 있거나 최근 5초 안에 체결·취소된 주문만 반환하므로 장기 이력 조회 용도로 사용하면 안 된다.

주문 생성과 취소의 전송 오류, timeout, 읽을 수 없는 성공 응답은 자동 재시도하지 않고 `UNKNOWN_EXECUTION_STATE`로 반환한다. `ClientOrderID`, `OrderStatus`, `OpenOrders`를 이용해 실제 처리 여부를 먼저 확인해야 한다.

## nonce

private 요청은 limiter 대기가 끝난 다음 Secret을 조회하고 서명한다. 한 `Client`에서 생성하는 millisecond nonce는 시계가 멈추거나 뒤로 이동하고 여러 goroutine이 동시에 호출해도 원자적으로 항상 증가한다.

nonce는 API key 단위다. 같은 API key를 여러 프로세스나 여러 `Client`가 동시에 공유하면 로컬 증가 규칙만으로 전체 순서를 보장할 수 없다. 운영에서는 API key 하나당 장수명 `Client` 하나를 사용하거나 외부 공유 단조 nonce 정책을 적용해야 한다.

## 요청 제한

공개 endpoint는 선택한 route, 즉 EIP별 로컬 pool을 사용한다. 공식 문서가 공개 REST의 단일 공통 수치를 명시하지 않으므로 기본값은 route별 초당 20건이며 `PublicRequestsPerSecond`로 보수적으로 낮출 수 있다.

private derivatives endpoint는 계정별 기본 500 point/10초 pool로 제한한다. 지갑·포지션·미체결 주문·기본 체결 조회는 2 point, `lastFillTime`을 사용한 체결 조회는 25 point, 주문 생성·취소는 10 point, 주문 상태 조회는 1 point를 차감한다. 로컬 limiter는 fixed window 근사이므로 거래소의 실제 제한과 오류 관측을 대체하지 않는다. 계정 등급이나 정책이 다르면 `DerivativesPointLimit`과 `DerivativesWindow`를 조정한다.

private 제한은 EIP를 바꿔도 우회되지 않도록 account ID를 기준으로 공유한다. Secret을 조회하기 전에 API key의 route 허용 목록과 read/trade 권한을 검사한다.

HTTP 429의 `Retry-After`는 공통 limiter에 반영한다. HTTP 200이어도 `result`가 `error`이거나 `error`·`errors`가 존재하면 인증, 권한, 제한, 잔고 부족, 주문 없음, 거래소 장애 범주로 정규화한다.

## WebSocket v1

public과 private stream은 모두 `wss://futures.kraken.com/ws/v1`을 사용한다. public stream은 다음 feed를 지원한다.

| feed | typed data | 동작 |
|---|---|---|
| `ticker` | `StreamTicker` | 전체 ticker snapshot과 최대 초당 1회 update |
| `ticker_lite` | `StreamTicker` | 축약 ticker update |
| `book` | `StreamBookSnapshot`, `StreamBookUpdate` | 전체 L2 snapshot 뒤 가격 레벨 delta |
| `trade` | `StreamTradeSnapshot`, `StreamTrade` | 최근 체결 snapshot 뒤 체결 delta |
| `heartbeat` | `StreamMessage` | 연결 상태 확인 feed |

```go
streamClient, err := krakenfutures.NewStreamClient(krakenfutures.StreamClientConfig{
	Connector:            streamConnector,
	RESTClient:           client,
	DefaultEgressRouteID: "seoul-a",
})
if err != nil {
	return err
}

public, err := streamClient.PublicStream(
	krakenfutures.PublicStreamRequest{Subscriptions: []krakenfutures.PublicStreamSubscription{
		{Feed: krakenfutures.PublicFeedTicker, ProductIDs: []string{"PI_XBTUSD"}},
		{Feed: krakenfutures.PublicFeedBook, ProductIDs: []string{"PI_XBTUSD"}},
	}},
	trade.WithEgressRoute("seoul-b"),
)
if err != nil {
	return err
}
defer public.Close()

err = public.Run(ctx, func(_ context.Context, message krakenfutures.StreamMessage) error {
	if message.Feed != string(krakenfutures.PublicFeedTicker) {
		return nil
	}
	var ticker krakenfutures.StreamTicker
	return message.Decode(&ticker)
})
```

public stream은 `Subscribe`와 `Unsubscribe`로 구독을 변경할 수 있다. 연결이 끊기면 선택한 route를 유지하면서 현재 구독 목록을 정렬된 순서로 복구한다.

`book`을 로컬 장부로 소비할 때는 위 `public.Run` 대신 `LocalOrderBook.Run`이 해당 public stream을 소유하도록 한다.

```go
book, err := krakenfutures.NewLocalOrderBook(krakenfutures.LocalOrderBookConfig{
	ProductID:     "PI_XBTUSD",
	ViewDepth:     20,
	EgressRouteID: "seoul-b",
})
if err != nil {
	return err
}

err = book.Run(ctx, public, func(ctx context.Context, view krakenfutures.LocalOrderBookView) error {
	return handleBook(view)
})
```

SDK는 `book_snapshot`의 전체 장부를 시작점으로 삼고, 이후 `book`의 `seq`가 직전 값보다 정확히 1 증가하는지 검사한다. `side=buy`는 bid, `side=sell`은 ask를 뜻하며 `qty=0`이면 해당 가격 레벨을 제거한다. 중복·과거 update는 무시하고 sequence gap이나 새 연결에서 snapshot보다 먼저 온 update는 장부를 폐기한 뒤 같은 EIP route로 재연결해 새 snapshot을 받는다.

`SynchronizationID`는 적용한 snapshot 횟수, `GapCount`는 재연결을 유발한 sequence 이상 횟수, `Generation`은 WebSocket 연결 세대를 나타낸다. `ViewDepth` 기본값은 20이며 handler 출력만 제한하고 내부 장부는 snapshot의 전체 가격 레벨과 이후 update를 유지한다. 상품·EIP 계약이 public stream과 다르면 네트워크 연결 전에 거부한다.

private stream은 다음 feed를 지원한다.

| feed | typed data | 비고 |
|---|---|---|
| `balances` | `StreamBalances` | holding, 단일 collateral, multi-collateral wallet |
| `fills` | `StreamFills` | 전체 또는 선택 상품 체결 |
| `open_orders` | `StreamOpenOrders` | 미체결 주문 snapshot과 delta |
| `open_orders_verbose` | `StreamOpenOrders` | 실패한 post-only 주문을 포함한 상세 delta |
| `open_positions` | `StreamOpenPositions` | 열린 포지션 snapshot |
| `account_log` | `StreamAccountLog` | wallet·포지션 원장 snapshot과 delta |
| `notifications_auth` | `StreamNotifications` | 점검·시장·기능 알림 |

```go
private, err := streamClient.PrivateStream(
	krakenfutures.PrivateStreamRequest{Subscriptions: []krakenfutures.PrivateStreamSubscription{
		{Feed: krakenfutures.PrivateFeedFills},
		{Feed: krakenfutures.PrivateFeedOpenOrders},
		{Feed: krakenfutures.PrivateFeedOpenPositions},
		{Feed: krakenfutures.PrivateFeedBalances},
	}},
	trade.WithEgressRoute("seoul-b"),
)
```

private 연결은 매번 API key로 새 challenge를 요청한다. challenge 원문을 SHA-256으로 해시하고 Base64 decode한 Secret으로 HMAC-SHA-512 서명한 뒤 Base64로 인코딩한다. 모든 private 구독에는 API key, 원 challenge와 서명을 함께 보낸다. 재연결 시 자격증명을 다시 조회하고 새 challenge를 서명한 뒤 전체 feed를 다시 구독한다.

Secret 조회 전에 API key의 route 허용 목록과 `read` 권한을 검사한다. 최초 handshake와 모든 재연결은 같은 EIP route를 사용한다. 거래소가 private 구독 승인 응답에서 인증 필드를 되돌려주더라도 `StreamMessage.Raw`에서는 `api_key`, `original_challenge`, `signed_challenge`를 제거한다. 자격증명 바이트와 송신 JSON은 사용 직후 덮어쓴다.

공식 연결 관리 지침은 60초보다 자주 ping을 보내도록 요구한다. 기본 ping 간격은 30초이며 `PingInterval`로 조정할 수 있다. challenge 응답 제한 시간은 기본 10초이고 `ChallengeTimeout`으로 설정한다.

## 공식 문서

- [Futures REST 인증](https://docs.kraken.com/api/docs/guides/futures-rest/)
- [Futures WebSocket 개요](https://docs.kraken.com/exchange/api-reference/futures-websocket)
- [Futures WebSocket challenge](https://docs.kraken.com/exchange/api-reference/futures-websocket/challenge)
- [Futures WebSocket challenge 서명](https://support.kraken.com/articles/360022635652-sign-challenge-websocket-api-derivatives)
- [Futures WebSocket ticker](https://docs.kraken.com/exchange/api-reference/futures-websocket/ticker)
- [Futures WebSocket book](https://docs.kraken.com/exchange/api-reference/futures-websocket/book)
- [Futures WebSocket fills](https://docs.kraken.com/exchange/api-reference/futures-websocket/fills)
- [Futures WebSocket open orders](https://docs.kraken.com/exchange/api-reference/futures-websocket/open_orders)
- [Futures WebSocket open positions](https://docs.kraken.com/exchange/api-reference/futures-websocket/open_position)
- [Futures WebSocket balances](https://docs.kraken.com/exchange/api-reference/futures-websocket/balances)
- [상품 목록](https://docs.kraken.com/api/docs/futures-api/trading/get-instruments)
- [Ticker](https://docs.kraken.com/api/docs/futures-api/trading/get-tickers)
- [호가](https://docs.kraken.com/api/docs/futures-api/trading/get-orderbook)
- [공개 체결](https://docs.kraken.com/api/docs/futures-api/trading/get-history)
- [캔들](https://docs.kraken.com/api/docs/futures-api/charts/candles)
- [지갑](https://docs.kraken.com/api/docs/futures-api/trading/get-accounts)
- [포지션](https://docs.kraken.com/api/docs/futures-api/trading/get-open-positions)
- [주문 생성](https://docs.kraken.com/api/docs/futures-api/trading/send-order)
- [주문 취소](https://docs.kraken.com/api/docs/futures-api/trading/cancel-order)
- [주문 상태](https://docs.kraken.com/api/docs/futures-api/trading/get-order-status)
- [체결 이력](https://docs.kraken.com/api/docs/futures-api/trading/get-fills)
