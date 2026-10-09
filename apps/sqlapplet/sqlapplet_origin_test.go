package sqlapplet

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/code/analysis/sccapplet"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/scctree"
)

const originReadDoc = "---\ntitle: \"Origin read\"\nsummary: \"A read-class applet served by the page's origin.\"\n---\n\n# Origin read\n\n```sql\nSELECT 1 AS x\n```\n"

const originEgressDoc = "---\ntitle: \"Origin egress\"\nsummary: \"A read that reaches past the endpoint.\"\n---\n\n```sql\nSELECT * FROM url('http://example.invalid/x.csv', 'CSV')\n```\n"

func newOriginServer(t *testing.T, docs map[string]string) (srv *httptest.Server) {
	t.Helper()
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		doc, ok := docs[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(doc))
	}))
	t.Cleanup(srv.Close)
	return
}

func TestLoadOriginAppletMintsReadClass(t *testing.T) {
	srv := newOriginServer(t, map[string]string{"/applets/origin-read.md": originReadDoc})
	reg := app.NewRegistry()
	id, err := loadOriginApplet(context.Background(), reg, srv.Client(), srv.URL, "/applets/origin-read.md")
	require.NoError(t, err)
	assert.Equal(t, app.AppIdT(appletIdPrefix+"origin-read"), id)
	m, ok := reg.LookupManifest(id)
	require.True(t, ok)
	assert.Equal(t, "Origin read", m.Title)
	assert.Equal(t, storeDefaultTopics, m.Topics)
}

func TestLoadOriginAppletRefuses(t *testing.T) {
	srv := newOriginServer(t, map[string]string{
		"/origin-read.md":   originReadDoc,
		"/origin-egress.md": originEgressDoc,
		"/prose.md":         "---\ntitle: \"Prose\"\nsummary: \"No fence.\"\n---\n\nJust prose.\n",
	})
	cases := map[string]string{
		"read-egress":     "/origin-egress.md",
		"no sql fence":    "/prose.md",
		"not served":      "/missing.md",
		"other scheme":    "javascript:alert(1)//x.md",
		"absolute url":    "http://elsewhere.invalid/origin-read.md",
		"scheme-relative": "//elsewhere.invalid/origin-read.md",
		"not markdown":    "/origin-read.txt",
		"backslash":       `/\elsewhere.invalid/origin-read.md`,
		"query":           "/origin-read.md?x=1",
		"fragment":        "/origin-read.md#x",
	}
	for name, p := range cases {
		t.Run(name, func(t *testing.T) {
			reg := app.NewRegistry()
			id, err := loadOriginApplet(context.Background(), reg, srv.Client(), srv.URL, p)
			require.Error(t, err)
			assert.Empty(t, id)
		})
	}
}

func TestLoadOriginAppletResolvesAgainstThePage(t *testing.T) {
	srv := newOriginServer(t, map[string]string{
		"/demo/applets/origin-read.md": originReadDoc,
		"/origin-read.md":              originReadDoc,
	})
	// the page is served from /demo/, as a published bundle is
	base := srv.URL + "/demo/"
	for _, p := range []string{"applets/origin-read.md", "./applets/origin-read.md", "/demo/applets/origin-read.md", "../origin-read.md"} {
		t.Run(p, func(t *testing.T) {
			id, err := loadOriginApplet(context.Background(), app.NewRegistry(), srv.Client(), base, p)
			require.NoError(t, err)
			assert.Equal(t, app.AppIdT(appletIdPrefix+"origin-read"), id)
		})
	}
}

func TestLoadOriginAppletRefusesNonReadClass(t *testing.T) {
	srv := newOriginServer(t, map[string]string{"/origin-egress.md": originEgressDoc})
	reg := app.NewRegistry()
	id, err := loadOriginApplet(context.Background(), reg, srv.Client(), srv.URL, "/origin-egress.md")
	require.ErrorContains(t, err, "must be read-class")
	assert.Empty(t, id)
	_, minted := reg.LookupManifest(app.AppIdT(appletIdPrefix + "origin-egress"))
	assert.False(t, minted)
}

func TestLoadOriginAppletCommittedWins(t *testing.T) {
	srv := newOriginServer(t, map[string]string{"/origin-read.md": originReadDoc})
	reg := app.NewRegistry()
	_, err := loadOriginApplet(context.Background(), reg, srv.Client(), srv.URL, "/origin-read.md")
	require.NoError(t, err)
	_, err = loadOriginApplet(context.Background(), reg, srv.Client(), srv.URL, "/origin-read.md")
	require.Error(t, err)
}

func TestOriginDocURLRefusesNonHttpOrigin(t *testing.T) {
	for _, origin := range []string{"", "file:///srv", "keelson", "http://"} {
		_, err := originDocURL(origin, "/a.md")
		assert.Error(t, err, origin)
	}
}

// A document `boxer code analysis sccapplet` writes is one a tab admits: it
// parses, its buffer is read-class, and it names the panes it draws in.
func TestSccAppletDocumentIsAdmitted(t *testing.T) {
	doc, _, err := sccapplet.Compose([]scctree.SccGroup{{Name: "Go", Files: []scctree.SccFile{
		{Filename: "a.go", Location: "./public/a/a.go", Code: 100, Complexity: 20},
	}}}, sccapplet.Options{Depth: 4, Revision: "0123abc"})
	require.NoError(t, err)
	def, err := ParseDocSource(originBookID, "repo-complexity.md", doc)
	require.NoError(t, err)
	require.NotNil(t, def)
	assert.Equal(t, analysis.QuerySecurityRead, def.Class)
	require.Len(t, def.Tabs, 3)
	assert.Equal(t, "treemap", def.Tabs[0].ID)
}
