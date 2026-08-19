package regex_explorer

// Tests for the ADR-0017 extraction hand-off.
//
// The publish/open tests run a real adhocdata.Service over an inprocbus
// with a stub key registrar and a stub windowhost.open subscriber, so
// they exercise the actual wire codecs without needing clickhouse-local.

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/apps/play/launchcfg"
	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	runtimeapp "github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/launchreply"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/launchrequest"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/windowhost"
)

// ---------------------------------------------------------------------------
// Snapshot — the render-thread half
// ---------------------------------------------------------------------------

func TestSnapshotEvalBuildsGoRows(t *testing.T) {
	inst := newApp()
	inst.pattern = `(a)(b)?`
	inst.haystack = "ab a"

	snap, err := inst.snapshotEval()
	require.NoError(t, err)
	assert.Equal(t, `(a)(b)?`, snap.pattern)
	assert.False(t, snap.hasCH, "no lane result, so the CH half must be absent")

	// Two matches, three rows each (group 0, 1, 2). The second match has
	// no `b`, so group 2 did not participate.
	want := []goMatchRow{
		{MatchIdx: 0, GroupIdx: 0, Text: "ab", StartByte: 0, StopByte: 2, Matched: 1},
		{MatchIdx: 0, GroupIdx: 1, Text: "a", StartByte: 0, StopByte: 1, Matched: 1},
		{MatchIdx: 0, GroupIdx: 2, Text: "b", StartByte: 1, StopByte: 2, Matched: 1},
		{MatchIdx: 1, GroupIdx: 0, Text: "a", StartByte: 3, StopByte: 4, Matched: 1},
		{MatchIdx: 1, GroupIdx: 1, Text: "a", StartByte: 3, StopByte: 4, Matched: 1},
		{MatchIdx: 1, GroupIdx: 2, Text: "", StartByte: -1, StopByte: -1, Matched: 0},
	}
	assert.Equal(t, want, snap.goRows)
}

func TestSnapshotEvalCarriesGroupNames(t *testing.T) {
	inst := newApp()
	inst.pattern = `(?P<year>\d{4})-(\d{2})`
	inst.haystack = "2026-07"

	snap, err := inst.snapshotEval()
	require.NoError(t, err)
	require.Len(t, snap.goRows, 3)
	assert.Equal(t, "", snap.goRows[0].GroupName, "the whole match has no name")
	assert.Equal(t, "year", snap.goRows[1].GroupName)
	assert.Equal(t, "", snap.goRows[2].GroupName, "an unnamed group has no name")
}

// TestSnapshotEvalDropsZeroWidthMatches pins the ordinal-alignment rule.
// Go enumerates a zero-width match at every position for `a*` over "xyz"
// while ClickHouse's extractAll yields none — keeping them would shift
// every match_idx and make the join compare unrelated rows.
func TestSnapshotEvalDropsZeroWidthMatches(t *testing.T) {
	inst := newApp()
	inst.pattern = `a*`
	inst.haystack = "xaz"

	snap, err := inst.snapshotEval()
	require.NoError(t, err)
	require.Len(t, snap.goRows, 1, "only the non-empty match survives")
	assert.Equal(t, int32(0), snap.goRows[0].MatchIdx)
	assert.Equal(t, "a", snap.goRows[0].Text)
}

func TestSnapshotEvalRefusesUnusableInput(t *testing.T) {
	cases := []struct {
		name     string
		pattern  string
		haystack string
		wantMsg  string
	}{
		{"empty haystack", "a", "", "haystack is empty"},
		{"empty pattern", "", "abc", "no pattern entered"},
		{"invalid pattern", "a(", "abc", "does not compile"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inst := newApp()
			inst.pattern = tc.pattern
			inst.haystack = tc.haystack
			_, err := inst.snapshotEval()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantMsg)
		})
	}
}

