package logging

import (
	"encoding/json"
	"github.com/stergiotis/boxer/public/observability/eh"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	cli "github.com/urfave/cli/v2"
)

var LoggingFlags = []cli.Flag{
	&cli.StringFlag{
		Name:        "logLevel",
		Category:    "logging",
		DefaultText: "info",
		EnvVars:     []string{"BOXER_LOG_LEVEL"},
		Action: func(context *cli.Context, s string) error {
			var lvl zerolog.Level
			switch strings.ToLower(s) {
			case "trace":
				lvl = zerolog.TraceLevel
				break
			case "debug":
				lvl = zerolog.DebugLevel
				break
			case "info":
				lvl = zerolog.InfoLevel
				break
			case "warn":
				lvl = zerolog.WarnLevel
				break
			case "error":
				lvl = zerolog.ErrorLevel
				break
			case "fatal":
				lvl = zerolog.FatalLevel
				break
			case "panic":
				lvl = zerolog.PanicLevel
				break
			default:
				return eh.Errorf("unhandled log level %s", s)
			}
			zerolog.SetGlobalLevel(lvl)
			return nil
		},
	},
	&cli.StringFlag{
		Name:        "logFormat",
		Category:    "logging",
		DefaultText: "json",
		EnvVars:     []string{"BOXER_LOG_FORMAT"},
		Action: func(context *cli.Context, s string) error {
			switch s {
			case "console":
				log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339})
				zerolog.InterfaceMarshalFunc = func(v any) ([]byte, error) {
					return json.MarshalIndent(v, "", "  ")
				}
				break
			case "diag":
				log.Logger = log.Output(NewCborDiagLogger(os.Stderr))
				break
			case "spew":
				log.Logger = log.Output(NewCborSpewLogger(os.Stderr))
				break
			case "json":
				log.Logger = log.Output(NewJsonIndentLogger(os.Stderr))
				break
			case "cbor":
				log.Logger = log.Output(os.Stderr)
				break
			default:
				return eh.Errorf("unhandled log format %s", s)
			}
			return nil
		},
	},
}
