package hello

import (
	"strconv"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
)

type memStorage struct{ m map[string][]byte }

func (s *memStorage) Get(key string) (value []byte, found bool, err error) {
	value, found = s.m[key]
	return
}
func (s *memStorage) Set(key string, value []byte) (err error) { s.m[key] = value; return }
func (s *memStorage) Delete(key string) (err error)            { delete(s.m, key); return }

func TestManifestRegisters(t *testing.T) {
	require.NoError(t, manifest.Validate())
	_, ok := app.DefaultRegistry.LookupManifest(AppId)
	assert.True(t, ok)
	assert.Equal(t, app.SurfaceWindowed, manifest.Surface)
}

// TestGreetingsSurviveTheWindow mounts a window over a store that already
// holds a count, greets twice, unmounts, and reads the stored count.
func TestGreetingsSurviveTheWindow(t *testing.T) {
	st := &memStorage{m: map[string][]byte{GreetingsKey: []byte("41")}}
	ctx := app.NewStaticMountContext(AppId, zerolog.Nop(), st, nil, make(chan struct{}))
	a := newApp()
	require.NoError(t, a.Mount(ctx))
	assert.Equal(t, uint64(41), a.greetings.Load())
	a.greet()
	a.greet()
	require.NoError(t, a.Unmount(ctx)) // retires the window's scope and waits for the writes
	stored, found, _ := st.Get(GreetingsKey)
	require.True(t, found)
	assert.Equal(t, strconv.Itoa(43), string(stored))
}

func TestHelpBookIndexes(t *testing.T) {
	help.SyncFromRegistry()
	b, ok := help.Book(AppId)
	require.True(t, ok)
	require.Empty(t, b.Validate())
	for _, info := range b.Docs() {
		doc, _, parsed := b.Doc(info.Path)
		require.Truef(t, parsed, "doc %q must parse", info.Path)
		require.Emptyf(t, doc.Dropped(), "doc %q loses content: %+v", info.Path, doc.Dropped())
	}
}
