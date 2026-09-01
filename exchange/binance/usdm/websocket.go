package usdm

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

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	"github.com/proven-trade/cex-sdk/model"
	corestream "github.com/proven-trade/cex-sdk/stream"
	"github.com/proven-trade/cex-sdk/transport"
)

const (
	DefaultPublicStreamURL        = "wss://fstream.binance.com/public/stream"
	DefaultRegularMarketStreamURL = "wss://fstream.binance.com/market/stream"
	DefaultPrivateStreamURL       = "wss://fstream.binance.com/private/ws"
	defaultStreamPingInterval     = 5 * time.Minute
	defaultStreamPingTimeout      = 10 * time.Second
	defaultSubscriptionInterval   = 250 * time.Millisecond
	defaultListenKeyKeepalive     = 50 * time.Minute
	maximumMarketSubscriptions    = 1024
	maximumListenKeyValidity      = 60 * time.Minute
)

var listenKeyPattern = regexp.MustCompile(`^[0-9A-Za-z]{1,256}$`)

// StreamClientConfig는 Binance USDⓈ-M Futures public·market·private WebSocket 설정이다.
type StreamClientConfig struct {
	Connector                  corestream.Connector
	RESTClient                 *Client
	DefaultEgressRouteID       transport.EgressRouteID
	PublicStreamURL            string
	RegularMarketStreamURL     string
	PrivateStreamURL           string
	AllowInsecureWebSocket     bool
	Observer                   corestream.StateObserver
	ReconnectPolicy            corestream.ReconnectPolicy
	Backoff                    corestream.Backoff
	MaxReconnectAttempts       int
	PingInterval               time.Duration
	PingTimeout                time.Duration
	SubscriptionInterval       time.Duration
	ListenKeyKeepaliveInterval time.Duration
}

// StreamClient는 Binance USDⓈ-M Futures WebSocket 세션을 생성한다.
type StreamClient struct {
	connector                  corestream.Connector
	restClient                 *Client
	defaultRouteID             transport.EgressRouteID
	publicStreamURL            string
	regularMarketStreamURL     string
	privateStreamURL           string
	observer                   corestream.StateObserver
	reconnectPolicy            corestream.ReconnectPolicy
	backoff                    corestream.Backoff
	maxReconnectAttempts       int
	pingInterval               time.Duration
	pingTimeout                time.Duration
	subscriptionInterval       time.Duration
	listenKeyKeepaliveInterval time.Duration
	nextID                     atomic.Uint64
}

