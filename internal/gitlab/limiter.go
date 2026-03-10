package gitlab

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
)

var errEmptyLimiterKey = errors.New("limiter key is required")

func sleepContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

type instanceLimiterSetOptions struct {
	Concurrency int
	MinInterval time.Duration
	Now         func() time.Time
	Sleep       func(context.Context, time.Duration) error
}

type instanceLimiterSet struct {
	mu      sync.Mutex
	byKey   map[string]*instanceLimiter
	options instanceLimiterSetOptions
}

func newInstanceLimiterSet(opts instanceLimiterSetOptions) *instanceLimiterSet {
	return &instanceLimiterSet{
		byKey:   make(map[string]*instanceLimiter),
		options: opts,
	}
}

func (s *instanceLimiterSet) Acquire(ctx context.Context, key string) (func(), error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, errEmptyLimiterKey
	}

	s.mu.Lock()
	limiter, ok := s.byKey[key]
	if !ok {
		limiter = &instanceLimiter{
			sem:         make(chan struct{}, s.options.Concurrency),
			minInterval: s.options.MinInterval,
			now:         s.options.Now,
			sleep:       s.options.Sleep,
		}
		s.byKey[key] = limiter
	}
	s.mu.Unlock()

	return limiter.Acquire(ctx)
}

type instanceLimiter struct {
	sem chan struct{}

	mu          sync.Mutex
	nextAllowed time.Time

	minInterval time.Duration
	now         func() time.Time
	sleep       func(context.Context, time.Duration) error
}

func (l *instanceLimiter) Acquire(ctx context.Context) (func(), error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case l.sem <- struct{}{}:
	}

	if l.minInterval > 0 {
		for {
			l.mu.Lock()
			now := l.now()
			wait := l.nextAllowed.Sub(now)
			if wait <= 0 {
				l.nextAllowed = now.Add(l.minInterval)
				l.mu.Unlock()
				break
			}
			l.mu.Unlock()

			if err := l.sleep(ctx, wait); err != nil {
				<-l.sem
				return nil, err
			}
		}
	}

	released := false
	return func() {
		if released {
			return
		}
		released = true
		<-l.sem
	}, nil
}
