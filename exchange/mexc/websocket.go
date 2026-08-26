package mexc

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

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	"github.com/proven-trade/cex-sdk/model"
	corestream "github.com/proven-trade/cex-sdk/stream"
	"github.com/proven-trade/cex-sdk/transport"
)

const (
	DefaultWebSocketURL        = "wss://wbs-api.mexc.com/ws"
	defaultStreamPingInterval  = 20 * time.Second
	defaultStreamPingTimeout   = 10 * time.Second
	defaultListenKeyKeepalive  = 30 * time.Minute
	maximumListenKeyValidity   = 60 * time.Minute
	maximumStreamSubscriptions = 30
)

var pingCommand = []byte(`{"method":"PING"}`)

// StreamClientConfig는 MEXC Spot V3 public·private Protobuf WebSocket 설정이다.
type StreamClientConfig struct {
	Connector                  corestream.Connector
	RESTClient                 *Client
	DefaultEgressRouteID       transport.EgressRouteID
	WebSocketURL               string
	AllowInsecureWebSocket     bool
	Observer                   corestream.StateObserver
	ReconnectPolicy            corestream.ReconnectPolicy
	Backoff                    corestream.Backoff
	MaxReconnectAttempts       int
	PingInterval               time.Duration
	PingTimeout                time.Duration
	ListenKeyKeepaliveInterval time.Duration
}

// StreamClient는 MEXC Spot V3 public·private WebSocket 세션을 생성한다.
type StreamClient struct {
	connector                  corestream.Connector
	restClient                 *Client
	defaultRouteID             transport.EgressRouteID
	webSocketURL               string
	observer                   corestream.StateObserver
	reconnectPolicy            corestream.ReconnectPolicy
	backoff                    corestream.Backoff
	maxReconnectAttempts       int
	pingInterval               time.Duration
	pingTimeout                time.Duration
	listenKeyKeepaliveInterval time.Duration
}

// NewStreamClient는 검증된 MEXC Spot V3 WebSocket 클라이언트를 생성한다.
func NewStreamClient(config StreamClientConfig) (*StreamClient, error) {
	if config.Connector == nil {
		return nil, fmt.Errorf("MEXC stream connector is required")
	}
	defaultRouteID := transport.EgressRouteID(strings.TrimSpace(string(config.DefaultEgressRouteID)))
	if defaultRouteID == "" {
		return nil, trade.ErrMissingEgressRoute
	}
	if config.WebSocketURL == "" {
		config.WebSocketURL = DefaultWebSocketURL
	}
	webSocketURL, err := validateMEXCWebSocketURL(
		config.WebSocketURL, config.AllowInsecureWebSocket,
	)
	if err != nil {
		return nil, fmt.Errorf("invalid MEXC WebSocket URL: %w", err)
	}
	if config.PingInterval == 0 {
		config.PingInterval = defaultStreamPingInterval
	}
	if config.PingTimeout == 0 {
		config.PingTimeout = defaultStreamPingTimeout
	}
	if config.ListenKeyKeepaliveInterval == 0 {
		config.ListenKeyKeepaliveInterval = defaultListenKeyKeepalive
	}
	if config.PingInterval < 0 || config.PingTimeout < 0 || config.MaxReconnectAttempts < 0 {
		return nil, fmt.Errorf("MEXC stream durations or reconnect attempts are invalid")
	}
	if config.PingInterval > 0 && config.PingTimeout == 0 {
		return nil, fmt.Errorf("MEXC stream ping timeout is required when ping is enabled")
	}
	if config.ListenKeyKeepaliveInterval <= 0 ||
		config.ListenKeyKeepaliveInterval >= maximumListenKeyValidity {
		return nil, fmt.Errorf("MEXC listen key keepalive interval must be shorter than 60 minutes")
	}
	return &StreamClient{
		connector: config.Connector, restClient: config.RESTClient,
		defaultRouteID: defaultRouteID, webSocketURL: webSocketURL,
		observer: config.Observer, reconnectPolicy: config.ReconnectPolicy,
		backoff: config.Backoff, maxReconnectAttempts: config.MaxReconnectAttempts,
		pingInterval: config.PingInterval, pingTimeout: config.PingTimeout,
		listenKeyKeepaliveInterval: config.ListenKeyKeepaliveInterval,
	}, nil
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
	pendingOrder  []string
}

