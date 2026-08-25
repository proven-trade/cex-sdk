package korbit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
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
	DefaultPublicWebSocketURL  = "wss://ws-api.korbit.co.kr/v2/public"
	DefaultPrivateWebSocketURL = "wss://ws-api.korbit.co.kr/v2/private"
	defaultStreamPingInterval  = 15 * time.Second
	defaultStreamPingTimeout   = 5 * time.Second
	maxStreamSubscriptions     = 1000
)

// StreamClientConfig는 코빗 public/private WebSocket 설정이다.
type StreamClientConfig struct {
	Connector              corestream.Connector
	Credentials            *credential.Descriptor
	CredentialProvider     credential.Provider
	DefaultEgressRouteID   transport.EgressRouteID
	PublicWebSocketURL     string
	PrivateWebSocketURL    string
	AllowInsecureWebSocket bool
	SigningMode            SigningMode
	ReceiveWindow          int
	Now                    func() time.Time
	Observer               corestream.StateObserver
	ReconnectPolicy        corestream.ReconnectPolicy
	Backoff                corestream.Backoff
	MaxReconnectAttempts   int
	PingInterval           time.Duration
	PingTimeout            time.Duration
}

// StreamClient는 코빗 public/private WebSocket 세션을 생성한다.
type StreamClient struct {
	connector            corestream.Connector
	credentials          *credential.Descriptor
	credentialProvider   credential.Provider
	defaultRouteID       transport.EgressRouteID
	publicURL            string
	privateURL           string
	signingMode          SigningMode
	receiveWindow        int
	now                  func() time.Time
	observer             corestream.StateObserver
	reconnectPolicy      corestream.ReconnectPolicy
	backoff              corestream.Backoff
	maxReconnectAttempts int
	pingInterval         time.Duration
	pingTimeout          time.Duration
}

// NewStreamClient는 코빗 WebSocket 클라이언트를 생성한다.
func NewStreamClient(config StreamClientConfig) (*StreamClient, error) {
	if config.Connector == nil {
		return nil, fmt.Errorf("Korbit stream connector is required")
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
		return nil, fmt.Errorf("invalid Korbit public WebSocket URL: %w", err)
	}
	privateURL, err := validateWebSocketURL(config.PrivateWebSocketURL, config.AllowInsecureWebSocket)
	if err != nil {
		return nil, fmt.Errorf("invalid Korbit private WebSocket URL: %w", err)
	}
	if config.SigningMode == "" {
		config.SigningMode = SigningModeHMACSHA256
	}
	if config.SigningMode != SigningModeHMACSHA256 && config.SigningMode != SigningModeED25519 {
		return nil, fmt.Errorf("unsupported Korbit signing mode %q", config.SigningMode)
	}
	if config.ReceiveWindow == 0 {
		config.ReceiveWindow = DefaultReceiveWindow
	}
	if config.ReceiveWindow < 1 || config.ReceiveWindow > 60000 {
		return nil, fmt.Errorf("Korbit stream receive window must be between 1 and 60000 milliseconds")
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
		return nil, fmt.Errorf("Korbit stream durations or reconnect attempts are invalid")
	}

	var credentialsCopy *credential.Descriptor
	if config.Credentials != nil {
		if err := config.Credentials.Validate(); err != nil {
			return nil, err
		}
		if config.Credentials.Exchange != model.ExchangeKorbit {
			return nil, fmt.Errorf("credential exchange must be Korbit")
		}
		if config.CredentialProvider == nil {
			return nil, fmt.Errorf("credential provider is required for private Korbit streams")
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
		publicURL: publicURL, privateURL: privateURL,
		signingMode: config.SigningMode, receiveWindow: config.ReceiveWindow, now: config.Now,
		observer: config.Observer, reconnectPolicy: config.ReconnectPolicy, backoff: config.Backoff,
		maxReconnectAttempts: config.MaxReconnectAttempts,
		pingInterval:         config.PingInterval, pingTimeout: config.PingTimeout,
	}, nil
}

type pendingStreamCommand struct {
	method       string
	subscription StreamSubscription
}

type managedStream struct {
	session *corestream.Session
	private bool

	commandMu sync.Mutex
	stateMu   sync.Mutex
	nextID    int64
	pending   map[int64]pendingStreamCommand
	subs      map[string]StreamSubscription
}

// PublicStream은 코빗 public 시세 WebSocket 연결을 관리한다.
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
	routeID, err := client.resolveStreamRoute(options...)
	if err != nil {
		return nil, err
	}
	managed := newManagedStream(subscriptions, false)
	session, err := client.newStreamSession(
		routeID, corestream.DialRequest{Endpoint: client.publicURL}, nil, managed.resubscribe,
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
	return public.managed.change(ctx, "subscribe", validated)
}

// Unsubscribe는 public 구독을 제거하고 재연결 복구 목록에도 반영한다.
func (public *PublicStream) Unsubscribe(ctx context.Context, subscriptions ...StreamSubscription) error {
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

// EgressRouteID는 public stream 연결과 재연결에 고정된 송신 경로를 반환한다.
func (public *PublicStream) EgressRouteID() transport.EgressRouteID {
	return public.managed.session.EgressRouteID()
}

func (public *PublicStream) hasOrderBookSubscription(symbol, level string) bool {
	public.managed.stateMu.Lock()
	defer public.managed.stateMu.Unlock()
	matchCount := 0
	matchedLevel := false
	for _, subscription := range public.managed.subs {
		if subscription.Channel != StreamChannelOrderBook {
			continue
		}
		for _, subscribedSymbol := range subscription.Symbols {
			if subscribedSymbol != symbol {
				continue
			}
			matchCount++
			matchedLevel = subscription.Level == level
		}
	}
	return matchCount == 1 && matchedLevel
}

// PrivateStream은 코빗 private 주문·체결·자산 WebSocket 연결을 관리한다.
type PrivateStream struct{ managed *managedStream }

// PrivateStream은 서명한 handshake 후 선택한 송신 경로에 고정된 private 세션을 생성한다.
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
		return nil, client.streamAuthenticationError(errors.New("private Korbit stream requires credentials"))
	}
	if err := client.credentials.RequireEgressRoute(routeID); err != nil {
		return nil, client.streamAuthorizationError(err)
	}
	if err := client.credentials.RequirePermission(credential.PermissionRead); err != nil {
		return nil, client.streamAuthorizationError(err)
	}
	managed := newManagedStream(subscriptions, true)
	session, err := client.newStreamSession(
		routeID, corestream.DialRequest{}, client.privateStreamDialRequest, managed.resubscribe,
	)
	if err != nil {
		return nil, err
	}
	managed.session = session
	return &PrivateStream{managed: managed}, nil
}

