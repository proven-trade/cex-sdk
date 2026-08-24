package transport

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sort"
	"sync"
	"time"
)

const (
	defaultDialTimeout           = 5 * time.Second
	defaultKeepAlive             = 30 * time.Second
	defaultResponseHeaderTimeout = 10 * time.Second
)

type registryConfig struct {
	verifyAddress         LocalAddressVerifier
	dialTimeout           time.Duration
	keepAlive             time.Duration
	responseHeaderTimeout time.Duration
}

// RegistryOption은 송신 경로 레지스트리를 설정한다.
type RegistryOption func(*registryConfig) error

// WithLocalAddressVerifier는 호스트 주소 검사기를 교체한다.
// 주로 통제된 테스트에서 사용하며 운영 환경에서는 기본 검사기를 사용해야 한다.
func WithLocalAddressVerifier(verifier LocalAddressVerifier) RegistryOption {
	return func(config *registryConfig) error {
		if verifier == nil {
			return fmt.Errorf("local address verifier cannot be nil")
		}
		config.verifyAddress = verifier
		return nil
	}
}

// WithDialTimeout은 TCP 연결 수립에 사용할 최대 시간을 변경한다.
func WithDialTimeout(timeout time.Duration) RegistryOption {
	return func(config *registryConfig) error {
		if timeout <= 0 {
			return fmt.Errorf("dial timeout must be positive")
		}
		config.dialTimeout = timeout
		return nil
	}
}

// WithResponseHeaderTimeout은 응답 헤더를 기다릴 최대 시간을 변경한다.
func WithResponseHeaderTimeout(timeout time.Duration) RegistryOption {
	return func(config *registryConfig) error {
		if timeout <= 0 {
			return fmt.Errorf("response header timeout must be positive")
		}
		config.responseHeaderTimeout = timeout
		return nil
	}
}

type routeTransport struct {
	route     EgressRoute
	client    *http.Client
	transport *http.Transport
}

// Registry는 local private IP마다 장기 재사용하는 HTTP transport 하나를 관리한다.
// transport 내부에서는 origin마다 연결 풀이 분리되며, 해당 transport가 만드는
// 모든 연결은 같은 source IP에 바인딩된다.
type Registry struct {
	mu     sync.RWMutex
	routes map[EgressRouteID]*routeTransport
	closed bool
}

type boundRoundTripper struct {
	registry *Registry
	routeID  EgressRouteID
}

// NewRegistry는 모든 송신 경로를 검증한 뒤 연결 풀을 생성한다.
func NewRegistry(routes []EgressRoute, options ...RegistryOption) (*Registry, error) {
	if len(routes) == 0 {
		return nil, fmt.Errorf("%w: at least one route is required", ErrInvalidEgressRoute)
	}

	config := registryConfig{
		verifyAddress:         VerifySystemLocalAddress,
		dialTimeout:           defaultDialTimeout,
		keepAlive:             defaultKeepAlive,
		responseHeaderTimeout: defaultResponseHeaderTimeout,
	}
	for index, apply := range options {
		if apply == nil {
			return nil, fmt.Errorf("registry option %d is nil", index)
		}
		if err := apply(&config); err != nil {
			return nil, fmt.Errorf("apply registry option %d: %w", index, err)
		}
	}

	normalized := make([]EgressRoute, 0, len(routes))
	seenIDs := make(map[EgressRouteID]struct{}, len(routes))
	seenLocalIPs := make(map[string]EgressRouteID, len(routes))
	for _, route := range routes {
		clean, err := normalizeRoute(route)
		if err != nil {
			return nil, err
		}
		if _, exists := seenIDs[clean.ID]; exists {
			return nil, fmt.Errorf("%w: duplicate route ID %q", ErrInvalidEgressRoute, clean.ID)
		}
		if previousID, exists := seenLocalIPs[clean.LocalPrivateIP.String()]; exists {
			return nil, fmt.Errorf(
				"%w: routes %q and %q use the same local private IP %s",
				ErrInvalidEgressRoute,
				previousID,
				clean.ID,
				clean.LocalPrivateIP,
			)
		}
		if err := config.verifyAddress(clean.LocalPrivateIP); err != nil {
			return nil, fmt.Errorf("verify route %q: %w", clean.ID, err)
		}
		seenIDs[clean.ID] = struct{}{}
		seenLocalIPs[clean.LocalPrivateIP.String()] = clean.ID
		normalized = append(normalized, clean)
	}

	registry := &Registry{routes: make(map[EgressRouteID]*routeTransport, len(normalized))}
	for _, route := range normalized {
		httpTransport := newHTTPTransport(route.LocalPrivateIP, config)
		registry.routes[route.ID] = &routeTransport{
			route:     route,
			transport: httpTransport,
			client:    &http.Client{Transport: httpTransport},
		}
	}

	return registry, nil
}

