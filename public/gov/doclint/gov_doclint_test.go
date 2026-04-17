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
