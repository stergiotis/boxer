package readaccess

import (
	"fmt"
	"slices"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/containers"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/observability/vcs"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/codegen"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	arrow2 "github.com/stergiotis/boxer/public/semistructured/leeway/ddl/arrow"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/golang"
	"github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/gocodegen"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
)

var CodeGeneratorName = "Leeway readaccess (" + vcs.ModuleInfo() + ")"

func NewGoClassBuilder(fatRuntime bool) *GoClassBuilder {
	return &GoClassBuilder{
		builder:    nil,
		tech:       golang.NewTechnologySpecificCodeGenerator(),
		fatRuntime: fatRuntime,
	}
}

func (inst *GoClassBuilder) SetCodeBuilder(s *strings.Builder) {
	inst.builder = s
	inst.tech.SetCodeBuilder(s)
}

func (inst *GoClassBuilder) GetCode() (code string, err error) {
	b := inst.builder
	if b == nil {
		err = common.ErrNoCodebuilder
		return
	}
	code = b.String()
	return
}

func (inst *GoClassBuilder) ResetCodeBuilder() {
	b := inst.builder
	if b != nil {
		b.Reset()
	}
}
func (inst *GoClassBuilder) PrepareCodeComposition() {
}
func (inst *GoClassBuilder) ComposeNamingConventionDependentCode(tableName naming.StylableName, ir *common.IntermediateTableRepresentation, namingConvention common.NamingConventionI, tableRowConfig common.TableRowConfigE, clsNamer gocodegen.GoClassNamerI) (err error) {
	return
}

func (inst *GoClassBuilder) ComposeAttributeClassAndFactoryCode(clsNamer gocodegen.GoClassNamerI, tableName naming.StylableName, sectionName naming.StylableName, sectionIdx int, totalSections int, sectionIRH *common.IntermediatePairHolder, tableRowConfig common.TableRowConfigE) (err error) {
	return
}

func (inst *GoClassBuilder) ComposeSectionClassAndFactoryCode(clsNamer gocodegen.GoClassNamerI, tableName naming.StylableName, sectionName naming.StylableName, sectionIdx int, totalSections int, sectionIRH *common.IntermediatePairHolder, tableRowConfig common.TableRowConfigE) (err error) {
	return
}

func (inst *GoClassBuilder) ComposeAttributeCode(clsNamer gocodegen.GoClassNamerI, tableName naming.StylableName, sectionName naming.StylableName, sectionIdx int, totalSections int, sectionIRH *common.IntermediatePairHolder, tableRowConfig common.TableRowConfigE) (err error) {
	return
}

func (inst *GoClassBuilder) ComposeSectionCode(clsNamer gocodegen.GoClassNamerI, tableName naming.StylableName, sectionName naming.StylableName, sectionIdx int, totalSections int, sectionIRH *common.IntermediatePairHolder, tableRowConfig common.TableRowConfigE) (err error) {
	return
}
func tableDescFromIr(ir *common.IntermediateTableRepresentation, tableName naming.StylableName) (tblDesc common.TableDesc, err error) {
	var manip *common.TableManipulator
	manip, err = common.NewTableManipulator()
	if err != nil {
		err = eh.Errorf("unable to create table manipulator: %w", err)
		return
	}
	err = manip.LoadFromIntermediates(ir.IterateColumnProps())
	if err != nil {
		err = eh.Errorf("unable to load intermediate table representation: %w", err)
		return
	}
	tblDesc, err = manip.BuildTableDesc()
	if err != nil {
		err = eh.Errorf("unable to build table desc: %w", err)
		return
	}
	tblDesc.DictionaryEntry.Name = tableName
	return
}

func ComposeMembershipPackInfo(tblDesc common.TableDesc, namer gocodegen.GoClassNamerReadAccessI) (membershipSpecs []common.MembershipSpecE, classNames []string, sectionToClassName []string, err error) {
	kv := containers.NewBinarySearchGrowingKV[common.MembershipSpecE, int](len(tblDesc.TaggedValuesSections), func(a common.MembershipSpecE, b common.MembershipSpecE) int {
		if a < b {
			return -1
		} else if a > b {
			return 1
		}
		return 0
	})
	for i, s := range tblDesc.TaggedValuesSections {
		if s.MembershipSpec != 0 {
			kv.MergeValue(s.MembershipSpec, i, func(old int, new int) int {
				if old < 0 {
					return old - 1
				} else {
					return -1
				}
			})
		}
	}
	totalShared := 0
	for v := range kv.IterateValues() {
		if v < 0 {
			totalShared++
		}
	}
	sharedIndex := 0
	membershipSpecs = make([]common.MembershipSpecE, 0, kv.Len())
	classNames = make([]string, 0, kv.Len())
	sectionToClassName = make([]string, len(tblDesc.TaggedValuesSections))
	for spec, n := range kv.Iterate() {
		var clsName string
		if n < 0 {
			sharedIndex++
			clsName, err = namer.ComposeSharedMembershipPackClassName(tblDesc.DictionaryEntry.Name, spec, sharedIndex, totalShared)
			if err != nil {
				err = eb.Build().Stringer("tableName", tblDesc.DictionaryEntry.Name).Stringer("spec", spec).Errorf("unable to compose shared membership pack class: %w", err)
				return
			}
			for i, s := range tblDesc.TaggedValuesSections {
				if s.MembershipSpec == spec {
					sectionToClassName[i] = clsName
				}
			}
		} else {
			var sectionName naming.StylableName
			var sectionIndex int
			for i, s := range tblDesc.TaggedValuesSections {
				if s.MembershipSpec == spec {
					sectionName = s.Name
					sectionIndex = i
					break
				}
			}
			clsName, err = namer.ComposeSectionMembershipPackClassName(tblDesc.DictionaryEntry.Name, sectionName)
			if err != nil {
				err = eb.Build().Stringer("tableName", tblDesc.DictionaryEntry.Name).Stringer("spec", spec).Errorf("unable to compose shared membership pack class: %w", err)
				return
			}
			sectionToClassName[sectionIndex] = clsName
		}
		membershipSpecs = append(membershipSpecs, spec)
		classNames = append(classNames, clsName)
	}
	return
}
func (inst *GoClassBuilder) composeResolveColumnIndexCodeDynamic(conv common.NamingConventionFwdI, cc common.IntermediateColumnContext, cp common.IntermediateColumnProps, i int, tableRowConfig common.TableRowConfigE, physicalColumnNamesSliceExpr string, physicalColumnNameExpr string, indexVariableName string) (code string, err error) {
	phy := make([]common.PhysicalColumnDesc, 0, 1)
	phy, err = conv.MapIntermediateToPhysicalColumns(cc, cp.Slice(i, i+1), phy, tableRowConfig)
	if err != nil {
		err = eh.Errorf("unable to map intermediate to physical colum: %w", err)
		return
	}
	if len(phy) != 1 {
		err = eb.Build().Int("len", len(phy)).Errorf("convention returned not exactly one physical column: %w", err)
		return
	}
	code = fmt.Sprintf(`	%s, err = runtime.LookupPhysicalColumnIndex(%s,%s)
	if err != nil {
		return
	}`,
		indexVariableName,
		physicalColumnNamesSliceExpr,
		physicalColumnNameExpr)
	return
}
func (inst *GoClassBuilder) composeResolveColumnIndexCodeStatic(cc common.IntermediateColumnContext, cp common.IntermediateColumnProps, i int, tableRowConfig common.TableRowConfigE) (index int, err error) {
	if i < 0 || i >= cp.Length() {
		err = eh.Errorf("index is out of range")
		return
	}
	index = int(cc.IndexOffset) + i
	return
}
func (inst *GoClassBuilder) getColumnIndexBySectionAndRole(ir *common.IntermediateTableRepresentation, sectionName naming.StylableName, role common.ColumnRoleE) (idx int, err error) {
	for cc, cp := range ir.IterateColumnProps() {
		if cc.PlainItemType == common.PlainItemTypeNone &&
			cc.SectionName.Compare(sectionName) == 0 {
			for i, r := range cp.Roles {
				if r == role {
					idx = int(cc.IndexOffset) + i
					return
				}
			}
		}
	}
	err = eb.Build().Stringer("sectionName", sectionName).Stringer("role", role).Errorf("unable to find columen")
	return
}
func getElementGoTypeName(ct canonicaltypes.PrimitiveAstNodeI, hints encodingaspects.AspectSet) (typeName string, scalarModifier canonicaltypes.ScalarModifierE, err error) {
	scalarModifier, err = common.ExtractScalarModifier(ct)
	if err != nil {
		return
	}
	typeName, _, _, err = codegen.GenerateGoCode(ct, hints)
	if err != nil {
		err = eh.Errorf("unable to get go type for canonical type: %w", err)
		return
	}
	switch scalarModifier {
	case canonicaltypes.ScalarModifierNone:
		break
	case canonicaltypes.ScalarModifierSet:
		typeName = strings.TrimPrefix(typeName, "[]") // FIXME encoding hints vs demoted canonical type
		break
	case canonicaltypes.ScalarModifierHomogenousArray:
		typeName = strings.TrimPrefix(typeName, "[]") // FIXME encoding hints vs demoted canonical type
		break
	default:
		err = eb.Build().Stringer("scalarModifier", scalarModifier).Stringer("ct", ct).Errorf("unhandled scalar modifier")
		return
	}
	return
}

