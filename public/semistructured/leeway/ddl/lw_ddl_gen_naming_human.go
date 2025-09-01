package ddl

import (
	"slices"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/base62"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	encodingaspects2 "github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	useaspects2 "github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
)

const IdPrefix string = "id"
const TimestampPrefix string = "ts"
const RoutingPrefix string = "ro"
const LifecyclePrefix string = "lc"
const TransactionPrefix string = "tx"
const OpaquePrefix string = "oq"
const TaggedValuePrefix string = "tv"

const SeparatorExplanation = "separator"

type HumanReadableNamingConvention struct {
	tableManipulator    *common.TableManipulator
	separator           string
	canonicalTypeParser *canonicaltypes.Parser
	aspectCoder         *useaspects2.CanonicalEt7AspectCoder
}

var parseStructure13 positionData
var parseStructure21 positionData

type positionData struct {
	prefixIndex         int
	sectionNameIndex    int
	columnNameIndex     int
	roleIndex           int
	canonicalTypeIndex  int
	encodingHintsIndex  int
	useAspectsIndex     int
	tableRowConfigIndex int
	valueSemanticsIndex int
	coSectionGroupIndex int
	streamingGroupIndex int
}

func addParseData(dest *positionData, s []string) {
	dest.prefixIndex = slices.Index(s, componentPrefix)
	dest.sectionNameIndex = slices.Index(s, componentSectionName)
	dest.columnNameIndex = slices.Index(s, componentColumnName)
	dest.roleIndex = slices.Index(s, componentRole)
	dest.canonicalTypeIndex = slices.Index(s, componentCanonicalType)
	dest.encodingHintsIndex = slices.Index(s, componentEncodingHints)
	dest.useAspectsIndex = slices.Index(s, componentUseAspects)
	dest.tableRowConfigIndex = slices.Index(s, componentTableRowConfig)
	dest.valueSemanticsIndex = slices.Index(s, componentValueSemantics)
	dest.coSectionGroupIndex = slices.Index(s, componentCoSectionGroup)
	dest.streamingGroupIndex = slices.Index(s, componentStreamingGroup)
	if dest.canonicalTypeIndex < 0 || dest.encodingHintsIndex < 0 ||
		dest.canonicalTypeIndex == dest.encodingHintsIndex {
		log.Panic().Msg("something is wrong with the naming components")
	}
}

const (
	componentPrefix         = "prefix"
	componentColumnName     = "name"
	componentSectionName    = "sectionName"
	componentRole           = "role"
	componentCanonicalType  = "canonicalType"
	componentEncodingHints  = "encodingHints"
	componentUseAspects     = "useAspects"
	componentValueSemantics = "valueSemantics"
	componentTableRowConfig = "tableRowConfig"
	componentCoSectionGroup = "coSectionGroup"
	componentStreamingGroup = "streamingGroup"
)

var ColumnsComponentsExplanation13 = []string{
	componentPrefix,
	SeparatorExplanation,
	componentColumnName,
	SeparatorExplanation,
	componentCanonicalType,
	SeparatorExplanation,
	componentEncodingHints,
	SeparatorExplanation,
	componentValueSemantics,
	SeparatorExplanation,
	componentTableRowConfig,
	SeparatorExplanation,
	componentStreamingGroup,
}
var ColumnsComponentsExplanation21 = []string{
	componentPrefix,
	SeparatorExplanation,
	componentSectionName,
	SeparatorExplanation,
	componentColumnName,
	SeparatorExplanation,
	componentRole,
	SeparatorExplanation,
	componentCanonicalType,
	SeparatorExplanation,
	componentEncodingHints,
	SeparatorExplanation,
	componentUseAspects,
	SeparatorExplanation,
	componentValueSemantics,
	SeparatorExplanation,
	componentTableRowConfig,
	SeparatorExplanation,
	componentCoSectionGroup,
	SeparatorExplanation,
	componentStreamingGroup,
}

var ErrUnhandledIntermediateColumnContextType = eh.Errorf("unhandled intermediate column context type")

func init() {
	addParseData(&parseStructure13, ColumnsComponentsExplanation13)
	addParseData(&parseStructure21, ColumnsComponentsExplanation21)
}

