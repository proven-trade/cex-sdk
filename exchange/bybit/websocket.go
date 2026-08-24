package bybit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	trade "github.com/proven-trade/proven-trade-sdk"
	"github.com/proven-trade/proven-trade-sdk/credential"
	"github.com/proven-trade/proven-trade-sdk/model"
	corestream "github.com/proven-trade/proven-trade-sdk/stream"
	"github.com/proven-trade/proven-trade-sdk/transport"
)

const (
	DefaultSpotPublicStreamURL          = "wss://stream.bybit.com/v5/public/spot"
	DefaultLinearPublicStreamURL        = "wss://stream.bybit.com/v5/public/linear"
	DefaultPrivateStreamURL             = "wss://stream.bybit.com/v5/private"
	DefaultTestnetSpotPublicStreamURL   = "wss://stream-testnet.bybit.com/v5/public/spot"
	DefaultTestnetLinearPublicStreamURL = "wss://stream-testnet.bybit.com/v5/public/linear"
	DefaultTestnetPrivateStreamURL      = "wss://stream-testnet.bybit.com/v5/private"
	defaultStreamPingInterval           = 20 * time.Second
	defaultStreamPingTimeout            = 10 * time.Second
	defaultStreamAuthTimeout            = 10 * time.Second
	defaultStreamAuthWindow             = 10 * time.Second
	defaultStreamCommandInterval        = 100 * time.Millisecond
	maxPublicTopics                     = 1000
	maxPublicArgumentCharacters         = 21000
)

var streamSymbolPattern = regexp.MustCompile(`^[A-Z0-9]+$`)

// StreamClientConfig는 Bybit V5 public/private WebSocket 설정이다.
type StreamClientConfig struct {
	Connector              corestream.Connector
	Credentials            *credential.Descriptor
	CredentialProvider     credential.Provider
	DefaultEgressRouteID   transport.EgressRouteID
	SpotPublicStreamURL    string
	LinearPublicStreamURL  string
	PrivateStreamURL       string
	Testnet                bool
	AllowInsecureWebSocket bool
	Now                    func() time.Time
	Observer               corestream.StateObserver
	ReconnectPolicy        corestream.ReconnectPolicy
	Backoff                corestream.Backoff
	MaxReconnectAttempts   int
	PingInterval           time.Duration
	PingTimeout            time.Duration
	AuthenticationTimeout  time.Duration
	AuthenticationWindow   time.Duration
	SubscriptionInterval   time.Duration
}

// StreamClient는 Bybit V5 WebSocket 세션을 생성한다.
type StreamClient struct {
	connector            corestream.Connector
	credentials          *credential.Descriptor
	credentialProvider   credential.Provider
	defaultRouteID       transport.EgressRouteID
	spotPublicURL        string
	linearPublicURL      string
	privateURL           string
	now                  func() time.Time
	observer             corestream.StateObserver
	reconnectPolicy      corestream.ReconnectPolicy
	backoff              corestream.Backoff
	maxReconnectAttempts int
	pingInterval         time.Duration
	pingTimeout          time.Duration
	authTimeout          time.Duration
	authWindow           time.Duration
	subscriptionInterval time.Duration
}

