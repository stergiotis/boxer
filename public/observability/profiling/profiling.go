// Package profiling wires runtime/pprof's file-based capture into a CLI:
// --pprofCpuOutputFile starts a CPU profile when the flag is parsed, and
// ProfilingHandleExit stops it — which is where runtime/pprof serialises the
// profile, so a host that forgets the exit hook writes an unreadable file.
//
// The HTTP half — a /debug/pprof listener — lives in the pprofhttp
// subpackage, which a host imports explicitly. ADR-0212 records why they are
// separate: net/http/pprof pulls net/http and the TLS tree into every binary
// that links it, and registers handlers on http.DefaultServeMux from its init.
// Keeping that out of this package is what let the boxer_enable_profiling
// build tag be retired, leaving the flags as the only gate.
package profiling

import (
	"os"
	"runtime/pprof"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	cli "github.com/urfave/cli/v2"
)

const (
	// flagNameCpuOutputFile is shared with ProfilingHandleExit, whose
	// stop path keys on IsSet of exactly this name.
	flagNameCpuOutputFile = "pprofCpuOutputFile"
)

var ProfilingFlags = []cli.Flag{
	&cli.StringFlag{
		Name:        flagNameCpuOutputFile,
		Category:    "profiling",
		DefaultText: "",
		FilePath:    "",
		Usage:       "",
		Required:    false,
		Hidden:      false,
		HasBeenSet:  false,
		Value:       "",
		Action:      cpuProfileFileAction,
	},
}

func ProfilingHandleExit(context *cli.Context) {
	if context.IsSet(flagNameCpuOutputFile) {
		pprof.StopCPUProfile()
	}
}

func cpuProfileFileAction(context *cli.Context, s string) error {
	f, err := os.Create(s)
	if err != nil {
		return eb.Build().Str("file", s).Errorf("unable to create cpu profiling file: %w", err)
	}
	log.Info().Str("file", s).Msg("started cpu profiling")
	err = pprof.StartCPUProfile(f)
	if err != nil {
		return eh.Errorf("unable to start cpu profiling: %w", err)
	}
	return nil
}
