// Package gen is the recordstore generator (ADR-0100 SD6): one invocation
// takes a leeway TableDesc, a set of component DTO sources and the
// envelope-role bindings, and emits a complete store package by driving
// the existing leeway generators (dml, ddl/clickhouse, readaccess,
// marshallgen) and then emitting the store glue over their output.
//
// It is driven the repo-idiomatic way: a gen_test.go in the target package
// calls Generate (see recordstore/example).
package gen

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	"github.com/stergiotis/boxer/public/semistructured/leeway/dml"
	"github.com/stergiotis/boxer/public/semistructured/leeway/gocodegen"
	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallgen"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/readaccess"
)

// Input parameterizes one store generation.
type Input struct {
	// PackageName is the Go package the emitted files declare.
	PackageName string
	// StoreName is the exported name prefix (e.g. "Device" yields
	// DeviceStore, DeviceEntity, DeviceEntityBuilder).
	StoreName string
	// TableName is the ClickHouse table (and naming.StylableName base for
	// the generated DML/RA classes, suffixed "_table").
	TableName string
	// Table is the physical schema. Its plain columns form the envelope;
	// the ADR-0100 roles are bound by PlainItemTypeE: EntityId → Key,
	// EntityTimestamp → Order, EntityLifecycle → Lifecycle (state view).
	Table common.TableDesc
	// RowConfig is the leeway table-row configuration.
	RowConfig common.TableRowConfigE
	// ComponentPaths are the lw:-tagged DTO sources, one kind per file.
	ComponentPaths []string
	// OutDir receives the emitted files.
	OutDir string
	// ImportPath is the generated package's own import path (e.g.
	// "github.com/stergiotis/boxer/public/storage/recordstore/example");
	// the store file imports the internal/lowlevel scaffolding through
	// it. Required unless Flat.
	ImportPath string
	// Flat keeps every emitted file in one package directory — the
	// pre-Update layout. The default (false) places the DML and RA
	// scaffolding in internal/lowlevel beneath OutDir, so the generated
	// package's public surface stays the store family (ADR-0100 Update
	// 2026-07-04).
	Flat bool

	// DDL overrides the table-level clauses (ADR-0102 seam). nil derives
	// the defaults: CREATE TABLE IF NOT EXISTS, ENGINE MergeTree(),
	// ORDER BY (Key[, Order]) resolved to physical names, and the
	// low-cardinality settings. When set, non-zero fields override their
	// derived counterparts and Indexes are taken as given; the runtime
	// DDLTail remains a raw suffix on top of whatever is composed here.
	DDL *clickhouse.TableOptions

	// keyGoType is the Key column's derived Go type ("uint64" or
	// "string"), resolved by emitStore from the schema.
	keyGoType string
}

