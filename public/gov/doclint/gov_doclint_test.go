//go:build llm_generated_opus47

package doclint

import (
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func collectFindings(t *testing.T, rule RuleI, roots []string) (out []Finding) {
	t.Helper()
	for f, err := range rule.Check(roots) {
		require.NoError(t, err)
		out = append(out, f)
	}
	return
}

func TestRuleDL005DetectsBannedFilenames(t *testing.T) {
	rule := NewRuleDL005()
	findings := collectFindings(t, rule, []string{"testdata/dl005"})
	bases := map[string]bool{}
	for _, f := range findings {
		require.Equal(t, "DL005", f.RuleId)
		require.Equal(t, FindingSeverityError, f.Severity)
		bases[filepath.Base(f.Path)] = true
	}
	require.True(t, bases["SPEC.md"], "SPEC.md should be flagged")
	require.True(t, bases["DESIGN.md"], "DESIGN.md should be flagged")
	require.False(t, bases["ok.md"], "ok.md must not be flagged")
}

func TestRuleDL001ValidatesFrontMatter(t *testing.T) {
	rule := NewRuleDL001()
	findings := collectFindings(t, rule, []string{"testdata/dl001"})

	type bucket struct {
		base string
		msg  string
	}
	var buckets []bucket
	for _, f := range findings {
		require.Equal(t, "DL001", f.RuleId)
		require.Equal(t, FindingSeverityError, f.Severity)
		buckets = append(buckets, bucket{filepath.Base(f.Path), f.Message})
	}

	hasFor := func(base string) bool {
		for _, b := range buckets {
			if b.base == base {
				return true
			}
		}
		return false
	}

	require.True(t, hasFor("no_frontmatter.md"), "missing front-matter must be flagged")
	require.True(t, hasFor("invalid_status.md"), "invalid status must be flagged")
	require.True(t, hasFor("invalid_type.md"), "invalid type must be flagged")
	require.False(t, hasFor("compliant.md"), "compliant fixture must not be flagged")
	require.False(t, hasFor("compliant_adr.md"), "compliant ADR fixture must not be flagged")
}

func TestExtractFrontMatter(t *testing.T) {
	cases := []struct {
		name string
		in   string
		fm   string
		body string
		ok   bool
	}{
		{"none", "# Hello\n", "", "", false},
		{"basic", "---\ntype: reference\nstatus: stable\n---\n# Body\n", "type: reference\nstatus: stable", "# Body\n", true},
		{"crlf", "---\r\ntype: reference\r\nstatus: stable\r\n---\r\n# Body\r\n", "type: reference\nstatus: stable", "# Body\n", true},
		{"no_close", "---\ntype: reference\n", "", "", false},
		{"empty_meta", "---\n---\n# Body\n", "", "# Body\n", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fm, body, ok := ExtractFrontMatter([]byte(c.in))
			require.Equal(t, c.ok, ok)
			require.Equal(t, c.fm, string(fm))
			require.Equal(t, c.body, string(body))
		})
	}
}

func TestRuleDL003ChecksReviewMetadata(t *testing.T) {
	rule := NewRuleDL003()
	findings := collectFindings(t, rule, []string{"testdata/dl003"})
	type entry struct {
		base string
		msg  string
	}
	var entries []entry
	for _, f := range findings {
		require.Equal(t, "DL003", f.RuleId)
		entries = append(entries, entry{filepath.Base(f.Path), f.Message})
	}

	hasFor := func(base string) bool {
		for _, e := range entries {
			if e.base == base {
				return true
			}
		}
		return false
	}
	severityFor := func(base string) FindingSeverityE {
		for _, f := range findings {
			if filepath.Base(f.Path) == base {
				return f.Severity
			}
		}
		return 0
	}

	require.True(t, hasFor("missing_reviewed_by.md"))
	require.True(t, hasFor("missing_reviewed_date.md"))
	require.True(t, hasFor("invalid_date.md"))
	require.Equal(t, FindingSeverityWarn, severityFor("invalid_date.md"))
	require.False(t, hasFor("compliant.md"))
	require.False(t, hasFor("draft_no_review_needed.md"))
}

