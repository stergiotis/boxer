package logging

import (
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"github.com/fxamacker/cbor/v2"
	gonanoid "github.com/matoous/go-nanoid/v2"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/observability/vcs"
	"github.com/yassinebenaid/godump"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v2"
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
func SetupConsoleLogger(w io.Writer) (err error) {
	var cborEncMode cbor.EncMode
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
	return
}
func SetupCborDiagLogger(w io.StringWriter) (err error) {
	log.Logger = log.Output(NewCborDiagLogger(w))
	return
}
func SetupGoDumpLogger(w io.Writer) (err error) {
	log.Logger = log.Output(NewCborGodumpLogger(w))
	return
}
func SetupJsonLogger(w io.Writer) (err error) {
	l := NewJsonIndentLogger(w)
	l.Indent = ""
	l.Prefix = ""
	log.Logger = log.Output(l)
	return
}
func SetupJsonIndentLogger(w io.Writer) (err error) {
	l := NewJsonIndentLogger(w)
	l.Indent = "  "
	l.Prefix = ""
	log.Logger = log.Output(l)
	return
}

var LoggingFlags = []cli.Flag{
	&cli.StringFlag{
		Name:     "logFile",
		Category: "logging",
		EnvVars:  []string{"BOXER_LOG_FILE"},
		Value:    "",
	},
	&cli.BoolFlag{
		Name:     "logCaller",
		Category: "logging",
		EnvVars:  []string{"BOXER_LOG_CALLER"},
		Value:    false,
	},
	&cli.BoolFlag{
		Name:     "logOsHostOnStart",
		Category: "logging",
		EnvVars:  []string{"BOXER_LOG_OS_HOST_ON_START"},
		Value:    false,
	},
	&cli.BoolFlag{
		Name:     "logOsArgsOnStart",
		Category: "logging",
		EnvVars:  []string{"BOXER_LOG_OS_ARGS_ON_START"},
		Value:    false,
	},
	&cli.BoolFlag{
		Name:     "logOsPidOnStart",
		Category: "logging",
		EnvVars:  []string{"BOXER_LOG_OS_PID_ON_START"},
		Value:    false,
	},
	&cli.BoolFlag{
		Name:     "logVcsRevisionOnStart",
		Category: "logging",
		EnvVars:  []string{"BOXER_LOG_VCS_REVISION_ON_START"},
		Value:    false,
	},
	&cli.BoolFlag{
		Name:     "logModuleInfoOnStart",
		Category: "logging",
		EnvVars:  []string{"BOXER_LOG_MODULE_INFO_IN_START"},
		Value:    false,
	},
	&cli.StringFlag{
		Name:     "logCorrelationId",
		Usage:    "If the supplied argument is empty, a nanoid(21) will be used.",
		Category: "logging",
		EnvVars:  []string{"BOXER_LOG_CORRELATION_ID"},
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

			var err error
			switch s {
			case "default":
				break
			case "console":
				err = SetupConsoleLogger(w)
				break
			case "diag":
				err = SetupCborDiagLogger(w)
				break
			case "godump":
				err = SetupGoDumpLogger(w)
				break
			case "json":
				err = SetupJsonLogger(w)
				break
			case "json-indent":
				err = SetupJsonIndentLogger(w)
				break
			case "cbor":
				checkZeroLogCborBuild()
				log.Logger = log.Output(w)
				break
			default:
				return eh.Errorf("unhandled log format %s", s)
			}
			if context.Bool("logCaller") {
				log.Logger = log.Logger.With().Caller().Logger()
			}

			{
				d := zerolog.Dict()
				b := false

				if context.IsSet("logCorrelationId") {
					runInstanceId := context.String("logCorrelationId")
					if runInstanceId == "" {
						runInstanceId = gonanoid.Must(21)
					}
					d = d.Str("correlationId", runInstanceId)
					b = true
				}
				if b {
					log.Logger = log.Logger.With().Dict("boxer", d).Logger()
				}
			}
			{
				o := zerolog.Dict()
				b := false
				if context.Bool("logOsHostOnStart") {
					var host string
					host, err = os.Hostname()
					if err != nil {
						log.Panic().Err(err).Msg("unable to use -logOsHostOnStart: unable to get os host")
					}
					o = o.Str("host", host)
					b = true
				}
				if context.Bool("logOsPidOnStart") {
					pid := os.Getpid()
					o = o.Int("pid", pid)
					b = true
				}
				if context.Bool("logOsArgsOnStart") {
					o = o.Strs("args", os.Args)
					b = true
				}
				if context.Bool("logVcsRevisionOnStart") {
					var rev string
					var mod bool
					rev, mod, err = vcs.GetVcsRevision()
					if err != nil {
						log.Panic().Err(err).Msg("unable to use -logVcsRevisionOnStart: unable to get vcs revision")
					}
					o = o.Str("vcsRevision", rev).Bool("vcsModified", mod)
				}
				if context.Bool("logModuleInfoOnStart") {
					mod := vcs.ModuleInfo()
					if mod == vcs.NoBuildInfo {
						log.Panic().Msg("unable to use -logModuleInfoOnStartup: no build information available")
					}
					o = o.Str("moduleInfo", mod)
				}
				if b {
					log.Info().Dict("boxer", zerolog.Dict().Dict("startup", o)).Msg("application startup")
				}
			}
			if err != nil {
				return eb.Build().Str("logger", s).Errorf("unable to setup logger: %w", err)
			}
			return nil
		},
	},
}