// PublicStream은 MEXC 공개 시세 Protobuf 연결을 관리한다.
type PublicStream struct{ managed *managedStream }

// PublicStream은 선택한 송신 경로에 고정된 공개 시세 세션을 생성한다.
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
	managed := newMEXCManagedStream(client, subscriptions, false)
	session, err := corestream.NewSession(corestream.SessionConfig{
		Connector: client.connector, EgressRouteID: routeID,
		Request:   corestream.DialRequest{Endpoint: client.webSocketURL},
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

// Run은 공개 JSON 제어 응답과 Protobuf 이벤트를 순서대로 처리기에 전달한다.
func (public *PublicStream) Run(
	ctx context.Context,
	handler func(context.Context, StreamMessage) error,
) error {
	return public.managed.run(ctx, handler)
}

// Subscribe는 공개 구독을 추가하고 재연결 복구 목록에도 반영한다.
func (public *PublicStream) Subscribe(
	ctx context.Context,
	subscriptions ...StreamSubscription,
) error {
	validated, err := validateStreamSubscriptions(subscriptions, false, true)
	if err != nil {
		return err
	}
	return public.managed.change(ctx, "SUBSCRIPTION", validated)
}

// Unsubscribe는 공개 구독을 제거하고 재연결 복구 목록에도 반영한다.
func (public *PublicStream) Unsubscribe(
	ctx context.Context,
	subscriptions ...StreamSubscription,
) error {
	validated, err := validateStreamSubscriptions(subscriptions, false, true)
	if err != nil {
		return err
	}
	return public.managed.change(ctx, "UNSUBSCRIPTION", validated)
}

// Close는 공개 WebSocket 세션을 종료한다.
func (public *PublicStream) Close() error { return public.managed.session.Close() }

// Generation은 성공한 공개 연결 세대 번호를 반환한다.
func (public *PublicStream) Generation() uint64 { return public.managed.session.Generation() }

// EgressRouteID는 공개 연결과 모든 재연결에 고정된 송신 경로를 반환한다.
func (public *PublicStream) EgressRouteID() transport.EgressRouteID {
	return public.managed.session.EgressRouteID()
}

func (public *PublicStream) reconnect() error { return public.managed.session.Reconnect() }

// UserDataStream은 listenKey 기반 private 계정·체결·주문 연결을 관리한다.
type UserDataStream struct {
	managed *managedStream
	client  *StreamClient
	routeID transport.EgressRouteID

	keyMu     sync.RWMutex
	listenKey string
}

// UserDataStream은 listenKey 발급과 WebSocket 연결을 같은 송신 경로 route에 고정한다.
func (client *StreamClient) UserDataStream(
	request StreamRequest,
	options ...trade.RequestOption,
) (*UserDataStream, error) {
	subscriptions, err := validateStreamSubscriptions(request.Subscriptions, true, true)
	if err != nil {
		return nil, err
	}
	routeID, err := client.resolveStreamRoute(options...)
	if err != nil {
		return nil, err
	}
	if client.restClient == nil || client.restClient.credentials == nil ||
		client.restClient.credentialProvider == nil {
		return nil, client.streamAuthenticationError(
			errors.New("private MEXC stream requires REST client credentials"),
		)
	}
	if err := client.restClient.credentials.RequireEgressRoute(routeID); err != nil {
		return nil, client.streamAuthorizationError(err)
	}
	if err := client.restClient.credentials.RequirePermission(credential.PermissionRead); err != nil {
		return nil, client.streamAuthorizationError(err)
	}
	managed := newMEXCManagedStream(client, subscriptions, true)
	userData := &UserDataStream{managed: managed, client: client, routeID: routeID}
	session, err := corestream.NewSession(corestream.SessionConfig{
		Connector: client.connector, EgressRouteID: routeID,
		RequestSource: userData.dialRequest, OnConnect: managed.resubscribe,
		Observer: client.observer, ReconnectPolicy: client.streamReconnectPolicy,
		Backoff: client.backoff, MaxReconnectAttempts: client.maxReconnectAttempts,
	})
	if err != nil {
		return nil, err
	}
	managed.session = session
	return userData, nil
}

// Run은 private JSON 제어 응답과 Protobuf 이벤트를 처리하고 listenKey를 갱신한다.
func (userData *UserDataStream) Run(
	ctx context.Context,
	handler func(context.Context, StreamMessage) error,
) error {
	if ctx == nil {
		return fmt.Errorf("MEXC user data stream context is nil")
	}
	keepaliveContext, cancelKeepalive := context.WithCancel(ctx)
	keepaliveDone := make(chan struct{})
	go func() {
		defer close(keepaliveDone)
		userData.keepalive(keepaliveContext)
	}()
	err := userData.managed.run(ctx, handler)
	cancelKeepalive()
	<-keepaliveDone
	return err
}

// Subscribe는 private 구독을 추가하고 재연결 복구 목록에도 반영한다.
func (userData *UserDataStream) Subscribe(
	ctx context.Context,
	subscriptions ...StreamSubscription,
) error {
	validated, err := validateStreamSubscriptions(subscriptions, true, true)
	if err != nil {
		return err
	}
	return userData.managed.change(ctx, "SUBSCRIPTION", validated)
}

// Unsubscribe는 private 구독을 제거하고 재연결 복구 목록에도 반영한다.
func (userData *UserDataStream) Unsubscribe(
	ctx context.Context,
	subscriptions ...StreamSubscription,
) error {
	validated, err := validateStreamSubscriptions(subscriptions, true, true)
	if err != nil {
		return err
	}
	return userData.managed.change(ctx, "UNSUBSCRIPTION", validated)
}

// ListenKey는 현재 연결 세대가 사용하는 접속 키를 반환한다.
func (userData *UserDataStream) ListenKey() string { return userData.currentListenKey() }

// Close는 private WebSocket 세션을 종료한다.
func (userData *UserDataStream) Close() error { return userData.managed.session.Close() }

// Generation은 성공한 private 연결 세대 번호를 반환한다.
func (userData *UserDataStream) Generation() uint64 {
	return userData.managed.session.Generation()
}

// EgressRouteID는 listenKey REST 요청과 WebSocket 연결에 고정된 송신 경로를 반환한다.
func (userData *UserDataStream) EgressRouteID() transport.EgressRouteID {
	return userData.routeID
}

func newMEXCManagedStream(
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
		managed.subscriptions[streamSubscriptionName(subscription)] = subscription
	}
	return managed
}

func (managed *managedStream) run(
	ctx context.Context,
	handler func(context.Context, StreamMessage) error,
) error {
	if ctx == nil {
		return fmt.Errorf("MEXC stream context is nil")
	}
	if handler == nil {
		return fmt.Errorf("MEXC stream handler is required")
	}
	heartbeatContext, cancelHeartbeat := context.WithCancel(ctx)
	heartbeatDone := make(chan struct{})
	go func() {
		defer close(heartbeatDone)
		managed.heartbeat(heartbeatContext)
	}()
	err := managed.session.Run(ctx, func(ctx context.Context, message corestream.Message) error {
		decoded, err := DecodeStreamMessage(message)
		if err != nil {
			return err
		}
		managed.handleControl(decoded)
		return handler(ctx, decoded)
	})
	cancelHeartbeat()
	<-heartbeatDone
	return err
}

func (managed *managedStream) heartbeat(ctx context.Context) {
	if managed.client.pingInterval == 0 {
		return
	}
	ticker := time.NewTicker(managed.client.pingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pingContext, cancel := context.WithTimeout(ctx, managed.client.pingTimeout)
			err := managed.session.Write(pingContext, corestream.Message{
				Type: corestream.MessageText, Data: append([]byte(nil), pingCommand...),
			})
			cancel()
			if err != nil && !errors.Is(err, corestream.ErrNotConnected) &&
				!errors.Is(err, corestream.ErrSessionClosed) && ctx.Err() == nil {
				_ = managed.session.Reconnect()
			}
		}
	}
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
	managed.pendingOrder = managed.pendingOrder[:0]
	managed.stateMu.Unlock()
	for _, subscription := range subscriptions {
		name := streamSubscriptionName(subscription)
		payload, err := encodeStreamCommand("SUBSCRIPTION", name)
		if err != nil {
			return err
		}
		if err := connection.Write(
			ctx, corestream.Message{Type: corestream.MessageText, Data: payload},
		); err != nil {
			return err
		}
		managed.stateMu.Lock()
		managed.pending[name] = pendingStreamCommand{
			operation: "SUBSCRIPTION", subscription: subscription,
		}
		managed.pendingOrder = append(managed.pendingOrder, name)
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
		return fmt.Errorf("MEXC stream command context is nil")
	}
	managed.commandMu.Lock()
	defer managed.commandMu.Unlock()
	for _, subscription := range subscriptions {
		name := streamSubscriptionName(subscription)
		managed.stateMu.Lock()
		_, exists := managed.subscriptions[name]
		_, pending := managed.pending[name]
		count := len(managed.subscriptions)
		managed.stateMu.Unlock()
		if pending {
			return validationError("WebSocket subscription %q has a pending command", name)
		}
		if operation == "SUBSCRIPTION" && exists || operation == "UNSUBSCRIPTION" && !exists {
			continue
		}
		if operation == "SUBSCRIPTION" && count >= maximumStreamSubscriptions {
			return validationError(
				"WebSocket subscription count cannot exceed %d", maximumStreamSubscriptions,
			)
		}
		payload, err := encodeStreamCommand(operation, name)
		if err != nil {
			return err
		}
		managed.stateMu.Lock()
		managed.pending[name] = pendingStreamCommand{
			operation: operation, subscription: subscription,
		}
		managed.pendingOrder = append(managed.pendingOrder, name)
		if operation == "SUBSCRIPTION" {
			managed.subscriptions[name] = subscription
		} else {
			delete(managed.subscriptions, name)
		}
		managed.stateMu.Unlock()
		if err := managed.session.Write(
			ctx, corestream.Message{Type: corestream.MessageText, Data: payload},
		); err != nil {
			managed.rollbackCommand(name)
			return err
		}
	}
	return nil
}

