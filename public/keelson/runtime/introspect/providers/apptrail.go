// apptrail — what each app did across every process the fact trail holds
// (ADR-0260 §SD5): window sessions, the newest log rows per app, and audited
// requests folded per subject. keelson('runtime_events') answers the same
// questions for the current run only; these three answer them across runs,
// which until now only the launcher's ranking and a Go caller could.
//
// A provider sees no predicate, so each table is bounded by construction —
// a look-back and a cap — rather than by the reader's WHERE. A reader
// narrows the bounded result to one app.

package providers

import (
	"context"
	"time"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// The tables' names, as keelson() resolves them and as a reader's
// `keelson.query.<table>` grant names them (ADR-0253 §SD1).
const (
	// TableAppRuns is window sessions across runs.
	TableAppRuns = "app_runs"
	// TableAppLogs is the newest log rows of each app.
	TableAppLogs = "app_logs"
	// TableAppAudit is audited requests folded per app, subject and result.
	TableAppAudit = "app_audit"
)

const (
	// appTrailLookBack is how far back the three tables read. A month holds
	// the questions the app center asks — when did it last run, what did it
	// last say — without scanning a trail that grows for as long as the
	// store is kept.
	appTrailLookBack = 30 * 24 * time.Hour
	// appRunsLimit and appAuditLimit cap the two aggregates.
	appRunsLimit  = 5000
	appAuditLimit = 5000
	// appLogsPerApp is the tail each app keeps; appLogsLimit caps the total.
	appLogsPerApp = 200
	appLogsLimit  = 10000
	// appTrailTimeout bounds one read of the store.
	appTrailTimeout = 15 * time.Second
)

// RegisterAppTrail registers the three tables into r. The reader is
// asserted off facts (the ADR-0155 §SD1 optional-capability pattern), so an
// in-memory store or a nil one answers with empty tables rather than absent
// ones, as keelson('runtime_events') does.
func RegisterAppTrail(r *introspect.Registry, facts factsstore.FactsStoreI) (err error) {
	reader, _ := facts.(factsstore.AppTrailReaderI)
	for _, p := range []introspect.Provider{
		appRunsProvider{reader: reader}, appLogsProvider{reader: reader}, appAuditProvider{reader: reader},
	} {
		if err = r.Register(p); err != nil {
			return
		}
	}
	return
}

// appTrailFilter is the look-back every table reads over.
func appTrailFilter(limit uint32) factsstore.AppTrailFilter {
	return factsstore.AppTrailFilter{Since: time.Now().Add(-appTrailLookBack), Limit: limit}
}

// A failed read is reported rather than rendered as an empty table: "the app
// never ran" and "the store did not answer" are different claims.

type appRunsProvider struct {
	reader factsstore.AppTrailReaderI
}

func (appRunsProvider) Name() string                         { return TableAppRuns }
func (appRunsProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }
func (appRunsProvider) Schema() *arrow.Schema                { return appRunsTable(nil).Schema() }

func (p appRunsProvider) Snapshot(proj introspect.Projection) (arrow.RecordBatch, error) {
	var rows []factsstore.AppRunRow
	if p.reader != nil {
		ctx, cancel := context.WithTimeout(context.Background(), appTrailTimeout)
		defer cancel()
		var err error
		if rows, err = p.reader.AppRuns(ctx, appTrailFilter(appRunsLimit)); err != nil {
			return nil, eb.Build().Str("table", TableAppRuns).Errorf("app trail read: %w", err)
		}
	}
	return appRunsTable(rows).Build(proj, len(rows)), nil
}

// unixMsOrZero is a time as epoch milliseconds, 0 for the zero time: the
// tables' "no such row", not 1970.
func unixMsOrZero(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}

func appRunsTable(rows []factsstore.AppRunRow) *introspect.Table {
	return introspect.NewTable().
		String("app_id", func(i int) string { return string(rows[i].AppId) }).
		String("run_id", func(i int) string { return rows[i].RunId }).
		Uint64("instance_key", func(i int) uint64 { return rows[i].InstanceKey }).
		// 0 when the start fell before the look-back.
		Int64("started_ms", func(i int) int64 { return unixMsOrZero(rows[i].StartedAt) }).
		// 0 when the session is open, or its process ended without a close.
		Int64("stopped_ms", func(i int) int64 { return unixMsOrZero(rows[i].StoppedAt) }).
		String("stop_reason", func(i int) string { return rows[i].StopReason }).
		// The process's last heartbeat or start record: for a session with no
		// stop, the latest moment it is known to have been alive. 0 when the
		// process wrote neither within the look-back.
		Int64("run_seen_ms", func(i int) int64 { return unixMsOrZero(rows[i].RunSeenAt) })
}

type appLogsProvider struct {
	reader factsstore.AppTrailReaderI
}

func (appLogsProvider) Name() string                         { return TableAppLogs }
func (appLogsProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }
func (appLogsProvider) Schema() *arrow.Schema                { return appLogsTable(nil).Schema() }

func (p appLogsProvider) Snapshot(proj introspect.Projection) (arrow.RecordBatch, error) {
	var rows []factsstore.AppLogRow
	if p.reader != nil {
		ctx, cancel := context.WithTimeout(context.Background(), appTrailTimeout)
		defer cancel()
		var err error
		if rows, err = p.reader.AppLogTail(ctx, appTrailFilter(appLogsLimit), appLogsPerApp); err != nil {
			return nil, eb.Build().Str("table", TableAppLogs).Errorf("app trail read: %w", err)
		}
	}
	return appLogsTable(rows).Build(proj, len(rows)), nil
}

// appLogsTable carries the envelope and the attribution. Fields and stack
// are not columns (ADR-0260 §SD5): the table stays at what logviewer shows
// of a live process.
func appLogsTable(rows []factsstore.AppLogRow) *introspect.Table {
	return introspect.NewTable().
		Int64("ts_ms", func(i int) int64 { return unixMsOrZero(rows[i].Ts) }).
		String("app_id", func(i int) string { return string(rows[i].AppId) }).
		Uint64("instance_key", func(i int) uint64 { return rows[i].InstanceKey }).
		String("run_id", func(i int) string { return rows[i].RunId }).
		String("level", func(i int) string { return rows[i].Level }).
		String("caller", func(i int) string { return rows[i].Caller }).
		String("message", func(i int) string { return rows[i].Message }).
		String("error", func(i int) string { return rows[i].Error })
}

type appAuditProvider struct {
	reader factsstore.AppTrailReaderI
}

func (appAuditProvider) Name() string                         { return TableAppAudit }
func (appAuditProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }
func (appAuditProvider) Schema() *arrow.Schema                { return appAuditTable(nil).Schema() }

func (p appAuditProvider) Snapshot(proj introspect.Projection) (arrow.RecordBatch, error) {
	var rows []factsstore.AppAuditRow
	if p.reader != nil {
		ctx, cancel := context.WithTimeout(context.Background(), appTrailTimeout)
		defer cancel()
		var err error
		if rows, err = p.reader.AppAuditSummary(ctx, appTrailFilter(appAuditLimit)); err != nil {
			return nil, eb.Build().Str("table", TableAppAudit).Errorf("app trail read: %w", err)
		}
	}
	return appAuditTable(rows).Build(proj, len(rows)), nil
}

func appAuditTable(rows []factsstore.AppAuditRow) *introspect.Table {
	return introspect.NewTable().
		String("app_id", func(i int) string { return string(rows[i].AppId) }).
		String("subject", func(i int) string { return rows[i].Subject }).
		String("result", func(i int) string { return rows[i].Result }).
		Uint64("requests", func(i int) uint64 { return rows[i].Requests }).
		Int64("first_ms", func(i int) int64 { return unixMsOrZero(rows[i].FirstAt) }).
		Int64("last_ms", func(i int) int64 { return unixMsOrZero(rows[i].LastAt) }).
		// Over the requests that recorded a latency; 0 when none did.
		Float64("mean_latency_ms", func(i int) float64 { return rows[i].MeanLatencyMs }).
		Uint64("max_latency_ms", func(i int) uint64 { return uint64(rows[i].MaxLatencyMs) })
}
