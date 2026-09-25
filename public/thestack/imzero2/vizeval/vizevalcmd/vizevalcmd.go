// Package vizevalcmd is the `imzero2 vizeval` subcommand (ADR-0257, proposed,
// §SD9): list what a scenario admits, and score candidates over it.
//
// It must run from the imzero2 binary: the scene launcher starts the host as a
// child of its own executable.
package vizevalcmd

import (
	"bufio"
	"encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/thestack/imzero2/scene"
	"github.com/stergiotis/boxer/public/thestack/imzero2/vizeval"
	"github.com/stergiotis/boxer/public/thestack/imzero2/vizeval/harness"
	"github.com/urfave/cli/v2"
)

const (
	flagOut        = "out"
	flagCandidates = "candidates"
	flagTimeout    = "timeout"
	flagClient     = "clientBinary"
	flagRoot       = "repoRoot"
)

// NewCommand builds the `vizeval` subcommand.
func NewCommand() *cli.Command {
	return &cli.Command{
		Name:  "vizeval",
		Usage: "score renderings of a scenario's leeway batch in play's Experiments pane (ADR-0257)",
		Subcommands: []*cli.Command{
			{
				Name:      "space",
				Usage:     "print the sinks each scenario admits, with their row caps and option spaces, as JSON lines",
				ArgsUsage: "<scenario" + vizeval.ScenarioSuffix + " | dir>…",
				Action:    runSpace,
			},
			{
				Name:      "score",
				Usage:     "render and measure candidates over each scenario",
				ArgsUsage: "<scenario" + vizeval.ScenarioSuffix + " | dir>…",
				Description: "Each candidate is one headless launch of play with the scenario's dataset,\n" +
					"the Experiments pane full-panel and seeded with the candidate. Without\n" +
					"--" + flagCandidates + ", every admitted sink is scored at its defaults.\n\n" +
					"   imzero2 vizeval score apps/play/vizeval\n" +
					"   imzero2 vizeval score --candidates c.jsonl apps/play/vizeval/10_hosts.vizeval.md\n\n" +
					"A candidates file holds one {\"sink\":…,\"options\":{…}} per line; a scenario\n" +
					"that does not admit a candidate's sink records it as inadmissible.\n" +
					"Output: <out>/<scenario>/index.md per scenario, a directory per candidate,\n" +
					"and every scorecard appended to <out>/scorecards.jsonl.",
				Flags: []cli.Flag{
					&cli.PathFlag{Name: flagOut, Value: "tmp/vizeval", Usage: "output directory"},
					&cli.PathFlag{Name: flagCandidates, Usage: "JSONL file of candidates; default: each admitted sink at its defaults"},
					&cli.DurationFlag{Name: flagTimeout, Value: 60 * time.Second, Usage: "bound on the wait for the carrier and each driver request"},
					&cli.PathFlag{Name: flagClient, Usage: "headless Rust client; default: the scene launcher's choice"},
					&cli.PathFlag{Name: flagRoot, Usage: "checkout holding rust/imzero2; default: found from the working directory"},
				},
				Action: runScore,
			},
		},
	}
}

func collect(args []string) (scs []*vizeval.Scenario, err error) {
	if len(args) == 0 {
		return nil, eh.Errorf("nothing to do: pass scenario documents or directories")
	}
	for _, p := range args {
		st, e := os.Stat(p)
		if e != nil {
			return nil, eb.Build().Str("path", p).Errorf("unable to stat: %w", e)
		}
		paths := []string{p}
		if st.IsDir() {
			if paths, err = filepath.Glob(filepath.Join(p, "*"+vizeval.ScenarioSuffix)); err != nil {
				return nil, eh.Errorf("unable to list scenarios: %w", err)
			}
		}
		for _, sp := range paths {
			sc, e := vizeval.ReadScenario(sp)
			if e != nil {
				return nil, e
			}
			scs = append(scs, sc)
		}
	}
	if len(scs) == 0 {
		return nil, eh.Errorf("no scenario documents found")
	}
	return scs, nil
}

func runSpace(ctx *cli.Context) (err error) {
	scs, err := collect(ctx.Args().Slice())
	if err != nil {
		return err
	}
	w := ctx.App.Writer
	for _, sc := range scs {
		for _, id := range sc.Spec.Sinks {
			spec, _ := vizeval.SinkByID(id)
			opts := make([]map[string]any, 0, len(spec.Space))
			for _, o := range spec.Space {
				e := map[string]any{"name": o.Name, "kind": o.Kind.String(), "default": o.Default, "description": o.Description}
				if o.Kind == vizeval.OptionKindEnum {
					e["choices"] = o.Choices
				}
				if o.Kind == vizeval.OptionKindInt || o.Kind == vizeval.OptionKindFloat {
					e["min"], e["max"] = o.Min, o.Max
				}
				opts = append(opts, e)
			}
			b, e := json.Marshal(map[string]any{
				"scenario": sc.Name, "sink": spec.ID, "rowCap": spec.RowCap, "options": opts,
			}, json.Deterministic(true))
			if e != nil {
				return eh.Errorf("unable to encode a space: %w", e)
			}
			_, _ = fmt.Fprintln(w, string(b))
		}
	}
	return nil
}

func readCandidates(path string) (cands []vizeval.Candidate, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, eb.Build().Str("path", path).Errorf("unable to open candidates: %w", err)
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	line := 0
	for sc.Scan() {
		line++
		text := strings.TrimSpace(sc.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		c, e := vizeval.UnmarshalCandidate([]byte(text))
		if e != nil {
			return nil, eb.Build().Int("line", line).Errorf("invalid candidate: %w", e)
		}
		cands = append(cands, c)
	}
	if err = sc.Err(); err != nil {
		return nil, eh.Errorf("unable to read candidates: %w", err)
	}
	return cands, nil
}

func runScore(ctx *cli.Context) (err error) {
	scs, err := collect(ctx.Args().Slice())
	if err != nil {
		return err
	}
	var fixed []vizeval.Candidate
	if p := ctx.Path(flagCandidates); p != "" {
		if fixed, err = readCandidates(p); err != nil {
			return err
		}
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
	opts := harness.Options{
		OutDir: out, RepoRoot: root, ClientBinary: ctx.Path(flagClient),
		Timeout: ctx.Duration(flagTimeout), Logger: log.Logger,
	}
	w := ctx.App.Writer
	failed := 0
	for _, sc := range scs {
		cands := fixed
		if cands == nil {
			if cands, err = harness.DefaultCandidates(sc); err != nil {
				return err
			}
		}
		cards, _, e := harness.Score(sc, cands, opts)
		if e != nil {
			_, _ = fmt.Fprintf(w, "%-32s error: %s\n", sc.Name, scene.PlainError(e))
			failed++
			continue
		}
		for _, c := range cards {
			line := fmt.Sprintf("%-12s %-32s %s", c.Status, sc.Name, c.Candidate.Canonical())
			if c.Reason != "" {
				line += "  " + c.Reason
			}
			_, _ = fmt.Fprintln(w, line)
			if c.Status == harness.StatusFailed {
				failed++
			}
		}
		_, _ = fmt.Fprintln(w, "  → "+filepath.Join(out, sc.Name, "index.md"))
	}
	if failed > 0 {
		return eb.Build().Int("failed", failed).Errorf("some candidates or scenarios failed")
	}
	return nil
}
