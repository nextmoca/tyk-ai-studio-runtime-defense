package needlepath

import (
	"errors"
	"testing"
	"time"
)

type fakeClock struct {
	current time.Time
}

func (c *fakeClock) now() time.Time { return c.current }

func (c *fakeClock) advance(d time.Duration) { c.current = c.current.Add(d) }

func newTestBreaker(threshold int) (*breaker, *fakeClock) {
	clock := &fakeClock{current: time.Unix(1_000_000, 0)}
	return newBreaker(threshold, 30*time.Second, clock.now), clock
}

var (
	errTimeout = &SelectionError{Class: ClassTimeout, Cause: errors.New("deadline")}
	errAuth    = &SelectionError{Class: ClassAuth, Cause: errors.New("401")}
)

// admit asks the breaker for a request slot and fails the test if it is refused.
func admit(t *testing.T, b *breaker) ticket {
	t.Helper()
	slot, ok := b.allow()
	if !ok {
		t.Fatal("allow() = false, want a request slot")
	}
	return slot
}

func refused(b *breaker) bool {
	_, ok := b.allow()
	return !ok
}

func TestBreakerOpensAfterConsecutiveAvailabilityFailures(t *testing.T) {
	b, _ := newTestBreaker(3)
	for call := 0; call < 3; call++ {
		b.report(admit(t, b), errTimeout)
	}
	if !refused(b) {
		t.Fatal("allow() = true with the circuit open")
	}
}

func TestBreakerCountsOnlyConsecutiveFailures(t *testing.T) {
	b, _ := newTestBreaker(3)
	for _, err := range []error{errTimeout, errTimeout, nil, errTimeout, errTimeout} {
		b.report(admit(t, b), err)
	}
	if refused(b) {
		t.Fatal("allow() = false after a success reset the count")
	}
}

func TestBreakerIgnoresRejectionsThatProveTheAPIIsReachable(t *testing.T) {
	b, _ := newTestBreaker(2)
	for call := 0; call < 5; call++ {
		b.report(admit(t, b), errAuth)
	}
	if refused(b) {
		t.Fatal("allow() = false after non-availability errors")
	}
}

func TestBreakerAdmitsOneProbeAfterTheCooldown(t *testing.T) {
	b, clock := newTestBreaker(1)
	b.report(admit(t, b), errTimeout)

	clock.advance(29 * time.Second)
	if !refused(b) {
		t.Fatal("allow() = true before the cool-down elapsed")
	}
	clock.advance(2 * time.Second)
	probe := admit(t, b)
	if !probe.probe {
		t.Fatal("ticket is not a probe")
	}
	if !refused(b) {
		t.Fatal("allow() = true for a second request while the probe is in flight")
	}
}

func TestBreakerClosesWhenTheProbeSucceeds(t *testing.T) {
	b, clock := newTestBreaker(1)
	b.report(admit(t, b), errTimeout)
	clock.advance(31 * time.Second)

	b.report(admit(t, b), nil)
	admit(t, b)
	admit(t, b)
}

func TestBreakerReopensWhenTheProbeFails(t *testing.T) {
	b, clock := newTestBreaker(1)
	b.report(admit(t, b), errTimeout)
	clock.advance(31 * time.Second)

	b.report(admit(t, b), errTimeout)
	if !refused(b) {
		t.Fatal("allow() = true right after a failed probe")
	}
	clock.advance(31 * time.Second)
	admit(t, b)
}

func TestBreakerReleasesTheProbeWhenTheCallIsCancelled(t *testing.T) {
	b, clock := newTestBreaker(1)
	b.report(admit(t, b), errTimeout)
	clock.advance(31 * time.Second)

	b.report(admit(t, b), &SelectionError{Class: ClassCanceled, Cause: errors.New("cancelled")})
	admit(t, b)
}

func TestBreakerIgnoresStaleResultsFromBeforeTheCircuitOpened(t *testing.T) {
	b, clock := newTestBreaker(2)
	stale := admit(t, b)
	b.report(admit(t, b), errTimeout)
	b.report(admit(t, b), errTimeout)
	clock.advance(31 * time.Second)
	probe := admit(t, b)

	// A request admitted before the circuit opened finishes while the probe is
	// in flight. Neither its cancellation nor its success may release or close
	// the probe's circuit.
	b.report(stale, &SelectionError{Class: ClassCanceled, Cause: errors.New("cancelled")})
	if !refused(b) {
		t.Fatal("stale cancellation released the probe slot")
	}
	b.report(stale, nil)
	if !refused(b) {
		t.Fatal("stale success closed the circuit under the probe")
	}

	b.report(probe, nil)
	admit(t, b)
}

func TestBreakerWithZeroThresholdIsDisabled(t *testing.T) {
	b, _ := newTestBreaker(0)
	for call := 0; call < 10; call++ {
		b.report(admit(t, b), errTimeout)
	}
	admit(t, b)
}