func TestChExtractRowsFlattensListOutcome(t *testing.T) {
	out := listOutcome{
		Matches:      []string{"a", "c"},
		Groups:       [][]string{{"a", "b"}, {"c", "d"}},
		YieldsGroups: true,
	}
	want := []chExtractRow{
		{MatchIdx: 0, GroupIdx: 0, Text: "a"},
		{MatchIdx: 1, GroupIdx: 0, Text: "c"},
		// extractAllGroups is 0-based over groups; capture numbering is
		// 1-based, so the shift is what makes the join key line up.
		{MatchIdx: 0, GroupIdx: 1, Text: "a"},
		{MatchIdx: 0, GroupIdx: 2, Text: "b"},
		{MatchIdx: 1, GroupIdx: 1, Text: "c"},
		{MatchIdx: 1, GroupIdx: 2, Text: "d"},
	}
	assert.Equal(t, want, chExtractRows(out))
}

// ---------------------------------------------------------------------------
// Arrow record shape
// ---------------------------------------------------------------------------

// readIPCStream decodes an Arrow IPC stream into its schema and first
// record batch. Also proves the encoders emit the *stream* form ADR-0134
// expects rather than the file form the chlocalbroker InputTables path
// takes — ipc.NewReader rejects the latter.
func readIPCStream(t *testing.T, raw []byte) (schema *arrow.Schema, rec arrow.RecordBatch) {
	t.Helper()
	rdr, err := ipc.NewReader(bytes.NewReader(raw), ipc.WithAllocator(memory.DefaultAllocator))
	require.NoError(t, err)
	t.Cleanup(rdr.Release)
	schema = rdr.Schema()
	require.True(t, rdr.Next(), "stream carried no record batch")
	rec = rdr.RecordBatch()
	rec.Retain()
	t.Cleanup(rec.Release)
	return
}

// fieldNames lists a schema's column names in order.
func fieldNames(schema *arrow.Schema) (out []string) {
	out = make([]string, 0, len(schema.Fields()))
	for _, f := range schema.Fields() {
		out = append(out, f.Name)
	}
	return
}

func TestEncodeGoMatchesRecordShape(t *testing.T) {
	rows := []goMatchRow{
		{MatchIdx: 0, GroupIdx: 0, GroupName: "", Text: "ab", StartByte: 0, StopByte: 2, Matched: 1},
		{MatchIdx: 0, GroupIdx: 1, GroupName: "g", Text: "", StartByte: -1, StopByte: -1, Matched: 0},
	}
	raw, err := encodeGoMatches(rows)
	require.NoError(t, err)

	schema, rec := readIPCStream(t, raw)
	assert.Equal(t,
		[]string{"match_idx", "group_idx", "group_name", "text", "start_byte", "stop_byte", "matched"},
		fieldNames(schema),
	)
	require.EqualValues(t, len(rows), rec.NumRows())
	assert.Equal(t, int32(1), rec.Column(1).(*array.Int32).Value(1))
	assert.Equal(t, "g", rec.Column(2).(*array.String).Value(1))
	assert.Equal(t, "ab", rec.Column(3).(*array.String).Value(0))
	assert.Equal(t, int32(-1), rec.Column(4).(*array.Int32).Value(1), "a group that did not participate carries -1")
	assert.Equal(t, uint8(0), rec.Column(6).(*array.Uint8).Value(1))
}

func TestEncodeChExtractRecordShape(t *testing.T) {
	rows := []chExtractRow{
		{MatchIdx: 0, GroupIdx: 0, Text: "ab"},
		{MatchIdx: 0, GroupIdx: 1, Text: "a"},
	}
	raw, err := encodeChExtract(rows)
	require.NoError(t, err)

	schema, rec := readIPCStream(t, raw)
	assert.Equal(t, []string{"match_idx", "group_idx", "text"}, fieldNames(schema))
	require.EqualValues(t, len(rows), rec.NumRows())
	assert.Equal(t, "a", rec.Column(2).(*array.String).Value(1))
}

