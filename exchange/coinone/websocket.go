package coinone

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	"github.com/proven-trade/cex-sdk/model"
	corestream "github.com/proven-trade/cex-sdk/stream"
	"github.com/proven-trade/cex-sdk/transport"
)

const (
	DefaultPublicWebSocketURL  = "wss://stream.coinone.co.kr"
	DefaultPrivateWebSocketURL = "wss://stream.coinone.co.kr/v1/private"
	defaultStreamPingInterval  = 10 * time.Minute
	defaultStreamPingTimeout   = 10 * time.Second
	maxStreamSubscriptions     = 1000
)

// StreamClientConfig는 코인원 public/private WebSocket 설정이다.
type StreamClientConfig struct {
	Connector              corestream.Connector
	Credentials            *credential.Descriptor
	CredentialProvider     credential.Provider
	DefaultEgressRouteID   transport.EgressRouteID
	PublicWebSocketURL     string
	PrivateWebSocketURL    string
	AllowInsecureWebSocket bool
	NonceSource            NonceSource
	Now                    func() time.Time
	Observer               corestream.StateObserver
	ReconnectPolicy        corestream.ReconnectPolicy
	Backoff                corestream.Backoff
	MaxReconnectAttempts   int
	PingInterval           time.Duration
	PingTimeout            time.Duration
}

// StreamClient는 코인원 public/private WebSocket 세션을 생성한다.
type StreamClient struct {
	connector            corestream.Connector
	credentials          *credential.Descriptor
	credentialProvider   credential.Provider
	defaultRouteID       transport.EgressRouteID
	publicURL            string
	privateURL           string
	nonceSource          NonceSource
	now                  func() time.Time
	observer             corestream.StateObserver
	reconnectPolicy      corestream.ReconnectPolicy
	backoff              corestream.Backoff
	maxReconnectAttempts int
	pingInterval         time.Duration
	pingTimeout          time.Duration
}

