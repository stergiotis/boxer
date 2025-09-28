package readaccess

import (
	"cmp"
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

func NewGoClassBuilder() *GoClassBuilder {
	return &GoClassBuilder{
		builder: nil,
		tech:    golang.NewTechnologySpecificCodeGenerator(),
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
				_, err = fmt.Fprintf(b, "\tinst.%s = runtime.NewRandomAccessTwoLevelLookupAccel[runtime.Membership%sIdx,runtime.AttributeIdx,int,int64](128)\n",
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

					_, err = fmt.Fprintf(b, "\tinst.%s = runtime.NewRandomAccessTwoLevelLookupAccel[runtime.Membership%sIdx,runtime.AttributeIdx,int,int64](128)\n",
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
				/*err = colIdxGen.GenerateCommonNameBased(b, clsName, inst.physicalColumns)
				if err != nil {
					return
				}*/
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
	//inst.Release()
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
				const tmpl = `	{
		err = runtime.LoadNonScalarValueFieldFromRecord(int(inst.%s),arrow.%s,rec,&inst.%s,&inst.%s,array.New%sData)
		if err != nil {
			return
		}
	}
	{
		err = runtime.LoadAccelFieldFromRecord(int(inst.%sAccel),rec,inst.%s)
		if err != nil {
			return
		}
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
func (inst *GoClassBuilder) composeSectionInnerClasses(attrClassesKv *containers.BinarySearchGrowingKV[string, *strings.Builder], clsNamer gocodegen.GoClassNamerI, tableName naming.StylableName, sectionNames []naming.StylableName, ir *common.IntermediateTableRepresentation, tableRowConfig common.TableRowConfigE) (err error) {
	b := inst.builder
	gocodegen.EmitGeneratingCodeLocation(b)
	colIdxGenerators := containers.NewBinarySearchGrowingKV[string, *ColumnIndexCodeGenerator](attrClassesKv.Len(), strings.Compare)
	getColIdxGenerator := func(cc common.IntermediateColumnContext) (gen *ColumnIndexCodeGenerator) {
		var has bool
		var clsName string
		clsName, err = clsNamer.ComposeSectionReadAccessInnerClassName(tableName, cc.PlainItemType, cc.SectionName, cc.SubType)
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
		clsName, err = clsNamer.ComposeSectionReadAccessInnerClassName(tableName, cc.PlainItemType, cc.SectionName, cc.SubType)
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
	emptyAccelFieldName := clsNamer.ComposeAccelFieldName("")
	emptyColumnIndexFieldName := clsNamer.ComposeColumnIndexFieldName("")
	{ // inner classes: struct
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
					switch role {
					case common.ColumnRoleCardinality:
						_, err = fmt.Fprintf(bc, "\t%s *runtime.RandomAccessTwoLevelLookupAccel[runtime.AttributeIdx,runtime.SetIdx,int,int64]\n\t%s uint32\n",
							emptyAccelFieldName,
							emptyColumnIndexFieldName)
						if err != nil {
							return
						}
						break
					case common.ColumnRoleLength:
						_, err = fmt.Fprintf(bc, "\t%s *runtime.RandomAccessTwoLevelLookupAccel[runtime.AttributeIdx,runtime.HomogenousArrayIdx,int,int64]\n\t%s uint32\n",
							emptyAccelFieldName,
							emptyColumnIndexFieldName)
						if err != nil {
							return
						}
						break
					default:
						err = eb.Build().Stringer("role", role).Stringer("subtype", cc.SubType).Errorf("unhandled role")
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
	{ // inner classes: New
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
					switch role {
					case common.ColumnRoleCardinality:
						colIdxGen.AddField(emptyColumnIndexFieldName, cc.IndexOffset+uint32(i))
						_, err = fmt.Fprintf(bc, "\tinst.%s = runtime.NewRandomAccessTwoLevelLookupAccel[runtime.AttributeIdx,runtime.SetIdx,int,int64](128)\n",
							emptyAccelFieldName)
						if err != nil {
							return
						}
						break
					case common.ColumnRoleLength:
						colIdxGen.AddField(emptyColumnIndexFieldName, cc.IndexOffset+uint32(i))
						_, err = fmt.Fprintf(bc, "\tinst.%s = runtime.NewRandomAccessTwoLevelLookupAccel[runtime.AttributeIdx,runtime.HomogenousArrayIdx,int,int64](128)\n",
							emptyAccelFieldName)
						if err != nil {
							return
						}
						break
					default:
						err = eb.Build().Stringer("role", role).Stringer("subtype", cc.SubType).Errorf("unhandled role")
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
	{ // inner class: .Reset()
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
	{ // inner class: .Release()
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
				_, err = bc.WriteString("\t// nothing to release\n")
				if err != nil {
					return
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

	{ // inner class: .Len()
		handledSections := containers.NewHashSet[string](len(sectionNames) + len(common.AllPlainItemTypes))
		for cc, cp := range ir.IterateColumnProps() {
			key := cc.PlainItemType.String() + "|" + cc.SectionName.String()
			if handledSections.Has(key) {
				//continue
			}
			var clsName string
			clsName, err = clsNamer.ComposeSectionReadAccessInnerClassName(tableName, cc.PlainItemType, cc.SectionName, cc.SubType)
			if err != nil {
				err = eh.Errorf("unable to compose read access inner class name: %w", err)
				return
			}

			var f string
			switch cc.SubType {
			case common.IntermediateColumnsSubTypeHomogenousArraySupport, common.IntermediateColumnsSubTypeSetSupport:
				f = emptyAccelFieldName
				break
			case common.IntermediateColumnsSubTypeScalar, common.IntermediateColumnsSubTypeHomogenousArray, common.IntermediateColumnsSubTypeSet:
				for _, colName := range cp.Names {
					f = clsNamer.ComposeValueField(colName.Convert(naming.UpperCamelCase).String())
					//handledSections.Add(key)
					break
				}
				break
			}
			if f != "" {
				_, err = fmt.Fprintf(b, `func (inst *%s) Len() (l int) {
	if inst.%s != nil {
		l = inst.%s.Len()
	}
	return
}
`,
					clsName,
					f,
					f)
				if err != nil {
					return
				}
			}
		}
	}
	{ // inner classes:  .LoadFromRecord(rec arrow.Record, row int) (err error)
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
								_, err = fmt.Fprintf(bc, `	{
		err = runtime.LoadNonScalarValueFieldFromRecord(int(inst.%s),arrow.%s,rec,&inst.%s,&inst.%s,array.New%sData)
		if err != nil {
			return
		}
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
								_, err = fmt.Fprintf(bc, `	{
		err = runtime.LoadScalarValueFieldFromRecord(int(inst.%s),arrow.%s,rec,&inst.%s,array.New%sData)
		if err != nil {
			return
		}
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
					switch role {
					case common.ColumnRoleCardinality, common.ColumnRoleLength:
						_, err = fmt.Fprint(bc, `	{
		err = runtime.LoadAccelFieldFromRecord(int(inst.ColumnIndex),rec,inst.Accel)
		if err != nil {
			return
		}
	}
`)
						if err != nil {
							return
						}
						break
					default:
						err = eb.Build().Stringer("role", role).Stringer("subtype", cc.SubType).Errorf("unhandled role")
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
	return
}
func (inst *GoClassBuilder) composeSectionClasses(clsNamer gocodegen.GoClassNamerI, tableName naming.StylableName, sectionNames []naming.StylableName, ir *common.IntermediateTableRepresentation, tableRowConfig common.TableRowConfigE, entityIRH *common.IntermediatePairHolder) (err error, kv *containers.BinarySearchGrowingKV[common.PlainItemTypeE, []common.IntermediateColumnSubTypeE]) {
	b := inst.builder
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

	attrClassesKv := containers.NewBinarySearchGrowingKV[string, *strings.Builder](len(sectionNames)+len(common.AllPlainItemTypes), strings.Compare)
	composeFieldName := func(st common.IntermediateColumnSubTypeE) (fieldNamePrefix string, err error) {
		switch st {
		case common.IntermediateColumnsSubTypeScalar, common.IntermediateColumnsSubTypeHomogenousArray, common.IntermediateColumnsSubTypeSet:
			t := naming.MustBeValidStylableName(st.String()).Convert(naming.UpperCamelCase).String()
			fieldNamePrefix = clsNamer.ComposeValueField(t)
			return
		case common.IntermediateColumnsSubTypeHomogenousArraySupport:
			fieldNamePrefix = "SupportHomogenousArray"
			break
		case common.IntermediateColumnsSubTypeSetSupport:
			fieldNamePrefix = "SupportSet"
			break
		default:
			err = eb.Build().Stringer("subType", st).Errorf("unhandled sub type")
			return
		}
		return
	}

	{ // inner section classes
		err = inst.composeSectionInnerClasses(attrClassesKv, clsNamer, tableName, sectionNames, ir, tableRowConfig)
		if err != nil {
			err = eh.Errorf("unable to compose inner section classes: %w", err)
			return
		}
	}

	{ // section class: struct
		gocodegen.EmitGeneratingCodeLocation(b)
		var sectionToClassNames []string
		_, _, sectionToClassNames, err = ComposeMembershipPackInfo(tblDesc, clsNamer)
		if err != nil {
			err = eh.Errorf("unable to compose membership pack info: %w", err)
			return
		}
		var subTypes = []common.IntermediateColumnSubTypeE{
			common.IntermediateColumnsSubTypeScalar,
			common.IntermediateColumnsSubTypeHomogenousArray,
			common.IntermediateColumnsSubTypeSet,
			common.IntermediateColumnsSubTypeHomogenousArraySupport,
			common.IntermediateColumnsSubTypeSetSupport,
		}

		kv = containers.NewBinarySearchGrowingKV[common.PlainItemTypeE, []common.IntermediateColumnSubTypeE](len(common.AllPlainItemTypes), cmp.Compare)
		subTypeSet := containers.NewHashSet[common.IntermediateColumnSubTypeE](len(common.AllIntermediateColumnsSubTypes))
		for _, pt := range common.AllPlainItemTypes {
			if !slices.Contains(tblDesc.PlainValuesItemTypes, pt) {
				continue
			}
			sectionName := naming.MustBeValidStylableName(pt.String())
			subTypeSet.Clear()
			for _, pt2 := range tblDesc.PlainValuesItemTypes {
				if pt != pt2 {
					continue
				}
				for _, st := range subTypes {
					var innerClsName string
					innerClsName, err = clsNamer.ComposeSectionReadAccessInnerClassName(tableName, pt, sectionName, st)
					if err != nil {
						err = eh.Errorf("unable to compose read access inner class name: %w", err)
						return
					}
					if attrClassesKv.Has(innerClsName) {
						subTypeSet.Add(st)
					}
				}
			}
			subTypeSlice := subTypeSet.SliceEx(nil)
			slices.Sort(subTypeSlice)
			kv.UpsertSingle(pt, subTypeSlice)
		}

		{ // struct (plain)
			for pt, subTypeSlice := range kv.Iterate() {
				sectionName := naming.MustBeValidStylableName(pt.String())
				var outerClsName string
				outerClsName, err = clsNamer.ComposeSectionReadAccessOuterClassName(tableName, pt, sectionName)
				if err != nil {
					err = eh.Errorf("unable to generate outer class name: %w", err)
					return
				}
				_, err = fmt.Fprintf(b, "type %s struct {\n", outerClsName)
				if err != nil {
					return
				}
				for _, st := range subTypeSlice {
					var innerClsName string
					innerClsName, err = clsNamer.ComposeSectionReadAccessInnerClassName(tableName, pt, sectionName, st)
					if err != nil {
						err = eh.Errorf("unable to compose read access inner class name: %w", err)
						return
					}
					var fieldName string
					fieldName, err = composeFieldName(st)
					if err != nil {
						err = eh.Errorf("unable to compose field name prefix: %w", err)
						return
					}
					_, err = fmt.Fprintf(b, "\t%s *%s\n", fieldName, innerClsName)
					if err != nil {
						return
					}
				}
				_, err = fmt.Fprint(b, "}\n\n")
				if err != nil {
					return
				}
			}
		}
		{ // factory
			for pt, subTypeSlice := range kv.Iterate() {
				sectionName := naming.MustBeValidStylableName(pt.String())
				var outerClsName string
				outerClsName, err = clsNamer.ComposeSectionReadAccessOuterClassName(tableName, pt, sectionName)
				if err != nil {
					err = eh.Errorf("unable to generate outer class name: %w", err)
					return
				}
				_, err = fmt.Fprintf(b, "func New%s() (inst *%s) {\n\tinst = &%s{}\n",
					outerClsName,
					outerClsName,
					outerClsName)
				if err != nil {
					return
				}
				for _, st := range subTypeSlice {
					var innerClsName string
					innerClsName, err = clsNamer.ComposeSectionReadAccessInnerClassName(tableName, pt, sectionName, st)
					if err != nil {
						err = eh.Errorf("unable to compose read access inner class name: %w", err)
						return
					}
					var fieldName string
					fieldName, err = composeFieldName(st)
					if err != nil {
						err = eh.Errorf("unable to compose field name prefix: %w", err)
						return
					}
					_, err = fmt.Fprintf(b, "\tinst.%s = New%s()\n", fieldName, innerClsName)
					if err != nil {
						return
					}
				}
				_, err = fmt.Fprint(b, "\treturn\n}\n\n")
				if err != nil {
					return
				}
			}
		}

		{ // .SetColumnIndices(indices []uint32) (restIndices []uint32)
			for pt, subTypeSlice := range kv.Iterate() {
				sectionName := naming.MustBeValidStylableName(pt.String())
				var outerClsName string
				outerClsName, err = clsNamer.ComposeSectionReadAccessOuterClassName(tableName, pt, sectionName)
				if err != nil {
					err = eh.Errorf("unable to compose read access outer class name: %w", err)
					return
				}
				_, err = fmt.Fprintf(b, "func (inst *%s) SetColumnIndices(indices []uint32) (restIndices []uint32) {\n\trestIndices = indices\n", outerClsName)
				if err != nil {
					return
				}
				for _, st := range subTypeSlice {
					var fieldName string
					fieldName, err = composeFieldName(st)
					if err != nil {
						err = eh.Errorf("unable to compose field name prefix: %w", err)
						return
					}
					_, err = fmt.Fprintf(b, "\trestIndices = slices.Concat(restIndices, inst.%s.SetColumnIndices(restIndices))\n", fieldName)
					if err != nil {
						return
					}
				}
				_, err = fmt.Fprint(b, "\treturn\n}\n\n")
				if err != nil {
					return
				}
			}
		}
		{ // .GetColumnIndices() (columnIndices []uint32)
			for pt, subTypeSlice := range kv.Iterate() {
				sectionName := naming.MustBeValidStylableName(pt.String())
				var outerClsName string
				outerClsName, err = clsNamer.ComposeSectionReadAccessOuterClassName(tableName, pt, sectionName)
				if err != nil {
					err = eh.Errorf("unable to compose read access outer class name: %w", err)
					return
				}
				_, err = fmt.Fprintf(b, "func (inst *%s) GetColumnIndices() (columnIndices []uint32) {\n", outerClsName)
				if err != nil {
					return
				}
				for _, st := range subTypeSlice {
					var fieldName string
					fieldName, err = composeFieldName(st)
					if err != nil {
						err = eh.Errorf("unable to compose field name prefix: %w", err)
						return
					}
					_, err = fmt.Fprintf(b, "\tcolumnIndices = slices.Concat(columnIndices, inst.%s.GetColumnIndices())\n", fieldName)
					if err != nil {
						return
					}
				}
				_, err = fmt.Fprint(b, "\treturn\n}\n\n")
				if err != nil {
					return
				}
			}
		}
		{ // .GetColumnIndexFieldNames() (fieldNames []string)
			for pt, subTypeSlice := range kv.Iterate() {
				sectionName := naming.MustBeValidStylableName(pt.String())
				var outerClsName string
				outerClsName, err = clsNamer.ComposeSectionReadAccessOuterClassName(tableName, pt, sectionName)
				if err != nil {
					err = eh.Errorf("unable to compose read access outer class name: %w", err)
					return
				}
				_, err = fmt.Fprintf(b, "func (inst *%s) GetColumnIndexFieldNames() (fieldNames []string) {\n", outerClsName)
				if err != nil {
					return
				}
				for _, st := range subTypeSlice {
					var fieldName string
					fieldName, err = composeFieldName(st)
					if err != nil {
						err = eh.Errorf("unable to compose field name prefix: %w", err)
						return
					}
					_, err = fmt.Fprintf(b, "\tfieldNames = slices.Concat(fieldNames, inst.%s.GetColumnIndexFieldNames())\n", fieldName)
					if err != nil {
						return
					}
				}
				_, err = fmt.Fprintf(b, "\treturn\n}\n\nvar _ runtime.ColumnIndexHandlingI = (*%s)(nil)\n\n", outerClsName)
				if err != nil {
					return
				}
			}
		}

		{ // plain .Release()
			for pt, subTypeSlice := range kv.Iterate() {
				sectionName := naming.MustBeValidStylableName(pt.String())
				var outerClsName string
				outerClsName, err = clsNamer.ComposeSectionReadAccessOuterClassName(tableName, pt, sectionName)
				if err != nil {
					err = eh.Errorf("unable to compose read access outer class name: %w", err)
					return
				}
				_, err = fmt.Fprintf(b, "func (inst *%s) Release() {\n", outerClsName)
				if err != nil {
					return
				}
				for _, st := range subTypeSlice {
					var fieldName string
					fieldName, err = composeFieldName(st)
					if err != nil {
						err = eh.Errorf("unable to compose field name prefix: %w", err)
						return
					}
					_, err = fmt.Fprintf(b, "\truntime.ReleaseIfNotNil(inst.%s)\n", fieldName)
					if err != nil {
						return
					}
				}
				_, err = fmt.Fprint(b, "}\n\n")
				if err != nil {
					return
				}
			}
		}

		{ // .LoadFromRecord(rec arrow.Record) (err error)
			for pt, subTypeSlice := range kv.Iterate() {
				sectionName := naming.MustBeValidStylableName(pt.String())
				var outerClsName string
				outerClsName, err = clsNamer.ComposeSectionReadAccessOuterClassName(tableName, pt, sectionName)
				if err != nil {
					err = eh.Errorf("unable to compose read access outer class name: %w", err)
					return
				}
				_, err = fmt.Fprintf(b, "func (inst *%s) LoadFromRecord(rec arrow.Record) (err error) {\n", outerClsName)
				if err != nil {
					return
				}
				for _, st := range subTypeSlice {
					var fieldName string
					fieldName, err = composeFieldName(st)
					if err != nil {
						err = eh.Errorf("unable to compose field name prefix: %w", err)
						return
					}
					_, err = fmt.Fprintf(b, `	err = inst.%s.LoadFromRecord(rec)
	if err != nil {
		err = eb.Build().Str("fieldName",%q).Errorf("unable to load from record: %%w", err)
		return
	}
`,
						fieldName,
						fieldName,
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
		}

		{ // struct
			for i, s := range tblDesc.TaggedValuesSections {
				const pt = common.PlainItemTypeNone
				var outerClsName string
				outerClsName, err = clsNamer.ComposeSectionReadAccessOuterClassName(tableName, pt, s.Name)
				if err != nil {
					err = eh.Errorf("unable to compose read access outer class name: %w", err)
					return
				}
				_, err = fmt.Fprintf(b, "type %s struct {\n", outerClsName)
				if err != nil {
					return
				}
				for _, st := range subTypes {
					var innerClsName string
					innerClsName, err = clsNamer.ComposeSectionReadAccessInnerClassName(tableName, pt, s.Name, st)
					if err != nil {
						err = eh.Errorf("unable to compose read access inner class name: %w", err)
						return
					}
					if attrClassesKv.Has(innerClsName) {
						var fieldName string
						fieldName, err = composeFieldName(st)
						if err != nil {
							err = eh.Errorf("unable to compose field name prefix: %w", err)
							return
						}
						_, err = fmt.Fprintf(b, "\t%s *%s\n", fieldName, innerClsName)
						if err != nil {
							return
						}
					}
				}
				{
					membPackClsName := sectionToClassNames[i]
					if membPackClsName != "" {
						_, err = fmt.Fprintf(b, "\tMembership *%s\n", membPackClsName)
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
		{ // factory
			for i, s := range tblDesc.TaggedValuesSections {
				const pt = common.PlainItemTypeNone
				var outerClsName string
				outerClsName, err = clsNamer.ComposeSectionReadAccessOuterClassName(tableName, pt, s.Name)
				if err != nil {
					err = eh.Errorf("unable to compose read access outer class name: %w", err)
					return
				}
				_, err = fmt.Fprintf(b, "func New%s() (inst *%s) {\n\tinst = &%s{}\n",
					outerClsName,
					outerClsName,
					outerClsName,
				)
				if err != nil {
					return
				}
				for _, st := range subTypes {
					var innerClsName string
					innerClsName, err = clsNamer.ComposeSectionReadAccessInnerClassName(tableName, pt, s.Name, st)
					if err != nil {
						err = eh.Errorf("unable to compose read access inner class name: %w", err)
						return
					}
					if attrClassesKv.Has(innerClsName) {
						var fieldName string
						fieldName, err = composeFieldName(st)
						if err != nil {
							err = eh.Errorf("unable to compose field name prefix: %w", err)
							return
						}
						_, err = fmt.Fprintf(b, "\tinst.%s = New%s()\n", fieldName, innerClsName)
						if err != nil {
							return
						}
					}
				}
				{
					membPackClsName := sectionToClassNames[i]
					if membPackClsName != "" {
						_, err = fmt.Fprintf(b, "\tinst.Membership = New%s%s()\n",
							membPackClsName,
							s.Name.Convert(naming.UpperCamelCase),
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

		{ // .SetColumnIndices(indices []uint32) (restIndices []uint32)
			for i, s := range tblDesc.TaggedValuesSections {
				const pt = common.PlainItemTypeNone
				var outerClsName string
				outerClsName, err = clsNamer.ComposeSectionReadAccessOuterClassName(tableName, pt, s.Name)
				if err != nil {
					err = eh.Errorf("unable to compose read access outer class name: %w", err)
					return
				}
				_, err = fmt.Fprintf(b, "func (inst *%s) SetColumnIndices(indices []uint32) (restIndices []uint32) {\n\trestIndices = indices\n", outerClsName)
				if err != nil {
					return
				}
				for _, st := range subTypes {
					var innerClsName string
					innerClsName, err = clsNamer.ComposeSectionReadAccessInnerClassName(tableName, pt, s.Name, st)
					if err != nil {
						err = eh.Errorf("unable to compose read access inner class name: %w", err)
						return
					}
					if attrClassesKv.Has(innerClsName) {
						var fieldName string
						fieldName, err = composeFieldName(st)
						if err != nil {
							err = eh.Errorf("unable to compose field name prefix: %w", err)
							return
						}
						_, err = fmt.Fprintf(b, `	if inst.%s != nil {
		restIndices = inst.%s.SetColumnIndices(restIndices)
	}
`,
							fieldName,
							fieldName)
						if err != nil {
							return
						}
					}
				}
				{
					membPackClsName := sectionToClassNames[i]
					if membPackClsName != "" {
						_, err = fmt.Fprint(b, `	if inst.Membership != nil {
		restIndices = inst.Membership.SetColumnIndices(restIndices)
	}
`)
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

		{ // .GetColumnIndices() (columnIndices []uint32)
			for i, s := range tblDesc.TaggedValuesSections {
				const pt = common.PlainItemTypeNone
				var outerClsName string
				outerClsName, err = clsNamer.ComposeSectionReadAccessOuterClassName(tableName, pt, s.Name)
				if err != nil {
					err = eh.Errorf("unable to compose read access outer class name: %w", err)
					return
				}
				_, err = fmt.Fprintf(b, "func (inst *%s) \tGetColumnIndices() (columnIndices []uint32) {\n", outerClsName)
				if err != nil {
					return
				}
				for _, st := range subTypes {
					var innerClsName string
					innerClsName, err = clsNamer.ComposeSectionReadAccessInnerClassName(tableName, pt, s.Name, st)
					if err != nil {
						err = eh.Errorf("unable to compose read access inner class name: %w", err)
						return
					}
					if attrClassesKv.Has(innerClsName) {
						var fieldName string
						fieldName, err = composeFieldName(st)
						if err != nil {
							err = eh.Errorf("unable to compose field name prefix: %w", err)
							return
						}
						_, err = fmt.Fprintf(b, `	if inst.%s != nil {
		columnIndices = slices.Concat(columnIndices, inst.%s.GetColumnIndices())
	}
`,
							fieldName,
							fieldName)
						if err != nil {
							return
						}
					}
				}
				{
					membPackClsName := sectionToClassNames[i]
					if membPackClsName != "" {
						_, err = fmt.Fprint(b, `	if inst.Membership != nil {
		columnIndices = slices.Concat(columnIndices, inst.Membership.GetColumnIndices())
	}
`)
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

		{ // .GetColumnIndexFieldNames() (columnIndexFieldNames []string)
			for i, s := range tblDesc.TaggedValuesSections {
				const pt = common.PlainItemTypeNone
				var outerClsName string
				outerClsName, err = clsNamer.ComposeSectionReadAccessOuterClassName(tableName, pt, s.Name)
				if err != nil {
					err = eh.Errorf("unable to compose read access outer class name: %w", err)
					return
				}
				_, err = fmt.Fprintf(b, "func (inst *%s) \tGetColumnIndexFieldNames() (columnIndexFieldNames []string) {\n", outerClsName)
				if err != nil {
					return
				}
				for _, st := range subTypes {
					var innerClsName string
					innerClsName, err = clsNamer.ComposeSectionReadAccessInnerClassName(tableName, pt, s.Name, st)
					if err != nil {
						err = eh.Errorf("unable to compose read access inner class name: %w", err)
						return
					}
					if attrClassesKv.Has(innerClsName) {
						var fieldName string
						fieldName, err = composeFieldName(st)
						if err != nil {
							err = eh.Errorf("unable to compose field name prefix: %w", err)
							return
						}
						_, err = fmt.Fprintf(b, `	if inst.%s != nil {
		columnIndexFieldNames = slices.Concat(columnIndexFieldNames, inst.%s.GetColumnIndexFieldNames())
	}
`,
							fieldName,
							fieldName)
						if err != nil {
							return
						}
					}
				}
				{
					membPackClsName := sectionToClassNames[i]
					if membPackClsName != "" {
						_, err = fmt.Fprint(b, `	if inst.Membership != nil {
		columnIndexFieldNames = slices.Concat(columnIndexFieldNames, inst.Membership.GetColumnIndexFieldNames())
	}
`)
						if err != nil {
							return
						}
					}
				}
				_, err = fmt.Fprintf(b, "\treturn\n}\n\nvar _ runtime.ColumnIndexHandlingI = (*%s)(nil)\n\n", outerClsName)
				if err != nil {
					return
				}
			}
		}
		{ // .Release()
			for i, s := range tblDesc.TaggedValuesSections {
				const pt = common.PlainItemTypeNone
				var outerClsName string
				outerClsName, err = clsNamer.ComposeSectionReadAccessOuterClassName(tableName, pt, s.Name)
				if err != nil {
					err = eh.Errorf("unable to compose read access outer class name: %w", err)
					return
				}
				_, err = fmt.Fprintf(b, "func (inst *%s) Release() {\n", outerClsName)
				if err != nil {
					return
				}
				for _, st := range subTypes {
					var innerClsName string
					innerClsName, err = clsNamer.ComposeSectionReadAccessInnerClassName(tableName, pt, s.Name, st)
					if err != nil {
						err = eh.Errorf("unable to compose read access inner class name: %w", err)
						return
					}
					if attrClassesKv.Has(innerClsName) {
						var fieldName string
						fieldName, err = composeFieldName(st)
						if err != nil {
							err = eh.Errorf("unable to compose field name prefix: %w", err)
							return
						}
						_, err = fmt.Fprintf(b, "\truntime.ReleaseIfNotNil(inst.%s)\n", fieldName)
						if err != nil {
							return
						}
					}
				}
				{
					membPackClsName := sectionToClassNames[i]
					if membPackClsName != "" {
						_, err = fmt.Fprint(b, "\truntime.ReleaseIfNotNil(inst.Membership)\n")
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
			for i, s := range tblDesc.TaggedValuesSections {
				const pt = common.PlainItemTypeNone
				var outerClsName string
				outerClsName, err = clsNamer.ComposeSectionReadAccessOuterClassName(tableName, pt, s.Name)
				if err != nil {
					err = eh.Errorf("unable to compose read access outer class name: %w", err)
					return
				}
				_, err = fmt.Fprintf(b, "func (inst *%s) LoadFromRecord(rec arrow.Record) (err error) {\n", outerClsName)
				if err != nil {
					return
				}
				for _, st := range subTypes {
					var innerClsName string
					innerClsName, err = clsNamer.ComposeSectionReadAccessInnerClassName(tableName, pt, s.Name, st)
					if err != nil {
						err = eh.Errorf("unable to compose read access inner class name: %w", err)
						return
					}
					if attrClassesKv.Has(innerClsName) {
						var fieldName string
						fieldName, err = composeFieldName(st)
						if err != nil {
							err = eh.Errorf("unable to compose field name prefix: %w", err)
							return
						}
						_, err = fmt.Fprintf(b, `	err = inst.%s.LoadFromRecord(rec)
	if err != nil {
		err = eb.Build().Str("innerClassName",%q).Errorf("unable to load from record: %%w", err)
		return
	}
`,
							fieldName,
							innerClsName,
						)
						if err != nil {
							return
						}
					}
				}
				{
					membPackClsName := sectionToClassNames[i]
					if membPackClsName != "" {
						_, err = fmt.Fprintf(b, `	err = inst.Membership.LoadFromRecord(rec)
	if err != nil {
		err = eb.Build().Str("innerClassName",%q).Errorf("unable to load from record: %%w", err)
		return
	}
`,
							membPackClsName,
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
	}

	{ // .Len()
		for pt, subTypeSlice := range kv.Iterate() {
			sectionName := naming.MustBeValidStylableName(pt.String())
			var outerClsName string
			outerClsName, err = clsNamer.ComposeSectionReadAccessOuterClassName(tableName, pt, sectionName)
			if err != nil {
				err = eh.Errorf("unable to generate outer class name: %w", err)
				return
			}
			_, err = fmt.Fprintf(b, "func (inst *%s) Len() (nEntities int) {\n", outerClsName)
			if err != nil {
				return
			}
			ok := false
			for _, st := range subTypeSlice {
				var fieldName string
				fieldName, err = composeFieldName(st)
				if err != nil {
					err = eh.Errorf("unable to compose field name prefix: %w", err)
					return
				}
				_, err = fmt.Fprintf(b, "\treturn inst.%s.Len()\n", fieldName)
				if err != nil {
					return
				}
				ok = true
				break
			}
			if !ok {
				err = eb.Build().Stringer("sectionName", sectionName).Errorf("no field for length termination found")
				return
			}
			_, err = fmt.Fprint(b, "}\n\n")
			if err != nil {
				return
			}
		}
	}
	return
}
func (inst *GoClassBuilder) composeEntityClasses(clsNamer gocodegen.GoClassNamerI, tableName naming.StylableName, sectionNames []naming.StylableName, ir *common.IntermediateTableRepresentation, tableRowConfig common.TableRowConfigE, entityIRH *common.IntermediatePairHolder, outerClassKv *containers.BinarySearchGrowingKV[common.PlainItemTypeE, []common.IntermediateColumnSubTypeE]) (err error) {
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
	{ // entity struct
		_, err = fmt.Fprintf(b, "type %s struct {\n", entityClsName)
		if err != nil {
			return
		}
		for pt := range outerClassKv.IterateKeys() {
			sectionName := naming.MustBeValidStylableName(pt.String())
			var outerClsName string
			outerClsName, err = clsNamer.ComposeSectionReadAccessOuterClassName(tableName, pt, sectionName)
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
		for pt := range outerClassKv.IterateKeys() {
			sectionName := naming.MustBeValidStylableName(pt.String())
			var outerClsName string
			outerClsName, err = clsNamer.ComposeSectionReadAccessOuterClassName(tableName, pt, sectionName)
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
		for pt := range outerClassKv.IterateKeys() {
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
		for pt := range outerClassKv.IterateKeys() {
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
		for pt := range outerClassKv.IterateKeys() {
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
		for pt := range outerClassKv.IterateKeys() {
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
		for pt := range outerClassKv.IterateKeys() {
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
		_, err = fmt.Fprintf(b, "func (inst *%s) GetNumberOfEntities() (nEntities int) {\n", entityClsName)
		if err != nil {
			return
		}
		ok := false
		for pt := range outerClassKv.IterateKeys() {
			sectionName := naming.MustBeValidStylableName(pt.String())
			_, err = fmt.Fprintf(b, "\treturn inst.%s.Len()\n",
				sectionName.Convert(naming.UpperCamelCase),
			)
			if err != nil {
				return
			}
			ok = true
			break
		}

		if !ok {
			for _, s := range tblDesc.TaggedValuesSections {
				const pt = common.PlainItemTypeNone
				_, err = fmt.Fprintf(b, "\treturn inst.%s.Len()\n",
					s.Name.Convert(naming.UpperCamelCase),
				)
				if err != nil {
					return
				}
				ok = true
				break
			}
		}
		if !ok {
			err = eh.Errorf("no plain and no tagged section")
			return
		}
		_, err = fmt.Fprint(b, "}\n\n")
		if err != nil {
			return
		}
	}

	return
}
func (inst *GoClassBuilder) ComposeEntityClassAndFactoryCode(clsNamer gocodegen.GoClassNamerI, tableName naming.StylableName, sectionNames []naming.StylableName, ir *common.IntermediateTableRepresentation, tableRowConfig common.TableRowConfigE, entityIRH *common.IntermediatePairHolder) (err error) {
	var kv *containers.BinarySearchGrowingKV[common.PlainItemTypeE, []common.IntermediateColumnSubTypeE]
	err, kv = inst.composeSectionClasses(clsNamer, tableName, sectionNames, ir, tableRowConfig, entityIRH)
	if err != nil {
		err = eh.Errorf("unable to compose section classes: %w", err)
		return
	}
	err = inst.composeEntityClasses(clsNamer, tableName, sectionNames, ir, tableRowConfig, entityIRH, kv)
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