func TestRuleDL004ChecksBannerConsistency(t *testing.T) {
	rule := NewRuleDL004()
	findings := collectFindings(t, rule, []string{"testdata/dl004"})
	bases := map[string]bool{}
	for _, f := range findings {
		require.Equal(t, "DL004", f.RuleId)
		require.Equal(t, FindingSeverityError, f.Severity)
		bases[filepath.Base(f.Path)] = true
	}
	require.True(t, bases["draft_no_banner.md"])
	require.True(t, bases["draft_wrong_banner.md"])
	require.True(t, bases["stable_with_banner.md"])
	require.False(t, bases["draft_with_banner.md"])
	require.False(t, bases["stable_no_banner.md"])
	require.False(t, bases["proposed_with_banner.md"])
}

func TestDetectStatusBanner(t *testing.T) {
	cases := []struct {
		name  string
		body  string
		found bool
		state string
	}{
		{"none", "# Hello\n", false, ""},
		{"canonical_draft", "> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.\n\n# Body\n", true, "draft"},
		{"canonical_proposed", "> **Status: proposed — pre-human-review.** Decision under consideration.\n", true, "proposed"},
		{"trailing_prose", "> **Status: draft — pre-human-review.** Not verified against the current documentation standard; migrated from `FFFI.md`. Do not cite as authoritative.\n", true, "draft"},
		{"leading_blanks", "\n\n> **Status: draft — pre-human-review.** ok.\n", true, "draft"},
		{"first_blockquote_not_banner", "> a regular blockquote\n\nbody\n", false, ""},
		{"first_line_not_blockquote", "# Heading\n\n> **Status: draft — pre-human-review.** later.\n", false, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			found, state := DetectStatusBanner([]byte(c.body))
			require.Equal(t, c.found, found)
			require.Equal(t, c.state, state)
		})
	}
}

func TestRuleDL010ChecksAdrSections(t *testing.T) {
	rule := NewRuleDL010()
	findings := collectFindings(t, rule, []string{"testdata/dl010"})
	type entry struct {
		base string
		msg  string
	}
	var entries []entry
	for _, f := range findings {
		require.Equal(t, "DL010", f.RuleId)
		require.Equal(t, FindingSeverityError, f.Severity)
		entries = append(entries, entry{filepath.Base(f.Path), f.Message})
	}

	countFor := func(base string) (n int) {
		for _, e := range entries {
			if e.base == base {
				n++
			}
		}
		return
	}

	require.Equal(t, 0, countFor("compliant_adr.md"))
	require.Equal(t, 1, countFor("missing_consequences.md"))
	require.Equal(t, 2, countFor("missing_multiple.md"))
	require.Equal(t, 0, countFor("non_adr.md"), "non-ADR files must not be checked by DL010")
	require.Equal(t, 0, countFor("descriptive_status.md"), "descriptive section suffix must satisfy presence check")
}

func TestRuleDL011ReportsOpenDrafts(t *testing.T) {
	rule := NewRuleDL011()
	findings := collectFindings(t, rule, []string{"testdata/dl011"})
	bases := map[string]string{}
	for _, f := range findings {
		require.Equal(t, "DL011", f.RuleId)
		require.Equal(t, FindingSeverityInfo, f.Severity)
		bases[filepath.Base(f.Path)] = f.Message
	}
	require.Contains(t, bases, "draft_doc.md")
	require.Contains(t, bases["draft_doc.md"], "draft")
	require.Contains(t, bases, "proposed_adr.md")
	require.Contains(t, bases["proposed_adr.md"], "proposed")
	require.NotContains(t, bases, "stable_doc.md")
	require.NotContains(t, bases, "accepted_adr.md")
}

func TestExtractH2TitlesAndPresence(t *testing.T) {
	body := []byte("# H1\n\n## Context\n\nfoo\n\n## Status — overridden\n\nbar\n\n## Decision\n")
	titles := extractH2Titles(body)
	require.True(t, isSectionPresent(titles, "Context"))
	require.True(t, isSectionPresent(titles, "Decision"))
	require.True(t, isSectionPresent(titles, "Status"), "descriptive suffix should still satisfy 'Status'")
	require.False(t, isSectionPresent(titles, "Consequences"))
}

