package canonform

// The ADR-0201 M1 invariance suite: rapid properties over random entities of
// small purpose-built tables. Equal digests under every change SD2 declares
// representation — width widenings, aspect toggles, section renames,
// attribute / membership / set permutations, membership-channel cardinality
// flips, secondary-membership edits under a fixed classifier — and distinct
// digests for content edits. Plus the strict-decode → CoreDetEncOptions
// re-encode self-check over every emitted attribute and entity item of a
// random batch, and the streaming assertion (values reach the leaf hashers,
// never the record hasher).
//
// The direct (non-random) forms of several of these live in encoder_test.go;
// the properties here generalize them over drawn values and orders.

import (
	"bytes"
	"math"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/fxamacker/cbor/v2"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/membershiprole"
	"github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
)

// --- value pools: the edges the form has rules for ---

var m1U8 = []uint8{0, 1, 23, 24, 255}
var m1I32 = []int32{0, -1, 1, 23, -24, math.MinInt32, math.MaxInt32}
var m1F32 = []float32{0, float32(math.Copysign(0, -1)), 1, -1, 1.5, 0.1, float32(math.Inf(1)), float32(math.Inf(-1)), 65504}

// m1Ms stays within ±9.2e12 ms so the nanosecond widening (×10⁶) cannot
// overflow int64.
var m1Ms = []int64{0, 1, -1, 999, 1000, -1000, 1_700_000_000_000, -8_000_000_000_000}
var m1Blob4 = [][]byte{{0, 0, 0, 0}, {1, 2, 3, 4}, {0xff, 0, 0, 1}, {0, 0, 0, 1}}

// m1Text4 holds four-byte, valid-UTF-8 values (an `s` lane refuses ill-formed
// text), zero-padding included — the shape a FixedString(4) read produces.
var m1Text4 = []string{"abcd", "ab\x00\x00", "zzzz", "a\x00\x00b"}
var m1Refs = []uint64{1, 2, 7, 23, 24, 1 << 40, math.MaxUint64}

func drawAttrCount(rt *rapid.T) int { return rapid.IntRange(1, 4).Draw(rt, "attrs") }

func drawRefs(rt *rapid.T, nAttrs int) (refs [][]uint64) {
	refs = make([][]uint64, nAttrs)
	for i := range refs {
		n := rapid.IntRange(1, 3).Draw(rt, "refCount")
		for range n {
			refs[i] = append(refs[i], rapid.SampledFrom(m1Refs).Draw(rt, "ref"))
		}
	}
	return
}

// permFromSeed returns a deterministic permutation of [0,n), so a failing
// case shrinks to one seed.
func permFromSeed(seed uint64, n int) (p []int) {
	r := rand.New(rand.NewPCG(seed, 0x9e3779b97f4a7c15))
	p = r.Perm(n)
	return
}

// --- extra Arrow builders (the widths encoder_test.go does not need) ---

func listU8(perEntity ...[]uint8) arrow.Array {
	lb := array.NewListBuilder(pool, arrow.PrimitiveTypes.Uint8)
	vb := lb.ValueBuilder().(*array.Uint8Builder)
	for _, vs := range perEntity {
		lb.Append(true)
		vb.AppendValues(vs, nil)
	}
	return lb.NewArray()
}

func listFsb(width int, perEntity ...[][]byte) arrow.Array {
	lb := array.NewListBuilder(pool, &arrow.FixedSizeBinaryType{ByteWidth: width})
	vb := lb.ValueBuilder().(*array.FixedSizeBinaryBuilder)
	for _, vs := range perEntity {
		lb.Append(true)
		for _, v := range vs {
			vb.Append(v)
		}
	}
	return lb.NewArray()
}

// yx4 / sx4 are the fixed-width four-byte string types, which ctabb does not
// abbreviate (widths are parameters).
var yx4 = canonicaltypes.StringAstNode{BaseType: canonicaltypes.BaseTypeStringBytes, WidthModifier: canonicaltypes.WidthModifierFixed, Width: 4}
var sx4 = canonicaltypes.StringAstNode{BaseType: canonicaltypes.BaseTypeStringUtf8, WidthModifier: canonicaltypes.WidthModifierFixed, Width: 4}

// --- widths: the value survives the declaration (SD2/SD3) ---

