package cryptocom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"sync"
	"time"

	trade "github.com/proven-trade/proven-trade-sdk"
	"github.com/proven-trade/proven-trade-sdk/credential"
	"github.com/proven-trade/proven-trade-sdk/model"
	corestream "github.com/proven-trade/proven-trade-sdk/stream"
	"github.com/proven-trade/proven-trade-sdk/transport"
)

type privateCryptoComManagedStream struct {
	session *corestream.Session
	client  *StreamClient

	commandMu        sync.Mutex
	stateMu          sync.Mutex
	authenticated    bool
	authenticationID string
	subscriptions    map[string]StreamSubscription
	pending          map[string]pendingCryptoComStreamCommand
	lastCommandAt    time.Time
}

// PrivateStream은 Crypto.com 주문·체결·잔고 사용자 WebSocket 연결을 관리한다.
type PrivateStream struct {
	managed *privateCryptoComManagedStream
}

// PrivateStream은 인증 후 선택한 EIP route에 고정된 사용자 세션을 생성한다.
func (client *StreamClient) PrivateStream(
	request StreamRequest,
	options ...trade.RequestOption,
) (*PrivateStream, error) {
	subscriptions, err := validateCryptoComPrivateStreamSubscriptions(
		request.Subscriptions, true,
	)
	if err != nil {
		return nil, err
	}
	routeID, err := client.resolveStreamRoute(options...)
	if err != nil {
		return nil, err
	}
	if client.credentials == nil || client.credentialProvider == nil {
		return nil, client.streamAuthenticationError(
			errors.New("private Crypto.com stream requires credentials"),
		)
	}
	if err := client.credentials.RequireEgressRoute(routeID); err != nil {
		return nil, client.streamAuthorizationError(err)
	}
	if err := client.credentials.RequirePermission(credential.PermissionRead); err != nil {
		return nil, client.streamAuthorizationError(err)
	}
	managed := newPrivateCryptoComManagedStream(client, subscriptions)
	session, err := corestream.NewSession(corestream.SessionConfig{
		Connector: client.connector, EgressRouteID: routeID,
		Request:   corestream.DialRequest{Endpoint: client.userURL},
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

// Run은 인증·heartbeat와 private 사용자 이벤트를 순서대로 해석해 handler에 전달한다.
func (private *PrivateStream) Run(
	ctx context.Context,
	handler func(context.Context, StreamMessage) error,
) error {
	if handler == nil {
		return fmt.Errorf("Crypto.com private stream handler is required")
	}
	return private.managed.session.Run(ctx, func(ctx context.Context, message corestream.Message) error {
		decoded, err := DecodeStreamMessage(message)
		if err != nil {
			return err
		}
		if decoded.Subscription != "" && !decoded.Private {
			return fmt.Errorf("Crypto.com private stream received public market data")
		}
		if decoded.Heartbeat {
			if err := private.managed.respondHeartbeat(ctx, decoded.ID); err != nil {
				if reconnectErr := private.managed.session.Reconnect(); reconnectErr != nil {
					return err
				}
				return nil
			}
			return handler(ctx, decoded)
		}
		if decoded.Method == "public/auth" {
			if err := private.managed.handleAuthentication(ctx, decoded); err != nil {
				return err
			}
			return handler(ctx, decoded)
		}
		if err := private.managed.handleControl(decoded); err != nil {
			return err
		}
		return handler(ctx, decoded)
	})
}

// Subscribe는 private 사용자 구독을 추가하고 재연결 복구 목록에도 반영한다.
func (private *PrivateStream) Subscribe(
	ctx context.Context,
	subscriptions ...StreamSubscription,
) error {
	validated, err := validateCryptoComPrivateStreamSubscriptions(subscriptions, true)
	if err != nil {
		return err
	}
	return private.managed.change(ctx, "subscribe", validated)
}

// Unsubscribe는 private 사용자 구독을 제거하고 재연결 복구 목록에도 반영한다.
func (private *PrivateStream) Unsubscribe(
	ctx context.Context,
	subscriptions ...StreamSubscription,
) error {
	validated, err := validateCryptoComPrivateStreamSubscriptions(subscriptions, true)
	if err != nil {
		return err
	}
	return private.managed.change(ctx, "unsubscribe", validated)
}

// Close는 private 사용자 stream 세션을 종료한다.
func (private *PrivateStream) Close() error {
	return private.managed.session.Close()
}

// Generation은 성공한 private 사용자 연결 세대 번호를 반환한다.
func (private *PrivateStream) Generation() uint64 {
	return private.managed.session.Generation()
}

// EgressRouteID는 private 최초 연결과 모든 재연결에 고정된 송신 경로를 반환한다.
func (private *PrivateStream) EgressRouteID() transport.EgressRouteID {
	return private.managed.session.EgressRouteID()
}

func newPrivateCryptoComManagedStream(
	client *StreamClient,
	subscriptions []StreamSubscription,
) *privateCryptoComManagedStream {
	managed := &privateCryptoComManagedStream{
		client: client, subscriptions: make(map[string]StreamSubscription, len(subscriptions)),
		pending: make(map[string]pendingCryptoComStreamCommand),
	}
	for _, subscription := range subscriptions {
		managed.subscriptions[cryptoComPrivateStreamSubscriptionKey(subscription)] = subscription
	}
	return managed
}

func (managed *privateCryptoComManagedStream) authenticate(
	ctx context.Context,
	connection corestream.Connection,
) error {
	managed.commandMu.Lock()
	defer managed.commandMu.Unlock()
	managed.stateMu.Lock()
	managed.authenticated = false
	managed.authenticationID = ""
	managed.pending = make(map[string]pendingCryptoComStreamCommand)
	managed.stateMu.Unlock()
	managed.lastCommandAt = time.Time{}
	if err := waitCryptoComStreamDuration(ctx, managed.client.connectionReadyDelay); err != nil {
		return err
	}
	payload, id, err := managed.client.encodeUserStreamAuthentication(ctx)
	if err != nil {
		return err
	}
	defer zeroBytes(payload)
	if err := managed.writeConnection(ctx, connection, payload); err != nil {
		return err
	}
	managed.stateMu.Lock()
	managed.authenticationID = id
	managed.stateMu.Unlock()
	return nil
}

func (managed *privateCryptoComManagedStream) handleAuthentication(
	ctx context.Context,
	message StreamMessage,
) error {
	managed.stateMu.Lock()
	authenticationID := managed.authenticationID
	managed.stateMu.Unlock()
	if authenticationID == "" || message.ID != authenticationID {
		return fmt.Errorf("unexpected Crypto.com private stream authentication response ID %q", message.ID)
	}
	if message.Error != nil || message.Code != "0" {
		messageText := ""
		if message.Error != nil {
			messageText = message.Error.Message
		}
		return managed.client.streamAuthenticationError(fmt.Errorf(
			"Crypto.com private stream authentication failed with code %s: %s",
			message.Code, messageText,
		))
	}
	if err := managed.activate(ctx); err != nil {
		if reconnectErr := managed.session.Reconnect(); reconnectErr != nil {
			return err
		}
	}
	return nil
}

func (managed *privateCryptoComManagedStream) activate(ctx context.Context) error {
	managed.commandMu.Lock()
	defer managed.commandMu.Unlock()
	subscriptions := managed.snapshotSubscriptions()
	managed.stateMu.Lock()
	managed.authenticated = true
	managed.authenticationID = ""
	managed.pending = make(map[string]pendingCryptoComStreamCommand, len(subscriptions))
	managed.stateMu.Unlock()
	for _, subscription := range subscriptions {
		if err := managed.sendCommand(ctx, "subscribe", subscription, false); err != nil {
			managed.deactivate()
			return err
		}
	}
	return nil
}

func (managed *privateCryptoComManagedStream) deactivate() {
	managed.stateMu.Lock()
	defer managed.stateMu.Unlock()
	managed.authenticated = false
	managed.authenticationID = ""
	managed.pending = make(map[string]pendingCryptoComStreamCommand)
}

func (managed *privateCryptoComManagedStream) change(
	ctx context.Context,
	operation string,
	subscriptions []StreamSubscription,
) error {
	if ctx == nil {
		return fmt.Errorf("Crypto.com private stream command context is nil")
	}
	managed.commandMu.Lock()
	defer managed.commandMu.Unlock()
	for _, subscription := range subscriptions {
		key := cryptoComPrivateStreamSubscriptionKey(subscription)
		managed.stateMu.Lock()
		_, exists := managed.subscriptions[key]
		count := len(managed.subscriptions)
		authenticated := managed.authenticated
		managed.stateMu.Unlock()
		if operation == "subscribe" && exists || operation == "unsubscribe" && !exists {
			continue
		}
		if operation == "subscribe" && count >= maximumCryptoComStreamSubscriptions {
			return validationError(
				"WebSocket subscription count cannot exceed %d",
				maximumCryptoComStreamSubscriptions,
			)
		}
		managed.stateMu.Lock()
		if operation == "subscribe" {
			managed.subscriptions[key] = subscription
		} else {
			delete(managed.subscriptions, key)
		}
		managed.stateMu.Unlock()
		if !authenticated {
			continue
		}
		if err := managed.sendCommand(ctx, operation, subscription, true); err != nil {
			return err
		}
	}
	return nil
}

func (managed *privateCryptoComManagedStream) sendCommand(
	ctx context.Context,
	operation string,
	subscription StreamSubscription,
	rollbackOnFailure bool,
) error {
	if err := managed.waitCommandSlot(ctx); err != nil {
		return err
	}
	payload, id, err := managed.client.encodeUserStreamCommand(operation, subscription)
	if err != nil {
		return err
	}
	managed.stateMu.Lock()
	managed.pending[id] = pendingCryptoComStreamCommand{
		operation: operation, subscription: subscription,
	}
	managed.stateMu.Unlock()
	if err := managed.session.Write(
		ctx, corestream.Message{Type: corestream.MessageText, Data: payload},
	); err != nil {
		if rollbackOnFailure {
			managed.rollbackCommand(id)
		} else {
			managed.removePending(id)
		}
		return err
	}
	managed.lastCommandAt = time.Now()
	return nil
}

func (managed *privateCryptoComManagedStream) respondHeartbeat(
	ctx context.Context,
	id string,
) error {
	payload, err := json.Marshal(struct {
		ID     string `json:"id"`
		Method string `json:"method"`
	}{ID: id, Method: "public/respond-heartbeat"})
	if err != nil {
		return fmt.Errorf("encode Crypto.com private heartbeat response: %w", err)
	}
	managed.commandMu.Lock()
	defer managed.commandMu.Unlock()
	if err := managed.waitCommandSlot(ctx); err != nil {
		return err
	}
	err = managed.session.Write(
		ctx, corestream.Message{Type: corestream.MessageText, Data: payload},
	)
	managed.lastCommandAt = time.Now()
	return err
}

func (managed *privateCryptoComManagedStream) writeConnection(
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

func (managed *privateCryptoComManagedStream) waitCommandSlot(ctx context.Context) error {
	delay := time.Until(managed.lastCommandAt.Add(managed.client.userCommandInterval))
	if delay <= 0 {
		return nil
	}
	return waitCryptoComStreamDuration(ctx, delay)
}

func (managed *privateCryptoComManagedStream) handleControl(message StreamMessage) error {
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
			"Crypto.com private stream response method %q does not match %q",
			message.Method, pending.operation,
		)
	}
	if message.Error != nil {
		managed.rollbackCommandLocked(pending)
	}
	return nil
}

func (managed *privateCryptoComManagedStream) rollbackCommand(id string) {
	managed.stateMu.Lock()
	defer managed.stateMu.Unlock()
	pending, exists := managed.pending[id]
	if !exists {
		return
	}
	delete(managed.pending, id)
	managed.rollbackCommandLocked(pending)
}

func (managed *privateCryptoComManagedStream) removePending(id string) {
	managed.stateMu.Lock()
	defer managed.stateMu.Unlock()
	delete(managed.pending, id)
}

func (managed *privateCryptoComManagedStream) rollbackCommandLocked(
	pending pendingCryptoComStreamCommand,
) {
	key := cryptoComPrivateStreamSubscriptionKey(pending.subscription)
	if pending.operation == "subscribe" {
		delete(managed.subscriptions, key)
	} else {
		managed.subscriptions[key] = pending.subscription
	}
}

func (managed *privateCryptoComManagedStream) snapshotSubscriptions() []StreamSubscription {
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

func (client *StreamClient) encodeUserStreamAuthentication(
	ctx context.Context,
) ([]byte, string, error) {
	material, err := client.credentialProvider.Resolve(ctx, client.credentials.SecretRef)
	defer material.Destroy()
	if err != nil {
		return nil, "", client.streamAuthenticationError(err)
	}
	if len(material.APIKey) == 0 || len(material.SecretKey) == 0 {
		return nil, "", client.streamAuthenticationError(
			errors.New("Crypto.com API key and HMAC secret are required"),
		)
	}
	id, nonce, err := client.nextUserStreamIdentity()
	if err != nil {
		return nil, "", err
	}
	signature, err := Sign(
		"public/auth", id, material.APIKey, map[string]any{}, nonce, material.SecretKey,
	)
	if err != nil {
		return nil, "", client.streamInternalError(err)
	}
	payload, err := json.Marshal(struct {
		ID     string `json:"id"`
		Method string `json:"method"`
		APIKey string `json:"api_key"`
		Sig    string `json:"sig"`
		Nonce  string `json:"nonce"`
	}{
		ID: id, Method: "public/auth", APIKey: string(material.APIKey),
		Sig: signature, Nonce: nonce,
	})
	if err != nil {
		return nil, "", fmt.Errorf("encode Crypto.com private stream authentication: %w", err)
	}
	return payload, id, nil
}

func (client *StreamClient) encodeUserStreamCommand(
	operation string,
	subscription StreamSubscription,
) ([]byte, string, error) {
	if operation != "subscribe" && operation != "unsubscribe" {
		return nil, "", fmt.Errorf("unsupported Crypto.com private stream operation %q", operation)
	}
	id, nonce, err := client.nextUserStreamIdentity()
	if err != nil {
		return nil, "", err
	}
	payload, err := json.Marshal(struct {
		ID     string `json:"id"`
		Method string `json:"method"`
		Params struct {
			Channels []string `json:"channels"`
		} `json:"params"`
		Nonce string `json:"nonce"`
	}{
		ID: id, Method: operation,
		Params: struct {
			Channels []string `json:"channels"`
		}{Channels: []string{cryptoComPrivateStreamSubscriptionTopic(subscription)}},
		Nonce: nonce,
	})
	if err != nil {
		return nil, "", fmt.Errorf("encode Crypto.com private stream command: %w", err)
	}
	return payload, id, nil
}

func (client *StreamClient) nextUserStreamIdentity() (string, string, error) {
	idValue := client.nextID.Add(1)
	if idValue <= 0 {
		return "", "", client.streamInternalError(
			errors.New("Crypto.com stream request ID space is exhausted"),
		)
	}
	nonceValue := client.now().UTC().UnixMilli()
	if nonceValue <= 0 {
		return "", "", validationError("Crypto.com stream nonce must be after the Unix epoch")
	}
	return strconv.FormatInt(idValue, 10), strconv.FormatInt(nonceValue, 10), nil
}

func validateCryptoComPrivateStreamSubscriptions(
	subscriptions []StreamSubscription,
	requireNonEmpty bool,
) ([]StreamSubscription, error) {
	if requireNonEmpty && len(subscriptions) == 0 {
		return nil, validationError("private WebSocket subscription is required")
	}
	if len(subscriptions) > maximumCryptoComStreamSubscriptions {
		return nil, validationError(
			"WebSocket subscription count cannot exceed %d",
			maximumCryptoComStreamSubscriptions,
		)
	}
	result := make([]StreamSubscription, 0, len(subscriptions))
	seen := make(map[string]struct{}, len(subscriptions))
	for _, subscription := range subscriptions {
		if err := validateCryptoComPrivateStreamSubscription(subscription); err != nil {
			return nil, err
		}
		key := cryptoComPrivateStreamSubscriptionKey(subscription)
		if _, exists := seen[key]; exists {
			return nil, validationError("duplicate private WebSocket subscription")
		}
		seen[key] = struct{}{}
		result = append(result, subscription)
	}
	return result, nil
}

func validateCryptoComPrivateStreamSubscription(subscription StreamSubscription) error {
	if subscription.CandleTimeframe != "" || subscription.BookDepth != 0 ||
		subscription.BookSubscriptionType != "" || subscription.BookUpdateFrequency != "" {
		return validationError("private WebSocket subscription does not accept public settings")
	}
	switch subscription.Channel {
	case StreamChannelUserOrders, StreamChannelUserTrades:
		if subscription.InstrumentName != "" {
			if err := validateInstrumentName(subscription.InstrumentName); err != nil {
				return err
			}
		}
	case StreamChannelUserBalances:
		if subscription.InstrumentName != "" {
			return validationError("balance WebSocket subscription does not accept instrument")
		}
	default:
		return validationError("unsupported private WebSocket channel %q", subscription.Channel)
	}
	return nil
}

func cryptoComPrivateStreamSubscriptionTopic(subscription StreamSubscription) string {
	if subscription.InstrumentName == "" {
		return string(subscription.Channel)
	}
	return string(subscription.Channel) + "." + subscription.InstrumentName
}

func cryptoComPrivateStreamSubscriptionKey(subscription StreamSubscription) string {
	return cryptoComPrivateStreamSubscriptionTopic(subscription)
}

func (client *StreamClient) streamAuthenticationError(cause error) error {
	accountID := ""
	if client.credentials != nil {
		accountID = client.credentials.AccountID
	}
	return &trade.APIError{
		Category: trade.ErrorAuthentication, Exchange: model.ExchangeCryptoCom,
		AccountID: accountID, Cause: cause,
	}
}

func (client *StreamClient) streamAuthorizationError(cause error) error {
	accountID := ""
	if client.credentials != nil {
		accountID = client.credentials.AccountID
	}
	return &trade.APIError{
		Category: trade.ErrorAuthorization, Exchange: model.ExchangeCryptoCom,
		AccountID: accountID, Cause: cause,
	}
}

func (client *StreamClient) streamInternalError(cause error) error {
	accountID := ""
	if client.credentials != nil {
		accountID = client.credentials.AccountID
	}
	return &trade.APIError{
		Category: trade.ErrorInternal, Exchange: model.ExchangeCryptoCom,
		AccountID: accountID, Cause: cause,
	}
}