// NewStreamClient는 2026년 분리 진입점 규칙을 적용한 USDⓈ-M Futures 클라이언트를 생성한다.
func NewStreamClient(config StreamClientConfig) (*StreamClient, error) {
	if config.Connector == nil {
		return nil, fmt.Errorf("Binance USD-M stream connector is required")
	}
	defaultRouteID := transport.EgressRouteID(strings.TrimSpace(string(config.DefaultEgressRouteID)))
	if defaultRouteID == "" {
		return nil, trade.ErrMissingEgressRoute
	}
	if config.PublicStreamURL == "" {
		config.PublicStreamURL = DefaultPublicStreamURL
	}
	if config.RegularMarketStreamURL == "" {
		config.RegularMarketStreamURL = DefaultRegularMarketStreamURL
	}
	if config.PrivateStreamURL == "" {
		config.PrivateStreamURL = DefaultPrivateStreamURL
	}
	publicURL, err := validateFuturesStreamURL(
		config.PublicStreamURL, config.AllowInsecureWebSocket,
	)
	if err != nil {
		return nil, fmt.Errorf("invalid Binance USD-M public stream URL: %w", err)
	}
	marketURL, err := validateFuturesStreamURL(
		config.RegularMarketStreamURL, config.AllowInsecureWebSocket,
	)
	if err != nil {
		return nil, fmt.Errorf("invalid Binance USD-M market stream URL: %w", err)
	}
	privateURL, err := validateFuturesStreamURL(
		config.PrivateStreamURL, config.AllowInsecureWebSocket,
	)
	if err != nil {
		return nil, fmt.Errorf("invalid Binance USD-M private stream URL: %w", err)
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
	if config.ListenKeyKeepaliveInterval == 0 {
		config.ListenKeyKeepaliveInterval = defaultListenKeyKeepalive
	}
	if config.PingInterval < 0 || config.PingTimeout < 0 ||
		config.SubscriptionInterval < 0 || config.MaxReconnectAttempts < 0 {
		return nil, fmt.Errorf("Binance USD-M stream durations or reconnect attempts are invalid")
	}
	if config.ListenKeyKeepaliveInterval <= 0 ||
		config.ListenKeyKeepaliveInterval >= maximumListenKeyValidity {
		return nil, fmt.Errorf("Binance USD-M listen key keepalive interval must be shorter than 60 minutes")
	}
	client := &StreamClient{
		connector: config.Connector, restClient: config.RESTClient,
		defaultRouteID: defaultRouteID, publicStreamURL: publicURL,
		regularMarketStreamURL: marketURL, privateStreamURL: privateURL,
		observer: config.Observer, reconnectPolicy: config.ReconnectPolicy,
		backoff: config.Backoff, maxReconnectAttempts: config.MaxReconnectAttempts,
		pingInterval: config.PingInterval, pingTimeout: config.PingTimeout,
		subscriptionInterval:       config.SubscriptionInterval,
		listenKeyKeepaliveInterval: config.ListenKeyKeepaliveInterval,
	}
	client.nextID.Store(uint64(time.Now().UnixMicro()))
	return client, nil
}

type pendingMarketCommand struct {
	operation     string
	subscriptions []StreamSubscription
}

// MarketStream은 같은 분리 진입점을 사용하는 public·market 구독 연결을 관리한다.
type MarketStream struct {
	session *corestream.Session
	client  *StreamClient
	route   StreamRoute

	commandMu     sync.Mutex
	lastCommandAt time.Time
	stateMu       sync.Mutex
	subscriptions map[string]StreamSubscription
	pending       map[string]pendingMarketCommand
}

// MarketStream은 선택한 송신 경로와 데이터 진입점에 고정된 시세 세션을 생성한다.
func (client *StreamClient) MarketStream(
	request MarketStreamRequest,
	options ...trade.RequestOption,
) (*MarketStream, error) {
	subscriptions, route, err := validateMarketSubscriptions(
		request.Subscriptions, "", true,
	)
	if err != nil {
		return nil, err
	}
	routeID, err := client.resolveStreamRoute(options...)
	if err != nil {
		return nil, err
	}
	market := &MarketStream{
		client: client, route: route,
		subscriptions: make(map[string]StreamSubscription, len(subscriptions)),
		pending:       make(map[string]pendingMarketCommand),
	}
	for _, subscription := range subscriptions {
		market.subscriptions[subscription.Name] = subscription
	}
	endpoint := client.publicStreamURL
	if route == StreamRouteMarket {
		endpoint = client.regularMarketStreamURL
	}
	session, err := corestream.NewSession(corestream.SessionConfig{
		Connector: client.connector, EgressRouteID: routeID,
		Request: corestream.DialRequest{Endpoint: endpoint}, OnConnect: market.resubscribe,
		Observer: client.observer, ReconnectPolicy: client.streamReconnectPolicy,
		Backoff: client.backoff, MaxReconnectAttempts: client.maxReconnectAttempts,
		PingInterval: client.pingInterval, PingTimeout: client.pingTimeout,
	})
	if err != nil {
		return nil, err
	}
	market.session = session
	return market, nil
}

// Run은 public·market 메시지를 순서대로 해석해 처리기에 전달한다.
func (market *MarketStream) Run(
	ctx context.Context,
	handler func(context.Context, MarketStreamMessage) error,
) error {
	if handler == nil {
		return fmt.Errorf("Binance USD-M market stream handler is required")
	}
	return market.session.Run(ctx, func(ctx context.Context, message corestream.Message) error {
		decoded, err := DecodeMarketStreamMessage(message)
		if err != nil {
			return corestream.ReconnectOnMessageError(err)
		}
		market.handleControl(decoded)
		return handler(ctx, decoded)
	})
}

// Subscribe는 같은 진입점의 구독을 추가하고 재연결 복구 목록에도 반영한다.
func (market *MarketStream) Subscribe(
	ctx context.Context,
	subscriptions ...StreamSubscription,
) error {
	validated, _, err := validateMarketSubscriptions(subscriptions, market.route, true)
	if err != nil {
		return err
	}
	return market.change(ctx, "SUBSCRIBE", validated)
}

// Unsubscribe는 같은 진입점의 구독을 제거하고 재연결 복구 목록에도 반영한다.
func (market *MarketStream) Unsubscribe(
	ctx context.Context,
	subscriptions ...StreamSubscription,
) error {
	validated, _, err := validateMarketSubscriptions(subscriptions, market.route, true)
	if err != nil {
		return err
	}
	return market.change(ctx, "UNSUBSCRIBE", validated)
}

// Close는 public·market stream 세션을 종료한다.
func (market *MarketStream) Close() error { return market.session.Close() }

// Generation은 성공한 public·market 연결 세대 번호를 반환한다.
func (market *MarketStream) Generation() uint64 { return market.session.Generation() }

// EgressRouteID는 public·market 연결과 재연결에 고정된 송신 경로를 반환한다.
func (market *MarketStream) EgressRouteID() transport.EgressRouteID {
	return market.session.EgressRouteID()
}

func (market *MarketStream) hasDiffDepthStream(symbol string) bool {
	prefix := strings.ToLower(symbol) + "@depth"
	market.stateMu.Lock()
	defer market.stateMu.Unlock()
	for _, subscription := range market.subscriptions {
		if subscription.Route != StreamRoutePublic {
			continue
		}
		if subscription.Name == prefix || subscription.Name == prefix+"@100ms" ||
			subscription.Name == prefix+"@500ms" {
			return true
		}
	}
	return false
}

func (market *MarketStream) resubscribe(
	ctx context.Context,
	connection corestream.Connection,
) error {
	market.commandMu.Lock()
	defer market.commandMu.Unlock()
	subscriptions := market.snapshotSubscriptions()
	market.stateMu.Lock()
	market.pending = make(map[string]pendingMarketCommand, 1)
	market.stateMu.Unlock()
	if len(subscriptions) == 0 {
		return nil
	}
	if err := market.waitCommandInterval(ctx); err != nil {
		return err
	}
	payload, id, err := market.encodeCommand("SUBSCRIBE", subscriptions)
	if err != nil {
		return err
	}
	if err := connection.Write(
		ctx, corestream.Message{Type: corestream.MessageText, Data: payload},
	); err != nil {
		return err
	}
	market.lastCommandAt = time.Now()
	market.stateMu.Lock()
	market.pending[id] = pendingMarketCommand{
		operation: "SUBSCRIBE", subscriptions: subscriptions,
	}
	market.stateMu.Unlock()
	return nil
}

func (market *MarketStream) change(
	ctx context.Context,
	operation string,
	subscriptions []StreamSubscription,
) error {
	if ctx == nil {
		return fmt.Errorf("Binance USD-M market stream command context is nil")
	}
	market.commandMu.Lock()
	defer market.commandMu.Unlock()
	market.stateMu.Lock()
	changed := make([]StreamSubscription, 0, len(subscriptions))
	for _, subscription := range subscriptions {
		_, exists := market.subscriptions[subscription.Name]
		if operation == "SUBSCRIBE" && !exists || operation == "UNSUBSCRIBE" && exists {
			changed = append(changed, subscription)
		}
	}
	if operation == "SUBSCRIBE" && len(market.subscriptions)+len(changed) > maximumMarketSubscriptions {
		market.stateMu.Unlock()
		return validationError(
			"WebSocket subscription count cannot exceed %d", maximumMarketSubscriptions,
		)
	}
	market.stateMu.Unlock()
	if len(changed) == 0 {
		return nil
	}
	if err := market.waitCommandInterval(ctx); err != nil {
		return err
	}
	payload, id, err := market.encodeCommand(operation, changed)
	if err != nil {
		return err
	}
	market.stateMu.Lock()
	market.pending[id] = pendingMarketCommand{operation: operation, subscriptions: changed}
	market.applyCommandLocked(operation, changed)
	market.stateMu.Unlock()
	if err := market.session.Write(
		ctx, corestream.Message{Type: corestream.MessageText, Data: payload},
	); err != nil {
		market.rollbackCommand(id)
		return err
	}
	market.lastCommandAt = time.Now()
	return nil
}

func (market *MarketStream) encodeCommand(
	operation string,
	subscriptions []StreamSubscription,
) ([]byte, string, error) {
	id := market.client.nextID.Add(1)
	names := make([]string, 0, len(subscriptions))
	for _, subscription := range subscriptions {
		names = append(names, subscription.Name)
	}
	sort.Strings(names)
	payload, err := json.Marshal(struct {
		Method string   `json:"method"`
		Params []string `json:"params"`
		ID     uint64   `json:"id"`
	}{Method: operation, Params: names, ID: id})
	if err != nil {
		return nil, "", fmt.Errorf("encode Binance USD-M stream command: %w", err)
	}
	return payload, strconv.FormatUint(id, 10), nil
}

func (market *MarketStream) handleControl(message MarketStreamMessage) {
	if message.Response == nil || len(message.Response.ID) == 0 {
		return
	}
	id, err := streamResponseID(message.Response.ID)
	if err != nil {
		return
	}
	market.stateMu.Lock()
	defer market.stateMu.Unlock()
	pending, exists := market.pending[id]
	if !exists {
		return
	}
	delete(market.pending, id)
	if message.Response.Error == nil {
		return
	}
	market.rollbackCommandLocked(pending)
}

func (market *MarketStream) rollbackCommand(id string) {
	market.stateMu.Lock()
	defer market.stateMu.Unlock()
	pending, exists := market.pending[id]
	if !exists {
		return
	}
	delete(market.pending, id)
	market.rollbackCommandLocked(pending)
}

func (market *MarketStream) rollbackCommandLocked(pending pendingMarketCommand) {
	operation := "SUBSCRIBE"
	if pending.operation == "SUBSCRIBE" {
		operation = "UNSUBSCRIBE"
	}
	market.applyCommandLocked(operation, pending.subscriptions)
}

func (market *MarketStream) applyCommandLocked(
	operation string,
	subscriptions []StreamSubscription,
) {
	for _, subscription := range subscriptions {
		if operation == "SUBSCRIBE" {
			market.subscriptions[subscription.Name] = subscription
		} else {
			delete(market.subscriptions, subscription.Name)
		}
	}
}

func (market *MarketStream) snapshotSubscriptions() []StreamSubscription {
	market.stateMu.Lock()
	defer market.stateMu.Unlock()
	names := make([]string, 0, len(market.subscriptions))
	for name := range market.subscriptions {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]StreamSubscription, 0, len(names))
	for _, name := range names {
		result = append(result, market.subscriptions[name])
	}
	return result
}

