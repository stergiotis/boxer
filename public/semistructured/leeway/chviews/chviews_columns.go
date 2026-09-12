package chviews

import (
	"fmt"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/semistructured/leeway/base62"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsql"
)

// Layout names, as the view reports them.
const (
	// LayoutForeign is a name this convention did not compose. It is a claim
	// about the name and not about the table it belongs to, which may be
	// leeway with one hand-added or MATERIALIZED column (ADR-0226 §SD2).
	LayoutForeign = "foreign"
	// LayoutPlain is the backbone arity, which carries no section, role,
	// use-aspects or co-section group.
	LayoutPlain = "plain"
	// LayoutTagged is the payload arity, which carries all of them.
	LayoutTagged = "tagged"
)

// passthroughColumns are carried from system.columns unchanged. Listed rather
// than taken with `*` so a ClickHouse upgrade that adds a column does not
// silently change the view's shape.
var passthroughColumns = []string{
	"database",
	"table",
	"name",
	"type",
	"position",
	"comment",
	"data_compressed_bytes",
	"data_uncompressed_bytes",
	"marks_bytes",
	"is_in_partition_key",
	"is_in_sorting_key",
	"is_in_primary_key",
}

// decodedColumns are what the decode adds, in the order the view presents
// them. Kept beside the body so a caller can name the contract without
// parsing SQL.
var decodedColumns = []string{
	"layout",
	"prefix",
	"section",
	"leeway_column",
	"role",
	"lane_kind",
	"canonical_type",
	"table_row_config",
	"co_section_group",
	"streaming_group",
	"handle",
	"encoding_hints",
	"use_aspects",
	"value_semantics",
	"aspects_decodable",
	"encoding_hints_seg",
	"use_aspects_seg",
	"value_semantics_seg",
}

// ColumnsColumns returns the view's result columns in order.
func ColumnsColumns() (names []string) {
	names = make([]string, 0, len(passthroughColumns)+len(decodedColumns))
	names = append(names, passthroughColumns...)
	names = append(names, decodedColumns...)
	return
}

// fieldExpr renders the expression that reads one named name-component out of
// the split name, choosing the field position per layout. A component absent
// from a layout reads as the empty string there — a plain name has no
// section, role, use-aspects or co-section group, and reporting ” is what
// lets one view carry both arities.
func fieldExpr(component string) (expr string) {
	ti, tagged := ddl.NameLayoutTagged.FieldIndex(component)
	pi, plain := ddl.NameLayoutPlain.FieldIndex(component)
	switch {
	case tagged && plain:
		return fmt.Sprintf("multiIf(layout = %s, parts[%d], layout = %s, parts[%d], '')",
			sqlLiteral(LayoutTagged), ti, sqlLiteral(LayoutPlain), pi)
	case tagged:
		return fmt.Sprintf("if(layout = %s, parts[%d], '')", sqlLiteral(LayoutTagged), ti)
	case plain:
		return fmt.Sprintf("if(layout = %s, parts[%d], '')", sqlLiteral(LayoutPlain), pi)
	}
	log.Panic().Str("component", component).Msg("name component belongs to no layout")
	return
}

// plainPrefixTables pairs each backbone prefix with the section a handle
// addresses it under, so a plain column reports a section the resolver
// accepts (ADR-0116).
func plainPrefixTables() (prefixes []string, sections []string) {
	prefixes = make([]string, 0, len(common.AllPlainItemTypes))
	sections = make([]string, 0, len(common.AllPlainItemTypes))
	for _, pit := range common.AllPlainItemTypes {
		section := ddl.PlainSectionName(pit)
		if section == "" {
			continue // the tagged case, and any item type no handle addresses
		}
		prefix, ok := ddl.PlainItemTypePrefix(pit)
		if !ok {
			continue
		}
		prefixes = append(prefixes, prefix)
		sections = append(sections, section)
	}
	return
}

// laneKindTables pair each role spelling with its lane classification, from
// the one classifier the transform contract's two validation halves already
// share (ADR-0181 §SD5). An unlisted role reports unknown, never a guess.
func laneKindTables() (roles []string, kinds []string) {
	roles = make([]string, 0, len(common.AllColumnRoles))
	kinds = make([]string, 0, len(common.AllColumnRoles))
	for _, role := range common.AllColumnRoles {
		if role == common.ColumnRoleUnspecific {
			continue // spelled empty; it is the default case, not a table entry
		}
		kind, _ := lwsql.ClassifyLaneRole(role)
		roles = append(roles, string(role))
		kinds = append(kinds, kind.String())
	}
	return
}

// rowConfigTables pair each row config's base62 code — the spelling that rides
// in the name — with its enum name.
func rowConfigTables() (codes []string, names []string) {
	codes = make([]string, 0, len(common.AllTableRowConfigs))
	names = make([]string, 0, len(common.AllTableRowConfigs))
	for _, cfg := range common.AllTableRowConfigs {
		codes = append(codes, base62.Encode(uint64(cfg)).String())
		names = append(names, cfg.String())
	}
	return
}

