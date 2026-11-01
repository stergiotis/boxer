// Package loghost is the in-process start hook that wires boxer's
// facts-based logging stack into a running CLI host. One call to Install
// decides whether log lines are persisted: with BOXER_LOG_FACTS set and
// ClickHouse answering, it builds a logbridge.Sink over chstore, fans
// zerolog's global log.Logger out to both the operator-facing writer and
// the Sink, and hands the log viewer a source that reads the rows back
// from boxer.facts. Otherwise no Sink is built and the viewer is told why
// nothing is persisted (ADR-0345, the update of 2026-10-30).
//
// It exists as its own package (rather than living in logbridge) because
// it depends on logviewer, and logviewer already depends on logbridge —
// folding the wiring into logbridge would create an import cycle. loghost
// therefore sits above logbridge, logviewer, factsstore and chstore and
// is the single seam a host binary imports to light up the whole stack.
//
// Typical use from a urfave/cli host — install in the imzero2 subcommand's
// Before so it runs AFTER App.Before installed the configured log.Logger
// via logging.Apply:
//
//	closeLogBridge := logbridge.NopCloser()
//	// ... in the imzero2 command's Before:
//	closeLogBridge = loghost.Install(c.Context)
//	// ... in App.After:
//	_ = closeLogBridge()
package loghost

import (
	"context"
	"io"
	"os"
	"sync"
	"sync/atomic"

	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore/chstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/logbridge"
	"github.com/stergiotis/boxer/public/keelson/runtime/logviewer"
	"github.com/stergiotis/boxer/public/keelson/runtime/runinfo"
	"github.com/stergiotis/boxer/public/observability/logging"
)

// Install rewires log.Logger onto the operator writer and, when log lines
// can be persisted, onto a logbridge.Sink beside it.
//
// Storage selection:
//   - BOXER_LOG_FACTS=1 (or any non-"0" non-empty value) and ClickHouse
//     reachable → a Sink over chstore persists every event to
//     boxer.facts, and the log viewer reads them back from there.
//     BOXER_LOG_FACTS_URL overrides the endpoint.
//   - otherwise → no Sink. The operator writer is installed the same way
//     (so --logFormat/--logFile/--logColor and the structured error
//     marshaler behave identically), and the log viewer says log lines
//     are not persisted and why. Nothing keeps log lines in memory for
//     it: an in-process copy was retired with the viewer's move to
//     boxer.facts.
//
// ctx scopes the store's setup round-trips (chstore.Ping / SetupTable);
// hosts typically pass the context their Action received.
//
// Returns a closer the caller invokes at shutdown, which drains the Sink
// when there is one and restores the previous logger. Always safe to
// call.
func Install(ctx context.Context) (closer func() error) {
	// The run is whatever has been initialised by now, and "" otherwise:
	// Install does not mint one, since it runs in every imzero2 subcommand
	// and a minted run would reach the processes they launch through the
	// environment. The host adopts its run when it has one (AdoptRun).
	runId := currentRun()
	if inst, rErr := runinfo.Get(); rErr == nil {
		runId = inst.RunId
		AdoptRun(runId)
	}
	// Reuse the writer logging.Apply already built (OperatorWriter) as the
	// operator-facing passthrough, so --logFormat/--logFile/--logColor are
	// honoured even after the bridge wraps log.Logger. The bridge feeds it
	// the same wire payload the Sink decodes (CBOR under the binary_log
	// build tag, JSON otherwise); the configured writer decodes that and
	// renders the human-readable form operators expect. Installing the
	// bridge here — after boxer's App.Before ran Apply — is what makes
	// OperatorWriter available, and avoids the v2 ordering hazard that put
	// a stale writer on top of the bridge before.
	passthrough := buildPassthroughWriter()
	store, cfg, target, why := openFactsStore(ctx, runId)
	if store == nil {
		closer = logbridge.InstallGlobal(passthrough, nil)
		logviewer.Register(&logviewer.Source{Run: currentRun, NotPersisted: why})
		log.Info().Str("reason", why).Msg("logbridge: log lines are not persisted")
		return
	}
	rs := newRunStore(store, cfg)
	logStore.Store(rs)
	sink, serr := logbridge.NewSink(rs, logbridge.Config{})
	if serr != nil {
		closer = logbridge.InstallGlobal(passthrough, nil)
		why = "the log bridge could not start: " + serr.Error()
		logviewer.Register(&logviewer.Source{Run: currentRun, NotPersisted: why})
		log.Warn().Err(serr).Msg("logbridge: NewSink failed — log lines are not persisted")
		return
	}
	closer = logbridge.InstallGlobal(passthrough, sink)
	logviewer.Register(&logviewer.Source{Sink: sink, Read: readerOf(store), Run: currentRun})
	log.Info().Str("store", target).Msg("logbridge: log bridge installed")
	return
}

