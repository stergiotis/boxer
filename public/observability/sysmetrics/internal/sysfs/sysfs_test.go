package sysfs_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/observability/sysmetrics/internal/sysfs"
)

func TestReader_DefaultRoot(t *testing.T) {
	r := sysfs.New("")
	assert.Equal(t, sysfs.DefaultRoot, r.Root())
}

func TestReader_ReadFile_And_String(t *testing.T) {
	tmp := t.TempDir()
	mustWrite(t, filepath.Join(tmp, "leaf"), "42\n")

	r := sysfs.New(tmp)

	data, err := r.ReadFile("leaf")
	require.NoError(t, err)
	assert.Equal(t, "42\n", string(data))

	s, err := r.ReadString("leaf")
	require.NoError(t, err)
	assert.Equal(t, "42", s, "trailing newline must be trimmed")
}

func TestReader_ReadFile_NotFound(t *testing.T) {
	tmp := t.TempDir()
	r := sysfs.New(tmp)
	_, err := r.ReadFile("missing")
	require.Error(t, err)
	assert.True(t, errors.Is(err, fs.ErrNotExist))
}

func TestReader_ListDir(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, "class", "hwmon", "hwmon0"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, "class", "hwmon", "hwmon1"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, "class", "hwmon", "other"), 0o755))

	r := sysfs.New(tmp)
	names, err := r.ListDir("class/hwmon")
	require.NoError(t, err)
	assert.Equal(t, []string{"hwmon0", "hwmon1", "other"}, names)
}

func TestReader_IterPrefix(t *testing.T) {
	tmp := t.TempDir()
	for _, n := range []string{"hwmon0", "hwmon1", "hwmon10", "other"} {
		require.NoError(t, os.MkdirAll(filepath.Join(tmp, "class", "hwmon", n), 0o755))
	}
	r := sysfs.New(tmp)

	var got []string
	for n, err := range r.IterPrefix("class/hwmon", "hwmon") {
		require.NoError(t, err)
		got = append(got, n)
	}
	assert.Equal(t, []string{"hwmon0", "hwmon1", "hwmon10"}, got)
}

func TestReader_IterPrefix_DirMissing(t *testing.T) {
	tmp := t.TempDir()
	r := sysfs.New(tmp)
	var sawErr bool
	var sawName bool
	for n, err := range r.IterPrefix("class/hwmon", "hwmon") {
		if err != nil {
			sawErr = true
		}
		if n != "" {
			sawName = true
		}
	}
	assert.True(t, sawErr)
	assert.False(t, sawName)
}

func TestReader_EvalSymlink(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "real", "device")
	link := filepath.Join(tmp, "alias")
	require.NoError(t, os.MkdirAll(target, 0o755))
	require.NoError(t, os.Symlink(target, link))

	r := sysfs.New(tmp)
	got, err := r.EvalSymlink("alias")
	require.NoError(t, err)
	resolved, err := filepath.EvalSymlinks(target)
	require.NoError(t, err)
	assert.Equal(t, resolved, got)
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}
