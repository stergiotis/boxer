package coverage

import (
	"path"
	"syscall"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/urfave/cli/v2"
)
import "runtime/coverage"

var CoverageFlags = []cli.Flag{
	&cli.StringFlag{
		Name:     "coverageTrapDir",
		Category: "coverage",
		Usage:    "Will write cover information to the dir whenever the program receives SIGUSR1. Use -cover -covermode=atomic to compile the program.",
		Action: func(context *cli.Context, s string) error {
			if s != "" {
				// TODO there does not seem to be another method to find out if the program is compiled with -cover -covermode=count
				err := coverage.ClearCounters()
				if err != nil {
					return eh.Errorf("program does not seem to be to be built with -cover -covermode=count flags (no cover support)")
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
