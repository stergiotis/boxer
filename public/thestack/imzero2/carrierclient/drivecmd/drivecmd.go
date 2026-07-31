// Package drivecmd is the `imzero2 drive` subcommand: replay an interaction
// trace against a running headless host (ADR-0154 §SD6).
//
// It is the executor for a seam ADR-0127's own replayer cannot reach — the
// headless build has no `egui_inspection` port, by design — while sharing that
// ADR's step vocabulary, so one trace runs on either.
package drivecmd

import (
	"os"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/thestack/imzero2/carrierclient"
	"github.com/urfave/cli/v2"
)

const (
	flagURL      = "url"
	flagTrace    = "trace"
	flagTimeout  = "timeout"
	flagSettle   = "settle"
	flagDryRun   = "dryRun"
	flagDumpTree = "dumpTree"
	flagLabel    = "label"
)

// NewCommand builds the `drive` subcommand.
func NewCommand() *cli.Command {
	return &cli.Command{
		Name:  "drive",
		Usage: "replay an interaction trace against a running headless imzero2 host",
		Description: "Connects to a headless host's remote-access carrier (ADR-0024) and\n" +
			"replays a JSON Lines trace against it: resolve a widget by name or id\n" +
			"from the accessibility tree, actuate it without coordinates, capture a\n" +
			"PNG when the trace says so.\n\n" +
			"The host must be running with IMZERO2_HEADLESS_LISTEN set, and — for\n" +
			"captures — IMZERO2_HEADLESS_DUMP_DIR, which is the directory it writes\n" +
			"them into (the trace names the file, the host owns the directory).\n\n" +
			"Only the ACTIVE connection is honoured (ADR-0086): the first connection\n" +
			"to a host is admitted active, so pointing this at a host someone is\n" +
			"already watching in a browser does nothing.\n\n" +
			"   imzero2 drive --url ws://127.0.0.1:8089/ --trace tour.jsonl\n" +
			"   imzero2 drive --trace tour.jsonl --dryRun     # resolve only, change nothing\n" +
			"   imzero2 drive --dumpTree                      # print the tree and exit\n\n" +
			"Trace steps, one JSON object per line ('#' comments and blank lines\n" +
			"ignored). The vocabulary is ADR-0127 SD2's:\n\n" +
			"   {\"do\":\"click\",   \"name\":\"Panes\"}\n" +
			"   {\"do\":\"click\",   \"x\":640, \"y\":400}          # painter-only fallback\n" +
			"   {\"do\":\"type\",    \"role\":\"text_input\", \"text\":\"SELECT 1\"}\n" +
			"   {\"do\":\"key\",     \"text\":\"Enter\", \"modifiers\":2}\n" +
			"   {\"do\":\"wait\",    \"name\":\"Run\"}\n" +
			"   {\"do\":\"capture\", \"text\":\"panes-open\", \"settleMs\":400}\n" +
			"   {\"do\":\"note\",    \"comment\":\"why this step exists\"}",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  flagURL,
				Value: "ws://127.0.0.1:8089/",
				Usage: "carrier WebSocket URL of the headless host",
			},
			&cli.PathFlag{
				Name:  flagTrace,
				Usage: "trace file to replay (JSON Lines); '-' reads stdin",
			},
			&cli.DurationFlag{
				Name:  flagTimeout,
				Value: 10 * time.Second,
				Usage: "per-request timeout",
			},
			&cli.IntFlag{
				Name:  flagSettle,
				Value: 250,
				Usage: "milliseconds to settle after a step that sets no settleMs of its own",
			},
			&cli.BoolFlag{
				Name:  flagDryRun,
				Usage: "resolve every anchor and report, without sending input or capturing",
			},
			&cli.BoolFlag{
				Name:  flagDumpTree,
				Usage: "print the accessibility tree and exit (no trace needed)",
			},
			&cli.StringFlag{
				Name:  flagLabel,
				Value: "imzero2-drive",
				Usage: "label shown in the host's viewer roster",
			},
		},
		Action: run,
	}
}

func run(ctx *cli.Context) (err error) {
	tracePath := ctx.Path(flagTrace)
	dumpTree := ctx.Bool(flagDumpTree)
	if tracePath == "" && !dumpTree {
		return eh.Errorf("nothing to do: pass --" + flagTrace + " or --" + flagDumpTree)
	}

	// Parse before connecting: a malformed trace should not first take the
	// active session away from whoever is holding it.
	var steps []carrierclient.Step
	if tracePath != "" {
		var f *os.File
		if tracePath == "-" {
			f = os.Stdin
		} else {
			if f, err = os.Open(tracePath); err != nil {
				return eb.Build().Str("path", tracePath).Errorf("unable to open trace: %w", err)
			}
			defer func() { _ = f.Close() }()
		}
		if steps, err = carrierclient.ParseTrace(f); err != nil {
			return err
		}
		log.Info().Int("steps", len(steps)).Str("trace", tracePath).Msg("trace loaded")
	}

	c, err := carrierclient.Connect(carrierclient.Config{
		URL:         ctx.String(flagURL),
		Label:       ctx.String(flagLabel),
		DialTimeout: ctx.Duration(flagTimeout),
		Logger:      log.Logger,
	})
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()
	h := c.Hello()
	log.Info().
		Uint32("widthPx", h.GetWidthPx()).
		Uint32("heightPx", h.GetHeightPx()).
		Float32("pixelsPerPoint", h.GetPixelsPerPoint()).
		Msg("connected to the headless host")

	if dumpTree {
		return printTree(c, ctx.Duration(flagTimeout))
	}
	return carrierclient.RunTrace(c, steps, carrierclient.RunOptions{
		Timeout:  ctx.Duration(flagTimeout),
		SettleMs: ctx.Int(flagSettle),
		DryRun:   ctx.Bool(flagDryRun),
		Logger:   log.Logger,
	})
}

// printTree writes the named nodes of one snapshot, which is how a trace author
// finds out what to anchor on.
func printTree(c *carrierclient.Client, timeout time.Duration) (err error) {
	snap, err := c.Tree(timeout)
	if err != nil {
		return err
	}
	log.Info().Int("nodes", len(snap.GetNodes())).Uint64("pass", snap.GetPass()).
		Msg("accessibility tree")
	for _, n := range snap.GetNodes() {
		// Unnamed, valueless nodes are layout containers; a locator cannot
		// address them and listing them buries the ones it can.
		if n.GetName() == "" && n.GetValue() == "" {
			continue
		}
		x, y := carrierclient.Center(n)
		log.Info().
			Uint64("id", n.GetId()).
			Str("role", n.GetRole()).
			Str("name", n.GetName()).
			Str("value", n.GetValue()).
			Float32("cx", x).Float32("cy", y).
			Uint32("flags", n.GetFlags()).
			Msg("node")
	}
	return nil
}