// Run은 private 주문·체결·자산 메시지를 순서대로 decode해 handler에 전달한다.
func (private *PrivateStream) Run(ctx context.Context, handler func(context.Context, StreamMessage) error) error {
	return private.managed.run(ctx, handler)
}

// Subscribe는 private 구독을 추가하고 재연결 복구 목록에도 반영한다.
func (private *PrivateStream) Subscribe(ctx context.Context, subscriptions ...StreamSubscription) error {
	validated, err := validateStreamSubscriptions(subscriptions, true, true)
	if err != nil {
		return err
	}
	return private.managed.change(ctx, "subscribe", validated)
}

// Unsubscribe는 private 구독을 제거하고 재연결 복구 목록에도 반영한다.
func (private *PrivateStream) Unsubscribe(ctx context.Context, subscriptions ...StreamSubscription) error {
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
	request corestream.DialRequest,
	requestSource corestream.DialRequestSource,
	onConnect corestream.ConnectHook,
) (*corestream.Session, error) {
	return corestream.NewSession(corestream.SessionConfig{
		Connector: client.connector, EgressRouteID: routeID,
		Request: request, RequestSource: requestSource, OnConnect: onConnect,
		Observer: client.observer, ReconnectPolicy: client.reconnectPolicy, Backoff: client.backoff,
		MaxReconnectAttempts: client.maxReconnectAttempts,
		PingInterval:         client.pingInterval, PingTimeout: client.pingTimeout,
	})
}

func newManagedStream(subscriptions []StreamSubscription, private bool) *managedStream {
	managed := &managedStream{
		private: private, pending: make(map[int64]pendingStreamCommand),
		subs: make(map[string]StreamSubscription, len(subscriptions)),
	}
	for _, subscription := range subscriptions {
		managed.subs[streamSubscriptionKey(subscription)] = subscription
	}
	return managed
}

