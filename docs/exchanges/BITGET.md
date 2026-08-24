# Bitget v3 UTA 어댑터

## 전제조건

이 어댑터는 Bitget Unified Trading Account의 v3 REST API를 기준으로 합니다. Classic v2 계정용 endpoint는 포함하지 않습니다.

private API를 사용하려면 다음 세 값이 `credential.Provider`의 `credential.Material`에 있어야 합니다.

| 필드 | Bitget 값 |
|---|---|
| `APIKey` | API Key |
| `SecretKey` | HMAC Secret Key |
| `Passphrase` | API Key 생성 시 설정한 Passphrase |

자격증명에 설정한 `AllowedEgressRouteIDs` 밖의 route는 Secret 조회 전에 차단됩니다.

## 상품 범위

| Category | 지원 |
|---|---|
| `SPOT` | 시세, 자산, 주문 |
| `USDT-FUTURES` | 시세, 자산, 포지션, 주문 |
| `MARGIN` | 아직 미지원 |
| `COIN-FUTURES` | 아직 미지원 |
| `USDC-FUTURES` | 아직 미지원 |

## 요청 제한

요청 한 건은 다음 두 제한을 원자적으로 차감합니다.

1. route 단위 전체 제한 `6000/IP/분`
2. endpoint별 문서 제한
   - 공개 시세: `20/IP/초`
   - 조회 private API: `20/UID/초`
   - 주문 생성·취소: `10/UID/초`

각 EIP route는 독립된 IP 제한 bucket을 사용합니다. UID 제한은 route를 변경해도 같은 계정 bucket을 사용하므로 EIP 변경을 UID 제한 우회 수단으로 사용하지 않습니다.

## 서명과 안전한 주문 실패

서명 문자열은 다음 순서로 구성합니다.

```text
timestamp + uppercase(method) + requestPath + optional("?" + sortedQuery) + exactBody
```

최종 문자열에 HMAC SHA-256을 적용하고 결과를 Base64로 인코딩합니다. 서명은 요청 제한 대기가 끝난 뒤 생성되므로 대기 시간 때문에 timestamp가 불필요하게 만료되지 않습니다.

주문 생성·취소의 전송 오류와 Bitget의 `40010`, `40725`, `45001` 응답은 실행 여부가 불명확할 수 있습니다. SDK는 이를 자동 재시도하지 않고 `trade.ErrUnknownExecutionState`로 반환합니다. `clientOid`를 사용해 `OrderInfo` 또는 private order stream으로 최종 상태를 확인해야 합니다.

## Demo Trading

Demo API Key를 사용할 때 클라이언트 설정에 `DemoTrading: true`를 지정합니다. 모든 REST 요청에 `paptrading: 1` 헤더가 추가됩니다.

## WebSocket

v3 UTA WebSocket은 다음 endpoint를 사용합니다.

| 환경 | public | private |
|---|---|---|
| 운영 | `wss://ws.bitget.com/v3/ws/public` | `wss://ws.bitget.com/v3/ws/private` |
| Demo | `wss://wspap.bitget.com/v3/ws/public` | `wss://wspap.bitget.com/v3/ws/private` |

`StreamClient`는 각 연결과 모든 재연결을 생성 시 선택한 EIP route에 고정합니다.

```go
streams, err := bitget.NewStreamClient(bitget.StreamClientConfig{
	Connector:             connector,
	Credentials:           descriptor,
	CredentialProvider:    secretProvider,
	DefaultEgressRouteID: "seoul-a",
})
if err != nil {
	return err
}

ticker, err := bitget.PublicStreamArgument(bitget.CategorySpot, "ticker", "BTCUSDT")
if err != nil {
	return err
}
public, err := streams.PublicStream(
	bitget.StreamRequest{Arguments: []bitget.StreamArgument{ticker}},
	trade.WithEgressRoute("seoul-b"),
)
if err != nil {
	return err
}
defer public.Close()

err = public.Run(ctx, func(ctx context.Context, message bitget.StreamMessage) error {
	if message.Event != "" || message.Pong {
		return nil
	}
	var tickers []bitget.StreamTicker
	if err := message.DecodeData(&tickers); err != nil {
		return err
	}
	return handleTickers(tickers)
})
```

