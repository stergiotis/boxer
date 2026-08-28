package canonwire

import (
	"math/rand/v2"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/code/synthesis/golang/align"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	"github.com/stergiotis/boxer/public/semistructured/leeway/dml"
	encodingaspects2 "github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/gocodegen"
	"github.com/stergiotis/boxer/public/semistructured/leeway/mapping"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/readaccess"
	useaspects2 "github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
	"github.com/stergiotis/boxer/public/unittest"
)

// The four goldens under example/ are the acceptance test for the encoder
// codegen: the generated encoder is only correct if it compiles against the
// generated readaccess classes it calls, and `go build ./example` is what says
// so. The tables are picked for the shapes the form has to handle —
//
//   - test_table: scalars, an `h` container, a shared membership pack carrying
//     both a ref channel and a mixed carrier channel;
//   - net_table: the four network lanes, whose readaccess accessors hand out
//     packed lane values (uint32 / [16]byte / [5]byte / [17]byte) rather than
//     net/netip types;
//   - json: the ambiguity story of ADR-0210 SD2 — four value-less sections
//     sharing the empty signature and two sharing `s`;
//   - place: a co-section group, which is one slot with two membership groups,
//     beside a standalone section whose `h` and `m` columns are co-containers;
//   - test_table_renamed: test_table's types under different names, orders,
//     hints and aspects — the cross-table requirement of ADR-0210;
//   - test_table_narrow: test_table with one section's membership spec
//     narrowed, which is the cross-table refusal.
//
// The readaccess and dml classes are generated beside them because the encoder
// calls the first by name and the smoke test writes through the second.

// sampleTableDesc is readaccess's own sample table, copied because it is
// unexported there. Keep it in step with
// readaccess/lw_ra_generator_test.go:sampleTableDesc.
func sampleTableDesc() (tbl common.TableDesc, err error) {
	var manip *common.TableManipulator
	manip, err = common.NewTableManipulator()
	if err != nil {
		err = eh.Errorf("unable to create table manipulator")
		return
	}
	var hintsId, hintsTs, hintsProc encodingaspects2.AspectSet
	hintsId, err = encodingaspects2.EncodeAspects(encodingaspects2.AspectDeltaEncoding, encodingaspects2.AspectLightGeneralCompression)
	if err != nil {
		err = eh.Errorf("unable to encode hints: %w", err)
		return
	}
	hintsTs, err = encodingaspects2.EncodeAspects(encodingaspects2.AspectDeltaEncoding, encodingaspects2.AspectLightGeneralCompression)
	if err != nil {
		err = eh.Errorf("unable to encode hints: %w", err)
		return
	}
	hintsProc, err = encodingaspects2.EncodeAspects(encodingaspects2.AspectLightGeneralCompression)
	if err != nil {
		err = eh.Errorf("unable to encode hints: %w", err)
		return
	}
	manip.AddPlainValueItem(common.PlainItemTypeEntityId, "id", ctabb.U64, hintsId, valueaspects.EmptyAspectSet)
	manip.AddPlainValueItem(common.PlainItemTypeEntityTimestamp, "ts", ctabb.Z32, hintsTs, valueaspects.EmptyAspectSet)
	manip.AddPlainValueItem(common.PlainItemTypeEntityTimestamp, "proc", ctabb.Z32h, hintsProc, valueaspects.EmptyAspectSet)
	{
		sec := manip.TaggedValueSection("geo").
			AddSectionMembership(common.MembershipSpecLowCardRef).
			AddSectionMembership(common.MembershipSpecMixedLowCardVerbatimHighCardParameters)
		sec.TaggedValueColumn("lat", ctabb.F32)
		sec.TaggedValueColumn("lng", ctabb.F32)
		sec.TaggedValueColumn("h3_res1", ctabb.U64)
		sec.TaggedValueColumn("h3_res2", ctabb.U64)
	}
	{
		sec := manip.TaggedValueSection("text").
			AddSectionMembership(common.MembershipSpecLowCardRef).
			AddSectionMembership(common.MembershipSpecMixedLowCardVerbatimHighCardParameters)
		sec.TaggedValueColumn("text", ctabb.S)
		sec.TaggedValueColumn("word_length", ctabb.U32h)
		sec.TaggedValueColumn("words", ctabb.Sh)
	}
	return manip.BuildTableDesc()
}

