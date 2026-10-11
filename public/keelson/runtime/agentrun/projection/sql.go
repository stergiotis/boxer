package projection

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json/v2"
	"io"
	"slices"

	"github.com/stergiotis/boxer/public/keelson/data/chlocalbroker"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// The SQL projection (ADR-0309): the journal prefix ships as one Arrow
// table, `journal`, and the profile's section order as another,
// `section_order`; one statement returns the request, one row per
// message, in JSON lines the host decodes into a Batch. It is the fold's
// twin for the engine: the test lanes run it in a clickhouse-local
// worker through the broker and hold it to the fold's digest (law L4);
// production sends from the fold. The server-side view over the plain
// shape is not written yet.

// RunnerI runs one statement with input tables in a local worker and
// returns its JSON-lines output.
type RunnerI interface {
	Run(ctx context.Context, sql string, tables map[string][]byte) (out []byte, err error)
}

// BrokerRunner runs statements through the chlocal broker on the bus.
type BrokerRunner struct {
	Bus  app.BusI
	Pool string
}

var _ RunnerI = BrokerRunner{}

// Run submits the statement to the pool with the tables as temporary
// tables.
func (inst BrokerRunner) Run(ctx context.Context, sql string, tables map[string][]byte) (out []byte, err error) {
	rep, err := chlocalbroker.ExecOnPool(ctx, inst.Bus, inst.Pool, chlocalbroker.ExecRequest{SQL: sql, Format: "JSONEachRow", InputTables: tables})
	if err != nil {
		err = eh.Errorf("projection worker: %w", err)
		return
	}
	defer func() { _ = rep.Close() }()
	if rerr := rep.Err(); rerr != nil {
		err = eh.Errorf("projection worker: %w", rerr)
		return
	}
	return io.ReadAll(rep)
}

