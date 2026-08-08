package buildtags

import (
	"os"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func collect(tags []string) (out []Finding) {
	out = make([]Finding, 0, 4)
	for f := range Check(tags) {
		out = append(out, f)
	}
	return
}

func TestParseTagsTolerance(t *testing.T) {
	assert.Equal(t, []string{"a", "b"}, ParseTags("a,b\n"))
	assert.Equal(t, []string{"a", "b"}, ParseTags(" a , b "))
	assert.Equal(t, []string{"a"}, ParseTags("a,,\n"))
	assert.Empty(t, ParseTags(""))
	assert.Empty(t, ParseTags("\n"))
}

func TestCheckAcceptsRequiredPlusLocal(t *testing.T) {
	tags := slices.Clone(RequiredTags)
	tags = append(tags, "myrepo_local_thing", "another_local")
	assert.Empty(t, collect(tags), "repo-local tags must pass silently")
}

func TestCheckAcceptsOptional(t *testing.T) {
	tags := slices.Concat(RequiredTags, OptionalTags)
	assert.Empty(t, collect(tags))
}

func TestCheckReportsMissingRequired(t *testing.T) {
	f := collect([]string{"myrepo_local_thing"})
	require.Len(t, f, len(RequiredTags))
	assert.Equal(t, FindingKindMissing, f[0].Kind)
	assert.Contains(t, f[0].Message(), "will not compile")
}

// The two retirements this package exists to catch, as the consumer
// repositories actually carried them.
func TestCheckReportsRetiredFamilies(t *testing.T) {
	tags := slices.Clone(RequiredTags)
	tags = append(tags,
		"identifier_tag_fixed16",
		"llm_generated_gemini3pro",
		"llm_generated_opus48",
		"boxer_enable_profiling",
	)
	f := collect(tags)
	require.Len(t, f, 3)
	for _, x := range f {
		assert.Equal(t, FindingKindRetired, x.Kind)
		assert.NotEmpty(t, x.Adr)
	}
	assert.Equal(t, "identifier_tag_fixed16", f[0].Tag)
	assert.Equal(t, "ADR-0106", f[0].Adr)
	assert.Equal(t, "ADR-0083", f[1].Adr)
	assert.Contains(t, f[1].Message(), "remove it")
}

func TestCheckOrderIsStableRegardlessOfInputOrder(t *testing.T) {
	a := collect([]string{"llm_generated_opus48", "identifier_tag_fixed16", "goexperiment.jsonv2"})
	b := collect([]string{"identifier_tag_fixed16", "goexperiment.jsonv2", "llm_generated_opus48"})
	assert.Equal(t, a, b)
}

func TestGoFlags(t *testing.T) {
	assert.Equal(t, "GOFLAGS=-tags=a,b", GoFlags([]string{"a", "b"}))
}

// The sets in this package are what consumers receive through the module pin;
// boxer's own ./tags is what its scripts and editors read. This asserts they
// have not diverged: every tag boxer sets must be declared (required or
// optional), every required tag must be present, and boxer must not itself
// carry a retired tag.
func TestRequiredAndOptionalMatchTagsFile(t *testing.T) {
	raw, err := os.ReadFile("../../../tags")
	require.NoError(t, err)
	own := ParseTags(string(raw))

	for _, tag := range own {
		declared := slices.Contains(RequiredTags, tag) || slices.Contains(OptionalTags, tag)
		assert.True(t, declared, "./tags carries %q but neither RequiredTags nor OptionalTags declares it", tag)
	}
	for _, req := range RequiredTags {
		assert.Contains(t, own, req, "RequiredTags declares %q but ./tags omits it", req)
	}
	assert.Empty(t, collect(own), "boxer's own ./tags must satisfy the contract it publishes")
}
