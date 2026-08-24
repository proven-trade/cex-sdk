package stream

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/proven-trade/proven-trade-sdk/transport"
)

func TestWebSocketConnectorUsesBoundRouteAndExchangesMessages(t *testing.T) {
	t.Parallel()

	remoteIP := make(chan string, 1)
	serverResult := make(chan error, 1)
	server := &http.Server{Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		host, _, err := net.SplitHostPort(request.RemoteAddr)
		if err != nil {
			serverResult <- err
			return
		}
		remoteIP <- host
		connection, err := websocket.Accept(writer, request, nil)
		if err != nil {
			serverResult <- err
			return
		}
		defer connection.CloseNow()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		messageType, data, err := connection.Read(ctx)
		if err == nil {
			err = connection.Write(ctx, messageType, data)
		}
		serverResult <- err
	})}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })

	registry, err := transport.NewRegistry([]transport.EgressRoute{
		{ID: "route-a", LocalPrivateIP: net.ParseIP("127.0.0.1")},
	})
	if err != nil {
		t.Fatalf("transport.NewRegistry() error = %v", err)
	}
	t.Cleanup(func() { _ = registry.Close() })
	connector, err := NewWebSocketConnector(ConnectorConfig{HTTPClients: registry})
	if err != nil {
		t.Fatalf("NewWebSocketConnector() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	connection, err := connector.Connect(ctx, "route-a", DialRequest{
		Endpoint: "ws://" + listener.Addr().String() + "/stream",
	})
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	t.Cleanup(func() { _ = connection.Close(1000, "test finished") })
	message := Message{Type: MessageText, Data: []byte("hello")}
	if err := connection.Write(ctx, message); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	got, err := connection.Read(ctx)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if got.Type != MessageText || string(got.Data) != "hello" {
		t.Fatalf("Read() message = %#v", got)
	}
	select {
	case gotIP := <-remoteIP:
		if gotIP != "127.0.0.1" {
			t.Fatalf("remote source IP = %q, want 127.0.0.1", gotIP)
		}
	case <-time.After(time.Second):
		t.Fatal("server did not observe source IP")
	}
	select {
	case serverErr := <-serverResult:
		if serverErr != nil {
			t.Fatalf("WebSocket server error = %v", serverErr)
		}
	case <-time.After(time.Second):
		t.Fatal("WebSocket server did not finish")
	}
}

func TestWebSocketConnectorReturnsSanitizedHandshakeError(t *testing.T) {
	t.Parallel()

	server := &http.Server{Handler: http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Retry-After", "2")
		writer.Header().Set("X-Request-ID", "request-123")
		http.Error(writer, "authorization failed", http.StatusUnauthorized)
	})}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })

	registry, err := transport.NewRegistry([]transport.EgressRoute{
		{ID: "route-a", LocalPrivateIP: net.ParseIP("127.0.0.1")},
	})
	if err != nil {
		t.Fatalf("transport.NewRegistry() error = %v", err)
	}
	t.Cleanup(func() { _ = registry.Close() })
	connector, err := NewWebSocketConnector(ConnectorConfig{HTTPClients: registry})
	if err != nil {
		t.Fatalf("NewWebSocketConnector() error = %v", err)
	}
	_, err = connector.Connect(context.Background(), "route-a", DialRequest{
		Endpoint: "ws://" + listener.Addr().String() + "/stream?token=secret-value",
		Header:   http.Header{"Authorization": {"Bearer secret-value"}},
	})
	var handshakeError *HandshakeError
	if !errors.As(err, &handshakeError) {
		t.Fatalf("Connect() error = %v, want HandshakeError", err)
	}
	if handshakeError.HTTPStatus != http.StatusUnauthorized || handshakeError.RetryAfter != "2" || handshakeError.RequestID != "request-123" {
		t.Fatalf("HandshakeError = %#v", handshakeError)
	}
	if strings.Contains(err.Error(), "secret-value") || strings.Contains(err.Error(), "Authorization") {
		t.Fatalf("HandshakeError exposed credentials: %v", err)
	}
}

func TestWebSocketConnectorValidatesRouteEndpointAndMessageType(t *testing.T) {
	t.Parallel()

	registry, err := transport.NewRegistry([]transport.EgressRoute{
		{ID: "route-a", LocalPrivateIP: net.ParseIP("127.0.0.1")},
	})
	if err != nil {
		t.Fatalf("transport.NewRegistry() error = %v", err)
	}
	t.Cleanup(func() { _ = registry.Close() })
	connector, err := NewWebSocketConnector(ConnectorConfig{HTTPClients: registry})
	if err != nil {
		t.Fatalf("NewWebSocketConnector() error = %v", err)
	}
	if _, err := connector.Connect(context.Background(), "missing", DialRequest{Endpoint: "wss://stream.example.test/ws"}); !errors.Is(err, transport.ErrUnknownEgressRoute) {
		t.Fatalf("Connect(missing route) error = %v", err)
	}
	if _, err := connector.Connect(context.Background(), "route-a", DialRequest{Endpoint: "https://stream.example.test/ws"}); err == nil {
		t.Fatal("Connect(HTTPS endpoint) error = nil")
	}
	if _, err := coderMessageType(0); err == nil {
		t.Fatal("coderMessageType(0) error = nil")
	}
}