func TestRuleDL006FlagsBareDirectoryNames(t *testing.T) {
	rule := NewRuleDL006()
	findings := collectFindings(t, rule, []string{"testdata/dl006"})
	bases := map[string][]string{}
	for _, f := range findings {
		require.Equal(t, "DL006", f.RuleId)
		require.Equal(t, FindingSeverityWarn, f.Severity)
		bases[filepath.Base(f.Path)] = append(bases[filepath.Base(f.Path)], f.Message)
	}
	require.Contains(t, bases, "bare_name.md")
	require.NotContains(t, bases, "qualified.md")
	require.NotContains(t, bases, "file_link.md")
	require.NotContains(t, bases, "external.md")
	require.NotContains(t, bases, "anchor.md")
}

func TestRuleDL007FlagsBrokenLinks(t *testing.T) {
	rule := NewRuleDL007()
	findings := collectFindings(t, rule, []string{"testdata/dl007"})
	bases := map[string]bool{}
	for _, f := range findings {
		require.Equal(t, "DL007", f.RuleId)
		require.Equal(t, FindingSeverityError, f.Severity)
		bases[filepath.Base(f.Path)] = true
	}
	require.True(t, bases["broken_link.md"])
	require.False(t, bases["all_resolve.md"])
	require.False(t, bases["external_only.md"])
	require.False(t, bases["anchor_only.md"])
	require.False(t, bases["sibling.md"])
}

func TestExtractInlineLinks(t *testing.T) {
	body := []byte("intro [first](./a.md) prose\n[second](https://example.com)\n[`third`](../b)\n")
	links := extractInlineLinks(body)
	require.Len(t, links, 3)
	require.Equal(t, "first", links[0].Text)
	require.Equal(t, "./a.md", links[0].URL)
	require.Equal(t, int32(1), links[0].Line)
	require.Equal(t, "second", links[1].Text)
	require.Equal(t, int32(2), links[1].Line)
	require.Equal(t, "`third`", links[2].Text)
	require.Equal(t, "../b", links[2].URL)
}

func TestExtractInlineLinksSkipsFencedCodeBlocks(t *testing.T) {
	body := []byte("real [outside](./real.md)\n\n```markdown\nfake [inside](./nope.md)\n```\n\nreal [after](./real2.md)\n")
	links := extractInlineLinks(body)
	require.Len(t, links, 2, "links inside ```...``` blocks must be skipped")
	require.Equal(t, "outside", links[0].Text)
	require.Equal(t, "after", links[1].Text)
}

func TestRuleDL009ChecksGoDocComments(t *testing.T) {
	rule := NewRuleDL009()
	findings := collectFindings(t, rule, []string{"testdata/dl009"})
	type entry struct {
		base     string
		msg      string
		severity FindingSeverityE
	}
	var entries []entry
	for _, f := range findings {
		require.Equal(t, "DL009", f.RuleId)
		entries = append(entries, entry{filepath.Base(f.Path), f.Message, f.Severity})
	}

	hasOn := func(base, want string) bool {
		for _, e := range entries {
			if e.base == base && strings.Contains(e.msg, want) {
				return true
			}
		}
		return false
	}
	severityOn := func(base, want string) FindingSeverityE {
		for _, e := range entries {
			if e.base == base && strings.Contains(e.msg, want) {
				return e.severity
			}
		}
		return 0
	}
	countFor := func(base string) (n int) {
		for _, e := range entries {
			if e.base == base {
				n++
			}
		}
		return
	}

	require.Equal(t, 0, countFor("compliant.go"), "compliant fixture must produce no findings")

	require.True(t, hasOn("missing_doc.go", "Foo"))
	require.True(t, hasOn("missing_doc.go", "ExportedType"))
	require.True(t, hasOn("missing_doc.go", "ExportedConst"))
	require.Equal(t, FindingSeverityInfo, severityOn("missing_doc.go", "Foo"),
		"missing doc comments are info-severity (baseline cleanup gap)")

	require.True(t, hasOn("wrong_prefix.go", "does not begin with 'Bar'"))
	require.Equal(t, FindingSeverityWarn, severityOn("wrong_prefix.go", "does not begin with 'Bar'"),
		"existing comment with wrong prefix is warn-severity (active style violation)")

	require.True(t, hasOn("no_period.go", "does not end with"))
	require.Equal(t, FindingSeverityWarn, severityOn("no_period.go", "does not end with"))

	require.Equal(t, 0, countFor("unexported.go"))
}

