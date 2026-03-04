package worker

import "time"

type ExponentialBackoff struct {
	Initial time.Duration
	Max     time.Duration
}

func (b ExponentialBackoff) Duration(attempt int32) time.Duration {
	initial := b.Initial
	if initial <= 0 {
		initial = time.Second
	}

	max := b.Max
	if max <= 0 {
		max = 30 * time.Second
	}

	if attempt <= 1 {
		if initial > max {
			return max
		}
		return initial
	}

	delay := initial
	for i := int32(1); i < attempt; i++ {
		if delay >= max {
			return max
		}
		next := delay * 2
		if next <= 0 || next > max {
			return max
		}
		delay = next
	}
	if delay > max {
		return max
	}
	return delay
}
