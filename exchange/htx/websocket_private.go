package htx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"sync"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	"github.com/proven-trade/cex-sdk/model"
	corestream "github.com/proven-trade/cex-sdk/stream"
	"github.com/proven-trade/cex-sdk/transport"
)

const (
	privateStreamRequestLimit  = 50
	privateStreamRequestWindow = time.Second
)

type pendingPrivateStreamCommand struct {
	operation    string
	subscription StreamSubscription
}

type privateStreamAuthenticationParams struct {
	AuthType         string `json:"authType"`
	AccessKey        string `json:"accessKey"`
	SignatureMethod  string `json:"signatureMethod"`
	SignatureVersion string `json:"signatureVersion"`
	Timestamp        string `json:"timestamp"`
	Signature        string `json:"signature"`
}

type privateStreamAuthenticationRequest struct {
	Action  string                            `json:"action"`
	Channel string                            `json:"ch"`
	Params  privateStreamAuthenticationParams `json:"params"`
}

type privateManagedStream struct {
	session *corestream.Session
	client  *StreamClient

	commandMu     sync.Mutex
	stateMu       sync.Mutex
	authenticated bool
	subscriptions map[string]StreamSubscription
	pending       map[string]pendingPrivateStreamCommand
	requestTimes  []time.Time
}

// PrivateStream은 HTX v2 주문·체결·계정 WebSocket 연결을 관리한다.
type PrivateStream struct{ managed *privateManagedStream }

// PrivateStream은 인증 후 선택한 송신 경로에 고정된 private 세션을 생성한다.
func (client *StreamClient) PrivateStream(
	request StreamRequest,
	options ...trade.RequestOption,
) (*PrivateStream, error) {
	subscriptions, err := validatePrivateStreamSubscriptions(request.Subscriptions, true)
	if err != nil {
		return nil, err
	}
	routeID, err := client.resolveStreamRoute(options...)
	if err != nil {
		return nil, err
	}
	if client.credentials == nil || client.credentialProvider == nil {
		return nil, client.streamAuthenticationError(
			errors.New("private HTX stream requires credentials"),
		)
	}
	if err := client.credentials.RequireEgressRoute(routeID); err != nil {
		return nil, client.streamAuthorizationError(err)
	}
	if err := client.credentials.RequirePermission(credential.PermissionRead); err != nil {
		return nil, client.streamAuthorizationError(err)
	}
	managed := newPrivateManagedStream(client, subscriptions)
	session, err := corestream.NewSession(corestream.SessionConfig{
		Connector: client.connector, EgressRouteID: routeID,
		Request:   corestream.DialRequest{Endpoint: client.privateURL},
		OnConnect: managed.authenticate, Observer: client.observer,
		ReconnectPolicy: client.streamReconnectPolicy, Backoff: client.backoff,
		MaxReconnectAttempts: client.maxReconnectAttempts,
	})
	if err != nil {
		return nil, err
	}
	managed.session = session
	return &PrivateStream{managed: managed}, nil
}

// Run은 v2 인증·heartbeat와 private 이벤트를 순서대로 decode해 handler에 전달한다.
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
	validated, err := validatePrivateStreamSubscriptions(subscriptions, true)
	if err != nil {
		return err
	}
	return private.managed.change(ctx, "sub", validated)
}

// Unsubscribe는 private 구독을 제거하고 재연결 복구 목록에도 반영한다.
func (private *PrivateStream) Unsubscribe(
	ctx context.Context,
	subscriptions ...StreamSubscription,
) error {
	validated, err := validatePrivateStreamSubscriptions(subscriptions, true)
	if err != nil {
		return err
	}
	return private.managed.change(ctx, "unsub", validated)
}

// Close는 private stream 세션을 종료한다.
func (private *PrivateStream) Close() error { return private.managed.session.Close() }

// Generation은 성공한 private 연결 세대 번호를 반환한다.
func (private *PrivateStream) Generation() uint64 { return private.managed.session.Generation() }

// EgressRouteID는 private 최초 연결과 모든 재연결에 고정된 송신 경로를 반환한다.
func (private *PrivateStream) EgressRouteID() transport.EgressRouteID {
	return private.managed.session.EgressRouteID()
}

func newPrivateManagedStream(
	client *StreamClient,
	subscriptions []StreamSubscription,
) *privateManagedStream {
	managed := &privateManagedStream{
		client: client, subscriptions: make(map[string]StreamSubscription, len(subscriptions)),
		pending: make(map[string]pendingPrivateStreamCommand),
	}
	for _, subscription := range subscriptions {
		managed.subscriptions[streamSubscriptionKey(subscription)] = subscription
	}
	return managed
}

