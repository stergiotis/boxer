package play

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryengine"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryengine/chserver"
	"github.com/stergiotis/boxer/public/keelson/runtime/runstream"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// play_dispatch_reach.go — ADR-0145 §SD5: an engine that is not this
// process's own plane may serve a confined run only once it has
// DEMONSTRATED that it can fetch from this plane. R3's rule, applied to the
// one wall boxer owns.
//
// The demonstration is the E6 probe, which had no consumer until now: mint a
// single-use nonce URL, make the engine evaluate a statement that fetches
// it, then ask whether it was fetched. A tunnelled engine somewhere else
// cannot fetch it, so the proof fails and the run stays refused — the safe
// direction.
//
// # What a proof does and does not mean
//
// It fails safe against ACCIDENT: an endpoint pinned at a server elsewhere
// cannot reach this loopback, so nothing is authorised. It does NOT defend
// against a deliberately constructed reverse tunnel, which would let a
// remote engine fetch this plane and pass. That is consistent with the
// disk-only threat model of ADR-0134 §SD2 — an adversary with local
// privilege is already out of scope — but a proof must not be read as a
// security boundary. It is a misconfiguration wall.
//
// # What this unlocks today: nothing, deliberately
//
// A confined statement names `keelson('<handle>')`, and the macro is
// resolved by THIS process's /query endpoint — an external server does not
// know the function, so such a run fails there whether or not it is proven.
// The mechanism is here because ADR-0145's C1 criterion is that the wall
// survives `keelson()` becoming a native, server-resolved table function,
// which is the recorded direction. Until then a proof widens nothing, and
// the wall's real work is the identity exemption in confine.

const (
	// reachProofTTL bounds how long a demonstration is honoured. Shorter
	// than a session and far longer than the probe round trip: a proof is a
	// statement about a moment, and re-proving costs one small query.
	reachProofTTL = 5 * time.Minute
	// reachProbeTimeout bounds one demonstration. It runs on a background
	// goroutine, so the ceiling exists to retire a stuck attempt rather than
	// to keep anything waiting.
	reachProbeTimeout = 10 * time.Second
	// reachRunWait bounds how long a run waits, off the frame, for a
	// demonstration already under way before its dispatch is decided again.
	reachRunWait = 5 * time.Second
	// reachRunWaitText is reachRunWait in a refusal's words.
	reachRunWaitText = "5s"
)

// reachProver remembers which endpoints have demonstrated they can fetch
// from this process's loopback plane.
//
// Safe for concurrent use: lane goroutines read it while a probe writes.
type reachProver struct {
	mu       sync.Mutex
	proven   map[string]time.Time // endpoint -> proof expiry
	inFlight map[string]struct{}  // endpoints being probed right now
	// changed is closed and replaced whenever a proof is recorded or a
	// demonstration ends, waking every await.
	changed chan struct{}
	// runWait is how long a run waits for a demonstration under way;
	// reachRunWait unless a test shortens it.
	runWait time.Duration
	// now is the clock, injectable so a test can expire a proof without
	// waiting out the TTL.
	now func() time.Time
}

func newReachProver() (inst *reachProver) {
	inst = &reachProver{
		proven:   make(map[string]time.Time, 2),
		inFlight: make(map[string]struct{}, 2),
		changed:  make(chan struct{}),
		runWait:  reachRunWait,
		now:      time.Now,
	}
	return
}

