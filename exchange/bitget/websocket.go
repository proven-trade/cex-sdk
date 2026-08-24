package bitget

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	trade "github.com/proven-trade/proven-trade-sdk"
	"github.com/proven-trade/proven-trade-sdk/credential"
	"github.com/proven-trade/proven-trade-sdk/model"
	corestream "github.com/proven-trade/proven-trade-sdk/stream"
	"github.com/proven-trade/proven-trade-sdk/transport"
)

const (
	DefaultPublicStreamURL      = "wss://ws.bitget.com/v3/ws/public"
	DefaultPrivateStreamURL     = "wss://ws.bitget.com/v3/ws/private"
	DefaultDemoPublicStreamURL  = "wss://wspap.bitget.com/v3/ws/public"
	DefaultDemoPrivateStreamURL = "wss://wspap.bitget.com/v3/ws/private"
	defaultHeartbeatInterval    = 30 * time.Second
	defaultHeartbeatTimeout     = 10 * time.Second
	defaultLoginTimeout         = 10 * time.Second
	defaultSubscriptionInterval = 15 * time.Second
	maxStreamArguments          = 1000
)

// StreamClientConfig는 Bitget v3 UTA public/private WebSocket 설정이다.
type StreamClientConfig struct {
	Connector              corestream.Connector
	Credentials            *credential.Descriptor
	CredentialProvider     credential.Provider
	DefaultEgressRouteID   transport.EgressRouteID
	PublicStreamURL        string
	PrivateStreamURL       string
	AllowInsecureWebSocket bool
	DemoTrading            bool
	Now                    func() time.Time
	Observer               corestream.StateObserver
	ReconnectPolicy        corestream.ReconnectPolicy
	Backoff                corestream.Backoff
	MaxReconnectAttempts   int
	HeartbeatInterval      time.Duration
	HeartbeatTimeout       time.Duration
	LoginTimeout           time.Duration
	SubscriptionInterval   time.Duration
}

// StreamClient는 Bitget v3 UTA WebSocket 세션을 생성한다.
type StreamClient struct {
	connector            corestream.Connector
	credentials          *credential.Descriptor
	credentialProvider   credential.Provider
	defaultRouteID       transport.EgressRouteID
	publicURL            string
	privateURL           string
	now                  func() time.Time
	observer             corestream.StateObserver
	reconnectPolicy      corestream.ReconnectPolicy
	backoff              corestream.Backoff
	maxReconnectAttempts int
	heartbeatInterval    time.Duration
	heartbeatTimeout     time.Duration
	loginTimeout         time.Duration
	subscriptionInterval time.Duration
}