func (managed *managedStream) handleControl(message StreamMessage) {
	if message.Control == nil || message.Control.Message == "PONG" {
		return
	}
	managed.stateMu.Lock()
	defer managed.stateMu.Unlock()
	name := message.Control.Message
	pending, exists := managed.pending[name]
	if !exists && len(managed.pendingOrder) > 0 {
		name = managed.pendingOrder[0]
		pending, exists = managed.pending[name]
	}
	if !exists {
		return
	}
	managed.deletePendingLocked(name)
	if message.Control.Code == 0 {
		return
	}
	managed.rollbackCommandLocked(pending)
}

func (managed *managedStream) rollbackCommand(name string) {
	managed.stateMu.Lock()
	defer managed.stateMu.Unlock()
	pending, exists := managed.pending[name]
	if !exists {
		return
	}
	managed.deletePendingLocked(name)
	managed.rollbackCommandLocked(pending)
}

func (managed *managedStream) deletePendingLocked(name string) {
	delete(managed.pending, name)
	for index, pendingName := range managed.pendingOrder {
		if pendingName == name {
			managed.pendingOrder = append(
				managed.pendingOrder[:index], managed.pendingOrder[index+1:]...,
			)
			return
		}
	}
}

func (managed *managedStream) rollbackCommandLocked(pending pendingStreamCommand) {
	name := streamSubscriptionName(pending.subscription)
	if pending.operation == "SUBSCRIPTION" {
		delete(managed.subscriptions, name)
	} else {
		managed.subscriptions[name] = pending.subscription
	}
}