// TestEncodeEmptyRowsStillCarriesSchema — a pattern that matched nothing
// must publish an empty table, not fail. "Zero rows" is a legitimate
// answer to compare against.
func TestEncodeEmptyRowsStillCarriesSchema(t *testing.T) {
	raw, err := encodeGoMatches(nil)
	require.NoError(t, err)
	schema, rec := readIPCStream(t, raw)
	assert.Len(t, schema.Fields(), 7)
	assert.EqualValues(t, 0, rec.NumRows())
}

// ---------------------------------------------------------------------------
// Seeded SQL
// ---------------------------------------------------------------------------

func TestBuildEvalSQLJoinsBothEngines(t *testing.T) {
	snap := evalSnapshot{pattern: `(a)`, haystack: "ab", hasCH: true}
	sql := buildEvalSQL(snap, evalHandles{goHandle: "h_go", chHandle: "h_ch"})

	assert.Contains(t, sql, "FROM keelson('h_go') AS g")
	assert.Contains(t, sql, "FULL OUTER JOIN keelson('h_ch') AS c")
	assert.Contains(t, sql, "ON g.match_idx = c.match_idx AND g.group_idx = c.group_idx")
	assert.Contains(t, sql, "ORDER BY match_idx, group_idx")
	// Load-bearing, not decoration: without it a missing side comes back
	// as '' rather than NULL, which reads as agreement on an empty string.
	assert.Contains(t, sql, "SETTINGS join_use_nulls = 1")
	assert.Contains(t, sql, "AS go_text")
	assert.Contains(t, sql, "AS ch_text")
}

func TestBuildEvalSQLDegradesToGoAlone(t *testing.T) {
	snap := evalSnapshot{pattern: `(a)`, haystack: "ab"}
	sql := buildEvalSQL(snap, evalHandles{goHandle: "h_go"})

	assert.Contains(t, sql, "FROM keelson('h_go')")
	assert.NotContains(t, sql, "FULL OUTER JOIN")
	assert.NotContains(t, sql, "join_use_nulls")
	// A partial result has to say it is partial (ADR-0017 §SD3).
	assert.Contains(t, sql, "only the Go side was published")
}

// TestBuildEvalSQLHeaderStaysCommented — a pattern or haystack with a
// newline in it must not break out of the `--` comment and become SQL.
func TestBuildEvalSQLHeaderStaysCommented(t *testing.T) {
	snap := evalSnapshot{pattern: "a\nDROP TABLE x", haystack: "b\nc", hasCH: true}
	sql := buildEvalSQL(snap, evalHandles{goHandle: "h_go", chHandle: "h_ch"})

	for line := range strings.SplitSeq(sql, "\n") {
		if strings.Contains(line, "DROP TABLE") {
			assert.True(t, strings.HasPrefix(line, "--"),
				"a newline in the pattern escaped the comment: %q", line)
		}
	}
	assert.NotContains(t, sql, "\nDROP TABLE")
}

func TestSqlCommentTruncatesLongInput(t *testing.T) {
	out := sqlComment(strings.Repeat("x", 500))
	assert.LessOrEqual(t, len(out), 124)
	assert.NotContains(t, out, "\n")
}

// ---------------------------------------------------------------------------
// Publish + open, over a real adhocdata service
// ---------------------------------------------------------------------------

// stubKeyRegistrar stands in for the chlocalbroker key store: the
// adhocdata service is the policy owner and only registers/deregisters
// per-dataset keys, which is all these tests need.
type stubKeyRegistrar struct {
	mu   sync.Mutex
	keys map[string][]byte
}

func (s *stubKeyRegistrar) RegisterDatasetKey(name string, key []byte) {
	s.mu.Lock()
	if s.keys == nil {
		s.keys = map[string][]byte{}
	}
	s.keys[name] = key
	s.mu.Unlock()
}

