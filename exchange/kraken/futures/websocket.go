package futures

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	"github.com/proven-trade/cex-sdk/model"
	corestream "github.com/proven-trade/cex-sdk/stream"
	"github.com/proven-trade/cex-sdk/transport"
)

const (
	DefaultStreamURL             = "wss://futures.kraken.com/ws/v1"
	defaultStreamPingInterval    = 30 * time.Second
	defaultStreamPingTimeout     = 10 * time.Second
	defaultChallengeTimeout      = 10 * time.Second
	defaultSubscriptionInterval  = 100 * time.Millisecond
	maximumStreamSubscriptions   = 50
	maximumStreamProductsPerFeed = 100
)

// StreamClientConfig는 Kraken Futures WebSocket v1 설정이다.
type StreamClientConfig struct {
	Connector              corestream.Connector
	RESTClient             *Client
	DefaultEgressRouteID   transport.EgressRouteID
	StreamURL              string
	AllowInsecureWebSocket bool
	Observer               corestream.StateObserver
	ReconnectPolicy        corestream.ReconnectPolicy
	Backoff                corestream.Backoff
	MaxReconnectAttempts   int
	PingInterval           time.Duration
	PingTimeout            time.Duration
	ChallengeTimeout       time.Duration
	SubscriptionInterval   time.Duration
}

// StreamClient는 Kraken Futures public/private WebSocket 세션을 생성한다.
type StreamClient struct {
	connector            corestream.Connector
	restClient           *Client
	defaultRouteID       transport.EgressRouteID
	streamURL            string
	observer             corestream.StateObserver
	reconnectPolicy      corestream.ReconnectPolicy
	backoff              corestream.Backoff
	maxReconnectAttempts int
	pingInterval         time.Duration
	pingTimeout          time.Duration
	challengeTimeout     time.Duration
	subscriptionInterval time.Duration
}

// NewStreamClient는 Kraken Futures WebSocket v1 클라이언트를 생성한다.
func NewStreamClient(config StreamClientConfig) (*StreamClient, error) {
	if config.Connector == nil {
		return nil, fmt.Errorf("Kraken Futures stream connector is required")
	}
	defaultRouteID := transport.EgressRouteID(strings.TrimSpace(string(config.DefaultEgressRouteID)))
	if defaultRouteID == "" {
		return nil, trade.ErrMissingEgressRoute
	}
	if config.StreamURL == "" {
		config.StreamURL = DefaultStreamURL
	}
	streamURL, err := validateStreamURL(config.StreamURL, config.AllowInsecureWebSocket)
	if err != nil {
		return nil, fmt.Errorf("invalid Kraken Futures stream URL: %w", err)
	}
	if config.PingInterval == 0 {
		config.PingInterval = defaultStreamPingInterval
	}
	if config.PingTimeout == 0 {
		config.PingTimeout = defaultStreamPingTimeout
	}
	if config.ChallengeTimeout == 0 {
		config.ChallengeTimeout = defaultChallengeTimeout
	}
	if config.SubscriptionInterval == 0 {
		config.SubscriptionInterval = defaultSubscriptionInterval
	}
	if config.PingInterval < 0 || config.PingTimeout < 0 || config.ChallengeTimeout < 0 ||
		config.SubscriptionInterval < 0 {
		return nil, fmt.Errorf("Kraken Futures stream durations cannot be negative")
	}
	if config.ChallengeTimeout == 0 {
		return nil, fmt.Errorf("Kraken Futures challenge timeout must be positive")
	}
	if config.MaxReconnectAttempts < 0 {
		return nil, fmt.Errorf("Kraken Futures maximum reconnect attempts cannot be negative")
	}
	return &StreamClient{
		connector: config.Connector, restClient: config.RESTClient, defaultRouteID: defaultRouteID,
		streamURL: streamURL, observer: config.Observer,
		reconnectPolicy: config.ReconnectPolicy, backoff: config.Backoff,
		maxReconnectAttempts: config.MaxReconnectAttempts,
		pingInterval:         config.PingInterval, pingTimeout: config.PingTimeout,
		challengeTimeout:     config.ChallengeTimeout,
		subscriptionInterval: config.SubscriptionInterval,
	}, nil
}

