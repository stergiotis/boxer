package queryrunfacts

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/env"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema/dml"
	"github.com/stergiotis/boxer/public/keelson/runtime/vocab"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/constructsql"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsql"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsqlsurface"
	"github.com/stergiotis/boxer/public/semistructured/leeway/namemint/registry"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
)

// readback.go is the S2 read side: the history SELECT that pivots
// KindQueryRun facts back into flat rows (play's History tab consumes it
// through whatever ClickHouse endpoint the user is on), and the per-run
// ProfileEvents drill-down composed as an ordinary query — the detail
// pane hands that one to the editor instead of rendering a second table,
// so the drill-down is itself visible, editable SQL.
//
// # Both are written against the SQL read surface
//
// Neither query computes a lane position itself. `LW_GET` locates the
// attribute carrying a membership on a scalar section, `LW_GET_LIST` does
// the same where the section stores a run per attribute, and `LW_SEL` /
// `LW_SEL_ATTRS` answer the plural question the mixed channel asks — the
// expansion pass resolves the lanes and emits the read-back expression
// (ADR-0181 §SD3). Column names are **handles** (ADR-0116), resolved
// against the table's generated schema rather than spelled out.
//
// The version this replaced did all of that by hand: `indexOf` into the
// membership lane, `arrayCumSum` over the cardinalities to reach an
// attribute position, `arrayElement` at that position, and seventeen
// physical column names as string constants. Three things were wrong with
// it, and only the first was ergonomic:
//
//   - The physical names went stale silently on a re-aspecting
//     regeneration. ADR-0168's Migration section records this file as one
//     of the four that broke that way at its M1.
//   - Indexing a flattened value lane by *attribute* position is only
//     valid while every attribute holds exactly one value. That held —
//     EncodeEntity uses BeginAttributeSingle throughout — but nothing
//     checked it, and the failure mode was every column of the row
//     shifting rather than an error. ADR-0183 D5 made that class loud on
//     the three generated read paths; hand-written SQL is not one of them,
//     so the guard here is to stop hand-writing it.
//   - The ProfileEvents drill-down paired the flattened value lane with
//     the per-attribute cardinality lane positionally, which is the same
//     assumption again. It now gathers through `LW_RAGGED_ELEM`, which
//     takes the raggedness as an argument instead of assuming it away.
//
// The jsonbench-on-facts trial measured what hand-writing this costs (~3×
// on the arithmetic, up to 2.4× on the gather, and the gather truncated
// silently); doc/explanation/leeway-sql-read-surface.md is the entry
// point, and `public/gov/capmapfacts/read.go` is the sibling this file
// follows.
//
// # What executes, and what is authored
//
// [ComposeHistorySql] returns SQL that runs as posted: play's History tab
// fetches over plain HTTP, outside the pass pipeline, so the expansion has
// to have happened already. [ComposeProfileEventsSql] returns the authored
// form — it goes into play's editor, where the pipeline expands it, and
// `LW_SEL` over a named membership is the thing a reader of that pane
// should see rather than the arithmetic it stands for.
//
// Both expand into the read-back helper family, which is a server-side
// install: see [CheckSurfaceVersion] for saying so usefully.

// HistoryRow is one captured run, flat. Zero values mean the fact did
// not carry the attribute (absent-when-zero counters, unstamped
// identity).
type HistoryRow struct {
	Id             uint64
	Ts             time.Time
	QueryId        string
	Event          string
	Kind           string
	Lane           string
	App            string
	RunId          string
	DurationMs     uint64
	ReadRows       uint64
	ReadBytes      uint64
	WrittenRows    uint64
	WrittenBytes   uint64
	ResultRows     uint64
	ResultBytes    uint64
	MemoryPeak     uint64
	NormalizedHash uint64
	ExceptionCode  int64
	Exception      string
	QueryText      string
}

// historyRowColumns is the SELECT-list arity ParseHistoryRows expects;
// compose and parse must move together.
const historyRowColumns = 20

// HistoryLimitCap bounds one history read the same way RecentLogs caps
// its window — an operator pane, not an export path.
const HistoryLimitCap = 500

