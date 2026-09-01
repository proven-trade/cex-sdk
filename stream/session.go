package stream

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/proven-trade/cex-sdk/transport"
)

const (
	closeNormal              = 1000
	closeGoingAway           = 1001
	defaultPingTimeout       = 5 * time.Second
	defaultMaxReconnectDelay = 5 * time.Minute
)

var errReconnectRequested = errors.New("stream reconnect requested")

// SessionConfig는 재연결 WebSocket 세션의 동작을 정의한다.
type SessionConfig struct {
	Connector            Connector
	EgressRouteID        transport.EgressRouteID
	Request              DialRequest
	RequestSource        DialRequestSource
	OnConnect            ConnectHook
	Observer             StateObserver
	ReconnectPolicy      ReconnectPolicy
	Backoff              Backoff
	MaxReconnectAttempts int
	MaxReconnectDelay    time.Duration
	PingInterval         time.Duration
	PingTimeout          time.Duration
}

// Session은 한 송신 경로에 고정된 자동 재연결 WebSocket 세션이다.
// 하나의 Session은 Run을 한 번만 호출할 수 있다.
type Session struct {
	config SessionConfig

	mu         sync.RWMutex
	connection Connection
	generation uint64
	started    bool
	closed     bool
	reconnect  bool
	cancel     context.CancelFunc
}

type permanentError struct {
	cause error
}

func (err *permanentError) Error() string { return err.cause.Error() }
func (err *permanentError) Unwrap() error { return err.cause }

// NewSession은 설정을 검증하고 재연결 세션을 생성한다.
func NewSession(config SessionConfig) (*Session, error) {
	if config.Connector == nil {
		return nil, fmt.Errorf("stream connector is required")
	}
	if config.EgressRouteID == "" {
		return nil, fmt.Errorf("stream egress route is required")
	}
	if config.RequestSource == nil {
		if err := validateEndpoint(config.Request.Endpoint); err != nil {
			return nil, err
		}
	} else if config.Request.Endpoint != "" || len(config.Request.Header) > 0 {
		return nil, fmt.Errorf("static dial request and request source cannot be used together")
	}
	if config.MaxReconnectAttempts < 0 {
		return nil, fmt.Errorf("maximum reconnect attempts cannot be negative")
	}
	if config.PingInterval < 0 || config.PingTimeout < 0 {
		return nil, fmt.Errorf("ping durations cannot be negative")
	}
	if config.MaxReconnectDelay < 0 {
		return nil, fmt.Errorf("maximum reconnect delay cannot be negative")
	}
	if config.MaxReconnectDelay == 0 {
		config.MaxReconnectDelay = defaultMaxReconnectDelay
	}
	if config.PingInterval > 0 && config.PingTimeout == 0 {
		config.PingTimeout = defaultPingTimeout
	}
	if config.ReconnectPolicy == nil {
		config.ReconnectPolicy = DefaultReconnectPolicy
	}
	if config.Backoff == nil {
		config.Backoff = FullJitterBackoff(250*time.Millisecond, 30*time.Second)
	}
	config.Request.Header = config.Request.Header.Clone()
	return &Session{config: config}, nil
}

// Run은 연결과 메시지 처리를 시작하고 연결 오류 시 정책에 따라 재연결한다.
func (session *Session) Run(ctx context.Context, handler MessageHandler) error {
	if ctx == nil {
		return fmt.Errorf("stream context cannot be nil")
	}
	if handler == nil {
		return fmt.Errorf("stream message handler is required")
	}
	runContext, err := session.start(ctx)
	if err != nil {
		return err
	}
	defer session.finish()
	ctx = runContext

	attempt := 0
	for {
		if err := session.stopError(ctx); err != nil {
			return err
		}
		session.observe(StateChange{State: StateConnecting, RouteID: session.config.EgressRouteID, Generation: session.Generation(), Attempt: attempt})
		request, err := session.dialRequest(ctx)
		if err != nil {
			attempt++
			if waitErr := session.waitToReconnect(ctx, err, attempt); waitErr != nil {
				return waitErr
			}
			continue
		}
		connection, err := session.config.Connector.Connect(ctx, session.config.EgressRouteID, request)
		if err != nil {
			attempt++
			session.observe(StateChange{State: StateDisconnected, RouteID: session.config.EgressRouteID, Generation: session.Generation(), Attempt: attempt, Cause: err})
			if waitErr := session.waitToReconnect(ctx, err, attempt); waitErr != nil {
				return waitErr
			}
			continue
		}

		generation, accepted := session.install(connection)
		if !accepted {
			_ = connection.Close(closeNormal, "session closed")
			return ErrSessionClosed
		}
		session.observe(StateChange{State: StateConnected, RouteID: session.config.EgressRouteID, Generation: generation})
		if session.config.OnConnect != nil {
			if err := session.config.OnConnect(ctx, connection); err != nil {
				session.remove(connection)
				_ = connection.Close(closeGoingAway, "connect hook failed")
				attempt++
				session.observe(StateChange{State: StateDisconnected, RouteID: session.config.EgressRouteID, Generation: generation, Attempt: attempt, Cause: err})
				if waitErr := session.waitToReconnect(ctx, err, attempt); waitErr != nil {
					return waitErr
				}
				continue
			}
		}

		attempt = 0
		err, handlerFailed := session.consume(ctx, connection, handler)
		err = session.reconnectCause(connection, err)
		session.remove(connection)
		_ = connection.Close(closeGoingAway, "connection ended")
		if handlerFailed {
			return err
		}
		if stopErr := session.stopError(ctx); stopErr != nil {
			return stopErr
		}
		attempt++
		session.observe(StateChange{State: StateDisconnected, RouteID: session.config.EgressRouteID, Generation: generation, Attempt: attempt, Cause: err})
		if waitErr := session.waitToReconnect(ctx, err, attempt); waitErr != nil {
			return waitErr
		}
	}
}

