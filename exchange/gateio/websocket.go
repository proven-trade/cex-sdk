package gateio

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
	DefaultWebSocketURL        = "wss://api.gateio.ws/ws/v4/"
	defaultStreamPingInterval  = 30 * time.Second
	defaultStreamPingTimeout   = 10 * time.Second
	maximumStreamSubscriptions = 1000
)

// StreamClientConfig는 Gate.io API v4 Spot public/private WebSocket 설정이다.
type StreamClientConfig struct {
	Connector              corestream.Connector
	Credentials            *credential.Descriptor
	CredentialProvider     credential.Provider
	DefaultEgressRouteID   transport.EgressRouteID
	WebSocketURL           string
	AllowInsecureWebSocket bool
	Now                    func() time.Time
	Observer               corestream.StateObserver
	ReconnectPolicy        corestream.ReconnectPolicy
	Backoff                corestream.Backoff
	MaxReconnectAttempts   int
	PingInterval           time.Duration
	PingTimeout            time.Duration
}

// StreamClient는 Gate.io API v4 Spot WebSocket 세션을 생성한다.
type StreamClient struct {
	connector            corestream.Connector
	credentials          *credential.Descriptor
	credentialProvider   credential.Provider
	defaultRouteID       transport.EgressRouteID
	webSocketURL         string
	now                  func() time.Time
	observer             corestream.StateObserver
	reconnectPolicy      corestream.ReconnectPolicy
	backoff              corestream.Backoff
	maxReconnectAttempts int
	pingInterval         time.Duration
	pingTimeout          time.Duration
	nextID               atomic.Int64
}