// networkSampleTableDesc is readaccess's network sample table, copied for the
// same reason. Keep it in step with
// readaccess/lw_ra_generator_test.go:networkSampleTableDesc.
func networkSampleTableDesc() (tbl common.TableDesc, err error) {
	var manip *common.TableManipulator
	manip, err = common.NewTableManipulator()
	if err != nil {
		err = eh.Errorf("unable to create table manipulator")
		return
	}
	var hintsId, hintsTs encodingaspects2.AspectSet
	hintsId, err = encodingaspects2.EncodeAspects(encodingaspects2.AspectDeltaEncoding, encodingaspects2.AspectLightGeneralCompression)
	if err != nil {
		err = eh.Errorf("unable to encode hints: %w", err)
		return
	}
	hintsTs, err = encodingaspects2.EncodeAspects(encodingaspects2.AspectDeltaEncoding, encodingaspects2.AspectLightGeneralCompression)
	if err != nil {
		err = eh.Errorf("unable to encode hints: %w", err)
		return
	}
	manip.AddPlainValueItem(common.PlainItemTypeEntityId, "id", ctabb.U64, hintsId, valueaspects.EmptyAspectSet)
	manip.AddPlainValueItem(common.PlainItemTypeEntityTimestamp, "ts", ctabb.Z32, hintsTs, valueaspects.EmptyAspectSet)
	{
		sec := manip.TaggedValueSection("net").
			AddSectionMembership(common.MembershipSpecLowCardRef).
			AddSectionMembership(common.MembershipSpecMixedLowCardVerbatimHighCardParameters)
		sec.TaggedValueColumn("ipv4", ctabb.V)
		sec.TaggedValueColumn("ipv6", ctabb.W)
		sec.TaggedValueColumn("ipv4_cidr", ctabb.Vc)
		sec.TaggedValueColumn("ipv6_cidr", ctabb.Wc)
	}
	return manip.BuildTableDesc()
}

// jsonTableDesc is the leeway JSON mapping in its lossless variant: four
// value-less sections share the empty signature and two share `s`, which is the
// pair of ambiguity sets ADR-0210 SD5's dispatch exists for.
func jsonTableDesc() (tbl common.TableDesc, err error) {
	var manip *common.TableManipulator
	manip, err = common.NewTableManipulator()
	if err != nil {
		err = eh.Errorf("unable to create table manipulator")
		return
	}
	mapping.LoadJsonMappingLossless(manip)
	return manip.BuildTableDesc()
}

// placeTableDesc is a co-section group — geo and h3 are one atomic unit, so
// they are one slot with two membership groups — beside a standalone section on
// a different membership channel whose two container columns, one `h` and one
// `m`, are co-containers of each other.
func placeTableDesc() (tbl common.TableDesc, err error) {
	var manip *common.TableManipulator
	manip, err = common.NewTableManipulator()
	if err != nil {
		err = eh.Errorf("unable to create table manipulator")
		return
	}
	var hintsId encodingaspects2.AspectSet
	hintsId, err = encodingaspects2.EncodeAspects(encodingaspects2.AspectDeltaEncoding, encodingaspects2.AspectLightGeneralCompression)
	if err != nil {
		err = eh.Errorf("unable to encode hints: %w", err)
		return
	}
	manip.AddPlainValueItem(common.PlainItemTypeEntityId, "id", ctabb.U64, hintsId, valueaspects.EmptyAspectSet)
	{
		sec := manip.TaggedValueSection("geo").
			AddSectionMembership(common.MembershipSpecLowCardRef).
			SectionCoSectionGroup("place")
		sec.TaggedValueColumn("lat", ctabb.F32)
		sec.TaggedValueColumn("lng", ctabb.F32)
	}
	{
		sec := manip.TaggedValueSection("h3").
			AddSectionMembership(common.MembershipSpecLowCardRef).
			SectionCoSectionGroup("place")
		sec.TaggedValueColumn("cell", ctabb.U64)
	}
	{
		// An `h` column beside an `m` column: the two are DML co-containers —
		// one element is appended to both at once — so the set's length is
		// content and the form keeps its duplicates (ADR-0210 SD3).
		sec := manip.TaggedValueSection("tags").
			AddSectionMembership(common.MembershipSpecLowCardVerbatim)
		sec.TaggedValueColumn("tag", ctabb.Sh)
		sec.TaggedValueColumn("tag_id", ctabb.U64m)
	}
	return manip.BuildTableDesc()
}

