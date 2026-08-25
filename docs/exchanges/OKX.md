# OKX V5 Spot·SWAP 어댑터

## 범위

`exchange/okx` 패키지는 OKX V5의 다음 REST 기능을 제공합니다.

| 상품 | 공개 시세 | 거래 계정 | 포지션 | 주문 |
|---|---:|---:|---:|---:|
| `SPOT` | 지원 | 지원 | 해당 없음 | 현금 거래 지원 |
| `SWAP` | 지원 | 지원 | 지원 | 교차·격리 마진 지원 |

공개 시세는 상품 규칙, 전체 ticker, 호가, 최근 체결, 캔들을 포함합니다. 주문은 생성, 단건 조회, 취소, 미체결 목록, 최근 7일 이력을 포함합니다. Spot margin, 선물, 옵션, 일괄 주문, 조건부 주문, 자산 이동은 아직 범위에 포함하지 않습니다.

## 클라이언트와 지역별 endpoint

기본 REST endpoint는 `https://www.okx.com`입니다. 계정 등록 지역에 따라 OKX가 별도 도메인을 요구하면 공식 안내에 맞는 주소를 `BaseURL`로 지정해야 합니다. SDK는 지역 제한을 우회하거나 임의의 대체 도메인을 선택하지 않습니다.

```go
client, err := okx.New(okx.Config{
	Executor:             executor,
	Credentials:          descriptor,
	CredentialProvider:   secretProvider,
	DefaultEgressRouteID: "seoul-a",
	BaseURL:              "https://www.okx.com",
	DemoTrading:          true,
})
if err != nil {
	return err
}
```

`DemoTrading: true`이면 모든 요청에 `x-simulated-trading: 1` 헤더를 추가합니다. 운영 키와 Demo 키 및 endpoint 설정을 섞지 않아야 합니다.

## 인증과 시간 보정

private API는 `credential.Material`의 다음 세 값을 사용합니다.

| 필드 | OKX 값 |
|---|---|
| `APIKey` | API Key |
| `SecretKey` | Secret Key |
| `Passphrase` | API Key 생성 시 설정한 Passphrase |

서명 원문은 다음과 같습니다.

```text
timestamp + uppercase(method) + requestPathWithSortedQuery + exactJSONBody
```

HMAC SHA-256 결과를 Base64로 인코딩해 `OK-ACCESS-SIGN`에 넣습니다. `OK-ACCESS-TIMESTAMP`는 UTC ISO-8601 밀리초 형식을 사용합니다. 서명은 요청 제한 대기가 끝난 뒤 최종 query와 JSON 바이트를 대상으로 생성합니다.

`ServerTime`은 요청 왕복 시간의 중간값과 서버 timestamp를 비교해 서명 시계 오프셋을 갱신합니다. 운영 환경에서도 NTP 또는 chrony 동기화가 전제입니다.

## 계정 모드와 주문

Spot 현금 주문은 `InstrumentTypeSpot`과 `TradeModeCash`를 사용합니다. Spot 시장가 주문은 `TargetCurrencyBase` 또는 `TargetCurrencyQuote`로 `sz`의 기준 통화를 명시할 수 있습니다.

SWAP 주문은 `TradeModeCross` 또는 `TradeModeIsolated`를 사용합니다. 계정이 long/short 포지션 모드이면 `PositionSideLong` 또는 `PositionSideShort`를 지정하고, net 모드이면 비워 두거나 `PositionSideNet`을 사용합니다. SDK는 계정의 실제 포지션 모드를 임의로 변경하지 않으므로 계정 설정과 요청이 맞지 않으면 OKX 오류를 그대로 정규화해 반환합니다.

```go
reference, err := client.PlaceOrder(
	ctx,
	okx.PlaceOrderRequest{
		InstrumentType: okx.InstrumentTypeSwap,
		InstrumentID:   "BTC-USDT-SWAP",
		TradeMode:      okx.TradeModeCross,
		ClientOrderID:  "strategya0001",
		Side:           okx.SideBuy,
		PositionSide:   okx.PositionSideLong,
		OrderType:      okx.OrderTypeLimit,
		Quantity:       "1",
		Price:          "60000",
	},
	trade.WithEgressRoute("seoul-b"),
)
```

가격과 수량은 `float64`가 아닌 decimal 문자열입니다. `ClientOrderID`는 OKX 규칙에 맞게 영문자와 숫자로 최대 32자까지 허용합니다.

## 요청 제한

SDK limiter는 공식 제한의 실제 scope를 분리합니다.