var ErrUnhandledNumberOfComponents = eh.Errorf("unhandled number of components")
var ErrNameComponentContainsSeparator = eh.Errorf("name component contains separator")
var ErrParseError = eh.Errorf("parse error")
var ErrInvalidColumns = eh.Errorf("invalid column name")
var ErrInvalidCanonicalType = eh.Errorf("invalid canonical type")
var ErrInvalidAspects = eh.Errorf("invalid useaspects")

var _ common.NamingConventionI = (*HumanReadableNamingConvention)(nil)
var _ common.NamingConventionFwdI = (*HumanReadableNamingConvention)(nil)

func plainItemTypeToPrefix(plainItemType common.PlainItemTypeE) (pfx string) {
	switch plainItemType {
	case common.PlainItemTypeEntityId:
		return IdPrefix
	case common.PlainItemTypeEntityTimestamp:
		return TimestampPrefix
	case common.PlainItemTypeEntityLifecycle:
		return LifecyclePrefix
	case common.PlainItemTypeEntityRouting:
		return RoutingPrefix
	case common.PlainItemTypeTransaction:
		return TransactionPrefix
	case common.PlainItemTypeOpaque:
		return OpaquePrefix
	}
	log.Panic().Stringer("plainItemType", plainItemType).Msg("unhandled plain item type")
	return
}
func prefixToPlainItemType(pfx string) (plainItemType common.PlainItemTypeE, err error) {
	switch pfx {
	case IdPrefix:
		return common.PlainItemTypeEntityId, nil
	case TimestampPrefix:
		return common.PlainItemTypeEntityTimestamp, nil
	case LifecyclePrefix:
		return common.PlainItemTypeEntityLifecycle, nil
	case RoutingPrefix:
		return common.PlainItemTypeEntityRouting, nil
	case TransactionPrefix:
		return common.PlainItemTypeTransaction, nil
	case OpaquePrefix:
		return common.PlainItemTypeOpaque, nil
	case TaggedValuePrefix:
		return common.PlainItemTypeNone, nil
	}
	err = eb.Build().Str("prefix", pfx).Errorf("unhandled prefix")
	return
}

func NewHumanReadableNamingConvention(separator string) (inst *HumanReadableNamingConvention, err error) {
	var tableManipulator *common.TableManipulator
	tableManipulator, err = common.NewTableManipulator()
	if err != nil {
		err = eh.Errorf("unable to create table manipulator: %w", err)
		return
	}
	inst = &HumanReadableNamingConvention{
		tableManipulator:    tableManipulator,
		separator:           separator,
		canonicalTypeParser: canonicaltypes.NewParser(),
	}
	return
}

func getParseStructure(l int) (p positionData, err error) {
	switch l {
	case 13:
		p = parseStructure13
	case 21:
		p = parseStructure21
	default:
		err = ErrUnhandledNumberOfComponents
	}
	return
}
func (inst *HumanReadableNamingConvention) ExtractValueSemantics(column common.PhysicalColumnDesc) (semantics valueaspects.AspectSet, err error) {
	var p positionData
	p, err = getParseStructure(len(column.NameComponents))
	if err != nil {
		err = eb.Build().Strs("components", column.NameComponents).Errorf("unable to extract value semantics: %w", err)
		return
	}
	semantics = valueaspects.AspectSet(column.NameComponents[p.valueSemanticsIndex])
	if !semantics.IsValid() {
		semantics = valueaspects.EmptyAspectSet
		err = eb.Build().Strs("components", column.NameComponents).Str("canonicalTypes", column.NameComponents[p.valueSemanticsIndex]).Errorf("unable to parse value semantics: %w", ErrParseError)
		return
	}
	return
}

