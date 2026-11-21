package fsbroker

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stergiotis/boxer/public/config/env/envtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServedRootIsCreatedAndNeverSlash(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "picker")
	envtest.Set(t, ServedRootEnv, dir)
	root, err := ServedRoot()
	require.NoError(t, err)
	assert.Equal(t, dir, root)
	fi, err := os.Stat(root)
	require.NoError(t, err)
	assert.True(t, fi.IsDir())

	envtest.Set(t, ServedRootEnv, "/")
	_, err = ServedRoot()
	assert.Error(t, err)
}
