// Package adr is the `boxer adr` command: it turns the doc/adr corpus — parsed
// by [github.com/stergiotis/boxer/public/gov/adrcorpus] — into ClickHouse-
// queryable Arrow tables, so the state of every ADR can be inspected with SQL
// via clickhouse-local.
//
// Three tables are emitted and bound by name: `adr` (one row per decision,
// carrying both the lifecycle status and the code-evidence columns), `coderef`
// (one row per citation, for drill-down) and `subtask` (one row per sub-item an
// ADR declares for itself, with its declared done-ness).
//
// The axes and the ADR-reference convention the evidence axis depends on are
// recorded in ADR-0092. Everything here is the *query surface*; the corpus
// model itself lives in adrcorpus, which the keelson providers expose as tables
// of the same names and schemas (ADR-0122 §SD4) without linking this command —
// a test pins the two schema sets equal, so a query written here runs there.
package adr

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/gov/adrcorpus"
	"github.com/stergiotis/boxer/public/keelson/data/chlocalpool"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/urfave/cli/v2"
)

const (
	adrArrowName     = "adr.arrow"
	coderefArrowName = "coderef.arrow"
	subtaskArrowName = "subtask.arrow"
)

// artifacts are the emitted Arrow files, bound as the query tables of the same
// name.
type artifacts struct {
	adrArrow     string
	coderefArrow string
	subtaskArrow string
}

func rootFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: "root", Value: ".", Usage: "repository root to scan for ADR code references"},
		&cli.StringFlag{Name: "adr-dir", Value: "doc/adr", Usage: "ADR markdown directory (relative to --root unless absolute)"},
	}
}

// NewCliCommand wires the `boxer adr` command and its subcommands.
func NewCliCommand() *cli.Command {
	return &cli.Command{
		Name:  "adr",
		Usage: "overview of doc/adr ADRs as ClickHouse-queryable Arrow tables (decision status × code-evidence implementation degree)",
		Subcommands: []*cli.Command{
			{
				Name:   "build",
				Usage:  "parse ADRs, scan code references, and emit adr.arrow + coderef.arrow + subtask.arrow",
				Flags:  append(rootFlags(), &cli.StringFlag{Name: "out", Value: ".adrcache", Usage: "output directory for the Arrow files (relative to --root unless absolute)"}),
				Action: actionBuild,
			},
			{
				Name:   "overview",
				Usage:  "print the canned two-axis overview via clickhouse-local (plain-text board if it is absent)",
				Flags:  rootFlags(),
				Action: actionOverview,
			},
			{
				Name:      "query",
				Usage:     "run an arbitrary SQL query against the `adr`, `coderef` and `subtask` tables",
				ArgsUsage: "<SQL>",
				Flags:     append(rootFlags(), &cli.StringFlag{Name: "format", Value: "PrettyCompact", Usage: "clickhouse-local --output-format"}),
				Action:    actionQuery,
			},
		},
	}
}

func resolvePaths(c *cli.Context) (root, adrDir string) {
	root = c.String("root")
	adrDir = c.String("adr-dir")
	if !filepath.IsAbs(adrDir) {
		adrDir = filepath.Join(root, adrDir)
	}
	return root, adrDir
}

// buildArtifacts parses the corpus, scans code references, folds the evidence
// into the rows, and emits the Arrow files into outDir. outDir is excluded
// from the code scan so emitted files never count as references.
func buildArtifacts(c *cli.Context, outDir string) (adrs []adrcorpus.Adr, refs []adrcorpus.CodeRef, subs []adrcorpus.Subtask, arts artifacts, err error) {
	root, adrDir := resolvePaths(c)
	if adrs, err = adrcorpus.ParseDir(adrDir); err != nil {
		return nil, nil, nil, arts, err
	}
	if refs, err = adrcorpus.ScanCodeRefs(root, adrDir, outDir); err != nil {
		return nil, nil, nil, arts, err
	}
	adrs = adrcorpus.Aggregate(adrs, refs)
	subs = adrcorpus.AllSubtasks(adrs)
	if err = os.MkdirAll(outDir, 0o755); err != nil {
		return nil, nil, nil, arts, eh.Errorf("unable to create out dir %q: %w", outDir, err)
	}
	arts = artifacts{
		adrArrow:     filepath.Join(outDir, adrArrowName),
		coderefArrow: filepath.Join(outDir, coderefArrowName),
		subtaskArrow: filepath.Join(outDir, subtaskArrowName),
	}
	if err = WriteAdrArrow(arts.adrArrow, adrs); err != nil {
		return nil, nil, nil, arts, err
	}
	if err = WriteCoderefArrow(arts.coderefArrow, refs); err != nil {
		return nil, nil, nil, arts, err
	}
	if err = WriteSubtaskArrow(arts.subtaskArrow, subs); err != nil {
		return nil, nil, nil, arts, err
	}
	return adrs, refs, subs, arts, nil
}

func actionBuild(c *cli.Context) error {
	root, _ := resolvePaths(c)
	outDir := c.String("out")
	if !filepath.IsAbs(outDir) {
		outDir = filepath.Join(root, outDir)
	}
	adrs, refs, subs, arts, err := buildArtifacts(c, outDir)
	if err != nil {
		return err
	}
	log.Info().Int("adrs", len(adrs)).Int("coderefs", len(refs)).Int("subtasks", len(subs)).
		Str("adrArrow", arts.adrArrow).Str("coderefArrow", arts.coderefArrow).
		Str("subtaskArrow", arts.subtaskArrow).
		Msg("emitted ADR Arrow tables")
	return nil
}

func actionOverview(c *cli.Context) error {
	tmp, err := os.MkdirTemp("", "boxer-adr-")
	if err != nil {
		return eh.Errorf("unable to create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	adrs, _, _, arts, err := buildArtifacts(c, tmp)
	if err != nil {
		return err
	}
	ok, err := RunOverview(arts.adrArrow, arts.coderefArrow, arts.subtaskArrow, os.Stdout)
	if err != nil {
		return err
	}
	if !ok {
		log.Warn().Str("lookedAt", chlocalpool.DefaultBinaryPath).
			Msg("clickhouse-local not found; printing the plain-text board instead")
		if _, e := fmt.Fprintln(os.Stdout, "ADR overview — decision status × code-evidence implementation degree:"); e != nil {
			return e
		}
		return RenderBoardASCII(adrs, os.Stdout)
	}
	return nil
}

func actionQuery(c *cli.Context) error {
	sql := c.Args().First()
	if sql == "" {
		return eh.Errorf("provide a SQL query as the argument, e.g. boxer adr query \"SELECT num,status,impl_evidence,title FROM adr ORDER BY code_refs DESC LIMIT 10\"")
	}
	tmp, err := os.MkdirTemp("", "boxer-adr-")
	if err != nil {
		return eh.Errorf("unable to create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	_, _, _, arts, err := buildArtifacts(c, tmp)
	if err != nil {
		return err
	}
	ok, err := RunQuery(arts.adrArrow, arts.coderefArrow, arts.subtaskArrow, sql, c.String("format"), os.Stdout)
	if err != nil {
		return err
	}
	if !ok {
		return eh.Errorf("clickhouse-local not found (looked at %s and $PATH); install it or run `boxer adr build` and query the Arrow files yourself", chlocalpool.DefaultBinaryPath)
	}
	return nil
}
