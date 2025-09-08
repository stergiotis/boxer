package dml

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/ettle/strcase"
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/containers"
	"github.com/stergiotis/boxer/public/functional"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/observability/vcs"
	canonicaltypes2 "github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/codegen"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/arrow"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/golang"
	"github.com/stergiotis/boxer/public/semistructured/leeway/dml/runtime"
	encodingaspects2 "github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
)

var CodeGeneratorName = "Leeway DML (" + vcs.ModuleInfo() + ")"

type codeBuildModeE uint8

const (
	codeBuildModeCode      codeBuildModeE = 0
	codeBuildModeInterface codeBuildModeE = 1
)

type structFieldOperationE uint8
type sectionOperationE uint8

const (
	structFieldOperationDeclaration              structFieldOperationE = 0
	structFieldOperationInitialization           structFieldOperationE = 1
	structFieldOperationAppendScalar             structFieldOperationE = 2
	structFieldOperationAppendContainer          structFieldOperationE = 3
	structFieldOperationArgUse                   structFieldOperationE = 4
	structFieldOperationArgDeclaration           structFieldOperationE = 5
	structFieldOperationArgDeclarationDemoted    structFieldOperationE = 6
	structFieldOperationStoreContainerLength     structFieldOperationE = 7
	structFieldOperationAppendContainerLength    structFieldOperationE = 8
	structFieldOperationPlainDeclaration         structFieldOperationE = 9
	structFieldOperationPlainAssignArg           structFieldOperationE = 10
	structFieldOperationPlainAppend              structFieldOperationE = 11
	structFieldOperationPlainReset               structFieldOperationE = 12
	structFieldOperationIncrementContainerLength structFieldOperationE = 13
	structFieldOperationDeclareContainerLength   structFieldOperationE = 14
	structFieldOperationResetContainerLength     structFieldOperationE = 15
)
const (
	sectionOperationA sectionOperationE = 0
)

type GoClassBuilder struct {
	builder         *strings.Builder
	classNamer      GoClassNamerI
	tech            *golang.TechnologySpecificCodeGenerator
	clsNames        classNames
}