func (market *MarketStream) waitCommandInterval(ctx context.Context) error {
	wait := market.client.subscriptionInterval - time.Since(market.lastCommandAt)
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

// UserDataStream은 listenKey 기반 private 계정·주문 이벤트 연결을 관리한다.
type UserDataStream struct {
	session *corestream.Session
	client  *StreamClient
	routeID transport.EgressRouteID

	keyMu     sync.RWMutex
	listenKey string
}

// UserDataStream은 같은 송신 경로에서 listenKey를 발급하고 private 연결을 생성한다.
func (client *StreamClient) UserDataStream(
	options ...trade.RequestOption,
) (*UserDataStream, error) {
	routeID, err := client.resolveStreamRoute(options...)
	if err != nil {
		return nil, err
	}
	if client.restClient == nil || client.restClient.credentials == nil ||
		client.restClient.credentialProvider == nil {
		return nil, client.streamAuthenticationError(
			errors.New("private Binance USD-M stream requires REST client credentials"),
		)
	}
	if err := client.restClient.credentials.RequireEgressRoute(routeID); err != nil {
		return nil, client.streamAuthorizationError(err)
	}
	if err := client.restClient.credentials.RequirePermission(credential.PermissionRead); err != nil {
		return nil, client.streamAuthorizationError(err)
	}
	userData := &UserDataStream{client: client, routeID: routeID}
	session, err := corestream.NewSession(corestream.SessionConfig{
		Connector: client.connector, EgressRouteID: routeID,
		RequestSource: userData.dialRequest, Observer: client.observer,
		ReconnectPolicy: client.streamReconnectPolicy, Backoff: client.backoff,
		MaxReconnectAttempts: client.maxReconnectAttempts,
		PingInterval:         client.pingInterval, PingTimeout: client.pingTimeout,
	})
	if err != nil {
		return nil, err
	}
	userData.session = session
	return userData, nil
}

// Run은 private 메시지를 순서대로 해석하고 listenKey를 주기적으로 갱신한다.
func (userData *UserDataStream) Run(
	ctx context.Context,
	handler func(context.Context, UserDataStreamMessage) error,
) error {
	if ctx == nil {
		return fmt.Errorf("Binance USD-M user data stream context is nil")
	}
	if handler == nil {
		return fmt.Errorf("Binance USD-M user data stream handler is required")
	}
	keepaliveContext, cancelKeepalive := context.WithCancel(ctx)
	defer cancelKeepalive()
	go userData.keepalive(keepaliveContext)
	return userData.session.Run(ctx, func(ctx context.Context, message corestream.Message) error {
		decoded, err := DecodeUserDataStreamMessage(message)
		if err != nil {
			return corestream.ReconnectOnMessageError(err)
		}
		if decoded.EventType == "listenKeyExpired" {
			_ = userData.session.Reconnect()
		}
		return handler(ctx, decoded)
	})
}

// Close는 private stream 세션을 종료한다.
func (userData *UserDataStream) Close() error { return userData.session.Close() }

// Generation은 성공한 private 연결 세대 번호를 반환한다.
func (userData *UserDataStream) Generation() uint64 { return userData.session.Generation() }

func (userData *UserDataStream) dialRequest(
	ctx context.Context,
) (corestream.DialRequest, error) {
	result, err := userData.client.restClient.StartUserDataStream(
		ctx, trade.WithEgressRoute(userData.routeID),
	)
	if err != nil {
		return corestream.DialRequest{}, err
	}
	if !listenKeyPattern.MatchString(result.ListenKey) {
		return corestream.DialRequest{}, validationError("invalid Binance USD-M listen key")
	}
	userData.setListenKey(result.ListenKey)
	return corestream.DialRequest{
		Endpoint: strings.TrimSuffix(userData.client.privateStreamURL, "/") + "/" + result.ListenKey,
	}, nil
}

func (userData *UserDataStream) keepalive(ctx context.Context) {
	ticker := time.NewTicker(userData.client.listenKeyKeepaliveInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			current := userData.currentListenKey()
			if current == "" {
				continue
			}
			result, err := userData.client.restClient.KeepaliveUserDataStream(
				ctx, trade.WithEgressRoute(userData.routeID),
			)
			if err != nil {
				_ = userData.session.Reconnect()
				continue
			}
			if result.ListenKey != "" && result.ListenKey != current {
				userData.setListenKey(result.ListenKey)
				_ = userData.session.Reconnect()
			}
		}
	}
}

