package capsurvey

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/stergiotis/boxer/public/code/analysis/golang/godep/godepcollect"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	cli "github.com/urfave/cli/v2"
)

// NewCliCommand returns the `capsurvey` subcommand (registered under `golang`,
// sibling to wasmsurvey). It classifies each package's capabilities with
// capslock and records them in the co-located PackageProps declarations
// (ADR-0120).
func NewCliCommand() *cli.Command {
	return &cli.Command{
		Name:  "capsurvey",
		Usage: "Survey what privileged operations each Go package can reach (ADR-0120)",
		Description: "Classifies every package's capabilities with google/capslock and records them in\n" +
			"its package_props.go as CapsDirect (what the package's own code does) and\n" +
			"CapsReachable (everything it can reach once dependencies are followed).\n\n" +
			"Read CapsDirect first. Reachability saturates — nearly every package\n" +
			"reaches nearly everything through the standard library — so its value is in the\n" +
			"negative: an absent bit proves a package cannot reach a capability at all.\n\n" +
			"The survey builds SSA for the whole module and is correspondingly hungry:\n" +
			"expect several GB of peak RSS and tens of seconds for ./... — it is a codegen\n" +
			"step, not part of a build.",
		Subcommands: []*cli.Command{
			{
				Name:   "report",
				Usage:  "Print each package's capability verdict without touching the tree",
				Flags:  append(computeFlags(), &cli.BoolFlag{Name: "show-safe", Usage: "include packages that reach nothing privileged"}),
				Action: runReport,
			},
			{
				Name:   "generate",
				Usage:  "Record the capability verdict in each package's package_props.go (overlays; never clobbers other fields)",
				Flags:  computeFlags(),
				Action: runGenerate,
			},
			{
				Name:   "verify",
				Usage:  "Reconcile declared capability verdicts against a fresh survey; non-zero exit on drift",
				Flags:  computeFlags(),
				Action: runVerify,
			},
		},
	}
}

func computeFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: "dir", Usage: "module dir to survey; empty resolves the nearest go.mod above the working dir"},
		&cli.StringSliceFlag{Name: "patterns", Usage: "go list patterns", Value: cli.NewStringSlice("./...")},
		&cli.StringFlag{Name: "tags", Usage: "comma-separated build tags; empty falls back to <root>/tags then GOFLAGS"},
	}
}

// capSurveyOptions builds Options from the flags, resolving the module dir to a
// concrete path.
func capSurveyOptions(c *cli.Context) (opts Options) {
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
	return Options{
		Dir:      dir,
		Patterns: c.StringSlice("patterns"),
		Tags:     godepcollect.ResolveTags(c.String("tags"), dir),
	}
}

func runReport(c *cli.Context) (err error) {
	opts := capSurveyOptions(c)
	var s Survey
	if s, err = Run(c.Context, opts); err != nil {
		return err
	}
	showSafe := c.Bool("show-safe")
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "package\tdirect\treachable")
	shown, safe := 0, 0
	for _, pr := range s.Packages {
		if pr.Direct.Safe() && !showSafe {
			safe++
			continue
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\n",
			strings.TrimPrefix(pr.ImportPath, s.RootModule+"/"), pr.Direct, pr.Reachable)
		shown++
	}
	_ = tw.Flush()
	fmt.Fprintf(os.Stdout, "\n%d package(s) surveyed", len(s.Packages))
	if safe > 0 {
		fmt.Fprintf(os.Stdout, "; %d reach nothing privileged (--show-safe to list)", safe)
	}
	fmt.Fprintln(os.Stdout)
	reportCaveats(s)
	return nil
}

func runGenerate(c *cli.Context) (err error) {
	opts := capSurveyOptions(c)
	var res GenerateResult
	if res, err = GenerateProps(c.Context, opts); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "capsurvey generate: %d created, %d updated, %d already current\n",
		res.Created, res.Updated, res.Unchanged)
	return nil
}

func runVerify(c *cli.Context) (err error) {
	opts := capSurveyOptions(c)
	var drift []VerifyResult
	if drift, err = VerifyProps(c.Context, opts); err != nil {
		return err
	}
	if len(drift) == 0 {
		fmt.Fprintln(os.Stdout, "capsurvey verify: every declared capability verdict matches the survey")
		return nil
	}
	tw := tabwriter.NewWriter(os.Stderr, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "package\tfield\tdeclared\tsurveyed")
	for _, d := range drift {
		if d.DeclaredDirect != d.SurveyedDirect {
			fmt.Fprintf(tw, "%s\tdirect\t%s\t%s\n", d.ImportPath, d.DeclaredDirect, d.SurveyedDirect)
		}
		if d.DeclaredReachable != d.SurveyedReachable {
			fmt.Fprintf(tw, "%s\treachable\t%s\t%s\n", d.ImportPath, d.DeclaredReachable, d.SurveyedReachable)
		}
	}
	_ = tw.Flush()
	return eb.Build().Int("packages", len(drift)).Errorf(
		"declared capability verdicts are stale; re-run `capsurvey generate`")
}

// reportCaveats surfaces the two ways a survey can be quietly incomplete, so a
// clean-looking report is not mistaken for a complete one.
func reportCaveats(s Survey) {
	if len(s.Failed) > 0 {
		fmt.Fprintf(os.Stderr, "warning: %d package(s) failed to load and were not surveyed: %s\n",
			len(s.Failed), strings.Join(s.Failed, " "))
	}
	if len(s.Unknown) > 0 {
		fmt.Fprintf(os.Stderr, "warning: capslock reported %d capability/ies unknown to public/packageprops and they were dropped: %s\n",
			len(s.Unknown), strings.Join(s.Unknown, " "))
	}
}
