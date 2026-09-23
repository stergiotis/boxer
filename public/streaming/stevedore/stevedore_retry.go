package stevedore

import (
	"context"
	"math/rand/v2"
	"time"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// RetryPolicy bounds the in-place retry of a transient error (ADR-0252 §SD3):
// exponential backoff from Base, capped at Max, with a jitter fraction so a
// pool of processes does not retry in step.
type RetryPolicy struct {
	// Attempts is how many times the work runs at most; one means no retry.
	Attempts uint32
	Base     time.Duration
	Max      time.Duration
	// Jitter is the fraction of each delay drawn at random, in [0, 1].
	Jitter float64
}

// DefaultRetryPolicy is three attempts over a few seconds — enough for a
// hiccup, short enough to stay under a framework's per-message timeout.
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{Attempts: 3, Base: 200 * time.Millisecond, Max: 2 * time.Second, Jitter: 0.2}
}

// Delay is the wait before attempt n (from one) runs again. A zero Base
// takes the default policy's, so a policy that only sets Attempts still
// backs off; a zero Max leaves the growth uncapped.
func (inst RetryPolicy) Delay(n uint32) time.Duration {
	d := inst.Base
	if d <= 0 {
		d = DefaultRetryPolicy().Base
	}
	for i := uint32(1); i < n && (inst.Max <= 0 || d < inst.Max); i++ {
		d *= 2
	}
	if inst.Max > 0 && d > inst.Max {
		d = inst.Max
	}
	if inst.Jitter > 0 && d > 0 {
		d = time.Duration(float64(d) * (1 - inst.Jitter*rand.Float64()))
	}
	return d
}

// Retry runs fn until it succeeds, returns a permanent error, exhausts the
// policy, or the context ends. The returned error is the last one, wrapped
// with the attempt count when the policy ran out.
func Retry(ctx context.Context, pol RetryPolicy, fn func(ctx context.Context) error) (err error) {
	attempts := pol.Attempts
	if attempts == 0 {
		attempts = 1
	}
	for n := uint32(1); ; n++ {
		err = fn(ctx)
		if err == nil || ClassOf(err) != ClassTransient {
			return
		}
		if n >= attempts {
			return eb.Build().Uint32("attempts", n).Errorf("attempts exhausted: %w", err)
		}
		if ctx.Err() != nil {
			return eb.Build().Uint32("attempts", n).Errorf("cancelled between attempts: %w", err)
		}
		t := time.NewTimer(pol.Delay(n))
		select {
		case <-ctx.Done():
			t.Stop()
			return eb.Build().Uint32("attempts", n).Errorf("cancelled while waiting to retry: %w", err)
		case <-t.C:
		}
	}
}
