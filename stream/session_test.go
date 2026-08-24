package stream

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/proven-trade/proven-trade-sdk/transport"
)

type fakeReadResult struct {
	message Message
	err     error
}

type fakeConnection struct {
	reads     chan fakeReadResult
	closed    chan struct{}
	closeOnce sync.Once
	mu        sync.Mutex
	writes    []Message
	pings     int
}

func newFakeConnection(results ...fakeReadResult) *fakeConnection {
	connection := &fakeConnection{
		reads:  make(chan fakeReadResult, len(results)),
		closed: make(chan struct{}),
	}
	for _, result := range results {
		connection.reads <- result
	}
	return connection
}

func (connection *fakeConnection) Read(ctx context.Context) (Message, error) {
	select {
	case <-ctx.Done():
		return Message{}, ctx.Err()
	case <-connection.closed:
		return Message{}, ErrSessionClosed
	case result := <-connection.reads:
		return result.message, result.err
	}
}

func (connection *fakeConnection) Write(ctx context.Context, message Message) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-connection.closed:
		return ErrSessionClosed
	default:
	}
	message.Data = append([]byte(nil), message.Data...)
	connection.mu.Lock()
	connection.writes = append(connection.writes, message)
	connection.mu.Unlock()
	return nil
}

func (connection *fakeConnection) Ping(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-connection.closed:
		return ErrSessionClosed
	default:
	}
	connection.mu.Lock()
	connection.pings++
	connection.mu.Unlock()
	return nil
}

func (connection *fakeConnection) Close(int, string) error {
	connection.closeOnce.Do(func() { close(connection.closed) })
	return nil
}

func (connection *fakeConnection) writtenMessages() []Message {
	connection.mu.Lock()
	defer connection.mu.Unlock()
	messages := make([]Message, len(connection.writes))
	for index, message := range connection.writes {
		messages[index] = Message{Type: message.Type, Data: append([]byte(nil), message.Data...)}
	}
	return messages
}

type connectStep struct {
	connection Connection
	err        error
}

type scriptedConnector struct {
	mu       sync.Mutex
	steps    []connectStep
	calls    int
	routes   []transport.EgressRouteID
	requests []DialRequest
}

func (connector *scriptedConnector) Connect(
	_ context.Context,
	routeID transport.EgressRouteID,
	request DialRequest,
) (Connection, error) {
	connector.mu.Lock()
	defer connector.mu.Unlock()
	connector.calls++
	connector.routes = append(connector.routes, routeID)
	connector.requests = append(connector.requests, DialRequest{
		Endpoint: request.Endpoint,
		Header:   request.Header.Clone(),
	})
	if len(connector.steps) == 0 {
		return nil, errors.New("unexpected connection attempt")
	}
	step := connector.steps[0]
	connector.steps = connector.steps[1:]
	return step.connection, step.err
}

func (connector *scriptedConnector) snapshot() (int, []transport.EgressRouteID, []DialRequest) {
	connector.mu.Lock()
	defer connector.mu.Unlock()
	routes := append([]transport.EgressRouteID(nil), connector.routes...)
	requests := make([]DialRequest, len(connector.requests))
	for index, request := range connector.requests {
		requests[index] = DialRequest{Endpoint: request.Endpoint, Header: request.Header.Clone()}
	}
	return connector.calls, routes, requests
}

