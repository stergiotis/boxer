//go:build llm_generated_opus47

// Package codegen drives the leeway code generators for the runtime
// facts schema (ADR-0026 M2.5b, plus ADR-0042's Phase-1 pivot). Mirrors
// the spinnaker pattern at boxerstaging/spinnaker/codegen but targets
// runtime/factsschema.
//
// Four artefacts are produced — all checked into the tree under
// runtime/factsschema/, all regenerated on demand when
// factsschema.LoadRuntimeFactsMapping changes:
//
//   - dml/runtime_facts_dml.out.go      — Arrow record builders
//     (FORMAT Arrow ingest path; consumed by factsstore/chstore today).
//   - dml_cbor/runtime_facts_dml.out.go — same generator output,
//     emitted against the arrowrowcbor shim → sparse self-describing
//     CBOR (the buscodec target per ADR-0042).
//   - ra/runtime_facts_ra.out.go        — typed readaccess code
//     over the same Arrow schema (universal read).
//   - ddl/runtime_facts_ddl.out.go      — CH CREATE TABLE wrapper
//     with the column block baked as a string constant and a runtime
//     ComposeCreateTableSql(engineClause) entrypoint.
//
// Regenerate by running:
//
//	go run ./cmd/runtimecodegen all
//
// or the individual subcommands.
package codegen

import (
	"fmt"
	"go/format"
	"os"
	"strings"

	"github.com/stergiotis/boxer/public/code/synthesis/golang"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	leewayddl "github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	leewayddlgolang "github.com/stergiotis/boxer/public/semistructured/leeway/ddl/golang"
	leewaydml "github.com/stergiotis/boxer/public/semistructured/leeway/dml"
	"github.com/stergiotis/boxer/public/semistructured/leeway/gocodegen"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/readaccess"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema"
)

const (
	// DefaultDMLOutputPath is the relative location (from repo root) of the
	// generated DML Go source (Arrow backend).
	DefaultDMLOutputPath = "public/keelson/runtime/factsschema/dml/runtime_facts_dml.out.go"

	// DefaultDMLCBOROutputPath is the relative location of the
	// generated DML Go source targeting the arrowrowcbor shim
	// (sparse self-describing CBOR backend; the buscodec target).
	DefaultDMLCBOROutputPath = "public/keelson/runtime/factsschema/dml_cbor/runtime_facts_dml.out.go"

	// DefaultReadAccessOutputPath is the relative location of the generated
	// readaccess Go source.
	DefaultReadAccessOutputPath = "public/keelson/runtime/factsschema/ra/runtime_facts_ra.out.go"

	// DefaultDDLOutputPath is the relative location of the generated
	// CH-DDL Go source.
	DefaultDDLOutputPath = "public/keelson/runtime/factsschema/ddl/runtime_facts_ddl.out.go"

	// DefaultOutputPath is retained for backwards compatibility with the
	// original DML-only entrypoint.
	DefaultOutputPath = DefaultDMLOutputPath

	dmlPackageName        = "dml"
	dmlCBORPackageName    = "dml_cbor"
	readAccessPackageName = "ra"
	ddlPackageName        = "ddl"

	// arrowrowcbor is the only non-Arrow backend that survived ADR-0042
	// Phase C; both the full-RB (arrowrowbinary) and sparse-RB
	// (arrowsparserb) shims were retired alongside the codec switch.
	arrowRowCBORImportPath = "github.com/stergiotis/boxer/public/keelson/runtime/factsschema/arrowrowcbor"

	// namingSeparator mirrors spinnaker — physical column names use ':' as
	// the separator between leeway-encoded segments.
	namingSeparator = ":"

	// settingsClause is the per-table SETTINGS suffix; pulled out so the
	// runtime and codegen wrappers share the same exact text.
	settingsClause = "allow_suspicious_low_cardinality_types=1"
)

func buildTableDesc() (tbl common.TableDesc, err error) {
	var manip *common.TableManipulator
	manip, err = factsschema.GetSchemaInManipulator()
	if err != nil {
		err = eh.Errorf("codegen: schema: %w", err)
		return
	}
	tbl, err = manip.BuildTableDesc()
	if err != nil {
		err = eh.Errorf("codegen: build table desc: %w", err)
		return
	}
	return
}

func writeFile(path string, data []byte) (err error) {
	_ = os.Remove(path)
	err = os.WriteFile(path, data, 0o644)
	if err != nil {
		err = eh.Errorf("codegen: write %q: %w", path, err)
		return
	}
	return
}

// GenerateDML emits the runtime.facts DML Go source to outPath using
// the default arrow/array builder package — preserves the historical
// behaviour.
func GenerateDML(outPath string) (err error) {
	return GenerateDMLWithBuilderPackage(outPath, dmlPackageName, leewaydml.DefaultBuilderPackage())
}

