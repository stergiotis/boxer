package helphost

import (
	"testing"
	"testing/fstest"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
)

// fixtureLibrary builds a help.LibraryI with one book whose corpus has
// two docs at known paths. Returned by every test that needs a
// non-empty library; isolated from help.DefaultLibrary so test
// reordering can't perturb it.
func fixtureLibrary(t *testing.T) (lib help.LibraryI, appId app.AppIdT) {
	t.Helper()
	appId = "github.com/test/helphost-fixture"
	fsys := fstest.MapFS{
		"overview.md":      {Data: []byte("# Overview\n\nfixture body\n")},
		"howto/replay.md":  {Data: []byte("# Replaying\n\nsteps go here\n")},
	}
	b, err := help.NewBook(appId, fsys)
	if err != nil {
		t.Fatalf("NewBook: %v", err)
	}
	lib = help.NewLibrary()
	if err := lib.Register(b); err != nil {
		t.Fatalf("Register: %v", err)
	}
	return
}

func TestHelpHost_ManifestValid(t *testing.T) {
	h := New()
	m := h.Manifest()
	if err := m.Validate(); err != nil {
		t.Fatalf("Manifest.Validate: %v", err)
	}
	if m.Id != ManifestId {
		t.Errorf("Manifest.Id: got %q, want %q", m.Id, ManifestId)
	}
	if m.Surface != app.SurfaceWindowed {
		t.Errorf("Manifest.Surface: got %v, want SurfaceWindowed", m.Surface)
	}
}

func TestHelpHost_OpenRef_SetsSelection(t *testing.T) {
	h := New()
	ref := help.RefT{
		AppId:   "github.com/test/x",
		Doc:     "howto/replay",
		Section: "gotchas",
	}
	h.OpenRef(ref)
	if got := h.CurrentRef(); got != ref {
		t.Errorf("CurrentRef: got %+v, want %+v", got, ref)
	}
}

func TestHelpHost_OpenRef_ExpandsBook(t *testing.T) {
	h := New()
	h.OpenRef(help.RefT{AppId: "github.com/test/x", Doc: "overview"})
	if !h.expandedApps["github.com/test/x"] {
		t.Errorf("OpenRef did not expand parent book in nav")
	}
}

func TestHelpHost_OpenRef_ZeroIsNoOp(t *testing.T) {
	h := New()
	h.OpenRef(help.RefT{AppId: "github.com/test/x", Doc: "overview"})
	h.OpenRef(help.RefT{})
	got := h.CurrentRef()
	if got.AppId != "github.com/test/x" || got.Doc != "overview" {
		t.Errorf("zero-ref OpenRef perturbed prior selection: got %+v", got)
	}
}

func TestHelpHost_SetLibrary_Replaces(t *testing.T) {
	h := New()
	lib, _ := fixtureLibrary(t)
	h.SetLibrary(lib)
	if h.lib != lib {
		t.Errorf("SetLibrary did not replace inst.lib")
	}
}

func TestHelpHost_SetLibrary_NilPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("SetLibrary(nil): want panic, got none")
		}
	}()
	New().SetLibrary(nil)
}

// TestHelpHost_CurrentRefAfterFixture verifies that the selected
// AppId+Doc pair resolves to a real entry in the injected library —
// catches the obvious wiring bug where OpenRef accepts a doc path the
// book doesn't know about.
func TestHelpHost_CurrentRefAfterFixture(t *testing.T) {
	h := New()
	lib, appId := fixtureLibrary(t)
	h.SetLibrary(lib)
	h.OpenRef(help.RefT{AppId: appId, Doc: "howto/replay"})

	ref := h.CurrentRef()
	b, ok := h.lib.Book(ref.AppId)
	if !ok {
		t.Fatalf("Book(%q): not found", ref.AppId)
	}
	_, _, ok = b.Doc(ref.Doc)
	if !ok {
		t.Fatalf("Doc(%q): not found in book", ref.Doc)
	}
}

// TestHelpHost_DefaultViewModeRendered locks in the zero-value of
// ViewModeE — Rendered, the friendly default for "I just opened
// Help to read something".
func TestHelpHost_DefaultViewModeRendered(t *testing.T) {
	h := New()
	if got := h.ViewMode(); got != ViewModeRendered {
		t.Errorf("default ViewMode: got %v, want ViewModeRendered", got)
	}
}

