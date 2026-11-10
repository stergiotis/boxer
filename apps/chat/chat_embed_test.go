package chat

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/llm"
	"github.com/stergiotis/boxer/public/llm/openaichat"
)

const embedHost app.AppIdT = "github.com/example/host"

// notesPreset is a preset that talks and does nothing else, so a turn needs
// no agent service.
func notesPreset() (p Preset) {
	m := BaseManifest()
	m.Id, m.Display, m.Title, m.Summary = "github.com/stergiotis/boxer/apps/chatlet/notes", "Notes", "Notes", "Talk about the notes"
	return Preset{Manifest: m, Book: "chatlet/test", System: "You talk about the notes.", Allow: "read", Changes: "ask", Reach: "host",
		Purpose: "Answers questions about the notes beside it."}
}

func embedded(t *testing.T, key string, layout EmbedLayoutE) (inst *App) {
	t.Helper()
	pane, err := NewEmbedded(notesPreset(), EmbedConfig{Host: embedHost, Key: key, Layout: layout})
	require.NoError(t, err)
	return pane.inst
}

// paneOver is appOver for a pane: the bus knows the calls as the host's,
// with the chat's capabilities on the host's manifest.
func paneOver(t *testing.T, model openaichat.ClientI, store agentrun.StoreI, key string) (inst *App) {
	t.Helper()
	bus := inprocbus.NewInst(zerolog.Nop())
	svc, err := llm.NewService(bus, zerolog.Nop(), llm.Config{Journal: llm.StoreJournal(store), Endpoint: "http://127.0.0.1:1234/v1", Model: "m", Client: model, Retain: llm.RetainRing})
	require.NoError(t, err)
	t.Cleanup(svc.Close)
	inst = embedded(t, key, EmbedLayoutFull)
	inst.bus = bus.NewClient(embedHost, EmbedCaps())
	inst.cli = llm.NewClient(inst.bus)
	inst.cli.Timeout = 5 * time.Second
	inst.coord = inst.newCoordinator(inst.conv.id)
	inst.coord.store = store
	return
}

func TestEmbedded_isTheHostsAppUnderAComposedIdentity(t *testing.T) {
	inst := embedded(t, "notes", EmbedLayoutCompact)
	assert.Equal(t, string(embedHost), inst.owner(), "the bus and the journal know the host")
	assert.Equal(t, string(embedHost)+"#notes", inst.identity())
	assert.Equal(t, inst.identity(), string(inst.preset.Manifest.Id), "the preset is pinned under the composed identity")
	assert.Equal(t, "notes", inst.paneKey())
	assert.True(t, inst.compact())
	assert.True(t, inst.guided(), "a pane is a preset's")

	plain, err := NewPreset(notesPreset())
	require.NoError(t, err)
	assert.NotEqual(t, plain.(*App).preset.digest(), inst.preset.digest(), "a pane's runs strand in the chatlet's own window and back")
	assert.Equal(t, "", plain.(*App).paneKey())
	assert.False(t, plain.(*App).compact())
}

func TestEmbedded_refusesWhatItCannotKeepApart(t *testing.T) {
	for _, cfg := range []EmbedConfig{
		{Key: "notes"},
		{Host: embedHost},
		{Host: embedHost, Key: "two words"},
		{Host: embedHost, Key: "a.b"},
		{Host: embedHost, Key: strings.Repeat("k", 65)},
		{Host: embedHost, Key: "notes", Layout: EmbedLayoutCompact + 1},
	} {
		_, err := NewEmbedded(notesPreset(), cfg)
		assert.Error(t, err, "%+v", cfg)
	}
	bad := notesPreset()
	bad.Allow = "everything"
	_, err := NewEmbedded(bad, EmbedConfig{Host: embedHost, Key: "notes"})
	assert.Error(t, err, "the preset's own checks hold")
}

