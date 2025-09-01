package dml

import (
	"math/rand/v2"
	"os"
	"testing"

	"github.com/stergiotis/boxer/public/observability/eh"
	canonicalTypes2 "github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	encodingaspects2 "github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
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
	manip.AddPlainValueItem(common.PlainItemTypeEntityId, "id", canonicalTypes2.MachineNumericTypeAstNode{
		BaseType:          canonicalTypes2.BaseTypeMachineNumericUnsigned,
		Width:             64,
		ByteOrderModifier: 0,
		ScalarModifier:    0,
	}, hintsId, valueaspects.EmptyAspectSet)
	manip.AddPlainValueItem(common.PlainItemTypeEntityTimestamp, "ts", canonicalTypes2.TemporalTypeAstNode{
		BaseType:       canonicalTypes2.BaseTypeTemporalUtcDatetime,
		Width:          32,
		ScalarModifier: 0,
	}, hintsTs, valueaspects.EmptyAspectSet)
	manip.MergeTaggedValueColumn("bool",
		"value",
		canonicalTypes2.StringAstNode{BaseType: canonicalTypes2.BaseTypeStringBool},
		encodingaspects2.EmptyAspectSet, valueaspects.EmptyAspectSet,
		useaspects.EmptyAspectSet, pathMembershipSpec, "", "")
	manip.MergeTaggedValueColumn("string",
		"value",
		canonicalTypes2.StringAstNode{BaseType: canonicalTypes2.BaseTypeStringUtf8},
		hintsString, valueaspects.EmptyAspectSet,
		useaspects.EmptyAspectSet, pathMembershipSpec, "", "")
	manip.MergeTaggedValueColumn("float64",
		"value",
		canonicalTypes2.MachineNumericTypeAstNode{BaseType: canonicalTypes2.BaseTypeMachineNumericFloat, Width: 64},
		hintsFloat64, valueaspects.EmptyAspectSet,
		useaspects.EmptyAspectSet,
		pathMembershipSpec, "", "")
	manip.MergeTaggedValueColumn("special", "ary1", canonicalTypes2.MachineNumericTypeAstNode{
		BaseType:          canonicalTypes2.BaseTypeMachineNumericUnsigned,
		Width:             32,
		ByteOrderModifier: canonicalTypes2.ByteOrderModifierNone,
		ScalarModifier:    canonicalTypes2.ScalarModifierHomogenousArray,
	}, encodingaspects2.EmptyAspectSet, valueaspects.EmptyAspectSet, useaspects.EmptyAspectSet,
		common.MembershipSpecMixedLowCardRefHighCardParameters, "", "")
	manip.MergeTaggedValueColumn("special", "ary2", canonicalTypes2.MachineNumericTypeAstNode{
		BaseType:          canonicalTypes2.BaseTypeMachineNumericUnsigned,
		Width:             32,
		ByteOrderModifier: canonicalTypes2.ByteOrderModifierNone,
		ScalarModifier:    canonicalTypes2.ScalarModifierHomogenousArray,
	}, encodingaspects2.EmptyAspectSet, valueaspects.EmptyAspectSet, useaspects.EmptyAspectSet,
		common.MembershipSpecMixedLowCardRefHighCardParameters, "", "")
	manip.MergeTaggedValueColumn("special", "spc", canonicalTypes2.StringAstNode{
		BaseType:       canonicalTypes2.BaseTypeStringUtf8,
		WidthModifier:  canonicalTypes2.WidthModifierNone,
		Width:          0,
		ScalarModifier: canonicalTypes2.ScalarModifierNone,
	}, encodingaspects2.EmptyAspectSet, valueaspects.EmptyAspectSet, useaspects.EmptyAspectSet,
		common.MembershipSpecMixedLowCardRefHighCardParameters, "", "")
	return manip.BuildTableDesc()
}

func TestGoClassBuilder(t *testing.T) {
	tblDesc, err := sampleTableDesc()
	require.NoError(t, err)

	var conv *ddl.HumanReadableNamingConvention
	chTech := clickhouse.NewTechnologySpecificCodeGenerator()
	conv, err = ddl.NewHumanReadableNamingConvention(":")
	require.NoError(t, err)
	driver := NewGoCodeGeneratorDriver(conv, chTech)

	tableRowConfig := common.TableRowConfigMultiAttributesPerRow
	var sourceCode []byte
	namingStyle := NewMultiTablePerPackageGoClassNamer()
	sourceCode, _, err = driver.GenerateGoClasses("example", naming.MustBeValidStylableName("testtable"), tblDesc, tableRowConfig, namingStyle)
	require.NoError(t, err)
	checkCodeInvariants(sourceCode, t)

	err = os.WriteFile("example/dml_testtable.gen.go", sourceCode, os.ModePerm)
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
	namingStyle := NewMultiTablePerPackageGoClassNamer()
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
