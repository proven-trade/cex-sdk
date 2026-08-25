package binance

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
	"unicode"

	trade "github.com/proven-trade/proven-trade-sdk"
	"github.com/proven-trade/proven-trade-sdk/credential"
	"github.com/proven-trade/proven-trade-sdk/model"
	corestream "github.com/proven-trade/proven-trade-sdk/stream"
	"github.com/proven-trade/proven-trade-sdk/transport"
)

const (
	DefaultMarketStreamURL = "wss://stream.binance.com:9443/stream"
	DefaultWebSocketAPIURL = "wss://ws-api.binance.com:443/ws-api/v3"
	maxMarketStreams       = 1024
	marketCommandInterval  = 250 * time.Millisecond
)

// StreamClientConfig는 Binance Spot public/private WebSocket 설정이다.
type StreamClientConfig struct {
	Connector              corestream.Connector
	Credentials            *credential.Descriptor
	CredentialProvider     credential.Provider
	DefaultEgressRouteID   transport.EgressRouteID
	MarketStreamURL        string
	WebSocketAPIURL        string
	AllowInsecureWebSocket bool
	ReceiveWindow          time.Duration
	Now                    func() time.Time
	Observer               corestream.StateObserver
	ReconnectPolicy        corestream.ReconnectPolicy
	Backoff                corestream.Backoff
	MaxReconnectAttempts   int
}

// StreamClient는 Binance Spot public/private WebSocket 세션을 생성한다.
type StreamClient struct {
	connector            corestream.Connector
	credentials          *credential.Descriptor
	credentialProvider   credential.Provider
	defaultEgressRouteID transport.EgressRouteID
	marketStreamURL      string
	webSocketAPIURL      string
	receiveWindowMillis  int64
	now                  func() time.Time
	observer             corestream.StateObserver
	reconnectPolicy      corestream.ReconnectPolicy
	backoff              corestream.Backoff
	maxReconnectAttempts int
}

// NewStreamClient는 Binance Spot WebSocket 클라이언트를 생성한다.
func NewStreamClient(config StreamClientConfig) (*StreamClient, error) {
	if config.Connector == nil {
		return nil, fmt.Errorf("Binance stream connector is required")
	}
	if strings.TrimSpace(string(config.DefaultEgressRouteID)) == "" {
		return nil, trade.ErrMissingEgressRoute
	}
	marketStreamURL := config.MarketStreamURL
	if marketStreamURL == "" {
		marketStreamURL = DefaultMarketStreamURL
	}
	validatedMarketURL, err := validateStreamBaseURL(marketStreamURL, config.AllowInsecureWebSocket)
	if err != nil {
		return nil, fmt.Errorf("invalid Binance market stream URL: %w", err)
	}
	webSocketAPIURL := config.WebSocketAPIURL
	if webSocketAPIURL == "" {
		webSocketAPIURL = DefaultWebSocketAPIURL
	}
	validatedAPIURL, err := validateStreamBaseURL(webSocketAPIURL, config.AllowInsecureWebSocket)
	if err != nil {
		return nil, fmt.Errorf("invalid Binance WebSocket API URL: %w", err)
	}
	if config.ReceiveWindow == 0 {
		config.ReceiveWindow = DefaultReceiveWindow
	}
	if config.ReceiveWindow <= 0 || config.ReceiveWindow > 60*time.Second || config.ReceiveWindow%time.Millisecond != 0 {
		return nil, fmt.Errorf("Binance stream receive window must be 1-60000 whole milliseconds")
	}
	if config.MaxReconnectAttempts < 0 {
		return nil, fmt.Errorf("Binance maximum reconnect attempts cannot be negative")
	}
	if config.Now == nil {
		config.Now = time.Now
	}

	var credentialsCopy *credential.Descriptor
	if config.Credentials != nil {
		if err := config.Credentials.Validate(); err != nil {
			return nil, err
		}
		if config.Credentials.Exchange != model.ExchangeBinance {
			return nil, fmt.Errorf("credential exchange must be Binance")
		}
		if config.CredentialProvider == nil {
			return nil, fmt.Errorf("credential provider is required for private Binance streams")
		}
		copyValue := *config.Credentials
		copyValue.Permissions = append([]credential.Permission(nil), config.Credentials.Permissions...)
		copyValue.AllowedEgressRouteIDs = append(
			[]transport.EgressRouteID(nil),
			config.Credentials.AllowedEgressRouteIDs...,
		)
		credentialsCopy = &copyValue
	}
	if config.Credentials == nil && config.CredentialProvider != nil {
		return nil, fmt.Errorf("credential descriptor is required with credential provider")
	}

	return &StreamClient{
		connector:            config.Connector,
		credentials:          credentialsCopy,
		credentialProvider:   config.CredentialProvider,
		defaultEgressRouteID: config.DefaultEgressRouteID,
		marketStreamURL:      validatedMarketURL,
		webSocketAPIURL:      validatedAPIURL,
		receiveWindowMillis:  config.ReceiveWindow.Milliseconds(),
		now:                  config.Now,
		observer:             config.Observer,
		reconnectPolicy:      config.ReconnectPolicy,
		backoff:              config.Backoff,
		maxReconnectAttempts: config.MaxReconnectAttempts,
	}, nil
}

