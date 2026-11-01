package servicedemo

import (
	"strconv"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/lifetime"
)

type memStorage struct{ m map[string][]byte }

func (s *memStorage) Get(key string) (value []byte, found bool, err error) {
	value, found = s.m[key]
	return
}
func (s *memStorage) Set(key string, value []byte) (err error) { s.m[key] = value; return }
func (s *memStorage) Delete(key string) (err error)            { delete(s.m, key); return }

func TestManifestIsAHeadlessApp(t *testing.T) {
	require.NoError(t, manifest.Validate())
	assert.Equal(t, app.SurfaceHeadless, manifest.Surface)
	_, ok := app.DefaultRegistry.LookupManifest(ManifestId)
	assert.True(t, ok)
}

func TestTicksResumeFromStorageAndStopOnCancel(t *testing.T) {
	st := &memStorage{m: map[string][]byte{TickKey: []byte("41")}}
	stop := make(chan struct{})
	ctx := app.NewStaticMountContext(ManifestId, zerolog.Nop(), st, nil, stop)
	scope := lifetime.New(nil)
	ctx.SetLifetime(scope)
	a := New(5 * time.Millisecond)
	require.NoError(t, a.Mount(ctx))
	require.Eventually(t, func() bool { return a.Ticks() >= 43 }, time.Second, time.Millisecond)
	close(stop)
	scope.Retire()
	select {
	case <-a.Stopped():
	case <-time.After(time.Second):
		t.Fatal("ticker did not stop on Cancel")
	}
	n := a.Ticks()
	stored, found, _ := st.Get(TickKey)
	require.True(t, found)
	assert.Equal(t, strconv.FormatUint(n, 10), string(stored))
	require.NoError(t, a.Unmount(ctx))
}