// The column names these queries are written in — handles (ADR-0116),
// `section:column`, resolved to physical names by a client-side pass
// against the generated schema.
//
// The list is short because it has to be: every identity lane and every
// cardinality lane a read needs is resolved by LW_GET and LW_SEL
// themselves. What remains is the envelope, the one lane named as the
// WHERE pruner, and the two lanes a selector projects through.
const (
	hId         = "`id:id`"
	hNaturalKey = "`id:naturalKey`"
	hTs         = "`timestamp:ts`"

	// hSymLr is the symbol section's membership lane, named only so the
	// kind test can be a `has()`: a selector is opaque to index analysis,
	// and `has()` over this lane is what prunes granules through the skip
	// index (indexOf and countEqual never do).
	hSymLr = "`symbol:lr`"

	// The parameter and value lanes a selector projects through.
	hSymMrhp  = "`symbol:mrhp`"
	hU64Mrhp  = "`u64Array:mrhp`"
	hU64Value = "`u64Array:value`"
	hU64Len   = "`u64Array:len`"
)

// The membership channels these sections carry. Every facts section
// carries more than one, so naming one is not optional.
const (
	// plainChannel is the ordinary one-membership-per-attribute channel:
	// the counters, the fingerprints, the enumerations.
	plainChannel = "chan:low-card-ref"
	// mixedChannel is one membership shared by several attributes, told
	// apart by a high-cardinality parameter: the app and run stamps, and
	// every ProfileEvents counter (the MembLogField pattern).
	mixedChannel = "chan:low-card-ref-high-card-params"
)

// membName is the spelling a membership is named by in an LW_ call: the
// registry's own folded natural key (`runtime-query-run-duration-ms`), which
// the extraction pass resolves back to the id before the statement ships
// (ADR-0171 §SD4).
//
// Names rather than ids, everywhere, for the sake of the one query a person
// reads — the ProfileEvents drill-down goes into play's editor, and
// `LW_SEL('u64Array', 'runtime-query-run-profile-event', …)` is what that
// pane should show instead of a nineteen-digit constant. Applying the same
// rule to the history query costs nothing: it is expanded before it ships,
// so its names never leave this process.
func membName(m registry.RegisteredNaturalKey) (name string) {
	return string(m.GetNaturalKey())
}

// getScalar reads the attribute carrying memb on a scalar section — the
// type default when the row does not carry it.
func getScalar(section string, memb string) (expr string) {
	return fmt.Sprintf("LW_GET('%s', '%s', '%s')", section, memb, plainChannel)
}

// getListFirst reads the single value of the attribute carrying memb on an
// array-valued section.
//
// Every QueryRun attribute on those sections holds exactly one value
// (EncodeEntity writes them with BeginAttributeSingle), so the first is
// the only one — and an absent membership yields an empty run, whose first
// element is the type default. That is the same answer the previous
// hand-written form gave, reached without assuming the run length.
//
// The verb follows the SECTION's arity, not the Go field's type: only
// `symbol` stores one value per attribute, so every `*Array` section reads
// through LW_GET_LIST even where the value is a scalar counter.
func getListFirst(section string, memb string) (expr string) {
	return fmt.Sprintf("arrayElement(LW_GET_LIST('%s', '%s', '%s'), 1)", section, memb, plainChannel)
}

// mixedFirstParam yields the high-card parameter of the first attribute
// carrying memb on the mixed channel — the stamped app id, the run id.
//
// It selects with LW_SEL rather than LW_SEL_ATTRS because the parameter
// lane is co-indexed with the membership lane, not with the attributes.
func mixedFirstParam(section string, memb string, paramLane string) (expr string) {
	return fmt.Sprintf("arrayElement(LW_CO_GATHER(%s, LW_SEL('%s', '%s', '%s')), 1)",
		paramLane, section, memb, mixedChannel)
}

// membershipIds resolves the names above for [prepare], over the runtime
// vocabulary alone.
//
// [github.com/stergiotis/boxer/public/keelson/runtime/introspect/providers.MembershipLookup]
// is the general form of this and is what play binds, but it answers for all
// four facts-writing vocabularies and pulls their packages in with it — more
// than doubling what a capture daemon links, to read names out of the one
// vocabulary this package writes. The two cannot disagree: the names are
// collision-free across the set (the provider's own test asserts it), so both
// reach the same registry entry for any name this file emits.
type membershipIds struct{}

