package sqlapplet

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
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
		"relative path":   "origin-read.md",
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