// SQL is the projection statement, one CTE per pass. It reads only the
// two input tables and no nondeterministic function; groupArray order is
// pinned by sorted subqueries under one thread.
const SQL = `
WITH
  sections AS (
    SELECT name AS sname, argMax(body, ord) AS sbody, max(ord) AS sord,
           argMax(level, ord) AS slevel, argMax(tier, ord) AS stier,
           argMax(exposure, ord) AS sexposure, argMax(origin, ord) AS sorigin
    FROM journal WHERE kind = 'section' GROUP BY name HAVING sbody != ''),
  placed AS (
    SELECT sname, sbody, sord, slevel, stier, sexposure, sorigin,
           if(o.name = '' OR o.name IS NULL, 1000000, o.pos) AS spos
    FROM sections AS s LEFT JOIN section_order AS o ON o.name = s.sname
    ORDER BY spos, sname),
  head AS (
    SELECT 0 AS part, toUInt32(0) AS ord, 'system' AS role,
           arrayStringConcat(groupArray(sbody), '\n\n') AS content,
           '' AS calls, '' AS call_key, '' AS tool_call_id, '' AS attachments,
           arrayMap(x -> toUInt32(x), groupArray(sord)) AS src, max(slevel) AS level, max(stier) AS tier,
           arrayStringConcat(arraySort(arrayDistinct(arrayFilter(x -> x != '', arrayFlatten(groupArray(splitByChar(',', sexposure)))))), ',') AS exposure,
           arrayStringConcat(arraySort(arrayDistinct(arrayFilter(x -> x != '', arrayFlatten(groupArray(splitByChar(',', sorigin)))))), ',') AS origin,
           groupArray(sname) AS sections
    FROM placed HAVING count() > 0),
  cut AS (
    SELECT argMax(target, ord) AS ccut, argMax(body, ord) AS csummary, max(ord) AS cord, argMax(run, ord) AS crun,
           argMax(level, ord) AS clevel, argMax(tier, ord) AS ctier, argMax(exposure, ord) AS cexposure, argMax(origin, ord) AS corigin
    FROM journal WHERE kind = 'compact'),
  summary AS (
    SELECT 1 AS part, toUInt32(cord) AS ord, 'system' AS role,
           concat('<recalled source="self" run="', replaceAll(replaceAll(replaceAll(replaceAll(crun, '&', '&amp;'), '"', '&quot;'), '<', '&lt;'), '>', '&gt;'),
                  '" ord="', toString(cord), '">\n', replaceRegexpAll(csummary, '(?i)</recalled', '&lt;/recalled'), '\n</recalled>') AS content,
           '' AS calls, '' AS call_key, '' AS tool_call_id, '' AS attachments,
           [toUInt32(cord)] AS src, greatest(clevel, toUInt64(3)) AS level, ctier AS tier, cexposure AS exposure, corigin AS origin, CAST([], 'Array(String)') AS sections
    FROM cut WHERE cord > 0),
  edits AS (
    SELECT target AS etarget, argMax(body, ord) AS ebody, argMax(form, ord) AS eform, max(ord) AS eord, argMax(attachments, ord) AS eattachments,
           argMax(level, ord) AS elevel, argMax(tier, ord) AS etier, argMax(exposure, ord) AS eexposure, argMax(origin, ord) AS eorigin,
           argMax(writer_run, ord) AS ewriter_run, argMax(writer_ordinal, ord) AS ewriter_ordinal
    FROM journal WHERE kind = 'edit' GROUP BY target),
  msgs AS (
    SELECT part, ord, role,
           if(reentry,
              concat('<recalled source="', if(wrun = run, 'self', 'other'), '" run="',
                     replaceAll(replaceAll(replaceAll(replaceAll(wrun, '&', '&amp;'), '"', '&quot;'), '<', '&lt;'), '>', '&gt;'),
                     '" ord="', toString(word), '">\n', replaceRegexpAll(raw, '(?i)</recalled', '&lt;/recalled'), '\n</recalled>'),
              raw) AS content,
           calls, call_key, tool_call_id, attachments, src,
           greatest(level0, if(reentry, if(wrun = run, toUInt64(3), toUInt64(4)), toUInt64(0))) AS level,
           tier, exposure, origin, sections
    FROM (
      SELECT 2 AS part, toUInt32(j.ord) AS ord, j.run AS run,
             multiIf(j.kind = 'reply', 'assistant', j.kind = 'outcome', 'tool', j.kind = 'note', 'system', 'user') AS role,
             if(e.etarget != 0, e.ebody, j.body) AS raw,
             if(e.etarget != 0 AND e.ewriter_run != '', e.ewriter_run, j.writer_run) AS wrun,
             if(e.etarget != 0 AND e.ewriter_run != '', e.ewriter_ordinal, j.writer_ordinal) AS word,
             j.kind != 'reply' AND wrun != '' AS reentry,
             j.calls AS calls, j.key AS call_key,
             if(j.kind = 'outcome', j.name, '') AS tool_call_id, if(e.etarget != 0, e.eattachments, j.attachments) AS attachments,
             if(e.etarget != 0, [toUInt32(j.ord), toUInt32(e.eord)], [toUInt32(j.ord)]) AS src,
             if(e.etarget != 0, greatest(j.level, e.elevel), j.level) AS level0,
             if(e.etarget != 0, greatest(j.tier, e.etier), j.tier) AS tier,
             arrayStringConcat(arraySort(arrayDistinct(arrayFilter(x -> x != '', arrayConcat(splitByChar(',', j.exposure), if(e.etarget != 0, splitByChar(',', e.eexposure), CAST([], 'Array(String)')))))), ',') AS exposure,
             arrayStringConcat(arraySort(arrayDistinct(arrayFilter(x -> x != '', arrayConcat(splitByChar(',', j.origin), if(e.etarget != 0, splitByChar(',', e.eorigin), CAST([], 'Array(String)')))))), ',') AS origin,
             CAST([], 'Array(String)') AS sections
      FROM journal AS j LEFT JOIN edits AS e ON e.etarget = j.ord
      WHERE j.kind IN ('user', 'reply', 'outcome', 'note')
        AND NOT (j.kind = 'outcome' AND j.role != 'tool')
        AND j.ord >= (SELECT if(count() = 0, 0, max(ccut)) FROM cut)
        AND NOT (e.etarget != 0 AND e.eform = 'drop'))),
  tools AS (
    SELECT 3 AS part, toUInt32(ord) AS ord, 'tools' AS role, body AS content, '' AS calls, '' AS call_key, '' AS tool_call_id, '' AS attachments,
           [toUInt32(ord)] AS src, toUInt64(0) AS level, toUInt64(0) AS tier, '' AS exposure, '' AS origin, CAST([], 'Array(String)') AS sections
    FROM journal WHERE kind = 'tools' ORDER BY ord DESC LIMIT 1)
SELECT part, ord, role, content, calls, call_key, tool_call_id, attachments, src, level, tier, exposure, origin, sections
FROM (SELECT * FROM head UNION ALL SELECT * FROM summary UNION ALL SELECT * FROM msgs UNION ALL SELECT * FROM tools)
ORDER BY part, ord
SETTINGS max_threads = 1, join_use_nulls = 0`

