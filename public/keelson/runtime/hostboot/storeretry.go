package hostboot

import (
	"context"
	"math/rand/v2"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/keelson/runtime/trail"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// The retry of a store a boot could not open (ADR-0408 §SD3). The wait
// before the first retry is storeRetryFirst; each failure doubles it up to
// storeRetryCeiling, and each wait is drawn between half and all of its
// value, so hosts that booted together do not retry together. A server
// that stays down sees at most one attempt a minute from a host, and a
// server that comes back is noticed within about a minute. They are not
// settings: they bound only how soon a recovered server is noticed.
// Variables, not constants, so the package's tests can shorten them.
var (
	storeRetryFirst   = 2 * time.Second
	storeRetryCeiling = time.Minute
)

// storeRetry is the background task of a host booted with its trail held:
// it retries the open until the trail lands what it holds, then records
// the span it was held and stops. Run it once; stop ends it at shutdown.
type storeRetry struct {
	trail *trail.Recorder
	exec  recordstore.ExecutorI
	// cause is why the boot's open failed, bounded.
	cause string
	log   zerolog.Logger
	// ledger rebuilds the model service's usage ledger from the trail's
	// store before the trail is released; nil without a model service.
	ledger func(ctx context.Context) (rows int, err error)
	// opened runs once the trail is durable, with the executor it
	// reached the server through.
	opened func(exec recordstore.ExecutorI)

	first, ceiling, bound time.Duration

	ledgerDone bool
	cancel     context.CancelFunc
	done       chan struct{}
}

// start runs the retry under ctx on a goroutine of its own.
func (inst *storeRetry) start(ctx context.Context) {
	ctx, inst.cancel = context.WithCancel(ctx)
	inst.done = make(chan struct{})
	// The first wait is announced before start returns, so the reason a
	// refusal carries names the next attempt from the host's first frame.
	d := inst.announce(inst.first, 0, nil)
	go inst.run(ctx, d)
}

// stop ends the retry, an attempt in flight included, and waits for it.
func (inst *storeRetry) stop() {
	inst.cancel()
	<-inst.done
}

// announce draws the next wait from wait and puts it in the trail's
// reason with the attempts so far and the last error.
func (inst *storeRetry) announce(wait time.Duration, attempts int, last error) (d time.Duration) {
	d = jittered(wait)
	inst.trail.SetHeldWhy(heldReason(inst.trail.HeldSince(), inst.cause, attempts, last, time.Now().Add(d)))
	return
}

func (inst *storeRetry) run(ctx context.Context, d time.Duration) {
	defer close(inst.done)
	since := inst.trail.HeldSince()
	wait := inst.first
	attempts := 0
	for {
		t := time.NewTimer(d)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
		err := inst.attempt(ctx)
		if err == nil {
			inst.recordOpened(since, attempts+1)
			return
		}
		if ctx.Err() != nil {
			return
		}
		attempts++
		wait = min(2*wait, inst.ceiling)
		d = inst.announce(wait, attempts, err)
		inst.log.Warn().Err(err).Int("attempts", attempts).Time("since", since).Dur("next", d).
			Msg("store: the trail's store has not opened yet; the host keeps retrying, and the trail holds its rows meanwhile")
	}
}

// attempt is one try within the bound: the ledger rebuilt from the trail's
// store, once, then the trail released.
func (inst *storeRetry) attempt(ctx context.Context) (err error) {
	ctx, cancel := context.WithTimeout(ctx, inst.bound)
	defer cancel()
	if inst.ledger != nil && !inst.ledgerDone {
		if _, err = inst.ledger(ctx); err != nil {
			return eh.Errorf("rebuild the usage ledger from the trail: %w", err)
		}
		inst.ledgerDone = true
	}
	return inst.trail.Release(ctx)
}

// recordOpened logs the recovery, writes the span the trail was held as
// its own row, and hands the executor on.
func (inst *storeRetry) recordOpened(since time.Time, attempts int) {
	at := time.Now().UTC()
	refused := inst.trail.Counts().Refused
	inst.log.Info().Time("since", since).Dur("held", at.Sub(since)).Int("attempts", attempts).Uint64("refused", refused).
		Msg("store: the trail reached ClickHouse and landed what it held; model calls and egress fetches are admitted again. App state, the run journal and the watchbill worker stay as they booted until the host restarts")
	row := heldRow(since, at, attempts, refused, inst.cause)
	if err := inst.trail.Event(context.Background(), at, trail.Context{Origin: inst.trail.OriginOf("", 0)}, row); err != nil {
		inst.log.Warn().Err(err).Msg("store: write the trail's held event")
	}
	inst.trail.FlushSoon()
	if inst.opened != nil {
		inst.opened(inst.exec)
	}
}

// heldRow is the trail's own row for the span it held (ADR-0408 §SD4):
// from and to, the attempts it took, the work refused meanwhile, and why
// the boot's open failed.
func heldRow(since time.Time, at time.Time, attempts int, refused uint64, cause string) (row trail.AuditEvent) {
	return trail.AuditEvent{Domain: trail.TrailDomain, Action: trail.ActionAuditHeld, Outcome: trail.OutcomeFailed, Retention: trail.RetentionTrail,
		AttrKeys: []string{"from", "to", "attempts", "refused", "cause"},
		AttrValues: []string{since.UTC().Format(time.RFC3339Nano), at.UTC().Format(time.RFC3339Nano), strconv.Itoa(attempts),
			strconv.FormatUint(refused, 10), trail.Bounded(cause)}}
}

// heldReason is what the held trail says, and so what a refused model call
// or fetch carries: that the store has not opened, since when, that the
// host is retrying and when next, and that no restart is needed. A zero
// next is a host still booting, whose retry starts once it is up.
func heldReason(since time.Time, cause string, attempts int, last error, next time.Time) (why string) {
	why = "the host's ClickHouse store has not opened since " + clock(since) + " (at boot: " + clip(cause) + ")"
	if next.IsZero() {
		return why + "; the host retries it in the background once it has started. No restart is needed: try again once it opens"
	}
	why += "; the host is retrying it in the background"
	if attempts > 0 && last != nil {
		why += " (" + strconv.Itoa(attempts) + " attempts so far, the last: " + clip(last.Error()) + ")"
	}
	why += ", the next attempt at " + clock(next) + ". No restart is needed: try again once it opens"
	return
}

// clock is t as a reader compares it with a clock: UTC, to the second.
func clock(t time.Time) string { return t.UTC().Format("15:04:05") + " UTC" }

// clip keeps a reason short enough for a status line.
func clip(s string) string {
	const max = 160
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	return string(r[:max-1]) + "…"
}

// jittered is d drawn uniformly between its half and itself.
func jittered(d time.Duration) time.Duration {
	if d <= 1 {
		return d
	}
	half := d / 2
	return half + rand.N(d-half+1)
}