func NewGoClassBuilder() *GoClassBuilder {
	return &GoClassBuilder{
		builder:         nil,
		tech:            golang.NewTechnologySpecificCodeGenerator(),
		clsNames: classNames{
			inEntityClassName:    "",
			inSectionClassName:   "",
			inAttributeClassName: "",
		},
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
func (inst *GoClassBuilder) composeNamingConventionDependentCode(tableName naming.StylableName, ir *common.IntermediateTableRepresentation, namingConvention common.NamingConventionI, tableRowConfig common.TableRowConfigE, clsNamer GoClassNamerI) (err error) {
	b := inst.builder
	arrowTech := arrow.NewTechnologySpecificCodeGenerator()
	arrowTech.SetCodeBuilder(b)
	ddlGenerator := ddl.NewGeneratorDriver()
	{ // schema factory
		var factoryName string
		factoryName, err = clsNamer.ComposeSchemaFactoryName(tableName)
		if err != nil {
			return
		}
		_, err = fmt.Fprintf(b, `func %s(allocator memory.Allocator) (builder *array.RecordBuilder) {
		schema := arrow.NewSchema([]arrow.Field{
`, factoryName)
		if err != nil {
			return
		}

		err = ddlGenerator.GenerateColumnsCode(ir.IterateColumnProps(),
			tableRowConfig,
			namingConvention,
			arrowTech,
			func(hint encodingaspects2.AspectE) (ok bool, msg string) {
				return true, ""
			})
		if err != nil {
			return
		}
		_, err = b.WriteString(
			`		}, nil)
		builder = array.NewRecordBuilder(allocator, schema)
		return
}
`)
		if err != nil {
			return
		}
	}
	return
}

var ErrUnhandledSubType = eh.Errorf("unhandled sub type")
var ErrUnhandledRole = eh.Errorf("unhandled column role")

func (inst *GoClassBuilder) composeSharedMethods(clsName string) (err error) {
	return
}
func (inst *GoClassBuilder) composeSectionRelatedCode(op sectionOperationE, sectionName string) (err error) {
	return
}
func (inst *GoClassBuilder) composeFieldRelatedCodeAll(op structFieldOperationE, iter common.IntermediateColumnIterator, separator string) (err error) {
	first := true
	for cc, cp := range iter {
		for i := 0; i < cp.Length(); i++ {
			if !first && separator != "" {
				_, err = inst.builder.WriteString(separator)
				if err != nil {
					return
				}
			}
			err = inst.composeFieldRelatedCode(op, cc, cp, i)
			if err != nil {
				return
			}
			first = false
		}
	}
	return
}
func (inst *GoClassBuilder) composeFieldRelatedCode(op structFieldOperationE, cc common.IntermediateColumnContext, cp *common.IntermediateColumnProps, i int) (err error) {
	b := inst.builder
	ct := cp.CanonicalType[i]
	encodingHints := cp.EncodingHints[i]
	var arrowBuilderClassName string
	var mayError bool
	arrowBuilderClassName, mayError, err = CanonicalTypeToArrowBuilderClassName(ct, encodingHints)
	if err != nil {
		return
	}
	var arrowConversionPrefix, arrowConversionSuffix string
	arrowConversionPrefix, arrowConversionSuffix, err = GoTypeToArrowType(ct, encodingHints)
	if err != nil {
		return
	}
	idx := cc.IndexOffset + uint32(i)
	argName := cp.Names[i].Convert(naming.NamingStyleLowerCamelCase).String()
	argName += strconv.FormatUint(uint64(idx), 10)
	plainFieldName := "plain" + cp.Names[i].Convert(naming.NamingStyleUpperCamelCase).String()
	plainFieldName += strconv.FormatUint(uint64(idx), 10)

	switch cc.SubType {
	case common.IntermediateColumnsSubTypeHomogenousArraySupport,
		common.IntermediateColumnsSubTypeSetSupport,
		common.IntermediateColumnsSubTypeMembershipSupport:
		break
	case common.IntermediateColumnsSubTypeHomogenousArray,
		common.IntermediateColumnsSubTypeSet,
		common.IntermediateColumnsSubTypeMembership:
		break
	case common.IntermediateColumnsSubTypeScalar:
		break
	default:
		err = ErrUnhandledSubType
		return
	}
	prefix := strcase.ToCamel(cc.SubType.String())

	switch op {
	case structFieldOperationArgUse:
		_, err = b.WriteString(argName)
		break
	case structFieldOperationArgDeclaration:
		_, err = fmt.Fprintf(b, "%s ", argName)
		if err != nil {
			return
		}
		err = inst.tech.GenerateType(cp.CanonicalType[i])
		if err != nil {
			return
		}
		break
	case structFieldOperationArgDeclarationDemoted:
		_, err = fmt.Fprintf(b, "%s ", argName)
		if err != nil {
			return
		}
		cts, _, _ := canonicaltypes2.DemoteToScalars(cp.CanonicalType[i])
		err = inst.tech.GenerateType(cts.(canonicaltypes2.PrimitiveAstNodeI))
		if err != nil {
			return
		}
		break
	case structFieldOperationDeclaration:
		switch cc.SubType {
		case common.IntermediateColumnsSubTypeHomogenousArraySupport,
			common.IntermediateColumnsSubTypeSetSupport,
			common.IntermediateColumnsSubTypeMembershipSupport,
			common.IntermediateColumnsSubTypeHomogenousArray,
			common.IntermediateColumnsSubTypeSet,
			common.IntermediateColumnsSubTypeMembership,
			common.IntermediateColumnsSubTypeScalar:
			if cc.PlainItemType == common.PlainItemTypeNone {
				_, err = fmt.Fprintf(b, `	%sFieldBuilder%03d *array.%sBuilder
	%sListBuilder%03d *array.ListBuilder
`, prefix, idx, arrowBuilderClassName, prefix, idx)
				if err != nil {
					return
				}
			} else {
				if ct.IsScalar() {
					_, err = fmt.Fprintf(b, `	%sFieldBuilder%03d *array.%sBuilder
`, prefix, idx, arrowBuilderClassName)
				} else {
					_, err = fmt.Fprintf(b, `	%sFieldBuilder%03d *array.%sBuilder
	%sListBuilder%03d *array.ListBuilder
`, prefix, idx, arrowBuilderClassName, prefix, idx)
				}
			}
			break
		default:
			err = ErrUnhandledSubType
		}
		break
	case structFieldOperationInitialization:
		switch cc.SubType {
		case common.IntermediateColumnsSubTypeHomogenousArraySupport,
			common.IntermediateColumnsSubTypeSetSupport,
			common.IntermediateColumnsSubTypeMembershipSupport,
			common.IntermediateColumnsSubTypeHomogenousArray,
			common.IntermediateColumnsSubTypeSet,
			common.IntermediateColumnsSubTypeMembership,
			common.IntermediateColumnsSubTypeScalar:
			if cc.PlainItemType == common.PlainItemTypeNone {
				_, err = fmt.Fprintf(b, `	inst.%sFieldBuilder%03d = builder.Field(%d).(*array.ListBuilder).ValueBuilder().(*array.%sBuilder)
	inst.%sListBuilder%03d = builder.Field(%d).(*array.ListBuilder)
`, prefix, idx, idx, arrowBuilderClassName, prefix, idx, idx)
			} else {
				if ct.IsScalar() {
					_, err = fmt.Fprintf(b, `	inst.%sFieldBuilder%03d = builder.Field(%d).(*array.%sBuilder)
`, prefix, idx, idx, arrowBuilderClassName)
				} else {
					_, err = fmt.Fprintf(b, `	inst.%sFieldBuilder%03d = builder.Field(%d).(*array.ListBuilder).ValueBuilder().(*array.%sBuilder)
	inst.%sListBuilder%03d = builder.Field(%d).(*array.ListBuilder)
`, prefix, idx, idx, arrowBuilderClassName, prefix, idx, idx)
				}
			}
			break
		default:
			err = ErrUnhandledSubType
		}
		break
	case structFieldOperationAppendScalar:
		if mayError {
			_, err = fmt.Fprintf(b, `	{
		err := inst.%sFieldBuilder%03d.Append(%s%s%s)
		inst.AppendError(err)
	}
`, prefix, idx, arrowConversionPrefix, argName, arrowConversionSuffix)
		} else {
			_, err = fmt.Fprintf(b, `	inst.%sFieldBuilder%03d.Append(%s%s%s)
`, prefix, idx, arrowConversionPrefix, argName, arrowConversionSuffix)
			if !ct.IsScalar() {
				_, err = fmt.Fprintf(b, `	inst.%sContainerLength%03d++
`, prefix, idx)
			}
		}
		break
	case structFieldOperationDeclareContainerLength:
		_, err = fmt.Fprintf(b, `	
%sContainerLength%03d int
`, prefix, idx)
	case structFieldOperationIncrementContainerLength:
		_, err = fmt.Fprintf(b, `	inst.%sContainerLength%03d++
`, prefix, idx)
		break
	case structFieldOperationResetContainerLength:
		_, err = fmt.Fprintf(b, `	inst.%sContainerLength%03d = 0
`, prefix, idx)
		break
	case structFieldOperationStoreContainerLength:
		_, err = fmt.Fprintf(b, `	l = inst.%sContainerLength%03d
	inst.%sContainerLength%03d = 0
`, prefix, idx, prefix, idx)
		break
	case structFieldOperationAppendContainer:
		if cc.PlainItemType == common.PlainItemTypeNone {
			_, err = fmt.Fprintf(b, `	inst.%sListBuilder%03d.Append(true)
`, prefix, idx)
			if err != nil {
				return
			}
		} else {
			_, err = fmt.Fprintf(b, `	inst.%sListBuilder%03d.Append(true)
`, prefix, idx)
		}
		break
	case structFieldOperationAppendContainerLength:
		// FIXME implement cast to uint64
		_, err = fmt.Fprintf(b, `	inst.%sFieldBuilder%03d.Append(uint64(l))
`, prefix, idx)
		break
	case structFieldOperationPlainDeclaration:
		_, err = fmt.Fprintf(b, `	%s `, plainFieldName)
		if err != nil {
			return
		}
		err = inst.tech.GenerateType(cp.CanonicalType[i])
		if err != nil {
			return
		}
		_, err = b.WriteRune('\n')
		if err != nil {
			return
		}
		break
	case structFieldOperationPlainAssignArg:
		_, err = fmt.Fprintf(b, `	inst.%s = %s
`, plainFieldName, argName)
		break
	case structFieldOperationPlainAppend:
		if ct.IsScalar() {
			_, err = fmt.Fprintf(b, `	inst.%sFieldBuilder%03d.Append(%sinst.%s%s)
`, prefix, idx, arrowConversionPrefix, plainFieldName, arrowConversionSuffix)
		} else {
			_, err = fmt.Fprintf(b, `	inst.%sListBuilder%03d.Append(true)
`, prefix, idx)
			_, err = fmt.Fprintf(b, `	for _, v := range inst.%s {
			inst.%sFieldBuilder%03d.Append(%sv%s)
	}
`, plainFieldName, prefix, idx, arrowConversionPrefix, arrowConversionSuffix)
		}
		break
	case structFieldOperationPlainReset:
		var zeroValueLiteral string
		_, zeroValueLiteral, _, err = codegen.GenerateGoCode(ct, cp.EncodingHints[i])
		if err != nil {
			err = eb.Build().Stringer("canonicalType", ct).Errorf("unable to generate zero value literal: %w", err)
			return
		}
		_, err = fmt.Fprintf(b, `	inst.%s = %s
`, plainFieldName, zeroValueLiteral)
		break
	}

	return
}
func (inst *GoClassBuilder) composeAttributeClassAndFactoryCode(sectionIRH *common.IntermediatePairHolder, tableRowConfig common.TableRowConfigE) (err error) {
	b := inst.builder

	_, err = fmt.Fprintf(b, `type %s struct {
	errs []error
	state runtime.EntityStateE
    parent *%s
`, inst.clsNames.inAttributeClassName, inst.clsNames.inSectionClassName)
	if err != nil {
		return
	}
	err = inst.composeFieldRelatedCodeAll(structFieldOperationDeclaration, sectionIRH.IterateColumnProps(), "")
	if err != nil {
		return
	}
	membershipIRH := sectionIRH.DeriveSubHolder(deriveSubHolderSelectMembership)
	err = inst.composeFieldRelatedCodeAll(structFieldOperationDeclareContainerLength, membershipIRH.IterateColumnProps(), "")
	if err != nil {
		return
	}
	nonScalarIRH := sectionIRH.DeriveSubHolder(deriveSubHolderSelectNonScalar)
	err = inst.composeFieldRelatedCodeAll(structFieldOperationDeclareContainerLength, nonScalarIRH.IterateColumnProps(), "")
	if err != nil {
		return
	}
	_, err = fmt.Fprintf(b, `}

func New%s(builder *array.RecordBuilder, parent *%s) (inst *%s) {
	inst = &%s{}
	inst.errs = make([]error,0,8)
	inst.state = runtime.EntityStateInitial
	inst.parent = parent
`, inst.clsNames.inAttributeClassName,
		inst.clsNames.inSectionClassName,
		inst.clsNames.inAttributeClassName,
		inst.clsNames.inAttributeClassName)
	if err != nil {
		return
	}
	err = inst.composeFieldRelatedCodeAll(structFieldOperationInitialization, sectionIRH.IterateColumnProps(), "")
	if err != nil {
		return
	}
	_, err = b.WriteString(`
	return inst
}
`)
	if err != nil {
		return
	}

	return
}

type classNames struct {
	inEntityClassName    string
	inSectionClassName   string
	inAttributeClassName string
}

func (inst *GoClassBuilder) composeSectionClassAndFactoryCode(sectionIRH *common.IntermediatePairHolder, tableRowConfig common.TableRowConfigE, scalarIRH *common.IntermediatePairHolder, nonScalarIRH *common.IntermediatePairHolder) (err error) {
	b := inst.builder

	_, err = fmt.Fprintf(b, `type %s struct {
	errs []error
    inAttr *%s
	state runtime.EntityStateE
    parent *%s
`, inst.clsNames.inSectionClassName, inst.clsNames.inAttributeClassName, inst.clsNames.inEntityClassName)
	if err != nil {
		return
	}

	err = inst.composeFieldRelatedCodeAll(structFieldOperationDeclaration, scalarIRH.IterateColumnProps(), "")
	if err != nil {
		return
	}
	err = inst.composeFieldRelatedCodeAll(structFieldOperationDeclaration, nonScalarIRH.IterateColumnProps(), "")
	if err != nil {
		return
	}

	_, err = fmt.Fprintf(b, `}
func New%s(builder *array.RecordBuilder, parent *%s) (inst *%s) {
	inst = &%s{}
	inAttr := New%s(builder,inst)
	inst.errs = make([]error,0,8)
	inst.state = runtime.EntityStateInitial
	inst.inAttr = inAttr
	inst.parent = parent
`,
		inst.clsNames.inSectionClassName,
		inst.clsNames.inEntityClassName,
		inst.clsNames.inSectionClassName,
		inst.clsNames.inSectionClassName,
		inst.clsNames.inAttributeClassName)
	if err != nil {
		return
	}
	err = inst.composeFieldRelatedCodeAll(structFieldOperationInitialization, scalarIRH.IterateColumnProps(), "")
	if err != nil {
		return
	}
	err = inst.composeFieldRelatedCodeAll(structFieldOperationInitialization, nonScalarIRH.IterateColumnProps(), "")
	if err != nil {
		return
	}
	_, err = b.WriteString(`
	return inst
}
`)
	if err != nil {
		return
	}

	return
}

func (inst *GoClassBuilder) composeStateVerificationCode(allowedSrcStates []runtime.EntityStateE, errReturnValue bool, instRetrExpr string) (err error) {
	b := inst.builder
	var onErrCode string
	if errReturnValue {
		onErrCode = `		err = runtime.ErrInvalidStateTransition
		return`
	} else {
		onErrCode = `		inst.AppendError(runtime.ErrInvalidStateTransition)
		return ` + instRetrExpr
	}

	switch len(allowedSrcStates) {
	case 0:
		break
	case 1:
		_, err = fmt.Fprintf(b, `	if inst.state != runtime.%s {
%s
	}
`, runtime.EntityStateVariableNames[allowedSrcStates[0]], onErrCode)
		break
	default:
		_, err = b.WriteString(`	switch inst.state {
	case `)
		for i, s := range allowedSrcStates {
			if i > 0 {
				_, err = fmt.Fprintf(b, `, runtime.%s`, runtime.EntityStateVariableNames[s])
			} else {
				_, err = fmt.Fprintf(b, `runtime.%s`, runtime.EntityStateVariableNames[s])
			}
			if err != nil {
				return
			}
		}
		_, err = fmt.Fprintf(b, `:
		break
	default:
%s
	}
`, onErrCode)
	}
	return
}
func (inst *GoClassBuilder) composeStateTransitionCode(allowedSrcStates []runtime.EntityStateE, destState runtime.EntityStateE, errReturnValue bool, instRetrExpr string) (err error) {
	b := inst.builder
	switch len(allowedSrcStates) {
	case 0:
		err = eh.Errorf("unable to generate code for empty state list")
		return
	default:
		_, err = b.WriteString(`	switch inst.state {
	case `)
		for i, s := range allowedSrcStates {
			if i > 0 {
				_, err = fmt.Fprintf(b, `, runtime.%s`, runtime.EntityStateVariableNames[s])
			} else {
				_, err = fmt.Fprintf(b, `runtime.%s`, runtime.EntityStateVariableNames[s])
			}
			if err != nil {
				return
			}
		}
		_, err = fmt.Fprintf(b, `:
		inst.state = runtime.%s
		break
`, runtime.EntityStateVariableNames[destState])
	}
	if errReturnValue {
		_, err = fmt.Fprintf(b, `	default:
		err = runtime.ErrInvalidStateTransition
		return
	}
	`)
	} else {
		_, err = fmt.Fprintf(b, `	default:
		inst.AppendError(runtime.ErrInvalidStateTransition)
		return %s
	}
	`, instRetrExpr)
	}
	return
}
func (inst *GoClassBuilder) composeErrorHandlingCode(className string) (err error) {
	b := inst.builder
	_, err = fmt.Fprintf(b, `
func (inst *%s) AppendError(err error) {
	inst.errs = eh.AppendError(inst.errs,err)
}
func (inst *%s) clearErrors() {
	inst.errs = eh.ClearErrors(inst.errs)
}
`, className, className)
	return
}

func (inst *GoClassBuilder) findFirstMatchingColumnAndGenerateCode(irh *common.IntermediatePairHolder, subType common.IntermediateColumnSubTypeE, role common.ColumnRoleE, op structFieldOperationE) (err error) {
	for cc, cp := range irh.IterateColumnProps() {
		if cc.SubType == subType {
			for i := 0; i < cp.Length(); i++ {
				if cp.Roles[i] == role {
					err = inst.composeFieldRelatedCode(op, cc, cp, i)
					if err != nil {
						return
					}
					return
				}
			}
		}
	}
	err = eb.Build().Stringer("role", role).Errorf("unable to find column with given role")
	return
}
func deriveSubHolderSelectNonScalar(cc common.IntermediateColumnContext) (keep bool) {
	switch cc.SubType {
	case common.IntermediateColumnsSubTypeHomogenousArray,
		common.IntermediateColumnsSubTypeSet:
		return true
	}
	return false
}
func deriveSubHolderSelectNonScalarSupport(cc common.IntermediateColumnContext) (keep bool) {
	switch cc.SubType {
	case common.IntermediateColumnsSubTypeHomogenousArraySupport,
		common.IntermediateColumnsSubTypeSetSupport:
		return true
	}
	return false
}
func deriveSubHolderSelectMembership(cc common.IntermediateColumnContext) (keep bool) {
	return cc.SubType == common.IntermediateColumnsSubTypeMembership
}
func deriveSubHolderSelectMembershipSupport(cc common.IntermediateColumnContext) (keep bool) {
	return cc.SubType == common.IntermediateColumnsSubTypeMembershipSupport
}
func deriveSubHolderSelectScalar(cc common.IntermediateColumnContext) (keep bool) {
	return cc.SubType == common.IntermediateColumnsSubTypeScalar
}
func deriveSubHolderSelectTaggedValues(cc common.IntermediateColumnContext) (keep bool) {
	return cc.PlainItemType == common.PlainItemTypeNone
}
func deriveSubHolderSelectPlainValues(cc common.IntermediateColumnContext) (keep bool) {
	return cc.PlainItemType != common.PlainItemTypeNone
}
func (inst *GoClassBuilder) composeAttributeCode(sectionIRH *common.IntermediatePairHolder, tableRowConfig common.TableRowConfigE) (err error) {
	b := inst.builder

	nonScalarIRH := sectionIRH.DeriveSubHolder(deriveSubHolderSelectNonScalar)
	nonScalarSupportIRH := sectionIRH.DeriveSubHolder(deriveSubHolderSelectNonScalarSupport)
	scalarIRH := sectionIRH.DeriveSubHolder(deriveSubHolderSelectScalar)
	membershipIRH := sectionIRH.DeriveSubHolder(deriveSubHolderSelectMembership)
	membershipSupportIRH := sectionIRH.DeriveSubHolder(deriveSubHolderSelectMembershipSupport)

	{ // beginAttribute
		_, err = fmt.Fprintf(b, "func (inst *%s) beginAttribute() {\n", inst.clsNames.inAttributeClassName)
		if err != nil {
			return
		}
		err = inst.composeFieldRelatedCodeAll(structFieldOperationAppendContainer, common.IterateColumnPropsMultiIntermediatePairHolders(nonScalarIRH, membershipIRH), "")
		if err != nil {
			return
		}
		err = inst.composeFieldRelatedCodeAll(structFieldOperationResetContainerLength, common.IterateColumnPropsMultiIntermediatePairHolders(nonScalarIRH, membershipIRH), "")
		if err != nil {
			return
		}
		// FIXME tableRowConfig
		err = inst.composeFieldRelatedCodeAll(structFieldOperationAppendContainer, scalarIRH.IterateColumnProps(), "")
		if err != nil {
			return
		}

		err = inst.composeFieldRelatedCodeAll(structFieldOperationAppendContainer, common.IterateColumnPropsMultiIntermediatePairHolders(nonScalarSupportIRH, membershipSupportIRH), "")
		if err != nil {
			return
		}

		_, err = b.WriteString(`	inst.state = runtime.EntityStateInSection
	inst.clearErrors()
}
`)
		if err != nil {
			return
		}
	}
	{ // AddToContainer/AddToCoContainers
		switch nonScalarIRH.CountColumns() {
		case 0:
			break
		case 1:
			_, err = fmt.Fprintf(b, "func (inst *%s) AddToContainer(", inst.clsNames.inAttributeClassName)
			break
		default:
			_, err = fmt.Fprintf(b, "func (inst *%s) AddToCoContainers(", inst.clsNames.inAttributeClassName)
			break
		}
		if err != nil {
			return
		}

		first := true
		for cc, cp := range nonScalarIRH.IterateColumnProps() {
			for i := 0; i < cp.Length(); i++ {
				if !first {
					_, err = b.WriteString(", ")
					if err != nil {
						return
					}
				}
				first = false
				err = inst.composeFieldRelatedCode(structFieldOperationArgDeclarationDemoted, cc, cp, i)
				if err != nil {
					return
				}
			}
		}
		if nonScalarIRH.CountColumns() > 0 {
			_, err = fmt.Fprintf(b, ") *%s {\n", inst.clsNames.inAttributeClassName)
			if err != nil {
				return
			}
			err = inst.composeStateVerificationCode([]runtime.EntityStateE{runtime.EntityStateInAttribute}, false, "inst")
			if err != nil {
				return
			}

			err = inst.composeFieldRelatedCodeAll(structFieldOperationAppendScalar, nonScalarIRH.IterateColumnProps(), "")
			if err != nil {
				return
			}
			err = inst.composeFieldRelatedCodeAll(structFieldOperationIncrementContainerLength, nonScalarIRH.IterateColumnProps(), "")
			if err != nil {
				return
			}
			_, err = b.WriteString("\treturn inst\n}\n")
			if err != nil {
				return
			}
		}
	}

	{ // membership
		var mixedParamsIdx [2]int
		var mixedParamsCc [2]common.IntermediateColumnContext
		var mixedParamsCp [2]*common.IntermediateColumnProps
		mixedParamsIdx[0] = -1
		mixedParamsIdx[1] = -1
		for cc, cp := range membershipIRH.IterateColumnProps() {
			for i := 0; i < cp.Length(); i++ {
				switch cp.Roles[i] {
				case common.ColumnRoleMixedRefHighCardParameters:
					mixedParamsIdx[0] = i
					mixedParamsCp[0] = cp
					mixedParamsCc[0] = cc
					break
				case common.ColumnRoleMixedVerbatimHighCardParameters:
					mixedParamsIdx[1] = i
					mixedParamsCp[1] = cp
					mixedParamsCc[1] = cc
					break
				}
			}
		}
		for cc, cp := range membershipIRH.IterateColumnProps() {
			for i := 0; i < cp.Length(); i++ {
				var funcName string
				mixed := -1
				switch cp.Roles[i] {
				case common.ColumnRoleHighCardRef:
					funcName = "AddMembershipHighCardRef"
					break
				case common.ColumnRoleHighCardRefParametrized:
					funcName = "AddMembershipHighCardRefParametrized"
					break
				case common.ColumnRoleHighCardVerbatim:
					funcName = "AddMembershipHighCardVerbatim"
					break
				case common.ColumnRoleLowCardRef:
					funcName = "AddMembershipLowCardRef"
					break
				case common.ColumnRoleLowCardRefParametrized:
					funcName = "AddMembershipLowCardRefParametrized"
					break
				case common.ColumnRoleLowCardVerbatim:
					funcName = "AddMembershipLowCardRefVerbatim"
					break
				case common.ColumnRoleMixedLowCardRef:
					funcName = "AddMembershipMixedLowCardRef"
					mixed = 0
					break
				case common.ColumnRoleMixedLowCardVerbatim:
					funcName = "AddMembershipMixedLowCardVerbatim"
					mixed = 1
					break
				case common.ColumnRoleMixedRefHighCardParameters, common.ColumnRoleMixedVerbatimHighCardParameters:
					// mixed, trigger on other
					break
				default:
					err = ErrUnhandledRole
					return
				}
				if mixed >= 0 && mixedParamsIdx[mixed] < 0 {
					err = eh.Errorf("unable to find all columns for mixed membership spec")
					return
				}

				if funcName != "" {
					f := func(funcName string, retrType string, instRetrExpr string) (err error) {
						if mixed >= 0 {
							_, err = fmt.Fprintf(b, "func (inst *%s) %s(", inst.clsNames.inAttributeClassName, funcName)
							if err != nil {
								return
							}
							err = inst.composeFieldRelatedCode(structFieldOperationArgDeclaration, cc, cp, i)
							if err != nil {
								return
							}
							_, err = b.WriteString(", ")
							if err != nil {
								return
							}
							err = inst.composeFieldRelatedCode(structFieldOperationArgDeclaration, mixedParamsCc[mixed], mixedParamsCp[mixed], mixedParamsIdx[mixed])
							if err != nil {
								return
							}
							_, err = fmt.Fprintf(b, ") %s {\n", retrType)
							if err != nil {
								return
							}
							err = inst.composeStateVerificationCode([]runtime.EntityStateE{runtime.EntityStateInAttribute}, false, instRetrExpr)
							if err != nil {
								return
							}
							err = inst.composeFieldRelatedCode(structFieldOperationAppendScalar, cc, cp, i)
							if err != nil {
								return
							}
							err = inst.composeFieldRelatedCode(structFieldOperationAppendScalar, mixedParamsCc[mixed], mixedParamsCp[mixed], mixedParamsIdx[mixed])
							if err != nil {
								return
							}
							err = inst.composeFieldRelatedCode(structFieldOperationIncrementContainerLength, cc, cp, i)
							if err != nil {
								return
							}
							err = inst.composeFieldRelatedCode(structFieldOperationIncrementContainerLength, mixedParamsCc[mixed], mixedParamsCp[mixed], mixedParamsIdx[mixed])
						} else {
							_, err = fmt.Fprintf(b, "func (inst *%s) %s(", inst.clsNames.inAttributeClassName, funcName)
							if err != nil {
								return
							}
							err = inst.composeFieldRelatedCode(structFieldOperationArgDeclaration, cc, cp, i)
							if err != nil {
								return
							}
							_, err = fmt.Fprintf(b, ") %s {\n", retrType)
							if err != nil {
								return
							}
							err = inst.composeStateVerificationCode([]runtime.EntityStateE{runtime.EntityStateInAttribute}, false, instRetrExpr)
							if err != nil {
								return
							}
							err = inst.composeFieldRelatedCode(structFieldOperationAppendScalar, cc, cp, i)
							if err != nil {
								return
							}
							err = inst.composeFieldRelatedCode(structFieldOperationIncrementContainerLength, cc, cp, i)
						}
						if err != nil {
							return
						}
						_, err = b.WriteString("\treturn")
						if err != nil {
							return
						}
						if instRetrExpr != "" {
							_, err = b.WriteString(" " + instRetrExpr)
						}
						_, err = b.WriteString("\n}\n")
						if err != nil {
							return
						}
						return
					}
					err = f(funcName, "*"+inst.clsNames.inAttributeClassName, "inst")
					if err != nil {
						return
					}
					err = f(funcName+"P", "", "")
					if err != nil {
						return
					}
				}
			}
		}
	}

	{ // handleMembershipSupportColumns
		_, err = fmt.Fprintf(b, `func (inst *%s) handleMembershipSupportColumns() {
	var l int
	var _ = l
`, inst.clsNames.inAttributeClassName)
		if err != nil {
			return
		}

		for cc, cp := range membershipSupportIRH.IterateColumnProps() {
			for i := 0; i < cp.Length(); i++ {
				var cardinalitySrcRole common.ColumnRoleE
				role := cp.Roles[i]
				if !role.IsCardinalityRole() {
					continue
				}
				cardinalitySrcRole, err = common.GetCardinalityRoleByMembershipRole(role)
				if err != nil {
					err = eb.Build().Stringer("role", role).Errorf("unable to resolve cardinality role: %w", err)
					return
				}
				err = inst.findFirstMatchingColumnAndGenerateCode(membershipIRH, common.IntermediateColumnsSubTypeMembership, cardinalitySrcRole, structFieldOperationStoreContainerLength)
				if err != nil {
					return
				}
				err = inst.composeFieldRelatedCode(structFieldOperationAppendContainerLength, cc, cp, i)
				if err != nil {
					return
				}
			}
		}
		_, err = b.WriteString(`}
`)
		if err != nil {
			return
		}
	}

	{ // handleNonScalarSupportColumns
		_, err = fmt.Fprintf(b, `func (inst *%s) handleNonScalarSupportColumns() {
	var l int
	var _ = l
`,
			inst.clsNames.inAttributeClassName)
		if err != nil {
			return
		}
		sectionIRH.DeriveSubHolder(deriveSubHolderSelectNonScalarSupport)
		for cc, cp := range sectionIRH.IterateColumnProps() {
			switch cc.SubType {
			case common.IntermediateColumnsSubTypeHomogenousArraySupport:
				for i := 0; i < cp.Length(); i++ {
					if cp.Roles[i] == common.ColumnRoleLength {
						err = inst.findFirstMatchingColumnAndGenerateCode(nonScalarIRH, common.IntermediateColumnsSubTypeHomogenousArray, common.ColumnRoleValue, structFieldOperationStoreContainerLength)
						if err != nil {
							return
						}
						err = inst.composeFieldRelatedCode(structFieldOperationAppendContainerLength, cc, cp, i)
						if err != nil {
							return
						}
					}
				}
				break
			}
		}
		for cc, cp := range sectionIRH.IterateColumnProps() {
			switch cc.SubType {
			case common.IntermediateColumnsSubTypeSetSupport:
				for i := 0; i < cp.Length(); i++ {
					if cp.Roles[i] == common.ColumnRoleCardinality {
						err = inst.findFirstMatchingColumnAndGenerateCode(nonScalarIRH, common.IntermediateColumnsSubTypeSet, common.ColumnRoleValue, structFieldOperationStoreContainerLength)
						if err != nil {
							return
						}
						err = inst.composeFieldRelatedCode(structFieldOperationAppendContainerLength, cc, cp, i)
						if err != nil {
							return
						}
					}
				}
				break
			}
		}
		_, err = b.WriteString(`}
`)
		if err != nil {
			return
		}
	}

	{ // completeAttribute
		_, err = fmt.Fprintf(b, `func (inst *%s) completeAttribute() {
	inst.handleMembershipSupportColumns()
	inst.handleNonScalarSupportColumns()
}
`,
			inst.clsNames.inAttributeClassName)
		if err != nil {
			return
		}
	}
	{ // EndSection
		_, err = fmt.Fprintf(b, `func (inst *%s) EndSection() *%s {
`,
			inst.clsNames.inAttributeClassName, inst.clsNames.inEntityClassName)
		if err != nil {
			return
		}
		err = inst.composeStateTransitionCode([]runtime.EntityStateE{runtime.EntityStateInAttribute}, runtime.EntityStateInitial, false, "inst.parent.parent")
		if err != nil {
			return
		}
		_, err = b.WriteString(`
	inst.completeAttribute()
	inst.parent.EndSection()
	return inst.parent.parent
}
`)
		if err != nil {
			return
		}
	}

	{ // EndAttribute
		_, err = fmt.Fprintf(b, `func (inst *%s) EndAttribute() *%s {
`,
			inst.clsNames.inAttributeClassName, inst.clsNames.inSectionClassName)
		if err != nil {
			return
		}
		err = inst.composeStateTransitionCode([]runtime.EntityStateE{runtime.EntityStateInAttribute}, runtime.EntityStateInSection, false, "inst.parent")
		if err != nil {
			return
		}
		_, err = b.WriteString(`
	inst.completeAttribute()
	inst.parent.endAttribute()
	return inst.parent
}
`)
		if err != nil {
			return
		}
	}

	err = inst.composeErrorHandlingCode(inst.clsNames.inAttributeClassName)
	if err != nil {
		return
	}

	return
}

func (inst *GoClassBuilder) composeSectionCode(sectionIRH *common.IntermediatePairHolder, tableRowConfig common.TableRowConfigE, scalarIRH *common.IntermediatePairHolder, nonScalarIRH *common.IntermediatePairHolder) (err error) {
	b := inst.builder

	{ // endAttribute
		_, err = fmt.Fprintf(b, `func (inst *%s) endAttribute() {
`, inst.clsNames.inSectionClassName)
		if err != nil {
			return
		}
		err = inst.composeStateTransitionCode([]runtime.EntityStateE{runtime.EntityStateInAttribute}, runtime.EntityStateInSection, false, "")
		_, err = b.WriteString(`}
`)
		if err != nil {
			return
		}
	}
	{ // BeginAttribute
		_, err = fmt.Fprintf(b, `func (inst *%s) BeginAttribute(`, inst.clsNames.inSectionClassName)
		if err != nil {
			return
		}

		err = inst.composeFieldRelatedCodeAll(structFieldOperationArgDeclaration, scalarIRH.IterateColumnProps(), ", ")
		if err != nil {
			return
		}
		_, err = fmt.Fprintf(b, `) *%s {
`, inst.clsNames.inAttributeClassName)
		if err != nil {
			return
		}
		err = inst.composeStateTransitionCode([]runtime.EntityStateE{runtime.EntityStateInSection}, runtime.EntityStateInAttribute, false, "inst.inAttr")
		if err != nil {
			return
		}
		err = inst.composeFieldRelatedCodeAll(structFieldOperationAppendScalar, scalarIRH.IterateColumnProps(), "")
		if err != nil {

		}
		_, err = fmt.Fprintf(b, `
	inst.inAttr.state = inst.state
	return inst.inAttr
}
`)
		if err != nil {
			return
		}
	}
	{ // CheckErrors
		_, err = fmt.Fprintf(b, `func (inst *%s) CheckErrors() (err error) {
	err = eh.CheckErrors(slices.Concat(inst.errs,inst.inAttr.errs))
	return
}
`, inst.clsNames.inSectionClassName)
		if err != nil {
			return
		}
	}
	err = inst.composeSharedMethods(inst.clsNames.inSectionClassName)
	if err != nil {
		return
	}
	{ // EndSection
		_, err = fmt.Fprintf(b, `func (inst *%s) EndSection() *%s {
`, inst.clsNames.inSectionClassName, inst.clsNames.inEntityClassName)
		if err != nil {
			return
		}
		err = inst.composeStateTransitionCode([]runtime.EntityStateE{runtime.EntityStateInSection}, runtime.EntityStateInitial, false, "inst.parent")
		if err != nil {
			return
		}
		_, err = b.WriteString(`
	return inst.parent
}
`)
		if err != nil {
			return
		}
	}

	{ // beginSection
		_, err = fmt.Fprintf(b, `
func (inst *%s) beginSection() {
	inst.state = runtime.EntityStateInSection
	inst.inAttr.beginAttribute()
`, inst.clsNames.inSectionClassName)
		if err != nil {
			return
		}
		err = inst.composeFieldRelatedCodeAll(structFieldOperationAppendContainer, nonScalarIRH.IterateColumnProps(), "")
		if err != nil {
			return
		}
		_, err = b.WriteString(`
}
`)
		if err != nil {
			return
		}
	}
	{ // resetSection
		_, err = fmt.Fprintf(b, `
func (inst *%s) resetSection() {
	inst.clearErrors()
	inst.state = runtime.EntityStateInitial
}
`, inst.clsNames.inSectionClassName)
		if err != nil {
			return
		}
	}

	err = inst.composeErrorHandlingCode(inst.clsNames.inSectionClassName)
	if err != nil {
		return
	}

	return
}
func itemTypeToSetterName(itemType common.PlainItemTypeE) (setterName string) {
	switch itemType {
	case common.PlainItemTypeEntityId:
		return "SetId"
	case common.PlainItemTypeEntityTimestamp:
		return "SetTimestamp"
	case common.PlainItemTypeEntityRouting:
		return "SetRouting"
	case common.PlainItemTypeEntityLifecycle:
		return "SetLifecycle"
	case common.PlainItemTypeTransaction:
		return "SetTransaction"
	case common.PlainItemTypeOpaque:
		return "SetOpaque"
	}
	return
}
func (inst *GoClassBuilder) composeEntityClassAndFactoryCode(tableName naming.StylableName, sectionNames []naming.StylableName, ir *common.IntermediateTableRepresentation, tableRowConfig common.TableRowConfigE, clsNamer GoClassNamerI, entityIRH *common.IntermediatePairHolder) (err error) {
	b := inst.builder
	inst.populateClsNames(tableName, "", 0, 0)
	_, err = fmt.Fprintf(b, `type %s struct {
	errs []error
	state runtime.EntityStateE
	allocator memory.Allocator
	builder *array.RecordBuilder
	records []arrow.Record
`, inst.clsNames.inEntityClassName)
	if err != nil {
		return
	}
	for i, sectionName := range sectionNames {
		inst.populateClsNames(tableName, sectionName, i, len(sectionNames))
		_, err = fmt.Fprintf(b, `	section%02dInst *%s
	section%02dState runtime.EntityStateE
`,
			i, inst.clsNames.inSectionClassName, i)
		if err != nil {
			return
		}
	}
	plainIRH := entityIRH.DeriveSubHolder(deriveSubHolderSelectPlainValues).DeriveSubHolder(deriveSubHolderSelectScalar)
	plainScalarIRH := plainIRH.DeriveSubHolder(deriveSubHolderSelectScalar)
	err = inst.composeFieldRelatedCodeAll(structFieldOperationPlainDeclaration, plainScalarIRH.IterateColumnProps(), "\n")
	if err != nil {
		return
	}
	err = inst.composeFieldRelatedCodeAll(structFieldOperationDeclaration, plainScalarIRH.IterateColumnProps(), "\n")
	if err != nil {
		return
	}
	var schemaFactoryName string
	schemaFactoryName, err = clsNamer.ComposeSchemaFactoryName(tableName)
	if err != nil {
		return
	}
	_, err = fmt.Fprintf(b, `
}

func New%s(allocator memory.Allocator, estimatedNumberOfRecords int) (inst *%s) {
	inst = &%s{}
	inst.errs = make([]error,0,8)
	inst.state = runtime.EntityStateInitial
	inst.allocator = allocator
	inst.records = make([]arrow.Record,0,estimatedNumberOfRecords)
	builder := %s(allocator)
	inst.builder = builder
	inst.initSections(builder)
`,
		inst.clsNames.inEntityClassName,
		inst.clsNames.inEntityClassName,
		inst.clsNames.inEntityClassName,
		schemaFactoryName)
	if err != nil {
		return
	}

	for cc, cp := range ir.IterateColumnProps() {
		if cc.PlainItemType != common.PlainItemTypeNone {
			for i := 0; i < cp.Length(); i++ {
				err = inst.composeFieldRelatedCode(structFieldOperationInitialization, cc, cp, i)
				if err != nil {
					return
				}
			}
		}
	}
	_, err = b.WriteString(`
	return inst
}
`)
	if err != nil {
		return
	}
	return
}
func (inst *GoClassBuilder) composeEntityCode(tableName naming.StylableName, sectionNames []naming.StylableName, ir *common.IntermediateTableRepresentation, tableRowConfig common.TableRowConfigE, entityIRH *common.IntermediatePairHolder) (err error) {
	plainIRH := entityIRH.DeriveSubHolder(deriveSubHolderSelectPlainValues)
	//taggedIRH := entityIRH.DeriveSubHolder(deriveSubHolderSelectTaggedValues)
	plainSetterIRH := plainIRH.DeriveSubHolder(func(cc common.IntermediateColumnContext) (keep bool) {
		keep = itemTypeToSetterName(cc.PlainItemType) != ""
		return
	})

	b := inst.builder
	{ // setter
		for cc, cp := range plainSetterIRH.IterateColumnProps() {
			setterName := itemTypeToSetterName(cc.PlainItemType)
			_, err = fmt.Fprintf(b, `func (inst *%s) %s(`, inst.clsNames.inEntityClassName, setterName)
			if err != nil {
				return
			}
			first := true
			for j := 0; j < cp.Length(); j++ {
				if !first {
					_, err = b.WriteString(", ")
					if err != nil {
						return
					}
				}
				first = false
				err = inst.composeFieldRelatedCode(structFieldOperationArgDeclaration, cc, cp, j)
				if err != nil {
					return
				}
			}
			_, err = fmt.Fprintf(b, `) *%s {
`, inst.clsNames.inEntityClassName)
			if err != nil {
				return
			}
			err = inst.composeStateVerificationCode([]runtime.EntityStateE{runtime.EntityStateInEntity}, false, "inst")
			if err != nil {
				return
			}
			for j := 0; j < cp.Length(); j++ {
				err = inst.composeFieldRelatedCode(structFieldOperationPlainAssignArg, cc, cp, j)
				if err != nil {
					return
				}
			}
			_, err = fmt.Fprintf(b, `
	return inst
}
`)
			if err != nil {
				return
			}
		}
	}
	{ // appendPlainValues
		_, err = fmt.Fprintf(b, `func (inst *%s) appendPlainValues() {
`, inst.clsNames.inEntityClassName)
		if err != nil {
			return
		}
		err = inst.composeFieldRelatedCodeAll(structFieldOperationPlainAppend, plainIRH.IterateColumnProps(), "\n")
		if err != nil {
			return
		}
		_, err = b.WriteString(`}
`)
		if err != nil {
			return
		}
	}
	{ // resetPlainValues
		_, err = fmt.Fprintf(b, `func (inst *%s) resetPlainValues() {
`, inst.clsNames.inEntityClassName)
		if err != nil {
			return
		}
		err = inst.composeFieldRelatedCodeAll(structFieldOperationPlainReset, plainIRH.IterateColumnProps(), "\n")
		if err != nil {
			return
		}
		_, err = b.WriteString(`}
`)
		if err != nil {
			return
		}
	}
	{ // reset sections
		_, err = fmt.Fprintf(b, `func (inst *%s) initSections(builder *array.RecordBuilder) {`, inst.clsNames.inEntityClassName)
		if err != nil {
			return
		}
		for i, sectionName := range sectionNames {
			inst.populateClsNames(tableName, sectionName, i, len(sectionNames))
			_, err = fmt.Fprintf(b, `
	inst.section%02dInst = New%s(builder, inst)`,
				i, inst.clsNames.inSectionClassName)
			if err != nil {
				return
			}
		}
		_, err = b.WriteString(`
}
`)
		if err != nil {
			return
		}
	}
	{ // beginSections
		_, err = fmt.Fprintf(b, `func (inst *%s) beginSections() {
`, inst.clsNames.inEntityClassName)
		for i, _ := range sectionNames {
			_, err = fmt.Fprintf(b, `	inst.section%02dInst.beginSection()
`, i)
			if err != nil {
				return
			}
		}
		_, err = b.WriteString(`}
`)
		if err != nil {
			return
		}
	}
	{ // resetSections
		_, err = fmt.Fprintf(b, `func (inst *%s) resetSections() {
`, inst.clsNames.inEntityClassName)
		for i, _ := range sectionNames {
			_, err = fmt.Fprintf(b, `	inst.section%02dInst.resetSection()
`, i)
			if err != nil {
				return
			}
		}
		_, err = b.WriteString(`}
`)
		if err != nil {
			return
		}
	}
	{ // CheckErrors
		_, err = fmt.Fprintf(b, `func (inst *%s) CheckErrors() (err error) {
	err = eh.CheckErrors(inst.errs)
`, inst.clsNames.inEntityClassName)
		if err != nil {
			return
		}
		for i, _ := range sectionNames {
			_, err = fmt.Fprintf(b, `	err = errors.Join(err, inst.section%02dInst.CheckErrors())
`, i)
			if err != nil {
				return
			}
		}
		_, err = b.WriteString(`
	return
}
`)
		if err != nil {
			return
		}
	}
	err = inst.composeSharedMethods(inst.clsNames.inEntityClassName)
	if err != nil {
		return
	}

	{ // section getter
		for i, secName := range sectionNames {
			inst.populateClsNames(tableName, secName, i, len(sectionNames))
			_, err = fmt.Fprintf(b, `func (inst *%s) GetSection%s() *%s {
	return inst.section%02dInst
}
`, inst.clsNames.inEntityClassName, secName.Convert(naming.NamingStyleUpperCamelCase).String(), inst.clsNames.inSectionClassName, i)
		}
	}
	{ // BeginEntity
		_, err = fmt.Fprintf(b, `func (inst *%s) BeginEntity() *%s {
`, inst.clsNames.inEntityClassName, inst.clsNames.inEntityClassName)
		err = inst.composeStateTransitionCode([]runtime.EntityStateE{runtime.EntityStateInitial}, runtime.EntityStateInEntity, false, "inst")
		if err != nil {
			return
		}
		_, err = fmt.Fprintf(b, `
	inst.beginSections()
	return inst
}
`)
		if err != nil {
			return
		}
	}
	{ // validateEntity
		_, err = fmt.Fprintf(b, `func (inst *%s) validateEntity() {
	// FIXME check coSectionGroup consistency
	return
}
`, inst.clsNames.inEntityClassName)
		if err != nil {
			return
		}
	}
	{ // CommitEntity
		_, err = fmt.Fprintf(b, `func (inst *%s) CommitEntity() (err error) {
	inst.validateEntity()
	err = inst.CheckErrors()
	if err != nil {
		err = eh.Errorf("unable to commit entity, found errors: %%w", err)
		return
	}
`, inst.clsNames.inEntityClassName)
		err = inst.composeStateTransitionCode([]runtime.EntityStateE{runtime.EntityStateInEntity}, runtime.EntityStateInitial, true, "inst")
		if err != nil {
			return
		}
		_, err = fmt.Fprintf(b, `
	inst.appendPlainValues()
	inst.resetPlainValues()
	inst.resetSections()
	return
}
`)
		if err != nil {
			return
		}
	}
	{ // RollbackEntity
		_, err = fmt.Fprintf(b, `func (inst *%s) RollbackEntity() (err error) {
`, inst.clsNames.inEntityClassName)
		err = inst.composeStateTransitionCode([]runtime.EntityStateE{runtime.EntityStateInEntity}, runtime.EntityStateInitial, true, "inst")
		if err != nil {
			return
		}
		_, err = fmt.Fprintf(b, `
	inst.appendPlainValues() // arrow fields must all have one row
	inst.resetPlainValues()
	inst.resetSections()
	rec := inst.builder.NewRecord()
	if rec.NumRows() > 1 {
		inst.records = append(inst.records, rec.NewSlice(0,rec.NumRows()-1))
	} else {
		// FIXME find better way to truncate builder
		inst.builder.NewRecord().Release()
	}
	return
}
`)
		if err != nil {
			return
		}
	}
	{ // TransferRecords
		_, err = fmt.Fprintf(b, `
// TransferRecords The returned Records must be Release()'d after use.
func (inst *%s) TransferRecords(recordsIn []arrow.Record) (recordsOut []arrow.Record, err error) {
`, inst.clsNames.inEntityClassName)
		err = inst.composeStateVerificationCode([]runtime.EntityStateE{runtime.EntityStateInitial}, true, "inst")
		if err != nil {
			return
		}
		_, err = fmt.Fprintf(b, `
	recordsOut = slices.Grow(recordsIn, len(inst.records)+1)
	copy(recordsOut, inst.records)
	clear(inst.records)
	inst.records = inst.records[:0]
	rec := inst.builder.NewRecord()
	if rec.NumRows() > 0 {
		recordsOut = append(recordsOut, rec)
	}
	return
}
`)
		if err != nil {
			return
		}
	}
	{ // GetSchema
		_, err = fmt.Fprintf(b, `
func (inst *%s) GetSchema() (schema *arrow.Schema) {
	return inst.builder.Schema()
}
`, inst.clsNames.inEntityClassName)
		if err != nil {
			return
		}
	}

	err = inst.composeErrorHandlingCode(inst.clsNames.inEntityClassName)
	if err != nil {
		return
	}
	return
}
func (inst *GoClassBuilder) populateClsNames(tableName naming.StylableName, sectionName naming.StylableName, sectionIndex int, totalSections int) {
	var err error
	inst.clsNames.inEntityClassName, err = inst.classNamer.ComposeEntityClassName(tableName)
	if err != nil {
		log.Panic().Err(err).Msg("unable to generate entity class name")
	}
	if sectionName != "" {
		inst.clsNames.inSectionClassName, err = inst.classNamer.ComposeSectionClassName(tableName, sectionName, sectionIndex, totalSections)
		if err != nil {
			log.Panic().Err(err).Msg("unable to generate entity class name")
		}
		inst.clsNames.inAttributeClassName, err = inst.classNamer.ComposeAttributeClassName(tableName, sectionName, sectionIndex, totalSections)
		if err != nil {
			log.Panic().Err(err).Msg("unable to generate entity class name")
		}
	}
}
func (inst *GoClassBuilder) ComposeGoImports(ir *common.IntermediateTableRepresentation, tableRowConfig common.TableRowConfigE) (err error) {
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
				if imports.Has(im) {
					continue
				}
				imports.Add(im)
				_, err = fmt.Fprintf(b, "\t%q\n", im)
				if err != nil {
					return
				}
			}
		}
	}
	for _, im := range []string{"slices", "github.com/stergiotis/boxer/public/observability/eh"} {
		_, err = fmt.Fprintf(b, "\t%q\n", im)
		if err != nil {
			return
		}
	}
	_, err = fmt.Fprintf(b, "\t%q\n", "errors")
	if err != nil {
		return
	}
	return
}
func (inst *GoClassBuilder) ComposeCode(tableName naming.StylableName, ir *common.IntermediateTableRepresentation, conv common.NamingConventionI, tableRowConfig common.TableRowConfigE, clsNamer GoClassNamerI) (err error) {
	b := inst.builder
	if b == nil {
		err = common.ErrNoBuilder
		return
	}
	inst.classNamer = clsNamer

	if conv != nil {
		err = inst.composeNamingConventionDependentCode(tableName, ir, conv, tableRowConfig, clsNamer)
		if err != nil {
			err = eb.Build().Stringer("tableName", tableName).Errorf("unable to compose naming convention dependent code: %w", err)
			return
		}
	}
	entityIRH := common.NewIntermediatePairHolder(ir.TotalLength())
	for cc, cp := range ir.IterateColumnProps() {
		entityIRH.Add(cc, cp)
	}

	sectionNames := make([]naming.StylableName, 0, 32)
	maxColumnsPerSection := 0
	for _, t := range ir.TaggedValueDesc {
		secName := t.SectionName
		sectionNames = append(sectionNames, secName)
		maxColumnsPerSection = max(maxColumnsPerSection, t.Length())
	}
	slices.Sort(sectionNames)
	sectionNames = slices.Compact(sectionNames)

	err = inst.composeEntityClassAndFactoryCode(tableName, sectionNames, ir, tableRowConfig, clsNamer, entityIRH)
	if err != nil {
		err = eb.Build().Stringer("tableName", tableName).Errorf("unable to compose entity class and factory code: %w", err)
		return
	}
	err = inst.composeEntityCode(tableName, sectionNames, ir, tableRowConfig, entityIRH)
	if err != nil {
		err = eb.Build().Stringer("tableName", tableName).Errorf("unable to compose entity code: %w", err)
		return
	}

	sectionIRH := common.NewIntermediatePairHolder(maxColumnsPerSection)
	for i, sectionName := range sectionNames {
		inst.populateClsNames(tableName, sectionName, i, len(sectionNames))
		sectionIRH.Reset()
		for cc, cp := range ir.IterateColumnProps() {
			if cc.SectionName == sectionName && cc.PlainItemType == common.PlainItemTypeNone {
				sectionIRH.Add(cc, cp)
			}
		}

		baseErr := eb.Build().Stringer("sectionName", sectionName).Stringer("tableName", tableName)

		{
			scalarIRH := sectionIRH.DeriveSubHolder(deriveSubHolderSelectScalar)
			nonScalarIRH := sectionIRH.DeriveSubHolder(deriveSubHolderSelectNonScalar)
			err = inst.composeSectionClassAndFactoryCode(sectionIRH, tableRowConfig, scalarIRH, nonScalarIRH)
			if err != nil {
				err = baseErr.Errorf("unable to compose section class and factory code: %w", err)
				return
			}
			err = inst.composeSectionCode(sectionIRH, tableRowConfig, scalarIRH, nonScalarIRH)
			if err != nil {
				err = baseErr.Errorf("unable to compose section code: %w", err)
				return
			}
		}

		{
			err = inst.composeAttributeClassAndFactoryCode(sectionIRH, tableRowConfig)
			if err != nil {
				err = baseErr.Errorf("unable to compose attribute class and factoy code: %w", err)
				return
			}
			err = inst.composeAttributeCode(sectionIRH, tableRowConfig)
			if err != nil {
				err = baseErr.Errorf("unable to compose attribute code: %w", err)
				return
			}
		}
	}

	return
}

