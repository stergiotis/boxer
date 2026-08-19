package main

// The canonical-leeway-JSON-mapping arm of the trial
// (doc/trials/jsonbench-on-facts/). Where the boxer.facts arm bends the corpus
// to fit a schema built for facts — paths demoted into the parameter channel
// because facts memberships are Ref-shaped, array-valued sections forcing a
// second cumulative-sum reconstruction on every read — this arm loads the same
// documents under mapping.LoadJsonMapping, which was designed for exactly this
// shape:
//
//   - memberships are MixedLowCardVerbatim, so a path rides `lmv` verbatim and
//     needs no vocabulary registration;
//   - the array indices ride `mvhp` in the canonical params form
//     (membership.AppendParams);
//   - every section is scalar, so `lmv` co-indexes 1:1 with the value lane and
//     `indexOf` alone resolves a path.
//
// Nulls are *not* represented: the mapping declares a `null` section and this
// arm deliberately leaves it empty, counting what it drops. That keeps the
// comparison against the facts arm honest — facts has no null section at all —
// and is recorded as a property of the load, not hidden.

import (
	"fmt"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v2"

	"github.com/stergiotis/boxer/public/code/synthesis/golang/align"
	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	leewayddl "github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	leewaydml "github.com/stergiotis/boxer/public/semistructured/leeway/dml"
	"github.com/stergiotis/boxer/public/semistructured/leeway/gocodegen"
	"github.com/stergiotis/boxer/public/semistructured/leeway/mapping"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
)

const (
	// jsonmapPackageName is the trial-local package the DML builder is
	// generated into. It stays under apps/ rather than being promoted into the
	// leeway packages: a shipped JSON shredder is ledger row 1 and needs a
	// design dialogue first.
	jsonmapPackageName = "jsonmap"
	jsonmapOutputPath  = "apps/jsonbench/jsonmap/dml_json.out.go"

	// jsonmapNamingSeparator matches the separator every other leeway table in
	// this tree uses, so `jsonbench resolve` (ADR-0116 column handles) works
	// against this table too.
	jsonmapNamingSeparator = ":"

	// jsonmapSettingsClause — the same suspicious-low-cardinality opt-in the
	// facts DDL needs (trial ledger row 11); the symbol section's membership
	// lanes are LowCardinality(String) over a high-cardinality domain.
	jsonmapSettingsClause = "allow_suspicious_low_cardinality_types=1"
)

// jsonmapTableRowConfig — one row carries many attributes, the configuration
// every leeway table in this tree uses and the one the generated example is
// built with.
const jsonmapTableRowConfig = common.TableRowConfigMultiAttributesPerRow

func jsonmapCommand() *cli.Command {
	return &cli.Command{
		Name:  "jsonmap",
		Usage: "the canonical leeway JSON mapping arm: codegen, DDL, ingest",
		Subcommands: []*cli.Command{
			jsonmapCodegenCommand(),
			jsonmapDdlCommand(),
			jsonmapIngestCommand(),
		},
	}
}

func jsonmapCodegenCommand() *cli.Command {
	return &cli.Command{
		Name:  "codegen",
		Usage: "regenerate the DML builder for the canonical JSON mapping",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "out", Value: jsonmapOutputPath, Usage: "output path, relative to the repo root"},
		},
		Action: runJsonmapCodegen,
	}
}

func runJsonmapCodegen(cCtx *cli.Context) (err error) {
	var tbl common.TableDesc
	tbl, err = mapping.NewJsonMapping()
	if err != nil {
		err = eh.Errorf("build json mapping table desc: %w", err)
		return
	}
	var conv *leewayddl.HumanReadableNamingConvention
	conv, err = leewayddl.NewHumanReadableNamingConvention(jsonmapNamingSeparator)
	if err != nil {
		err = eh.Errorf("naming convention: %w", err)
		return
	}
	driver := leewaydml.NewGoCodeGeneratorDriver(conv, clickhouse.NewTechnologySpecificCodeGenerator())
	var code []byte
	code, _, err = driver.GenerateGoClasses(
		jsonmapPackageName,
		naming.MustBeValidStylableName("json"),
		tbl,
		jsonmapTableRowConfig,
		gocodegen.NewMultiTablePerPackageGoClassNamer(),
	)
	if err != nil {
		err = eh.Errorf("generate dml go classes: %w", err)
		return
	}
	out := cCtx.String("out")
	err = align.WriteAligned(out, code)
	if err != nil {
		err = eh.Errorf("write %s: %w", out, err)
		return
	}
	log.Info().Str("out", out).Int("bytes", len(code)).Msg("canonical JSON mapping DML generated")
	return
}

