package doclint

import (
	"path/filepath"
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
