package stream

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// pongConnection은 application ping 응답을 Read가 관측해야 Ping이 끝나는 거래소 연결을 흉내 낸다.
// Ping을 보내면 서버가 데이터 메시지 여러 개를 먼저 보내고 그 뒤에 pong을 보낸다.
type pongConnection struct {
	reads     chan Message
	pong      chan struct{}
	closed    chan struct{}
	closeOnce sync.Once
	before    int
	answer    bool
	pings     atomic.Int64
	sequence  atomic.Int64
}

func newPongConnection(before int, answer bool) *pongConnection {
	return &pongConnection{
		reads: make(chan Message, 1024), pong: make(chan struct{}, 1),
		closed: make(chan struct{}), before: before, answer: answer,
	}
}

func (connection *pongConnection) Read(ctx context.Context) (Message, error) {
	select {
	case <-ctx.Done():
		return Message{}, ctx.Err()
	case <-connection.closed:
		return Message{}, ErrSessionClosed
	case message := <-connection.reads:
		if string(message.Data) == "pong" {
			select {
			case connection.pong <- struct{}{}:
			default:
			}
		}
		return message, nil
	}
}

func (connection *pongConnection) Write(context.Context, Message) error { return nil }

func (connection *pongConnection) Ping(ctx context.Context) error {
	connection.pings.Add(1)
	for range connection.before {
		connection.reads <- Message{Type: MessageText, Data: []byte(strconv.FormatInt(connection.sequence.Add(1), 10))}
	}
	if connection.answer {
		connection.reads <- Message{Type: MessageText, Data: []byte("pong")}
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-connection.closed:
		return ErrSessionClosed
	case <-connection.pong:
		return nil
	}
}

func (connection *pongConnection) Close(int, string) error {
	connection.closeOnce.Do(func() { close(connection.closed) })
	return nil
}

type heartbeatConnection struct {
	*pongConnection
}

func (connection heartbeatConnection) IsHeartbeat(message Message) bool {
	return string(message.Data) == "pong"
}

func TestSessionKeepsReadingWhilePingWaitsForPong(t *testing.T) {
	t.Parallel()

	connection := newPongConnection(8, true)
	connector := &scriptedConnector{steps: []connectStep{{connection: connection}}}
	session, err := NewSession(SessionConfig{
		Connector: connector, EgressRouteID: "route-a",
		Request:      DialRequest{Endpoint: "wss://stream.example.test/ws"},
		PingInterval: 5 * time.Millisecond, PingTimeout: 200 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var data int
	err = session.Run(ctx, func(_ context.Context, message Message) error {
		if string(message.Data) != "pong" {
			data++
		}
		if connection.pings.Load() >= 20 {
			cancel()
		}
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}
	if calls, _, _ := connector.snapshot(); calls != 1 || data < 8*19 {
		t.Fatalf("Connect() calls = %d, data messages = %d", calls, data)
	}
}

func TestSessionReconnectsAfterPingTimeout(t *testing.T) {
	t.Parallel()

	silent := newPongConnection(0, false)
	healthy := newFakeConnection(fakeReadResult{message: Message{Type: MessageText, Data: []byte("data")}})
	connector := &scriptedConnector{steps: []connectStep{{connection: silent}, {connection: healthy}}}
	var causes []error
	var mu sync.Mutex
	session, err := NewSession(SessionConfig{
		Connector: connector, EgressRouteID: "route-a",
		Request:      DialRequest{Endpoint: "wss://stream.example.test/ws"},
		PingInterval: 5 * time.Millisecond, PingTimeout: 20 * time.Millisecond,
		Backoff: func(int) time.Duration { return 0 },
		Observer: func(change StateChange) {
			if change.State == StateDisconnected {
				mu.Lock()
				causes = append(causes, change.Cause)
				mu.Unlock()
			}
		},
	})
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err = session.Run(ctx, func(context.Context, Message) error {
		cancel()
		return nil
	})
	if !errors.Is(err, context.Canceled) || session.Generation() != 2 {
		t.Fatalf("Run() error = %v, generation = %d", err, session.Generation())
	}
	mu.Lock()
	defer mu.Unlock()
	if len(causes) != 1 || !errors.Is(causes[0], ErrPingFailed) || !errors.Is(causes[0], context.DeadlineExceeded) {
		t.Fatalf("disconnect causes = %v", causes)
	}
}

func TestSessionIdleTimeoutIgnoresHeartbeatsAndReconnects(t *testing.T) {
	t.Parallel()

	quiet := heartbeatConnection{newPongConnection(0, true)}
	healthy := newFakeConnection(fakeReadResult{message: Message{Type: MessageText, Data: []byte("data")}})
	connector := &scriptedConnector{steps: []connectStep{{connection: quiet}, {connection: healthy}}}
	var cause error
	session, err := NewSession(SessionConfig{
		Connector: connector, EgressRouteID: "route-a",
		Request:      DialRequest{Endpoint: "wss://stream.example.test/ws"},
		PingInterval: 5 * time.Millisecond, PingTimeout: 50 * time.Millisecond,
		IdleTimeout: 60 * time.Millisecond,
		Backoff:     func(int) time.Duration { return 0 },
		Observer: func(change StateChange) {
			if change.State == StateDisconnected && cause == nil {
				cause = change.Cause
			}
		},
	})
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := time.Now()
	err = session.Run(ctx, func(_ context.Context, message Message) error {
		if string(message.Data) == "data" {
			cancel()
		}
		return nil
	})
	if !errors.Is(err, context.Canceled) || session.Generation() != 2 {
		t.Fatalf("Run() error = %v, generation = %d", err, session.Generation())
	}
	if !errors.Is(cause, ErrIdleTimeout) {
		t.Fatalf("disconnect cause = %v, want ErrIdleTimeout", cause)
	}
	if quiet.pings.Load() < 3 || time.Since(started) < 60*time.Millisecond {
		t.Fatalf("idle timeout fired after %s with %d heartbeats", time.Since(started), quiet.pings.Load())
	}
}

func TestSessionIdleTimeoutResetsOnData(t *testing.T) {
	t.Parallel()

	connection := newPongConnection(1, true)
	connector := &scriptedConnector{steps: []connectStep{{connection: connection}}}
	session, err := NewSession(SessionConfig{
		Connector: connector, EgressRouteID: "route-a",
		Request:      DialRequest{Endpoint: "wss://stream.example.test/ws"},
		PingInterval: 5 * time.Millisecond, PingTimeout: 50 * time.Millisecond,
		IdleTimeout: 40 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	err = session.Run(ctx, func(context.Context, Message) error { return nil })
	if !errors.Is(err, context.DeadlineExceeded) || session.Generation() != 1 {
		t.Fatalf("Run() error = %v, generation = %d", err, session.Generation())
	}
	if _, err := NewSession(SessionConfig{
		Connector: connector, EgressRouteID: "route-a",
		Request: DialRequest{Endpoint: "wss://stream.example.test/ws"}, IdleTimeout: -1,
	}); err == nil {
		t.Fatal("NewSession(negative idle timeout) error = nil")
	}
}
