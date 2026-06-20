package adr

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/keelson/data/chlocalpool"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/urfave/cli/v2"
)

const (
	adrArrowName     = "adr.arrow"
	coderefArrowName = "coderef.arrow"
)

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
				Usage:  "parse ADRs, scan code references, and emit adr.arrow + coderef.arrow",
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
				Usage:     "run an arbitrary SQL query against the `adr` and `coderef` tables",
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
// into the rows, and emits both Arrow files into outDir. outDir is excluded
// from the code scan so emitted files never count as references.
func buildArtifacts(c *cli.Context, outDir string) (adrs []Adr, refs []CodeRef, adrArrow, coderefArrow string, err error) {
	root, adrDir := resolvePaths(c)
	if adrs, err = ParseDir(adrDir); err != nil {
		return nil, nil, "", "", err
	}
	if refs, err = ScanCodeRefs(root, adrDir, outDir); err != nil {
		return nil, nil, "", "", err
	}
	adrs = Aggregate(adrs, refs)
	if err = os.MkdirAll(outDir, 0o755); err != nil {
		return nil, nil, "", "", eh.Errorf("unable to create out dir %q: %w", outDir, err)
	}
	adrArrow = filepath.Join(outDir, adrArrowName)
	coderefArrow = filepath.Join(outDir, coderefArrowName)
	if err = WriteAdrArrow(adrArrow, adrs); err != nil {
		return nil, nil, "", "", err
	}
	if err = WriteCoderefArrow(coderefArrow, refs); err != nil {
		return nil, nil, "", "", err
	}
	return adrs, refs, adrArrow, coderefArrow, nil
}

func actionBuild(c *cli.Context) error {
	root, _ := resolvePaths(c)
	outDir := c.String("out")
	if !filepath.IsAbs(outDir) {
		outDir = filepath.Join(root, outDir)
	}
	adrs, refs, adrArrow, coderefArrow, err := buildArtifacts(c, outDir)
	if err != nil {
		return err
	}
	log.Info().Int("adrs", len(adrs)).Int("coderefs", len(refs)).
		Str("adrArrow", adrArrow).Str("coderefArrow", coderefArrow).
		Msg("emitted ADR Arrow tables")
	return nil
}

func actionOverview(c *cli.Context) error {
	tmp, err := os.MkdirTemp("", "boxer-adr-")
	if err != nil {
		return eh.Errorf("unable to create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	adrs, _, adrArrow, coderefArrow, err := buildArtifacts(c, tmp)
	if err != nil {
		return err
	}
	ok, err := RunOverview(adrArrow, coderefArrow, os.Stdout)
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
	_, _, adrArrow, coderefArrow, err := buildArtifacts(c, tmp)
	if err != nil {
		return err
	}
	ok, err := RunQuery(adrArrow, coderefArrow, sql, c.String("format"), os.Stdout)
	if err != nil {
		return err
	}
	if !ok {
		return eh.Errorf("clickhouse-local not found (looked at %s and $PATH); install it or run `boxer adr build` and query the Arrow files yourself", chlocalpool.DefaultBinaryPath)
	}
	return nil
}
