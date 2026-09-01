# Upbit Spot 어댑터

## 전제조건

private API를 사용하려면 `credential.Provider`가 반환하는 `credential.Material`에 다음 값을 넣어야 합니다.

| 필드 | Upbit 값 |
|---|---|
| `APIKey` | Access Key |
| `SecretKey` | Secret Key 원문 |

Secret Key는 Base64 디코딩하지 않고 HS512 서명 키로 직접 사용합니다. `credential.Descriptor.AccountID`에는 개별 Access Key가 아니라 요청 제한을 공유하는 업비트 계정 pocket 식별자를 넣어야 합니다. 같은 pocket의 여러 키가 같은 요청 제한을 공유하기 때문입니다.

자격증명에 설정한 `AllowedEgressRouteIDs` 밖의 route는 Secret 조회 전에 차단됩니다.

## 지원 범위

| 영역 | 메서드 |
|---|---|
| 마켓 | `Markets` |
| 공개 시세 | `Tickers`, `OrderBooks`, `RecentTrades`, `MinuteCandles` |
| 계정 | `Accounts` |
| 주문 | `PlaceOrder`, `OrderInfo`, `CancelOrder`, `OpenOrders`, `ClosedOrders` |

주문 생성은 `limit`, 매수 시장가 `price`, 매도 시장가 `market`, 최유리 지정가 `best`를 지원합니다. `best` 매수는 총액 `Price`, 매도는 수량 `Volume`, 그리고 `ioc` 또는 `fok`가 필요합니다. 주문 가능 정보와 일괄 취소는 아직 포함하지 않습니다.

가격, 수량, 금액은 `float64`로 변환하지 않습니다. private 응답은 거래소가 제공한 문자열을 유지하고 공개 시세의 JSON 숫자는 `Decimal` 문자열 타입으로 손실 없이 보존합니다.

## JWT와 쿼리 해시

인증 요청은 요청 제한 대기가 끝난 뒤 최종 파라미터를 기준으로 서명합니다.

1. 요청마다 UUID v4 nonce를 생성합니다.
2. 파라미터가 있으면 URL 인코딩 전 쿼리 문자열을 SHA-512 해시합니다.
3. `access_key`, `nonce`, `query_hash`, `query_hash_alg=SHA512`를 HS512 JWT payload로 만듭니다.
4. `Authorization: Bearer <JWT>` 헤더로 전송합니다.

반복 파라미터는 `states[]=wait&states[]=watch`처럼 순서와 키를 그대로 유지합니다. POST 주문은 JSON 본문의 필드 순서와 쿼리 해시 입력 순서를 같은 ordered parameter에서 생성합니다.

`NonceSource`는 테스트를 위한 주입 지점입니다. 운영에서는 기본 암호학적 난수 UUID 생성기를 사용해야 하며, 고정 nonce를 설정하면 안 됩니다.

## 요청 제한

SDK는 공개 API를 송신 경로별 IP bucket으로, private API를 `AccountID`별 pocket bucket으로 관리합니다.

| 그룹 | 로컬 제한 | 범위 |
|---|---:|---|
| `market`, `ticker`, `orderbook`, `trade`, `candle` | 각 10회/초 | IP route |
| `default` | 30회/초 | 계정 pocket |
| `order` | 12회/초 | 계정 pocket |

2026-08-21 적용된 주문 그룹 상향값 `12회/초`를 반영했습니다. 응답의 `Remaining-Req` 중 `group`과 `sec` 값을 로컬 limiter에 반영합니다. HTTP 429와 418은 `Retry-After`가 있으면 그 기간, 없으면 최소 1초 동안 관련 bucket을 차단합니다.

여러 공인 송신 IP를 사용해도 pocket 단위 제한은 증가하지 않습니다. route 선택은 API Key IP 허용 목록 준수와 네트워크 격리를 위한 기능이며 거래소 제한 우회 용도가 아닙니다.

## 안전한 주문 실패

주문 생성·취소의 전송 오류, 응답 본문 읽기 실패, HTTP 5xx는 실제 체결 여부를 단정할 수 없습니다. SDK는 자동 재시도하지 않고 `trade.ErrUnknownExecutionState`로 반환합니다.

