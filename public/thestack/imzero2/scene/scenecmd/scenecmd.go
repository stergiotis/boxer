// Package scenecmd is the `imzero2 scene` subcommand: run scene documents
// against a headless host this command launches itself (ADR-0248 §SD4).
//
// `imzero2 drive` is for a host that is already running; this is `drive` plus
// the launch, the preconditions, the teardown and the gallery.
package scenecmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/thestack/imzero2/scene"
	"github.com/urfave/cli/v3"
)

const (
	flagOut     = "out"
	flagTimeout = "timeout"
	flagSettle  = "settle"
	flagDryRun  = "dryRun"
	flagList    = "list"
	flagOnly    = "only"
	flagClient  = "clientBinary"
	flagRoot    = "repoRoot"
	flagHostDir = "hostDir"
	flagIgnore  = "ignoreRequires"
	flagSlow    = "slow"
	flagLayout  = "layout"
	flagLayOnly = "layoutOnly"
)

// NewCommand builds the `scene` subcommand.
func NewCommand() *cli.Command {
	return &cli.Command{
		Name:      "scene",
		Usage:     "launch an app headless, run a scene document against it, tear down",
		ArgsUsage: "<doc" + scene.DocSuffix + " | dir>…",
		Description: "A scene document is markdown: frontmatter with a `scene:` launch spec,\n" +
			"prose, an optional `sql` fence that seeds the app's buffer, and a\n" +
			"`jsonl trace` fence of `imzero2 drive` steps. One document is one launch;\n" +
			"a directory is its *" + scene.DocSuffix + " files in name order.\n\n" +
			"   imzero2 scene apps/play/scenes                 # every scene of a tour\n" +
			"   imzero2 scene --only history apps/play/scenes  # those whose name contains it\n" +
			"   imzero2 scene --dryRun apps/play/scenes        # resolve anchors, capture nothing\n\n" +
			"Exit status is the assertion: non-zero when any scene failed. A scene whose\n" +
			"precondition does not hold is skipped, reported as skipped, and is not a pass.\n\n" +
			"Every capture that writes a tree sidecar is checked for layout invariants\n" +
			"(ADR-0363 M1); a finding fails the scene unless its layoutExceptions cover it.\n" +
			"   imzero2 scene --layoutOnly apps/play/scenes    # check what an earlier run left in --out",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagOut, Value: "tmp/scenes", Usage: "directory for captures, logs and the gallery index", TakesFile: true},
			&cli.DurationFlag{Name: flagTimeout, Value: 60 * time.Second, Usage: "bound on the wait for the carrier and on each driver request"},
			&cli.IntFlag{Name: flagSettle, Value: 300, Usage: "milliseconds to settle after a step that sets no settleMs of its own"},
			&cli.BoolFlag{Name: flagDryRun, Usage: "launch and resolve every anchor, without sending input or capturing"},
			&cli.BoolFlag{Name: flagList, Usage: "list the scenes and exit"},
			&cli.BoolFlag{Name: flagSlow, Usage: "also run scenes tagged slow, which are skipped otherwise"},
			&cli.BoolFlag{Name: flagIgnore, Usage: "run scenes whose preconditions do not hold instead of skipping them"},
			&cli.BoolFlag{Name: flagLayout, Value: true, Usage: "check the layout invariants over every capture with a tree sidecar; --layout=false skips them"},
			&cli.BoolFlag{Name: flagLayOnly, Usage: "launch nothing: check the layout of the captures an earlier run left in --out"},
			&cli.StringSliceFlag{Name: flagOnly, Usage: "run only scenes whose name contains this; repeatable"},
			&cli.StringFlag{Name: flagClient, Usage: "headless Rust client; default: the CPU rasterizer build, then the wgpu build", TakesFile: true},
			&cli.StringFlag{Name: flagRoot, Usage: "checkout holding rust/imzero2 (the client, the fonts); default: found from the working directory", TakesFile: true},
			&cli.StringFlag{Name: flagHostDir, Usage: "working directory of the launched host; default: --" + flagRoot, TakesFile: true},
		},
		Action: run,
	}
}