// widthPair drives the same drawn content through two one-section tables that
// differ only in the value column's declared width and asserts equal digests.
func widthPair(t *testing.T, rt *rapid.T, ctA canonicaltypes.PrimitiveAstNodeI, ctB canonicaltypes.PrimitiveAstNodeI, valuesA func() arrow.Array, valuesB func() arrow.Array, refs [][]uint64) {
	t.Helper()
	tblA, irA, convA := oneSection(t, "sec", ctA)
	tblB, irB, convB := oneSection(t, "sec", ctB)
	dA := oneSectionData{id: 1, ts: 5e9, refs: refs, values: valuesA}
	dB := oneSectionData{id: 2, ts: 5e9, refs: refs, values: valuesB}
	a := digestsOf(t, &tblA, irA, oneSectionBatch(t, irA, convA, dA), Options{})
	b := digestsOf(t, &tblB, irB, oneSectionBatch(t, irB, convB, dB), Options{})
	require.Equal(rt, hexs(a[0]), hexs(b[0]))
}

func TestPropertyWidthWidening(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		n := drawAttrCount(rt)
		refs := drawRefs(rt, n)
		u8s := make([]uint8, n)
		u64s := make([]uint64, n)
		i32s := make([]int32, n)
		i64s := make([]int64, n)
		f32s := make([]float32, n)
		f64s := make([]float64, n)
		mss := make([]int64, n)
		nss := make([]int64, n)
		blobs := make([][]byte, n)
		for i := range n {
			u8 := rapid.SampledFrom(m1U8).Draw(rt, "u8")
			u8s[i], u64s[i] = u8, uint64(u8)
			i32 := rapid.SampledFrom(m1I32).Draw(rt, "i32")
			i32s[i], i64s[i] = i32, int64(i32)
			f := rapid.SampledFrom(m1F32).Draw(rt, "f32")
			f32s[i], f64s[i] = f, float64(f)
			ms := rapid.SampledFrom(m1Ms).Draw(rt, "ms")
			mss[i], nss[i] = ms, ms*1_000_000
			blobs[i] = rapid.SampledFrom(m1Blob4).Draw(rt, "blob")
		}
		widthPair(t, rt, ctabb.U8, ctabb.U64,
			func() arrow.Array { return listU8(u8s) }, func() arrow.Array { return listU64(u64s) }, refs)
		widthPair(t, rt, ctabb.I32, ctabb.I64,
			func() arrow.Array { return listI32(i32s) }, func() arrow.Array { return listI64(i64s) }, refs)
		widthPair(t, rt, ctabb.F32, ctabb.F64,
			func() arrow.Array { return listF32(f32s) }, func() arrow.Array { return listF64(f64s) }, refs)
		widthPair(t, rt, ctabb.Z32, ctabb.Z64,
			func() arrow.Array { return listTs(arrow.Millisecond, mss) }, func() arrow.Array { return listTs(arrow.Nanosecond, nss) }, refs)
		// y → yx: the fixed-width modifier is erased and padding is kept, so
		// equality holds exactly when the stored bytes fill the width.
		widthPair(t, rt, ctabb.Y, yx4,
			func() arrow.Array {
				ss := make([]string, n)
				for i := range n {
					ss[i] = string(blobs[i])
				}
				return listBin(ss)
			},
			func() arrow.Array { return listFsb(4, blobs) }, refs)
		// s → sx: the text twin of the same erasure.
		texts := make([]string, n)
		textBlobs := make([][]byte, n)
		for i := range n {
			texts[i] = rapid.SampledFrom(m1Text4).Draw(rt, "text4")
			textBlobs[i] = []byte(texts[i])
		}
		widthPair(t, rt, ctabb.S, sx4,
			func() arrow.Array { return listBin(texts) },
			func() arrow.Array { return listFsb(4, textBlobs) }, refs)
	})
}

// --- representation toggles: renames, hints, aspects (SD2) ---

