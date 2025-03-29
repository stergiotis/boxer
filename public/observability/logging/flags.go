package logging

import (
	"fmt"
	"github.com/fxamacker/cbor/v2"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
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
	for _, ts := range tags {
		u := strings.Split(ts, ",")
		for _, t := range u {
			if strings.Trim(t, " \t\n\r") == "binary_log" {
				return
			}
		}
	}
	panic("cbor logging unavailable, build did not include the `binary_log` build tag")
}

type cborConsolePrinter struct {
	decMode   cbor.DecMode
	diagMode  cbor.DiagMode
	dumper    *godump.Dumper
	treshhold int
}

func newCborConsolePrinter(threshold int) (inst *cborConsolePrinter, err error) {
	var diagMode cbor.DiagMode
	var decMode cbor.DecMode
	decMode, err = cbor.DecOptions{}.DecMode()
	if err != nil {
		err = eh.Errorf("unable to create cbor decoding mode: %w", err)
		return
	}
	diagMode, err = cbor.DiagOptions{
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
	if err != nil {
		err = eh.Errorf("unable to create cbor diag mode: %w", err)
		return
	}
	dumper := &godump.Dumper{
		Indentation:             "  ",
		ShowPrimitiveNamedTypes: false,
		HidePrivateFields:       false,
		Theme:                   godump.DefaultTheme,
	}
	inst = &cborConsolePrinter{
		decMode:   decMode,
		diagMode:  diagMode,
		dumper:    dumper,
		treshhold: threshold,
	}
	return
}

func (inst *cborConsolePrinter) prettyPrintToString(cbor []byte) (s string, err error) {
	s, err = inst.diagMode.Diagnose(cbor)
	if err != nil {
		return
	}
	if inst.treshhold > 0 && len(s) > inst.treshhold {
		var a any
		err = inst.decMode.Unmarshal(cbor, &a)
		if err != nil {
			return
		}
		s = inst.dumper.Sprint(a)
		return
	}
	return
}
func formatFieldValue(i any, pp *cborConsolePrinter) (s string) {
	var err error
	switch it := i.(type) {
	case []byte:
		ss := string(it)
		if containsEmbeddedCborJson(ss) {
			var b []byte
			b, err = unpackEmbeddedCborJson(ss)
			if err != nil {
				break
			}
			s, err = pp.prettyPrintToString(b)
			if err != nil {
				break
			}
			return s
		}
		break
	case string:
		if containsEmbeddedCbor(it) {
			var b []byte
			b, err = unpackEmbeddedCbor(it)
			if err != nil {
				return it
			}
			s, err = pp.prettyPrintToString(b)
			if err != nil {
				return it
			}
			return s
		}
		return it
	}

	return fmt.Sprintf("%s", i)
}

var LoggingFlags = []cli.Flag{
	&cli.StringFlag{
		Name:     "logFile",
		Category: "logging",
		EnvVars:  []string{"BOXER_LOG_FILE"},
		Value:    "",
	},
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
		Usage:       "one of the following: \"default\", \"console\", \"diag\", \"godump\", \"json-indent\", \"cbor\"",
		Category:    "logging",
		DefaultText: "json",
		EnvVars:     []string{"BOXER_LOG_FORMAT"},
		Action: func(context *cli.Context, s string) error {
			logFile := context.String("logFile")
			var w *os.File
			if logFile == "" || logFile == "-" {
				w = os.Stderr
			} else {
				var err error
				w, err = os.OpenFile(logFile, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o666)
				if err != nil {
					return eb.Build().Str("logFile", logFile).Errorf("unable to open log file: %w", err)
				}
			}

			switch s {
			case "default":
				break
			case "console":
				var cborEncMode cbor.EncMode
				var err error
				cborEncMode, err = cbor.CanonicalEncOptions().EncMode()
				if err != nil {
					return eh.Errorf("unable to create cbor encoding mode: %w", err)
				}
				const threshold = 70
				var pp *cborConsolePrinter
				pp, err = newCborConsolePrinter(threshold)
				if err != nil {
					return eh.Errorf("unable to create cbor console printer: %w", err)
				}
				log.Logger = log.Output(zerolog.ConsoleWriter{
					Out: w,
					FormatFieldValue: func(i interface{}) string {
						return formatFieldValue(i, pp)
					},
					FormatErrFieldValue: func(i interface{}) string {
						return formatFieldValue(i, pp)
					},
					TimeFormat: time.RFC3339})
				zerolog.InterfaceMarshalFunc = func(v any) (b []byte, err error) {
					var se string
					se, err = embeddAsCbor(cborEncMode, v)
					if err != nil {
						return nil, err
					}
					return []byte(se), nil
				}
				break
			case "diag":
				log.Logger = log.Output(NewCborDiagLogger(w))
				break
			case "godump":
				log.Logger = log.Output(NewCborGodumpLogger(w))
				break
			case "json-indent":
				log.Logger = log.Output(NewJsonIndentLogger(w))
				break
			case "cbor":
				checkZeroLogCborBuild()
				log.Logger = log.Output(w)
				break
			default:
				return eh.Errorf("unhandled log format %s", s)
			}
			return nil
		},
	},
}
