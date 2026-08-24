# Gate.io API v4 Spot REST·WebSocket 어댑터

구현 기준은 Gate.io API v4 Spot REST·JSON WebSocket과 기본 주소 `https://api.gateio.ws/api/v4`, `wss://api.gateio.ws/ws/v4/`입니다. 공통 Spot API와 Futures는 후속 단계에서 추가합니다.

## 전제조건

private API를 사용하려면 `credential.Provider`가 반환하는 `credential.Material`에 다음 값을 넣어야 합니다.

| 필드 | Gate.io 값 |
|---|---|
| `APIKey` | API Key |
| `SecretKey` | API Secret 원문 |

`credential.Descriptor.AccountID`에는 Gate.io UID 요청 제한을 공유하는 계정의 안정적인 식별자를 넣어야 합니다. 자격증명의 `AllowedEgressRouteIDs` 밖에 있는 route는 Secret 조회 전에 차단됩니다. API Key의 IP 허용 목록에는 허용할 route에 연결된 EIP를 모두 등록해야 합니다.

```go
client, err := gateio.New(gateio.Config{
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

ticker, err := client.Ticker(ctx, "BTC_USDT", trade.WithEgressRoute("seoul-b"))
```

## 지원 범위

| 영역 | 메서드 | API |
|---|---|---|
| 거래쌍 규칙 | `CurrencyPairs` | `GET /spot/currency_pairs` |
| 현재가 | `Ticker` | `GET /spot/tickers` |
| 호가 | `OrderBook` | `GET /spot/order_book` |
| 공개 체결 | `RecentTrades` | `GET /spot/trades` |
| 캔들 | `Candles` | `GET /spot/candlesticks` |
| 계정 | `Accounts` | `GET /spot/accounts` |
| 주문 생성 | `PlaceOrder` | `POST /spot/orders` |
| 주문 상세 | `OrderInfo` | `GET /spot/orders/{order_id}` |
| 주문 취소 | `CancelOrder` | `DELETE /spot/orders/{order_id}` |
| 전체 미체결 | `OpenOrders` | `GET /spot/open_orders` |
| 내 체결 | `MyTrades` | `GET /spot/my_trades` |

가격, 수량, 금액, 수수료는 `float64`로 변환하지 않고 문자열로 보존합니다. 객체와 배열 항목의 `Raw`에는 해당 원본 JSON을 보존합니다. 캔들은 `timestamp, quote volume, close, high, low, open, base volume, closed` 순서로 해석하며, 구형 7개 필드 응답은 `BaseVolume`을 빈 문자열로 둡니다.

## 인증과 서명

private 요청은 요청 제한 대기가 끝난 뒤 자격증명을 조회하고 최종 요청을 서명합니다.

1. 본문 bytes의 SHA-512 해시를 소문자 16진수로 계산합니다.
2. `대문자 HTTP method + 줄바꿈 + /api/v4 경로 + 줄바꿈 + query + 줄바꿈 + body hash + 줄바꿈 + Unix second`를 서명 원문으로 만듭니다.
3. API Secret으로 HMAC-SHA-512 서명하고 소문자 16진수로 변환합니다.
4. `KEY`, `Timestamp`, `SIGN` 헤더를 설정합니다.
5. POST는 서명한 것과 정확히 같은 JSON bytes를 전송하고 GET·DELETE는 본문 없이 실제 URL과 같은 query 순서를 서명합니다.

SDK가 지원하는 거래쌍, 통화, 주문 ID와 숫자 query는 URL 인코딩 전후가 달라지지 않는 문자만 허용합니다. 임의 문자를 받는 endpoint를 추가할 때는 Gate.io 규칙에 맞게 URL 인코딩 전 query 원문과 실제 URL의 파라미터 순서를 별도로 보존해야 합니다.

Provider가 반환한 API Key와 Secret byte slice는 요청 뒤 가능한 범위에서 덮어씁니다. Go 문자열과 HTTP 계층 내부 복사본까지 완전히 지울 수 있다는 보장은 하지 않습니다.

## WebSocket

`StreamClient`는 public과 private이 같은 JSON endpoint를 사용하되, private 구독 명령마다 현재 Unix second와 API Key로 새 서명을 만듭니다.