// aspectedSection is oneSection with the section renamed, the value column
// re-hinted, a value aspect added and the section's membership-uniformity
// hint declared — all of which SD2 erases (the uniformity hint is erased
// under a nil classifier; a classifier that honours it is SD5's stated
// carve-out).
func aspectedSection(t *testing.T, ct canonicaltypes.PrimitiveAstNodeI) (tbl common.TableDesc, ir *common.IntermediateTableRepresentation, conv common.NamingConventionI) {
	return buildTable(t, func(manip *common.TableManipulator) {
		manip.SetTableName("other")
		manip.PlainValueColumn(common.PlainItemTypeEntityId, "id", ctabb.U64)
		manip.PlainValueColumn(common.PlainItemTypeEntityTimestamp, "ts", ctabb.Z64)
		sec := manip.TaggedValueSection("renamed").
			AddSectionMembership(common.MembershipSpecLowCardRef).
			AddSectionUseAspects(useaspects.AspectSectionMembershipsAllPrimary)
		sec.TaggedValueColumn("v", ct)
	})
}

func TestPropertyRenameAndAspectsErased(t *testing.T) {
	tblA, irA, convA := oneSection(t, "alpha", ctabb.I64)
	tblB, irB, convB := aspectedSection(t, ctabb.I64)
	rapid.Check(t, func(rt *rapid.T) {
		n := drawAttrCount(rt)
		refs := drawRefs(rt, n)
		vals := make([]int64, n)
		for i := range n {
			vals[i] = int64(rapid.SampledFrom(m1I32).Draw(rt, "v"))
		}
		mk := func() arrow.Array { return listI64(vals) }
		a := digestsOf(t, &tblA, irA, oneSectionBatch(t, irA, convA, oneSectionData{id: 1, ts: 5e9, refs: refs, values: mk}), Options{})
		b := digestsOf(t, &tblB, irB, oneSectionBatch(t, irB, convB, oneSectionData{id: 1, ts: 5e9, refs: refs, values: mk}), Options{})
		require.Equal(rt, hexs(a[0]), hexs(b[0]), "table name, section name and use-aspect hints are representation")
	})
}

// --- permutations: attribute, membership and set-element order (SD1/SD4/SD5) ---

func TestPropertyPermutationInvariance(t *testing.T) {
	tbl, ir, conv := oneSection(t, "i64", ctabb.I64)
	tblSet, irSet, convSet := oneSection(t, "u64m", ctabb.U64m)
	rapid.Check(t, func(rt *rapid.T) {
		n := drawAttrCount(rt)
		refs := drawRefs(rt, n)
		vals := make([]int64, n)
		for i := range n {
			vals[i] = int64(rapid.SampledFrom(m1I32).Draw(rt, "v"))
		}
		enc := func(seed uint64) string {
			ap := permFromSeed(seed, n)
			prefs := make([][]uint64, n)
			pvals := make([]int64, n)
			for i, j := range ap {
				pvals[i] = vals[j]
				rp := permFromSeed(seed+uint64(i)+1, len(refs[j]))
				rs := make([]uint64, len(refs[j]))
				for k, l := range rp {
					rs[k] = refs[j][l]
				}
				prefs[i] = rs
			}
			d := oneSectionData{id: 1, ts: 5e9, refs: prefs, values: func() arrow.Array { return listI64(pvals) }}
			return hexs(digestsOf(t, &tbl, ir, oneSectionBatch(t, ir, conv, d), Options{})[0])
		}
		require.Equal(rt, enc(rapid.Uint64().Draw(rt, "seedA")), enc(rapid.Uint64().Draw(rt, "seedB")),
			"attribute order and membership order are representation")

		// Set elements: order and duplication are representation.
		m := rapid.IntRange(1, 5).Draw(rt, "elems")
		elems := make([]uint64, m)
		for i := range m {
			elems[i] = rapid.SampledFrom(m1Refs).Draw(rt, "elem")
		}
		encSet := func(es []uint64) string {
			d := oneSectionData{id: 1, ts: 5e9, refs: [][]uint64{{7}},
				values:    func() arrow.Array { return listU64(es) },
				lenOrCard: func() arrow.Array { return listU64([]uint64{uint64(len(es))}) }}
			return hexs(digestsOf(t, &tblSet, irSet, oneSectionBatch(t, irSet, convSet, d), Options{})[0])
		}
		shuffled := make([]uint64, m)
		for i, j := range permFromSeed(rapid.Uint64().Draw(rt, "seedC"), m) {
			shuffled[i] = elems[j]
		}
		withDup := append(append([]uint64(nil), elems...), elems[0])
		require.Equal(rt, encSet(elems), encSet(shuffled), "set element order is representation")
		require.Equal(rt, encSet(elems), encSet(withDup), "set duplicates are representation")
	})
}

