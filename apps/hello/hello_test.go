package hello

import (
	"context"
	"strconv"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/apptest"
)

type memStorage struct{ m map[string][]byte }

func (s *memStorage) Get(ctx context.Context, key string) (value []byte, found bool, err error) {
	value, found = s.m[key]
	return
}
func (s *memStorage) Set(ctx context.Context, key string, value []byte) (err error) {
	s.m[key] = value
	return
}
func (s *memStorage) Delete(ctx context.Context, key string) (err error) { delete(s.m, key); return }

// TestConforms runs the conformance kit (apptest, ADR-0396 §8): the
// manifest registers as it is, the help book is sound, and the window
// mounts, draws two frames headless with no id used twice, and unmounts.
func TestConforms(t *testing.T) {
	apptest.ManifestOK(t, manifest)
	apptest.HelpCorpus(t, AppId)
	apptest.FrameHeadless(t, app.DefaultRegistry, AppId, apptest.FrameOpts{Strict: true})
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
	stored, found, _ := st.Get(t.Context(), GreetingsKey)
	require.True(t, found)
	assert.Equal(t, strconv.Itoa(43), string(stored))
}