// Two panes of one host share its runs on the journal; each lists, resumes
// and deletes only its own, and a pane under the same key in another window
// of the host picks its conversations up.
func TestEmbedded_twoPanesOfOneHostKeepTheirConversationsApart(t *testing.T) {
	store := agentrun.NewMemStore()
	a := paneOver(t, &fakeModel{}, store, "notes")
	require.True(t, a.startTurn("q1"))
	drainUntil(t, a)
	settleTitle(t, a)
	run, found, err := agentrun.RunOfConversation(context.Background(), store, string(embedHost), a.conv.id)
	require.NoError(t, err)
	require.True(t, found, "the run is the host's")
	assert.Equal(t, "notes", runPane(run))

	b := paneOver(t, &fakeModel{}, store, "todo")
	b.listRuns()
	require.Eventually(t, func() bool {
		if runs, _, ok := b.runsJob.TakeResult(); ok {
			b.runs = *runs
			return true
		}
		return false
	}, 5*time.Second, 5*time.Millisecond)
	assert.Empty(t, b.runs, "another pane's run is not listed")
	b.resumeRun(a.conv.id)
	require.Eventually(t, func() bool { b.drain(); return b.note.text != "" }, 5*time.Second, 5*time.Millisecond)
	assert.NotEqual(t, a.conv.id, b.conv.id, "nor resumed")

	again := paneOver(t, &fakeModel{}, store, "notes")
	again.listRuns()
	require.Eventually(t, func() bool { again.renderRunsPanelData(); return len(again.runs) == 1 }, 5*time.Second, 5*time.Millisecond)
	assert.Equal(t, a.conv.id, again.runs[0].Conversation)
}

func TestEmbedded_ownRunsKeepsAWindowsOwnAndAPanesOwn(t *testing.T) {
	runs := []agentrun.Run{{Conversation: "plain", Settings: `{"keep":true}`}, {Conversation: "old"}, {Conversation: "n", Settings: `{"pane":"notes"}`}, {Conversation: "t", Settings: `{"pane":"todo"}`}}
	conv := func(rs []agentrun.Run) (out []string) {
		for _, r := range rs {
			out = append(out, r.Conversation)
		}
		return
	}
	assert.Equal(t, []string{"plain", "old"}, conv(ownRuns(runs, "")), "a window of its own sees no pane's run")
	assert.Equal(t, []string{"n"}, conv(ownRuns(runs, "notes")))
	assert.Len(t, runs, 4, "the list is not changed")
}

func TestEmbedded_storageAndPeersAreUnderThePanesKey(t *testing.T) {
	inner := &mapStorage{}
	ctx := embedMountContext{MountContextI: app.NewStaticMountContext(embedHost, zerolog.Nop(), inner, nil, nil), prefix: storagePrefixOf("notes")}
	s := ctx.Storage()
	require.NoError(t, s.Set(titleStorePrefix+"c1", []byte("t")))
	key := storagePrefixOf("notes") + titleStorePrefix + "c1"
	got, found, _ := inner.Get(key)
	require.True(t, found)
	assert.Equal(t, "t", string(got))
	assert.True(t, strings.HasPrefix(key, EmbedPersistedKeys("notes")[0]), "the host declares what the pane writes")
	v, found, _ := s.Get(titleStorePrefix + "c1")
	assert.True(t, found)
	assert.Equal(t, "t", string(v))
	require.NoError(t, s.Delete(titleStorePrefix+"c1"))
	_, found, _ = inner.Get(key)
	assert.False(t, found)

	assert.Equal(t, "7", peerKeyOf("7", ""))
	assert.Equal(t, "7#notes", peerKeyOf("7", "notes"))
}

func TestEmbedCaps_areTheChatsAndACopy(t *testing.T) {
	caps := EmbedCaps()
	assert.Equal(t, manifest.Caps, caps)
	caps[0].Reason = "changed"
	assert.NotEqual(t, "changed", manifest.Caps[0].Reason)
}

// A pane mounts on the host's context: the host's bus, its window and its
// storage under the pane's key, and its turn only acts on Ctrl+Enter while
// its own composer has the focus.
func TestPane_mountsOnTheHostsContext(t *testing.T) {
	pane, err := NewEmbedded(notesPreset(), EmbedConfig{Host: embedHost, Key: "notes"})
	require.NoError(t, err)
	bus := inprocbus.NewInst(zerolog.Nop())
	bus.SetRequestTimeout(200 * time.Millisecond)
	inner := &mapStorage{}
	ctx := app.NewStaticMountContext(embedHost, zerolog.Nop(), inner, bus.NewClient(embedHost, EmbedCaps()), nil)
	ctx.SetInstanceKey(7)
	require.NoError(t, pane.Mount(ctx))
	inst := pane.inst
	assert.Equal(t, uint64(7), inst.window)
	assert.Equal(t, 0, ctx.Ids().Depth(), "the pane's scope is closed after Mount")
	require.NotNil(t, inst.storage)
	require.NoError(t, inst.storage.Set("k", []byte("v")))
	_, found, _ := inner.Get(storagePrefixOf("notes") + "k")
	assert.True(t, found)
	assert.Nil(t, inst.simpleStore, "a pane's look is its document's")
	assert.Equal(t, string(embedHost)+"#notes", pane.Identity())
	require.NoError(t, pane.Unmount(ctx))
}
