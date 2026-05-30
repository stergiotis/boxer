package dev

import (
	"os"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/urfave/cli/v2"
)

func NewCliCommand() *cli.Command {
	return &cli.Command{
		Name: "dev",
		Subcommands: []*cli.Command{
			{
				Name: "panic",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  "message",
						Value: "default panic message",
					},
				},
				Action: func(context *cli.Context) error {
					log.Panic().Str("str", "strval").Uint64("uint64", 0xdeadbeef).Msg(context.String("message"))
					return nil
				},
			},
			newEntryPointsSubcommand(),
		},
	}
}

// newRedirectFlag builds an "override"-category StringFlag that, when set to a
// non-empty path, opens that file with openFlags and points dst (one of
// &os.Stdin/&os.Stdout/&os.Stderr) at it. what names the stream and verb the
// direction ("input from file" / "output to file") in the log and error
// messages.
func newRedirectFlag(flagName string, openFlags int, dst **os.File, what string, verb string) *cli.StringFlag {
	return &cli.StringFlag{
		Category: "override",
		Name:     flagName,
		Value:    "",
		Action: func(context *cli.Context, s string) error {
			if s != "" {
				f, err := os.OpenFile(s, openFlags, os.ModePerm)
				if err != nil {
					return eb.Build().Str(flagName, s).Errorf("unable to replace %s with %s: %w", what, verb, err)
				}
				*dst = f
				log.Info().Str(flagName, s).Msg("attaching " + what + " to file")
			}
			return nil
		},
	}
}

var IoOverrideFlags = []cli.Flag{
	newRedirectFlag("stdinFromFile", os.O_RDONLY, &os.Stdin, "os.Stdin", "input from file"),
	newRedirectFlag("stdoutToFile", os.O_WRONLY|os.O_TRUNC|os.O_CREATE, &os.Stdout, "os.Stdout", "output to file"),
	newRedirectFlag("stderrToFile", os.O_WRONLY|os.O_TRUNC|os.O_CREATE, &os.Stderr, "os.Stderr", "output to file"),
}
