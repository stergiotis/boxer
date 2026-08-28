package canonform

import (
	"bytes"
	"embed"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/fxamacker/cbor/v2"
	"github.com/stergiotis/boxer/public/semistructured/leeway/anchor"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonwire/runtime"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	"github.com/stergiotis/boxer/public/semistructured/leeway/membershiprole"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/streamreadaccess"
	"github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
	"github.com/stretchr/testify/require"
)

//go:embed *.out.txt
var goldFS embed.FS

// rewriteGold regenerates the committed goldens instead of comparing; flip,
// run, flip back, review the diff.
const rewriteGold = false

func gold(t *testing.T, name string, got string) {
	t.Helper()
	if rewriteGold {
		require.NoError(t, os.WriteFile(name, []byte(got), 0o644))
		return
	}
	b, err := goldFS.ReadFile(name)
	require.NoError(t, err, "golden %s missing — set rewriteGold = true once to create it", name)
	require.Equal(t, string(b), got, "golden %s", name)
}

// --- fixture construction ---

type colBuilder func(t *testing.T, cc common.IntermediateColumnContext, role common.ColumnRoleE, phy string) arrow.Array

func buildTable(t *testing.T, load func(manip *common.TableManipulator)) (tbl common.TableDesc, ir *common.IntermediateTableRepresentation, conv common.NamingConventionI) {
	t.Helper()
	manip, err := common.NewTableManipulator()
	require.NoError(t, err)
	load(manip)
	tbl, err = manip.BuildTableDesc()
	require.NoError(t, err)
	ir = common.NewIntermediateTableRepresentation()
	require.NoError(t, ir.LoadFromTable(&tbl, clickhouse.NewTechnologySpecificCodeGenerator()))
	conv, err = ddl.NewHumanReadableNamingConvention(":")
	require.NoError(t, err)
	return
}

// buildBatch assembles a dense batch column by column in IR order, the
// layout NewDriver assumes.
func buildBatch(t *testing.T, ir *common.IntermediateTableRepresentation, conv common.NamingConventionI, nEntities int, build colBuilder) arrow.RecordBatch {
	t.Helper()
	var fields []arrow.Field
	var cols []arrow.Array
	var buf []common.PhysicalColumnDesc
	var err error
	for cc, cp := range ir.IterateColumnProps() {
		buf, err = conv.MapIntermediateToPhysicalColumns(cc, *cp, buf[:0], common.TableRowConfigMultiAttributesPerRow)
		require.NoError(t, err)
		require.Len(t, buf, len(cp.Names))
		for j, phy := range buf {
			arr := build(t, cc, cp.Roles[j], phy.String())
			require.NotNil(t, arr, "no array for column %s (role %s)", phy.String(), cp.Roles[j])
			fields = append(fields, arrow.Field{Name: phy.String(), Type: arr.DataType(), Nullable: false})
			cols = append(cols, arr)
		}
	}
	return array.NewRecordBatch(arrow.NewSchema(fields, nil), cols, int64(nEntities))
}

var pool = memory.NewGoAllocator()

func plainU64(vs ...uint64) arrow.Array {
	b := array.NewUint64Builder(pool)
	b.AppendValues(vs, nil)
	return b.NewArray()
}

func listU64(perEntity ...[]uint64) arrow.Array {
	lb := array.NewListBuilder(pool, arrow.PrimitiveTypes.Uint64)
	vb := lb.ValueBuilder().(*array.Uint64Builder)
	for _, vs := range perEntity {
		lb.Append(true)
		vb.AppendValues(vs, nil)
	}
	return lb.NewArray()
}

func listU32(perEntity ...[]uint32) arrow.Array {
	lb := array.NewListBuilder(pool, arrow.PrimitiveTypes.Uint32)
	vb := lb.ValueBuilder().(*array.Uint32Builder)
	for _, vs := range perEntity {
		lb.Append(true)
		vb.AppendValues(vs, nil)
	}
	return lb.NewArray()
}

func listI32(perEntity ...[]int32) arrow.Array {
	lb := array.NewListBuilder(pool, arrow.PrimitiveTypes.Int32)
	vb := lb.ValueBuilder().(*array.Int32Builder)
	for _, vs := range perEntity {
		lb.Append(true)
		vb.AppendValues(vs, nil)
	}
	return lb.NewArray()
}

func listI64(perEntity ...[]int64) arrow.Array {
	lb := array.NewListBuilder(pool, arrow.PrimitiveTypes.Int64)
	vb := lb.ValueBuilder().(*array.Int64Builder)
	for _, vs := range perEntity {
		lb.Append(true)
		vb.AppendValues(vs, nil)
	}
	return lb.NewArray()
}