// NewStreamClient는 코인원 WebSocket 클라이언트를 생성한다.
func NewStreamClient(config StreamClientConfig) (*StreamClient, error) {
	if config.Connector == nil {
		return nil, fmt.Errorf("Coinone stream connector is required")
	}
	defaultRouteID := transport.EgressRouteID(strings.TrimSpace(string(config.DefaultEgressRouteID)))
	if defaultRouteID == "" {
		return nil, trade.ErrMissingEgressRoute
	}
	if config.PublicWebSocketURL == "" {
		config.PublicWebSocketURL = DefaultPublicWebSocketURL
	}
	if config.PrivateWebSocketURL == "" {
		config.PrivateWebSocketURL = DefaultPrivateWebSocketURL
	}
	publicURL, err := validateWebSocketURL(config.PublicWebSocketURL, config.AllowInsecureWebSocket)
	if err != nil {
		return nil, fmt.Errorf("invalid Coinone public WebSocket URL: %w", err)
	}
	privateURL, err := validateWebSocketURL(config.PrivateWebSocketURL, config.AllowInsecureWebSocket)
	if err != nil {
		return nil, fmt.Errorf("invalid Coinone private WebSocket URL: %w", err)
	}
	if config.NonceSource == nil {
		config.NonceSource = randomNonce
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
	if config.PingInterval < 0 || config.PingTimeout < 0 || config.MaxReconnectAttempts < 0 {
		return nil, fmt.Errorf("Coinone stream durations or reconnect attempts are invalid")
	}

	var credentialsCopy *credential.Descriptor
	if config.Credentials != nil {
		if err := config.Credentials.Validate(); err != nil {
			return nil, err
		}
		if config.Credentials.Exchange != model.ExchangeCoinone {
			return nil, fmt.Errorf("credential exchange must be Coinone")
		}
		if config.CredentialProvider == nil {
			return nil, fmt.Errorf("credential provider is required for private Coinone streams")
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
		connector:   &coinoneConnector{next: config.Connector},
		credentials: credentialsCopy, credentialProvider: config.CredentialProvider,
		defaultRouteID: defaultRouteID, publicURL: publicURL, privateURL: privateURL,
		nonceSource: config.NonceSource, now: config.Now, observer: config.Observer,
		reconnectPolicy: config.ReconnectPolicy, backoff: config.Backoff,
		maxReconnectAttempts: config.MaxReconnectAttempts,
		pingInterval:         config.PingInterval, pingTimeout: config.PingTimeout,
	}, nil
}

type managedStream struct {
	session *corestream.Session
	private bool

	mu            sync.Mutex
	subscriptions map[string]StreamSubscription
	commandMu     sync.Mutex
}

// PublicStream은 코인원 public 시세 WebSocket 연결을 관리한다.
type PublicStream struct{ managed *managedStream }

// PublicStream은 선택한 송신 경로에 고정된 public 시세 세션을 생성한다.
func (client *StreamClient) PublicStream(
	request StreamRequest,
	options ...trade.RequestOption,
) (*PublicStream, error) {
	subscriptions, err := validateStreamSubscriptions(request.Subscriptions, false, true)
	if err != nil {
		return nil, err
	}
	routeID, err := client.resolveRoute(options...)
	if err != nil {
		return nil, err
	}
	managed := newManagedStream(subscriptions, false)
	session, err := client.newSession(
		routeID, corestream.DialRequest{Endpoint: client.publicURL}, nil,
		managed.resubscribe, client.reconnectPolicy,
	)
	if err != nil {
		return nil, err
	}
	managed.session = session
	return &PublicStream{managed: managed}, nil
}

// Run은 public 시세 메시지를 순서대로 decode해 handler에 전달한다.
func (public *PublicStream) Run(ctx context.Context, handler func(context.Context, StreamMessage) error) error {
	return public.managed.run(ctx, handler)
}

// Subscribe는 public 구독을 추가하고 재연결 복구 목록에도 반영한다.
func (public *PublicStream) Subscribe(ctx context.Context, subscriptions ...StreamSubscription) error {
	validated, err := validateStreamSubscriptions(subscriptions, false, true)
	if err != nil {
		return err
	}
	return public.managed.change(ctx, "SUBSCRIBE", validated)
}

// Unsubscribe는 public 구독을 제거하고 재연결 복구 목록에도 반영한다.
func (public *PublicStream) Unsubscribe(ctx context.Context, subscriptions ...StreamSubscription) error {
	validated, err := validateStreamSubscriptions(subscriptions, false, true)
	if err != nil {
		return err
	}
	return public.managed.change(ctx, "UNSUBSCRIBE", validated)
}

// Close는 public stream 세션을 종료한다.
func (public *PublicStream) Close() error { return public.managed.session.Close() }

// Generation은 성공한 public 연결 세대 번호를 반환한다.
func (public *PublicStream) Generation() uint64 { return public.managed.session.Generation() }

// EgressRouteID는 public stream 연결과 재연결에 고정된 송신 경로를 반환한다.
func (public *PublicStream) EgressRouteID() transport.EgressRouteID {
	return public.managed.session.EgressRouteID()
}

func (public *PublicStream) hasOrderBookSubscription(quoteCurrency, targetCurrency string) bool {
	public.managed.mu.Lock()
	defer public.managed.mu.Unlock()
	for _, subscription := range public.managed.subscriptions {
		if subscription.Channel != StreamChannelOrderBook || len(subscription.Topics) != 1 {
			continue
		}
		topic := subscription.Topics[0]
		if topic.QuoteCurrency == quoteCurrency && topic.TargetCurrency == targetCurrency {
			return true
		}
	}
	return false
}

// PrivateStream은 코인원 private 내 주문·자산 WebSocket 연결을 관리한다.
type PrivateStream struct{ managed *managedStream }

// PrivateStream은 인증 handshake 후 선택한 송신 경로에 고정된 private 세션을 생성한다.
func (client *StreamClient) PrivateStream(
	request StreamRequest,
	options ...trade.RequestOption,
) (*PrivateStream, error) {
	subscriptions, err := validateStreamSubscriptions(request.Subscriptions, true, true)
	if err != nil {
		return nil, err
	}
	routeID, err := client.resolveRoute(options...)
	if err != nil {
		return nil, err
	}
	if client.credentials == nil || client.credentialProvider == nil {
		return nil, client.authenticationError(errors.New("private Coinone stream requires credentials"))
	}
	if err := client.credentials.RequireEgressRoute(routeID); err != nil {
		return nil, client.authorizationError(err)
	}
	if err := client.credentials.RequirePermission(credential.PermissionRead); err != nil {
		return nil, client.authorizationError(err)
	}
	managed := newManagedStream(subscriptions, true)
	session, err := client.newSession(
		routeID, corestream.DialRequest{}, client.privateDialRequest,
		managed.resubscribe, client.privateReconnectPolicy,
	)
	if err != nil {
		return nil, err
	}
	managed.session = session
	return &PrivateStream{managed: managed}, nil
}

// Run은 private 주문·자산 메시지를 순서대로 decode해 handler에 전달한다.
func (private *PrivateStream) Run(ctx context.Context, handler func(context.Context, StreamMessage) error) error {
	return private.managed.run(ctx, handler)
}

// Subscribe는 private 구독을 추가하고 재연결 복구 목록에도 반영한다.
func (private *PrivateStream) Subscribe(ctx context.Context, subscriptions ...StreamSubscription) error {
	validated, err := validateStreamSubscriptions(subscriptions, true, true)
	if err != nil {
		return err
	}
	return private.managed.change(ctx, "SUBSCRIBE", validated)
}

// Unsubscribe는 private 구독을 제거하고 재연결 복구 목록에도 반영한다.
func (private *PrivateStream) Unsubscribe(ctx context.Context, subscriptions ...StreamSubscription) error {
	validated, err := validateStreamSubscriptions(subscriptions, true, true)
	if err != nil {
		return err
	}
	return private.managed.change(ctx, "UNSUBSCRIBE", validated)
}

// Close는 private stream 세션을 종료한다.
func (private *PrivateStream) Close() error { return private.managed.session.Close() }

// Generation은 성공한 private 연결 세대 번호를 반환한다.
func (private *PrivateStream) Generation() uint64 { return private.managed.session.Generation() }

func (client *StreamClient) newSession(
	routeID transport.EgressRouteID,
	request corestream.DialRequest,
	requestSource corestream.DialRequestSource,
	onConnect corestream.ConnectHook,
	reconnectPolicy corestream.ReconnectPolicy,
) (*corestream.Session, error) {
	return corestream.NewSession(corestream.SessionConfig{
		Connector: client.connector, EgressRouteID: routeID,
		Request: request, RequestSource: requestSource, OnConnect: onConnect,
		Observer: client.observer, ReconnectPolicy: reconnectPolicy, Backoff: client.backoff,
		MaxReconnectAttempts: client.maxReconnectAttempts,
		PingInterval:         client.pingInterval, PingTimeout: client.pingTimeout,
	})
}

func newManagedStream(subscriptions []StreamSubscription, private bool) *managedStream {
	managed := &managedStream{
		private: private, subscriptions: make(map[string]StreamSubscription, len(subscriptions)),
	}
	for _, subscription := range subscriptions {
		managed.subscriptions[streamSubscriptionKey(subscription)] = subscription
	}
	return managed
}

func (managed *managedStream) run(ctx context.Context, handler func(context.Context, StreamMessage) error) error {
	if handler == nil {
		return fmt.Errorf("Coinone stream handler is required")
	}
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
	for _, subscription := range managed.snapshotSubscriptions() {
		if err := writeStreamCommand(ctx, connection, "SUBSCRIBE", subscription, managed.private); err != nil {
			return err
		}
	}
	return nil
}

func (managed *managedStream) change(
	ctx context.Context,
	operation string,
	subscriptions []StreamSubscription,
) error {
	if ctx == nil {
		return fmt.Errorf("Coinone stream command context is nil")
	}
	managed.commandMu.Lock()
	defer managed.commandMu.Unlock()
	for _, subscription := range subscriptions {
		key := streamSubscriptionKey(subscription)
		managed.mu.Lock()
		_, exists := managed.subscriptions[key]
		count := len(managed.subscriptions)
		managed.mu.Unlock()
		if operation == "SUBSCRIBE" && exists || operation == "UNSUBSCRIBE" && !exists {
			continue
		}
		if operation == "SUBSCRIBE" && count >= maxStreamSubscriptions {
			return validationError("WebSocket subscription count cannot exceed %d", maxStreamSubscriptions)
		}
		payload, err := encodeStreamCommand(operation, subscription, managed.private)
		if err != nil {
			return err
		}
		if err := managed.session.Write(
			ctx, corestream.Message{Type: corestream.MessageText, Data: payload},
		); err != nil {
			return err
		}
		managed.mu.Lock()
		if operation == "SUBSCRIBE" {
			managed.subscriptions[key] = subscription
		} else {
			delete(managed.subscriptions, key)
		}
		managed.mu.Unlock()
	}
	return nil
}

func (managed *managedStream) snapshotSubscriptions() []StreamSubscription {
	managed.mu.Lock()
	defer managed.mu.Unlock()
	keys := make([]string, 0, len(managed.subscriptions))
	for key := range managed.subscriptions {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]StreamSubscription, 0, len(keys))
	for _, key := range keys {
		result = append(result, managed.subscriptions[key])
	}
	return result
}

func writeStreamCommand(
	ctx context.Context,
	connection corestream.Connection,
	operation string,
	subscription StreamSubscription,
	private bool,
) error {
	payload, err := encodeStreamCommand(operation, subscription, private)
	if err != nil {
		return err
	}
	return connection.Write(ctx, corestream.Message{Type: corestream.MessageText, Data: payload})
}

func encodeStreamCommand(
	operation string,
	subscription StreamSubscription,
	private bool,
) ([]byte, error) {
	var topic any
	if private {
		if subscription.Channel == StreamChannelMyOrder && len(subscription.Topics) > 0 {
			topic = subscription.Topics
		}
	} else {
		topic = subscription.Topics[0]
	}
	wireFormat := subscription.Format
	if wireFormat == StreamFormatDefault {
		wireFormat = ""
	}
	payload, err := json.Marshal(struct {
		RequestType string        `json:"request_type"`
		Channel     StreamChannel `json:"channel"`
		Topic       any           `json:"topic,omitempty"`
		Format      StreamFormat  `json:"format,omitempty"`
	}{
		RequestType: operation, Channel: subscription.Channel,
		Topic: topic, Format: wireFormat,
	})
	if err != nil {
		return nil, fmt.Errorf("encode Coinone stream command: %w", err)
	}
	return payload, nil
}

func (client *StreamClient) privateDialRequest(ctx context.Context) (corestream.DialRequest, error) {
	material, err := client.credentialProvider.Resolve(ctx, client.credentials.SecretRef)
	defer material.Destroy()
	if err != nil {
		return corestream.DialRequest{}, client.authenticationError(err)
	}
	if len(material.APIKey) == 0 || len(material.SecretKey) == 0 {
		return corestream.DialRequest{}, client.authenticationError(
			errors.New("Coinone access token and secret key are required"),
		)
	}
	nonce, err := client.nonceSource()
	if err != nil {
		return corestream.DialRequest{}, client.authenticationError(err)
	}
	timestamp := client.now().UnixMilli()
	if timestamp <= 0 {
		return corestream.DialRequest{}, client.authenticationError(
			errors.New("Coinone stream timestamp must be after the Unix epoch"),
		)
	}
	fields := payloadFields{}
	fields.addInt64("timestamp", timestamp)
	body, err := encodePrivatePayload(material.APIKey, nonce, fields)
	if err != nil {
		return corestream.DialRequest{}, client.authenticationError(err)
	}
	defer clear(body)
	payload, signature, err := SignPayload(material.SecretKey, body)
	if err != nil {
		return corestream.DialRequest{}, client.authenticationError(err)
	}
	return corestream.DialRequest{
		Endpoint: client.privateURL,
		Header: http.Header{
			"X-Coinone-Payload":   {payload},
			"X-Coinone-Signature": {signature},
		},
	}, nil
}

func (client *StreamClient) resolveRoute(options ...trade.RequestOption) (transport.EgressRouteID, error) {
	resolved, err := trade.ResolveRequestOptions(client.defaultRouteID, options...)
	if err != nil {
		return "", err
	}
	if resolved.Timeout != 0 {
		return "", fmt.Errorf("Coinone stream timeout must be controlled by Run context")
	}
	return resolved.EgressRouteID, nil
}

func (client *StreamClient) privateReconnectPolicy(cause error) bool {
	if websocket.CloseStatus(cause) == websocket.StatusCode(4280) {
		return false
	}
	if client.reconnectPolicy != nil {
		return client.reconnectPolicy(cause)
	}
	return corestream.DefaultReconnectPolicy(cause)
}

func (client *StreamClient) authenticationError(cause error) error {
	accountID := ""
	if client.credentials != nil {
		accountID = client.credentials.AccountID
	}
	return &trade.APIError{
		Category: trade.ErrorAuthentication, Exchange: model.ExchangeCoinone,
		AccountID: accountID, Cause: cause,
	}
}

func (client *StreamClient) authorizationError(cause error) error {
	accountID := ""
	if client.credentials != nil {
		accountID = client.credentials.AccountID
	}
	return &trade.APIError{
		Category: trade.ErrorAuthorization, Exchange: model.ExchangeCoinone,
		AccountID: accountID, Cause: cause,
	}
}

func validateStreamSubscriptions(
	subscriptions []StreamSubscription,
	private bool,
	requireNonEmpty bool,
) ([]StreamSubscription, error) {
	if requireNonEmpty && len(subscriptions) == 0 {
		return nil, validationError("WebSocket subscription is required")
	}
	if len(subscriptions) > maxStreamSubscriptions {
		return nil, validationError("WebSocket subscription count cannot exceed %d", maxStreamSubscriptions)
	}
	result := make([]StreamSubscription, 0, len(subscriptions))
	seen := make(map[string]struct{}, len(subscriptions))
	for _, subscription := range subscriptions {
		validated, err := validateStreamSubscription(subscription, private)
		if err != nil {
			return nil, err
		}
		key := streamSubscriptionKey(validated)
		if _, exists := seen[key]; exists {
			return nil, validationError("duplicate WebSocket subscription")
		}
		seen[key] = struct{}{}
		result = append(result, validated)
	}
	return result, nil
}

func validateStreamSubscription(subscription StreamSubscription, private bool) (StreamSubscription, error) {
	if subscription.Format == "" {
		subscription.Format = StreamFormatDefault
	}
	if subscription.Format != StreamFormatDefault && subscription.Format != StreamFormatShort {
		return StreamSubscription{}, validationError("unsupported WebSocket format %q", subscription.Format)
	}
	if private {
		if subscription.Channel != StreamChannelMyOrder && subscription.Channel != StreamChannelMyAsset {
			return StreamSubscription{}, validationError("unsupported private WebSocket channel %q", subscription.Channel)
		}
		if subscription.Channel == StreamChannelMyAsset && len(subscription.Topics) > 0 {
			return StreamSubscription{}, validationError("MYASSET does not accept topics")
		}
	} else {
		switch subscription.Channel {
		case StreamChannelOrderBook, StreamChannelTicker, StreamChannelTrade, StreamChannelChart:
		default:
			return StreamSubscription{}, validationError("unsupported public WebSocket channel %q", subscription.Channel)
		}
		if len(subscription.Topics) != 1 {
			return StreamSubscription{}, validationError("public WebSocket subscription requires exactly one topic")
		}
	}
	topics := make([]StreamTopic, len(subscription.Topics))
	copy(topics, subscription.Topics)
	topicSeen := make(map[string]struct{}, len(topics))
	for index, topic := range topics {
		if err := validatePair(topic.QuoteCurrency, topic.TargetCurrency); err != nil {
			return StreamSubscription{}, err
		}
		if subscription.Channel == StreamChannelChart {
			if !topic.Interval.validStream() {
				return StreamSubscription{}, validationError("unsupported WebSocket candle interval %q", topic.Interval)
			}
		} else if topic.Interval != "" {
			return StreamSubscription{}, validationError("WebSocket interval is only supported for CHART")
		}
		key := topic.QuoteCurrency + "\x00" + topic.TargetCurrency + "\x00" + string(topic.Interval)
		if _, exists := topicSeen[key]; exists {
			return StreamSubscription{}, validationError("duplicate WebSocket topic")
		}
		topicSeen[key] = struct{}{}
		topics[index] = topic
	}
	subscription.Topics = topics
	return subscription, nil
}

func (interval CandleInterval) validStream() bool {
	switch interval {
	case Candle1Minute, Candle3Minutes, Candle5Minutes, Candle15Minutes, Candle30Minutes,
		Candle1Hour, Candle2Hours, Candle4Hours, Candle6Hours, Candle1Day, Candle1Week:
		return true
	default:
		return false
	}
}

func streamSubscriptionKey(subscription StreamSubscription) string {
	encoded, _ := json.Marshal(subscription.Topics)
	return string(subscription.Channel) + "\x00" + string(subscription.Format) + "\x00" + string(encoded)
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

type coinoneConnector struct {
	next corestream.Connector
}

func (connector *coinoneConnector) Connect(
	ctx context.Context,
	routeID transport.EgressRouteID,
	request corestream.DialRequest,
) (corestream.Connection, error) {
	connection, err := connector.next.Connect(ctx, routeID, request)
	if err != nil {
		return nil, err
	}
	return &coinoneConnection{next: connection, pong: make(chan struct{}, 1)}, nil
}

type coinoneConnection struct {
	next    corestream.Connection
	pong    chan struct{}
	writeMu sync.Mutex
}

func (connection *coinoneConnection) Read(ctx context.Context) (corestream.Message, error) {
	message, err := connection.next.Read(ctx)
	if err == nil && isCoinonePong(message.Data) {
		select {
		case connection.pong <- struct{}{}:
		default:
		}
	}
	return message, err
}

func (connection *coinoneConnection) Write(ctx context.Context, message corestream.Message) error {
	connection.writeMu.Lock()
	defer connection.writeMu.Unlock()
	return connection.next.Write(ctx, message)
}

func (connection *coinoneConnection) Ping(ctx context.Context) error {
	select {
	case <-connection.pong:
	default:
	}
	if err := connection.Write(ctx, corestream.Message{
		Type: corestream.MessageText, Data: []byte(`{"request_type":"PING"}`),
	}); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-connection.pong:
		return nil
	}
}

func (connection *coinoneConnection) Close(code int, reason string) error {
	return connection.next.Close(code, reason)
}

func isCoinonePong(data []byte) bool {
	var value struct {
		ResponseType string `json:"response_type"`
		ShortType    string `json:"r"`
	}
	if json.Unmarshal(data, &value) != nil {
		return false
	}
	return value.ResponseType == "PONG" || value.ShortType == "PONG"
}