func (managed *managedStream) snapshotSubscriptions() []StreamSubscription {
	managed.stateMu.Lock()
	defer managed.stateMu.Unlock()
	names := make([]string, 0, len(managed.subscriptions))
	for name := range managed.subscriptions {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]StreamSubscription, 0, len(names))
	for _, name := range names {
		result = append(result, managed.subscriptions[name])
	}
	return result
}

func encodeStreamCommand(operation, subscription string) ([]byte, error) {
	payload, err := json.Marshal(struct {
		Method string   `json:"method"`
		Params []string `json:"params"`
	}{Method: operation, Params: []string{subscription}})
	if err != nil {
		return nil, fmt.Errorf("encode MEXC stream command: %w", err)
	}
	return payload, nil
}

func (userData *UserDataStream) dialRequest(
	ctx context.Context,
) (corestream.DialRequest, error) {
	listenKey := userData.currentListenKey()
	if listenKey == "" {
		result, err := userData.client.restClient.StartUserDataStream(
			ctx, trade.WithEgressRoute(userData.routeID),
		)
		if err != nil {
			return corestream.DialRequest{}, err
		}
		listenKey = result.ListenKey
		userData.setListenKey(listenKey)
	}
	endpoint, err := userDataEndpoint(userData.client.webSocketURL, listenKey)
	if err != nil {
		return corestream.DialRequest{}, err
	}
	return corestream.DialRequest{Endpoint: endpoint}, nil
}