// GenerateDMLCBOR emits a DML package targeting the arrowrowcbor
// shim. Wire format is sparse self-describing CBOR — each row is a
// definite-length map keyed by short field names, with empty sections
// omitted entirely. The buscodec target per ADR-0042's M11.
func GenerateDMLCBOR(outPath string) (err error) {
	return GenerateDMLWithBuilderPackage(outPath, dmlCBORPackageName, leewaydml.BuilderPackage{
		ImportPath: arrowRowCBORImportPath,
		Alias:      "arrowrowcbor",
		RecordType: "*arrowrowcbor.Record",
	})
}

// GenerateDMLWithBuilderPackage is the generic entry point — every
// other GenerateDML* convenience wrapper calls this with a different
// BuilderPackage. Useful when adding new backends without growing
// codegen's API.
func GenerateDMLWithBuilderPackage(outPath, packageName string, builderPkg leewaydml.BuilderPackage) (err error) {
	var tbl common.TableDesc
	tbl, err = buildTableDesc()
	if err != nil {
		return
	}
	var conv *leewayddl.HumanReadableNamingConvention
	conv, err = leewayddl.NewHumanReadableNamingConvention(namingSeparator)
	if err != nil {
		err = eh.Errorf("codegen: naming convention: %w", err)
		return
	}
	tech := clickhouse.NewTechnologySpecificCodeGenerator()
	driver := leewaydml.NewGoCodeGeneratorDriverWithBuilderPackage(conv, tech, builderPkg)
	clsNamer := gocodegen.NewMultiTablePerPackageGoClassNamer()
	var code []byte
	code, _, err = driver.GenerateGoClasses(
		packageName,
		naming.MustBeValidStylableName(factsschema.TableName),
		tbl,
		factsschema.TableRowConfig,
		clsNamer,
	)
	if err != nil {
		err = eh.Errorf("codegen: generate dml go classes (%s): %w", builderPkg.Alias, err)
		return
	}
	return writeFile(outPath, code)
}

// GenerateReadAccess emits the runtime.facts readaccess Go source to
// outPath. Mirrors spinnaker.GenerateReadAccess — uses the golang
// (Arrow) technology backend and a "fat runtime" so callers get the
// full membership lookup accelerators.
func GenerateReadAccess(outPath string) (err error) {
	var tbl common.TableDesc
	tbl, err = buildTableDesc()
	if err != nil {
		return
	}
	var conv *leewayddl.HumanReadableNamingConvention
	conv, err = leewayddl.NewHumanReadableNamingConvention(namingSeparator)
	if err != nil {
		err = eh.Errorf("codegen: naming convention: %w", err)
		return
	}
	tech := leewayddlgolang.NewTechnologySpecificCodeGenerator()
	driver := readaccess.NewGoCodeGeneratorDriver(conv, tech, true)
	clsNamer := gocodegen.NewMultiTablePerPackageGoClassNamer()
	var code []byte
	code, _, err = driver.GenerateGoClasses(
		readAccessPackageName,
		tbl.DictionaryEntry.Name,
		tbl,
		factsschema.TableRowConfig,
		clsNamer,
	)
	if err != nil {
		err = eh.Errorf("codegen: generate readaccess go classes: %w", err)
		return
	}
	return writeFile(outPath, code)
}

// ComposeCreateTableSql emits a ClickHouse-compatible CREATE TABLE
// statement for runtime.facts. engineClause specifies the storage
// engine plus partition / order keys, e.g.
//
//	"MergeTree() PARTITION BY toYYYYMM(ts) ORDER BY (id)"
//
// Caller is responsible for executing the SQL against a live CH
// instance; this package never opens a connection. The emitted SQL
// is idempotent — it uses CREATE DATABASE / TABLE IF NOT EXISTS.
//
// Moved here from factsschema/ddl.go so the codegen entrypoint can
// share the column-emission code path with GenerateDDL. The runtime
// counterpart is the generated factsschema/ddl package, which bakes
// the column block as a string constant and exposes an identically
// shaped ComposeCreateTableSql(engineClause) function — preferred by
// build-tag-ungated callers (chstore, integration tests).
func ComposeCreateTableSql(engineClause string) (sql string, err error) {
	if engineClause == "" {
		err = eh.Errorf("compose ddl: empty engine clause")
		return
	}
	var columns string
	columns, err = emitColumnsSQL()
	if err != nil {
		return
	}
	var buf strings.Builder
	_, _ = fmt.Fprintf(&buf,
		"CREATE DATABASE IF NOT EXISTS %s;\nCREATE TABLE IF NOT EXISTS %s.%s (\n",
		factsschema.DatabaseName, factsschema.DatabaseName, factsschema.TableName)
	buf.WriteString(columns)
	_, _ = fmt.Fprintf(&buf, "\n) ENGINE %s\nSETTINGS %s;\n", engineClause, settingsClause)
	sql = buf.String()
	return
}

