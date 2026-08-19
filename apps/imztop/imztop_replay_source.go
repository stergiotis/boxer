package imztop

import (
	"context"
	"iter"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/keelson/data/storeexec"
	"github.com/stergiotis/boxer/public/keelson/runtime/sysmetricsbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/sysmfacts"
	"github.com/stergiotis/boxer/public/keelson/runtime/sysmreplay"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/sysmsnap"
)

// StoreSource is the production [BundleSourceI]: stored history read from
// `boxer.facts` through the ADR-0184 record store.
//
// This is the one place imztop reaches a database, and the reason ADR-0197 §SD7
// is a decision rather than a detail — the app that ADR-0090 made
// capability-free now dials something. The connection is opened when a user
// enters replay, not at Mount, so an imztop nobody replays makes no connection
// at all.
type StoreSource struct {
	reader *sysmreplay.Reader
	store  *sysmfacts.SysmetricsStore
	host   string
	url    string

	// Last decimation plan, for the status line. Written on the transport's
	// goroutine and read on the render thread, so it takes a lock.
	decMu     sync.Mutex
	decStored int64
	decShown  int
}

var _ BundleSourceI = (*StoreSource)(nil)

// StoreSourceOptions configures [NewStoreSource].
type StoreSourceOptions struct {
	// Host is the token whose series to replay. Empty takes the local
	// hostname, sanitized the way the scraper sanitizes it.
	//
	// That default is right for the co-located deployment, where the scraper
	// runs beside the GUI. It is wrong when the plane is bridged from an
	// external sysmetricsd on another box, which publishes under its own
	// token: imztop's live consumer is handed only the snapshot and never sees
	// the subject it arrived on, so it cannot learn the token from the plane
	// (ADR-0184 §SD8 records that gap). Picking a host is deferred; setting
	// this is the workaround until it lands.
	Host string
	// Endpoint overrides the ClickHouse URL the CLICKHOUSE_* registry entries
	// resolve to. Empty takes the registry's.
	//
	// It exists because the registry caches each variable on first read, so a
	// process that has already resolved the endpoint cannot be repointed by
	// changing the environment — a caller wanting history from a different
	// server has to say so here.
	Endpoint string
	Log      zerolog.Logger
}

// NewStoreSource dials ClickHouse and binds the replay reader to it.
//
// Every failure it can hit means "there is nothing to replay", and each says
// which one it was, because they call for different actions from whoever reads
// the message: an unreachable server is a deployment problem, and a table that
// does not match is a tee that has never run here.
func NewStoreSource(ctx context.Context, opts StoreSourceOptions) (inst *StoreSource, err error) {
	host := opts.Host
	if host == "" {
		host = sysmetricsbus.DefaultHostToken()
	}

	cfg := chclient.ConfigFromEnv()
	if opts.Endpoint != "" {
		cfg.URL = opts.Endpoint
	}
	client := chclient.New(cfg, nil)
	if perr := client.Ping(ctx); perr != nil {
		err = eh.Errorf("imztop: replay needs ClickHouse at %s: %w", cfg.URL, perr)
		return
	}
	exec, err := storeexec.New(client, nil)
	if err != nil {
		err = eh.Errorf("imztop: replay executor: %w", err)
		return
	}
	store := sysmfacts.NewSysmetricsStore(exec, nil, sysmfacts.SysmetricsStoreConfig{})
	if verr := store.VerifySchema(ctx); verr != nil {
		store.Close()
		err = eh.Errorf("imztop: no stored metric history at %s — %s does not match the store, "+
			"which is what an installation that has never run `sysmetricsd --tee` looks like: %w",
			cfg.URL, sysmfacts.SysmetricsTableName, verr)
		return
	}
	// The same executor the store was built on, so coverage and replay cannot
	// end up answering about different servers (ADR-0197 §SD10).
	reader, err := sysmreplay.New(sysmreplay.Options{Store: store, Exec: exec, Host: host, Log: opts.Log})
	if err != nil {
		store.Close()
		return
	}
	inst = &StoreSource{reader: reader, store: store, host: host, url: cfg.URL}
	return
}