func (managed *privateManagedStream) run(
	ctx context.Context,
	handler func(context.Context, StreamMessage) error,
) error {
	if handler == nil {
		return fmt.Errorf("HTX private stream handler is required")
	}
	return managed.session.Run(ctx, func(ctx context.Context, message corestream.Message) error {
		decoded, err := DecodePrivateStreamMessage(message)
		if err != nil {
			return err
		}
		if decoded.Ping != nil {
			payload, err := json.Marshal(struct {
				Action string `json:"action"`
				Data   struct {
					Timestamp int64 `json:"ts"`
				} `json:"data"`
			}{Action: "pong", Data: struct {
				Timestamp int64 `json:"ts"`
			}{Timestamp: *decoded.Ping}})
			if err != nil {
				return fmt.Errorf("encode HTX private stream pong: %w", err)
			}
			if err := managed.session.Write(
				ctx, corestream.Message{Type: corestream.MessageText, Data: payload},
			); err != nil {
				if reconnectErr := managed.session.Reconnect(); reconnectErr != nil {
					return err
				}
				return nil
			}
			return handler(ctx, decoded)
		}
		if decoded.Topic == "auth" {
			if decoded.Error != nil || decoded.Code != 200 {
				return managed.client.streamAuthenticationError(fmt.Errorf(
					"HTX private stream authentication failed with code %d: %s",
					decoded.Code, decoded.Message,
				))
			}
			if err := managed.activate(ctx); err != nil {
				if reconnectErr := managed.session.Reconnect(); reconnectErr != nil {
					return err
				}
				return nil
			}
			return handler(ctx, decoded)
		}
		managed.handleControl(decoded)
		return handler(ctx, decoded)
	})
}

func (managed *privateManagedStream) authenticate(
	ctx context.Context,
	connection corestream.Connection,
) error {
	managed.commandMu.Lock()
	defer managed.commandMu.Unlock()
	managed.stateMu.Lock()
	managed.authenticated = false
	managed.pending = make(map[string]pendingPrivateStreamCommand)
	managed.stateMu.Unlock()
	if err := managed.waitForRequest(ctx); err != nil {
		return err
	}
	payload, err := managed.client.encodePrivateAuthentication(ctx)
	if err != nil {
		return err
	}
	return connection.Write(
		ctx, corestream.Message{Type: corestream.MessageText, Data: payload},
	)
}

func (managed *privateManagedStream) activate(ctx context.Context) error {
	managed.commandMu.Lock()
	defer managed.commandMu.Unlock()
	subscriptions := managed.snapshotSubscriptions()
	managed.stateMu.Lock()
	managed.authenticated = true
	managed.pending = make(map[string]pendingPrivateStreamCommand, len(subscriptions))
	managed.stateMu.Unlock()
	for _, subscription := range subscriptions {
		if err := managed.waitForRequest(ctx); err != nil {
			managed.deactivate()
			return err
		}
		payload, err := encodePrivateStreamCommand("sub", subscription)
		if err != nil {
			managed.deactivate()
			return err
		}
		pendingKey := privateStreamPendingKey("sub", subscription)
		managed.stateMu.Lock()
		managed.pending[pendingKey] = pendingPrivateStreamCommand{
			operation: "sub", subscription: subscription,
		}
		managed.stateMu.Unlock()
		if err := managed.session.Write(
			ctx, corestream.Message{Type: corestream.MessageText, Data: payload},
		); err != nil {
			managed.deactivate()
			return err
		}
	}
	return nil
}

func (managed *privateManagedStream) deactivate() {
	managed.stateMu.Lock()
	defer managed.stateMu.Unlock()
	managed.authenticated = false
	managed.pending = make(map[string]pendingPrivateStreamCommand)
}

