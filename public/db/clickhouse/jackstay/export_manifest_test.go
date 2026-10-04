package jackstay

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An export that will remove files a complete manifest lists first saves the
// manifest as incomplete, so a pack whose export stops part-way is not read
// as complete with chunks missing.
func TestBeginExport_IncompleteBeforeRemoving(t *testing.T) {
	now := time.Unix(100, 0)
	for _, restart := range []bool{false, true} {
		dir, m := writeTestPack(t, "native-bytes", 2, 3, 3)
		inv := Inventory{Server: m.Server}
		plan := Plan{Selection: m.Selection}
		got, resumed, err := beginExport(dir, m.Source, &inv, &plan, ExportRequest{Restart: restart}, now)
		require.NoError(t, err)
		assert.Equal(t, !restart, resumed)
		assert.False(t, got.Complete)
		onDisk, err := LoadPackManifest(dir)
		require.NoError(t, err)
		assert.False(t, onDisk.Complete, "restart=%v", restart)
		_, err = OpenPack(dir)
		assert.ErrorContains(t, err, "not complete")
		_, err = os.Stat(filepath.Join(dir, m.Tables[0].Chunks[0].File))
		if restart {
			assert.ErrorIs(t, err, os.ErrNotExist, "a restart removes the earlier export's files")
		} else {
			assert.NoError(t, err)
		}
	}

	// A refused request leaves the pack as it was.
	dir, m := writeTestPack(t, "native-bytes", 2, 3, 3)
	inv := Inventory{Server: m.Server}
	plan := Plan{Selection: m.Selection}
	_, _, err := beginExport(dir, m.Source, &inv, &plan, ExportRequest{SampleNum: 1, SampleDen: 2}, now)
	assert.ErrorContains(t, err, "ask for the same")
	onDisk, err := LoadPackManifest(dir)
	require.NoError(t, err)
	assert.True(t, onDisk.Complete)
}

// A manifest written while tables carried a "done" flag still loads.
func TestLoadPackManifest_OldDoneField(t *testing.T) {
	dir, m := writeTestPack(t, "native-bytes", 2, 3, 3)
	path := filepath.Join(dir, PackManifestName)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	old := strings.Replace(string(data), `"chunks":`, `"done": true, "chunks":`, 1)
	require.NotEqual(t, string(data), old)
	require.NoError(t, os.WriteFile(path, []byte(old), 0o644))
	back, err := LoadPackManifest(dir)
	require.NoError(t, err)
	assert.Equal(t, m, back)
}
