package futures

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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
	defaultStreamPingInterval   = 15 * time.Second
	defaultStreamPingTimeout    = 9 * time.Second
	defaultSubscriptionInterval = 100 * time.Millisecond
	maximumStreamSubscriptions  = 300
)

// ConnectIDSource는 Futures WebSocket handshake마다 고유한 connectId를 생성한다.
type ConnectIDSource func() (string, error)

// StreamClientConfig는 KuCoin Classic Futures public/private WebSocket 설정이다.
type StreamClientConfig struct {
	Connector              corestream.Connector
	RESTClient             *Client
	DefaultEgressRouteID   transport.EgressRouteID
	AllowInsecureWebSocket bool
	Observer               corestream.StateObserver
	ReconnectPolicy        corestream.ReconnectPolicy
	Backoff                corestream.Backoff
	MaxReconnectAttempts   int
	PingInterval           time.Duration
	PingTimeout            time.Duration
	SubscriptionInterval   time.Duration
	ConnectIDSource        ConnectIDSource
}

// StreamClient는 KuCoin Classic Futures public/private WebSocket 세션을 생성한다.
type StreamClient struct {
	connector              corestream.Connector
	restClient             *Client
	defaultRouteID         transport.EgressRouteID
	allowInsecureWebSocket bool
	observer               corestream.StateObserver
	reconnectPolicy        corestream.ReconnectPolicy
	backoff                corestream.Backoff
	maxReconnectAttempts   int
	pingInterval           time.Duration
	pingTimeout            time.Duration
	subscriptionInterval   time.Duration
	connectIDSource        ConnectIDSource
	nextMessageID          atomic.Int64
}

// NewStreamClient는 KuCoin Classic Futures WebSocket 클라이언트를 생성한다.
func NewStreamClient(config StreamClientConfig) (*StreamClient, error) {
	if config.Connector == nil {
		return nil, fmt.Errorf("KuCoin Futures stream connector is required")
	}
	if config.RESTClient == nil {
		return nil, fmt.Errorf("KuCoin Futures REST client is required for WebSocket token issuance")
	}
	defaultRouteID := transport.EgressRouteID(strings.TrimSpace(string(config.DefaultEgressRouteID)))
	if defaultRouteID == "" {
		return nil, trade.ErrMissingEgressRoute
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
	if config.PingInterval < 0 || config.PingTimeout < 0 ||
		config.SubscriptionInterval < 0 || config.MaxReconnectAttempts < 0 {
		return nil, fmt.Errorf("KuCoin Futures stream durations or reconnect attempts are invalid")
	}
	if config.ConnectIDSource == nil {
		config.ConnectIDSource = randomConnectID
	}
	client := &StreamClient{
		restClient: config.RESTClient, defaultRouteID: defaultRouteID,
		allowInsecureWebSocket: config.AllowInsecureWebSocket,
		observer:               config.Observer, reconnectPolicy: config.ReconnectPolicy,
		backoff: config.Backoff, maxReconnectAttempts: config.MaxReconnectAttempts,
		pingInterval: config.PingInterval, pingTimeout: config.PingTimeout,
		subscriptionInterval: config.SubscriptionInterval,
		connectIDSource:      config.ConnectIDSource,
	}
	client.nextMessageID.Store(time.Now().UnixMilli())
	client.connector = &kucoinConnector{next: config.Connector, nextID: &client.nextMessageID}
	return client, nil
}

type pendingStreamCommand struct {
	operation    string
	subscription StreamSubscription
}

type managedStream struct {
	session  *corestream.Session
	private  bool
	interval time.Duration
	nextID   *atomic.Int64

	commandMu     sync.Mutex
	lastCommandAt time.Time
	stateMu       sync.Mutex
	subscriptions map[string]StreamSubscription
	pending       map[string]pendingStreamCommand
}

// PublicStream은 KuCoin Futures public 시세 WebSocket 연결을 관리한다.
type PublicStream struct{ managed *managedStream }

// PublicStream은 token을 발급하고 선택한 EIP route에 고정된 public 세션을 생성한다.
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
	managed := newManagedStream(
		subscriptions, false, client.subscriptionInterval, &client.nextMessageID,
	)
	session, err := client.newStreamSession(routeID, false, managed.resubscribe)
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

// PrivateStream은 KuCoin Futures private 주문·잔고·포지션 WebSocket 연결을 관리한다.
type PrivateStream struct{ managed *managedStream }

// PrivateStream은 매 연결마다 private token을 발급하고 선택한 EIP route에 고정한다.
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
	if client.restClient.credentials == nil || client.restClient.credentialProvider == nil {
		return nil, &trade.APIError{
			Category: trade.ErrorAuthentication, Exchange: model.ExchangeKuCoin,
			Cause: errors.New("private KuCoin Futures stream requires credentials"),
		}
	}
	if err := client.restClient.credentials.RequireEgressRoute(routeID); err != nil {
		return nil, &trade.APIError{
			Category: trade.ErrorAuthorization, Exchange: model.ExchangeKuCoin,
			AccountID: client.restClient.credentials.AccountID, Cause: err,
		}
	}
	if err := client.restClient.credentials.RequirePermission(credential.PermissionRead); err != nil {
		return nil, &trade.APIError{
			Category: trade.ErrorAuthorization, Exchange: model.ExchangeKuCoin,
			AccountID: client.restClient.credentials.AccountID, Cause: err,
		}
	}
	managed := newManagedStream(
		subscriptions, true, client.subscriptionInterval, &client.nextMessageID,
	)
	session, err := client.newStreamSession(routeID, true, managed.resubscribe)
	if err != nil {
		return nil, err
	}
	managed.session = session
	return &PrivateStream{managed: managed}, nil
}