func (userData *UserDataStream) setListenKey(value string) {
	userData.keyMu.Lock()
	userData.listenKey = value
	userData.keyMu.Unlock()
}

func (userData *UserDataStream) currentListenKey() string {
	userData.keyMu.RLock()
	defer userData.keyMu.RUnlock()
	return userData.listenKey
}

// AggregateTradeStream은 정규 market 진입점의 합산 체결 구독을 만든다.
func AggregateTradeStream(symbol string) (StreamSubscription, error) {
	return symbolStream(symbol, "aggTrade", StreamRouteMarket)
}

// MarkPriceStream은 정규 market 진입점의 마크가 구독을 만든다.
func MarkPriceStream(symbol string, updateSpeed time.Duration) (StreamSubscription, error) {
	if updateSpeed != 0 && updateSpeed != time.Second {
		return StreamSubscription{}, validationError("mark price update speed must be default or 1s")
	}
	subscription, err := symbolStream(symbol, "markPrice", StreamRouteMarket)
	if err != nil {
		return StreamSubscription{}, err
	}
	if updateSpeed == time.Second {
		subscription.Name += "@1s"
	}
	return subscription, nil
}

// KlineStream은 정규 market 진입점의 캔들 구독을 만든다.
func KlineStream(symbol string, interval CandleInterval) (StreamSubscription, error) {
	if err := validateSymbol(symbol); err != nil {
		return StreamSubscription{}, err
	}
	if !interval.valid() {
		return StreamSubscription{}, validationError("unsupported WebSocket candle interval %q", interval)
	}
	return StreamSubscription{
		Route: StreamRouteMarket, Name: strings.ToLower(symbol) + "@kline_" + string(interval),
	}, nil
}