// NewStreamClient는 Bitget v3 UTA WebSocket 클라이언트를 생성한다.
func NewStreamClient(config StreamClientConfig) (*StreamClient, error) {
	if config.Connector == nil {
		return nil, fmt.Errorf("Bitget stream connector is required")
	}
	defaultRouteID := transport.EgressRouteID(strings.TrimSpace(string(config.DefaultEgressRouteID)))
	if defaultRouteID == "" {
		return nil, trade.ErrMissingEgressRoute
	}
	if config.PublicStreamURL == "" {
		if config.DemoTrading {
			config.PublicStreamURL = DefaultDemoPublicStreamURL
		} else {
			config.PublicStreamURL = DefaultPublicStreamURL
		}
	}
	if config.PrivateStreamURL == "" {
		if config.DemoTrading {
			config.PrivateStreamURL = DefaultDemoPrivateStreamURL
		} else {
			config.PrivateStreamURL = DefaultPrivateStreamURL
		}
	}
	publicURL, err := validateWebSocketURL(config.PublicStreamURL, config.AllowInsecureWebSocket)
	if err != nil {
		return nil, fmt.Errorf("invalid Bitget public stream URL: %w", err)
	}
	privateURL, err := validateWebSocketURL(config.PrivateStreamURL, config.AllowInsecureWebSocket)
	if err != nil {
		return nil, fmt.Errorf("invalid Bitget private stream URL: %w", err)
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.HeartbeatInterval == 0 {
		config.HeartbeatInterval = defaultHeartbeatInterval
	}
	if config.HeartbeatTimeout == 0 {
		config.HeartbeatTimeout = defaultHeartbeatTimeout
	}
	if config.LoginTimeout == 0 {
		config.LoginTimeout = defaultLoginTimeout
	}
	if config.SubscriptionInterval == 0 {
		config.SubscriptionInterval = defaultSubscriptionInterval
	}
	if config.HeartbeatInterval < 0 || config.HeartbeatTimeout < 0 || config.LoginTimeout <= 0 || config.SubscriptionInterval < 0 {
		return nil, fmt.Errorf("Bitget stream durations are invalid")
	}
	if config.MaxReconnectAttempts < 0 {
		return nil, fmt.Errorf("Bitget maximum reconnect attempts cannot be negative")
	}

	var credentialsCopy *credential.Descriptor
	if config.Credentials != nil {
		if err := config.Credentials.Validate(); err != nil {
			return nil, err
		}
		if config.Credentials.Exchange != model.ExchangeBitget {
			return nil, fmt.Errorf("credential exchange must be Bitget")
		}
		if config.CredentialProvider == nil {
			return nil, fmt.Errorf("credential provider is required for private Bitget streams")
		}
		copyValue := *config.Credentials
		copyValue.Permissions = append([]credential.Permission(nil), config.Credentials.Permissions...)
		copyValue.AllowedEgressRouteIDs = append([]transport.EgressRouteID(nil), config.Credentials.AllowedEgressRouteIDs...)
		credentialsCopy = &copyValue
	}
	if config.Credentials == nil && config.CredentialProvider != nil {
		return nil, fmt.Errorf("credential descriptor is required with credential provider")
	}

	return &StreamClient{
		connector:            &bitgetConnector{next: config.Connector},
		credentials:          credentialsCopy,
		credentialProvider:   config.CredentialProvider,
		defaultRouteID:       defaultRouteID,
		publicURL:            publicURL,
		privateURL:           privateURL,
		now:                  config.Now,
		observer:             config.Observer,
		reconnectPolicy:      config.ReconnectPolicy,
		backoff:              config.Backoff,
		maxReconnectAttempts: config.MaxReconnectAttempts,
		heartbeatInterval:    config.HeartbeatInterval,
		heartbeatTimeout:     config.HeartbeatTimeout,
		loginTimeout:         config.LoginTimeout,
		subscriptionInterval: config.SubscriptionInterval,
	}, nil
}

type managedStream struct {
	session *corestream.Session

	mu        sync.Mutex
	arguments map[string]StreamArgument

	commandMu     sync.Mutex
	lastCommandAt time.Time
	interval      time.Duration
}

// PublicStream은 Bitget public channel 연결을 관리한다.
type PublicStream struct {
	managed *managedStream
}

// PublicStream은 선택한 EIP route에 고정된 public channel 세션을 생성한다.
func (client *StreamClient) PublicStream(request StreamRequest, options ...trade.RequestOption) (*PublicStream, error) {
	arguments, err := validateStreamArguments(request.Arguments, false)
	if err != nil {
		return nil, err
	}
	routeID, err := client.resolveRoute(options...)
	if err != nil {
		return nil, err
	}
	managed := newManagedStream(arguments, client.subscriptionInterval)
	session, err := corestream.NewSession(corestream.SessionConfig{
		Connector:            client.connector,
		EgressRouteID:        routeID,
		Request:              corestream.DialRequest{Endpoint: client.publicURL},
		OnConnect:            managed.resubscribe,
		Observer:             client.observer,
		ReconnectPolicy:      client.reconnectPolicy,
		Backoff:              client.backoff,
		MaxReconnectAttempts: client.maxReconnectAttempts,
		PingInterval:         client.heartbeatInterval,
		PingTimeout:          client.heartbeatTimeout,
	})
	if err != nil {
		return nil, err
	}
	managed.session = session
	return &PublicStream{managed: managed}, nil
}

// Run은 public 메시지를 순서대로 decode해 handler에 전달한다.
func (public *PublicStream) Run(ctx context.Context, handler func(context.Context, StreamMessage) error) error {
	if handler == nil {
		return fmt.Errorf("Bitget public stream handler is required")
	}
	return public.managed.run(ctx, handler)
}

// Subscribe는 public channel을 추가하고 재연결 목록에도 반영한다.
func (public *PublicStream) Subscribe(ctx context.Context, arguments ...StreamArgument) error {
	validated, err := validateStreamArguments(arguments, false)
	if err != nil {
		return err
	}
	return public.managed.change(ctx, "subscribe", validated)
}

// Unsubscribe는 public channel을 제거하고 재연결 목록에도 반영한다.
func (public *PublicStream) Unsubscribe(ctx context.Context, arguments ...StreamArgument) error {
	validated, err := validateStreamArguments(arguments, false)
	if err != nil {
		return err
	}
	return public.managed.change(ctx, "unsubscribe", validated)
}

// Close는 public stream 세션을 종료한다.
func (public *PublicStream) Close() error { return public.managed.session.Close() }

// Generation은 성공한 public stream 연결 세대 번호를 반환한다.
func (public *PublicStream) Generation() uint64 { return public.managed.session.Generation() }

// EgressRouteID는 public stream 연결과 재연결에 고정된 송신 경로를 반환한다.
func (public *PublicStream) EgressRouteID() transport.EgressRouteID {
	return public.managed.session.EgressRouteID()
}

func (public *PublicStream) hasSpotBooksArgument(symbol string) bool {
	public.managed.mu.Lock()
	defer public.managed.mu.Unlock()
	for _, argument := range public.managed.arguments {
		if argument.InstrumentType == "spot" && argument.Topic == "books" &&
			strings.EqualFold(argument.Symbol, symbol) {
			return true
		}
	}
	return false
}

// PrivateStream은 Bitget private UTA channel 연결을 관리한다.
type PrivateStream struct {
	managed *managedStream
}

// PrivateStream은 로그인 후 계정 channel을 구독하는 private 세션을 생성한다.
func (client *StreamClient) PrivateStream(request StreamRequest, options ...trade.RequestOption) (*PrivateStream, error) {
	arguments, err := validateStreamArguments(request.Arguments, true)
	if err != nil {
		return nil, err
	}
	routeID, err := client.resolveRoute(options...)
	if err != nil {
		return nil, err
	}
	if client.credentials == nil || client.credentialProvider == nil {
		return nil, client.authenticationError(errors.New("private Bitget stream requires credentials"))
	}
	if err := client.credentials.RequireEgressRoute(routeID); err != nil {
		return nil, client.authorizationError(err)
	}
	if err := client.credentials.RequirePermission(credential.PermissionRead); err != nil {
		return nil, client.authorizationError(err)
	}
	managed := newManagedStream(arguments, client.subscriptionInterval)
	session, err := corestream.NewSession(corestream.SessionConfig{
		Connector:     client.connector,
		EgressRouteID: routeID,
		Request:       corestream.DialRequest{Endpoint: client.privateURL},
		OnConnect: func(ctx context.Context, connection corestream.Connection) error {
			return client.loginAndSubscribe(ctx, connection, managed)
		},
		Observer:             client.observer,
		ReconnectPolicy:      client.privateReconnectPolicy,
		Backoff:              client.backoff,
		MaxReconnectAttempts: client.maxReconnectAttempts,
		PingInterval:         client.heartbeatInterval,
		PingTimeout:          client.heartbeatTimeout,
	})
	if err != nil {
		return nil, err
	}
	managed.session = session
	return &PrivateStream{managed: managed}, nil
}

// Run은 private 메시지를 순서대로 decode해 handler에 전달한다.
func (private *PrivateStream) Run(ctx context.Context, handler func(context.Context, StreamMessage) error) error {
	if handler == nil {
		return fmt.Errorf("Bitget private stream handler is required")
	}
	return private.managed.run(ctx, handler)
}

// Close는 private stream 세션을 종료한다.
func (private *PrivateStream) Close() error { return private.managed.session.Close() }

// Generation은 성공한 private stream 연결 세대 번호를 반환한다.
func (private *PrivateStream) Generation() uint64 { return private.managed.session.Generation() }

func newManagedStream(arguments []StreamArgument, interval time.Duration) *managedStream {
	managed := &managedStream{arguments: make(map[string]StreamArgument, len(arguments)), interval: interval}
	for _, argument := range arguments {
		managed.arguments[streamArgumentKey(argument)] = argument
	}
	return managed
}

func (managed *managedStream) run(ctx context.Context, handler func(context.Context, StreamMessage) error) error {
	return managed.session.Run(ctx, func(ctx context.Context, message corestream.Message) error {
		decoded, err := DecodeStreamMessage(message)
		if err != nil {
			return err
		}
		return handler(ctx, decoded)
	})
}

func (managed *managedStream) resubscribe(ctx context.Context, connection corestream.Connection) error {
	managed.commandMu.Lock()
	defer managed.commandMu.Unlock()
	arguments := managed.snapshotArguments()
	if len(arguments) == 0 {
		return nil
	}
	if err := writeStreamOperation(ctx, connection, "subscribe", arguments); err != nil {
		return err
	}
	managed.lastCommandAt = time.Now()
	return nil
}

func (managed *managedStream) change(ctx context.Context, operation string, arguments []StreamArgument) error {
	managed.commandMu.Lock()
	defer managed.commandMu.Unlock()
	managed.mu.Lock()
	selected := make([]StreamArgument, 0, len(arguments))
	for _, argument := range arguments {
		_, exists := managed.arguments[streamArgumentKey(argument)]
		if (operation == "subscribe" && !exists) || (operation == "unsubscribe" && exists) {
			selected = append(selected, argument)
		}
	}
	if operation == "subscribe" && len(managed.arguments)+len(selected) > maxStreamArguments {
		managed.mu.Unlock()
		return validationError("WebSocket channel count cannot exceed %d", maxStreamArguments)
	}
	managed.mu.Unlock()
	if len(selected) == 0 {
		return nil
	}
	if err := managed.waitCommandSlot(ctx); err != nil {
		return err
	}
	if err := writeStreamOperation(ctx, managed.session, operation, selected); err != nil {
		return err
	}
	managed.lastCommandAt = time.Now()
	managed.mu.Lock()
	for _, argument := range selected {
		key := streamArgumentKey(argument)
		if operation == "subscribe" {
			managed.arguments[key] = argument
		} else {
			delete(managed.arguments, key)
		}
	}
	managed.mu.Unlock()
	return nil
}

func (managed *managedStream) snapshotArguments() []StreamArgument {
	managed.mu.Lock()
	defer managed.mu.Unlock()
	keys := make([]string, 0, len(managed.arguments))
	for key := range managed.arguments {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	arguments := make([]StreamArgument, 0, len(keys))
	for _, key := range keys {
		arguments = append(arguments, managed.arguments[key])
	}
	return arguments
}

func (managed *managedStream) waitCommandSlot(ctx context.Context) error {
	delay := time.Until(managed.lastCommandAt.Add(managed.interval))
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type streamOperation struct {
	Operation string           `json:"op"`
	Arguments []StreamArgument `json:"args"`
}

type streamWriter interface {
	Write(context.Context, corestream.Message) error
}

func writeStreamOperation(ctx context.Context, writer streamWriter, operation string, arguments []StreamArgument) error {
	payload, err := json.Marshal(streamOperation{Operation: operation, Arguments: arguments})
	if err != nil {
		return fmt.Errorf("encode Bitget stream operation: %w", err)
	}
	return writer.Write(ctx, corestream.Message{Type: corestream.MessageText, Data: payload})
}

func (client *StreamClient) loginAndSubscribe(ctx context.Context, connection corestream.Connection, managed *managedStream) error {
	material, err := client.credentialProvider.Resolve(ctx, client.credentials.SecretRef)
	defer material.Destroy()
	if err != nil {
		return client.authenticationError(err)
	}
	if len(material.APIKey) == 0 || len(material.SecretKey) == 0 || len(material.Passphrase) == 0 {
		return client.authenticationError(errors.New("Bitget API key, HMAC secret, and passphrase are required"))
	}
	timestamp := strconv.FormatInt(client.now().UnixMilli(), 10)
	signature, err := SignHMACSHA256(material.SecretKey, []byte(timestamp+"GET/user/verify"))
	if err != nil {
		return client.authenticationError(err)
	}
	payload, err := json.Marshal(streamLoginRequest{
		Operation: "login",
		Arguments: []streamLoginArgument{{
			APIKey:     string(material.APIKey),
			Passphrase: string(material.Passphrase),
			Timestamp:  timestamp,
			Signature:  signature,
		}},
	})
	if err != nil {
		return fmt.Errorf("encode Bitget stream login: %w", err)
	}
	defer clearSensitiveBytes(payload)
	if err := connection.Write(ctx, corestream.Message{Type: corestream.MessageText, Data: payload}); err != nil {
		return err
	}
	loginContext, cancel := context.WithTimeout(ctx, client.loginTimeout)
	defer cancel()
	response, err := connection.Read(loginContext)
	if err != nil {
		return err
	}
	decoded, err := DecodeStreamMessage(response)
	if err != nil {
		return err
	}
	if decoded.Event != "login" || decoded.Code != "0" {
		return &LoginError{Code: decoded.Code, Message: decoded.Message}
	}
	return managed.resubscribe(ctx, connection)
}

type streamLoginRequest struct {
	Operation string                `json:"op"`
	Arguments []streamLoginArgument `json:"args"`
}

type streamLoginArgument struct {
	APIKey     string `json:"apiKey"`
	Passphrase string `json:"passphrase"`
	Timestamp  string `json:"timestamp"`
	Signature  string `json:"sign"`
}

func (client *StreamClient) privateReconnectPolicy(err error) bool {
	var loginError *LoginError
	if errors.As(err, &loginError) {
		return false
	}
	if client.reconnectPolicy != nil {
		return client.reconnectPolicy(err)
	}
	return corestream.DefaultReconnectPolicy(err)
}

func (client *StreamClient) resolveRoute(options ...trade.RequestOption) (transport.EgressRouteID, error) {
	resolved, err := trade.ResolveRequestOptions(client.defaultRouteID, options...)
	if err != nil {
		return "", err
	}
	if resolved.Timeout != 0 {
		return "", fmt.Errorf("Bitget stream timeout must be controlled by Run context")
	}
	return resolved.EgressRouteID, nil
}

func (client *StreamClient) authenticationError(cause error) error {
	accountID := ""
	if client.credentials != nil {
		accountID = client.credentials.AccountID
	}
	return &trade.APIError{Category: trade.ErrorAuthentication, Exchange: model.ExchangeBitget, AccountID: accountID, Cause: cause}
}

func (client *StreamClient) authorizationError(cause error) error {
	accountID := ""
	if client.credentials != nil {
		accountID = client.credentials.AccountID
	}
	return &trade.APIError{Category: trade.ErrorAuthorization, Exchange: model.ExchangeBitget, AccountID: accountID, Cause: cause}
}

// PublicStreamArgument는 상품 유형, topic, symbol로 public 구독 인자를 만든다.
func PublicStreamArgument(category Category, topic, symbol string) (StreamArgument, error) {
	instrumentType, err := streamInstrumentType(category)
	if err != nil {
		return StreamArgument{}, err
	}
	argument := StreamArgument{InstrumentType: instrumentType, Topic: topic, Symbol: strings.ToUpper(symbol)}
	validated, err := validateStreamArguments([]StreamArgument{argument}, false)
	if err != nil {
		return StreamArgument{}, err
	}
	return validated[0], nil
}

// KlineStreamArgument는 public candlestick 구독 인자를 만든다.
func KlineStreamArgument(category Category, symbol, interval string) (StreamArgument, error) {
	argument, err := PublicStreamArgument(category, "kline", symbol)
	if err != nil {
		return StreamArgument{}, err
	}
	argument.Interval = interval
	validated, err := validateStreamArguments([]StreamArgument{argument}, false)
	if err != nil {
		return StreamArgument{}, err
	}
	return validated[0], nil
}

// PrivateStreamArgument는 UTA account, position, order 또는 fill 구독 인자를 만든다.
func PrivateStreamArgument(topic string) (StreamArgument, error) {
	argument := StreamArgument{InstrumentType: "UTA", Topic: topic}
	validated, err := validateStreamArguments([]StreamArgument{argument}, true)
	if err != nil {
		return StreamArgument{}, err
	}
	return validated[0], nil
}

func validateStreamArguments(arguments []StreamArgument, private bool) ([]StreamArgument, error) {
	if len(arguments) == 0 {
		return nil, validationError("at least one WebSocket channel is required")
	}
	if len(arguments) > maxStreamArguments {
		return nil, validationError("WebSocket channel count cannot exceed %d", maxStreamArguments)
	}
	validated := make([]StreamArgument, 0, len(arguments))
	seen := make(map[string]struct{}, len(arguments))
	for _, argument := range arguments {
		for _, value := range []string{argument.InstrumentType, argument.Topic, argument.Symbol, argument.Interval} {
			if strings.TrimSpace(value) != value || strings.ContainsFunc(value, unicode.IsControl) {
				return nil, validationError("WebSocket channel contains invalid whitespace")
			}
		}
		if private {
			if argument.InstrumentType != "UTA" || (argument.Topic != "account" && argument.Topic != "position" && argument.Topic != "order" && argument.Topic != "fill") || argument.Symbol != "" || argument.Interval != "" {
				return nil, validationError("invalid private UTA WebSocket channel")
			}
		} else {
			if (argument.InstrumentType != "spot" && argument.InstrumentType != "usdt-futures") || argument.Symbol == "" {
				return nil, validationError("invalid public WebSocket instrument or symbol")
			}
			switch argument.Topic {
			case "ticker", "books", "books1", "books5", "books50", "publicTrade":
				if argument.Interval != "" {
					return nil, validationError("only kline channel accepts interval")
				}
			case "kline":
				if argument.Interval == "" {
					return nil, validationError("kline channel requires interval")
				}
			default:
				return nil, validationError("unsupported public WebSocket topic %q", argument.Topic)
			}
		}
		key := streamArgumentKey(argument)
		if _, exists := seen[key]; exists {
			return nil, validationError("duplicate WebSocket channel")
		}
		seen[key] = struct{}{}
		validated = append(validated, argument)
	}
	return validated, nil
}

func streamArgumentKey(argument StreamArgument) string {
	return argument.InstrumentType + "\x00" + argument.Topic + "\x00" + argument.Symbol + "\x00" + argument.Interval
}

func streamInstrumentType(category Category) (string, error) {
	switch category {
	case CategorySpot:
		return "spot", nil
	case CategoryUSDTFutures:
		return "usdt-futures", nil
	default:
		return "", validationError("unsupported WebSocket category %q", category)
	}
}

func validateWebSocketURL(raw string, allowInsecure bool) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" || parsed.RawQuery != "" {
		return "", fmt.Errorf("invalid WebSocket URL")
	}
	if parsed.Scheme != "wss" && !(allowInsecure && parsed.Scheme == "ws") {
		return "", fmt.Errorf("WebSocket URL must use WSS")
	}
	parsed.Path = strings.TrimSuffix(parsed.Path, "/")
	return parsed.String(), nil
}

type bitgetConnector struct {
	next corestream.Connector
}

func (connector *bitgetConnector) Connect(ctx context.Context, routeID transport.EgressRouteID, request corestream.DialRequest) (corestream.Connection, error) {
	connection, err := connector.next.Connect(ctx, routeID, request)
	if err != nil {
		return nil, err
	}
	return &bitgetConnection{next: connection, pong: make(chan struct{}, 1)}, nil
}

type bitgetConnection struct {
	next corestream.Connection
	pong chan struct{}
}

func (connection *bitgetConnection) Read(ctx context.Context) (corestream.Message, error) {
	message, err := connection.next.Read(ctx)
	if err == nil {
		trimmed := strings.TrimSpace(string(message.Data))
		if trimmed == "pong" || trimmed == `"pong"` {
			select {
			case connection.pong <- struct{}{}:
			default:
			}
		}
	}
	return message, err
}

func (connection *bitgetConnection) Write(ctx context.Context, message corestream.Message) error {
	return connection.next.Write(ctx, message)
}

func (connection *bitgetConnection) Ping(ctx context.Context) error {
	select {
	case <-connection.pong:
	default:
	}
	if err := connection.next.Write(ctx, corestream.Message{Type: corestream.MessageText, Data: []byte("ping")}); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-connection.pong:
		return nil
	}
}

func (connection *bitgetConnection) Close(code int, reason string) error {
	return connection.next.Close(code, reason)
}

func clearSensitiveBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
