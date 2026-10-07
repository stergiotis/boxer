package play

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opengine"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
	"github.com/stergiotis/boxer/public/keelson/runtime/runstream"
)

// The person's Publish runs publish_result through the gesture path: the
// handler an agent's call runs, with no call behind it (ADR-0288
// §SD4).
func TestThePersonPublishesThroughTheGesturePath(t *testing.T) {
	fakeAppletDocs(t)
	l, h, reader := bundleLauncher(t)
	eng := opengine.New(playOps.Catalog(), h)
	ctx := app.NewStaticFrameContext(nil, nil)
	ctx.SetOperationsGesture(eng.Gesture)
	l.inner.gestureCtx = ctx
	eng.BeginFrame()
	l.inner.publish.last = l.lastPublish

	l.inner.publish.bundle = "mine"
	l.inner.personPublish()
	assert.Contains(t, l.inner.publishStatusLine(), "Not published: the window holds no result", "a refusal is shown, not dropped")

	mainIntResult(t, l, runstream.Terminal{}, 4, 5, 6)
	l.inner.publish.panes = "table, chart"
	l.inner.personPublish()
	last := waitPublished(t, l)
	require.Empty(t, last.Error)
	assert.Equal(t, "Published mine (revision 1, 3 rows)", l.inner.publishStatusLine())

	got, err := adhocdata.ResolveBundleRequest(reader, "mine", nil)
	require.NoError(t, err)
	assert.Contains(t, string(got.Document), "tabs: [table, chart]")
	var logged bool
	for _, e := range eng.Log() {
		if e.Op == opPublishResult && e.Writer == opwire.WriterPerson {
			logged = true
		}
	}
	assert.True(t, logged, "the engine logs the publish as the person's")
}

func TestSplitPanes(t *testing.T) {
	assert.Equal(t, []string{"table", "chart"}, splitPanes(" table, chart "))
	assert.Equal(t, []string{"a", "b"}, splitPanes("a b"))
	assert.Nil(t, splitPanes("  "))
}
