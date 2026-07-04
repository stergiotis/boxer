package clickhouse

import (
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	"github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
)

// This file is the table-clause seam (ADR-0102): the ClickHouse-specific
// composition of a complete CREATE TABLE from the neutral IR plus
// generation-time TableOptions. Before it, every consumer hand-wrapped the
// generated column body in clause strings — and had to know the physical
// (encoded) column names to write a working ORDER BY. Table-level clauses
// are materialization policy, so they live here, with the target, never in
// the neutral TableDesc (ADR-0074 discipline).

// ColumnRef addresses one physical column by leeway-level coordinates;
// ComposeCreateTable resolves it to the quoted physical (encoded) name
// through the IR and the naming convention. Exactly one selector must be
// used:
//   - Plain: a plain column by its leeway name;
//   - PlainItem: a plain column by its PlainItemTypeE lane (for callers
//     that bind roles, e.g. the recordstore generator);
//   - Section + Column: a tagged section's value column;
//   - Section + Role: a tagged section's channel/support column (e.g.
//     common.ColumnRoleLowCardRef — the membership identity column the
//     read-back presence conjuncts prune on).
type ColumnRef struct {
	Plain     naming.StylableName
	PlainItem common.PlainItemTypeE
	Section   naming.StylableName
	Column    naming.StylableName
	Role      common.ColumnRoleE
}

// IndexSpec is one data-skipping index. Type is the raw ClickHouse index
// type expression (e.g. "bloom_filter", "bloom_filter(0.01)", "minmax",
// "set(100)"); Granularity 0 omits the GRANULARITY clause (ClickHouse
// default); an empty Name derives a stable identifier from the reference.
type IndexSpec struct {
	Ref         ColumnRef
	Type        string
	Granularity uint32
	Name        string
}

// CreateModeE selects the CREATE TABLE statement form.
type CreateModeE uint8

const (
	CreateModePlain CreateModeE = iota
	CreateModeIfNotExists
	CreateModeOrReplace
)

// TableOptions are the generation-time table-level clauses. Engine is
// required; PartitionBy and TTL are raw passthrough expressions in v1
// (structured treatment deferred until a consumer partitions); Tail is the
// final raw escape hatch, emitted verbatim after every structured clause.
type TableOptions struct {
	Mode        CreateModeE
	Engine      string // rendered as "ENGINE = <Engine>"
	OrderBy     []ColumnRef
	PartitionBy string // raw ClickHouse expression, e.g. "toYYYYMM(ts…)"
	TTL         string // raw ClickHouse expression
	Indexes     []IndexSpec
	Settings    []string // joined with ", " after SETTINGS
	Tail        string
}

// ComposeCreateTable renders the complete CREATE TABLE statement: the
// generated column body, the INDEX clauses, and the table-level clauses in
// ClickHouse's canonical order (ENGINE, PARTITION BY, ORDER BY, TTL,
// SETTINGS), with every ColumnRef resolved to its quoted physical name.
// tableName is emitted verbatim (qualify and quote at the call site when
// needed).
func ComposeCreateTable(tableName string, ir *common.IntermediateTableRepresentation, tableRowConfig common.TableRowConfigE, conv common.NamingConventionI, opts TableOptions) (sql string, err error) {
	if opts.Engine == "" {
		err = eh.Errorf("compose create table %s: Engine is required", tableName)
		return
	}
	var b strings.Builder
	switch opts.Mode {
	case CreateModePlain:
		b.WriteString("CREATE TABLE ")
	case CreateModeIfNotExists:
		b.WriteString("CREATE TABLE IF NOT EXISTS ")
	case CreateModeOrReplace:
		b.WriteString("CREATE OR REPLACE TABLE ")
	default:
		err = eh.Errorf("compose create table %s: unknown create mode %d", tableName, opts.Mode)
		return
	}
	b.WriteString(tableName)
	b.WriteString(" (\n")

	tech := NewTechnologySpecificCodeGenerator()
	tech.SetCodeBuilder(&b)
	err = ddl.NewGeneratorDriver().GenerateColumnsCode(ir.IterateColumnProps(), tableRowConfig, conv, tech,
		func(hint encodingaspects.AspectE) (bool, string) { return true, "" })
	if err != nil {
		err = eh.Errorf("compose create table %s: columns: %w", tableName, err)
		return
	}

	for _, idx := range opts.Indexes {
		if idx.Type == "" {
			err = eb.Build().Str("table", tableName).Errorf("index spec has no type expression")
			return
		}
		var col string
		col, err = resolveColumnRef(idx.Ref, ir, tableRowConfig, conv)
		if err != nil {
			err = eh.Errorf("compose create table %s: index column: %w", tableName, err)
			return
		}
		name := idx.Name
		if name == "" {
			name = deriveIndexName(idx.Ref)
		}
		b.WriteString(",\n\tINDEX ")
		b.WriteString(name)
		b.WriteString(" ")
		b.WriteString(col)
		b.WriteString(" TYPE ")
		b.WriteString(idx.Type)
		if idx.Granularity > 0 {
			b.WriteString(" GRANULARITY ")
			b.WriteString(uitoa(idx.Granularity))
		}
	}

	b.WriteString("\n) ENGINE = ")
	b.WriteString(opts.Engine)
	if opts.PartitionBy != "" {
		b.WriteString("\nPARTITION BY ")
		b.WriteString(opts.PartitionBy)
	}
	if len(opts.OrderBy) > 0 {
		cols := make([]string, 0, len(opts.OrderBy))
		for _, ref := range opts.OrderBy {
			var col string
			col, err = resolveColumnRef(ref, ir, tableRowConfig, conv)
			if err != nil {
				err = eh.Errorf("compose create table %s: order by: %w", tableName, err)
				return
			}
			cols = append(cols, col)
		}
		b.WriteString("\nORDER BY (")
		b.WriteString(strings.Join(cols, ", "))
		b.WriteString(")")
	}
	if opts.TTL != "" {
		b.WriteString("\nTTL ")
		b.WriteString(opts.TTL)
	}
	if len(opts.Settings) > 0 {
		b.WriteString("\nSETTINGS ")
		b.WriteString(strings.Join(opts.Settings, ", "))
	}
	if opts.Tail != "" {
		b.WriteString("\n")
		b.WriteString(opts.Tail)
	}
	sql = b.String()
	return
}