// All satisfies [BundleSourceI], decimating the window when it holds more
// bundles than the fold can show (ADR-0197 §SD6, closed by §SD11).
//
// The decision is made here rather than by the caller because this is the only
// layer that can ask the store how much is in there, and it runs on the
// transport's goroutine where a blocking count is affordable. A window inside
// the budget is read exactly as before.
//
// Decimation samples whole recorded bundles rather than aggregating them, so a
// long range loses resolution and nothing else — see the sysmreplay package's
// decimation notes for why a bundle is not a thing that can be averaged.
func (inst *StoreSource) All(ctx context.Context, w sysmreplay.Window) iter.Seq2[*sysmsnap.BundleSnapshot, error] {
	if plan, ok := inst.planFor(ctx, w); ok {
		w.Decimate = plan
	}
	return inst.reader.All(ctx, w)
}

// planFor returns the decimation plan for a window, or ok=false to read it
// whole.
//
// Every failure here degrades to reading the window whole: a plan is an
// optimisation, and a replay that shows the head of a long range is a better
// answer than one that refuses because a count did not come back.
func (inst *StoreSource) planFor(ctx context.Context, w sysmreplay.Window) (times []int64, ok bool) {
	if w.From.IsZero() || w.To.IsZero() || len(w.Decimate) > 0 {
		return
	}
	n, err := inst.reader.CountBundles(ctx, w)
	if err != nil {
		log.Warn().Err(err).Msg("imztop: could not count stored bundles; replaying the window whole")
		return
	}
	if !sysmreplay.NeedsDecimation(n, defaultHistorySlots) {
		return
	}
	plan, err := inst.reader.PlanDecimation(ctx, w, defaultHistorySlots)
	if err != nil {
		log.Warn().Err(err).Msg("imztop: could not plan decimation; replaying the window whole")
		return
	}
	if len(plan.TimesMS) == 0 {
		return
	}
	inst.noteDecimated(n, len(plan.TimesMS))
	times, ok = plan.TimesMS, true
	return
}

// noteDecimated records what the last plan did, for the status line. The UI
// reads it to say "showing N of M" rather than leaving a long range silently
// sparser than it looks.
func (inst *StoreSource) noteDecimated(stored int64, shown int) {
	inst.decMu.Lock()
	inst.decStored, inst.decShown = stored, shown
	inst.decMu.Unlock()
}

// Decimation reports the last plan: how many bundles the window holds and how
// many are being replayed. ok is false when the last read was not decimated.
func (inst *StoreSource) Decimation() (stored int64, shown int, ok bool) {
	inst.decMu.Lock()
	stored, shown = inst.decStored, inst.decShown
	inst.decMu.Unlock()
	ok = shown > 0
	return
}

// Preview reports mean CPU busy per bin, for the strip's load layer
// (ADR-0197 §SD9). It blocks on a section read; call it off the render thread.
func (inst *StoreSource) Preview(ctx context.Context, w sysmreplay.Window, bucket time.Duration) (points []sysmreplay.PreviewPoint, err error) {
	points, err = inst.reader.Preview(ctx, w, bucket)
	return
}

// Coverage reports where stored history is, for the availability strip
// (ADR-0197 §SD9). It blocks on a database read; call it off the render thread.
func (inst *StoreSource) Coverage(ctx context.Context, w sysmreplay.Window, bucket time.Duration) (runs []sysmreplay.CoverageRun, err error) {
	runs, err = inst.reader.CoverageRuns(ctx, w, bucket)
	return
}

// Host reports the token being replayed.
func (inst *StoreSource) Host() (host string) {
	host = inst.host
	return
}

// Endpoint reports the ClickHouse URL, for the status line that has to explain
// where the history came from.
func (inst *StoreSource) Endpoint() (url string) {
	url = inst.url
	return
}

// Close releases the store. The source is unusable afterwards.
func (inst *StoreSource) Close() {
	inst.store.Close()
}