// isProven reports whether endpoint holds an unexpired demonstration. It
// only reads the cache — proving is never done on a dispatch path, because
// dispatch is consulted by things that must not perform I/O (applet
// stamping resolves a decision purely to classify a buffer).
func (inst *reachProver) isProven(endpoint string) (yes bool) {
	// A nil prover proves nothing, which is the safe answer rather than a
	// panic: a Client built as a literal (tests do) then simply has no
	// demonstrations, and every confined run stays refused.
	if inst == nil || endpoint == "" {
		return
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	expiry, ok := inst.proven[endpoint]
	if !ok {
		return
	}
	if !expiry.After(inst.now()) {
		delete(inst.proven, endpoint)
		return
	}
	yes = true
	return
}

// record marks endpoint as proven from now until the TTL.
func (inst *reachProver) record(endpoint string) {
	if inst == nil {
		return
	}
	inst.mu.Lock()
	inst.proven[endpoint] = inst.now().Add(reachProofTTL)
	inst.signalLocked()
	inst.mu.Unlock()
}

// signalLocked wakes every await. The caller holds mu.
func (inst *reachProver) signalLocked() {
	if inst.changed != nil {
		close(inst.changed)
	}
	inst.changed = make(chan struct{})
}

// await waits until endpoint is proven, the demonstration under way for it
// ends, ctx is done or bound passes, whichever comes first, and reports
// whether endpoint is proven then. With no demonstration under way it
// answers at once.
func (inst *reachProver) await(ctx context.Context, endpoint string, bound time.Duration) (proven bool) {
	if inst == nil || endpoint == "" {
		return
	}
	timer := time.NewTimer(bound)
	defer timer.Stop()
	for {
		inst.mu.Lock()
		_, busy := inst.inFlight[endpoint]
		if inst.changed == nil {
			inst.changed = make(chan struct{})
		}
		changed := inst.changed
		inst.mu.Unlock()
		if proven = inst.isProven(endpoint); proven || !busy {
			return
		}
		select {
		case <-changed:
		case <-timer.C:
			return inst.isProven(endpoint)
		case <-ctx.Done():
			return inst.isProven(endpoint)
		}
	}
}

// begin claims the right to probe endpoint, so concurrent lanes hitting the
// same refusal produce one demonstration rather than one each. done releases
// the claim; ok is false when a probe is already under way.
func (inst *reachProver) begin(endpoint string) (done func(), ok bool) {
	if inst == nil {
		return
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if _, busy := inst.inFlight[endpoint]; busy {
		return
	}
	inst.inFlight[endpoint] = struct{}{}
	ok = true
	done = func() {
		inst.mu.Lock()
		delete(inst.inFlight, endpoint)
		inst.signalLocked()
		inst.mu.Unlock()
	}
	return
}

// runnerFunc executes one statement on the engine under test and reports
// whether it ran. Injectable so the prover's policy can be tested without a
// server.
type runnerFunc func(ctx context.Context, endpoint string, sql string) (err error)

// prove performs one demonstration: mint a nonce, make the engine fetch it,
// and record the proof if it did.
//
// A failure is not an error state to escalate — it is the answer "not
// proven", which is what the wall then acts on. Only the mint failing is
// reported, since that is this process's own fault rather than the engine's.
func (inst *reachProver) prove(ctx context.Context, endpoint string, run runnerFunc) (proven bool, err error) {
	if inst == nil {
		err = eh.Errorf("play: no reachability prover")
		return
	}
	probe, ok := introspect.LocalProbe()
	if !ok {
		err = eh.Errorf("play: no local introspection plane to prove reachability to")
		return
	}
	nonce, url, mintErr := probe.MintProbe()
	if mintErr != nil {
		err = eh.Errorf("play: mint reachability probe: %w", mintErr)
		return
	}
	// The engine fetches the nonce as ordinary data. A statement it cannot
	// run, or a URL it cannot reach, both come back as "not fetched" — the
	// check is what decides, never the shape of the failure.
	runErr := run(ctx, endpoint, probeStatement(url))
	proven = probe.CheckProbe(nonce)
	if proven {
		inst.record(endpoint)
		return
	}
	if runErr != nil {
		log.Debug().Err(runErr).Str("endpoint", env.RedactURL(endpoint)).
			Msg("play: reachability probe statement failed; endpoint stays unproven")
	}
	return
}

// probeStatement is what the engine under test evaluates. LineAsString needs
// no structure argument, so the statement carries nothing that could fail
// for a reason unrelated to reachability.
func probeStatement(nonceURL string) (sql string) {
	sql = "SELECT count() FROM url('" + nonceURL + "','LineAsString')"
	return
}

// proveReachInBackground starts one demonstration for endpoint, deduped, and
// returns immediately.
//
// It is called from a refusal rather than before one: the refused run is
// already not happening, so nothing waits, and a re-run lands on a decision
// that can see the proof. Making the dispatch path itself block on a probe
// would put a network round trip inside a function that applet stamping
// calls to classify a buffer.
func (inst *Client) proveReachInBackground(endpoint string) {
	if endpoint == "" || inst.reach.isProven(endpoint) {
		return
	}
	done, ok := inst.reach.begin(endpoint)
	if !ok {
		return
	}
	started := inst.Go(func(ctx context.Context) {
		defer done()
		ctx, cancel := context.WithTimeout(ctx, reachProbeTimeout)
		defer cancel()
		proven, err := inst.reach.prove(ctx, endpoint, inst.runProbeStatement)
		switch {
		case err != nil:
			log.Debug().Err(err).Str("endpoint", env.RedactURL(endpoint)).Msg("play: reachability probe unavailable")
		case proven:
			log.Info().Str("endpoint", env.RedactURL(endpoint)).
				Msg("play: endpoint demonstrated it can reach this process's introspection plane")
		default:
			log.Debug().Str("endpoint", env.RedactURL(endpoint)).
				Msg("play: endpoint did not fetch the probe; confined runs stay refused")
		}
	})
	if !started {
		done()
	}
}

// runProbeStatement executes the probe on one endpoint through an ordinary
// delivery. The statement is not confined — it reads a nonce URL, not sealed
// data — so it passes the engine's own gate without a declaration.
func (inst *Client) runProbeStatement(ctx context.Context, endpoint string, sql string) (err error) {
	eng, err := chserver.New(chserver.Config{
		Endpoint:   endpoint,
		User:       inst.cfg.User,
		Password:   inst.cfg.Password,
		HTTPClient: inst.http,
	})
	if err != nil {
		return
	}
	st, _, err := eng.Deliver(ctx, queryengine.Request{SQL: sql, Format: "TabSeparated"})
	if err != nil {
		return
	}
	defer func() { _ = st.Close() }()
	_, term, err := queryengine.Collect(st)
	if err != nil {
		return
	}
	if term.State == runstream.TerminalFailed {
		err = term.Err
	}
	return
}

// dispatchAwaitingReach is a run's dispatch, made on the run's own
// goroutine: decide, and when the statement was refused only because its
// endpoint has not demonstrated it may see sealed data while a
// demonstration is under way, wait for that demonstration up to
// reachRunWait under ctx and decide again. A statement still refused then
// says the run waited. decide is the dispatch the run would make.
func (inst *Client) dispatchAwaitingReach(ctx context.Context, decide func() dispatchDecision) (dec dispatchDecision) {
	dec = decide()
	if dec.pending == nil {
		return
	}
	bound := reachRunWait
	if inst.reach != nil && inst.reach.runWait > 0 {
		bound = inst.reach.runWait
	}
	inst.reach.await(ctx, dec.pending.targetURL, bound)
	dec = decide()
	if dec.pending != nil {
		dec.reason = strings.TrimSuffix(dec.reason, reachStartedNote) +
			"; the run waited up to " + reachRunWaitText + " for the demonstration under way, which did not succeed in that time"
		dec.pending = nil
	}
	return
}
