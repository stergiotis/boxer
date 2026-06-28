package swisstopo

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testTilesDir resolves the swissALTI3D tile directory from
// $SWISSTOPO_TILES_DIR, falling back to ~/data/swisstopo. Tests skip when
// the directory is absent, so the fallback only matters on a machine that
// has tiles staged there.
func testTilesDir() (dir string) {
	if d := os.Getenv("SWISSTOPO_TILES_DIR"); d != "" {
		return d
	}
	home, homeErr := os.UserHomeDir()
	if homeErr != nil {
		return ""
	}
	return filepath.Join(home, "data", "swisstopo")
}

func tilesAvailable(t *testing.T) {
	t.Helper()
	dir := testTilesDir()
	_, statErr := os.Stat(dir)
	if statErr != nil {
		t.Skipf("test tiles not available at %s", dir)
	}
}

func tilePath(t *testing.T, eKm int32, nKm int32) string {
	t.Helper()
	// Filename pattern: swissalti3d_YYYY_EEEE-NNNN_2_2056_5728.tif — year varies.
	pattern := filepath.Join(testTilesDir(), fmt.Sprintf("swissalti3d_*_%d-%d_2_2056_5728.tif", eKm, nKm))
	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) == 0 {
		t.Skipf("tile %d-%d not found", eKm, nKm)
	}
	return matches[0]
}

func TestReadSwissALTI3DTile_BernGrid(t *testing.T) {
	tilesAvailable(t)
	path := tilePath(t, 2600, 1200)

	pixels, err := readSwissALTI3DTile(path)
	require.NoError(t, err)
	assert.Equal(t, int(pixelWidth*pixelHeight), len(pixels), "pixel count must be 500*500")

	// Reference elevations verified against swisstopo REST API
	// https://api3.geo.admin.ch/rest/services/height?easting=E&northing=N
	type pixelRef struct {
		row, col int32
		apiElev  float32 // from swisstopo height API
		desc     string
	}
	refs := []pixelRef{
		{row: 250, col: 250, apiElev: 538.7, desc: "center (Bern area)"},
		{row: 0, col: 0, apiElev: 561.9, desc: "NW corner"},
		{row: 0, col: 499, apiElev: 555.3, desc: "NE corner"},
		{row: 499, col: 0, apiElev: 555.4, desc: "SW corner"},
		{row: 499, col: 499, apiElev: 519.6, desc: "SE corner"},
	}

	for _, ref := range refs {
		t.Run(ref.desc, func(t *testing.T) {
			got := pixels[ref.row*pixelWidth+ref.col]
			// API returns DTM2 at ~1m resolution, our tiles are 2m COG.
			// Allow 2m tolerance for interpolation differences.
			assert.InDelta(t, ref.apiElev, got, 2.0,
				"pixel[%d,%d] = %.2f, API = %.1f", ref.row, ref.col, got, ref.apiElev)
		})
	}
}

func TestReadSwissALTI3DTile_ElevationRange(t *testing.T) {
	tilesAvailable(t)
	path := tilePath(t, 2600, 1200)

	pixels, err := readSwissALTI3DTile(path)
	require.NoError(t, err)

	// Bern area elevations must be in a sane range
	var minElev float32 = 9999
	var maxElev float32 = -9999
	for _, v := range pixels {
		if v < minElev {
			minElev = v
		}
		if v > maxElev {
			maxElev = v
		}
	}

	t.Logf("Bern tile elevation range: %.1f – %.1f m", minElev, maxElev)
	assert.Greater(t, minElev, float32(400.0), "min elevation in Bern tile must be > 400m")
	assert.Less(t, maxElev, float32(800.0), "max elevation in Bern tile must be < 800m")
}

func TestReadSwissALTI3DTile_MountainTile(t *testing.T) {
	tilesAvailable(t)
	// Pilatus area tile — should contain high elevations
	path := tilePath(t, 2661, 1202)

	pixels, err := readSwissALTI3DTile(path)
	require.NoError(t, err)

	var maxElev float32 = -9999
	for _, v := range pixels {
		if v > maxElev {
			maxElev = v
		}
	}

	t.Logf("Pilatus tile max elevation: %.1f m", maxElev)
	assert.Greater(t, maxElev, float32(2000.0), "Pilatus tile must contain elevations > 2000m")
}
