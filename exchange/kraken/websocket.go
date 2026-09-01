package kraken

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
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
	DefaultSpotPublicStreamURL      = "wss://ws.kraken.com/v2"
	DefaultSpotPrivateStreamURL     = "wss://ws-auth.kraken.com/v2"
	defaultSpotStreamPingInterval   = 20 * time.Second
	defaultSpotStreamPingTimeout    = 10 * time.Second
	defaultSpotSubscriptionInterval = 100 * time.Millisecond
	maximumSpotSubscriptions        = 50
	maximumSpotSymbolsPerRequest    = 100
)

// SpotStreamClientConfig는 Kraken Spot WebSocket v2 설정이다.
type SpotStreamClientConfig struct {
	Connector              corestream.Connector
	RESTClient             *Client
	DefaultEgressRouteID   transport.EgressRouteID
	PublicStreamURL        string
	PrivateStreamURL       string
	AllowInsecureWebSocket bool
	Observer               corestream.StateObserver
	ReconnectPolicy        corestream.ReconnectPolicy
	Backoff                corestream.Backoff
	MaxReconnectAttempts   int
	PingInterval           time.Duration
	PingTimeout            time.Duration
	SubscriptionInterval   time.Duration
}

// SpotStreamClient는 Kraken Spot public/private WebSocket 세션을 생성한다.
type SpotStreamClient struct {
	connector            corestream.Connector
	restClient           *Client
	defaultRouteID       transport.EgressRouteID
	publicURL            string
	privateURL           string
	observer             corestream.StateObserver
	reconnectPolicy      corestream.ReconnectPolicy
	backoff              corestream.Backoff
	maxReconnectAttempts int
	pingInterval         time.Duration
	pingTimeout          time.Duration
	subscriptionInterval time.Duration
}

// NewSpotStreamClient는 Kraken Spot WebSocket v2 클라이언트를 생성한다.
func NewSpotStreamClient(config SpotStreamClientConfig) (*SpotStreamClient, error) {
	if config.Connector == nil {
		return nil, fmt.Errorf("Kraken Spot stream connector is required")
	}
	defaultRouteID := transport.EgressRouteID(strings.TrimSpace(string(config.DefaultEgressRouteID)))
	if defaultRouteID == "" {
		return nil, trade.ErrMissingEgressRoute
	}
	if config.PublicStreamURL == "" {
		config.PublicStreamURL = DefaultSpotPublicStreamURL
	}
	if config.PrivateStreamURL == "" {
		config.PrivateStreamURL = DefaultSpotPrivateStreamURL
	}
	publicURL, err := validateSpotWebSocketURL(
		config.PublicStreamURL, config.AllowInsecureWebSocket,
	)
	if err != nil {
		return nil, fmt.Errorf("invalid Kraken Spot public stream URL: %w", err)
	}
	privateURL, err := validateSpotWebSocketURL(
		config.PrivateStreamURL, config.AllowInsecureWebSocket,
	)
	if err != nil {
		return nil, fmt.Errorf("invalid Kraken Spot private stream URL: %w", err)
	}
	if config.PingInterval == 0 {
		config.PingInterval = defaultSpotStreamPingInterval
	}
	if config.PingTimeout == 0 {
		config.PingTimeout = defaultSpotStreamPingTimeout
	}
	if config.SubscriptionInterval == 0 {
		config.SubscriptionInterval = defaultSpotSubscriptionInterval
	}
	if config.PingInterval < 0 || config.PingTimeout < 0 || config.SubscriptionInterval < 0 {
		return nil, fmt.Errorf("Kraken Spot stream durations cannot be negative")
	}
	if config.MaxReconnectAttempts < 0 {
		return nil, fmt.Errorf("Kraken Spot maximum reconnect attempts cannot be negative")
	}
	return &SpotStreamClient{
		connector: config.Connector, restClient: config.RESTClient, defaultRouteID: defaultRouteID,
		publicURL: publicURL, privateURL: privateURL, observer: config.Observer,
		reconnectPolicy: config.ReconnectPolicy, backoff: config.Backoff,
		maxReconnectAttempts: config.MaxReconnectAttempts,
		pingInterval:         config.PingInterval, pingTimeout: config.PingTimeout,
		subscriptionInterval: config.SubscriptionInterval,
	}, nil
}