// LookupMembership is constructsql.MembershipIdsI — one method, spelled as
// marshallreflect.LookupI and readback.IdLookup spell it.
func (membershipIds) LookupMembership(name string) (id uint64, err error) {
	e, err := vocab.NkRegistry.Lookup(naming.StylableName(name))
	if err != nil {
		return 0, eb.Build().Str("name", name).Errorf("queryrunfacts: membership is not in the runtime vocabulary: %w", err)
	}
	return e.GetId().Value(), nil
}

// ComposeHistorySql builds the newest-first history SELECT over
// factsTable, expanded and ready to post. limit is clamped to
// [1, HistoryLimitCap]; zero applies the cap. Column order is the
// ParseHistoryRows contract.
//
// factsTable must be database-qualified: its database half is what
// resolves the unqualified references inside.
func ComposeHistorySql(factsTable string, limit int) (sql string, err error) {
	sql, err = composeHistoryAuthored(factsTable, limit)
	if err != nil {
		return "", err
	}
	sql, err = prepare(sql, factsTable)
	if err != nil {
		return "", err
	}
	// FORMAT is appended after preparation: the passes parse what they are
	// given, and FORMAT is not part of the statement grammar they read.
	return sql + "\nFORMAT TabSeparated", nil
}

// composeHistoryAuthored is the query as written — handles and LW_ calls,
// before any expansion. Kept separate so a test can assert the authored
// shape (which memberships are read, and the SELECT arity the parser
// depends on) without reading through an expansion.
func composeHistoryAuthored(factsTable string, limit int) (sql string, err error) {
	if factsTable == "" {
		err = eh.Errorf("queryrunfacts: history needs factsTable")
		return
	}
	if limit <= 0 || limit > HistoryLimitCap {
		limit = HistoryLimitCap
	}
	sym := func(m registry.RegisteredNaturalKey) string { return getScalar("symbol", membName(m)) }
	str := func(m registry.RegisteredNaturalKey) string { return getListFirst("stringArray", membName(m)) }
	u64 := func(m registry.RegisteredNaturalKey) string { return getListFirst("u64Array", membName(m)) }
	sql = fmt.Sprintf(`SELECT
  %s AS id,
  toUnixTimestamp(%s) AS ts_sec,
  %s AS query_id,
  %s AS event,
  %s AS kind,
  %s AS lane,
  %s AS app,
  %s AS run_id,
  %s AS duration_ms,
  %s AS read_rows,
  %s AS read_bytes,
  %s AS written_rows,
  %s AS written_bytes,
  %s AS result_rows,
  %s AS result_bytes,
  %s AS memory_peak,
  %s AS normalized_hash,
  %s AS exception_code,
  %s AS exception,
  %s AS query_text
FROM %s
WHERE has(%s, %d)
ORDER BY %s DESC, id
LIMIT %d`,
		hId,
		hTs,
		hNaturalKey,
		sym(vocab.MembQueryRunEventType),
		sym(vocab.MembQueryRunQueryKind),
		sym(vocab.MembQueryRunLane),
		mixedFirstParam("symbol", membName(vocab.MembRuntimeApp), hSymMrhp),
		mixedFirstParam("symbol", membName(vocab.MembRuntimeRun), hSymMrhp),
		u64(vocab.MembQueryRunDurationMs),
		u64(vocab.MembQueryRunReadRows),
		u64(vocab.MembQueryRunReadBytes),
		u64(vocab.MembQueryRunWrittenRows),
		u64(vocab.MembQueryRunWrittenBytes),
		u64(vocab.MembQueryRunResultRows),
		u64(vocab.MembQueryRunResultBytes),
		u64(vocab.MembQueryRunMemoryPeakBytes),
		u64(vocab.MembQueryRunNormalizedHash),
		getListFirst("i64Array", membName(vocab.MembQueryRunExceptionCode)),
		str(vocab.MembQueryRunExceptionText),
		str(vocab.MembQueryRunQueryText),
		factsTable,
		// The kind test stays an id: `has()` over a membership lane takes a
		// literal, and no expansion turns a name into one — it is a plain
		// ClickHouse built-in, which is exactly why it prunes.
		hSymLr, vocab.MembKindQueryRun.GetId().Value(),
		hTs,
		limit)
	return
}