func (s *stubKeyRegistrar) DeregisterDatasetKey(name string) {
	s.mu.Lock()
	delete(s.keys, name)
	s.mu.Unlock()
}

func (s *stubKeyRegistrar) live() (n int) {
	s.mu.Lock()
	n = len(s.keys)
	s.mu.Unlock()
	return
}

// capturedOpen is what the stub window host saw.
type capturedOpen struct {
	mu   sync.Mutex
	reqs []launchrequest.LaunchRequest
}

func (c *capturedOpen) all() (out []launchrequest.LaunchRequest) {
	c.mu.Lock()
	out = append(out, c.reqs...)
	c.mu.Unlock()
	return
}

// evalTestRig wires an inprocbus, a real adhocdata.Service, and a stub
// windowhost.open subscriber, and hands back an App bound to a client
// carrying the three caps the manifest declares.
type evalTestRig struct {
	app   *App
	keys  *stubKeyRegistrar
	opens *capturedOpen
	// svc is the real service, kept for [evalTestRig.flushRetracts]: a
	// retract only leaves the dataset queryable for RetractGrace, and the
	// key stays registered until the UNLOAD step (ADR-0188 §SD3).
	svc *adhocdata.Service
}

// flushRetracts runs the pending UNLOAD steps now, so a test can assert on
// what a retract eventually costs without waiting out the grace.
func (rig *evalTestRig) flushRetracts() {
	rig.svc.FlushRetracts()
}

func setupEvalRig(t *testing.T) (rig *evalTestRig) {
	t.Helper()
	logger := zerolog.New(zerolog.NewTestWriter(t))
	bus := inprocbus.NewInst(logger)
	bus.SetRequestTimeout(10 * time.Second)

	keys := &stubKeyRegistrar{}
	svc, err := adhocdata.NewService(adhocdata.Config{
		Bus:      bus,
		Registry: introspect.NewRegistry(),
		Keys:     keys,
		Dir:      t.TempDir(),
		Log:      logger,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close(t.Context()) })

	// Stand in for windowhost's OpenService: capture the request and
	// reply with a window key, so RequestOpen's round-trip completes.
	opens := &capturedOpen{}
	hostClient := bus.NewClient("test.windowhost", []runtimeapp.SubjectFilter{
		{Pattern: windowhost.OpenSubject, Direction: runtimeapp.CapDirectionSub, Reason: "test"},
		{Pattern: inprocbus.InboxPrefix + ">", Direction: runtimeapp.CapDirectionPub, Reason: "test"},
	})
	unsub, err := hostClient.Subscribe(windowhost.OpenSubject, func(msg *runtimeapp.Msg) {
		req, decErr := buscodec.Decode[launchrequest.LaunchRequest](msg.Payload)
		if decErr != nil {
			_ = buscodec.Reply(hostClient.Publish, msg.Reply,
				launchreply.LaunchReply{Reason: "decode: " + decErr.Error()})
			return
		}
		opens.mu.Lock()
		opens.reqs = append(opens.reqs, req)
		opens.mu.Unlock()
		_ = buscodec.Reply(hostClient.Publish, msg.Reply, launchreply.LaunchReply{WindowKey: 42})
	})
	require.NoError(t, err)
	t.Cleanup(unsub)

	inst := newApp()
	inst.setBus(bus.NewClient("test.regex_explorer", []runtimeapp.SubjectFilter{
		{Pattern: adhocdata.SubjectPublish, Direction: runtimeapp.CapDirectionPub, Reason: "test"},
		{Pattern: adhocdata.SubjectRetract, Direction: runtimeapp.CapDirectionPub, Reason: "test"},
		{Pattern: windowhost.OpenSubject, Direction: runtimeapp.CapDirectionPub, Reason: "test"},
	}))

	rig = &evalTestRig{app: inst, keys: keys, opens: opens, svc: svc}
	return
}

