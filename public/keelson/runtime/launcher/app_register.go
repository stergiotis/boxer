package launcher

// The launcher as a registered app (ADR-0214 §SD1).
//
// helphost is the template, and the choice to copy it is the decision: a
// launcher that is an ordinary app gets window chrome, geometry memory, an
// app-lifecycle audit row and a keelson('windows') entry from machinery that
// already exists, where an overlay drawn by the host chrome would have owed
// its own version of each (§Alternatives, O3).

import (
	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// ManifestId is the launcher app's stable identity. Durably public: it is
// what `--launch` accepts and what audit rows record.
const ManifestId app.AppIdT = "github.com/stergiotis/boxer/public/keelson/runtime/launcher"

// LauncherKeyLabel names the global shortcut for the surfaces that advertise
// it. A label rather than a key binding — the binding itself is the shell
// chrome's, wired to the §SD9 fetcher; this is only how it is spelled to a
// reader, kept beside the app so the two cannot disagree.
const LauncherKeyLabel = "F2"

// Default is the process-wide launcher component. One value backs the app's
// windows and the windowhost's empty-state pane (§SD2), which is what keeps
// the query and the facet filters from resetting as a person moves between
// them. Mirrors app.DefaultRegistry / help.DefaultLibrary in shape and role.
//
// The host arrives later, through SetHost: the window host is constructed
// after registration, and the launcher is one of the apps it hosts.
var Default = New(app.DefaultRegistry, nil, help.DefaultLibrary, log.Logger)

var manifest = app.Manifest{
	Id:      ManifestId,
	Version: "0.1.0",
	Display: "Apps",
	Title:   "Apps — launcher",
	Summary: "Find an app by name, topic or what it does",
	// Phosphor squares-four: the app-grid metaphor. Not the magnifying glass
	// the search box uses — the launcher is where apps live, and searching is
	// one thing done there.
	Icon:     icons.PhSquaresFour,
	Topics:   []app.TopicT{app.TopicRuntime},
	Keywords: []string{"launcher", "apps", "open", "start", "run", "find", "search", "palette", "menu"},
	Surface:  app.SurfaceWindowed,
	SurfaceHints: app.SurfaceHints{
		PreferredWidth:  styletokens.SurfaceApp.W,
		PreferredHeight: styletokens.SurfaceApp.H,
	},
	// No declared caps. The launcher opens apps through the host interface it
	// is handed (§SD3), not by publishing to windowhost.open — a host-mediated
	// capability in the ADR-0155 §SD1 sense rather than a subject the cap
	// broker arbitrates. An app that wanted to open others *over the bus*
	// would declare windowhost.OpenSubject; this one is the host's own
	// surface.
}

// launcherApp is one window onto the shared component. Per-window state is
// exactly the id stack: everything a person can change — the query, the
// chips, the selection — belongs to the launcher, not to one of its windows.
type launcherApp struct {
	inst *Inst
	ids  *c.WidgetIdStack
}

var _ app.AppI = (*launcherApp)(nil)

// NewApp wraps inst as an AppI. Exported for hosts that build their own
// component rather than using Default — the screenshot driver, and tests.
func NewApp(inst *Inst) (a app.AppI) {
	a = &launcherApp{inst: inst, ids: c.NewWidgetIdStack()}
	return
}

func (inst *launcherApp) Manifest() (m app.Manifest) {
	m = manifest
	return
}

func (inst *launcherApp) Mount(ctx app.MountContextI) (err error) {
	// A window that just opened should be typeable into. The shortcut's whole
	// promise is F2-then-type (§SD9).
	inst.inst.FocusQueryField()
	return
}

func (inst *launcherApp) Unmount(ctx app.MountContextI) (err error) {
	return
}

// Frame draws the component into the host-created window body. The host has
// already wrapped this call in the window and in a per-window id salt, so two
// launcher windows drawing the same component cannot collide on the wire.
func (inst *launcherApp) Frame(ctx app.FrameContextI) (err error) {
	inst.ids.Reset()
	inst.inst.Render(inst.ids)
	return
}

func init() {
	// Factory rather than singleton: a second launcher window is a fresh
	// wrapper with its own id stack over the same component, which is the
	// shape that makes two windows render independently while agreeing about
	// what is filtered.
	err := app.DefaultRegistry.RegisterFactory(manifest, func() (a app.AppI, ctorErr error) {
		a = NewApp(Default)
		return
	})
	if err != nil {
		log.Warn().Err(err).Msg("launcher: failed to register factory")
	}
}

// hostAssertion documents the structural contract between this package and
// windowhost without importing it: *windowhost.Inst carries OpenOrRaiseApp
// and OpenAppIds precisely so it satisfies HostI, and hostboot wires the two
// together. Written as a comment rather than a compile-time assertion because
// the assertion would be the import the dependency direction forbids (§SD3).