가능하면 고유한 `Identifier`를 주문에 넣고, 불명확한 결과가 나오면 `OrderInfo`로 최종 상태를 확인해야 합니다. 주문 목록을 전체 마켓으로 조회하려면 실수로 조회 범위를 넓히지 않도록 `AllMarkets: true`를 명시해야 합니다.

## WebSocket

업비트 WebSocket endpoint는 다음과 같습니다.

| 연결 | Endpoint | 제한 측정 단위 |
|---|---|---|
| public 시세 | `wss://api.upbit.com/websocket/v1` | IP |
| private Exchange | `wss://api.upbit.com/websocket/v1/private` | 계정 pocket |

`StreamClient`의 모든 연결과 재연결은 세션 생성 시 선택한 송신 경로에 고정됩니다.

```go
streams, err := upbit.NewStreamClient(upbit.StreamClientConfig{
	Connector:             connector,
	Credentials:           descriptor,
	CredentialProvider:    secretProvider,
	DefaultEgressRouteID: "seoul-a",
})
if err != nil {
	return err
}

public, err := streams.PublicStream(
	upbit.StreamRequest{
		Types: []upbit.StreamDataType{
			{Type: "ticker", Codes: []string{"KRW-BTC"}},
			{Type: "trade", Codes: []string{"KRW-BTC"}, OnlyRealtime: true},
		},
		Format: upbit.StreamFormatDefault,
	},
	trade.WithEgressRoute("seoul-b"),
)
if err != nil {
	return err
}
defer public.Close()

err = public.Run(ctx, func(ctx context.Context, message upbit.StreamMessage) error {
	if message.Error != nil {
		return fmt.Errorf("Upbit stream error: %s", message.Error.Message)
	}
	if message.Status != "" {
		return nil
	}
	switch message.Type {
	case "ticker":
		var event upbit.StreamTicker
		if err := message.Decode(&event); err != nil {
			return err
		}
		return handleTicker(event)
	case "trade":
		var event upbit.StreamTrade
		if err := message.Decode(&event); err != nil {
			return err
		}
		return handleTrade(event)
	default:
		return nil
	}
})
```

public typed 범위는 다음과 같습니다.

- `ticker`: `StreamTicker`
- `trade`: `StreamTrade`
- `orderbook`: `StreamOrderBook`
- `candle.{unit}`: `StreamCandle`

`OnlySnapshot`과 `OnlyRealtime`은 동시에 지정할 수 없습니다. 응답 형식은 `DEFAULT`, `SIMPLE`, `JSON_LIST`, `SIMPLE_LIST`를 지원합니다. 오더북 `StreamOrderBook`은 전체 필드와 축약 필드를 모두 decode하며, 다른 typed 구조체에서 `SIMPLE` 계열을 사용할 때는 `Payload`에서 축약 필드 구조체로 직접 decode합니다.

### Spot 로컬 오더북 snapshot

업비트 오더북은 증분 delta가 아니라 SNAPSHOT과 REALTIME 모두 각 메시지에 완전한 호가 목록을 제공합니다. 공식 응답에는 오더북 sequence가 없으므로 REST snapshot과 임의의 연속 번호를 조합하지 않습니다. `LocalOrderBook`은 각 메시지의 마켓·timestamp·정렬·가격·수량을 독립적으로 검증하고 최신 snapshot view로 전달합니다.

```go
public, err := streams.PublicStream(
	upbit.StreamRequest{
		Types: []upbit.StreamDataType{{
			Type:  "orderbook",
			Codes: []string{"KRW-BTC.5"},
			Level: "10000",
		}},
		Format: upbit.StreamFormatSimpleList,
	},
	trade.WithEgressRoute("seoul-b"),
)
if err != nil {
	return err
}
defer public.Close()

book, err := upbit.NewLocalOrderBook(upbit.LocalOrderBookConfig{
	Market:        "KRW-BTC",
	EgressRouteID: "seoul-b",
	ViewDepth:     5,
})
if err != nil {
	return err
}

err = book.Run(ctx, public, func(ctx context.Context, view upbit.LocalOrderBookView) error {
	return consumeBook(view)
})
```

운영 계약은 다음과 같습니다.