func TestSessionReconnectsOnSameRouteAndRunsConnectHookAgain(t *testing.T) {
	t.Parallel()

	lost := errors.New("connection lost")
	first := newFakeConnection(
		fakeReadResult{message: Message{Type: MessageText, Data: []byte("first")}},
		fakeReadResult{err: lost},
	)
	second := newFakeConnection(
		fakeReadResult{message: Message{Type: MessageText, Data: []byte("second")}},
	)
	connector := &scriptedConnector{steps: []connectStep{
		{connection: first},
		{connection: second},
	}}

	var sourceMu sync.Mutex
	sourceCalls := 0
	requestSource := func(context.Context) (DialRequest, error) {
		sourceMu.Lock()
		defer sourceMu.Unlock()
		sourceCalls++
		return DialRequest{
			Endpoint: "wss://stream.example.test/ws",
			Header:   http.Header{"X-Session-Token": {string(rune('0' + sourceCalls))}},
		}, nil
	}
	var states []StateChange
	session, err := NewSession(SessionConfig{
		Connector:     connector,
		EgressRouteID: "route-b",
		RequestSource: requestSource,
		OnConnect: func(ctx context.Context, connection Connection) error {
			return connection.Write(ctx, Message{Type: MessageText, Data: []byte("subscribe")})
		},
		Observer: func(change StateChange) {
			states = append(states, change)
		},
		Backoff: func(int) time.Duration { return 0 },
	})
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var messages []string
	err = session.Run(ctx, func(_ context.Context, message Message) error {
		messages = append(messages, string(message.Data))
		if len(messages) == 2 {
			cancel()
		}
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}
	if len(messages) != 2 || messages[0] != "first" || messages[1] != "second" {
		t.Fatalf("messages = %v", messages)
	}
	if generation := session.Generation(); generation != 2 {
		t.Fatalf("Generation() = %d, want 2", generation)
	}

	calls, routes, requests := connector.snapshot()
	if calls != 2 || len(routes) != 2 || routes[0] != "route-b" || routes[1] != "route-b" {
		t.Fatalf("connect calls = %d, routes = %v", calls, routes)
	}
	if got := requests[0].Header.Get("X-Session-Token"); got != "1" {
		t.Fatalf("first token = %q, want 1", got)
	}
	if got := requests[1].Header.Get("X-Session-Token"); got != "2" {
		t.Fatalf("second token = %q, want 2", got)
	}
	if len(first.writtenMessages()) != 1 || len(second.writtenMessages()) != 1 {
		t.Fatalf("subscription writes = %d, %d", len(first.writtenMessages()), len(second.writtenMessages()))
	}
	connectedGenerations := make([]uint64, 0, 2)
	for _, state := range states {
		if state.State == StateConnected {
			connectedGenerations = append(connectedGenerations, state.Generation)
		}
	}
	if len(connectedGenerations) != 2 || connectedGenerations[0] != 1 || connectedGenerations[1] != 2 {
		t.Fatalf("connected generations = %v", connectedGenerations)
	}
}

func TestSessionCloseStopsBlockingRead(t *testing.T) {
	t.Parallel()

	connection := newFakeConnection()
	connector := &scriptedConnector{steps: []connectStep{{connection: connection}}}
	connected := make(chan struct{})
	var connectedOnce sync.Once
	session, err := NewSession(SessionConfig{
		Connector:     connector,
		EgressRouteID: "route-a",
		Request:       DialRequest{Endpoint: "wss://stream.example.test/ws"},
		Observer: func(change StateChange) {
			if change.State == StateConnected {
				connectedOnce.Do(func() { close(connected) })
			}
		},
	})
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}
	if err := session.Write(context.Background(), Message{Type: MessageText}); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("Write() before Run error = %v, want ErrNotConnected", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- session.Run(context.Background(), func(context.Context, Message) error { return nil })
	}()
	select {
	case <-connected:
	case <-time.After(time.Second):
		t.Fatal("session did not connect")
	}
	if err := session.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	select {
	case runErr := <-done:
		if !errors.Is(runErr, ErrSessionClosed) {
			t.Fatalf("Run() error = %v, want ErrSessionClosed", runErr)
		}
	case <-time.After(time.Second):
		t.Fatal("Run() did not stop after Close()")
	}
	if err := session.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if err := session.Run(context.Background(), func(context.Context, Message) error { return nil }); !errors.Is(err, ErrSessionClosed) {
		t.Fatalf("second Run() error = %v, want ErrSessionClosed", err)
	}
}

