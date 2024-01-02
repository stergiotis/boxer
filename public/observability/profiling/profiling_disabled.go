//go:build !boxer_enable_profiling

package profiling

import (
	"github.com/stergiotis/boxer/public/observability/eh"
	cli "github.com/urfave/cli/v2"
)

func ProfilingHandleExit(context *cli.Context) {
}

var errorDisabled = eh.Errorf("unable to activate profiling: program needs to be compiled with boxer_enable_profiling build tag")
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
			return errorDisabled
		},
	},
	&cli.StringFlag{
		Name:     "httpServerAddress",
		Category: "profiling",
		Action: func(context *cli.Context, s string) error {
			return errorDisabled
		},
	},
}
