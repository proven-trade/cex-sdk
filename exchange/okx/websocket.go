package okx

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
	"sync/atomic"
	"time"
	"unicode"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	"github.com/proven-trade/cex-sdk/model"
	corestream "github.com/proven-trade/cex-sdk/stream"
	"github.com/proven-trade/cex-sdk/transport"
)

const (
	DefaultPublicStreamURL       = "wss://ws.okx.com:8443/ws/v5/public"
	DefaultPrivateStreamURL      = "wss://ws.okx.com:8443/ws/v5/private"
	DefaultBusinessStreamURL     = "wss://ws.okx.com:8443/ws/v5/business"
	DefaultDemoPublicStreamURL   = "wss://wspap.okx.com:8443/ws/v5/public"
	DefaultDemoPrivateStreamURL  = "wss://wspap.okx.com:8443/ws/v5/private"
	DefaultDemoBusinessStreamURL = "wss://wspap.okx.com:8443/ws/v5/business"
	defaultStreamPingInterval    = 20 * time.Second
	defaultStreamPingTimeout     = 10 * time.Second
	defaultStreamLoginTimeout    = 10 * time.Second
	defaultStreamCommandInterval = 8 * time.Second
	maxStreamArguments           = 1000
	maxOperationArguments        = 100
	maxOperationBytes            = 64 * 1024
)

// StreamClientConfig는 OKX V5 public/business/private WebSocket 설정이다.
type StreamClientConfig struct {
	Connector              corestream.Connector
	Credentials            *credential.Descriptor
	CredentialProvider     credential.Provider
	DefaultEgressRouteID   transport.EgressRouteID
	PublicStreamURL        string
	PrivateStreamURL       string
	BusinessStreamURL      string
	DemoTrading            bool
	AllowInsecureWebSocket bool
	Now                    func() time.Time
	Observer               corestream.StateObserver
	ReconnectPolicy        corestream.ReconnectPolicy
	Backoff                corestream.Backoff
	MaxReconnectAttempts   int
	PingInterval           time.Duration
	PingTimeout            time.Duration
	LoginTimeout           time.Duration
	SubscriptionInterval   time.Duration
}

// StreamClient는 OKX V5 WebSocket 세션을 생성한다.
type StreamClient struct {
	connector            corestream.Connector
	credentials          *credential.Descriptor
	credentialProvider   credential.Provider
	defaultRouteID       transport.EgressRouteID
	publicURL            string
	privateURL           string
	businessURL          string
	now                  func() time.Time
	observer             corestream.StateObserver
	reconnectPolicy      corestream.ReconnectPolicy
	backoff              corestream.Backoff
	maxReconnectAttempts int
	pingInterval         time.Duration
	pingTimeout          time.Duration
	loginTimeout         time.Duration
	subscriptionInterval time.Duration
}

