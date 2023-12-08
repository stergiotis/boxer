package logging

import (
	"encoding/json"
	"github.com/stergiotis/boxer/public/observability/eh"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	cli "github.com/urfave/cli/v2"
)

func getBuildTags(info *debug.BuildInfo) []string {
	for _, v := range info.Settings {
		if v.Key == "-tags" {
			return strings.Split(v.Value, " ")
		}
	}
	return []string{}
}

func checkZeroLogCborBuild() {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		panic("unable to read build info: can not verify that cbor logging is available")
	}
	tags := getBuildTags(info)
	for _, t := range tags {
		if t == "binary_log" {
			return
		}
	}
	panic("cbor logging unavailable, build did not include the `binary_log` build tag")
}

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
				checkZeroLogCborBuild()
				log.Logger = log.Output(os.Stderr)
				break
			default:
				return eh.Errorf("unhandled log format %s", s)
			}
			return nil
		},
	},
}