- 로컬 오더북과 WebSocket의 `EgressRouteID`가 다르면 연결 전에 거부합니다.
- 마켓 코드는 대문자로 정규화하며 `.1`, `.5`, `.15`, `.30` 호가 개수 옵션을 검증합니다.
- KRW 마켓의 `Level`은 JSON 숫자로 전송하며, 다른 stream type에는 지정할 수 없습니다.
- 각 이벤트가 완전한 snapshot이므로 이전 상태를 병합하지 않고 통째로 교체합니다.
- 네트워크 재연결 시 같은 송신 경로와 새 ticket으로 다시 구독하며, 다음 완전한 snapshot부터 `Generation`이 증가한 view를 제공합니다.
- 공식 오더북 응답에 sequence가 없으므로 탐지할 수 없는 gap count를 만들지 않습니다. `SnapshotID`, `Generation`, `Timestamp`, `StreamType`으로 수신과 재연결을 관측합니다.

private stream은 handshake 요청의 `Authorization` 헤더에 HS512 JWT를 넣습니다.

```go
private, err := streams.PrivateStream(
	upbit.StreamRequest{
		Types: []upbit.StreamDataType{
			{Type: "myOrder", Codes: []string{"KRW-BTC"}},
			{Type: "myAsset"},
		},
	},
	trade.WithEgressRoute("seoul-b"),
)
if err != nil {
	return err
}
defer private.Close()
```

세션 생성 시 자격증명의 read 권한과 route 허용 목록을 Secret 조회보다 먼저 검사합니다. 실제 연결과 재연결에서는 다음을 매번 새로 수행합니다.

1. Secret Provider에서 Access Key와 Secret Key 조회
2. 중복되지 않는 nonce 생성
3. query hash가 없는 WebSocket handshake용 HS512 JWT 생성
4. `Authorization: Bearer <JWT>` 헤더로 private endpoint 연결
5. 연결별 새 ticket을 포함한 `myOrder`·`myAsset` 구독 요청 전송
6. 사용한 자격증명 byte slice 파기

private typed event는 `MyOrderEvent`와 `MyAssetEvent`입니다. 자격증명과 JWT는 상태 이벤트나 decode 오류에 포함하지 않습니다.

### 연결 관리와 요청 제한

업비트는 120초 동안 송수신이 없으면 idle connection을 종료합니다. SDK는 기본 30초마다 WebSocket PING frame을 보내고 10초 안에 PONG을 받지 못하면 같은 route로 재연결합니다.

- 연결 요청: 초당 최대 5회
- 데이터 요청 메시지: 연결당 초당 최대 5회, 분당 최대 100회
- 인증 없는 연결 요청: IP 단위
- 인증 연결 요청: 계정 pocket 단위
- Origin 헤더가 있는 연결: 별도 10초당 1회 제한

SDK는 브라우저가 아니므로 Origin 헤더를 설정하지 않습니다. 한 세션은 최초 연결 또는 재연결 직후 구독 요청 한 건만 보내며, 구독을 바꾸려면 기존 세션을 종료하고 새 세션을 만듭니다. 여러 세션과 프로세스의 연결 시도 합계는 운영 메트릭에서 감시해야 합니다. `Run` context가 세션 수명을 결정하므로 stream 생성 시 `trade.WithTimeout`은 허용하지 않습니다.

## 공식 기준 문서

- [Upbit 인증](https://docs.upbit.com/kr/reference/auth)
- [Upbit 요청 수 제한](https://docs.upbit.com/kr/reference/rate-limits)
- [Upbit WebSocket 사용 안내](https://docs.upbit.com/kr/reference/websocket-guide)
- [Upbit 호가 WebSocket](https://docs.upbit.com/kr/reference/websocket-orderbook)
- [Upbit WebSocket 요청 형식](https://docs.upbit.com/kr/reference/websocket-request-format)
- [Upbit 내 주문 WebSocket](https://docs.upbit.com/kr/reference/websocket-myorder)
- [Upbit 내 자산 WebSocket](https://docs.upbit.com/kr/reference/websocket-myasset)
- [Upbit 주문 생성](https://docs.upbit.com/kr/reference/new-order)
- [Upbit order 요청 수 제한 상향](https://docs.upbit.com/kr/changelog/order-rate-limit-update)