// TickerStream은 정규 market 진입점의 24시간 ticker 구독을 만든다.
func TickerStream(symbol string) (StreamSubscription, error) {
	return symbolStream(symbol, "ticker", StreamRouteMarket)
}

// BookTickerStream은 고빈도 public 진입점의 최우선 호가 구독을 만든다.
func BookTickerStream(symbol string) (StreamSubscription, error) {
	return symbolStream(symbol, "bookTicker", StreamRoutePublic)
}

// DiffDepthStream은 고빈도 public 진입점의 증분 호가 구독을 만든다.
func DiffDepthStream(symbol string, updateSpeed time.Duration) (StreamSubscription, error) {
	if updateSpeed != 0 && updateSpeed != 100*time.Millisecond &&
		updateSpeed != 500*time.Millisecond {
		return StreamSubscription{}, validationError("depth update speed must be default, 100ms, or 500ms")
	}
	subscription, err := symbolStream(symbol, "depth", StreamRoutePublic)
	if err != nil {
		return StreamSubscription{}, err
	}
	if updateSpeed != 0 {
		subscription.Name += "@" + strconv.FormatInt(updateSpeed.Milliseconds(), 10) + "ms"
	}
	return subscription, nil
}

// PartialDepthStream은 고빈도 public 진입점의 제한 단계 호가 구독을 만든다.
func PartialDepthStream(
	symbol string,
	levels int,
	updateSpeed time.Duration,
) (StreamSubscription, error) {
	if levels != 5 && levels != 10 && levels != 20 {
		return StreamSubscription{}, validationError("partial depth levels must be 5, 10, or 20")
	}
	if updateSpeed != 0 && updateSpeed != 100*time.Millisecond &&
		updateSpeed != 500*time.Millisecond {
		return StreamSubscription{}, validationError(
			"partial depth update speed must be default, 100ms, or 500ms",
		)
	}
	if err := validateSymbol(symbol); err != nil {
		return StreamSubscription{}, err
	}
	name := strings.ToLower(symbol) + "@depth" + strconv.Itoa(levels)
	if updateSpeed != 0 {
		name += "@" + strconv.FormatInt(updateSpeed.Milliseconds(), 10) + "ms"
	}
	return StreamSubscription{Route: StreamRoutePublic, Name: name}, nil
}