// --- membership-channel cardinality is carriage (SD5) ---

// refSecTable is a one-section table whose single membership channel is the
// given spec; refSecBatch drives it whatever the spec's cardinality.
func refSecTable(t *testing.T, spec common.MembershipSpecE) (tbl common.TableDesc, ir *common.IntermediateTableRepresentation, conv common.NamingConventionI) {
	return buildTable(t, func(manip *common.TableManipulator) {
		manip.SetTableName("one")
		manip.PlainValueColumn(common.PlainItemTypeEntityId, "id", ctabb.U64)
		sec := manip.TaggedValueSection("sec").AddSectionMembership(spec)
		sec.TaggedValueColumn("v", ctabb.I64)
	})
}

func refSecBatch(t *testing.T, ir *common.IntermediateTableRepresentation, conv common.NamingConventionI, refs [][]uint64, vals []int64) arrow.RecordBatch {
	t.Helper()
	nAttrs := len(refs)
	return buildBatch(t, ir, conv, 1, func(t *testing.T, cc common.IntermediateColumnContext, role common.ColumnRoleE, phy string) arrow.Array {
		switch {
		case cc.Scope != common.IntermediateColumnScopeTagged:
			return plainU64(1)
		case role == common.ColumnRoleValue:
			return listI64(vals)
		case role == common.ColumnRoleLength || role == common.ColumnRoleCardinality:
			return ones(nAttrs)
		case role == common.ColumnRoleLowCardRef || role == common.ColumnRoleHighCardRef:
			var flat []uint64
			for _, rs := range refs {
				flat = append(flat, rs...)
			}
			return listU64(flat)
		case role == common.ColumnRoleLowCardRefCardinality || role == common.ColumnRoleHighCardRefCardinality:
			cards := make([]uint64, nAttrs)
			for i, rs := range refs {
				cards[i] = uint64(len(rs))
			}
			return listU64(cards)
		}
		t.Fatalf("fixture does not know how to build column %s (role %s)", phy, role)
		return nil
	})
}

func TestPropertyChannelCardinalityIsCarriage(t *testing.T) {
	tblL, irL, convL := refSecTable(t, common.MembershipSpecLowCardRef)
	tblH, irH, convH := refSecTable(t, common.MembershipSpecHighCardRef)
	rapid.Check(t, func(rt *rapid.T) {
		n := drawAttrCount(rt)
		refs := drawRefs(rt, n)
		vals := make([]int64, n)
		for i := range n {
			vals[i] = int64(rapid.SampledFrom(m1I32).Draw(rt, "v"))
		}
		a := digestsOf(t, &tblL, irL, refSecBatch(t, irL, convL, refs, vals), Options{})
		b := digestsOf(t, &tblH, irH, refSecBatch(t, irH, convH, refs, vals), Options{})
		require.Equal(rt, hexs(a[0]), hexs(b[0]), "low-card vs high-card ref carriage is representation")
	})
}

// --- secondary memberships are not content (SD5) ---

// verbSecTable carries one LowCardVerbatim channel; under PathPrefixClassifier
// a "/"-prefixed verbatim is primary and anything else secondary.
func verbSecTable(t *testing.T) (tbl common.TableDesc, ir *common.IntermediateTableRepresentation, conv common.NamingConventionI) {
	return buildTable(t, func(manip *common.TableManipulator) {
		manip.SetTableName("one")
		manip.PlainValueColumn(common.PlainItemTypeEntityId, "id", ctabb.U64)
		sec := manip.TaggedValueSection("sec").AddSectionMembership(common.MembershipSpecLowCardVerbatim)
		sec.TaggedValueColumn("v", ctabb.I64)
	})
}

