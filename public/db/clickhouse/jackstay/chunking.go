package jackstay

import (
	"context"
	"math"
	"math/bits"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/gov/datacatalog"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// ChunkingKindE is how a table is cut into chunks, the unit of copying and
// verification (ADR-0259 §SD4).
//
//codelint:enum-prefix=Chunking
type ChunkingKindE uint8

const (
	// ChunkingSingle: the whole table is one chunk; leaves are the only
	// subdivision.
	ChunkingSingle ChunkingKindE = iota
	// ChunkingPartition: one chunk per value of the source's partition key,
	// evaluated on both sides, so the target's own partitioning does not
	// matter.
	ChunkingPartition
	// ChunkingRange: ranges of the first sorting-key expression, cut at
	// bounds taken from a sample of the source when the plan first chunked
	// the table.
	ChunkingRange
)

var AllChunkingKinds = []ChunkingKindE{ChunkingSingle, ChunkingPartition, ChunkingRange}

func (inst ChunkingKindE) String() (s string) {
	switch inst {
	case ChunkingSingle:
		return "single"
	case ChunkingPartition:
		return "partition"
	case ChunkingRange:
		return "range"
	}
	return "invalid"
}

func (inst ChunkingKindE) MarshalText() (text []byte, err error) {
	s := inst.String()
	if s == "invalid" {
		err = eb.Build().Uint8("kind", uint8(inst)).Errorf("invalid chunking kind")
		return
	}
	return []byte(s), nil
}

func (inst *ChunkingKindE) UnmarshalText(text []byte) (err error) {
	for _, k := range AllChunkingKinds {
		if k.String() == string(text) {
			*inst = k
			return
		}
	}
	return eb.Build().Str("kind", string(text)).Errorf("unknown chunking kind")
}

// Chunking is a table's chunk layout, kept in the plan and reused by later
// plans of the same table so digests stay comparable from run to run
// (ADR-0259 §SD4).
type Chunking struct {
	Kind ChunkingKindE `json:"kind"`
	// Exprs are the partition-key expressions (partition chunking) or the
	// one first sorting-key expression (range chunking), as SQL.
	Exprs []string `json:"exprs,omitempty"`
	// BoundType is the ClickHouse type bounds are cast to; DateTime types are
	// rewritten to UTC so the text of a bound means one instant on any server.
	BoundType string `json:"boundType,omitempty"`
	// Bounds are the ascending, distinct range bounds as text. Chunk i holds
	// the rows with Bounds[i-1] <= x < Bounds[i]; the first and last chunks
	// are open-ended, so there are len(Bounds)+1 chunks.
	Bounds []string `json:"bounds,omitempty"`
	// Leaves is the number of leaves per chunk, a power of two.
	Leaves uint32 `json:"leaves"`
}

// ChunkingOptions size the chunk layout.
type ChunkingOptions struct {
	// TargetChunkRows is the row count a range chunk aims for.
	TargetChunkRows uint64
	// MaxRangeChunks caps the number of range chunks.
	MaxRangeChunks int
	// TargetLeafRows is the row count a leaf aims for.
	TargetLeafRows uint64
	// MaxLeaves caps the leaves per chunk.
	MaxLeaves uint32
	// SampleSize is the reservoir the range bounds are drawn from.
	SampleSize int
}

func DefaultChunkingOptions() (opts ChunkingOptions) {
	return ChunkingOptions{
		TargetChunkRows: 1_000_000,
		MaxRangeChunks:  256,
		TargetLeafRows:  1024,
		MaxLeaves:       4096,
		SampleSize:      8192,
	}
}

// leavesFor picks a power-of-two leaf count so that a chunk of chunkRows rows
// has leaves of about TargetLeafRows rows.
func leavesFor(chunkRows uint64, opts ChunkingOptions) (leaves uint32) {
	if opts.TargetLeafRows == 0 || chunkRows <= opts.TargetLeafRows {
		return 1
	}
	want := (chunkRows + opts.TargetLeafRows - 1) / opts.TargetLeafRows
	leaves = uint32(1) << (64 - bits.LeadingZeros64(want-1))
	leaves = min(leaves, max(opts.MaxLeaves, 1))
	return
}

// leavesPerChunk sizes the leaves of a table of rows rows cut into chunks
// chunks, as if the rows were spread evenly; a chunk count of zero (no active
// parts yet) is treated as one.
func leavesPerChunk(rows uint64, chunks uint64, opts ChunkingOptions) (leaves uint32) {
	return leavesFor(rows/max(chunks, 1), opts)
}

// rangeIndex parses a range chunk id: the decimal index of the chunk, with no
// sign, no leading zeros and no overflow. ok is false for anything else, such
// as a partition chunk id.
func rangeIndex(id string) (i int, ok bool) {
	if id == "" || (len(id) > 1 && id[0] == '0') {
		return 0, false
	}
	for j := 0; j < len(id); j++ {
		ch := id[j]
		if ch < '0' || ch > '9' {
			return 0, false
		}
		d := int(ch - '0')
		if i > (math.MaxInt-d)/10 {
			return 0, false
		}
		i = i*10 + d
	}
	return i, true
}

// orderableType reports whether a range of values of a column of this type
// can be written as `lo <= x AND x < hi` with literal bounds.
func orderableType(typ string) (ok bool) {
	t := stripLowCardinality(typ)
	for _, p := range []string{"Int", "UInt", "Float", "Decimal", "Date", "DateTime", "String", "FixedString", "UUID", "IPv4", "IPv6", "Enum8", "Enum16", "Bool"} {
		if strings.HasPrefix(t, p) {
			return true
		}
	}
	return false
}

func stripLowCardinality(typ string) (t string) {
	t = strings.TrimSpace(typ)
	for strings.HasPrefix(t, "LowCardinality(") && strings.HasSuffix(t, ")") {
		t = strings.TrimSpace(t[len("LowCardinality(") : len(t)-1])
	}
	return
}

func isDateTimeType(t string) (ok bool) {
	return strings.HasPrefix(t, "DateTime")
}

func isFloatType(t string) (ok bool) {
	return strings.HasPrefix(stripLowCardinality(t), "Float")
}

// boundType is the type a bound is cast to: the column's own type, without
// LowCardinality, and with a DateTime's timezone replaced by UTC. Comparing
// instants is timezone-free, so the UTC form compares correctly against a
// column in any zone.
func boundType(typ string) (t string) {
	t = stripLowCardinality(typ)
	switch {
	case strings.HasPrefix(t, "DateTime64("):
		inner := t[len("DateTime64(") : len(t)-1]
		scale, _, _ := strings.Cut(inner, ",")
		t = "DateTime64(" + strings.TrimSpace(scale) + ", 'UTC')"
	case strings.HasPrefix(t, "DateTime"):
		t = "DateTime('UTC')"
	}
	return
}

// QuoteString renders s as a single-quoted ClickHouse string literal.
func QuoteString(s string) (quoted string) {
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('\'')
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if ch == '\'' || ch == '\\' {
			b.WriteByte('\\')
		}
		b.WriteByte(ch)
	}
	b.WriteByte('\'')
	return b.String()
}

