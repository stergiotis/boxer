package coverage

import (
	"context"
	"path"
	"syscall"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/urfave/cli/v3"
)

var CoverageFlags = []cli.Flag{
	&cli.StringFlag{
		Name:     "coverageTrapDir",
		Category: "coverage",
		Usage:    "Will write cover information to the dir whenever the program receives SIGUSR1. Use -cover -covermode=atomic to compile the program.",
		Action: func(ctx context.Context, cmd *cli.Command, s string) error {
			if s != "" {
				err := ProbeRuntimeSupport()
				if err != nil {
					return eh.Errorf("program has no runtime coverage snapshot support (build with -cover -covermode=atomic): %w", err)
				}
				sig := syscall.SIGUSR1
				collector := NewCollector()
				err = collector.SetupSignalTrap(path.Join(s, "counters"), path.Join(s, "meta"), sig)
				if err != nil {
					return eh.Errorf("unable to setup signal trap: %w", err)
				}
				log.Info().Str("directory", s).Stringer("signal", sig).Msg("successfully setup signal trap for writing cover information")
			}
			return nil
		},
	},
}
