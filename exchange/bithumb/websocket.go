package bithumb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	"github.com/proven-trade/cex-sdk/model"
	corestream "github.com/proven-trade/cex-sdk/stream"
	"github.com/proven-trade/cex-sdk/transport"
)

const (
	DefaultPublicWebSocketURL  = "wss://ws-api.bithumb.com/websocket/v1"
	DefaultPrivateWebSocketURL = "wss://ws-api.bithumb.com/websocket/v2/private"
	defaultStreamPingInterval  = 30 * time.Second
	defaultStreamPingTimeout   = 10 * time.Second
	maxStreamDataTypes         = 1000
)

// StreamClientConfig는 빗썸 public v1과 private v2 WebSocket 설정이다.
type StreamClientConfig struct {
	Connector              corestream.Connector
	Credentials            *credential.Descriptor
	CredentialProvider     credential.Provider
	DefaultEgressRouteID   transport.EgressRouteID
	PublicWebSocketURL     string
	PrivateWebSocketURL    string
	AllowInsecureWebSocket bool
	NonceSource            NonceSource
	TicketSource           NonceSource
	Now                    func() time.Time
	Observer               corestream.StateObserver
	ReconnectPolicy        corestream.ReconnectPolicy
	Backoff                corestream.Backoff
	MaxReconnectAttempts   int
	PingInterval           time.Duration
	PingTimeout            time.Duration
}

// StreamClient는 빗썸 public/private WebSocket 세션을 생성한다.
type StreamClient struct {
	connector            corestream.Connector
	credentials          *credential.Descriptor
	credentialProvider   credential.Provider
	defaultRouteID       transport.EgressRouteID
	publicURL            string
	privateURL           string
	nonceSource          NonceSource
	ticketSource         NonceSource
	now                  func() time.Time
	observer             corestream.StateObserver
	reconnectPolicy      corestream.ReconnectPolicy
	backoff              corestream.Backoff
	maxReconnectAttempts int
	pingInterval         time.Duration
	pingTimeout          time.Duration
}

