// Package filenaming lints Go file and package names against ADR-0048.
//
// Four rules, numbered as that ADR numbers them:
//
//	N1  a Go file's basename is snake_case lowercase
//	N2  a file carrying the generated-code header ends .out.go or .gen.go (ADR-0048 N2+N3)
//	N6  a package name is lowercase with no underscores
//	N7  a file directly under apps/<n>/ is prefixed <n>_
//
// It is a port of scripts/ci/file-naming.sh, moved into Go so the composite
// gate can carry it to consuming repositories through the module pin instead of
// each of them copying the shell (ADR-0179). Behaviour is preserved, including
// the finding syntax, the baseline semantics, and the stale-baseline check.
//
// Not to be confused with the sibling package gov/filename, which *renames*
// files to snake_case. This one only reports.
package filenaming

import (
	"bufio"
	"io/fs"
	"iter"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/stergiotis/boxer/public/gov/pathfilter"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// RuleE identifies which of the four rules a finding violates.
type RuleE uint8

const (
	RuleN1 RuleE = 1
	// RuleN2 covers ADR-0048 N2 (checked-in generated file, .out.go) and N3
	// (build-time generated file, .gen.go) together: from a file's content
	// alone, a checker cannot tell which of the two a generator intended, so
	// the check is "one of the two", not "specifically N2".
	RuleN2 RuleE = 2
	RuleN6 RuleE = 3
	RuleN7 RuleE = 4
)

var AllRules = []RuleE{RuleN1, RuleN2, RuleN6, RuleN7}

func (inst RuleE) String() (s string) {
	switch inst {
	case RuleN1:
		s = "N1"
	case RuleN2:
		s = "N2"
	case RuleN6:
		s = "N6"
	case RuleN7:
		s = "N7"
	default:
		s = "unknown"
	}
	return
}

// kind is the finding's line prefix, kept identical to the shell version so an
// existing baseline file keeps matching.
func (inst RuleE) kind() (s string) {
	switch inst {
	case RuleN1:
		s = "file"
	case RuleN2:
		s = "generated-suffix"
	case RuleN6:
		s = "package"
	case RuleN7:
		s = "app-prefix"
	default:
		s = "unknown"
	}
	return
}

// Finding is one naming violation. Its String form is the baseline line format:
// "<kind>:<path>".
type Finding struct {
	Rule RuleE
	Path string
}

func (inst Finding) String() (s string) {
	return inst.Rule.kind() + ":" + inst.Path
}

// Config parameterises [Check].
//
// The zero value audits ./public and ./apps, which is boxer's layout; a
// consumer with a different one sets Roots.
type Config struct {
	// Dir is the repository root. Empty means the working directory.
	Dir string
	// Roots are the trees to audit, relative to Dir. Empty means
	// {"public", "apps"}.
	Roots []string
	// AppsDir is the tree N7 applies to, relative to Dir. Empty means
	// "apps"; N7 is skipped when it does not exist.
	AppsDir string
	// Exclude are repository-specific exclusion patterns, on top of the
	// generated-file and attic filters this package always applies. See
	// [github.com/stergiotis/boxer/public/gov/pathfilter] for the syntax.
	Exclude []string
}

func (inst Config) dir() (s string) {
	if inst.Dir == "" {
		return "."
	}
	return inst.Dir
}

func (inst Config) roots() (s []string) {
	if len(inst.Roots) == 0 {
		return []string{"public", "apps"}
	}
	return inst.Roots
}

func (inst Config) appsDir() (s string) {
	if inst.AppsDir == "" {
		return "apps"
	}
	return inst.AppsDir
}

var (
	reFileBase        = regexp.MustCompile(`^[a-z][a-z0-9_]*\.go$`)
	rePackage         = regexp.MustCompile(`^[a-z][a-z0-9]*$`)
	rePackageDcl      = regexp.MustCompile(`^package ([a-zA-Z0-9_]+)`)
	reGeneratedHeader = regexp.MustCompile(`^// Code generated .* DO NOT EDIT\.$`)
)

// generatedHeaderScanLines bounds how far into a file N2 looks for the
// generated-code header — matching the `head -n 5` scripts/ci/lint.sh's gofmt
// step already uses, so a file's generated-ness reads the same way everywhere
// in the toolchain rather than each check defining it slightly differently.
const generatedHeaderScanLines = 5

// hasGeneratedHeaderE reports whether path opens with the canonical Go
// generated-code header (https://go.dev/s/generatedcode), searched within the
// first generatedHeaderScanLines lines. A header past that window is not
// judged either way, matching the gofmt step's own convention.
func hasGeneratedHeaderE(path string) (ok bool, err error) {
	f, openErr := os.Open(path)
	if openErr != nil {
		err = eb.Build().Str("path", path).Errorf("open: %w", openErr)
		return
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for i := 0; i < generatedHeaderScanLines && sc.Scan(); i++ {
		if reGeneratedHeader.MatchString(sc.Text()) {
			ok = true
			return
		}
	}
	return
}

// isAudited reports whether a discovered .go file is in scope. Generated files
// own their own layout and attic/ is dead code, so both are excluded — matching
// the filters the vet and staticcheck steps apply.
func isAudited(path string) (ok bool) {
	base := filepath.Base(path)
	if !strings.HasSuffix(base, ".go") {
		return false
	}
	if strings.HasSuffix(base, ".out.go") || strings.HasSuffix(base, ".gen.go") {
		return false
	}
	return !slices.Contains(strings.Split(filepath.ToSlash(path), "/"), "attic")
}

// goFilesE walks the configured roots and yields every audited .go file, as a
// path relative to Dir.
func goFilesE(cfg Config) (out []string, err error) {
	out = make([]string, 0, 512)
	for _, root := range cfg.roots() {
		abs := filepath.Join(cfg.dir(), root)
		if _, statErr := os.Stat(abs); statErr != nil {
			// A root a repository does not have is not an error: a
			// consumer with no apps/ tree is well-formed.
			continue
		}
		walkErr := filepath.WalkDir(abs, func(p string, d fs.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if d.IsDir() {
				if d.Name() == "attic" {
					return filepath.SkipDir
				}
				return nil
			}
			rel, relErr := filepath.Rel(cfg.dir(), p)
			if relErr != nil {
				return relErr
			}
			rel = filepath.ToSlash(rel)
			if isAudited(rel) {
				out = append(out, rel)
			}
			return nil
		})
		if walkErr != nil {
			err = eb.Build().Str("root", abs).Errorf("walk: %w", walkErr)
			return
		}
	}
	slices.Sort(out)
	return
}

// packageNameE reads the first non-external-test package declaration in dir.
//
// An external <pkg>_test package is Go-standard and exempt, so a directory
// holding only those yields "" and is skipped by the caller.
func packageNameE(dir string) (name string, err error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		err = eb.Build().Str("dir", dir).Errorf("read dir: %w", err)
		return
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") {
			names = append(names, e.Name())
		}
	}
	slices.Sort(names)

	for _, n := range names {
		f, openErr := os.Open(filepath.Join(dir, n))
		if openErr != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			m := rePackageDcl.FindStringSubmatch(sc.Text())
			if m == nil {
				continue
			}
			if strings.HasSuffix(m[1], "_test") {
				break
			}
			_ = f.Close()
			return m[1], nil
		}
		_ = f.Close()
	}
	return "", nil
}

