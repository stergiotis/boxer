// Package loghost is the in-process start hook that wires boxer's
// facts-based logging stack into a running CLI host. One call to Install
// builds a logbridge.Sink over a FactsStoreI, fans zerolog's global
// log.Logger out to both the operator-facing writer and the Sink, and
// hands the Sink to the logviewer AppI so the in-process log viewer has a
// live tail to read from.
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

	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore/chstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/logbridge"
	"github.com/stergiotis/boxer/public/keelson/runtime/logviewer"
	"github.com/stergiotis/boxer/public/observability/logging"
)

// Install builds a logbridge.Sink and rewires log.Logger to fan every
// event out to both the operator writer and the Sink. The Sink is ALWAYS
// installed so the in-process logviewer (and the logdemo companion app)
// have a live tail to read from out of the box — no env var required.
//
// Storage backend selection:
//   - BOXER_LOG_FACTS=1 (or any non-"0" non-empty value) → chstore,
//     persisting every event to boxer.facts. BOXER_LOG_FACTS_URL
//     overrides the default localhost CH URL. If the CH endpoint is
//     unreachable, we log a warning and fall back to in-memory.
//   - default (BOXER_LOG_FACTS unset or "0") → InMemoryFactsStore.
//     Events still land in the Sink's tail ring (which the viewer
//     reads); the flush ring drains into RAM that the process drops
//     on exit. No external dependency.
//
// ctx scopes the store's setup round-trips (chstore.Ping / SetupTable);
// hosts typically pass the cli.Context's Context.
//
// Returns a closer the caller invokes at shutdown. Always safe to
// call (closes the Sink either way).
func Install(ctx context.Context) (closer func() error) {
	closer = logbridge.NopCloser()
	store, storeKind := selectFactsStore(ctx)
	sink, serr := logbridge.NewSink(store, logbridge.Config{})
	if serr != nil {
		log.Warn().Err(serr).Msg("logbridge: NewSink failed — continuing without facts capture")
		return
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
	closer = logbridge.InstallGlobal(passthrough, sink)
	// Hand the same Sink to the logviewer AppI so the operator can
	// tail the live log stream from inside the running process. The
	// AppI is already registered via side-effect import (init()) when
	// `logviewer` is in the import graph; RegisterSink wires the data
	// source it reads from on each frame.
	logviewer.RegisterSink(sink)
	log.Info().Str("store", storeKind).Msg("logbridge: log bridge installed")
	return
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
// drop every event from the Sink, leaving the logviewer's tail empty
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

// selectFactsStore picks the FactsStoreI backing the log bridge. When
// BOXER_LOG_FACTS is enabled and the ClickHouse endpoint reachable we
// route through chstore so events persist. Otherwise we fall back to
// an in-memory store — the Sink and its tail ring still work, which
// is all the logviewer / logdemo demo needs.
func selectFactsStore(ctx context.Context) (store factsstore.FactsStoreI, kind string) {
	v := chstore.LogFactsEnabled.Get()
	if v == "" || v == "0" {
		store = factsstore.NewInMemoryFactsStore()
		kind = "memory"
		return
	}
	cfg := chstore.Defaults()
	if url := chstore.LogFactsURL.Get(); url != "" {
		cfg.URL = url
	}
	chStore, err := chstore.New(cfg)
	if err != nil {
		log.Warn().Err(err).Msg("logbridge: chstore.New failed — falling back to in-memory facts store")
		store = factsstore.NewInMemoryFactsStore()
		kind = "memory"
		return
	}
	if perr := chStore.Ping(ctx); perr != nil {
		log.Warn().Err(perr).Str("url", cfg.URL).Msg("logbridge: ClickHouse unreachable — falling back to in-memory facts store")
		store = factsstore.NewInMemoryFactsStore()
		kind = "memory"
		return
	}
	if serr := chStore.SetupTable(ctx, ""); serr != nil {
		log.Warn().Err(serr).Msg("logbridge: SetupTable failed — falling back to in-memory facts store")
		store = factsstore.NewInMemoryFactsStore()
		kind = "memory"
		return
	}
	store = chStore
	kind = "clickhouse@" + cfg.URL
	return
}