type managedStream struct {
	session  *corestream.Session
	interval time.Duration

	mu                   sync.Mutex
	publicSubscriptions  map[string]PublicStreamSubscription
	privateSubscriptions []PrivateStreamSubscription

	commandMu     sync.Mutex
	lastCommandAt time.Time
}

// PublicStream은 Futures public feed 연결과 구독 목록을 관리한다.
type PublicStream struct {
	managed *managedStream
}

// PublicStream은 선택한 송신 경로에 고정된 Futures public 세션을 생성한다.
func (client *StreamClient) PublicStream(
	request PublicStreamRequest,
	options ...trade.RequestOption,
) (*PublicStream, error) {
	subscriptions, err := validatePublicStreamSubscriptions(request.Subscriptions)
	if err != nil {
		return nil, err
	}
	routeID, err := client.resolveRoute(options...)
	if err != nil {
		return nil, err
	}
	managed := &managedStream{
		interval:            client.subscriptionInterval,
		publicSubscriptions: make(map[string]PublicStreamSubscription, len(subscriptions)),
	}
	for _, subscription := range subscriptions {
		managed.publicSubscriptions[publicSubscriptionKey(subscription)] = subscription
	}
	session, err := corestream.NewSession(corestream.SessionConfig{
		Connector: client.connector, EgressRouteID: routeID,
		Request:   corestream.DialRequest{Endpoint: client.streamURL},
		OnConnect: managed.resubscribePublic, Observer: client.observer,
		ReconnectPolicy: client.reconnectPolicy, Backoff: client.backoff,
		MaxReconnectAttempts: client.maxReconnectAttempts,
		PingInterval:         client.pingInterval, PingTimeout: client.pingTimeout,
	})
	if err != nil {
		return nil, err
	}
	managed.session = session
	return &PublicStream{managed: managed}, nil
}

// Run은 public frame을 순서대로 decode해 handler에 전달한다.
func (public *PublicStream) Run(
	ctx context.Context,
	handler func(context.Context, StreamMessage) error,
) error {
	if handler == nil {
		return fmt.Errorf("Kraken Futures public stream handler is required")
	}
	return public.managed.run(ctx, handler)
}

// Subscribe는 public 구독을 추가하고 재연결 목록에도 반영한다.
func (public *PublicStream) Subscribe(
	ctx context.Context,
	subscriptions ...PublicStreamSubscription,
) error {
	validated, err := validatePublicStreamSubscriptions(subscriptions)
	if err != nil {
		return err
	}
	return public.managed.changePublic(ctx, "subscribe", validated)
}

// Unsubscribe는 public 구독을 제거하고 재연결 목록에도 반영한다.
func (public *PublicStream) Unsubscribe(
	ctx context.Context,
	subscriptions ...PublicStreamSubscription,
) error {
	validated, err := validatePublicStreamSubscriptions(subscriptions)
	if err != nil {
		return err
	}
	return public.managed.changePublic(ctx, "unsubscribe", validated)
}

// Close는 public stream 세션을 종료한다.
func (public *PublicStream) Close() error { return public.managed.session.Close() }

// Generation은 성공한 public stream 연결 세대 번호를 반환한다.
func (public *PublicStream) Generation() uint64 { return public.managed.session.Generation() }

// EgressRouteID는 public 연결과 모든 재연결에 고정된 송신 경로를 반환한다.
func (public *PublicStream) EgressRouteID() transport.EgressRouteID {
	return public.managed.session.EgressRouteID()
}

