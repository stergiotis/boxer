package test

import (
	"bytes"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	canonicalTypes2 "github.com/stergiotis/boxer/public/semistructured/leeway/canonicalTypes"
	common2 "github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	"github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/mapping"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
	"github.com/stretchr/testify/require"
	"github.com/yassinebenaid/godump"
)

func TestSimpleRoundtrip(t *testing.T) {
	rnd := rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))
	tblOp, err := common2.NewTableOperations()
	require.NoError(t, err)
	var marshaller *common2.TableMarshaller
	marshaller, err = common2.NewTableMarshaller()
	require.NoError(t, err)
	buf := bytes.NewBuffer(make([]byte, 0, 4096))
	var validator *common2.TableValidator
	validator = common2.NewTableValidator()
	var tblDesc1, tblDesc2 common2.TableDesc
	var dto1, dto2 common2.TableDescDto
	for i := 0; i < 100; i++ {
		switch i {
		case 0:
			tblDesc1, err = mapping.NewJsonMapping()
			tblDesc1.DictionaryEntry.Name = ""
			tblDesc1.DictionaryEntry.Comment = ""
			break
		default:
			tblDesc1, err = common2.GenerateSampleTableDesc(rnd, nil, nil)
		}
		require.NoError(t, err)
		err = validator.ValidateTable(&tblDesc1)
		require.NoError(t, err)
		require.NotNil(t, tblDesc1.TaggedValuesSections)

		{ // TableDesc->TableDto->TableDesc->TableDescDto
			dto1.Reset()
			err = tblDesc1.LoadTo(&dto1)
			require.NoError(t, err)
			tblDesc2.Reset()
			err = tblDesc2.LoadFrom(&dto1)
			require.NoError(t, err)
			dto2.Reset()
			err = tblDesc2.LoadTo(&dto2)
			require.NoError(t, err)
			require.EqualValues(t, dto2, dto1)
		}

		{ // TableDto-[marshaller]->cbor-[unmarshaller]->TableDescDto
			tblDesc1.Reset()
			err = tblDesc1.LoadFrom(&dto1)
			require.NoError(t, err)
			tblDesc2.Reset()
			err = tblDesc2.LoadFrom(&dto2)
			require.NoError(t, err)
			require.EqualValues(t, dto2, dto1)
			buf.Reset()
			err = marshaller.EncodeDtoCbor(buf, &dto1)
			require.NoError(t, err)
			dto1.Reset()
			err = marshaller.DecodeDtoCbor(buf, &dto1)
			require.NoError(t, err)
			tblDesc1.Reset()
			err = tblDesc1.LoadFrom(&dto1)
			require.NoError(t, err)

			var r int
			r, err = tblOp.Compare(&tblDesc1, &tblDesc2)
			require.NoError(t, err)
			require.Zero(t, r)
		}
	}
}
func TestNull(t *testing.T) {
	manip, err := common2.NewTableManipulator()
	require.NoError(t, err)
	var tbl common2.TableDesc
	tbl, err = manip.BuildTableDesc()
	require.NoError(t, err)
	require.NotNil(t, tbl.PlainValuesNames)
	require.NotNil(t, tbl.TaggedValuesSections)
}
func TestSmoke(t *testing.T) {
	tech := clickhouse.NewTechnologySpecificCodeGenerator()
	manip, err := common2.NewTableManipulator()
	require.NoError(t, err)
	ctp := canonicalTypes2.NewParser()
	ct1 := ctp.MustParsePrimitiveTypeAst("bh")
	ct2 := ctp.MustParsePrimitiveTypeAst("s")
	ct3 := ctp.MustParsePrimitiveTypeAst("sx1024")

	manip.AddPlainValueItem(common2.PlainItemTypeEntityId, "a", ct1, encodingaspects.EmptyAspectSet, valueaspects.EmptyAspectSet)
	manip.AddPlainValueItem(common2.PlainItemTypeEntityId, "b", ct2, encodingaspects.EmptyAspectSet, valueaspects.EmptyAspectSet)

	manip.MergeTaggedValueColumn("sec0", "u", ct1, encodingaspects.EmptyAspectSet, valueaspects.EmptyAspectSet, useaspects.EmptyAspectSet, common2.MembershipSpecHighCardRef, naming.Key("coSectionGroup1"), naming.Key("streamingGroup1"))
	manip.MergeTaggedValueColumn("sec0", "v", ct2, encodingaspects.EmptyAspectSet, valueaspects.EmptyAspectSet, useaspects.EmptyAspectSet, common2.MembershipSpecHighCardRef, naming.Key("coSectionGroup2"), naming.Key("streamingGroup2"))
	manip.MergeTaggedValueColumn("sec0", "w", ct3, encodingaspects.EmptyAspectSet, valueaspects.EmptyAspectSet, useaspects.EmptyAspectSet, common2.MembershipSpecHighCardRef, naming.Key("coSectionGroup3"), naming.Key("streamingGroup3"))
	manip.MergeTaggedValueColumn("sec1", "u", ct1, encodingaspects.EmptyAspectSet, valueaspects.EmptyAspectSet, useaspects.EmptyAspectSet, common2.MembershipSpecMixedLowCardRefHighCardParameters, naming.Key("coSectionGroup4"), naming.Key("streamingGroup4"))
	manip.SetOpaqueColumnStreamingGroup("opaqueStreamingGroupKey")

	normalizer := common2.NewTableNormalizer(naming.DefaultNamingStyle)
	ir := common2.NewIntermediateTableRepresentation()
	var tblDesc1, tblDesc2 common2.TableDesc
	tblDesc1, err = manip.BuildTableDesc()
	require.NoError(t, err)
	_, _, _, err = normalizer.Normalize(&tblDesc1)
	require.NoError(t, err)
	err = ir.LoadFromTable(&tblDesc1, tech)
	require.NoError(t, err)
	manip.Reset()
	err = manip.LoadFromIntermediates(ir.IterateColumnProps())
	require.NoError(t, err)
	tblDesc2, err = manip.BuildTableDesc()
	require.NoError(t, err)
	_, _, _, err = normalizer.Normalize(&tblDesc2)
	require.NoError(t, err)
	require.EqualValues(t, tblDesc1, tblDesc2)

	var conv *ddl.HumanReadableNamingConvention
	conv, err = ddl.NewHumanReadableNamingConvention(":")
	require.NoError(t, err)
	phys := make([]common2.PhysicalColumnDesc, 0, 128)
	const tableRowConfig = common2.TableRowConfigMultiAttributesPerRow
	for cc, cp := range ir.IterateColumnProps() {
		phys, err = conv.MapIntermediateToPhysicalColumns(cc, *cp, phys, tableRowConfig)
		require.NoError(t, err)
	}
	var tableRowConfig2 common2.TableRowConfigE
	tblDesc2, tableRowConfig2, err = conv.DiscoverTableFromPhysicalColumns(phys)
	require.NoError(t, err)
	tblDesc1.DictionaryEntry = tblDesc2.DictionaryEntry
	require.NoError(t, err)

	normalizer.Normalize(&tblDesc2)
	require.EqualValues(t, tblDesc2, tblDesc1)
	require.Equal(t, tableRowConfig, tableRowConfig2)
}
func TestTableOpsRoundtrip(t *testing.T) {
	rnd := rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))
	tech := clickhouse.NewTechnologySpecificCodeGenerator()
	acceptCanonicalType := tech.CheckTypeCompatibility
	acceptEncodingAspect := ddl.EncodingAspectFilterFuncFromTechnology(tech, common2.ImplementationStatusPartial)
	tblOp, err := common2.NewTableOperations()
	tblNormalizer := common2.NewTableNormalizer(naming.DefaultNamingStyle)
	require.NoError(t, err)
	var validator *common2.TableValidator
	validator = common2.NewTableValidator()
	require.NoError(t, err)
	var tblDesc1, tblDesc2 common2.TableDesc
	for i := 0; i < 100; i++ {
		switch i {
		case 0:
			tblDesc1, err = mapping.NewJsonMapping()
			tblDesc1.DictionaryEntry.Name = ""
			tblDesc1.DictionaryEntry.Comment = ""
			break
		default:
			tblDesc1, err = common2.GenerateSampleTableDesc(rnd, acceptCanonicalType, acceptEncodingAspect)
		}
		require.NoError(t, err)
		err = validator.ValidateTable(&tblDesc1)
		require.NoError(t, err)
		require.NotNil(t, tblDesc1.TaggedValuesSections)
		_, _, _, err = tblNormalizer.Normalize(&tblDesc1)
		require.NoError(t, err)
		tblDesc2, err = tblOp.DeepCopy(&tblDesc1)
		require.NoError(t, err)
		_, _, _, err = tblNormalizer.Normalize(&tblDesc2)
		require.NoError(t, err)

		require.EqualValues(t, tblDesc2, tblDesc1)
		tblNormalizer.Scramble(&tblDesc2, rnd)
		_, _, _, err = tblNormalizer.Normalize(&tblDesc2)
		require.NoError(t, err)

		require.EqualValues(t, tblDesc2, tblDesc1)
	}
}