func jsonmapDdlCommand() *cli.Command {
	return &cli.Command{
		Name:  "ddl",
		Usage: "emit or apply the canonical JSON mapping DDL against a benchmark-local database",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "url", Value: "http://localhost:8123/"},
			&cli.StringFlag{Name: "user", Value: "default"},
			&cli.StringFlag{Name: "password"},
			&cli.StringFlag{Name: "database", Required: true},
			&cli.StringFlag{Name: "table", Value: "json"},
			&cli.StringFlag{
				Name: "engine",
				// tuple() rather than a workload-shaped key, for the reason the
				// trial's arm A0 exists: a store holding a mixture of document
				// shapes cannot sort on paths most of its rows do not carry.
				// Re-keying is a separate, measurable lever (arm E).
				Value: "MergeTree() ORDER BY tuple()",
				Usage: "MergeTree clause",
			},
			&cli.BoolFlag{Name: "apply", Usage: "execute the DDL instead of printing it"},
			&cli.BoolFlag{Name: "drop", Usage: "DROP DATABASE first (benchmark databases only)"},
		},
		Action: runJsonmapDdl,
	}
}

func runJsonmapDdl(cCtx *cli.Context) (err error) {
	db := cCtx.String("database")
	// The same guard the facts DDL command carries: this drops databases, and
	// the live store must never be a target.
	if db == "boxer" {
		err = eh.Errorf("refusing to target the live facts database %q", db)
		return
	}
	table := cCtx.String("table")
	var sql string
	sql, err = composeJsonmapCreateTableSQL(db, table, cCtx.String("engine"))
	if err != nil {
		return
	}
	if !cCtx.Bool("apply") {
		fmt.Println(sql)
		return
	}
	cli0 := chclient.New(chclient.Config{
		URL:      cCtx.String("url"),
		User:     cCtx.String("user"),
		Password: cCtx.String("password"),
	}, nil)
	ctx := cCtx.Context
	if cCtx.Bool("drop") {
		err = cli0.Exec(ctx, "DROP DATABASE IF EXISTS "+db)
		if err != nil {
			err = eh.Errorf("drop database %s: %w", db, err)
			return
		}
	}
	for stmt := range strings.SplitSeq(sql, ";") {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		err = cli0.Exec(ctx, stmt)
		if err != nil {
			err = eh.Errorf("exec ddl: %w", err)
			return
		}
	}
	log.Info().Str("database", db).Str("table", table).Msg("canonical JSON mapping DDL applied")
	return
}

// composeJsonmapCreateTableSQL runs the leeway DDL pipeline over
// mapping.NewJsonMapping and wraps the emitted column block. The columns come
// out of the same generator the facts table's DDL does, so this arm's table is
// provably the canonical mapping rather than a hand-copied approximation.
func composeJsonmapCreateTableSQL(database string, table string, engineClause string) (sql string, err error) {
	if engineClause == "" {
		err = eh.Errorf("compose ddl: empty engine clause")
		return
	}
	var columns string
	columns, err = emitJsonmapColumnsSQL()
	if err != nil {
		return
	}
	var buf strings.Builder
	_, _ = fmt.Fprintf(&buf, "CREATE DATABASE IF NOT EXISTS %s;\nCREATE TABLE IF NOT EXISTS %s.%s (\n", database, database, table)
	buf.WriteString(columns)
	_, _ = fmt.Fprintf(&buf, "\n) ENGINE %s\nSETTINGS %s;\n", engineClause, jsonmapSettingsClause)
	sql = buf.String()
	return
}

func emitJsonmapColumnsSQL() (columns string, err error) {
	var tbl common.TableDesc
	tbl, err = mapping.NewJsonMapping()
	if err != nil {
		err = eh.Errorf("compose ddl: build table desc: %w", err)
		return
	}
	var conv *leewayddl.HumanReadableNamingConvention
	conv, err = leewayddl.NewHumanReadableNamingConvention(jsonmapNamingSeparator)
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
	err = leewayddl.NewGeneratorDriver().GenerateColumnsCode(
		ir.IterateColumnProps(),
		jsonmapTableRowConfig,
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
