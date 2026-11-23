package launcher

import (
	"testing"
	"testing/fstest"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
)

func TestOriginLabel(t *testing.T) {
	assert.Equal(t, "example.com/dep@v1.2.3",
		originLabel(app.Origin{Package: "example.com/dep/apps/b", Module: "example.com/dep", ModuleVersion: "v1.2.3"}))
	assert.Equal(t, "example.com/main",
		originLabel(app.Origin{Package: "example.com/main/apps/a", Module: "example.com/main", ModuleVersion: "(devel)", Main: true}),
		"a local build has no version to show")
	assert.Equal(t, "example.com/dep@v1.2.3 → ../dep",
		originLabel(app.Origin{Module: "example.com/dep", ModuleVersion: "v1.2.3", Replace: "../dep"}))
	assert.Equal(t, "example.com/x/pkg", originLabel(app.Origin{Package: "example.com/x/pkg"}),
		"without build information the package is the answer")
	assert.Equal(t, "", originLabel(app.Origin{}))
}

// Help lands on the app's book: the overview when the book has one, else its
// first document, queued before the Help center is raised.
func TestOpenHelp_QueuesTheDocumentBeforeTheRaise(t *testing.T) {
	lib := help.NewLibrary()
	withOverview, err := help.NewBook("test.withoverview", fstest.MapFS{
		"howto/a.md":  {Data: []byte("# A\n\nbody\n")},
		"overview.md": {Data: []byte("# Overview\n\nbody\n")},
	})
	require.NoError(t, err)
	require.NoError(t, lib.Register(withOverview))
	without, err := help.NewBook("test.without", fstest.MapFS{
		"b.md": {Data: []byte("# B\n\nbody\n")},
		"c.md": {Data: []byte("# C\n\nbody\n")},
	})
	require.NoError(t, err)
	require.NoError(t, lib.Register(without))

	assert.Equal(t, "overview", overviewDoc(lib, "test.withoverview"))
	assert.Equal(t, "b", overviewDoc(lib, "test.without"), "no overview: the first document in path order")
	assert.Equal(t, "", overviewDoc(lib, "test.nobook"))

	host := &recordingHost{}
	inst := New(app.NewRegistry(), host, lib, zerolog.Nop())
	inst.SetHelpApp("test.help")
	var queued []string
	inst.SetHelpRef(func(ref help.RefT) {
		queued = append(queued, string(ref.AppId)+"/"+ref.Doc)
		host.calls = append(host.calls, "queued")
	})
	inst.openHelp(overviewDoc(lib, "test.withoverview"), "test.withoverview")
	inst.openHelp("", "test.nobook")
	assert.Equal(t, []string{"test.withoverview/overview"}, queued, "an app without a book queues nothing")
	assert.Equal(t, []string{"queued", "raise:test.help", "raise:test.help"}, host.calls,
		"the ref is queued before the raise, so a fresh window opens on it")
}
