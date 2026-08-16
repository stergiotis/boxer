package codevol

import (
	"os"
	"path/filepath"
	"runtime/debug"
	"testing"

	"github.com/stretchr/testify/require"
)

// A test binary carries the main module but no dependency list — the
// toolchain omits it — so this pins only what is observable here. The
// built-binary case, where the dependency list is the whole point, is the
// integration lane's job.
func TestModules_ReportsMainModule(t *testing.T) {
	mods, ok := Modules()
	require.True(t, ok, "a test binary always carries build info")

	var main []ModuleInfo
	for _, m := range mods {
		if m.IsMain {
			main = append(main, m)
		}
	}
	require.Len(t, main, 1, "exactly one main module")
	require.Equal(t, PartyFirst, main[0].Party)
	require.Equal(t, "github.com/stergiotis/boxer", main[0].Path)
}

// modulesFrom is the mapping every caller goes through, so the shapes that
// only a built binary produces — versions, sums, replace directives — are
// pinned here against a synthetic BuildInfo rather than left to the
// integration lane alone.
func TestModulesFrom_MapsVersionsSumsAndReplaces(t *testing.T) {
	mods := modulesFrom(&debug.BuildInfo{
		Main: debug.Module{Path: "example.com/main", Version: "(devel)"},
		Deps: []*debug.Module{
			{Path: "example.com/plain", Version: "v1.2.3", Sum: "h1:abc="},
			{
				Path: "example.com/replaced", Version: "v0.1.0",
				Replace: &debug.Module{Path: "example.com/fork", Version: "v0.2.0"},
			},
			{
				Path: "example.com/localised", Version: "v0.1.0",
				Replace: &debug.Module{Path: "../local"},
			},
			nil, // the slice is []*Module; a nil entry must not panic
		},
	})
	require.Len(t, mods, 4)

	by := map[string]ModuleInfo{}
	for _, m := range mods {
		by[m.Path] = m
	}
	require.True(t, by["example.com/main"].IsMain)
	require.Equal(t, PartyFirst, by["example.com/main"].Party)

	require.Equal(t, PartyThird, by["example.com/plain"].Party)
	require.Equal(t, "v1.2.3", by["example.com/plain"].Version)
	require.Equal(t, "h1:abc=", by["example.com/plain"].Sum)
	require.Empty(t, by["example.com/plain"].ReplacedBy)

	require.Equal(t, "example.com/fork@v0.2.0", by["example.com/replaced"].ReplacedBy)
	// A directory replacement has no version; the path alone is the answer.
	require.Equal(t, "../local", by["example.com/localised"].ReplacedBy)
}

func TestModuleIndex_LongestPrefixWinsAndStdlibIsUnmatched(t *testing.T) {
	idx := NewModuleIndex([]ModuleInfo{
		{Path: "example.com/a", Party: PartyThird},
		{Path: "example.com/a/b", Party: PartyThird},
		{Path: "github.com/stergiotis/boxer", IsMain: true, Party: PartyFirst},
	})

	mod, party := idx.Lookup("example.com/a/b/c")
	require.Equal(t, "example.com/a/b", mod, "the inner module wins, as the toolchain resolves it")
	require.Equal(t, PartyThird, party)

	mod, _ = idx.Lookup("example.com/a/z")
	require.Equal(t, "example.com/a", mod)

	mod, party = idx.Lookup("github.com/stergiotis/boxer/public/code")
	require.Equal(t, "github.com/stergiotis/boxer", mod)
	require.Equal(t, PartyFirst, party)

	// A prefix that only matches textually must not match: "example.comX" is
	// not inside "example.com/a".
	mod, party = idx.Lookup("example.comX/a")
	require.Equal(t, "std", mod)
	require.Equal(t, PartyStdlib, party)

	// No module owns the standard library, so an unmatched path is stdlib.
	mod, party = idx.Lookup("net/http")
	require.Equal(t, "std", mod)
	require.Equal(t, PartyStdlib, party)
}

func TestModuleIndex_NilIsUsable(t *testing.T) {
	var idx *ModuleIndex
	mod, party := idx.Lookup("anything/at/all")
	require.Equal(t, "std", mod)
	require.Equal(t, PartyStdlib, party)
}

func TestPackageOfSymbol(t *testing.T) {
	for _, tc := range []struct {
		name string
		sym  string
		want string
	}{
		{"plain func", "github.com/foo/bar.Baz", "github.com/foo/bar"},
		{"pointer method", "github.com/foo/bar.(*T).M", "github.com/foo/bar"},
		{"stdlib", "net/http.(*Server).Serve", "net/http"},
		{"single segment", "runtime.mallocgc", "runtime"},
		{"dotted domain only", "gopkg.in/yaml.v3.Marshal", "gopkg.in/yaml"},
		{"type descriptor", "type:github.com/foo/bar.T", "github.com/foo/bar"},
		{"eq wrapper", "type:.eq.github.com/foo/bar.T", "github.com/foo/bar"},
		{"generic instantiation", "github.com/foo/bar.F[go.shape.int]", "github.com/foo/bar"},
		{"itab is not attributable", "go:itab.github.com/foo/bar.T,io.Writer", ""},
		{"no package", "main", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, PackageOfSymbol(tc.sym))
		})
	}
}

