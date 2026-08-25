package coinbase

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	trade "github.com/proven-trade/proven-trade-sdk"
	"github.com/proven-trade/proven-trade-sdk/credential"
	"github.com/proven-trade/proven-trade-sdk/model"
	corestream "github.com/proven-trade/proven-trade-sdk/stream"
	"github.com/proven-trade/proven-trade-sdk/transport"
)

const (
	DefaultMarketDataStreamURL      = "wss://advanced-trade-ws.coinbase.com"
	DefaultUserDataStreamURL        = "wss://advanced-trade-ws-user.coinbase.com"
	defaultStreamPingInterval       = 20 * time.Second
	defaultStreamPingTimeout        = 10 * time.Second
	defaultSubscriptionInterval     = 150 * time.Millisecond
	maximumStreamSubscriptions      = 20
	maximumStreamProductsPerChannel = 100
)

// StreamClientConfig는 Coinbase Advanced Trade WebSocket 설정이다.
type StreamClientConfig struct {
	Connector              corestream.Connector
	Credentials            *credential.Descriptor
	CredentialProvider     credential.Provider
	DefaultEgressRouteID   transport.EgressRouteID
	MarketDataStreamURL    string
	UserDataStreamURL      string
	AllowInsecureWebSocket bool
	Now                    func() time.Time
	Random                 io.Reader
	Observer               corestream.StateObserver
	ReconnectPolicy        corestream.ReconnectPolicy
	Backoff                corestream.Backoff
	MaxReconnectAttempts   int
	PingInterval           time.Duration
	PingTimeout            time.Duration
	SubscriptionInterval   time.Duration
}

// StreamClient는 Coinbase market data와 user WebSocket 세션을 생성한다.
type StreamClient struct {
	connector            corestream.Connector
	credentials          *credential.Descriptor
	credentialProvider   credential.Provider
	defaultRouteID       transport.EgressRouteID
	marketDataURL        string
	userDataURL          string
	now                  func() time.Time
	random               io.Reader
	randomMu             sync.Mutex
	observer             corestream.StateObserver
	reconnectPolicy      corestream.ReconnectPolicy
	backoff              corestream.Backoff
	maxReconnectAttempts int
	pingInterval         time.Duration
	pingTimeout          time.Duration
	subscriptionInterval time.Duration
}

