package chstore

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// SchemaContract is the facts table's contract under cfg, for the
// process-wide schema check (ADR-0387): the columns ComposeSetupSQL
// creates, read back out of it. The facts table carries no stamp of its
// own, so it is judged by its columns alone.
func SchemaContract(cfg Config) (c recordstore.TableContract, err error) {
	sql, err := ComposeSetupSQL(cfg, "")
	if err != nil {
		return
	}
	cols, err := recordstore.ParseCreateTableColumns(sql)
	if err != nil {
		err = eh.Errorf("chstore: schema contract: %w", err)
		return
	}
	c = recordstore.TableContract{Kind: factsschema.TableName, Table: cfg.Database + "." + cfg.Table, Columns: cols}
	return
}