// Run은 private 주문·잔고·포지션 메시지를 순서대로 decode해 handler에 전달한다.
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
	private bool,
	onConnect corestream.ConnectHook,
) (*corestream.Session, error) {
	return corestream.NewSession(corestream.SessionConfig{
		Connector: client.connector, EgressRouteID: routeID,
		RequestSource: func(ctx context.Context) (corestream.DialRequest, error) {
			return client.streamDialRequest(ctx, routeID, private)
		},
		OnConnect: onConnect, Observer: client.observer,
		ReconnectPolicy: client.streamReconnectPolicy, Backoff: client.backoff,
		MaxReconnectAttempts: client.maxReconnectAttempts,
		PingInterval:         client.pingInterval, PingTimeout: client.pingTimeout,
	})
}

func (client *StreamClient) streamDialRequest(
	ctx context.Context,
	routeID transport.EgressRouteID,
	private bool,
) (corestream.DialRequest, error) {
	var token WebSocketToken
	var err error
	if private {
		token, err = client.restClient.PrivateWebSocketToken(ctx, trade.WithEgressRoute(routeID))
	} else {
		token, err = client.restClient.PublicWebSocketToken(ctx, trade.WithEgressRoute(routeID))
	}
	if err != nil {
		return corestream.DialRequest{}, err
	}
	server, err := client.selectInstanceServer(token.Servers)
	if err != nil {
		return corestream.DialRequest{}, err
	}
	connectID, err := client.connectIDSource()
	if err != nil {
		return corestream.DialRequest{}, fmt.Errorf("create KuCoin Futures stream connect ID: %w", err)
	}
	if !identifierPatternForConnectID(connectID) {
		return corestream.DialRequest{}, validationError("invalid KuCoin Futures stream connect ID")
	}
	parsed, err := url.Parse(server.Endpoint)
	if err != nil {
		return corestream.DialRequest{}, fmt.Errorf("parse KuCoin Futures stream endpoint: %w", err)
	}
	query := parsed.Query()
	query.Set("token", token.Token)
	query.Set("connectId", connectID)
	parsed.RawQuery = query.Encode()
	return corestream.DialRequest{Endpoint: parsed.String()}, nil
}

func (client *StreamClient) selectInstanceServer(
	servers []WebSocketInstanceServer,
) (WebSocketInstanceServer, error) {
	for _, server := range servers {
		parsed, err := url.Parse(server.Endpoint)
		if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
			continue
		}
		if parsed.Scheme != "wss" && !(client.allowInsecureWebSocket && parsed.Scheme == "ws") {
			continue
		}
		if !strings.EqualFold(server.Protocol, "websocket") || server.PingInterval <= 0 || server.PingTimeout <= 0 {
			continue
		}
		if client.pingInterval >= time.Duration(server.PingInterval)*time.Millisecond {
			return WebSocketInstanceServer{}, fmt.Errorf("KuCoin Futures stream ping interval must be shorter than server interval")
		}
		if client.pingTimeout > time.Duration(server.PingTimeout)*time.Millisecond {
			return WebSocketInstanceServer{}, fmt.Errorf("KuCoin Futures stream ping timeout exceeds server timeout")
		}
		return server, nil
	}
	return WebSocketInstanceServer{}, fmt.Errorf("KuCoin Futures WebSocket token has no usable instance server")
}

