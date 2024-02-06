package main

import (
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/fffi/compiletime"
	"github.com/stergiotis/boxer/public/observability/logging"
	"github.com/stergiotis/boxer/public/observability/ph"
	"github.com/stergiotis/boxer/public/observability/profiling"
	"github.com/stergiotis/boxer/public/observability/vcs"
	"github.com/stergiotis/boxer/public/imzero/demo"
	"github.com/stergiotis/boxer/public/imzero/nerdfont/generator"
	"github.com/urfave/cli/v2"
	"os"
)

func writeIndefArrayBegin() {
	_, _ = os.Stderr.Write([]byte{0x9f})
}
func writeIndefArrayEnd() {
	_, _ = os.Stderr.Write([]byte{0xff})
}

func main() {
	exitCode := 0
	logging.SetupZeroLog()
	//defer func() {
	//	os.Exit(exitCode)
	//}()
	var _ = exitCode
	defer ph.PanicHandler(2, nil, writeIndefArrayEnd)
	app := cli.App{
		Name:                 "imzero",
		Copyright:            "Copyright © 2023-2024 Panos Stergiotis",
		HelpName:             "",
		Usage:                "",
		UsageText:            "",
		ArgsUsage:            "",
		Version:              vcs.BuildVersionInfo(),
		Description:          "",
		DefaultCommand:       "",
		EnableBashCompletion: false,
		Flags: append(append([]cli.Flag{},
			logging.LoggingFlags...),
			profiling.ProfilingFlags...),
		Commands: []*cli.Command{
			compiletime.NewCommand(nil, nil),
			demo.NewCommand(),
			{
				Name:        "nerdfont",
				Subcommands: []*cli.Command{generator.NewCommand()},
			},
		},
		After: func(context *cli.Context) error {
			profiling.ProfilingHandleExit(context)
			return nil
		},
	}
	writeIndefArrayBegin()
	err := app.Run(os.Args)
	if err != nil {
		exitCode = 1
		log.Error().Stack().Err(err).Msg("an error occurred")
	}
}