// Generate emits the store package: <table>_ddl_clickhouse.out.sql, one
// <kind>.out.go per component and <store>_store.out.go into OutDir, plus
// the <table>_dml.out.go / <table>_ra.out.go scaffolding — into
// internal/lowlevel by default, or beside the rest with Flat.
func (inst Input) Generate() (err error) {
	if inst.PackageName == "" || inst.StoreName == "" || inst.TableName == "" || inst.OutDir == "" {
		err = eh.Errorf("PackageName, StoreName, TableName and OutDir are required")
		return
	}
	if !inst.Flat && inst.ImportPath == "" {
		err = eh.Errorf("ImportPath is required for the internal/lowlevel layout (set Flat for the single-package layout)")
		return
	}
	conv, err := ddl.NewHumanReadableNamingConvention(":")
	if err != nil {
		err = eh.Errorf("naming convention: %w", err)
		return
	}
	tableStylable := naming.MustBeValidStylableName(inst.TableName + "_table")

	// 1. DML (the Arrow write target).
	dmlDriver := dml.NewGoCodeGeneratorDriver(conv, clickhouse.NewTechnologySpecificCodeGenerator())
	namer := gocodegen.NewMultiTablePerPackageGoClassNamer()
	code, _, err := dmlDriver.GenerateGoClasses(inst.scaffoldPkg(), tableStylable, inst.Table, inst.RowConfig, namer)
	if err != nil {
		err = eh.Errorf("generate dml: %w", err)
		return
	}
	err = inst.write(inst.scaffoldFile(inst.TableName+"_dml.out.go"), code)
	if err != nil {
		return
	}

	// 2. The table IR — shared by the DDL composition (step 6) and the
	// store emission.
	ir := common.NewIntermediateTableRepresentation()
	err = ir.LoadFromTable(&inst.Table, clickhouse.NewTechnologySpecificCodeGenerator())
	if err != nil {
		err = eh.Errorf("load table IR: %w", err)
		return
	}

	// 3. RA (the read-access classes decode drives).
	raDriver := readaccess.NewGoCodeGeneratorDriver(conv, clickhouse.NewTechnologySpecificCodeGenerator(), true)
	code, _, err = raDriver.GenerateGoClasses(inst.scaffoldPkg(), tableStylable, inst.Table, inst.RowConfig, gocodegen.NewMultiTablePerPackageGoClassNamer())
	if err != nil {
		err = eh.Errorf("generate ra: %w", err)
		return
	}
	err = inst.write(inst.scaffoldFile(inst.TableName+"_ra.out.go"), code)
	if err != nil {
		return
	}

	// 4. Per-component marshallgen codecs (Columns, BuildEntities,
	// AddSections, FillFromArrow).
	plans := make([]*mappingplan.Plan, 0, len(inst.ComponentPaths))
	for _, in := range inst.ComponentPaths {
		out := strings.TrimSuffix(filepath.Base(in), ".go") + ".out.go"
		var plan *mappingplan.Plan
		plan, err = marshallgen.ParsePlan(in)
		if err != nil {
			err = eh.Errorf("parse component %s: %w", in, err)
			return
		}
		var rendered []byte
		rendered, err = marshallgen.EmitPlan(plan, marshallgen.NoOpWrapper{})
		if err != nil {
			err = eh.Errorf("emit component codec %s: %w", in, err)
			return
		}
		err = inst.write(out, rendered)
		if err != nil {
			return
		}
		plans = append(plans, plan)
	}

	// 5. The store glue over everything above. Runs before the DDL
	// composition so the role gates (duplicate roles, unsupported shapes)
	// report their domain-level errors rather than a downstream
	// column-reference failure.
	storeCode, err := inst.emitStore(ir, conv, plans)
	if err != nil {
		err = eh.Errorf("emit store: %w", err)
		return
	}
	err = inst.write(strings.ToLower(inst.StoreName)+"_store.out.go", storeCode)
	if err != nil {
		return
	}

	// 6. DDL — the complete CREATE TABLE through the ADR-0102 table-clause
	// seam: derived clause defaults (roles → ORDER BY, resolved to
	// physical names) merged with Input.DDL overrides.
	ddlSql, err := clickhouse.ComposeCreateTable(inst.TableName, ir, inst.RowConfig, conv, inst.tableOptions())
	if err != nil {
		err = eh.Errorf("generate ddl: %w", err)
		return
	}
	err = inst.write(inst.TableName+"_ddl_clickhouse.out.sql", []byte(ddlSql))
	return
}

// tableOptions merges the derived clause defaults with Input.DDL:
// non-zero override fields win, Indexes are taken as given. The defaults
// bind ORDER BY to the envelope roles — Key leading (the point-lookup
// guidance), Order second when the schema has one.
func (inst Input) tableOptions() (opts clickhouse.TableOptions) {
	opts = clickhouse.TableOptions{
		Mode:     clickhouse.CreateModeIfNotExists,
		Engine:   "MergeTree()",
		OrderBy:  []clickhouse.ColumnRef{{PlainItem: common.PlainItemTypeEntityId}},
		Settings: []string{"allow_suspicious_low_cardinality_types=1"},
	}
	if slices.Contains(inst.Table.PlainValuesItemTypes, common.PlainItemTypeEntityTimestamp) {
		opts.OrderBy = append(opts.OrderBy, clickhouse.ColumnRef{PlainItem: common.PlainItemTypeEntityTimestamp})
	}
	if inst.DDL == nil {
		return
	}
	o := *inst.DDL
	if o.Mode != clickhouse.CreateModePlain {
		opts.Mode = o.Mode
	}
	if o.Engine != "" {
		opts.Engine = o.Engine
	}
	if o.OrderBy != nil {
		opts.OrderBy = o.OrderBy
	}
	if o.PartitionBy != "" {
		opts.PartitionBy = o.PartitionBy
	}
	if o.TTL != "" {
		opts.TTL = o.TTL
	}
	if o.Settings != nil {
		opts.Settings = o.Settings
	}
	if o.Tail != "" {
		opts.Tail = o.Tail
	}
	opts.Indexes = o.Indexes
	return
}

// scaffoldPkg is the package the DML/RA scaffolding declares: the
// consumer package itself in the Flat layout, "lowlevel" otherwise.
func (inst Input) scaffoldPkg() string {
	if inst.Flat {
		return inst.PackageName
	}
	return "lowlevel"
}

// scaffoldFile places a scaffolding file: beside the store in the Flat
// layout, under internal/lowlevel otherwise.
func (inst Input) scaffoldFile(name string) string {
	if inst.Flat {
		return name
	}
	return filepath.Join("internal", "lowlevel", name)
}

func (inst Input) write(name string, data []byte) (err error) {
	path := filepath.Join(inst.OutDir, name)
	err = os.MkdirAll(filepath.Dir(path), 0755)
	if err != nil {
		err = eh.Errorf("create directory for %s: %w", path, err)
		return
	}
	err = os.WriteFile(path, data, 0644)
	if err != nil {
		err = eh.Errorf("write %s: %w", path, err)
	}
	return
}