func listF32(perEntity ...[]float32) arrow.Array {
	lb := array.NewListBuilder(pool, arrow.PrimitiveTypes.Float32)
	vb := lb.ValueBuilder().(*array.Float32Builder)
	for _, vs := range perEntity {
		lb.Append(true)
		vb.AppendValues(vs, nil)
	}
	return lb.NewArray()
}

func listF64(perEntity ...[]float64) arrow.Array {
	lb := array.NewListBuilder(pool, arrow.PrimitiveTypes.Float64)
	vb := lb.ValueBuilder().(*array.Float64Builder)
	for _, vs := range perEntity {
		lb.Append(true)
		vb.AppendValues(vs, nil)
	}
	return lb.NewArray()
}

func listBin(perEntity ...[]string) arrow.Array {
	lb := array.NewListBuilder(pool, arrow.BinaryTypes.Binary)
	vb := lb.ValueBuilder().(*array.BinaryBuilder)
	for _, vs := range perEntity {
		lb.Append(true)
		for _, s := range vs {
			vb.Append([]byte(s))
		}
	}
	return lb.NewArray()
}

func listTs(unit arrow.TimeUnit, perEntity ...[]int64) arrow.Array {
	dt := &arrow.TimestampType{Unit: unit}
	lb := array.NewListBuilder(pool, dt)
	vb := lb.ValueBuilder().(*array.TimestampBuilder)
	for _, vs := range perEntity {
		lb.Append(true)
		for _, v := range vs {
			vb.Append(arrow.Timestamp(v))
		}
	}
	return lb.NewArray()
}

// ones builds a List<Uint64> support column with one count per attribute.
func ones(attrsPerEntity ...int) arrow.Array {
	per := make([][]uint64, len(attrsPerEntity))
	for e, n := range attrsPerEntity {
		per[e] = make([]uint64, n)
		for i := range per[e] {
			per[e][i] = 1
		}
	}
	return listU64(per...)
}

// digestsOf drives rec through a fresh driver + encoder and returns a copy of
// the record digests.
func digestsOf(t *testing.T, tbl *common.TableDesc, ir *common.IntermediateTableRepresentation, rec arrow.RecordBatch, opts Options) (digests [][]byte) {
	t.Helper()
	d, err := streamreadaccess.NewDriver(tbl, ir, streamreadaccess.DefaultFormatters())
	require.NoError(t, err)
	enc, err := NewEncoder(tbl, ir, opts)
	require.NoError(t, err)
	require.NoError(t, d.DriveRecordBatch(enc, rec))
	require.NoError(t, enc.Err())
	for i := range enc.NumRecords() {
		digests = append(digests, append([]byte(nil), enc.RecordDigest(i)...))
	}
	return
}

// oneSection builds "id + one tagged section" with a LowCardRef membership
// channel: the shape every width / order / alias test below varies.
func oneSection(t *testing.T, section string, ct canonicaltypes.PrimitiveAstNodeI) (tbl common.TableDesc, ir *common.IntermediateTableRepresentation, conv common.NamingConventionI) {
	return buildTable(t, func(manip *common.TableManipulator) {
		manip.SetTableName("one")
		manip.PlainValueColumn(common.PlainItemTypeEntityId, "id", ctabb.U64)
		manip.PlainValueColumn(common.PlainItemTypeEntityTimestamp, "ts", ctabb.Z64)
		sec := manip.TaggedValueSection(naming.StylableName(section)).
			AddSectionMembership(common.MembershipSpecLowCardRef)
		sec.TaggedValueColumn("v", ct)
	})
}

// oneSectionBatch: one entity, the given attributes (each a membership ref
// list + a value array builder). refsPerAttr gives the refs of each attribute;
// values builds the value column (List<X>, one list per entity). ts and id
// are fixed unless overridden.
type oneSectionData struct {
	id        uint64
	ts        int64 // nanoseconds
	refs      [][]uint64
	values    func() arrow.Array
	lenOrCard func() arrow.Array // optional: array length / set cardinality support column
}