// n7Exempt reports whether a file directly under apps/<app>/ is excused from
// the prefix rule.
func n7Exempt(base string, app string) (ok bool) {
	switch base {
	case "main.go", "doc.go", "app_register.go", app + ".go":
		return true
	}
	return strings.HasSuffix(base, "_test.go")
}

// CheckE yields every naming violation under cfg, sorted and deduplicated.
func CheckE(cfg Config) (findings []Finding, err error) {
	var files []string
	files, err = goFilesE(cfg)
	if err != nil {
		return
	}
	findings = make([]Finding, 0, 16)
	excl := pathfilter.NewMatcher(cfg.Exclude)
	if !excl.IsEmpty() {
		kept := make([]string, 0, len(files))
		for _, f := range files {
			if !excl.Match(f) {
				kept = append(kept, f)
			}
		}
		files = kept
	}

	// N1 — file basenames.
	dirs := make([]string, 0, len(files))
	for _, f := range files {
		if !reFileBase.MatchString(filepath.Base(f)) {
			findings = append(findings, Finding{Rule: RuleN1, Path: f})
		}
		dirs = append(dirs, filepath.Dir(f))
	}

	// N6 — package names, once per directory.
	slices.Sort(dirs)
	dirs = slices.Compact(dirs)
	for _, d := range dirs {
		var pkg string
		pkg, err = packageNameE(filepath.Join(cfg.dir(), d))
		if err != nil {
			return
		}
		if pkg == "" {
			continue
		}
		if !rePackage.MatchString(pkg) {
			findings = append(findings, Finding{Rule: RuleN6, Path: d})
		}
	}

	// N7 — app prefix, for files directly under apps/<n>/ only.
	appsRel := cfg.appsDir()
	prefix := appsRel + "/"
	for _, f := range files {
		if !strings.HasPrefix(f, prefix) {
			continue
		}
		rest := strings.TrimPrefix(f, prefix)
		app, base, found := strings.Cut(rest, "/")
		if !found || strings.Contains(base, "/") {
			// Not directly under apps/<n>/.
			continue
		}
		if n7Exempt(base, app) {
			continue
		}
		if !strings.HasPrefix(base, app+"_") {
			findings = append(findings, Finding{Rule: RuleN7, Path: f})
		}
	}

	// N2/N3 — a file whose content carries the generated-code header must be
	// named .out.go or .gen.go. files is already every .go file minus attic
	// minus anything already so named (isAudited), so it is exactly the
	// candidate set: a correctly-suffixed file cannot violate this by
	// construction, and is never rescanned here.
	//
	// _test.go is a structural exemption, not a grandfather: Go's test
	// discovery requires the literal _test.go suffix, so a generated test
	// file cannot trade that for .out.go/.gen.go and still run under `go
	// test`.
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		var generated bool
		generated, err = hasGeneratedHeaderE(filepath.Join(cfg.dir(), f))
		if err != nil {
			return
		}
		if generated {
			findings = append(findings, Finding{Rule: RuleN2, Path: f})
		}
	}

	slices.SortFunc(findings, func(a, b Finding) int { return strings.Compare(a.String(), b.String()) })
	findings = slices.CompactFunc(findings, func(a, b Finding) bool { return a.String() == b.String() })
	return
}

