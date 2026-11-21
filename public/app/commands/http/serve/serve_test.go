package serve

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFileDumpServiceCreatesAnEnterableDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "fresh", "out")
	svc := NewFileDumpService(&Config{OutputDirectory: dir, FilePattern: "body-%d.bin"})
	require.NoError(t, svc.skipExistingFiles())

	rec := httptest.NewRecorder()
	svc.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader("hello")))
	require.Equal(t, http.StatusAccepted, rec.Code)
	b, err := os.ReadFile(filepath.Join(dir, "body-1.bin"))
	require.NoError(t, err)
	require.Equal(t, "hello", string(b))
}

func TestFileDumpServiceBoundsTheBody(t *testing.T) {
	dir := t.TempDir()
	svc := NewFileDumpService(&Config{OutputDirectory: dir, FilePattern: "body-%d.bin", MaxBodyBytes: 4})
	require.NoError(t, svc.skipExistingFiles())

	rec := httptest.NewRecorder()
	svc.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader("more than four bytes")))
	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Empty(t, entries, "a truncated body is not kept")
}

func TestFileDumpServiceReturnsAFailedBind(t *testing.T) {
	svc := NewFileDumpService(&Config{Listen: "no-port", OutputDirectory: t.TempDir(), FilePattern: "b-%d"})
	require.Error(t, svc.Listen())
}
