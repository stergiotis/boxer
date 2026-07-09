package readaccess

import (
	"math/rand/v2"
	"os"
	"testing"

	"github.com/stergiotis/boxer/public/code/synthesis/golang"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	"github.com/stergiotis/boxer/public/semistructured/leeway/dml"
	encodingaspects2 "github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/gocodegen"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
	"github.com/stergiotis/boxer/public/unittest"
	"github.com/stretchr/testify/require"
)

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

func TestReadAccessGoClassBuilder(t *testing.T) {
	tblDesc, err := sampleTableDesc()
	require.NoError(t, err)

	var conv *ddl.HumanReadableNamingConvention
	chTech := clickhouse.NewTechnologySpecificCodeGenerator()
	conv, err = ddl.NewHumanReadableNamingConvention(":")
	require.NoError(t, err)
	driver := NewGoCodeGeneratorDriver(conv, chTech, true)

	tableRowConfig := common.TableRowConfigMultiAttributesPerRow
	var sourceCode []byte
	namingConvention := gocodegen.NewMultiTablePerPackageGoClassNamer()
	sourceCode, _, err = driver.GenerateGoClasses("example", naming.MustBeValidStylableName("test_table"), tblDesc, tableRowConfig, namingConvention)
	require.NoError(t, err)

	err = golang.WriteAligned("example/readaccess_testtable_ra.out.go", sourceCode)
	require.NoError(t, err)
}

// networkSampleTableDesc mirrors sampleTableDesc but carries a section of network
// (ipv4/ipv6) columns, which resolve to the multi-word arrow class name
// FixedSizeBinary. It is kept separate from sampleTableDesc so the shared golden and
// the compiled example/ roundtrip corpus stay untouched.
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
		sec.TaggedValueColumn("ipv4", ctabb.V) // NetworkTypeAstNode -> arrow FixedSizeBinary
		sec.TaggedValueColumn("ipv6", ctabb.W) // NetworkTypeAstNode -> arrow FixedSizeBinary
	}
	return manip.BuildTableDesc()
}

// TestReadAccessNetworkGolden locks the codegen for network (ipv4/ipv6) columns,
// which resolve to the multi-word arrow class name FixedSizeBinary:
//
//  1. the load constant must be the real arrow.FIXED_SIZE_BINARY, never the
//     non-existent arrow.FIXEDSIZEBINARY a naive strings.ToUpper produced;
//  2. the [N]byte getter must convert array.FixedSizeBinary.Value(i) ([]byte) via
//     ArrowTypeToGoType, not assign []byte to [N]byte; and
//  3. each scalar host address gets a GetAttrValue<Col>Addr net/netip.Addr
//     accessor (AddrFrom4 for ipv4, AddrFrom16 for ipv6).
//
// The string assertions pin the exact emitted forms. None of these is caught by
// the wellFormed check (format.Source resolves no identifiers and type-checks
// nothing), so the test also writes the classes into the compiled example package
// as readaccess_nettable_ra.out.go: a `go build`/`go test` of that package then
// fails to compile if any regression returns (golang.WriteAligned itself
// type-checks before writing). The RA classes reference no dml-generated types,
// so this golden compiles standalone.
func TestReadAccessNetworkGolden(t *testing.T) {
	tblDesc, err := networkSampleTableDesc()
	require.NoError(t, err)

	var conv *ddl.HumanReadableNamingConvention
	chTech := clickhouse.NewTechnologySpecificCodeGenerator()
	conv, err = ddl.NewHumanReadableNamingConvention(":")
	require.NoError(t, err)
	driver := NewGoCodeGeneratorDriver(conv, chTech, true)

	tableRowConfig := common.TableRowConfigMultiAttributesPerRow
	var sourceCode []byte
	namingConvention := gocodegen.NewMultiTablePerPackageGoClassNamer()
	sourceCode, _, err = driver.GenerateGoClasses("example", naming.MustBeValidStylableName("net_table"), tblDesc, tableRowConfig, namingConvention)
	require.NoError(t, err)

	err = golang.WriteAligned("example/readaccess_nettable_ra.out.go", sourceCode)
	require.NoError(t, err)

	src := string(sourceCode)
	require.Contains(t, src, "arrow.FIXED_SIZE_BINARY", "network columns must load via the real arrow.FIXED_SIZE_BINARY constant")
	require.NotContains(t, src, "arrow.FIXEDSIZEBINARY", "arrow has no FIXEDSIZEBINARY constant; class name must be UpperSnakeCase, not strings.ToUpper")
	require.Contains(t, src, "array.NewFixedSizeBinaryData", "sanity: the FixedSizeBinary array constructor should still be emitted")
	// netip.Addr convenience accessors, delegating to the packed-byte getters.
	require.Contains(t, src, `"net/netip"`, "a network host-address column must pull in the net/netip import")
	require.Contains(t, src, "netip.AddrFrom4(inst.GetAttrValueIpv4(entityIdx, attrIdx))", "ipv4 host address must expose a netip.Addr accessor via AddrFrom4")
	require.Contains(t, src, "netip.AddrFrom16(inst.GetAttrValueIpv6(entityIdx, attrIdx))", "ipv6 host address must expose a netip.Addr accessor via AddrFrom16")
}

