package wasmsurvey

import (
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/packageprops"
	cli "github.com/urfave/cli/v2"
)

// cmdPathPrefix is the part of the invocation that lives above this package:
// `wasmsurvey` mounts under golang → analysis → code, each in a parent package
// that imports this one. A test here cannot walk up to them without an import
// cycle, and the whole tree is assembled inline in public/app's mainC(), so it
// is not reachable as a value either. Pinned as a string, deliberately, with
// the tail below checked against the command tree this package does own.
const cmdPathPrefix = "boxer code analysis golang wasmsurvey"

// TestSeededCommentNamesAReachablePath guards the defect that made ADR-0080's
// tooling look deleted: 410 generated files told the reader to run
// `wasmsurvey props generate`, which answers "No help topic for 'wasmsurvey'"
// because the command is four levels down. The comment is the only actionable
// line in a seeded file, so a short form there is worse than no form.
func TestSeededCommentNamesAReachablePath(t *testing.T) {
	src, err := renderPropsFile(PackageReport{Name: "x", ImportPath: "example.com/x"}, packageprops.KindUnspecified)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	got := string(src)
	if !strings.Contains(got, "`"+cmdPathPrefix+" props generate`") {
		t.Errorf("seeded comment does not name the full command path %q:\n%s", cmdPathPrefix, got)
	}
	// The failure mode is a *bare* group name reading as a top-level command.
	if strings.Contains(got, "`wasmsurvey props") {
		t.Errorf("seeded comment names `wasmsurvey props …` without its parents; that path does not resolve:\n%s", got)
	}
}

// TestHarvestHeaderNamesAReachablePath covers the same defect in the generated
// table's header, which additionally has to carry --tracked: `props drift`
// compares against the tracked scope, so a regen without it lands a table the
// gate rejects.
func TestHarvestHeaderNamesAReachablePath(t *testing.T) {
	src, err := renderHarvestGo(nil, "proptable")
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	first, _, _ := strings.Cut(string(src), "\n")
	if !strings.Contains(first, cmdPathPrefix+" props harvest --tracked --emit go") {
		t.Errorf("harvest header does not name the full regen command: %q", first)
	}
	// Go detects a generated file by a single line matching
	// `^// Code generated .* DO NOT EDIT\.$`. Wrapping it hides the marker from
	// every tool that honours the convention, so the header stays one line.
	if !strings.HasPrefix(first, "// Code generated ") || !strings.HasSuffix(first, " DO NOT EDIT.") {
		t.Errorf("header no longer matches Go's generated-file convention: %q", first)
	}
}

// The verbs the comments name must exist in the command tree this package
// owns. Re-parenting wasmsurvey is out of reach here (see cmdPathPrefix), but
// renaming the group or a verb under it is exactly what this catches.
func TestNamedVerbsExistInTheCommandTree(t *testing.T) {
	root := NewCliCommand()
	if root.Name != "wasmsurvey" {
		t.Fatalf("command renamed to %q; the seeded comment's path is now wrong", root.Name)
	}
	verbs := map[string]bool{}
	found := false
	for _, sub := range root.Subcommands {
		if sub.Name != "props" {
			continue
		}
		found = true
		for _, v := range sub.Subcommands {
			verbs[v.Name] = true
		}
	}
	if !found {
		t.Fatal("no `props` group under wasmsurvey; every seeded comment names it")
	}
	for _, verb := range []string{"generate", "harvest", "drift", "verify"} {
		if !verbs[verb] {
			t.Errorf("`props %s` is named in package documentation but absent from the command tree", verb)
		}
	}
}

// modeFlagOf returns the `mode` flag of a command's flag set, or nil.
func modeFlagOf(flags []cli.Flag) (f *cli.StringFlag) {
	for _, raw := range flags {
		if sf, ok := raw.(*cli.StringFlag); ok && sf.Name == "mode" {
			return sf
		}
	}
	return nil
}

// TestVerifyHasNoModeDefault is the structural half of the guard in
// runPropsVerify. `props verify` is the command scripts/ci/lint.sh runs, and
// its two modes differ by orders of magnitude: static reads the import graph,
// empirical shells out to `tinygo build` per candidate package. A default of
// `both` would mean an edit dropping `--mode static` from the lint step turns
// it into a TinyGo sweep whose verdict also depends on the runner's TinyGo
// version. Without a default the same edit fails before any work starts.
func TestVerifyHasNoModeDefault(t *testing.T) {
	root := NewCliCommand()
	for _, sub := range root.Subcommands {
		if sub.Name != "props" {
			continue
		}
		for _, verb := range sub.Subcommands {
			if verb.Name != "verify" {
				continue
			}
			f := modeFlagOf(verb.Flags)
			if f == nil {
				t.Fatal("`props verify` has no --mode flag; the CI step passes one")
			}
			if f.Value != "" {
				t.Errorf("--mode has default %q; it must have none, so dropping it in CI fails loudly instead of probing", f.Value)
			}
			return
		}
	}
	t.Fatal("no `props verify` command found")
}

// The exploratory commands keep their default. Requiring an explicit mode
// everywhere would be friction for no safety: they are run by hand, and
// empirical confirmation is what the bare survey exists for.
func TestBareSurveyKeepsItsModeDefault(t *testing.T) {
	f := modeFlagOf(NewCliCommand().Flags)
	if f == nil {
		t.Fatal("the bare survey has no --mode flag")
	}
	if f.Value != "both" {
		t.Errorf("bare survey --mode default is %q, want \"both\"", f.Value)
	}
}
