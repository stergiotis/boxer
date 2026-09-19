// Package scenecmd is the `imzero2 scene` subcommand: run scene documents
// against a headless host this command launches itself (ADR-0248 §SD4).
//
// `imzero2 drive` is for a host that is already running; this is `drive` plus
// the launch, the preconditions, the teardown and the gallery.
package scenecmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/thestack/imzero2/scene"
	"github.com/urfave/cli/v2"
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
	flagIgnore  = "ignoreRequires"
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
			"precondition does not hold is skipped, reported as skipped, and is not a pass.",
		Flags: []cli.Flag{
			&cli.PathFlag{Name: flagOut, Value: "tmp/scenes", Usage: "directory for captures, logs and the gallery index"},
			&cli.DurationFlag{Name: flagTimeout, Value: 60 * time.Second, Usage: "bound on the wait for the carrier and on each driver request"},
			&cli.IntFlag{Name: flagSettle, Value: 300, Usage: "milliseconds to settle after a step that sets no settleMs of its own"},
			&cli.BoolFlag{Name: flagDryRun, Usage: "launch and resolve every anchor, without sending input or capturing"},
			&cli.BoolFlag{Name: flagList, Usage: "list the scenes and exit"},
			&cli.BoolFlag{Name: flagIgnore, Usage: "run scenes whose preconditions do not hold instead of skipping them"},
			&cli.StringSliceFlag{Name: flagOnly, Usage: "run only scenes whose name contains this; repeatable"},
			&cli.PathFlag{Name: flagClient, Usage: "headless Rust client; default: the CPU rasterizer build, then the wgpu build"},
			&cli.PathFlag{Name: flagRoot, Usage: "repository root; default: found from the working directory"},
		},
		Action: run,
	}
}

func run(ctx *cli.Context) (err error) {
	if ctx.NArg() == 0 {
		return eh.Errorf("nothing to run: pass scene documents or directories")
	}
	docs, err := scene.CollectDocs(ctx.Args().Slice())
	if err != nil {
		return err
	}
	if only := ctx.StringSlice(flagOnly); len(only) > 0 {
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
	w := ctx.App.Writer
	if ctx.Bool(flagList) {
		for _, d := range docs {
			_, _ = fmt.Fprintf(w, "%-40s %s\n", d.Name, d.Title())
		}
		return nil
	}

	root := ctx.Path(flagRoot)
	if root == "" {
		if root, err = scene.FindRepoRoot("."); err != nil {
			return err
		}
	}
	out, err := filepath.Abs(ctx.Path(flagOut))
	if err != nil {
		return eh.Errorf("unable to resolve --"+flagOut+": %w", err)
	}
	if err = os.MkdirAll(out, 0o755); err != nil {
		return eh.Errorf("unable to create --"+flagOut+": %w", err)
	}

	var results []scene.Result
	counts := map[scene.StatusE]int{}
	for _, d := range docs {
		res := scene.RunDoc(d, scene.Options{
			OutDir:         out,
			RepoRoot:       root,
			ClientBinary:   ctx.Path(flagClient),
			Timeout:        ctx.Duration(flagTimeout),
			SettleMs:       ctx.Int(flagSettle),
			DryRun:         ctx.Bool(flagDryRun),
			IgnoreRequires: ctx.Bool(flagIgnore),
			Out:            w,
			Logger:         log.Logger,
		})
		results = append(results, res)
		counts[res.Status]++
		line := fmt.Sprintf("%-5s %-40s %5.1fs", res.Status, d.Name, res.Duration.Seconds())
		if res.Reason != "" {
			line += "  " + res.Reason
		}
		_, _ = fmt.Fprintln(w, line)
		if res.Err != nil {
			// The rendered error, fields included: what a step read and what
			// it expected are fields, and a bare message would drop them.
			_, _ = fmt.Fprintln(w, indent(scene.PlainError(res.Err)))
			_, _ = fmt.Fprintln(w, "      host log: "+filepath.Join(out, "logs", d.Name+".host.log"))
		}
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

func indent(text string) string {
	return "      " + strings.ReplaceAll(text, "\n", "\n      ")
}