func newHTTPTransport(localIP net.IP, config registryConfig) *http.Transport {
	httpTransport := http.DefaultTransport.(*http.Transport).Clone()

	// proxy를 사용하면 거래소가 관측하는 주소가 달라져 경로 계약을 위반한다.
	// 명시적인 proxy 지원은 향후 별도 경로 타입으로 제공해야 한다.
	httpTransport.Proxy = nil
	httpTransport.DialContext = (&net.Dialer{
		Timeout:   config.dialTimeout,
		KeepAlive: config.keepAlive,
		LocalAddr: &net.TCPAddr{IP: cloneIP(localIP)},
	}).DialContext
	httpTransport.ResponseHeaderTimeout = config.responseHeaderTimeout
	httpTransport.ForceAttemptHTTP2 = true

	return httpTransport
}

// Do는 선택한 송신 경로로 요청을 보낸다.
// 호출자가 전달한 context와 요청 객체를 변경하지 않도록 요청을 복제한다.
func (registry *Registry) Do(
	ctx context.Context,
	routeID EgressRouteID,
	request *http.Request,
) (*http.Response, error) {
	if ctx == nil {
		return nil, fmt.Errorf("request context cannot be nil")
	}
	if request == nil {
		return nil, fmt.Errorf("HTTP request cannot be nil")
	}

	entry, err := registry.lookup(routeID)
	if err != nil {
		return nil, err
	}

	response, err := entry.client.Do(request.Clone(ctx))
	if err != nil {
		return nil, fmt.Errorf("send request through route %q: %w", routeID, err)
	}
	return response, nil
}

// HTTPClient는 모든 연결을 지정한 route의 private IP로 보내는 HTTP 클라이언트를 반환한다.
// WebSocket upgrade처럼 호출자가 http.Client를 요구하는 프로토콜에 사용한다.
// 자격증명 헤더가 다른 origin으로 전달되지 않도록 redirect는 허용하지 않는다.
func (registry *Registry) HTTPClient(routeID EgressRouteID) (*http.Client, error) {
	if _, err := registry.lookup(routeID); err != nil {
		return nil, err
	}
	return &http.Client{
		Transport: &boundRoundTripper{registry: registry, routeID: routeID},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}, nil
}

func (roundTripper *boundRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	if request == nil {
		return nil, fmt.Errorf("HTTP request cannot be nil")
	}
	entry, err := roundTripper.registry.lookup(roundTripper.routeID)
	if err != nil {
		return nil, err
	}
	return entry.transport.RoundTrip(request.Clone(request.Context()))
}

// Route는 등록된 송신 경로의 방어적 복사본을 반환한다.
func (registry *Registry) Route(routeID EgressRouteID) (EgressRoute, error) {
	entry, err := registry.lookup(routeID)
	if err != nil {
		return EgressRoute{}, err
	}
	return cloneRoute(entry.route), nil
}

// Routes는 등록된 모든 송신 경로를 ID 순서로 반환한다.
func (registry *Registry) Routes() []EgressRoute {
	registry.mu.RLock()
	defer registry.mu.RUnlock()

	routes := make([]EgressRoute, 0, len(registry.routes))
	for _, entry := range registry.routes {
		routes = append(routes, cloneRoute(entry.route))
	}
	sort.Slice(routes, func(left, right int) bool {
		return routes[left].ID < routes[right].ID
	})
	return routes
}

func (registry *Registry) lookup(routeID EgressRouteID) (*routeTransport, error) {
	registry.mu.RLock()
	defer registry.mu.RUnlock()

	if registry.closed {
		return nil, ErrRegistryClosed
	}
	entry, exists := registry.routes[routeID]
	if !exists {
		return nil, fmt.Errorf("%w: %q", ErrUnknownEgressRoute, routeID)
	}
	return entry, nil
}

// CloseIdleConnections는 모든 경로의 유휴 keep-alive 연결을 닫는다.
func (registry *Registry) CloseIdleConnections() {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	for _, entry := range registry.routes {
		entry.transport.CloseIdleConnections()
	}
}

// Close는 새로운 요청을 거부하도록 만들고 유휴 연결을 닫는다.
// 여러 번 호출해도 안전하다.
func (registry *Registry) Close() error {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if registry.closed {
		return nil
	}
	registry.closed = true
	for _, entry := range registry.routes {
		entry.transport.CloseIdleConnections()
	}
	return nil
}