// renamedSampleTableDesc is sampleTableDesc's twin: the same canonical types in
// the same co-topology and under the same membership specs, but with every
// section and column renamed, the two tagged sections declared in the opposite
// order, the columns permuted inside each section, the two plain timestamp
// columns swapped, and different encoding hints, value aspects and section
// use-aspects.
//
// None of what differs is on the wire (ADR-0210 SD2), so an entity written
// through test_table must decode into this table and back. The column mapping
// follows from the key order — the canonical types stable-sorted — and not from
// the names:
//
//	geo.lat        -> coords.north          text.text        -> phrases.phrase
//	geo.lng        -> coords.east           text.words       -> phrases.tokens
//	geo.h3_res1    -> coords.coarse         text.word_length -> phrases.token_lengths
//	geo.h3_res2    -> coords.fine
func renamedSampleTableDesc() (tbl common.TableDesc, err error) {
	var manip *common.TableManipulator
	manip, err = common.NewTableManipulator()
	if err != nil {
		err = eh.Errorf("unable to create table manipulator")
		return
	}
	var hintsIdent, hintsSeen, hintsStamped encodingaspects2.AspectSet
	hintsIdent, err = encodingaspects2.EncodeAspects(encodingaspects2.AspectDoubleDeltaEncoding, encodingaspects2.AspectHeavyGeneralCompression)
	if err != nil {
		err = eh.Errorf("unable to encode hints: %w", err)
		return
	}
	hintsSeen, err = encodingaspects2.EncodeAspects(encodingaspects2.AspectUltraLightGeneralCompression)
	if err != nil {
		err = eh.Errorf("unable to encode hints: %w", err)
		return
	}
	hintsStamped, err = encodingaspects2.EncodeAspects(encodingaspects2.AspectSparse, encodingaspects2.AspectHeavyGeneralCompression)
	if err != nil {
		err = eh.Errorf("unable to encode hints: %w", err)
		return
	}
	var semanticsIdent valueaspects.AspectSet
	semanticsIdent, err = valueaspects.EncodeAspects(valueaspects.AspectIdSurrogateKey, valueaspects.AspectImmutable)
	if err != nil {
		err = eh.Errorf("unable to encode value aspects: %w", err)
		return
	}
	// The plain timestamp columns are declared the other way round: the `h`
	// one first. The wire keys plains by item type and orders their columns by
	// canonical type (SD2), so neither the swap nor the item-type declaration
	// order reaches the bytes.
	manip.AddPlainValueItem(common.PlainItemTypeEntityTimestamp, "stamped_at", ctabb.Z32h, hintsStamped, valueaspects.EmptyAspectSet)
	manip.AddPlainValueItem(common.PlainItemTypeEntityTimestamp, "seen_at", ctabb.Z32, hintsSeen, valueaspects.EmptyAspectSet)
	manip.AddPlainValueItem(common.PlainItemTypeEntityId, "ident", ctabb.U64, hintsIdent, semanticsIdent)
	{
		// text's twin, declared first and with its columns permuted.
		sec := manip.TaggedValueSection("phrases").
			AddSectionUseAspects(useaspects2.AspectDocumentation, useaspects2.AspectQuality).
			AddSectionMembership(common.MembershipSpecLowCardRef).
			AddSectionMembership(common.MembershipSpecMixedLowCardVerbatimHighCardParameters)
		sec.TaggedValueColumn("tokens", ctabb.Sh).
			AddColumnEncodingHints(encodingaspects2.AspectHeavyGeneralCompression)
		sec.TaggedValueColumn("phrase", ctabb.S).
			AddColumnValueSemantics(valueaspects.AspectHumanReadable)
		sec.TaggedValueColumn("token_lengths", ctabb.U32h).
			AddColumnEncodingHints(encodingaspects2.AspectLightBiasSmallInteger)
	}
	{
		// geo's twin, declared second and with its columns interleaved.
		sec := manip.TaggedValueSection("coords").
			AddSectionUseAspects(useaspects2.AspectSpatial).
			AddSectionMembership(common.MembershipSpecLowCardRef).
			AddSectionMembership(common.MembershipSpecMixedLowCardVerbatimHighCardParameters)
		sec.TaggedValueColumn("coarse", ctabb.U64).
			AddColumnEncodingHints(encodingaspects2.AspectSparse)
		sec.TaggedValueColumn("north", ctabb.F32).
			AddColumnValueSemantics(valueaspects.AspectMeasured)
		sec.TaggedValueColumn("fine", ctabb.U64)
		sec.TaggedValueColumn("east", ctabb.F32).
			AddColumnValueSemantics(valueaspects.AspectMeasured)
	}
	return manip.BuildTableDesc()
}