func (public *PublicStream) hasBookSubscription(productID string) bool {
	public.managed.mu.Lock()
	defer public.managed.mu.Unlock()
	for _, subscription := range public.managed.publicSubscriptions {
		if subscription.Feed != PublicFeedBook {
			continue
		}
		for _, subscribedProductID := range subscription.ProductIDs {
			if subscribedProductID == productID {
				return true
			}
		}
	}
	return false
}

func (public *PublicStream) reconnect() error {
	return public.managed.session.Reconnect()
}

// PrivateStream은 Futures private account feed 연결을 관리한다.
type PrivateStream struct {
	managed *managedStream
}

// PrivateStream은 매 연결마다 새 challenge를 서명하고 private feed를 구독한다.
func (client *StreamClient) PrivateStream(
	request PrivateStreamRequest,
	options ...trade.RequestOption,
) (*PrivateStream, error) {
	subscriptions, err := validatePrivateStreamSubscriptions(request.Subscriptions)
	if err != nil {
		return nil, err
	}
	routeID, err := client.resolveRoute(options...)
	if err != nil {
		return nil, err
	}
	if err := client.validatePrivateAccess(routeID); err != nil {
		return nil, err
	}
	managed := &managedStream{
		interval:             client.subscriptionInterval,
		privateSubscriptions: append([]PrivateStreamSubscription(nil), subscriptions...),
	}
	session, err := corestream.NewSession(corestream.SessionConfig{
		Connector: client.connector, EgressRouteID: routeID,
		Request: corestream.DialRequest{Endpoint: client.streamURL},
		OnConnect: func(ctx context.Context, connection corestream.Connection) error {
			return client.authenticateAndSubscribe(ctx, connection, routeID, managed)
		},
		Observer: client.observer, ReconnectPolicy: client.privateReconnectPolicy,
		Backoff: client.backoff, MaxReconnectAttempts: client.maxReconnectAttempts,
		PingInterval: client.pingInterval, PingTimeout: client.pingTimeout,
	})
	if err != nil {
		return nil, err
	}
	managed.session = session
	return &PrivateStream{managed: managed}, nil
}

// Run은 private frame을 순서대로 decode해 handler에 전달한다.
func (private *PrivateStream) Run(
	ctx context.Context,
	handler func(context.Context, StreamMessage) error,
) error {
	if handler == nil {
		return fmt.Errorf("Kraken Futures private stream handler is required")
	}
	return private.managed.run(ctx, handler)
}

// Close는 private stream 세션을 종료한다.
func (private *PrivateStream) Close() error { return private.managed.session.Close() }

// Generation은 성공한 private stream 연결 세대 번호를 반환한다.
func (private *PrivateStream) Generation() uint64 { return private.managed.session.Generation() }

func (managed *managedStream) run(
	ctx context.Context,
	handler func(context.Context, StreamMessage) error,
) error {
	return managed.session.Run(ctx, func(ctx context.Context, message corestream.Message) error {
		decoded, err := DecodeStreamMessage(message)
		if err != nil {
			return err
		}
		if streamMessageRejected(decoded) {
			return &StreamRequestError{
				Event: decoded.Event, Feed: decoded.Feed, Message: decoded.Message,
			}
		}
		return handler(ctx, decoded)
	})
}

func (managed *managedStream) resubscribePublic(
	ctx context.Context,
	connection corestream.Connection,
) error {
	managed.commandMu.Lock()
	defer managed.commandMu.Unlock()
	return managed.writePublicLocked(ctx, connection, "subscribe", managed.snapshotPublic())
}

