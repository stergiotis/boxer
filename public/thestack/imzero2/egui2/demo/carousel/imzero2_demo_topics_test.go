package demo

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	runtimeapp "github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// TestEveryRegisteredManifestIsClassifiable is ADR-0158's tree-wide gate.
// This package blank-imports every app in the repository, so its registry is
// the whole set; minting the applet corpus first adds the documents.
//
// It exists because of the §SD9 hazard: Manifest.Validate rejects an
// unregistered topic, but RegisterFactory handles a rejection by logging at
// Warn and dropping the app — so a typo costs an app its place in the
// launcher with nothing going red. The named TopicT type makes an in-tree
// typo a compile error, which covers the Go manifests; this test covers what
// the compiler cannot see, principally applet frontmatter.
func TestEveryRegisteredManifestIsClassifiable(t *testing.T) {
	mintCorpusOnce(t)

	manifests := runtimeapp.AllManifests()
	require.NotEmpty(t, manifests, "the carousel blank-imports every app; an empty registry means that broke")

	for _, m := range manifests {
		if m.Surface == runtimeapp.SurfaceWindowed {
			assert.NotEmpty(t, m.Topics,
				"%s: a windowed app with no topics is reachable from no launcher section (ADR-0158 §SD2)", m.Id)
		}
		for _, topic := range m.Topics {
			assert.True(t, topic.IsRegistered(),
				"%s: topic %q is not in the ADR-0158 §SD1 vocabulary", m.Id, topic)
		}
		assert.True(t, m.Kind.IsValid(), "%s: invalid Kind", m.Id)
		// Validate is what registration runs; assert it wholesale too, so a
		// rule added later is covered here without editing this test.
		assert.NoError(t, m.Validate(), "%s: manifest does not validate", m.Id)
	}
}

// TestProvenanceIsNotATopic pins the §SD4/§SD5 separation that the whole ADR
// turns on: the subject vocabulary must not readmit provenance words through
// the back door. "applet" and "demo" answer Kind; if they ever become topics,
// the axis collision this ADR removed is back.
func TestProvenanceIsNotATopic(t *testing.T) {
	for _, banned := range []string{"applet", "applets", "demo", "demos", "tools", "other"} {
		_, ok := runtimeapp.ParseTopic(banned)
		assert.False(t, ok, "%q classifies by provenance or is a catch-all; it must not be a topic", banned)
	}
}
