package dml

import (
	"math/rand/v2"
	"os"
	"testing"

	"github.com/stergiotis/boxer/public/observability/eh"
	canonicaltypes2 "github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	encodingaspects2 "github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/gocodegen"
	"github.com/stergiotis/boxer/public/semistructured/leeway/mapping"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
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
	const pathMembershipSpec = common.MembershipSpecMixedLowCardVerbatimHighCardParameters
	var hintsString, hintsFloat64, hintsId, hintsTs encodingaspects2.AspectSet
	hintsString, err = encodingaspects2.EncodeAspects(encodingaspects2.AspectLightGeneralCompression)
	if err != nil {
		err = eh.Errorf("unable to encode hints: %w", err)
		return
	}
	hintsFloat64, err = encodingaspects2.EncodeAspects(encodingaspects2.AspectNone)
	if err != nil {
		err = eh.Errorf("unable to encode hints: %w", err)
		return
	}
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
	manip.AddPlainValueItem(common.PlainItemTypeEntityId, "id", canonicaltypes2.MachineNumericTypeAstNode{
		BaseType:          canonicaltypes2.BaseTypeMachineNumericUnsigned,
		Width:             64,
		ByteOrderModifier: 0,
		ScalarModifier:    0,
	}, hintsId, valueaspects.EmptyAspectSet)
	manip.AddPlainValueItem(common.PlainItemTypeEntityTimestamp, "ts", canonicaltypes2.TemporalTypeAstNode{
		BaseType:       canonicaltypes2.BaseTypeTemporalUtcDatetime,
		Width:          32,
		ScalarModifier: 0,
	}, hintsTs, valueaspects.EmptyAspectSet)
	manip.MergeTaggedValueColumn("bool",
		"value",
		canonicaltypes2.StringAstNode{BaseType: canonicaltypes2.BaseTypeStringBool},
		encodingaspects2.EmptyAspectSet, valueaspects.EmptyAspectSet,
		useaspects.EmptyAspectSet, pathMembershipSpec, "", "")
	manip.MergeTaggedValueColumn("string",
		"value",
		canonicaltypes2.StringAstNode{BaseType: canonicaltypes2.BaseTypeStringUtf8},
		hintsString, valueaspects.EmptyAspectSet,
		useaspects.EmptyAspectSet, pathMembershipSpec, "", "")
	manip.MergeTaggedValueColumn("float64",
		"value",
		canonicaltypes2.MachineNumericTypeAstNode{BaseType: canonicaltypes2.BaseTypeMachineNumericFloat, Width: 64},
		hintsFloat64, valueaspects.EmptyAspectSet,
		useaspects.EmptyAspectSet,
		pathMembershipSpec, "", "")
	manip.MergeTaggedValueColumn("special", "ary1", canonicaltypes2.MachineNumericTypeAstNode{
		BaseType:          canonicaltypes2.BaseTypeMachineNumericUnsigned,
		Width:             32,
		ByteOrderModifier: canonicaltypes2.ByteOrderModifierNone,
		ScalarModifier:    canonicaltypes2.ScalarModifierHomogenousArray,
	}, encodingaspects2.EmptyAspectSet, valueaspects.EmptyAspectSet, useaspects.EmptyAspectSet,
		common.MembershipSpecMixedLowCardRefHighCardParameters, "", "")
	manip.MergeTaggedValueColumn("special", "ary2", canonicaltypes2.MachineNumericTypeAstNode{
		BaseType:          canonicaltypes2.BaseTypeMachineNumericUnsigned,
		Width:             32,
		ByteOrderModifier: canonicaltypes2.ByteOrderModifierNone,
		ScalarModifier:    canonicaltypes2.ScalarModifierHomogenousArray,
	}, encodingaspects2.EmptyAspectSet, valueaspects.EmptyAspectSet, useaspects.EmptyAspectSet,
		common.MembershipSpecMixedLowCardRefHighCardParameters, "", "")
	manip.MergeTaggedValueColumn("special", "spc", canonicaltypes2.StringAstNode{
		BaseType:       canonicaltypes2.BaseTypeStringUtf8,
		WidthModifier:  canonicaltypes2.WidthModifierNone,
		Width:          0,
		ScalarModifier: canonicaltypes2.ScalarModifierNone,
	}, encodingaspects2.EmptyAspectSet, valueaspects.EmptyAspectSet, useaspects.EmptyAspectSet,
		common.MembershipSpecMixedLowCardRefHighCardParameters, "", "")
	return manip.BuildTableDesc()
}

func TestGenerateDml(t *testing.T) {
	TestGenerateDmlSample(t)
	TestGenerateDmlJsonMapping(t)
}
func TestGenerateDmlSample(t *testing.T) {
	tblDesc, err := sampleTableDesc()
	require.NoError(t, err)

	var conv *ddl.HumanReadableNamingConvention
	chTech := clickhouse.NewTechnologySpecificCodeGenerator()
	conv, err = ddl.NewHumanReadableNamingConvention(":")
	require.NoError(t, err)
	driver := NewGoCodeGeneratorDriver(conv, chTech)

	tableRowConfig := common.TableRowConfigMultiAttributesPerRow
	var sourceCode []byte
	namingStyle := gocodegen.NewMultiTablePerPackageGoClassNamer()
	sourceCode, _, err = driver.GenerateGoClasses("example", naming.MustBeValidStylableName("testtable"), tblDesc, tableRowConfig, namingStyle)
	require.NoError(t, err)
	checkCodeInvariants(sourceCode, t)

	err = os.WriteFile("example/dml_testtable.out.go", sourceCode, os.ModePerm)
	require.NoError(t, err)
}
func TestGenerateDmlJsonMapping(t *testing.T) {
	tblDesc, err := mapping.NewJsonMapping()
	require.NoError(t, err)

	var conv *ddl.HumanReadableNamingConvention
	conv, err = ddl.NewHumanReadableNamingConvention(":")
	require.NoError(t, err)
	chTech := clickhouse.NewTechnologySpecificCodeGenerator()
	driver := NewGoCodeGeneratorDriver(conv, chTech)

	var sourceCode []byte
	const tableRowConfig = common.TableRowConfigMultiAttributesPerRow
	namingStyle := gocodegen.NewMultiTablePerPackageGoClassNamer()
	sourceCode, _, err = driver.GenerateGoClasses("example", naming.MustBeValidStylableName("json"), tblDesc, tableRowConfig, namingStyle)
	require.NoError(t, err)
	checkCodeInvariants(sourceCode, t)

	p := "./example/dml_json.out.go"
	_ = os.Remove(p)
	err = os.WriteFile(p, sourceCode, os.ModePerm)
	require.NoError(t, err)
}
func TestGoClassBuilderSample(t *testing.T) {
	rnd := rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))
	manip, err := common.NewTableManipulator()
	require.NoError(t, err)

	var conv *ddl.HumanReadableNamingConvention
	tech := clickhouse.NewTechnologySpecificCodeGenerator()
	conv, err = ddl.NewHumanReadableNamingConvention(":")
	require.NoError(t, err)
	driver := NewGoCodeGeneratorDriver(conv, tech)

	tableRowConfig := common.TableRowConfigMultiAttributesPerRow
	var sourceCode []byte
	namingStyle := gocodegen.NewMultiTablePerPackageGoClassNamer()
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
		var wellFormed bool
		sourceCode, wellFormed, err = driver.GenerateGoClasses("example", naming.MustBeValidStylableName("testtable"), tblDesc, tableRowConfig, namingStyle)
		unittest.NoError(t, err)
		require.True(t, wellFormed)
		checkCodeInvariants(sourceCode, t)
	}
}