func verbSecBatch(t *testing.T, ir *common.IntermediateTableRepresentation, conv common.NamingConventionI, verbs [][]string, vals []int64) arrow.RecordBatch {
	t.Helper()
	nAttrs := len(verbs)
	return buildBatch(t, ir, conv, 1, func(t *testing.T, cc common.IntermediateColumnContext, role common.ColumnRoleE, phy string) arrow.Array {
		switch {
		case cc.Scope != common.IntermediateColumnScopeTagged:
			return plainU64(1)
		case role == common.ColumnRoleValue:
			return listI64(vals)
		case role == common.ColumnRoleLength || role == common.ColumnRoleCardinality:
			return ones(nAttrs)
		case role == common.ColumnRoleLowCardVerbatim:
			var flat []string
			for _, vs := range verbs {
				flat = append(flat, vs...)
			}
			return listBin(flat)
		case role == common.ColumnRoleLowCardVerbatimCardinality:
			cards := make([]uint64, nAttrs)
			for i, vs := range verbs {
				cards[i] = uint64(len(vs))
			}
			return listU64(cards)
		}
		t.Fatalf("fixture does not know how to build column %s (role %s)", phy, role)
		return nil
	})
}

func TestPropertySecondaryMembershipsAreNotContent(t *testing.T) {
	tbl, ir, conv := verbSecTable(t)
	opts := Options{Classifier: membershiprole.PathPrefixClassifier{}}
	rapid.Check(t, func(rt *rapid.T) {
		n := drawAttrCount(rt)
		verbs := make([][]string, n)
		vals := make([]int64, n)
		for i := range n {
			vals[i] = int64(rapid.SampledFrom(m1I32).Draw(rt, "v"))
			verbs[i] = []string{"/" + rapid.SampledFrom([]string{"p", "q", "r"}).Draw(rt, "prim")}
			for range rapid.IntRange(0, 2).Draw(rt, "secCount") {
				verbs[i] = append(verbs[i], rapid.SampledFrom([]string{"label", "x", "gov"}).Draw(rt, "sec"))
			}
		}
		base := hexs(digestsOf(t, &tbl, ir, verbSecBatch(t, ir, conv, verbs, vals), opts)[0])

		// Editing, adding and removing secondaries moves nothing.
		edited := make([][]string, n)
		for i := range verbs {
			edited[i] = []string{verbs[i][0]}
			for range rapid.IntRange(0, 3).Draw(rt, "secCount2") {
				edited[i] = append(edited[i], rapid.SampledFrom([]string{"other", "tag"}).Draw(rt, "sec2"))
			}
		}
		require.Equal(rt, base, hexs(digestsOf(t, &tbl, ir, verbSecBatch(t, ir, conv, edited, vals), opts)[0]),
			"secondary memberships are annotations, not content")

		// An attribute carrying only secondaries contributes nothing.
		withOverlay := append(append([][]string(nil), verbs...), []string{"overlay"})
		valsOverlay := append(append([]int64(nil), vals...), 999)
		require.Equal(rt, base, hexs(digestsOf(t, &tbl, ir, verbSecBatch(t, ir, conv, withOverlay, valsOverlay), opts)[0]),
			"an attribute with no primary membership is omitted entirely")

		// Editing a primary is a content change.
		primEdited := append([][]string(nil), verbs...)
		primEdited[0] = append([]string{verbs[0][0] + "!"}, verbs[0][1:]...)
		require.NotEqual(rt, base, hexs(digestsOf(t, &tbl, ir, verbSecBatch(t, ir, conv, primEdited, vals), opts)[0]))
	})
}

// --- content sensitivity ---

func TestPropertyContentSensitivity(t *testing.T) {
	tbl, ir, conv := oneSection(t, "i64", ctabb.I64)
	rapid.Check(t, func(rt *rapid.T) {
		n := drawAttrCount(rt)
		refs := drawRefs(rt, n)
		vals := make([]int64, n)
		for i := range n {
			vals[i] = int64(rapid.SampledFrom(m1I32).Draw(rt, "v"))
		}
		enc := func(rs [][]uint64, vs []int64) string {
			d := oneSectionData{id: 1, ts: 5e9, refs: rs, values: func() arrow.Array { return listI64(vs) }}
			return hexs(digestsOf(t, &tbl, ir, oneSectionBatch(t, ir, conv, d), Options{})[0])
		}
		base := enc(refs, vals)
		i := rapid.IntRange(0, n-1).Draw(rt, "site")
		mutVals := append([]int64(nil), vals...)
		mutVals[i] ^= 1
		require.NotEqual(rt, base, enc(refs, mutVals), "a value edit is content")
		mutRefs := make([][]uint64, n)
		for j := range refs {
			mutRefs[j] = append([]uint64(nil), refs[j]...)
		}
		mutRefs[i][0] ^= 1
		require.NotEqual(rt, base, enc(mutRefs, vals), "a primary membership edit is content")
	})
}

