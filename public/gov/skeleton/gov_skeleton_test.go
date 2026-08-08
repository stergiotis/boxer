package skeleton

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/gov/buildtags"
	"github.com/stergiotis/boxer/public/gov/doclint"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testParams() (p Params) {
	return Params{
		Module:       "example.com/owner/thing",
		Name:         "thing",
		AppPackage:   "./public/app",
		RequiredTags: strings.Join(buildtags.RequiredTags, ","),
	}
}

func TestDeriveParamsE(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"),
		[]byte("module example.com/owner/thing\n\ngo 1.26.0\n"), 0o644))

	p, err := DeriveParamsE(dir)
	require.NoError(t, err)
	assert.Equal(t, "example.com/owner/thing", p.Module)
	assert.Equal(t, "thing", p.Name)
	assert.Equal(t, "./public/app", p.AppPackage)
	assert.Equal(t, strings.Join(buildtags.RequiredTags, ","), p.RequiredTags)
}

func TestDeriveParamsEReportsAMissingModuleLine(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("go 1.26.0\n"), 0o644))
	_, err := DeriveParamsE(dir)
	require.Error(t, err)
}

// A round trip must be stable: write, then check, with nothing to report. If
// this fails the tool is unusable — every consumer would be permanently red.
func TestWriteThenCheckIsClean(t *testing.T) {
	dir := t.TempDir()
	files := DefaultFiles()
	p := testParams()

	written, err := WriteE(dir, files, p)
	require.NoError(t, err)
	assert.Len(t, written, len(files))

	results, err := CheckE(dir, files, p)
	require.NoError(t, err)
	for _, r := range results {
		assert.False(t, r.Blocking(), "%s should not block right after a write (%s)", r.Path, r.Status)
	}
}

func TestCheckDetectsDriftInAGeneratedFile(t *testing.T) {
	dir := t.TempDir()
	files := DefaultFiles()
	p := testParams()
	_, err := WriteE(dir, files, p)
	require.NoError(t, err)

	victim := filepath.Join(dir, "scripts", "ci", "lint.sh")
	body, err := os.ReadFile(victim)
	require.NoError(t, err)
	edited := strings.Replace(string(body), "rc=0", "rc=0 # a local tweak", 1)
	require.NotEqual(t, string(body), edited)
	require.NoError(t, os.WriteFile(victim, []byte(edited), 0o755))

	results, err := CheckE(dir, files, p)
	require.NoError(t, err)

	var found bool
	for _, r := range results {
		if r.Path != "scripts/ci/lint.sh" {
			continue
		}
		found = true
		assert.Equal(t, StatusDrift, r.Status)
		assert.True(t, r.Blocking())
		assert.Positive(t, r.FirstDiffLine)
		assert.Contains(t, r.Got, "a local tweak")
	}
	assert.True(t, found)
}

// A file the consumer owns is theirs: edited content is never reported as
// drift, and never overwritten by a second write.
func TestSeededFilesAreNeverOverwrittenOrReconciled(t *testing.T) {
	dir := t.TempDir()
	files := DefaultFiles()
	p := testParams()
	_, err := WriteE(dir, files, p)
	require.NoError(t, err)

	own := filepath.Join(dir, "AGENTS.md")
	require.NoError(t, os.WriteFile(own, []byte("# my own router\n"), 0o644))
	tagsPath := filepath.Join(dir, "tags")
	require.NoError(t, os.WriteFile(tagsPath, []byte("goexperiment.jsonv2,thing_local\n"), 0o644))

	written, err := WriteE(dir, files, p)
	require.NoError(t, err)
	assert.NotContains(t, written, "AGENTS.md")
	assert.NotContains(t, written, "tags")

	body, err := os.ReadFile(own)
	require.NoError(t, err)
	assert.Equal(t, "# my own router\n", string(body))

	results, err := CheckE(dir, files, p)
	require.NoError(t, err)
	for _, r := range results {
		if r.Ownership == OwnershipSeeded {
			assert.Equal(t, StatusOwned, r.Status)
			assert.False(t, r.Blocking())
		}
	}
}

func TestCheckReportsAnAbsentGeneratedFileAsBlocking(t *testing.T) {
	dir := t.TempDir()
	files := DefaultFiles()
	p := testParams()
	_, err := WriteE(dir, files, p)
	require.NoError(t, err)
	require.NoError(t, os.Remove(filepath.Join(dir, "scripts", "boxer-path.sh")))

	results, err := CheckE(dir, files, p)
	require.NoError(t, err)
	for _, r := range results {
		if r.Path == "scripts/boxer-path.sh" {
			assert.Equal(t, StatusAbsent, r.Status)
			assert.True(t, r.Blocking())
		}
	}
}

