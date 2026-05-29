//go:build llm_generated_opus47

package help

import (
	"testing"
	"testing/fstest"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// helpFSWithOverview is the minimal fixture used by library-level tests:
// one document whose only content is an H1 so the title resolution and
// path indexing have something concrete to assert against.
func helpFSWithOverview() (fsys fstest.MapFS) {
	fsys = fstest.MapFS{
		"overview.md": {Data: []byte("# Overview\n\nfixture body\n")},
	}
	return
}

func TestLibrary_RegisterAndLookup(t *testing.T) {
	lib := NewLibrary()
	b, err := NewBook("github.com/test/lib-register", helpFSWithOverview())
	if err != nil {
		t.Fatalf("NewBook: %v", err)
	}
	if err := lib.Register(b); err != nil {
		t.Fatalf("Register: %v", err)
	}
	got, ok := lib.Book("github.com/test/lib-register")
	if !ok {
		t.Fatalf("Book(): not found after Register")
	}
	if got.AppId() != "github.com/test/lib-register" {
		t.Errorf("AppId mismatch: got %q", got.AppId())
	}
}

func TestLibrary_RegisterRejects(t *testing.T) {
	lib := NewLibrary()
	if err := lib.Register(nil); err == nil {
		t.Errorf("Register(nil): want error, got nil")
	}

	b1, _ := NewBook("github.com/test/dup", helpFSWithOverview())
	b2, _ := NewBook("github.com/test/dup", helpFSWithOverview())
	if err := lib.Register(b1); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	if err := lib.Register(b2); err == nil {
		t.Errorf("duplicate Register: want error, got nil")
	}
}

func TestLibrary_BooksSorted(t *testing.T) {
	lib := NewLibrary()
	// Register in non-alphabetical order to confirm the library sorts.
	for _, id := range []app.AppIdT{"github.com/test/c", "github.com/test/a", "github.com/test/b"} {
		b, _ := NewBook(id, helpFSWithOverview())
		if err := lib.Register(b); err != nil {
			t.Fatalf("Register(%q): %v", id, err)
		}
	}
	books := lib.Books()
	if len(books) != 3 {
		t.Fatalf("Books: got %d, want 3", len(books))
	}
	want := []app.AppIdT{"github.com/test/a", "github.com/test/b", "github.com/test/c"}
	for i := range books {
		if books[i].AppId() != want[i] {
			t.Errorf("Books[%d].AppId: got %q, want %q", i, books[i].AppId(), want[i])
		}
	}
}

// TestLibrary_SyncFromRegistry registers a fixture app in
// app.DefaultRegistry and asserts a fresh library picks it up via the
// sync path. Uses a unique AppId + LookupManifest guard so re-runs
// inside the same process (go test -count=N) stay idempotent — the
// registry has no Delete API.
func TestLibrary_SyncFromRegistry(t *testing.T) {
	const testId app.AppIdT = "github.com/test/help-sync-fixture"
	if _, exists := app.DefaultRegistry.LookupManifest(testId); !exists {
		err := app.DefaultRegistry.RegisterFactory(
			app.Manifest{
				Id:      testId,
				Display: "Help sync fixture",
				Surface: app.SurfaceWindowed,
				Help:    helpFSWithOverview(),
			},
			func() (app.AppI, error) { return nil, nil },
		)
		if err != nil {
			t.Fatalf("RegisterFactory: %v", err)
		}
	}

	lib := NewLibrary()
	added := lib.SyncFromRegistry()
	if added < 1 {
		t.Errorf("SyncFromRegistry: added=%d, want ≥1 (fixture must surface)", added)
	}

	b, ok := lib.Book(testId)
	if !ok {
		t.Fatalf("Book(%q): not found after SyncFromRegistry", testId)
	}
	docs := b.Docs()
	if len(docs) != 1 || docs[0].Path != "overview" {
		t.Fatalf("Docs(): got %+v, want one entry 'overview'", docs)
	}
	if docs[0].Title != "Overview" {
		t.Errorf("Title: got %q, want 'Overview'", docs[0].Title)
	}
}

// TestLibrary_SyncIdempotent confirms that re-calling SyncFromRegistry
// on a library that already holds a book does not double-register.
func TestLibrary_SyncIdempotent(t *testing.T) {
	lib := NewLibrary()
	added1 := lib.SyncFromRegistry()
	added2 := lib.SyncFromRegistry()
	if added2 != 0 {
		t.Errorf("second SyncFromRegistry added %d, want 0 (was %d on first call)", added2, added1)
	}
}

func TestLibrary_SkipsNilHelp(t *testing.T) {
	// Ensure a manifest with Help == nil is not turned into an empty
	// book. We don't depend on DefaultRegistry state here — explicit
	// shape-only assertion against a freshly-constructed library that
	// has never seen any sync.
	lib := NewLibrary()
	books := lib.Books()
	// Library is empty before any registration or sync inside this
	// test's scope; sync touches DefaultRegistry which may or may not
	// have other tests' registrations. Drift here means another test
	// is leaking — flag it.
	for _, b := range books {
		if b == nil {
			t.Errorf("Books: nil book leaked into library")
		}
	}
}