func newManagedStream(
	subscriptions []StreamSubscription,
	private bool,
	interval time.Duration,
	nextID *atomic.Int64,
) *managedStream {
	managed := &managedStream{
		private: private, interval: interval, nextID: nextID,
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
		return fmt.Errorf("KuCoin Futures stream handler is required")
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
		if err := managed.waitCommandInterval(ctx); err != nil {
			return err
		}
		id := managed.nextCommandID()
		payload, err := encodeStreamCommand(id, "subscribe", subscription, managed.private)
		if err != nil {
			return err
		}
		if err := connection.Write(
			ctx, corestream.Message{Type: corestream.MessageText, Data: payload},
		); err != nil {
			return err
		}
		managed.lastCommandAt = time.Now()
		managed.stateMu.Lock()
		managed.pending[id] = pendingStreamCommand{operation: "subscribe", subscription: subscription}
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
		return fmt.Errorf("KuCoin Futures stream command context is nil")
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
			return validationError("WebSocket subscription count cannot exceed %d", maximumStreamSubscriptions)
		}
		if err := managed.waitCommandInterval(ctx); err != nil {
			return err
		}
		id := managed.nextCommandID()
		payload, err := encodeStreamCommand(id, operation, subscription, managed.private)
		if err != nil {
			return err
		}
		if err := managed.session.Write(
			ctx, corestream.Message{Type: corestream.MessageText, Data: payload},
		); err != nil {
			return err
		}
		managed.lastCommandAt = time.Now()
		managed.stateMu.Lock()
		managed.pending[id] = pendingStreamCommand{operation: operation, subscription: subscription}
		if operation == "subscribe" {
			managed.subscriptions[key] = subscription
		} else {
			delete(managed.subscriptions, key)
		}
		managed.stateMu.Unlock()
	}
	return nil
}