func symbolStream(
	symbol string,
	channel string,
	route StreamRoute,
) (StreamSubscription, error) {
	if err := validateSymbol(symbol); err != nil {
		return StreamSubscription{}, err
	}
	return StreamSubscription{Route: route, Name: strings.ToLower(symbol) + "@" + channel}, nil
}

func validateMarketSubscriptions(
	subscriptions []StreamSubscription,
	requiredRoute StreamRoute,
	requireNonEmpty bool,
) ([]StreamSubscription, StreamRoute, error) {
	if requireNonEmpty && len(subscriptions) == 0 {
		return nil, "", validationError("at least one WebSocket subscription is required")
	}
	if len(subscriptions) > maximumMarketSubscriptions {
		return nil, "", validationError(
			"WebSocket subscription count cannot exceed %d", maximumMarketSubscriptions,
		)
	}
	route := requiredRoute
	result := make([]StreamSubscription, 0, len(subscriptions))
	seen := make(map[string]struct{}, len(subscriptions))
	for _, subscription := range subscriptions {
		if subscription.Route != StreamRoutePublic && subscription.Route != StreamRouteMarket {
			return nil, "", validationError("unsupported WebSocket stream route %q", subscription.Route)
		}
		if route == "" {
			route = subscription.Route
		}
		if subscription.Route != route {
			return nil, "", validationError("WebSocket subscriptions from different routes cannot share a connection")
		}
		if subscription.Name == "" || strings.TrimSpace(subscription.Name) != subscription.Name ||
			strings.ContainsFunc(subscription.Name, unicode.IsControl) {
			return nil, "", validationError("invalid WebSocket stream name")
		}
		if inferred, ok := streamRouteForName(subscription.Name); !ok || inferred != subscription.Route {
			return nil, "", validationError("WebSocket stream %q does not match route %q", subscription.Name, subscription.Route)
		}
		if _, exists := seen[subscription.Name]; exists {
			return nil, "", validationError("duplicate WebSocket stream %q", subscription.Name)
		}
		seen[subscription.Name] = struct{}{}
		result = append(result, subscription)
	}
	return result, route, nil
}