| endpoint 종류 | 로컬 bucket | 제한 |
|---|---|---:|
| 상품 규칙 | route + 상품 유형 | 20/2초 |
| 전체 ticker | route | 20/2초 |
| 호가 | route | 40/2초 |
| 최근 체결 | route | 100/2초 |
| 캔들 | route | 40/2초 |
| 잔고·포지션 | 계정 | 10/2초 |
| 주문 생성·취소·단건 조회 | 계정 + 상품 + HTTP method | 60/2초 |
| 미체결 주문 | 계정 | 60/2초 |
| 최근 7일 주문 이력 | 계정 | 40/2초 |

송신 경로를 바꾸면 IP scope만 분리됩니다. 사용자 ID 또는 사용자+상품 scope 제한은 같은 계정 bucket을 공유하므로 다중 송신 IP를 계정 제한 우회 수단으로 사용하지 않습니다.

## 안전한 주문 실패

OKX는 top-level `code`가 `0`이어도 주문 접수 항목의 `sCode`로 실패를 반환할 수 있습니다. SDK는 두 수준을 모두 검사합니다.

주문 생성·취소의 전송 타임아웃, 연결 단절, 읽을 수 없는 응답, HTTP 5xx, `50004`·`50013`·`50026`·`51149`처럼 실행 여부가 불명확한 결과는 `trade.ErrUnknownExecutionState`로 반환하며 자동 재시도하지 않습니다. 고유한 `ClientOrderID`로 `OrderInfo` 또는 향후 private order stream을 조회해 최종 상태를 조정해야 합니다.

## 요청별 송신 경로 선택

모든 메서드는 `trade.RequestOption`을 받습니다. 옵션이 없으면 클라이언트 기본 route를 사용하고, `trade.WithEgressRoute`를 지정하면 해당 요청만 선택한 local source IP 전용 연결 풀로 보냅니다. 자격증명에 허용되지 않은 route는 Secret 조회 전에 거부됩니다.

## WebSocket

OKX V5 WebSocket은 채널 종류별 endpoint가 나뉩니다.

| 환경 | public | private | business |
|---|---|---|---|
| 운영 | `wss://ws.okx.com:8443/ws/v5/public` | `wss://ws.okx.com:8443/ws/v5/private` | `wss://ws.okx.com:8443/ws/v5/business` |
| Demo | `wss://wspap.okx.com:8443/ws/v5/public` | `wss://wspap.okx.com:8443/ws/v5/private` | `wss://wspap.okx.com:8443/ws/v5/business` |

- public: ticker, 체결, 호가
- business: 캔들
- private: 계정, 잔고·포지션, 포지션, 주문

지역별 WebSocket 도메인이 필요한 계정은 `PublicStreamURL`, `PrivateStreamURL`, `BusinessStreamURL`을 공식 안내에 맞게 함께 지정해야 합니다.

```go
streams, err := okx.NewStreamClient(okx.StreamClientConfig{
	Connector:             connector,
	Credentials:           descriptor,
	CredentialProvider:    secretProvider,
	DefaultEgressRouteID: "seoul-a",
	DemoTrading:          true,
})
if err != nil {
	return err
}

ticker, err := okx.PublicStreamArgument("tickers", "BTC-USDT")
if err != nil {
	return err
}
public, err := streams.PublicStream(
	okx.PublicStreamRequest{
		Endpoint:  okx.StreamEndpointPublic,
		Arguments: []okx.StreamArgument{ticker},
	},
	trade.WithEgressRoute("seoul-b"),
)
if err != nil {
	return err
}
defer public.Close()

err = public.Run(ctx, func(ctx context.Context, message okx.StreamMessage) error {
	if message.Event != "" || message.Pong {
		return nil
	}
	var tickers []okx.Ticker
	if err := message.DecodeData(&tickers); err != nil {
		return err
	}
	return handleTickers(tickers)
})
```

public typed data는 다음 구조체로 decode할 수 있습니다.

- `tickers`: `[]Ticker`
- `trades`: `[]StreamTrade`
- `books`, `books5`, `bbo-tbt`: `[]OrderBook`
- `candle{bar}`: `[]Candle`

캔들은 `CandleStreamArgument`로 인자를 만들고 `StreamEndpointBusiness` 세션에서 구독해야 합니다. public과 business 인자를 같은 연결에 섞으면 생성 시 거부합니다. 동적 `Subscribe`와 `Unsubscribe`가 성공하면 현재 목록을 갱신하고 재연결 때 같은 endpoint와 송신 경로에서 복구합니다.

## Spot·SWAP 로컬 오더북

`LocalOrderBook`은 public `books`, `books5`, `bbo-tbt` channel을 지원합니다. `books`는 최초 400단계 snapshot 뒤의 증분 update를 적용하고, `books5`와 `bbo-tbt`는 각각 5단계와 1단계의 완전 snapshot으로 매번 장부를 교체합니다.

