package wasmsurvey

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/stergiotis/boxer/public/code/analysis/golang/godep/godepcollect"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	cli "github.com/urfave/cli/v2"
)

// newPropsCommand is the `props` group (ADR-0080): generate seeds per-package
// PackageProps declarations from the survey, harvest reads them into a table,
// and verify reconciles declaration against the computed verdict.
func newPropsCommand() *cli.Command {
	return &cli.Command{
		Name:  "props",
		Usage: "Seed/harvest/verify co-located PackageProps declarations (ADR-0080)",
		Subcommands: []*cli.Command{
			{
				Name:   "generate",
				Usage:  "Seed a package_props.go in each in-scope package from the survey verdict (idempotent-create; --overwrite re-seeds)",
				Flags:  append(computeFlags(), &cli.BoolFlag{Name: "overwrite", Usage: "rewrite existing props files (initial rollout / refresh) instead of skipping them"}),
				Action: runPropsGenerate,
			},
			{
				Name:  "harvest",
				Usage: "Read committed PackageProps declarations into an overview table (no survey, no TinyGo)",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "dir", Usage: "module dir; empty resolves nearest go.mod above wd"},
					&cli.StringSliceFlag{Name: "patterns", Usage: "scope, e.g. ./public/math/...", Value: cli.NewStringSlice("./...")},
				},
				Action: runPropsHarvest,
			},
			{
				Name:   "verify",
				Usage:  "Reconcile declared PackageProps against the freshly computed verdict; non-zero exit on a regression",
				Flags:  computeFlags(),
				Action: runPropsVerify,
			},
		},
	}
}

func runPropsGenerate(c *cli.Context) (err error) {
	var opts Options
	if opts, err = wasmSurveyOptions(c); err != nil {
		return err
	}
	var res GenerateResult
	if res, err = GenerateProps(c.Context, opts, c.Bool("overwrite")); err != nil {
		return err
	}
	for _, p := range res.WrittenPaths {
		fmt.Fprintf(os.Stdout, "wrote %s\n", p)
	}
	fmt.Fprintf(os.Stdout, "props generate: %d created, %d overwritten, %d skipped\n", res.Created, res.Overwritten, res.Skipped)
	return nil
}

func runPropsHarvest(c *cli.Context) (err error) {
	dir := c.String("dir")
	if dir == "" {
		if wd, e := os.Getwd(); e == nil {
			if root, ok := godepcollect.ModuleRoot(wd); ok {
				dir = root
			} else {
				dir = wd
			}
		}
	}
	var modPath string
	if modPath, err = readModulePath(dir); err != nil {
		return err
	}
	var rows []HarvestRow
	if rows, err = HarvestProps(dir, modPath); err != nil {
		return err
	}
	prefixes := patternsToPrefixes(modPath, c.StringSlice("patterns"))

	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "package\twasi\tjs\tfreestanding")
	n := 0
	for _, r := range rows {
		if !inScope(r.ImportPath, prefixes) {
			continue
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
			strings.TrimPrefix(r.ImportPath, modPath+"/"), r.WASMWASI, r.WASMJS, r.WASMFreestanding)
		n++
	}
	_ = tw.Flush()
	fmt.Fprintf(os.Stdout, "%d declared package(s)\n", n)
	return nil
}

func runPropsVerify(c *cli.Context) (err error) {
	var opts Options
	if opts, err = wasmSurveyOptions(c); err != nil {
		return err
	}
	var mismatches []Mismatch
	if mismatches, err = VerifyProps(c.Context, opts, opts.Dir); err != nil {
		return err
	}
	if len(mismatches) == 0 {
		fmt.Fprintln(os.Stdout, "props verify: all declarations match the computed verdict")
		return nil
	}
	regressions := 0
	for _, m := range mismatches {
		tag := "drift"
		if m.IsRegression {
			tag = "REGRESSION"
			regressions++
		}
		fmt.Fprintf(os.Stdout, "%-11s %s [%s] declared=%s computed=%s\n",
			tag, m.ImportPath, m.Target, m.Declared, m.Computed)
	}
	fmt.Fprintf(os.Stdout, "props verify: %d mismatch(es), %d regression(s)\n", len(mismatches), regressions)
	if regressions > 0 {
		return eb.Build().Errorf("props verify failed: %d declared-amenable package(s) are now blocked", regressions)
	}
	return nil
}

// readModulePath returns the module path from <root>/go.mod.
func readModulePath(root string) (modPath string, err error) {
	var f *os.File
	if f, err = os.Open(filepath.Join(root, "go.mod")); err != nil {
		return "", eb.Build().Str("root", root).Errorf("open go.mod: %w", err)
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if after, ok := strings.CutPrefix(strings.TrimSpace(sc.Text()), "module "); ok {
			return strings.TrimSpace(after), nil
		}
	}
	return "", eb.Build().Str("root", root).Errorf("no module directive in go.mod")
}