func oneSectionBatch(t *testing.T, ir *common.IntermediateTableRepresentation, conv common.NamingConventionI, d oneSectionData) arrow.RecordBatch {
	t.Helper()
	nAttrs := len(d.refs)
	return buildBatch(t, ir, conv, 1, func(t *testing.T, cc common.IntermediateColumnContext, role common.ColumnRoleE, phy string) arrow.Array {
		switch {
		case cc.Scope != common.IntermediateColumnScopeTagged && cc.PlainItemType == common.PlainItemTypeEntityId:
			return plainU64(d.id)
		case cc.Scope != common.IntermediateColumnScopeTagged && cc.PlainItemType == common.PlainItemTypeEntityTimestamp:
			b := array.NewTimestampBuilder(pool, &arrow.TimestampType{Unit: arrow.Nanosecond})
			b.Append(arrow.Timestamp(d.ts))
			return b.NewArray()
		case role == common.ColumnRoleValue:
			return d.values()
		case role == common.ColumnRoleLength || role == common.ColumnRoleCardinality:
			if d.lenOrCard != nil {
				return d.lenOrCard()
			}
			return ones(nAttrs)
		case role == common.ColumnRoleLowCardRef:
			var flat []uint64
			for _, rs := range d.refs {
				flat = append(flat, rs...)
			}
			return listU64(flat)
		case role == common.ColumnRoleLowCardRefCardinality:
			cards := make([]uint64, nAttrs)
			for i, rs := range d.refs {
				cards[i] = uint64(len(rs))
			}
			return listU64(cards)
		}
		t.Fatalf("fixture does not know how to build column %s (role %s)", phy, role)
		return nil
	})
}

func hexs(b []byte) string { return hex.EncodeToString(b) }

// --- pins ---

func TestBlake3KeysPinned(t *testing.T) {
	d := NewBlake3Digester()
	lk, rk := d.LeafKey(), d.RecordKey()
	got := fmt.Sprintf("leaf   %s %s\nrecord %s %s\n", ContextLeafV1, hexs(lk[:]), ContextRecordV1, hexs(rk[:]))
	gold(t, "canonform_keys_gold.out.txt", got)
	require.NotEqual(t, lk, rk)
}

// The anchor fixtures (three demo domains, twenty entities each) through the
// encoder: every record digest, plus the canonical items of the first entity
// captured with the recording digester. Any byte change here is a form
// change (ADR-0201 Verification plan).
func TestAnchorGoldens(t *testing.T) {
	manip, err := anchor.GetSchemaInManipulator()
	require.NoError(t, err)
	tbl, err := manip.BuildTableDesc()
	require.NoError(t, err)
	ir := common.NewIntermediateTableRepresentation()
	require.NoError(t, ir.LoadFromTable(&tbl, clickhouse.NewTechnologySpecificCodeGenerator()))
	records, err := anchor.GenerateAlpineEvents(nil, 20)
	require.NoError(t, err)
	records, err = anchor.GenerateCyberThreatEvents(records)
	require.NoError(t, err)
	records, err = anchor.GenerateDroneMissionEvents(records)
	require.NoError(t, err)
	require.Len(t, records, 3)

	d, err := streamreadaccess.NewDriver(&tbl, ir, streamreadaccess.DefaultFormatters())
	require.NoError(t, err)

	var digests strings.Builder
	var leaves, record bytes.Buffer
	var items strings.Builder
	for bi, rec := range records {
		var opts Options
		if bi == 0 {
			opts.Digester = NewRecordingDigester(NewBlake3Digester(), &leaves, &record)
		}
		enc, err := NewEncoder(&tbl, ir, opts)
		require.NoError(t, err)
		if bi == 0 {
			// Capture the first entity only: stop recording after it.
			first := true
			opts.OnRecord = func(entityIdx int, digest []byte) error {
				if first {
					first = false
					items.WriteString("record " + hexs(record.Bytes()) + "\n")
					// Leaf items are concatenated CBOR items; split them by
					// decoding one item at a time.
					rest := leaves.Bytes()
					for len(rest) > 0 {
						var v any
						r, err := cbor.UnmarshalFirst(rest, &v)
						if err != nil {
							return err
						}
						items.WriteString("leaf   " + hexs(rest[:len(rest)-len(r)]) + "\n")
						rest = r
					}
					opts.Digester = NewBlake3Digester() // stop recording
				}
				return nil
			}
			enc, err = NewEncoder(&tbl, ir, opts)
			require.NoError(t, err)
		}
		require.NoError(t, d.DriveRecordBatch(enc, rec))
		require.NoError(t, enc.Err())
		for i := range enc.NumRecords() {
			fmt.Fprintf(&digests, "%d %02d %s\n", bi, i, hexs(enc.RecordDigest(i)))
		}
	}
	gold(t, "canonform_anchor_digests_gold.out.txt", digests.String())
	gold(t, "canonform_anchor_items_gold.out.txt", items.String())

	// The self-check of ADR-0201 M1, applied to the captured items: decoding
	// with the library and re-encoding under CoreDetEncOptions reproduces the
	// bytes, so the output is RFC 8949 §4.2 deterministic and well-formed.
	em, err := cbor.CoreDetEncOptions().EncMode()
	require.NoError(t, err)
	dm, err := cbor.DecOptions{}.DecMode()
	require.NoError(t, err)
	n := 0
	for line := range strings.SplitSeq(items.String(), "\n") {
		if line == "" {
			continue
		}
		raw, err := hex.DecodeString(strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(line, "record"), "leaf")))
		require.NoError(t, err)
		var v any
		require.NoError(t, dm.Unmarshal(raw, &v), line)
		again, err := em.Marshal(v)
		require.NoError(t, err)
		require.Equal(t, hexs(raw), hexs(again), "re-encoding under CoreDetEncOptions must reproduce the item: %s", line)
		n++
	}
	require.Greater(t, n, 1, "the first anchor entity must yield at least one leaf and the record item")
}