type managedSpotStream struct {
	session  *corestream.Session
	interval time.Duration
	nextID   atomic.Int64

	mu                   sync.Mutex
	publicSubscriptions  map[string]SpotPublicSubscription
	privateSubscriptions []SpotPrivateSubscription

	commandMu     sync.Mutex
	lastCommandAt time.Time
}

// SpotPublicStream은 Spot public channel 연결과 구독 목록을 관리한다.
type SpotPublicStream struct {
	managed *managedSpotStream
}

// PublicStream은 선택한 송신 경로에 고정된 Spot public 세션을 생성한다.
func (client *SpotStreamClient) PublicStream(
	request SpotPublicStreamRequest,
	options ...trade.RequestOption,
) (*SpotPublicStream, error) {
	subscriptions, err := validateSpotPublicSubscriptions(request.Subscriptions)
	if err != nil {
		return nil, err
	}
	routeID, err := client.resolveRoute(options...)
	if err != nil {
		return nil, err
	}
	managed := &managedSpotStream{
		interval:            client.subscriptionInterval,
		publicSubscriptions: make(map[string]SpotPublicSubscription, len(subscriptions)),
	}
	for _, subscription := range subscriptions {
		managed.publicSubscriptions[spotPublicSubscriptionKey(subscription)] = subscription
	}
	session, err := corestream.NewSession(corestream.SessionConfig{
		Connector: client.connector, EgressRouteID: routeID,
		Request:   corestream.DialRequest{Endpoint: client.publicURL},
		OnConnect: managed.resubscribePublic, Observer: client.observer,
		ReconnectPolicy: client.reconnectPolicy, Backoff: client.backoff,
		MaxReconnectAttempts: client.maxReconnectAttempts,
		PingInterval:         client.pingInterval, PingTimeout: client.pingTimeout,
	})
	if err != nil {
		return nil, err
	}
	managed.session = session
	return &SpotPublicStream{managed: managed}, nil
}

// Run은 public 메시지를 순서대로 decode해 handler에 전달한다.
func (public *SpotPublicStream) Run(
	ctx context.Context,
	handler func(context.Context, SpotStreamMessage) error,
) error {
	if handler == nil {
		return fmt.Errorf("Kraken Spot public stream handler is required")
	}
	return public.managed.run(ctx, handler)
}

// Subscribe는 public 구독을 추가하고 재연결 목록에도 반영한다.
func (public *SpotPublicStream) Subscribe(
	ctx context.Context,
	subscriptions ...SpotPublicSubscription,
) error {
	validated, err := validateSpotPublicSubscriptions(subscriptions)
	if err != nil {
		return err
	}
	return public.managed.changePublic(ctx, "subscribe", validated)
}

// Unsubscribe는 public 구독을 제거하고 재연결 목록에도 반영한다.
func (public *SpotPublicStream) Unsubscribe(
	ctx context.Context,
	subscriptions ...SpotPublicSubscription,
) error {
	validated, err := validateSpotPublicSubscriptions(subscriptions)
	if err != nil {
		return err
	}
	return public.managed.changePublic(ctx, "unsubscribe", validated)
}

// Close는 public stream 세션을 종료한다.
func (public *SpotPublicStream) Close() error { return public.managed.session.Close() }

// Generation은 성공한 public stream 연결 세대 번호를 반환한다.
func (public *SpotPublicStream) Generation() uint64 { return public.managed.session.Generation() }

// EgressRouteID는 public 연결과 모든 재연결에 고정된 송신 경로를 반환한다.
func (public *SpotPublicStream) EgressRouteID() transport.EgressRouteID {
	return public.managed.session.EgressRouteID()
}

func (public *SpotPublicStream) hasBookSubscription(symbol string, depth int) bool {
	public.managed.mu.Lock()
	defer public.managed.mu.Unlock()
	for _, subscription := range public.managed.publicSubscriptions {
		if subscription.Channel != SpotChannelBook ||
			effectiveSpotBookDepth(subscription.Depth) != depth ||
			(subscription.Snapshot != nil && !*subscription.Snapshot) {
			continue
		}
		for _, subscribedSymbol := range subscription.Symbols {
			if subscribedSymbol == symbol {
				return true
			}
		}
	}
	return false
}

func (public *SpotPublicStream) reconnect() error {
	return public.managed.session.Reconnect()
}

// SpotPrivateStream은 Spot private account channel 연결을 관리한다.
type SpotPrivateStream struct {
	managed *managedSpotStream
}