func streamRouteForName(name string) (StreamRoute, bool) {
	switch {
	case name == "!bookTicker", strings.Contains(name, "@bookTicker"),
		strings.Contains(name, "@depth"):
		return StreamRoutePublic, true
	case name == "!markPrice@arr", name == "!markPrice@arr@1s",
		name == "!miniTicker@arr", name == "!ticker@arr", name == "!forceOrder@arr",
		name == "!contractInfo", name == "!assetIndex@arr",
		strings.Contains(name, "@aggTrade"), strings.Contains(name, "@markPrice"),
		strings.Contains(name, "@kline_"), strings.Contains(name, "@continuousKline_"),
		strings.Contains(name, "@miniTicker"), strings.Contains(name, "@ticker"),
		strings.Contains(name, "@forceOrder"), strings.Contains(name, "@compositeIndex"),
		strings.Contains(name, "@assetIndex"):
		return StreamRouteMarket, true
	default:
		return "", false
	}
}

func streamResponseID(raw json.RawMessage) (string, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return "", nil
	}
	if trimmed[0] == '"' {
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return "", err
		}
		return value, nil
	}
	var number json.Number
	if err := json.Unmarshal(raw, &number); err != nil {
		return "", err
	}
	return number.String(), nil
}

func (client *StreamClient) resolveStreamRoute(
	options ...trade.RequestOption,
) (transport.EgressRouteID, error) {
	resolved, err := trade.ResolveRequestOptions(client.defaultRouteID, options...)
	if err != nil {
		return "", err
	}
	if resolved.Timeout != 0 {
		return "", fmt.Errorf("Binance USD-M stream timeout must be controlled by Run context")
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
	if client.restClient != nil && client.restClient.credentials != nil {
		accountID = client.restClient.credentials.AccountID
	}
	return &trade.APIError{
		Category: trade.ErrorAuthentication, Exchange: model.ExchangeBinance,
		AccountID: accountID, Cause: cause,
	}
}

func (client *StreamClient) streamAuthorizationError(cause error) error {
	accountID := ""
	if client.restClient != nil && client.restClient.credentials != nil {
		accountID = client.restClient.credentials.AccountID
	}
	return &trade.APIError{
		Category: trade.ErrorAuthorization, Exchange: model.ExchangeBinance,
		AccountID: accountID, Cause: cause,
	}
}

func validateFuturesStreamURL(raw string, allowInsecure bool) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" ||
		parsed.Fragment != "" {
		return "", fmt.Errorf("invalid WebSocket URL")
	}
	if parsed.Scheme != "wss" && !(allowInsecure && parsed.Scheme == "ws") {
		return "", fmt.Errorf("WebSocket URL must use WSS")
	}
	return strings.TrimSuffix(parsed.String(), "/"), nil
}
