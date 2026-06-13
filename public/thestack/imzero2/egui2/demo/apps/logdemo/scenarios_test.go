//go:build binary_log

package logdemo

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/observability/eh"
	runtimeapp "github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/logbridge"
)

// scenarioHarness wires the same host chain the carousel uses
// (NewSink → host logger → app logger → mountCtx) so each scenario
// emit is decoded back through CBOR into a LogRow with typed Fields,
// which is what the logviewer's detail pane reads.
//
// Returns a fresh *App and the Sink it writes through. Caller owns
// closing the sink; FlushN=1 means every emit drains synchronously
// to the tail buffer the assertions read.
func scenarioHarness(t *testing.T) (inst *App, sink *logbridge.Sink) {
	t.Helper()
	store := factsstore.NewInMemoryFactsStore()
	s, err := logbridge.NewSink(store, logbridge.Config{
		Capacity:     32,
		FlushN:       1,
		TailCapacity: 32,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	hostLogger := logbridge.NewLogger(nil, s)
	appLogger := runtimeapp.AppLogger(hostLogger, manifest.Id)
	mountCtx := runtimeapp.NewStaticMountContext(manifest.Id, appLogger, nil, nil, nil)

	a := newApp()
	require.NoError(t, a.Mount(mountCtx))
	return a, s
}

// fieldsByName indexes a LogRow's Fields by name so per-field type +
// value asserts read as table lookups rather than linear scans. Fails
// the test if a name appears twice (the scenarios shouldn't emit
// duplicates).
func fieldsByName(t *testing.T, row factsstore.LogRow) (m map[string]factsstore.LogField) {
	t.Helper()
	m = make(map[string]factsstore.LogField, len(row.Fields))
	for _, f := range row.Fields {
		_, dup := m[f.Name]
		require.Falsef(t, dup, "duplicate field name in scenario row: %q", f.Name)
		m[f.Name] = f
	}
	return
}

// TestEmitScenarioHTTP_TypedFields walks one full rotation of the
// HTTP fixture set and asserts every fixture's level matches the
// status-derived rule and that every typed field round-trips through
// CBOR with the expected Kind. Catches regressions where a fixture
// switched type (Int → Uint, etc.) or where the level rule drifts.
func TestEmitScenarioHTTP_TypedFields(t *testing.T) {
	a, sink := scenarioHarness(t)
	for i := 0; i < len(httpFixtures); i++ {
		a.emitScenarioHTTP()
	}
	rows := sink.Tail(0)
	require.Len(t, rows, len(httpFixtures))

	for i, row := range rows {
		fx := httpFixtures[(uint64(i)+1)%uint64(len(httpFixtures))]

		switch {
		case fx.status >= 500:
			assert.Equalf(t, "error", row.Level, "fixture[%d] %d should emit error", i, fx.status)
			assert.NotEmpty(t, row.Error, "5xx fixture must populate the Error envelope field via .Err(...)")
		case fx.status >= 400:
			assert.Equalf(t, "warn", row.Level, "fixture[%d] %d should emit warn", i, fx.status)
		default:
			assert.Equalf(t, "info", row.Level, "fixture[%d] %d should emit info", i, fx.status)
		}

		fs := fieldsByName(t, row)
		assert.Equal(t, factsstore.LogFieldKindString, fs["method"].Kind)
		assert.Equal(t, fx.method, fs["method"].Str)
		assert.Equal(t, factsstore.LogFieldKindString, fs["path"].Kind)
		assert.Equal(t, fx.path, fs["path"].Str)
		// CBOR encodes positive integers as the unsigned major type
		// regardless of the writer's typed slot, so .Uint64 → KindUint
		// (would also be KindUint if we'd used .Int64 with a positive
		// value — see the httpFixture banner for the rationale).
		assert.Equal(t, factsstore.LogFieldKindUint, fs["status"].Kind, "status must round-trip as uint64")
		assert.Equal(t, fx.status, fs["status"].Uint)
		assert.Equal(t, factsstore.LogFieldKindFloat, fs["latency_ms"].Kind, "latency_ms must round-trip as float64")
		assert.Equal(t, factsstore.LogFieldKindUint, fs["resp_bytes"].Kind, "resp_bytes must round-trip as uint64")
		assert.Equal(t, fx.respBytes, fs["resp_bytes"].Uint)
		assert.Equal(t, factsstore.LogFieldKindString, fs["remote_addr"].Kind)
		assert.Equal(t, factsstore.LogFieldKindTime, fs["served_at"].Kind, "served_at must round-trip as time.Time")
		assert.False(t, fs["served_at"].Time.IsZero(), "served_at must be populated")
	}
}

// TestEmitScenarioDB_TypedFields covers the DB scenario: every 4th
// emit flips to error with a non-empty Error envelope; long-duration
// fixtures emit warn; bool / uint / int / float / time all
// round-trip with the expected Kind.
func TestEmitScenarioDB_TypedFields(t *testing.T) {
	a, sink := scenarioHarness(t)
	const n = 4 // exercise the wantErr arm at least once
	for i := 0; i < n; i++ {
		a.emitScenarioDB()
	}
	rows := sink.Tail(0)
	require.Len(t, rows, n)

	sawErr := false
	sawWarn := false
	for _, row := range rows {
		fs := fieldsByName(t, row)
		assert.Equal(t, factsstore.LogFieldKindString, fs["query"].Kind)
		assert.Equal(t, factsstore.LogFieldKindUint, fs["rows"].Kind, "rows must round-trip as uint64 (positive int CBOR-encodes as unsigned)")
		assert.Equal(t, factsstore.LogFieldKindFloat, fs["duration_ms"].Kind)
		assert.Equal(t, factsstore.LogFieldKindUint, fs["conn_id"].Kind)
		assert.Equal(t, factsstore.LogFieldKindBool, fs["replica"].Kind, "replica must round-trip as bool")
		assert.Equal(t, factsstore.LogFieldKindTime, fs["ts"].Kind)
		switch row.Level {
		case "error":
			sawErr = true
			assert.NotEmpty(t, row.Error, "error rows must populate the Error envelope field")
		case "warn":
			sawWarn = true
		}
	}
	assert.True(t, sawErr, "DB scenario must hit the every-4th-call error arm within %d emits", n)
	assert.True(t, sawWarn, "DB scenario must hit the slow-query warn arm within one fixture rotation")
}

// TestEmitScenarioAuth_TypedFields covers the auth scenario: bool /
// bytes / uint / time all round-trip with the expected Kind, and
// denied requests emit warn while granted requests emit info.
func TestEmitScenarioAuth_TypedFields(t *testing.T) {
	a, sink := scenarioHarness(t)
	for i := 0; i < len(authFixtures); i++ {
		a.emitScenarioAuth()
	}
	rows := sink.Tail(0)
	require.Len(t, rows, len(authFixtures))

	for i, row := range rows {
		fx := authFixtures[(uint64(i)+1)%uint64(len(authFixtures))]
		fs := fieldsByName(t, row)
		assert.Equal(t, factsstore.LogFieldKindString, fs["user"].Kind)
		assert.Equal(t, fx.user, fs["user"].Str)
		assert.Equal(t, factsstore.LogFieldKindString, fs["role"].Kind)
		assert.Equal(t, factsstore.LogFieldKindString, fs["subject"].Kind)
		assert.Equal(t, factsstore.LogFieldKindBool, fs["granted"].Kind)
		assert.Equal(t, fx.granted, fs["granted"].Bool)
		assert.Equal(t, factsstore.LogFieldKindUint, fs["attempt"].Kind)
		assert.Equal(t, fx.attempt, fs["attempt"].Uint)
		assert.Equal(t, factsstore.LogFieldKindBytes, fs["session"].Kind, "session must round-trip as []byte")
		assert.Equal(t, fx.session, fs["session"].Bytes)
		assert.Equal(t, factsstore.LogFieldKindTime, fs["at"].Kind)

		if fx.granted {
			assert.Equal(t, "info", row.Level, "granted auth → info")
		} else {
			assert.Equal(t, "warn", row.Level, "denied auth → warn")
		}
	}
}

// TestEmitScenarioBoxerErr_ChainSurvives walks the boxer-error
// fixture set and asserts each variant lands a non-empty Error
// envelope field. We don't assert the specific human-formatted
// markers here — that contract is owned by
// TestInstallGlobal_BoxerErrorRendersAsRichText in the logbridge
// package, where ErrorMarshalFunc is actually wired. In this test
// the marshaler is not installed, so .Err falls back to .Error()
// (a plain string), but the envelope field must still populate so
// the detail pane has something to show. Also confirms the level
// is "error" (the scenario's only emit level).
func TestEmitScenarioBoxerErr_ChainSurvives(t *testing.T) {
	a, sink := scenarioHarness(t)
	for i := 0; i < len(boxerErrFixtures); i++ {
		a.emitScenarioBoxerErr()
	}
	rows := sink.Tail(0)
	require.Len(t, rows, len(boxerErrFixtures))

	for i, row := range rows {
		assert.Equalf(t, "error", row.Level, "fixture[%d] must emit at error level", i)
		assert.NotEmptyf(t, row.Error, "fixture[%d] must populate the Error envelope field via .Err(...)", i)
		fs := fieldsByName(t, row)
		assert.Equal(t, factsstore.LogFieldKindString, fs["scenario"].Kind)
		assert.Equal(t, "boxer_err", fs["scenario"].Str)
		assert.Equal(t, factsstore.LogFieldKindString, fs["variant"].Kind,
			"variant tag must accompany every boxer-err event so a viewer query can group by chain shape")
		assert.Equal(t, factsstore.LogFieldKindTime, fs["at"].Kind)
	}
}

// TestBuildBoxerErr_Variants asserts the fixture builder returns the
// expected error shape for each variant: "single" is a plain leaf,
// "wrapped" unwraps to a non-nil cause, and "structured" carries
// non-empty CBOR data on the innermost cause. These are the
// preconditions the human-format renderer relies on; if any variant
// degrades to a no-stack flat error the visual richness in the
// detail pane silently regresses.
func TestBuildBoxerErr_Variants(t *testing.T) {
	single := buildBoxerErr(boxerErrFixture{kind: "single"})
	require.NotNil(t, single)
	assert.Nil(t, errors.Unwrap(single), "single variant must be a leaf — Unwrap returns nil")

	wrapped := buildBoxerErr(boxerErrFixture{kind: "wrapped"})
	require.NotNil(t, wrapped)
	assert.NotNil(t, errors.Unwrap(wrapped),
		"wrapped variant must have at least one cause level (else the human format prints a flat single line)")

	structured := buildBoxerErr(boxerErrFixture{kind: "structured"})
	require.NotNil(t, structured)
	// Walk to the leaf — the eb.Build() call sits at the bottom.
	leaf := structured
	for {
		next := errors.Unwrap(leaf)
		if next == nil {
			break
		}
		leaf = next
	}
	wd, ok := leaf.(eh.ErrorWithStructuredDataI)
	require.True(t, ok, "structured variant must Unwrap down to an eh.ErrorWithStructuredDataI leaf")
	assert.NotEmpty(t, wd.GetCBORStructuredData(),
		"leaf must carry non-empty CBOR data — eb.Build() outputs are what the human format renders as +key=value lines")
}

// TestScenarioCounter_PerInstance — two App instances must have
// independent rotation counters, otherwise opening two demo windows
// would race the fixture index and produce non-deterministic per-
// window output.
func TestScenarioCounter_PerInstance(t *testing.T) {
	a1, _ := scenarioHarness(t)
	a2, _ := scenarioHarness(t)
	for i := 0; i < 3; i++ {
		a1.emitScenarioHTTP()
	}
	assert.Equal(t, uint64(0), a2.scenarioCounter.Load(),
		"app2's counter must be unaffected by app1's emits")
	a2.emitScenarioHTTP()
	assert.Equal(t, uint64(1), a2.scenarioCounter.Load())
}