```go
books, err := okx.PublicStreamArgument("books", "BTC-USDT")
if err != nil {
	return err
}
public, err := streams.PublicStream(
	okx.PublicStreamRequest{
		Endpoint:  okx.StreamEndpointPublic,
		Arguments: []okx.StreamArgument{books},
	},
	trade.WithEgressRoute("seoul-b"),
)
if err != nil {
	return err
}
defer public.Close()

book, err := okx.NewLocalOrderBook(okx.LocalOrderBookConfig{
	Channel:       "books",
	InstrumentID:  "BTC-USDT",
	ViewDepth:     20,
	EgressRouteID: "seoul-b",
})
if err != nil {
	return err
}

err = book.Run(ctx, public, func(ctx context.Context, view okx.LocalOrderBookView) error {
	return handleBook(view)
})
```

`books` update의 `prevSeqId`가 직전 `seqId`와 다르면 장부를 폐기하고 WebSocket을 같은 송신 경로로 다시 연결해 새 snapshot을 받습니다. 약 60초 동안 변경이 없을 때 오는 빈 heartbeat update의 `prevSeqId == seqId`와 정비 중 `seqId`가 감소하는 공식 예외는 정상 처리합니다. 수량 0은 해당 가격 단계를 삭제합니다.

2026년 6월 23일부터 JSON `books` 계열의 `checksum`은 폐지되어 항상 0이므로 SDK는 무결성 판단에 사용하지 않고 `prevSeqId`·`seqId`만 검증합니다. `SynchronizationID`는 적용한 snapshot 횟수, `GapCount`는 재연결을 유발한 sequence 이상 횟수, `Generation`은 WebSocket 연결 세대를 나타냅니다. channel·상품·송신 경로가 public stream과 일치하지 않으면 연결 전에 거부합니다.

private stream은 연결마다 Unix 초 timestamp와 `timestamp + "GET" + "/users/self/verify"` 원문으로 새 서명을 만들어 login합니다.

```go
orders, err := okx.OrderStreamArgument(okx.InstrumentTypeSwap, "BTC-USDT-SWAP")
if err != nil {
	return err
}
positions, err := okx.PositionStreamArgument("BTC-USDT-SWAP")
if err != nil {
	return err
}
private, err := streams.PrivateStream(
	okx.PrivateStreamRequest{
		Arguments: []okx.StreamArgument{
			okx.AccountStreamArgument(),
			okx.BalanceAndPositionStreamArgument(),
			orders,
			positions,
		},
	},
	trade.WithEgressRoute("seoul-b"),
)
if err != nil {
	return err
}
defer private.Close()
```

private typed data는 `[]Balance`, `[]Position`, `[]Order`, `[]StreamBalanceAndPosition`으로 decode할 수 있습니다. 연결이 끊기면 같은 송신 경로에서 최신 Secret과 timestamp로 다시 login한 뒤 현재 channel 목록을 구독합니다. 로그인이 명시적으로 거절되면 같은 키로 무한 재연결하지 않습니다.

OKX는 연결당 login·subscribe·unsubscribe 요청을 합해 시간당 480회로 제한합니다. SDK는 동적 구독 명령을 기본 8초 간격으로 직렬화하고, 한 operation의 인자를 최대 100개씩 분할하며 64KiB를 넘는 요청은 거부합니다. 프로세스 전체의 IP당 연결 시도 제한 `3/초`와 private channel별 sub-account 연결 수는 운영 메트릭에서 별도로 감시해야 합니다.

연결 유지를 위해 기본 20초마다 application 문자열 `ping`을 보내고 10초 안에 `pong`이 없으면 같은 route로 재연결합니다. stream 수명은 `Run`의 context로 제어하므로 생성 시 `trade.WithTimeout`은 허용하지 않습니다.

## 공식 기준 문서

- [OKX V5 API 안내와 인증](https://www.okx.com/docs-v5/en/#overview-rest-authentication)
- [OKX V5 Public Data](https://www.okx.com/docs-v5/en/#public-data-rest-api)
- [OKX V5 Market Data](https://www.okx.com/docs-v5/en/#order-book-trading-market-data)
- [OKX V5 Trading Account](https://www.okx.com/docs-v5/en/#trading-account-rest-api)
- [OKX V5 Place Order](https://www.okx.com/docs-v5/en/#order-book-trading-trade-post-place-order)
- [OKX V5 WebSocket](https://www.okx.com/docs-v5/en/#overview-websocket)
- [OKX V5 Public Channels](https://www.okx.com/docs-v5/en/#order-book-trading-market-data-ws-channel)
- [OKX Order Book checksum 폐지 안내](https://www.okx.com/en-gb/help/okx-order-book-channels-checksum-field-deprecation)
- [OKX V5 Private Channels](https://www.okx.com/docs-v5/en/#order-book-trading-account-websocket)