// MarketStream은 Binance public market stream 연결을 관리한다.
type MarketStream struct {
	session *corestream.Session

	mu      sync.Mutex
	streams map[string]struct{}

	commandMu     sync.Mutex
	lastCommandAt time.Time
	nextID        atomic.Uint64
}

// MarketStream은 선택한 송신 경로에 고정된 public stream 세션을 생성한다.
func (client *StreamClient) MarketStream(
	request MarketStreamRequest,
	options ...trade.RequestOption,
) (*MarketStream, error) {
	streams, err := validateMarketStreams(request.Streams)
	if err != nil {
		return nil, err
	}
	if request.TimeUnit != StreamTimeMilliseconds && request.TimeUnit != StreamTimeMicroseconds {
		return nil, validationError("unsupported WebSocket time unit %q", request.TimeUnit)
	}
	routeID, err := client.resolveStreamRoute(options...)
	if err != nil {
		return nil, err
	}
	endpoint, err := addStreamTimeUnit(client.marketStreamURL, request.TimeUnit)
	if err != nil {
		return nil, err
	}
	market := &MarketStream{streams: make(map[string]struct{}, len(streams))}
	for _, streamName := range streams {
		market.streams[streamName] = struct{}{}
	}
	session, err := corestream.NewSession(corestream.SessionConfig{
		Connector:            client.connector,
		EgressRouteID:        routeID,
		Request:              corestream.DialRequest{Endpoint: endpoint},
		OnConnect:            market.resubscribe,
		Observer:             client.observer,
		ReconnectPolicy:      client.reconnectPolicy,
		Backoff:              client.backoff,
		MaxReconnectAttempts: client.maxReconnectAttempts,
	})
	if err != nil {
		return nil, err
	}
	market.session = session
	return market, nil
}

// Run은 public stream 메시지를 순서대로 decode해 handler에 전달한다.
func (market *MarketStream) Run(
	ctx context.Context,
	handler func(context.Context, MarketStreamMessage) error,
) error {
	if handler == nil {
		return fmt.Errorf("Binance market stream handler is required")
	}
	return market.session.Run(ctx, func(ctx context.Context, message corestream.Message) error {
		decoded, err := DecodeMarketStreamMessage(message)
		if err != nil {
			return err
		}
		return handler(ctx, decoded)
	})
}

// Subscribe는 연결 중인 세션에 public stream을 추가하고 재연결 목록에도 반영한다.
func (market *MarketStream) Subscribe(ctx context.Context, streams ...string) error {
	validated, err := validateMarketStreams(streams)
	if err != nil {
		return err
	}
	market.commandMu.Lock()
	defer market.commandMu.Unlock()
	market.mu.Lock()
	newStreams := make([]string, 0, len(validated))
	for _, streamName := range validated {
		if _, exists := market.streams[streamName]; !exists {
			newStreams = append(newStreams, streamName)
		}
	}
	if len(market.streams)+len(newStreams) > maxMarketStreams {
		market.mu.Unlock()
		return validationError("WebSocket stream count cannot exceed %d", maxMarketStreams)
	}
	market.mu.Unlock()
	if len(newStreams) == 0 {
		return nil
	}
	if err := market.waitCommandSlot(ctx); err != nil {
		return err
	}
	if err := market.writeCommand(ctx, "SUBSCRIBE", newStreams); err != nil {
		return err
	}
	market.mu.Lock()
	for _, streamName := range newStreams {
		market.streams[streamName] = struct{}{}
	}
	market.mu.Unlock()
	return nil
}