func (userData *UserDataStream) keepalive(ctx context.Context) {
	ticker := time.NewTicker(userData.client.listenKeyKeepaliveInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			listenKey := userData.currentListenKey()
			if listenKey == "" {
				continue
			}
			_, err := userData.client.restClient.KeepaliveUserDataStream(
				ctx, listenKey, trade.WithEgressRoute(userData.routeID),
			)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				userData.clearListenKey(listenKey)
				_ = userData.managed.session.Reconnect()
			}
		}
	}
}

func (userData *UserDataStream) setListenKey(value string) {
	userData.keyMu.Lock()
	userData.listenKey = value
	userData.keyMu.Unlock()
}

func (userData *UserDataStream) clearListenKey(expected string) {
	userData.keyMu.Lock()
	if userData.listenKey == expected {
		userData.listenKey = ""
	}
	userData.keyMu.Unlock()
}

func (userData *UserDataStream) currentListenKey() string {
	userData.keyMu.RLock()
	defer userData.keyMu.RUnlock()
	return userData.listenKey
}

// AggregateTradesStream은 공개 합산 체결 구독을 만든다.
func AggregateTradesStream(
	symbol string,
	interval StreamUpdateInterval,
) (StreamSubscription, error) {
	return validatedStreamSubscription(StreamSubscription{
		Channel: StreamChannelAggregateTrades, Symbol: symbol, UpdateInterval: interval,
	}, false)
}

// CandleStream은 공개 캔들 구독을 만든다.
func CandleStream(
	symbol string,
	interval StreamCandleInterval,
) (StreamSubscription, error) {
	return validatedStreamSubscription(StreamSubscription{
		Channel: StreamChannelCandles, Symbol: symbol, CandleInterval: interval,
	}, false)
}

// DiffDepthStream은 version 범위 기반 공개 증분 호가 구독을 만든다.
func DiffDepthStream(
	symbol string,
	interval StreamUpdateInterval,
) (StreamSubscription, error) {
	return validatedStreamSubscription(StreamSubscription{
		Channel: StreamChannelDiffDepth, Symbol: symbol, UpdateInterval: interval,
	}, false)
}

// PartialDepthStream은 5·10·20단계 공개 완전 호가 구독을 만든다.
func PartialDepthStream(
	symbol string,
	depth StreamDepthLevel,
) (StreamSubscription, error) {
	return validatedStreamSubscription(StreamSubscription{
		Channel: StreamChannelPartialDepth, Symbol: symbol, Depth: depth,
	}, false)
}