func (managed *privateManagedStream) change(
	ctx context.Context,
	operation string,
	subscriptions []StreamSubscription,
) error {
	if ctx == nil {
		return fmt.Errorf("HTX private stream command context is nil")
	}
	managed.commandMu.Lock()
	defer managed.commandMu.Unlock()
	for _, subscription := range subscriptions {
		key := streamSubscriptionKey(subscription)
		managed.stateMu.Lock()
		_, exists := managed.subscriptions[key]
		count := len(managed.subscriptions)
		authenticated := managed.authenticated
		managed.stateMu.Unlock()
		if operation == "sub" && exists || operation == "unsub" && !exists {
			continue
		}
		if operation == "sub" && count >= maximumStreamSubscriptions {
			return validationError(
				"WebSocket subscription count cannot exceed %d", maximumStreamSubscriptions,
			)
		}
		if authenticated {
			if err := managed.waitForRequest(ctx); err != nil {
				return err
			}
		}
		managed.stateMu.Lock()
		if operation == "sub" {
			managed.subscriptions[key] = subscription
		} else {
			delete(managed.subscriptions, key)
		}
		managed.stateMu.Unlock()
		if !authenticated {
			continue
		}
		payload, err := encodePrivateStreamCommand(operation, subscription)
		if err != nil {
			managed.rollbackDesired(operation, subscription)
			return err
		}
		pendingKey := privateStreamPendingKey(operation, subscription)
		managed.stateMu.Lock()
		managed.pending[pendingKey] = pendingPrivateStreamCommand{
			operation: operation, subscription: subscription,
		}
		managed.stateMu.Unlock()
		if err := managed.session.Write(
			ctx, corestream.Message{Type: corestream.MessageText, Data: payload},
		); err != nil {
			managed.rollbackCommand(pendingKey)
			return err
		}
	}
	return nil
}