func (inst *GoClassBuilder) composeMembershipPacks(ir *common.IntermediateTableRepresentation, tblDesc common.TableDesc, clsNamer gocodegen.GoClassNamerReadAccessI, tableRowConfig common.TableRowConfigE, useDictEncoding bool) (err error) {
	b := inst.builder
	gocodegen.EmitGeneratingCodeLocation(b)

	var membershipSpecs []common.MembershipSpecE
	var classNames, sectionToClassName []string
	membershipSpecs, classNames, sectionToClassName, err = ComposeMembershipPackInfo(tblDesc, clsNamer)
	if err != nil {
		err = eh.Errorf("unable to compose membership pack info: %w", err)
		return
	}
	arrowTech := arrow2.NewTechnologySpecificCodeGenerator()
	{ // struct
		for i, spec := range membershipSpecs {
			clsName := classNames[i]
			_, err = fmt.Fprintf(b, `type %s struct {
`, clsName)
			if err != nil {
				return
			}
			for s := range spec.Iterate() {
				var ct1, ct2 canonicaltypes.PrimitiveAstNodeI
				var hints1, hints2 encodingaspects.AspectSet
				var role1, role2 common.ColumnRoleE
				ct1, hints1, role1, ct2, hints2, role2, _, err = arrowTech.ResolveMembership(s)
				if err != nil {
					err = eh.Errorf("unable to get membership column canonical type: %w", err)
					return
				}
				var typeName1 string
				typeName1, _, err = gocodegen.CanonicalTypeToArrowBaseClassName(ct1, hints1, useDictEncoding)
				if err != nil {
					err = eh.Errorf("unable to get arrow class name for canonical type: %w", err)
					return
				}
				name1 := naming.MustBeValidStylableName(role1.LongString()).Convert(naming.UpperCamelCase).String()
				columnIndexFieldName1 := clsNamer.ComposeColumnIndexFieldName(name1)
				const tmpl = `	%s *array.List
	%s *array.%s
	%s *runtime.RandomAccessTwoLevelLookupAccel[runtime.Membership%sIdx,runtime.AttributeIdx,int,int64]
	%s uint32
	%sAccel uint32
`
				_, err = fmt.Fprintf(b, tmpl,
					clsNamer.ComposeValueField(name1),
					clsNamer.ComposeValueFieldElementAccessor(name1),
					typeName1,
					clsNamer.ComposeAccelFieldName(name1),
					name1,
					columnIndexFieldName1,
					columnIndexFieldName1,
				)
				if err != nil {
					return
				}
				if s.ContainsMixed() {
					var typeName2 string
					typeName2, _, err = gocodegen.CanonicalTypeToArrowBaseClassName(ct2, hints2, useDictEncoding)
					if err != nil {
						err = eh.Errorf("unable to get arrow class name for canonical type: %w", err)
						return
					}
					name2 := naming.MustBeValidStylableName(role2.LongString()).Convert(naming.UpperCamelCase).String()
					columnIndexFieldName2 := clsNamer.ComposeColumnIndexFieldName(name2)
					_, err = fmt.Fprintf(b, tmpl,
						clsNamer.ComposeValueField(name2),
						clsNamer.ComposeValueFieldElementAccessor(name2),
						typeName2,
						clsNamer.ComposeAccelFieldName(name2),
						name2,
						columnIndexFieldName2,
						columnIndexFieldName2,
					)
					if err != nil {
						return
					}
				}
			}
			_, err = fmt.Fprint(b, `
}
`)
			if err != nil {
				return
			}
		}
	}

	{ // New
		colIdxGen := NewColumnIndexCodeGenerator()
		commonEmitted := containers.NewHashSet[string](32)
		for i, sec := range tblDesc.TaggedValuesSections {
			clsName := sectionToClassName[i]
			idx := slices.Index(classNames, clsName)
			if idx < 0 {
				continue
			}
			spec := membershipSpecs[idx]
			colIdxGen.Reset()
			_, err = fmt.Fprintf(b, `func New%s%s() (inst *%s) {
	inst = &%s{}
`,
				clsName,
				sec.Name.Convert(naming.UpperCamelCase),
				clsName,
				clsName)
			if err != nil {
				return
			}
			for s := range spec.Iterate() {
				var role1, role2 common.ColumnRoleE
				_, _, role1, _, _, role2, _, err = arrowTech.ResolveMembership(s)
				if err != nil {
					err = eh.Errorf("unable to get membership column canonical type: %w", err)
					return
				}
				name1 := naming.MustBeValidStylableName(role1.LongString()).Convert(naming.UpperCamelCase).String()
				columnIndexFieldName1 := clsNamer.ComposeColumnIndexFieldName(name1)
				var idx1, idx1Accel int
				idx1, err = inst.getColumnIndexBySectionAndRole(ir, sec.Name, role1)
				if err != nil {
					err = eh.Errorf("unable to find column: %w", err)
					return
				}
				var cardRole1 common.ColumnRoleE
				cardRole1, err = common.GetCardinalityRoleByMembershipRole(role1)
				if err != nil {
					err = eh.Errorf("unable to resolve cardinality role: %w", err)
					return
				}
				idx1Accel, err = inst.getColumnIndexBySectionAndRole(ir, sec.Name, cardRole1)
				if err != nil {
					err = eh.Errorf("unable to find column: %w", err)
					return
				}
				colIdxGen.AddField(columnIndexFieldName1, uint32(idx1))
				colIdxGen.AddField(columnIndexFieldName1+"Accel", uint32(idx1Accel))
				_, err = fmt.Fprintf(b, "\tinst.%s = runtime.NewRandomAccessTwoLevelLookupAccel[runtime.Membership%sIdx,runtime.AttributeIdx,int,int64](runtime.AccelEstimatedInitialLength)\n",
					clsNamer.ComposeAccelFieldName(name1),
					name1,
				)
				if err != nil {
					return
				}

				if s.ContainsMixed() {
					var idx2, idx2Accel int
					name2 := naming.MustBeValidStylableName(role2.LongString()).Convert(naming.UpperCamelCase).String()
					idx2, err = inst.getColumnIndexBySectionAndRole(ir, sec.Name, role2)
					if err != nil {
						err = eh.Errorf("unable to find column: %w", err)
						return
					}
					var cardRole2 common.ColumnRoleE
					cardRole2, err = common.GetCardinalityRoleByMembershipRole(role2)
					if err != nil {
						err = eh.Errorf("unable to resolve cardinality role: %w", err)
						return
					}
					idx2Accel, err = inst.getColumnIndexBySectionAndRole(ir, sec.Name, cardRole2)
					if err != nil {
						err = eh.Errorf("unable to find column: %w", err)
						return
					}
					columnIndexFieldName2 := clsNamer.ComposeColumnIndexFieldName(name2)
					colIdxGen.AddField(columnIndexFieldName2, uint32(idx2))
					colIdxGen.AddField(columnIndexFieldName2+"Accel", uint32(idx2Accel))

					_, err = fmt.Fprintf(b, "\tinst.%s = runtime.NewRandomAccessTwoLevelLookupAccel[runtime.Membership%sIdx,runtime.AttributeIdx,int,int64](runtime.AccelEstimatedInitialLength)\n",
						clsNamer.ComposeAccelFieldName(name2),
						name2,
					)
					if err != nil {
						return
					}
				}
			}

			err = colIdxGen.GenerateInstInit(b)
			_, err = fmt.Fprint(b, "\treturn\n}\n\n")
			if err != nil {
				return
			}
			if !commonEmitted.AddEx(clsName) {
				err = colIdxGen.GenerateCommon(b, clsName)
				if err != nil {
					return
				}
			}
		}
	}

	{ // .Release()
		for i, spec := range membershipSpecs {
			clsName := classNames[i]
			_, err = fmt.Fprintf(b, `func (inst *%s) Release() {
`, clsName)
			if err != nil {
				return
			}
			for s := range spec.Iterate() {
				var role1, role2 common.ColumnRoleE
				_, _, role1, _, _, role2, _, err = arrowTech.ResolveMembership(s)
				if err != nil {
					err = eh.Errorf("unable to get membership column canonical type: %w", err)
					return
				}
				name1 := naming.MustBeValidStylableName(role1.LongString()).Convert(naming.UpperCamelCase).String()
				const tmpl = "\truntime.ReleaseIfNotNil(inst.%s)\n\truntime.ReleaseIfNotNil(inst.%s)\n"
				_, err = fmt.Fprintf(b, tmpl,
					clsNamer.ComposeValueField(name1),
					clsNamer.ComposeValueFieldElementAccessor(name1),
				)
				if err != nil {
					return
				}
				if s.ContainsMixed() {
					name2 := naming.MustBeValidStylableName(role2.LongString()).Convert(naming.UpperCamelCase).String()
					_, err = fmt.Fprintf(b, tmpl,
						clsNamer.ComposeValueField(name2),
						clsNamer.ComposeValueFieldElementAccessor(name2),
					)
					if err != nil {
						return
					}
				}
			}
			_, err = fmt.Fprint(b, "}\n\n")
			if err != nil {
				return
			}
		}
	}

	{ // .Reset()
		for i, spec := range membershipSpecs {
			clsName := classNames[i]
			_, err = fmt.Fprintf(b, `func (inst *%s) Reset() {
`, clsName)
			if err != nil {
				return
			}
			for s := range spec.Iterate() {
				var role1, role2 common.ColumnRoleE
				_, _, role1, _, _, role2, _, err = arrowTech.ResolveMembership(s)
				if err != nil {
					err = eh.Errorf("unable to get membership column canonical type: %w", err)
					return
				}
				name1 := naming.MustBeValidStylableName(role1.LongString()).Convert(naming.UpperCamelCase).String()
				_, err = fmt.Fprintf(b, "\tinst.%s = nil\n\tinst.%s = nil\n",
					clsNamer.ComposeValueField(name1),
					clsNamer.ComposeValueFieldElementAccessor(name1),
				)
				if err != nil {
					return
				}
				if s.ContainsMixed() {
					name2 := naming.MustBeValidStylableName(role2.LongString()).Convert(naming.UpperCamelCase).String()
					_, err = fmt.Fprintf(b, "\tinst.%s = nil\n\tinst.%s = nil\n",
						clsNamer.ComposeValueField(name2),
						clsNamer.ComposeValueFieldElementAccessor(name2),
					)
					if err != nil {
						return
					}
				}
			}
			_, err = fmt.Fprint(b, "}\n\n")
			if err != nil {
				return
			}
		}
	}

	{ // .LoadFromRecord(rec arrow.Record) (err error)
		for i, spec := range membershipSpecs {
			clsName := classNames[i]
			_, err = fmt.Fprintf(b, `func (inst *%s) LoadFromRecord(rec arrow.Record) (err error) {
`, clsName)
			if err != nil {
				return
			}
			for s := range spec.Iterate() {
				var ct1, ct2 canonicaltypes.PrimitiveAstNodeI
				var hints1, hints2 encodingaspects.AspectSet
				var role1, role2 common.ColumnRoleE
				ct1, hints1, role1, ct2, hints2, role2, _, err = arrowTech.ResolveMembership(s)
				if err != nil {
					err = eh.Errorf("unable to get membership column canonical type: %w", err)
					return
				}
				var typeName1 string
				typeName1, _, err = gocodegen.CanonicalTypeToArrowBaseClassName(ct1, hints1, useDictEncoding)
				if err != nil {
					err = eh.Errorf("unable to get arrow class name for canonical type: %w", err)
					return
				}
				name1 := naming.MustBeValidStylableName(role1.LongString()).Convert(naming.UpperCamelCase).String()
				const tmpl = `	err = runtime.LoadNonScalarValueFieldFromRecord(inst.%s,arrow.%s,rec,&inst.%s,&inst.%s,array.New%sData)
	if err != nil {
		return
	}
	err = runtime.LoadAccelFieldFromRecord(inst.%sAccel,rec,inst.%s)
	if err != nil {
		return
	}
`
				columnIndexFieldName1 := clsNamer.ComposeColumnIndexFieldName(name1)
				_, err = fmt.Fprintf(b, tmpl,
					columnIndexFieldName1,
					naming.MustBeValidStylableName(typeName1).Convert(naming.UpperSnakeCase),
					clsNamer.ComposeValueField(name1),
					clsNamer.ComposeValueFieldElementAccessor(name1),
					typeName1,

					columnIndexFieldName1,
					clsNamer.ComposeAccelFieldName(name1),
				)
				if err != nil {
					return
				}
				if s.ContainsMixed() {
					name2 := naming.MustBeValidStylableName(role2.LongString()).Convert(naming.UpperCamelCase).String()
					var typeName2 string
					typeName2, _, err = gocodegen.CanonicalTypeToArrowBaseClassName(ct2, hints2, useDictEncoding)
					if err != nil {
						err = eh.Errorf("unable to get arrow class name for canonical type: %w", err)
						return
					}
					columnIndexFieldName2 := clsNamer.ComposeColumnIndexFieldName(name2)
					_, err = fmt.Fprintf(b, tmpl,
						columnIndexFieldName2,
						naming.MustBeValidStylableName(typeName2).Convert(naming.UpperSnakeCase),
						clsNamer.ComposeValueField(name2),
						clsNamer.ComposeValueFieldElementAccessor(name2),
						typeName2,

						columnIndexFieldName1,
						clsNamer.ComposeAccelFieldName(name2),
					)
					if err != nil {
						return
					}
				}
			}
			_, err = fmt.Fprint(b, "\treturn\n}\n\n")
			if err != nil {
				return
			}
		}
	}
	{ // .Len() (nEntities int)
		for i, spec := range membershipSpecs {
			clsName := classNames[i]
			_, err = fmt.Fprintf(b, `func (inst *%s) Len() (nEntities int) {
`, clsName)
			if err != nil {
				return
			}
			for s := range spec.Iterate() {
				var role1, _ common.ColumnRoleE
				_, _, role1, _, _, _, _, err = arrowTech.ResolveMembership(s)
				if err != nil {
					err = eh.Errorf("unable to get membership column canonical type: %w", err)
					return
				}
				name1 := naming.MustBeValidStylableName(role1.LongString()).Convert(naming.UpperCamelCase).String()
				f := clsNamer.ComposeValueField(name1)
				_, err = fmt.Fprintf(b, "\tif inst.%s != nil {\n\t\tnEntities = inst.%s.Len()\n}\n",
					f,
					f,
				)
				if err != nil {
					return
				}
				break
			}
			_, err = fmt.Fprint(b, "\treturn\n}\n\n")
			if err != nil {
				return
			}
		}
	}
	{ // .GetNumberOfMemberItemsXXX() (nItems int64)
		for i, spec := range membershipSpecs {
			clsName := classNames[i]
			for s := range spec.Iterate() {
				var role1 common.ColumnRoleE
				_, _, role1, _, _, _, _, err = arrowTech.ResolveMembership(s)
				if err != nil {
					err = eh.Errorf("unable to get membership column canonical type: %w", err)
					return
				}
				name1 := naming.MustBeValidStylableName(role1.LongString()).Convert(naming.UpperCamelCase).String()
				f1 := clsNamer.ComposeValueField(name1)
				const tmpl = `func (inst *%s) GetNumberOfMemberItem%s(entityIdx runtime.EntityIdx) (nItems int64) {
	if inst.%s != nil {
		b, e := inst.%s.ValueOffsets(int(entityIdx))
		nItems = e - b
	}
	return
}
`
				_, err = fmt.Fprintf(b, tmpl,
					clsName,
					name1,
					f1,
					f1,
				)
				if err != nil {
					return
				}
			}
		}
	}
	{ // .GetMembValueXXX(entityIdx runtime.EntityIdx,membIdx runtime.MemberIdx) (iter.Seq[XXX])
		for i, spec := range membershipSpecs {
			clsName := classNames[i]
			for s := range spec.Iterate() {
				var role1, role2 common.ColumnRoleE
				var ct1, ct2 canonicaltypes.PrimitiveAstNodeI
				var hint1, hint2 encodingaspects.AspectSet
				ct1, hint1, role1, ct2, hint2, role2, _, err = arrowTech.ResolveMembership(s)
				if err != nil {
					err = eh.Errorf("unable to get membership column canonical type: %w", err)
					return
				}

				var typeName1 string
				typeName1, _, err = getElementGoTypeName(ct1, hint1)
				if err != nil {
					err = eh.Errorf("unable to get element go type name: %w", err)
					return
				}
				name1 := naming.MustBeValidStylableName(role1.LongString()).Convert(naming.UpperCamelCase).String()
				const tmpl = `func (inst *%s) GetMembValue%s(entityIdx runtime.EntityIdx, attrIdx runtime.AttributeIdx) iter.Seq[%s] {
	accel := inst.%s
	accel.SetCurrentEntityIdx(int(entityIdx))
	r := accel.LookupForwardRange(attrIdx)
	if !r.IsEmpty() {
		b, _ := inst.%s.ValueOffsets(int(entityIdx))	
		return func(yield func(%s) bool) {
			vs := inst.%s
			for i := r.BeginIncl; i < r.EndExcl; i++ {
				if !yield(vs.Value(int(b)+int(i))) {
					break
				}
			}
		}
	}
	return nil
}
`
				_, err = fmt.Fprintf(b, tmpl,
					clsName,
					name1,
					typeName1,
					clsNamer.ComposeAccelFieldName(name1),

					clsNamer.ComposeValueField(name1),

					typeName1,
					clsNamer.ComposeValueFieldElementAccessor(name1),
				)
				if err != nil {
					return
				}
				if s.ContainsMixed() {
					var typeName2 string
					typeName2, _, err = getElementGoTypeName(ct2, hint2)
					if err != nil {
						err = eh.Errorf("unable to get element go type name: %w", err)
						return
					}
					name2 := naming.MustBeValidStylableName(role2.LongString()).Convert(naming.UpperCamelCase).String()
					_, err = fmt.Fprintf(b, tmpl,
						clsName,
						name2,
						typeName2,
						clsNamer.ComposeAccelFieldName(name2),

						clsNamer.ComposeValueField(name2),

						typeName2,
						clsNamer.ComposeValueFieldElementAccessor(name2),
					)
					if err != nil {
						return
					}
				}
			}
		}
	}

	return
}
func isElementAccessorNeeded(cc common.IntermediateColumnContext, role common.ColumnRoleE, tableRowConfig common.TableRowConfigE) (needed bool, err error) {
	if role != common.ColumnRoleValue {
		err = eb.Build().Stringer("role", role).Errorf("unhandled role")
		return
	}
	needed = !(cc.PlainItemType != common.PlainItemTypeNone && cc.SubType == common.IntermediateColumnsSubTypeScalar)
	return
}
func (inst *GoClassBuilder) composeSectionAttributeClasses(clsNamer gocodegen.GoClassNamerI, tableName naming.StylableName, sectionNames []naming.StylableName, ir *common.IntermediateTableRepresentation, tableRowConfig common.TableRowConfigE) (err error) {
	b := inst.builder
	var tblDesc common.TableDesc
	tblDesc, err = tableDescFromIr(ir, tableName)
	if err != nil {
		err = eh.Errorf("unable to get table desc: %w", err)
		return
	}
	attrClassesKv := containers.NewBinarySearchGrowingKV[string, *strings.Builder](len(sectionNames)+len(common.AllPlainItemTypes), strings.Compare)

	gocodegen.EmitGeneratingCodeLocation(b)
	colIdxGenerators := containers.NewBinarySearchGrowingKV[string, *ColumnIndexCodeGenerator](attrClassesKv.Len(), strings.Compare)
	getColIdxGenerator := func(cc common.IntermediateColumnContext) (gen *ColumnIndexCodeGenerator) {
		var has bool
		var clsName string
		clsName, err = clsNamer.ComposeSectionReadAccessAttributeClassName(tableName, cc.PlainItemType, cc.SectionName)
		if err != nil {
			log.Panic().Err(err).Msg("unable to compose read access inner class name")
		}
		gen, has = colIdxGenerators.Get(clsName)
		if !has {
			gen = NewColumnIndexCodeGenerator()
			colIdxGenerators.UpsertSingle(clsName, gen)
		}
		return
	}

	getAttrClassBuilder := func(cc common.IntermediateColumnContext) (builder *strings.Builder) {
		var has bool
		var clsName string
		clsName, err = clsNamer.ComposeSectionReadAccessAttributeClassName(tableName, cc.PlainItemType, cc.SectionName)
		if err != nil {
			log.Panic().Err(err).Msg("unable to compose read access inner class name")
		}
		builder, has = attrClassesKv.Get(clsName)
		if !has {
			builder = &strings.Builder{}
			attrClassesKv.UpsertSingle(clsName, builder)
		}
		return
	}
	resetAttrClassBuilders := func() {
		for bc := range attrClassesKv.IterateValues() {
			bc.Reset()
		}
	}
	setAccelFieldName := clsNamer.ComposeAccelFieldName("Set") // FIXME name clashes with regular attributes possible?
	homogenousArrayAccelFieldName := clsNamer.ComposeAccelFieldName("HomogenousArray")
	setColumnIndexFieldName := clsNamer.ComposeColumnIndexFieldName("Set")
	homogenousArrayColumnIndexFieldName := clsNamer.ComposeColumnIndexFieldName("HomogenousArray")
	{ // attribute classes: struct
		for cc, cp := range ir.IterateColumnProps() {
			bc := getAttrClassBuilder(cc)
			switch cc.SubType {
			case common.IntermediateColumnsSubTypeScalar, common.IntermediateColumnsSubTypeHomogenousArray, common.IntermediateColumnsSubTypeSet:
				{
					for i, colName := range cp.Names {
						ct := cp.CanonicalType[i]
						role := cp.Roles[i]
						switch role {
						case common.ColumnRoleValue:
							var typeName string
							var scalarTypeName string
							var elementAccessor bool
							elementAccessor, err = isElementAccessorNeeded(cc, role, tableRowConfig)
							if err != nil {
								return
							}
							scalarTypeName, _, err = gocodegen.CanonicalTypeToArrowBaseClassName(ct, cp.EncodingHints[i], common.UseArrowDictionaryEncoding)
							if err != nil {
								err = eh.Errorf("unable to get arrow class name for canonical type: %w", err)
								return
							}
							if elementAccessor {
								typeName = "List"
							} else {
								typeName = scalarTypeName
							}
							fieldName := colName.Convert(naming.UpperCamelCase).String()
							_, err = fmt.Fprintf(bc, "\t%s *array.%s\n\t%s uint32\n",
								clsNamer.ComposeValueField(fieldName),
								typeName,
								clsNamer.ComposeColumnIndexFieldName(fieldName),
							)
							if err != nil {
								return
							}
							if elementAccessor {
								_, err = fmt.Fprintf(bc, "\t%s *array.%s\n",
									clsNamer.ComposeValueFieldElementAccessor(fieldName),
									scalarTypeName,
								)
								if err != nil {
									return
								}
							}
							break
						default:
							err = eb.Build().Stringer("role", role).Stringer("subtype", cc.SubType).Errorf("unhandled role")
							return
						}
					}
				}
				break
			case common.IntermediateColumnsSubTypeHomogenousArraySupport, common.IntermediateColumnsSubTypeSetSupport:
				for _, role := range cp.Roles {
					var f1, f2, t string
					switch role {
					case common.ColumnRoleCardinality:
						f1 = setAccelFieldName
						f2 = setColumnIndexFieldName
						t = "SetIdx"
						break
					case common.ColumnRoleLength:
						f1 = homogenousArrayAccelFieldName
						f2 = homogenousArrayColumnIndexFieldName
						t = "HomogenousArrayIdx"
						break
					default:
						err = eb.Build().Stringer("role", role).Stringer("subtype", cc.SubType).Errorf("unhandled role")
						return
					}
					_, err = fmt.Fprintf(bc, "\t%s *runtime.RandomAccessTwoLevelLookupAccel[runtime.%s,runtime.AttributeIdx,int,int64]\n\t%s uint32\n",
						f1,
						t,
						f2)
					if err != nil {
						return
					}
				}
				break
			}
		}
		for clsName, bc := range attrClassesKv.Iterate() {
			if bc.Len() > 0 {
				_, err = fmt.Fprintf(b, "type %s struct {\n", clsName)
				if err != nil {
					return
				}
				_, err = b.WriteString(bc.String())
				if err != nil {
					return
				}
				_, err = b.WriteString("}\n\n")
				if err != nil {
					return
				}
			}
		}
	}
	{ // attribute class: factory
		resetAttrClassBuilders()
		for cc, cp := range ir.IterateColumnProps() {
			colIdxGen := getColIdxGenerator(cc)
			bc := getAttrClassBuilder(cc)
			switch cc.SubType {
			case common.IntermediateColumnsSubTypeScalar, common.IntermediateColumnsSubTypeHomogenousArray, common.IntermediateColumnsSubTypeSet:
				{
					for i, colName := range cp.Names {
						role := cp.Roles[i]
						switch role {
						case common.ColumnRoleValue:
							fieldName := colName.Convert(naming.UpperCamelCase).String()
							colIdxGen.AddField(clsNamer.ComposeColumnIndexFieldName(fieldName), cc.IndexOffset+uint32(i))
							break
						default:
							err = eb.Build().Stringer("role", role).Stringer("subtype", cc.SubType).Errorf("unhandled role")
							return
						}
					}
				}
				break
			case common.IntermediateColumnsSubTypeHomogenousArraySupport, common.IntermediateColumnsSubTypeSetSupport:
				for i, role := range cp.Roles {
					var f1, f2, t string
					switch role {
					case common.ColumnRoleCardinality:
						f1 = setColumnIndexFieldName
						f2 = setAccelFieldName
						t = "SetIdx"
						break
					case common.ColumnRoleLength:
						f1 = homogenousArrayColumnIndexFieldName
						f2 = homogenousArrayAccelFieldName
						t = "HomogenousArrayIdx"
					default:
						err = eb.Build().Stringer("role", role).Stringer("subtype", cc.SubType).Errorf("unhandled role")
						return
					}
					colIdxGen.AddField(f1, cc.IndexOffset+uint32(i))
					_, err = fmt.Fprintf(bc, "\tinst.%s = runtime.NewRandomAccessTwoLevelLookupAccel[runtime.%s,runtime.AttributeIdx,int,int64](runtime.AccelEstimatedInitialLength)\n",
						f2,
						t)
					if err != nil {
						return
					}
				}
				break
			}
		}
		for clsName, gen := range colIdxGenerators.Iterate() {
			if gen.Length() > 0 {
				_, err = fmt.Fprintf(b, "func New%s() (inst *%s) {\n\tinst = &%s{}\n",
					clsName,
					clsName,
					clsName)
				if err != nil {
					return
				}
				err = gen.GenerateInstInit(b)
				if err != nil {
					err = eh.Errorf("unable to generate column index init code: %w", err)
					return
				}
				bc, has := attrClassesKv.Get(clsName)
				if has {
					_, err = b.WriteString(bc.String())
					if err != nil {
						return
					}
				}
				_, err = b.WriteString("\treturn\n}\n\n")
				if err != nil {
					return
				}
				err = gen.GenerateCommon(b, clsName)
				if err != nil {
					err = eh.Errorf("unable to generate column index code: %w", err)
					return
				}
			}
		}
	}
	{ // .Reset()
		gocodegen.EmitGeneratingCodeLocation(b)
		resetAttrClassBuilders()

		for cc, cp := range ir.IterateColumnProps() {
			bc := getAttrClassBuilder(cc)
			switch cc.SubType {
			case common.IntermediateColumnsSubTypeScalar, common.IntermediateColumnsSubTypeHomogenousArray, common.IntermediateColumnsSubTypeSet:
				{
					for i, colName := range cp.Names {
						role := cp.Roles[i]
						switch role {
						case common.ColumnRoleValue:
							fieldName := colName.Convert(naming.UpperCamelCase).String()
							_, err = fmt.Fprintf(bc, "\tinst.%s = nil\n", clsNamer.ComposeValueField(fieldName))
							if err != nil {
								return
							}
							var elementAccessor bool
							elementAccessor, err = isElementAccessorNeeded(cc, role, tableRowConfig)
							if err != nil {
								return
							}
							if elementAccessor {
								_, err = fmt.Fprintf(bc, "\tinst.%s = nil\n", clsNamer.ComposeValueFieldElementAccessor(fieldName))
								if err != nil {
									return
								}
							}
							break
						default:
							err = eb.Build().Stringer("role", role).Stringer("subtype", cc.SubType).Errorf("unhandled role")
							return
						}
					}
				}
				break
			case common.IntermediateColumnsSubTypeHomogenousArraySupport, common.IntermediateColumnsSubTypeSetSupport:
				for _, role := range cp.Roles {
					var f string
					switch role {
					case common.ColumnRoleCardinality:
						f = setAccelFieldName
						break
					case common.ColumnRoleLength:
						f = homogenousArrayAccelFieldName
						break
					default:
						err = eb.Build().Stringer("role", role).Stringer("subtype", cc.SubType).Errorf("unhandled role")
						return
					}
					_, err = fmt.Fprintf(bc, "\tif inst.%s != nil {\n\t\tinst.%s.Reset()\n\t}\n",
						f,
						f,
					)
					if err != nil {
						return
					}
				}
				break
			}
		}
		for clsName, bc := range attrClassesKv.Iterate() {
			if bc.Len() > 0 {
				_, err = fmt.Fprintf(b, "func (inst *%s) Reset() {\n", clsName)
				if err != nil {
					return
				}
				_, err = b.WriteString(bc.String())
				if err != nil {
					return
				}
				_, err = b.WriteString("}\n\n")
				if err != nil {
					return
				}
			}
		}
	}
	{ // .Release()
		gocodegen.EmitGeneratingCodeLocation(b)
		resetAttrClassBuilders()

		for cc, cp := range ir.IterateColumnProps() {
			bc := getAttrClassBuilder(cc)
			switch cc.SubType {
			case common.IntermediateColumnsSubTypeScalar, common.IntermediateColumnsSubTypeHomogenousArray, common.IntermediateColumnsSubTypeSet:
				{
					for i, colName := range cp.Names {
						role := cp.Roles[i]
						switch role {
						case common.ColumnRoleValue:
							fieldName := colName.Convert(naming.UpperCamelCase).String()
							_, err = fmt.Fprintf(bc, "\truntime.ReleaseIfNotNil(inst.%s)\n", clsNamer.ComposeValueField(fieldName))
							if err != nil {
								return
							}
							var elementAccessor bool
							elementAccessor, err = isElementAccessorNeeded(cc, role, tableRowConfig)
							if err != nil {
								return
							}
							if elementAccessor {
								_, err = fmt.Fprintf(bc, "\truntime.ReleaseIfNotNil(inst.%s)\n", clsNamer.ComposeValueFieldElementAccessor(fieldName))
								if err != nil {
									return
								}
							}
							break
						default:
							err = eb.Build().Stringer("role", role).Stringer("subtype", cc.SubType).Errorf("unhandled role")
							return
						}
					}
				}
				break
			case common.IntermediateColumnsSubTypeHomogenousArraySupport, common.IntermediateColumnsSubTypeSetSupport:
				for _, role := range cp.Roles {
					var f string
					switch role {
					case common.ColumnRoleCardinality:
						f = setAccelFieldName
						break
					case common.ColumnRoleLength:
						f = homogenousArrayAccelFieldName
						break
					default:
						err = eb.Build().Stringer("role", role).Stringer("subtype", cc.SubType).Errorf("unhandled role")
						return
					}
					_, err = fmt.Fprintf(bc, "\truntime.ReleaseIfNotNil(inst.%s)\n", f)
					if err != nil {
						return
					}
				}
				break
			}
		}
		for clsName, bc := range attrClassesKv.Iterate() {
			if bc.Len() > 0 {
				_, err = fmt.Fprintf(b, `
var _ runtime.ReleasableI = (*%s)(nil)

func (inst *%s) Release() {
`,
					clsName,
					clsName)
				if err != nil {
					return
				}
				_, err = b.WriteString(bc.String())
				if err != nil {
					return
				}
				_, err = b.WriteString("}\n\n")
				if err != nil {
					return
				}
			}
		}
	}
	{ // .Len()
		gocodegen.EmitGeneratingCodeLocation(b)
		resetAttrClassBuilders()

		for cc, cp := range ir.IterateColumnProps() {
			bc := getAttrClassBuilder(cc)
			switch cc.SubType {
			case common.IntermediateColumnsSubTypeScalar, common.IntermediateColumnsSubTypeHomogenousArray, common.IntermediateColumnsSubTypeSet:
				{
					for i, colName := range cp.Names {
						role := cp.Roles[i]
						switch role {
						case common.ColumnRoleValue:
							if bc.Len() == 0 {
								f := clsNamer.ComposeValueField(colName.Convert(naming.UpperCamelCase).String())
								_, err = fmt.Fprintf(bc, "\tif inst.%s != nil {\n\t\tnEntities = inst.%s.Len()\n\t}\n",
									f,
									f,
								)
								if err != nil {
									return
								}
							}
							break
						default:
							err = eb.Build().Stringer("role", role).Stringer("subtype", cc.SubType).Errorf("unhandled role")
							return
						}
					}
				}
				break
			}
		}
		for clsName, bc := range attrClassesKv.Iterate() {
			if bc.Len() > 0 {
				_, err = fmt.Fprintf(b, `
func (inst *%s) Len() (nEntities int) {
`,
					clsName)
				if err != nil {
					return
				}
				_, err = b.WriteString(bc.String())
				if err != nil {
					return
				}
				_, err = b.WriteString("\treturn\n}\n\n")
				if err != nil {
					return
				}
			}
		}
	}

	{ // .LoadFromRecord(rec arrow.Record) (err error)
		gocodegen.EmitGeneratingCodeLocation(b)
		for bc := range attrClassesKv.Values() {
			bc.Reset()
		}

		for cc, cp := range ir.IterateColumnProps() {
			bc := getAttrClassBuilder(cc)
			switch cc.SubType {
			case common.IntermediateColumnsSubTypeScalar, common.IntermediateColumnsSubTypeHomogenousArray, common.IntermediateColumnsSubTypeSet:
				{
					for i, colName := range cp.Names {
						ct := cp.CanonicalType[i]
						role := cp.Roles[i]
						switch role {
						case common.ColumnRoleValue:
							var typeName string
							typeName, _, err = gocodegen.CanonicalTypeToArrowBaseClassName(ct, cp.EncodingHints[i], common.UseArrowDictionaryEncoding)
							if err != nil {
								err = eh.Errorf("unable to get arrow class name for canonical type: %w", err)
								return
							}
							var elementAccessor bool
							elementAccessor, err = isElementAccessorNeeded(cc, role, tableRowConfig)
							if err != nil {
								return
							}
							if elementAccessor {
								fieldName := colName.Convert(naming.UpperCamelCase).String()
								_, err = fmt.Fprintf(bc, `	err = runtime.LoadNonScalarValueFieldFromRecord(inst.%s,arrow.%s,rec,&inst.%s,&inst.%s,array.New%sData)
	if err != nil {
		return
	}
`,
									clsNamer.ComposeColumnIndexFieldName(fieldName),
									strings.ToUpper(typeName),
									clsNamer.ComposeValueField(fieldName),
									clsNamer.ComposeValueFieldElementAccessor(fieldName),
									typeName,
								)
							} else {
								fieldName := colName.Convert(naming.UpperCamelCase).String()
								_, err = fmt.Fprintf(bc, `	err = runtime.LoadScalarValueFieldFromRecord(inst.%s,arrow.%s,rec,&inst.%s,array.New%sData)
	if err != nil {
		return
	}
`,
									clsNamer.ComposeColumnIndexFieldName(fieldName),
									strings.ToUpper(typeName),
									clsNamer.ComposeValueField(fieldName),
									naming.MustBeValidStylableName(typeName).Convert(naming.UpperCamelCase),
								)
							}
							if err != nil {
								return
							}
							break
						default:
							err = eb.Build().Stringer("role", role).Stringer("subtype", cc.SubType).Errorf("unhandled role")
							return
						}
					}
				}
				break
			case common.IntermediateColumnsSubTypeHomogenousArraySupport, common.IntermediateColumnsSubTypeSetSupport:
				for _, role := range cp.Roles {
					var f1, f2 string
					switch role {
					case common.ColumnRoleCardinality:
						f1 = setColumnIndexFieldName
						f2 = setAccelFieldName
						break
					case common.ColumnRoleLength:
						f1 = homogenousArrayColumnIndexFieldName
						f2 = homogenousArrayAccelFieldName
						break
					default:
						err = eb.Build().Stringer("role", role).Stringer("subtype", cc.SubType).Errorf("unhandled role")
						return
					}
					_, err = fmt.Fprintf(bc, `	err = runtime.LoadAccelFieldFromRecord(inst.%s,rec,inst.%s)
	if err != nil {
		return
	}
`, f1, f2)
					if err != nil {
						return
					}
				}
				break
			}
		}
		for clsName, bc := range attrClassesKv.Iterate() {
			if bc.Len() > 0 {
				_, err = fmt.Fprintf(b, "func (inst *%s)  LoadFromRecord(rec arrow.Record) (err error) {\n", clsName)
				if err != nil {
					return
				}
				_, err = b.WriteString(bc.String())
				if err != nil {
					return
				}
				_, err = b.WriteString("\treturn\n}\n\n")
				if err != nil {
					return
				}
			}
		}
	}

	{ // .GetAttrValueXXX
		for _, s := range tblDesc.TaggedValuesSections {
			const pt = common.PlainItemTypeNone
			for i, attrName := range s.ValueColumnNames {
				ct := s.ValueColumnTypes[i]
				var scalarModifier canonicaltypes.ScalarModifierE
				var typeName string
				typeName, scalarModifier, err = getElementGoTypeName(ct, s.ValueEncodingHints[i])
				if err != nil {
					err = eh.Errorf("unable to get element go type name: %w", err)
					return
				}
				subType := common.GetSubTypeByScalarModifier(scalarModifier)

				var clsName string
				clsName, err = clsNamer.ComposeSectionReadAccessAttributeClassName(tableName, pt, s.Name)
				if err != nil {
					err = eh.Errorf("unable to compose read access inner class name: %w", err)
					return
				}
				attrNameS := attrName.Convert(naming.UpperCamelCase).String()
				switch subType {
				case common.IntermediateColumnsSubTypeScalar:
					_, err = fmt.Fprintf(b, `func (inst *%s) GetAttrValue%s(entityIdx runtime.EntityIdx,attrIdx runtime.AttributeIdx) (scalarAttrValue %s) {
	b, e := inst.%s.ValueOffsets(int(entityIdx))
	if int64(attrIdx) > (e-b) {
		log.Panic().Str("attribute",%q).Int("beginIncl",int(b)).Int("endExcl",int(e)).Int("attrIdx",int(attrIdx)).Msg("attribute index is out of range")
	}
	scalarAttrValue = inst.%s.Value(int(b) + int(attrIdx))
	return
}
`,
						clsName,
						attrNameS,
						typeName,
						clsNamer.ComposeValueField(attrNameS),

						attrNameS,
						clsNamer.ComposeValueFieldElementAccessor(attrNameS),
					)
					break
				case common.IntermediateColumnsSubTypeSet, common.IntermediateColumnsSubTypeHomogenousArray:
					var f string
					switch subType {
					case common.IntermediateColumnsSubTypeSet:
						f = setAccelFieldName
						break
					case common.IntermediateColumnsSubTypeHomogenousArray:
						f = homogenousArrayAccelFieldName
						break
					}
					_, err = fmt.Fprintf(b, `func (inst *%s) GetAttrValue%s(entityIdx runtime.EntityIdx,attrIdx runtime.AttributeIdx) iter.Seq[%s] {
	accel := inst.%s
	accel.SetCurrentEntityIdx(int(entityIdx))
	r := accel.LookupForwardRange(attrIdx)
	if !r.IsEmpty() {
		return func(yield func(%s) bool) {
			vs := inst.%s
			for i := r.BeginIncl; i < r.EndExcl; i++ {
				if !yield(vs.Value(int(i))) {
					break
				}
			}
		}
	}
	return nil
}
`,
						clsName,
						attrNameS,
						typeName,

						f,
						typeName,
						clsNamer.ComposeValueFieldElementAccessor(attrNameS),
					)
					if err != nil {
						return
					}
					break
				}
				if err != nil {
					return
				}
			}
		}

		for i, pt := range tblDesc.PlainValuesItemTypes {
			ct := tblDesc.PlainValuesTypes[i]
			hints := tblDesc.PlainValuesEncodingHints[i]
			var scalarModifier canonicaltypes.ScalarModifierE
			var typeName string
			typeName, scalarModifier, err = getElementGoTypeName(ct, hints)
			if err != nil {
				err = eh.Errorf("unable to get element go type name: %w", err)
				return
			}
			subType := common.GetSubTypeByScalarModifier(scalarModifier)

			var clsName string
			clsName, err = clsNamer.ComposeSectionReadAccessAttributeClassName(tableName, pt, "")
			if err != nil {
				err = eh.Errorf("unable to compose read access inner class name: %w", err)
				return
			}
			var typeConvPrefix, typeConvSuffix string
			typeConvPrefix, typeConvSuffix, err = gocodegen.ArrowTypeToGoType(ct, hints, common.UseArrowDictionaryEncoding)
			if err != nil {
				err = eh.Errorf("unable to get arrow to go type conversion info: %w", err)
				return
			}
			attrName := tblDesc.PlainValuesNames[i]
			attrNameS := attrName.Convert(naming.UpperCamelCase).String()
			switch subType {
			case common.IntermediateColumnsSubTypeScalar:
				_, err = fmt.Fprintf(b, `func (inst *%s) GetAttrValue%s(entityIdx runtime.EntityIdx) (scalarAttrValue %s) {
	scalarAttrValue = %sinst.%s.Value(int(entityIdx))%s
	return
}
`,
					clsName,
					attrNameS,
					typeName,
					typeConvPrefix,
					clsNamer.ComposeValueField(attrNameS),
					typeConvSuffix,
				)
				break
			case common.IntermediateColumnsSubTypeSet, common.IntermediateColumnsSubTypeHomogenousArray:
				_, err = fmt.Fprintf(b, `func (inst *%s) GetAttrValue%s(entityIdx runtime.EntityIdx) iter.Seq[%s] {
		return func(yield func(%s) bool) {
			b, e := inst.%s.ValueOffsets(int(entityIdx))	
			vs := inst.%s
			for i := b; i < e; i++ {
				if !yield(%svs.Value(int(i))%s) {
					break
				}
			}
		}
}
`,
					clsName,
					attrNameS,
					typeName,
					typeName,
					clsNamer.ComposeValueField(attrNameS),
					clsNamer.ComposeValueFieldElementAccessor(attrNameS),
					typeConvPrefix,
					typeConvSuffix,
				)
				if err != nil {
					return
				}
				break
			}
			if err != nil {
				return
			}
		}
	}

	{ // .GetNumberOfAttributes(i runtime.EntityIdx) (nAttributes int)
		gocodegen.EmitGeneratingCodeLocation(b)

		for _, s := range tblDesc.TaggedValuesSections {
			attrName := s.ValueColumnNames[0]
			var clsName string
			clsName, err = clsNamer.ComposeSectionReadAccessAttributeClassName(tableName, common.PlainItemTypeNone, s.Name)
			if err != nil {
				err = eh.Errorf("unable to compose read access inner class name: %w", err)
				return
			}
			attrNameS := attrName.Convert(naming.UpperCamelCase).String()
			_, err = fmt.Fprintf(b, `func (inst *%s) GetNumberOfAttributes(entityIdx runtime.EntityIdx) (nAttributes int64) {
	b, e := inst.%s.ValueOffsets(int(entityIdx))
	nAttributes = e-b
	return
}
`,
				clsName,
				clsNamer.ComposeValueField(attrNameS),
			)
			if err != nil {
				return
			}
		}
	}

	return
}
func (inst *GoClassBuilder) composeSectionClasses(clsNamer gocodegen.GoClassNamerI, tableName naming.StylableName, sectionNames []naming.StylableName, ir *common.IntermediateTableRepresentation, tableRowConfig common.TableRowConfigE, entityIRH *common.IntermediatePairHolder) (err error) {
	var tblDesc common.TableDesc
	tblDesc, err = tableDescFromIr(ir, tableName)
	if err != nil {
		err = eh.Errorf("unable to get table desc: %w", err)
		return
	}

	{ // membership packs
		err = inst.composeMembershipPacks(ir, tblDesc, clsNamer, tableRowConfig, common.UseArrowDictionaryEncoding)
		if err != nil {
			err = eh.Errorf("unable to compose membership packs: %w", err)
			return
		}
	}

	{ // attribute classes
		err = inst.composeSectionAttributeClasses(clsNamer, tableName, sectionNames, ir, tableRowConfig)
		if err != nil {
			err = eh.Errorf("unable to compose inner section classes: %w", err)
			return
		}
	}

	b := inst.builder
	gocodegen.EmitGeneratingCodeLocation(b)
	var sectionToClassNames []string
	_, _, sectionToClassNames, err = ComposeMembershipPackInfo(tblDesc, clsNamer)
	if err != nil {
		err = eh.Errorf("unable to compose membership pack info: %w", err)
		return
	}

	composeCode := func(o func(sec common.TaggedValuesSection, outerClsName string) (err error),
		a func(sec common.TaggedValuesSection, attrClsName string) (err error),
		m func(sec common.TaggedValuesSection, membClsName string) (err error),
		s func(sec common.TaggedValuesSection, outerClsName string) (err error)) {
		for i, sec := range tblDesc.TaggedValuesSections {
			const pt = common.PlainItemTypeNone
			var outerClsName string
			outerClsName, err = clsNamer.ComposeSectionReadAccessOuterClassName(tableName, pt, sec.Name)
			if err != nil {
				err = eh.Errorf("unable to generate outer class name: %w", err)
				return
			}
			err = o(sec, outerClsName)
			if err != nil {
				return
			}
			if len(sec.ValueColumnNames) > 0 {
				var attrClsName string
				attrClsName, err = clsNamer.ComposeSectionReadAccessAttributeClassName(tableName, pt, sec.Name)
				if err != nil {
					err = eh.Errorf("unable to generate attribute class name: %w", err)
					return
				}
				err = a(sec, attrClsName)
				if err != nil {
					return
				}
			}

			if sectionToClassNames[i] != "" {
				err = m(sec, sectionToClassNames[i])
				if err != nil {
					return
				}
			}
			err = s(sec, outerClsName)
			if err != nil {
				return
			}
		}
	}

	{ // struct
		composeCode(func(sec common.TaggedValuesSection, outerClsName string) (err error) {
			_, err = fmt.Fprintf(b, "type %s struct {\n", outerClsName)
			return
		}, func(sec common.TaggedValuesSection, attrClsName string) (err error) {
			_, err = fmt.Fprintf(b, "\tAttributes *%s\n", attrClsName)
			return
		}, func(sec common.TaggedValuesSection, membClsName string) (err error) {
			_, err = fmt.Fprintf(b, "\tMemberships *%s\n", membClsName)
			return
		}, func(sec common.TaggedValuesSection, outerClsName string) (err error) {
			_, err = fmt.Fprintf(b, "}\n\nvar _ runtime.ColumnIndexHandlingI = (*%s)(nil)\n", outerClsName)
			return
		})
	}
	{ // factory
		composeCode(func(sec common.TaggedValuesSection, outerClsName string) (err error) {
			_, err = fmt.Fprintf(b, "func New%s() (inst *%s) {\n\tinst = &%s{}\n",
				outerClsName,
				outerClsName,
				outerClsName,
			)
			return
		}, func(sec common.TaggedValuesSection, attrClsName string) (err error) {
			_, err = fmt.Fprintf(b, "\tinst.Attributes = New%s()\n", attrClsName)
			return
		}, func(sec common.TaggedValuesSection, membClsName string) (err error) {
			_, err = fmt.Fprintf(b, "\tinst.Memberships = New%s%s()\n",
				membClsName,
				sec.Name.Convert(naming.UpperCamelCase),
			)
			return
		}, func(sec common.TaggedValuesSection, outerClsName string) (err error) {
			_, err = fmt.Fprint(b, "\treturn\n}\n\n")
			return
		})
	}
	composeDelegate := func(funcName string, argsDecl string, retrDecl string, retrAssign string, afterFunc string, prolog string, args string, epilog string) {
		composeCode(func(sec common.TaggedValuesSection, outerClsName string) (err error) {
			_, err = fmt.Fprintf(b, "func (inst *%s) %s(%s) %s {\n%s",
				outerClsName,
				funcName,
				argsDecl,
				retrDecl,
				prolog,
			)
			return
		}, func(sec common.TaggedValuesSection, attrClsName string) (err error) {
			_, err = fmt.Fprintf(b, "\t%sinst.Attributes.%s(%s)%s\n",
				retrAssign,
				funcName,
				args,
				afterFunc,
			)
			return
		}, func(sec common.TaggedValuesSection, membClsName string) (err error) {
			_, err = fmt.Fprintf(b, "\t%sinst.Memberships.%s(%s)%s\n",
				retrAssign,
				funcName,
				args,
				afterFunc,
			)
			return
		}, func(sec common.TaggedValuesSection, outerClsName string) (err error) {
			_, err = fmt.Fprintf(b, "%s\treturn\n}\n\n",
				epilog)
			return
		})
	}
	{ // .SetColumnIndices(indices []uint32) (restIndices []uint32)
		composeDelegate("SetColumnIndices",
			"indices []uint32",
			"(restIndices []uint32)",
			"restIndices = ",
			"",
			"\trestIndices = indices\n",
			"restIndices",
			"",
		)
	}
	{ // .GetColumnIndices() (columnIndices []uint32)
		composeDelegate("GetColumnIndices",
			"",
			"(columnIndices []uint32)",
			"columnIndices = slices.Concat(columnIndices,",
			")",
			"",
			"",
			"",
		)
	}
	{ // .GetColumnIndexFieldNames() (fieldNames []string)
		composeDelegate("GetColumnIndexFieldNames",
			"",
			"(fieldNames []string)",
			"fieldNames = slices.Concat(fieldNames,",
			")",
			"",
			"",
			"",
		)
	}
	{ // .Release()
		composeCode(func(sec common.TaggedValuesSection, outerClsName string) (err error) {
			_, err = fmt.Fprintf(b, "func (inst *%s) Release() {\n",
				outerClsName,
			)
			return
		}, func(sec common.TaggedValuesSection, attrClsName string) (err error) {
			_, err = fmt.Fprint(b, "\truntime.ReleaseIfNotNil(inst.Attributes)\n")
			return
		}, func(sec common.TaggedValuesSection, membClsName string) (err error) {
			_, err = fmt.Fprint(b, "\truntime.ReleaseIfNotNil(inst.Memberships)\n")
			return
		}, func(sec common.TaggedValuesSection, outerClsName string) (err error) {
			_, err = fmt.Fprint(b, "}\n\n")
			return
		})
	}
	{ // .LoadFromRecord(rec arrow.Record) (err error)
		composeDelegate("LoadFromRecord",
			"rec arrow.Record",
			"(err error)",
			"err = ",
			"\nif err != nil {\n\terr = eb.Build().Errorf(\"unable to load from record: %w\", err)\n\treturn\n}",
			"",
			"rec",
			"",
		)
	}
	{ // .Len() (nEntities int)
		composeCode(func(sec common.TaggedValuesSection, outerClsName string) (err error) {
			_, err = fmt.Fprintf(b, "func (inst *%s) Len() (nEntities int) {\n",
				outerClsName,
			)
			return
		}, func(sec common.TaggedValuesSection, attrClsName string) (err error) {
			if sec.MembershipSpec.Count() == 0 {
				_, err = fmt.Fprint(b, "\tnEntities = inst.Attributes.Len()\n")
			}
			return
		}, func(sec common.TaggedValuesSection, membClsName string) (err error) {
			_, err = fmt.Fprint(b, "\tnEntities = inst.Memberships.Len()\n")
			return
		}, func(sec common.TaggedValuesSection, outerClsName string) (err error) {
			_, err = fmt.Fprint(b, "\treturn\n}\n\n")
			return
		})
	}
	{ // Getters for public Attributes to enable generic programming (interfaces)
		composeCode(func(sec common.TaggedValuesSection, outerClsName string) (err error) {
			_, err = fmt.Fprintf(b, "func (inst *%s) GetAttributes() *", outerClsName)
			return
		}, func(sec common.TaggedValuesSection, attrClsName string) (err error) {
			_, err = fmt.Fprintf(b, "%s {\n\treturn inst.Attributes\n", attrClsName)
			return
		}, func(sec common.TaggedValuesSection, membClsName string) (err error) {
			return
		}, func(sec common.TaggedValuesSection, outerClsName string) (err error) {
			_, err = fmt.Fprint(b, "}\n\n")
			return
		})
		composeCode(func(sec common.TaggedValuesSection, outerClsName string) (err error) {
			_, err = fmt.Fprintf(b, "func (inst *%s) GetMemberships() *", outerClsName)
			return
		}, func(sec common.TaggedValuesSection, attrClsName string) (err error) {
			return
		}, func(sec common.TaggedValuesSection, membClsName string) (err error) {
			_, err = fmt.Fprintf(b, "%s {\n\treturn inst.Memberships\n", membClsName)
			return
		}, func(sec common.TaggedValuesSection, outerClsName string) (err error) {
			_, err = fmt.Fprint(b, "}\n\n")
			return
		})
	}
	if inst.fatRuntime {
		// section introspection
		{ // .GetSectionName() naming.StylableName
			composeCode(func(sec common.TaggedValuesSection, outerClsName string) (err error) {
				_, err = fmt.Fprintf(b, "func (inst *%s) GetSectionName() naming.StylableName {\n",
					outerClsName,
				)
				return
			}, func(sec common.TaggedValuesSection, attrClsName string) (err error) {
				_, err = fmt.Fprintf(b, "\treturn %q\n", sec.Name.Convert(naming.DefaultNamingStyle))
				return
			}, func(sec common.TaggedValuesSection, membClsName string) (err error) {
				return
			}, func(sec common.TaggedValuesSection, outerClsName string) (err error) {
				_, err = fmt.Fprintf(b, "}\n\nvar _ fatruntime.SectionIntrospectionI = (*%s)(nil)\n\n", outerClsName)
				return
			})
		}
		{ // .GetSectionUseAspects() useaspects.AspectSet
			composeCode(func(sec common.TaggedValuesSection, outerClsName string) (err error) {
				_, err = fmt.Fprintf(b, "func (inst *%s) GetSectionUseAspects() useaspects.AspectSet {\n",
					outerClsName,
				)
				return
			}, func(sec common.TaggedValuesSection, attrClsName string) (err error) {
				_, err = fmt.Fprintf(b, "\treturn %q\n", sec.UseAspects.String())
				return
			}, func(sec common.TaggedValuesSection, membClsName string) (err error) {
				return
			}, func(sec common.TaggedValuesSection, outerClsName string) (err error) {
				_, err = fmt.Fprint(b, "}\n\n")
				return
			})
		}
		{ // .GetSectionStreamingGroup() naming.Key
			composeCode(func(sec common.TaggedValuesSection, outerClsName string) (err error) {
				_, err = fmt.Fprintf(b, "func (inst *%s) GetSectionStreamingGroup() naming.Key {\n",
					outerClsName,
				)
				return
			}, func(sec common.TaggedValuesSection, attrClsName string) (err error) {
				_, err = fmt.Fprintf(b, "\treturn %q\n", sec.StreamingGroup)
				return
			}, func(sec common.TaggedValuesSection, membClsName string) (err error) {
				return
			}, func(sec common.TaggedValuesSection, outerClsName string) (err error) {
				_, err = fmt.Fprint(b, "}\n\n")
				return
			})
		}
		{ // .GetSectionCoSectionGroup() naming.Key
			composeCode(func(sec common.TaggedValuesSection, outerClsName string) (err error) {
				_, err = fmt.Fprintf(b, "func (inst *%s) GetSectionCoSectionGroup() naming.Key {\n",
					outerClsName,
				)
				return
			}, func(sec common.TaggedValuesSection, attrClsName string) (err error) {
				_, err = fmt.Fprintf(b, "\treturn %q\n", sec.CoSectionGroup)
				return
			}, func(sec common.TaggedValuesSection, membClsName string) (err error) {
				return
			}, func(sec common.TaggedValuesSection, outerClsName string) (err error) {
				_, err = fmt.Fprint(b, "}\n\n")
				return
			})
		}
		{ // .GetSectionMembershipSpec() common.MembershipSpecE
			composeCode(func(sec common.TaggedValuesSection, outerClsName string) (err error) {
				_, err = fmt.Fprintf(b, "func (inst *%s) GetSectionMembershipSpec() common.MembershipSpecE {\n",
					outerClsName,
				)
				return
			}, func(sec common.TaggedValuesSection, attrClsName string) (err error) {
				_, err = fmt.Fprintf(b, "\treturn 0b%b\n", sec.MembershipSpec)
				return
			}, func(sec common.TaggedValuesSection, membClsName string) (err error) {
				return
			}, func(sec common.TaggedValuesSection, outerClsName string) (err error) {
				_, err = fmt.Fprint(b, "}\n\n")
				return
			})
		}
	}

	return
}
func extractEffectivePlainItemTypes(tblDesc common.TableDesc) (ptsEff []common.PlainItemTypeE) {
	ptsEff = slices.Clone(common.AllPlainItemTypes)
	ptsEff = slices.DeleteFunc(ptsEff, func(e common.PlainItemTypeE) bool {
		if e == common.PlainItemTypeNone {
			return true
		}
		return !slices.Contains(tblDesc.PlainValuesItemTypes, e)
	})
	return
}
func (inst *GoClassBuilder) composeEntityClasses(clsNamer gocodegen.GoClassNamerI, tableName naming.StylableName, sectionNames []naming.StylableName, ir *common.IntermediateTableRepresentation, tableRowConfig common.TableRowConfigE, entityIRH *common.IntermediatePairHolder) (err error) {
	b := inst.builder
	var tblDesc common.TableDesc
	tblDesc, err = tableDescFromIr(ir, tableName)
	if err != nil {
		err = eh.Errorf("unable to get table desc: %w", err)
		return
	}

	gocodegen.EmitGeneratingCodeLocation(b)

	var entityClsName string
	entityClsName, err = clsNamer.ComposeEntityReadAccessClassName(tableName)
	if err != nil {
		err = eh.Errorf("unable to compose entity class name: %w", err)
		return
	}
	ptsEff := extractEffectivePlainItemTypes(tblDesc)
	{ // entity struct
		_, err = fmt.Fprintf(b, "type %s struct {\n", entityClsName)
		if err != nil {
			return
		}
		for _, pt := range ptsEff {
			sectionName := naming.MustBeValidStylableName(pt.String())
			var outerClsName string
			outerClsName, err = clsNamer.ComposeSectionReadAccessAttributeClassName(tableName, pt, sectionName)
			if err != nil {
				err = eh.Errorf("unable to compose read access outer class name: %w", err)
				return
			}
			_, err = fmt.Fprintf(b, "\t%s *%s\n",
				sectionName.Convert(naming.UpperCamelCase),
				outerClsName)
			if err != nil {
				return
			}
		}

		for _, s := range tblDesc.TaggedValuesSections {
			const pt = common.PlainItemTypeNone
			var outerClsName string
			outerClsName, err = clsNamer.ComposeSectionReadAccessOuterClassName(tableName, pt, s.Name)
			if err != nil {
				err = eh.Errorf("unable to compose read access outer class name: %w", err)
				return
			}
			_, err = fmt.Fprintf(b, "\t%s *%s\n",
				s.Name.Convert(naming.UpperCamelCase),
				outerClsName)
			if err != nil {
				return
			}
		}
		_, err = fmt.Fprint(b, "}\n\n")
		if err != nil {
			return
		}
	}
	{ // factory
		_, err = fmt.Fprintf(b, "func New%s() (inst *%s) {\n\tinst = &%s{}\n",
			entityClsName,
			entityClsName,
			entityClsName)
		if err != nil {
			return
		}
		for _, pt := range ptsEff {
			sectionName := naming.MustBeValidStylableName(pt.String())
			var outerClsName string
			outerClsName, err = clsNamer.ComposeSectionReadAccessAttributeClassName(tableName, pt, sectionName)
			if err != nil {
				err = eh.Errorf("unable to compose read access outer class name: %w", err)
				return
			}
			_, err = fmt.Fprintf(b, "\tinst.%s = New%s()\n",
				sectionName.Convert(naming.UpperCamelCase),
				outerClsName)
			if err != nil {
				return
			}
		}

		for _, s := range tblDesc.TaggedValuesSections {
			const pt = common.PlainItemTypeNone
			var outerClsName string
			outerClsName, err = clsNamer.ComposeSectionReadAccessOuterClassName(tableName, pt, s.Name)
			if err != nil {
				err = eh.Errorf("unable to compose read access outer class name: %w", err)
				return
			}
			_, err = fmt.Fprintf(b, "\tinst.%s = New%s()\n",
				s.Name.Convert(naming.UpperCamelCase),
				outerClsName)
			if err != nil {
				return
			}
		}
		_, err = fmt.Fprint(b, "\treturn\n}\n\n")
		if err != nil {
			return
		}
	}
	{ // .Release()
		_, err = fmt.Fprintf(b, "func (inst *%s) Release() {\n", entityClsName)
		if err != nil {
			return
		}
		for _, pt := range ptsEff {
			sectionName := naming.MustBeValidStylableName(pt.String())
			_, err = fmt.Fprintf(b, "\truntime.ReleaseIfNotNil(inst.%s)\n",
				sectionName.Convert(naming.UpperCamelCase))
			if err != nil {
				return
			}
		}

		for _, s := range tblDesc.TaggedValuesSections {
			_, err = fmt.Fprintf(b, "\truntime.ReleaseIfNotNil(inst.%s)\n",
				s.Name.Convert(naming.UpperCamelCase))
			if err != nil {
				return
			}
		}
		_, err = fmt.Fprint(b, "}\n\n")
		if err != nil {
			return
		}
	}
	{ // .LoadFromRecord(rec arrow.Record) (err error)
		_, err = fmt.Fprintf(b, "func (inst *%s) LoadFromRecord(rec arrow.Record) (err error) {\n", entityClsName)
		if err != nil {
			return
		}
		const tmpl = `	if inst.%s != nil {
		err = inst.%s.LoadFromRecord(rec)
		if err != nil {
			err = eb.Build().Str("tableName",%q).Str("fieldName",%q).Errorf("unable to load from record: %%w", err)
			return
		}
	}
`
		for _, pt := range ptsEff {
			sectionName := naming.MustBeValidStylableName(pt.String()).Convert(naming.UpperCamelCase)
			_, err = fmt.Fprintf(b,
				tmpl,
				sectionName,
				sectionName,
				tableName,
				sectionName,
			)
			if err != nil {
				return
			}
		}

		for _, s := range tblDesc.TaggedValuesSections {
			sectionName := s.Name.Convert(naming.UpperCamelCase)
			_, err = fmt.Fprintf(b,
				tmpl,
				sectionName,
				sectionName,
				tableName,
				sectionName,
			)
			if err != nil {
				return
			}
		}
		_, err = fmt.Fprint(b, "\treturn\n}\n\n")
		if err != nil {
			return
		}
	}
	{ // .SetColumnIndices(indices []uint32)
		_, err = fmt.Fprintf(b, "func (inst *%s) SetColumnIndices(indices []uint32) (rest []uint32) {\n\trest = indices\n", entityClsName)
		if err != nil {
			return
		}
		const tmpl = `	if inst.%s != nil {
		rest = inst.%s.SetColumnIndices(rest)
	}
`
		for _, pt := range ptsEff {
			sectionName := naming.MustBeValidStylableName(pt.String()).Convert(naming.UpperCamelCase)
			_, err = fmt.Fprintf(b,
				tmpl,
				sectionName,
				sectionName,
			)
			if err != nil {
				return
			}
		}

		for _, s := range tblDesc.TaggedValuesSections {
			sectionName := s.Name.Convert(naming.UpperCamelCase)
			_, err = fmt.Fprintf(b,
				tmpl,
				sectionName,
				sectionName,
			)
			if err != nil {
				return
			}
		}
		_, err = fmt.Fprint(b, "\treturn\n}\n\n")
		if err != nil {
			return
		}
	}
	{ // .GetColumnIndices() (columnIndices []uint32)
		_, err = fmt.Fprintf(b, "func (inst *%s) GetColumnIndices() (columnIndices []uint32) {\n", entityClsName)
		if err != nil {
			return
		}
		const tmpl = `	if inst.%s != nil {
		columnIndices = slices.Concat(columnIndices, inst.%s.GetColumnIndices())
	}
`
		for _, pt := range ptsEff {
			sectionName := naming.MustBeValidStylableName(pt.String()).Convert(naming.UpperCamelCase)
			_, err = fmt.Fprintf(b,
				tmpl,
				sectionName,
				sectionName,
			)
			if err != nil {
				return
			}
		}

		for _, s := range tblDesc.TaggedValuesSections {
			sectionName := s.Name.Convert(naming.UpperCamelCase)
			_, err = fmt.Fprintf(b,
				tmpl,
				sectionName,
				sectionName,
			)
			if err != nil {
				return
			}
		}
		_, err = fmt.Fprint(b, "\treturn\n}\n\n")
		if err != nil {
			return
		}
	}
	{ // .GetColumnIndexFieldNames() (fieldNames []string)
		_, err = fmt.Fprintf(b, "func (inst *%s) GetColumnIndexFieldNames() (fieldNames []string) {\n", entityClsName)
		if err != nil {
			return
		}
		const tmpl = `	if inst.%s != nil {
		fieldNames = slices.Concat(fieldNames, inst.%s.GetColumnIndexFieldNames())
	}
`
		for _, pt := range ptsEff {
			sectionName := naming.MustBeValidStylableName(pt.String()).Convert(naming.UpperCamelCase)
			_, err = fmt.Fprintf(b,
				tmpl,
				sectionName,
				sectionName,
			)
			if err != nil {
				return
			}
		}

		for _, s := range tblDesc.TaggedValuesSections {
			sectionName := s.Name.Convert(naming.UpperCamelCase)
			_, err = fmt.Fprintf(b,
				tmpl,
				sectionName,
				sectionName,
			)
			if err != nil {
				return
			}
		}
		_, err = fmt.Fprintf(b, "\treturn\n}\n\nvar _ runtime.ColumnIndexHandlingI = (*%s)(nil)\n\n", entityClsName)
		if err != nil {
			return
		}
	}
	{ // .GetNumberOfEntities()
		fieldName := ""
		for _, pt := range ptsEff {
			fieldName = naming.MustBeValidStylableName(pt.String()).Convert(naming.UpperCamelCase).String()
			break
		}

		if fieldName == "" {
			for _, s := range tblDesc.TaggedValuesSections {
				fieldName = s.Name.Convert(naming.UpperCamelCase).String()
				break
			}
		}
		if fieldName == "" {
			err = eh.Errorf("no plain and no tagged section")
			return
		}
		_, err = fmt.Fprintf(b, `func (inst *%s) GetNumberOfEntities() (nEntities int) {
	if inst.%s != nil {
		nEntities = inst.%s.Len()
	}
	return
}
`,
			entityClsName,
			fieldName,
			fieldName,
		)
		if err != nil {
			return
		}
	}

	return
}
func (inst *GoClassBuilder) ComposeEntityClassAndFactoryCode(clsNamer gocodegen.GoClassNamerI, tableName naming.StylableName, sectionNames []naming.StylableName, ir *common.IntermediateTableRepresentation, tableRowConfig common.TableRowConfigE, entityIRH *common.IntermediatePairHolder) (err error) {
	err = inst.composeSectionClasses(clsNamer, tableName, sectionNames, ir, tableRowConfig, entityIRH)
	if err != nil {
		err = eh.Errorf("unable to compose section classes: %w", err)
		return
	}
	err = inst.composeEntityClasses(clsNamer, tableName, sectionNames, ir, tableRowConfig, entityIRH)
	if err != nil {
		err = eh.Errorf("unable to compose entity classes: %w", err)
		return
	}

	return
}
func (inst *GoClassBuilder) ComposeEntityCode(clsNamer gocodegen.GoClassNamerI, tableName naming.StylableName, sectionNames []naming.StylableName, ir *common.IntermediateTableRepresentation, tableRowConfig common.TableRowConfigE, entityIRH *common.IntermediatePairHolder) (err error) {
	return
}
func (inst *GoClassBuilder) ComposeGoImports(ir *common.IntermediateTableRepresentation, tableRowConfig common.TableRowConfigE, suppressedImports *containers.HashSet[string]) (err error) {
	b := inst.builder
	if b == nil {
		err = common.ErrNoBuilder
		return
	}
	imports := containers.NewHashSet[string](32)

	for _, cp := range ir.IterateColumnProps() {
		for i, ct := range cp.CanonicalType {
			var imp []string
			_, _, imp, err = codegen.GenerateGoCode(ct, cp.EncodingHints[i])
			if err != nil {
				err = eb.Build().Stringer("canonicalType", ct).Errorf("unable to generate go code for canonical type: %w", err)
				return
			}
			for _, im := range imp {
				if imports.Has(im) || (suppressedImports != nil && suppressedImports.Has(im)) {
					continue
				}
				imports.Add(im)
				gocodegen.EmitGeneratingCodeLocation(b)
				_, err = fmt.Fprintf(b, "\t%q\n", im)
				if err != nil {
					return
				}
			}
		}
	}
	for _, im := range []string{} {
		_, err = fmt.Fprintf(b, "\t%q\n", im)
		if err != nil {
			return
		}
	}
	return
}

var _ gocodegen.CodeComposerI = (*GoClassBuilder)(nil)
var _ common.CodeBuilderHolderI = (*GoClassBuilder)(nil)