// PrivateStream은 매 연결마다 REST token을 발급하고 private channel을 구독한다.
func (client *SpotStreamClient) PrivateStream(
	request SpotPrivateStreamRequest,
	options ...trade.RequestOption,
) (*SpotPrivateStream, error) {
	subscriptions, err := validateSpotPrivateSubscriptions(request.Subscriptions)
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
	managed := &managedSpotStream{
		interval:             client.subscriptionInterval,
		privateSubscriptions: append([]SpotPrivateSubscription(nil), subscriptions...),
	}
	session, err := corestream.NewSession(corestream.SessionConfig{
		Connector: client.connector, EgressRouteID: routeID,
		Request: corestream.DialRequest{Endpoint: client.privateURL},
		OnConnect: func(ctx context.Context, connection corestream.Connection) error {
			token, tokenErr := client.restClient.WebSocketToken(
				ctx, trade.WithEgressRoute(routeID),
			)
			if tokenErr != nil {
				return tokenErr
			}
			return managed.subscribePrivate(ctx, connection, token.Value)
		},
		Observer: client.observer, ReconnectPolicy: client.privateReconnectPolicy,
		Backoff: client.backoff, MaxReconnectAttempts: client.maxReconnectAttempts,
		PingInterval: client.pingInterval, PingTimeout: client.pingTimeout,
	})
	if err != nil {
		return nil, err
	}
	managed.session = session
	return &SpotPrivateStream{managed: managed}, nil
}

// Run은 private 메시지를 순서대로 decode해 handler에 전달한다.
func (private *SpotPrivateStream) Run(
	ctx context.Context,
	handler func(context.Context, SpotStreamMessage) error,
) error {
	if handler == nil {
		return fmt.Errorf("Kraken Spot private stream handler is required")
	}
	return private.managed.run(ctx, handler)
}

// Close는 private stream 세션을 종료한다.
func (private *SpotPrivateStream) Close() error { return private.managed.session.Close() }

// Generation은 성공한 private stream 연결 세대 번호를 반환한다.
func (private *SpotPrivateStream) Generation() uint64 { return private.managed.session.Generation() }

func (managed *managedSpotStream) run(
	ctx context.Context,
	handler func(context.Context, SpotStreamMessage) error,
) error {
	return managed.session.Run(ctx, func(ctx context.Context, message corestream.Message) error {
		decoded, err := DecodeSpotStreamMessage(message)
		if err != nil {
			return corestream.ReconnectOnMessageError(err)
		}
		if decoded.Success != nil && !*decoded.Success {
			return &SpotStreamRequestError{
				Method: decoded.Method, RequestID: decoded.RequestID, Message: decoded.Error,
			}
		}
		return handler(ctx, decoded)
	})
}

func (managed *managedSpotStream) resubscribePublic(
	ctx context.Context,
	connection corestream.Connection,
) error {
	managed.commandMu.Lock()
	defer managed.commandMu.Unlock()
	return managed.writePublicLocked(ctx, connection, "subscribe", managed.snapshotPublic())
}

func (managed *managedSpotStream) changePublic(
	ctx context.Context,
	operation string,
	subscriptions []SpotPublicSubscription,
) error {
	managed.commandMu.Lock()
	defer managed.commandMu.Unlock()
	managed.mu.Lock()
	selected := make([]SpotPublicSubscription, 0, len(subscriptions))
	for _, subscription := range subscriptions {
		_, exists := managed.publicSubscriptions[spotPublicSubscriptionKey(subscription)]
		if operation == "subscribe" && !exists || operation == "unsubscribe" && exists {
			selected = append(selected, subscription)
		}
	}
	if operation == "subscribe" && len(managed.publicSubscriptions)+len(selected) > maximumSpotSubscriptions {
		managed.mu.Unlock()
		return validationError("Spot WebSocket subscription count cannot exceed %d", maximumSpotSubscriptions)
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
		key := spotPublicSubscriptionKey(subscription)
		if operation == "subscribe" {
			managed.publicSubscriptions[key] = subscription
		} else {
			delete(managed.publicSubscriptions, key)
		}
	}
	managed.mu.Unlock()
	return nil
}