// Unsubscribe는 연결 중인 세션에서 public stream을 제거하고 재연결 목록에도 반영한다.
func (market *MarketStream) Unsubscribe(ctx context.Context, streams ...string) error {
	validated, err := validateMarketStreams(streams)
	if err != nil {
		return err
	}
	market.commandMu.Lock()
	defer market.commandMu.Unlock()
	market.mu.Lock()
	active := make([]string, 0, len(validated))
	for _, streamName := range validated {
		if _, exists := market.streams[streamName]; exists {
			active = append(active, streamName)
		}
	}
	market.mu.Unlock()
	if len(active) == 0 {
		return nil
	}
	if err := market.waitCommandSlot(ctx); err != nil {
		return err
	}
	if err := market.writeCommand(ctx, "UNSUBSCRIBE", active); err != nil {
		return err
	}
	market.mu.Lock()
	for _, streamName := range active {
		delete(market.streams, streamName)
	}
	market.mu.Unlock()
	return nil
}

// Close는 public stream 세션을 종료한다.
func (market *MarketStream) Close() error {
	return market.session.Close()
}

// Generation은 성공한 public stream 연결 세대 번호를 반환한다.
func (market *MarketStream) Generation() uint64 {
	return market.session.Generation()
}

// EgressRouteID는 public stream 연결과 재연결에 고정된 송신 경로를 반환한다.
func (market *MarketStream) EgressRouteID() transport.EgressRouteID {
	return market.session.EgressRouteID()
}

func (market *MarketStream) hasDiffDepthStream(symbol string) bool {
	prefix := strings.ToLower(symbol) + "@depth"
	market.mu.Lock()
	defer market.mu.Unlock()
	for streamName := range market.streams {
		if streamName == prefix || streamName == prefix+"@100ms" {
			return true
		}
	}
	return false
}

func (market *MarketStream) resubscribe(ctx context.Context, connection corestream.Connection) error {
	market.commandMu.Lock()
	defer market.commandMu.Unlock()
	market.mu.Lock()
	streams := make([]string, 0, len(market.streams))
	for streamName := range market.streams {
		streams = append(streams, streamName)
	}
	market.mu.Unlock()
	sort.Strings(streams)
	if len(streams) == 0 {
		return nil
	}
	payload, err := json.Marshal(marketControlRequest{
		Method: "SUBSCRIBE",
		Params: streams,
		ID:     market.nextID.Add(1),
	})
	if err != nil {
		return fmt.Errorf("encode Binance subscription request: %w", err)
	}
	if err := connection.Write(ctx, corestream.Message{Type: corestream.MessageText, Data: payload}); err != nil {
		return err
	}
	market.lastCommandAt = time.Now()
	return nil
}

func (market *MarketStream) writeCommand(ctx context.Context, method string, streams []string) error {
	payload, err := json.Marshal(marketControlRequest{
		Method: method,
		Params: streams,
		ID:     market.nextID.Add(1),
	})
	if err != nil {
		return fmt.Errorf("encode Binance stream command: %w", err)
	}
	if err := market.session.Write(ctx, corestream.Message{Type: corestream.MessageText, Data: payload}); err != nil {
		return err
	}
	market.lastCommandAt = time.Now()
	return nil
}