func (managed *privateManagedStream) waitForRequest(ctx context.Context) error {
	for {
		now := time.Now()
		cutoff := now.Add(-privateStreamRequestWindow)
		first := 0
		for first < len(managed.requestTimes) && !managed.requestTimes[first].After(cutoff) {
			first++
		}
		if first > 0 {
			managed.requestTimes = append([]time.Time(nil), managed.requestTimes[first:]...)
		}
		if len(managed.requestTimes) < privateStreamRequestLimit {
			managed.requestTimes = append(managed.requestTimes, now)
			return nil
		}
		delay := time.Until(managed.requestTimes[0].Add(privateStreamRequestWindow))
		if delay < 0 {
			delay = 0
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (managed *privateManagedStream) handleControl(message StreamMessage) {
	if message.Action != "sub" && message.Action != "unsub" {
		return
	}
	pendingKey := message.Action + "\x00" + message.Topic
	managed.stateMu.Lock()
	defer managed.stateMu.Unlock()
	pending, exists := managed.pending[pendingKey]
	if !exists {
		return
	}
	delete(managed.pending, pendingKey)
	if message.Error == nil && message.Code == 200 {
		return
	}
	managed.rollbackCommandLocked(pending)
}

func (managed *privateManagedStream) rollbackCommand(pendingKey string) {
	managed.stateMu.Lock()
	defer managed.stateMu.Unlock()
	pending, exists := managed.pending[pendingKey]
	if !exists {
		return
	}
	delete(managed.pending, pendingKey)
	managed.rollbackCommandLocked(pending)
}

func (managed *privateManagedStream) rollbackDesired(
	operation string,
	subscription StreamSubscription,
) {
	managed.stateMu.Lock()
	defer managed.stateMu.Unlock()
	managed.rollbackCommandLocked(pendingPrivateStreamCommand{
		operation: operation, subscription: subscription,
	})
}

func (managed *privateManagedStream) rollbackCommandLocked(pending pendingPrivateStreamCommand) {
	key := streamSubscriptionKey(pending.subscription)
	if pending.operation == "sub" {
		delete(managed.subscriptions, key)
	} else {
		managed.subscriptions[key] = pending.subscription
	}
}

func (managed *privateManagedStream) snapshotSubscriptions() []StreamSubscription {
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

func (client *StreamClient) encodePrivateAuthentication(ctx context.Context) ([]byte, error) {
	material, err := client.credentialProvider.Resolve(ctx, client.credentials.SecretRef)
	defer material.Destroy()
	if err != nil {
		return nil, client.streamAuthenticationError(err)
	}
	if len(material.APIKey) == 0 || len(material.SecretKey) == 0 {
		return nil, client.streamAuthenticationError(
			errors.New("HTX private stream access key and HMAC secret are required"),
		)
	}
	now := client.now().UTC()
	if now.Unix() <= 0 {
		return nil, validationError("HTX private stream timestamp must be after the Unix epoch")
	}
	timestamp := now.Format("2006-01-02T15:04:05")
	values := url.Values{
		"accessKey":        {string(material.APIKey)},
		"signatureMethod":  {"HmacSHA256"},
		"signatureVersion": {"2.1"},
		"timestamp":        {timestamp},
	}
	endpoint, err := url.Parse(client.privateURL)
	if err != nil {
		return nil, fmt.Errorf("parse HTX private stream URL: %w", err)
	}
	signature, err := SignHMACSHA256Base64(
		material.SecretKey,
		SignaturePayload(http.MethodGet, endpoint.Host, endpoint.EscapedPath(), canonicalQuery(values)),
	)
	if err != nil {
		return nil, client.streamAuthenticationError(err)
	}
	payload, err := json.Marshal(privateStreamAuthenticationRequest{
		Action: "req", Channel: "auth",
		Params: privateStreamAuthenticationParams{
			AuthType: "api", AccessKey: string(material.APIKey),
			SignatureMethod: "HmacSHA256", SignatureVersion: "2.1",
			Timestamp: timestamp, Signature: signature,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("encode HTX private stream authentication: %w", err)
	}
	return payload, nil
}

func encodePrivateStreamCommand(
	operation string,
	subscription StreamSubscription,
) ([]byte, error) {
	if operation != "sub" && operation != "unsub" {
		return nil, fmt.Errorf("unsupported HTX private stream operation %q", operation)
	}
	payload, err := json.Marshal(struct {
		Action  string `json:"action"`
		Channel string `json:"ch"`
	}{Action: operation, Channel: privateStreamSubscriptionTopic(subscription)})
	if err != nil {
		return nil, fmt.Errorf("encode HTX private stream command: %w", err)
	}
	return payload, nil
}

func validatePrivateStreamSubscriptions(
	subscriptions []StreamSubscription,
	requireNonEmpty bool,
) ([]StreamSubscription, error) {
	if requireNonEmpty && len(subscriptions) == 0 {
		return nil, validationError("private WebSocket subscription is required")
	}
	if len(subscriptions) > maximumStreamSubscriptions {
		return nil, validationError(
			"WebSocket subscription count cannot exceed %d", maximumStreamSubscriptions,
		)
	}
	result := make([]StreamSubscription, 0, len(subscriptions))
	seen := make(map[string]struct{}, len(subscriptions))
	for _, subscription := range subscriptions {
		if err := validatePrivateStreamSubscription(subscription); err != nil {
			return nil, err
		}
		key := streamSubscriptionKey(subscription)
		if _, exists := seen[key]; exists {
			return nil, validationError("duplicate private WebSocket subscription")
		}
		seen[key] = struct{}{}
		result = append(result, subscription)
	}
	return result, nil
}

func validatePrivateStreamSubscription(subscription StreamSubscription) error {
	if subscription.DepthType != "" || subscription.CandleInterval != "" ||
		subscription.MBPDepth != 0 {
		return validationError("private WebSocket subscription does not accept public interval")
	}
	switch subscription.Channel {
	case StreamChannelOrders:
		if !privateStreamSymbolValid(subscription.Symbol) {
			return validationError("invalid HTX private stream symbol %q", subscription.Symbol)
		}
		if subscription.Mode != 0 {
			return validationError("order WebSocket subscription does not accept mode")
		}
	case StreamChannelClearing:
		if !privateStreamSymbolValid(subscription.Symbol) {
			return validationError("invalid HTX private stream symbol %q", subscription.Symbol)
		}
		if subscription.Mode < 0 || subscription.Mode > 1 {
			return validationError("unsupported clearing WebSocket mode %d", subscription.Mode)
		}
	case StreamChannelAccounts:
		if subscription.Symbol != "" {
			return validationError("account WebSocket subscription does not accept symbol")
		}
		if subscription.Mode < 0 || subscription.Mode > 2 {
			return validationError("unsupported account WebSocket mode %d", subscription.Mode)
		}
	default:
		return validationError("unsupported private WebSocket channel %q", subscription.Channel)
	}
	return nil
}

func privateStreamSubscriptionTopic(subscription StreamSubscription) string {
	switch subscription.Channel {
	case StreamChannelOrders:
		return "orders#" + subscription.Symbol
	case StreamChannelClearing:
		return "trade.clearing#" + subscription.Symbol + "#" + strconv.Itoa(int(subscription.Mode))
	case StreamChannelAccounts:
		return "accounts.update#" + strconv.Itoa(int(subscription.Mode))
	default:
		return ""
	}
}

func privateStreamPendingKey(operation string, subscription StreamSubscription) string {
	return operation + "\x00" + privateStreamSubscriptionTopic(subscription)
}

func (client *StreamClient) streamAuthenticationError(cause error) error {
	accountID := ""
	if client.credentials != nil {
		accountID = client.credentials.AccountID
	}
	return &trade.APIError{
		Category: trade.ErrorAuthentication, Exchange: model.ExchangeHTX,
		AccountID: accountID, Cause: cause,
	}
}

func (client *StreamClient) streamAuthorizationError(cause error) error {
	accountID := ""
	if client.credentials != nil {
		accountID = client.credentials.AccountID
	}
	return &trade.APIError{
		Category: trade.ErrorAuthorization, Exchange: model.ExchangeHTX,
		AccountID: accountID, Cause: cause,
	}
}