// narrowSampleTableDesc is sampleTableDesc with the `text` section's membership
// spec narrowed to LowCardRef alone. Every signature is unchanged, so the slot
// key still matches; what does not match is the carriage, and the narrowing
// step of ADR-0210 SD5 is what turns that into ErrChannelNotAccepted rather
// than a silently dropped membership.
func narrowSampleTableDesc() (tbl common.TableDesc, err error) {
	var manip *common.TableManipulator
	manip, err = common.NewTableManipulator()
	if err != nil {
		err = eh.Errorf("unable to create table manipulator")
		return
	}
	var hintsId, hintsTs, hintsProc encodingaspects2.AspectSet
	hintsId, err = encodingaspects2.EncodeAspects(encodingaspects2.AspectDeltaEncoding, encodingaspects2.AspectLightGeneralCompression)
	if err != nil {
		err = eh.Errorf("unable to encode hints: %w", err)
		return
	}
	hintsTs, err = encodingaspects2.EncodeAspects(encodingaspects2.AspectDeltaEncoding, encodingaspects2.AspectLightGeneralCompression)
	if err != nil {
		err = eh.Errorf("unable to encode hints: %w", err)
		return
	}
	hintsProc, err = encodingaspects2.EncodeAspects(encodingaspects2.AspectLightGeneralCompression)
	if err != nil {
		err = eh.Errorf("unable to encode hints: %w", err)
		return
	}
	manip.AddPlainValueItem(common.PlainItemTypeEntityId, "id", ctabb.U64, hintsId, valueaspects.EmptyAspectSet)
	manip.AddPlainValueItem(common.PlainItemTypeEntityTimestamp, "ts", ctabb.Z32, hintsTs, valueaspects.EmptyAspectSet)
	manip.AddPlainValueItem(common.PlainItemTypeEntityTimestamp, "proc", ctabb.Z32h, hintsProc, valueaspects.EmptyAspectSet)
	{
		sec := manip.TaggedValueSection("geo").
			AddSectionMembership(common.MembershipSpecLowCardRef).
			AddSectionMembership(common.MembershipSpecMixedLowCardVerbatimHighCardParameters)
		sec.TaggedValueColumn("lat", ctabb.F32)
		sec.TaggedValueColumn("lng", ctabb.F32)
		sec.TaggedValueColumn("h3_res1", ctabb.U64)
		sec.TaggedValueColumn("h3_res2", ctabb.U64)
	}
	{
		// The narrowing: no mixed carrier channel here.
		sec := manip.TaggedValueSection("text").
			AddSectionMembership(common.MembershipSpecLowCardRef)
		sec.TaggedValueColumn("text", ctabb.S)
		sec.TaggedValueColumn("word_length", ctabb.U32h)
		sec.TaggedValueColumn("words", ctabb.Sh)
	}
	return manip.BuildTableDesc()
}