| 구분 | `StreamChannel` | Gate.io 채널 |
|---|---|---|
| public 현재가 | `StreamChannelTicker` | `spot.tickers` |
| public 체결 | `StreamChannelTrades` | `spot.trades` |
| public 캔들 | `StreamChannelCandles` | `spot.candlesticks` |
| public 최우선 호가 | `StreamChannelBookTicker` | `spot.book_ticker` |
| public 증분 호가 | `StreamChannelOrderBookUpdate` | `spot.order_book_update` |
| private 주문 | `StreamChannelOrders` | `spot.orders` |
| private 내 체결 | `StreamChannelUserTrades` | `spot.usertrades` |
| private 잔고 | `StreamChannelBalances` | `spot.balances` |

```go
streamClient, err := gateio.NewStreamClient(gateio.StreamClientConfig{
	Connector:            connector,
	Credentials:          descriptor,
	CredentialProvider:   provider,
	DefaultEgressRouteID: "seoul-a",
})
if err != nil {
	return err
}

public, err := streamClient.PublicStream(
	gateio.StreamRequest{Subscriptions: []gateio.StreamSubscription{
		{Channel: gateio.StreamChannelTicker, CurrencyPair: "BTC_USDT"},
		{
			Channel:        gateio.StreamChannelOrderBookUpdate,
			CurrencyPair:   "BTC_USDT",
			UpdateInterval: gateio.StreamUpdate100Millis,
		},
	}},
	trade.WithEgressRoute("seoul-b"),
)
if err != nil {
	return err
}
defer public.Close()

return public.Run(ctx, func(_ context.Context, message gateio.StreamMessage) error {
	if message.Channel != gateio.StreamChannelTicker || message.Event != "update" {
		return nil
	}
	var ticker gateio.StreamTicker
	return message.Decode(&ticker)
})
```

public 캔들은 REST와 달리 10초부터 7일까지의 공식 구간만 허용합니다. 증분 호가는 20ms 또는 100ms 통지 주기를 명시해야 합니다. private 주문·내 체결은 안전한 복구 범위를 분명히 하기 위해 거래쌍을 요구하며, 잔고 채널은 거래쌍을 받지 않습니다.

private 서명 원문은 `channel=<channel>&event=<subscribe 또는 unsubscribe>&time=<Unix second>`이고 HMAC-SHA-512 소문자 hex를 `auth.SIGN`에 넣습니다. API Key는 `auth.KEY`, 인증 방식은 `api_key`입니다. 구독 시각과 서버 시각 차이는 60초 이하여야 하므로 인스턴스의 시각 동기화가 필요합니다.

연결은 선택한 EIP route에 수명주기 동안 고정됩니다. 재연결하면 같은 route로 새 handshake를 수행하고 현재 구독을 새 시각으로 다시 서명해 복구합니다. 실행 중 `Subscribe`와 `Unsubscribe`를 사용할 수 있으며, 서버가 오류 응답을 보낸 변경은 로컬 복구 목록에서 되돌립니다. 기본 heartbeat는 30초마다 WebSocket protocol ping을 보내고 10초 안에 pong을 기다립니다. Gate.io의 연결 제한은 IP당 300개이며 여러 프로세스·클라이언트의 합산 연결 수는 SDK 밖에서도 관리해야 합니다.

`StreamChannelOrderBookUpdate`의 수량은 증감량이 아니라 해당 가격의 절대 수량이며 0이면 가격 단계를 삭제해야 합니다. 연결 뒤 이벤트를 임시 저장하고 `OrderBook`에서 `with_id=true` snapshot을 받은 다음 `U <= snapshotID+1 <= u`인 첫 이벤트부터 적용합니다. 이후에는 직전 `u+1`이 다음 범위에 포함되는지 확인하고 공백이 생기거나 재연결되면 기존 로컬 호가장을 버리고 다시 snapshot을 받아야 합니다.

JSON endpoint만 지원하며 Gate.io SBE binary push는 현재 범위에 포함하지 않습니다. public 이벤트 유실과 private 재연결 구간은 REST 조회로 최종 상태를 재조정해야 합니다. 시스템의 `spot.system` upgrade 알림을 받으면 연결 종료를 기다리지 말고 운영 계층에서 새 세션으로 교체하는 것이 안전합니다.

## 요청 제한과 EIP

기본 로컬 quota는 현재 공식 기본 제한을 따릅니다.