// Write는 현재 연결에 메시지를 전송한다.
func (session *Session) Write(ctx context.Context, message Message) error {
	if ctx == nil {
		return fmt.Errorf("stream write context cannot be nil")
	}
	session.mu.RLock()
	connection, closed := session.connection, session.closed
	session.mu.RUnlock()
	if closed {
		return ErrSessionClosed
	}
	if connection == nil {
		return ErrNotConnected
	}
	message.Data = append([]byte(nil), message.Data...)
	return connection.Write(ctx, message)
}

// Generation은 성공한 WebSocket 연결 세대 번호를 반환한다.
func (session *Session) Generation() uint64 {
	session.mu.RLock()
	defer session.mu.RUnlock()
	return session.generation
}

// EgressRouteID는 최초 연결과 모든 재연결에 고정된 송신 경로를 반환한다.
func (session *Session) EgressRouteID() transport.EgressRouteID {
	return session.config.EgressRouteID
}

// Reconnect는 현재 연결만 종료해 같은 route의 새 연결을 시작하게 한다.
func (session *Session) Reconnect() error {
	session.mu.Lock()
	if session.closed {
		session.mu.Unlock()
		return ErrSessionClosed
	}
	connection := session.connection
	if connection == nil {
		session.mu.Unlock()
		return ErrNotConnected
	}
	session.reconnect = true
	session.mu.Unlock()
	return connection.Close(closeGoingAway, "reconnect requested")
}

// Close는 현재 연결을 닫고 이후 재연결을 중단한다.
func (session *Session) Close() error {
	session.mu.Lock()
	if session.closed {
		session.mu.Unlock()
		return nil
	}
	session.closed = true
	session.reconnect = false
	cancel := session.cancel
	connection := session.connection
	session.connection = nil
	session.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if connection != nil {
		return connection.Close(closeNormal, "session closed")
	}
	return nil
}

func (session *Session) start(ctx context.Context) (context.Context, error) {
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.closed {
		return nil, ErrSessionClosed
	}
	if session.started {
		return nil, ErrSessionAlreadyRun
	}
	session.started = true
	runContext, cancel := context.WithCancel(ctx)
	session.cancel = cancel
	return runContext, nil
}

func (session *Session) finish() {
	session.mu.Lock()
	connection := session.connection
	session.connection = nil
	cancel := session.cancel
	session.cancel = nil
	session.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if connection != nil {
		_ = connection.Close(closeNormal, "session finished")
	}
	session.observe(StateChange{State: StateClosed, RouteID: session.config.EgressRouteID, Generation: session.Generation()})
}

func (session *Session) install(connection Connection) (uint64, bool) {
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.closed {
		return session.generation, false
	}
	session.connection = connection
	session.reconnect = false
	session.generation++
	return session.generation, true
}

func (session *Session) remove(connection Connection) {
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.connection == connection {
		session.connection = nil
	}
}

func (session *Session) reconnectCause(connection Connection, cause error) error {
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.connection == connection && session.reconnect {
		session.reconnect = false
		return errReconnectRequested
	}
	return cause
}

func (session *Session) dialRequest(ctx context.Context) (DialRequest, error) {
	request := session.config.Request
	if session.config.RequestSource != nil {
		resolved, err := session.config.RequestSource(ctx)
		if err != nil {
			return DialRequest{}, err
		}
		request = resolved
	}
	if err := validateEndpoint(request.Endpoint); err != nil {
		return DialRequest{}, &permanentError{cause: err}
	}
	request.Header = request.Header.Clone()
	return request, nil
}

