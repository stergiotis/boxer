package jackstay

import (
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/gov/datacatalog"
)

// digestSettings pin the RowBinary encoding the row digest hashes, so a server
// profile cannot change the bytes (ADR-0259 §SD4). JSON is hashed as its
// string form: with paths in canonical order, logically equal documents give
// equal bytes, which the native RowBinary form does not.
const digestSettings = " SETTINGS output_format_json_quote_64bit_integers = 0" +
	", output_format_binary_encode_types_in_binary_format = 0" +
	", output_format_binary_write_json_as_string = 1" +
	" FORMAT JSONEachRow"

// DigestSpec is what one side's digest queries read: the table, its key, the
// columns the row digest covers and the chunk layout. Source and target specs
// of one table differ only in Ref and Final.
type DigestSpec struct {
	Ref         datacatalog.TableRef
	KeyExprs    []string
	CopyColumns []string
	Chunking    Chunking
	// Final reads the table with FINAL, so merge-semantics engines are
	// compared on their merged rows.
	Final bool
	// Where restricts every query to a subset of rows (a chunk, a sample);
	// empty reads the whole table.
	Where string
}

// RowHashExpr hashes the copied columns' RowBinary bytes. The list is
// explicit and in plan order, never `*`.
func (inst *DigestSpec) RowHashExpr() (sql string) {
	cols := make([]string, 0, len(inst.CopyColumns))
	for _, c := range inst.CopyColumns {
		cols = append(cols, QuoteIdent(c))
	}
	return "cityHash64(formatRowNoNewline('RowBinary', " + strings.Join(cols, ", ") + "))"
}

// KeyHashExpr hashes the sorting key. A table with no sorting key (Log,
// Memory, ORDER BY tuple()) is keyed by the whole row: its rows can then only
// be missing or extra, never changed.
func (inst *DigestSpec) KeyHashExpr() (sql string) {
	if len(inst.KeyExprs) == 0 {
		return inst.RowHashExpr()
	}
	return "cityHash64(tuple(" + strings.Join(inst.KeyExprs, ", ") + "))"
}

func (inst *DigestSpec) keyTextExpr() (sql string) {
	if len(inst.KeyExprs) == 0 {
		cols := make([]string, 0, len(inst.CopyColumns))
		for _, c := range inst.CopyColumns {
			cols = append(cols, QuoteIdent(c))
		}
		return "substring(toString(tuple(" + strings.Join(cols, ", ") + ")), 1, 200)"
	}
	return "substring(toString(tuple(" + strings.Join(inst.KeyExprs, ", ") + ")), 1, 200)"
}

func (inst *DigestSpec) from() (sql string) {
	sql = " FROM " + QuoteRef(inst.Ref)
	if inst.Final {
		sql += " FINAL"
	}
	if inst.Where != "" {
		sql += " WHERE " + inst.Where
	}
	return
}

// With returns a copy of the spec restricted by an additional predicate.
func (inst DigestSpec) With(pred string) (out DigestSpec) {
	out = inst
	switch {
	case pred == "" || pred == "1":
	case out.Where == "":
		out.Where = pred
	default:
		out.Where = "(" + out.Where + ") AND (" + pred + ")"
	}
	return
}

// pidExpr is the partition id a chunk of partition chunking lives in, so a
// chunk can be read by `_partition_id` (pruned) and dropped by partition.
func (inst *DigestSpec) pidExpr() (sql string) {
	if inst.Chunking.Kind == ChunkingPartition {
		return "_partition_id"
	}
	return "''"
}

// columnList is the copy columns, quoted, in plan order.
func (inst *DigestSpec) columnList() (sql string) {
	cols := make([]string, 0, len(inst.CopyColumns))
	for _, c := range inst.CopyColumns {
		cols = append(cols, QuoteIdent(c))
	}
	return strings.Join(cols, ", ")
}

// LeafPredicate selects the rows of the given leaves.
func (inst *DigestSpec) LeafPredicate(leaves []uint32) (sql string) {
	var b strings.Builder
	b.WriteString("(" + inst.KeyHashExpr() + ") % " + strconv.FormatUint(uint64(max(inst.Chunking.Leaves, 1)), 10) + " IN (")
	for i, l := range leaves {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(strconv.FormatUint(uint64(l), 10))
	}
	b.WriteString(")")
	return b.String()
}

// SamplePredicate selects num of every den keys. It hashes the key with a
// salt, so the sample is independent of the leaf layout, and whole keys are
// in or out together.
func (inst *DigestSpec) SamplePredicate(num uint32, den uint32) (sql string) {
	keys := "tuple(" + strings.Join(inst.KeyExprs, ", ") + ")"
	if len(inst.KeyExprs) == 0 {
		keys = inst.RowHashExpr()
	}
	return "cityHash64('jackstay-sample', " + keys + ") % " + strconv.FormatUint(uint64(max(den, 1)), 10) + " < " + strconv.FormatUint(uint64(num), 10)
}

