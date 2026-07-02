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
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	"github.com/stergiotis/boxer/public/semistructured/leeway/dml"
	"github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
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
}

// Generate emits the store package: <table>_dml.out.go, <table>_ra.out.go,
// <table>_ddl_clickhouse.out.sql, one <kind>.out.go per component and
// <store>_store.out.go.
func (inst Input) Generate() (err error) {
	if inst.PackageName == "" || inst.StoreName == "" || inst.TableName == "" || inst.OutDir == "" {
		err = eh.Errorf("PackageName, StoreName, TableName and OutDir are required")
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
	code, _, err := dmlDriver.GenerateGoClasses(inst.PackageName, tableStylable, inst.Table, inst.RowConfig, namer)
	if err != nil {
		err = eh.Errorf("generate dml: %w", err)
		return
	}
	err = inst.write(inst.TableName+"_dml.out.go", code)
	if err != nil {
		return
	}

	// 2. DDL column body (the engine clause is the store's DDLTail).
	b := &strings.Builder{}
	tech := clickhouse.NewTechnologySpecificCodeGenerator()
	b.WriteString("CREATE TABLE IF NOT EXISTS " + inst.TableName + " (\n")
	tech.SetCodeBuilder(b)
	ir := common.NewIntermediateTableRepresentation()
	err = ir.LoadFromTable(&inst.Table, tech)
	if err != nil {
		err = eh.Errorf("load table IR: %w", err)
		return
	}
	ddlGen := ddl.NewGeneratorDriver()
	err = ddlGen.GenerateColumnsCode(ir.IterateColumnProps(), inst.RowConfig, conv, tech,
		func(hint encodingaspects.AspectE) (bool, string) { return true, "" })
	if err != nil {
		err = eh.Errorf("generate ddl: %w", err)
		return
	}
	b.WriteString("\n)")
	err = inst.write(inst.TableName+"_ddl_clickhouse.out.sql", []byte(b.String()))
	if err != nil {
		return
	}

	// 3. RA (the read-access classes decode drives).
	raDriver := readaccess.NewGoCodeGeneratorDriver(conv, clickhouse.NewTechnologySpecificCodeGenerator(), true)
	code, _, err = raDriver.GenerateGoClasses(inst.PackageName, tableStylable, inst.Table, inst.RowConfig, gocodegen.NewMultiTablePerPackageGoClassNamer())
	if err != nil {
		err = eh.Errorf("generate ra: %w", err)
		return
	}
	err = inst.write(inst.TableName+"_ra.out.go", code)
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

	// 5. The store glue over everything above.
	storeCode, err := inst.emitStore(ir, conv, plans)
	if err != nil {
		err = eh.Errorf("emit store: %w", err)
		return
	}
	err = inst.write(strings.ToLower(inst.StoreName)+"_store.out.go", storeCode)
	return
}

func (inst Input) write(name string, data []byte) (err error) {
	path := filepath.Join(inst.OutDir, name)
	err = os.WriteFile(path, data, 0644)
	if err != nil {
		err = eh.Errorf("write %s: %w", path, err)
	}
	return
}