func (session *Session) consume(ctx context.Context, connection Connection, handler MessageHandler) (error, bool) {
	readContext, cancel := context.WithCancel(ctx)
	defer cancel()
	type readResult struct {
		message Message
		err     error
	}
	results := make(chan readResult, 1)
	go func() {
		for {
			message, err := connection.Read(readContext)
			select {
			case results <- readResult{message: message, err: err}:
			case <-readContext.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()

	var ping <-chan time.Time
	var ticker *time.Ticker
	if session.config.PingInterval > 0 {
		ticker = time.NewTicker(session.config.PingInterval)
		defer ticker.Stop()
		ping = ticker.C
	}
	for {
		select {
		case <-ctx.Done():
			cancel()
			_ = connection.Close(closeNormal, "context finished")
			return session.stopError(ctx), false
		case result := <-results:
			if result.err != nil {
				return result.err, false
			}
			if err := handler(ctx, result.message); err != nil {
				cancel()
				var reconnect *reconnectMessageError
				return err, !errors.As(err, &reconnect)
			}
		case <-ping:
			pingContext, pingCancel := context.WithTimeout(ctx, session.config.PingTimeout)
			err := connection.Ping(pingContext)
			pingCancel()
			if err != nil {
				cancel()
				return err, false
			}
		}
	}
}

func (session *Session) waitToReconnect(ctx context.Context, cause error, attempt int) error {
	if stopErr := session.stopError(ctx); stopErr != nil {
		return stopErr
	}
	if !session.config.ReconnectPolicy(cause) {
		return cause
	}
	if session.config.MaxReconnectAttempts > 0 && attempt > session.config.MaxReconnectAttempts {
		return cause
	}
	delay := session.config.Backoff(attempt)
	if retryDelay := handshakeRetryAfter(cause, time.Now()); retryDelay > delay {
		delay = retryDelay
	}
	if delay > session.config.MaxReconnectDelay {
		delay = session.config.MaxReconnectDelay
	}
	if delay < 0 {
		delay = 0
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return session.stopError(ctx)
	case <-timer.C:
		return session.stopError(ctx)
	}
}

func (session *Session) stopError(ctx context.Context) error {
	session.mu.RLock()
	closed := session.closed
	session.mu.RUnlock()
	if closed {
		return ErrSessionClosed
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

func (session *Session) observe(change StateChange) {
	if session.config.Observer != nil {
		session.config.Observer(change)
	}
}

// DefaultReconnectPolicy는 영구적인 4xx handshake 오류를 제외한 연결 오류를 재시도한다.
func DefaultReconnectPolicy(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrSessionClosed) {
		return false
	}
	var handshakeError *HandshakeError
	if errors.As(err, &handshakeError) {
		status := handshakeError.HTTPStatus
		if status >= http.StatusBadRequest && status < http.StatusInternalServerError &&
			status != http.StatusRequestTimeout && status != http.StatusTooManyRequests && status != http.StatusTeapot {
			return false
		}
	}
	var permanent *permanentError
	if errors.As(err, &permanent) {
		return false
	}
	return true
}

// ExponentialBackoff는 상한이 있는 지수 재연결 대기 함수를 만든다.
func ExponentialBackoff(minimum, maximum time.Duration) Backoff {
	return func(attempt int) time.Duration {
		if minimum <= 0 || maximum < minimum {
			return 0
		}
		if attempt < 1 {
			attempt = 1
		}
		delay := minimum
		for index := 1; index < attempt && delay < maximum; index++ {
			if delay > maximum/2 {
				return maximum
			}
			delay *= 2
		}
		if delay > maximum {
			return maximum
		}
		return delay
	}
}

// FullJitterBackoff는 지수 상한 안에서 균등 난수 지연을 반환해 여러
// 세션이 동시에 재연결하는 현상을 줄인다.
func FullJitterBackoff(minimum, maximum time.Duration) Backoff {
	exponential := ExponentialBackoff(minimum, maximum)
	return func(attempt int) time.Duration {
		ceiling := exponential(attempt)
		if ceiling <= 0 {
			return 0
		}
		return time.Duration(rand.Int64N(int64(ceiling) + 1))
	}
}

func handshakeRetryAfter(err error, now time.Time) time.Duration {
	var handshakeError *HandshakeError
	if !errors.As(err, &handshakeError) {
		return 0
	}
	value := handshakeError.RetryAfter
	if seconds, parseErr := strconv.Atoi(value); parseErr == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	if retryAt, parseErr := http.ParseTime(value); parseErr == nil && retryAt.After(now) {
		return retryAt.Sub(now)
	}
	return 0
}
