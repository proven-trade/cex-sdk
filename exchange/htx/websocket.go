package htx

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
	DefaultPublicWebSocketURL     = "wss://api.huobi.pro/ws"
	DefaultAWSPublicWebSocketURL  = "wss://api-aws.huobi.pro/ws"
	DefaultPrivateWebSocketURL    = "wss://api.huobi.pro/ws/v2"
	DefaultAWSPrivateWebSocketURL = "wss://api-aws.huobi.pro/ws/v2"
	maximumStreamSubscriptions    = 200
)

// StreamClientConfig는 HTX 일반 시세 WebSocket 설정이다.
type StreamClientConfig struct {
	Connector              corestream.Connector
	Credentials            *credential.Descriptor
	CredentialProvider     credential.Provider
	DefaultEgressRouteID   transport.EgressRouteID
	PublicWebSocketURL     string
	PrivateWebSocketURL    string
	AllowInsecureWebSocket bool
	Now                    func() time.Time
	Observer               corestream.StateObserver
	ReconnectPolicy        corestream.ReconnectPolicy
	Backoff                corestream.Backoff
	MaxReconnectAttempts   int
}

// StreamClient는 HTX 일반 시세 WebSocket 세션을 생성한다.
type StreamClient struct {
	connector            corestream.Connector
	credentials          *credential.Descriptor
	credentialProvider   credential.Provider
	defaultRouteID       transport.EgressRouteID
	publicURL            string
	privateURL           string
	now                  func() time.Time
	observer             corestream.StateObserver
	reconnectPolicy      corestream.ReconnectPolicy
	backoff              corestream.Backoff
	maxReconnectAttempts int
	nextID               atomic.Int64
}

