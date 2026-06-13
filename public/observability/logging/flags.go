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
	"github.com/mattn/go-isatty"
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

// consoleCborThreshold bounds the CBOR-diagnostic pretty-printer used by
// the console writer: a diagnosis shorter than this prints inline, a
// longer one falls back to a godump tree (see cborConsolePrinter). It is
// shared by every console writer so rendering stays identical across the
// primary logger and any passthrough that mirrors it.
const consoleCborThreshold = 70

// NewConsoleWriter builds the human-readable zerolog ConsoleWriter used
// for --logFormat=console. It is the single source of truth for that
// writer's configuration: every console writer in the process — notably
// the facts-log-bridge operator passthrough (thestack/cmd/imzero2) —
// must obtain its writer here rather than hand-rolling a
// zerolog.ConsoleWriter.
//
// Why a shared constructor and not a copied struct literal:
// SetupConsoleLogger installs a process-global InterfaceMarshalFunc
// (embeddAsCbor) that CBOR-embeds every value zerolog's console writer
// routes through the marshaler — which, per zerolog console.go
// writeFields, is every field that is neither a string nor a
// json.Number (bool, nil, slices, structs via .Interface). Only the
// FormatFieldValue installed here expands those embedded
// `data:application/cbor;base64,…` blobs back to a scalar. A console
// writer that omits FormatFieldValue therefore prints the raw blob for
// every bool. The marshal half and the format half are two ends of one
// codec; this constructor keeps them coupled so they cannot drift.
func NewConsoleWriter(out io.Writer, noColor bool) (cw zerolog.ConsoleWriter, err error) {
	var pp *cborConsolePrinter
	pp, err = newCborConsolePrinter(consoleCborThreshold)
	if err != nil {
		err = eh.Errorf("unable to create cbor console printer: %w", err)
		return
	}
	fv := func(i any) string {
		return formatFieldValue(i, pp)
	}
	cw = zerolog.ConsoleWriter{
		Out:                 out,
		NoColor:             noColor,
		FormatFieldValue:    fv,
		FormatErrFieldValue: fv,
		FieldsExclude:       []string{zerolog.ErrorFieldName},
		FormatExtra:         eh.ConsoleFormatErrorExtra(true),
		TimeFormat:          time.RFC3339,
	}
	return
}

// NewFormatWriter builds the operator-facing writer for `format`,
// wrapping `out`. It is the single source of truth for translating
// --logFormat into a concrete io.Writer, shared by applyWriter (the
// primary logger) and — via OperatorWriter — the facts-log-bridge
// passthrough, so both render identically and honor --logFile/--logColor.
//
// Each non-raw format writer decodes the zerolog wire and reformats it,
// which is exactly what lets the bridge reuse the same writer over its
// own wire payload. "cbor" and "default" return `out` unchanged (raw
// wire bytes); "cbor" additionally requires the binary_log build tag,
// which the caller verifies (applyWriter calls checkZeroLogCborBuild).
// The console writer carries the CBOR field-expansion formatter
// (NewConsoleWriter); installing its process-global marshalers is the
// caller's job (installConsoleCborMarshalers).
func NewFormatWriter(format string, out io.Writer, noColor bool) (w io.Writer, err error) {
	switch format {
	case "console":
		return NewConsoleWriter(out, noColor)
	case "diag":
		return NewCborDiagLogger(asStringWriter(out)), nil
	case "godump":
		return NewCborGodumpLogger(out), nil
	case "json":
		l := NewJsonIndentLogger(out)
		l.Indent = ""
		l.Prefix = ""
		return l, nil
	case "json-indent":
		l := NewJsonIndentLogger(out)
		l.Indent = "  "
		l.Prefix = ""
		return l, nil
	case "cbor", "default":
		return out, nil
	default:
		// Defense-in-depth: categorial validation rejects out-of-set
		// values upstream. A reachable default signals Allowed/switch drift.
		return nil, eb.Build().Str("format", format).Errorf("unhandled log format (Allowed/switch drift)")
	}
}

// asStringWriter adapts an io.Writer to io.StringWriter for the CBOR-diag
// logger. The real destinations (*os.File) already implement it; the
// adapter only covers in-memory writers used in tests.
func asStringWriter(w io.Writer) io.StringWriter {
	if sw, ok := w.(io.StringWriter); ok {
		return sw
	}
	return stringWriterAdapter{w: w}
}

type stringWriterAdapter struct{ w io.Writer }

func (s stringWriterAdapter) WriteString(str string) (int, error) {
	return s.w.Write([]byte(str))
}

