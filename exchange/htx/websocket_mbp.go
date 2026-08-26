package htx

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	corestream "github.com/proven-trade/cex-sdk/stream"
	"github.com/proven-trade/cex-sdk/transport"
)

const mbpRefreshMinimumInterval = 100 * time.Millisecond

// MBPStream은 HTX `/feed` 증분 호가와 refresh 응답 연결을 관리한다.
type MBPStream struct {
	managed           *managedStream
	refreshGeneration uint64
	lastRefresh       time.Time
}

// MBPStream은 선택한 송신 경로에 고정된 증분 호가 세션을 생성한다.
func (client *StreamClient) MBPStream(
	request StreamRequest,
	options ...trade.RequestOption,
) (*MBPStream, error) {
	subscriptions, err := validateMBPStreamSubscriptions(request.Subscriptions, true)
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
		Request:   corestream.DialRequest{Endpoint: client.mbpURL},
		OnConnect: managed.resubscribe, Observer: client.observer,
		ReconnectPolicy: client.streamReconnectPolicy, Backoff: client.backoff,
		MaxReconnectAttempts: client.maxReconnectAttempts,
	})
	if err != nil {
		return nil, err
	}
	managed.session = session
	return &MBPStream{managed: managed}, nil
}

// Run은 증분 호가·refresh·heartbeat 메시지를 순서대로 handler에 전달한다.
func (mbp *MBPStream) Run(
	ctx context.Context,
	handler func(context.Context, StreamMessage) error,
) error {
	return mbp.managed.run(ctx, handler)
}

// Subscribe는 증분 호가 구독을 추가하고 재연결 복구 목록에도 반영한다.
func (mbp *MBPStream) Subscribe(
	ctx context.Context,
	subscriptions ...StreamSubscription,
) error {
	validated, err := validateMBPStreamSubscriptions(subscriptions, true)
	if err != nil {
		return err
	}
	return mbp.managed.change(ctx, "subscribe", validated)
}

// Unsubscribe는 증분 호가 구독을 제거하고 재연결 복구 목록에도 반영한다.
func (mbp *MBPStream) Unsubscribe(
	ctx context.Context,
	subscriptions ...StreamSubscription,
) error {
	validated, err := validateMBPStreamSubscriptions(subscriptions, true)
	if err != nil {
		return err
	}
	return mbp.managed.change(ctx, "unsubscribe", validated)
}

// RequestSnapshot은 현재 구독의 sequence 정렬용 refresh 전체 이미지를 요청한다.
func (mbp *MBPStream) RequestSnapshot(
	ctx context.Context,
	subscription StreamSubscription,
) (string, error) {
	if ctx == nil {
		return "", fmt.Errorf("HTX MBP refresh context is nil")
	}
	validated, err := validateMBPStreamSubscriptions([]StreamSubscription{subscription}, true)
	if err != nil {
		return "", err
	}
	subscription = validated[0]
	if !subscription.MBPDepth.refreshSupported() {
		return "", validationError(
			"HTX MBP depth %d does not support refresh request", subscription.MBPDepth,
		)
	}
	mbp.managed.commandMu.Lock()
	defer mbp.managed.commandMu.Unlock()
	key := streamSubscriptionKey(subscription)
	mbp.managed.stateMu.Lock()
	_, exists := mbp.managed.subscriptions[key]
	mbp.managed.stateMu.Unlock()
	if !exists {
		return "", validationError("HTX MBP refresh requires an active exact subscription")
	}
	if err := mbp.waitForRefresh(ctx); err != nil {
		return "", err
	}
	id := "htx-" + strconv.FormatInt(mbp.managed.client.nextID.Add(1), 10)
	payload, err := json.Marshal(struct {
		Request string `json:"req"`
		ID      string `json:"id"`
	}{Request: streamSubscriptionTopic(subscription), ID: id})
	if err != nil {
		return "", fmt.Errorf("encode HTX MBP refresh request: %w", err)
	}
	if err := mbp.managed.session.Write(
		ctx, corestream.Message{Type: corestream.MessageText, Data: payload},
	); err != nil {
		return "", err
	}
	return id, nil
}

func (mbp *MBPStream) waitForRefresh(ctx context.Context) error {
	generation := mbp.Generation()
	if generation != mbp.refreshGeneration {
		mbp.refreshGeneration = generation
		mbp.lastRefresh = time.Time{}
	}
	if !mbp.lastRefresh.IsZero() {
		delay := time.Until(mbp.lastRefresh.Add(mbpRefreshMinimumInterval))
		if delay > 0 {
			timer := time.NewTimer(delay)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
	mbp.lastRefresh = time.Now()
	return nil
}

// Close는 MBP stream 세션을 종료한다.
func (mbp *MBPStream) Close() error { return mbp.managed.session.Close() }

// Generation은 성공한 MBP 연결 세대 번호를 반환한다.
func (mbp *MBPStream) Generation() uint64 { return mbp.managed.session.Generation() }

// EgressRouteID는 MBP 최초 연결과 모든 재연결에 고정된 송신 경로를 반환한다.
func (mbp *MBPStream) EgressRouteID() transport.EgressRouteID {
	return mbp.managed.session.EgressRouteID()
}

func (mbp *MBPStream) reconnect() error { return mbp.managed.session.Reconnect() }

func validateMBPStreamSubscriptions(
	subscriptions []StreamSubscription,
	requireNonEmpty bool,
) ([]StreamSubscription, error) {
	if requireNonEmpty && len(subscriptions) == 0 {
		return nil, validationError("MBP WebSocket subscription is required")
	}
	if len(subscriptions) > maximumStreamSubscriptions {
		return nil, validationError(
			"WebSocket subscription count cannot exceed %d", maximumStreamSubscriptions,
		)
	}
	result := make([]StreamSubscription, 0, len(subscriptions))
	seen := make(map[string]struct{}, len(subscriptions))
	for _, subscription := range subscriptions {
		if err := validateMBPStreamSubscription(subscription); err != nil {
			return nil, err
		}
		key := streamSubscriptionKey(subscription)
		if _, exists := seen[key]; exists {
			return nil, validationError("duplicate MBP WebSocket subscription")
		}
		seen[key] = struct{}{}
		result = append(result, subscription)
	}
	return result, nil
}

func validateMBPStreamSubscription(subscription StreamSubscription) error {
	if subscription.Channel != StreamChannelMBP {
		return validationError("unsupported MBP WebSocket channel %q", subscription.Channel)
	}
	if err := validateSymbol(subscription.Symbol); err != nil {
		return err
	}
	if !subscription.MBPDepth.valid() {
		return validationError("unsupported MBP WebSocket depth %d", subscription.MBPDepth)
	}
	if subscription.DepthType != "" || subscription.CandleInterval != "" ||
		subscription.Mode != 0 {
		return validationError("MBP WebSocket subscription contains unrelated options")
	}
	return nil
}

func (depth StreamMBPDepth) valid() bool {
	switch depth {
	case StreamMBPDepth5, StreamMBPDepth20, StreamMBPDepth150, StreamMBPDepth400:
		return true
	default:
		return false
	}
}

func (depth StreamMBPDepth) refreshSupported() bool {
	return depth == StreamMBPDepth5 || depth == StreamMBPDepth20 ||
		depth == StreamMBPDepth150
}