func TestHelpHost_SetViewMode_RoundTrip(t *testing.T) {
	h := New()
	h.SetViewMode(ViewModeSource)
	if got := h.ViewMode(); got != ViewModeSource {
		t.Errorf("after SetViewMode(Source): got %v, want ViewModeSource", got)
	}
	h.SetViewMode(ViewModeRendered)
	if got := h.ViewMode(); got != ViewModeRendered {
		t.Errorf("after SetViewMode(Rendered): got %v, want ViewModeRendered", got)
	}
}

// TestHelpHost_ConsumeScrollTarget_FiresOnce verifies the one-shot
// semantics that keep the user from being yanked back to the anchor
// while they're scrolling away from it.
func TestHelpHost_ConsumeScrollTarget_FiresOnce(t *testing.T) {
	h := New()
	h.OpenRef(help.RefT{
		AppId:   "github.com/test/x",
		Doc:     "howto/replay",
		Section: "gotchas",
	})
	first := h.consumeScrollTarget()
	if first != "gotchas" {
		t.Errorf("first consume: got %q, want %q", first, "gotchas")
	}
	second := h.consumeScrollTarget()
	if second != "" {
		t.Errorf("second consume on same selection: got %q, want empty", second)
	}
}

func TestHelpHost_ConsumeScrollTarget_RefiresOnSectionChange(t *testing.T) {
	h := New()
	h.OpenRef(help.RefT{AppId: "x", Doc: "y", Section: "first"})
	_ = h.consumeScrollTarget()
	// User clicks a different section in the same doc.
	h.selectedSection = "second"
	got := h.consumeScrollTarget()
	if got != "second" {
		t.Errorf("after section change: got %q, want %q", got, "second")
	}
}

func TestHelpHost_ConsumeScrollTarget_RefiresOnDocChange(t *testing.T) {
	h := New()
	h.OpenRef(help.RefT{AppId: "x", Doc: "y", Section: "anchor"})
	_ = h.consumeScrollTarget()
	// Same section slug but different doc — must scroll fresh.
	h.OpenRef(help.RefT{AppId: "x", Doc: "z", Section: "anchor"})
	got := h.consumeScrollTarget()
	if got != "anchor" {
		t.Errorf("after doc change with same section: got %q, want %q", got, "anchor")
	}
}

func TestHelpHost_ConsumeScrollTarget_EmptySectionNoOp(t *testing.T) {
	h := New()
	h.OpenRef(help.RefT{AppId: "x", Doc: "y"})
	if got := h.consumeScrollTarget(); got != "" {
		t.Errorf("empty section: got %q, want empty", got)
	}
}

// TestHelpHost_ViewModePersistsAcrossOpenRef confirms that switching
// the active doc keeps the view mode — matches the typical authoring
// workflow ("show me source for every doc I open").
func TestHelpHost_ViewModePersistsAcrossOpenRef(t *testing.T) {
	h := New()
	h.SetViewMode(ViewModeSource)
	h.OpenRef(help.RefT{AppId: "github.com/test/x", Doc: "a"})
	h.OpenRef(help.RefT{AppId: "github.com/test/x", Doc: "b"})
	if got := h.ViewMode(); got != ViewModeSource {
		t.Errorf("ViewMode after two OpenRef calls: got %v, want ViewModeSource", got)
	}
}

// TestHelpHost_RegisteredInDefaultRegistry confirms that init() in
// app_register.go landed the manifest into app.DefaultRegistry, which
// is how shells (launchers, dock hosts) discover HelpHost.
func TestHelpHost_RegisteredInDefaultRegistry(t *testing.T) {
	m, ok := app.DefaultRegistry.LookupManifest(ManifestId)
	if !ok {
		t.Fatalf("HelpHost manifest not in DefaultRegistry; init() did not fire")
	}
	if m.Display == "" {
		t.Errorf("registered manifest has empty Display")
	}
	got, err := app.DefaultRegistry.Open(ManifestId)
	if err != nil {
		t.Fatalf("Open(ManifestId): %v", err)
	}
	if _, isHelpHost := got.(*HelpHost); !isHelpHost {
		t.Errorf("Open returned %T, want *HelpHost", got)
	}
}
