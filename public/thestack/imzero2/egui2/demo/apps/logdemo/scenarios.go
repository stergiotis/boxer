//go:build llm_generated_opus47

package logdemo

import (
	"errors"
	"fmt"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Scenario emitters generate realistic-looking structured events so
// the logviewer's detail pane has interesting per-kind content to
// render: HTTP request → method/path/status/latency_ms/bytes/remote;
// DB query → query/rows/duration/conn/replica/ts (+ optional err);
// Auth event → user/role/granted/subject/attempt/session.
//
// Each call rotates through a small fixture set so consecutive clicks
// produce visually distinct rows in the viewer (otherwise the table
// just shows N copies of the same row). The rotation index is the
// per-instance scenario counter — rotation is per-tile, so two open
// logdemo windows step through fixtures independently.
//
// Levels are derived from payload semantics rather than picked at
// random: a 4xx HTTP status emits warn, a 5xx emits error, a denied
// auth emits warn. That way the table's level tinting actually
// follows the data instead of being decorative.

// http fixtures rotate request shapes (method/path/status/bytes).
// Status codes drive the emit level: 2xx/3xx → info, 4xx → warn,
// 5xx → error.
//
// Numeric fields are uint64 because (a) HTTP status / response bytes
// are always non-negative, and (b) zerolog → CBOR encodes positive
// int64 as the unsigned major type anyway, so the LogField reader
// would land it under KindUint regardless. Picking uint64 at source
// makes that wire reality explicit and keeps the round-trip type
// stable.
type httpFixture struct {
	method     string
	path       string
	status     uint64
	respBytes  uint64
	remoteAddr string
}

var httpFixtures = []httpFixture{
	{"GET", "/api/v1/cards/42", 200, 1834, "10.0.0.7"},
	{"POST", "/api/v1/cards", 201, 312, "10.0.0.12"},
	{"GET", "/api/v1/users/me", 304, 0, "10.0.0.7"},
	{"DELETE", "/api/v1/cards/8881", 404, 64, "10.0.0.99"},
	{"PUT", "/api/v1/cards/13", 422, 184, "10.0.0.12"},
	{"GET", "/api/v1/health", 503, 24, "10.0.0.42"},
}

// emitScenarioHTTP fires one structured HTTP-request event. Status
// drives the level (and the row-tint downstream).
func (inst *App) emitScenarioHTTP() {
	fx := httpFixtures[inst.scenarioCounter.Add(1)%uint64(len(httpFixtures))]
	lvl := zerolog.InfoLevel
	switch {
	case fx.status >= 500:
		lvl = zerolog.ErrorLevel
	case fx.status >= 400:
		lvl = zerolog.WarnLevel
	}
	logger := inst.scenarioLogger()
	ev := levelEvent(logger, lvl).
		Str("method", fx.method).
		Str("path", fx.path).
		Uint64("status", fx.status).
		Float64("latency_ms", scenarioLatency(fx.status)).
		Uint64("resp_bytes", fx.respBytes).
		Str("remote_addr", fx.remoteAddr).
		Time("served_at", time.Now().UTC())
	if fx.status >= 500 {
		ev = ev.Err(errors.New("upstream failed: connection refused"))
	}
	ev.Msg(httpMessage(fx))
	inst.emitted.Add(1)
}

// httpMessage builds a short, scannable summary string. The
// structured fields below are the source of truth; the message is
// the eye-catcher in the table's Message column.
func httpMessage(fx httpFixture) (s string) {
	s = fmt.Sprintf("%s %s → %d", fx.method, fx.path, fx.status)
	return
}

// scenarioLatency picks a latency that scales with status — slow on
// errors, fast on cache-hits, normal otherwise — so the float column
// reads as plausible for the row.
func scenarioLatency(status uint64) (ms float64) {
	switch {
	case status == 304:
		ms = 1.7
	case status >= 500:
		ms = 2840.4
	case status >= 400:
		ms = 41.6
	default:
		ms = 23.4
	}
	return
}

// dbFixtures rotate query shapes. The replica flag toggles bool;
// duration_ms is float64; rows / conn_id are uint64 (always non-neg
// for queries — see the httpFixture banner for the wire-type
// rationale). The error case is emitted on a separate cycle (every
// 4th call).
type dbFixture struct {
	query    string
	rows     uint64
	duration float64
	connId   uint64
	replica  bool
}

var dbFixtures = []dbFixture{
	{"SELECT id, title FROM cards WHERE owner = $1", 12, 4.7, 7, false},
	{"INSERT INTO cards (id, body) VALUES ($1, $2)", 1, 11.3, 7, false},
	{"SELECT count(*) FROM cards WHERE created_at > now() - '1d'::interval", 1, 184.0, 12, true},
	{"UPDATE cards SET deleted_at = now() WHERE id = ANY($1)", 0, 9.1, 7, false},
}

// emitScenarioDB fires one structured DB-query event. Slow queries
// (duration > 100ms) emit at warn so the row tints in the viewer;
// every fourth call injects an err and emits at error.
func (inst *App) emitScenarioDB() {
	n := inst.scenarioCounter.Add(1)
	fx := dbFixtures[n%uint64(len(dbFixtures))]

	lvl := zerolog.InfoLevel
	wantErr := n%4 == 0
	switch {
	case wantErr:
		lvl = zerolog.ErrorLevel
	case fx.duration >= 100:
		lvl = zerolog.WarnLevel
	}

	logger := inst.scenarioLogger()
	ev := levelEvent(logger, lvl).
		Str("query", fx.query).
		Uint64("rows", fx.rows).
		Float64("duration_ms", fx.duration).
		Uint64("conn_id", fx.connId).
		Bool("replica", fx.replica).
		Time("ts", time.Now().UTC())
	if wantErr {
		ev = ev.Err(errors.New("pq: deadlock detected (Class 40 — Transaction Rollback)"))
	}
	ev.Msg(dbMessage(fx, wantErr))
	inst.emitted.Add(1)
}

// dbMessage summarises the query in the Message column. The full
// SQL stays in the structured `query` field for the detail pane.
func dbMessage(fx dbFixture, hasErr bool) (s string) {
	if hasErr {
		s = "db query failed"
		return
	}
	if fx.duration >= 100 {
		s = fmt.Sprintf("slow query (%.0fms)", fx.duration)
		return
	}
	s = "db query"
	return
}

// authFixtures rotate principal / role / decision triples. The
// granted flag flips bool; session is bytes (rendered as hex by the
// detail pane); attempt is uint64; at is time.
type authFixture struct {
	user    string
	role    string
	subject string
	granted bool
	attempt uint64
	session []byte
}

var authFixtures = []authFixture{
	{"alice@example.com", "admin", "ch.local.exec", true, 1,
		[]byte{0xa3, 0x4f, 0x12, 0xde, 0xad, 0xbe, 0xef, 0x01}},
	{"bob@example.com", "viewer", "ch.local.exec", false, 3,
		[]byte{0x77, 0x88, 0x99, 0xaa, 0xbb, 0xcc, 0xdd, 0xee}},
	{"svc-ingest", "service", "factsstore.write_log", true, 1,
		[]byte{0x0c, 0x0d, 0x0e, 0x0f, 0x10, 0x11, 0x12, 0x13}},
	{"mallory@external.example", "viewer", "secrets.read", false, 7,
		[]byte{0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88}},
}

// emitScenarioAuth fires one structured auth-decision event. Denied
// requests emit at warn so the row tints in the viewer.
func (inst *App) emitScenarioAuth() {
	fx := authFixtures[inst.scenarioCounter.Add(1)%uint64(len(authFixtures))]
	lvl := zerolog.InfoLevel
	if !fx.granted {
		lvl = zerolog.WarnLevel
	}
	logger := inst.scenarioLogger()
	levelEvent(logger, lvl).
		Str("user", fx.user).
		Str("role", fx.role).
		Str("subject", fx.subject).
		Bool("granted", fx.granted).
		Uint64("attempt", fx.attempt).
		Bytes("session", fx.session).
		Time("at", time.Now().UTC()).
		Msg(authMessage(fx))
	inst.emitted.Add(1)
}

func authMessage(fx authFixture) (s string) {
	if fx.granted {
		s = fmt.Sprintf("granted %s → %s", fx.user, fx.subject)
		return
	}
	s = fmt.Sprintf("denied %s → %s", fx.user, fx.subject)
	return
}

// boxerErrFixtures rotate the shape of the wrapped error chain so
// consecutive Boxer-error clicks show the operator different patterns:
// a single-cause chain, a multi-level wrap, and a chain whose innermost
// error carries CBOR structured data (built via eb.Build()).
type boxerErrFixture struct {
	kind string // "single", "wrapped", "structured" — chosen by counter rotation
}

var boxerErrFixtures = []boxerErrFixture{
	{kind: "single"},
	{kind: "wrapped"},
	{kind: "structured"},
}

// emitScenarioBoxerErr fires one error-level event whose error
// argument is a boxer-machinery wrapped error. Relies on
// logbridge.InstallGlobal having flipped zerolog.ErrorMarshalFunc to
// eh.ErrorMarshalFuncHuman so the rendered string carries cause
// arrows, stack frames, and (for the "structured" fixture) the CBOR
// diagnostic of the attached fields. The viewer's detail pane shows
// the result in red monospace under "error:" — multi-line works
// because the pane uses plain Label.Selectable(true).
func (inst *App) emitScenarioBoxerErr() {
	fx := boxerErrFixtures[inst.scenarioCounter.Add(1)%uint64(len(boxerErrFixtures))]
	err := buildBoxerErr(fx)
	logger := inst.scenarioLogger()
	logger.Error().
		Err(err).
		Str("scenario", "boxer_err").
		Str("variant", fx.kind).
		Time("at", time.Now().UTC()).
		Msg("boxer-formatted error chain — see error: in the detail pane")
	inst.emitted.Add(1)
}

// buildBoxerErr constructs a wrapped error of the requested kind so
// each variant exercises a different code path through eh's
// MarshalError tree builder. Kept as a separate function so the
// scenario test can inspect the produced error directly without
// going through the full emit path.
func buildBoxerErr(fx boxerErrFixture) (err error) {
	switch fx.kind {
	case "wrapped":
		// Three-level wrap: leaf → middle → top. Each level adds a
		// stack at its own callsite, exercising the per-stack grouping
		// in eh.MarshalError.
		leaf := eh.New("connection refused")
		mid := eh.Errorf("dial tcp 10.0.0.7:9000: %w", leaf)
		err = eh.Errorf("query \"SELECT * FROM cards\" failed: %w", mid)
		return
	case "structured":
		// Innermost error carries CBOR structured data via eb.Build().
		// MarshalError emits these as a `data` field next to the fact;
		// ErrorMarshalFuncHuman renders them as `+ key=value` lines so
		// the operator sees the structured payload inline.
		leaf := eb.Build().
			Str("op", "Sink.appendTail").
			Uint64("ring_capacity", 32).
			Uint64("ring_len", 32).
			Bool("dropped_oldest", true).
			Errorf("ring full — drop-oldest engaged")
		err = eh.Errorf("logbridge.flush: %w", leaf)
		return
	default:
		// Plain single-cause via eh.New so the human format prints a
		// minimal chain (one message, one stack).
		err = eh.New("scenario emitter: plain boxer error with stack")
		return
	}
}

// scenarioLogger returns the per-instance logger when Mount has run,
// otherwise the package-level fallback. Mirrors emit() so test paths
// (which skip Mount) still produce output.
func (inst *App) scenarioLogger() (logger zerolog.Logger) {
	if inst.loggerInit {
		logger = inst.logger
		return
	}
	logger = log.Logger
	return
}

// levelEvent maps a zerolog.Level constant to the matching event
// builder method on a Logger. Centralised here so each scenario
// reads as a flat field-chain rather than a switch.
func levelEvent(logger zerolog.Logger, lvl zerolog.Level) (ev *zerolog.Event) {
	switch lvl {
	case zerolog.TraceLevel:
		ev = logger.Trace()
	case zerolog.DebugLevel:
		ev = logger.Debug()
	case zerolog.WarnLevel:
		ev = logger.Warn()
	case zerolog.ErrorLevel:
		ev = logger.Error()
	case zerolog.FatalLevel:
		// Fatal calls os.Exit; the demo never wants that. Down-cast to
		// Error so the row still shows up tinted.
		ev = logger.Error()
	case zerolog.PanicLevel:
		// Panic raises after Msg; same down-cast for the same reason.
		ev = logger.Error()
	default:
		ev = logger.Info()
	}
	return
}
