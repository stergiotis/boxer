package regex_explorer

// Extraction hand-off to the SQL playground (ADR-0017).
//
// The Preview tab knows every match and capture group with byte offsets
// (Go's regexp, the ADR-0054 offset authority); the List tab knows what
// ClickHouse returned from extractAll / extractAllGroups. Neither is
// queryable, and the two can disagree — extractAll returns capture group
// 1, not the full match, whenever the pattern captures.
//
// This publishes *both* as ad-hoc datasets (ADR-0134) and opens a play
// window (ADR-0135) seeded with a FULL OUTER JOIN over them, so the
// disagreement becomes something the user can query over their own
// pattern rather than a caveat in a UI label.
//
// Threading follows the imzero2 rule: the render thread snapshots both
// result sets into plain data ([App.snapshotEval]), the worker encodes,
// publishes and opens. Nothing here calls c.*.

import (
	"bytes"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/apps/play/launchcfg"
	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/windowhost"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// Dataset aliases. The alias is advisory — the seeded SQL names the
// minted handle, which is what the endpoint resolves — but it is what
// shows up in the introspection catalogue, so it should read.
const (
	goDatasetAlias = "regex_matches"
	chDatasetAlias = "regex_ch_extract"
)

// goMatchRow is one (match, group) pair as Go's regexp sees it. Group 0
// is the whole match.
type goMatchRow struct {
	MatchIdx  int32
	GroupIdx  int32
	GroupName string
	Text      string
	// StartByte / StopByte are -1 for a group that did not participate
	// in this match, matching FindAllStringSubmatchIndex's convention.
	StartByte int32
	StopByte  int32
	Matched   uint8
}

// chExtractRow is one element ClickHouse returned, keyed to join against
// [goMatchRow]: group_idx 0 is the extractAll element, k>=1 is
// extractAllGroups[match][k].
type chExtractRow struct {
	MatchIdx int32
	GroupIdx int32
	Text     string
}

// evalSnapshot is everything the worker needs, taken on the render
// thread. Plain data only — no lane, no compiled regexp, no c.* handle.
type evalSnapshot struct {
	// key fingerprints the inputs this snapshot describes, so the status
	// it produces can be retired when the editors move on — the same
	// freshness rule [queryLane] applies to every other result surface in
	// this window.
	key      queryKey
	pattern  string
	haystack string
	goRows   []goMatchRow
	chRows   []chExtractRow
	// hasCH records that the CH lane held a result describing this exact
	// input. False means the seeded SQL degrades to the Go side alone
	// (ADR-0017 §SD3) — a partial answer, labelled, rather than a failed
	// click.
	hasCH bool
}

// snapshotEval gathers both engines' current extraction into plain data.
// Render-thread only: it reads the input fields and the CH lane, and it
// recomputes the Go side through the compile cache.
//
// The Go side drops zero-width whole matches, exactly as
// [nonEmptyMatches] does for the preview and the status bar. That is not
// cosmetic here: Go enumerates repeated empty matches and ClickHouse does
// not (pattern `a*` over "xyz" — four for Go, none for ClickHouse), so
// keeping them would shift every match_idx and make the join compare
// unrelated rows. A capture group that participated but matched the
// empty string still gets a row, with matched=1 and an empty text.
func (inst *App) snapshotEval() (snap evalSnapshot, err error) {
	if inst.haystack == "" {
		err = eh.Errorf("nothing to hand off: the haystack is empty")
		return
	}
	switch inst.patternState() {
	case patternEmpty:
		err = eh.Errorf("nothing to hand off: no pattern entered")
		return
	case patternInvalid:
		err = eh.Errorf("nothing to hand off: the pattern does not compile")
		return
	}

	snap.key = inst.singleKey()
	snap.pattern = inst.effectivePattern(inst.pattern)
	snap.haystack = inst.haystack

	re, compileErr := inst.getCompiledRegexp(snap.pattern)
	if compileErr != nil || re == nil {
		err = eh.Errorf("compile pattern: %w", compileErr)
		return
	}
	names := re.SubexpNames()
	matchIdx := int32(0)
	for _, m := range re.FindAllStringSubmatchIndex(snap.haystack, -1) {
		if len(m) < 2 || m[0] == m[1] {
			// Zero-width whole match — see the doc comment.
			continue
		}
		for k := 0; k*2+1 < len(m); k++ {
			start, stop := m[2*k], m[2*k+1]
			row := goMatchRow{
				MatchIdx:  matchIdx,
				GroupIdx:  int32(k),
				GroupName: subexpName(names, k),
				StartByte: -1,
				StopByte:  -1,
			}
			if start >= 0 && stop >= 0 {
				row.Text = snap.haystack[start:stop]
				row.StartByte = int32(start)
				row.StopByte = int32(stop)
				row.Matched = 1
			}
			snap.goRows = append(snap.goRows, row)
		}
		matchIdx++
	}

	// The CH half, only when the lane's result describes what is on
	// screen. A stale answer joined against a fresh Go side would report
	// disagreements that are really just latency.
	if view := inst.listLane.view(inst.singleKey()); view.Has && view.Fresh && view.Err == nil {
		snap.hasCH = true
		snap.chRows = chExtractRows(view.Value)
	}
	return
}

// subexpName returns group k's (?P<name>…) name, or "" when it has none.
// names[0] is always empty — the whole match has no name.
func subexpName(names []string, k int) (name string) {
	if k < len(names) {
		name = names[k]
	}
	return
}

// chExtractRows flattens a [listOutcome] into join-shaped rows.
func chExtractRows(out listOutcome) (rows []chExtractRow) {
	rows = make([]chExtractRow, 0, len(out.Matches))
	for i, m := range out.Matches {
		rows = append(rows, chExtractRow{MatchIdx: int32(i), GroupIdx: 0, Text: m})
	}
	for i, groups := range out.Groups {
		for k, g := range groups {
			// extractAllGroups reports groups only, 0-based; capture
			// group numbering is 1-based, so shift.
			rows = append(rows, chExtractRow{MatchIdx: int32(i), GroupIdx: int32(k + 1), Text: g})
		}
	}
	return
}

// requestEvalInPlay is the click handler's worker body: encode both
// datasets, publish them, compose the seeded SQL, and open a play window
// on it. Blocks on three bus round-trips, so it must not run on the
// render thread.
//
// Status lands in the eval* fields under mu for the affordance row to
// surface. A re-click while in flight is dropped by the caller.
func (inst *App) requestEvalInPlay(snap evalSnapshot) {
	handles, err := inst.publishEvalDatasets(snap)
	if err == nil {
		err = inst.openEvalPlayground(buildEvalSQL(snap, handles))
	}

	inst.mu.Lock()
	inst.evalBusy = false
	inst.evalKey = snap.key
	if err != nil {
		inst.evalErr = err.Error()
		inst.evalStatus = ""
	} else {
		inst.evalErr = ""
		inst.evalStatus = evalStatusLine(snap)
	}
	inst.mu.Unlock()
}

// evalStatusLine describes what was published. The degraded path says
// ClickHouse was never asked rather than reporting zero rows for it —
// "0 ClickHouse row(s)" reads as an answer, and this surface exists to
// make the two engines' answers comparable.
func evalStatusLine(snap evalSnapshot) (line string) {
	if !snap.hasCH {
		line = fmt.Sprintf("opened a playground over %d Go row(s); ClickHouse had no result for this input, so only the Go side was published",
			len(snap.goRows))
		return
	}
	line = fmt.Sprintf("opened a playground over %d Go row(s) / %d ClickHouse row(s)",
		len(snap.goRows), len(snap.chRows))
	return
}

// evalStatusView is the render-side snapshot of the hand-off's outcome
// against the inputs currently on screen — the [laneView.Fresh] rule,
// applied to the one result surface in this window that is not a lane. An
// outcome describing inputs the user has edited past is dropped rather
// than presented as current.
func (inst *App) evalStatusView(want queryKey) (busy bool, status string, errText string) {
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	busy = inst.evalBusy
	if inst.evalKey != want {
		return
	}
	status, errText = inst.evalStatus, inst.evalErr
	return
}

// evalHandles are the two published dataset handles. chHandle is empty on
// the degraded path (ADR-0017 §SD3).
type evalHandles struct {
	goHandle string
	chHandle string
}

// publishEvalDatasets publishes both datasets, reusing this instance's
// prior handles so republishing does not consume a new slot against the
// ADR-0134 MaxDatasets cap. One window therefore holds at most two.
func (inst *App) publishEvalDatasets(snap evalSnapshot) (handles evalHandles, err error) {
	bus := inst.busSnapshot()
	if bus == nil {
		err = eh.Errorf("no bus wired")
		return
	}

	stream, err := encodeGoMatches(snap.goRows)
	if err != nil {
		err = eh.Errorf("encode %s: %w", goDatasetAlias, err)
		return
	}
	inst.mu.RLock()
	priorGo, priorCh := inst.evalGoHandle, inst.evalChHandle
	inst.mu.RUnlock()

	res, err := adhocdata.PublishRequest(bus, adhocdata.PublishInput{
		Alias: goDatasetAlias, Handle: priorGo, ArrowIPCStream: stream,
	})
	if err != nil {
		err = eh.Errorf("publish %s: %w", goDatasetAlias, err)
		return
	}
	handles.goHandle = res.Handle
	// Record it before attempting the second publish, not after both
	// succeed. The two publishes fail independently — the ClickHouse one
	// can hit the MaxDatasets or byte quota that the first one just made
	// tighter — and a handle minted but not recorded is one nothing can
	// retract: Unmount cannot see it, and the next attempt mints another.
	inst.mu.Lock()
	inst.evalGoHandle = handles.goHandle
	inst.mu.Unlock()

	if snap.hasCH {
		var chStream []byte
		chStream, err = encodeChExtract(snap.chRows)
		if err != nil {
			err = eh.Errorf("encode %s: %w", chDatasetAlias, err)
			return
		}
		var chRes adhocdata.PublishResult
		chRes, err = adhocdata.PublishRequest(bus, adhocdata.PublishInput{
			Alias: chDatasetAlias, Handle: priorCh, ArrowIPCStream: chStream,
		})
		if err != nil {
			err = eh.Errorf("publish %s: %w", chDatasetAlias, err)
			return
		}
		handles.chHandle = chRes.Handle
		inst.mu.Lock()
		inst.evalChHandle = handles.chHandle
		inst.mu.Unlock()
	}
	return
}

// openEvalPlayground opens a play window on sql, bound to the
// introspection endpoint — the one that resolves `keelson('<handle>')`
// (ADR-0134). AutoRun so the join is on screen when the window appears.
func (inst *App) openEvalPlayground(sql string) (err error) {
	bus := inst.busSnapshot()
	if bus == nil {
		err = eh.Errorf("no bus wired")
		return
	}
	cfgBytes, err := buscodec.Encode(launchcfg.PlayLaunch{
		At:       time.Now().UTC(),
		Sql:      sql,
		AutoRun:  true,
		Endpoint: launchcfg.EndpointIntrospection,
	})
	if err != nil {
		err = eh.Errorf("encode launch config: %w", err)
		return
	}
	// launchcfg carries the app id so naming play does not drag the whole
	// playground — and its registry-registering init() — into every host
	// that links this app or the regexsummary widget (ADR-0017 §SD4).
	if _, err = windowhost.RequestOpen(bus, launchcfg.AppId, launchcfg.Kind, cfgBytes); err != nil {
		err = eh.Errorf("open playground: %w", err)
		return
	}
	return
}

// buildEvalSQL composes the seeded buffer. With both datasets present it
// is a FULL OUTER JOIN keyed on (match_idx, group_idx); with only the Go
// side it degrades to a plain SELECT that says so (ADR-0017 §SD3).
//
// `join_use_nulls = 1` is load-bearing, not decoration: without it a
// missing side comes back as the empty string rather than NULL, reading as
// agreement on an empty string — the exact failure this surface exists to
// expose.
func buildEvalSQL(snap evalSnapshot, handles evalHandles) (sql string) {
	var b strings.Builder
	b.WriteString("-- regex explorer: one extraction, both engines.\n")
	b.WriteString("--   pattern:  " + sqlComment(snap.pattern) + "\n")
	b.WriteString("--   haystack: " + sqlComment(snap.haystack) + "\n")

	if handles.chHandle == "" {
		b.WriteString("--\n")
		b.WriteString("-- ClickHouse had no result for this input when the hand-off ran\n")
		b.WriteString("-- (query in flight, or no bus), so only the Go side was published.\n")
		b.WriteString("-- Re-run the hand-off once the List tab shows a result to get the join.\n")
		b.WriteString("SELECT match_idx, group_idx, group_name, text, start_byte, stop_byte, matched\n")
		b.WriteString("FROM keelson('" + handles.goHandle + "')\n")
		b.WriteString("ORDER BY match_idx, group_idx\n")
		sql = b.String()
		return
	}

	b.WriteString("--   go_* — Go regexp (RE2), the byte-offset authority (ADR-0054)\n")
	b.WriteString("--   ch_* — ClickHouse extractAll/extractAllGroups, the engine being predicted\n")
	b.WriteString("-- A NULL on either side is a disagreement worth looking at. Note that\n")
	b.WriteString("-- extractAll returns capture group 1 — not the full match — whenever the\n")
	b.WriteString("-- pattern captures, so group_idx 0 lines up only for group-less patterns.\n")
	b.WriteString("SELECT coalesce(g.match_idx, c.match_idx) AS match_idx,\n")
	b.WriteString("       coalesce(g.group_idx, c.group_idx) AS group_idx,\n")
	b.WriteString("       g.group_name,\n")
	b.WriteString("       g.text AS go_text,\n")
	b.WriteString("       c.text AS ch_text,\n")
	b.WriteString("       g.start_byte, g.stop_byte\n")
	b.WriteString("FROM keelson('" + handles.goHandle + "') AS g\n")
	b.WriteString("FULL OUTER JOIN keelson('" + handles.chHandle + "') AS c\n")
	b.WriteString("  ON g.match_idx = c.match_idx AND g.group_idx = c.group_idx\n")
	b.WriteString("ORDER BY match_idx, group_idx\n")
	b.WriteString("SETTINGS join_use_nulls = 1\n")
	sql = b.String()
	return
}

// sqlComment renders s safely inside a single-line `--` comment: newlines
// would end the comment and turn the rest of the pattern into SQL, and a
// long haystack would bury the query. %q escapes both, so the header
// stays one line whatever the user typed.
//
// Truncation counts runes, not bytes. Cutting the %q output at a byte
// offset splits a multi-byte rune — which a regex explorer hits the
// moment someone pastes a Unicode-class pattern — and the invalid UTF-8
// then rides into play's editor buffer.
func sqlComment(s string) (out string) {
	const maxRunes = 100
	truncated := false
	if utf8.RuneCountInString(s) > maxRunes {
		i, n := 0, 0
		for i < len(s) && n < maxRunes {
			_, size := utf8.DecodeRuneInString(s[i:])
			i += size
			n++
		}
		s = s[:i]
		truncated = true
	}
	out = fmt.Sprintf("%q", s)
	if truncated {
		// %q always closes with a quote; reopen past the ellipsis.
		out = out[:len(out)-1] + `…"`
	}
	return
}

// goMatchesSchema / chExtractSchema are the two published shapes. Kept as
// package-level values so the encoders and the tests name one definition.
var goMatchesSchema = arrow.NewSchema([]arrow.Field{
	{Name: "match_idx", Type: arrow.PrimitiveTypes.Int32},
	{Name: "group_idx", Type: arrow.PrimitiveTypes.Int32},
	{Name: "group_name", Type: arrow.BinaryTypes.String},
	{Name: "text", Type: arrow.BinaryTypes.String},
	{Name: "start_byte", Type: arrow.PrimitiveTypes.Int32},
	{Name: "stop_byte", Type: arrow.PrimitiveTypes.Int32},
	{Name: "matched", Type: arrow.PrimitiveTypes.Uint8},
}, nil)

var chExtractSchema = arrow.NewSchema([]arrow.Field{
	{Name: "match_idx", Type: arrow.PrimitiveTypes.Int32},
	{Name: "group_idx", Type: arrow.PrimitiveTypes.Int32},
	{Name: "text", Type: arrow.BinaryTypes.String},
}, nil)

// encodeGoMatches renders the Go side as an Arrow IPC *stream* — no file
// footer, which is the form ADR-0134 expects. (The chlocalbroker
// InputTables path takes the *file* form; the two are easy to confuse and
// fail differently.)
func encodeGoMatches(rows []goMatchRow) (out []byte, err error) {
	return encodeRecord(goMatchesSchema, func(rb *array.RecordBuilder) {
		matchIdx := rb.Field(0).(*array.Int32Builder)
		groupIdx := rb.Field(1).(*array.Int32Builder)
		groupName := rb.Field(2).(*array.StringBuilder)
		text := rb.Field(3).(*array.StringBuilder)
		startByte := rb.Field(4).(*array.Int32Builder)
		stopByte := rb.Field(5).(*array.Int32Builder)
		matched := rb.Field(6).(*array.Uint8Builder)
		for _, r := range rows {
			matchIdx.Append(r.MatchIdx)
			groupIdx.Append(r.GroupIdx)
			groupName.Append(r.GroupName)
			text.Append(r.Text)
			startByte.Append(r.StartByte)
			stopByte.Append(r.StopByte)
			matched.Append(r.Matched)
		}
	})
}

// encodeChExtract renders the ClickHouse side as an Arrow IPC stream.
func encodeChExtract(rows []chExtractRow) (out []byte, err error) {
	return encodeRecord(chExtractSchema, func(rb *array.RecordBuilder) {
		matchIdx := rb.Field(0).(*array.Int32Builder)
		groupIdx := rb.Field(1).(*array.Int32Builder)
		text := rb.Field(2).(*array.StringBuilder)
		for _, r := range rows {
			matchIdx.Append(r.MatchIdx)
			groupIdx.Append(r.GroupIdx)
			text.Append(r.Text)
		}
	})
}

// encodeRecord builds one record batch through fill and writes it as an
// Arrow IPC stream. An empty row set still emits the schema, so a pattern
// that matched nothing publishes an empty table rather than failing —
// "zero rows" is a legitimate answer to compare.
func encodeRecord(schema *arrow.Schema, fill func(rb *array.RecordBuilder)) (out []byte, err error) {
	rb := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer rb.Release()
	fill(rb)
	rec := rb.NewRecordBatch()
	defer rec.Release()

	var buf bytes.Buffer
	w := ipc.NewWriter(&buf, ipc.WithSchema(schema))
	if err = w.Write(rec); err != nil {
		err = eh.Errorf("write arrow record: %w", err)
		return
	}
	if err = w.Close(); err != nil {
		err = eh.Errorf("close arrow stream: %w", err)
		return
	}
	out = buf.Bytes()
	return
}

// retractEvalDatasets drops both published handles. Called from Unmount
// (and [EmbeddedApp.Close]); safe to call more than once and safe when
// nothing was ever published.
func (inst *App) retractEvalDatasets() {
	inst.mu.Lock()
	goHandle, chHandle := inst.evalGoHandle, inst.evalChHandle
	inst.evalGoHandle, inst.evalChHandle = "", ""
	inst.mu.Unlock()
	if goHandle == "" && chHandle == "" {
		return
	}
	bus := inst.busSnapshot()
	if bus == nil {
		return
	}
	for _, h := range []string{goHandle, chHandle} {
		if h == "" {
			continue
		}
		// A retract failure is not actionable here — the window is going
		// away and the ephemeral store bounds the exposure either way.
		_ = adhocdata.RetractRequest(bus, h)
	}
}
