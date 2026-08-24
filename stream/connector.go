package stream

import (
	"context"
	"fmt"
	"net/url"

	"github.com/coder/websocket"
	"github.com/proven-trade/proven-trade-sdk/transport"
)

const defaultReadLimit int64 = 4 << 20

// HandshakeError는 WebSocket upgrade 실패의 HTTP 상태와 헤더를 보존한다.
// 요청 헤더와 endpoint query는 민감할 수 있으므로 저장하지 않는다.
type HandshakeError struct {
	HTTPStatus int
	RetryAfter string
	RequestID  string
	Cause      error
}

// Error는 handshake 실패를 민감 정보 없이 표현한다.
func (handshakeError *HandshakeError) Error() string {
	if handshakeError == nil {
		return "<nil>"
	}
	if handshakeError.HTTPStatus != 0 {
		return fmt.Sprintf("WebSocket handshake failed with HTTP status %d", handshakeError.HTTPStatus)
	}
	return "WebSocket handshake failed"
}

// Unwrap은 원본 handshake 오류를 노출한다.
func (handshakeError *HandshakeError) Unwrap() error {
	if handshakeError == nil {
		return nil
	}
	return handshakeError.Cause
}

// ConnectorConfig는 실제 WebSocket connector 설정이다.
type ConnectorConfig struct {
	HTTPClients HTTPClientProvider
	ReadLimit   int64
}

// WebSocketConnector는 coder/websocket을 route 바인딩 HTTP client와 연결한다.
type WebSocketConnector struct {
	httpClients HTTPClientProvider
	readLimit   int64
}

// NewWebSocketConnector는 route별 WebSocket connector를 생성한다.
func NewWebSocketConnector(config ConnectorConfig) (*WebSocketConnector, error) {
	if config.HTTPClients == nil {
		return nil, fmt.Errorf("WebSocket HTTP client provider is required")
	}
	if config.ReadLimit == 0 {
		config.ReadLimit = defaultReadLimit
	}
	if config.ReadLimit < 0 {
		return nil, fmt.Errorf("WebSocket read limit cannot be negative")
	}
	return &WebSocketConnector{httpClients: config.HTTPClients, readLimit: config.ReadLimit}, nil
}

// Connect는 지정 route를 통해 WebSocket handshake를 수행한다.
func (connector *WebSocketConnector) Connect(
	ctx context.Context,
	routeID transport.EgressRouteID,
	request DialRequest,
) (Connection, error) {
	if ctx == nil {
		return nil, fmt.Errorf("WebSocket context cannot be nil")
	}
	if err := validateEndpoint(request.Endpoint); err != nil {
		return nil, err
	}
	httpClient, err := connector.httpClients.HTTPClient(routeID)
	if err != nil {
		return nil, err
	}
	connection, response, err := websocket.Dial(ctx, request.Endpoint, &websocket.DialOptions{
		HTTPClient:      httpClient,
		HTTPHeader:      request.Header.Clone(),
		CompressionMode: websocket.CompressionDisabled,
	})
	if err != nil {
		handshakeError := &HandshakeError{Cause: err}
		if response != nil {
			handshakeError.HTTPStatus = response.StatusCode
			handshakeError.RetryAfter = response.Header.Get("Retry-After")
			handshakeError.RequestID = response.Header.Get("X-Request-ID")
			if response.Body != nil {
				_ = response.Body.Close()
			}
		}
		return nil, handshakeError
	}
	connection.SetReadLimit(connector.readLimit)
	return &coderConnection{connection: connection}, nil
}

type coderConnection struct {
	connection *websocket.Conn
}

func (connection *coderConnection) Read(ctx context.Context) (Message, error) {
	messageType, data, err := connection.connection.Read(ctx)
	if err != nil {
		return Message{}, err
	}
	typeValue := MessageBinary
	if messageType == websocket.MessageText {
		typeValue = MessageText
	}
	return Message{Type: typeValue, Data: append([]byte(nil), data...)}, nil
}

func (connection *coderConnection) Write(ctx context.Context, message Message) error {
	messageType, err := coderMessageType(message.Type)
	if err != nil {
		return err
	}
	return connection.connection.Write(ctx, messageType, message.Data)
}

func (connection *coderConnection) Ping(ctx context.Context) error {
	return connection.connection.Ping(ctx)
}

func (connection *coderConnection) Close(code int, reason string) error {
	return connection.connection.Close(websocket.StatusCode(code), reason)
}

func coderMessageType(messageType MessageType) (websocket.MessageType, error) {
	switch messageType {
	case MessageText:
		return websocket.MessageText, nil
	case MessageBinary:
		return websocket.MessageBinary, nil
	default:
		return 0, fmt.Errorf("unsupported WebSocket message type %d", messageType)
	}
}

func validateEndpoint(endpoint string) error {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return fmt.Errorf("invalid WebSocket endpoint")
	}
	if parsed.Scheme != "ws" && parsed.Scheme != "wss" {
		return fmt.Errorf("WebSocket endpoint must use ws or wss")
	}
	return nil
}
