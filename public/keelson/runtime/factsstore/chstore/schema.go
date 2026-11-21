package chstore

import (
	"github.com/stergiotis/boxer/public/semistructured/leeway/factsschema"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// SchemaContract is the facts table's contract under cfg, for the
// process-wide schema check (ADR-0387): the columns ComposeSetupSQL
// creates, read back out of it, the stamp it writes, and the layout of
// [EngineClause] (ADR-0397). A table created with another layout is judged
// by its columns and carries a note naming the recipe that copies it.
func SchemaContract(cfg Config) (c recordstore.TableContract, err error) {
	t, err := composeFactsTable(cfg, "")
	if err != nil {
		return
	}
	c = recordstore.TableContract{
		Kind:         factsschema.TableName,
		Table:        cfg.Database + "." + cfg.Table,
		Columns:      t.columns,
		Stamp:        t.stamp,
		Layout:       t.layout,
		LayoutRecipe: factsschema.MigrationRecipe,
	}
	return
}