// BookTickerStream은 공개 최우선 호가 구독을 만든다.
func BookTickerStream(
	symbol string,
	interval StreamUpdateInterval,
) (StreamSubscription, error) {
	return validatedStreamSubscription(StreamSubscription{
		Channel: StreamChannelBookTicker, Symbol: symbol, UpdateInterval: interval,
	}, false)
}

// AccountStream은 private 잔고 변경 구독을 만든다.
func AccountStream() StreamSubscription {
	return StreamSubscription{Channel: StreamChannelAccount}
}

// AccountDealsStream은 private 체결 구독을 만든다.
func AccountDealsStream() StreamSubscription {
	return StreamSubscription{Channel: StreamChannelAccountDeals}
}

// AccountOrdersStream은 private 주문 변경 구독을 만든다.
func AccountOrdersStream() StreamSubscription {
	return StreamSubscription{Channel: StreamChannelAccountOrders}
}

func validatedStreamSubscription(
	subscription StreamSubscription,
	private bool,
) (StreamSubscription, error) {
	if err := validateStreamSubscription(subscription, private); err != nil {
		return StreamSubscription{}, err
	}
	return subscription, nil
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
		name := streamSubscriptionName(subscription)
		if _, exists := seen[name]; exists {
			return nil, validationError("duplicate WebSocket subscription %q", name)
		}
		seen[name] = struct{}{}
		result = append(result, subscription)
	}
	return result, nil
}

func validateStreamSubscription(subscription StreamSubscription, private bool) error {
	if private {
		switch subscription.Channel {
		case StreamChannelAccount, StreamChannelAccountDeals, StreamChannelAccountOrders:
		default:
			return validationError("unsupported private WebSocket channel %q", subscription.Channel)
		}
		if subscription.Symbol != "" || subscription.UpdateInterval != "" ||
			subscription.CandleInterval != "" || subscription.Depth != 0 {
			return validationError("private WebSocket subscription does not accept public options")
		}
		return nil
	}
	if err := validateSymbol(subscription.Symbol); err != nil {
		return err
	}
	switch subscription.Channel {
	case StreamChannelAggregateTrades, StreamChannelDiffDepth, StreamChannelBookTicker:
		if !streamUpdateIntervalValid(subscription.UpdateInterval) {
			return validationError(
				"unsupported WebSocket update interval %q", subscription.UpdateInterval,
			)
		}
	case StreamChannelCandles:
		if !streamCandleIntervalValid(subscription.CandleInterval) {
			return validationError(
				"unsupported WebSocket candle interval %q", subscription.CandleInterval,
			)
		}
	case StreamChannelPartialDepth:
		if subscription.Depth != StreamDepth5 && subscription.Depth != StreamDepth10 &&
			subscription.Depth != StreamDepth20 {
			return validationError("WebSocket partial depth must be 5, 10, or 20")
		}
	default:
		return validationError("unsupported public WebSocket channel %q", subscription.Channel)
	}
	if subscription.Channel != StreamChannelAggregateTrades &&
		subscription.Channel != StreamChannelDiffDepth &&
		subscription.Channel != StreamChannelBookTicker && subscription.UpdateInterval != "" {
		return validationError("WebSocket update interval is not supported by this channel")
	}
	if subscription.Channel != StreamChannelCandles && subscription.CandleInterval != "" {
		return validationError("WebSocket candle interval is only supported for candles")
	}
	if subscription.Channel != StreamChannelPartialDepth && subscription.Depth != 0 {
		return validationError("WebSocket depth is only supported for partial depth")
	}
	return nil
}

func streamUpdateIntervalValid(interval StreamUpdateInterval) bool {
	return interval == StreamUpdate10Millis || interval == StreamUpdate100Millis
}