// NewStreamClient는 Bybit V5 WebSocket 클라이언트를 생성한다.
func NewStreamClient(config StreamClientConfig) (*StreamClient, error) {
	if config.Connector == nil {
		return nil, fmt.Errorf("Bybit stream connector is required")
	}
	defaultRouteID := transport.EgressRouteID(strings.TrimSpace(string(config.DefaultEgressRouteID)))
	if defaultRouteID == "" {
		return nil, trade.ErrMissingEgressRoute
	}
	setDefaultStreamURLs(&config)
	spotPublicURL, err := validateWebSocketURL(config.SpotPublicStreamURL, config.AllowInsecureWebSocket)
	if err != nil {
		return nil, fmt.Errorf("invalid Bybit Spot public stream URL: %w", err)
	}
	linearPublicURL, err := validateWebSocketURL(config.LinearPublicStreamURL, config.AllowInsecureWebSocket)
	if err != nil {
		return nil, fmt.Errorf("invalid Bybit Linear public stream URL: %w", err)
	}
	privateURL, err := validateWebSocketURL(config.PrivateStreamURL, config.AllowInsecureWebSocket)
	if err != nil {
		return nil, fmt.Errorf("invalid Bybit private stream URL: %w", err)
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
	if config.AuthenticationTimeout == 0 {
		config.AuthenticationTimeout = defaultStreamAuthTimeout
	}
	if config.AuthenticationWindow == 0 {
		config.AuthenticationWindow = defaultStreamAuthWindow
	}
	if config.SubscriptionInterval == 0 {
		config.SubscriptionInterval = defaultStreamCommandInterval
	}
	if config.PingInterval < 0 || config.PingTimeout < 0 || config.AuthenticationTimeout <= 0 ||
		config.AuthenticationWindow <= 0 || config.AuthenticationWindow > time.Minute ||
		config.AuthenticationWindow%time.Millisecond != 0 || config.SubscriptionInterval < 0 {
		return nil, fmt.Errorf("Bybit stream durations are invalid")
	}
	if config.MaxReconnectAttempts < 0 {
		return nil, fmt.Errorf("Bybit maximum reconnect attempts cannot be negative")
	}

	var credentialsCopy *credential.Descriptor
	if config.Credentials != nil {
		if err := config.Credentials.Validate(); err != nil {
			return nil, err
		}
		if config.Credentials.Exchange != model.ExchangeBybit {
			return nil, fmt.Errorf("credential exchange must be Bybit")
		}
		if config.CredentialProvider == nil {
			return nil, fmt.Errorf("credential provider is required for private Bybit streams")
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
		connector:            &bybitConnector{next: config.Connector},
		credentials:          credentialsCopy,
		credentialProvider:   config.CredentialProvider,
		defaultRouteID:       defaultRouteID,
		spotPublicURL:        spotPublicURL,
		linearPublicURL:      linearPublicURL,
		privateURL:           privateURL,
		now:                  config.Now,
		observer:             config.Observer,
		reconnectPolicy:      config.ReconnectPolicy,
		backoff:              config.Backoff,
		maxReconnectAttempts: config.MaxReconnectAttempts,
		pingInterval:         config.PingInterval,
		pingTimeout:          config.PingTimeout,
		authTimeout:          config.AuthenticationTimeout,
		authWindow:           config.AuthenticationWindow,
		subscriptionInterval: config.SubscriptionInterval,
	}, nil
}

func setDefaultStreamURLs(config *StreamClientConfig) {
	if config.SpotPublicStreamURL == "" {
		config.SpotPublicStreamURL = DefaultSpotPublicStreamURL
		if config.Testnet {
			config.SpotPublicStreamURL = DefaultTestnetSpotPublicStreamURL
		}
	}
	if config.LinearPublicStreamURL == "" {
		config.LinearPublicStreamURL = DefaultLinearPublicStreamURL
		if config.Testnet {
			config.LinearPublicStreamURL = DefaultTestnetLinearPublicStreamURL
		}
	}
	if config.PrivateStreamURL == "" {
		config.PrivateStreamURL = DefaultPrivateStreamURL
		if config.Testnet {
			config.PrivateStreamURL = DefaultTestnetPrivateStreamURL
		}
	}
}

type managedStream struct {
	session  *corestream.Session
	category Category
	private  bool

	mu     sync.Mutex
	topics map[string]struct{}

	commandMu     sync.Mutex
	lastCommandAt time.Time
	interval      time.Duration
	nextID        atomic.Uint64
}

// PublicStream은 Bybit public topic 연결을 관리한다.
type PublicStream struct {
	managed *managedStream
}

// PublicStream은 선택한 EIP route와 상품 category에 고정된 public 세션을 생성한다.
func (client *StreamClient) PublicStream(
	request PublicStreamRequest,
	options ...trade.RequestOption,
) (*PublicStream, error) {
	topics, err := validatePublicTopics(request.Category, request.Topics)
	if err != nil {
		return nil, err
	}
	routeID, err := client.resolveRoute(options...)
	if err != nil {
		return nil, err
	}
	endpoint := client.spotPublicURL
	if request.Category == CategoryLinear {
		endpoint = client.linearPublicURL
	}
	managed := newManagedStream(request.Category, topics, false, client.subscriptionInterval)
	session, err := corestream.NewSession(corestream.SessionConfig{
		Connector:            client.connector,
		EgressRouteID:        routeID,
		Request:              corestream.DialRequest{Endpoint: endpoint},
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
	return &PublicStream{managed: managed}, nil
}

// Run은 public 메시지를 순서대로 decode해 handler에 전달한다.
func (public *PublicStream) Run(ctx context.Context, handler func(context.Context, StreamMessage) error) error {
	if handler == nil {
		return fmt.Errorf("Bybit public stream handler is required")
	}
	return public.managed.run(ctx, handler)
}

// Subscribe는 public topic을 추가하고 재연결 목록에도 반영한다.
func (public *PublicStream) Subscribe(ctx context.Context, topics ...string) error {
	validated, err := validatePublicTopics(public.managed.category, topics)
	if err != nil {
		return err
	}
	return public.managed.change(ctx, "subscribe", validated)
}

// Unsubscribe는 public topic을 제거하고 재연결 목록에도 반영한다.
func (public *PublicStream) Unsubscribe(ctx context.Context, topics ...string) error {
	validated, err := validatePublicTopics(public.managed.category, topics)
	if err != nil {
		return err
	}
	return public.managed.change(ctx, "unsubscribe", validated)
}

// Close는 public stream 세션을 종료한다.
func (public *PublicStream) Close() error { return public.managed.session.Close() }

// Generation은 성공한 public stream 연결 세대 번호를 반환한다.
func (public *PublicStream) Generation() uint64 { return public.managed.session.Generation() }

// PrivateStream은 Bybit private topic 연결을 관리한다.
type PrivateStream struct {
	managed *managedStream
}

// PrivateStream은 인증 후 계정 topic을 구독하는 private 세션을 생성한다.
func (client *StreamClient) PrivateStream(
	request PrivateStreamRequest,
	options ...trade.RequestOption,
) (*PrivateStream, error) {
	topics, err := validatePrivateTopics(request.Topics)
	if err != nil {
		return nil, err
	}
	routeID, err := client.resolveRoute(options...)
	if err != nil {
		return nil, err
	}
	if client.credentials == nil || client.credentialProvider == nil {
		return nil, client.authenticationError(errors.New("private Bybit stream requires credentials"))
	}
	if err := client.credentials.RequireEgressRoute(routeID); err != nil {
		return nil, client.authorizationError(err)
	}
	if err := client.credentials.RequirePermission(credential.PermissionRead); err != nil {
		return nil, client.authorizationError(err)
	}
	managed := newManagedStream("", topics, true, client.subscriptionInterval)
	session, err := corestream.NewSession(corestream.SessionConfig{
		Connector:     client.connector,
		EgressRouteID: routeID,
		Request:       corestream.DialRequest{Endpoint: client.privateURL},
		OnConnect: func(ctx context.Context, connection corestream.Connection) error {
			return client.authenticateAndSubscribe(ctx, connection, managed)
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
		return fmt.Errorf("Bybit private stream handler is required")
	}
	return private.managed.run(ctx, handler)
}

// Close는 private stream 세션을 종료한다.
func (private *PrivateStream) Close() error { return private.managed.session.Close() }

// Generation은 성공한 private stream 연결 세대 번호를 반환한다.
func (private *PrivateStream) Generation() uint64 { return private.managed.session.Generation() }

func newManagedStream(category Category, topics []string, private bool, interval time.Duration) *managedStream {
	managed := &managedStream{
		category: category, private: private, topics: make(map[string]struct{}, len(topics)), interval: interval,
	}
	for _, topic := range topics {
		managed.topics[topic] = struct{}{}
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
	return managed.writeTopics(ctx, connection, "subscribe", managed.snapshotTopics())
}

func (managed *managedStream) change(ctx context.Context, operation string, topics []string) error {
	managed.commandMu.Lock()
	defer managed.commandMu.Unlock()
	managed.mu.Lock()
	selected := make([]string, 0, len(topics))
	for _, topic := range topics {
		_, exists := managed.topics[topic]
		if (operation == "subscribe" && !exists) || (operation == "unsubscribe" && exists) {
			selected = append(selected, topic)
		}
	}
	if operation == "subscribe" && len(managed.topics)+len(selected) > maxPublicTopics {
		managed.mu.Unlock()
		return validationError("WebSocket topic count cannot exceed %d", maxPublicTopics)
	}
	if operation == "subscribe" {
		totalCharacters := 0
		for topic := range managed.topics {
			totalCharacters += len(topic)
		}
		for _, topic := range selected {
			totalCharacters += len(topic)
		}
		if totalCharacters > maxPublicArgumentCharacters {
			managed.mu.Unlock()
			return validationError(
				"public WebSocket topic arguments exceed %d characters",
				maxPublicArgumentCharacters,
			)
		}
	}
	managed.mu.Unlock()
	if len(selected) == 0 {
		return nil
	}
	if err := managed.writeTopics(ctx, managed.session, operation, selected); err != nil {
		return err
	}
	managed.mu.Lock()
	for _, topic := range selected {
		if operation == "subscribe" {
			managed.topics[topic] = struct{}{}
		} else {
			delete(managed.topics, topic)
		}
	}
	managed.mu.Unlock()
	return nil
}

func (managed *managedStream) snapshotTopics() []string {
	managed.mu.Lock()
	defer managed.mu.Unlock()
	topics := make([]string, 0, len(managed.topics))
	for topic := range managed.topics {
		topics = append(topics, topic)
	}
	sort.Strings(topics)
	return topics
}

func (managed *managedStream) writeTopics(
	ctx context.Context,
	writer streamWriter,
	operation string,
	topics []string,
) error {
	if len(topics) == 0 {
		return nil
	}
	batchSize := 10
	if !managed.private && managed.category == CategoryLinear {
		batchSize = maxPublicTopics
	}
	for start := 0; start < len(topics); start += batchSize {
		end := min(start+batchSize, len(topics))
		if err := managed.waitCommandSlot(ctx); err != nil {
			return err
		}
		requestID := strconv.FormatUint(managed.nextID.Add(1), 10)
		if err := writeStreamOperation(ctx, writer, requestID, operation, topics[start:end]); err != nil {
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
	RequestID string   `json:"req_id,omitempty"`
	Operation string   `json:"op"`
	Arguments []string `json:"args"`
}

type streamWriter interface {
	Write(context.Context, corestream.Message) error
}

func writeStreamOperation(
	ctx context.Context,
	writer streamWriter,
	requestID, operation string,
	topics []string,
) error {
	payload, err := json.Marshal(streamOperation{
		RequestID: requestID, Operation: operation, Arguments: topics,
	})
	if err != nil {
		return fmt.Errorf("encode Bybit stream operation: %w", err)
	}
	return writer.Write(ctx, corestream.Message{Type: corestream.MessageText, Data: payload})
}

func (client *StreamClient) authenticateAndSubscribe(
	ctx context.Context,
	connection corestream.Connection,
	managed *managedStream,
) error {
	material, err := client.credentialProvider.Resolve(ctx, client.credentials.SecretRef)
	defer material.Destroy()
	if err != nil {
		return client.authenticationError(err)
	}
	if len(material.APIKey) == 0 || len(material.SecretKey) == 0 {
		return client.authenticationError(errors.New("Bybit API key and HMAC secret are required"))
	}
	expires := client.now().Add(client.authWindow).UnixMilli()
	signature, err := SignHMACSHA256(
		material.SecretKey, []byte("GET/realtime"+strconv.FormatInt(expires, 10)),
	)
	if err != nil {
		return client.authenticationError(err)
	}
	payload, err := json.Marshal(struct {
		Operation string `json:"op"`
		Arguments []any  `json:"args"`
	}{
		Operation: "auth",
		Arguments: []any{string(material.APIKey), expires, signature},
	})
	if err != nil {
		return fmt.Errorf("encode Bybit stream authentication: %w", err)
	}
	defer clearSensitiveBytes(payload)
	if err := connection.Write(ctx, corestream.Message{Type: corestream.MessageText, Data: payload}); err != nil {
		return err
	}
	authContext, cancel := context.WithTimeout(ctx, client.authTimeout)
	defer cancel()
	response, err := connection.Read(authContext)
	if err != nil {
		return err
	}
	decoded, err := DecodeStreamMessage(response)
	if err != nil {
		return err
	}
	if decoded.Operation != "auth" || decoded.Success == nil || !*decoded.Success {
		return &StreamAuthError{Message: decoded.ReturnMessage}
	}
	return managed.resubscribe(ctx, connection)
}

func (client *StreamClient) privateReconnectPolicy(err error) bool {
	var authError *StreamAuthError
	if errors.As(err, &authError) {
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
		return "", fmt.Errorf("Bybit stream timeout must be controlled by Run context")
	}
	return resolved.EgressRouteID, nil
}

func (client *StreamClient) authenticationError(cause error) error {
	accountID := ""
	if client.credentials != nil {
		accountID = client.credentials.AccountID
	}
	return &trade.APIError{
		Category: trade.ErrorAuthentication, Exchange: model.ExchangeBybit,
		AccountID: accountID, Cause: cause,
	}
}

func (client *StreamClient) authorizationError(cause error) error {
	accountID := ""
	if client.credentials != nil {
		accountID = client.credentials.AccountID
	}
	return &trade.APIError{
		Category: trade.ErrorAuthorization, Exchange: model.ExchangeBybit,
		AccountID: accountID, Cause: cause,
	}
}

// TickerStreamTopic은 ticker topic 이름을 만든다.
func TickerStreamTopic(category Category, symbol string) (string, error) {
	return buildPublicTopic(category, "tickers."+symbol)
}

// PublicTradeStreamTopic은 공개 체결 topic 이름을 만든다.
func PublicTradeStreamTopic(category Category, symbol string) (string, error) {
	return buildPublicTopic(category, "publicTrade."+symbol)
}

// OrderBookStreamTopic은 지정한 depth의 호가 topic 이름을 만든다.
func OrderBookStreamTopic(category Category, symbol string, depth int) (string, error) {
	return buildPublicTopic(category, "orderbook."+strconv.Itoa(depth)+"."+symbol)
}

// KlineStreamTopic은 지정한 구간의 캔들 topic 이름을 만든다.
func KlineStreamTopic(category Category, symbol string, interval CandleInterval) (string, error) {
	return buildPublicTopic(category, "kline."+string(interval)+"."+symbol)
}

// PrivateStreamTopic은 지원하는 private topic 이름을 검증해 반환한다.
func PrivateStreamTopic(topic string) (string, error) {
	validated, err := validatePrivateTopics([]string{topic})
	if err != nil {
		return "", err
	}
	return validated[0], nil
}

func buildPublicTopic(category Category, topic string) (string, error) {
	validated, err := validatePublicTopics(category, []string{topic})
	if err != nil {
		return "", err
	}
	return validated[0], nil
}

func validatePublicTopics(category Category, topics []string) ([]string, error) {
	if err := validateCategory(category); err != nil {
		return nil, err
	}
	if len(topics) == 0 {
		return nil, validationError("at least one public WebSocket topic is required")
	}
	if len(topics) > maxPublicTopics {
		return nil, validationError("WebSocket topic count cannot exceed %d", maxPublicTopics)
	}
	validated := make([]string, 0, len(topics))
	seen := make(map[string]struct{}, len(topics))
	totalCharacters := 0
	for _, topic := range topics {
		if err := validatePublicTopic(topic); err != nil {
			return nil, err
		}
		if _, exists := seen[topic]; exists {
			return nil, validationError("duplicate public WebSocket topic %q", topic)
		}
		seen[topic] = struct{}{}
		totalCharacters += len(topic)
		validated = append(validated, topic)
	}
	if totalCharacters > maxPublicArgumentCharacters {
		return nil, validationError("public WebSocket topic arguments exceed %d characters", maxPublicArgumentCharacters)
	}
	return validated, nil
}

func validatePublicTopic(topic string) error {
	if topic == "" || strings.TrimSpace(topic) != topic || strings.ContainsFunc(topic, unicode.IsControl) {
		return validationError("public WebSocket topic contains invalid whitespace")
	}
	parts := strings.Split(topic, ".")
	switch parts[0] {
	case "tickers", "publicTrade":
		if len(parts) != 2 {
			return validationError("invalid public WebSocket topic %q", topic)
		}
	case "orderbook":
		if len(parts) != 3 || (parts[1] != "1" && parts[1] != "50" && parts[1] != "200" && parts[1] != "1000") {
			return validationError("invalid orderbook WebSocket topic %q", topic)
		}
	case "kline":
		if len(parts) != 3 || !CandleInterval(parts[1]).valid() {
			return validationError("invalid kline WebSocket topic %q", topic)
		}
	default:
		return validationError("unsupported public WebSocket topic %q", topic)
	}
	symbol := parts[len(parts)-1]
	if !streamSymbolPattern.MatchString(symbol) {
		return validationError("WebSocket symbol must contain uppercase letters and digits")
	}
	return nil
}

func validatePrivateTopics(topics []string) ([]string, error) {
	if len(topics) == 0 {
		return nil, validationError("at least one private WebSocket topic is required")
	}
	validated := make([]string, 0, len(topics))
	seen := make(map[string]struct{}, len(topics))
	allInOne := make(map[string]bool)
	categorized := make(map[string]bool)
	for _, topic := range topics {
		if strings.TrimSpace(topic) != topic || strings.ContainsFunc(topic, unicode.IsControl) {
			return nil, validationError("private WebSocket topic contains invalid whitespace")
		}
		base := topic
		switch topic {
		case "wallet", "position", "order", "execution":
			allInOne[base] = true
		case "position.linear", "order.spot", "order.linear", "execution.spot", "execution.linear":
			base = strings.SplitN(topic, ".", 2)[0]
			categorized[base] = true
		default:
			return nil, validationError("unsupported private WebSocket topic %q", topic)
		}
		if _, exists := seen[topic]; exists {
			return nil, validationError("duplicate private WebSocket topic %q", topic)
		}
		seen[topic] = struct{}{}
		validated = append(validated, topic)
	}
	for base := range allInOne {
		if categorized[base] {
			return nil, validationError("all-in-one and categorised %s topics cannot be mixed", base)
		}
	}
	return validated, nil
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

// DecodeStreamMessage는 Bybit WebSocket frame을 공통 stream 메시지로 변환한다.
func DecodeStreamMessage(message corestream.Message) (StreamMessage, error) {
	var envelope struct {
		Topic         string          `json:"topic"`
		Type          string          `json:"type"`
		Timestamp     int64           `json:"ts"`
		CreationTime  int64           `json:"creationTime"`
		Operation     string          `json:"op"`
		RequestID     string          `json:"req_id"`
		ConnectionID  string          `json:"conn_id"`
		Success       *bool           `json:"success"`
		ReturnMessage string          `json:"ret_msg"`
		Data          json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(message.Data, &envelope); err != nil {
		return StreamMessage{}, fmt.Errorf("decode Bybit stream message: %w", err)
	}
	return StreamMessage{
		Topic: envelope.Topic, Type: envelope.Type, Timestamp: envelope.Timestamp,
		CreationTime: envelope.CreationTime, Operation: envelope.Operation,
		RequestID: envelope.RequestID, ConnectionID: envelope.ConnectionID,
		Success: envelope.Success, ReturnMessage: envelope.ReturnMessage,
		Data: cloneBytes(envelope.Data),
		Pong: envelope.Operation == "pong" || strings.EqualFold(envelope.ReturnMessage, "pong"),
		Raw:  cloneBytes(message.Data),
	}, nil
}

type bybitConnector struct {
	next corestream.Connector
}

func (connector *bybitConnector) Connect(
	ctx context.Context,
	routeID transport.EgressRouteID,
	request corestream.DialRequest,
) (corestream.Connection, error) {
	connection, err := connector.next.Connect(ctx, routeID, request)
	if err != nil {
		return nil, err
	}
	return &bybitConnection{next: connection, pong: make(chan struct{}, 1)}, nil
}

type bybitConnection struct {
	next corestream.Connection
	pong chan struct{}
}

func (connection *bybitConnection) Read(ctx context.Context) (corestream.Message, error) {
	message, err := connection.next.Read(ctx)
	if err == nil {
		var response struct {
			Operation     string `json:"op"`
			ReturnMessage string `json:"ret_msg"`
		}
		if json.Unmarshal(message.Data, &response) == nil &&
			(response.Operation == "pong" || strings.EqualFold(response.ReturnMessage, "pong")) {
			select {
			case connection.pong <- struct{}{}:
			default:
			}
		}
	}
	return message, err
}

func (connection *bybitConnection) Write(ctx context.Context, message corestream.Message) error {
	return connection.next.Write(ctx, message)
}

func (connection *bybitConnection) Ping(ctx context.Context) error {
	select {
	case <-connection.pong:
	default:
	}
	if err := connection.next.Write(
		ctx, corestream.Message{Type: corestream.MessageText, Data: []byte(`{"op":"ping"}`)},
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

func (connection *bybitConnection) Close(code int, reason string) error {
	return connection.next.Close(code, reason)
}

func clearSensitiveBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
