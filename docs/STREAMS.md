# 공통 WebSocket 연결 계층

## 목적

`stream` 패키지는 거래소별 구독 형식과 메시지 모델을 구현하기 전에 필요한 공통 연결 수명주기를 제공합니다.

- WebSocket 연결 전체를 하나의 `egressRouteId`에 고정
- 연결 단절 시 같은 route로 재연결
- 재연결마다 임시 endpoint 또는 인증 헤더 갱신
- 연결 직후 인증·구독 메시지 재전송
- 연결 상태와 세대 번호 관측
- 선택적 ping과 읽기 크기 제한

이미 수립된 WebSocket 연결의 송신 원본 IP는 바꿀 수 없습니다. 다른 송신 경로를 사용하려면 기존 세션을 종료하고 새 route로 새 세션을 생성해야 합니다.

## 연결 생성

REST에서 사용하는 `transport.Registry`가 WebSocket handshake에도 같은 local source IP 바인딩을 제공합니다.

```go
registry, err := transport.NewRegistry([]transport.EgressRoute{
	{
		ID:               "seoul-a",
		LocalSourceIP:    net.ParseIP("10.0.10.21"),
		ExpectedPublicIP: net.ParseIP("203.0.113.10"),
	},
})
if err != nil {
	return err
}
defer registry.Close()

connector, err := stream.NewWebSocketConnector(stream.ConnectorConfig{
	HTTPClients: registry,
	ReadLimit:   4 << 20,
})
if err != nil {
	return err
}
```

`Registry.HTTPClient`는 지정 route의 전용 transport를 사용하고 HTTP redirect를 따르지 않습니다. 따라서 handshake 중 인증 헤더가 다른 origin으로 전달되지 않으며, registry를 닫은 뒤에는 기존에 발급한 클라이언트도 새 연결을 만들 수 없습니다.

## 재연결 세션

```go
session, err := stream.NewSession(stream.SessionConfig{
	Connector:     connector,
	EgressRouteID: "seoul-a",
	Request: stream.DialRequest{
		Endpoint: "wss://stream.example.com/ws",
	},
	OnConnect: func(ctx context.Context, connection stream.Connection) error {
		return connection.Write(ctx, stream.Message{
			Type: stream.MessageText,
			Data: []byte(`{"op":"subscribe","channel":"ticker"}`),
		})
	},
	PingInterval: 20 * time.Second,
})
if err != nil {
	return err
}
defer session.Close()

err = session.Run(ctx, func(ctx context.Context, message stream.Message) error {
	// 거래소별 decoder에서 메시지를 검증하고 변환합니다.
	return handleMessage(message.Data)
})
```

`Run`은 연결이 끝날 때까지 블로킹하며 한 세션에서 한 번만 호출할 수 있습니다. handler는 메시지 순서를 보존하기 위해 직렬로 실행됩니다. 처리 시간이 긴 작업은 애플리케이션이 크기가 제한된 큐와 자체 backpressure 정책을 사용해야 합니다. handler가 오류를 반환하면 데이터 처리 실패로 간주하고 자동 재연결하지 않습니다.

## 임시 인증 정보 갱신

거래소가 private stream용 임시 endpoint, listen key 또는 토큰을 발급하는 경우 정적 `Request` 대신 `RequestSource`를 사용합니다.

```go
RequestSource: func(ctx context.Context) (stream.DialRequest, error) {
	endpoint, token, err := issuePrivateStreamToken(ctx)
	if err != nil {
		return stream.DialRequest{}, err
	}
	return stream.DialRequest{
		Endpoint: endpoint,
		Header: http.Header{
			"Authorization": {"Bearer " + token},
		},
	}, nil
},
```

`RequestSource`와 `OnConnect`는 최초 연결뿐 아니라 모든 재연결에서 다시 실행됩니다. 자격증명 route 허용 검사는 토큰 발급 REST 호출 전에 거래소 어댑터가 수행해야 하며, 토큰을 오류나 상태 이벤트에 넣으면 안 됩니다.

## 재연결 정책

기본 정책은 네트워크 오류와 서버 오류를 재시도하고, 영구적인 HTTP 4xx handshake 오류는 재시도하지 않습니다. 예외적으로 일시적일 수 있는 `408`, `418`, `429`는 재시도합니다.

- 기본 backoff: `250ms`에서 시작해 `30s`까지 증가
- `Retry-After`: 숫자 초 또는 HTTP 날짜 형식을 인식하고 backoff보다 길면 우선 적용
- `MaxReconnectAttempts`: `0`이면 제한 없음, 양수이면 연속 실패 횟수 제한
- 성공적으로 연결되면 실패 횟수 초기화
- `context` 취소 또는 `Session.Close` 호출 시 재연결 중단

운영 환경에서 jitter 또는 거래소별 정책이 필요하면 `Backoff`와 `ReconnectPolicy`를 주입합니다.

## 상태 관측

`Observer`는 `connecting`, `connected`, `disconnected`, `closed` 상태를 동기적으로 전달합니다. `Generation`은 성공한 연결마다 증가하므로 재구독 횟수와 메시지 세대를 구분하는 데 사용할 수 있습니다. 관측 콜백은 연결 루프를 막지 않도록 빠르게 끝나야 합니다.

handshake 실패 오류에는 HTTP 상태, `Retry-After`, `X-Request-ID`만 보존합니다. endpoint query, 요청 헤더, 응답 본문은 오류에 포함하지 않습니다.

## 거래소 어댑터의 책임

공통 계층은 바이트 메시지와 연결 수명주기만 담당합니다. 각 거래소 WebSocket 어댑터는 다음을 구현해야 합니다.

- public/private endpoint와 인증 방식
- 구독·구독 해제 메시지 및 요청 ID
- ping/pong 또는 애플리케이션 heartbeat 규칙
- 이벤트 타입과 공통 모델 변환
- 거래소 sequence 검사와 gap 복구
- 구독 수·연결 수·메시지 제한
- 자격증명과 route 허용 관계 검증

공통 계층의 자동 재연결만으로 로컬 오더북 정합성이 복구되지는 않습니다. sequence gap이 발생하면 해당 거래소 절차에 따라 REST snapshot과 buffered delta를 다시 결합하거나, 새 WebSocket snapshot부터 장부를 교체해야 합니다.