func (managed *managedStream) run(ctx context.Context, handler func(context.Context, StreamMessage) error) error {
	if handler == nil {
		return fmt.Errorf("Korbit stream handler is required")
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

func (managed *managedStream) resubscribe(ctx context.Context, connection corestream.Connection) error {
	managed.commandMu.Lock()
	defer managed.commandMu.Unlock()
	subscriptions := managed.snapshotSubscriptions()
	managed.stateMu.Lock()
	managed.pending = make(map[int64]pendingStreamCommand, len(subscriptions))
	items := managed.commandItemsLocked("subscribe", subscriptions)
	managed.stateMu.Unlock()
	payload, err := json.Marshal(items)
	if err != nil {
		return fmt.Errorf("encode Korbit stream subscriptions: %w", err)
	}
	return connection.Write(ctx, corestream.Message{Type: corestream.MessageText, Data: payload})
}

func (managed *managedStream) change(
	ctx context.Context,
	method string,
	subscriptions []StreamSubscription,
) error {
	if ctx == nil {
		return fmt.Errorf("Korbit stream command context is nil")
	}
	managed.commandMu.Lock()
	defer managed.commandMu.Unlock()
	for _, subscription := range subscriptions {
		key := streamSubscriptionKey(subscription)
		managed.stateMu.Lock()
		_, exists := managed.subs[key]
		if method == "subscribe" && exists || method == "unsubscribe" && !exists {
			managed.stateMu.Unlock()
			continue
		}
		if method == "subscribe" && len(managed.subs) >= maxStreamSubscriptions {
			managed.stateMu.Unlock()
			return validationError("WebSocket subscription count cannot exceed %d", maxStreamSubscriptions)
		}
		if method == "subscribe" {
			managed.subs[key] = subscription
		} else {
			delete(managed.subs, key)
		}
		items := managed.commandItemsLocked(method, []StreamSubscription{subscription})
		managed.stateMu.Unlock()
		payload, err := json.Marshal(items)
		if err != nil {
			return fmt.Errorf("encode Korbit stream command: %w", err)
		}
		if err := managed.session.Write(
			ctx, corestream.Message{Type: corestream.MessageText, Data: payload},
		); err != nil {
			return err
		}
	}
	return nil
}

func (managed *managedStream) commandItemsLocked(
	method string,
	subscriptions []StreamSubscription,
) []streamCommandItem {
	items := make([]streamCommandItem, 0, len(subscriptions))
	for _, subscription := range subscriptions {
		managed.nextID++
		managed.pending[managed.nextID] = pendingStreamCommand{method: method, subscription: subscription}
		items = append(items, streamCommandItem{
			RequestID: managed.nextID, Method: method, Type: subscription.Channel,
			Symbols: append([]string(nil), subscription.Symbols...), Level: subscription.Level,
			AccountSeqs: append([]int(nil), subscription.AccountSeqs...),
		})
	}
	return items
}

func (managed *managedStream) handleControl(message StreamMessage) {
	if message.Status == "" || message.RequestID == nil {
		return
	}
	managed.stateMu.Lock()
	pending, exists := managed.pending[*message.RequestID]
	delete(managed.pending, *message.RequestID)
	if exists && message.Status == "fail" && pending.method == "subscribe" {
		delete(managed.subs, streamSubscriptionKey(pending.subscription))
	}
	managed.stateMu.Unlock()
}

func (managed *managedStream) snapshotSubscriptions() []StreamSubscription {
	managed.stateMu.Lock()
	defer managed.stateMu.Unlock()
	keys := make([]string, 0, len(managed.subs))
	for key := range managed.subs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]StreamSubscription, 0, len(keys))
	for _, key := range keys {
		result = append(result, managed.subs[key])
	}
	return result
}

type streamCommandItem struct {
	RequestID   int64         `json:"requestId"`
	Method      string        `json:"method"`
	Type        StreamChannel `json:"type"`
	Symbols     []string      `json:"symbols,omitempty"`
	Level       string        `json:"level,omitempty"`
	AccountSeqs []int         `json:"accountSeqs,omitempty"`
}

func (client *StreamClient) privateStreamDialRequest(ctx context.Context) (corestream.DialRequest, error) {
	material, err := client.credentialProvider.Resolve(ctx, client.credentials.SecretRef)
	defer material.Destroy()
	if err != nil {
		return corestream.DialRequest{}, client.streamAuthenticationError(err)
	}
	if len(material.APIKey) == 0 || len(material.SecretKey) == 0 {
		return corestream.DialRequest{}, client.streamAuthenticationError(
			errors.New("Korbit API key and signing key are required"),
		)
	}
	timestamp := client.now().UnixMilli()
	if timestamp <= 0 {
		return corestream.DialRequest{}, validationError("Korbit timestamp must be after the Unix epoch")
	}
	parameters := url.Values{
		"recvWindow": {strconv.Itoa(client.receiveWindow)},
		"timestamp":  {strconv.FormatInt(timestamp, 10)},
	}
	unsigned := parameters.Encode()
	signature, err := signParameters(client.signingMode, material.SecretKey, unsigned)
	if err != nil {
		return corestream.DialRequest{}, client.streamAuthenticationError(err)
	}
	endpoint := client.privateURL + "?" + unsigned + "&signature=" + url.QueryEscape(signature)
	header := make(http.Header)
	header.Set("X-KAPI-KEY", string(material.APIKey))
	return corestream.DialRequest{
		Endpoint: endpoint, Header: header,
	}, nil
}