// generateGoldens writes the readaccess, dml and canonwire classes of one table
// into example/. align.WriteAligned type-checks what it writes, and the
// package's own `go build` is what proves the encoder calls the accessors it
// was generated against.
func generateGoldens(t *testing.T, fileStem string, tableName string, tblDesc common.TableDesc) {
	t.Helper()
	conv, err := ddl.NewHumanReadableNamingConvention(":")
	require.NoError(t, err)
	chTech := clickhouse.NewTechnologySpecificCodeGenerator()
	namer := gocodegen.NewMultiTablePerPackageGoClassNamer()
	name := naming.MustBeValidStylableName(tableName)
	const tableRowConfig = common.TableRowConfigMultiAttributesPerRow

	raSrc, _, err := readaccess.NewGoCodeGeneratorDriver(conv, chTech, true).
		GenerateGoClasses("example", name, tblDesc, tableRowConfig, namer)
	require.NoError(t, err)
	require.NoError(t, align.WriteAligned(filepath.Join("example", fileStem+"_ra.out.go"), raSrc))

	dmlSrc, _, err := dml.NewGoCodeGeneratorDriver(conv, chTech).
		GenerateGoClasses("example", name, tblDesc, tableRowConfig, namer)
	require.NoError(t, err)
	require.NoError(t, align.WriteAligned(filepath.Join("example", fileStem+"_dml.out.go"), dmlSrc))

	cwSrc, wellFormed, err := NewGoCodeGeneratorDriver(conv, chTech).
		GenerateGoClasses("example", name, tblDesc, tableRowConfig, namer)
	require.NoError(t, err)
	require.True(t, wellFormed)
	require.NoError(t, align.WriteAligned(filepath.Join("example", fileStem+"_canonwire.out.go"), cwSrc))
}

func TestGenerateCanonWireTestTable(t *testing.T) {
	tblDesc, err := sampleTableDesc()
	require.NoError(t, err)
	generateGoldens(t, "testtable", "test_table", tblDesc)
}

func TestGenerateCanonWireNetTable(t *testing.T) {
	tblDesc, err := networkSampleTableDesc()
	require.NoError(t, err)
	generateGoldens(t, "nettable", "net_table", tblDesc)
}

func TestGenerateCanonWireJson(t *testing.T) {
	tblDesc, err := jsonTableDesc()
	require.NoError(t, err)
	generateGoldens(t, "json", "json", tblDesc)
}

func TestGenerateCanonWirePlace(t *testing.T) {
	tblDesc, err := placeTableDesc()
	require.NoError(t, err)
	generateGoldens(t, "place", "place", tblDesc)
}

func TestGenerateCanonWireTestTableRenamed(t *testing.T) {
	tblDesc, err := renamedSampleTableDesc()
	require.NoError(t, err)
	generateGoldens(t, "testtablerenamed", "test_table_renamed", tblDesc)
}

func TestGenerateCanonWireTestTableNarrow(t *testing.T) {
	tblDesc, err := narrowSampleTableDesc()
	require.NoError(t, err)
	generateGoldens(t, "testtablenarrow", "test_table_narrow", tblDesc)
}