// ChunkListQuery lists the chunks with their row counts and partition ids.
// It hashes nothing, so it is cheaper than a digest.
func (inst *DigestSpec) ChunkListQuery() (sql string) {
	return "SELECT chunk, any(pid) AS pid, any(display) AS display, count() AS n FROM (SELECT " +
		inst.Chunking.ChunkExpr() + " AS chunk, " + inst.pidExpr() + " AS pid, " + inst.Chunking.DisplayExpr() + " AS display" +
		inst.from() + ") GROUP BY chunk ORDER BY chunk" + jsonSettings
}

// SelectNative streams the copy columns of the spec's rows as Native.
func (inst *DigestSpec) SelectNative() (sql string) {
	return "SELECT " + inst.columnList() + inst.from() + " FORMAT Native"
}

// InsertNative is the insert a Native stream of SelectNative lands in. Block
// deduplication is off: a chunk cleared and copied again carries the same
// blocks, and a replicated target would otherwise drop them as repeats.
func (inst *DigestSpec) InsertNative() (sql string) {
	return "INSERT INTO " + QuoteRef(inst.Ref) + " (" + inst.columnList() + ") SETTINGS insert_deduplicate = 0 FORMAT Native"
}

func (inst *DigestSpec) leafExpr() (sql string) {
	return "kh % " + strconv.FormatUint(uint64(max(inst.Chunking.Leaves, 1)), 10)
}

// LeafDigestQuery is the one scan of §SD4: per (chunk, leaf), the row count and
// the wrapping sums of the key and row hashes.
func (inst *DigestSpec) LeafDigestQuery() (sql string) {
	return "SELECT chunk, any(display) AS display, any(pid) AS pid, " + inst.leafExpr() + " AS leaf, count() AS n, " +
		"sumWithOverflow(kh) AS kd, sumWithOverflow(rh) AS rd FROM (SELECT " +
		inst.Chunking.ChunkExpr() + " AS chunk, " + inst.Chunking.DisplayExpr() + " AS display, " + inst.pidExpr() + " AS pid, " +
		inst.KeyHashExpr() + " AS kh, " + inst.RowHashExpr() + " AS rh" + inst.from() +
		") GROUP BY chunk, leaf ORDER BY chunk, leaf" + digestSettings
}

// ChunkLeaf addresses one leaf of one chunk.
type ChunkLeaf struct {
	Chunk string
	Leaf  uint32
}

// PairQuery fetches the per-row (key hash, row hash) pairs of the given leaves,
// with the key's text for examples.
func (inst *DigestSpec) PairQuery(leaves []ChunkLeaf) (sql string) {
	var in strings.Builder
	for i, cl := range leaves {
		if i > 0 {
			in.WriteString(", ")
		}
		in.WriteString("(" + QuoteString(cl.Chunk) + ", " + strconv.FormatUint(uint64(cl.Leaf), 10) + ")")
	}
	return "SELECT chunk, " + inst.leafExpr() + " AS leaf, kh, rh, keytext FROM (SELECT " +
		inst.Chunking.ChunkExpr() + " AS chunk, " + inst.KeyHashExpr() + " AS kh, " + inst.RowHashExpr() + " AS rh, " +
		inst.keyTextExpr() + " AS keytext" + inst.from() + ") WHERE (chunk, " + inst.leafExpr() + ") IN (" + in.String() + ")" +
		digestSettings
}

type leafDigestRow struct {
	Chunk   string `json:"chunk"`
	Display string `json:"display"`
	Pid     string `json:"pid"`
	Leaf    uint32 `json:"leaf"`
	N       uint64 `json:"n"`
	Kd      uint64 `json:"kd"`
	Rd      uint64 `json:"rd"`
}

type pairRow struct {
	Chunk   string `json:"chunk"`
	Leaf    uint32 `json:"leaf"`
	Kh      uint64 `json:"kh"`
	Rh      uint64 `json:"rh"`
	KeyText string `json:"keytext"`
}

// IsMergeEngine reports whether an engine collapses rows during merges, so
// that two tables with the same logical content can hold different rows until
// both are fully merged.
func IsMergeEngine(engine string) (ok bool) {
	if !strings.HasSuffix(engine, "MergeTree") {
		return false
	}
	for _, p := range []string{"Replacing", "Collapsing", "Summing", "Aggregating", "Graphite", "Coalescing"} {
		if strings.Contains(engine, p) {
			return true
		}
	}
	return false
}