func NewDefaultGoClassNamer() *DefaultGoClassNamer {
	return &DefaultGoClassNamer{}
}

func (inst *DefaultGoClassNamer) ComposeSchemaFactoryName(tableName naming.StylableName) (functionName string, err error) {
	return "createRecordBuilder", nil
}

func (inst *DefaultGoClassNamer) ComposeEntityClassName(tableName naming.StylableName) (fullClassName string, err error) {
	return "InEntity", nil
}

func (inst *DefaultGoClassNamer) ComposeSectionClassName(tableName naming.StylableName, sectionName naming.StylableName, sectionIndex int, sectionCount int) (fullClassName string, err error) {
	return "InEntity" + sectionName.Convert(naming.NamingStyleUpperCamelCase).String(), nil
}

func (inst *DefaultGoClassNamer) ComposeAttributeClassName(tableName naming.StylableName, sectionName naming.StylableName, sectionIndex int, sectionCount int) (fullClassName string, err error) {
	return "InEntity" + sectionName.Convert(naming.NamingStyleUpperCamelCase).String() + "InAttr", nil
}

func (inst *DefaultGoClassNamer) PromiseToBeReferentialTransparent() (_ functional.InterfaceIsReferentialTransparentType) {
	return
}

func NewMultiTablePerPackageGoClassNamer() *MultiTablePerPackageClassNamer {
	return &MultiTablePerPackageClassNamer{}
}