// NewStreamClient는 Coinbase Advanced Trade WebSocket 클라이언트를 생성한다.
func NewStreamClient(config StreamClientConfig) (*StreamClient, error) {
	if config.Connector == nil {
		return nil, fmt.Errorf("Coinbase stream connector is required")
	}
	defaultRouteID := transport.EgressRouteID(strings.TrimSpace(string(config.DefaultEgressRouteID)))
	if defaultRouteID == "" {
		return nil, trade.ErrMissingEgressRoute
	}
	if config.MarketDataStreamURL == "" {
		config.MarketDataStreamURL = DefaultMarketDataStreamURL
	}
	if config.UserDataStreamURL == "" {
		config.UserDataStreamURL = DefaultUserDataStreamURL
	}
	marketDataURL, err := validateCoinbaseWebSocketURL(
		config.MarketDataStreamURL, config.AllowInsecureWebSocket,
	)
	if err != nil {
		return nil, fmt.Errorf("invalid Coinbase market data stream URL: %w", err)
	}
	userDataURL, err := validateCoinbaseWebSocketURL(
		config.UserDataStreamURL, config.AllowInsecureWebSocket,
	)
	if err != nil {
		return nil, fmt.Errorf("invalid Coinbase user data stream URL: %w", err)
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.Random == nil {
		config.Random = rand.Reader
	}
	if config.PingInterval == 0 {
		config.PingInterval = defaultStreamPingInterval
	}
	if config.PingTimeout == 0 {
		config.PingTimeout = defaultStreamPingTimeout
	}
	if config.SubscriptionInterval == 0 {
		config.SubscriptionInterval = defaultSubscriptionInterval
	}
	if config.PingInterval < 0 || config.PingTimeout < 0 || config.SubscriptionInterval < 0 {
		return nil, fmt.Errorf("Coinbase stream durations cannot be negative")
	}
	if config.MaxReconnectAttempts < 0 {
		return nil, fmt.Errorf("Coinbase maximum reconnect attempts cannot be negative")
	}

	var credentialsCopy *credential.Descriptor
	if config.Credentials != nil {
		if err := config.Credentials.Validate(); err != nil {
			return nil, err
		}
		if config.Credentials.Exchange != model.ExchangeCoinbase {
			return nil, fmt.Errorf("credential exchange must be Coinbase")
		}
		if config.CredentialProvider == nil {
			return nil, fmt.Errorf("credential provider is required for Coinbase user streams")
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
		connector: config.Connector, credentials: credentialsCopy,
		credentialProvider: config.CredentialProvider, defaultRouteID: defaultRouteID,
		marketDataURL: marketDataURL, userDataURL: userDataURL,
		now: config.Now, random: config.Random, observer: config.Observer,
		reconnectPolicy: config.ReconnectPolicy, backoff: config.Backoff,
		maxReconnectAttempts: config.MaxReconnectAttempts,
		pingInterval:         config.PingInterval, pingTimeout: config.PingTimeout,
		subscriptionInterval: config.SubscriptionInterval,
	}, nil
}

type managedCoinbaseStream struct {
	session *corestream.Session

	mu            sync.Mutex
	subscriptions map[StreamChannel]StreamSubscription

	commandMu     sync.Mutex
	lastCommandAt time.Time
	interval      time.Duration
}

// PublicStream은 market data endpoint의 구독과 재연결을 관리한다.
type PublicStream struct {
	managed *managedCoinbaseStream
}

// PublicStream은 선택한 송신 경로에 고정된 공개 market data 세션을 생성한다.
// 연결 유지를 위해 heartbeats 채널을 자동으로 함께 구독한다.
func (client *StreamClient) PublicStream(
	request PublicStreamRequest,
	options ...trade.RequestOption,
) (*PublicStream, error) {
	subscriptions, err := normalizePublicSubscriptions(request.Subscriptions, true)
	if err != nil {
		return nil, err
	}
	routeID, err := client.resolveStreamRoute(options...)
	if err != nil {
		return nil, err
	}
	managed := newManagedCoinbaseStream(subscriptions, client.subscriptionInterval)
	session, err := corestream.NewSession(corestream.SessionConfig{
		Connector:     client.connector,
		EgressRouteID: routeID,
		Request:       corestream.DialRequest{Endpoint: client.marketDataURL},
		OnConnect: func(ctx context.Context, connection corestream.Connection) error {
			return managed.writeSubscriptions(ctx, connection, "subscribe", managed.snapshot(), nil)
		},
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

// Run은 공개 frame을 순서대로 decode해 handler에 전달한다.
func (stream *PublicStream) Run(ctx context.Context, handler func(context.Context, StreamMessage) error) error {
	if handler == nil {
		return fmt.Errorf("Coinbase public stream handler is required")
	}
	return stream.managed.session.Run(ctx, func(messageContext context.Context, message corestream.Message) error {
		decoded, err := DecodeStreamMessage(message)
		if err != nil {
			return err
		}
		return handler(messageContext, decoded)
	})
}

// Subscribe는 공개 채널 구독을 추가하고 재연결 목록에도 반영한다.
func (stream *PublicStream) Subscribe(ctx context.Context, subscriptions ...StreamSubscription) error {
	validated, err := normalizePublicSubscriptions(subscriptions, false)
	if err != nil {
		return err
	}
	return stream.managed.change(ctx, "subscribe", validated)
}

// Unsubscribe는 공개 채널 구독을 제거하고 재연결 목록에도 반영한다.
// 자동 heartbeat 구독은 제거할 수 없다.
func (stream *PublicStream) Unsubscribe(ctx context.Context, subscriptions ...StreamSubscription) error {
	validated, err := normalizePublicSubscriptions(subscriptions, false)
	if err != nil {
		return err
	}
	for _, subscription := range validated {
		if subscription.Channel == StreamChannelHeartbeats {
			return validationError("automatic heartbeat subscription cannot be removed")
		}
	}
	return stream.managed.change(ctx, "unsubscribe", validated)
}

// Close는 공개 stream 세션을 종료한다.
func (stream *PublicStream) Close() error { return stream.managed.session.Close() }

// Generation은 성공한 공개 stream 연결 세대 번호를 반환한다.
func (stream *PublicStream) Generation() uint64 { return stream.managed.session.Generation() }

// EgressRouteID는 공개 연결과 모든 재연결에 고정된 송신 경로를 반환한다.
func (stream *PublicStream) EgressRouteID() transport.EgressRouteID {
	return stream.managed.session.EgressRouteID()
}

func (stream *PublicStream) hasSubscription(channel StreamChannel, productID string) bool {
	stream.managed.mu.Lock()
	defer stream.managed.mu.Unlock()
	subscription, exists := stream.managed.subscriptions[channel]
	if !exists {
		return false
	}
	for _, subscribedProductID := range subscription.ProductIDs {
		if subscribedProductID == productID {
			return true
		}
	}
	return false
}

func (stream *PublicStream) reconnect() error {
	return stream.managed.session.Reconnect()
}

// UserStream은 user data endpoint의 인증 구독과 재연결을 관리한다.
type UserStream struct {
	session *corestream.Session
}

// UserStream은 선택한 송신 경로에서 user와 heartbeat 채널을 인증 구독한다.
func (client *StreamClient) UserStream(
	request UserStreamRequest,
	options ...trade.RequestOption,
) (*UserStream, error) {
	productIDs, err := validateStreamProductIDs(request.ProductIDs, true)
	if err != nil {
		return nil, err
	}
	routeID, err := client.resolveStreamRoute(options...)
	if err != nil {
		return nil, err
	}
	if client.credentials == nil || client.credentialProvider == nil {
		return nil, client.streamAuthenticationError(errors.New("Coinbase user stream requires credentials"))
	}
	if err := client.credentials.RequireEgressRoute(routeID); err != nil {
		return nil, client.streamAuthorizationError(err)
	}
	if err := client.credentials.RequirePermission(credential.PermissionRead); err != nil {
		return nil, client.streamAuthorizationError(err)
	}
	subscriptions := []StreamSubscription{
		{Channel: StreamChannelHeartbeats},
		{Channel: StreamChannelUser, ProductIDs: productIDs},
	}
	managed := newManagedCoinbaseStream(subscriptions, client.subscriptionInterval)
	session, err := corestream.NewSession(corestream.SessionConfig{
		Connector:     client.connector,
		EgressRouteID: routeID,
		Request:       corestream.DialRequest{Endpoint: client.userDataURL},
		OnConnect: func(ctx context.Context, connection corestream.Connection) error {
			return client.authenticateSubscriptions(ctx, connection, managed)
		},
		Observer:             client.observer,
		ReconnectPolicy:      client.userReconnectPolicy,
		Backoff:              client.backoff,
		MaxReconnectAttempts: client.maxReconnectAttempts,
		PingInterval:         client.pingInterval,
		PingTimeout:          client.pingTimeout,
	})
	if err != nil {
		return nil, err
	}
	managed.session = session
	return &UserStream{session: session}, nil
}

// Run은 user frame을 순서대로 decode해 handler에 전달한다.
func (stream *UserStream) Run(ctx context.Context, handler func(context.Context, StreamMessage) error) error {
	if handler == nil {
		return fmt.Errorf("Coinbase user stream handler is required")
	}
	return stream.session.Run(ctx, func(messageContext context.Context, message corestream.Message) error {
		decoded, err := DecodeStreamMessage(message)
		if err != nil {
			return err
		}
		if decoded.Type == "error" && isStreamAuthenticationMessage(decoded.Message) {
			return &StreamAuthenticationError{Message: decoded.Message}
		}
		return handler(messageContext, decoded)
	})
}

// Close는 user stream 세션을 종료한다.
func (stream *UserStream) Close() error { return stream.session.Close() }

// Generation은 성공한 user stream 연결 세대 번호를 반환한다.
func (stream *UserStream) Generation() uint64 { return stream.session.Generation() }

func newManagedCoinbaseStream(
	subscriptions []StreamSubscription,
	interval time.Duration,
) *managedCoinbaseStream {
	managed := &managedCoinbaseStream{
		subscriptions: make(map[StreamChannel]StreamSubscription, len(subscriptions)), interval: interval,
	}
	for _, subscription := range subscriptions {
		managed.subscriptions[subscription.Channel] = cloneStreamSubscription(subscription)
	}
	return managed
}

func (managed *managedCoinbaseStream) change(
	ctx context.Context,
	operation string,
	subscriptions []StreamSubscription,
) error {
	if ctx == nil {
		return fmt.Errorf("Coinbase stream command context is required")
	}
	managed.commandMu.Lock()
	defer managed.commandMu.Unlock()
	managed.mu.Lock()
	selected := make([]StreamSubscription, 0, len(subscriptions))
	for _, subscription := range subscriptions {
		current, exists := managed.subscriptions[subscription.Channel]
		products := streamProductDifference(subscription.ProductIDs, current.ProductIDs, operation == "unsubscribe")
		if subscription.Channel == StreamChannelHeartbeats {
			if operation == "subscribe" && !exists {
				selected = append(selected, cloneStreamSubscription(subscription))
			}
			continue
		}
		if len(products) > 0 {
			selected = append(selected, StreamSubscription{
				Channel: subscription.Channel, ProductIDs: products,
			})
		}
	}
	newChannels := 0
	if operation == "subscribe" {
		for _, subscription := range selected {
			if _, exists := managed.subscriptions[subscription.Channel]; !exists {
				newChannels++
			}
		}
	}
	if len(managed.subscriptions)+newChannels > maximumStreamSubscriptions {
		managed.mu.Unlock()
		return validationError("WebSocket subscription count cannot exceed %d", maximumStreamSubscriptions)
	}
	managed.mu.Unlock()
	if len(selected) == 0 {
		return nil
	}
	if err := managed.writeSubscriptionsLocked(ctx, managed.session, operation, selected, nil); err != nil {
		return err
	}
	managed.mu.Lock()
	for _, subscription := range selected {
		if operation == "subscribe" {
			current := managed.subscriptions[subscription.Channel]
			current.Channel = subscription.Channel
			current.ProductIDs = mergeStreamProducts(current.ProductIDs, subscription.ProductIDs)
			managed.subscriptions[subscription.Channel] = current
		} else {
			current := managed.subscriptions[subscription.Channel]
			current.ProductIDs = removeStreamProducts(current.ProductIDs, subscription.ProductIDs)
			if len(current.ProductIDs) == 0 {
				delete(managed.subscriptions, subscription.Channel)
			} else {
				managed.subscriptions[subscription.Channel] = current
			}
		}
	}
	managed.mu.Unlock()
	return nil
}

func (managed *managedCoinbaseStream) snapshot() []StreamSubscription {
	managed.mu.Lock()
	defer managed.mu.Unlock()
	result := make([]StreamSubscription, 0, len(managed.subscriptions))
	for _, subscription := range managed.subscriptions {
		result = append(result, cloneStreamSubscription(subscription))
	}
	sort.Slice(result, func(left, right int) bool {
		return result[left].Channel < result[right].Channel
	})
	return result
}

type streamJWTSource func() (string, error)

func (managed *managedCoinbaseStream) writeSubscriptions(
	ctx context.Context,
	writer streamWriter,
	operation string,
	subscriptions []StreamSubscription,
	tokenSource streamJWTSource,
) error {
	managed.commandMu.Lock()
	defer managed.commandMu.Unlock()
	return managed.writeSubscriptionsLocked(ctx, writer, operation, subscriptions, tokenSource)
}

func (managed *managedCoinbaseStream) writeSubscriptionsLocked(
	ctx context.Context,
	writer streamWriter,
	operation string,
	subscriptions []StreamSubscription,
	tokenSource streamJWTSource,
) error {
	for _, subscription := range subscriptions {
		if err := managed.waitCommandSlot(ctx); err != nil {
			return err
		}
		token := ""
		var err error
		if tokenSource != nil {
			token, err = tokenSource()
			if err != nil {
				return err
			}
		}
		if err := writeCoinbaseStreamOperation(ctx, writer, operation, subscription, token); err != nil {
			return err
		}
		managed.lastCommandAt = time.Now()
	}
	return nil
}

func (managed *managedCoinbaseStream) waitCommandSlot(ctx context.Context) error {
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
	Type       string        `json:"type"`
	ProductIDs []string      `json:"product_ids,omitempty"`
	Channel    StreamChannel `json:"channel"`
	JWT        string        `json:"jwt,omitempty"`
}

func writeCoinbaseStreamOperation(
	ctx context.Context,
	writer streamWriter,
	operation string,
	subscription StreamSubscription,
	token string,
) error {
	payload, err := json.Marshal(streamOperation{
		Type: operation, ProductIDs: subscription.ProductIDs, Channel: subscription.Channel, JWT: token,
	})
	if err != nil {
		return fmt.Errorf("encode Coinbase stream operation: %w", err)
	}
	defer clearBytes(payload)
	return writer.Write(ctx, corestream.Message{Type: corestream.MessageText, Data: payload})
}

func (client *StreamClient) authenticateSubscriptions(
	ctx context.Context,
	connection corestream.Connection,
	managed *managedCoinbaseStream,
) error {
	material, err := client.credentialProvider.Resolve(ctx, client.credentials.SecretRef)
	defer material.Destroy()
	if err != nil {
		return client.streamAuthenticationError(err)
	}
	if len(material.APIKey) == 0 || len(material.SecretKey) == 0 {
		return client.streamAuthenticationError(
			errors.New("Coinbase API key name and EC private key are required"),
		)
	}
	tokenSource := func() (string, error) {
		client.randomMu.Lock()
		defer client.randomMu.Unlock()
		token, signErr := SignWebSocketJWT(
			string(material.APIKey), material.SecretKey, client.now(), client.random,
		)
		if signErr != nil {
			return "", client.streamAuthenticationError(signErr)
		}
		return token, nil
	}
	return managed.writeSubscriptions(ctx, connection, "subscribe", managed.snapshot(), tokenSource)
}

func (client *StreamClient) userReconnectPolicy(err error) bool {
	var authError *StreamAuthenticationError
	if errors.As(err, &authError) || errors.Is(err, trade.ErrAuthentication) ||
		errors.Is(err, trade.ErrAuthorization) {
		return false
	}
	if client.reconnectPolicy != nil {
		return client.reconnectPolicy(err)
	}
	return corestream.DefaultReconnectPolicy(err)
}

func (client *StreamClient) resolveStreamRoute(
	options ...trade.RequestOption,
) (transport.EgressRouteID, error) {
	resolved, err := trade.ResolveRequestOptions(client.defaultRouteID, options...)
	if err != nil {
		return "", err
	}
	if resolved.Timeout != 0 {
		return "", fmt.Errorf("Coinbase stream timeout must be controlled by Run context")
	}
	return resolved.EgressRouteID, nil
}

func (client *StreamClient) streamAuthenticationError(cause error) error {
	accountID := ""
	if client.credentials != nil {
		accountID = client.credentials.AccountID
	}
	return &trade.APIError{
		Category: trade.ErrorAuthentication, Exchange: model.ExchangeCoinbase,
		AccountID: accountID, Cause: cause,
	}
}

func (client *StreamClient) streamAuthorizationError(cause error) error {
	accountID := ""
	if client.credentials != nil {
		accountID = client.credentials.AccountID
	}
	return &trade.APIError{
		Category: trade.ErrorAuthorization, Exchange: model.ExchangeCoinbase,
		AccountID: accountID, Cause: cause,
	}
}

func normalizePublicSubscriptions(
	subscriptions []StreamSubscription,
	includeHeartbeat bool,
) ([]StreamSubscription, error) {
	if len(subscriptions) == 0 {
		return nil, validationError("at least one public WebSocket subscription is required")
	}
	if len(subscriptions) > maximumStreamSubscriptions {
		return nil, validationError("WebSocket subscription count cannot exceed %d", maximumStreamSubscriptions)
	}
	grouped := make(map[StreamChannel]StreamSubscription, len(subscriptions)+1)
	for _, subscription := range subscriptions {
		validated, err := validatePublicSubscription(subscription)
		if err != nil {
			return nil, err
		}
		current, exists := grouped[validated.Channel]
		if exists && hasStreamProductOverlap(current.ProductIDs, validated.ProductIDs) {
			return nil, validationError("duplicate WebSocket product in channel %q", validated.Channel)
		}
		current.Channel = validated.Channel
		current.ProductIDs = mergeStreamProducts(current.ProductIDs, validated.ProductIDs)
		grouped[validated.Channel] = current
	}
	if includeHeartbeat {
		grouped[StreamChannelHeartbeats] = StreamSubscription{Channel: StreamChannelHeartbeats}
	}
	if len(grouped) > maximumStreamSubscriptions {
		return nil, validationError("WebSocket subscription count cannot exceed %d", maximumStreamSubscriptions)
	}
	result := make([]StreamSubscription, 0, len(grouped))
	for _, subscription := range grouped {
		result = append(result, subscription)
	}
	sort.Slice(result, func(left, right int) bool {
		return result[left].Channel < result[right].Channel
	})
	return result, nil
}

func validatePublicSubscription(subscription StreamSubscription) (StreamSubscription, error) {
	switch subscription.Channel {
	case StreamChannelHeartbeats:
		if len(subscription.ProductIDs) != 0 {
			return StreamSubscription{}, validationError("heartbeat subscription cannot contain products")
		}
		return StreamSubscription{Channel: StreamChannelHeartbeats}, nil
	case StreamChannelCandles, StreamChannelMarketTrades, StreamChannelStatus,
		StreamChannelTicker, StreamChannelTickerBatch, StreamChannelLevel2:
		if len(subscription.ProductIDs) == 0 {
			return StreamSubscription{}, validationError(
				"WebSocket channel %q requires at least one product", subscription.Channel,
			)
		}
	default:
		return StreamSubscription{}, validationError(
			"unsupported public WebSocket channel %q", subscription.Channel,
		)
	}
	products, err := validateStreamProductIDs(subscription.ProductIDs, false)
	if err != nil {
		return StreamSubscription{}, err
	}
	return StreamSubscription{Channel: subscription.Channel, ProductIDs: products}, nil
}

func validateStreamProductIDs(productIDs []string, allowEmpty bool) ([]string, error) {
	if len(productIDs) == 0 {
		if allowEmpty {
			return nil, nil
		}
		return nil, validationError("at least one WebSocket product ID is required")
	}
	if len(productIDs) > maximumStreamProductsPerChannel {
		return nil, validationError(
			"WebSocket product count cannot exceed %d", maximumStreamProductsPerChannel,
		)
	}
	result := append([]string(nil), productIDs...)
	seen := make(map[string]struct{}, len(result))
	for _, productID := range result {
		if !pathSegmentPattern.MatchString(productID) || strings.TrimSpace(productID) != productID {
			return nil, validationError("invalid WebSocket product ID %q", productID)
		}
		if _, exists := seen[productID]; exists {
			return nil, validationError("duplicate WebSocket product ID %q", productID)
		}
		seen[productID] = struct{}{}
	}
	sort.Strings(result)
	return result, nil
}

func cloneStreamSubscription(subscription StreamSubscription) StreamSubscription {
	return StreamSubscription{
		Channel: subscription.Channel, ProductIDs: append([]string(nil), subscription.ProductIDs...),
	}
}

func streamProductDifference(requested, current []string, intersection bool) []string {
	currentSet := make(map[string]struct{}, len(current))
	for _, productID := range current {
		currentSet[productID] = struct{}{}
	}
	result := make([]string, 0, len(requested))
	for _, productID := range requested {
		_, exists := currentSet[productID]
		if exists == intersection {
			result = append(result, productID)
		}
	}
	return result
}

func mergeStreamProducts(left, right []string) []string {
	result := append(append([]string(nil), left...), right...)
	sort.Strings(result)
	return result
}

func removeStreamProducts(current, removed []string) []string {
	removedSet := make(map[string]struct{}, len(removed))
	for _, productID := range removed {
		removedSet[productID] = struct{}{}
	}
	result := make([]string, 0, len(current))
	for _, productID := range current {
		if _, remove := removedSet[productID]; !remove {
			result = append(result, productID)
		}
	}
	return result
}

func hasStreamProductOverlap(left, right []string) bool {
	seen := make(map[string]struct{}, len(left))
	for _, productID := range left {
		seen[productID] = struct{}{}
	}
	for _, productID := range right {
		if _, exists := seen[productID]; exists {
			return true
		}
	}
	return false
}

func validateCoinbaseWebSocketURL(raw string, allowInsecure bool) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" ||
		parsed.RawQuery != "" || (parsed.Path != "" && parsed.Path != "/") {
		return "", fmt.Errorf("invalid WebSocket URL")
	}
	if parsed.Scheme != "wss" && !(allowInsecure && parsed.Scheme == "ws") {
		return "", fmt.Errorf("WebSocket URL must use WSS")
	}
	parsed.Path = ""
	return parsed.String(), nil
}

func isStreamAuthenticationMessage(message string) bool {
	normalized := strings.ToLower(message)
	return strings.Contains(normalized, "auth") || strings.Contains(normalized, "jwt") ||
		strings.Contains(normalized, "signature") || strings.Contains(normalized, "api key")
}

func clearBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}

// DecodeStreamMessage는 Coinbase WebSocket frame을 공통 stream 메시지로 변환한다.
func DecodeStreamMessage(message corestream.Message) (StreamMessage, error) {
	var envelope struct {
		Type           string          `json:"type"`
		Channel        StreamChannel   `json:"channel"`
		ClientID       string          `json:"client_id"`
		Timestamp      string          `json:"timestamp"`
		SequenceNumber int64           `json:"sequence_num"`
		Message        string          `json:"message"`
		Events         json.RawMessage `json:"events"`
	}
	if err := json.Unmarshal(message.Data, &envelope); err != nil {
		return StreamMessage{}, fmt.Errorf("decode Coinbase stream message: %w", err)
	}
	return StreamMessage{
		Type: envelope.Type, Channel: envelope.Channel, ClientID: envelope.ClientID,
		Timestamp: envelope.Timestamp, SequenceNumber: envelope.SequenceNumber,
		Message: envelope.Message, Events: cloneBytes(envelope.Events), Raw: cloneBytes(message.Data),
	}, nil
}
