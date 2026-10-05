package tabhost

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/basemap"
)

func reportManifest(id app.AppIdT, caps ...app.SubjectFilter) (m app.Manifest) {
	return app.Manifest{Id: id, Version: "0.1.0", Display: string(id), Topics: []app.TopicT{app.TopicRuntime},
		Summary: "fixture", Surface: app.SurfaceWindowed, Caps: caps}
}

// With no in-tab services every declared subject is a gap, the basemap's is
// answered in the tab, and an app that declares nothing is listed with none.
func TestReportListsGapsAndAnswered(t *testing.T) {
	reg := app.NewRegistry()
	add := func(m app.Manifest) {
		require.NoError(t, reg.RegisterFactory(m, func() (a app.AppI, err error) {
			return app.NewLegacyFuncApp(m, func() (e error) { return })
		}))
	}
	add(reportManifest("org.test.b", app.SubjectFilter{Pattern: "llm.*", Direction: app.CapDirectionPub, Reason: "ask the model"}))
	add(reportManifest("org.test.a", append(basemap.ClientCaps("tiles"),
		app.SubjectFilter{Pattern: "fs.dialog.read", Direction: app.CapDirectionPub, Reason: "open a file"})...))
	add(reportManifest("org.test.c"))

	rep := Report(reg, Services{})
	require.Equal(t, []string{"org.test.a", "org.test.b", "org.test.c"}, rep.Apps)
	require.Len(t, rep.Gaps, 2)
	require.Equal(t, Gap{App: "org.test.a", Pattern: "fs.dialog.read", Direction: "pub", Reason: "open a file"}, rep.Gaps[0])
	require.Equal(t, "org.test.b", rep.Gaps[1].App)
	require.Len(t, rep.Answered, 1)
	require.Equal(t, basemap.ClientCaps("")[0].Pattern, rep.Answered[0].Pattern)
}