// --- invariances (ADR-0201 Context: what the form must not see) ---

func TestWidthInvarianceIntegers(t *testing.T) {
	tblA, irA, convA := oneSection(t, "i32", ctabb.I32)
	tblB, irB, convB := oneSection(t, "i64", ctabb.I64)
	dA := oneSectionData{id: 1, ts: 5e9, refs: [][]uint64{{7}, {8}}, values: func() arrow.Array { return listI32([]int32{3, -4}) }}
	dB := oneSectionData{id: 2, ts: 5e9, refs: [][]uint64{{7}, {8}}, values: func() arrow.Array { return listI64([]int64{3, -4}) }}
	a := digestsOf(t, &tblA, irA, oneSectionBatch(t, irA, convA, dA), Options{})
	b := digestsOf(t, &tblB, irB, oneSectionBatch(t, irB, convB, dB), Options{})
	require.Equal(t, hexs(a[0]), hexs(b[0]), "i32 and i64 sections holding the same values (and a different id) must digest identically")

	// Unsigned of the same value too, and a float column holding 3.0 and -4.0.
	tblC, irC, convC := oneSection(t, "f64", ctabb.F64)
	dC := oneSectionData{id: 3, ts: 5e9, refs: [][]uint64{{7}, {8}}, values: func() arrow.Array { return listF64([]float64{3.0, -4.0}) }}
	c := digestsOf(t, &tblC, irC, oneSectionBatch(t, irC, convC, dC), Options{})
	require.Equal(t, hexs(a[0]), hexs(c[0]), "numeric reduction: f64 3.0 / -4.0 ≡ i32 3 / -4")

	// Distinct values stay distinct.
	dD := oneSectionData{id: 1, ts: 5e9, refs: [][]uint64{{7}, {8}}, values: func() arrow.Array { return listI32([]int32{3, -5}) }}
	x := digestsOf(t, &tblA, irA, oneSectionBatch(t, irA, convA, dD), Options{})
	require.NotEqual(t, hexs(a[0]), hexs(x[0]))
}

func TestWidthInvarianceFloats(t *testing.T) {
	tblA, irA, convA := oneSection(t, "f32", ctabb.F32)
	tblB, irB, convB := oneSection(t, "f64", ctabb.F64)
	f := float32(0.1)
	dA := oneSectionData{id: 1, ts: 5e9, refs: [][]uint64{{7}, {8}}, values: func() arrow.Array { return listF32([]float32{f, 1.5}) }}
	dB := oneSectionData{id: 1, ts: 5e9, refs: [][]uint64{{7}, {8}}, values: func() arrow.Array { return listF64([]float64{float64(f), 1.5}) }}
	a := digestsOf(t, &tblA, irA, oneSectionBatch(t, irA, convA, dA), Options{})
	b := digestsOf(t, &tblB, irB, oneSectionBatch(t, irB, convB, dB), Options{})
	require.Equal(t, hexs(a[0]), hexs(b[0]), "f32 x ≡ f64(x)")
	dC := oneSectionData{id: 1, ts: 5e9, refs: [][]uint64{{7}, {8}}, values: func() arrow.Array { return listF64([]float64{0.1, 1.5}) }}
	c := digestsOf(t, &tblB, irB, oneSectionBatch(t, irB, convB, dC), Options{})
	require.NotEqual(t, hexs(a[0]), hexs(c[0]), "f64 0.1 is a different number than f32 0.1")
}