func (inst *MultiTablePerPackageClassNamer) ComposeSchemaFactoryName(tableName naming.StylableName) (functionName string, err error) {
	return "createRecordBuilder" + tableName.Convert(naming.NamingStyleUpperCamelCase).String(), nil
}

func (inst *MultiTablePerPackageClassNamer) ComposeEntityClassName(tableName naming.StylableName) (fullClassName string, err error) {
	return "InEntity" + tableName.Convert(naming.NamingStyleUpperCamelCase).String(), nil
}

func (inst *MultiTablePerPackageClassNamer) ComposeSectionClassName(tableName naming.StylableName, sectionName naming.StylableName, sectionIndex int, sectionCount int) (fullClassName string, err error) {
	return "InEntity" + tableName.Convert(naming.NamingStyleUpperCamelCase).String() + "Section" + sectionName.Convert(naming.NamingStyleUpperCamelCase).String(), nil
}

func (inst *MultiTablePerPackageClassNamer) ComposeAttributeClassName(tableName naming.StylableName, sectionName naming.StylableName, sectionIndex int, sectionCount int) (fullClassName string, err error) {
	return "InEntity" + tableName.Convert(naming.NamingStyleUpperCamelCase).String() + "Section" + sectionName.Convert(naming.NamingStyleUpperCamelCase).String() + "InAttr", nil
}

func (inst *MultiTablePerPackageClassNamer) PromiseToBeReferentialTransparent() (_ functional.InterfaceIsReferentialTransparentType) {
	return
}

var _ TechnologySpecificBuilderI = (*GoClassBuilder)(nil)
