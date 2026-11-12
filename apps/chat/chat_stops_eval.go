package chat

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json/v2"
	"slices"
	"strconv"
	"strings"
	"time"

	"lukechampine.com/blake3"

	"github.com/stergiotis/boxer/public/keelson/data/chlocalbroker"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun/projection"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

// Evaluating stops (ADR-0370 §SD1, proposed): the run's entries, the tool
// calls its replies asked for, the artefact and a clock go to the host's
// local SQL worker as four tables, and one statement returns every stop's
// rows. A row's text is its identity; the worker returns it as the JSON of
// its columns in the statement's order.

// stopsPool is the local worker pool the checks run in.
const stopsPool = "chatstops"

// stopCheckTimeout bounds one check; past it every stop is unevaluated,
// which stops the turn.
const stopCheckTimeout = 20 * time.Second

// StopsCaps is the capability a window whose conversations carry stops
// needs: a publish on the pool the checks run in.
func StopsCaps() []app.SubjectFilter {
	return []app.SubjectFilter{{Pattern: chlocalbroker.SubjectExecPrefix + stopsPool, Direction: app.CapDirectionPub,
		Reason: "chat: evaluate the conversation's stops over its own run in the host's local SQL worker (ADR-0370)"}}
}

// stopRunnerI runs a statement over input tables in a local worker; the
// projection's twin runs through the same seam.
type stopRunnerI = projection.RunnerI

// stopInputs is what a check reads: the run so far, the artefact's head and
// where the turn stands in time.
type stopInputs struct {
	entries  []agentrun.Entry
	artefact string
	revision int
	turn     string
	round    int
	// working is the turn's elapsed time without what it waited on the
	// person; elapsed is the wall clock since it started.
	working time.Duration
	elapsed time.Duration
	now     time.Time
}

// tables are the four relations as Arrow files.
func (inst stopInputs) tables() (t map[string][]byte, err error) {
	t = make(map[string][]byte, len(stopRelations))
	if t[stopRelJournal], err = agentrun.EntriesArrow(inst.entries); err != nil {
		return nil, err
	}
	type call struct {
		ordinal  uint64
		turn     string
		position uint64
		name     string
		args     string
	}
	var calls []call
	for _, e := range inst.entries {
		if e.Kind != agentrun.KindReply || e.Calls == "" {
			continue
		}
		for i, tc := range projection.ParseCalls(e.Calls) {
			calls = append(calls, call{ordinal: uint64(e.Ordinal), turn: e.Turn, position: uint64(i), name: tc.Name, args: tc.Arguments})
		}
	}
	file := func(tab *introspect.Table, n int) ([]byte, error) {
		rec := tab.Build(introspect.Projection{}, n)
		defer rec.Release()
		return introspect.EncodeFile(rec)
	}
	if t[stopRelToolcalls], err = file(introspect.NewTable().
		Uint64("ordinal", func(i int) uint64 { return calls[i].ordinal }).
		String("turn", func(i int) string { return calls[i].turn }).
		Uint64("position", func(i int) uint64 { return calls[i].position }).
		String("name", func(i int) string { return calls[i].name }).
		String("arguments", func(i int) string { return calls[i].args }), len(calls)); err != nil {
		return nil, err
	}
	lines := 0
	if inst.artefact != "" {
		lines = strings.Count(inst.artefact, "\n") + 1
	}
	if t[stopRelArtefact], err = file(introspect.NewTable().
		String("text", func(int) string { return inst.artefact }).
		Uint64("bytes", func(int) uint64 { return uint64(len(inst.artefact)) }).
		Uint64("lines", func(int) uint64 { return uint64(lines) }).
		Uint64("revision", func(int) uint64 { return uint64(max(inst.revision, 0)) }), 1); err != nil {
		return nil, err
	}
	if t[stopRelClock], err = file(introspect.NewTable().
		String("turn", func(int) string { return inst.turn }).
		Uint64("round", func(int) uint64 { return uint64(max(inst.round, 0)) }).
		Uint64("working_ms", func(int) uint64 { return uint64(max(inst.working.Milliseconds(), 0)) }).
		Uint64("turn_ms", func(int) uint64 { return uint64(max(inst.elapsed.Milliseconds(), 0)) }).
		Int64("now_ms", func(int) int64 { return inst.now.UnixMilli() }), 1); err != nil {
		return nil, err
	}
	return
}