func TestEndsWithSentenceTerminator(t *testing.T) {
	require.True(t, endsWithSentenceTerminator("Foo does X."))
	require.True(t, endsWithSentenceTerminator("Stop!"))
	require.True(t, endsWithSentenceTerminator("Why?"))
	require.False(t, endsWithSentenceTerminator("Foo does X"))
	require.False(t, endsWithSentenceTerminator(""))
}

func TestIsInScopeForDL009(t *testing.T) {
	require.True(t, IsInScopeForDL009("public/foo/bar.go"))
	require.False(t, IsInScopeForDL009("public/foo/bar_test.go"))
	require.False(t, IsInScopeForDL009("public/foo/bar.gen.go"))
	require.False(t, IsInScopeForDL009("public/foo/bar.out.go"))
	require.False(t, IsInScopeForDL009("public/foo/bar.idl.go"))
	require.False(t, IsInScopeForDL009("doc/standard.md"))
}

func TestIsInScopeForDL001ModuleRootReadme(t *testing.T) {
	moduleRoot := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(moduleRoot, "go.mod"), []byte("module x\n"), 0o644))
	rootReadme := filepath.Join(moduleRoot, "README.md")
	require.NoError(t, os.WriteFile(rootReadme, []byte("# x\n"), 0o644))
	require.False(t, IsInScopeForDL001(rootReadme, "README.md"),
		"module-root README.md (sibling of go.mod) must be out of scope")

	pkgDir := filepath.Join(moduleRoot, "pkg")
	require.NoError(t, os.Mkdir(pkgDir, 0o755))
	pkgReadme := filepath.Join(pkgDir, "README.md")
	require.NoError(t, os.WriteFile(pkgReadme, []byte("# pkg\n"), 0o644))
	require.True(t, IsInScopeForDL001(pkgReadme, "README.md"),
		"package-level README.md (no sibling go.mod) must remain in scope")
}

func TestRuleDL008ChecksGoDocLinks(t *testing.T) {
	rule := NewRuleDL008()
	findings := collectFindings(t, rule, []string{"testdata/dl008"})
	type entry struct {
		base string
		msg  string
	}
	var entries []entry
	for _, f := range findings {
		require.Equal(t, "DL008", f.RuleId)
		require.Equal(t, FindingSeverityWarn, f.Severity)
		entries = append(entries, entry{filepath.Base(f.Path), f.Message})
	}

	hasOn := func(base, want string) bool {
		for _, e := range entries {
			if e.base == base && strings.Contains(e.msg, want) {
				return true
			}
		}
		return false
	}
	countFor := func(base string) (n int) {
		for _, e := range entries {
			if e.base == base {
				n++
			}
		}
		return
	}

	require.Equal(t, 0, countFor("compliant.go"), "compliant fixture must produce no findings")
	require.True(t, hasOn("broken.go", "[DoesNotExist]"))
	require.True(t, hasOn("broken.go", "[AlsoMissing]"))
	require.False(t, hasOn("broken.go", "[Foo]"), "valid same-package reference must not be flagged")
	require.Equal(t, 0, countFor("excluded_forms.go"),
		"qualified [pkg.Sym], lowercase, generic [T], slice/array brackets must all be ignored in v1")
}

func TestFindDL008CandidatesSkipsCodeBlocks(t *testing.T) {
	text := "Foo references [Real].\n\n\tindented code uses [FakeInBlock]\n\nplain prose [AnotherReal]\n"
	cands := findDL008Candidates(text, token.Position{Line: 1})
	got := make([]string, 0, len(cands))
	for _, c := range cands {
		got = append(got, c.name)
	}
	require.Equal(t, []string{"Real", "AnotherReal"}, got)
}

func TestFindDL008CandidatesSkipsBacktickSpans(t *testing.T) {
	text := "describing the `[Name]` syntax matches no candidate, but [RealRef] does"
	cands := findDL008Candidates(text, token.Position{Line: 1})
	require.Len(t, cands, 1)
	require.Equal(t, "RealRef", cands[0].name)
}

func TestParseFormatAndSeverity(t *testing.T) {
	f, err := ParseFormatE("json")
	require.NoError(t, err)
	require.Equal(t, FormatJson, f)
	_, err = ParseFormatE("xml")
	require.Error(t, err)

	s, err := ParseSeverityE("error")
	require.NoError(t, err)
	require.Equal(t, FindingSeverityError, s)
	_, err = ParseSeverityE("panic")
	require.Error(t, err)
}