func (client *StreamClient) resolveStreamRoute(
	options ...trade.RequestOption,
) (transport.EgressRouteID, error) {
	resolved, err := trade.ResolveRequestOptions(client.defaultRouteID, options...)
	if err != nil {
		return "", err
	}
	if resolved.Timeout != 0 {
		return "", fmt.Errorf("Korbit stream timeout must be controlled by Run context")
	}
	return resolved.EgressRouteID, nil
}

func (client *StreamClient) streamAuthenticationError(cause error) error {
	accountID := ""
	if client.credentials != nil {
		accountID = client.credentials.AccountID
	}
	return &trade.APIError{
		Category: trade.ErrorAuthentication, Exchange: model.ExchangeKorbit,
		AccountID: accountID, Cause: cause,
	}
}

func (client *StreamClient) streamAuthorizationError(cause error) error {
	accountID := ""
	if client.credentials != nil {
		accountID = client.credentials.AccountID
	}
	return &trade.APIError{
		Category: trade.ErrorAuthorization, Exchange: model.ExchangeKorbit,
		AccountID: accountID, Cause: cause,
	}
}

func validateStreamSubscriptions(
	subscriptions []StreamSubscription,
	private, require bool,
) ([]StreamSubscription, error) {
	if require && len(subscriptions) == 0 || len(subscriptions) > maxStreamSubscriptions {
		return nil, validationError("WebSocket subscription count must be 1-%d", maxStreamSubscriptions)
	}
	seen := make(map[string]struct{}, len(subscriptions))
	result := make([]StreamSubscription, 0, len(subscriptions))
	for _, subscription := range subscriptions {
		if !subscription.Channel.valid() || subscription.Channel.private() != private {
			return nil, validationError("unsupported WebSocket channel %q for this endpoint", subscription.Channel)
		}
		if subscription.Channel == StreamChannelMyAsset {
			if len(subscription.Symbols) > 0 {
				return nil, validationError("myAsset does not accept symbols")
			}
		} else {
			if len(subscription.Symbols) == 0 || len(subscription.Symbols) > 100 {
				return nil, validationError("WebSocket channel requires 1-100 symbols")
			}
		}
		if subscription.Level != "" {
			if subscription.Channel != StreamChannelOrderBook {
				return nil, validationError("WebSocket level is only supported for orderbook")
			}
			if err := validatePositiveDecimal("WebSocket order book level", subscription.Level); err != nil {
				return nil, err
			}
		}
		if len(subscription.AccountSeqs) > 0 && !private {
			return nil, validationError("accountSeqs is only supported for private channels")
		}
		if len(subscription.AccountSeqs) > 100 {
			return nil, validationError("WebSocket accountSeqs cannot exceed 100 items")
		}
		symbols := append([]string(nil), subscription.Symbols...)
		symbolSeen := make(map[string]struct{}, len(symbols))
		for _, symbol := range symbols {
			if err := validateSymbol(symbol); err != nil {
				return nil, err
			}
			if _, exists := symbolSeen[symbol]; exists {
				return nil, validationError("duplicate WebSocket symbol %q", symbol)
			}
			symbolSeen[symbol] = struct{}{}
		}
		accountSeqs := append([]int(nil), subscription.AccountSeqs...)
		accountSeen := make(map[int]struct{}, len(accountSeqs))
		for _, accountSeq := range accountSeqs {
			if accountSeq < 1 {
				return nil, validationError("WebSocket accountSeq must be positive")
			}
			if _, exists := accountSeen[accountSeq]; exists {
				return nil, validationError("duplicate WebSocket accountSeq %d", accountSeq)
			}
			accountSeen[accountSeq] = struct{}{}
		}
		validated := StreamSubscription{
			Channel: subscription.Channel, Symbols: symbols,
			Level: subscription.Level, AccountSeqs: accountSeqs,
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

func streamSubscriptionKey(subscription StreamSubscription) string {
	var builder strings.Builder
	builder.WriteString(string(subscription.Channel))
	builder.WriteByte('|')
	builder.WriteString(strings.Join(subscription.Symbols, ","))
	builder.WriteByte('|')
	builder.WriteString(subscription.Level)
	builder.WriteByte('|')
	for index, accountSeq := range subscription.AccountSeqs {
		if index > 0 {
			builder.WriteByte(',')
		}
		builder.WriteString(strconv.Itoa(accountSeq))
	}
	return builder.String()
}

func validateWebSocketURL(raw string, allowInsecure bool) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("invalid WebSocket endpoint")
	}
	if parsed.Scheme != "wss" && !(allowInsecure && parsed.Scheme == "ws") {
		return "", fmt.Errorf("WebSocket endpoint must use WSS")
	}
	return parsed.String(), nil
}