// adoptedRun is the run the host adopted; logStore is the store Install
// built, when it built one.
var (
	adoptedRun atomic.Pointer[string]
	logStore   atomic.Pointer[runStore]
)

// AdoptRun sets the run this process's log rows are stamped with from now
// on (ADR-0191 §SD3) and the run the log viewer's This run scope reads.
// The host calls it once its run identity exists — hostboot, right after
// runinfo.Init. Rows the bridge wrote before carry the unknown run "".
// Safe to call before or without Install, and more than once.
func AdoptRun(runId string) {
	adoptedRun.Store(&runId)
	if rs := logStore.Load(); rs != nil {
		rs.adopt(runId)
	}
}

// currentRun is the adopted run, read by the viewer at query time.
func currentRun() (runId string) {
	if p := adoptedRun.Load(); p != nil {
		runId = *p
	}
	return
}

// runStore is the store the log bridge writes through. A chstore.Store
// stamps the run it was configured with, so adopting a run swaps in a
// store built from the same configuration with that run; the bridge keeps
// one writer, and every other method goes to the store Install built.
type runStore struct {
	factsstore.FactsStoreI
	cfg chstore.Config

	mu  sync.Mutex
	run string
	cur atomic.Pointer[chstore.Store]
}

// newRunStore wraps base, which was built from cfg.
func newRunStore(base *chstore.Store, cfg chstore.Config) (inst *runStore) {
	inst = &runStore{FactsStoreI: base, cfg: cfg, run: cfg.RunId}
	inst.cur.Store(base)
	return
}

func (inst *runStore) adopt(runId string) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.run == runId {
		return
	}
	cfg := inst.cfg
	cfg.RunId = runId
	next, err := chstore.New(cfg)
	if err != nil {
		log.Warn().Err(err).Msg("logbridge: adopting the run failed — log rows keep the previous run")
		return
	}
	inst.run = runId
	inst.cur.Store(next)
}

func (inst *runStore) WriteLog(row factsstore.LogRow) (id uint64, err error) {
	id, err = inst.cur.Load().WriteLog(row)
	return
}

func (inst *runStore) WriteLogs(rows []factsstore.LogRow) (ids []uint64, err error) {
	ids, err = inst.cur.Load().WriteLogs(rows)
	return
}

// readerOf adapts the store's log-row read to the viewer's query.
func readerOf(store *chstore.Store) logviewer.ReadFunc {
	return func(ctx context.Context, q logviewer.Query) (rows []logviewer.Entry, err error) {
		limit := uint32(0)
		if q.Limit > 0 {
			limit = uint32(q.Limit)
		}
		read, err := store.LogRows(ctx, chstore.LogRowsFilter{
			Since: q.Since, Levels: q.Levels, AppContains: q.App, MessageContains: q.Message,
			RunId: q.RunId, InstanceKey: q.InstanceKey, Limit: limit,
		})
		if err != nil {
			return
		}
		rows = make([]logviewer.Entry, len(read))
		for i, r := range read {
			rows[i] = logviewer.Entry{Row: r.Row, RunId: r.RunId}
		}
		return
	}
}

