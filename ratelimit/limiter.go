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

// Rule은 하나의 고정 구간 요청 제한을 정의한다.
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

type bucket struct {
	windowStart  time.Time
	used         int
	blockedUntil time.Time
}

// Snapshot은 특정 규칙의 현재 로컬 상태다.
type Snapshot struct {
	Rule         Rule
	WindowStart  time.Time
	Used         int
	BlockedUntil time.Time
}

// Limiter는 여러 규칙의 차감을 원자적으로 처리하는 요청 제한기다.
type Limiter struct {
	mu      sync.Mutex
	rules   map[string]Rule
	buckets map[string]*bucket
}

// New는 검증된 요청 제한기를 생성한다.
func New(rules ...Rule) (*Limiter, error) {
	limiter := &Limiter{
		rules:   make(map[string]Rule, len(rules)),
		buckets: make(map[string]*bucket, len(rules)),
	}
	for _, rule := range rules {
		if err := limiter.SetRule(rule); err != nil {
			return nil, err
		}
	}
	return limiter, nil
}

// SetRule은 규칙을 추가하거나 갱신한다.
// 구간이나 한도가 달라지면 기존 로컬 사용량을 안전하게 초기화한다.
func (limiter *Limiter) SetRule(rule Rule) error {
	rule.Key = strings.TrimSpace(rule.Key)
	if rule.Key == "" || rule.Limit <= 0 || rule.Window <= 0 {
		return fmt.Errorf("%w: key, positive limit, and positive window are required", ErrInvalidRule)
	}

	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	previous, exists := limiter.rules[rule.Key]
	limiter.rules[rule.Key] = rule
	if !exists || previous.Limit != rule.Limit || previous.Window != rule.Window {
		limiter.buckets[rule.Key] = &bucket{}
	}
	return nil
}

// Wait는 모든 차감 규칙에 여유가 생길 때까지 기다린 뒤 한 번에 차감한다.
// 일부 규칙만 먼저 차감하는 일이 없도록 검사와 반영을 같은 잠금에서 수행한다.
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

	for {
		now := time.Now()
		waitUntil, err := limiter.tryAcquire(now, normalized)
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

func (limiter *Limiter) tryAcquire(now time.Time, charges []Charge) (time.Time, error) {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()

	var waitUntil time.Time
	for _, charge := range charges {
		rule, exists := limiter.rules[charge.Key]
		if !exists {
			return time.Time{}, fmt.Errorf("%w: %q", ErrUnknownRule, charge.Key)
		}
		if charge.Units > rule.Limit {
			return time.Time{}, fmt.Errorf(
				"%w: charge %d exceeds rule %q limit %d",
				ErrInvalidRule,
				charge.Units,
				charge.Key,
				rule.Limit,
			)
		}
		state := limiter.bucketFor(rule, now)
		if state.blockedUntil.After(waitUntil) && state.blockedUntil.After(now) {
			waitUntil = state.blockedUntil
		}
		if state.used+charge.Units > rule.Limit {
			windowEnd := state.windowStart.Add(rule.Window)
			if windowEnd.After(waitUntil) {
				waitUntil = windowEnd
			}
		}
	}
	if !waitUntil.IsZero() {
		return waitUntil, nil
	}
	for _, charge := range charges {
		rule := limiter.rules[charge.Key]
		limiter.bucketFor(rule, now).used += charge.Units
	}
	return time.Time{}, nil
}

func (limiter *Limiter) bucketFor(rule Rule, now time.Time) *bucket {
	state, exists := limiter.buckets[rule.Key]
	if !exists {
		state = &bucket{}
		limiter.buckets[rule.Key] = state
	}
	windowStart := now.Truncate(rule.Window)
	if state.windowStart.IsZero() || !state.windowStart.Equal(windowStart) {
		state.windowStart = windowStart
		state.used = 0
	}
	return state
}

// ObserveUsed는 거래소 응답 헤더에서 관측한 사용량이 로컬 값보다 크면 반영한다.
func (limiter *Limiter) ObserveUsed(key string, used int) error {
	if used < 0 {
		return fmt.Errorf("observed usage cannot be negative")
	}
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	rule, exists := limiter.rules[key]
	if !exists {
		return fmt.Errorf("%w: %q", ErrUnknownRule, key)
	}
	state := limiter.bucketFor(rule, time.Now())
	if used > state.used {
		state.used = used
	}
	return nil
}

// BlockFor는 거래소가 지시한 시간 동안 지정한 규칙의 신규 요청을 막는다.
func (limiter *Limiter) BlockFor(keys []string, duration time.Duration) error {
	if duration <= 0 {
		return fmt.Errorf("block duration must be positive")
	}
	until := time.Now().Add(duration)
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	for _, key := range keys {
		if _, exists := limiter.rules[key]; !exists {
			return fmt.Errorf("%w: %q", ErrUnknownRule, key)
		}
	}
	for _, key := range keys {
		state := limiter.buckets[key]
		if state == nil {
			state = &bucket{}
			limiter.buckets[key] = state
		}
		if until.After(state.blockedUntil) {
			state.blockedUntil = until
		}
	}
	return nil
}

// Snapshot은 테스트와 관측을 위한 규칙 상태 복사본을 반환한다.
func (limiter *Limiter) Snapshot(key string) (Snapshot, error) {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	rule, exists := limiter.rules[key]
	if !exists {
		return Snapshot{}, fmt.Errorf("%w: %q", ErrUnknownRule, key)
	}
	state := limiter.bucketFor(rule, time.Now())
	return Snapshot{
		Rule:         rule,
		WindowStart:  state.windowStart,
		Used:         state.used,
		BlockedUntil: state.blockedUntil,
	}, nil
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
	sort.Slice(result, func(left, right int) bool {
		return result[left].Key < result[right].Key
	})
	return result, nil
}