| bucket | 기본 제한 | 범위 |
|---|---:|---|
| `gateio:route:<route>:public:<endpoint>:10seconds` | 200회/10초 | 선택한 EIP route와 공개 endpoint |
| `gateio:account:<account>:private:<endpoint>:10seconds` | 200회/10초 | UID와 private endpoint |
| `gateio:account:<account>:spot-order:<market>:1second` | 10회/초 | UID와 거래쌍의 주문 생성 |
| `gateio:account:<account>:spot-cancel:1second` | 200회/초 | UID의 주문 취소 |

`X-Gate-RateLimit-Limit`과 `X-Gate-RateLimit-Requests-Remain`이 로컬 설정과 일치하면 관측 사용량을 반영합니다. remaining이 0이고 `X-Gate-RateLimit-Reset-Timestamp`가 미래 시각이면 해당 bucket을 reset까지 막습니다. 계정 등급이나 운영 정책이 다르면 `Config`의 `PublicQuota`, `PrivateQuota`, `OrderQuota`, `CancelQuota`를 더 보수적인 값으로 재정의할 수 있습니다.

Public endpoint는 IP와 endpoint 기준이므로 요청별 EIP가 각각 독립된 bucket을 사용합니다. Private, 주문, 취소 제한은 UID 기준이므로 EIP를 바꿔도 quota가 늘어나지 않습니다. 다중 EIP는 공개 처리량 분산, API Key IP 허용 목록, 장애 격리를 위한 기능이며 거래소 제한 우회 용도가 아닙니다.

## 주문 안전 계약

- 현재 native 어댑터는 순수 `spot` 계정의 지정가와 시장가만 허용합니다.
- `ClientOrderID`는 필수이며 `t-` 접두사 뒤에 최대 28자의 영문자, 숫자, 밑줄, 하이픈, 점만 허용합니다.
- 지정가는 가격과 수량을 요구하며 GTC·IOC·POC·FOK를 지원합니다.
- 시장가는 가격을 받지 않으며 IOC·FOK를 지원합니다.
- 시장가 매수의 `Amount`는 견적 통화 금액이고 시장가 매도의 `Amount`는 기준 통화 수량입니다.
- SDK는 자동 반올림하지 않습니다. `CurrencyPairs`의 정밀도와 최소·최대 주문 값을 적용한 문자열을 전달해야 합니다.
- 주문 상세와 취소는 주문 ID와 거래쌍을 함께 요구합니다.

주문 생성·취소의 전송 오류, 응답 본문 읽기 실패, 성공 응답 JSON 파싱 실패, 5xx 오류는 실제 처리 여부를 단정할 수 없습니다. SDK는 자동 재시도하지 않고 `trade.ErrUnknownExecutionState`로 반환합니다. 불명확한 주문 생성 결과는 고유한 `ClientOrderID`로 상태를 확인한 뒤 처리해야 합니다.

## 오류 처리

Gate.io의 비정상 응답은 일반적으로 비-2xx 상태와 `label`, `message`, 선택적인 `detail` JSON을 사용합니다. SDK는 HTTP 상태와 label을 함께 검사해 인증·서명, 권한·IP 허용 목록, 잔고 부족, 주문 없음, 요청 제한, 거래소 장애를 공통 `trade.APIError`로 변환합니다. 원본 label과 message, 요청 ID를 보존하되 인증 헤더와 서명 원문은 오류에 포함하지 않습니다.

## 운영 검증

자동 테스트는 REST 서명 원문·본문·query 일치, 요청별 route 선택, route 허용 목록 사전 검사, Secret 덮어쓰기, 요청 제한 분리, 오류 분류, mutation 불명확 상태를 검증합니다. WebSocket은 public 재연결·재구독, private 명령 서명, typed event decode, 동적 구독 실패 rollback을 검증합니다. 실제 Gate.io 계정과 지정 EIP를 이용한 읽기·주문·장시간 stream smoke는 아직 대기 상태입니다.

## 공식 기준

- [Gate.io API v4 개요·인증·요청 제한](https://www.gate.com/docs/developers/apiv4/en/)
- [Gate.io API v4 Spot](https://www.gate.com/docs/developers/apiv4/en/spot/)
- [Gate.io API v4 Spot WebSocket](https://www.gate.com/docs/developers/apiv4/ws/en/)
- [Gate.io API v4 반환 label](https://www.gate.com/docs/developers/apiv4/en/#label-list)