// NewStreamClient는 검증된 HTX 일반 시세 WebSocket 클라이언트를 생성한다.
func NewStreamClient(config StreamClientConfig) (*StreamClient, error) {
	if config.Connector == nil {
		return nil, fmt.Errorf("HTX stream connector is required")
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
	publicURL, err := validateStreamURL(
		config.PublicWebSocketURL, config.AllowInsecureWebSocket,
	)
	if err != nil {
		return nil, fmt.Errorf("invalid HTX public WebSocket URL: %w", err)
	}
	privateURL, err := validateStreamURL(
		config.PrivateWebSocketURL, config.AllowInsecureWebSocket,
	)
	if err != nil {
		return nil, fmt.Errorf("invalid HTX private WebSocket URL: %w", err)
	}
	if config.MaxReconnectAttempts < 0 {
		return nil, fmt.Errorf("HTX maximum reconnect attempts cannot be negative")
	}
	if config.Now == nil {
		config.Now = time.Now
	}

	var credentialsCopy *credential.Descriptor
	if config.Credentials != nil {
		if err := config.Credentials.Validate(); err != nil {
			return nil, err
		}
		if config.Credentials.Exchange != model.ExchangeHTX {
			return nil, fmt.Errorf("credential exchange must be HTX")
		}
		if config.CredentialProvider == nil {
			return nil, fmt.Errorf("credential provider is required for private HTX streams")
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
		credentialProvider: config.CredentialProvider,
		defaultRouteID:     defaultRouteID, publicURL: publicURL, privateURL: privateURL,
		now:      config.Now,
		observer: config.Observer, reconnectPolicy: config.ReconnectPolicy,
		backoff: config.Backoff, maxReconnectAttempts: config.MaxReconnectAttempts,
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

	commandMu     sync.Mutex
	stateMu       sync.Mutex
	subscriptions map[string]StreamSubscription
	pending       map[string]pendingStreamCommand
}

// PublicStream은 HTX 일반 공개 시세 WebSocket 연결을 관리한다.
type PublicStream struct{ managed *managedStream }

// PublicStream은 선택한 EIP route에 고정된 공개 시세 세션을 생성한다.
func (client *StreamClient) PublicStream(
	request StreamRequest,
	options ...trade.RequestOption,
) (*PublicStream, error) {
	subscriptions, err := validateStreamSubscriptions(request.Subscriptions, true)
	if err != nil {
		return nil, err
	}
	routeID, err := client.resolveStreamRoute(options...)
	if err != nil {
		return nil, err
	}
	managed := newManagedStream(client, subscriptions)
	session, err := corestream.NewSession(corestream.SessionConfig{
		Connector: client.connector, EgressRouteID: routeID,
		Request:   corestream.DialRequest{Endpoint: client.publicURL},
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

// Run은 공개 시세 메시지와 heartbeat를 순서대로 decode해 handler에 전달한다.
func (public *PublicStream) Run(
	ctx context.Context,
	handler func(context.Context, StreamMessage) error,
) error {
	return public.managed.run(ctx, handler)
}

// Subscribe는 공개 시세 구독을 추가하고 재연결 복구 목록에도 반영한다.
func (public *PublicStream) Subscribe(
	ctx context.Context,
	subscriptions ...StreamSubscription,
) error {
	validated, err := validateStreamSubscriptions(subscriptions, true)
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
	validated, err := validateStreamSubscriptions(subscriptions, true)
	if err != nil {
		return err
	}
	return public.managed.change(ctx, "unsubscribe", validated)
}

// Close는 공개 시세 stream 세션을 종료한다.
func (public *PublicStream) Close() error { return public.managed.session.Close() }

// Generation은 성공한 공개 시세 연결 세대 번호를 반환한다.
func (public *PublicStream) Generation() uint64 { return public.managed.session.Generation() }

// EgressRouteID는 최초 연결과 모든 재연결에 고정된 송신 경로를 반환한다.
func (public *PublicStream) EgressRouteID() transport.EgressRouteID {
	return public.managed.session.EgressRouteID()
}

func (public *PublicStream) reconnect() error { return public.managed.session.Reconnect() }

func newManagedStream(
	client *StreamClient,
	subscriptions []StreamSubscription,
) *managedStream {
	managed := &managedStream{
		client: client, subscriptions: make(map[string]StreamSubscription, len(subscriptions)),
		pending: make(map[string]pendingStreamCommand),
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
		return fmt.Errorf("HTX stream handler is required")
	}
	return managed.session.Run(ctx, func(ctx context.Context, message corestream.Message) error {
		decoded, err := DecodeStreamMessage(message)
		if err != nil {
			return err
		}
		if decoded.Ping != nil {
			payload, err := json.Marshal(struct {
				Pong int64 `json:"pong"`
			}{Pong: *decoded.Ping})
			if err != nil {
				return fmt.Errorf("encode HTX stream pong: %w", err)
			}
			if err := managed.session.Write(
				ctx, corestream.Message{Type: corestream.MessageText, Data: payload},
			); err != nil {
				if reconnectErr := managed.session.Reconnect(); reconnectErr != nil {
					return err
				}
				return nil
			}
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
		payload, id, err := managed.client.encodeStreamCommand("subscribe", subscription)
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
		return fmt.Errorf("HTX stream command context is nil")
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
		payload, id, err := managed.client.encodeStreamCommand(operation, subscription)
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
	if message.ID == "" || message.Status == "" {
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

func (client *StreamClient) encodeStreamCommand(
	operation string,
	subscription StreamSubscription,
) ([]byte, string, error) {
	id := "htx-" + strconv.FormatInt(client.nextID.Add(1), 10)
	topic := streamSubscriptionTopic(subscription)
	request := struct {
		Subscribe   string `json:"sub,omitempty"`
		Unsubscribe string `json:"unsub,omitempty"`
		ID          string `json:"id"`
	}{ID: id}
	if operation == "subscribe" {
		request.Subscribe = topic
	} else if operation == "unsubscribe" {
		request.Unsubscribe = topic
	} else {
		return nil, "", fmt.Errorf("unsupported HTX stream operation %q", operation)
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return nil, "", fmt.Errorf("encode HTX stream command: %w", err)
	}
	return payload, id, nil
}

func validateStreamSubscriptions(
	subscriptions []StreamSubscription,
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
		if err := validateStreamSubscription(subscription); err != nil {
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

func validateStreamSubscription(subscription StreamSubscription) error {
	if err := validateSymbol(subscription.Symbol); err != nil {
		return err
	}
	if subscription.Mode != 0 {
		return validationError("public WebSocket subscription does not accept private mode")
	}
	switch subscription.Channel {
	case StreamChannelTicker, StreamChannelBBO, StreamChannelTrades:
		if subscription.DepthType != "" || subscription.CandleInterval != "" {
			return validationError("WebSocket channel %q does not accept an interval", subscription.Channel)
		}
	case StreamChannelDepth:
		if !subscription.DepthType.valid() {
			return validationError("unsupported WebSocket depth type %q", subscription.DepthType)
		}
		if subscription.CandleInterval != "" {
			return validationError("depth WebSocket subscription does not accept candle interval")
		}
	case StreamChannelCandles:
		if !subscription.CandleInterval.valid() {
			return validationError(
				"unsupported WebSocket candle interval %q", subscription.CandleInterval,
			)
		}
		if subscription.DepthType != "" {
			return validationError("candle WebSocket subscription does not accept depth type")
		}
	default:
		return validationError("unsupported public WebSocket channel %q", subscription.Channel)
	}
	return nil
}

func streamSubscriptionTopic(subscription StreamSubscription) string {
	switch subscription.Channel {
	case StreamChannelTicker:
		return "market." + subscription.Symbol + ".ticker"
	case StreamChannelDepth:
		return "market." + subscription.Symbol + ".depth." + string(subscription.DepthType)
	case StreamChannelBBO:
		return "market." + subscription.Symbol + ".bbo"
	case StreamChannelTrades:
		return "market." + subscription.Symbol + ".trade.detail"
	case StreamChannelCandles:
		return "market." + subscription.Symbol + ".kline." + string(subscription.CandleInterval)
	default:
		return ""
	}
}

func streamSubscriptionKey(subscription StreamSubscription) string {
	return string(subscription.Channel) + "\x00" + subscription.Symbol + "\x00" +
		string(subscription.DepthType) + "\x00" + string(subscription.CandleInterval) +
		"\x00" + strconv.Itoa(int(subscription.Mode))
}

func (client *StreamClient) resolveStreamRoute(
	options ...trade.RequestOption,
) (transport.EgressRouteID, error) {
	resolved, err := trade.ResolveRequestOptions(client.defaultRouteID, options...)
	if err != nil {
		return "", err
	}
	if resolved.Timeout != 0 {
		return "", fmt.Errorf("HTX stream timeout must be controlled by Run context")
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

func validateStreamURL(value string, allowInsecure bool) (string, error) {
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