// An absent file the consumer owns is a to-do for a new repository, not a
// failure — it must not block a reconciliation.
func TestAbsentSeededFileDoesNotBlock(t *testing.T) {
	dir := t.TempDir()
	files := DefaultFiles()
	p := testParams()
	results, err := CheckE(dir, files, p)
	require.NoError(t, err)
	for _, r := range results {
		if r.Ownership == OwnershipSeeded {
			assert.Equal(t, StatusAbsent, r.Status)
			assert.False(t, r.Blocking(), "%s is the consumer's to write", r.Path)
		}
	}
}

// Every emitted shell script must actually parse. This is what catches a
// template that renders into something broken.
func TestEmittedShellScriptsParse(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash unavailable")
	}
	dir := t.TempDir()
	files := DefaultFiles()
	p := testParams()
	_, err := WriteE(dir, files, p)
	require.NoError(t, err)

	var checked uint32
	for _, f := range files {
		rel, _, rErr := RenderE(f, p)
		require.NoError(t, rErr)
		if !strings.HasSuffix(rel, ".sh") {
			continue
		}
		out, sErr := exec.Command("bash", "-n", filepath.Join(dir, rel)).CombinedOutput()
		assert.NoError(t, sErr, "%s does not parse: %s", rel, string(out))
		checked++
	}
	assert.Positive(t, checked)
}

func TestGeneratedFilesCarryTheMarkerAndSeededOnesDoNot(t *testing.T) {
	p := testParams()
	for _, f := range DefaultFiles() {
		rel, content, err := RenderE(f, p)
		require.NoError(t, err)
		if f.Ownership == OwnershipGenerated {
			assert.Contains(t, string(content), GeneratedMarker,
				"%s is generated and must say so", rel)
		} else {
			assert.NotContains(t, string(content), GeneratedMarker,
				"%s is the consumer's; a DO NOT EDIT header would be a lie", rel)
		}
	}
}

// The launcher is named after the repository, so the path itself is templated.
func TestLauncherPathIsTemplated(t *testing.T) {
	p := testParams()
	p.Name = "sailboat"
	for _, f := range DefaultFiles() {
		rel, content, err := RenderE(f, p)
		require.NoError(t, err)
		if !strings.HasSuffix(rel, ".sh") || strings.Contains(rel, "/") {
			continue
		}
		assert.Equal(t, "sailboat.sh", rel)
		assert.Contains(t, string(content), "./public/app")
	}
}

// The seeded tag manifest must satisfy the contract that polices it; a skeleton
// that emits a tags file its own gate rejects is worse than no skeleton.
func TestSeededTagsSatisfyTheBuildTagContract(t *testing.T) {
	dir := t.TempDir()
	_, err := WriteE(dir, DefaultFiles(), testParams())
	require.NoError(t, err)

	raw, err := os.ReadFile(filepath.Join(dir, "tags"))
	require.NoError(t, err)
	for f := range buildtags.Check(buildtags.ParseTags(string(raw))) {
		t.Errorf("seeded tags violate the contract: %s", f.Message())
	}
}

func TestRenderRejectsAnEmptyName(t *testing.T) {
	p := testParams()
	p.Name = ""
	_, _, err := RenderE(DefaultFiles()[0], p)
	require.Error(t, err)
}

func TestRenderRejectsANameWithAPathSeparator(t *testing.T) {
	p := testParams()
	p.Name = "a/b"
	_, _, err := RenderE(DefaultFiles()[0], p)
	require.Error(t, err)
}

func TestFirstDiffPinpointsTheLine(t *testing.T) {
	line, want, got := firstDiff([]byte("a\nb\nc\n"), []byte("a\nX\nc\n"))
	assert.Equal(t, int32(2), line)
	assert.Equal(t, "b", want)
	assert.Equal(t, "X", got)

	line, _, _ = firstDiff([]byte("a\n"), []byte("a\n"))
	assert.Zero(t, line)

	line, _, got = firstDiff([]byte("a\n"), []byte("a\nextra\n"))
	assert.Equal(t, int32(2), line)
	assert.Equal(t, "extra", got)
}

// boxer must not emit a document its own doclint rejects. This caught a seeded
// AGENTS.md with no front-matter, which failed DL001 the first time a consumer
// ran the gate on a freshly written skeleton.
func TestEmittedMarkdownSatisfiesDoclint(t *testing.T) {
	dir := t.TempDir()
	_, err := WriteE(dir, DefaultFiles(), testParams())
	require.NoError(t, err)

	linter := doclint.NewDefaultLinter()
	for f, runErr := range linter.Run([]string{dir}) {
		require.NoError(t, runErr)
		if f.Severity >= doclint.FindingSeverityError {
			t.Errorf("emitted %s violates %s: %s", f.Path, f.RuleId, f.Message)
		}
	}
}

