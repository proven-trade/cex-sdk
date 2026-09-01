// Package ratelimit은 거래소의 여러 요청 제한 규칙을 함께 적용한다.
package ratelimit

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	// ErrInvalidRule은 요청 제한 규칙이 올바르지 않음을 나타낸다.
	ErrInvalidRule = errors.New("invalid rate limit rule")
	// ErrUnknownRule은 차감하려는 규칙이 등록되지 않았음을 나타낸다.
	ErrUnknownRule = errors.New("unknown rate limit rule")
)

// Rule은 하나의 rolling-window 요청 제한을 정의한다.
type Rule struct {
	Key    string
	Limit  int
	Window time.Duration
}

// Charge는 요청 한 건이 특정 규칙에서 소비하는 양을 나타낸다.
type Charge struct {
	Key   string
	Units int
}

// Snapshot은 특정 규칙의 현재 상태다.
type Snapshot struct {
	Rule         Rule
	WindowStart  time.Time
	Used         int
	BlockedUntil time.Time
}

// Backend는 limiter 상태 저장소의 원자적 계약이다. 분산 구현은 Wait에서
// 모든 charge를 한 번의 원자적 연산으로 검사·차감하고 context 취소를
// 존중해야 한다. SetRule, ObserveUsed, BlockFor와 Snapshot도 같은 공유
// namespace를 사용해야 여러 SDK 프로세스가 하나의 거래소 한도를 공유한다.
type Backend interface {
	SetRule(Rule) error
	Wait(context.Context, ...Charge) error
	ObserveUsed(string, int) error
	BlockFor([]string, time.Duration) error
	Snapshot(string) (Snapshot, error)
}

// Limiter는 여러 규칙의 차감을 backend를 통해 원자적으로 처리한다.
type Limiter struct {
	backend Backend
}

// New는 프로세스 내부 rolling-window backend를 사용하는 limiter를 생성한다.
// 여러 프로세스나 pod가 같은 API key를 공유하면 NewWithBackend로 분산
// backend를 주입해야 한다.
func New(rules ...Rule) (*Limiter, error) {
	return NewWithBackend(newMemoryBackend(), rules...)
}

// NewWithBackend는 공유 상태를 구현하는 backend를 사용하는 limiter를 생성한다.
func NewWithBackend(backend Backend, rules ...Rule) (*Limiter, error) {
	if backend == nil {
		return nil, fmt.Errorf("rate limit backend is required")
	}
	limiter := &Limiter{backend: backend}
	for _, rule := range rules {
		if err := limiter.SetRule(rule); err != nil {
			return nil, err
		}
	}
	return limiter, nil
}

// SetRule은 규칙을 추가하거나 갱신한다.
func (limiter *Limiter) SetRule(rule Rule) error {
	rule.Key = strings.TrimSpace(rule.Key)
	if rule.Key == "" || rule.Limit <= 0 || rule.Window <= 0 {
		return fmt.Errorf("%w: key, positive limit, and positive window are required", ErrInvalidRule)
	}
	return limiter.backend.SetRule(rule)
}

// Wait는 모든 차감 규칙에 여유가 생길 때까지 기다린 뒤 한 번에 차감한다.
func (limiter *Limiter) Wait(ctx context.Context, charges ...Charge) error {
	if ctx == nil {
		return fmt.Errorf("context cannot be nil")
	}
	normalized, err := normalizeCharges(charges)
	if err != nil {
		return err
	}
	if len(normalized) == 0 {
		return nil
	}
	return limiter.backend.Wait(ctx, normalized...)
}

// ObserveUsed는 거래소 응답 헤더에서 관측한 사용량을 backend에 반영한다.
func (limiter *Limiter) ObserveUsed(key string, used int) error {
	if used < 0 {
		return fmt.Errorf("observed usage cannot be negative")
	}
	return limiter.backend.ObserveUsed(key, used)
}

// BlockFor는 거래소가 지시한 시간 동안 지정한 규칙의 신규 요청을 막는다.
func (limiter *Limiter) BlockFor(keys []string, duration time.Duration) error {
	if duration <= 0 {
		return fmt.Errorf("block duration must be positive")
	}
	return limiter.backend.BlockFor(keys, duration)
}

// Snapshot은 테스트와 관측을 위한 규칙 상태 복사본을 반환한다.
func (limiter *Limiter) Snapshot(key string) (Snapshot, error) {
	return limiter.backend.Snapshot(key)
}

type usageEvent struct {
	at    time.Time
	units int
}

type bucket struct {
	events       []usageEvent
	used         int
	blockedUntil time.Time
}

type memoryBackend struct {
	mu      sync.Mutex
	rules   map[string]Rule
	buckets map[string]*bucket
}

func newMemoryBackend() *memoryBackend {
	return &memoryBackend{rules: make(map[string]Rule), buckets: make(map[string]*bucket)}
}

func (backend *memoryBackend) SetRule(rule Rule) error {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	previous, exists := backend.rules[rule.Key]
	backend.rules[rule.Key] = rule
	if !exists || previous.Limit != rule.Limit || previous.Window != rule.Window {
		backend.buckets[rule.Key] = &bucket{}
	}
	return nil
}

