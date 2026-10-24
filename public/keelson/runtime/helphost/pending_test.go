package helphost

import (
	"testing"
	"testing/fstest"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
)

func drainRefs() {
	for !popRef().IsZero() {
	}
}

// The fixture's docs sort "howto/replay" before "overview"; F1 must still
// land on the overview.
func TestRefForApp_PrefersOverview(t *testing.T) {
	lib, appId := fixtureLibrary(t)
	ref := RefForApp(lib, appId)
	if ref.AppId != appId || ref.Doc != "overview" {
		t.Fatalf("got %v, want %s/overview", ref, appId)
	}
}

func TestRefForApp_FirstDocWithoutOverview(t *testing.T) {
	appId := app.AppIdT("github.com/test/helphost-no-overview")
	b, err := help.NewBook(appId, fstest.MapFS{
		"b.md": {Data: []byte("# B\n")},
		"a.md": {Data: []byte("# A\n")},
	})
	if err != nil {
		t.Fatalf("NewBook: %v", err)
	}
	lib := help.NewLibrary()
	if err := lib.Register(b); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if ref := RefForApp(lib, appId); ref.Doc != "a" {
		t.Fatalf("got %v, want the first doc in path order", ref)
	}
}

func TestRefForApp_ZeroWithoutABookOrForHelpItself(t *testing.T) {
	lib, _ := fixtureLibrary(t)
	for _, appId := range []app.AppIdT{"", "github.com/test/no-book", ManifestId} {
		if ref := RefForApp(lib, appId); !ref.IsZero() {
			t.Fatalf("appId %q: got %v, want the zero ref", appId, ref)
		}
	}
}

// The raise half: a ref pushed while the window is open is adopted at its
// next Frame, and an empty queue leaves the selection alone.
func TestPushRef_DrainedInOrderAndZeroDropped(t *testing.T) {
	drainRefs()
	PushRef(help.RefT{})
	PushRef(help.RefT{AppId: "a", Doc: "x"})
	PushRef(help.RefT{AppId: "b", Doc: "y"})
	h := New()
	h.OpenRef(popRef())
	if got := h.CurrentRef(); got.AppId != "a" || got.Doc != "x" {
		t.Fatalf("first pop: got %v", got)
	}
	h.OpenRef(popRef())
	if got := h.CurrentRef(); got.AppId != "b" {
		t.Fatalf("second pop: got %v", got)
	}
	h.OpenRef(popRef())
	if got := h.CurrentRef(); got.AppId != "b" {
		t.Fatalf("an empty queue must leave the selection alone: got %v", got)
	}
}
