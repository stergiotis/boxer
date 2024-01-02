package profiling

import (
	"github.com/urfave/cli/v2"
)

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
		Action:      cpuProfileFileAction,
	},
	&cli.StringFlag{
		Name:     "httpServerAddress",
		Category: "profiling",
		Action:   httpServerAddressAction,
	},
}
