package logging

import (
	"github.com/fxamacker/cbor/v2"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/yassinebenaid/godump"
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
				dumper := godump.Dumper{
					Indentation:             "  ",
					ShowPrimitiveNamedTypes: false,
					HidePrivateFields:       false,
					Theme:                   godump.DefaultTheme,
				}
				var cbordiagmode cbor.DiagMode
				var cborencmode cbor.EncMode
				var err error
				if false {
					cborencmode, err = cbor.CanonicalEncOptions().EncMode()
					if err != nil {
						log.Warn().Err(err).Msg("unable to create cbor encoder, skipping")
						err = nil
					}
					cbordiagmode, err = cbor.DiagOptions{
						ByteStringEncoding:      0,
						ByteStringHexWhitespace: false,
						ByteStringText:          false,
						ByteStringEmbeddedCBOR:  false,
						CBORSequence:            false,
						FloatPrecisionIndicator: false,
						MaxNestedLevels:         0,
						MaxArrayElements:        0,
						MaxMapPairs:             0,
					}.DiagMode()
				}
				zerolog.InterfaceMarshalFunc = func(v any) ([]byte, error) {
					if cborencmode != nil && cbordiagmode != nil {
						c, err := cborencmode.Marshal(v)
						if err == nil {
							s, err = cbordiagmode.Diagnose(c)
							if err == nil {
								return []byte(s), nil
							}
						}
					}
					return []byte(dumper.Sprintln(v)), nil
					//return json.MarshalIndent(v, "", "  ")
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