// emitColumnsSQL runs the boxer DDL pipeline and returns just the
// column block (no scaffolding) for the runtime.facts schema. Used by
// both ComposeCreateTableSql and GenerateDDL.
func emitColumnsSQL() (columns string, err error) {
	var tbl common.TableDesc
	tbl, err = buildTableDesc()
	if err != nil {
		return
	}
	var conv *leewayddl.HumanReadableNamingConvention
	conv, err = leewayddl.NewHumanReadableNamingConvention(namingSeparator)
	if err != nil {
		err = eh.Errorf("compose ddl: naming convention: %w", err)
		return
	}
	chTech := clickhouse.NewTechnologySpecificCodeGenerator()
	ir := common.NewIntermediateTableRepresentation()
	err = ir.LoadFromTable(&tbl, chTech)
	if err != nil {
		err = eh.Errorf("compose ddl: load ir: %w", err)
		return
	}
	var buf strings.Builder
	chTech.SetCodeBuilder(&buf)
	driver := leewayddl.NewGeneratorDriver()
	err = driver.GenerateColumnsCode(
		ir.IterateColumnProps(),
		factsschema.TableRowConfig,
		conv,
		chTech,
		leewayddl.EncodingAspectFilterFuncFromTechnology(chTech, common.ImplementationStatusFull),
	)
	if err != nil {
		err = eh.Errorf("compose ddl: generate columns: %w", err)
		return
	}
	columns = buf.String()
	return
}

// GenerateDDL emits the runtime.facts CH-DDL Go source to outPath.
// The generated file exposes the column block as a string constant
// (ColumnsSQL) and a runtime ComposeCreateTableSql(engineClause)
// function with the same signature as the codegen-internal one.
func GenerateDDL(outPath string) (err error) {
	var columns string
	columns, err = emitColumnsSQL()
	if err != nil {
		return
	}
	if strings.Contains(columns, "`") {
		err = eh.Errorf("codegen: generated columns contain a backtick — cannot embed in a Go raw string literal")
		return
	}

	var buf strings.Builder
	_, err = golang.AddCodeGenComment(&buf, leewayddl.CodeGeneratorName)
	if err != nil {
		err = eh.Errorf("codegen: header: %w", err)
		return
	}
	_, _ = fmt.Fprintf(&buf, "package %s\n\n", ddlPackageName)
	buf.WriteString("import (\n")
	buf.WriteString("\t\"fmt\"\n")
	buf.WriteString("\t\"strings\"\n\n")
	buf.WriteString("\t\"github.com/stergiotis/boxer/public/observability/eh\"\n")
	buf.WriteString(")\n\n")

	_, _ = fmt.Fprintf(&buf, "// DatabaseName mirrors factsschema.DatabaseName at codegen time.\nconst DatabaseName = %q\n\n", factsschema.DatabaseName)
	_, _ = fmt.Fprintf(&buf, "// TableName mirrors factsschema.TableName at codegen time.\nconst TableName = %q\n\n", factsschema.TableName)
	_, _ = fmt.Fprintf(&buf, "// SettingsClause is the SETTINGS suffix emitted after the engine clause.\nconst SettingsClause = %q\n\n", settingsClause)

	buf.WriteString("// ColumnsSQL is the leeway-encoded column block (no enclosing\n")
	buf.WriteString("// parentheses, no trailing comma) produced by the ET7 DDL pipeline\n")
	buf.WriteString("// from factsschema.LoadRuntimeFactsMapping.\n")
	buf.WriteString("const ColumnsSQL = `")
	buf.WriteString(columns)
	buf.WriteString("`\n\n")

	buf.WriteString("// ComposeCreateTableSql emits a ClickHouse-compatible CREATE TABLE\n")
	buf.WriteString("// statement for runtime.facts. Drop-in replacement for the historical\n")
	buf.WriteString("// factsschema.ComposeCreateTableSql — same signature, same semantics,\n")
	buf.WriteString("// but with the column block baked at codegen time so no leeway pipeline\n")
	buf.WriteString("// runs at process startup.\n")
	buf.WriteString("func ComposeCreateTableSql(engineClause string) (sql string, err error) {\n")
	buf.WriteString("\tif engineClause == \"\" {\n")
	buf.WriteString("\t\terr = eh.Errorf(\"compose ddl: empty engine clause\")\n")
	buf.WriteString("\t\treturn\n")
	buf.WriteString("\t}\n")
	buf.WriteString("\tvar b strings.Builder\n")
	buf.WriteString("\tfmt.Fprintf(&b, \"CREATE DATABASE IF NOT EXISTS %s;\\nCREATE TABLE IF NOT EXISTS %s.%s (\\n\", DatabaseName, DatabaseName, TableName)\n")
	buf.WriteString("\tb.WriteString(ColumnsSQL)\n")
	buf.WriteString("\tfmt.Fprintf(&b, \"\\n) ENGINE %s\\nSETTINGS %s;\\n\", engineClause, SettingsClause)\n")
	buf.WriteString("\tsql = b.String()\n")
	buf.WriteString("\treturn\n")
	buf.WriteString("}\n")

	src := []byte(buf.String())
	formatted, ferr := format.Source(src)
	if ferr == nil {
		src = formatted
	}
	return writeFile(outPath, src)
}
