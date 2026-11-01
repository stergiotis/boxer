package mdgraph

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/markdown/mdextract"
)

func corpus(files ...string) (notes []Note) {
	for i := 0; i+1 < len(files); i += 2 {
		notes = append(notes, Note{Path: files[i], Doc: mdextract.Extract([]byte(files[i+1]))})
	}
	return
}

func resolveOne(t *testing.T, notes []Note, src int32, body string) (string, ResolutionE, string) {
	t.Helper()
	doc := mdextract.Extract([]byte(body))
	require.Len(t, doc.Links, 1)
	r := NewResolver(notes)
	dst, res, key := r.Resolve(src, doc.Links[0])
	if dst < 0 {
		return "", res, key
	}
	return notes[dst].Path, res, key
}

func TestResolveWikilink(t *testing.T) {
	notes := corpus(
		"doc/a/index.md", "",
		"doc/b/index.md", "",
		"doc/b/Topic.md", "---\naliases: [Subject, other name]\n---\n",
		"README.md", "",
	)
	for _, tc := range []struct {
		name string
		src  int32
		body string
		want string
		res  ResolutionE
		key  string
	}{
		{"by name, case ignored", 0, "[[topic]]", "doc/b/Topic.md", ResolutionNote, ""},
		{"with extension", 0, "[[Topic.md]]", "doc/b/Topic.md", ResolutionNote, ""},
		{"with heading", 0, "[[Topic#Part]]", "doc/b/Topic.md", ResolutionNote, ""},
		{"alias", 0, "[[other name]]", "doc/b/Topic.md", ResolutionNote, ""},
		{"ambiguous: the closer directory", 0, "[[index]]", "doc/a/index.md", ResolutionNote, ""},
		{"ambiguous: the closer directory, other side", 1, "[[index]]", "doc/b/index.md", ResolutionNote, ""},
		{"ambiguous from the root: the first in byte order", 3, "[[index]]", "doc/a/index.md", ResolutionNote, ""},
		{"path suffix", 3, "[[b/index]]", "doc/b/index.md", ResolutionNote, ""},
		{"embed", 0, "![[Topic]]", "doc/b/Topic.md", ResolutionNote, ""},
		{"unresolved", 0, "[[Missing Page]]", "", ResolutionUnresolved, "missing page"},
		{"attachment", 0, "![[diagram.png]]", "", ResolutionSkipped, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, res, key := resolveOne(t, notes, tc.src, tc.body)
			require.Equal(t, tc.res, res)
			require.Equal(t, tc.want, got)
			require.Equal(t, tc.key, key)
		})
	}
}

func TestResolveInline(t *testing.T) {
	notes := corpus(
		"doc/adr/0001-x.md", "",
		"doc/adr/0002-y.md", "",
		"doc/README.md", "",
	)
	for _, tc := range []struct {
		name string
		body string
		want string
		res  ResolutionE
	}{
		{"sibling", "[y](0002-y.md)", "doc/adr/0002-y.md", ResolutionNote},
		{"without extension", "[y](0002-y)", "doc/adr/0002-y.md", ResolutionNote},
		{"parent", "[r](../README.md#intro)", "doc/README.md", ResolutionNote},
		{"root-relative", "[r](/doc/README.md)", "doc/README.md", ResolutionNote},
		{"vault-relative", "[r](doc/README.md)", "doc/README.md", ResolutionNote},
		{"bare name falls back to the page rule", "[r](README.md)", "doc/README.md", ResolutionNote},
		{"percent-encoded", "[y](0002%2Dy.md)", "doc/adr/0002-y.md", ResolutionNote},
		{"missing", "[m](../gone/x.md)", "", ResolutionUnresolved},
		{"external", "[e](https://example.com/a.md)", "", ResolutionSkipped},
		{"fragment only", "[s](#section)", "", ResolutionSkipped},
		{"image", "![i](pic.svg)", "", ResolutionSkipped},
		{"other file", "[g](../../main.go)", "", ResolutionSkipped},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, res, _ := resolveOne(t, notes, 0, tc.body)
			require.Equal(t, tc.res, res)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestBuild(t *testing.T) {
	notes := corpus(
		"a.md", "---\ntags: [Project/Alpha]\n---\nSee [[b]] and [[b|again]], ![[b]], [[a]], [[ghost]] and #todo.\n",
		"b.md", "Back to [a](a.md). #TODO\n",
		"c.md", "Alone.\n",
	)
	g := Build(notes, Options{Tags: true, Unresolved: true})
	require.Equal(t, []string{"a.md", "b.md", "c.md"}, g.NodeKey[:3])

	type edge struct {
		src, dst string
		kind     EdgeKindE
		weight   int32
	}
	var got []edge
	for j := range g.EdgeSrc {
		got = append(got, edge{g.NodeKey[g.EdgeSrc[j]], g.NodeKey[g.EdgeDst[j]], g.EdgeKind[j], g.EdgeWeight[j]})
	}
	require.ElementsMatch(t, []edge{
		{"a.md", "b.md", EdgeKindLink, 2},
		{"a.md", "b.md", EdgeKindEmbed, 1},
		{"a.md", "ghost", EdgeKindLink, 1},
		{"a.md", "todo", EdgeKindTag, 1},
		{"a.md", "project/alpha", EdgeKindTag, 1},
		{"b.md", "a.md", EdgeKindLink, 1},
		{"b.md", "todo", EdgeKindTag, 1},
	}, got)

	ghost, ok := g.GetNode(NodeKindUnresolved, "ghost")
	require.True(t, ok)
	require.Equal(t, NodeKindUnresolved, g.NodeKind[ghost])
	todo, ok := g.GetNode(NodeKindTag, "todo")
	require.True(t, ok)
	deg := g.Degrees()
	require.EqualValues(t, 2, deg[todo])
	require.EqualValues(t, 0, deg[2])

	bare := Build(notes, Options{})
	require.Len(t, bare.NodeKey, 3)
	require.Len(t, bare.EdgeSrc, 3)
}

func TestBuildIsDeterministic(t *testing.T) {
	notes := corpus("x.md", "[[y]] [[z]] #t1 #t2", "y.md", "[[z]] #t2", "z.md", "")
	a := Build(notes, Options{Tags: true, Unresolved: true})
	b := Build(notes, Options{Tags: true, Unresolved: true})
	require.Equal(t, a.NodeKey, b.NodeKey)
	require.Equal(t, a.EdgeSrc, b.EdgeSrc)
	require.Equal(t, a.EdgeDst, b.EdgeDst)
}