func TestEvalHandoffPublishesBothAndOpensPlay(t *testing.T) {
	rig := setupEvalRig(t)
	inst := rig.app
	inst.pattern = `(a)`
	inst.haystack = "ab a"

	snap, err := inst.snapshotEval()
	require.NoError(t, err)
	// Stand in for a fresh CH lane result: the lane itself needs a live
	// broker, and what matters here is that the second dataset ships.
	snap.hasCH = true
	snap.chRows = chExtractRows(listOutcome{Matches: []string{"a", "a"}, YieldsGroups: true})

	inst.requestEvalInPlay(snap)

	inst.mu.RLock()
	evalErr, goHandle, chHandle := inst.evalErr, inst.evalGoHandle, inst.evalChHandle
	busy := inst.evalBusy
	inst.mu.RUnlock()
	require.Empty(t, evalErr)
	assert.False(t, busy, "the worker must clear busy when it finishes")
	require.NotEmpty(t, goHandle)
	require.NotEmpty(t, chHandle)
	assert.NotEqual(t, goHandle, chHandle, "two datasets, two handles")

	reqs := rig.opens.all()
	require.Len(t, reqs, 1)
	assert.Equal(t, launchcfg.AppId, reqs[0].TargetAppId)
	assert.Equal(t, launchcfg.Kind, reqs[0].ConfigKind)

	cfg, err := buscodec.Decode[launchcfg.PlayLaunch](reqs[0].Config)
	require.NoError(t, err)
	assert.True(t, cfg.AutoRun, "the join should be on screen when the window appears")
	// Ad-hoc handles only resolve at the in-process keelson endpoint.
	assert.Equal(t, launchcfg.EndpointIntrospection, cfg.Endpoint)
	assert.Contains(t, cfg.Sql, "keelson('"+goHandle+"')")
	assert.Contains(t, cfg.Sql, "keelson('"+chHandle+"')")
	assert.Contains(t, cfg.Sql, "FULL OUTER JOIN")
}

// TestEvalHandoffReusesHandles pins the MaxDatasets discipline: a second
// hand-off from the same window republishes under the handles it already
// holds rather than minting new ones.
func TestEvalHandoffReusesHandles(t *testing.T) {
	rig := setupEvalRig(t)
	inst := rig.app
	inst.pattern = `(a)`
	inst.haystack = "ab a"

	run := func() (goHandle string, chHandle string) {
		snap, err := inst.snapshotEval()
		require.NoError(t, err)
		snap.hasCH = true
		snap.chRows = chExtractRows(listOutcome{Matches: []string{"a"}})
		inst.requestEvalInPlay(snap)
		inst.mu.RLock()
		defer inst.mu.RUnlock()
		require.Empty(t, inst.evalErr)
		return inst.evalGoHandle, inst.evalChHandle
	}

	firstGo, firstCh := run()
	secondGo, secondCh := run()
	assert.Equal(t, firstGo, secondGo)
	assert.Equal(t, firstCh, secondCh)
	assert.Equal(t, 2, rig.keys.live(), "one window holds at most two datasets")
}

func TestEvalHandoffDegradesWithoutClickHouseResult(t *testing.T) {
	rig := setupEvalRig(t)
	inst := rig.app
	inst.pattern = `(a)`
	inst.haystack = "ab"

	snap, err := inst.snapshotEval()
	require.NoError(t, err)
	require.False(t, snap.hasCH)

	inst.requestEvalInPlay(snap)

	inst.mu.RLock()
	evalErr, goHandle, chHandle := inst.evalErr, inst.evalGoHandle, inst.evalChHandle
	inst.mu.RUnlock()
	require.Empty(t, evalErr, "a missing CH result is a partial answer, not a failure")
	assert.NotEmpty(t, goHandle)
	assert.Empty(t, chHandle, "nothing to publish for the CH side")
	assert.Equal(t, 1, rig.keys.live())

	reqs := rig.opens.all()
	require.Len(t, reqs, 1)
	cfg, err := buscodec.Decode[launchcfg.PlayLaunch](reqs[0].Config)
	require.NoError(t, err)
	assert.NotContains(t, cfg.Sql, "FULL OUTER JOIN")
	assert.Contains(t, cfg.Sql, "only the Go side was published")
}