// --- the self-check: every emitted item is RFC 8949 §4.2 deterministic ---

func TestPropertyStrictReencode(t *testing.T) {
	tbl, ir, conv := oneSection(t, "u64m", ctabb.U64m)
	em, err := cbor.CoreDetEncOptions().EncMode()
	require.NoError(t, err)
	dm, err := cbor.DecOptions{DupMapKey: cbor.DupMapKeyEnforcedAPF}.DecMode()
	require.NoError(t, err)
	rapid.Check(t, func(rt *rapid.T) {
		n := drawAttrCount(rt)
		refs := drawRefs(rt, n)
		elems := make([][]uint64, n)
		cards := make([]uint64, n)
		for i := range n {
			m := rapid.IntRange(0, 4).Draw(rt, "elems")
			for range m {
				elems[i] = append(elems[i], rapid.SampledFrom(m1Refs).Draw(rt, "elem"))
			}
			cards[i] = uint64(m)
		}
		var flat []uint64
		for _, es := range elems {
			flat = append(flat, es...)
		}
		var leaves, record bytes.Buffer
		opts := Options{Digester: NewRecordingDigester(NewBlake3Digester(), &leaves, &record)}
		d := oneSectionData{id: 1, ts: 5e9, refs: refs,
			values:    func() arrow.Array { return listU64(flat) },
			lenOrCard: func() arrow.Array { return listU64(cards) }}
		_ = digestsOf(t, &tbl, ir, oneSectionBatch(t, ir, conv, d), opts)

		check := func(concat []byte) (nItems int) {
			rest := concat
			for len(rest) > 0 {
				var v any
				after, uerr := cbor.UnmarshalFirst(rest, &v)
				require.NoError(rt, uerr)
				item := rest[:len(rest)-len(after)]
				var v2 any
				require.NoError(rt, dm.Unmarshal(item, &v2))
				again, merr := em.Marshal(v2)
				require.NoError(rt, merr)
				require.Equal(rt, hexs(item), hexs(again), "re-encoding under CoreDetEncOptions must reproduce the item")
				rest = after
				nItems++
			}
			return
		}
		require.Equal(rt, n, check(leaves.Bytes()), "one leaf item per attribute")
		require.Equal(rt, 1, check(record.Bytes()))
	})
}

// --- streaming: values reach the leaf hashers, never the record hasher ---

// countingWriter counts bytes and remembers the global write sequence number
// of its first write.
type countingWriter struct {
	seq      *int
	total    int
	firstSeq int
	writes   int
}

func (w *countingWriter) Write(p []byte) (int, error) {
	*w.seq++
	if w.writes == 0 {
		w.firstSeq = *w.seq
	}
	w.writes++
	w.total += len(p)
	return len(p), nil
}

// The record hasher sees the entity item only — plains and 32-byte leaf
// digests — while the value bytes stream into the leaf hashers. A megabyte
// value must therefore reach the leaf side and not the record side, and every
// leaf write must precede the record item's writes (ADR-0201 SD7: nothing is
// materialized, the failure mode its verification plan names).
func TestStreamingNoMaterialization(t *testing.T) {
	tbl, ir, conv := oneSection(t, "s", ctabb.S)
	seq := 0
	leaves := &countingWriter{seq: &seq}
	record := &countingWriter{seq: &seq}
	big := strings.Repeat("z", 1<<20)
	d := oneSectionData{id: 1, ts: 5e9, refs: [][]uint64{{7}}, values: func() arrow.Array { return listBin([]string{big}) }}
	opts := Options{Digester: NewRecordingDigester(NewBlake3Digester(), leaves, record)}
	_ = digestsOf(t, &tbl, ir, oneSectionBatch(t, ir, conv, d), opts)
	require.GreaterOrEqual(t, leaves.total, 1<<20, "the value bytes stream into the leaf hasher")
	require.Less(t, record.total, 256, "the record hasher sees plains and digests, never value bytes")
	require.Greater(t, record.firstSeq, leaves.firstSeq+leaves.writes-1, "every leaf write precedes the entity item")
}
