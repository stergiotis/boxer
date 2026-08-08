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
	"github.com/stergiotis/boxer/public/gov/filenaming"
	"github.com/stergiotis/boxer/public/gov/pathfilter"
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
	linter.SetExclude(cfg.Exclude)

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
	// An excluded path is excluded from every step, not just the two that
	// walk the filesystem. A repository that puts research spikes outside its
	// audited tree says so once, in one place.
	excl := pathfilter.NewMatcher(cfg.Exclude)
	if !excl.IsEmpty() {
		module := cfg.Module
		if module == "" {
			module = moduleOf(cfg.root())
		}
		kept := make([]dev.EntryPointAudit, 0, len(audits))
		for _, a := range audits {
			rel := strings.TrimPrefix(a.PkgPath, module+"/")
			if !excl.Match(rel) {
				kept = append(kept, a)
			}
		}
		audits = kept
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

// StepFileNaming applies the ADR-0048 Go file and package naming rules.
//
// Hard gate after baseline subtraction, in both directions: a new violation
// fails, and so does a baseline entry that no longer reproduces. The second
// half is what stops the baseline quietly accumulating dead exemptions.
type StepFileNaming struct{}

var _ StepI = (*StepFileNaming)(nil)

func NewStepFileNaming() (inst *StepFileNaming) { return &StepFileNaming{} }

func (inst *StepFileNaming) Name() (s string) { return "file-naming" }

func (inst *StepFileNaming) Run(ctx context.Context, cfg Config, w io.Writer) (status StatusE, err error) {
	var findings []filenaming.Finding
	findings, err = filenaming.CheckE(filenaming.Config{
		Dir:     cfg.root(),
		Roots:   cfg.NamingRoots,
		Exclude: cfg.Exclude,
	})
	if err != nil {
		return
	}

	baselinePath := cfg.NamingBaseline
	if baselinePath != "" && !filepath.IsAbs(baselinePath) {
		baselinePath = filepath.Join(cfg.root(), baselinePath)
	}
	var baseline []string
	baseline, err = filenaming.LoadBaselineE(baselinePath)
	if err != nil {
		return
	}

	fresh, stale := filenaming.Diff(findings, baseline)

	status = StatusPass
	if len(fresh) > 0 {
		if baselinePath == "" {
			_, _ = fmt.Fprintln(w, "naming violations:")
		} else {
			_, _ = fmt.Fprintf(w, "new naming violations (not in %s):\n", baselinePath)
		}
		for _, x := range fresh {
			_, _ = fmt.Fprintf(w, "  %s\n", x)
		}
		status = StatusFail
	}
	if len(stale) > 0 {
		_, _ = fmt.Fprintf(w, "baseline entries no longer violating (remove these lines from %s):\n", baselinePath)
		for _, x := range stale {
			_, _ = fmt.Fprintf(w, "  %s\n", x)
		}
		status = StatusFail
	}
	return
}

// moduleOf reads the module path from go.mod under dir, best effort: an
// unreadable go.mod simply means package paths are matched unmodified.
func moduleOf(dir string) (module string) {
	raw, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return ""
	}
	for line := range strings.SplitSeq(string(raw), "\n") {
		rest, found := strings.CutPrefix(strings.TrimSpace(line), "module ")
		if found {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}