func TestRetractEvalDatasetsDropsBothHandles(t *testing.T) {
	rig := setupEvalRig(t)
	inst := rig.app
	inst.pattern = `(a)`
	inst.haystack = "ab"

	snap, err := inst.snapshotEval()
	require.NoError(t, err)
	snap.hasCH = true
	snap.chRows = chExtractRows(listOutcome{Matches: []string{"a"}})
	inst.requestEvalInPlay(snap)
	require.Equal(t, 2, rig.keys.live())

	inst.retractEvalDatasets()
	// The app gives the handles back at once; the service holds the keys for
	// RetractGrace so an in-flight query still resolves (ADR-0188 §SD3).
	rig.flushRetracts()
	assert.Equal(t, 0, rig.keys.live())

	inst.mu.RLock()
	defer inst.mu.RUnlock()
	assert.Empty(t, inst.evalGoHandle)
	assert.Empty(t, inst.evalChHandle)
}

// TestRetractEvalDatasetsIsSafeWhenNothingPublished — Unmount calls this
// unconditionally, including on a window that never clicked the button.
func TestRetractEvalDatasetsIsSafeWhenNothingPublished(t *testing.T) {
	inst := newApp()
	inst.setBus(&runtimeapp.NoopBus{})
	inst.retractEvalDatasets()
	inst.retractEvalDatasets()
}

// ---------------------------------------------------------------------------
// Failure and staleness — found by adversarial review, pinned here
// ---------------------------------------------------------------------------

// TestPartialPublishRetainsTheHandleItMinted — the two publishes fail
// independently, and the ClickHouse one can hit the very quota the Go one
// just made tighter. A handle minted but not recorded is one nothing can
// retract: Unmount cannot see it, and every retry mints another.
//
// Driven by the MaxDatasets quota: fill the service to one slot short, so
// the Go publish takes the last slot and the ClickHouse publish is refused.
func TestPartialPublishRetainsTheHandleItMinted(t *testing.T) {
	rig := setupEvalRig(t)
	inst := rig.app
	inst.pattern = `(a)`
	inst.haystack = "ab"

	filler, err := encodeChExtract([]chExtractRow{{MatchIdx: 0, GroupIdx: 0, Text: "x"}})
	require.NoError(t, err)
	bus := inst.busSnapshot()
	for i := range adhocdata.MaxDatasets - 1 {
		_, pErr := adhocdata.PublishRequest(bus, adhocdata.PublishInput{
			Alias:          fmt.Sprintf("filler_%d", i),
			ArrowIPCStream: filler,
		})
		require.NoErrorf(t, pErr, "filler %d", i)
	}
	require.Equal(t, adhocdata.MaxDatasets-1, rig.keys.live())

	snap, err := inst.snapshotEval()
	require.NoError(t, err)
	snap.hasCH = true
	snap.chRows = chExtractRows(listOutcome{Matches: []string{"a"}})

	inst.requestEvalInPlay(snap)

	inst.mu.RLock()
	evalErr, goHandle := inst.evalErr, inst.evalGoHandle
	inst.mu.RUnlock()
	require.NotEmpty(t, evalErr, "the ClickHouse publish must have been refused")
	assert.NotEmptyf(t, goHandle,
		"the Go dataset was published (live=%d) but its handle was not retained — nothing could retract it",
		rig.keys.live())

	// Retrying republishes under the recorded handle rather than minting
	// a fresh one each time.
	before := rig.keys.live()
	for range 3 {
		inst.requestEvalInPlay(snap)
	}
	assert.Equalf(t, before, rig.keys.live(),
		"repeated failing hand-offs minted more datasets: %d -> %d live", before, rig.keys.live())

	// And what it holds, it gives back — once the grace it was retracted
	// under has been flushed (ADR-0188 §SD3).
	inst.retractEvalDatasets()
	rig.flushRetracts()
	assert.Equal(t, adhocdata.MaxDatasets-1, rig.keys.live(),
		"the app's own dataset was not retracted")
}

