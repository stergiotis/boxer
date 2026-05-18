//go:build llm_generated_opus47

package codelint

import (
	"os"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/urfave/cli/v2"
	"golang.org/x/tools/go/packages"
)

func NewCliCommand() *cli.Command {
	return &cli.Command{
		Name:      "codelint",
		Usage:     "Lint Go code against CODINGSTANDARDS.md (CS-prefixed rules)",
		ArgsUsage: "[pattern ...]   package patterns to load; defaults to './public/...'",
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
			&cli.StringFlag{
				Name:  "tags",
				Value: "",
				Usage: "comma-separated build tags (passed to packages.Load)",
			},
		},
		Action: codelintAction,
	}
}

func codelintAction(ctx *cli.Context) (err error) {
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

	patterns := ctx.Args().Slice()
	if len(patterns) == 0 {
		patterns = []string{"./public/..."}
	}

	var tags []string
	if t := ctx.String("tags"); t != "" {
		for _, x := range strings.Split(t, ",") {
			x = strings.TrimSpace(x)
			if x != "" {
				tags = append(tags, x)
			}
		}
	}

	var pkgs []*packages.Package
	pkgs, err = LoadPackagesE(LoadConfig{Ctx: ctx.Context, BuildTags: tags}, patterns...)
	if err != nil {
		return
	}

	linter := NewLinter()
	linter.Register(NewRuleCS001())
	linter.Register(NewRuleCS002())
	linter.Register(NewRuleCS003())
	linter.Register(NewRuleCS004())
	linter.Register(NewRuleCS005())
	linter.Register(NewRuleCS006())
	linter.Register(NewRuleCS007())
	linter.Register(NewRuleCS008())
	linter.Register(NewRuleCS009())
	linter.Register(NewRuleCS010())

	var rep ReporterI
	rep, err = NewReporterE(format, os.Stdout)
	if err != nil {
		return
	}

	var errCount, warnCount, infoCount uint64
	for f, runErr := range linter.Run(pkgs) {
		if runErr != nil {
			err = eh.Errorf("codelint: %w", runErr)
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
		Strs("patterns", patterns).
		Msg("codelint complete")

	if errCount > 0 {
		err = eb.Build().
			Uint64("errors", errCount).
			Errorf("codelint: error-severity findings present")
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
		err = eb.Build().Str("format", s).Errorf("codelint: unknown output format")
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
		err = eb.Build().Str("severity", s).Errorf("codelint: unknown severity")
	}
	return
}
