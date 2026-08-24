# Gate.io API v4 무기한 Futures REST·WebSocket 어댑터

구현 기준은 Gate.io API v4 Perpetual Futures REST·WebSocket입니다. REST 기본 주소는 `https://api.gateio.ws/api/v4`, WebSocket 기본 주소는 `wss://fx-ws.gateio.ws/v4/ws/{settle}`입니다. Go 패키지는 `exchange/gateio/futures`이며 USDT·BTC·USD1 결제 통화를 명시적으로 선택합니다.

## 전제조건

private API는 Spot과 같은 Gate.io API Key·Secret 및 `model.ExchangeGateIO` 자격증명 설명자를 사용합니다. `credential.Descriptor.AccountID`에는 Futures UID 요청 제한을 공유하는 계정 식별자를 넣고, API Key의 허용 IP에는 사용할 EIP를 모두 등록해야 합니다.

```go
client, err := futures.New(futures.Config{
	Executor: executor,
	Credentials: &credential.Descriptor{
		AccountID: "gateio-main",
		Exchange:  model.ExchangeGateIO,
		SecretRef: "secret/gateio-main",
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

tickers, err := client.Tickers(
	ctx,
	futures.TickersRequest{
		Settlement: futures.SettlementUSDT,
		Contract:   "BTC_USDT",
	},
	trade.WithEgressRoute("seoul-b"),
)
```

Futures private WebSocket은 서명 정보와 별도로 Gate.io 숫자 사용자 ID를 payload에 요구합니다. `StreamClientConfig.UserID`에 실제 Futures UID를 넣고, 내부 제한 키인 `credential.Descriptor.AccountID`와 혼동하지 않아야 합니다.

```go
streamClient, err := futures.NewStreamClient(futures.StreamClientConfig{
	Connector:            connector,
	Credentials:          credentials,
	CredentialProvider:   provider,
	UserID:               "1666",
	DefaultEgressRouteID: "seoul-a",
})
if err != nil {
	return err
}

private, err := streamClient.PrivateStream(
	futures.StreamRequest{
		Settlement: futures.SettlementUSDT,
		Subscriptions: []futures.StreamSubscription{
			{Channel: futures.StreamChannelOrders, Contract: "!all"},
			{Channel: futures.StreamChannelPositions, Contract: "BTC_USDT"},
		},
	},
	trade.WithEgressRoute("seoul-b"),
)
if err != nil {
	return err
}
defer private.Close()
```

## 지원 범위

| 영역 | 메서드 | API |
|---|---|---|
| 계약 목록 | `Contracts` | `GET /futures/{settle}/contracts` |
| 계약 상세 | `Contract` | `GET /futures/{settle}/contracts/{contract}` |
| 거래 통계 | `Tickers` | `GET /futures/{settle}/tickers` |
| 호가 | `OrderBook` | `GET /futures/{settle}/order_book` |
| 공개 체결 | `RecentTrades` | `GET /futures/{settle}/trades` |
| 캔들 | `Candles` | `GET /futures/{settle}/candlesticks` |
| 계정 | `Account` | `GET /futures/{settle}/accounts` |
| 포지션 | `Positions` | `GET /futures/{settle}/positions` |
| 주문 생성 | `PlaceOrder` | `POST /futures/{settle}/orders` |
| 주문 상세 | `OrderInfo` | `GET /futures/{settle}/orders/{order_id}` |
| 주문 취소 | `CancelOrder` | `DELETE /futures/{settle}/orders/{order_id}` |
| 주문 목록 | `Orders` | `GET /futures/{settle}/orders` |
| 계정 체결 | `MyTrades` | `GET /futures/{settle}/my_trades` |

가격·수량·금액·수수료는 문자열로 보존합니다. 주문·체결의 JSON 정수 ID는 `Identifier`가 부동소수점 변환 없이 문자열로 보존하며, double timestamp는 `Decimal`이 원문 십진 표현을 유지합니다. 목록 항목과 단일 객체의 `Raw`에는 해당 원본 JSON을 보존합니다.

## WebSocket 지원 범위