// validate checks a layout read back from a plan file: the kind is known, a
// partition or range layout names its expressions, a range layout has a bound
// type and distinct bounds, and Leaves is a power of two. Bounds are text of
// a typed sort, so their order is not checked.
func (inst *Chunking) validate() (err error) {
	switch inst.Kind {
	case ChunkingSingle:
	case ChunkingPartition:
		if len(inst.Exprs) == 0 {
			return eh.Errorf("partition chunking names no partition key expression")
		}
	case ChunkingRange:
		if len(inst.Exprs) != 1 {
			return eb.Build().Int("exprs", len(inst.Exprs)).Errorf("range chunking needs exactly one key expression")
		}
		if inst.BoundType == "" {
			return eh.Errorf("range chunking names no bound type")
		}
		if len(inst.Bounds) == 0 {
			return eh.Errorf("range chunking has no bounds")
		}
		seen := make(map[string]struct{}, len(inst.Bounds))
		for _, b := range inst.Bounds {
			if _, dup := seen[b]; dup {
				return eb.Build().Str("bound", b).Errorf("range chunking repeats a bound")
			}
			seen[b] = struct{}{}
		}
		// Bounds out of order make chunk predicates overlap or leave gaps.
		// Numeric bounds compare as numbers; the other types' text sorts
		// as the values do.
		numeric := false
		for _, p := range []string{"Int", "UInt", "Float", "Decimal"} {
			numeric = numeric || strings.HasPrefix(inst.BoundType, p)
		}
		for i := 1; i < len(inst.Bounds); i++ {
			a, b := inst.Bounds[i-1], inst.Bounds[i]
			ascending := a < b
			if numeric {
				fa, ea := strconv.ParseFloat(a, 64)
				fb, eb2 := strconv.ParseFloat(b, 64)
				if ea == nil && eb2 == nil {
					ascending = fa < fb
				}
			}
			if !ascending {
				return eb.Build().Str("bound", a).Str("next", b).Errorf("range bounds are not ascending")
			}
		}
	default:
		return eb.Build().Uint8("kind", uint8(inst.Kind)).Errorf("invalid chunking kind")
	}
	if inst.Leaves == 0 || inst.Leaves&(inst.Leaves-1) != 0 {
		return eb.Build().Uint32("leaves", inst.Leaves).Errorf("leaves is not a power of two")
	}
	return
}

