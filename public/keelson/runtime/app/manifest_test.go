package app

import (
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManifest_Validate_OK(t *testing.T) {
	m := Manifest{
		Id:      "org.test.ok",
		Display: "OK",
		Topics:  []TopicT{TopicRuntime},
		Surface: SurfaceWindowed,
	}
	err := m.Validate()
	require.NoError(t, err)
}

func TestManifest_Validate_Headless_OK(t *testing.T) {
	m := Manifest{
		Id:      "org.test.headless",
		Display: "Headless tool",
		Surface: SurfaceHeadless,
	}
	err := m.Validate()
	require.NoError(t, err)
}

func TestManifest_Validate_EmptyId(t *testing.T) {
	m := Manifest{
		Display: "X",
		Surface: SurfaceWindowed,
	}
	err := m.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty Id")
}

func TestManifest_Validate_EmptyDisplay(t *testing.T) {
	m := Manifest{
		Id:      "org.test.x",
		Surface: SurfaceWindowed,
	}
	err := m.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty Display")
	assert.Contains(t, err.Error(), "org.test.x")
}

func TestManifest_Validate_UnspecifiedSurface(t *testing.T) {
	m := Manifest{
		Id:      "org.test.x",
		Display: "X",
	}
	err := m.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Surface must be set")
	assert.Contains(t, err.Error(), "org.test.x")
}

func TestManifest_Validate_WorkingsetNeedsLaunchKind(t *testing.T) {
	m := Manifest{
		Id:         "org.test.x",
		Display:    "X",
		Surface:    SurfaceWindowed,
		Topics:     []TopicT{TopicRuntime},
		Workingset: true,
	}
	err := m.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Workingset requires a non-empty LaunchKind")
	assert.Contains(t, err.Error(), "org.test.x")
}

func TestManifest_Validate_WorkingsetWithLaunchKind_OK(t *testing.T) {
	m := Manifest{
		Id:         "org.test.x",
		Display:    "X",
		Surface:    SurfaceWindowed,
		Topics:     []TopicT{TopicRuntime},
		LaunchKind: "xLaunch",
		Workingset: true,
	}
	require.NoError(t, m.Validate())
}

func TestManifest_Validate_LaunchKindWithoutWorkingset_OK(t *testing.T) {
	// Launchable without participating: the common ADR-0135 shape.
	m := Manifest{
		Id:         "org.test.x",
		Display:    "X",
		Surface:    SurfaceWindowed,
		Topics:     []TopicT{TopicRuntime},
		LaunchKind: "xLaunch",
	}
	require.NoError(t, m.Validate())
}

func TestLaunchReasonE_String(t *testing.T) {
	cases := map[LaunchReasonE]string{
		LaunchReasonPlain:   "plain",
		LaunchReasonCaller:  "caller",
		LaunchReasonRestore: "restore",
	}
	for r, want := range cases {
		assert.Equal(t, want, r.String(), "for %d", uint8(r))
	}
}

func TestStaticMountContext_LaunchReason_DefaultsPlain(t *testing.T) {
	mc := NewStaticMountContext("org.test.x", zerolog.Nop(), nil, nil, nil)
	assert.Equal(t, LaunchReasonPlain, mc.LaunchReason())
	mc.SetLaunchReason(LaunchReasonRestore)
	assert.Equal(t, LaunchReasonRestore, mc.LaunchReason())
}

func TestManifest_WindowTitle_TitleWins(t *testing.T) {
	m := Manifest{Title: "Hacker News", Display: "HN"}
	assert.Equal(t, "Hacker News", m.WindowTitle())
}

func TestManifest_WindowTitle_DisplayFallback(t *testing.T) {
	m := Manifest{Display: "Regex Explorer"}
	assert.Equal(t, "Regex Explorer", m.WindowTitle())
}

func TestManifest_WindowTitle_IconPrefix(t *testing.T) {
	m := Manifest{Title: "Top", Icon: "📊"}
	assert.Equal(t, "📊 Top", m.WindowTitle())
}

func TestManifest_WindowTitle_IconWithDisplayFallback(t *testing.T) {
	m := Manifest{Display: "HN", Icon: "🗞"}
	assert.Equal(t, "🗞 HN", m.WindowTitle())
}

func TestManifest_WindowTitle_IconOnly(t *testing.T) {
	m := Manifest{Icon: "?"}
	assert.Equal(t, "?", m.WindowTitle())
}

func TestManifest_WindowTitle_Empty(t *testing.T) {
	assert.Equal(t, "", Manifest{}.WindowTitle())
}

func TestSurfaceE_String(t *testing.T) {
	cases := map[SurfaceE]string{
		SurfaceHeadless:    "headless",
		SurfaceWindowed:    "windowed",
		SurfaceUnspecified: "unspecified",
	}
	for s, want := range cases {
		assert.Equal(t, want, s.String(), "for %d", uint8(s))
	}
}

func TestCapDirectionE_String(t *testing.T) {
	cases := map[CapDirectionE]string{
		CapDirectionPub:         "pub",
		CapDirectionSub:         "sub",
		CapDirectionBoth:        "pub+sub",
		CapDirectionUnspecified: "unspecified",
	}
	for d, want := range cases {
		assert.Equal(t, want, d.String(), "for %d", uint8(d))
	}
}

func TestAppIdT_SubjectAlias(t *testing.T) {
	cases := map[AppIdT]string{
		"github.com/stergiotis/boxer/apps/play": "play",
		"github.com/.../apps/widgets":           "widgets",
		"github.com/.../apps/widgets/table":     "table",
		"runtime.broker":                        "runtime_broker",
		"runtime.persist":                       "runtime_persist",
		"play":                                  "play",
		"a-b_c":                                 "a-b_c",
		"weird/name with spaces":                "name_with_spaces",
	}
	for id, want := range cases {
		assert.Equal(t, want, id.SubjectAlias(), "id=%q", id)
	}
}

func TestAllSurfaces_Contents(t *testing.T) {
	assert.Contains(t, AllSurfaces, SurfaceHeadless)
	assert.Contains(t, AllSurfaces, SurfaceWindowed)
	assert.NotContains(t, AllSurfaces, SurfaceUnspecified)
	assert.Len(t, AllSurfaces, 2)
}

// --- ADR-0158 classification ------------------------------------------------

func TestManifest_Validate_WindowedNeedsTopics(t *testing.T) {
	m := Manifest{
		Id:      "org.test.x",
		Display: "X",
		Surface: SurfaceWindowed,
	}
	err := m.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "declares no Topics")
	assert.Contains(t, err.Error(), "org.test.x")
}

