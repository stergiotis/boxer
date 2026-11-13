package chat

import (
	"encoding/json"
	"slices"

	"github.com/zeebo/xxh3"

	"github.com/stergiotis/boxer/public/hashing/splitmix64"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// An embedded chat (ADR-0368) is a preset's chat drawn inside
// another app's window, as a sqlapplet's play is (ADR-0132 §SD8,
// ADR-0134 §SD7). The host keeps the governance: the chat's capabilities
// ride the host's manifest (EmbedCaps), the host is the app the bus, the
// agent service and the run journal know, and only a host the person
// registered as a coordinator gets a grant asked for. What the pane adds is
// a composed identity, `<host app id>#<key>`, on what the chat stamps
// itself — provenance, the artefact's properties, the run's pinning — and
// a key on its runs and storage, so two panes of one host keep their
// conversations apart.

// EmbedLayoutE is how much of the window an embedded chat draws.
type EmbedLayoutE uint8

const (
	// EmbedLayoutFull is the window's bar and dock, as in a window of its
	// own.
	EmbedLayoutFull EmbedLayoutE = iota
	// EmbedLayoutCompact is the bar, the transcript and the composer; the
	// other tabs are a row of links above the transcript, one shown at a
	// time in its place.
	EmbedLayoutCompact
)

// EmbedConfig is what a host says about the pane it embeds.
type EmbedConfig struct {
	// Host is the embedding app's manifest id: the app the bus knows the
	// pane's calls as, and the first half of its composed identity.
	Host app.AppIdT
	// Key names the pane within the host, the second half: one token of
	// letters, digits, '-' and '_'. A chatlet's slug is the usual key. Two
	// panes of one host take two keys; a pane that takes the same key in
	// another window of the host shares its conversations.
	Key string
	// Layout is how much of the window the pane draws.
	Layout EmbedLayoutE
}

// embedding is a pane's side of App.
type embedding struct {
	host   app.AppIdT
	key    string
	layout EmbedLayoutE
	// salt scopes the pane's widget ids within the host's window.
	salt uint64
	// shown is the compact layout's tab in the transcript's place; zero
	// for the transcript.
	shown chatTabE
	// draftFocused is whether the composer held the keyboard focus on the
	// last frame: the pane acts on Ctrl+Enter only then, since the host's
	// window may hold another composer.
	draftFocused bool
}

// identity is the pane's composed identity (ADR-0134 §SD7's shape).
func (inst *embedding) identity() string { return string(inst.host) + "#" + inst.key }

// storagePrefix keeps the pane's keys apart from the host's and from
// another pane's.
func storagePrefixOf(key string) string { return "chat-" + key + "-" }

// Pane is a chat embedded in a host's window. The host calls Mount from its
// own Mount, Frame from its frame wherever the pane is to be drawn, and
// Unmount from its own Unmount, each with its own context.
type Pane struct {
	inst *App
}

// NewEmbedded is a pane of the chat specialised by p inside the host cfg
// names; a preset that does not validate is refused, as is a key that is
// not one token. The preset's manifest names no app the host runs: its id
// becomes the composed identity, and its capabilities are the host's to
// declare.
func NewEmbedded(p Preset, cfg EmbedConfig) (pane *Pane, err error) {
	if cfg.Host == "" {
		return nil, eb.Build().Str("key", cfg.Key).Errorf("an embedded chat needs its host's app id")
	}
	if !validEmbedKey(cfg.Key) {
		return nil, eb.Build().Str("host", string(cfg.Host)).Str("key", cfg.Key).Errorf("an embedded chat's key is one token of letters, digits, '-' and '_'")
	}
	if cfg.Layout > EmbedLayoutCompact {
		return nil, eb.Build().Str("host", string(cfg.Host)).Str("key", cfg.Key).Uint8("layout", uint8(cfg.Layout)).Errorf("no such layout")
	}
	e := &embedding{host: cfg.Host, key: cfg.Key, layout: cfg.Layout}
	e.salt = splitmix64.Forward(xxh3.HashString(e.identity()) ^ embedSaltTag)
	p.Manifest.Id = app.AppIdT(e.identity())
	a, err := NewPreset(p)
	if err != nil {
		return
	}
	inst := a.(*App)
	inst.embed = e
	pane = &Pane{inst: inst}
	return
}

// embedSaltTag keeps a pane's widget scope apart from another package's
// salt of the same string.
const embedSaltTag uint64 = 0x63686174_70616e65

// validEmbedKey is the app storage's key rule, since the key prefixes the
// pane's keys there.
func validEmbedKey(key string) bool {
	if key == "" || len(key) > 64 {
		return false
	}
	for _, r := range key {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

// Identity is the pane's composed identity, `<host>#<key>`.
func (inst *Pane) Identity() string { return inst.inst.embed.identity() }

// Mount takes the host's context: its bus, storage, lifetime, tasks and
// window. The pane's storage keys are prefixed with its key.
func (inst *Pane) Mount(ctx app.MountContextI) (err error) {
	a := inst.inst
	scoped := embedMountContext{MountContextI: ctx, prefix: storagePrefixOf(a.embed.key)}
	// What Mount derives from the id stack is the pane's, as its frame is.
	for range c.IdScope(ctx.Ids().PrepareHighEntropy(a.embed.salt)) {
		err = a.Mount(scoped)
	}
	return
}

// Frame draws the pane into the host's current region.
func (inst *Pane) Frame(ctx app.FrameContextI) (err error) {
	a := inst.inst
	for range c.IdScope(a.ids.PrepareHighEntropy(a.embed.salt)) {
		err = a.Frame(ctx)
	}
	return
}

// Unmount ends the pane's turn and closes its run, as closing a window
// does; the host's window drain waits for the close.
func (inst *Pane) Unmount(ctx app.MountContextI) (err error) {
	return inst.inst.Unmount(ctx)
}

// EmbedCaps is what a host adds to its manifest to embed a chat: the
// chat's own capabilities. A preset widens none of them, and a host that
// declares fewer has the calls it left out refused at its bus.
func EmbedCaps() (caps []app.SubjectFilter) { return slices.Clone(manifest.Caps) }

// EmbedPersistedKeys is what a host adds to its manifest's PersistedKeys
// for a pane under key, so the host injects the persist cap: the roster's
// titles, prefixed with the pane's key.
func EmbedPersistedKeys(key string) (keys []string) {
	return []string{storagePrefixOf(key) + titleStorePrefix}
}

// embedMountContext is the host's context with the pane's storage.
type embedMountContext struct {
	app.MountContextI
	prefix string
}

func (inst embedMountContext) Storage() (s app.StorageI) {
	inner := inst.MountContextI.Storage()
	if inner == nil {
		return nil
	}
	return prefixedStorage{inner: inner, prefix: inst.prefix}
}

// prefixedStorage puts every key under a prefix.
type prefixedStorage struct {
	inner  app.StorageI
	prefix string
}

func (inst prefixedStorage) Get(key string) (value []byte, found bool, err error) {
	return inst.inner.Get(inst.prefix + key)
}
func (inst prefixedStorage) Set(key string, value []byte) (err error) {
	return inst.inner.Set(inst.prefix+key, value)
}
func (inst prefixedStorage) Delete(key string) (err error) {
	return inst.inner.Delete(inst.prefix + key)
}

// paneKey is the key the window's runs are kept under, empty for a window
// of its own.
func (inst *App) paneKey() string {
	if inst.embed == nil {
		return ""
	}
	return inst.embed.key
}

// identity is what the chat stamps itself as: the pane's composed identity,
// else the app it is.
func (inst *App) identity() string {
	if inst.embed != nil {
		return inst.embed.identity()
	}
	return inst.owner()
}

// peerKey is the key this window's turn is published under for the
// roster: the window, and within an embedding host's window the pane.
func peerKeyOf(window string, pane string) string {
	if pane == "" {
		return window
	}
	return window + "#" + pane
}

// runPane is the pane a run was written by, empty for a window of its own.
func runPane(r agentrun.Run) string {
	var s struct {
		Pane string `json:"pane"`
	}
	_ = json.Unmarshal([]byte(r.Settings), &s)
	return s.Pane
}

// ownRuns is the runs written under pane: a host lists every run it owns,
// and each pane shows its own.
func ownRuns(runs []agentrun.Run, pane string) (out []agentrun.Run) {
	out = runs[:0:0]
	for _, r := range runs {
		if runPane(r) == pane {
			out = append(out, r)
		}
	}
	return
}
