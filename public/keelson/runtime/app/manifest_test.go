//go:build llm_generated_opus47

package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManifest_Validate_OK(t *testing.T) {
	m := Manifest{
		Id:      "org.test.ok",
		Display: "OK",
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
		"github.com/.../apps/widgets":            "widgets",
		"github.com/.../apps/widgets/table":      "table",
		"runtime.broker":                         "runtime_broker",
		"runtime.persist":                        "runtime_persist",
		"play":                                   "play",
		"a-b_c":                                  "a-b_c",
		"weird/name with spaces":                 "name_with_spaces",
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
