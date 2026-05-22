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
	"github.com/stergiotis/boxer/public/config/env"
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
	zerolog.ErrorMarshalFunc = eh.ErrorMarshalFuncHuman
	log.Logger = log.Output(zerolog.ConsoleWriter{
		Out: w,
		FormatFieldValue: func(i interface{}) string {
			return formatFieldValue(i, pp)
		},
		FormatErrFieldValue: func(i interface{}) string {
			return formatFieldValue(i, pp)
		},
		FieldsExclude: []string{zerolog.ErrorFieldName},
		FormatExtra:   eh.ConsoleFormatErrorExtra(true),
		TimeFormat:    time.RFC3339})
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

// Environment variable declarations for the logging subsystem.
// Registered with the boxer-wide registry per ADR-0009.
var (
	// LogFile is the BOXER_LOG_FILE env-var spec.
	LogFile = env.NewString(env.Spec{
		Name:        "BOXER_LOG_FILE",
		Description: "path to the log file; empty or \"-\" routes to stderr",
		Category:    env.CategoryObservability,
		CliFlagName: "logFile",
	})

	// LogCaller is the BOXER_LOG_CALLER env-var spec.
	LogCaller = env.NewBool(env.Spec{
		Name:        "BOXER_LOG_CALLER",
		Description: "include caller file:line in log records",
		Category:    env.CategoryObservability,
		CliFlagName: "logCaller",
	})

	// LogOsHostOnStart is the BOXER_LOG_OS_HOST_ON_START env-var spec.
	LogOsHostOnStart = env.NewBool(env.Spec{
		Name:        "BOXER_LOG_OS_HOST_ON_START",
		Description: "log the host name on application startup",
		Category:    env.CategoryObservability,
		CliFlagName: "logOsHostOnStart",
	})

	// LogOsArgsOnStart is the BOXER_LOG_OS_ARGS_ON_START env-var spec.
	LogOsArgsOnStart = env.NewBool(env.Spec{
		Name:        "BOXER_LOG_OS_ARGS_ON_START",
		Description: "log os.Args on application startup",
		Category:    env.CategoryObservability,
		CliFlagName: "logOsArgsOnStart",
	})

	// LogOsPidOnStart is the BOXER_LOG_OS_PID_ON_START env-var spec.
	LogOsPidOnStart = env.NewBool(env.Spec{
		Name:        "BOXER_LOG_OS_PID_ON_START",
		Description: "log the OS process id on application startup",
		Category:    env.CategoryObservability,
		CliFlagName: "logOsPidOnStart",
	})

	// LogVcsRevisionOnStart is the BOXER_LOG_VCS_REVISION_ON_START env-var spec.
	LogVcsRevisionOnStart = env.NewBool(env.Spec{
		Name:        "BOXER_LOG_VCS_REVISION_ON_START",
		Description: "log the VCS revision on application startup",
		Category:    env.CategoryObservability,
		CliFlagName: "logVcsRevisionOnStart",
	})

	// LogModuleInfoOnStart is the BOXER_LOG_MODULE_INFO_ON_START env-var spec.
	// Renamed from BOXER_LOG_MODULE_INFO_IN_START in passing per ADR-0009 §6
	// (the four sibling flags use _ON_START).
	LogModuleInfoOnStart = env.NewBool(env.Spec{
		Name:        "BOXER_LOG_MODULE_INFO_ON_START",
		Description: "log the Go module info on application startup",
		Category:    env.CategoryObservability,
		CliFlagName: "logModuleInfoOnStart",
	})

	// LogCorrelationId is the BOXER_LOG_CORRELATION_ID env-var spec.
	LogCorrelationId = env.NewString(env.Spec{
		Name:        "BOXER_LOG_CORRELATION_ID",
		Description: "correlation id for log records; empty seeds a nanoid(21)",
		Category:    env.CategoryObservability,
		CliFlagName: "logCorrelationId",
	})

	// LogLevel is the BOXER_LOG_LEVEL env-var spec.
	LogLevel = env.NewCategorialString(env.Spec{
		Name:        "BOXER_LOG_LEVEL",
		Default:     "info",
		Description: "zerolog level",
		Category:    env.CategoryObservability,
		CliFlagName: "logLevel",
	}, []string{"trace", "debug", "info", "warn", "error", "fatal", "panic"})

	// LogFormat is the BOXER_LOG_FORMAT env-var spec.
	LogFormat = env.NewCategorialString(env.Spec{
		Name:        "BOXER_LOG_FORMAT",
		Default:     "json",
		Description: "log output format",
		Category:    env.CategoryObservability,
		CliFlagName: "logFormat",
	}, []string{"default", "console", "diag", "godump", "json", "json-indent", "cbor"})
)

var LoggingFlags = []cli.Flag{
	LogFile.AsCliFlag(),
	LogCaller.AsCliFlag(),
	LogOsHostOnStart.AsCliFlag(),
	LogOsArgsOnStart.AsCliFlag(),
	LogOsPidOnStart.AsCliFlag(),
	LogVcsRevisionOnStart.AsCliFlag(),
	LogModuleInfoOnStart.AsCliFlag(),
	LogCorrelationId.AsCliFlag(),
	LogLevel.AsCliFlag(env.WithStringAction(func(context *cli.Context, s string) error {
		var lvl zerolog.Level
		switch s {
		case "trace":
			lvl = zerolog.TraceLevel
		case "debug":
			lvl = zerolog.DebugLevel
		case "info":
			lvl = zerolog.InfoLevel
		case "warn":
			lvl = zerolog.WarnLevel
		case "error":
			lvl = zerolog.ErrorLevel
		case "fatal":
			lvl = zerolog.FatalLevel
		case "panic":
			lvl = zerolog.PanicLevel
		}
		zerolog.SetGlobalLevel(lvl)
		return nil
	})),
	LogFormat.AsCliFlag(env.WithStringAction(func(context *cli.Context, s string) error {
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
		case "diag":
			err = SetupCborDiagLogger(w)
		case "godump":
			err = SetupGoDumpLogger(w)
		case "json":
			err = SetupJsonLogger(w)
		case "json-indent":
			err = SetupJsonIndentLogger(w)
		case "cbor":
			checkZeroLogCborBuild()
			log.Logger = log.Output(w)
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
	})),
}
