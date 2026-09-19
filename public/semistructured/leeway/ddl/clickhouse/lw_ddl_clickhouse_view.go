package clickhouse

import (
	"strings"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/marshalling"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
)

// This file is the view half of the table-clause seam (ADR-0102 Update
// 2026-09-18): a leeway table materialised as a ClickHouse view. A derived
// table — a per-kind cut of a shared table, a bridge between two layouts —
// has no writer and follows its source, so it is a view; but it is still a
// leeway table, read by tooling that classifies columns by their physical
// (encoded) names. Before this, such a view spelled every physical name by
// hand. Here the names, their order and their types come from the same IR
// the CREATE TABLE is composed from, and the caller supplies only what it
// alone knows: the expression that fills each column, and the source.

// ViewColumn describes one physical column to the expression callback, by
// the leeway coordinates the column was declared under.
type ViewColumn struct {
	// Physical is the physical (encoded) column name, unquoted — the alias
	// the view gives the expression.
	Physical string
	// Type is the ClickHouse type the table's DDL declares for the column,
	// without its CODEC clause.
	Type string
	// Section is the tagged section the column belongs to; empty for a
	// plain column.
	Section naming.StylableName
	// Name is the leeway column name as authored in the TableDesc.
	Name naming.StylableName
	// Role is the column's role in its section (value, a membership
	// channel, a support column); ColumnRoleUnspecific for a plain column.
	Role common.ColumnRoleE
	// PlainItem is the plain column's lane; PlainItemTypeNone for a tagged
	// section's column.
	PlainItem common.PlainItemTypeE
}

// ViewExpr is the caller's SELECT expression for one column.
type ViewExpr struct {
	// SQL is the raw ClickHouse expression over the view's source. Required.
	SQL string
	// Exact states that SQL already has the column's declared Type, so it
	// is aliased as it stands. The default wraps it in CAST(… AS Type),
	// which is what keeps the view's types from drifting from the table's;
	// set Exact for a column passed through from a same-typed source, where
	// the cast would only stand between a filter on the view and the
	// source's primary key.
	Exact bool
}

// ViewOptions are the generation-time clauses of the view.
type ViewOptions struct {
	// Mode selects CREATE VIEW, CREATE VIEW IF NOT EXISTS or CREATE OR
	// REPLACE VIEW.
	Mode CreateModeE
	// Clauses is a raw passthrough emitted between the view name and AS —
	// e.g. "SQL SECURITY DEFINER".
	Clauses string
	// From is the raw source, emitted verbatim after FROM: a table, or a
	// parenthesised subquery, with any WHERE that follows it. Required.
	From string
	// Settings are joined with ", " into a SETTINGS clause closing the
	// view's SELECT, so they apply whenever the view is read. A table with a
	// LowCardinality lane over a non-string type needs
	// "allow_suspicious_low_cardinality_types=1" here as it does on the
	// CREATE TABLE: the cast to the lane's type is refused without it, at
	// CREATE VIEW and again at every SELECT from the view.
	Settings []string
}