| 구분 | 채널 | SDK 채널 |
|---|---|---|
| public 통계 | `futures.tickers` | `StreamChannelTicker` |
| public 체결 | `futures.trades` | `StreamChannelTrades` |
| public 캔들 | `futures.candlesticks` | `StreamChannelCandles` |
| public 최우선 호가 | `futures.book_ticker` | `StreamChannelBookTicker` |
| public 증분 호가 | `futures.order_book_update` | `StreamChannelOrderBookUpdate` |
| public V2 호가 | `futures.obu` | `StreamChannelOrderBookV2` |
| private 주문 | `futures.orders` | `StreamChannelOrders` |
| private 계정 체결 | `futures.usertrades` | `StreamChannelUserTrades` |
| private 잔고 | `futures.balances` | `StreamChannelBalances` |
| private 포지션 | `futures.positions` | `StreamChannelPositions` |

- `StreamRequest.Settlement`가 BTC·USDT·USD1 endpoint를 결정합니다. 최초 연결과 모든 재연결은 선택한 EIP route와 정산 통화를 그대로 유지합니다.
- 캔들은 10초, 1·5·15·30분, 1·4·8시간, 1·7일을 지원합니다. 계약에 `mark_` 또는 `index_` 접두사를 붙이면 마크가·지수가 캔들을 구독합니다.
- 증분 호가는 20ms·100ms를 지원합니다. 20ms는 Gate.io 규칙에 따라 20단계만 허용하며 100ms의 단계 값은 생략하거나 20·50·100을 지정합니다.
- 주문·계정 체결·포지션은 계약명 대신 `!all`을 지정할 수 있습니다. 잔고 구독은 사용자 ID만 payload로 전송합니다.
- public·private 실행 중 구독 추가·해제를 지원합니다. 서버가 동적 명령을 거절하면 재연결 복구 목록도 이전 상태로 되돌립니다.
- private 구독은 최초 연결, 재연결, 실행 중 변경마다 Secret을 다시 읽고 `channel`, `event`, Unix second를 HMAC-SHA-512로 서명합니다. 자격증명의 route·읽기 권한은 Secret 조회 전에 검사합니다.
- 숫자 또는 문자열로 오는 수량·가격·시각은 `Decimal`로 정밀도 손실 없이 보존합니다. 모든 handshake에 `X-Gate-Size-Decimal: 1`을 넣어 소수 계약 수량을 문자열로 받으며, 이 헤더가 없을 때 발생할 수 있는 정수 내림을 방지합니다. JSON text frame만 해석하며 연결 생존 확인은 WebSocket protocol ping/pong을 사용합니다.

### Futures Order Book V2 로컬 오더북

새 로컬 오더북은 서버가 첫 전체 snapshot을 직접 보내는 `futures.obu`를 사용합니다. 50단계는 20ms, 400단계는 100ms 간격이며 정산 통화별 endpoint와 선택한 EIP route를 재연결에서도 유지합니다.

```go
public, err := streamClient.PublicStream(
	futures.StreamRequest{
		Settlement: futures.SettlementUSDT,
		Subscriptions: []futures.StreamSubscription{{
			Channel:        futures.StreamChannelOrderBookV2,
			Contract:       "BTC_USDT",
			OrderBookDepth: futures.StreamOrderBookDepth50,
		}},
	},
	trade.WithEgressRoute("seoul-b"),
)
if err != nil {
	return err
}
defer public.Close()

orderBook, err := futures.NewLocalOrderBook(futures.LocalOrderBookConfig{
	Settlement:    futures.SettlementUSDT,
	Contract:      "BTC_USDT",
	Depth:         futures.StreamOrderBookDepth50,
	EgressRouteID: "seoul-b",
	ViewDepth:     20,
})
if err != nil {
	return err
}

return orderBook.Run(ctx, public, func(
	_ context.Context,
	view futures.LocalOrderBookView,
) error {
	return consume(view)
})
```

`full=true` snapshot은 기존 장부 전체를 교체하며 같은 세션에서 여러 번 와도 모두 새 동기화 지점으로 적용합니다. 증분 이벤트는 `U == 현재 UpdateID + 1`일 때만 적용하고 ID를 `u`로 전진시킵니다. 빈 `b`·`a` 증분도 ID와 시각을 갱신합니다. 불연속·중복·겹침 이벤트나 새 연결에서 snapshot보다 먼저 온 증분은 기존 장부를 버리고 같은 정산 통화·EIP로 재연결해 새 snapshot부터 복구합니다.

`SynchronizationID`는 적용한 전체 snapshot 횟수, `GapCount`는 복구를 유발한 불연속 횟수, `Generation`은 WebSocket 연결 세대입니다. `ViewDepth` 기본값은 20이고 구독 깊이를 넘을 수 없으며 내부 장부도 50 또는 400단계로 제한합니다. 로컬 오더북과 public stream의 정산 통화·계약·깊이·EIP가 하나라도 다르면 네트워크 연결 전에 거부합니다.