func run(ctx context.Context, cmd *cli.Command) (err error) {
	if cmd.NArg() == 0 {
		return eh.Errorf("nothing to run: pass scene documents or directories")
	}
	docs, err := scene.CollectDocs(cmd.Args().Slice())
	if err != nil {
		return err
	}
	if only := cmd.StringSlice(flagOnly); len(only) > 0 {
		kept := docs[:0]
		for _, d := range docs {
			for _, o := range only {
				if strings.Contains(d.Name, o) {
					kept = append(kept, d)
					break
				}
			}
		}
		docs = kept
	}
	if len(docs) == 0 {
		return eh.Errorf("no scene documents selected")
	}
	w := cmd.Root().Writer
	if cmd.Bool(flagList) {
		for _, d := range docs {
			_, _ = fmt.Fprintf(w, "%-40s %s\n", d.Name, d.Title())
		}
		return nil
	}

	root := cmd.String(flagRoot)
	if root == "" {
		if root, err = scene.FindRepoRoot("."); err != nil {
			return err
		}
	}
	out, err := filepath.Abs(cmd.String(flagOut))
	if err != nil {
		return eh.Errorf("unable to resolve --"+flagOut+": %w", err)
	}
	if err = os.MkdirAll(out, 0o755); err != nil {
		return eh.Errorf("unable to create --"+flagOut+": %w", err)
	}

	var results []scene.Result
	counts := map[scene.StatusE]int{}
	for _, d := range docs {
		if cmd.Bool(flagLayOnly) {
			res, checkable := checkLayoutOnly(d, out)
			if !checkable {
				// Nothing to check is not a scene of this run.
				continue
			}
			results = append(results, res)
			counts[res.Status]++
			report(w, res, "")
			continue
		}
		res := scene.RunDoc(d, scene.Options{
			OutDir:         out,
			RepoRoot:       root,
			HostDir:        cmd.String(flagHostDir),
			ClientBinary:   cmd.String(flagClient),
			Timeout:        cmd.Duration(flagTimeout),
			SettleMs:       cmd.Int(flagSettle),
			DryRun:         cmd.Bool(flagDryRun),
			IgnoreRequires: cmd.Bool(flagIgnore),
			Slow:           cmd.Bool(flagSlow),
			Out:            w,
			Logger:         log.Logger,
		})
		if res.Status == scene.StatusPass && cmd.Bool(flagLayout) && !cmd.Bool(flagDryRun) {
			applyLayout(&res, out)
		}
		results = append(results, res)
		counts[res.Status]++
		report(w, res, filepath.Join(out, "logs", d.Name+".host.log"))
	}
	// The host's periodic dump sink always writes its first frame.
	_ = os.Remove(filepath.Join(out, "frame_000000.png"))
	if err = scene.WriteIndex(out, results); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(w, "\n%d passed, %d failed, %d skipped → %s\n",
		counts[scene.StatusPass], counts[scene.StatusFail], counts[scene.StatusSkip], filepath.Join(out, "index.md"))
	if counts[scene.StatusFail] > 0 {
		return eb.Build().Int("failed", counts[scene.StatusFail]).Errorf("scenes failed")
	}
	return nil
}

// report prints one scene's status line, and its error when it has one.
func report(w io.Writer, res scene.Result, hostLog string) {
	line := fmt.Sprintf("%-5s %-40s %5.1fs", res.Status, res.Doc.Name, res.Duration.Seconds())
	if res.Reason != "" {
		line += "  " + res.Reason
	}
	_, _ = fmt.Fprintln(w, line)
	if res.Err != nil {
		// The rendered error, fields included: what a step read and what
		// it expected are fields, and a bare message would drop them.
		_, _ = fmt.Fprintln(w, indent(scene.PlainError(res.Err)))
		if hostLog != "" {
			_, _ = fmt.Fprintln(w, "      host log: "+hostLog)
		}
	}
}

// applyLayout checks a passed scene's captures and fails it on a finding no
// exception covers.
func applyLayout(res *scene.Result, out string) {
	rep, err := scene.CheckLayout(res.Doc, out)
	if err == nil {
		err = rep.Err()
	}
	if err != nil {
		res.Status, res.Err = scene.StatusFail, err
	}
	if s := rep.Summary(); s != "" {
		res.Reason = strings.TrimPrefix(res.Reason+"; "+s, "; ")
	}
}

// checkLayoutOnly is applyLayout over files an earlier run wrote. checkable is
// false for a scene that asks for no tree sidecar. One whose sidecars are
// not there is skipped: there is nothing to check, which is not a pass.
func checkLayoutOnly(d *scene.Doc, out string) (res scene.Result, checkable bool) {
	start := time.Now()
	res = scene.Result{Doc: d, Status: scene.StatusPass}
	defer func() { res.Duration = time.Since(start) }()
	rep, err := scene.CheckLayout(d, out)
	switch {
	case err != nil:
		res.Status, res.Err = scene.StatusFail, err
		return res, true
	case len(rep.Checked) == 0 && len(rep.Missing) == 0:
		return res, false
	case len(rep.Checked) == 0:
		res.Status, res.Reason = scene.StatusSkip, "not in --"+flagOut+": "+strings.Join(rep.Missing, ", ")
		return res, true
	}
	for _, c := range d.CaptureFiles() {
		res.Captures = append(res.Captures, c.PNG)
		res.Sidecars = append(res.Sidecars, c.Sidecars...)
	}
	if err = rep.Err(); err != nil {
		res.Status, res.Err = scene.StatusFail, err
	}
	res.Reason = rep.Summary()
	if len(rep.Missing) > 0 {
		res.Reason += "; not in --" + flagOut + ": " + strings.Join(rep.Missing, ", ")
	}
	return res, true
}

func indent(text string) string {
	return "      " + strings.ReplaceAll(text, "\n", "\n      ")
}