func (market *MarketStream) waitCommandSlot(ctx context.Context) error {
	delay := time.Until(market.lastCommandAt.Add(marketCommandInterval))
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

type marketControlRequest struct {
	Method string   `json:"method"`
	Params []string `json:"params"`
	ID     uint64   `json:"id"`
}

// UserDataStream은 Binance Spot 계정과 주문 이벤트 연결을 관리한다.
type UserDataStream struct {
	session *corestream.Session
}

// UserDataStream은 HMAC 서명 구독을 사용하는 private stream 세션을 생성한다.
func (client *StreamClient) UserDataStream(options ...trade.RequestOption) (*UserDataStream, error) {
	routeID, err := client.resolveStreamRoute(options...)
	if err != nil {
		return nil, err
	}
	if client.credentials == nil || client.credentialProvider == nil {
		return nil, client.authenticationError(errors.New("private Binance stream requires credentials"))
	}
	if err := client.credentials.RequireEgressRoute(routeID); err != nil {
		return nil, client.authorizationError(err)
	}
	if err := client.credentials.RequirePermission(credential.PermissionRead); err != nil {
		return nil, client.authorizationError(err)
	}
	userData := &UserDataStream{}
	session, err := corestream.NewSession(corestream.SessionConfig{
		Connector:            client.connector,
		EgressRouteID:        routeID,
		Request:              corestream.DialRequest{Endpoint: client.webSocketAPIURL},
		OnConnect:            client.subscribeUserData,
		Observer:             client.observer,
		ReconnectPolicy:      client.reconnectPolicy,
		Backoff:              client.backoff,
		MaxReconnectAttempts: client.maxReconnectAttempts,
	})
	if err != nil {
		return nil, err
	}
	userData.session = session
	return userData, nil
}

// Run은 private stream 메시지를 순서대로 decode해 handler에 전달한다.
func (userData *UserDataStream) Run(
	ctx context.Context,
	handler func(context.Context, UserDataStreamMessage) error,
) error {
	if handler == nil {
		return fmt.Errorf("Binance user data stream handler is required")
	}
	return userData.session.Run(ctx, func(ctx context.Context, message corestream.Message) error {
		decoded, err := DecodeUserDataStreamMessage(message)
		if err != nil {
			return err
		}
		return handler(ctx, decoded)
	})
}

// Close는 private stream 세션을 종료한다.
func (userData *UserDataStream) Close() error {
	return userData.session.Close()
}

// Generation은 성공한 private stream 연결 세대 번호를 반환한다.
func (userData *UserDataStream) Generation() uint64 {
	return userData.session.Generation()
}

func (client *StreamClient) subscribeUserData(ctx context.Context, connection corestream.Connection) error {
	material, err := client.credentialProvider.Resolve(ctx, client.credentials.SecretRef)
	defer material.Destroy()
	if err != nil {
		return client.authenticationError(err)
	}
	if len(material.APIKey) == 0 || len(material.SecretKey) == 0 {
		return client.authenticationError(errors.New("Binance API key and HMAC secret are required"))
	}
	timestamp := client.now().UnixMilli()
	values := url.Values{}
	values.Set("apiKey", string(material.APIKey))
	values.Set("recvWindow", strconv.FormatInt(client.receiveWindowMillis, 10))
	values.Set("timestamp", strconv.FormatInt(timestamp, 10))
	signature, err := SignHMACSHA256(material.SecretKey, []byte(values.Encode()))
	if err != nil {
		return client.authenticationError(err)
	}
	payload, err := json.Marshal(userDataSubscribeRequest{
		ID:     "user-data-subscribe",
		Method: "userDataStream.subscribe.signature",
		Params: userDataSubscribeParams{
			APIKey:        string(material.APIKey),
			Timestamp:     timestamp,
			ReceiveWindow: client.receiveWindowMillis,
			Signature:     signature,
		},
	})
	if err != nil {
		return fmt.Errorf("encode Binance user data subscription: %w", err)
	}
	defer clearBytes(payload)
	return connection.Write(ctx, corestream.Message{Type: corestream.MessageText, Data: payload})
}

type userDataSubscribeRequest struct {
	ID     string                  `json:"id"`
	Method string                  `json:"method"`
	Params userDataSubscribeParams `json:"params"`
}

type userDataSubscribeParams struct {
	APIKey        string `json:"apiKey"`
	Timestamp     int64  `json:"timestamp"`
	ReceiveWindow int64  `json:"recvWindow"`
	Signature     string `json:"signature"`
}

func (client *StreamClient) resolveStreamRoute(options ...trade.RequestOption) (transport.EgressRouteID, error) {
	resolved, err := trade.ResolveRequestOptions(client.defaultEgressRouteID, options...)
	if err != nil {
		return "", err
	}
	if resolved.Timeout != 0 {
		return "", fmt.Errorf("Binance stream timeout must be controlled by Run context")
	}
	return resolved.EgressRouteID, nil
}

func (client *StreamClient) authenticationError(cause error) error {
	accountID := ""
	if client.credentials != nil {
		accountID = client.credentials.AccountID
	}
	return &trade.APIError{
		Category:  trade.ErrorAuthentication,
		Exchange:  model.ExchangeBinance,
		AccountID: accountID,
		Cause:     cause,
	}
}

func (client *StreamClient) authorizationError(cause error) error {
	accountID := ""
	if client.credentials != nil {
		accountID = client.credentials.AccountID
	}
	return &trade.APIError{
		Category:  trade.ErrorAuthorization,
		Exchange:  model.ExchangeBinance,
		AccountID: accountID,
		Cause:     cause,
	}
}

// SymbolMarketStream은 상품과 channel을 Binance stream 이름으로 변환한다.
func SymbolMarketStream(symbol, channel string) (string, error) {
	if err := validateSymbol(symbol); err != nil {
		return "", err
	}
	switch channel {
	case "aggTrade", "trade", "miniTicker", "ticker", "bookTicker", "avgPrice", "depth":
		return strings.ToLower(symbol) + "@" + channel, nil
	default:
		return "", validationError("unsupported WebSocket channel %q", channel)
	}
}

// KlineMarketStream은 상품과 캔들 간격을 Binance kline stream 이름으로 변환한다.
func KlineMarketStream(symbol string, interval KlineInterval) (string, error) {
	if err := validateSymbol(symbol); err != nil {
		return "", err
	}
	if !interval.valid() {
		return "", validationError("unsupported WebSocket kline interval %q", interval)
	}
	return strings.ToLower(symbol) + "@kline_" + string(interval), nil
}

// PartialDepthMarketStream은 지정 단계와 갱신 주기의 partial depth stream 이름을 만든다.
func PartialDepthMarketStream(symbol string, levels int, updateSpeed time.Duration) (string, error) {
	if err := validateSymbol(symbol); err != nil {
		return "", err
	}
	if levels != 5 && levels != 10 && levels != 20 {
		return "", validationError("partial depth levels must be 5, 10, or 20")
	}
	if updateSpeed != 0 && updateSpeed != 100*time.Millisecond {
		return "", validationError("partial depth update speed must be default or 100ms")
	}
	name := strings.ToLower(symbol) + "@depth" + strconv.Itoa(levels)
	if updateSpeed == 100*time.Millisecond {
		name += "@100ms"
	}
	return name, nil
}

func validateMarketStreams(streams []string) ([]string, error) {
	if len(streams) == 0 {
		return nil, validationError("at least one WebSocket stream is required")
	}
	if len(streams) > maxMarketStreams {
		return nil, validationError("WebSocket stream count cannot exceed %d", maxMarketStreams)
	}
	validated := make([]string, 0, len(streams))
	seen := make(map[string]struct{}, len(streams))
	for _, streamName := range streams {
		if streamName == "" || strings.TrimSpace(streamName) != streamName {
			return nil, validationError("WebSocket stream name is empty or has surrounding whitespace")
		}
		if strings.ContainsFunc(streamName, unicode.IsControl) {
			return nil, validationError("WebSocket stream name contains a control character")
		}
		if _, exists := seen[streamName]; exists {
			return nil, validationError("duplicate WebSocket stream %q", streamName)
		}
		seen[streamName] = struct{}{}
		validated = append(validated, streamName)
	}
	return validated, nil
}

func validateStreamBaseURL(raw string, allowInsecure bool) (string, error) {
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

func addStreamTimeUnit(raw string, unit StreamTimeUnit) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parse Binance market stream URL: %w", err)
	}
	if unit == StreamTimeMicroseconds {
		query := parsed.Query()
		query.Set("timeUnit", string(unit))
		parsed.RawQuery = query.Encode()
	}
	return parsed.String(), nil
}

func clearBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