func (inst *Chunking) boundLiterals() (lits []string) {
	lits = make([]string, 0, len(inst.Bounds))
	for _, b := range inst.Bounds {
		lits = append(lits, "CAST("+QuoteString(b)+" AS "+inst.BoundType+")")
	}
	return
}

// ChunkExpr is the SQL expression that names a row's chunk as a String. Both
// sides evaluate the same expression, which is what makes a chunk id mean the
// same rows on both.
func (inst *Chunking) ChunkExpr() (sql string) {
	switch inst.Kind {
	case ChunkingPartition:
		// RowBinary of the key values, not their text: DateTime text depends
		// on the server's timezone, the bytes do not.
		return "hex(formatRowNoNewline('RowBinary', " + strings.Join(inst.Exprs, ", ") + "))"
	case ChunkingRange:
		return "toString(arrayCount(b -> b <= (" + inst.Exprs[0] + "), [" + strings.Join(inst.boundLiterals(), ", ") + "]))"
	}
	return "''"
}

// ChunkPredicate selects the rows of chunk id. pid, when known, is the chunk's
// partition id on the table being read; it prunes where the expression form
// cannot. Range chunks become bounds on the first sorting-key expression,
// which the primary index prunes. A NaN of a Float key compares false against
// every bound, so [Chunking.ChunkExpr] names its chunk "0"; chunk 0's predicate
// selects NaN rows too, so the two agree and no row is left to no chunk. An
// id that is not a chunk of the layout selects nothing.
func (inst *Chunking) ChunkPredicate(id string, pid string) (sql string) {
	switch inst.Kind {
	case ChunkingPartition:
		if pid != "" {
			return "_partition_id = " + QuoteString(pid)
		}
		return inst.ChunkExpr() + " = " + QuoteString(id)
	case ChunkingRange:
		i, ok := rangeIndex(id)
		if !ok || i > len(inst.Bounds) {
			return "0"
		}
		lits := inst.boundLiterals()
		x := "(" + inst.Exprs[0] + ")"
		parts := make([]string, 0, 2)
		if i > 0 {
			parts = append(parts, x+" >= "+lits[i-1])
		}
		if i < len(inst.Bounds) {
			below := x + " < " + lits[i]
			if i == 0 && isFloatType(inst.BoundType) {
				below = "(" + below + " OR isNaN(" + inst.Exprs[0] + "))"
			}
			parts = append(parts, below)
		}
		if len(parts) == 0 {
			return "1"
		}
		return strings.Join(parts, " AND ")
	}
	return "1"
}

// DisplayExpr renders a chunk's key for an operator to read. It is shown, never
// matched on.
func (inst *Chunking) DisplayExpr() (sql string) {
	if inst.Kind == ChunkingPartition {
		return "toString(tuple(" + strings.Join(inst.Exprs, ", ") + "))"
	}
	return "''"
}

// RangeDisplay describes range chunk id (a decimal index) by its bounds.
func (inst *Chunking) RangeDisplay(id string) (s string) {
	if inst.Kind != ChunkingRange {
		return ""
	}
	i, ok := rangeIndex(id)
	if !ok {
		return ""
	}
	lo, hi := "-∞", "+∞"
	if i > 0 && i-1 < len(inst.Bounds) {
		lo = inst.Bounds[i-1]
	}
	if i < len(inst.Bounds) {
		hi = inst.Bounds[i]
	}
	return "[" + lo + ", " + hi + ")"
}