public channel의 1차 typed 범위는 다음과 같습니다.

- `ticker`: `[]StreamTicker`
- `books`, `books1`, `books5`, `books15`: `[]StreamOrderBook`
- `publicTrade`: `[]StreamPublicTrade`
- `kline`: `[]Candle`

`PublicStreamArgument`와 `KlineStreamArgument`가 Spot·USDT Futures 인자를 만듭니다. 동적 `Subscribe`와 `Unsubscribe`에 성공하면 현재 구독 목록을 갱신하고, 연결이 끊어졌을 때 같은 route에서 전체 목록을 다시 구독합니다.

private stream은 API Key, HMAC Secret, Passphrase를 사용합니다.

```go
account, err := bitget.PrivateStreamArgument("account")
if err != nil {
	return err
}
orders, err := bitget.PrivateStreamArgument("order")
if err != nil {
	return err
}
private, err := streams.PrivateStream(
	bitget.StreamRequest{Arguments: []bitget.StreamArgument{account, orders}},
	trade.WithEgressRoute("seoul-b"),
)
if err != nil {
	return err
}
defer private.Close()
```

연결마다 다음 순서로 인증합니다.

1. 자격증명 read 권한과 EIP route 허용 목록 검사
2. Secret Provider에서 API Key, HMAC Secret, Passphrase 조회
3. `timestamp + "GET" + "/user/verify"`를 HMAC SHA-256과 Base64로 서명
4. login 요청 전송 후 `event=login`, `code=0` 응답 확인
5. account, position, order, fill channel 구독
6. 사용한 민감 byte slice 파기

로그인이 명시적으로 거절되면 같은 잘못된 자격증명으로 무한 재연결하지 않습니다. 네트워크 단절이면 같은 route에 새 연결을 만들고 최신 timestamp와 Secret으로 로그인부터 다시 수행합니다.

private typed data는 `[]StreamAccount`, `[]Position`, `[]Order`, `[]StreamFill`로 decode할 수 있습니다. 원본 `Data`도 보존하므로 이후 추가되는 필드는 별도 구조체로 받을 수 있습니다.

### Heartbeat와 연결 제한

Bitget은 WebSocket control ping이 아니라 application 문자열 heartbeat를 요구합니다. SDK는 기본 30초마다 `ping`을 보내고 10초 안에 `pong`이 관측되지 않으면 연결 실패로 처리해 같은 route로 재연결합니다.

- 연결 시도: IP당 5분에 300회
- 동시 연결: IP당 최대 100개
- 구독 요청: 연결당 시간당 240회
- 구독 channel: 연결당 최대 1,000개, 안정성을 위해 50개 미만 권장
- 수신 메시지: 연결당 초당 최대 10개이며 ping, login, subscribe, unsubscribe 포함

SDK의 동적 구독 명령은 기본 15초 간격으로 직렬화해 시간당 240회 제한을 지킵니다. 여러 프로세스와 세션을 합산하는 IP 연결 제한은 운영 메트릭에서 별도로 감시해야 합니다. `Run` context가 세션 수명을 결정하므로 stream 생성 시 `trade.WithTimeout`은 허용하지 않습니다.

## 공식 기준 문서

- [Bitget v3 UTA Quick Start](https://www.bitget.com/api-doc/uta/guide)
- [Bitget v3 UTA Ticker WebSocket](https://www.bitget.com/api-doc/uta/websocket/public/Tickers-Channel)
- [Bitget v3 UTA Account WebSocket](https://www.bitget.com/api-doc/uta/websocket/private/Account-Channel)
- [Bitget v3 UTA Place Order](https://www.bitget.com/api-doc/uta/trade/Place-Order)
- [Bitget v3 UTA Error Code](https://www.bitget.com/api-doc/uta/error-code/restapi)
