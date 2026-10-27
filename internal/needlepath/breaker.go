package needlepath

import (
	"sync"
	"time"
)

// breaker stops calling the hosted API after a run of availability failures,
// so a slow or unreachable API costs each gateway request nothing while the
// circuit is open. After the cool-down it admits one probe request; the probe's
// result closes the circuit or reopens it. A threshold of zero disables it.
type breaker struct {
	mu        sync.Mutex
	threshold int
	cooldown  time.Duration
	now       func() time.Time
	failures  int
	open      bool
	openedAt  time.Time
	probing   bool
	epoch     uint64
}

// ticket identifies the request an allow() granted, so that a late result from
// a request admitted in an earlier circuit state cannot close, reopen, or
// release the probe that belongs to a later one.
type ticket struct {
	probe bool
	epoch uint64
}

func newBreaker(threshold int, cooldown time.Duration, now func() time.Time) *breaker {
	if now == nil {
		now = time.Now
	}
	return &breaker{threshold: threshold, cooldown: cooldown, now: now}
}

// allow reports whether a request may be sent. Every allowed request must be
// followed by exactly one report with the returned ticket.
func (b *breaker) allow() (ticket, bool) {
	if b.threshold <= 0 {
		return ticket{}, true
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.open {
		return ticket{epoch: b.epoch}, true
	}
	if b.now().Sub(b.openedAt) < b.cooldown || b.probing {
		return ticket{}, false
	}
	b.probing = true
	return ticket{probe: true, epoch: b.epoch}, true
}

// report records the result of an allowed request. Timeouts, transport errors
// and 5xx responses count against the API. Any other answer, including
// rejections such as 401 or 429, shows the API is reachable. A cancelled call
// says nothing either way.
func (b *breaker) report(t ticket, err error) {
	if b.threshold <= 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	class := ClassOf(err)
	switch {
	case t.probe:
		b.reportProbe(err == nil || !isAvailabilityFailure(class), class == ClassCanceled && err != nil)
	case t.epoch != b.epoch || b.open:
		// Admitted before the circuit last changed state; its result is stale.
	case err == nil || (!isAvailabilityFailure(class) && class != ClassCanceled):
		b.failures = 0
	case isAvailabilityFailure(class):
		b.failures++
		if b.failures >= b.threshold {
			b.trip()
		}
	}
}

func (b *breaker) reportProbe(reachable, cancelled bool) {
	b.probing = false
	switch {
	case cancelled:
		// The probe said nothing about the API; the next request may probe.
	case reachable:
		b.open = false
		b.failures = 0
		b.epoch++
	default:
		b.openedAt = b.now()
	}
}

func (b *breaker) trip() {
	b.open = true
	b.openedAt = b.now()
	b.epoch++
}

func isAvailabilityFailure(class ErrorClass) bool {
	return class == ClassTimeout || class == ClassTransport || class == ClassServer
}
