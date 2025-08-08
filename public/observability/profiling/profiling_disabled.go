//go:build !boxer_enable_profiling

package profiling

import (
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/urfave/cli/v2"
)

func ProfilingHandleExit(context *cli.Context) {
}

var errDisabled = eh.Errorf("unable to activate profiling: program needs to be compiled with boxer_enable_profiling build tag")

func cpuProfileFileAction(context *cli.Context, s string) error {
	return errDisabled
}
func httpServerAddressAction(context *cli.Context, s string) error {
	return errDisabled
}
