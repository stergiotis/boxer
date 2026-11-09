package chat

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/persist"
)

// simpleRig is the app's storage over a real persist service, reached with
// the caps the window host injects for a manifest with PersistedKeys.
func simpleRig(t *testing.T) (storage app.StorageI, stored func() (string, bool)) {
	t.Helper()
	assert.Contains(t, manifest.PersistedKeys, simpleViewKey)
	bus := inprocbus.NewInst(zerolog.Nop())
	bus.SetRequestTimeout(2 * time.Second)
	backend := persist.NewMemoryBackend()
	svc, err := persist.NewService(bus, zerolog.Nop(), backend)
	require.NoError(t, err)
	t.Cleanup(svc.Close)
	alias := ManifestId.SubjectAlias()
	caps := append(slices.Clone(manifest.Caps), app.SubjectFilter{Pattern: persist.SubjectPrefix + alias + ".>", Direction: app.CapDirectionPub})
	storage, err = persist.NewClient(bus.NewClient(ManifestId, caps), ManifestId)
	require.NoError(t, err)
	stored = func() (string, bool) {
		v, found, _ := backend.Get(persist.StorageRef{Alias: alias}, simpleViewKey)
		return string(v), found
	}
	return
}

func simpleWindow(t *testing.T, storage app.StorageI) (inst *App) {
	t.Helper()
	inst = newApp()
	inst.simpleStore = storage
	inst.loadSimpleView()
	require.Eventually(t, func() bool {
		inst.syncSimpleView()
		return inst.simpleRead
	}, 5*time.Second, 2*time.Millisecond)
	return
}

func keeps(t *testing.T, inst *App, stored func() (string, bool), want string) {
	t.Helper()
	require.Eventually(t, func() bool {
		inst.syncSimpleView()
		v, found := stored()
		return found && v == want && !inst.simpleWriting.Load()
	}, 5*time.Second, 2*time.Millisecond)
}

func TestTheSimpleViewIsRememberedForTheNextWindow(t *testing.T) {
	storage, stored := simpleRig(t)

	first := simpleWindow(t, storage)
	assert.False(t, first.simple, "off until the person asks")
	first.syncSimpleView()
	_, found := stored()
	assert.False(t, found, "nothing is written until it changes")

	first.simple = true
	keeps(t, first, stored, simpleViewOn)

	second := simpleWindow(t, storage)
	assert.True(t, second.simple, "the next window opens as the last was left")

	second.simple = false
	keeps(t, second, stored, simpleViewOff)
	assert.False(t, simpleWindow(t, storage).simple, "unticking is remembered too")
}

// A tick made before the store answers is newer than what the store holds.
func TestATickBeforeTheStoreAnswersStands(t *testing.T) {
	storage, stored := simpleRig(t)
	require.NoError(t, storage.Set(simpleViewKey, []byte(simpleViewOff)))
	inst := newApp()
	inst.simpleStore = storage
	inst.loadSimpleView()
	inst.simple = true
	keeps(t, inst, stored, simpleViewOn)
	assert.True(t, inst.simple)
}

func TestAPresetAndASceneDoNotUseTheStore(t *testing.T) {
	storage, _ := simpleRig(t)
	assert.Equal(t, storage, simpleStoreFor(storage, false, ""), "a plain window keeps it")
	assert.Equal(t, storage, simpleStoreFor(storage, false, "imzero2-demo"), "as does one with a viewer")
	assert.Nil(t, simpleStoreFor(storage, true, ""), "a preset's document says")
	assert.Nil(t, simpleStoreFor(storage, false, "scene-chat-stream"), "a scene shares the machine's store")
}

// With no store the setting still works for the window, and nothing is read.
func TestAWindowThatDoesNotKeepItWritesNothing(t *testing.T) {
	_, stored := simpleRig(t)
	inst := newApp()
	inst.loadSimpleView()
	inst.simple = true
	inst.simpleRead = true
	inst.syncSimpleView()
	assert.True(t, inst.simple)
	assert.False(t, inst.simpleWriting.Load())
	_, found := stored()
	assert.False(t, found)
}

// failingReads is a store that cannot be read and can still be written.
type failingReads struct {
	app.StorageI
}

func (failingReads) Get(string) ([]byte, bool, error) {
	return nil, false, errors.New("the store did not answer")
}

// A read that fails must not stop the window keeping a later choice.
func TestAChoiceIsKeptAfterAFailedRead(t *testing.T) {
	storage, stored := simpleRig(t)
	inst := simpleWindow(t, failingReads{StorageI: storage})
	assert.False(t, inst.simple, "the default")
	inst.simple = true
	keeps(t, inst, stored, simpleViewOn)
}

func TestGuidedFollowsThePresetOrTheSetting(t *testing.T) {
	inst := newApp()
	assert.False(t, inst.guided(), "the plain chat keeps its wording")
	inst.simple = true
	assert.True(t, inst.guided())
	inst.simple = false
	inst.preset = &Preset{}
	assert.True(t, inst.guided(), "a preset is guided whatever its bar")
}
