package notebook

import (
	"io/fs"
	"slices"

	"github.com/zeebo/xxh3"

	"github.com/stergiotis/boxer/public/hashing/splitmix64"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/task"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/markdown/mdnotebook"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/notebookview"
)

// An embedded pane (ADR-0380 §SD5) is a notebook's live reading drawn in a
// region of another app's frame, under the rules an embedded applet and an
// embedded chatlet follow (ADR-0134 §SD7, ADR-0368): the host drives it from
// its own Mount, Frame and Unmount; its capabilities are the host's to
// declare, from EmbedCaps; its runs are attributed to `<host>#<key>`.
//
// A host that already holds the relations a notebook shows needs no pane:
// it uses notebookview over them, bound (ADR-0371 §SD6).

// EmbedConfig is what a host says about the pane it embeds.
type EmbedConfig struct {
	// Host is the embedding app's manifest id: the first half of the
	// pane's composed identity.
	Host app.AppIdT
	// Key names the pane within the host, the second half: one token of
	// letters, digits, '-' and '_'. A notebook's slug is the usual key.
	Key string
	// Files is the notebook's directory, where its stored results,
	// recordings and images are read from; nil for a notebook that names no
	// file, which then has no stored reading worth showing.
	Files fs.FS
	// RunOnMount starts a run when the pane is mounted. Without it the
	// first run waits for the person's gesture.
	RunOnMount bool
	// Run stands in for the server, for a test; nil sends to the server
	// the environment names.
	Run mdnotebook.RunFunc
	// MaxRows and MaxBytes bound one record's result; zero takes
	// LiveMaxRows and LiveMaxBytes.
	MaxRows  int64
	MaxBytes int64
}

// EmbeddedPane is a notebook's pane in a host's window. The host calls
// Mount from its own Mount, Frame from its frame wherever the pane is to be
// drawn, and Unmount from its own Unmount, each with its own context.
type EmbeddedPane struct {
	pane       *Pane
	host       app.AppIdT
	key        string
	salt       uint64
	runOnMount bool
}

// NewEmbedded is a pane over the notebook text holds, inside the host cfg
// names. A text that is not a notebook is refused, and so is one whose
// records do not classify read (ADR-0380 §SD3): such a notebook is
// collected, which a person confirms, and is not run on a host's behalf.
func NewEmbedded(text []byte, cfg EmbedConfig) (pane *EmbeddedPane, err error) {
	if cfg.Host == "" {
		return nil, eb.Build().Str("key", cfg.Key).Errorf("an embedded notebook needs its host's app id")
	}
	if !validEmbedKey(cfg.Key) {
		return nil, eb.Build().Str("host", string(cfg.Host)).Str("key", cfg.Key).Errorf("an embedded notebook's key is one token of letters, digits, '-' and '_'")
	}
	nb, err := mdnotebook.Read(text)
	if err != nil {
		return nil, eb.Build().Str("host", string(cfg.Host)).Str("key", cfg.Key).Errorf("read the notebook: %w", err)
	}
	identity := string(cfg.Host) + "#" + cfg.Key
	run := cfg.Run
	p := newPane(paneConfig{identity: identity, scopeKey: "notebook-" + cfg.Key, run: func() mdnotebook.RunFunc { return run }, maxRows: cfg.MaxRows, maxBytes: cfg.MaxBytes})
	p.setDocument(nb, cfg.Files, identity+"\x00"+mdnotebook.BlobHash(text))
	if p.refusal != "" {
		return nil, eb.Build().Str("host", string(cfg.Host)).Str("key", cfg.Key).Str("class", p.class.String()).Errorf("not embedded: %s", p.refusal)
	}
	pane = &EmbeddedPane{pane: p, host: cfg.Host, key: cfg.Key, runOnMount: cfg.RunOnMount}
	pane.salt = splitmix64.Forward(xxh3.HashString(identity) ^ embedSaltTag)
	return
}

// embedSaltTag keeps a pane's widget scope apart from another package's
// salt of the same string.
const embedSaltTag uint64 = 0x6e6f7465_70616e65

// validEmbedKey is the rule chat's embedded panes follow, so a key reads
// the same in both.
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

// Identity is the pane's composed identity, `<host>#<key>`: what its runs
// are attributed to in the server's query log.
func (inst *EmbeddedPane) Identity() string { return inst.pane.cfg.identity }

// Mount takes the host's context: its ids, tasks and run identity. A pane
// configured to run on mount starts its run here.
func (inst *EmbeddedPane) Mount(ctx app.MountContextI) (err error) {
	p := inst.pane
	p.mount(ctx)
	if inst.runOnMount {
		err = p.startRun()
	}
	return
}

// Frame draws the pane into the host's current region.
func (inst *EmbeddedPane) Frame(ctx app.FrameContextI) (err error) {
	p := inst.pane
	for range c.IdScope(p.ids.PrepareHighEntropy(inst.salt)) {
		p.frame(ctx)
	}
	return
}

// Unmount cancels a run and lets go of what is shown.
func (inst *EmbeddedPane) Unmount(ctx app.MountContextI) (err error) {
	inst.pane.close()
	return
}

// Run starts a run, as the pane's Run live does. A refusal says what
// stands in the way.
func (inst *EmbeddedPane) Run() (err error) { return inst.pane.startRun() }

// State is what the pane shows and how its last run ended.
func (inst *EmbeddedPane) State() LiveState { return inst.pane.State() }

// SetRecordings gives the pane what plays its recordings; nil draws them
// silent.
func (inst *EmbeddedPane) SetRecordings(r notebookview.RecordingsI) { inst.pane.recordings = r }

// EmbedCaps is what a host adds to its manifest to embed a pane: what a
// run needs, and nothing of collection. A document widens none of them.
func EmbedCaps() (caps []app.SubjectFilter) {
	return append(slices.Clone(task.ProducerCaps()),
		// A declaration, not a route: nothing publishes on
		// `ch.server.read.*`. It says what the host's process dials on the
		// pane's behalf, where a reviewer looks for it (ADR-0026 §SD10).
		app.SubjectFilter{Pattern: "ch.server.read.records", Direction: app.CapDirectionPub,
			Reason: "notebook pane: a live reading runs each record's statement on the server the environment names, read-only (ADR-0380 §SD3)"})
}
