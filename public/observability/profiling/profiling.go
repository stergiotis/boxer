package profiling

import (
	"github.com/stergiotis/boxer/public/observability/eh"
	"os"
	"runtime/pprof"

	"github.com/rs/zerolog/log"
	cli "github.com/urfave/cli/v2"
)

func ProfilingHandleExit(context *cli.Context) {
	if context.IsSet("cpuProfileFile") {
		pprof.StopCPUProfile()
	}
}

var ProfilingFlags = []cli.Flag{
	&cli.StringFlag{
		Name:        "cpuProfileFile",
		Category:    "profiling",
		DefaultText: "",
		FilePath:    "",
		Usage:       "",
		Required:    false,
		Hidden:      false,
		HasBeenSet:  false,
		Value:       "",
		Action: func(context *cli.Context, s string) error {
			f, err := os.Create(s)
			if err != nil {
				return eh.Errorf("unable to create cpu profiling file %q: %w", s, err)
			}
			log.Info().Str("file", s).Msg("started cpu profiling")
			err = pprof.StartCPUProfile(f)
			if err != nil {
				return eh.Errorf("unable to start cpu profiling: %w", err)
			}
			return nil
		},
	},
}