// LoadBaselineE reads a baseline file: one "<kind>:<path>" line per
// grandfathered violation, with '#' comments and blank lines ignored.
//
// A missing file is not an error — it means an empty baseline, which is how a
// repository that has never needed one behaves.
func LoadBaselineE(path string) (out []string, err error) {
	out = make([]string, 0, 16)
	if path == "" {
		return
	}
	raw, readErr := os.ReadFile(path)
	if readErr != nil {
		if os.IsNotExist(readErr) {
			return
		}
		err = eb.Build().Str("path", path).Errorf("read baseline: %w", readErr)
		return
	}
	for line := range strings.SplitSeq(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	slices.Sort(out)
	out = slices.Compact(out)
	return
}

// Diff splits findings against a baseline.
//
// New are violations the baseline does not excuse. Stale are baseline entries
// that no longer reproduce — reported so the baseline cannot quietly accumulate
// entries that are keeping nothing alive.
func Diff(findings []Finding, baseline []string) (fresh []string, stale []string) {
	have := make(map[string]struct{}, len(findings))
	for _, f := range findings {
		have[f.String()] = struct{}{}
	}
	excused := make(map[string]struct{}, len(baseline))
	for _, b := range baseline {
		excused[b] = struct{}{}
	}

	fresh = make([]string, 0, len(findings))
	for _, f := range findings {
		if _, ok := excused[f.String()]; !ok {
			fresh = append(fresh, f.String())
		}
	}
	stale = make([]string, 0, len(baseline))
	for _, b := range baseline {
		if _, ok := have[b]; !ok {
			stale = append(stale, b)
		}
	}
	return
}

// All yields findings in order, for callers that prefer iteration.
func All(findings []Finding) iter.Seq[Finding] {
	return func(yield func(Finding) bool) {
		for _, f := range findings {
			if !yield(f) {
				return
			}
		}
	}
}
