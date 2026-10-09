package stevedoredemo

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v3"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/streaming/stevedore"
	"github.com/stergiotis/boxer/public/streaming/stevedore/drive"
)

func newRunCommand() *cli.Command {
	return &cli.Command{
		Name:      "run",
		Usage:     "drive the line-splitting handler over a tree, or over stdin lines, with no framework: one JSON line per item",
		ArgsUsage: "<dir>… | -",
		Flags: append([]cli.Flag{
			&cli.IntFlag{Name: "workers", Value: 1, Usage: "requests handled at once"},
			&cli.IntFlag{Name: "flush-every", Value: 64, Usage: "requests landed between flushes"},
			&cli.DurationFlag{Name: "deadline", Value: 20 * time.Second, Usage: "bound on one request's handling"},
			&cli.Int64Flag{Name: "max-body", Value: 16 * 1024 * 1024, Usage: "bound on a request body, bytes"},
			&cli.Uint64Flag{Name: "skip", Usage: "requests to leave out from the start: a checkpoint a previous run printed"},
		}, deadLetterFlags()...),
		Action: runRun,
	}
}

func runRun(cliCtx context.Context, cmd *cli.Command) (err error) {
	if cmd.NArg() == 0 {
		return eh.Errorf("name a directory to walk, or - for stdin lines")
	}
	dead, closeDead, err := openDeadLetters(cliCtx, cmd)
	if err != nil {
		return
	}
	defer closeDead()

	cfg := drive.Config{
		Workers:    cmd.Int("workers"),
		FlushEvery: cmd.Int("flush-every"),
		Deadline:   cmd.Duration("deadline"),
		MaxBody:    int(cmd.Int64("max-body")),
		Skip:       cmd.Uint64("skip"),
		Logger:     &log.Logger,
	}
	ctx, stop := signal.NotifyContext(cliCtx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	handler := stevedore.HandlerFunc(splitLines)

	for _, arg := range cmd.Args().Slice() {
		var src drive.SourceI
		if arg == "-" {
			src = drive.Lines{R: os.Stdin, Origin: "stdin"}
		} else {
			src = drive.Tree{FS: os.DirFS(arg), Hint: "text", MaxBody: cmd.Int64("max-body")}
		}
		res, rerr := drive.Run(ctx, cfg, src, handler, printSink{}, dead)
		log.Info().Str("source", src.Name()).Uint64("requests", res.Requests).Uint64("items", res.Items).
			Uint64("deadLetters", res.DeadLetters).Uint64("done", res.Done).Msg("stevedoredemo run ends")
		if rerr != nil {
			return eh.Errorf("run: %w", rerr)
		}
	}
	return
}
