package capmapsimilarity_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/gov/capmapcorpus"
	"github.com/stergiotis/boxer/public/gov/capmapsimilarity"
)

// prose is long enough for the compressor to find structure in: NCD on a
// sentence is noise, and the ranker is for notes.
const prose = `Access decisions are taken against a policy that names subjects, resources and
actions, evaluated at the point of use rather than at login. The policy is
versioned with the service it protects, reviewed with its code, and its
decisions are logged with the identity that requested them. Revocation takes
effect at the next evaluation, which is why sessions are short and tokens carry
no rights of their own. Roles are a convenience for authoring policy, not a
mechanism of enforcement; a role that is never referenced by a rule grants
nothing. Exceptions are recorded as rules with an expiry and an owner.`

const unrelated = `Racks are arranged in hot and cold aisles with blanking panels closing every
unused unit, so supply air reaches the intake side and return air is not drawn
back through the equipment. Inlet temperature is measured at the top of each
rack, where it is highest, and the setpoint follows the manufacturer's allowable
envelope rather than the historical eighteen degrees. Containment doors and
overhead baffles keep the two air masses apart; a gap of a few centimetres is
enough to bypass a chiller's worth of cooling. Floor tiles are perforated only
where a rack draws air.`

func note(name string, level int, parent string, body string) string {
	var b strings.Builder
	b.WriteString("---\nname: \"" + name + "\"\nlevel: " + string(rune('0'+level)) + "\nparent_ids:")
	if parent == "" {
		b.WriteString(" []\n")
	} else {
		b.WriteString("\n  - \"[[" + parent + "]]\"\n")
	}
	b.WriteString("---\n")
	if body != "" {
		b.WriteString("\n# Vision and Scope\n\n" + body + "\n")
	}
	return b.String()
}

