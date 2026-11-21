package importmove

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func plan() Plan {
	return Plan{
		Mappings: []Mapping{
			{From: "example.com/m/a/b", To: "example.com/m/x/b"},
			{From: "example.com/m/a/b/deep", To: "example.com/m/y/deep"},
			{From: "example.com/m/old/env", To: "example.com/m/old/grammarenv"},
		},
		Keep: []string{"example.com/m/a/b/stays"},
	}
}

func TestTargetRespectsSegmentsLongestPrefixAndKeep(t *testing.T) {
	p := plan()
	for in, want := range map[string]string{
		"example.com/m/a/b":        "example.com/m/x/b",
		"example.com/m/a/b/c":      "example.com/m/x/b/c",
		"example.com/m/a/b/deep/e": "example.com/m/y/deep/e",
		"example.com/m/old/env":    "example.com/m/old/grammarenv",
	} {
		got, ok := p.Target(in)
		require.True(t, ok, in)
		assert.Equal(t, want, got, in)
	}
	for _, in := range []string{"example.com/m/a/bc", "example.com/m/a/b/stays", "example.com/m/a/b/stays/sub", "example.com/m/a"} {
		_, ok := p.Target(in)
		assert.False(t, ok, in)
	}
}

// generateLine is split so go generate does not take the fixture's
// directive for one of this package's own.
const generateLine = "//go:" + "generate go run example.com/m/a/b/gen -out x.go"

const before = `package p

` + generateLine + `

import (
	"fmt"

	"example.com/m/a/b"
	"example.com/m/a/b/stays"
	"example.com/m/a/bc"
	"example.com/m/old/env"
	q "example.com/m/a/b/c"
)

const tmpl = "import \"example.com/m/a/b/c\""

func f() { fmt.Println(b.X, stays.Y, bc.Z, env.W, q.V, tmpl) }
`

const after = `package p

` + generateLine + `

import (
	"fmt"

	"example.com/m/a/b/stays"
	"example.com/m/a/bc"
	env "example.com/m/old/grammarenv"
	"example.com/m/x/b"
	q "example.com/m/x/b/c"
)

const tmpl = "import \"example.com/m/a/b/c\""

func f() { fmt.Println(b.X, stays.Y, bc.Z, env.W, q.V, tmpl) }
`

func TestRewriteFile(t *testing.T) {
	out, res, err := plan().RewriteFile("p.go", []byte(before))
	require.NoError(t, err)
	assert.Equal(t, 3, res.Rewrites)
	assert.Equal(t, []string{"example.com/m/old/grammarenv"}, res.Aliased, "the last segment changed, so the old name stays as an alias")
	assert.Equal(t, after, string(out))
	require.Len(t, res.Findings, 2)
	kinds := map[FindingKindE]string{}
	for _, f := range res.Findings {
		kinds[f.Kind] = f.Moved
	}
	assert.Equal(t, "example.com/m/a/b/gen", kinds[FindingKindGoGenerate])
	assert.Equal(t, "example.com/m/a/b/c", kinds[FindingKindStringLiteral])
}

func TestRunWritesAndScansTemplates(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "p.go"), []byte(before), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "x.tmpl"), []byte("line\nimport \"example.com/m/a/b\"\n"), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".hidden"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".hidden", "h.go"), []byte(before), 0o600))

	results, err := plan().Run(root, false)
	require.NoError(t, err)
	require.Len(t, results, 2, "the Go file and the template; hidden directories are skipped")
	unchanged, err := os.ReadFile(filepath.Join(root, "p.go"))
	require.NoError(t, err)
	assert.Equal(t, before, string(unchanged), "a dry run writes nothing")

	_, err = plan().Run(root, true)
	require.NoError(t, err)
	written, err := os.ReadFile(filepath.Join(root, "p.go"))
	require.NoError(t, err)
	assert.Equal(t, after, string(written))
	for _, r := range results {
		if filepath.Ext(r.Path) == ".tmpl" {
			require.Len(t, r.Findings, 1)
			assert.Equal(t, 2, r.Findings[0].Line)
			assert.Equal(t, FindingKindTemplate, r.Findings[0].Kind)
		}
	}
}

func TestValidate(t *testing.T) {
	require.Error(t, Plan{}.Validate())
	require.Error(t, Plan{Mappings: []Mapping{{From: "a/", To: "b"}}}.Validate())
	require.Error(t, Plan{Mappings: []Mapping{{From: "a", To: "a"}}}.Validate())
	require.NoError(t, plan().Validate())
}

func TestRunReportsAnUnparsableFileAndGoesOn(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "broken.go"), []byte("package p\nimport \"example.com/m/a/b\"\nfunc {"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "p.go"), []byte(before), 0o600))
	results, err := plan().Run(root, false)
	require.NoError(t, err)
	require.Len(t, results, 2)
	unparsed := 0
	for _, r := range results {
		if r.Unparsed != nil {
			unparsed++
		}
	}
	assert.Equal(t, 1, unparsed)
}
