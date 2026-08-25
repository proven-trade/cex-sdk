package cryptocom

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

	trade "github.com/proven-trade/proven-trade-sdk"
	"github.com/proven-trade/proven-trade-sdk/credential"
	"github.com/proven-trade/proven-trade-sdk/model"
	corestream "github.com/proven-trade/proven-trade-sdk/stream"
	"github.com/proven-trade/proven-trade-sdk/transport"
)

const (
	DefaultMarketWebSocketURL            = "wss://stream.crypto.com/exchange/v1/market"
	DefaultUATMarketWebSocketURL         = "wss://uat-stream.3ona.co/exchange/v1/market"
	DefaultUserWebSocketURL              = "wss://stream.crypto.com/exchange/v1/user"
	DefaultUATUserWebSocketURL           = "wss://uat-stream.3ona.co/exchange/v1/user"
	DefaultStreamConnectionReadyDelay    = time.Second
	DefaultMarketStreamRequestsPerSecond = 100
	DefaultUserStreamRequestsPerSecond   = 150
	maximumCryptoComStreamSubscriptions  = 200
)

// StreamClientConfig는 Crypto.com 공개 시세와 private 사용자 WebSocket 설정이다.
type StreamClientConfig struct {
	Connector               corestream.Connector
	Credentials             *credential.Descriptor
	CredentialProvider      credential.Provider
	DefaultEgressRouteID    transport.EgressRouteID
	MarketWebSocketURL      string
	UserWebSocketURL        string
	AllowInsecureWebSocket  bool
	ConnectionReadyDelay    time.Duration
	MarketRequestsPerSecond int
	UserRequestsPerSecond   int
	Now                     func() time.Time
	Observer                corestream.StateObserver
	ReconnectPolicy         corestream.ReconnectPolicy
	Backoff                 corestream.Backoff
	MaxReconnectAttempts    int
}

// StreamClient는 Crypto.com 공개 시세와 private 사용자 WebSocket 세션을 생성한다.
type StreamClient struct {
	connector             corestream.Connector
	credentials           *credential.Descriptor
	credentialProvider    credential.Provider
	defaultRouteID        transport.EgressRouteID
	marketURL             string
	userURL               string
	connectionReadyDelay  time.Duration
	marketCommandInterval time.Duration
	userCommandInterval   time.Duration
	now                   func() time.Time
	observer              corestream.StateObserver
	reconnectPolicy       corestream.ReconnectPolicy
	backoff               corestream.Backoff
	maxReconnectAttempts  int
	nextID                atomic.Int64
}