// aspectNames renders a guarded call into chpack's aspect vocabulary. The
// guard is on the argument, not on a branch: LW_ASPECT_DECODE throws on a
// char outside the v0 range, and a scan that reaches every table on a server
// must not let one unrecognised name fail the whole query. A segment the
// codec cannot read is handed in as empty, and aspects_decodable is how a
// reader tells that apart from a genuinely empty set.
func aspectNames(fn string, seg string) (expr string) {
	return fmt.Sprintf("%s(if(LW_ASPECT_DECODABLE(%s), %s, ''))", fn, seg, seg)
}

func columnsBody() (sql string) {
	plainPrefixes, plainSections := plainPrefixTables()
	roles, laneKinds := laneKindTables()
	rowConfigCodes, rowConfigNames := rowConfigTables()

	layoutExpr := fmt.Sprintf("multiIf(length(parts) = %d AND parts[1] = %s, %s, length(parts) = %d AND has(%s, parts[1]), %s, %s)",
		ddl.NameLayoutTagged.FieldCount(), sqlLiteral(ddl.TaggedValuePrefix), sqlLiteral(LayoutTagged),
		ddl.NameLayoutPlain.FieldCount(), sqlStringArray(plainPrefixes), sqlLiteral(LayoutPlain),
		sqlLiteral(LayoutForeign))

	sectionIndex, _ := ddl.NameLayoutTagged.FieldIndex(ddl.ComponentSectionName)
	sectionExpr := fmt.Sprintf("multiIf(layout = %s, parts[%d], layout = %s, %s, '')",
		sqlLiteral(LayoutTagged), sectionIndex, sqlLiteral(LayoutPlain),
		sqlTransform("parts[1]", plainPrefixes, plainSections, "''"))

	// A backbone column is a value column by construction: the plain layout
	// carries no support lanes, which is why it also carries no role.
	laneKindExpr := fmt.Sprintf("multiIf(layout = %s, '', layout = %s, %s, %s)",
		sqlLiteral(LayoutForeign), sqlLiteral(LayoutPlain), sqlLiteral(lwsql.LaneKindValue.String()),
		sqlTransform("role", roles, laneKinds, sqlLiteral(lwsql.LaneKindUnknown.String())))

	rowConfigExpr := sqlTransform("row_config_seg", rowConfigCodes, rowConfigNames,
		"if(row_config_seg = '', '', concat('unknown-', row_config_seg))")

	inner := make([][2]string, 0, len(passthroughColumns)+len(decodedColumns)+4)
	for _, c := range passthroughColumns {
		inner = append(inner, [2]string{c, c})
	}
	inner = append(inner,
		[2]string{fmt.Sprintf("splitByString(%s, name)", separatorLiteral()), "parts"},
		[2]string{layoutExpr, "layout"},
		[2]string{fmt.Sprintf("if(layout = %s, '', parts[1])", sqlLiteral(LayoutForeign)), "prefix"},
		[2]string{sectionExpr, "section"},
		[2]string{fieldExpr(ddl.ComponentColumnName), "leeway_column"},
		[2]string{fieldExpr(ddl.ComponentRole), "role"},
		[2]string{laneKindExpr, "lane_kind"},
		[2]string{fieldExpr(ddl.ComponentCanonicalType), "canonical_type"},
		[2]string{fieldExpr(ddl.ComponentTableRowConfig), "row_config_seg"},
		[2]string{rowConfigExpr, "table_row_config"},
		[2]string{fieldExpr(ddl.ComponentCoSectionGroup), "co_section_group"},
		[2]string{fieldExpr(ddl.ComponentStreamingGroup), "streaming_group"},
		// The handle's colon is ADR-0116's colon-always marker, not the name
		// separator; they coincide today and are not the same decision.
		[2]string{"if(section = '', '', concat(section, ':', leeway_column))", "handle"},
		[2]string{fieldExpr(ddl.ComponentEncodingHints), "encoding_hints_seg"},
		[2]string{fieldExpr(ddl.ComponentUseAspects), "use_aspects_seg"},
		[2]string{fieldExpr(ddl.ComponentValueSemantics), "value_semantics_seg"},
		// Parenthesised: an alias binds tighter than AND, so the bare chain
		// would name only its last conjunct.
		[2]string{"(LW_ASPECT_DECODABLE(encoding_hints_seg) AND LW_ASPECT_DECODABLE(use_aspects_seg) AND LW_ASPECT_DECODABLE(value_semantics_seg))", "aspects_decodable"},
		[2]string{aspectNames("LW_ASPECT_NAMES_ENC", "encoding_hints_seg"), "encoding_hints"},
		[2]string{aspectNames("LW_ASPECT_NAMES_USE", "use_aspects_seg"), "use_aspects"},
		[2]string{aspectNames("LW_ASPECT_NAMES_SEM", "value_semantics_seg"), "value_semantics"},
	)

	outer := make([][2]string, 0, len(passthroughColumns)+len(decodedColumns))
	for _, c := range ColumnsColumns() {
		outer = append(outer, [2]string{c, c})
	}

	sql = "SELECT\n" + selectList(outer) + "\nFROM\n(\n" +
		indent("SELECT\n"+selectList(inner)+"\nFROM system.columns\nWHERE "+notSystemDatabases("database"), "    ") +
		"\n)"
	return
}