// resolveColumnRef walks the IR, renders each candidate through the naming
// convention, and returns the double-quoted physical name of the single
// column the reference selects. Zero matches and ambiguous references are
// errors — a table clause naming the wrong column must fail at generation
// time, not at CREATE time.
func resolveColumnRef(ref ColumnRef, ir *common.IntermediateTableRepresentation, tableRowConfig common.TableRowConfigE, conv common.NamingConventionI) (quoted string, err error) {
	selectors := 0
	if ref.Plain != "" {
		selectors++
	}
	if ref.PlainItem != common.PlainItemTypeNone {
		selectors++
	}
	if ref.Section != "" {
		selectors++
		if (ref.Column == "") == (ref.Role == common.ColumnRoleUnspecific) {
			err = eb.Build().Str("section", string(ref.Section)).Errorf("a Section reference needs exactly one of Column or Role")
			return
		}
	}
	if selectors != 1 {
		err = eb.Build().Errorf("column reference must use exactly one selector (Plain, PlainItem or Section), got %d", selectors)
		return
	}

	var matches []string
	for cc, cp := range ir.IterateColumnProps() {
		var phys []common.PhysicalColumnDesc
		phys, err = conv.MapIntermediateToPhysicalColumns(cc, *cp, nil, tableRowConfig)
		if err != nil {
			err = eh.Errorf("render physical columns: %w", err)
			return
		}
		for i := range cp.Names {
			var hit bool
			switch {
			case ref.Plain != "":
				hit = cc.SectionName == "" && cp.Names[i] == ref.Plain
			case ref.PlainItem != common.PlainItemTypeNone:
				hit = cc.SectionName == "" && cc.PlainItemType == ref.PlainItem
			case ref.Column != "":
				hit = cc.SectionName == ref.Section && cp.Names[i] == ref.Column && cp.Roles[i] == common.ColumnRoleValue
			default:
				hit = cc.SectionName == ref.Section && cp.Roles[i] == ref.Role
			}
			if hit {
				matches = append(matches, `"`+phys[i].String()+`"`)
			}
		}
	}
	switch len(matches) {
	case 0:
		err = eb.Build().Str("ref", refString(ref)).Errorf("column reference resolves to no physical column")
	case 1:
		quoted = matches[0]
	default:
		err = eb.Build().Str("ref", refString(ref)).Int("matches", len(matches)).Errorf("column reference is ambiguous")
	}
	return
}

func refString(ref ColumnRef) string {
	switch {
	case ref.Plain != "":
		return "plain:" + string(ref.Plain)
	case ref.PlainItem != common.PlainItemTypeNone:
		return "plainItem:" + ref.PlainItem.String()
	case ref.Column != "":
		return "section:" + string(ref.Section) + "/column:" + string(ref.Column)
	default:
		return "section:" + string(ref.Section) + "/role:" + ref.Role.String()
	}
}

// deriveIndexName builds a stable identifier from the reference: "idx_"
// plus the selector parts with non-identifier bytes folded to '_'.
func deriveIndexName(ref ColumnRef) string {
	raw := refString(ref)
	var b strings.Builder
	b.WriteString("idx_")
	for i := 0; i < len(raw); i++ {
		ch := raw[i]
		switch {
		case ch >= 'a' && ch <= 'z', ch >= 'A' && ch <= 'Z', ch >= '0' && ch <= '9', ch == '_':
			b.WriteByte(ch)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

func uitoa(v uint32) string {
	if v == 0 {
		return "0"
	}
	var buf [10]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