// ComposeCreateView renders a CREATE VIEW whose columns are exactly the
// physical columns ComposeCreateTable would declare for the same IR — same
// names, same order, same types — each filled by the expression exprFor
// returns for it. exprFor is called once per column, in DDL order; an empty
// expression or an error from it fails the composition, so a column cannot
// be left out. viewName is emitted verbatim (qualify and quote at the call
// site when needed).
func ComposeCreateView(viewName string, ir *common.IntermediateTableRepresentation, tableRowConfig common.TableRowConfigE, conv common.NamingConventionI, opts ViewOptions, exprFor func(col ViewColumn) (ViewExpr, error)) (sql string, err error) {
	if opts.From == "" {
		err = eb.Build().Str("viewName", viewName).Errorf("compose create view: From is required")
		return
	}
	if exprFor == nil {
		err = eb.Build().Str("viewName", viewName).Errorf("compose create view: exprFor is required")
		return
	}
	var b strings.Builder
	switch opts.Mode {
	case CreateModePlain:
		b.WriteString("CREATE VIEW ")
	case CreateModeIfNotExists:
		b.WriteString("CREATE VIEW IF NOT EXISTS ")
	case CreateModeOrReplace:
		b.WriteString("CREATE OR REPLACE VIEW ")
	default:
		err = eb.Build().Str("viewName", viewName).Uint8("mode", uint8(opts.Mode)).Errorf("compose create view: unknown create mode")
		return
	}
	b.WriteString(viewName)
	if opts.Clauses != "" {
		b.WriteString(" ")
		b.WriteString(opts.Clauses)
	}
	b.WriteString(" AS\nSELECT")

	tech := NewTechnologySpecificCodeGenerator()
	var typeB strings.Builder
	tech.SetCodeBuilder(&typeB)
	n := 0
	for cc, cp := range ir.IterateColumnProps() {
		var phys []common.PhysicalColumnDesc
		phys, err = conv.MapIntermediateToPhysicalColumns(cc, *cp, nil, tableRowConfig)
		if err != nil {
			err = eb.Build().Str("viewName", viewName).Errorf("compose create view: render physical columns: %w", err)
			return
		}
		for i, phy := range phys {
			col := ViewColumn{
				Physical:  phy.String(),
				Section:   cc.SectionName,
				Name:      cp.Names[i],
				PlainItem: cc.PlainItemType,
			}
			if cc.SectionName != "" {
				col.Role = cp.Roles[i]
			}
			col.Type, err = columnType(tech, &typeB, phy)
			if err != nil {
				err = eb.Build().Str("viewName", viewName).Str("column", col.Physical).Errorf("compose create view: column type: %w", err)
				return
			}
			var expr ViewExpr
			expr, err = exprFor(col)
			if err != nil {
				err = eb.Build().Str("viewName", viewName).Str("column", col.Physical).Errorf("compose create view: expression: %w", err)
				return
			}
			if strings.TrimSpace(expr.SQL) == "" {
				err = eb.Build().Str("viewName", viewName).Str("column", col.Physical).Errorf("compose create view: column has no expression — a view of a leeway table fills every physical column")
				return
			}
			if n > 0 {
				b.WriteString(",")
			}
			b.WriteString("\n\t")
			if expr.Exact {
				b.WriteString(expr.SQL)
			} else {
				b.WriteString("CAST(")
				b.WriteString(expr.SQL)
				b.WriteString(" AS ")
				b.WriteString(col.Type)
				b.WriteString(")")
			}
			b.WriteString(" AS ")
			b.WriteString(marshalling.EscapeIdentifier(col.Physical))
			n++
		}
	}
	if n == 0 {
		err = eb.Build().Str("viewName", viewName).Errorf("compose create view: the table has no columns")
		return
	}
	b.WriteString("\nFROM ")
	b.WriteString(opts.From)
	if len(opts.Settings) > 0 {
		b.WriteString("\nSETTINGS ")
		b.WriteString(strings.Join(opts.Settings, ", "))
	}
	sql = b.String()
	return
}

// columnType renders one physical column's ClickHouse type through the
// generator the CREATE TABLE uses, so the two cannot disagree. typeB is the
// scratch builder tech writes to.
func columnType(tech *TechnologySpecificCodeGenerator, typeB *strings.Builder, phy common.PhysicalColumnDesc) (chType string, err error) {
	ct, hints, list, err := columnShape(phy)
	if err != nil {
		return
	}
	if compatible, msg := tech.CheckTypeCompatibility(ct); !compatible {
		err = eb.Build().Stringer("column", phy).Str("msg", msg).Errorf("canonical type of column is not supported in ClickHouse")
		return
	}
	typeB.Reset()
	err = tech.generateColumnType(ct, hints, list)
	if err != nil {
		err = eh.Errorf("generate column type: %w", err)
		return
	}
	chType = strings.TrimSpace(typeB.String())
	return
}