// buildPassthroughWriter returns the operator-facing writer the bridge
// fans events to alongside the Sink. It reuses logging.OperatorWriter()
// — the exact writer logging.Apply configured from
// --logFormat/--logFile/--logColor — so the operator stream renders
// identically to the primary logger and honors the full logger config:
// every format, the file destination, and color. Earlier this function
// hand-rolled a console-only writer that silently dropped --logFile and
// every non-console format, and once even drifted from the primary
// console writer's field formatter (rendering bools as raw
// `data:application/cbor;base64,…` blobs).
//
// Falls back to raw os.Stderr only when Apply configured no writer (e.g.
// a host that wires the bridge without logging.LoggingFlags) so the
// bridge still installs and the operator sees something.
//
// The writer is wrapped in fullWriteAdapter so MultiLevelWriter does NOT
// short-circuit the Sink. See fullWriteAdapter's doc for the gory detail.
func buildPassthroughWriter() (w io.Writer) {
	inner := logging.OperatorWriter()
	if inner == nil {
		inner = os.Stderr
	}
	w = fullWriteAdapter{inner: inner}
	return
}

// fullWriteAdapter wraps a writer so it always reports `len(p)` bytes
// written, regardless of how many output bytes the inner writer
// actually produced. Necessary because zerolog.ConsoleWriter.Write
// returns the *decoded* event length, not the input CBOR length —
// zerolog's multiLevelWriter treats `n != len(p)` as io.ErrShortWrite
// and, critically, SHORT-CIRCUITS subsequent writers in the chain
// (writer.go:84-92 — `if err == nil` guard). Without this adapter, a
// ConsoleWriter sitting upstream of the bridge Sink would silently
// drop every event from the Sink, leaving boxer.facts without them
// even though the operator sees pretty logs on stderr — exactly the
// "logs on stderr, blank viewer" symptom that motivated the fix.
type fullWriteAdapter struct {
	inner io.Writer
}

func (w fullWriteAdapter) Write(p []byte) (n int, err error) {
	_, err = w.inner.Write(p)
	n = len(p)
	return
}

// openFactsStore returns the ClickHouse store log lines are persisted
// to, or nil and why not: BOXER_LOG_FACTS unset, or set with ClickHouse
// not answering. target names the endpoint for the start-up line.
//
// The server and its credentials come from chstore.ConfigFromEnv (the
// CLICKHOUSE_* entries), with BOXER_LOG_FACTS_URL overriding the endpoint
// for this store alone.
func openFactsStore(ctx context.Context, runId string) (store *chstore.Store, cfg chstore.Config, target string, why string) {
	name := chstore.LogFactsEnabled.Spec().Name
	v := chstore.LogFactsEnabled.Get()
	if v == "" || v == "0" {
		why = name + " is not set"
		return
	}
	cfg = chstore.ConfigFromEnv()
	if url := chstore.LogFactsURL.Get(); url != "" {
		cfg.URL = url
	}
	// Stamp the run on every row this store writes (ADR-0191 §SD3); the
	// membership is what lets a reader filter on it without unpacking the
	// field lane. "" until the host adopts its run.
	cfg.RunId = runId
	chStore, err := chstore.New(cfg)
	if err != nil {
		log.Warn().Err(err).Msg("logbridge: chstore.New failed — log lines are not persisted")
		why = name + " is set but the ClickHouse store could not be configured"
		return
	}
	if perr := chStore.Ping(ctx); perr != nil {
		log.Warn().Err(perr).Str("url", cfg.URL).Msg("logbridge: ClickHouse unreachable — log lines are not persisted")
		why = name + " is set but ClickHouse at " + cfg.URL + " did not answer; the status line says whether the audit trail is durable"
		return
	}
	if serr := chStore.SetupTable(ctx, ""); serr != nil {
		log.Warn().Err(serr).Msg("logbridge: SetupTable failed — log lines are not persisted")
		why = name + " is set but boxer.facts could not be set up on " + cfg.URL
		return
	}
	store = chStore
	target = "clickhouse@" + cfg.URL
	return
}