func TestWidthInvarianceTemporal(t *testing.T) {
	tblA, irA, convA := oneSection(t, "z32", ctabb.Z32)
	tblB, irB, convB := oneSection(t, "z64", ctabb.Z64)
	// z32 arrives as Timestamp(ms) on the Arrow lane, z64 as Timestamp(ns).
	dA := oneSectionData{id: 1, ts: 5e9, refs: [][]uint64{{7}}, values: func() arrow.Array { return listTs(arrow.Millisecond, []int64{1_700_000_000_000}) }}
	dB := oneSectionData{id: 1, ts: 5e9, refs: [][]uint64{{7}}, values: func() arrow.Array { return listTs(arrow.Nanosecond, []int64{1_700_000_000_000_000_000}) }}
	a := digestsOf(t, &tblA, irA, oneSectionBatch(t, irA, convA, dA), Options{})
	b := digestsOf(t, &tblB, irB, oneSectionBatch(t, irB, convB, dB), Options{})
	require.Equal(t, hexs(a[0]), hexs(b[0]), "a whole-second instant digests identically from z32 and z64")
	dC := oneSectionData{id: 1, ts: 5e9, refs: [][]uint64{{7}}, values: func() arrow.Array { return listTs(arrow.Nanosecond, []int64{1_700_000_000_000_000_001}) }}
	c := digestsOf(t, &tblB, irB, oneSectionBatch(t, irB, convB, dC), Options{})
	require.NotEqual(t, hexs(a[0]), hexs(c[0]), "one nanosecond later is different content")
}

func TestSetsAreOrderAndDuplicateInvariant(t *testing.T) {
	tblA, irA, convA := oneSection(t, "u32m", ctabb.U32m)
	tblB, irB, convB := oneSection(t, "u64m", ctabb.U64m)
	dA := oneSectionData{id: 1, ts: 5e9, refs: [][]uint64{{7}}, values: func() arrow.Array { return listU32([]uint32{3, 1, 2, 2}) }, lenOrCard: func() arrow.Array { return listU64([]uint64{4}) }}
	dB := oneSectionData{id: 1, ts: 5e9, refs: [][]uint64{{7}}, values: func() arrow.Array { return listU64([]uint64{1, 2, 3}) }, lenOrCard: func() arrow.Array { return listU64([]uint64{3}) }}
	a := digestsOf(t, &tblA, irA, oneSectionBatch(t, irA, convA, dA), Options{})
	b := digestsOf(t, &tblB, irB, oneSectionBatch(t, irB, convB, dB), Options{})
	require.Equal(t, hexs(a[0]), hexs(b[0]), "a set is its distinct elements, whatever the order, width or duplication")

	// An array of the same elements is a different thing, and its order counts.
	tblC, irC, convC := oneSection(t, "u32h", ctabb.U32h)
	dC := oneSectionData{id: 1, ts: 5e9, refs: [][]uint64{{7}}, values: func() arrow.Array { return listU32([]uint32{1, 2, 3}) }, lenOrCard: func() arrow.Array { return listU64([]uint64{3}) }}
	dD := oneSectionData{id: 1, ts: 5e9, refs: [][]uint64{{7}}, values: func() arrow.Array { return listU32([]uint32{3, 2, 1}) }, lenOrCard: func() arrow.Array { return listU64([]uint64{3}) }}
	c := digestsOf(t, &tblC, irC, oneSectionBatch(t, irC, convC, dC), Options{})
	d := digestsOf(t, &tblC, irC, oneSectionBatch(t, irC, convC, dD), Options{})
	require.NotEqual(t, hexs(a[0]), hexs(c[0]), "h vs m of equal elements differ")
	require.NotEqual(t, hexs(c[0]), hexs(d[0]), "array order is content")
}

func TestAttributeAndMembershipOrderInvariant(t *testing.T) {
	tbl, ir, conv := oneSection(t, "i64", ctabb.I64)
	dA := oneSectionData{id: 1, ts: 5e9, refs: [][]uint64{{7, 9}, {8}}, values: func() arrow.Array { return listI64([]int64{3, -4}) }}
	dB := oneSectionData{id: 1, ts: 5e9, refs: [][]uint64{{8}, {9, 7}}, values: func() arrow.Array { return listI64([]int64{-4, 3}) }}
	a := digestsOf(t, &tbl, ir, oneSectionBatch(t, ir, conv, dA), Options{})
	b := digestsOf(t, &tbl, ir, oneSectionBatch(t, ir, conv, dB), Options{})
	require.Equal(t, hexs(a[0]), hexs(b[0]), "attribute order and membership order within an attribute are representation")
}

func TestAliasingIsContent(t *testing.T) {
	tbl, ir, conv := oneSection(t, "i64", ctabb.I64)
	aliased := oneSectionData{id: 1, ts: 5e9, refs: [][]uint64{{7, 8}}, values: func() arrow.Array { return listI64([]int64{3}) }}
	separate := oneSectionData{id: 1, ts: 5e9, refs: [][]uint64{{7}, {8}}, values: func() arrow.Array { return listI64([]int64{3, 3}) }}
	a := digestsOf(t, &tbl, ir, oneSectionBatch(t, ir, conv, aliased), Options{})
	b := digestsOf(t, &tbl, ir, oneSectionBatch(t, ir, conv, separate), Options{})
	require.NotEqual(t, hexs(a[0]), hexs(b[0]), "one value with two tags ≠ two attributes with one tag each")
}