// The symbol reader must report a clean error, never a panic or a wrong
// answer, when pointed at something that is not an unstripped ELF binary —
// that is what lets the provider degrade to an empty table.
func TestReadSymbolsFile_NonElfIsAnError(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "not-a-binary")
	require.NoError(t, os.WriteFile(p, []byte("#!/bin/sh\necho hi\n"), 0o644))

	_, err := readSymbolsFile(p, nil)
	require.Error(t, err)
}

func TestReadSelfSymbols_ReadsOwnTable(t *testing.T) {
	mods, _ := Modules()
	rep, err := ReadSelfSymbols(NewModuleIndex(mods))
	if err != nil {
		// Non-ELF platform, or a stripped test binary: the contract is a
		// clean error, which the caller turns into an empty table.
		t.Skipf("no readable symbol table here: %v", err)
	}

	require.NotEmpty(t, rep.Packages)
	require.Greater(t, rep.TotalText, uint64(0))
	require.True(t, rep.ModuleExact)

	// Per-package sums must reconcile with the totals, minus what was
	// deliberately left unattributed.
	var text, data uint64
	var first, third, std int
	for _, p := range rep.Packages {
		text += p.TextBytes
		data += p.DataBytes
		switch p.Party {
		case PartyFirst:
			first++
		case PartyThird:
			third++
		case PartyStdlib:
			std++
		}
	}
	require.LessOrEqual(t, text, rep.TotalText)
	require.LessOrEqual(t, data, rep.TotalData)

	// Own packages resolve against the main module, and everything the index
	// does not know falls through to std. Third-party attribution cannot be
	// asserted here: a test binary's build info carries no dependency list,
	// so the index holds only the main module and testify's packages land in
	// std. The integration lane checks the built-binary case, where the
	// three-way split is the whole point.
	require.Greater(t, first, 0, "own packages must be attributed to the main module")
	require.Greater(t, std, 0, "unknown-module packages must fall through to std")
	_ = third
}

func TestCountFiles_ClassifiesLines(t *testing.T) {
	dir := t.TempDir()
	src := "package p\n" + // code
		"\n" + // blank
		"// a comment\n" + // comment
		"/* block\n   spanning */\n" + // 2 comment lines
		"const S = \"// not a comment\"\n" + // code, despite the //
		"var X = 1 // trailing\n" // code (trailing comment does not make it a comment line)
	p := filepath.Join(dir, "a.go")
	require.NoError(t, os.WriteFile(p, []byte(src), 0o644))

	v := CountFiles([]string{p}, nil)
	require.Equal(t, 1, v.Files)
	require.Equal(t, 3, v.CodeLines, "package clause, the const, the var")
	require.Equal(t, 3, v.CommentLines, "the line comment plus both block lines")
	require.Equal(t, 1, v.BlankLines)
	require.Equal(t, 0, v.GeneratedFiles)
	// The three classes partition the file.
	require.Equal(t, 7, v.CodeLines+v.CommentLines+v.BlankLines)
}

func TestCountFiles_DetectsGeneratedMarker(t *testing.T) {
	dir := t.TempDir()
	gen := filepath.Join(dir, "gen.go")
	require.NoError(t, os.WriteFile(gen,
		[]byte("// Code generated by thing; DO NOT EDIT.\n\npackage p\n\nvar X = 1\n"), 0o644))
	// A marker that is not on its own line must not count.
	notGen := filepath.Join(dir, "notgen.go")
	require.NoError(t, os.WriteFile(notGen,
		[]byte("// see: Code generated by thing; DO NOT EDIT. (nope)\npackage p\n"), 0o644))

	v := CountFiles([]string{gen}, nil)
	require.Equal(t, 1, v.GeneratedFiles)
	require.Equal(t, v.CodeLines, v.GeneratedCode)

	v = CountFiles([]string{notGen}, nil)
	require.Equal(t, 0, v.GeneratedFiles)
}

func TestCountFiles_OtherLanguagesAndMissingFiles(t *testing.T) {
	dir := t.TempDir()
	c := filepath.Join(dir, "x.c")
	require.NoError(t, os.WriteFile(c, []byte("int main(){\nreturn 0;\n}\n"), 0o644))

	// An unreadable path is skipped, not fatal: a tally is best-effort.
	v := CountFiles([]string{filepath.Join(dir, "gone.go")}, []string{c, filepath.Join(dir, "gone.c")})
	require.Equal(t, 0, v.Files)
	require.Equal(t, 3, v.OtherLangLines)
}