func (managed *managedStream) handleControl(message StreamMessage) {
	if message.ID == "" || message.Type != "ack" && message.Type != "error" {
		return
	}
	managed.stateMu.Lock()
	defer managed.stateMu.Unlock()
	pending, exists := managed.pending[message.ID]
	if !exists {
		return
	}
	delete(managed.pending, message.ID)
	if message.Type != "error" {
		return
	}
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

func (managed *managedStream) waitCommandInterval(ctx context.Context) error {
	wait := managed.interval - time.Since(managed.lastCommandAt)
	if wait <= 0 {
		return nil
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (managed *managedStream) nextCommandID() string {
	return strconv.FormatInt(managed.nextID.Add(1), 10)
}

func encodeStreamCommand(
	id, operation string,
	subscription StreamSubscription,
	private bool,
) ([]byte, error) {
	payload, err := json.Marshal(struct {
		ID             string `json:"id"`
		Type           string `json:"type"`
		Topic          string `json:"topic"`
		PrivateChannel bool   `json:"privateChannel"`
		Response       bool   `json:"response"`
	}{
		ID: id, Type: operation, Topic: streamTopic(subscription),
		PrivateChannel: private, Response: true,
	})
	if err != nil {
		return nil, fmt.Errorf("encode KuCoin Futures stream command: %w", err)
	}
	return payload, nil
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
		return nil, validationError("WebSocket subscription count cannot exceed %d", maximumStreamSubscriptions)
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
		case StreamChannelOrders, StreamChannelPositions:
			if subscription.Symbol != "" {
				if err := validateSymbol(subscription.Symbol); err != nil {
					return err
				}
			}
		case StreamChannelBalance:
			if subscription.Symbol != "" {
				return validationError("Futures balance subscription does not accept symbol")
			}
		default:
			return validationError("unsupported private WebSocket channel %q", subscription.Channel)
		}
		if subscription.Interval != "" {
			return validationError("private WebSocket subscription does not accept interval")
		}
		return nil
	}
	switch subscription.Channel {
	case StreamChannelTicker, StreamChannelLevel2, StreamChannelOrderBook5,
		StreamChannelOrderBook50, StreamChannelCandles, StreamChannelTrade:
	default:
		return validationError("unsupported public WebSocket channel %q", subscription.Channel)
	}
	if err := validateSymbol(subscription.Symbol); err != nil {
		return err
	}
	if subscription.Channel == StreamChannelCandles {
		if !subscription.Interval.valid() {
			return validationError("unsupported WebSocket candle interval %q", subscription.Interval)
		}
	} else if subscription.Interval != "" {
		return validationError("WebSocket interval is only supported for candles")
	}
	return nil
}

func streamTopic(subscription StreamSubscription) string {
	switch subscription.Channel {
	case StreamChannelTicker:
		return "/contractMarket/tickerV2:" + subscription.Symbol
	case StreamChannelLevel2:
		return "/contractMarket/level2:" + subscription.Symbol
	case StreamChannelOrderBook5:
		return "/contractMarket/level2Depth5:" + subscription.Symbol
	case StreamChannelOrderBook50:
		return "/contractMarket/level2Depth50:" + subscription.Symbol
	case StreamChannelCandles:
		return "/contractMarket/limitCandle:" + subscription.Symbol + "_" + string(subscription.Interval)
	case StreamChannelTrade:
		return "/contractMarket/execution:" + subscription.Symbol
	case StreamChannelOrders:
		if subscription.Symbol != "" {
			return "/contractMarket/tradeOrders:" + subscription.Symbol
		}
		return "/contractMarket/tradeOrders"
	case StreamChannelBalance:
		return "/contractAccount/wallet"
	case StreamChannelPositions:
		if subscription.Symbol != "" {
			return "/contract/position:" + subscription.Symbol
		}
		return "/contract/positionAll"
	default:
		return ""
	}
}

func streamSubscriptionKey(subscription StreamSubscription) string {
	return string(subscription.Channel) + "\x00" + subscription.Symbol + "\x00" + string(subscription.Interval)
}

func (client *StreamClient) resolveStreamRoute(
	options ...trade.RequestOption,
) (transport.EgressRouteID, error) {
	resolved, err := trade.ResolveRequestOptions(client.defaultRouteID, options...)
	if err != nil {
		return "", err
	}
	if resolved.Timeout != 0 {
		return "", fmt.Errorf("KuCoin Futures stream timeout must be controlled by Run context")
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

type kucoinConnector struct {
	next   corestream.Connector
	nextID *atomic.Int64
}

func (connector *kucoinConnector) Connect(
	ctx context.Context,
	routeID transport.EgressRouteID,
	request corestream.DialRequest,
) (corestream.Connection, error) {
	connection, err := connector.next.Connect(ctx, routeID, request)
	if err != nil {
		return nil, err
	}
	return &kucoinConnection{
		next: connection, nextID: connector.nextID, pong: make(chan struct{}, 1),
	}, nil
}

type kucoinConnection struct {
	next           corestream.Connection
	nextID         *atomic.Int64
	pong           chan struct{}
	writeMu        sync.Mutex
	pingMu         sync.Mutex
	expectedPongMu sync.Mutex
	expectedPongID string
}

func (connection *kucoinConnection) Read(ctx context.Context) (corestream.Message, error) {
	message, err := connection.next.Read(ctx)
	if id, ok := kucoinPongID(message.Data); err == nil && ok {
		connection.expectedPongMu.Lock()
		if id == connection.expectedPongID {
			select {
			case connection.pong <- struct{}{}:
			default:
			}
		}
		connection.expectedPongMu.Unlock()
	}
	return message, err
}

func (connection *kucoinConnection) Write(
	ctx context.Context,
	message corestream.Message,
) error {
	connection.writeMu.Lock()
	defer connection.writeMu.Unlock()
	return connection.next.Write(ctx, message)
}

func (connection *kucoinConnection) Ping(ctx context.Context) error {
	connection.pingMu.Lock()
	defer connection.pingMu.Unlock()
	select {
	case <-connection.pong:
	default:
	}
	id := strconv.FormatInt(connection.nextID.Add(1), 10)
	connection.expectedPongMu.Lock()
	connection.expectedPongID = id
	connection.expectedPongMu.Unlock()
	defer func() {
		connection.expectedPongMu.Lock()
		if connection.expectedPongID == id {
			connection.expectedPongID = ""
		}
		connection.expectedPongMu.Unlock()
	}()
	payload, err := json.Marshal(struct {
		ID   string `json:"id"`
		Type string `json:"type"`
	}{ID: id, Type: "ping"})
	if err != nil {
		return fmt.Errorf("encode KuCoin Futures stream ping: %w", err)
	}
	if err := connection.Write(
		ctx, corestream.Message{Type: corestream.MessageText, Data: payload},
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

func (connection *kucoinConnection) Close(code int, reason string) error {
	return connection.next.Close(code, reason)
}

func isKuCoinPong(data []byte) bool {
	_, ok := kucoinPongID(data)
	return ok
}

func kucoinPongID(data []byte) (string, bool) {
	var value struct {
		ID   json.RawMessage `json:"id"`
		Type string          `json:"type"`
	}
	if json.Unmarshal(data, &value) != nil || value.Type != "pong" {
		return "", false
	}
	id, err := optionalScalarText(value.ID)
	return id, err == nil && id != ""
}

func randomConnectID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func identifierPatternForConnectID(value string) bool {
	if len(value) < 1 || len(value) > 64 {
		return false
	}
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || character == '-' || character == '_' {
			continue
		}
		return false
	}
	return true
}