func TestPlainsRule(t *testing.T) {
	tbl, ir, conv := oneSection(t, "i64", ctabb.I64)
	base := oneSectionData{id: 1, ts: 5e9, refs: [][]uint64{{7}}, values: func() arrow.Array { return listI64([]int64{3}) }}
	otherId := base
	otherId.id = 2
	otherTs := base
	otherTs.ts = 6e9
	a := digestsOf(t, &tbl, ir, oneSectionBatch(t, ir, conv, base), Options{})
	b := digestsOf(t, &tbl, ir, oneSectionBatch(t, ir, conv, otherId), Options{})
	c := digestsOf(t, &tbl, ir, oneSectionBatch(t, ir, conv, otherTs), Options{})
	require.Equal(t, hexs(a[0]), hexs(b[0]), "the entity id is excluded by default")
	require.NotEqual(t, hexs(a[0]), hexs(c[0]), "every other plain (here the timestamp) is included by default")
	a2 := digestsOf(t, &tbl, ir, oneSectionBatch(t, ir, conv, base), Options{IncludeEntityId: true})
	b2 := digestsOf(t, &tbl, ir, oneSectionBatch(t, ir, conv, otherId), Options{IncludeEntityId: true})
	require.NotEqual(t, hexs(a2[0]), hexs(b2[0]), "IncludeEntityId opts the id in")
	require.NotEqual(t, hexs(a[0]), hexs(a2[0]))
}

// Secondary memberships are not content: under PathPrefixClassifier a label
// overlay (a membership-only co-section declared AllSecondary) may change or
// vanish without moving the digest, while the primary path moves it; and a
// standalone attribute carrying only secondary memberships contributes
// nothing.
func TestSecondaryMembershipsAreNotContent(t *testing.T) {
	tbl, ir, conv := buildTable(t, func(manip *common.TableManipulator) {
		manip.SetTableName("roles")
		manip.PlainValueColumn(common.PlainItemTypeEntityId, "id", ctabb.U64)
		point := manip.TaggedValueSection("point").
			SectionCoSectionGroup("geo").
			AddSectionMembership(common.MembershipSpecLowCardVerbatim)
		point.TaggedValueColumn("v", ctabb.U64)
		manip.TaggedValueSection("labels").
			SectionCoSectionGroup("geo").
			AddSectionMembership(common.MembershipSpecLowCardVerbatim).
			AddSectionUseAspects(useaspects.AspectSectionMembershipsAllSecondary)
		manip.TaggedValueSection("notes").
			AddSectionMembership(common.MembershipSpecLowCardVerbatim)
	})
	type data struct {
		path, label string
		notes       []string // verbatim memberships of the standalone "notes" section, one attribute each
	}
	batch := func(d data) arrow.RecordBatch {
		return buildBatch(t, ir, conv, 1, func(t *testing.T, cc common.IntermediateColumnContext, role common.ColumnRoleE, phy string) arrow.Array {
			switch {
			case cc.Scope != common.IntermediateColumnScopeTagged:
				return plainU64(1)
			case cc.SectionName == "point" && role == common.ColumnRoleValue:
				return listU64([]uint64{42})
			case cc.SectionName == "point" && role == common.ColumnRoleLowCardVerbatim:
				return listBin([]string{d.path})
			case cc.SectionName == "labels" && role == common.ColumnRoleLowCardVerbatim:
				return listBin([]string{d.label})
			case cc.SectionName == "notes" && role == common.ColumnRoleLowCardVerbatim:
				return listBin(d.notes)
			case role == common.ColumnRoleLowCardVerbatimCardinality:
				if cc.SectionName == "notes" {
					return ones(len(d.notes))
				}
				return ones(1)
			}
			t.Fatalf("fixture does not know how to build column %s (role %s)", phy, role)
			return nil
		})
	}
	cls := Options{Classifier: membershiprole.PathPrefixClassifier{}}
	base := digestsOf(t, &tbl, ir, batch(data{path: "/p", label: "lbl"}), cls)
	relabel := digestsOf(t, &tbl, ir, batch(data{path: "/p", label: "other"}), cls)
	repath := digestsOf(t, &tbl, ir, batch(data{path: "/q", label: "lbl"}), cls)
	require.Equal(t, hexs(base[0]), hexs(relabel[0]), "a secondary label is an annotation, not content")
	require.NotEqual(t, hexs(base[0]), hexs(repath[0]), "the primary path is content")

	// An attribute with only secondary memberships ("note" has no '/' prefix)
	// is omitted: adding it changes nothing under the classifier, and changes
	// the digest without one (every membership primary).
	withNote := digestsOf(t, &tbl, ir, batch(data{path: "/p", label: "lbl", notes: []string{"note"}}), cls)
	require.Equal(t, hexs(base[0]), hexs(withNote[0]), "an attribute carrying only secondary memberships contributes nothing")
	plainBase := digestsOf(t, &tbl, ir, batch(data{path: "/p", label: "lbl"}), Options{})
	plainWithNote := digestsOf(t, &tbl, ir, batch(data{path: "/p", label: "lbl", notes: []string{"note"}}), Options{})
	require.NotEqual(t, hexs(plainBase[0]), hexs(plainWithNote[0]), "with a nil classifier every membership is primary")
	require.NotEqual(t, hexs(base[0]), hexs(plainBase[0]), "the digest is a function of the classifier")
}