// Every case here is a marker shape that occurs verbatim in this repository
// (ADR-0173 §SD10). The tool field is free-form, so these are the forms the
// normalisation has to survive rather than a specification of it.
func TestGeneratorOf(t *testing.T) {
	for _, tc := range []struct {
		name, line, want string
		wantOk           bool
	}{
		{
			name: "the convention's own form",
			line: "// Code generated by protoc-gen-go. DO NOT EDIT.",
			want: "protoc-gen-go", wantOk: true,
		}, {
			name: "ANTLR names its input first; only the tool is kept",
			line: "// Code generated from ClickHouseLexer.g4 by ANTLR 4.13.2. DO NOT EDIT.",
			want: "ANTLR 4.13.2", wantOk: true,
		}, {
			// Without this, one tool becomes a bucket per calling package.
			name: "a trailing invoker parenthetical is dropped",
			line: "// Code generated by Leeway DML (github.com/x/y/anchor.test); DO NOT EDIT.",
			want: "Leeway DML", wantOk: true,
		}, {
			name: "an em-dash terminator",
			line: "// Code generated by boxer/public/semistructured/leeway/marshall/go/marshallgen — DO NOT EDIT.",
			want: "boxer/public/semistructured/leeway/marshall/go/marshallgen", wantOk: true,
		}, {
			// The leading "." must survive: several markers name a script by
			// relative path, which is why the trim is right-hand only.
			name: "a relative script path keeps its leading dot",
			line: "// Code generated by ./boxer.sh designsystem colors vendor — DO NOT EDIT.",
			want: "./boxer.sh designsystem colors vendor", wantOk: true,
		}, {
			name: "a backticked command line survives intact",
			line: "// Code generated by `wasmsurvey props harvest --emit go` DO NOT EDIT.",
			want: "`wasmsurvey props harvest --emit go`", wantOk: true,
		}, {
			// Not normalised to "copy paste": the column reports what somebody
			// typed, and pretending otherwise would hide that they differ.
			name: "a typo is a distinct tool, not an error",
			line: "// Code generated by copy pase; DO NOT EDIT.",
			want: "copy pase", wantOk: true,
		}, {
			name: "a marker with no tool at all still groups",
			line: "// Code generated  DO NOT EDIT.",
			want: GeneratorUnknown, wantOk: true,
		}, {
			name: "not a marker: the line must stand alone",
			line: "// see: Code generated by thing; DO NOT EDIT. (nope)",
			want: "", wantOk: false,
		}, {
			name: "not a marker: ordinary comment",
			line: "// a comment",
			want: "", wantOk: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := GeneratorOf(tc.line)
			require.Equal(t, tc.wantOk, ok)
			require.Equal(t, tc.want, got)
		})
	}
}

// A package with files from several generators reports all of them, sorted,
// with no duplicates — the shape a quarter of this repository's generated
// packages actually have.
func TestCountFiles_GeneratorsAreSortedAndDeduplicated(t *testing.T) {
	dir := t.TempDir()
	write := func(name, marker string) string {
		p := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(p, []byte(marker+"\n\npackage p\n"), 0o644))
		return p
	}
	paths := []string{
		write("z.go", "// Code generated by Leeway readaccess (github.com/x/y.test); DO NOT EDIT."),
		write("a.go", "// Code generated by Leeway DML (github.com/x/y.test); DO NOT EDIT."),
		// Same tool as z.go from a different caller: one entry, not two.
		write("m.go", "// Code generated by Leeway readaccess (github.com/x/other.test); DO NOT EDIT."),
		write("plain.go", "// Code generated from Foo.g4 by ANTLR 4.13.2. DO NOT EDIT."),
	}

	v := CountFiles(paths, nil)
	require.Equal(t, 4, v.GeneratedFiles)
	require.Equal(t, []string{"ANTLR 4.13.2", "Leeway DML", "Leeway readaccess"}, v.Generators)
}

// A package nobody generated reports no generators, not a placeholder.
func TestCountFiles_NoGeneratorsWhenNothingGenerated(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.go")
	require.NoError(t, os.WriteFile(p, []byte("package p\nvar X = 1\n"), 0o644))

	require.Empty(t, CountFiles([]string{p}, nil).Generators)
}

// A file whose last line has no trailing newline still counts that line.
func TestCountFiles_NoTrailingNewline(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.go")
	require.NoError(t, os.WriteFile(p, []byte("package p\nvar X = 1"), 0o644))

	v := CountFiles([]string{p}, nil)
	require.Equal(t, 2, v.CodeLines)
}