func streamCandleIntervalValid(interval StreamCandleInterval) bool {
	switch interval {
	case StreamCandle1Minute, StreamCandle5Minutes, StreamCandle15Minutes,
		StreamCandle30Minutes, StreamCandle1Hour, StreamCandle4Hours,
		StreamCandle8Hours, StreamCandle1Day, StreamCandle1Week, StreamCandle1Month:
		return true
	default:
		return false
	}
}

func streamSubscriptionName(subscription StreamSubscription) string {
	switch subscription.Channel {
	case StreamChannelAggregateTrades:
		return "spot@public.aggre.deals.v3.api.pb@" +
			string(subscription.UpdateInterval) + "@" + subscription.Symbol
	case StreamChannelCandles:
		return "spot@public.kline.v3.api.pb@" + subscription.Symbol +
			"@" + string(subscription.CandleInterval)
	case StreamChannelDiffDepth:
		return "spot@public.aggre.depth.v3.api.pb@" +
			string(subscription.UpdateInterval) + "@" + subscription.Symbol
	case StreamChannelPartialDepth:
		return "spot@public.limit.depth.v3.api.pb@" + subscription.Symbol +
			"@" + strconv.Itoa(int(subscription.Depth))
	case StreamChannelBookTicker:
		return "spot@public.aggre.bookTicker.v3.api.pb@" +
			string(subscription.UpdateInterval) + "@" + subscription.Symbol
	case StreamChannelAccount:
		return "spot@private.account.v3.api.pb"
	case StreamChannelAccountDeals:
		return "spot@private.deals.v3.api.pb"
	case StreamChannelAccountOrders:
		return "spot@private.orders.v3.api.pb"
	default:
		return ""
	}
}

func (client *StreamClient) resolveStreamRoute(
	options ...trade.RequestOption,
) (transport.EgressRouteID, error) {
	resolved, err := trade.ResolveRequestOptions(client.defaultRouteID, options...)
	if err != nil {
		return "", err
	}
	if resolved.Timeout != 0 {
		return "", fmt.Errorf("MEXC stream timeout must be controlled by Run context")
	}
	return resolved.EgressRouteID, nil
}

func (client *StreamClient) streamReconnectPolicy(cause error) bool {
	if errors.Is(cause, trade.ErrAuthentication) || errors.Is(cause, trade.ErrAuthorization) ||
		errors.Is(cause, trade.ErrValidation) || errors.Is(cause, trade.ErrUnknownExecutionState) {
		return false
	}
	if client.reconnectPolicy != nil {
		return client.reconnectPolicy(cause)
	}
	return corestream.DefaultReconnectPolicy(cause)
}

func (client *StreamClient) streamAuthenticationError(cause error) error {
	accountID := ""
	if client.restClient != nil && client.restClient.credentials != nil {
		accountID = client.restClient.credentials.AccountID
	}
	return &trade.APIError{
		Category: trade.ErrorAuthentication, Exchange: model.ExchangeMEXC,
		AccountID: accountID, Cause: cause,
	}
}

func (client *StreamClient) streamAuthorizationError(cause error) error {
	accountID := ""
	if client.restClient != nil && client.restClient.credentials != nil {
		accountID = client.restClient.credentials.AccountID
	}
	return &trade.APIError{
		Category: trade.ErrorAuthorization, Exchange: model.ExchangeMEXC,
		AccountID: accountID, Cause: cause,
	}
}

func validateMEXCWebSocketURL(value string, allowInsecure bool) (string, error) {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" ||
		parsed.RawQuery != "" {
		return "", fmt.Errorf("WebSocket URL is malformed")
	}
	if parsed.Scheme != "wss" && !(allowInsecure && parsed.Scheme == "ws") {
		return "", fmt.Errorf("WebSocket URL must use WSS")
	}
	return parsed.String(), nil
}

func userDataEndpoint(baseURL, listenKey string) (string, error) {
	if !listenKeyPattern.MatchString(listenKey) {
		return "", validationError("invalid MEXC listen key")
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("parse MEXC WebSocket URL: %w", err)
	}
	query := parsed.Query()
	query.Set("listenKey", listenKey)
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}
