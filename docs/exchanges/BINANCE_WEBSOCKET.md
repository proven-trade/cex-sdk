# Binance Spot WebSocket 1차 구현

## 기준

- public market stream: `wss://stream.binance.com:9443/stream`
- private user data stream: `wss://ws-api.binance.com:443/ws-api/v3`
- public 형식: JSON combined stream
- private 인증: HMAC API Key의 `userDataStream.subscribe.signature`

공식 기준 문서는 [Binance WebSocket Streams](https://developers.binance.com/docs/binance-spot-api-docs/web-socket-streams), [WebSocket API](https://developers.binance.com/docs/binance-spot-api-docs/websocket-api/general-api-information), [User Data Stream](https://developers.binance.com/docs/binance-spot-api-docs/user-data-stream)입니다.

## public stream

`StreamClient.MarketStream`은 하나의 연결에서 최대 1,024개 stream을 관리합니다. 연결은 생성할 때 선택한 EIP route에 고정되고, 끊어지면 같은 route로 재연결한 뒤 현재 구독 목록을 다시 전송합니다.

```go
tradeStream, err := binance.SymbolMarketStream("BTCUSDT", "aggTrade")
if err != nil {
	return err
}
klineStream, err := binance.KlineMarketStream("BTCUSDT", binance.Kline1Minute)
if err != nil {
	return err
}

market, err := streams.MarketStream(
	binance.MarketStreamRequest{
		Streams:  []string{tradeStream, klineStream},
		TimeUnit: binance.StreamTimeMicroseconds,
	},
	trade.WithEgressRoute("seoul-b"),
)
if err != nil {
	return err
}
defer market.Close()

err = market.Run(ctx, func(ctx context.Context, message binance.MarketStreamMessage) error {
	if message.Response != nil {
		return handleControlResponse(message.Response)
	}
	switch message.EventType {
	case "aggTrade":
		var event binance.AggregateTradeEvent
		if err := message.Decode(&event); err != nil {
			return err
		}
		return handleAggregateTrade(event)
	case "kline":
		var event binance.KlineEvent
		if err := message.Decode(&event); err != nil {
			return err
		}
		return handleKline(event)
	default:
		return nil
	}
})
```

다음 helper를 제공합니다.

- `SymbolMarketStream`: aggregate trade, trade, mini ticker, ticker, book ticker, average price, diff depth
- `KlineMarketStream`: Binance가 지원하는 캔들 간격
- `PartialDepthMarketStream`: 5·10·20단계, 기본 또는 100ms 갱신

그 밖의 all-market stream과 새로 추가된 native stream은 검증된 문자열을 `MarketStreamRequest.Streams`에 직접 넣을 수 있습니다. symbol 부분은 Binance 규칙에 따라 소문자여야 하며 helper가 이를 자동 변환합니다.

연결 중 `Subscribe`와 `Unsubscribe`를 호출하면 성공적으로 보낸 뒤 재연결 구독 목록에도 반영됩니다. Binance가 ping, pong, JSON 제어 메시지를 합쳐 초당 5개로 제한하므로 SDK는 JSON 구독 명령을 250ms 간격으로 직렬화합니다. 서버 ping에 대한 pong 처리는 공통 WebSocket 구현이 담당합니다.

## private user data stream

현재 Binance Spot 문서의 WebSocket API 서명 구독을 사용합니다. REST listen key를 생성하거나 갱신하지 않습니다.

```go
streams, err := binance.NewStreamClient(binance.StreamClientConfig{
	Connector:             connector,
	Credentials:           descriptor,
	CredentialProvider:    secretProvider,
	DefaultEgressRouteID: "seoul-a",
})
if err != nil {
	return err
}

userData, err := streams.UserDataStream(trade.WithEgressRoute("seoul-b"))
if err != nil {
	return err
}
defer userData.Close()

err = userData.Run(ctx, func(ctx context.Context, message binance.UserDataStreamMessage) error {
	if message.Response != nil {
		if message.Response.Error != nil {
			return fmt.Errorf("Binance subscription rejected: %s", message.Response.Error.Message)
		}
		return nil
	}
	if message.EventType != "executionReport" {
		return nil
	}
	var event binance.ExecutionReportEvent
	if err := message.Decode(&event); err != nil {
		return err
	}
	return handleExecution(event)
})
```

세션 생성 시 자격증명의 read 권한과 route 허용 목록을 Secret 조회보다 먼저 검사합니다. 실제 연결 직후에는 다음 순서로 처리합니다.

1. Secret Provider에서 API Key와 HMAC Secret 조회
2. 현재 timestamp와 receive window 생성
3. 정렬된 파라미터를 HMAC SHA-256으로 서명
4. `userDataStream.subscribe.signature` 전송
5. 사용한 자격증명 byte slice 파기

재연결할 때 이 과정을 모두 다시 실행하므로 오래된 timestamp나 서명을 재사용하지 않습니다. 현재 typed event는 `outboundAccountPosition`, `balanceUpdate`, `executionReport`, `externalLockUpdate`를 제공합니다. 그 밖의 이벤트도 `Payload`와 `Raw`에서 손실 없이 decode할 수 있습니다.

## 응답과 오류 처리

public 구독 응답과 private WebSocket API 응답은 이벤트와 섞여 수신됩니다. handler는 먼저 `Response`가 nil인지 확인해야 합니다.

- public 성공: `Result`가 JSON `null`
- public 실패: `Response.Error.Code`와 `Message`
- private 성공: `Status == 200`, `Result`에 `subscriptionId`
- private 실패: HTTP 유사 `Status`와 `Response.Error`

서버가 요청을 거절한 응답은 네트워크 단절이 아니므로 공통 계층이 임의로 재연결하지 않습니다. 애플리케이션 handler가 오류를 반환해 세션을 종료하고 설정 또는 자격증명을 수정해야 합니다.

## 연결 수명과 제한

- Binance 연결은 최대 24시간이므로 정상 운영에서도 재연결이 발생합니다.
- `serverShutdown` 이벤트가 오면 새 연결 준비 상태를 관측해야 합니다.
- public 연결 하나당 최대 1,024개 stream입니다.
- IP당 5분 동안 300회 연결 시도 제한은 여러 프로세스와 세션을 합산해 운영에서 감시해야 합니다.
- private WebSocket API 연결은 한 계정당 하나의 active subscription만 생성합니다.
- `Run` context가 세션 전체 수명을 결정하며 `trade.WithTimeout`은 허용하지 않습니다.

## 로컬 오더북과 자동 갭 복구

`LocalOrderBook`은 공식 Binance 절차에 따라 diff depth 이벤트를 먼저 버퍼링한 뒤 같은 EIP route로 REST depth snapshot을 조회합니다. 최초 적용 이벤트가 `lastUpdateId + 1`을 포함해야 동기화를 완료하며, 이후 update ID가 끊기거나 WebSocket 연결 세대가 바뀌면 새 snapshot으로 자동 재동기화합니다.

```go
depthStream, err := binance.SymbolMarketStream("BTCUSDT", "depth")
if err != nil {
	return err
}
market, err := streams.MarketStream(
	binance.MarketStreamRequest{Streams: []string{depthStream}},
	trade.WithEgressRoute("seoul-b"),
)
if err != nil {
	return err
}
defer market.Close()

book, err := binance.NewLocalOrderBook(binance.LocalOrderBookConfig{
	RESTClient:    restClient,
	Symbol:        "BTCUSDT",
	EgressRouteID: "seoul-b",
	ViewDepth:     20,
})
if err != nil {
	return err
}

err = book.Run(ctx, market, func(ctx context.Context, view binance.LocalOrderBookView) error {
	return consumeBook(view)
})
```

운영 계약은 다음과 같습니다.

- REST snapshot과 WebSocket은 반드시 같은 `EgressRouteID`를 사용하며 다르면 연결 전에 거부합니다.
- 전체 diff depth인 `<symbol>@depth` 또는 `<symbol>@depth@100ms`만 허용하며 partial depth stream은 거부합니다.
- snapshot 기본 limit은 Binance 최대치인 5,000, 사용자에게 전달하는 정렬된 상위 호가는 기본 20단계입니다.
- snapshot보다 오래된 이벤트는 버리고, 최초 연결 이벤트가 snapshot 다음 update ID를 포함하지 못하면 snapshot을 다시 조회합니다.
- 동기화 뒤 `U > localUpdateId + 1`이면 gap으로 판단하고 해당 이벤트를 보존한 채 snapshot부터 다시 맞춥니다.
- 재연결 세대가 바뀌면 이전 세대의 로컬 상태를 폐기하고 같은 EIP로 재동기화합니다.
- 동기화 중 버퍼가 `MaxBufferedEvents`를 넘으면 `ErrDepthBufferOverflow`로 종료해 손상된 장부를 노출하지 않습니다.
- `SynchronizationID`, `Generation`, `GapCount`, `LastUpdateID`로 재동기화와 데이터 연속성을 관측할 수 있습니다.

Binance snapshot은 각 방향 최대 5,000단계만 제공하므로 snapshot에 없고 이후에도 변경되지 않은 더 먼 가격 단계는 로컬 장부에 존재하지 않을 수 있습니다.