## 주문 계약

- `Settlement`는 `btc`, `usdt`, `usd1` 중 하나를 반드시 선택합니다.
- 계약 수량 `Size`는 매수 양수·매도 음수인 signed decimal 문자열입니다. 계약의 `EnableDecimal`이 false이면 운영 계층에서 정수 수량만 전달해야 합니다.
- 지정가는 양수 `Price`를 요구하며 GTC·IOC·POC·FOK를 지원합니다. 시간 정책을 생략하면 GTC를 적용합니다.
- 시장가는 Gate.io 규칙대로 `price=0`, `tif=ioc`로 전송합니다.
- 단방향 전량 청산은 `Size="0"`, `Close=true`를 사용합니다. 양방향 전량 청산은 `AutoSizeCloseLong` 또는 `AutoSizeCloseShort`와 `ReduceOnly=true`를 사용합니다.
- `ClientOrderID`는 필수이며 `t-` 접두사 뒤 최대 28자의 영문자·숫자·밑줄·하이픈·점만 허용합니다. 상세 조회와 취소는 거래소 주문 ID와 사용자 주문 ID를 모두 받습니다.
- 자기 거래 방지는 미설정, `-`, CO·CN·CB 정책을 지원합니다. 포지션 증거금 방식은 미설정, isolated, cross를 지원합니다.

주문 생성·취소의 전송 오류, 성공 응답 본문 읽기 또는 JSON 해석 실패, 5xx 응답은 실제 처리 여부를 단정하지 않고 `trade.ErrUnknownExecutionState`로 반환합니다. 자동 재시도하지 말고 고유한 `ClientOrderID`로 상태를 확인해야 합니다. 미체결 주문을 취소한 뒤에는 사용자 주문 ID로 조회할 수 있는 시간이 제한되므로 최종 거래소 주문 ID도 보존해야 합니다.

## 인증과 EIP

private 요청 서명은 Spot과 같은 Gate.io API v4 규칙을 사용합니다. HTTP method, `/api/v4` 전체 경로, 실제 정렬 query, SHA-512 본문 해시, Unix second를 줄바꿈으로 결합하고 API Secret으로 HMAC-SHA-512 서명합니다. 요청 제한 대기 뒤 Secret을 조회하고 최종 요청을 서명하며, 반환된 민감 byte slice는 요청 후 가능한 범위에서 덮어씁니다.

| bucket | 기본 제한 | 범위 |
|---|---:|---|
| `gateio:route:<route>:futures-public:<endpoint>:10seconds` | 200회/10초 | 선택한 EIP route와 공개 endpoint |
| `gateio:account:<account>:futures-private:<endpoint>:10seconds` | 200회/10초 | UID와 private endpoint |
| `gateio:account:<account>:futures-order:1second` | 100회/초 | UID의 주문 생성 |
| `gateio:account:<account>:futures-cancel:1second` | 200회/초 | UID의 주문 취소 |

공개 bucket은 요청별 EIP마다 분리되지만 private·주문·취소 bucket은 UID를 기준으로 공유합니다. `X-Gate-RateLimit-*` 응답 헤더가 설정 quota와 일치하면 로컬 제한기의 관측 사용량과 reset 대기를 보정합니다. 계정 등급이나 거래 효율 기반 제한이 더 낮으면 `Config` quota를 보수적으로 재정의해야 합니다.

## 운영 검증

자동 테스트는 REST 공개·private 전체 수명주기와 WebSocket public·private 구독, 서명 원문과 JSON 본문 일치, 요청별 route, 같은 route 재연결, route 허용 목록 사전 차단, Secret 덮어쓰기, 요청 제한 분리, 정확한 ID·timestamp·decimal 해석, 소수 수량 handshake 헤더, V2 snapshot 선도착·update ID 공백 복구, 동적 구독 실패 rollback, 주문 검증과 불명확한 mutation 상태를 검증합니다. 실제 Gate.io Futures 계정과 지정 EIP를 이용한 읽기·주문 smoke는 아직 대기 상태입니다.

## 공식 기준

- [Gate.io API v4 Perpetual Futures](https://www.gate.com/docs/developers/apiv4/en/futures/)
- [Gate.io API v4 Futures WebSocket](https://www.gate.com/docs/developers/futures/)
- [Gate.io API v4 인증·요청 제한](https://www.gate.com/docs/developers/apiv4/en/)