func TestManifest_Validate_HeadlessNeedsNoTopics(t *testing.T) {
	// A headless app has no launcher presence, so it has nothing to be
	// sectioned into (ADR-0158 §SD2).
	m := Manifest{
		Id:      "org.test.x",
		Display: "X",
		Surface: SurfaceHeadless,
	}
	require.NoError(t, m.Validate())
}

func TestManifest_Validate_UnregisteredTopicRefused(t *testing.T) {
	m := Manifest{
		Id:      "org.test.x",
		Display: "X",
		Surface: SurfaceWindowed,
		Topics:  []TopicT{TopicRuntime, TopicT("nonsense")},
	}
	err := m.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unregistered topic")
	assert.Contains(t, err.Error(), "nonsense")
}

func TestManifest_Validate_InvalidKindRefused(t *testing.T) {
	m := Manifest{
		Id:      "org.test.x",
		Display: "X",
		Surface: SurfaceWindowed,
		Topics:  []TopicT{TopicRuntime},
		Kind:    KindE(99),
	}
	err := m.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid Kind")
}

func TestManifest_Validate_KeywordsUngoverned(t *testing.T) {
	// ADR-0158 §SD4: keywords are never rendered as structure, so there is
	// no vocabulary for them to violate. Anything goes.
	m := Manifest{
		Id:       "org.test.x",
		Display:  "X",
		Surface:  SurfaceWindowed,
		Topics:   []TopicT{TopicRuntime},
		Keywords: []string{"cpu", "HTOP", "not a topic", "runtime"},
	}
	require.NoError(t, m.Validate())
}

func TestTopicT_IsRegistered(t *testing.T) {
	for _, tp := range AllTopics {
		assert.True(t, tp.IsRegistered(), "%q is in AllTopics", tp)
	}
	assert.False(t, TopicT("").IsRegistered(), "the empty topic is malformed, not uncategorised")
	assert.False(t, TopicT("Runtime").IsRegistered(), "the vocabulary is lowercase")
	assert.False(t, TopicT("nonsense").IsRegistered())
}

func TestParseTopic(t *testing.T) {
	got, ok := ParseTopic("code")
	require.True(t, ok)
	assert.Equal(t, TopicCode, got)

	_, ok = ParseTopic("nonsense")
	assert.False(t, ok, "an unknown token must not synthesise a vocabulary member")
}

func TestKindE_String(t *testing.T) {
	assert.Equal(t, "app", KindApp.String(), "the zero value reads as the common case")
	assert.Equal(t, "applet", KindApplet.String())
	assert.Equal(t, "demo", KindDemo.String())
}