// The renamed table must key its slots exactly as the source does: the slot
// signatures are the whole cross-table contract of ADR-0210 SD2, and if the
// renaming, the reordering or the re-hinting moved one of them the crosstable
// test would fail with a decode error rather than with a mismatch.
func TestRenamedTableKeysTheSameSlots(t *testing.T) {
	src, err := sampleTableDesc()
	require.NoError(t, err)
	renamed, err := renamedSampleTableDesc()
	require.NoError(t, err)
	srcSlots, err := BuildSlotTable(&src)
	require.NoError(t, err)
	renamedSlots, err := BuildSlotTable(&renamed)
	require.NoError(t, err)

	sigs := func(tbl *SlotTable) (out []string) {
		for i := range tbl.Slots {
			out = append(out, tbl.Slots[i].Signature)
		}
		return
	}
	require.Equal(t, sigs(&srcSlots), sigs(&renamedSlots))
	require.Empty(t, srcSlots.Ambiguous())
	require.Empty(t, renamedSlots.Ambiguous())

	groups := func(tbl *SlotTable) (out []string) {
		for i := range tbl.Plains {
			out = append(out, tbl.Plains[i].ItemType.String()+"="+tbl.Plains[i].Group)
		}
		return
	}
	require.Equal(t, groups(&srcSlots), groups(&renamedSlots))
}

// The ambiguity sets the JSON table's goldens are there to exercise, asserted
// on the slot table rather than on the emitted text.
func TestJsonSlotAmbiguity(t *testing.T) {
	tblDesc, err := jsonTableDesc()
	require.NoError(t, err)
	slots, err := BuildSlotTable(&tblDesc)
	require.NoError(t, err)
	require.Equal(t, []string{"", "s"}, slots.Ambiguous())
	require.Len(t, slots.BySignature[""], 4)
	require.Len(t, slots.BySignature["s"], 2)
}

// acceptMaskRow matches one row of a generated canonWireAcceptMasks table:
// the per-section bitmasks of one slot, in signature order.
var acceptMaskLineRe = regexp.MustCompile(`^\t\{([^}]*)\},`)

// parseAcceptMasks reads the accepted-channel table back out of a golden. The
// masks are baked at generation time, so nothing at run time can catch them
// drifting from the specs they were computed from; parsing them back is the
// only place that check can live.
func parseAcceptMasks(t *testing.T, path string) (masks [][]uint32) {
	t.Helper()
	src, err := os.ReadFile(path)
	require.NoError(t, err)
	lines := strings.Split(string(src), "\n")
	start := -1
	for i, l := range lines {
		if strings.HasPrefix(l, "var canonWireAcceptMasks") && strings.HasSuffix(l, "[][]uint32{") {
			start = i + 1
			break
		}
	}
	require.GreaterOrEqual(t, start, 0, "no accepted-channel table in %s", path)
	for _, l := range lines[start:] {
		if l == "}" {
			return
		}
		m := acceptMaskLineRe.FindStringSubmatch(l)
		require.NotNil(t, m, "unparsable row %q in %s", l, path)
		row := make([]uint32, 0, 2)
		for _, f := range strings.Split(m[1], ",") {
			f = strings.TrimSpace(f)
			if f == "" {
				continue
			}
			var v uint64
			v, err = strconv.ParseUint(strings.TrimPrefix(f, "0x"), 16, 32)
			require.NoError(t, err, "unparsable mask %q in %s", f, path)
			row = append(row, uint32(v))
		}
		masks = append(masks, row)
	}
	t.Fatalf("unterminated accepted-channel table in %s", path)
	return
}