func (managed *managedSpotStream) snapshotPublic() []SpotPublicSubscription {
	managed.mu.Lock()
	defer managed.mu.Unlock()
	keys := make([]string, 0, len(managed.publicSubscriptions))
	for key := range managed.publicSubscriptions {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	subscriptions := make([]SpotPublicSubscription, len(keys))
	for index, key := range keys {
		subscriptions[index] = managed.publicSubscriptions[key]
	}
	return subscriptions
}

func (managed *managedSpotStream) writePublicLocked(
	ctx context.Context,
	writer spotStreamWriter,
	operation string,
	subscriptions []SpotPublicSubscription,
) error {
	for _, subscription := range subscriptions {
		if err := managed.waitCommandSlot(ctx); err != nil {
			return err
		}
		params := spotStreamParams{
			Channel: string(subscription.Channel), Symbol: subscription.Symbols,
			Depth: subscription.Depth, Interval: subscription.Interval,
			EventTrigger: string(subscription.EventTrigger), Snapshot: subscription.Snapshot,
		}
		if err := writeSpotStreamOperation(
			ctx, writer, managed.nextID.Add(1), operation, params,
		); err != nil {
			return err
		}
		managed.lastCommandAt = time.Now()
	}
	return nil
}

func (managed *managedSpotStream) subscribePrivate(
	ctx context.Context,
	connection corestream.Connection,
	token string,
) error {
	managed.commandMu.Lock()
	defer managed.commandMu.Unlock()
	for _, subscription := range managed.privateSubscriptions {
		if err := managed.waitCommandSlot(ctx); err != nil {
			return err
		}
		params := spotStreamParams{
			Channel: string(subscription.Channel), Token: token, Snapshot: subscription.Snapshot,
			SnapOrders: subscription.SnapOrders, SnapTrades: subscription.SnapTrades,
			OrderStatus: subscription.OrderStatus,
		}
		if err := writeSpotStreamOperation(
			ctx, connection, managed.nextID.Add(1), "subscribe", params,
		); err != nil {
			return err
		}
		managed.lastCommandAt = time.Now()
	}
	return nil
}

func (managed *managedSpotStream) waitCommandSlot(ctx context.Context) error {
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

type spotStreamWriter interface {
	Write(context.Context, corestream.Message) error
}

type spotStreamParams struct {
	Channel      string   `json:"channel"`
	Symbol       []string `json:"symbol,omitempty"`
	Depth        int      `json:"depth,omitempty"`
	Interval     int      `json:"interval,omitempty"`
	EventTrigger string   `json:"event_trigger,omitempty"`
	Snapshot     *bool    `json:"snapshot,omitempty"`
	Token        string   `json:"token,omitempty"`
	SnapOrders   *bool    `json:"snap_orders,omitempty"`
	SnapTrades   *bool    `json:"snap_trades,omitempty"`
	OrderStatus  *bool    `json:"order_status,omitempty"`
}

type spotStreamOperation struct {
	Method    string           `json:"method"`
	Params    spotStreamParams `json:"params"`
	RequestID int64            `json:"req_id"`
}

func writeSpotStreamOperation(
	ctx context.Context,
	writer spotStreamWriter,
	requestID int64,
	method string,
	params spotStreamParams,
) error {
	payload, err := json.Marshal(spotStreamOperation{
		Method: method, Params: params, RequestID: requestID,
	})
	if err != nil {
		return fmt.Errorf("encode Kraken Spot stream operation: %w", err)
	}
	defer clearSpotStreamBytes(payload)
	return writer.Write(ctx, corestream.Message{Type: corestream.MessageText, Data: payload})
}

func (client *SpotStreamClient) resolveRoute(
	options ...trade.RequestOption,
) (transport.EgressRouteID, error) {
	resolved, err := trade.ResolveRequestOptions(client.defaultRouteID, options...)
	if err != nil {
		return "", err
	}
	if resolved.Timeout != 0 {
		return "", fmt.Errorf("Kraken Spot stream timeout must be controlled by Run context")
	}
	return resolved.EgressRouteID, nil
}

func (client *SpotStreamClient) validatePrivateAccess(routeID transport.EgressRouteID) error {
	if client.restClient == nil || client.restClient.credentials == nil ||
		client.restClient.credentialProvider == nil {
		return client.authenticationError(errors.New("private Kraken Spot stream requires REST credentials"))
	}
	if err := client.restClient.credentials.RequireEgressRoute(routeID); err != nil {
		return client.authorizationError(err)
	}
	if err := client.restClient.credentials.RequirePermission(credential.PermissionRead); err != nil {
		return client.authorizationError(err)
	}
	return nil
}

func (client *SpotStreamClient) privateReconnectPolicy(err error) bool {
	if errors.Is(err, trade.ErrAuthentication) || errors.Is(err, trade.ErrAuthorization) {
		return false
	}
	if client.reconnectPolicy != nil {
		return client.reconnectPolicy(err)
	}
	return corestream.DefaultReconnectPolicy(err)
}

func (client *SpotStreamClient) authenticationError(cause error) error {
	accountID := ""
	if client.restClient != nil && client.restClient.credentials != nil {
		accountID = client.restClient.credentials.AccountID
	}
	return &trade.APIError{
		Category: trade.ErrorAuthentication, Exchange: model.ExchangeKraken,
		AccountID: accountID, Cause: cause,
	}
}

func (client *SpotStreamClient) authorizationError(cause error) error {
	accountID := ""
	if client.restClient != nil && client.restClient.credentials != nil {
		accountID = client.restClient.credentials.AccountID
	}
	return &trade.APIError{
		Category: trade.ErrorAuthorization, Exchange: model.ExchangeKraken,
		AccountID: accountID, Cause: cause,
	}
}

func validateSpotPublicSubscriptions(
	subscriptions []SpotPublicSubscription,
) ([]SpotPublicSubscription, error) {
	if len(subscriptions) == 0 {
		return nil, validationError("at least one Spot public WebSocket subscription is required")
	}
	if len(subscriptions) > maximumSpotSubscriptions {
		return nil, validationError(
			"Spot WebSocket subscription count cannot exceed %d", maximumSpotSubscriptions,
		)
	}
	validated := make([]SpotPublicSubscription, len(subscriptions))
	seen := make(map[string]struct{}, len(subscriptions))
	for index, subscription := range subscriptions {
		value, err := validateSpotPublicSubscription(subscription)
		if err != nil {
			return nil, err
		}
		key := spotPublicSubscriptionKey(value)
		if _, exists := seen[key]; exists {
			return nil, validationError("duplicate Spot public WebSocket subscription")
		}
		seen[key] = struct{}{}
		validated[index] = value
	}
	return validated, nil
}

func validateSpotPublicSubscription(
	subscription SpotPublicSubscription,
) (SpotPublicSubscription, error) {
	switch subscription.Channel {
	case SpotChannelTicker, SpotChannelBook, SpotChannelTrade, SpotChannelOHLC:
		if len(subscription.Symbols) == 0 || len(subscription.Symbols) > maximumSpotSymbolsPerRequest {
			return SpotPublicSubscription{}, validationError(
				"Spot WebSocket symbol count must be between 1 and %d", maximumSpotSymbolsPerRequest,
			)
		}
	case SpotChannelInstrument:
		if len(subscription.Symbols) != 0 {
			return SpotPublicSubscription{}, validationError("instrument channel does not accept symbols")
		}
	default:
		return SpotPublicSubscription{}, validationError(
			"unsupported Spot public WebSocket channel %q", subscription.Channel,
		)
	}
	symbols := append([]string(nil), subscription.Symbols...)
	sort.Strings(symbols)
	for index, symbol := range symbols {
		if err := validateSpotStreamSymbol(symbol); err != nil {
			return SpotPublicSubscription{}, err
		}
		if index > 0 && symbol == symbols[index-1] {
			return SpotPublicSubscription{}, validationError("duplicate Spot WebSocket symbol %q", symbol)
		}
	}
	subscription.Symbols = symbols
	if subscription.Channel == SpotChannelBook {
		switch subscription.Depth {
		case 0, 10, 25, 100, 500, 1000:
		default:
			return SpotPublicSubscription{}, validationError("unsupported Spot book depth %d", subscription.Depth)
		}
	} else if subscription.Depth != 0 {
		return SpotPublicSubscription{}, validationError("depth is available only for Spot book channel")
	}
	if subscription.Channel == SpotChannelOHLC {
		if !validSpotOHLCInterval(subscription.Interval) {
			return SpotPublicSubscription{}, validationError(
				"unsupported Spot OHLC interval %d", subscription.Interval,
			)
		}
	} else if subscription.Interval != 0 {
		return SpotPublicSubscription{}, validationError("interval is available only for Spot OHLC channel")
	}
	if subscription.Channel == SpotChannelTicker {
		if subscription.EventTrigger != "" && subscription.EventTrigger != SpotTickerOnTrade &&
			subscription.EventTrigger != SpotTickerOnBBO {
			return SpotPublicSubscription{}, validationError(
				"unsupported Spot ticker event trigger %q", subscription.EventTrigger,
			)
		}
	} else if subscription.EventTrigger != "" {
		return SpotPublicSubscription{}, validationError(
			"event trigger is available only for Spot ticker channel",
		)
	}
	return subscription, nil
}

func effectiveSpotBookDepth(depth int) int {
	if depth == 0 {
		return 10
	}
	return depth
}

func validateSpotPrivateSubscriptions(
	subscriptions []SpotPrivateSubscription,
) ([]SpotPrivateSubscription, error) {
	if len(subscriptions) == 0 {
		return nil, validationError("at least one Spot private WebSocket subscription is required")
	}
	if len(subscriptions) > 2 {
		return nil, validationError("Spot private WebSocket accepts at most two subscriptions")
	}
	validated := append([]SpotPrivateSubscription(nil), subscriptions...)
	seen := make(map[SpotPrivateChannel]struct{}, len(subscriptions))
	for _, subscription := range subscriptions {
		if subscription.Channel != SpotChannelExecutions && subscription.Channel != SpotChannelBalances {
			return nil, validationError(
				"unsupported Spot private WebSocket channel %q", subscription.Channel,
			)
		}
		if _, exists := seen[subscription.Channel]; exists {
			return nil, validationError(
				"duplicate Spot private WebSocket channel %q", subscription.Channel,
			)
		}
		seen[subscription.Channel] = struct{}{}
		if subscription.Channel == SpotChannelBalances &&
			(subscription.SnapOrders != nil || subscription.SnapTrades != nil || subscription.OrderStatus != nil) {
			return nil, validationError("balances channel does not accept execution snapshot options")
		}
		if subscription.Channel == SpotChannelExecutions && subscription.Snapshot != nil {
			return nil, validationError("executions channel must use snap_orders and snap_trades")
		}
	}
	return validated, nil
}

func validateSpotStreamSymbol(symbol string) error {
	if len(symbol) < 3 || len(symbol) > 32 || strings.TrimSpace(symbol) != symbol ||
		strings.ContainsFunc(symbol, unicode.IsControl) || !strings.Contains(symbol, "/") {
		return validationError("Spot WebSocket symbol has an invalid format")
	}
	for _, character := range symbol {
		if character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' ||
			character == '/' || character == '.' || character == '-' || character == '_' {
			continue
		}
		return validationError("Spot WebSocket symbol has an invalid format")
	}
	return nil
}

func validSpotOHLCInterval(interval int) bool {
	switch interval {
	case 1, 5, 15, 30, 60, 240, 1440, 10080, 21600:
		return true
	default:
		return false
	}
}

func spotPublicSubscriptionKey(subscription SpotPublicSubscription) string {
	return fmt.Sprintf(
		"%s|%s|%d|%d|%s|%s",
		subscription.Channel, strings.Join(subscription.Symbols, ","),
		subscription.Depth, subscription.Interval, subscription.EventTrigger,
		spotOptionalBoolKey(subscription.Snapshot),
	)
}

func spotOptionalBoolKey(value *bool) string {
	if value == nil {
		return "default"
	}
	if *value {
		return "true"
	}
	return "false"
}

func validateSpotWebSocketURL(raw string, allowInsecure bool) (string, error) {
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

// DecodeSpotStreamMessage는 Spot WebSocket v2 frame을 공통 stream 메시지로 변환한다.
func DecodeSpotStreamMessage(message corestream.Message) (SpotStreamMessage, error) {
	var envelope struct {
		Channel   string          `json:"channel"`
		Type      string          `json:"type"`
		Method    string          `json:"method"`
		RequestID int64           `json:"req_id"`
		Success   *bool           `json:"success"`
		Error     string          `json:"error"`
		TimeIn    string          `json:"time_in"`
		TimeOut   string          `json:"time_out"`
		Result    json.RawMessage `json:"result"`
		Data      json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(message.Data, &envelope); err != nil {
		return SpotStreamMessage{}, fmt.Errorf("decode Kraken Spot stream message: %w", err)
	}
	return SpotStreamMessage{
		Channel: envelope.Channel, Type: envelope.Type, Method: envelope.Method,
		RequestID: envelope.RequestID, Success: envelope.Success, Error: envelope.Error,
		TimeIn: envelope.TimeIn, TimeOut: envelope.TimeOut,
		Result: cloneBytes(envelope.Result), Data: cloneBytes(envelope.Data),
		Raw: cloneBytes(message.Data),
	}, nil
}

func clearSpotStreamBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