func TestGoClassBuilderSample(t *testing.T) {
	seed1, seed2 := rand.Uint64(), rand.Uint64()
	t.Logf("randomized test seed: %d %d (rand.NewPCG)", seed1, seed2) // review G-15: log seed for reproducibility
	rnd := rand.New(rand.NewPCG(seed1, seed2))
	manip, err := common.NewTableManipulator()
	require.NoError(t, err)

	var conv *ddl.HumanReadableNamingConvention
	tech := clickhouse.NewTechnologySpecificCodeGenerator()
	conv, err = ddl.NewHumanReadableNamingConvention(":")
	require.NoError(t, err)
	driver := NewGoCodeGeneratorDriver(conv, tech, true)

	tableRowConfig := common.TableRowConfigMultiAttributesPerRow
	var sourceCode []byte
	namingConvention := gocodegen.NewMultiTablePerPackageGoClassNamer()
	acceptCanonicalType := tech.CheckTypeCompatibility
	acceptEncodingAspect := ddl.EncodingAspectFilterFuncFromTechnology(tech, common.ImplementationStatusFull)
	n := 1000
	if testing.Short() {
		n = 10
	}
	for i := 0; i < n; i++ {
		manip.Reset()
		err = common.PopulateManipulator(manip, rnd, acceptCanonicalType, acceptEncodingAspect)
		require.NoError(t, err)
		manip.SetTableName("sample")
		var tblDesc common.TableDesc
		tblDesc, err = manip.BuildTableDesc()
		require.NoError(t, err)
		var wellFormed bool
		sourceCode, wellFormed, err = driver.GenerateGoClasses("example", naming.MustBeValidStylableName("testtable"), tblDesc, tableRowConfig, namingConvention)
		var _ = sourceCode
		unittest.NoError(t, err)
		if !wellFormed && testing.Verbose() {
			_ = os.WriteFile("tmp.out.go", sourceCode, os.ModePerm)
		}
		require.True(t, wellFormed)
	}
}
func TestDmlSample(t *testing.T) {
	tblDesc, err := sampleTableDesc()
	require.NoError(t, err)

	var conv *ddl.HumanReadableNamingConvention
	conv, err = ddl.NewHumanReadableNamingConvention(":")
	require.NoError(t, err)
	chTech := clickhouse.NewTechnologySpecificCodeGenerator()
	driver := dml.NewGoCodeGeneratorDriver(conv, chTech)

	var sourceCode []byte
	tableRowConfig := common.TableRowConfigMultiAttributesPerRow
	namingStyle := gocodegen.NewMultiTablePerPackageGoClassNamer()
	sourceCode, _, err = driver.GenerateGoClasses("example", naming.MustBeValidStylableName("test_table"), tblDesc, tableRowConfig, namingStyle)
	require.NoError(t, err)

	p := "./example/readaccess_testtable_dml.out.go"
	_ = os.Remove(p)
	err = golang.WriteAligned(p, sourceCode)
	require.NoError(t, err)
}

func TestComposeMembershipPackInfo(t *testing.T) {
	manip, err := common.NewTableManipulator()
	require.NoError(t, err)
	{
		sec := manip.TaggedValueSection("secA").AddSectionMembership(common.MembershipSpecHighCardRef)
		sec.TaggedValueColumn("colA", ctabb.S)
	}
	{
		sec := manip.TaggedValueSection("secB").AddSectionMembership(common.MembershipSpecHighCardRef)
		sec.TaggedValueColumn("colB", ctabb.S)
	}
	{
		sec := manip.TaggedValueSection("secC").AddSectionMembership(common.MembershipSpecHighCardRef, common.MembershipSpecLowCardRef)
		sec.TaggedValueColumn("colC", ctabb.S)
	}
	{
		sec := manip.TaggedValueSection("secD").AddSectionMembership(common.MembershipSpecHighCardRef, common.MembershipSpecLowCardRef)
		sec.TaggedValueColumn("colD", ctabb.S)
	}
	{
		sec := manip.TaggedValueSection("secE").AddSectionMembership(common.MembershipSpecLowCardRef)
		sec.TaggedValueColumn("colE", ctabb.S)
	}
	{
		sec := manip.TaggedValueSection("secF").AddSectionMembership(common.MembershipSpecHighCardVerbatim)
		sec.TaggedValueColumn("colF", ctabb.S)
	}
	var tblDesc common.TableDesc
	tblDesc, err = manip.BuildTableDesc()
	require.NoError(t, err)
	tblDesc.DictionaryEntry.Name = "tableXyz"
	require.NoError(t, err)
	namer := gocodegen.NewMultiTablePerPackageGoClassNamer()
	var membershipSpecs []common.MembershipSpecE
	var classNames []string
	var sectionToClassNames []string
	membershipSpecs, classNames, sectionToClassNames, err = ComposeMembershipPackInfo(tblDesc, namer)
	require.NoError(t, err)
	require.EqualValues(t, []common.MembershipSpecE{
		common.MembershipSpecHighCardRef,
		common.MembershipSpecHighCardVerbatim,
		common.MembershipSpecLowCardRef,
		common.MembershipSpecHighCardRef.AddLowCardRefOnly(),
	}, membershipSpecs)
	require.EqualValues(t, []string{"MembershipPackTableXyzShared1", "MembershipPackTableXyzSecF", "MembershipPackTableXyzSecE", "MembershipPackTableXyzShared2"}, classNames)
	require.EqualValues(t, []string{"MembershipPackTableXyzShared1", "MembershipPackTableXyzShared1", "MembershipPackTableXyzShared2", "MembershipPackTableXyzShared2", "MembershipPackTableXyzSecE", "MembershipPackTableXyzSecF"}, sectionToClassNames)
}