func (backend *memoryBackend) Wait(ctx context.Context, charges ...Charge) error {
	for {
		now := time.Now()
		waitUntil, err := backend.tryAcquire(now, charges)
		if err != nil {
			return err
		}
		if waitUntil.IsZero() {
			return nil
		}
		wait := time.Until(waitUntil)
		if wait <= 0 {
			continue
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (backend *memoryBackend) tryAcquire(now time.Time, charges []Charge) (time.Time, error) {
	backend.mu.Lock()
	defer backend.mu.Unlock()

	var waitUntil time.Time
	for _, charge := range charges {
		rule, exists := backend.rules[charge.Key]
		if !exists {
			return time.Time{}, fmt.Errorf("%w: %q", ErrUnknownRule, charge.Key)
		}
		if charge.Units > rule.Limit {
			return time.Time{}, fmt.Errorf(
				"%w: charge %d exceeds rule %q limit %d",
				ErrInvalidRule, charge.Units, charge.Key, rule.Limit,
			)
		}
		state := backend.bucketFor(rule, now)
		if state.blockedUntil.After(now) && state.blockedUntil.After(waitUntil) {
			waitUntil = state.blockedUntil
		}
		if excess := state.used + charge.Units - rule.Limit; excess > 0 {
			expiresAt := eventExpiration(state.events, excess, rule.Window)
			if expiresAt.After(waitUntil) {
				waitUntil = expiresAt
			}
		}
	}
	if !waitUntil.IsZero() {
		return waitUntil, nil
	}
	for _, charge := range charges {
		state := backend.bucketFor(backend.rules[charge.Key], now)
		state.events = append(state.events, usageEvent{at: now, units: charge.Units})
		state.used += charge.Units
	}
	return time.Time{}, nil
}

func eventExpiration(events []usageEvent, units int, window time.Duration) time.Time {
	for _, event := range events {
		units -= event.units
		if units <= 0 {
			return event.at.Add(window)
		}
	}
	return time.Time{}
}

func (backend *memoryBackend) bucketFor(rule Rule, now time.Time) *bucket {
	state := backend.buckets[rule.Key]
	if state == nil {
		state = &bucket{}
		backend.buckets[rule.Key] = state
	}
	cutoff := now.Add(-rule.Window)
	first := 0
	for first < len(state.events) && !state.events[first].at.After(cutoff) {
		state.used -= state.events[first].units
		first++
	}
	if first > 0 {
		state.events = append([]usageEvent(nil), state.events[first:]...)
	}
	return state
}

func (backend *memoryBackend) ObserveUsed(key string, used int) error {
	if used < 0 {
		return fmt.Errorf("observed usage cannot be negative")
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	rule, exists := backend.rules[key]
	if !exists {
		return fmt.Errorf("%w: %q", ErrUnknownRule, key)
	}
	now := time.Now()
	state := backend.bucketFor(rule, now)
	if used > state.used {
		delta := used - state.used
		state.events = append(state.events, usageEvent{at: now, units: delta})
		state.used = used
	}
	return nil
}

func (backend *memoryBackend) BlockFor(keys []string, duration time.Duration) error {
	if duration <= 0 {
		return fmt.Errorf("block duration must be positive")
	}
	until := time.Now().Add(duration)
	backend.mu.Lock()
	defer backend.mu.Unlock()
	for _, key := range keys {
		if _, exists := backend.rules[key]; !exists {
			return fmt.Errorf("%w: %q", ErrUnknownRule, key)
		}
	}
	for _, key := range keys {
		state := backend.buckets[key]
		if state == nil {
			state = &bucket{}
			backend.buckets[key] = state
		}
		if until.After(state.blockedUntil) {
			state.blockedUntil = until
		}
	}
	return nil
}

func (backend *memoryBackend) Snapshot(key string) (Snapshot, error) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	rule, exists := backend.rules[key]
	if !exists {
		return Snapshot{}, fmt.Errorf("%w: %q", ErrUnknownRule, key)
	}
	state := backend.bucketFor(rule, time.Now())
	windowStart := time.Time{}
	if len(state.events) > 0 {
		windowStart = state.events[0].at
	}
	return Snapshot{Rule: rule, WindowStart: windowStart, Used: state.used, BlockedUntil: state.blockedUntil}, nil
}

func normalizeCharges(charges []Charge) ([]Charge, error) {
	combined := make(map[string]int, len(charges))
	for _, charge := range charges {
		charge.Key = strings.TrimSpace(charge.Key)
		if charge.Key == "" || charge.Units <= 0 {
			return nil, fmt.Errorf("charge key and positive units are required")
		}
		combined[charge.Key] += charge.Units
	}
	result := make([]Charge, 0, len(combined))
	for key, units := range combined {
		result = append(result, Charge{Key: key, Units: units})
	}
	sort.Slice(result, func(left, right int) bool { return result[left].Key < result[right].Key })
	return result, nil
}