// The launcher carries a seam for the same reason lint.sh does: the second
// repository to adopt this had coverage instrumentation in its launcher, and a
// template that silently dropped it would have been a functional regression.
func TestLauncherHonoursTheLocalSeam(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash unavailable")
	}
	dir := t.TempDir()
	p := testParams()
	_, err := WriteE(dir, DefaultFiles(), p)
	require.NoError(t, err)

	require.NoError(t, os.MkdirAll(filepath.Join(dir, "scripts", "dev"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "scripts", "dev", "launcher-local.sh"),
		[]byte("EXTRA_BUILD_FLAGS+=(-cover)\nexport SEAM_RAN=1\n"), 0o644))

	// Stub `go` so the launcher's build is observable without a real toolchain.
	bin := filepath.Join(dir, "stubbin")
	require.NoError(t, os.MkdirAll(bin, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(bin, "go"),
		[]byte("#!/bin/bash\necho \"go $* seam=${SEAM_RAN:-0}\" > \"$(dirname \"$0\")/../observed.txt\"\ntouch \"${!#}\" 2>/dev/null || true\nexit 0\n"), 0o755))

	cmd := exec.Command("bash", filepath.Join(dir, "thing.sh"))
	cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"))
	_ = cmd.Run() // the stubbed build produces no runnable binary; only the flags matter

	observed, err := os.ReadFile(filepath.Join(dir, "observed.txt"))
	require.NoError(t, err, "the launcher did not reach its go build")
	assert.Contains(t, string(observed), "-cover", "EXTRA_BUILD_FLAGS was not passed through")
	assert.Contains(t, string(observed), "seam=1", "the seam file was not sourced")
}

// Without the seam file the launcher must still build cleanly — an empty
// EXTRA_BUILD_FLAGS array must not expand to a stray empty argument.
func TestLauncherWithoutTheSeamPassesNoStrayArgument(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash unavailable")
	}
	dir := t.TempDir()
	_, err := WriteE(dir, DefaultFiles(), testParams())
	require.NoError(t, err)

	bin := filepath.Join(dir, "stubbin")
	require.NoError(t, os.MkdirAll(bin, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(bin, "go"),
		[]byte("#!/bin/bash\nfor a in \"$@\"; do [ -z \"$a\" ] && { echo EMPTY-ARG > \"$(dirname \"$0\")/../observed.txt\"; exit 3; }; done\necho ok > \"$(dirname \"$0\")/../observed.txt\"\nexit 0\n"), 0o755))

	cmd := exec.Command("bash", filepath.Join(dir, "thing.sh"))
	cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"))
	_ = cmd.Run()

	observed, err := os.ReadFile(filepath.Join(dir, "observed.txt"))
	require.NoError(t, err)
	assert.Equal(t, "ok\n", string(observed))
}

// The adoption ADR's path hardcodes a number. Seeding it into a repository that
// already has an ADR corpus collides with whatever its ADR-0001 already is —
// which is exactly what happened on the second real adoption.
func TestAdoptionAdrIsNotSeededIntoAnExistingCorpus(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "doc", "adr"), 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "doc", "adr", "0001-something-else.md"),
		[]byte("---\ntype: adr\n---\n\n# ADR-0001\n"), 0o644))

	written, err := WriteE(dir, DefaultFiles(), testParams())
	require.NoError(t, err)
	assert.NotContains(t, written, "doc/adr/0001-adopt-boxer-standards.md")
	_, statErr := os.Stat(filepath.Join(dir, "doc", "adr", "0001-adopt-boxer-standards.md"))
	assert.True(t, os.IsNotExist(statErr), "must not collide with the existing ADR-0001")

	// And a suppressed seed is not then reported as something the repository owes.
	results, err := CheckE(dir, DefaultFiles(), testParams())
	require.NoError(t, err)
	for _, r := range results {
		assert.NotEqual(t, "doc/adr/0001-adopt-boxer-standards.md", r.Path,
			"a suppressed seed should not appear in the reconciliation at all")
	}
}

func TestAdoptionAdrIsSeededIntoAnEmptyRepository(t *testing.T) {
	dir := t.TempDir()
	written, err := WriteE(dir, DefaultFiles(), testParams())
	require.NoError(t, err)
	assert.Contains(t, written, "doc/adr/0001-adopt-boxer-standards.md")
}

// A repository whose Go tree is not laid out like boxer's must be able to say
// so without editing the generated wrapper. Found on the second adoption, whose
// code lives under src/go rather than public.
func TestLintWrapperSourcesGateFlags(t *testing.T) {
	p := testParams()
	for _, f := range DefaultFiles() {
		rel, content, err := RenderE(f, p)
		require.NoError(t, err)
		if rel != "scripts/ci/lint.sh" {
			continue
		}
		body := string(content)
		assert.Contains(t, body, "gate-flags.sh", "no seam for repository-specific gate configuration")
		assert.Contains(t, body, "GATE_FLAGS", "the seam sets no variable the gate call uses")
		assert.Contains(t, body, `"${GATE_FLAGS[@]+"${GATE_FLAGS[@]}"}"`,
			"an unset array must not expand to a stray empty argument")
	}
}