// NewStreamClient는 검증된 Gate.io API v4 Spot WebSocket 클라이언트를 생성한다.
func NewStreamClient(config StreamClientConfig) (*StreamClient, error) {
	if config.Connector == nil {
		return nil, fmt.Errorf("Gate.io stream connector is required")
	}
	defaultRouteID := transport.EgressRouteID(strings.TrimSpace(string(config.DefaultEgressRouteID)))
	if defaultRouteID == "" {
		return nil, trade.ErrMissingEgressRoute
	}
	if config.WebSocketURL == "" {
		config.WebSocketURL = DefaultWebSocketURL
	}
	webSocketURL, err := validateWebSocketURL(
		config.WebSocketURL, config.AllowInsecureWebSocket,
	)
	if err != nil {
		return nil, fmt.Errorf("invalid Gate.io WebSocket URL: %w", err)
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
		return nil, fmt.Errorf("Gate.io stream durations or reconnect attempts are invalid")
	}

	var credentialsCopy *credential.Descriptor
	if config.Credentials != nil {
		if err := config.Credentials.Validate(); err != nil {
			return nil, err
		}
		if config.Credentials.Exchange != model.ExchangeGateIO {
			return nil, fmt.Errorf("credential exchange must be Gate.io")
		}
		if config.CredentialProvider == nil {
			return nil, fmt.Errorf("credential provider is required for private Gate.io streams")
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

	client := &StreamClient{
		connector: config.Connector, credentials: credentialsCopy,
		credentialProvider: config.CredentialProvider, defaultRouteID: defaultRouteID,
		webSocketURL: webSocketURL, now: config.Now, observer: config.Observer,
		reconnectPolicy: config.ReconnectPolicy, backoff: config.Backoff,
		maxReconnectAttempts: config.MaxReconnectAttempts,
		pingInterval:         config.PingInterval, pingTimeout: config.PingTimeout,
	}
	client.nextID.Store(time.Now().UnixMicro())
	return client, nil
}

type pendingStreamCommand struct {
	operation    string
	subscription StreamSubscription
}

type managedStream struct {
	session *corestream.Session
	client  *StreamClient
	private bool

	commandMu     sync.Mutex
	stateMu       sync.Mutex
	subscriptions map[string]StreamSubscription
	pending       map[string]pendingStreamCommand
}

// PublicStream은 Gate.io public 시세 WebSocket 연결을 관리한다.
type PublicStream struct{ managed *managedStream }

// PublicStream은 선택한 EIP route에 고정된 public 시세 세션을 생성한다.
func (client *StreamClient) PublicStream(
	request StreamRequest,
	options ...trade.RequestOption,
) (*PublicStream, error) {
	subscriptions, err := validateStreamSubscriptions(request.Subscriptions, false, true)
	if err != nil {
		return nil, err
	}
	routeID, err := client.resolveStreamRoute(options...)
	if err != nil {
		return nil, err
	}
	managed := newManagedStream(client, subscriptions, false)
	session, err := client.newStreamSession(routeID, managed.resubscribe)
	if err != nil {
		return nil, err
	}
	managed.session = session
	return &PublicStream{managed: managed}, nil
}

// Run은 public 메시지를 순서대로 decode해 handler에 전달한다.
func (public *PublicStream) Run(
	ctx context.Context,
	handler func(context.Context, StreamMessage) error,
) error {
	return public.managed.run(ctx, handler)
}

// Subscribe는 public 구독을 추가하고 재연결 복구 목록에도 반영한다.
func (public *PublicStream) Subscribe(
	ctx context.Context,
	subscriptions ...StreamSubscription,
) error {
	validated, err := validateStreamSubscriptions(subscriptions, false, true)
	if err != nil {
		return err
	}
	return public.managed.change(ctx, "subscribe", validated)
}

// Unsubscribe는 public 구독을 제거하고 재연결 복구 목록에도 반영한다.
func (public *PublicStream) Unsubscribe(
	ctx context.Context,
	subscriptions ...StreamSubscription,
) error {
	validated, err := validateStreamSubscriptions(subscriptions, false, true)
	if err != nil {
		return err
	}
	return public.managed.change(ctx, "unsubscribe", validated)
}

// Close는 public stream 세션을 종료한다.
func (public *PublicStream) Close() error { return public.managed.session.Close() }

// Generation은 성공한 public 연결 세대 번호를 반환한다.
func (public *PublicStream) Generation() uint64 { return public.managed.session.Generation() }

// PrivateStream은 Gate.io private 주문·체결·잔고 WebSocket 연결을 관리한다.
type PrivateStream struct{ managed *managedStream }

// PrivateStream은 매 구독을 새로 서명하고 선택한 EIP route에 고정된 private 세션을 생성한다.
func (client *StreamClient) PrivateStream(
	request StreamRequest,
	options ...trade.RequestOption,
) (*PrivateStream, error) {
	subscriptions, err := validateStreamSubscriptions(request.Subscriptions, true, true)
	if err != nil {
		return nil, err
	}
	routeID, err := client.resolveStreamRoute(options...)
	if err != nil {
		return nil, err
	}
	if client.credentials == nil || client.credentialProvider == nil {
		return nil, client.streamAuthenticationError(
			errors.New("private Gate.io stream requires credentials"),
		)
	}
	if err := client.credentials.RequireEgressRoute(routeID); err != nil {
		return nil, client.streamAuthorizationError(err)
	}
	if err := client.credentials.RequirePermission(credential.PermissionRead); err != nil {
		return nil, client.streamAuthorizationError(err)
	}
	managed := newManagedStream(client, subscriptions, true)
	session, err := client.newStreamSession(routeID, managed.resubscribe)
	if err != nil {
		return nil, err
	}
	managed.session = session
	return &PrivateStream{managed: managed}, nil
}

// Run은 private 주문·체결·잔고 메시지를 순서대로 decode해 handler에 전달한다.
func (private *PrivateStream) Run(
	ctx context.Context,
	handler func(context.Context, StreamMessage) error,
) error {
	return private.managed.run(ctx, handler)
}

// Subscribe는 private 구독을 추가하고 재연결 복구 목록에도 반영한다.
func (private *PrivateStream) Subscribe(
	ctx context.Context,
	subscriptions ...StreamSubscription,
) error {
	validated, err := validateStreamSubscriptions(subscriptions, true, true)
	if err != nil {
		return err
	}
	return private.managed.change(ctx, "subscribe", validated)
}

// Unsubscribe는 private 구독을 제거하고 재연결 복구 목록에도 반영한다.
func (private *PrivateStream) Unsubscribe(
	ctx context.Context,
	subscriptions ...StreamSubscription,
) error {
	validated, err := validateStreamSubscriptions(subscriptions, true, true)
	if err != nil {
		return err
	}
	return private.managed.change(ctx, "unsubscribe", validated)
}

// Close는 private stream 세션을 종료한다.
func (private *PrivateStream) Close() error { return private.managed.session.Close() }

// Generation은 성공한 private 연결 세대 번호를 반환한다.
func (private *PrivateStream) Generation() uint64 { return private.managed.session.Generation() }

func (client *StreamClient) newStreamSession(
	routeID transport.EgressRouteID,
	onConnect corestream.ConnectHook,
) (*corestream.Session, error) {
	return corestream.NewSession(corestream.SessionConfig{
		Connector: client.connector, EgressRouteID: routeID,
		Request:   corestream.DialRequest{Endpoint: client.webSocketURL},
		OnConnect: onConnect, Observer: client.observer,
		ReconnectPolicy: client.streamReconnectPolicy, Backoff: client.backoff,
		MaxReconnectAttempts: client.maxReconnectAttempts,
		PingInterval:         client.pingInterval, PingTimeout: client.pingTimeout,
	})
}

func newManagedStream(
	client *StreamClient,
	subscriptions []StreamSubscription,
	private bool,
) *managedStream {
	managed := &managedStream{
		client: client, private: private,
		subscriptions: make(map[string]StreamSubscription, len(subscriptions)),
		pending:       make(map[string]pendingStreamCommand),
	}
	for _, subscription := range subscriptions {
		managed.subscriptions[streamSubscriptionKey(subscription)] = subscription
	}
	return managed
}

func (managed *managedStream) run(
	ctx context.Context,
	handler func(context.Context, StreamMessage) error,
) error {
	if handler == nil {
		return fmt.Errorf("Gate.io stream handler is required")
	}
	return managed.session.Run(ctx, func(ctx context.Context, message corestream.Message) error {
		decoded, err := DecodeStreamMessage(message)
		if err != nil {
			return err
		}
		managed.handleControl(decoded)
		return handler(ctx, decoded)
	})
}

func (managed *managedStream) resubscribe(
	ctx context.Context,
	connection corestream.Connection,
) error {
	managed.commandMu.Lock()
	defer managed.commandMu.Unlock()
	subscriptions := managed.snapshotSubscriptions()
	managed.stateMu.Lock()
	managed.pending = make(map[string]pendingStreamCommand, len(subscriptions))
	managed.stateMu.Unlock()
	for _, subscription := range subscriptions {
		payload, id, err := managed.client.encodeStreamCommand(
			ctx, "subscribe", subscription, managed.private,
		)
		if err != nil {
			return err
		}
		if err := connection.Write(
			ctx, corestream.Message{Type: corestream.MessageText, Data: payload},
		); err != nil {
			return err
		}
		managed.stateMu.Lock()
		managed.pending[id] = pendingStreamCommand{
			operation: "subscribe", subscription: subscription,
		}
		managed.stateMu.Unlock()
	}
	return nil
}

func (managed *managedStream) change(
	ctx context.Context,
	operation string,
	subscriptions []StreamSubscription,
) error {
	if ctx == nil {
		return fmt.Errorf("Gate.io stream command context is nil")
	}
	managed.commandMu.Lock()
	defer managed.commandMu.Unlock()
	for _, subscription := range subscriptions {
		key := streamSubscriptionKey(subscription)
		managed.stateMu.Lock()
		_, exists := managed.subscriptions[key]
		count := len(managed.subscriptions)
		managed.stateMu.Unlock()
		if operation == "subscribe" && exists || operation == "unsubscribe" && !exists {
			continue
		}
		if operation == "subscribe" && count >= maximumStreamSubscriptions {
			return validationError(
				"WebSocket subscription count cannot exceed %d", maximumStreamSubscriptions,
			)
		}
		payload, id, err := managed.client.encodeStreamCommand(
			ctx, operation, subscription, managed.private,
		)
		if err != nil {
			return err
		}
		managed.stateMu.Lock()
		managed.pending[id] = pendingStreamCommand{
			operation: operation, subscription: subscription,
		}
		if operation == "subscribe" {
			managed.subscriptions[key] = subscription
		} else {
			delete(managed.subscriptions, key)
		}
		managed.stateMu.Unlock()
		if err := managed.session.Write(
			ctx, corestream.Message{Type: corestream.MessageText, Data: payload},
		); err != nil {
			managed.rollbackCommand(id)
			return err
		}
	}
	return nil
}

func (managed *managedStream) handleControl(message StreamMessage) {
	if message.ID == "" || message.Event != "subscribe" && message.Event != "unsubscribe" {
		return
	}
	managed.stateMu.Lock()
	defer managed.stateMu.Unlock()
	pending, exists := managed.pending[message.ID]
	if !exists {
		return
	}
	delete(managed.pending, message.ID)
	if message.Error == nil {
		return
	}
	managed.rollbackCommandLocked(pending)
}

func (managed *managedStream) rollbackCommand(id string) {
	managed.stateMu.Lock()
	defer managed.stateMu.Unlock()
	pending, exists := managed.pending[id]
	if !exists {
		return
	}
	delete(managed.pending, id)
	managed.rollbackCommandLocked(pending)
}

func (managed *managedStream) rollbackCommandLocked(pending pendingStreamCommand) {
	key := streamSubscriptionKey(pending.subscription)
	if pending.operation == "subscribe" {
		delete(managed.subscriptions, key)
	} else {
		managed.subscriptions[key] = pending.subscription
	}
}

func (managed *managedStream) snapshotSubscriptions() []StreamSubscription {
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

type streamAuthentication struct {
	Method    string `json:"method"`
	APIKey    string `json:"KEY"`
	Signature string `json:"SIGN"`
}

func (client *StreamClient) encodeStreamCommand(
	ctx context.Context,
	operation string,
	subscription StreamSubscription,
	private bool,
) ([]byte, string, error) {
	timestamp := client.now().Unix()
	if timestamp <= 0 {
		return nil, "", validationError("Gate.io stream timestamp must be after the Unix epoch")
	}
	id := client.nextID.Add(1)
	channel := streamChannelName(subscription.Channel)
	var authentication *streamAuthentication
	if private {
		material, err := client.credentialProvider.Resolve(ctx, client.credentials.SecretRef)
		defer material.Destroy()
		if err != nil {
			return nil, "", client.streamAuthenticationError(err)
		}
		if len(material.APIKey) == 0 || len(material.SecretKey) == 0 {
			return nil, "", client.streamAuthenticationError(
				errors.New("Gate.io stream API key and HMAC secret are required"),
			)
		}
		signatureText := fmt.Sprintf(
			"channel=%s&event=%s&time=%d", channel, operation, timestamp,
		)
		signature, err := SignHMACSHA512(material.SecretKey, []byte(signatureText))
		if err != nil {
			return nil, "", client.streamAuthenticationError(err)
		}
		authentication = &streamAuthentication{
			Method: "api_key", APIKey: string(material.APIKey), Signature: signature,
		}
	}
	payload, err := json.Marshal(struct {
		Time    int64                 `json:"time"`
		ID      int64                 `json:"id"`
		Channel string                `json:"channel"`
		Event   string                `json:"event"`
		Payload []string              `json:"payload,omitempty"`
		Auth    *streamAuthentication `json:"auth,omitempty"`
	}{
		Time: timestamp, ID: id, Channel: channel, Event: operation,
		Payload: streamSubscriptionPayload(subscription), Auth: authentication,
	})
	if err != nil {
		return nil, "", fmt.Errorf("encode Gate.io stream command: %w", err)
	}
	return payload, strconv.FormatInt(id, 10), nil
}

func validateStreamSubscriptions(
	subscriptions []StreamSubscription,
	private bool,
	requireNonEmpty bool,
) ([]StreamSubscription, error) {
	if requireNonEmpty && len(subscriptions) == 0 {
		return nil, validationError("WebSocket subscription is required")
	}
	if len(subscriptions) > maximumStreamSubscriptions {
		return nil, validationError(
			"WebSocket subscription count cannot exceed %d", maximumStreamSubscriptions,
		)
	}
	result := make([]StreamSubscription, 0, len(subscriptions))
	seen := make(map[string]struct{}, len(subscriptions))
	for _, subscription := range subscriptions {
		if err := validateStreamSubscription(subscription, private); err != nil {
			return nil, err
		}
		key := streamSubscriptionKey(subscription)
		if _, exists := seen[key]; exists {
			return nil, validationError("duplicate WebSocket subscription")
		}
		seen[key] = struct{}{}
		result = append(result, subscription)
	}
	return result, nil
}

func validateStreamSubscription(subscription StreamSubscription, private bool) error {
	if private {
		switch subscription.Channel {
		case StreamChannelOrders, StreamChannelUserTrades:
			if err := validateCurrencyPair(subscription.CurrencyPair); err != nil {
				return err
			}
		case StreamChannelBalances:
			if subscription.CurrencyPair != "" {
				return validationError("balance WebSocket subscription does not accept currency pair")
			}
		default:
			return validationError("unsupported private WebSocket channel %q", subscription.Channel)
		}
		if subscription.CandleInterval != "" || subscription.UpdateInterval != "" {
			return validationError("private WebSocket subscription does not accept interval")
		}
		return nil
	}
	switch subscription.Channel {
	case StreamChannelTicker, StreamChannelTrades, StreamChannelBookTicker:
	case StreamChannelCandles:
		if !streamCandleIntervalValid(subscription.CandleInterval) {
			return validationError(
				"unsupported WebSocket candle interval %q", subscription.CandleInterval,
			)
		}
	case StreamChannelOrderBookUpdate:
		if subscription.UpdateInterval != StreamUpdate20Millis &&
			subscription.UpdateInterval != StreamUpdate100Millis {
			return validationError(
				"unsupported WebSocket order book interval %q", subscription.UpdateInterval,
			)
		}
	default:
		return validationError("unsupported public WebSocket channel %q", subscription.Channel)
	}
	if err := validateCurrencyPair(subscription.CurrencyPair); err != nil {
		return err
	}
	if subscription.Channel != StreamChannelCandles && subscription.CandleInterval != "" {
		return validationError("WebSocket candle interval is only supported for candles")
	}
	if subscription.Channel != StreamChannelOrderBookUpdate && subscription.UpdateInterval != "" {
		return validationError("WebSocket update interval is only supported for order book updates")
	}
	return nil
}

func streamCandleIntervalValid(interval CandleInterval) bool {
	switch interval {
	case Candle10Seconds, Candle1Minute, Candle5Minutes, Candle15Minutes,
		Candle30Minutes, Candle1Hour, Candle4Hours, Candle8Hours, Candle1Day, Candle7Days:
		return true
	default:
		return false
	}
}

func streamChannelName(channel StreamChannel) string {
	switch channel {
	case StreamChannelTicker:
		return "spot.tickers"
	case StreamChannelTrades:
		return "spot.trades"
	case StreamChannelCandles:
		return "spot.candlesticks"
	case StreamChannelBookTicker:
		return "spot.book_ticker"
	case StreamChannelOrderBookUpdate:
		return "spot.order_book_update"
	case StreamChannelOrders:
		return "spot.orders"
	case StreamChannelUserTrades:
		return "spot.usertrades"
	case StreamChannelBalances:
		return "spot.balances"
	default:
		return ""
	}
}

func streamSubscriptionPayload(subscription StreamSubscription) []string {
	switch subscription.Channel {
	case StreamChannelCandles:
		return []string{string(subscription.CandleInterval), subscription.CurrencyPair}
	case StreamChannelOrderBookUpdate:
		return []string{subscription.CurrencyPair, string(subscription.UpdateInterval)}
	case StreamChannelBalances:
		return nil
	default:
		return []string{subscription.CurrencyPair}
	}
}

func streamSubscriptionKey(subscription StreamSubscription) string {
	return string(subscription.Channel) + "\x00" + subscription.CurrencyPair + "\x00" +
		string(subscription.CandleInterval) + "\x00" + string(subscription.UpdateInterval)
}

func (client *StreamClient) resolveStreamRoute(
	options ...trade.RequestOption,
) (transport.EgressRouteID, error) {
	resolved, err := trade.ResolveRequestOptions(client.defaultRouteID, options...)
	if err != nil {
		return "", err
	}
	if resolved.Timeout != 0 {
		return "", fmt.Errorf("Gate.io stream timeout must be controlled by Run context")
	}
	return resolved.EgressRouteID, nil
}

func (client *StreamClient) streamReconnectPolicy(cause error) bool {
	if errors.Is(cause, trade.ErrAuthentication) || errors.Is(cause, trade.ErrAuthorization) ||
		errors.Is(cause, trade.ErrValidation) {
		return false
	}
	if client.reconnectPolicy != nil {
		return client.reconnectPolicy(cause)
	}
	return corestream.DefaultReconnectPolicy(cause)
}

func (client *StreamClient) streamAuthenticationError(cause error) error {
	accountID := ""
	if client.credentials != nil {
		accountID = client.credentials.AccountID
	}
	return &trade.APIError{
		Category: trade.ErrorAuthentication, Exchange: model.ExchangeGateIO,
		AccountID: accountID, Cause: cause,
	}
}

func (client *StreamClient) streamAuthorizationError(cause error) error {
	accountID := ""
	if client.credentials != nil {
		accountID = client.credentials.AccountID
	}
	return &trade.APIError{
		Category: trade.ErrorAuthorization, Exchange: model.ExchangeGateIO,
		AccountID: accountID, Cause: cause,
	}
}

func validateWebSocketURL(value string, allowInsecure bool) (string, error) {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return "", fmt.Errorf("WebSocket URL is malformed")
	}
	if parsed.Scheme != "wss" && !(allowInsecure && parsed.Scheme == "ws") {
		return "", fmt.Errorf("WebSocket URL must use WSS")
	}
	return parsed.String(), nil
}