func (inst *HumanReadableNamingConvention) ExtractPlainItemType(column common.PhysicalColumnDesc) (plainItemType common.PlainItemTypeE, err error) {
	var p positionData
	p, err = getParseStructure(len(column.NameComponents))
	if err != nil {
		err = eb.Build().Strs("components", column.NameComponents).Errorf("unable to extract plain item type: %w", err)
		return
	}
	plainItemType, err = prefixToPlainItemType(column.NameComponents[p.prefixIndex])
	if err != nil {
		err = eb.Build().Strs("components", column.NameComponents).Errorf("unable to extract plain item type: %w", err)
		return
	}
	return
}
func (inst *HumanReadableNamingConvention) ExtractTableRowConfig(column common.PhysicalColumnDesc) (tableRowConfig common.TableRowConfigE, err error) {
	var p positionData
	p, err = getParseStructure(len(column.NameComponents))
	if err != nil {
		err = eb.Build().Strs("components", column.NameComponents).Errorf("unable to extract table row config: %w", err)
		return
	}
	n, valid := base62.Decode(base62.Base62Num(column.NameComponents[p.tableRowConfigIndex]))
	tableRowConfig = common.TableRowConfigE(n)
	if !valid {
		err = eb.Build().Strs("components", column.NameComponents).Errorf("unable to parse table row config")
		return
	}
	if !tableRowConfig.IsValid() {
		err = eb.Build().Strs("components", column.NameComponents).Errorf("extracted table row config is invalid")
		return
	}
	return
}
func (inst *HumanReadableNamingConvention) ExtractEncodingHints(column common.PhysicalColumnDesc) (hints encodingaspects2.AspectSet, err error) {
	var p positionData
	p, err = getParseStructure(len(column.NameComponents))
	if err != nil {
		err = eb.Build().Strs("components", column.NameComponents).Errorf("unable to extract encoding hints: %w", err)
		return
	}
	hints = encodingaspects2.AspectSet(column.NameComponents[p.encodingHintsIndex])
	if !hints.IsValid() {
		hints = encodingaspects2.EmptyAspectSet
		err = eb.Build().Strs("components", column.NameComponents).Str("canonicalTypes", column.NameComponents[p.encodingHintsIndex]).Errorf("unable to parse encoding hints: %w", ErrParseError)
		return
	}
	return
}
func (inst *HumanReadableNamingConvention) ExtractCanonicalType(column common.PhysicalColumnDesc) (ct canonicaltypes.PrimitiveAstNodeI, err error) {
	var p positionData
	p, err = getParseStructure(len(column.NameComponents))
	if err != nil {
		err = eb.Build().Strs("components", column.NameComponents).Errorf("unable to extract canonical type: %w", err)
		return
	}
	var cto canonicaltypes.AstNodeI
	cts := column.NameComponents[p.canonicalTypeIndex]
	cto, err = inst.canonicalTypeParser.ParsePrimitiveTypeOrGroupAst(cts)
	if err != nil {
		err = eb.Build().Strs("components", column.NameComponents).Str("canonicalTypes", cts).Errorf("unable to parse canonical type: %w %w", err, ErrParseError)
		return
	}
	var ok bool
	ct, ok = cto.(canonicaltypes.PrimitiveAstNodeI)
	if !ok {
		err = eb.Build().Strs("components", column.NameComponents).Stringer("canonicalType", cto).Errorf("unable to extract primitive canonical type: type is not primitive")
		return
	}
	return
}
func (inst *HumanReadableNamingConvention) MapIntermediateToPhysicalColumns(cc common.IntermediateColumnContext, cp common.IntermediateColumnProps, in []common.PhysicalColumnDesc, tableRowConfig common.TableRowConfigE) (out []common.PhysicalColumnDesc, err error) {
	out = slices.Grow(in, len(cp.Names))
	if cc.IsPlainColumn() {
		pfx := plainItemTypeToPrefix(cc.PlainItemType)
		for i, name := range cp.Names {
			var p common.PhysicalColumnDesc
			p, err = inst.composePlainValueColumn(pfx, name.String(), cp.CanonicalType[i], cp.EncodingHints[i], cp.ValueSemantics[i], tableRowConfig, cc.StreamingGroup)
			if err != nil {
				err = eb.Build().Stringer("plainItemType", cc.PlainItemType).Stringer("columnName", name).Errorf("unable to compose physical column for plain value column: %w", err)
				return
			}
			out = append(out, p)
		}
	} else if cc.IsTaggedColumn() {
		secName := cc.SectionName
		secAsp := cc.UseAspects
		for i, name := range cp.Names {
			var p common.PhysicalColumnDesc
			p, err = inst.composeTaggedValuesColumn(secName.String(), secAsp, name.String(), cp.CanonicalType[i], cp.EncodingHints[i], cp.ValueSemantics[i], cp.Roles[i], tableRowConfig, cc.CoSectionGroup, cc.StreamingGroup)
			if err != nil {
				err = eb.Build().Stringer("sectionName", secName).Stringer("columnName", name).Errorf("unable to compose physical column for tagged value column: %w", err)
				return
			}
			out = append(out, p)
		}
	} else {
		err = ErrUnhandledIntermediateColumnContextType
	}
	return
}
func (inst *HumanReadableNamingConvention) composePlainValueColumn(prefix string, name string, ct canonicaltypes.PrimitiveAstNodeI, hints encodingaspects2.AspectSet, valueSemantics valueaspects.AspectSet, tableRowConfig common.TableRowConfigE, streamingGroup naming.Key) (column common.PhysicalColumnDesc, err error) {
	err = inst.checkNameComponent(name)
	if err != nil {
		err = eh.Errorf("column name is for the given naming convention: %w", err)
		return
	}
	if !ct.IsValid() {
		err = eb.Build().Stringer("canonicalType", ct).Str("prefix", prefix).Str("name", name).Errorf("%w", ErrInvalidCanonicalType)
		return
	}
	components := make([]string, 13)
	components[0] = prefix
	components[1] = inst.separator
	components[2] = name
	components[3] = inst.separator
	components[4] = ct.String()
	components[5] = inst.separator
	components[6] = hints.String()
	components[7] = inst.separator
	components[8] = valueSemantics.String()
	components[9] = inst.separator
	components[10] = base62.Encode(uint64(tableRowConfig)).String()
	components[11] = inst.separator
	components[12] = streamingGroup.String()
	column.NameComponents = components
	column.Comment = ""
	column.GeneratingNamingConvention = inst
	column.NameComponentsExplanation = ColumnsComponentsExplanation13
	return
}
func (inst *HumanReadableNamingConvention) composeTaggedValuesColumn(sectionName string, useAspects useaspects2.AspectSet, name string, ct canonicaltypes.AstNodeI, hints encodingaspects2.AspectSet, valueSemantics valueaspects.AspectSet, role common.ColumnRoleE, tableRowConfig common.TableRowConfigE, coSectionGroup naming.Key, streamingGroup naming.Key) (column common.PhysicalColumnDesc, err error) {
	err = inst.checkNameComponent(sectionName)
	if err != nil {
		return
	}
	err = inst.checkNameComponent(name)
	if err != nil {
		return
	}
	if !useAspects.IsValid() {
		err = eb.Build().Stringer("useAspects", useAspects).Str("sectionName", sectionName).Str("name", name).Errorf("%w", ErrInvalidAspects)
		return
	}
	if !ct.IsValid() {
		err = eb.Build().Stringer("canonicalType", ct).Str("sectionName", sectionName).Str("name", name).Errorf("%w", ErrInvalidAspects)
		return
	}
	components := make([]string, 21)
	components[0] = TaggedValuePrefix
	components[1] = inst.separator
	components[2] = sectionName
	components[3] = inst.separator
	components[4] = name
	components[5] = inst.separator
	components[6] = role.String()
	components[7] = inst.separator
	components[8] = ct.String()
	components[9] = inst.separator
	components[10] = hints.String()
	components[11] = inst.separator
	components[12] = useAspects.String()
	components[13] = inst.separator
	components[14] = valueSemantics.String()
	components[15] = inst.separator
	components[16] = base62.Encode(uint64(tableRowConfig)).String()
	components[17] = inst.separator
	components[18] = coSectionGroup.String()
	components[19] = inst.separator
	components[20] = streamingGroup.String()
	column.NameComponentsExplanation = ColumnsComponentsExplanation21
	column.NameComponents = components
	column.GeneratingNamingConvention = inst
	column.Comment = ""
	return
}
func (inst *HumanReadableNamingConvention) checkNameComponent(component string) (err error) {
	if strings.Contains(component, inst.separator) {
		return eb.Build().Str("component", component).Str("separator", inst.separator).Errorf("invalid name component: %w", ErrNameComponentContainsSeparator)
	}
	if component == "" {
		return ErrInvalidColumns
	}
	return
}
func (inst *HumanReadableNamingConvention) ParseColumns(columnNames []string) (phys []common.PhysicalColumnDesc, err error) {
	phys = make([]common.PhysicalColumnDesc, 0, len(columnNames))
	for _, c := range columnNames {
		var phy common.PhysicalColumnDesc
		phy, err = inst.ParseColumn(c)
		if err != nil {
			err = eb.Build().Str("columnName", c).Errorf("unable to parse column name: %w", err)
			return
		}
		phys = append(phys, phy)
	}
	return
}
func (inst *HumanReadableNamingConvention) DiscoverTableFromColumnNames(columnNames []string) (table common.TableDesc, tableRowConfig common.TableRowConfigE, err error) {
	var phys []common.PhysicalColumnDesc
	phys, err = inst.ParseColumns(columnNames)
	if err != nil {
		err = eh.Errorf("unable to parse column names: %w", err)
		return
	}
	return inst.discoverTableFromSortedPhysicalColumns(phys)
}
func (inst *HumanReadableNamingConvention) DiscoverTableFromPhysicalColumns(phys []common.PhysicalColumnDesc) (table common.TableDesc, tableRowConfig common.TableRowConfigE, err error) {
	return inst.discoverTableFromSortedPhysicalColumns(phys)
}
func (inst *HumanReadableNamingConvention) discoverTableFromSortedPhysicalColumns(phys []common.PhysicalColumnDesc) (table common.TableDesc, tableRowConfig common.TableRowConfigE, err error) {
	tbl := inst.tableManipulator
	first := true
	tbl.Reset()
	for _, phy := range phys {
		components := phy.NameComponents
		var trc string
		l := len(components)
		switch l {
		case 13:
			hints := encodingaspects2.AspectSet(components[parseStructure13.encodingHintsIndex])
			if !hints.IsValid() {
				err = eb.Build().Stringer("physicalColumn", phy).Str("component", components[parseStructure13.encodingHintsIndex]).Errorf("unable to parse encoding aspects (hints): %w", err)
				return
			}
			var ct canonicaltypes.PrimitiveAstNodeI
			ct, err = inst.canonicalTypeParser.ParsePrimitiveTypeAst(components[parseStructure13.canonicalTypeIndex])
			if err != nil {
				err = eb.Build().Stringer("physicalColumn", phy).Str("component", components[parseStructure13.canonicalTypeIndex]).Errorf("unable to parse canonical type: %w", err)
				return
			}
			var itemType common.PlainItemTypeE
			itemType, err = prefixToPlainItemType(components[parseStructure13.prefixIndex])
			if err != nil {
				err = eb.Build().Stringer("physicalColumn", phy).Str("component", components[parseStructure13.prefixIndex]).Errorf("unable to translate prefix to plain item type: %w", err)
				return
			}
			valueSemantics := valueaspects.AspectSet(components[parseStructure13.valueSemanticsIndex])
			if !valueSemantics.IsValid() {
				err = eb.Build().Stringer("physicalColumn", phy).Str("component", components[parseStructure13.valueSemanticsIndex]).Errorf("unable to parse value semantics aspects: %w", err)
				return
			}
			name := components[parseStructure13.columnNameIndex]
			var nameS naming.StylableName
			nameS, err = naming.MakeStylableName(name)
			if err != nil {
				err = eb.Build().Stringer("physicalColumn", phy).Str("component", components[parseStructure13.columnNameIndex]).Errorf("column name is not a valid stylable name: %w", err)
				return
			}
			var streamingGroupK naming.Key
			streamingGroup := components[parseStructure13.streamingGroupIndex]
			streamingGroupK, err = naming.MakeKey(streamingGroup)
			if err != nil {
				err = eb.Build().Stringer("physicalColumn", phy).Str("component", components[parseStructure13.streamingGroupIndex]).Errorf("co section group is not a valid key: %w", err)
				return
			}
			if streamingGroupK != "" {
				switch itemType {
				case common.PlainItemTypeOpaque:
					tbl.SetOpaqueColumnStreamingGroup(streamingGroupK)
					break
				default:
					err = eb.Build().Stringer("physicalColumn", phy).Stringer("plainItemType", itemType).Str("component", components[parseStructure13.streamingGroupIndex]).Errorf("found non-empty streaming group index in unsupported plain item type: %w", err)
					return
				}
			}

			tbl.AddPlainValueItem(itemType, nameS, ct, hints, valueSemantics)
			trc = components[parseStructure13.tableRowConfigIndex]
			break
		case 21:
			switch components[parseStructure21.prefixIndex] {
			case TaggedValuePrefix:
				var ct canonicaltypes.PrimitiveAstNodeI
				ct, err = inst.canonicalTypeParser.ParsePrimitiveTypeAst(components[parseStructure21.canonicalTypeIndex])
				if err != nil {
					err = eb.Build().Stringer("physicalColumn", phy).Str("component", components[parseStructure21.canonicalTypeIndex]).Errorf("unable to parse canonical type: %w", err)
					return
				}
				hints := encodingaspects2.AspectSet(components[parseStructure21.encodingHintsIndex])
				if !hints.IsValid() {
					err = eb.Build().Stringer("physicalColumn", phy).Str("component", components[parseStructure21.encodingHintsIndex]).Errorf("unable to parse encoding aspects (hints) type: %w", err)
					return
				}
				useAspects := useaspects2.AspectSet(components[parseStructure21.useAspectsIndex])
				if useAspects != "" && !useAspects.IsValid() {
					err = eb.Build().Stringer("physicalColumn", phy).Str("component", components[parseStructure21.useAspectsIndex]).Errorf("unable to parse use aspects")
					return
				}
				valueSemantics := valueaspects.AspectSet(components[parseStructure21.valueSemanticsIndex])
				if valueSemantics != "" && !valueSemantics.IsValid() {
					err = eb.Build().Stringer("physicalColumn", phy).Str("component", components[parseStructure21.valueSemanticsIndex]).Errorf("unable to parse value semantic aspects")
					return
				}
				sectionName := components[parseStructure21.sectionNameIndex]
				role := common.ColumnRoleE(components[parseStructure21.roleIndex])
				var sectionNameS naming.StylableName
				sectionNameS, err = naming.MakeStylableName(sectionName)
				if err != nil {
					err = eb.Build().Stringer("physicalColumn", phy).Str("component", components[parseStructure21.sectionNameIndex]).Errorf("section name is not a valid stylable name: %w", err)
					return
				}
				var coSectionGroupK, streamingGroupK naming.Key
				coSectionGroup := components[parseStructure21.coSectionGroupIndex]
				streamingGroup := components[parseStructure21.streamingGroupIndex]
				coSectionGroupK, err = naming.MakeKey(coSectionGroup)
				if err != nil {
					err = eb.Build().Stringer("physicalColumn", phy).Str("component", components[parseStructure21.coSectionGroupIndex]).Errorf("co section group is not a valid key: %w", err)
					return
				}
				streamingGroupK, err = naming.MakeKey(streamingGroup)
				if err != nil {
					err = eb.Build().Stringer("physicalColumn", phy).Str("component", components[parseStructure21.streamingGroupIndex]).Errorf("streaming group index is not a valid key: %w", err)
					return
				}

				if role == common.ColumnRoleValue {
					name := components[parseStructure21.columnNameIndex]
					var nameS naming.StylableName
					nameS, err = naming.MakeStylableName(name)
					if err != nil {
						err = eb.Build().Stringer("physicalColumn", phy).Str("component", components[parseStructure13.columnNameIndex]).Errorf("column name is not a valid stylable name: %w", err)
						return
					}

					tbl.MergeTaggedValueColumn(sectionNameS,
						nameS,
						ct,
						hints,
						valueSemantics,
						useAspects,
						common.MembershipSpecNone,
						coSectionGroupK,
						streamingGroupK)
				} else {
					membership := common.MembershipSpecNone
					switch role {
					case common.ColumnRoleHighCardRef:
						membership = membership.AddHighCardRefOnly()
						break
					case common.ColumnRoleHighCardRefParametrized:
						membership = membership.AddHighCardRefParametrized()
						break
					case common.ColumnRoleHighCardVerbatim:
						membership = membership.AddHighCardVerbatim()
						break
					case common.ColumnRoleLowCardRef:
						membership = membership.AddLowCardRefOnly()
						break
					case common.ColumnRoleLowCardRefParametrized:
						membership = membership.AddLowCardRefParametrized()
						break
					case common.ColumnRoleLowCardVerbatim:
						membership = membership.AddLowCardVerbatim()
						break
					case common.ColumnRoleMixedLowCardVerbatim:
						membership = membership.AddMixedLowCardVerbatimHighCardParameters()
						break
					case common.ColumnRoleMixedLowCardRef:
						membership = membership.AddMixedLowCardRefHighCardParameters()
						break
					case common.ColumnRoleMixedRefHighCardParameters, common.ColumnRoleMixedVerbatimHighCardParameters:
						// mixed, trigger on other
						break
					case common.ColumnRoleCardinality, common.ColumnRoleLength, common.ColumnRoleCusumCardinality, common.ColumnRoleCusumLength:
						// support column
					}
					tbl.MergeTaggedValueSection(sectionNameS,
						useAspects,
						membership,
						coSectionGroupK,
						streamingGroupK)
				}
				break
			default:
				err = eb.Build().Stringer("physicalColumn", phy).Str("prefix", components[0]).Errorf("unknown column prefix")
				return
			}
			trc = components[parseStructure21.tableRowConfigIndex]
			break
		default:
			err = eb.Build().Stringer("physicalColumn", phy).Int("components", l).Errorf("unhandled number of components")
			return
		}
		{ // tableRowConfig
			var valid bool
			var tmp uint64
			tmp, valid = base62.Decode(base62.Base62Num(trc))
			if !valid {
				err = eb.Build().Stringer("physicalColumn", phy).Errorf("unable to parse embedded table row config: %w", err)
				return
			}
			if first {
				tableRowConfig = common.TableRowConfigE(tmp)
				if !tableRowConfig.IsValid() {
					err = eb.Build().Stringer("physicalColumn", phy).Uint64("tableRowConfig", tmp).Errorf("column contains invalid table row config")
					return
				}
				first = false
			} else {
				if common.TableRowConfigE(tmp) != tableRowConfig {
					err = eb.Build().Stringer("tableRowConfig1", common.TableRowConfigE(tmp)).Stringer("tableRowConfig2", tableRowConfig).Errorf("table row configuration is inconsistent between columns")
					return
				}
			}
		}
	}
	table, err = tbl.BuildTableDesc()
	return
}
func (inst *HumanReadableNamingConvention) ParseColumn(fullColumnName string) (column common.PhysicalColumnDesc, err error) {
	var components []string
	var l int
	{
		tmp := strings.Split(fullColumnName, inst.separator) // TODO use SplitN?
		u := len(tmp)
		l = u + u - 1
		components = make([]string, 0, l)
		for i, t := range tmp {
			components = append(components, t)
			if i != u-1 {
				components = append(components, inst.separator)
			}
		}
	}

	switch l {
	case 13:
		_, err = inst.canonicalTypeParser.ParsePrimitiveTypeOrGroupAst(components[parseStructure13.canonicalTypeIndex])
		if err != nil {
			err = eb.Build().Str("column", fullColumnName).Str("component", components[l-1]).Errorf("unable to parse canonical type: %w", err)
			return
		}
		switch components[parseStructure13.prefixIndex] {
		case IdPrefix,
			TimestampPrefix,
			RoutingPrefix,
			LifecyclePrefix,
			TransactionPrefix,
			OpaquePrefix:
			break
		default:
			err = eb.Build().Str("column", fullColumnName).Str("prefix", components[0]).Errorf("unknown column prefix")
			return
		}
		column.NameComponents = components
		column.NameComponentsExplanation = ColumnsComponentsExplanation13
		column.Comment = ""
		column.GeneratingNamingConvention = inst
		break
	case 21:
		_, err = inst.canonicalTypeParser.ParsePrimitiveTypeOrGroupAst(components[parseStructure21.canonicalTypeIndex])
		if err != nil {
			err = eb.Build().Str("column", fullColumnName).Str("component", components[parseStructure21.canonicalTypeIndex]).Errorf("unable to parse canonical type: %w", err)
			return
		}
		switch components[parseStructure21.prefixIndex] {
		case TaggedValuePrefix:
			break
		default:
			err = eb.Build().Str("column", fullColumnName).Str("prefix", components[parseStructure21.prefixIndex]).Errorf("unknown column prefix")
			return
		}
		column.NameComponents = components
		column.NameComponentsExplanation = ColumnsComponentsExplanation21
		column.Comment = ""
		column.GeneratingNamingConvention = inst
		break
	default:
		err = eb.Build().Str("column", fullColumnName).Int("components", l).Str("separator", inst.separator).Errorf("unknown number of name components")
		return
	}
	return
}