// --- Value vectors (ADR-0201 SD3 / SD4; measured against the spike) ---
//
// The writer's own vectors live with it in canonwire/runtime; these pin the
// quotient's rules on top of it, the numeric reduction above all.

func TestCborWriterNumbers(t *testing.T) {
	enc := func(f func(c *runtime.CborWriter)) string {
		var b bytes.Buffer
		c, err := runtime.NewCborWriter(&b)
		require.NoError(t, err)
		f(c)
		require.NoError(t, c.Err())
		return hexs(b.Bytes())
	}
	cases := []struct {
		name string
		want string
		f    func(c *runtime.CborWriter)
	}{
		{"int 3", "03", func(c *runtime.CborWriter) { c.WriteInt(3) }},
		{"int -4", "23", func(c *runtime.CborWriter) { c.WriteInt(-4) }},
		{"uint64 max", "1bffffffffffffffff", func(c *runtime.CborWriter) { c.WriteUint(math.MaxUint64) }},
		{"int64 min", "3b7fffffffffffffff", func(c *runtime.CborWriter) { c.WriteInt(math.MinInt64) }},
		{"float 3.0 reduces", "03", func(c *runtime.CborWriter) { writeFloatReduced(c, 3.0) }},
		{"float -4.0 reduces", "23", func(c *runtime.CborWriter) { writeFloatReduced(c, -4.0) }},
		{"float -0.0 reduces", "00", func(c *runtime.CborWriter) { writeFloatReduced(c, math.Copysign(0, -1)) }},
		{"float 2^63 reduces to uint", "1b8000000000000000", func(c *runtime.CborWriter) { writeFloatReduced(c, math.Pow(2, 63)) }},
		{"float -2^63 reduces to int", "3b7fffffffffffffff", func(c *runtime.CborWriter) { writeFloatReduced(c, -math.Pow(2, 63)) }},
		{"float 2^64 stays float", "fa5f800000", func(c *runtime.CborWriter) { writeFloatReduced(c, math.Pow(2, 64)) }},
		{"float 1e20 stays float", "fb4415af1d78b58c40", func(c *runtime.CborWriter) { writeFloatReduced(c, 1e20) }},
		{"float 1.5 is float16", "f93e00", func(c *runtime.CborWriter) { writeFloatReduced(c, 1.5) }},
		{"f32 0.1 is float32", "fa3dcccccd", func(c *runtime.CborWriter) { writeFloatReduced(c, float64(float32(0.1))) }},
		{"f64 0.1 is float64", "fb3fb999999999999a", func(c *runtime.CborWriter) { writeFloatReduced(c, 0.1) }},
		{"NaN", "f97e00", func(c *runtime.CborWriter) { writeFloatReduced(c, math.NaN()) }},
		{"+Inf", "f97c00", func(c *runtime.CborWriter) { writeFloatReduced(c, math.Inf(1)) }},
		{"-Inf", "f9fc00", func(c *runtime.CborWriter) { writeFloatReduced(c, math.Inf(-1)) }},
		{"text abc", "63616263", func(c *runtime.CborWriter) { c.WriteTextString("abc") }},
		{"bytes abc", "43616263", func(c *runtime.CborWriter) { c.WriteBytes([]byte("abc")) }},
		{"empty array", "80", func(c *runtime.CborWriter) { c.ArrayHead(0) }},
		{"map head 2", "a2", func(c *runtime.CborWriter) { c.MapHead(2) }},
		{"tag 258", "d90102", func(c *runtime.CborWriter) { c.Tag(runtime.TagSet) }},
		{"tag 1001", "d903e9", func(c *runtime.CborWriter) { c.Tag(runtime.TagExtendedTime) }},
		{"true false null", "f5f4f6", func(c *runtime.CborWriter) { c.WriteBool(true); c.WriteBool(false); c.WriteNull() }},
		{"head 24", "1818", func(c *runtime.CborWriter) { c.WriteUint(24) }},
		{"head 256", "190100", func(c *runtime.CborWriter) { c.WriteUint(256) }},
		{"head 65536", "1a00010000", func(c *runtime.CborWriter) { c.WriteUint(65536) }},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, enc(tc.f), tc.name)
	}
}