// JournalTable is the entries as the worker's `journal` input table, an
// Arrow file.
func JournalTable(entries []agentrun.Entry) (b []byte, err error) {
	rows := slices.Clone(entries)
	slices.SortFunc(rows, func(a, b agentrun.Entry) int { return int(a.Ordinal) - int(b.Ordinal) })
	t := introspect.NewTable().
		Uint64("ord", func(i int) uint64 { return uint64(rows[i].Ordinal) }).
		String("run", func(i int) string { return rows[i].Run }).
		String("kind", func(i int) string { return rows[i].Kind.String() }).
		String("key", func(i int) string { return rows[i].Key }).
		String("role", func(i int) string { return rows[i].Role }).
		String("body", func(i int) string { return rows[i].Body }).
		String("calls", func(i int) string { return rows[i].Calls }).
		String("name", func(i int) string { return rows[i].Name }).
		String("book", func(i int) string { return rows[i].Book }).
		String("form", func(i int) string { return rows[i].Form }).
		Uint64("target", func(i int) uint64 { return uint64(rows[i].Target) }).
		Uint64("from", func(i int) uint64 { return uint64(rows[i].From) }).
		Uint64("level", func(i int) uint64 { return uint64(rows[i].Label.Level) }).
		Uint64("tier", func(i int) uint64 { return uint64(rows[i].Label.Tier) }).
		String("exposure", func(i int) string { return agentrun.MarksString(rows[i].Label.Exposure) }).
		String("origin", func(i int) string { return agentrun.MarksString(rows[i].Label.Origin) }).
		String("attachments", func(i int) string { return rows[i].Attachments }).
		String("writer_run", func(i int) string { return rows[i].WriterRun }).
		Uint64("writer_ordinal", func(i int) uint64 { return uint64(rows[i].WriterOrdinal) })
	rec := t.Build(introspect.Projection{}, len(rows))
	defer rec.Release()
	return introspect.EncodeFile(rec)
}

// OrderTable is the section order as the worker's `section_order` input
// table.
func OrderTable(order Order) (b []byte, err error) {
	order = order.Distinct()
	t := introspect.NewTable().
		String("name", func(i int) string { return order[i] }).
		Uint64("pos", func(i int) uint64 { return uint64(i) })
	rec := t.Build(introspect.Projection{}, len(order))
	defer rec.Release()
	return introspect.EncodeFile(rec)
}

// Project runs the SQL projection over entries in the worker and decodes
// the rows.
func Project(ctx context.Context, runner RunnerI, entries []agentrun.Entry, order Order) (b Batch, err error) {
	journal, err := JournalTable(entries)
	if err != nil {
		return
	}
	orderTab, err := OrderTable(order)
	if err != nil {
		return
	}
	out, err := runner.Run(ctx, SQL, map[string][]byte{"journal": journal, "section_order": orderTab})
	if err != nil {
		return
	}
	return DecodeRows(out)
}

type row struct {
	Part        uint8    `json:"part"`
	Ord         uint32   `json:"ord"`
	Role        string   `json:"role"`
	Content     string   `json:"content"`
	Calls       string   `json:"calls"`
	Key         string   `json:"call_key"`
	ToolCallId  string   `json:"tool_call_id"`
	Attachments string   `json:"attachments"`
	Src         []uint32 `json:"src"`
	Level       uint64   `json:"level"`
	Tier        uint64   `json:"tier"`
	Exposure    string   `json:"exposure"`
	Origin      string   `json:"origin"`
	Sections    []string `json:"sections"`
}

// DecodeRows reads the worker's JSON lines into a Batch.
func DecodeRows(out []byte) (b Batch, err error) {
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 0, 1<<20), 64<<20)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var r row
		if err = json.Unmarshal(line, &r); err != nil {
			err = eb.Build().Str("line", string(line)).Errorf("projection row: %w", err)
			return
		}
		if r.Part == 3 {
			b.Tools = toolNames(r.Content)
			continue
		}
		m := Message{Part: PartE(r.Part), Ordinal: r.Ord, Role: r.Role, Content: r.Content, Calls: r.Calls, Key: r.Key, ToolCallId: r.ToolCallId, Attachments: r.Attachments,
			Sources: r.Src, Label: agentrun.Label{Level: agentrun.LevelE(r.Level), Tier: agentrun.TierE(r.Tier), Exposure: agentrun.ParseMarks(r.Exposure), Origin: agentrun.ParseMarks(r.Origin)}}
		if m.Part == PartHead {
			b.Sections = r.Sections
		}
		b.Messages = append(b.Messages, m)
	}
	if err = sc.Err(); err != nil {
		err = eh.Errorf("projection rows: %w", err)
	}
	for i := range b.Messages {
		if len(b.Messages[i].Sources) == 0 {
			b.Messages[i].Sources = nil
		}
	}
	return
}
