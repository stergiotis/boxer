// Package drivecmd is the `imzero2 drive` subcommand: run interaction steps
// against a running headless host and print its accessibility tree
// (ADR-0154 §SD6). Steps come from a trace file, from --step, or both; the
// tree view is how a caller with no trace yet finds out what to anchor on.
//
// It is the executor for a seam ADR-0127's own replayer cannot reach — the
// headless build has no `egui_inspection` port, by design — while sharing that
// ADR's step vocabulary, so one trace runs on either.
package drivecmd

import (
	"math"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/thestack/imzero2/carrierclient"
	"github.com/urfave/cli/v2"
)

const (
	flagURL        = "url"
	flagTrace      = "trace"
	flagTimeout    = "timeout"
	flagSettle     = "settle"
	flagDryRun     = "dryRun"
	flagDumpTree   = "dumpTree"
	flagLabel      = "label"
	flagStep       = "step"
	flagTreeText   = "treeText"
	flagTreeRole   = "treeRole"
	flagTreeUnder  = "treeUnder"
	flagTreeLimit  = "treeLimit"
	flagTreeHidden = "treeHidden"
	flagTreeFormat = "treeFormat"
)

// NewCommand builds the `drive` subcommand.
func NewCommand() *cli.Command {
	return &cli.Command{
		Name:  "drive",
		Usage: "run interaction steps against a running headless imzero2 host, and print its accessibility tree",
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
			"   imzero2 drive --dumpTree --treeText rows      # what is on screen, filtered\n" +
			"   imzero2 drive --step '{\"do\":\"click\",\"name\":\"Run\"}' --dumpTree\n\n" +
			"--step takes one trace line and may repeat; the steps run after those of\n" +
			"--trace. --dumpTree prints once the steps are done, so one connection\n" +
			"acts and then reports what the action left on screen. The tree goes to\n" +
			"stdout, one node per line; the log goes to stderr:\n\n" +
			"   button \"Run\" #1234 @640,412 [disabled]\n" +
			"   label =\"3 rows\" #5678 @700,440\n\n" +
			"role, name, =value, #id as a step's \"id\" takes it, @ the bounds centre\n" +
			"in logical points. Indentation is nesting among the printed nodes.\n\n" +
			"Trace steps, one JSON object per line ('#' comments and blank lines\n" +
			"ignored). The vocabulary is ADR-0127 SD2's:\n\n" +
			"   {\"do\":\"click\",   \"name\":\"Panes\"}\n" +
			"   {\"do\":\"click\",   \"x\":640, \"y\":400}          # painter-only fallback\n" +
			"   {\"do\":\"type\",    \"role\":\"text_input\", \"text\":\"SELECT 1\"}\n" +
			"   {\"do\":\"key\",     \"text\":\"Enter\", \"modifiers\":2}\n" +
			"   {\"do\":\"click\",   \"name\":\"Row 3\", \"button\":\"secondary\"}\n" +
			"   {\"do\":\"wait\",    \"name\":\"Run\"}\n" +
			"   {\"do\":\"tree\",    \"text\":\"rows\", \"role\":\"label\"}  # print matches mid-trace\n" +
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
			&cli.StringSliceFlag{
				Name:  flagStep,
				Usage: "one trace step as JSON; repeatable, run in order after --" + flagTrace,
			},
			&cli.BoolFlag{
				Name:  flagDumpTree,
				Usage: "print the accessibility tree to stdout after the steps (no trace needed)",
			},
			&cli.StringFlag{
				Name:  flagTreeText,
				Usage: "--" + flagDumpTree + ": keep nodes whose name or value contains this, ignoring case",
			},
			&cli.StringFlag{
				Name:  flagTreeRole,
				Usage: "--" + flagDumpTree + ": keep nodes of this role (button, check_box, text_input, label…)",
			},
			&cli.Uint64Flag{
				Name:  flagTreeUnder,
				Usage: "--" + flagDumpTree + ": keep only this node id and its descendants",
			},
			&cli.IntFlag{
				Name:  flagTreeLimit,
				Value: carrierclient.DefaultTreeLimit,
				Usage: "--" + flagDumpTree + ": print at most this many nodes",
			},
			&cli.StringFlag{
				Name:  flagTreeFormat,
				Value: treeFormatLines,
				Usage: "--" + flagDumpTree + ": '" + treeFormatLines + "' for a reader, '" + treeFormatJSONL + "' for a program (one object per node, nothing clipped)",
			},
			&cli.BoolFlag{
				Name:  flagTreeHidden,
				Usage: "--" + flagDumpTree + ": include nodes that are laid out but not on screen",
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

const (
	treeFormatLines = "lines"
	treeFormatJSONL = "jsonl"
)

func run(ctx *cli.Context) (err error) {
	treeFormat := ctx.String(flagTreeFormat)
	if treeFormat != treeFormatLines && treeFormat != treeFormatJSONL {
		return eb.Build().Str("format", treeFormat).Errorf("unknown --" + flagTreeFormat)
	}
	tracePath := ctx.Path(flagTrace)
	dumpTree := ctx.Bool(flagDumpTree)
	inline := ctx.StringSlice(flagStep)
	if tracePath == "" && len(inline) == 0 && !dumpTree {
		return eh.Errorf("nothing to do: pass --" + flagTrace + ", --" + flagStep + " or --" + flagDumpTree)
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
	if len(inline) > 0 {
		// The flag library splits a slice flag's value on commas, which a JSON
		// object is full of. Joining the pieces with commas again restores
		// every value exactly, and leaves them separated by commas — the
		// inside of a JSON array.
		var more []carrierclient.Step
		if more, err = carrierclient.ParseSteps([]byte("[" + strings.Join(inline, ",") + "]")); err != nil {
			return eb.Build().Errorf("unable to parse --"+flagStep+": %w", err)
		}
		steps = append(steps, more...)
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

	err = carrierclient.RunTrace(c, steps, carrierclient.RunOptions{
		Timeout:  ctx.Duration(flagTimeout),
		SettleMs: ctx.Int(flagSettle),
		DryRun:   ctx.Bool(flagDryRun),
		Out:      ctx.App.Writer,
		Logger:   log.Logger,
	})
	if err != nil || !dumpTree {
		return err
	}
	// After the steps rather than instead of them: the last step has settled
	// by now, so this is what the run left on screen, read over the connection
	// that made it so.
	snap, err := c.Tree(ctx.Duration(flagTimeout))
	if err != nil {
		return err
	}
	limit := ctx.Int(flagTreeLimit)
	if treeFormat == treeFormatJSONL && !ctx.IsSet(flagTreeLimit) {
		// The lines format says so in its header when the limit cut the list;
		// JSONL has no header, and a program reading a silently shortened list
		// would take it for the scene. Unbounded unless the caller bounds it.
		limit = math.MaxInt
	}
	view := carrierclient.SelectNodes(snap, carrierclient.TreeFilter{
		Under:  ctx.Uint64(flagTreeUnder),
		Text:   ctx.String(flagTreeText),
		Role:   ctx.String(flagTreeRole),
		Hidden: ctx.Bool(flagTreeHidden),
		Limit:  limit,
	})
	if treeFormat == treeFormatJSONL {
		return carrierclient.WriteTreeJSONL(ctx.App.Writer, view)
	}
	return carrierclient.WriteTree(ctx.App.Writer, view)
}