// stopRow is one row a stop returned: its text, and the id the record
// keeps of it.
type stopRow struct {
	id   string
	text string
}

// stopResult is what one stop returned at a check, or why it could not be
// evaluated.
type stopResult struct {
	name string
	rows []stopRow
	err  string
}

// stopRowId names a row by its text.
func stopRowId(text string) string {
	sum := blake3.Sum256([]byte(text))
	return hex.EncodeToString(sum[:8])
}

// stopSelect wraps one stop's statement so its rows come back as text under
// its name. One more row than a stop may return is asked for, so an
// overflow is seen and not cut.
func stopSelect(s stop) string {
	return "SELECT " + sqlString(s.spec.Name) + " AS stop, r AS row FROM (SELECT formatRow('JSONEachRow', *) AS r FROM (\n" + s.sql + "\n) LIMIT " + strconv.Itoa(maxStopRows+1) + ")"
}

// evalStops runs every stop of set over in. A stop that cannot be
// evaluated comes back with its error, never as quiet (ADR-0370 §SD4).
// The stops go to the worker as one statement; when that fails, each is
// run alone so the error lands on the stop that caused it.
func evalStops(ctx context.Context, runner stopRunnerI, set stopSet, in stopInputs) (results []stopResult) {
	if len(set) == 0 {
		return
	}
	all := func(why string) []stopResult {
		for _, s := range set {
			results = append(results, stopResult{name: s.spec.Name, err: why})
		}
		return results
	}
	if runner == nil {
		return all("this host runs no local SQL worker, so the stop cannot be checked")
	}
	tables, err := in.tables()
	if err != nil {
		return all("the run could not be handed to the worker: " + err.Error())
	}
	ctx, cancel := context.WithTimeout(ctx, stopCheckTimeout)
	defer cancel()
	parts := make([]string, 0, len(set))
	for _, s := range set {
		parts = append(parts, stopSelect(s))
	}
	out, err := runner.Run(ctx, strings.Join(parts, "\nUNION ALL\n"), tables)
	if err == nil {
		if rows, derr := decodeStopRows(out); derr == nil {
			return collectStopRows(set, rows, nil)
		} else {
			err = derr
		}
	}
	if len(set) == 1 {
		return all(stopErrorText(err))
	}
	// One statement failed the union: find which.
	rows, failed := make(map[string][]string), make(map[string]string)
	for _, s := range set {
		one, oerr := runner.Run(ctx, stopSelect(s), tables)
		if oerr == nil {
			var decoded map[string][]string
			if decoded, oerr = decodeStopRows(one); oerr == nil {
				rows[s.spec.Name] = decoded[s.spec.Name]
				continue
			}
		}
		failed[s.spec.Name] = stopErrorText(oerr)
	}
	return collectStopRows(set, rows, failed)
}

// stopErrorText is a check's error as the person reads it: one line.
func stopErrorText(err error) string {
	return clip(strings.Join(strings.Fields(err.Error()), " "), 400)
}

// decodeStopRows reads the worker's JSON lines into each stop's row texts.
func decodeStopRows(out []byte) (rows map[string][]string, err error) {
	rows = make(map[string][]string)
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 0, 1<<16), 16<<20)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var r struct {
			Stop string `json:"stop"`
			Row  string `json:"row"`
		}
		if err = json.Unmarshal(line, &r); err != nil {
			return nil, err
		}
		rows[r.Stop] = append(rows[r.Stop], strings.TrimSpace(r.Row))
	}
	return rows, sc.Err()
}

// collectStopRows orders each stop's rows and refuses an overflow.
func collectStopRows(set stopSet, rows map[string][]string, failed map[string]string) (results []stopResult) {
	for _, s := range set {
		res := stopResult{name: s.spec.Name, err: failed[s.spec.Name]}
		texts := rows[s.spec.Name]
		switch {
		case res.err != "":
		case len(texts) > maxStopRows:
			res.err = "the stop returns more than " + strconv.Itoa(maxStopRows) + " rows; narrow it, since its rows are what is acknowledged"
		default:
			for _, text := range texts {
				res.rows = append(res.rows, stopRow{id: stopRowId(text), text: text})
			}
			slices.SortFunc(res.rows, func(a, b stopRow) int { return strings.Compare(a.id, b.id) })
			res.rows = slices.CompactFunc(res.rows, func(a, b stopRow) bool { return a.id == b.id })
		}
		results = append(results, res)
	}
	return
}