// ComposeProfileEventsSql is the per-run drill-down, written as an
// ordinary user-visible query and returned **authored** — handles and
// LW_ calls, for play's editor to expand (ADR-0115 S2). The deep tier
// stays one more query away by design.
//
// The ProfileEvents counters are the u64-section attributes carrying the
// mixed ProfileEvent membership: names come from the parameter lane
// through LW_SEL, counts from the value lane through the attribute
// indices LW_SEL_ATTRS returns. The two selectors are co-indexed with
// each other, so zipping them pairs each name with its own count.
//
// The value gather goes through LW_RAGGED_ELEM rather than indexing the
// value lane directly: that lane is flattened across attributes, so an
// attribute index is not a value index, and pairing the two reads the
// wrong rows without raising anything.
func ComposeProfileEventsSql(factsTable string, factId uint64) (sql string, err error) {
	if factsTable == "" {
		err = eh.Errorf("queryrunfacts: profile drill-down needs factsTable")
		return
	}
	pe := membName(vocab.MembQueryRunProfileEvent)
	sql = fmt.Sprintf(`SELECT
  pe.1 AS event,
  pe.2 AS count
FROM %s
ARRAY JOIN arrayZip(
  LW_CO_GATHER(%s, LW_SEL('u64Array', '%s', '%s')),
  arrayMap(a -> LW_RAGGED_ELEM(%s, %s, a, 1), LW_SEL_ATTRS('u64Array', '%s', '%s'))
) AS pe
WHERE %s = %d
ORDER BY count DESC`,
		factsTable,
		hU64Mrhp, pe, mixedChannel,
		hU64Value, hU64Len, pe, mixedChannel,
		hId, factId)
	return
}

// prepare rewrites an authored query into the one that executes: column
// handles become physical names, and the LW_ extraction calls become the
// read-back expressions that locate an attribute by membership.
//
// Both resolve against the schema [dml.CreateSchemaFacts] generates rather
// than against the server: the names and lanes are a property of the code
// that writes the table, so the history query can be composed — and
// tested — with nothing running, and a handle or a section spelled wrongly
// here fails here instead of as a column ClickHouse has never heard of.
//
// Handles first, extraction second. The expansion emits physical names,
// and those contain colons; feeding them back through the handle resolver
// would invite it to read one as a `section:column` of its own.
func prepare(sql string, table string) (out string, err error) {
	database, _, qualified := strings.Cut(table, ".")
	if !qualified || database == "" {
		return "", eb.Build().Str("table", table).Errorf("queryrunfacts: table is not database-qualified")
	}
	resolver := lwsql.NewResolver(passes.NewStaticSchemaProvider(
		map[string][]string{table: factsColumnNames()}))

	out = sql
	for _, pass := range []nanopass.Pass{
		passes.ResolveColumnNames(resolver, database, nil),
		constructsql.ExtractExpandPassWithIds(resolver, membershipIds{}, database),
	} {
		out, err = pass.Apply(env.NewEnvironment(), out)
		if err != nil {
			return "", eb.Build().Str("pass", pass.Name).Errorf("queryrunfacts: readback pass failed: %w", err)
		}
	}
	return out, nil
}

// factsColumnNames is the table's physical column list, off the generated
// Arrow schema — the same source the DML builders write through, so the
// read side and the write side cannot disagree about a name.
func factsColumnNames() (names []string) {
	fields := dml.CreateSchemaFacts().Fields()
	names = make([]string, 0, len(fields))
	for i := range fields {
		names = append(names, fields[i].Name)
	}
	return names
}

// SurfaceVersionSql asks an endpoint which revision of leeway's SQL read
// surface it carries. Pair it with [CheckSurfaceVersion].
func SurfaceVersionSql() (sql string) {
	return "SELECT " + lwsqlsurface.VersionFunctionName + "() AS v FORMAT TabSeparated"
}