// TestGeneratedAcceptMasksMatchSpecs pins the generated accept masks of every
// golden to SpecChannels of the section's declared MembershipSpecE. They are
// the whole input to the narrowing step of ADR-0210 SD5: a mask too wide lets
// an attribute into a section that cannot store its carriage, and a mask too
// narrow refuses one the table does accept.
func TestGeneratedAcceptMasksMatchSpecs(t *testing.T) {
	cases := []struct {
		desc func() (common.TableDesc, error)
		stem string
	}{
		{sampleTableDesc, "testtable"},
		{networkSampleTableDesc, "nettable"},
		{jsonTableDesc, "json"},
		{placeTableDesc, "place"},
		{renamedSampleTableDesc, "testtablerenamed"},
		{narrowSampleTableDesc, "testtablenarrow"},
	}
	for _, tc := range cases {
		t.Run(tc.stem, func(t *testing.T) {
			tblDesc, err := tc.desc()
			require.NoError(t, err)
			var slots SlotTable
			slots, err = BuildSlotTable(&tblDesc)
			require.NoError(t, err)
			got := parseAcceptMasks(t, filepath.Join("example", tc.stem+"_canonwire.out.go"))
			require.Len(t, got, len(slots.Slots))
			for i := range slots.Slots {
				secs := slots.Slots[i].Sections
				require.Len(t, got[i], len(secs), "slot %d", i)
				for g := range secs {
					var want uint32
					for _, ch := range SpecChannels(secs[g].MembershipSpec) {
						want |= uint32(1) << uint(ch)
					}
					require.Equal(t, want, got[i][g], "slot %d section %s", i, secs[g].Name)
				}
			}
		})
	}
}

// TestCanonWireGoClassBuilderSample mirrors the readaccess and dml fuzz tests:
// random tables from the shared manipulator, asserting the generated source is
// well-formed. A table the readaccess generator itself refuses is skipped —
// there would be no accessors for the encoder to call.
func TestCanonWireGoClassBuilderSample(t *testing.T) {
	seed1, seed2 := rand.Uint64(), rand.Uint64()
	t.Logf("randomized test seed: %d %d (rand.NewPCG)", seed1, seed2)
	rnd := rand.New(rand.NewPCG(seed1, seed2))
	manip, err := common.NewTableManipulator()
	require.NoError(t, err)

	conv, err := ddl.NewHumanReadableNamingConvention(":")
	require.NoError(t, err)
	tech := clickhouse.NewTechnologySpecificCodeGenerator()
	raDriver := readaccess.NewGoCodeGeneratorDriver(conv, tech, true)
	driver := NewGoCodeGeneratorDriver(conv, tech)

	const tableRowConfig = common.TableRowConfigMultiAttributesPerRow
	namer := gocodegen.NewMultiTablePerPackageGoClassNamer()
	acceptCanonicalType := tech.CheckTypeCompatibility
	acceptEncodingAspect := ddl.EncodingAspectFilterFuncFromTechnology(tech, common.ImplementationStatusFull)
	n := 1000
	if testing.Short() {
		n = 10
	}
	skipped := 0
	for i := 0; i < n; i++ {
		manip.Reset()
		require.NoError(t, common.PopulateManipulator(manip, rnd, acceptCanonicalType, acceptEncodingAspect))
		manip.SetTableName("sample")
		var tblDesc common.TableDesc
		tblDesc, err = manip.BuildTableDesc()
		require.NoError(t, err)
		tableName := naming.MustBeValidStylableName("testtable")
		if _, _, raErr := raDriver.GenerateGoClasses("example", tableName, tblDesc, tableRowConfig, namer); raErr != nil {
			// The encoder calls accessors that would not exist: a canonical
			// type the readaccess generator refuses is out of scope here, not
			// a failure of this generator.
			skipped++
			continue
		}
		var sourceCode []byte
		var wellFormed bool
		sourceCode, wellFormed, err = driver.GenerateGoClasses("example", tableName, tblDesc, tableRowConfig, namer)
		unittest.NoError(t, err)
		if !wellFormed && testing.Verbose() {
			// Into the test's temp dir, never the package dir: a `.go` file
			// dropped beside the sources makes the package uncompilable for
			// everyone until someone notices and deletes it.
			dump := filepath.Join(t.TempDir(), "malformed.out.go.txt")
			if werr := os.WriteFile(dump, sourceCode, 0o644); werr == nil {
				t.Logf("malformed generated source written to %s", dump)
			}
		}
		require.True(t, wellFormed)
	}
	if skipped == n {
		t.Skipf("all %d random tables carry canonical types the readaccess generator refuses", n)
	}
	if skipped > 0 {
		t.Logf("%d of %d random tables skipped: the readaccess generator refuses their canonical types", skipped, n)
	}
}