// NewStreamClient는 OKX V5 WebSocket 클라이언트를 생성한다.
func NewStreamClient(config StreamClientConfig) (*StreamClient, error) {
	if config.Connector == nil {
		return nil, fmt.Errorf("OKX stream connector is required")
	}
	defaultRouteID := transport.EgressRouteID(strings.TrimSpace(string(config.DefaultEgressRouteID)))
	if defaultRouteID == "" {
		return nil, trade.ErrMissingEgressRoute
	}
	setDefaultStreamURLs(&config)
	publicURL, err := validateWebSocketURL(config.PublicStreamURL, config.AllowInsecureWebSocket)
	if err != nil {
		return nil, fmt.Errorf("invalid OKX public stream URL: %w", err)
	}
	privateURL, err := validateWebSocketURL(config.PrivateStreamURL, config.AllowInsecureWebSocket)
	if err != nil {
		return nil, fmt.Errorf("invalid OKX private stream URL: %w", err)
	}
	businessURL, err := validateWebSocketURL(config.BusinessStreamURL, config.AllowInsecureWebSocket)
	if err != nil {
		return nil, fmt.Errorf("invalid OKX business stream URL: %w", err)
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.PingInterval == 0 {
		config.PingInterval = defaultStreamPingInterval
	}
	if config.PingTimeout == 0 {
		config.PingTimeout = defaultStreamPingTimeout
	}
	if config.LoginTimeout == 0 {
		config.LoginTimeout = defaultStreamLoginTimeout
	}
	if config.SubscriptionInterval == 0 {
		config.SubscriptionInterval = defaultStreamCommandInterval
	}
	if config.PingInterval < 0 || config.PingTimeout < 0 || config.LoginTimeout <= 0 ||
		config.SubscriptionInterval < 0 {
		return nil, fmt.Errorf("OKX stream durations are invalid")
	}
	if config.MaxReconnectAttempts < 0 {
		return nil, fmt.Errorf("OKX maximum reconnect attempts cannot be negative")
	}

	var credentialsCopy *credential.Descriptor
	if config.Credentials != nil {
		if err := config.Credentials.Validate(); err != nil {
			return nil, err
		}
		if config.Credentials.Exchange != model.ExchangeOKX {
			return nil, fmt.Errorf("credential exchange must be OKX")
		}
		if config.CredentialProvider == nil {
			return nil, fmt.Errorf("credential provider is required for private OKX streams")
		}
		copyValue := *config.Credentials
		copyValue.Permissions = append([]credential.Permission(nil), config.Credentials.Permissions...)
		copyValue.AllowedEgressRouteIDs = append(
			[]transport.EgressRouteID(nil), config.Credentials.AllowedEgressRouteIDs...,
		)
		credentialsCopy = &copyValue
	}
	if config.Credentials == nil && config.CredentialProvider != nil {
		return nil, fmt.Errorf("credential descriptor is required with credential provider")
	}

	return &StreamClient{
		connector:            &okxConnector{next: config.Connector},
		credentials:          credentialsCopy,
		credentialProvider:   config.CredentialProvider,
		defaultRouteID:       defaultRouteID,
		publicURL:            publicURL,
		privateURL:           privateURL,
		businessURL:          businessURL,
		now:                  config.Now,
		observer:             config.Observer,
		reconnectPolicy:      config.ReconnectPolicy,
		backoff:              config.Backoff,
		maxReconnectAttempts: config.MaxReconnectAttempts,
		pingInterval:         config.PingInterval,
		pingTimeout:          config.PingTimeout,
		loginTimeout:         config.LoginTimeout,
		subscriptionInterval: config.SubscriptionInterval,
	}, nil
}

func setDefaultStreamURLs(config *StreamClientConfig) {
	if config.PublicStreamURL == "" {
		config.PublicStreamURL = DefaultPublicStreamURL
		if config.DemoTrading {
			config.PublicStreamURL = DefaultDemoPublicStreamURL
		}
	}
	if config.PrivateStreamURL == "" {
		config.PrivateStreamURL = DefaultPrivateStreamURL
		if config.DemoTrading {
			config.PrivateStreamURL = DefaultDemoPrivateStreamURL
		}
	}
	if config.BusinessStreamURL == "" {
		config.BusinessStreamURL = DefaultBusinessStreamURL
		if config.DemoTrading {
			config.BusinessStreamURL = DefaultDemoBusinessStreamURL
		}
	}
}

type managedStream struct {
	session *corestream.Session

	mu        sync.Mutex
	arguments map[string]StreamArgument

	commandMu     sync.Mutex
	lastCommandAt time.Time
	interval      time.Duration
	nextID        atomic.Uint64
}

// PublicStream은 OKX public 또는 business channel 연결을 관리한다.
type PublicStream struct {
	managed  *managedStream
	endpoint StreamEndpoint
}

// PublicStream은 선택한 endpoint와 송신 경로에 고정된 시세 세션을 생성한다.
func (client *StreamClient) PublicStream(
	request PublicStreamRequest,
	options ...trade.RequestOption,
) (*PublicStream, error) {
	arguments, err := validatePublicStreamArguments(request.Endpoint, request.Arguments)
	if err != nil {
		return nil, err
	}
	routeID, err := client.resolveRoute(options...)
	if err != nil {
		return nil, err
	}
	endpointURL := client.publicURL
	if request.Endpoint == StreamEndpointBusiness {
		endpointURL = client.businessURL
	}
	managed := newManagedStream(arguments, client.subscriptionInterval)
	session, err := corestream.NewSession(corestream.SessionConfig{
		Connector:            client.connector,
		EgressRouteID:        routeID,
		Request:              corestream.DialRequest{Endpoint: endpointURL},
		OnConnect:            managed.resubscribe,
		Observer:             client.observer,
		ReconnectPolicy:      client.reconnectPolicy,
		Backoff:              client.backoff,
		MaxReconnectAttempts: client.maxReconnectAttempts,
		PingInterval:         client.pingInterval,
		PingTimeout:          client.pingTimeout,
	})
	if err != nil {
		return nil, err
	}
	managed.session = session
	return &PublicStream{managed: managed, endpoint: request.Endpoint}, nil
}

// Run은 public 또는 business 메시지를 순서대로 decode해 handler에 전달한다.
func (public *PublicStream) Run(ctx context.Context, handler func(context.Context, StreamMessage) error) error {
	if handler == nil {
		return fmt.Errorf("OKX public stream handler is required")
	}
	return public.managed.run(ctx, handler)
}

// Subscribe는 channel을 추가하고 재연결 목록에도 반영한다.
func (public *PublicStream) Subscribe(ctx context.Context, arguments ...StreamArgument) error {
	validated, err := validatePublicStreamArguments(public.endpoint, arguments)
	if err != nil {
		return err
	}
	return public.managed.change(ctx, "subscribe", validated)
}

// Unsubscribe는 channel을 제거하고 재연결 목록에도 반영한다.
func (public *PublicStream) Unsubscribe(ctx context.Context, arguments ...StreamArgument) error {
	validated, err := validatePublicStreamArguments(public.endpoint, arguments)
	if err != nil {
		return err
	}
	return public.managed.change(ctx, "unsubscribe", validated)
}

// Close는 public stream 세션을 종료한다.
func (public *PublicStream) Close() error { return public.managed.session.Close() }

// Generation은 성공한 public stream 연결 세대 번호를 반환한다.
func (public *PublicStream) Generation() uint64 { return public.managed.session.Generation() }

// EgressRouteID는 public 연결과 재연결에 고정된 송신 경로를 반환한다.
func (public *PublicStream) EgressRouteID() transport.EgressRouteID {
	return public.managed.session.EgressRouteID()
}

func (public *PublicStream) hasArgument(argument StreamArgument) bool {
	if public.endpoint != StreamEndpointPublic {
		return false
	}
	public.managed.mu.Lock()
	defer public.managed.mu.Unlock()
	_, exists := public.managed.arguments[streamArgumentKey(argument)]
	return exists
}

func (public *PublicStream) reconnect() error {
	return public.managed.session.Reconnect()
}

// PrivateStream은 OKX private channel 연결을 관리한다.
type PrivateStream struct {
	managed *managedStream
}

// PrivateStream은 로그인 후 계정 channel을 구독하는 private 세션을 생성한다.
func (client *StreamClient) PrivateStream(
	request PrivateStreamRequest,
	options ...trade.RequestOption,
) (*PrivateStream, error) {
	arguments, err := validatePrivateStreamArguments(request.Arguments)
	if err != nil {
		return nil, err
	}
	routeID, err := client.resolveRoute(options...)
	if err != nil {
		return nil, err
	}
	if client.credentials == nil || client.credentialProvider == nil {
		return nil, client.authenticationError(errors.New("private OKX stream requires credentials"))
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
		PingInterval:         client.pingInterval,
		PingTimeout:          client.pingTimeout,
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
		return fmt.Errorf("OKX private stream handler is required")
	}
	return private.managed.run(ctx, handler)
}

// Close는 private stream 세션을 종료한다.
func (private *PrivateStream) Close() error { return private.managed.session.Close() }

// Generation은 성공한 private stream 연결 세대 번호를 반환한다.
func (private *PrivateStream) Generation() uint64 { return private.managed.session.Generation() }

func newManagedStream(arguments []StreamArgument, interval time.Duration) *managedStream {
	managed := &managedStream{
		arguments: make(map[string]StreamArgument, len(arguments)), interval: interval,
	}
	for _, argument := range arguments {
		managed.arguments[streamArgumentKey(argument)] = argument
	}
	return managed
}

func (managed *managedStream) run(ctx context.Context, handler func(context.Context, StreamMessage) error) error {
	return managed.session.Run(ctx, func(ctx context.Context, message corestream.Message) error {
		decoded, err := DecodeStreamMessage(message)
		if err != nil {
			return corestream.ReconnectOnMessageError(err)
		}
		return handler(ctx, decoded)
	})
}

func (managed *managedStream) resubscribe(ctx context.Context, connection corestream.Connection) error {
	managed.commandMu.Lock()
	defer managed.commandMu.Unlock()
	return managed.writeArguments(ctx, connection, "subscribe", managed.snapshotArguments())
}

func (managed *managedStream) change(
	ctx context.Context,
	operation string,
	arguments []StreamArgument,
) error {
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
	if err := managed.writeArguments(ctx, managed.session, operation, selected); err != nil {
		return err
	}
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

func (managed *managedStream) writeArguments(
	ctx context.Context,
	writer streamWriter,
	operation string,
	arguments []StreamArgument,
) error {
	for start := 0; start < len(arguments); start += maxOperationArguments {
		end := min(start+maxOperationArguments, len(arguments))
		if err := managed.waitCommandSlot(ctx); err != nil {
			return err
		}
		requestID := strconv.FormatUint(managed.nextID.Add(1), 10)
		if err := writeStreamOperation(ctx, writer, requestID, operation, arguments[start:end]); err != nil {
			return err
		}
		managed.lastCommandAt = time.Now()
	}
	return nil
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
	ID        string           `json:"id,omitempty"`
	Operation string           `json:"op"`
	Arguments []StreamArgument `json:"args"`
}

type streamWriter interface {
	Write(context.Context, corestream.Message) error
}

func writeStreamOperation(
	ctx context.Context,
	writer streamWriter,
	requestID, operation string,
	arguments []StreamArgument,
) error {
	payload, err := json.Marshal(streamOperation{
		ID: requestID, Operation: operation, Arguments: arguments,
	})
	if err != nil {
		return fmt.Errorf("encode OKX stream operation: %w", err)
	}
	if len(payload) > maxOperationBytes {
		return validationError("WebSocket operation exceeds %d bytes", maxOperationBytes)
	}
	return writer.Write(ctx, corestream.Message{Type: corestream.MessageText, Data: payload})
}

func (client *StreamClient) loginAndSubscribe(
	ctx context.Context,
	connection corestream.Connection,
	managed *managedStream,
) error {
	material, err := client.credentialProvider.Resolve(ctx, client.credentials.SecretRef)
	defer material.Destroy()
	if err != nil {
		return client.authenticationError(err)
	}
	if len(material.APIKey) == 0 || len(material.SecretKey) == 0 || len(material.Passphrase) == 0 {
		return client.authenticationError(errors.New("OKX API key, HMAC secret, and passphrase are required"))
	}
	timestamp := strconv.FormatInt(client.now().Unix(), 10)
	signature, err := SignHMACSHA256(
		material.SecretKey, []byte(timestamp+"GET/users/self/verify"),
	)
	if err != nil {
		return client.authenticationError(err)
	}
	payload, err := json.Marshal(struct {
		Operation string                `json:"op"`
		Arguments []streamLoginArgument `json:"args"`
	}{
		Operation: "login",
		Arguments: []streamLoginArgument{{
			APIKey: string(material.APIKey), Passphrase: string(material.Passphrase),
			Timestamp: timestamp, Signature: signature,
		}},
	})
	if err != nil {
		return fmt.Errorf("encode OKX stream login: %w", err)
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
		return &StreamLoginError{Code: decoded.Code, Message: decoded.Message}
	}
	return managed.resubscribe(ctx, connection)
}

type streamLoginArgument struct {
	APIKey     string `json:"apiKey"`
	Passphrase string `json:"passphrase"`
	Timestamp  string `json:"timestamp"`
	Signature  string `json:"sign"`
}

func (client *StreamClient) privateReconnectPolicy(err error) bool {
	var loginError *StreamLoginError
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
		return "", fmt.Errorf("OKX stream timeout must be controlled by Run context")
	}
	return resolved.EgressRouteID, nil
}

func (client *StreamClient) authenticationError(cause error) error {
	accountID := ""
	if client.credentials != nil {
		accountID = client.credentials.AccountID
	}
	return &trade.APIError{
		Category: trade.ErrorAuthentication, Exchange: model.ExchangeOKX,
		AccountID: accountID, Cause: cause,
	}
}

func (client *StreamClient) authorizationError(cause error) error {
	accountID := ""
	if client.credentials != nil {
		accountID = client.credentials.AccountID
	}
	return &trade.APIError{
		Category: trade.ErrorAuthorization, Exchange: model.ExchangeOKX,
		AccountID: accountID, Cause: cause,
	}
}

// PublicStreamArgument는 ticker, trade 또는 order book public 인자를 만든다.
func PublicStreamArgument(channel, instrumentID string) (StreamArgument, error) {
	argument := StreamArgument{Channel: channel, InstrumentID: instrumentID}
	validated, err := validatePublicStreamArguments(StreamEndpointPublic, []StreamArgument{argument})
	if err != nil {
		return StreamArgument{}, err
	}
	return validated[0], nil
}

// CandleStreamArgument는 business WebSocket 캔들 인자를 만든다.
func CandleStreamArgument(instrumentID string, interval CandleInterval) (StreamArgument, error) {
	argument := StreamArgument{Channel: "candle" + string(interval), InstrumentID: instrumentID}
	validated, err := validatePublicStreamArguments(StreamEndpointBusiness, []StreamArgument{argument})
	if err != nil {
		return StreamArgument{}, err
	}
	return validated[0], nil
}

// AccountStreamArgument는 private account channel 인자를 만든다.
func AccountStreamArgument() StreamArgument {
	return StreamArgument{Channel: "account"}
}

// BalanceAndPositionStreamArgument는 잔고·포지션 통합 private channel 인자를 만든다.
func BalanceAndPositionStreamArgument() StreamArgument {
	return StreamArgument{Channel: "balance_and_position"}
}

// PositionStreamArgument는 SWAP private position channel 인자를 만든다.
func PositionStreamArgument(instrumentID string) (StreamArgument, error) {
	argument := StreamArgument{
		Channel: "positions", InstrumentType: InstrumentTypeSwap, InstrumentID: instrumentID,
	}
	validated, err := validatePrivateStreamArguments([]StreamArgument{argument})
	if err != nil {
		return StreamArgument{}, err
	}
	return validated[0], nil
}

// OrderStreamArgument는 Spot 또는 SWAP private order channel 인자를 만든다.
func OrderStreamArgument(instrumentType InstrumentType, instrumentID string) (StreamArgument, error) {
	argument := StreamArgument{
		Channel: "orders", InstrumentType: instrumentType, InstrumentID: instrumentID,
	}
	validated, err := validatePrivateStreamArguments([]StreamArgument{argument})
	if err != nil {
		return StreamArgument{}, err
	}
	return validated[0], nil
}

func validatePublicStreamArguments(
	endpoint StreamEndpoint,
	arguments []StreamArgument,
) ([]StreamArgument, error) {
	if endpoint != StreamEndpointPublic && endpoint != StreamEndpointBusiness {
		return nil, validationError("stream endpoint must be public or business")
	}
	if len(arguments) == 0 {
		return nil, validationError("at least one public WebSocket channel is required")
	}
	if len(arguments) > maxStreamArguments {
		return nil, validationError("WebSocket channel count cannot exceed %d", maxStreamArguments)
	}
	validated := make([]StreamArgument, 0, len(arguments))
	seen := make(map[string]struct{}, len(arguments))
	for _, argument := range arguments {
		if err := validateArgumentText(argument); err != nil {
			return nil, err
		}
		if err := validateOperationSize([]StreamArgument{argument}); err != nil {
			return nil, err
		}
		if argument.InstrumentType != "" || argument.InstrumentFamily != "" || argument.InstrumentID == "" {
			return nil, validationError("public WebSocket channel requires only instId filter")
		}
		if endpoint == StreamEndpointPublic {
			switch argument.Channel {
			case "tickers", "trades", "books", "books5", "bbo-tbt":
			default:
				return nil, validationError("unsupported public WebSocket channel %q", argument.Channel)
			}
		} else if !validCandleChannel(argument.Channel) {
			return nil, validationError("unsupported business WebSocket channel %q", argument.Channel)
		}
		key := streamArgumentKey(argument)
		if _, exists := seen[key]; exists {
			return nil, validationError("duplicate public WebSocket channel")
		}
		seen[key] = struct{}{}
		validated = append(validated, argument)
	}
	return validated, nil
}

func validatePrivateStreamArguments(arguments []StreamArgument) ([]StreamArgument, error) {
	if len(arguments) == 0 {
		return nil, validationError("at least one private WebSocket channel is required")
	}
	if len(arguments) > maxStreamArguments {
		return nil, validationError("WebSocket channel count cannot exceed %d", maxStreamArguments)
	}
	validated := make([]StreamArgument, 0, len(arguments))
	seen := make(map[string]struct{}, len(arguments))
	for _, argument := range arguments {
		if err := validateArgumentText(argument); err != nil {
			return nil, err
		}
		if err := validateOperationSize([]StreamArgument{argument}); err != nil {
			return nil, err
		}
		switch argument.Channel {
		case "account", "balance_and_position":
			if argument.InstrumentType != "" || argument.InstrumentFamily != "" || argument.InstrumentID != "" {
				return nil, validationError("private %s channel does not accept instrument filters", argument.Channel)
			}
		case "positions":
			if argument.InstrumentType != InstrumentTypeSwap {
				return nil, validationError("private positions channel supports only SWAP")
			}
		case "orders":
			if err := validateInstrumentType(argument.InstrumentType); err != nil {
				return nil, err
			}
		default:
			return nil, validationError("unsupported private WebSocket channel %q", argument.Channel)
		}
		key := streamArgumentKey(argument)
		if _, exists := seen[key]; exists {
			return nil, validationError("duplicate private WebSocket channel")
		}
		seen[key] = struct{}{}
		validated = append(validated, argument)
	}
	return validated, nil
}

func validateArgumentText(argument StreamArgument) error {
	for _, value := range []string{
		argument.Channel, string(argument.InstrumentType), argument.InstrumentFamily, argument.InstrumentID,
	} {
		if strings.TrimSpace(value) != value || strings.ContainsFunc(value, unicode.IsControl) {
			return validationError("WebSocket channel contains invalid whitespace")
		}
	}
	if argument.Channel == "" {
		return validationError("WebSocket channel is required")
	}
	return nil
}

func validateOperationSize(arguments []StreamArgument) error {
	payload, err := json.Marshal(streamOperation{Operation: "subscribe", Arguments: arguments})
	if err != nil {
		return fmt.Errorf("encode OKX stream arguments: %w", err)
	}
	if len(payload) > maxOperationBytes {
		return validationError("WebSocket operation exceeds %d bytes", maxOperationBytes)
	}
	return nil
}

func validCandleChannel(channel string) bool {
	if !strings.HasPrefix(channel, "candle") {
		return false
	}
	return CandleInterval(strings.TrimPrefix(channel, "candle")).valid()
}

func streamArgumentKey(argument StreamArgument) string {
	return argument.Channel + "\x00" + string(argument.InstrumentType) + "\x00" +
		argument.InstrumentFamily + "\x00" + argument.InstrumentID
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

// DecodeStreamMessage는 OKX WebSocket frame을 공통 stream 메시지로 변환한다.
func DecodeStreamMessage(message corestream.Message) (StreamMessage, error) {
	trimmed := strings.TrimSpace(string(message.Data))
	if trimmed == "pong" || trimmed == `"pong"` {
		return StreamMessage{Pong: true, Raw: cloneBytes(message.Data)}, nil
	}
	var envelope struct {
		RequestID    string          `json:"id"`
		Event        string          `json:"event"`
		Code         string          `json:"code"`
		Message      string          `json:"msg"`
		ConnectionID string          `json:"connId"`
		Argument     StreamArgument  `json:"arg"`
		Action       string          `json:"action"`
		Data         json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(message.Data, &envelope); err != nil {
		return StreamMessage{}, fmt.Errorf("decode OKX stream message: %w", err)
	}
	return StreamMessage{
		RequestID: envelope.RequestID, Event: envelope.Event, Code: envelope.Code, Message: envelope.Message,
		ConnectionID: envelope.ConnectionID, Argument: envelope.Argument,
		Action: envelope.Action, Data: cloneBytes(envelope.Data), Raw: cloneBytes(message.Data),
	}, nil
}

type okxConnector struct {
	next corestream.Connector
}

func (connector *okxConnector) Connect(
	ctx context.Context,
	routeID transport.EgressRouteID,
	request corestream.DialRequest,
) (corestream.Connection, error) {
	connection, err := connector.next.Connect(ctx, routeID, request)
	if err != nil {
		return nil, err
	}
	return &okxConnection{next: connection, pong: make(chan struct{}, 1)}, nil
}

type okxConnection struct {
	next corestream.Connection
	pong chan struct{}
}

func (connection *okxConnection) Read(ctx context.Context) (corestream.Message, error) {
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

func (connection *okxConnection) Write(ctx context.Context, message corestream.Message) error {
	return connection.next.Write(ctx, message)
}

func (connection *okxConnection) Ping(ctx context.Context) error {
	select {
	case <-connection.pong:
	default:
	}
	if err := connection.next.Write(
		ctx, corestream.Message{Type: corestream.MessageText, Data: []byte("ping")},
	); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-connection.pong:
		return nil
	}
}

func (connection *okxConnection) Close(code int, reason string) error {
	return connection.next.Close(code, reason)
}

func clearSensitiveBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