// CheckSurfaceVersion turns the answer to [SurfaceVersionSql] into a
// verdict on whether the history query can run against that endpoint.
//
// It exists because the alternative diagnostic is useless: the history
// query expands into the read-back family (ADR-0181 §SD3), so a server
// without it answers `Unknown function LW_VALUE_BY_TAG_EQUAL` — true, and
// no help at all to someone whose History tab has just gone empty. This is
// the version handshake ADR-0171 §SD2 exists for, used for the thing it
// was meant for: telling a caller *why* an expansion cannot run here.
//
// queryErr is what the version query itself returned, if anything; an
// unknown-function answer is the not-provisioned-here case. A mismatched
// revision is refused rather than attempted, because the expansion this
// build emits is written against the surface this build declares, and a
// silently-different one produces wrong rows rather than an error.
//
// The split — SQL here, transport at the call site — is this package's
// existing Compose/Parse shape, and it keeps the check usable from a
// caller that already has an HTTP path to the endpoint.
func CheckSurfaceVersion(raw []byte, queryErr error) (err error) {
	if queryErr != nil {
		return eb.Build().Str("versionFunction", lwsqlsurface.VersionFunctionName).Errorf(
			"queryrunfacts: this server carries no leeway SQL read surface, which the history query expands into; install it with `boxer leeway sqlsurface install`: %w",
			queryErr)
	}
	got, pErr := strconv.ParseUint(strings.TrimSpace(string(raw)), 10, 64)
	if pErr != nil {
		return eb.Build().Str("versionFunction", lwsqlsurface.VersionFunctionName).Str("answer", strings.TrimSpace(string(raw))).
			Errorf("queryrunfacts: the version function's answer is not a version: %w", pErr)
	}
	if got != uint64(lwsqlsurface.Version) {
		return eb.Build().Uint64("serverVersion", got).Int("buildVersion", lwsqlsurface.Version).Errorf(
			"queryrunfacts: this server's leeway SQL read surface version and the one this build emits differ; reconcile them with `boxer leeway sqlsurface install`")
	}
	return nil
}

// ParseHistoryRows decodes the TabSeparated history payload. Column
// order and arity are ComposeHistorySql's contract; a mismatch is an
// error, not a skip, so drift fails loudly.
func ParseHistoryRows(raw []byte) (rows []HistoryRow, err error) {
	rows = []HistoryRow{}
	if len(raw) == 0 {
		return
	}
	for line := range strings.SplitSeq(strings.TrimRight(string(raw), "\n"), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) != historyRowColumns {
			err = eb.Build().Int("want", historyRowColumns).Int("got", len(parts)).Str("line", line).Errorf("queryrunfacts: history row has the wrong column count")
			return
		}
		var row HistoryRow
		u := func(i int) (v uint64) {
			if err != nil {
				return
			}
			v, perr := strconv.ParseUint(parts[i], 10, 64)
			if perr != nil {
				err = eb.Build().Int("column", i).Str("raw", parts[i]).Errorf("queryrunfacts: history row: parse column: %w", perr)
			}
			return v
		}
		row.Id = u(0)
		tsSec := u(1)
		row.DurationMs = u(8)
		row.ReadRows = u(9)
		row.ReadBytes = u(10)
		row.WrittenRows = u(11)
		row.WrittenBytes = u(12)
		row.ResultRows = u(13)
		row.ResultBytes = u(14)
		row.MemoryPeak = u(15)
		row.NormalizedHash = u(16)
		if err != nil {
			return
		}
		excCode, perr := strconv.ParseInt(parts[17], 10, 64)
		if perr != nil {
			err = eb.Build().Str("exceptionCode", parts[17]).Errorf("queryrunfacts: history row: parse exception code: %w", perr)
			return
		}
		row.Ts = time.Unix(int64(tsSec), 0).UTC()
		row.QueryId = UnescapeTabSeparated(parts[2])
		row.Event = UnescapeTabSeparated(parts[3])
		row.Kind = UnescapeTabSeparated(parts[4])
		row.Lane = UnescapeTabSeparated(parts[5])
		row.App = UnescapeTabSeparated(parts[6])
		row.RunId = UnescapeTabSeparated(parts[7])
		row.ExceptionCode = excCode
		row.Exception = UnescapeTabSeparated(parts[18])
		row.QueryText = UnescapeTabSeparated(parts[19])
		rows = append(rows, row)
	}
	return
}

// UnescapeTabSeparated reverses ClickHouse's TabSeparated string
// escaping (the chstore recentlogs convention): \\, \t, \n, \0; other
// escapes pass through unchanged.
func UnescapeTabSeparated(s string) (out string) {
	if !strings.ContainsRune(s, '\\') {
		out = s
		return
	}
	b := strings.Builder{}
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 >= len(s) {
			b.WriteByte(s[i])
			continue
		}
		switch s[i+1] {
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case '\\':
			b.WriteByte('\\')
		case '0':
			b.WriteByte(0)
		default:
			b.WriteByte(s[i])
			b.WriteByte(s[i+1])
		}
		i++
	}
	out = b.String()
	return
}