func writeVault(t *testing.T, files map[string]string) (root string) {
	t.Helper()
	root = t.TempDir()
	for rel, content := range files {
		path := filepath.Join(root, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
	return root
}

func entryFor(t *testing.T, res capmapsimilarity.Result, slug string) (e capmapsimilarity.Entry) {
	t.Helper()
	for _, e = range res.Entries {
		if e.Slug == slug {
			return e
		}
	}
	t.Fatalf("no entry for %s", slug)
	return e
}

// Two notes that are paraphrases of one another rank each other first, an
// unrelated one is not kept, and a parent is never compared with its child even
// when the child restates it verbatim.
func TestRankFindsParaphrasesAndSkipsAncestry(t *testing.T) {
	paraphrase := strings.Replace(prose, "Roles are a convenience", "Groups are a convenience", 1)
	root := writeVault(t, map[string]string{
		"biz/capability.md":              note("Business", 1, "", ""),
		"biz/identity/capability.md":     note("Identity", 2, "biz", prose),
		"biz/identity/access-control.md": note("Access Control", 3, "identity", paraphrase),
		"biz/identity/authorization.md":  note("Authorization", 3, "identity", prose),
		"biz/facilities/capability.md":   note("Facilities", 2, "biz", ""),
		"biz/facilities/cooling.md":      note("Cooling", 3, "facilities", unrelated),
		"biz/facilities/unwritten.md":    note("Unwritten", 3, "facilities", ""),
	})
	corpus, err := capmapcorpus.ParseDir(root)
	require.NoError(t, err)

	res, err := capmapsimilarity.Rank(corpus, capmapsimilarity.Options{})
	require.NoError(t, err)
	assert.Equal(t, capmapsimilarity.DefaultThreshold, res.Threshold)
	assert.Equal(t, 3, res.Unwritten, "business, facilities and unwritten carry no prose")

	// identity is the parent of both access-control and authorization, so of
	// the six pairs among the four written notes, those two are not measured.
	assert.Equal(t, 4, res.Compared)

	ac := entryFor(t, res, "access-control")
	require.NotEmpty(t, ac.Similar, "a paraphrase must be found")
	assert.Equal(t, "authorization", ac.Similar[0].Slug)
	assert.Less(t, ac.Similar[0].Ncd, 0.4)
	for _, n := range ac.Similar {
		assert.NotEqual(t, "cooling", n.Slug, "an unrelated note is over the threshold")
		assert.NotEqual(t, "identity", n.Slug, "an ancestor is never a neighbour")
	}
	assert.Empty(t, entryFor(t, res, "cooling").Similar)
	auth := entryFor(t, res, "authorization")
	require.Len(t, auth.Similar, 1)
	assert.Equal(t, ac.Similar[0].Ncd, auth.Similar[0].Ncd, "a pair is measured once and shared")

	for _, e := range res.Entries {
		assert.NotEqual(t, "unwritten", e.Slug, "nothing to measure, nothing to report")
	}
	assert.Empty(t, entryFor(t, res, "identity").Similar, "compared with cooling only, and unlike it")
}

// Within a catalog by default, across them on request — and never both, so the
// two runs partition the pairs.
func TestRankCatalogRuleIsAPartition(t *testing.T) {
	root := writeVault(t, map[string]string{
		"business/capability.md": note("Business", 1, "", ""),
		"business/access.md":     note("Access", 2, "business", prose),
		"business/access-two.md": note("Access Two", 2, "business", strings.Replace(prose, "policy", "rule", -1)),
		"platform/capability.md": note("Platform", 1, "", ""),
		"platform/authz.md":      note("Authz", 2, "platform", prose),
	})
	corpus, err := capmapcorpus.ParseDir(root)
	require.NoError(t, err)

	within, err := capmapsimilarity.Rank(corpus, capmapsimilarity.Options{Threshold: 1})
	require.NoError(t, err)
	assert.Equal(t, 1, within.Compared, "access × access-two")
	cross, err := capmapsimilarity.Rank(corpus, capmapsimilarity.Options{Threshold: 1, Cross: true})
	require.NoError(t, err)
	assert.Equal(t, 2, cross.Compared, "authz × each business note")
	assert.Equal(t, "authz", entryFor(t, cross, "access").Similar[0].Slug)
	assert.InDelta(t, 0, entryFor(t, cross, "access").Similar[0].Ncd, 0.1, "identical prose is near zero")
}

// The cap is per competence and the order is nearest first, ties by slug.
func TestRankTopIsNearestFirst(t *testing.T) {
	files := map[string]string{"c/capability.md": note("C", 1, "", "")}
	for _, s := range []string{"a", "b", "d", "e"} {
		files["c/"+s+".md"] = note(strings.ToUpper(s), 2, "c", strings.Replace(prose, "policy", s+"policy", 2))
	}
	corpus, err := capmapcorpus.ParseDir(writeVault(t, files))
	require.NoError(t, err)
	res, err := capmapsimilarity.Rank(corpus, capmapsimilarity.Options{Threshold: 1, Top: 2, Workers: 1})
	require.NoError(t, err)
	for _, e := range res.Entries {
		assert.LessOrEqual(t, len(e.Similar), 2, "%s", e.Slug)
		for i := 1; i < len(e.Similar); i++ {
			assert.LessOrEqual(t, e.Similar[i-1].Ncd, e.Similar[i].Ncd)
		}
	}
	again, err := capmapsimilarity.Rank(corpus, capmapsimilarity.Options{Threshold: 1, Top: 2, Workers: 4})
	require.NoError(t, err)
	assert.Equal(t, res, again, "the worker count must not change the answer")
}

func TestTextIsTheSectionsAndNothingElse(t *testing.T) {
	comp := capmapcorpus.Competence{Name: "N", Sections: []capmapcorpus.Section{
		{Heading: "A", Text: "one"}, {Heading: "Empty", Text: "  "}, {Heading: "B", Text: "two"},
	}}
	assert.Equal(t, "# A\n\none\n\n# B\n\ntwo\n\n", capmapsimilarity.Text(comp))
	assert.Empty(t, capmapsimilarity.Text(capmapcorpus.Competence{Name: "N"}))
}