func (managed *managedStream) changePublic(
	ctx context.Context,
	operation string,
	subscriptions []PublicStreamSubscription,
) error {
	managed.commandMu.Lock()
	defer managed.commandMu.Unlock()
	managed.mu.Lock()
	selected := make([]PublicStreamSubscription, 0, len(subscriptions))
	for _, subscription := range subscriptions {
		_, exists := managed.publicSubscriptions[publicSubscriptionKey(subscription)]
		if (operation == "subscribe" && !exists) || (operation == "unsubscribe" && exists) {
			selected = append(selected, subscription)
		}
	}
	if operation == "subscribe" && len(managed.publicSubscriptions)+len(selected) > maximumStreamSubscriptions {
		managed.mu.Unlock()
		return validationError(
			"Futures WebSocket subscription count cannot exceed %d", maximumStreamSubscriptions,
		)
	}
	managed.mu.Unlock()
	if len(selected) == 0 {
		return nil
	}
	if err := managed.writePublicLocked(ctx, managed.session, operation, selected); err != nil {
		return err
	}
	managed.mu.Lock()
	for _, subscription := range selected {
		key := publicSubscriptionKey(subscription)
		if operation == "subscribe" {
			managed.publicSubscriptions[key] = subscription
		} else {
			delete(managed.publicSubscriptions, key)
		}
	}
	managed.mu.Unlock()
	return nil
}

