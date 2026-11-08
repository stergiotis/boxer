package scene

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/thestack/imzero2/scene/layoutcheck"
)

const matrixDoc = "---\n" +
	"type: reference\n" +
	"scene:\n" +
	"  launch: play\n" +
	"  env: {BOXER_PLAY_AUTORUN: \"1\"}\n" +
	"  matrix:\n" +
	"    sizes: [1400x1000, 800X600]\n" +
	"    themes: [dark, light]\n" +
	"  layoutExceptions:\n" +
	"    - invariant: hit-size\n" +
	"      name: Close window\n" +
	"      reason: egui's window chrome\n" +
	"    - invariant: overlap\n" +
	"      capture: table.png\n" +
	"      id: 16890102277250500721\n" +
	"      reason: drawn over on purpose\n" +
	"---\n\n" +
	"# A matrix\n\n" +
	"```jsonl trace\n" +
	"{\"do\":\"wait\",\"name\":\"Run\"}\n" +
	"{\"do\":\"capture\",\"text\":\"table.png\",\"sidecars\":[\"tree\"]}\n" +
	"```\n"

func TestMatrixVariants(t *testing.T) {
	doc, err := ParseDoc("x/grid.scene.md", []byte(matrixDoc))
	require.NoError(t, err)
	require.Len(t, doc.Spec.LayoutExceptions, 2)
	assert.Equal(t, uint64(16890102277250500721), doc.Spec.LayoutExceptions[1].ID)

	vs := doc.Variants()
	require.Len(t, vs, 4)
	names := make([]string, 0, len(vs))
	for _, v := range vs {
		names = append(names, v.Name)
		assert.True(t, v.Spec.Matrix.IsEmpty())
		assert.Equal(t, "1", v.Spec.Env["BOXER_PLAY_AUTORUN"])
	}
	assert.Equal(t, []string{"grid-1400x1000-dark", "grid-1400x1000-light", "grid-800x600-dark", "grid-800x600-light"}, names)

	v := vs[3]
	assert.Equal(t, "800X600", v.Spec.Size)
	assert.Equal(t, "fresh", v.Spec.Env["IMZERO2_THEME"])
	assert.Equal(t, "dark", vs[0].Spec.Env["IMZERO2_THEME"])
	assert.Equal(t, []string{"table-800x600-light"}, v.Captures())
	assert.Equal(t, "table-800x600-light.tree.jsonl", v.CaptureFiles()[0].Sidecars[0])
	assert.Equal(t, "table-800x600-light", v.Spec.LayoutExceptions[1].Capture)
	assert.Empty(t, v.Spec.LayoutExceptions[0].Capture)

	// The original is untouched.
	assert.Equal(t, []string{"table.png"}, doc.Captures())
	assert.Empty(t, doc.Spec.Env["IMZERO2_THEME"])
	assert.Equal(t, "table.png", doc.Spec.LayoutExceptions[1].Capture)
}

func TestMatrixOneAxis(t *testing.T) {
	src := "---\nscene:\n  launch: play\n  size: 900x700\n  matrix: {themes: [light]}\n---\n"
	doc, err := ParseDoc("x/one.scene.md", []byte(src))
	require.NoError(t, err)
	vs := doc.Variants()
	require.Len(t, vs, 1)
	assert.Equal(t, "one-light", vs[0].Name)
	assert.Equal(t, "900x700", vs[0].Spec.Size)
	assert.Equal(t, []string{"one-light"}, vs[0].Captures())

	plain, err := ParseDoc("x/plain.scene.md", []byte("---\nscene:\n  launch: play\n---\n"))
	require.NoError(t, err)
	assert.Equal(t, []*Doc{plain}, plain.Variants(), "no matrix, no change")
}

func TestMatrixRejects(t *testing.T) {
	for name, scene := range map[string]string{
		"size and sizes":      "  size: 900x700\n  matrix: {sizes: [800x600]}\n",
		"bad size":            "  matrix: {sizes: [wide]}\n",
		"unknown theme":       "  matrix: {themes: [fresh]}\n",
		"theme in env":        "  env: {IMZERO2_THEME: fresh}\n  matrix: {themes: [dark]}\n",
		"duplicate":           "  matrix: {themes: [dark, dark]}\n",
		"exception no reason": "  layoutExceptions: [{invariant: overlap, name: Run}]\n",
		"misspelt key":        "  matrix: {size: [800x600]}\n",
	} {
		_, err := ParseDoc("x/bad.scene.md", []byte("---\nscene:\n  launch: play\n"+scene+"---\n"))
		assert.Error(t, err, name)
	}
}

func TestCheckLayout(t *testing.T) {
	out := t.TempDir()
	src := "---\nscene:\n  launch: play\n  size: 1400x1000\n" +
		"  layoutExceptions:\n" +
		"    - {invariant: hit-size, role: button, contains: Pin, reason: a glyph button beside the title}\n" +
		"---\n\n```jsonl trace\n" +
		"{\"do\":\"capture\",\"text\":\"wide\",\"sidecars\":[\"tree\"]}\n" +
		"{\"do\":\"resize\",\"w\":1000,\"h\":800}\n" +
		"{\"do\":\"capture\",\"text\":\"narrow\",\"sidecars\":[\"tree\"]}\n" +
		"{\"do\":\"capture\",\"text\":\"pictureonly\"}\n" +
		"{\"do\":\"capture\",\"text\":\"absent\",\"sidecars\":[\"tree\"]}\n" +
		"```\n"
	doc, err := ParseDoc("x/layout.scene.md", []byte(src))
	require.NoError(t, err)
	tree := `{"id":1,"role":"button","name":"Export","value":"","cx":1150,"cy":20,"x":1100,"y":10,"w":100,"h":20,"flags":0,"depth":0}
{"id":2,"role":"button","name":"Pin","value":"","cx":900,"cy":47,"x":893,"y":40,"w":13,"h":18,"flags":0,"depth":0}
`
	require.NoError(t, os.WriteFile(filepath.Join(out, "wide.tree.jsonl"), []byte(tree), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(out, "narrow.tree.jsonl"), []byte(tree), 0o644))

	rep, err := CheckLayout(doc, out)
	require.NoError(t, err)
	assert.Equal(t, []string{"wide", "narrow"}, rep.Checked)
	assert.Equal(t, []string{"absent.tree.jsonl"}, rep.Missing)
	// Export fits 1400 and not the 1000 the resize left.
	require.Len(t, rep.Findings, 1)
	assert.Equal(t, "narrow", rep.Findings[0].Capture)
	assert.Equal(t, layoutcheck.InvariantOutside, rep.Findings[0].Invariant)
	assert.Len(t, rep.Excused, 2)
	require.Error(t, rep.Err())
	assert.Contains(t, PlainError(rep.Err()), `narrow: outside  button "Export"`)
	assert.Equal(t, "layout: 2 checked, 2 excused", rep.Summary())
}
