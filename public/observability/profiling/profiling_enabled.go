//go:build boxer_enable_profiling

package profiling

import (
	"github.com/stergiotis/boxer/public/observability/eh"
	"net/http"
	_ "net/http/pprof"
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
	&cli.StringFlag{
		Name:     "httpServerAddress",
		Category: "profiling",
		Action: func(context *cli.Context, s string) error {
			go func() {
				err := http.ListenAndServe(s, nil)
				if err != nil {
					log.Error().Str("address", s).Err(err).Msg("unable to start http server, ignoring error")
				}
			}()
			return nil
		},
	},
}