func (managed *managedStream) snapshotPublic() []PublicStreamSubscription {
	managed.mu.Lock()
	defer managed.mu.Unlock()
	keys := make([]string, 0, len(managed.publicSubscriptions))
	for key := range managed.publicSubscriptions {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	subscriptions := make([]PublicStreamSubscription, len(keys))
	for index, key := range keys {
		subscriptions[index] = managed.publicSubscriptions[key]
	}
	return subscriptions
}

func (managed *managedStream) writePublicLocked(
	ctx context.Context,
	writer streamWriter,
	event string,
	subscriptions []PublicStreamSubscription,
) error {
	for _, subscription := range subscriptions {
		if err := managed.waitCommandSlot(ctx); err != nil {
			return err
		}
		if err := writeStreamOperation(ctx, writer, streamOperation{
			Event: event, Feed: string(subscription.Feed), ProductIDs: subscription.ProductIDs,
		}); err != nil {
			return err
		}
		managed.lastCommandAt = time.Now()
	}
	return nil
}

func (managed *managedStream) writePrivateLocked(
	ctx context.Context,
	connection corestream.Connection,
	apiKey, challenge, signature string,
) error {
	managed.commandMu.Lock()
	defer managed.commandMu.Unlock()
	for _, subscription := range managed.privateSubscriptions {
		if err := managed.waitCommandSlot(ctx); err != nil {
			return err
		}
		if err := writeStreamOperation(ctx, connection, streamOperation{
			Event: "subscribe", Feed: string(subscription.Feed), ProductIDs: subscription.ProductIDs,
			APIKey: apiKey, OriginalChallenge: challenge, SignedChallenge: signature,
		}); err != nil {
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

type streamWriter interface {
	Write(context.Context, corestream.Message) error
}

type streamOperation struct {
	Event             string   `json:"event"`
	Feed              string   `json:"feed,omitempty"`
	ProductIDs        []string `json:"product_ids,omitempty"`
	APIKey            string   `json:"api_key,omitempty"`
	OriginalChallenge string   `json:"original_challenge,omitempty"`
	SignedChallenge   string   `json:"signed_challenge,omitempty"`
}

func writeStreamOperation(
	ctx context.Context,
	writer streamWriter,
	operation streamOperation,
) error {
	payload, err := json.Marshal(operation)
	if err != nil {
		return fmt.Errorf("encode Kraken Futures stream operation: %w", err)
	}
	defer clear(payload)
	return writer.Write(ctx, corestream.Message{Type: corestream.MessageText, Data: payload})
}

func (client *StreamClient) authenticateAndSubscribe(
	ctx context.Context,
	connection corestream.Connection,
	routeID transport.EgressRouteID,
	managed *managedStream,
) error {
	if err := client.restClient.credentials.RequireEgressRoute(routeID); err != nil {
		return client.authorizationError(err)
	}
	var material credential.Material
	defer material.Destroy()
	resolvedMaterial, err := client.restClient.credentialProvider.Resolve(
		ctx, client.restClient.credentials.SecretRef,
	)
	material = resolvedMaterial
	if err != nil {
		return client.authenticationError(err)
	}
	if len(material.APIKey) == 0 || len(material.SecretKey) == 0 {
		return client.authenticationError(
			errors.New("Kraken Futures API key and Base64 secret are required"),
		)
	}
	authContext, cancel := context.WithTimeout(ctx, client.challengeTimeout)
	apiKey := string(material.APIKey)
	if err := writeStreamOperation(authContext, connection, streamOperation{
		Event: "challenge", APIKey: apiKey,
	}); err != nil {
		cancel()
		return normalizeChallengeError(ctx, err)
	}
	challenge, err := client.readChallenge(authContext, connection)
	cancel()
	if err != nil {
		return normalizeChallengeError(ctx, err)
	}
	signature, err := SignChallenge(challenge, material.SecretKey)
	if err != nil {
		return client.authenticationError(err)
	}
	return managed.writePrivateLocked(ctx, connection, apiKey, challenge, signature)
}

func normalizeChallengeError(parent context.Context, err error) error {
	if errors.Is(err, context.DeadlineExceeded) && parent.Err() == nil {
		return fmt.Errorf("Kraken Futures stream challenge timed out")
	}
	return err
}

func (client *StreamClient) readChallenge(
	ctx context.Context,
	connection corestream.Connection,
) (string, error) {
	for {
		frame, err := connection.Read(ctx)
		if err != nil {
			return "", err
		}
		message, decodeErr := DecodeStreamMessage(frame)
		clear(frame.Data)
		if decodeErr != nil {
			return "", decodeErr
		}
		clear(message.Raw)
		if message.Event == "error" {
			return "", client.authenticationError(errors.New(message.Message))
		}
		if message.Event == "challenge" {
			if message.Message == "" {
				return "", fmt.Errorf("Kraken Futures stream challenge response is invalid")
			}
			return message.Message, nil
		}
	}
}

func (client *StreamClient) resolveRoute(
	options ...trade.RequestOption,
) (transport.EgressRouteID, error) {
	resolved, err := trade.ResolveRequestOptions(client.defaultRouteID, options...)
	if err != nil {
		return "", err
	}
	if resolved.Timeout != 0 {
		return "", fmt.Errorf("Kraken Futures stream timeout must be controlled by Run context")
	}
	return resolved.EgressRouteID, nil
}

func (client *StreamClient) validatePrivateAccess(routeID transport.EgressRouteID) error {
	if client.restClient == nil || client.restClient.credentials == nil ||
		client.restClient.credentialProvider == nil {
		return client.authenticationError(
			errors.New("private Kraken Futures stream requires REST credentials"),
		)
	}
	if err := client.restClient.credentials.RequireEgressRoute(routeID); err != nil {
		return client.authorizationError(err)
	}
	if err := client.restClient.credentials.RequirePermission(credential.PermissionRead); err != nil {
		return client.authorizationError(err)
	}
	return nil
}

func (client *StreamClient) privateReconnectPolicy(err error) bool {
	if errors.Is(err, trade.ErrAuthentication) || errors.Is(err, trade.ErrAuthorization) {
		return false
	}
	if client.reconnectPolicy != nil {
		return client.reconnectPolicy(err)
	}
	return corestream.DefaultReconnectPolicy(err)
}

func (client *StreamClient) authenticationError(cause error) error {
	accountID := ""
	if client.restClient != nil && client.restClient.credentials != nil {
		accountID = client.restClient.credentials.AccountID
	}
	return &trade.APIError{
		Category: trade.ErrorAuthentication, Exchange: model.ExchangeKraken,
		AccountID: accountID, Cause: cause,
	}
}

func (client *StreamClient) authorizationError(cause error) error {
	accountID := ""
	if client.restClient != nil && client.restClient.credentials != nil {
		accountID = client.restClient.credentials.AccountID
	}
	return &trade.APIError{
		Category: trade.ErrorAuthorization, Exchange: model.ExchangeKraken,
		AccountID: accountID, Cause: cause,
	}
}

func validatePublicStreamSubscriptions(
	subscriptions []PublicStreamSubscription,
) ([]PublicStreamSubscription, error) {
	if len(subscriptions) == 0 {
		return nil, validationError("at least one Futures public WebSocket subscription is required")
	}
	if len(subscriptions) > maximumStreamSubscriptions {
		return nil, validationError(
			"Futures WebSocket subscription count cannot exceed %d", maximumStreamSubscriptions,
		)
	}
	validated := make([]PublicStreamSubscription, len(subscriptions))
	seen := make(map[string]struct{}, len(subscriptions))
	for index, subscription := range subscriptions {
		value, err := validatePublicStreamSubscription(subscription)
		if err != nil {
			return nil, err
		}
		key := publicSubscriptionKey(value)
		if _, exists := seen[key]; exists {
			return nil, validationError("duplicate Futures public WebSocket subscription")
		}
		seen[key] = struct{}{}
		validated[index] = value
	}
	return validated, nil
}

func validatePublicStreamSubscription(
	subscription PublicStreamSubscription,
) (PublicStreamSubscription, error) {
	switch subscription.Feed {
	case PublicFeedTicker, PublicFeedTickerLite, PublicFeedTrade, PublicFeedBook:
		if len(subscription.ProductIDs) == 0 ||
			len(subscription.ProductIDs) > maximumStreamProductsPerFeed {
			return PublicStreamSubscription{}, validationError(
				"Futures WebSocket product count must be between 1 and %d",
				maximumStreamProductsPerFeed,
			)
		}
	case PublicFeedHeartbeat:
		if len(subscription.ProductIDs) != 0 {
			return PublicStreamSubscription{}, validationError(
				"Futures heartbeat feed does not accept products",
			)
		}
	default:
		return PublicStreamSubscription{}, validationError(
			"unsupported Futures public WebSocket feed %q", subscription.Feed,
		)
	}
	products, err := validateStreamProducts(subscription.ProductIDs)
	if err != nil {
		return PublicStreamSubscription{}, err
	}
	subscription.ProductIDs = products
	return subscription, nil
}

func validatePrivateStreamSubscriptions(
	subscriptions []PrivateStreamSubscription,
) ([]PrivateStreamSubscription, error) {
	if len(subscriptions) == 0 {
		return nil, validationError("at least one Futures private WebSocket subscription is required")
	}
	if len(subscriptions) > 7 {
		return nil, validationError("Futures private WebSocket accepts at most seven subscriptions")
	}
	validated := make([]PrivateStreamSubscription, len(subscriptions))
	seen := make(map[string]struct{}, len(subscriptions))
	for index, subscription := range subscriptions {
		switch subscription.Feed {
		case PrivateFeedBalances, PrivateFeedOpenOrders, PrivateFeedOpenOrdersVerbose,
			PrivateFeedOpenPositions, PrivateFeedAccountLog, PrivateFeedNotifications:
			if len(subscription.ProductIDs) != 0 {
				return nil, validationError(
					"Futures private WebSocket feed %q does not accept products", subscription.Feed,
				)
			}
		case PrivateFeedFills:
			if len(subscription.ProductIDs) > maximumStreamProductsPerFeed {
				return nil, validationError(
					"Futures WebSocket product count cannot exceed %d", maximumStreamProductsPerFeed,
				)
			}
		default:
			return nil, validationError(
				"unsupported Futures private WebSocket feed %q", subscription.Feed,
			)
		}
		products, err := validateStreamProducts(subscription.ProductIDs)
		if err != nil {
			return nil, err
		}
		subscription.ProductIDs = products
		key := privateSubscriptionKey(subscription)
		if _, exists := seen[key]; exists {
			return nil, validationError("duplicate Futures private WebSocket subscription")
		}
		seen[key] = struct{}{}
		validated[index] = subscription
	}
	return validated, nil
}

func validateStreamProducts(products []string) ([]string, error) {
	validated := append([]string(nil), products...)
	sort.Strings(validated)
	for index, product := range validated {
		if err := validateStreamProduct(product); err != nil {
			return nil, err
		}
		if index > 0 && product == validated[index-1] {
			return nil, validationError("duplicate Futures WebSocket product %q", product)
		}
	}
	return validated, nil
}

func validateStreamProduct(product string) error {
	if len(product) < 3 || len(product) > 64 || strings.TrimSpace(product) != product ||
		strings.ContainsFunc(product, unicode.IsControl) {
		return validationError("Futures WebSocket product has an invalid format")
	}
	for _, character := range product {
		if character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' ||
			character == '_' || character == ':' || character == '-' || character == '.' {
			continue
		}
		return validationError("Futures WebSocket product has an invalid format")
	}
	return nil
}

func publicSubscriptionKey(subscription PublicStreamSubscription) string {
	return string(subscription.Feed) + "|" + strings.Join(subscription.ProductIDs, ",")
}

func privateSubscriptionKey(subscription PrivateStreamSubscription) string {
	return string(subscription.Feed) + "|" + strings.Join(subscription.ProductIDs, ",")
}

func validateStreamURL(raw string, allowInsecure bool) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" ||
		parsed.RawQuery != "" {
		return "", fmt.Errorf("invalid WebSocket URL")
	}
	if parsed.Scheme != "wss" && !(allowInsecure && parsed.Scheme == "ws") {
		return "", fmt.Errorf("WebSocket URL must use WSS")
	}
	parsed.Path = strings.TrimSuffix(parsed.Path, "/")
	return parsed.String(), nil
}

// DecodeStreamMessage는 Futures WebSocket v1 frame을 공통 stream 메시지로 변환한다.
func DecodeStreamMessage(message corestream.Message) (StreamMessage, error) {
	var envelope struct {
		Event      string   `json:"event"`
		Feed       string   `json:"feed"`
		Message    string   `json:"message"`
		ProductID  string   `json:"product_id"`
		ProductIDs []string `json:"product_ids"`
		Account    string   `json:"account"`
	}
	if err := json.Unmarshal(message.Data, &envelope); err != nil {
		return StreamMessage{}, fmt.Errorf("decode Kraken Futures stream message: %w", err)
	}
	raw, err := redactStreamAuthentication(message.Data)
	if err != nil {
		return StreamMessage{}, err
	}
	return StreamMessage{
		Event: envelope.Event, Feed: envelope.Feed, Message: envelope.Message,
		ProductID: envelope.ProductID, ProductIDs: append([]string(nil), envelope.ProductIDs...),
		Account: envelope.Account, Raw: raw,
	}, nil
}

func redactStreamAuthentication(data []byte) ([]byte, error) {
	if !bytes.Contains(data, []byte(`"api_key"`)) &&
		!bytes.Contains(data, []byte(`"original_challenge"`)) &&
		!bytes.Contains(data, []byte(`"signed_challenge"`)) {
		return cloneBytes(data), nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, fmt.Errorf("redact Kraken Futures stream authentication: %w", err)
	}
	for _, key := range []string{"api_key", "original_challenge", "signed_challenge"} {
		clear(fields[key])
		delete(fields, key)
	}
	redacted, err := json.Marshal(fields)
	if err != nil {
		return nil, fmt.Errorf("encode redacted Kraken Futures stream message: %w", err)
	}
	return redacted, nil
}

func streamMessageRejected(message StreamMessage) bool {
	return message.Event == "error" || message.Event == "subscribed_failed" ||
		message.Event == "unsubscribed_failed"
}