func TestSessionReturnsHandlerErrorWithoutReconnect(t *testing.T) {
	t.Parallel()

	connection := newFakeConnection(fakeReadResult{message: Message{Type: MessageBinary, Data: []byte{1}}})
	connector := &scriptedConnector{steps: []connectStep{{connection: connection}}}
	session, err := NewSession(SessionConfig{
		Connector:     connector,
		EgressRouteID: "route-a",
		Request:       DialRequest{Endpoint: "wss://stream.example.test/ws"},
	})
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}
	handlerError := errors.New("handler failed")
	if err := session.Run(context.Background(), func(context.Context, Message) error { return handlerError }); !errors.Is(err, handlerError) {
		t.Fatalf("Run() error = %v, want handler error", err)
	}
	if calls, _, _ := connector.snapshot(); calls != 1 {
		t.Fatalf("Connect() calls = %d, want 1", calls)
	}
}

func TestSessionStopsAfterMaximumReconnectAttempts(t *testing.T) {
	t.Parallel()

	connectError := errors.New("dial failed")
	connector := &scriptedConnector{steps: []connectStep{
		{err: connectError},
		{err: connectError},
	}}
	session, err := NewSession(SessionConfig{
		Connector:            connector,
		EgressRouteID:        "route-a",
		Request:              DialRequest{Endpoint: "wss://stream.example.test/ws"},
		MaxReconnectAttempts: 1,
		Backoff:              func(int) time.Duration { return 0 },
	})
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}
	if err := session.Run(context.Background(), func(context.Context, Message) error { return nil }); !errors.Is(err, connectError) {
		t.Fatalf("Run() error = %v, want connect error", err)
	}
	if calls, _, _ := connector.snapshot(); calls != 2 {
		t.Fatalf("Connect() calls = %d, want 2", calls)
	}
}

func TestDefaultReconnectPolicy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "ordinary failure", err: errors.New("network failure"), want: true},
		{name: "server failure", err: &HandshakeError{HTTPStatus: http.StatusBadGateway}, want: true},
		{name: "rate limit", err: &HandshakeError{HTTPStatus: http.StatusTooManyRequests}, want: true},
		{name: "exchange throttle", err: &HandshakeError{HTTPStatus: http.StatusTeapot}, want: true},
		{name: "unauthorized", err: &HandshakeError{HTTPStatus: http.StatusUnauthorized}, want: false},
		{name: "invalid endpoint", err: &permanentError{cause: errors.New("invalid endpoint")}, want: false},
		{name: "canceled", err: context.Canceled, want: false},
		{name: "nil", err: nil, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := DefaultReconnectPolicy(test.err); got != test.want {
				t.Fatalf("DefaultReconnectPolicy() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestExponentialBackoffAndRetryAfter(t *testing.T) {
	t.Parallel()

	backoff := ExponentialBackoff(100*time.Millisecond, 350*time.Millisecond)
	wants := []time.Duration{100 * time.Millisecond, 100 * time.Millisecond, 200 * time.Millisecond, 350 * time.Millisecond, 350 * time.Millisecond}
	for attempt, want := range wants {
		if got := backoff(attempt); got != want {
			t.Fatalf("backoff(%d) = %s, want %s", attempt, got, want)
		}
	}
	if got := handshakeRetryAfter(&HandshakeError{RetryAfter: "3"}, time.Now()); got != 3*time.Second {
		t.Fatalf("numeric Retry-After = %s, want 3s", got)
	}
	now := time.Date(2026, time.August, 24, 0, 0, 0, 0, time.UTC)
	retryAt := now.Add(5 * time.Second).Format(http.TimeFormat)
	if got := handshakeRetryAfter(&HandshakeError{RetryAfter: retryAt}, now); got != 5*time.Second {
		t.Fatalf("date Retry-After = %s, want 5s", got)
	}
}
