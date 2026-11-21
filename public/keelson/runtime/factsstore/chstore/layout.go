package chstore

import (
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema"
	factsddl "github.com/stergiotis/boxer/public/keelson/runtime/factsschema/ddl"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// EngineClause is the engine clause an empty argument selects: the
// BOXER_FACTS_ENGINE setting, whose default is
// factsschema.DefaultEngineClause (ADR-0397).
func EngineClause() (clause string) {
	return FactsEngine.Get()
}

// factsTable is the facts table's CREATE script for one config and engine
// clause, with what was read out of it on the way.
type factsTable struct {
	sql     string
	columns []recordstore.ColumnSpec
	layout  recordstore.TableLayout
	stamp   string
}

// composeFactsTable composes the facts table's script: the generated CREATE
// DATABASE and CREATE TABLE over engineClause, retargeted to cfg's database
// and table, with the insert deduplication window ADR-0387 §SD6 gives a
// plain MergeTree table and a stamp naming the columns and the layout
// (ADR-0397). An empty engineClause selects [EngineClause]. A clause that
// carries its own SETTINGS is refused: ClickHouse reads a second SETTINGS
// clause after the table's as the statement's own, so the script's table
// settings would be dropped without an error.
func composeFactsTable(cfg Config, engineClause string) (t factsTable, err error) {
	if cfg.Database == "" || cfg.Table == "" {
		err = eh.Errorf("chstore: compose setup sql: cfg requires Database + Table")
		return
	}
	if engineClause == "" {
		engineClause = EngineClause()
	}
	layout, settings, err := recordstore.ParseEngineClause(engineClause)
	if err != nil {
		err = eh.Errorf("chstore: compose setup sql: engine clause: %w", err)
		return
	}
	if settings != "" {
		err = eb.Build().Str("engineClause", engineClause).Errorf("chstore: compose setup sql: the engine clause carries SETTINGS; the setup script appends the table's settings after it, and ClickHouse would read them as the statement's, so leave them out of the clause")
		return
	}
	sql, err := factsddl.ComposeCreateTableSql(engineClause)
	if err != nil {
		err = eh.Errorf("chstore: compose setup sql: %w", err)
		return
	}
	sql = strings.ReplaceAll(sql,
		factsschema.DatabaseName+"."+factsschema.TableName,
		cfg.Database+"."+cfg.Table)
	if cfg.Database != factsschema.DatabaseName {
		sql = strings.ReplaceAll(sql,
			"CREATE DATABASE IF NOT EXISTS "+factsschema.DatabaseName+";",
			"CREATE DATABASE IF NOT EXISTS "+cfg.Database+";")
	}
	cols, err := recordstore.ParseCreateTableColumns(sql)
	if err != nil {
		err = eh.Errorf("chstore: compose setup sql: %w", err)
		return
	}
	// The generated composer ends the script with its SETTINGS clause; the
	// window and the stamp continue it.
	const tail = ";\n"
	if !strings.HasSuffix(sql, tail) {
		err = eh.Errorf("chstore: compose setup sql: the generated script does not end in its SETTINGS clause")
		return
	}
	sql = strings.TrimSuffix(sql, tail)
	if recordstore.IsPlainMergeTree(layout.Engine) {
		sql += ", " + recordstore.DedupWindowSetting + "=" + strconv.Itoa(recordstore.DedupWindowBlocks)
	}
	stamp := recordstore.TableStamp{
		Kind:    factsschema.TableName,
		Columns: recordstore.ColumnsDigest(cols),
		Naming:  recordstore.LeewayNamingVersion,
		Layout:  layout.Digest(),
	}.String()
	sql += recordstore.CommentClause(stamp) + tail
	t = factsTable{sql: sql, columns: cols, layout: layout, stamp: stamp}
	return
}