// NewStreamClient는 빗썸 WebSocket 클라이언트를 생성한다.
func NewStreamClient(config StreamClientConfig) (*StreamClient, error) {
	if config.Connector == nil {
		return nil, fmt.Errorf("Bithumb stream connector is required")
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
		return nil, fmt.Errorf("invalid Bithumb public WebSocket URL: %w", err)
	}
	privateURL, err := validateWebSocketURL(config.PrivateWebSocketURL, config.AllowInsecureWebSocket)
	if err != nil {
		return nil, fmt.Errorf("invalid Bithumb private WebSocket URL: %w", err)
	}
	if config.NonceSource == nil {
		config.NonceSource = randomNonce
	}
	if config.TicketSource == nil {
		config.TicketSource = randomNonce
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
		return nil, fmt.Errorf("Bithumb stream durations or reconnect attempts are invalid")
	}

	var credentialsCopy *credential.Descriptor
	if config.Credentials != nil {
		if err := config.Credentials.Validate(); err != nil {
			return nil, err
		}
		if config.Credentials.Exchange != model.ExchangeBithumb {
			return nil, fmt.Errorf("credential exchange must be Bithumb")
		}
		if config.CredentialProvider == nil {
			return nil, fmt.Errorf("credential provider is required for private Bithumb streams")
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
		nonceSource: config.NonceSource, ticketSource: config.TicketSource, now: config.Now,
		observer: config.Observer, reconnectPolicy: config.ReconnectPolicy, backoff: config.Backoff,
		maxReconnectAttempts: config.MaxReconnectAttempts,
		pingInterval:         config.PingInterval, pingTimeout: config.PingTimeout,
	}, nil
}

type managedStream struct {
	session      *corestream.Session
	request      StreamRequest
	ticketSource NonceSource
}

// PublicStream은 빗썸 public v1 시세 WebSocket 연결을 관리한다.
type PublicStream struct{ managed *managedStream }

// PublicStream은 선택한 송신 경로에 고정된 public v1 시세 세션을 생성한다.
func (client *StreamClient) PublicStream(
	request StreamRequest,
	options ...trade.RequestOption,
) (*PublicStream, error) {
	validated, err := validateStreamRequest(request, false)
	if err != nil {
		return nil, err
	}
	routeID, err := client.resolveRoute(options...)
	if err != nil {
		return nil, err
	}
	managed := &managedStream{request: validated, ticketSource: client.ticketSource}
	session, err := client.newSession(
		routeID, corestream.DialRequest{Endpoint: client.publicURL}, nil, managed.subscribe,
	)
	if err != nil {
		return nil, err
	}
	managed.session = session
	return &PublicStream{managed: managed}, nil
}

// Run은 public v1 시세 이벤트를 순서대로 decode해 handler에 전달한다.
func (public *PublicStream) Run(ctx context.Context, handler func(context.Context, StreamMessage) error) error {
	return public.managed.run(ctx, handler)
}

// Close는 public stream 세션을 종료한다.
func (public *PublicStream) Close() error { return public.managed.session.Close() }

// Generation은 성공한 public 연결 세대 번호를 반환한다.
func (public *PublicStream) Generation() uint64 { return public.managed.session.Generation() }

// EgressRouteID는 public stream 연결과 재연결에 고정된 송신 경로를 반환한다.
func (public *PublicStream) EgressRouteID() transport.EgressRouteID {
	return public.managed.session.EgressRouteID()
}

func (public *PublicStream) hasOrderBookSubscription(market string) bool {
	for _, dataType := range public.managed.request.Types {
		if dataType.Type != "orderbook" {
			continue
		}
		for _, code := range dataType.Codes {
			if strings.EqualFold(code, market) {
				return true
			}
		}
	}
	return false
}

// PrivateStream은 빗썸 private v2 내 주문·자산 WebSocket 연결을 관리한다.
type PrivateStream struct{ managed *managedStream }

// PrivateStream은 JWT handshake 후 private v2 이벤트를 구독하는 세션을 생성한다.
func (client *StreamClient) PrivateStream(
	request StreamRequest,
	options ...trade.RequestOption,
) (*PrivateStream, error) {
	validated, err := validateStreamRequest(request, true)
	if err != nil {
		return nil, err
	}
	routeID, err := client.resolveRoute(options...)
	if err != nil {
		return nil, err
	}
	if client.credentials == nil || client.credentialProvider == nil {
		return nil, client.authenticationError(errors.New("private Bithumb stream requires credentials"))
	}
	if err := client.credentials.RequireEgressRoute(routeID); err != nil {
		return nil, client.authorizationError(err)
	}
	if err := client.credentials.RequirePermission(credential.PermissionRead); err != nil {
		return nil, client.authorizationError(err)
	}
	managed := &managedStream{request: validated, ticketSource: client.ticketSource}
	session, err := client.newSession(
		routeID, corestream.DialRequest{}, client.privateDialRequest, managed.subscribe,
	)
	if err != nil {
		return nil, err
	}
	managed.session = session
	return &PrivateStream{managed: managed}, nil
}

// Run은 private v2 주문·자산 이벤트를 순서대로 decode해 handler에 전달한다.
func (private *PrivateStream) Run(ctx context.Context, handler func(context.Context, StreamMessage) error) error {
	return private.managed.run(ctx, handler)
}

// Close는 private stream 세션을 종료한다.
func (private *PrivateStream) Close() error { return private.managed.session.Close() }

// Generation은 성공한 private 연결 세대 번호를 반환한다.
func (private *PrivateStream) Generation() uint64 { return private.managed.session.Generation() }

func (client *StreamClient) newSession(
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

func (managed *managedStream) run(ctx context.Context, handler func(context.Context, StreamMessage) error) error {
	if handler == nil {
		return fmt.Errorf("Bithumb stream handler is required")
	}
	return managed.session.Run(ctx, func(ctx context.Context, message corestream.Message) error {
		decoded, err := DecodeStreamMessage(message)
		if err != nil {
			return err
		}
		return handler(ctx, decoded)
	})
}

func (managed *managedStream) subscribe(ctx context.Context, connection corestream.Connection) error {
	ticket := managed.request.Ticket
	if ticket == "" {
		resolved, err := managed.ticketSource()
		if err != nil {
			return fmt.Errorf("create Bithumb stream ticket: %w", err)
		}
		ticket = resolved
	}
	payload, err := encodeSubscription(ticket, managed.request.Types, managed.request.Format)
	if err != nil {
		return err
	}
	return connection.Write(ctx, corestream.Message{Type: corestream.MessageText, Data: payload})
}

func encodeSubscription(ticket string, types []StreamDataType, format StreamFormat) ([]byte, error) {
	items := make([]json.RawMessage, 0, len(types)+2)
	ticketJSON, err := json.Marshal(struct {
		Ticket string `json:"ticket"`
	}{Ticket: ticket})
	if err != nil {
		return nil, fmt.Errorf("encode Bithumb stream ticket: %w", err)
	}
	items = append(items, ticketJSON)
	for _, dataType := range types {
		dataJSON, marshalErr := json.Marshal(dataType)
		if marshalErr != nil {
			return nil, fmt.Errorf("encode Bithumb stream data type: %w", marshalErr)
		}
		items = append(items, dataJSON)
	}
	formatJSON, err := json.Marshal(struct {
		Format StreamFormat `json:"format"`
	}{Format: format})
	if err != nil {
		return nil, fmt.Errorf("encode Bithumb stream format: %w", err)
	}
	items = append(items, formatJSON)
	payload, err := json.Marshal(items)
	if err != nil {
		return nil, fmt.Errorf("encode Bithumb stream subscription: %w", err)
	}
	return payload, nil
}

func (client *StreamClient) privateDialRequest(ctx context.Context) (corestream.DialRequest, error) {
	material, err := client.credentialProvider.Resolve(ctx, client.credentials.SecretRef)
	defer material.Destroy()
	if err != nil {
		return corestream.DialRequest{}, client.authenticationError(err)
	}
	if len(material.APIKey) == 0 || len(material.SecretKey) == 0 {
		return corestream.DialRequest{}, client.authenticationError(
			errors.New("Bithumb access key and secret key are required"),
		)
	}
	nonce, err := client.nonceSource()
	if err != nil {
		return corestream.DialRequest{}, client.authenticationError(err)
	}
	token, err := SignJWT(
		material.APIKey, material.SecretKey, nonce, client.now().UnixMilli(), "",
	)
	if err != nil {
		return corestream.DialRequest{}, client.authenticationError(err)
	}
	return corestream.DialRequest{
		Endpoint: client.privateURL,
		Header:   http.Header{"Authorization": {"Bearer " + token}},
	}, nil
}

func (client *StreamClient) resolveRoute(options ...trade.RequestOption) (transport.EgressRouteID, error) {
	resolved, err := trade.ResolveRequestOptions(client.defaultRouteID, options...)
	if err != nil {
		return "", err
	}
	if resolved.Timeout != 0 {
		return "", fmt.Errorf("Bithumb stream timeout must be controlled by Run context")
	}
	return resolved.EgressRouteID, nil
}

func (client *StreamClient) authenticationError(cause error) error {
	accountID := ""
	if client.credentials != nil {
		accountID = client.credentials.AccountID
	}
	return &trade.APIError{
		Category: trade.ErrorAuthentication, Exchange: model.ExchangeBithumb,
		AccountID: accountID, Cause: cause,
	}
}

func (client *StreamClient) authorizationError(cause error) error {
	accountID := ""
	if client.credentials != nil {
		accountID = client.credentials.AccountID
	}
	return &trade.APIError{
		Category: trade.ErrorAuthorization, Exchange: model.ExchangeBithumb,
		AccountID: accountID, Cause: cause,
	}
}

func validateStreamRequest(request StreamRequest, private bool) (StreamRequest, error) {
	if request.Format == "" {
		request.Format = StreamFormatDefault
	}
	if request.Format != StreamFormatDefault && request.Format != StreamFormatSimple {
		return StreamRequest{}, validationError("unsupported WebSocket format %q", request.Format)
	}
	if request.Ticket != "" &&
		(strings.TrimSpace(request.Ticket) != request.Ticket || strings.ContainsFunc(request.Ticket, unicode.IsControl)) {
		return StreamRequest{}, validationError("WebSocket ticket contains invalid whitespace")
	}
	if len(request.Types) == 0 || len(request.Types) > maxStreamDataTypes {
		return StreamRequest{}, validationError("WebSocket data type count must be 1-%d", maxStreamDataTypes)
	}
	seen := make(map[string]struct{}, len(request.Types))
	types := make([]StreamDataType, 0, len(request.Types))
	for _, dataType := range request.Types {
		if dataType.Type == "" || strings.TrimSpace(dataType.Type) != dataType.Type ||
			dataType.OnlySnapshot && dataType.OnlyRealtime {
			return StreamRequest{}, validationError("invalid WebSocket data type")
		}
		if private {
			if dataType.Type != "myOrder" && dataType.Type != "myAsset" {
				return StreamRequest{}, validationError("unsupported private WebSocket type %q", dataType.Type)
			}
			if dataType.OnlySnapshot || dataType.OnlyRealtime || dataType.Level != "" {
				return StreamRequest{}, validationError("private WebSocket type does not accept public stream options")
			}
			if dataType.Type == "myAsset" && len(dataType.Codes) > 0 {
				return StreamRequest{}, validationError("myAsset does not accept market codes")
			}
		} else {
			if dataType.Type != "ticker" && dataType.Type != "trade" && dataType.Type != "orderbook" {
				return StreamRequest{}, validationError("unsupported public WebSocket type %q", dataType.Type)
			}
			if len(dataType.Codes) == 0 {
				return StreamRequest{}, validationError("public WebSocket type requires market codes")
			}
			if dataType.Type != "orderbook" && dataType.Level != "" {
				return StreamRequest{}, validationError("WebSocket level is only supported for orderbook")
			}
			if dataType.Level != "" {
				if err := validatePositiveDecimal("WebSocket level", string(dataType.Level)); err != nil {
					return StreamRequest{}, err
				}
			}
		}
		codes := make([]string, len(dataType.Codes))
		copy(codes, dataType.Codes)
		codeSeen := make(map[string]struct{}, len(codes))
		for _, code := range codes {
			if !marketPattern.MatchString(code) {
				return StreamRequest{}, validationError("invalid WebSocket market code %q", code)
			}
			if _, exists := codeSeen[code]; exists {
				return StreamRequest{}, validationError("duplicate WebSocket market code %q", code)
			}
			codeSeen[code] = struct{}{}
		}
		dataType.Codes = codes
		keyBytes, _ := json.Marshal(dataType)
		key := string(keyBytes)
		if _, exists := seen[key]; exists {
			return StreamRequest{}, validationError("duplicate WebSocket data type")
		}
		seen[key] = struct{}{}
		types = append(types, dataType)
	}
	request.Types = types
	return request, nil
}

func validateWebSocketURL(raw string, allowInsecure bool) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" || parsed.RawQuery != "" {
		return "", fmt.Errorf("invalid WebSocket URL")
	}
	if parsed.Scheme != "wss" && !(allowInsecure && parsed.Scheme == "ws") {
		return "", fmt.Errorf("WebSocket URL must use WSS")
	}
	parsed.Path = strings.TrimSuffix(parsed.Path, "/")
	return parsed.String(), nil
}
