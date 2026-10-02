package jackstay

import (
	"slices"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/gov/datacatalog"
)

// rowBinarySettings pin the RowBinary encoding of formatRowNoNewline, which
// the row digest and the partition chunk id both hash, so a server profile
// cannot change the bytes (ADR-0259 §SD4). JSON is written as its string
// form: with paths in canonical order, logically equal documents give equal
// bytes, which the native RowBinary form does not.
const rowBinarySettings = ", output_format_binary_encode_types_in_binary_format = 0" +
	", output_format_binary_write_json_as_string = 1"

// digestSettings close every query that evaluates [Chunking.ChunkExpr] or a
// row hash, [DigestSpec.ChunkListQuery] included, so chunk ids and digests
// from any two such queries are comparable by construction.
const digestSettings = " SETTINGS output_format_json_quote_64bit_integers = 0" + rowBinarySettings + " FORMAT JSONEachRow"

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
	// Filter is the table's row filter (ADR-0271 §SD1): every query reads
	// only the slice it selects, on either side. Empty reads every row.
	Filter string
	// Where restricts every query to a subset of the slice (a chunk, a
	// sample); empty reads the whole slice.
	Where string
	// sel records what Where was built from, for a source that cannot run
	// SQL (a pack, [OpenPack]): only the builders below set it.
	sel specSel
}

// specSel is the structured form of a spec's Where.
type specSel struct {
	// chunked is set when the spec selects one chunk.
	chunked bool
	chunk   string
	// leaves, when not nil, restricts the chunk to these leaves.
	leaves []uint32
	// sampled is set when a sample predicate restricts the rows.
	sampled bool
	// opaque is set when [DigestSpec.With] added a predicate of unknown
	// shape.
	opaque bool
}

// RowHashExpr hashes the copied columns' RowBinary bytes. The list is
// explicit and in plan order, never `*`.
func (inst *DigestSpec) RowHashExpr() (sql string) {
	return "cityHash64(formatRowNoNewline('RowBinary', " + inst.columnList() + "))"
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
		return "substring(toString(tuple(" + inst.columnList() + ")), 1, 200)"
	}
	return "substring(toString(tuple(" + strings.Join(inst.KeyExprs, ", ") + ")), 1, 200)"
}

func (inst *DigestSpec) from() (sql string) {
	sql = " FROM " + QuoteRef(inst.Ref)
	if inst.Final {
		sql += " FINAL"
	}
	if w := andPredicates(inst.Filter, inst.Where); w != "" {
		sql += " WHERE " + w
	}
	return
}

// SlicePredicate is the predicate a target-side DELETE restricts itself
// with: the row filter and the spec's own restriction.
func (inst *DigestSpec) SlicePredicate() (sql string) {
	return andPredicates(inst.Filter, inst.Where)
}

// With returns a copy of the spec restricted by an additional predicate.
// A source that cannot run SQL refuses a spec built this way; the
// structured builders ([DigestSpec.ForChunk], [DigestSpec.ForLeaves],
// [DigestSpec.ForSample]) are what it can serve.
func (inst DigestSpec) With(pred string) (out DigestSpec) {
	out = inst.with(pred)
	if pred != "" && pred != "1" {
		out.sel.opaque = true
	}
	return
}

// ForChunk restricts the spec to one chunk. pid, when known, is the chunk's
// partition id on the side the spec reads.
func (inst DigestSpec) ForChunk(id string, pid string) (out DigestSpec) {
	out = inst.with(inst.Chunking.ChunkPredicate(id, pid))
	out.sel.chunked, out.sel.chunk = true, id
	return
}

// ForLeaves restricts the spec to the given leaves.
func (inst DigestSpec) ForLeaves(leaves []uint32) (out DigestSpec) {
	out = inst.with(inst.LeafPredicate(leaves))
	out.sel.leaves = slices.Clone(leaves)
	return
}

// ForSample restricts the spec to num of every den keys; a zero den is no
// restriction.
func (inst DigestSpec) ForSample(num uint32, den uint32) (out DigestSpec) {
	if den == 0 {
		return inst
	}
	out = inst.with(inst.SamplePredicate(num, den))
	out.sel.sampled = true
	return
}

func (inst DigestSpec) with(pred string) (out DigestSpec) {
	out = inst
	out.sel.leaves = slices.Clone(inst.sel.leaves)
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

// ChunkListQuery lists the chunks with their partition ids and display text.
// It hashes nothing, so it is cheaper than a digest.
func (inst *DigestSpec) ChunkListQuery() (sql string) {
	return "SELECT chunk, any(pid) AS pid, any(display) AS display FROM (SELECT " +
		inst.Chunking.ChunkExpr() + " AS chunk, " + inst.pidExpr() + " AS pid, " + inst.Chunking.DisplayExpr() + " AS display" +
		inst.from() + ") GROUP BY chunk ORDER BY chunk" + digestSettings
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
