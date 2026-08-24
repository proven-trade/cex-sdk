// Package stream은 EIP 경로 고정 WebSocket 연결과 재연결 세션을 제공한다.
package stream

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/proven-trade/proven-trade-sdk/transport"
)

var (
	// ErrNotConnected는 현재 사용 가능한 WebSocket 연결이 없음을 나타낸다.
	ErrNotConnected = errors.New("stream is not connected")
	// ErrSessionClosed는 명시적으로 종료한 세션을 다시 사용할 수 없음을 나타낸다.
	ErrSessionClosed = errors.New("stream session is closed")
	// ErrSessionAlreadyRun은 같은 세션의 Run을 두 번 호출했음을 나타낸다.
	ErrSessionAlreadyRun = errors.New("stream session has already run")
)

// MessageType은 WebSocket text 또는 binary frame 종류다.
type MessageType uint8

const (
	MessageText MessageType = iota + 1
	MessageBinary
)

// Message는 WebSocket 메시지 한 건이다.
type Message struct {
	Type MessageType
	Data []byte
}

// Connection은 재연결 세션이 요구하는 WebSocket 연결 계약이다.
// Close는 진행 중인 Read와 Ping을 해제해야 한다.
type Connection interface {
	Read(context.Context) (Message, error)
	Write(context.Context, Message) error
	Ping(context.Context) error
	Close(code int, reason string) error
}

// Connector는 지정한 EIP route로 WebSocket handshake를 수행한다.
type Connector interface {
	Connect(context.Context, transport.EgressRouteID, DialRequest) (Connection, error)
}

// HTTPClientProvider는 route에 바인딩된 HTTP 클라이언트를 반환한다.
type HTTPClientProvider interface {
	HTTPClient(transport.EgressRouteID) (*http.Client, error)
}

// DialRequest는 handshake endpoint와 헤더다.
// Header에는 Secret 원문 대신 거래소가 요구하는 제한된 인증 토큰만 넣어야 한다.
type DialRequest struct {
	Endpoint string
	Header   http.Header
}

// DialRequestSource는 매 재연결마다 최신 endpoint와 인증 헤더를 만든다.
type DialRequestSource func(context.Context) (DialRequest, error)

// ConnectHook은 최초 연결과 재연결 직후 구독 또는 인증 메시지를 전송한다.
type ConnectHook func(context.Context, Connection) error

// MessageHandler는 수신 메시지를 순서대로 처리한다.
type MessageHandler func(context.Context, Message) error

// ReconnectPolicy는 연결 오류 후 재연결 여부를 결정한다.
type ReconnectPolicy func(error) bool

// Backoff는 연속 재연결 실패 횟수에 따른 대기 시간을 반환한다.
type Backoff func(attempt int) time.Duration

// State는 WebSocket 세션의 연결 상태다.
type State string

const (
	StateConnecting   State = "connecting"
	StateConnected    State = "connected"
	StateDisconnected State = "disconnected"
	StateClosed       State = "closed"
)

// StateChange는 관측 가능한 세션 상태 전환이다.
type StateChange struct {
	State      State
	RouteID    transport.EgressRouteID
	Generation uint64
	Attempt    int
	Cause      error
}

// StateObserver는 연결 상태 전환을 동기적으로 전달받는다.
type StateObserver func(StateChange)