func TestIntermediateRoundtrip(t *testing.T) {
	rnd := rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))
	tech := clickhouse.NewTechnologySpecificCodeGenerator()
	acceptCanonicalType := tech.CheckTypeCompatibility
	acceptEncodingAspect := ddl.EncodingAspectFilterFuncFromTechnology(tech, common2.ImplementationStatusPartial)
	tblNormalizer := common2.NewTableNormalizer(naming.DefaultNamingStyle)
	var validator *common2.TableValidator
	validator = common2.NewTableValidator()
	ir := common2.NewIntermediateTableRepresentation()
	var tblDesc1, tblDesc2 common2.TableDesc
	manip, err := common2.NewTableManipulator()
	require.NoError(t, err)
	for i := 0; i < 100; i++ {
		switch i {
		case 0:
			tblDesc1, err = mapping.NewJsonMapping()
			tblDesc1.DictionaryEntry.Name = ""
			tblDesc1.DictionaryEntry.Comment = ""
			break
		default:
			tblDesc1, err = common2.GenerateSampleTableDesc(rnd, acceptCanonicalType, acceptEncodingAspect)
		}
		require.NoError(t, err)
		err = validator.ValidateTable(&tblDesc1)
		require.NoError(t, err)
		require.NotNil(t, tblDesc1.TaggedValuesSections)
		_, _, _, err = tblNormalizer.Normalize(&tblDesc1)
		require.NoError(t, err)

		ir.Reset()
		err = ir.LoadFromTable(&tblDesc1, tech)
		require.NoError(t, err)
		manip.Reset()
		err = manip.LoadFromIntermediates(ir.IterateColumnProps())
		require.NoError(t, err)

		tblDesc2, err = manip.BuildTableDesc()
		require.NoError(t, err)
		_, _, _, err = tblNormalizer.Normalize(&tblDesc2)
		require.NoError(t, err)

		require.EqualValues(t, tblDesc2, tblDesc1)
	}
}
func TestNamingConventionRoundtrip(t *testing.T) {
	rnd := rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))
	tech := clickhouse.NewTechnologySpecificCodeGenerator()
	acceptCanonicalType := tech.CheckTypeCompatibility
	acceptEncodingAspect := ddl.EncodingAspectFilterFuncFromTechnology(tech, common2.ImplementationStatusPartial)
	tblOp, err := common2.NewTableOperations()
	tblNormalizer := common2.NewTableNormalizer(naming.DefaultNamingStyle)
	require.NoError(t, err)
	var validator *common2.TableValidator
	validator = common2.NewTableValidator()
	var conv *ddl.HumanReadableNamingConvention
	conv, err = ddl.NewHumanReadableNamingConvention(":")
	require.NoError(t, err)
	ir := common2.NewIntermediateTableRepresentation()
	var tblDesc1, tblDesc2 common2.TableDesc
	const tableRowConfig = common2.TableRowConfigMultiAttributesPerRow
	d := godump.Dumper{
		Indentation:             "",
		ShowPrimitiveNamedTypes: false,
		HidePrivateFields:       true,
		Theme:                   godump.Theme{},
	}
	const extraChecks = false
	phys := make([]common2.PhysicalColumnDesc, 0, 128)
	ss := make([]string, 0, 128)
	names := make([]string, 0, len(phys))
	m := make(map[string]string, 100)
	for i := 0; i < 100; i++ {
		switch i {
		case 0:
			tblDesc1, err = mapping.NewJsonMapping()
			tblDesc1.DictionaryEntry.Name = ""
			tblDesc1.DictionaryEntry.Comment = ""
			break
		default:
			tblDesc1, err = common2.GenerateSampleTableDesc(rnd, acceptCanonicalType, acceptEncodingAspect)
		}
		require.NoError(t, err)
		err = validator.ValidateTable(&tblDesc1)
		require.NoError(t, err)
		require.NotNil(t, tblDesc1.TaggedValuesSections)
		_, _, _, err = tblNormalizer.Normalize(&tblDesc1)
		require.NoError(t, err)
		tblDesc2, err = tblOp.DeepCopy(&tblDesc1)
		require.NoError(t, err)
		_, _, _, err = tblNormalizer.Normalize(&tblDesc2)
		require.NoError(t, err)

		require.EqualValues(t, tblDesc2, tblDesc1)

		phys = phys[:0]
		names = names[:0]

		if extraChecks {
			ss = ss[:0]
			clear(m)
		}

		ir.Reset()
		err = ir.LoadFromTable(&tblDesc1, tech)
		require.NoError(t, err)
		for cc, cp := range ir.IterateColumnProps() {
			phys, err = conv.MapIntermediateToPhysicalColumns(cc, *cp, phys, tableRowConfig)
			require.NoError(t, err)
			if extraChecks {
				for j, name := range cp.Names {
					tmp := d.Sprint(struct {
						CC    string
						Name  string
						Role  string
						Hints string
						Type  string
					}{
						CC:    d.Sprint(cc),
						Name:  name.String(),
						Role:  cp.Roles[j].String(),
						Hints: cp.EncodingHints[j].String(),
						Type:  cp.CanonicalType[j].String(),
					})
					if slices.Index(ss, tmp) >= 0 {
						require.Fail(t, tmp)
					}
					ss = append(ss, tmp)
				}
			}
		}
		if extraChecks {
			require.EqualValues(t, len(phys), len(ss))
		}
		for j, p := range phys {
			name := strings.Join(p.NameComponents, "")
			names = append(names, name)
			if extraChecks {
				ps := ss[j]
				if m[name] != "" {
					require.Fail(t, "name=%s,prevName=%s", name, m[name])
				}
				m[name] = ps
			}
		}
		rnd.Shuffle(len(names), func(i, j int) {
			names[j], names[i] = names[i], names[j]
		})
		var tableRowConfig2 common2.TableRowConfigE
		tblDesc1, tableRowConfig2, err = conv.DiscoverTableFromColumnNames(names)
		require.NoError(t, err)
		tblDesc1.DictionaryEntry = tblDesc2.DictionaryEntry
		_, _, _, err = tblNormalizer.Normalize(&tblDesc1)
		require.NoError(t, err)
		require.EqualValues(t, tblDesc2, tblDesc1)
		require.EqualValues(t, tableRowConfig, tableRowConfig2)
	}
}
