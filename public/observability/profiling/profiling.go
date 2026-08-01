package profiling

import (
	"github.com/urfave/cli/v2"
)

const (
	// flagNameCpuOutputFile is shared with ProfilingHandleExit, whose
	// stop path keys on IsSet of exactly this name.
	flagNameCpuOutputFile     = "pprofCpuOutputFile"
	flagNameHttpListenAddress = "pprofHttpListenAddress"
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
	&cli.StringFlag{
		Name:     flagNameHttpListenAddress,
		Category: "profiling",
		Action:   httpServerAddressAction,
	},
}
