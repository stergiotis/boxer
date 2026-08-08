package gate

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/stergiotis/boxer/public/dev"
	"github.com/stergiotis/boxer/public/gov/buildtags"
	"github.com/stergiotis/boxer/public/gov/codelint"
	"github.com/stergiotis/boxer/public/gov/doclint"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"golang.org/x/tools/go/packages"
)

// resolveTags returns the build tags for a run: the explicit set when Config
// carries one, otherwise the parsed contents of the tag manifest.
func resolveTags(cfg Config) (tags []string, err error) {
	if len(cfg.Tags) > 0 {
		return cfg.Tags, nil
	}
	p := filepath.Join(cfg.root(), cfg.tagsFile())
	var raw []byte
	raw, err = os.ReadFile(p)
	if err != nil {
		err = eb.Build().Str("file", p).Errorf("read tags file: %w", err)
		return
	}
	tags = buildtags.ParseTags(string(raw))
	return
}

// StepBuildTags verifies the repository's tag manifest against the contract
// boxer publishes through its module pin.
type StepBuildTags struct{}

var _ StepI = (*StepBuildTags)(nil)

func NewStepBuildTags() (inst *StepBuildTags) { return &StepBuildTags{} }

func (inst *StepBuildTags) Name() (s string) { return "buildtags" }

func (inst *StepBuildTags) Run(ctx context.Context, cfg Config, w io.Writer) (status StatusE, err error) {
	p := filepath.Join(cfg.root(), cfg.tagsFile())
	var raw []byte
	raw, err = os.ReadFile(p)
	if err != nil {
		err = eb.Build().Str("file", p).Errorf("read tags file: %w", err)
		return
	}

	status = StatusPass
	for f := range buildtags.Check(buildtags.ParseTags(string(raw))) {
		_, _ = fmt.Fprintf(w, "%s:  %s\n", p, f.Message())
		status = StatusFail
	}
	return
}

// StepDoclint runs the published Markdown rule set.
//
// Error-severity findings fail; warnings are printed and do not. That split is
// doclint's own calibration, mirrored here rather than reinvented.
type StepDoclint struct{}

var _ StepI = (*StepDoclint)(nil)

func NewStepDoclint() (inst *StepDoclint) { return &StepDoclint{} }

func (inst *StepDoclint) Name() (s string) { return "doclint" }

func (inst *StepDoclint) Run(ctx context.Context, cfg Config, w io.Writer) (status StatusE, err error) {
	linter := doclint.NewDefaultLinter()

	status = StatusPass
	for f, runErr := range linter.Run(cfg.docRoots()) {
		if runErr != nil {
			err = eb.Build().Strs("roots", cfg.docRoots()).Errorf("doclint walk: %w", runErr)
			return
		}
		if f.Severity < doclint.FindingSeverityWarn {
			continue
		}
		_, _ = fmt.Fprintf(w, "%s:%d:%d  %s  %s  %s\n",
			f.Path, f.Line, f.Col, f.RuleId, f.Severity.String(), f.Message)
		if f.Severity == doclint.FindingSeverityError {
			status = StatusFail
		} else if status == StatusPass {
			status = StatusWarn
		}
	}
	return
}

// StepCodelint runs the published CS rule set over the repository's Go code.
//
// Warn-only: the rules are calibrated per-rule and promoted to error severity
// individually as in-tree fallout reaches zero, so the gate reports them
// without failing. When a rule graduates, this step graduates with it.
type StepCodelint struct{}

var _ StepI = (*StepCodelint)(nil)

func NewStepCodelint() (inst *StepCodelint) { return &StepCodelint{} }

func (inst *StepCodelint) Name() (s string) { return "codelint" }

func (inst *StepCodelint) Run(ctx context.Context, cfg Config, w io.Writer) (status StatusE, err error) {
	var tags []string
	tags, err = resolveTags(cfg)
	if err != nil {
		return
	}

	var pkgs []*packages.Package
	pkgs, err = codelint.LoadPackagesE(codelint.LoadConfig{
		Ctx:       ctx,
		BuildTags: tags,
		Dir:       cfg.root(),
	}, cfg.codePatterns()...)
	if err != nil {
		return
	}

	linter := codelint.NewDefaultLinter()
	status = StatusPass
	for f, runErr := range linter.Run(pkgs) {
		if runErr != nil {
			err = eb.Build().Strs("patterns", cfg.codePatterns()).Errorf("codelint run: %w", runErr)
			return
		}
		if f.Severity < codelint.FindingSeverityWarn {
			continue
		}
		_, _ = fmt.Fprintf(w, "%s:%d:%d  %s  %s  %s\n",
			f.Path, f.Line, f.Col, f.RuleId, f.Severity.String(), f.Message)
		if f.Severity == codelint.FindingSeverityError {
			status = StatusFail
		} else if status == StatusPass {
			status = StatusWarn
		}
	}
	return
}

// StepEntryPoints audits every `package main` against the entry-point standard.
//
// Hard gate after baseline subtraction: a new non-conformant main fails, a
// grandfathered one is reported and does not.
type StepEntryPoints struct{}

var _ StepI = (*StepEntryPoints)(nil)

func NewStepEntryPoints() (inst *StepEntryPoints) { return &StepEntryPoints{} }

func (inst *StepEntryPoints) Name() (s string) { return "entry-points" }

func (inst *StepEntryPoints) Run(ctx context.Context, cfg Config, w io.Writer) (status StatusE, err error) {
	var tags []string
	tags, err = resolveTags(cfg)
	if err != nil {
		return
	}

	baseline := cfg.EntryPointsBaseline
	if baseline != "" && !filepath.IsAbs(baseline) {
		baseline = filepath.Join(cfg.root(), baseline)
	}

	var audits []dev.EntryPointAudit
	audits, err = dev.AuditEntryPointsE(ctx, dev.EntryPointsConfig{
		Root:         cfg.root(),
		BaselinePath: baseline,
		Tags:         tags,
	})
	if err != nil {
		return
	}
	if len(audits) == 0 {
		status = StatusSkip
		_, _ = fmt.Fprintln(w, "no main packages discovered")
		return
	}

	status = StatusPass
	for _, a := range audits {
		if a.Failing() {
			status = StatusFail
			break
		}
	}
	// The table is the finding: which main misses which of the three checks
	// is the whole diagnostic, so it prints whenever something is off.
	if status == StatusFail {
		dev.WriteEntryPointsTable(w, audits)
	}
	return
}

// stepNames is the published step list as names, for help text and validation.
func stepNames(steps []StepI) (out []string) {
	out = make([]string, 0, len(steps))
	for _, s := range steps {
		out = append(out, s.Name())
	}
	return
}

// ValidateStepNamesE reports names in want that no step in steps provides, so a
// typo in --steps fails loudly instead of silently running nothing.
func ValidateStepNamesE(steps []StepI, want []string) (err error) {
	have := make(map[string]struct{}, len(steps))
	for _, s := range steps {
		have[s.Name()] = struct{}{}
	}
	unknown := make([]string, 0, len(want))
	for _, n := range want {
		if _, ok := have[n]; !ok {
			unknown = append(unknown, n)
		}
	}
	if len(unknown) > 0 {
		err = eb.Build().
			Str("unknown", strings.Join(unknown, ",")).
			Str("known", strings.Join(stepNames(steps), ",")).
			Errorf("unknown gate step")
	}
	return
}
