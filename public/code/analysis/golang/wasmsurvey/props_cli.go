package wasmsurvey

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/stergiotis/boxer/public/code/analysis/golang/godep/godepcollect"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/packageprops"
	"github.com/stergiotis/boxer/public/packageprops/proptable"
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
				Usage: "Read committed PackageProps declarations into a table or a Go Table literal (no survey, no TinyGo)",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "dir", Usage: "module dir; empty resolves nearest go.mod above wd"},
					&cli.StringSliceFlag{Name: "patterns", Usage: "scope, e.g. ./public/math/...", Value: cli.NewStringSlice("./...")},
					&cli.StringFlag{Name: "emit", Usage: "output format: table | go", Value: "table"},
					&cli.StringFlag{Name: "out", Usage: "output path for --emit go (\"-\" or empty = stdout)"},
					&cli.StringFlag{Name: "package", Usage: "package clause for --emit go", Value: "proptable"},
					&cli.BoolFlag{Name: "tracked", Usage: "read only git-tracked declarations — required to regenerate the committed table, since `props drift` gates it on that same scope"},
				},
				Action: runPropsHarvest,
			},
			{
				Name:  "drift",
				Usage: "Reconcile the generated props table against the git-tracked declarations; non-zero exit on any difference",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "dir", Usage: "module dir; empty resolves nearest go.mod above wd"},
				},
				Action: runPropsDrift,
			},
			{
				Name:  "verify",
				Usage: "Reconcile declared PackageProps against the freshly computed verdict; non-zero exit on a regression",
				Flags: append(computeFlags(), &cli.BoolFlag{
					Name:  "show-unjudged",
					Usage: "also list packages the survey left unjudged (computed=unknown), which without TinyGo is most of them",
				}),
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
	// --tracked and `props drift` must agree on scope, or a regen lands a
	// table the gate immediately rejects. Regenerating the committed table is
	// therefore always the --tracked form; the working-tree default stays for
	// the human `--emit table` overview, where seeing your own in-flight
	// package is the point.
	var rows []HarvestRow
	if c.Bool("tracked") {
		rows, err = HarvestTracked(c.Context, dir, modPath)
	} else {
		rows, err = HarvestProps(dir, modPath)
	}
	if err != nil {
		return err
	}
	prefixes := patternsToPrefixes(modPath, c.StringSlice("patterns"))
	scoped := make([]HarvestRow, 0, len(rows))
	for _, r := range rows {
		if inScope(r.ImportPath, prefixes) {
			scoped = append(scoped, r)
		}
	}

	switch c.String("emit") {
	case "go":
		var src []byte
		if src, err = renderHarvestGo(scoped, c.String("package")); err != nil {
			return err
		}
		out := c.String("out")
		if out == "" || out == "-" {
			_, err = os.Stdout.Write(src)
			return err
		}
		if err = os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return eb.Build().Str("out", out).Errorf("mkdir for emit: %w", err)
		}
		if err = os.WriteFile(out, src, 0o644); err != nil {
			return eb.Build().Str("out", out).Errorf("write emit: %w", err)
		}
		fmt.Fprintf(os.Stdout, "wrote %s (%d packages)\n", out, len(scoped))
		return nil
	case "table", "":
		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "package\twasi\tjs\tfreestanding\tkind")
		for _, r := range scoped {
			kind := "" // blank for the common ordinary-code case
			if r.Kind != packageprops.KindUnspecified {
				kind = r.Kind.String()
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
				strings.TrimPrefix(r.ImportPath, modPath+"/"), r.WASMWASI, r.WASMJS, r.WASMFreestanding, kind)
		}
		_ = tw.Flush()
		fmt.Fprintf(os.Stdout, "%d declared package(s)\n", len(scoped))
		return nil
	default:
		return eb.Build().Str("emit", c.String("emit")).Errorf("unknown --emit (want table|go)")
	}
}

// runPropsDrift gates the generated table against the tracked declarations.
//
// It needs neither the survey nor TinyGo — it parses declarations and compares
// them to the compiled-in table — so it is cheap enough to run on every lint
// pass, which is the property that makes it a gate rather than a chore.
func runPropsDrift(c *cli.Context) (err error) {
	root := c.String("dir")
	if root == "" {
		wd, e := os.Getwd()
		if e != nil {
			return eh.Errorf("props drift: working dir: %w", e)
		}
		if r, ok := godepcollect.ModuleRoot(wd); ok {
			root = r
		} else {
			root = wd
		}
	}
	modPath, err := readModulePath(root)
	if err != nil {
		return err
	}
	declared, err := HarvestTracked(c.Context, root, modPath)
	if err != nil {
		return err
	}
	drifts := DriftAgainstTable(proptable.Table, declared)
	if len(drifts) == 0 {
		fmt.Fprintf(os.Stdout, "props drift: table matches %d tracked declaration(s)\n", len(declared))
		return nil
	}
	for _, d := range drifts {
		fmt.Fprintf(os.Stdout, "%-8s %s\n", d.Kind, strings.TrimPrefix(d.ImportPath, modPath+"/"))
	}
	fmt.Fprintf(os.Stdout, "props drift: %d difference(s) against %d tracked declaration(s)\n",
		len(drifts), len(declared))
	return eb.Build().Int("drifts", len(drifts)).
		Errorf("props drift: the generated table disagrees with the tracked declarations; regenerate it with `props harvest --tracked --emit go --out public/packageprops/proptable/proptable.out.go` (--tracked is required: it is the scope this check compares against)")
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
	// A computed verdict of "unknown" is the oracle declining, not a
	// disagreement: static mode proves red soundly and says nothing about the
	// rest (ADR-0078), so without TinyGo it leaves most packages unjudged.
	// Listing those beside real findings buries them — the first run of this
	// command against the tree reported 882 mismatches, of which 870 were
	// abstentions and 12 were the regressions that mattered. They are counted
	// here and listed only on request, because a gate whose output is mostly
	// noise is a gate that gets tuned out.
	showUnjudged := c.Bool("show-unjudged")
	regressions, drifts, unjudged := 0, 0, 0
	for _, m := range mismatches {
		tag := "drift"
		switch {
		case m.Computed == packageprops.WASMUnknown:
			unjudged++
			if !showUnjudged {
				continue
			}
			tag = "unjudged"
		case m.IsRegression:
			tag = "REGRESSION"
			regressions++
		default:
			drifts++
		}
		fmt.Fprintf(os.Stdout, "%-11s %s [%s] declared=%s computed=%s\n",
			tag, m.ImportPath, m.Target, m.Declared, m.Computed)
	}
	fmt.Fprintf(os.Stdout, "props verify: %d regression(s), %d drift(s), %d unjudged\n",
		regressions, drifts, unjudged)
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
