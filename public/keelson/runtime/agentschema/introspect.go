package agentschema

import (
	"encoding/json/v2"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// TableName is keelson('agent_relations'): the declared relations and
// their rules, as compiled — the aspect defaults are declared but not yet
// compiled into the views, so the table does not list them (see
// Relation.Aspects).
const TableName = "agent_relations"

// RegisterIntrospect registers keelson('agent_relations') over relations;
// nil registers the journal relations.
func RegisterIntrospect(reg *introspect.Registry, relations []Relation) (err error) {
	if relations == nil {
		relations = JournalRelations()
	}
	rows := make([]relationRow, 0, len(relations))
	for _, r := range relations {
		row := relationRow{Relation: r}
		if len(r.Withhold) > 0 {
			b, merr := json.Marshal(r.Withhold)
			if merr != nil {
				return eb.Build().Str("relation", r.Name).Errorf("encoding the withhold rules: %w", merr)
			}
			row.withhold = string(b)
		}
		rows = append(rows, row)
	}
	return reg.Register(relationsProvider{rows: rows})
}

// relationRow is a relation with its rules encoded for the table.
type relationRow struct {
	Relation
	withhold string
}

type relationsProvider struct{ rows []relationRow }

func (relationsProvider) Name() string                         { return TableName }
func (relationsProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessStatic }
func (relationsProvider) Schema() *arrow.Schema                { return relationsTable(nil).Schema() }

func (inst relationsProvider) Snapshot(proj introspect.Projection) (rec arrow.RecordBatch, err error) {
	rec = relationsTable(inst.rows).Build(proj, len(inst.rows))
	return
}

func relationsTable(rows []relationRow) *introspect.Table {
	return introspect.NewTable().
		String("name", func(i int) string { return rows[i].Name }).
		String("over", func(i int) string { return rows[i].Over }).
		String("scope_setting", func(i int) string { return rows[i].Scope.Setting }).
		String("scope_column", func(i int) string { return rows[i].Scope.Column }).
		String("scope_via", func(i int) string {
			if v := rows[i].Scope.Via; v != nil {
				return v.Table + "." + v.Column + " -> " + v.Key
			}
			return ""
		}).
		String("scope_except", func(i int) string { return rows[i].Scope.Except }).
		StringList("passed", func(i int) []string { return rows[i].Passed() }).
		String("memberships", func(i int) string { return rows[i].Memberships }).
		String("withhold", func(i int) string { return rows[i].withhold }).
		String("summary", func(i int) string { return rows[i].Summary })
}