// Network and temporal scalars straight from Arrow arrays (SD3).
func TestScalarVectors(t *testing.T) {
	enc := func(arr arrow.Array, i int, ct canonicaltypes.PrimitiveAstNodeI) string {
		var b bytes.Buffer
		c, err := runtime.NewCborWriter(&b)
		require.NoError(t, err)
		require.NoError(t, writeScalar(c, arr, i, ct))
		require.NoError(t, c.Err())
		return hexs(b.Bytes())
	}
	fsb := func(width int, vals ...[]byte) arrow.Array {
		b := array.NewFixedSizeBinaryBuilder(pool, &arrow.FixedSizeBinaryType{ByteWidth: width})
		for _, v := range vals {
			b.Append(v)
		}
		return b.NewArray()
	}
	u32 := func(vals ...uint32) arrow.Array {
		b := array.NewUint32Builder(pool)
		b.AppendValues(vals, nil)
		return b.NewArray()
	}
	// IPv4 1.2.3.4 as the uint32 lane, and the same address IPv4-mapped in an
	// IPv6 column, reduce to one form: 52(h'01020304').
	require.Equal(t, "d8344401020304", enc(u32(0x01020304), 0, ctabb.V))
	mapped := append(append(make([]byte, 10), 0xff, 0xff), 1, 2, 3, 4)
	require.Equal(t, "d8344401020304", enc(fsb(16, mapped), 0, ctabb.W))
	// A real IPv6 address keeps tag 54 and its 16 bytes.
	v6 := []byte{0x20, 0x01, 0x0d, 0xb8, 0x12, 0x34, 0xde, 0xed, 0xbe, 0xef, 0xca, 0xfe, 0xfa, 0xce, 0xfe, 0xed}
	require.Equal(t, "d8365020010db81234deedbeefcafefacefeed", enc(fsb(16, v6), 0, ctabb.W))
	// Prefixes: 10.0.0.0/8 packs as 4 address bytes + prefix byte; RFC 9164
	// truncates to the bytes the prefix covers → 52([8, h'0a']).
	require.Equal(t, "d8348208410a", enc(fsb(5, []byte{10, 0, 0, 0, 8}), 0, ctabb.Vc))
	// 2001:db8:1234::/48 → 54([48, h'20010db81234']).
	pfx := append([]byte{0x20, 0x01, 0x0d, 0xb8, 0x12, 0x34}, make([]byte, 10)...)
	require.Equal(t, "d8368218304620010db81234", enc(fsb(17, append(pfx, 48)), 0, ctabb.Wc))
	// An IPv4-mapped /104 prefix reduces to an IPv4 /8.
	mappedPfx := append(append(make([]byte, 10), 0xff, 0xff), 10, 0, 0, 0, 104)
	require.Equal(t, "d8348208410a", enc(fsb(17, mappedPfx), 0, ctabb.Wc))

	// Temporal: whole seconds at ms unit → 1001({1: secs}); sub-second → {1, -9}.
	ts := func(unit arrow.TimeUnit, v int64) arrow.Array {
		b := array.NewTimestampBuilder(pool, &arrow.TimestampType{Unit: unit})
		b.Append(arrow.Timestamp(v))
		return b.NewArray()
	}
	require.Equal(t, "d903e9a1011a6553f100", enc(ts(arrow.Millisecond, 1_700_000_000_000), 0, ctabb.Z32))
	require.Equal(t, "d903e9a1011a6553f100", enc(ts(arrow.Nanosecond, 1_700_000_000_000_000_000), 0, ctabb.Z64))
	require.Equal(t, "d903e9a2011a6553f10028187b", enc(ts(arrow.Nanosecond, 1_700_000_000_000_000_123), 0, ctabb.Z64))
	// A ClickHouse DateTime arriving as uint32 seconds.
	require.Equal(t, "d903e9a1011a6553f100", enc(u32(1_700_000_000), 0, ctabb.Z32))
	// Pre-epoch keeps nanoseconds non-negative: -1 ms = {1: -1, -9: 999000000}.
	require.Equal(t, "d903e9a20120281a3b8b87c0", enc(ts(arrow.Millisecond, -1), 0, ctabb.Z64))
}

func TestTextLaneRefused(t *testing.T) {
	tbl, ir, _ := oneSection(t, "i64", ctabb.I64)
	enc, err := NewEncoder(&tbl, ir, Options{})
	require.NoError(t, err)
	enc.BeginBatch()
	_, _ = enc.WriteString("3")
	require.Error(t, enc.Err())
}