// NewStreamClient는 검증된 Crypto.com 공개 시세와 private 사용자 WebSocket 클라이언트를 생성한다.
func NewStreamClient(config StreamClientConfig) (*StreamClient, error) {
	if config.Connector == nil {
		return nil, fmt.Errorf("Crypto.com stream connector is required")
	}
	defaultRouteID := transport.EgressRouteID(strings.TrimSpace(string(config.DefaultEgressRouteID)))
	if defaultRouteID == "" {
		return nil, trade.ErrMissingEgressRoute
	}
	if config.MarketWebSocketURL == "" {
		config.MarketWebSocketURL = DefaultMarketWebSocketURL
	}
	marketURL, err := validateCryptoComStreamURL(
		config.MarketWebSocketURL, config.AllowInsecureWebSocket,
	)
	if err != nil {
		return nil, fmt.Errorf("invalid Crypto.com market WebSocket URL: %w", err)
	}
	if config.UserWebSocketURL == "" {
		config.UserWebSocketURL = DefaultUserWebSocketURL
	}
	userURL, err := validateCryptoComStreamURL(
		config.UserWebSocketURL, config.AllowInsecureWebSocket,
	)
	if err != nil {
		return nil, fmt.Errorf("invalid Crypto.com user WebSocket URL: %w", err)
	}
	if config.ConnectionReadyDelay == 0 {
		config.ConnectionReadyDelay = DefaultStreamConnectionReadyDelay
	}
	if config.ConnectionReadyDelay < 0 {
		return nil, fmt.Errorf("Crypto.com connection ready delay cannot be negative")
	}
	if config.MarketRequestsPerSecond == 0 {
		config.MarketRequestsPerSecond = DefaultMarketStreamRequestsPerSecond
	}
	if config.MarketRequestsPerSecond < 1 ||
		config.MarketRequestsPerSecond > DefaultMarketStreamRequestsPerSecond {
		return nil, fmt.Errorf("Crypto.com market stream quota must be between 1 and 100")
	}
	if config.UserRequestsPerSecond == 0 {
		config.UserRequestsPerSecond = DefaultUserStreamRequestsPerSecond
	}
	if config.UserRequestsPerSecond < 1 ||
		config.UserRequestsPerSecond > DefaultUserStreamRequestsPerSecond {
		return nil, fmt.Errorf("Crypto.com user stream quota must be between 1 and 150")
	}
	if config.MaxReconnectAttempts < 0 {
		return nil, fmt.Errorf("Crypto.com maximum reconnect attempts cannot be negative")
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	var credentialsCopy *credential.Descriptor
	if config.Credentials != nil {
		if err := config.Credentials.Validate(); err != nil {
			return nil, err
		}
		if config.Credentials.Exchange != model.ExchangeCryptoCom {
			return nil, fmt.Errorf("credential exchange must be Crypto.com")
		}
		if config.CredentialProvider == nil {
			return nil, fmt.Errorf("credential provider is required for private Crypto.com stream")
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
		credentialProvider: config.CredentialProvider,
		defaultRouteID:     defaultRouteID, marketURL: marketURL, userURL: userURL,
		connectionReadyDelay:  config.ConnectionReadyDelay,
		marketCommandInterval: time.Second / time.Duration(config.MarketRequestsPerSecond),
		userCommandInterval:   time.Second / time.Duration(config.UserRequestsPerSecond),
		now:                   config.Now, observer: config.Observer,
		reconnectPolicy: config.ReconnectPolicy, backoff: config.Backoff,
		maxReconnectAttempts: config.MaxReconnectAttempts,
	}, nil
}

type pendingCryptoComStreamCommand struct {
	operation    string
	subscription StreamSubscription
}

type managedCryptoComStream struct {
	session *corestream.Session
	client  *StreamClient

	commandMu     sync.Mutex
	stateMu       sync.Mutex
	subscriptions map[string]StreamSubscription
	pending       map[string]pendingCryptoComStreamCommand
	lastCommandAt time.Time
}

// PublicStream은 Crypto.com 공개 시세 WebSocket 연결을 관리한다.
type PublicStream struct {
	managed *managedCryptoComStream
}

// PublicStream은 선택한 EIP route에 고정된 공개 시세 세션을 생성한다.
func (client *StreamClient) PublicStream(
	request StreamRequest,
	options ...trade.RequestOption,
) (*PublicStream, error) {
	subscriptions, err := validateCryptoComStreamSubscriptions(request.Subscriptions, true)
	if err != nil {
		return nil, err
	}
	routeID, err := client.resolveStreamRoute(options...)
	if err != nil {
		return nil, err
	}
	managed := newManagedCryptoComStream(client, subscriptions)
	session, err := corestream.NewSession(corestream.SessionConfig{
		Connector: client.connector, EgressRouteID: routeID,
		Request:   corestream.DialRequest{Endpoint: client.marketURL},
		OnConnect: managed.resubscribe, Observer: client.observer,
		ReconnectPolicy: client.streamReconnectPolicy, Backoff: client.backoff,
		MaxReconnectAttempts: client.maxReconnectAttempts,
	})
	if err != nil {
		return nil, err
	}
	managed.session = session
	return &PublicStream{managed: managed}, nil
}

// Run은 공개 시세 메시지와 heartbeat를 순서대로 해석해 handler에 전달한다.
func (public *PublicStream) Run(
	ctx context.Context,
	handler func(context.Context, StreamMessage) error,
) error {
	if handler == nil {
		return fmt.Errorf("Crypto.com stream handler is required")
	}
	return public.managed.session.Run(ctx, func(ctx context.Context, message corestream.Message) error {
		decoded, err := DecodeStreamMessage(message)
		if err != nil {
			return err
		}
		if decoded.Private {
			return fmt.Errorf("Crypto.com market stream received private user data")
		}
		if decoded.Heartbeat {
			if err := public.managed.respondHeartbeat(ctx, decoded.ID); err != nil {
				if reconnectErr := public.managed.session.Reconnect(); reconnectErr != nil {
					return err
				}
				return nil
			}
		}
		if err := public.managed.handleControl(decoded); err != nil {
			return err
		}
		return handler(ctx, decoded)
	})
}

// Subscribe는 공개 시세 구독을 추가하고 재연결 복구 목록에도 반영한다.
func (public *PublicStream) Subscribe(
	ctx context.Context,
	subscriptions ...StreamSubscription,
) error {
	validated, err := validateCryptoComStreamSubscriptions(subscriptions, true)
	if err != nil {
		return err
	}
	return public.managed.change(ctx, "subscribe", validated)
}

// Unsubscribe는 공개 시세 구독을 제거하고 재연결 복구 목록에도 반영한다.
func (public *PublicStream) Unsubscribe(
	ctx context.Context,
	subscriptions ...StreamSubscription,
) error {
	validated, err := validateCryptoComStreamSubscriptions(subscriptions, true)
	if err != nil {
		return err
	}
	return public.managed.change(ctx, "unsubscribe", validated)
}

// Close는 공개 시세 stream 세션을 종료한다.
func (public *PublicStream) Close() error {
	return public.managed.session.Close()
}

// Generation은 성공한 공개 시세 연결 세대 번호를 반환한다.
func (public *PublicStream) Generation() uint64 {
	return public.managed.session.Generation()
}

// EgressRouteID는 최초 연결과 모든 재연결에 고정된 송신 경로를 반환한다.
func (public *PublicStream) EgressRouteID() transport.EgressRouteID {
	return public.managed.session.EgressRouteID()
}

func (public *PublicStream) reconnect() error {
	return public.managed.session.Reconnect()
}

func newManagedCryptoComStream(
	client *StreamClient,
	subscriptions []StreamSubscription,
) *managedCryptoComStream {
	managed := &managedCryptoComStream{
		client: client, subscriptions: make(map[string]StreamSubscription, len(subscriptions)),
		pending: make(map[string]pendingCryptoComStreamCommand),
	}
	for _, subscription := range subscriptions {
		managed.subscriptions[cryptoComStreamSubscriptionKey(subscription)] = subscription
	}
	return managed
}

func (managed *managedCryptoComStream) resubscribe(
	ctx context.Context,
	connection corestream.Connection,
) error {
	managed.commandMu.Lock()
	defer managed.commandMu.Unlock()
	if err := waitCryptoComStreamDuration(ctx, managed.client.connectionReadyDelay); err != nil {
		return err
	}
	managed.lastCommandAt = time.Time{}
	subscriptions := managed.snapshotSubscriptions()
	managed.stateMu.Lock()
	managed.pending = make(map[string]pendingCryptoComStreamCommand, len(subscriptions))
	managed.stateMu.Unlock()
	for _, subscription := range subscriptions {
		payload, id, err := managed.client.encodeStreamCommand("subscribe", subscription)
		if err != nil {
			return err
		}
		if err := managed.writeConnection(ctx, connection, payload); err != nil {
			return err
		}
		managed.stateMu.Lock()
		managed.pending[id] = pendingCryptoComStreamCommand{
			operation: "subscribe", subscription: subscription,
		}
		managed.stateMu.Unlock()
	}
	return nil
}

func (managed *managedCryptoComStream) change(
	ctx context.Context,
	operation string,
	subscriptions []StreamSubscription,
) error {
	if ctx == nil {
		return fmt.Errorf("Crypto.com stream command context is nil")
	}
	managed.commandMu.Lock()
	defer managed.commandMu.Unlock()
	for _, subscription := range subscriptions {
		key := cryptoComStreamSubscriptionKey(subscription)
		managed.stateMu.Lock()
		_, exists := managed.subscriptions[key]
		count := len(managed.subscriptions)
		managed.stateMu.Unlock()
		if operation == "subscribe" && exists || operation == "unsubscribe" && !exists {
			continue
		}
		if operation == "subscribe" && count >= maximumCryptoComStreamSubscriptions {
			return validationError(
				"WebSocket subscription count cannot exceed %d", maximumCryptoComStreamSubscriptions,
			)
		}
		payload, id, err := managed.client.encodeStreamCommand(operation, subscription)
		if err != nil {
			return err
		}
		managed.stateMu.Lock()
		managed.pending[id] = pendingCryptoComStreamCommand{
			operation: operation, subscription: subscription,
		}
		if operation == "subscribe" {
			managed.subscriptions[key] = subscription
		} else {
			delete(managed.subscriptions, key)
		}
		managed.stateMu.Unlock()
		if err := managed.writeSession(ctx, payload); err != nil {
			managed.rollbackCommand(id)
			return err
		}
	}
	return nil
}

func (managed *managedCryptoComStream) respondHeartbeat(ctx context.Context, id string) error {
	payload, err := json.Marshal(struct {
		ID     string `json:"id"`
		Method string `json:"method"`
	}{ID: id, Method: "public/respond-heartbeat"})
	if err != nil {
		return fmt.Errorf("encode Crypto.com heartbeat response: %w", err)
	}
	managed.commandMu.Lock()
	defer managed.commandMu.Unlock()
	return managed.writeSession(ctx, payload)
}

func (managed *managedCryptoComStream) writeSession(ctx context.Context, payload []byte) error {
	if err := managed.waitCommandSlot(ctx); err != nil {
		return err
	}
	err := managed.session.Write(
		ctx, corestream.Message{Type: corestream.MessageText, Data: payload},
	)
	managed.lastCommandAt = time.Now()
	return err
}

func (managed *managedCryptoComStream) writeConnection(
	ctx context.Context,
	connection corestream.Connection,
	payload []byte,
) error {
	if err := managed.waitCommandSlot(ctx); err != nil {
		return err
	}
	err := connection.Write(ctx, corestream.Message{Type: corestream.MessageText, Data: payload})
	managed.lastCommandAt = time.Now()
	return err
}

func (managed *managedCryptoComStream) waitCommandSlot(ctx context.Context) error {
	delay := time.Until(managed.lastCommandAt.Add(managed.client.marketCommandInterval))
	if delay <= 0 {
		return nil
	}
	return waitCryptoComStreamDuration(ctx, delay)
}

func (managed *managedCryptoComStream) handleControl(message StreamMessage) error {
	if message.ID == "" || message.Heartbeat ||
		(message.Method != "subscribe" && message.Method != "unsubscribe") {
		return nil
	}
	managed.stateMu.Lock()
	defer managed.stateMu.Unlock()
	pending, exists := managed.pending[message.ID]
	if !exists {
		return nil
	}
	delete(managed.pending, message.ID)
	if message.Method != pending.operation {
		managed.rollbackCommandLocked(pending)
		return fmt.Errorf(
			"Crypto.com stream response method %q does not match %q",
			message.Method, pending.operation,
		)
	}
	if message.Error != nil {
		managed.rollbackCommandLocked(pending)
	}
	return nil
}

func (managed *managedCryptoComStream) rollbackCommand(id string) {
	managed.stateMu.Lock()
	defer managed.stateMu.Unlock()
	pending, exists := managed.pending[id]
	if !exists {
		return
	}
	delete(managed.pending, id)
	managed.rollbackCommandLocked(pending)
}

func (managed *managedCryptoComStream) rollbackCommandLocked(
	pending pendingCryptoComStreamCommand,
) {
	key := cryptoComStreamSubscriptionKey(pending.subscription)
	if pending.operation == "subscribe" {
		delete(managed.subscriptions, key)
	} else {
		managed.subscriptions[key] = pending.subscription
	}
}

func (managed *managedCryptoComStream) snapshotSubscriptions() []StreamSubscription {
	managed.stateMu.Lock()
	defer managed.stateMu.Unlock()
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

func (client *StreamClient) encodeStreamCommand(
	operation string,
	subscription StreamSubscription,
) ([]byte, string, error) {
	if operation != "subscribe" && operation != "unsubscribe" {
		return nil, "", fmt.Errorf("unsupported Crypto.com stream operation %q", operation)
	}
	nonceValue := client.now().UTC().UnixMilli()
	if nonceValue <= 0 {
		return nil, "", validationError("Crypto.com stream nonce must be after the Unix epoch")
	}
	id := strconv.FormatInt(client.nextID.Add(1), 10)
	type commandParams struct {
		Channels             []string `json:"channels"`
		BookSubscriptionType string   `json:"book_subscription_type,omitempty"`
		BookUpdateFrequency  string   `json:"book_update_frequency,omitempty"`
	}
	request := struct {
		ID     string        `json:"id"`
		Method string        `json:"method"`
		Params commandParams `json:"params"`
		Nonce  string        `json:"nonce"`
	}{
		ID: id, Method: operation,
		Params: commandParams{Channels: []string{cryptoComStreamSubscriptionTopic(subscription)}},
		Nonce:  strconv.FormatInt(nonceValue, 10),
	}
	if operation == "subscribe" && subscription.Channel == StreamChannelBook {
		request.Params.BookSubscriptionType = string(subscription.BookSubscriptionType)
		request.Params.BookUpdateFrequency = string(subscription.BookUpdateFrequency)
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return nil, "", fmt.Errorf("encode Crypto.com stream command: %w", err)
	}
	return payload, id, nil
}

func validateCryptoComStreamSubscriptions(
	subscriptions []StreamSubscription,
	requireNonEmpty bool,
) ([]StreamSubscription, error) {
	if requireNonEmpty && len(subscriptions) == 0 {
		return nil, validationError("WebSocket subscription is required")
	}
	if len(subscriptions) > maximumCryptoComStreamSubscriptions {
		return nil, validationError(
			"WebSocket subscription count cannot exceed %d", maximumCryptoComStreamSubscriptions,
		)
	}
	result := make([]StreamSubscription, 0, len(subscriptions))
	seen := make(map[string]struct{}, len(subscriptions))
	for _, subscription := range subscriptions {
		if err := validateCryptoComStreamSubscription(subscription); err != nil {
			return nil, err
		}
		key := cryptoComStreamSubscriptionKey(subscription)
		if _, exists := seen[key]; exists {
			return nil, validationError("duplicate WebSocket subscription")
		}
		seen[key] = struct{}{}
		result = append(result, subscription)
	}
	return result, nil
}

func validateCryptoComStreamSubscription(subscription StreamSubscription) error {
	if err := validateInstrumentName(subscription.InstrumentName); err != nil {
		return err
	}
	switch subscription.Channel {
	case StreamChannelTicker, StreamChannelTrades:
		if subscription.CandleTimeframe != "" || subscription.BookDepth != 0 ||
			subscription.BookSubscriptionType != "" || subscription.BookUpdateFrequency != "" {
			return validationError(
				"WebSocket channel %q does not accept channel-specific settings", subscription.Channel,
			)
		}
	case StreamChannelCandles:
		if !subscription.CandleTimeframe.valid() {
			return validationError(
				"unsupported WebSocket candle timeframe %q", subscription.CandleTimeframe,
			)
		}
		if subscription.BookDepth != 0 || subscription.BookSubscriptionType != "" ||
			subscription.BookUpdateFrequency != "" {
			return validationError("candle WebSocket subscription does not accept book settings")
		}
	case StreamChannelBook:
		if subscription.CandleTimeframe != "" {
			return validationError("book WebSocket subscription does not accept candle timeframe")
		}
		if !subscription.BookDepth.valid() {
			return validationError("unsupported WebSocket book depth %d", subscription.BookDepth)
		}
		if !subscription.BookSubscriptionType.valid() {
			return validationError(
				"unsupported WebSocket book subscription type %q",
				subscription.BookSubscriptionType,
			)
		}
		if !subscription.BookUpdateFrequency.valid() {
			return validationError(
				"unsupported WebSocket book update frequency %q",
				subscription.BookUpdateFrequency,
			)
		}
		if subscription.BookSubscriptionType == StreamBookSnapshot &&
			subscription.BookUpdateFrequency != StreamBookUpdate500Milliseconds {
			return validationError("snapshot WebSocket book frequency must be 500 milliseconds")
		}
	default:
		return validationError("unsupported public WebSocket channel %q", subscription.Channel)
	}
	return nil
}

func (value StreamBookDepth) valid() bool {
	return value == StreamBookDepth10 || value == StreamBookDepth50
}

func (value StreamBookSubscriptionType) valid() bool {
	return value == StreamBookSnapshot || value == StreamBookSnapshotAndUpdate
}

func (value StreamBookUpdateFrequency) valid() bool {
	return value == StreamBookUpdate10Milliseconds ||
		value == StreamBookUpdate100Milliseconds || value == StreamBookUpdate500Milliseconds
}

func cryptoComStreamSubscriptionTopic(subscription StreamSubscription) string {
	switch subscription.Channel {
	case StreamChannelTicker, StreamChannelTrades:
		return string(subscription.Channel) + "." + subscription.InstrumentName
	case StreamChannelCandles:
		return string(subscription.Channel) + "." + string(subscription.CandleTimeframe) + "." +
			subscription.InstrumentName
	case StreamChannelBook:
		return string(subscription.Channel) + "." + subscription.InstrumentName + "." +
			strconv.Itoa(int(subscription.BookDepth))
	default:
		return ""
	}
}

func cryptoComStreamSubscriptionKey(subscription StreamSubscription) string {
	return cryptoComStreamSubscriptionTopic(subscription)
}

func (client *StreamClient) resolveStreamRoute(
	options ...trade.RequestOption,
) (transport.EgressRouteID, error) {
	resolved, err := trade.ResolveRequestOptions(client.defaultRouteID, options...)
	if err != nil {
		return "", err
	}
	if resolved.Timeout != 0 {
		return "", fmt.Errorf("Crypto.com stream timeout must be controlled by Run context")
	}
	return resolved.EgressRouteID, nil
}

func (client *StreamClient) streamReconnectPolicy(cause error) bool {
	if errors.Is(cause, trade.ErrValidation) || errors.Is(cause, trade.ErrAuthentication) ||
		errors.Is(cause, trade.ErrAuthorization) {
		return false
	}
	if client.reconnectPolicy != nil {
		return client.reconnectPolicy(cause)
	}
	return corestream.DefaultReconnectPolicy(cause)
}

func validateCryptoComStreamURL(value string, allowInsecure bool) (string, error) {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" ||
		parsed.Fragment != "" {
		return "", fmt.Errorf("WebSocket URL is malformed")
	}
	if parsed.Scheme != "wss" && !(allowInsecure && parsed.Scheme == "ws") {
		return "", fmt.Errorf("WebSocket URL must use WSS")
	}
	return parsed.String(), nil
}

func waitCryptoComStreamDuration(ctx context.Context, duration time.Duration) error {
	if duration <= 0 {
		return nil
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