type typeRow struct {
	Type string `json:"type"`
}

type sampleRow struct {
	Sample []string `json:"sample"`
}

type partitionsRow struct {
	Partitions uint64 `json:"partitions"`
}

// DeriveChunking chooses the chunk layout of a source table (ADR-0259 §SD4):
// partitions when the table has a partition key, else ranges of the first
// sorting-key expression when its type is orderable and the table is large
// enough to need more than one chunk, else a single chunk. Leaves are sized
// for the rows of one chunk: a partitioned table's active partitions are
// counted from system.parts first. Range bounds come from a sorted reservoir
// sample of the source, one scan of that expression.
func DeriveChunking(ctx context.Context, q QueryI, ref datacatalog.TableRef, sortingKey string, partitionKey string, rows uint64, opts ChunkingOptions) (c Chunking, err error) {
	if pk := SplitKeyExprs(partitionKey); len(pk) > 0 {
		var parts []partitionsRow
		parts, err = queryRows[partitionsRow](ctx, q, "SELECT uniqExact(partition_id) AS partitions FROM system.parts WHERE active AND database = "+
			QuoteString(ref.Database)+" AND table = "+QuoteString(ref.Name)+jsonSettings)
		if err != nil {
			err = eb.Build().Str("table", ref.String()).Errorf("unable to count the source's partitions: %w", err)
			return
		}
		var partitions uint64
		if len(parts) == 1 {
			partitions = parts[0].Partitions
		}
		c = Chunking{Kind: ChunkingPartition, Exprs: pk, Leaves: leavesPerChunk(rows, partitions, opts)}
		return
	}
	c = Chunking{Kind: ChunkingSingle, Leaves: leavesFor(rows, opts)}
	keys := SplitKeyExprs(sortingKey)
	if len(keys) == 0 || opts.TargetChunkRows == 0 || rows <= opts.TargetChunkRows {
		return
	}
	x := keys[0]
	var types []typeRow
	types, err = queryRows[typeRow](ctx, q, "SELECT toTypeName("+x+") AS type FROM "+QuoteRef(ref)+" LIMIT 1"+jsonSettings)
	if err != nil {
		err = eb.Build().Str("table", ref.String()).Errorf("unable to read the chunking key type: %w", err)
		return
	}
	if len(types) == 0 || !orderableType(types[0].Type) {
		return
	}
	bt := boundType(types[0].Type)
	text := "toString(v)"
	if isDateTimeType(bt) {
		text = "toString(v, 'UTC')"
	}
	var samples []sampleRow
	samples, err = queryRows[sampleRow](ctx, q, "SELECT arrayMap(v -> "+text+", arraySort(groupArraySample("+strconv.Itoa(opts.SampleSize)+", 1)("+x+"))) AS sample FROM "+QuoteRef(ref)+jsonSettings)
	if err != nil {
		err = eb.Build().Str("table", ref.String()).Errorf("unable to sample the chunking key: %w", err)
		return
	}
	if len(samples) != 1 {
		return
	}
	nChunks := int(min((rows+opts.TargetChunkRows-1)/opts.TargetChunkRows, uint64(max(opts.MaxRangeChunks, 1))))
	bounds := pickBounds(samples[0].Sample, nChunks)
	if len(bounds) == 0 {
		return
	}
	c = Chunking{
		Kind:      ChunkingRange,
		Exprs:     []string{x},
		BoundType: bt,
		Bounds:    bounds,
		Leaves:    leavesPerChunk(rows, uint64(len(bounds)+1), opts),
	}
	return
}

// pickBounds takes nChunks-1 bounds at equal ranks of a sorted sample and drops
// repeats, which a skewed key produces.
func pickBounds(sorted []string, nChunks int) (bounds []string) {
	if nChunks < 2 || len(sorted) < 2 {
		return
	}
	bounds = make([]string, 0, nChunks-1)
	for i := 1; i < nChunks; i++ {
		b := sorted[i*len(sorted)/nChunks]
		if len(bounds) > 0 && bounds[len(bounds)-1] == b {
			continue
		}
		bounds = append(bounds, b)
	}
	return
}