// operatorWriter is the fully-configured operator-facing writer that
// applyWriter built from --logFormat/--logFile/--logColor. It is set once
// during Apply (urfave/cli App.Before) and read once during the
// facts-log-bridge install (a subcommand Before), on the same goroutine,
// so it needs no synchronization.
var operatorWriter io.Writer

func setOperatorWriter(w io.Writer) { operatorWriter = w }

// OperatorWriter returns the writer Apply configured for operator-facing
// output: the same --logFormat, --logFile destination, and --logColor as
// the primary logger. The facts-log-bridge reuses it as its passthrough
// so the operator stream honors the full logger config instead of
// re-deriving a stderr console writer (which silently dropped --logFile
// and every non-console format). Returns nil if Apply has not run.
func OperatorWriter() io.Writer { return operatorWriter }

// installConsoleCborMarshalers wires the process-global zerolog
// marshalers the console format depends on: InterfaceMarshalFunc embeds
// any non-string/number field value as a CBOR data URI (which
// NewConsoleWriter's FormatFieldValue expands back to a scalar), and
// ErrorMarshalFunc renders boxer error chains human-readably. Safe to
// call from both SetupConsoleLogger and applyWriter.
func installConsoleCborMarshalers() (err error) {
	var cborEncMode cbor.EncMode
	cborEncMode, err = cbor.CanonicalEncOptions().EncMode()
	if err != nil {
		return eh.Errorf("unable to create cbor encoding mode: %w", err)
	}
	zerolog.ErrorMarshalFunc = eh.ErrorMarshalFuncHuman
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

func SetupConsoleLogger(w io.Writer) (err error) {
	if err = installConsoleCborMarshalers(); err != nil {
		return
	}
	var cw zerolog.ConsoleWriter
	cw, err = NewConsoleWriter(w, false)
	if err != nil {
		return
	}
	log.Logger = log.Output(cw)
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

	// LogColor is the BOXER_LOG_COLOR env-var spec. Honored by the
	// console format. An explicit value wins; otherwise color auto-detects
	// from whether stderr is a terminal, and a --logFile destination
	// always disables it (ANSI escapes in a log file are noise).
	LogColor = env.NewBool(env.Spec{
		Name:        "BOXER_LOG_COLOR",
		Default:     "true",
		Description: "colorize console log output (auto-detects TTY when unset; off for file destinations)",
		Category:    env.CategoryObservability,
		CliFlagName: "logColor",
	})
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
	LogLevel.AsCliFlag(),
	LogFormat.AsCliFlag(),
	LogColor.AsCliFlag(),
}

// Apply configures zerolog from the parsed cli.Context. Wire it as
// cli.App.Before so it runs for every invocation regardless of which
// flags the user supplied — flag-level Action closures only fire when
// the flag is explicitly set, which silently swallowed startup-info
// logging before this refactor.
//
// Order of effects: writer → global level → caller frame → correlation
// id → "application startup" record. The startup record is emitted
// only when at least one of the host/pid/args/vcs/module flags is set.
func Apply(ctx *cli.Context) (err error) {
	if err = applyWriter(ctx); err != nil {
		return
	}
	if err = applyLevel(ctx); err != nil {
		return
	}
	if ctx.Bool("logCaller") {
		log.Logger = log.Logger.With().Caller().Logger()
	}
	applyCorrelationId(ctx)
	if err = emitStartupRecord(ctx); err != nil {
		return
	}
	return
}

func applyWriter(ctx *cli.Context) (err error) {
	// Resolve format: prefer the CLI/env-bound flag value, fall back
	// to LogFormat.Get() when the flag is absent from the app. A
	// non-empty ctx.String reliably means the flag is present (no
	// allowed value is the empty string), so an empty result here
	// distinguishes "flag not on app" from "flag on app, defaulted".
	// This lets smaller mains wire Before: logging.Apply without
	// having to also list LoggingFlags. urfave/cli v2 runs
	// App.Before before flag Actions (command.go:215-226 in v2.27.7),
	// so the categorial Action inside env.CategorialStringVar.AsCliFlag
	// fires too late to catch invalid CLI values before Apply runs —
	// we re-validate here.
	format := ctx.String("logFormat")
	if format == "" {
		format = LogFormat.Get()
	} else if !LogFormat.IsAllowed(format) {
		return eb.Build().Str("format", format).Strs("allowed", LogFormat.Allowed()).
			Errorf("invalid --logFormat value")
	}

	logFile := ctx.String("logFile")
	var dest io.Writer
	destIsFile := false
	if logFile == "" || logFile == "-" {
		dest = os.Stderr
	} else {
		var f *os.File
		f, err = os.OpenFile(logFile, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o666)
		if err != nil {
			return eb.Build().Str("logFile", logFile).Errorf("unable to open log file: %w", err)
		}
		dest = f
		destIsFile = true
	}

	noColor := resolveNoColor(ctx, destIsFile)

	if format == "cbor" {
		checkZeroLogCborBuild()
	}
	var w io.Writer
	w, err = NewFormatWriter(format, dest, noColor)
	if err != nil {
		return eb.Build().Str("logger", format).Errorf("unable to setup logger: %w", err)
	}
	// The console format couples to a process-global CBOR field codec
	// (see NewConsoleWriter / installConsoleCborMarshalers); install the
	// marshalers before any event is emitted.
	if format == "console" {
		if err = installConsoleCborMarshalers(); err != nil {
			return eb.Build().Str("logger", format).Errorf("unable to setup logger: %w", err)
		}
	}
	log.Logger = log.Output(w)
	// Remember the resolved writer so the facts-log-bridge passthrough
	// (OperatorWriter) renders exactly like the primary logger — same
	// format, --logFile destination, and color.
	setOperatorWriter(w)
	return
}

// resolveNoColor decides whether the console writer should suppress ANSI
// color. An explicit --logColor / BOXER_LOG_COLOR wins; otherwise color
// auto-detects from whether stderr is a terminal. A file destination
// always forces no-color — color escapes in a log file are noise — which
// also keeps piped/redirected output clean by default.
func resolveNoColor(ctx *cli.Context, destIsFile bool) bool {
	if destIsFile {
		return true
	}
	if ctx.IsSet("logColor") {
		return !ctx.Bool("logColor")
	}
	return !isatty.IsTerminal(os.Stderr.Fd())
}

func applyLevel(ctx *cli.Context) (err error) {
	// See applyWriter for the empty-string fallback rationale.
	level := ctx.String("logLevel")
	if level == "" {
		level = LogLevel.Get()
	} else if !LogLevel.IsAllowed(level) {
		return eb.Build().Str("level", level).Strs("allowed", LogLevel.Allowed()).
			Errorf("invalid --logLevel value")
	}
	var lvl zerolog.Level
	switch level {
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
	default:
		return eb.Build().Str("level", level).Errorf("unhandled log level (Allowed/switch drift)")
	}
	zerolog.SetGlobalLevel(lvl)
	return
}

func applyCorrelationId(ctx *cli.Context) {
	if !ctx.IsSet("logCorrelationId") {
		return
	}
	runInstanceId := ctx.String("logCorrelationId")
	if runInstanceId == "" {
		runInstanceId = gonanoid.Must(21)
	}
	// correlationId is added as a top-level field on the persistent
	// logger context rather than nested under "boxer". Before the
	// refactor it shared the "boxer" namespace with the startup
	// record, which produced a duplicate "boxer" JSON key when both
	// fired in the same invocation (jsontext rejects duplicates).
	log.Logger = log.Logger.With().Str("correlationId", runInstanceId).Logger()
}

func emitStartupRecord(ctx *cli.Context) (err error) {
	o := zerolog.Dict()
	any := false
	if ctx.Bool("logOsHostOnStart") {
		var host string
		host, err = os.Hostname()
		if err != nil {
			return eb.Build().Errorf("unable to use -logOsHostOnStart: %w", err)
		}
		o = o.Str("host", host)
		any = true
	}
	if ctx.Bool("logOsPidOnStart") {
		o = o.Int("pid", os.Getpid())
		any = true
	}
	if ctx.Bool("logOsArgsOnStart") {
		o = o.Strs("args", os.Args)
		any = true
	}
	if ctx.Bool("logVcsRevisionOnStart") {
		var rev string
		var mod bool
		rev, mod, err = vcs.GetVcsRevision()
		if err != nil {
			return eb.Build().Errorf("unable to use -logVcsRevisionOnStart: %w", err)
		}
		o = o.Str("vcsRevision", rev).Bool("vcsModified", mod)
		any = true
	}
	if ctx.Bool("logModuleInfoOnStart") {
		mod := vcs.ModuleInfo()
		if mod == vcs.NoBuildInfo {
			return eb.Build().Errorf("unable to use -logModuleInfoOnStart: no build information available")
		}
		o = o.Str("moduleInfo", mod)
		any = true
	}
	if any {
		log.Info().Dict("boxer", zerolog.Dict().Dict("startup", o)).Msg("application startup")
	}
	return
}