// TestStatusIsRetiredWhenTheInputsChange — every other result surface in
// this window refuses to present an answer as describing inputs it does
// not (see queryLane); the hand-off's outcome is held to the same rule.
func TestStatusIsRetiredWhenTheInputsChange(t *testing.T) {
	rig := setupEvalRig(t)
	inst := rig.app
	inst.pattern = `(a)`
	inst.haystack = "ab a"

	snap, err := inst.snapshotEval()
	require.NoError(t, err)
	inst.requestEvalInPlay(snap)

	_, shown, _ := inst.evalStatusView(inst.singleKey())
	require.NotEmpty(t, shown, "the outcome describes what is on screen and must be visible")

	inst.pattern = `(zzz)`
	inst.haystack = "nothing here"

	_, stale, staleErr := inst.evalStatusView(inst.singleKey())
	assert.Emptyf(t, stale, "status %q still shown after the inputs changed", stale)
	assert.Empty(t, staleErr)
}

// TestDegradedStatusSaysClickHouseWasNotAsked — "0 ClickHouse row(s)"
// reads as an answer, and this surface exists to make the two engines'
// answers comparable.
func TestDegradedStatusSaysClickHouseWasNotAsked(t *testing.T) {
	rig := setupEvalRig(t)
	inst := rig.app
	inst.pattern = `(a)`
	inst.haystack = "ab"

	snap, err := inst.snapshotEval()
	require.NoError(t, err)
	require.False(t, snap.hasCH)
	inst.requestEvalInPlay(snap)

	_, status, _ := inst.evalStatusView(inst.singleKey())
	assert.NotContains(t, status, "0 ClickHouse row(s)")
	assert.Contains(t, status, "no result for this input")
}

// TestSqlCommentTruncatesOnRuneBoundaries — the header is cut to a length
// cap, and a regex explorer is exactly where someone pastes a Unicode
// pattern. A byte-offset cut splits a rune and the invalid UTF-8 rides
// into play's editor buffer.
func TestSqlCommentTruncatesOnRuneBoundaries(t *testing.T) {
	for _, s := range []string{
		strings.Repeat("α", 200),
		strings.Repeat("日", 200),
		strings.Repeat("x", 200),
		"\xff\xfe" + strings.Repeat("é", 200),
	} {
		out := sqlComment(s)
		assert.Truef(t, utf8.ValidString(out), "sqlComment produced invalid UTF-8: %q", out)
		assert.NotContains(t, out, "\n", "the header must stay one line")
	}
}

func TestSeededSqlIsValidUtf8(t *testing.T) {
	snap := evalSnapshot{pattern: strings.Repeat("α", 200), haystack: strings.Repeat("日", 200), hasCH: true}
	sql := buildEvalSQL(snap, evalHandles{goHandle: "h_go", chHandle: "h_ch"})
	assert.True(t, utf8.ValidString(sql), "seeded SQL carries invalid UTF-8")
}

func TestEvalHandoffReportsMissingBus(t *testing.T) {
	inst := newApp()
	inst.setBus(&runtimeapp.NoopBus{})
	inst.pattern = `a`
	inst.haystack = "ab"

	snap, err := inst.snapshotEval()
	require.NoError(t, err)
	inst.requestEvalInPlay(snap)

	inst.mu.RLock()
	defer inst.mu.RUnlock()
	assert.NotEmpty(t, inst.evalErr, "a refused publish must surface, not silently do nothing")
	assert.False(t, inst.evalBusy)
}
