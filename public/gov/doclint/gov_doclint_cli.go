package doclint

import (
	"os"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/urfave/cli/v2"
)

func NewCliCommand() *cli.Command {
	return &cli.Command{
		Name:      "doclint",
		Usage:     "Lint Markdown documentation against doc/DOCUMENTATION_STANDARD.md",
		ArgsUsage: "[path ...]   roots to walk; defaults to '.'",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "format",
				Value: "human",
				Usage: "output format: human | json",
			},
			&cli.StringFlag{
				Name:  "min-severity",
				Value: "warn",
				Usage: "minimum severity to emit: info | warn | error",
			},
		},
		Action: doclintAction,
	}
}

func doclintAction(ctx *cli.Context) (err error) {
	var format FormatE
	format, err = ParseFormatE(ctx.String("format"))
	if err != nil {
		return
	}
	var minSev FindingSeverityE
	minSev, err = ParseSeverityE(ctx.String("min-severity"))
	if err != nil {
		return
	}

	roots := ctx.Args().Slice()
	if len(roots) == 0 {
		roots = []string{"."}
	}

	linter := NewLinter()
	linter.Register(NewRuleDL001())
	linter.Register(NewRuleDL005())

	var rep ReporterI
	rep, err = NewReporterE(format, os.Stdout)
	if err != nil {
		return
	}

	var errCount uint64
	var warnCount uint64
	var infoCount uint64
	for f, runErr := range linter.Run(roots) {
		if runErr != nil {
			err = eh.Errorf("doclint: %w", runErr)
			return
		}
		if f.Severity < minSev {
			continue
		}
		rep.Add(f)
		switch f.Severity {
		case FindingSeverityError:
			errCount++
		case FindingSeverityWarn:
			warnCount++
		case FindingSeverityInfo:
			infoCount++
		}
	}

	err = rep.FinishE()
	if err != nil {
		return
	}

	log.Info().
		Uint64("errors", errCount).
		Uint64("warns", warnCount).
		Uint64("info", infoCount).
		Strs("roots", roots).
		Msg("doclint complete")

	if errCount > 0 {
		err = eb.Build().
			Uint64("errors", errCount).
			Errorf("doclint: error-severity findings present")
	}
	return
}

func ParseFormatE(s string) (f FormatE, err error) {
	switch strings.ToLower(s) {
	case "human", "":
		f = FormatHuman
	case "json":
		f = FormatJson
	default:
		err = eb.Build().Str("format", s).Errorf("doclint: unknown output format")
	}
	return
}

func ParseSeverityE(s string) (sev FindingSeverityE, err error) {
	switch strings.ToLower(s) {
	case "info":
		sev = FindingSeverityInfo
	case "warn", "warning":
		sev = FindingSeverityWarn
	case "error", "err":
		sev = FindingSeverityError
	default:
		err = eb.Build().Str("severity", s).Errorf("doclint: unknown severity")
	}
	return
}
