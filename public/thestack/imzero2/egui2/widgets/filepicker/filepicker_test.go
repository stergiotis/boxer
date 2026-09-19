package filepicker

import (
	"errors"
	"io/fs"
	"path"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/fsbrowser"
)

// testDirEntry is a minimal fs.DirEntry stub for unit-testing
// sort/filter without going through a full fstest.MapFS round-trip.
type testDirEntry struct {
	name  string
	isDir bool
}

func (e testDirEntry) Name() string {
	return e.name
}

func (e testDirEntry) IsDir() bool {
	return e.isDir
}

func (e testDirEntry) Type() fs.FileMode {
	if e.isDir {
		return fs.ModeDir
	}
	return 0
}

func (e testDirEntry) Info() (info fs.FileInfo, err error) {
	return
}

// fileRow and dirRow are browser rows as a listing would report them.
func fileRow(p string) fsbrowser.Entry {
	return fsbrowser.Entry{Name: path.Base(p), Path: p}
}

func dirRow(p string) fsbrowser.Entry {
	return fsbrowser.Entry{Name: path.Base(p), Path: p, IsDir: true, Mode: fs.ModeDir}
}

// selectRows stands in for the browser reporting a selection change:
// the browser state holds exactly sel, cursor on the last of them, and
// the dialog re-derives its picks from rows.
func selectRows(inst *Inst, rows []fsbrowser.Entry, sel ...string) {
	inst.st.ClearSelection()
	for _, p := range sel {
		inst.st.Select(p, true)
		inst.st.SetCursor(p)
	}
	inst.syncPicks(rows)
}

func TestNormalizeExtensions(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{"empty", nil, nil},
		{"only-empty-strings", []string{"", "  ", "."}, nil},
		{"strip-dot-and-lowercase", []string{".GO", "Md", " .Txt "}, []string{"go", "md", "txt"}},
		{"keeps-duplicates", []string{".go", "go"}, []string{"go", "go"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeExtensions(tt.in)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPassesExtFilter(t *testing.T) {
	filter := []string{"go", "md"}
	tests := []struct {
		name   string
		entry  fs.DirEntry
		filter []string
		want   bool
	}{
		{"dir-always-passes", testDirEntry{name: "anywhere", isDir: true}, filter, true},
		{"empty-filter-everything-passes", testDirEntry{name: "x.bin"}, nil, true},
		{"matches-go", testDirEntry{name: "main.go"}, filter, true},
		{"matches-md-uppercase-ext", testDirEntry{name: "README.MD"}, filter, true},
		{"no-match", testDirEntry{name: "data.bin"}, filter, false},
		{"no-extension", testDirEntry{name: "Makefile"}, filter, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := passesExtFilter(tt.entry, tt.filter)
			if got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNew_DefaultsAndOptions(t *testing.T) {
	t.Run("open-mode-defaults", func(t *testing.T) {
		inst := New("a", ModeOpen)
		if inst.title != "Open" {
			t.Errorf("title: got %q, want Open", inst.title)
		}
		if inst.open {
			t.Error("expected hidden by default")
		}
		if inst.fsys == nil {
			t.Error("expected non-nil fsys")
		}
		if inst.displayRoot != "" {
			t.Errorf("expected empty displayRoot, got %q", inst.displayRoot)
		}
	})
	t.Run("save-mode-with-options", func(t *testing.T) {
		fsys := fstest.MapFS{}
		inst := New("b", ModeSave,
			WithTitle("Custom"),
			WithDefaultFilename("init.txt"),
			WithFsBackend(fsys),
			WithStartDir("start"),
			WithExtensionFilter(".GO", "md"))
		if inst.title != "Custom" {
			t.Errorf("title: got %q", inst.title)
		}
		if inst.filename != "init.txt" {
			t.Errorf("filename: got %q", inst.filename)
		}
		if inst.startDir != "start" {
			t.Errorf("startDir: got %q", inst.startDir)
		}
		if inst.fileFilter == nil {
			t.Error("expected fileFilter set by WithExtensionFilter")
		} else {
			if !inst.fileFilter(testDirEntry{name: "x.go"}) {
				t.Error("expected x.go to pass go|md filter")
			}
			if inst.fileFilter(testDirEntry{name: "z.txt"}) {
				t.Error("expected z.txt to be rejected by go|md filter")
			}
		}
		if inst.filterDesc != "go md" {
			t.Errorf("filterDesc: got %q, want %q", inst.filterDesc, "go md")
		}
		if _, ok := inst.fsys.(fstest.MapFS); !ok {
			t.Errorf("expected MapFS backend, got %T", inst.fsys)
		}
	})
	t.Run("two-instances-have-distinct-scope-keys-and-abs-ids", func(t *testing.T) {
		a := New("a", ModeOpen)
		b := New("b", ModeOpen)
		if a.scopeKey == b.scopeKey {
			t.Error("expected distinct scopeKeys")
		}
		if a.absId == b.absId {
			t.Error("expected distinct absIds")
		}
	})
	t.Run("nil-fs-option-is-ignored", func(t *testing.T) {
		inst := New("c", ModeOpen, WithFsBackend(nil))
		if inst.fsys == nil {
			t.Error("expected default fsys to remain after nil option")
		}
	})
	t.Run("empty-title-option-is-ignored", func(t *testing.T) {
		inst := New("d", ModeSave, WithTitle(""))
		if inst.title != "Save" {
			t.Errorf("title: got %q, want Save", inst.title)
		}
	})
	t.Run("display-root-option", func(t *testing.T) {
		inst := New("e", ModeOpen, WithDisplayRoot("/sandbox/"))
		if inst.displayRoot != "/sandbox/" {
			t.Errorf("expected displayRoot to be set, got %q", inst.displayRoot)
		}
	})
}

func TestShow(t *testing.T) {
	t.Run("default-cwd-is-fs-root", func(t *testing.T) {
		fsys := fstest.MapFS{
			"home/me/f.go": {},
		}
		inst := New("a", ModeOpen, WithFsBackend(fsys))
		inst.Show()
		if inst.st.Dir() != "." {
			t.Errorf("cwd: got %q, want \".\"", inst.st.Dir())
		}
		if !inst.IsOpen() {
			t.Error("expected open after Show")
		}
	})
	t.Run("respects-start-dir-override", func(t *testing.T) {
		fsys := fstest.MapFS{
			"some/path/f.txt": {},
		}
		inst := New("a", ModeOpen,
			WithFsBackend(fsys),
			WithStartDir("some/path"))
		inst.Show()
		if inst.st.Dir() != "some/path" {
			t.Errorf("cwd: got %q, want some/path", inst.st.Dir())
		}
	})
	t.Run("idempotent-when-already-open", func(t *testing.T) {
		fsys := fstest.MapFS{}
		inst := New("a", ModeOpen, WithFsBackend(fsys))
		inst.Show()
		inst.st.SetDir("elsewhere")
		inst.Show() // second Show should not reset cwd
		if inst.st.Dir() != "elsewhere" {
			t.Errorf("cwd: got %q, expected unchanged", inst.st.Dir())
		}
	})
	t.Run("re-show-keeps-cwd-over-start-dir", func(t *testing.T) {
		inst := New("a", ModeOpen, WithFsBackend(fstest.MapFS{}), WithStartDir("start"))
		inst.Show()
		inst.st.SetDir("elsewhere")
		inst.Hide()
		inst.Show()
		if inst.st.Dir() != "elsewhere" {
			t.Errorf("cwd: got %q, want the directory the user left", inst.st.Dir())
		}
	})
}

func TestHide(t *testing.T) {
	inst := New("a", ModeOpen, WithFsBackend(fstest.MapFS{"a": {}}))
	inst.Show()
	selectRows(inst, []fsbrowser.Entry{fileRow("a")}, "a")
	if inst.selected != "a" || len(inst.picked) != 1 {
		t.Fatalf("setup: selected %q, picked %v", inst.selected, inst.picked)
	}
	inst.Hide()
	if inst.IsOpen() {
		t.Error("expected hidden")
	}
	if inst.selected != "" || len(inst.picked) != 0 {
		t.Errorf("expected picks cleared, got selected %q, picked %v", inst.selected, inst.picked)
	}
	if len(inst.st.Selection()) != 0 {
		t.Error("expected the browser's selection cleared")
	}
}

func TestCanCommit(t *testing.T) {
	t.Run("open-needs-selection", func(t *testing.T) {
		inst := New("a", ModeOpen)
		if inst.canCommit() {
			t.Error("expected canCommit=false without selection")
		}
		selectRows(inst, []fsbrowser.Entry{dirRow("x/d"), fileRow("x/y")}, "x/d")
		if inst.canCommit() {
			t.Error("expected canCommit=false with only a directory selected")
		}
		selectRows(inst, []fsbrowser.Entry{dirRow("x/d"), fileRow("x/y")}, "x/y")
		if !inst.canCommit() {
			t.Error("expected canCommit=true with selection")
		}
	})
	t.Run("save-needs-filename", func(t *testing.T) {
		inst := New("a", ModeSave)
		if inst.canCommit() {
			t.Error("expected canCommit=false without filename")
		}
		inst.filename = "  "
		if inst.canCommit() {
			t.Error("expected canCommit=false with whitespace-only filename")
		}
		inst.filename = "out.txt"
		if !inst.canCommit() {
			t.Error("expected canCommit=true with filename")
		}
	})
	t.Run("pickfolder-always-commitable", func(t *testing.T) {
		inst := New("a", ModePickFolder)
		// Even at the FS root with no selection, ModePickFolder
		// commits the cwd. canCommit() must be true.
		if !inst.canCommit() {
			t.Error("expected canCommit=true for ModePickFolder")
		}
	})
}

func TestCommitPaths(t *testing.T) {
	t.Run("open-single-returns-one-path", func(t *testing.T) {
		inst := New("a", ModeOpen)
		selectRows(inst, []fsbrowser.Entry{fileRow("path/to/file.txt")}, "path/to/file.txt")
		got := inst.commitPaths()
		want := []string{"path/to/file.txt"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})
	t.Run("save-joins-cwd-and-filename", func(t *testing.T) {
		inst := New("a", ModeSave)
		inst.st.SetDir("work/proj")
		inst.filename = "out.txt"
		got := inst.commitPaths()
		want := []string{"work/proj/out.txt"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})
	t.Run("save-cleans-and-trims-filename", func(t *testing.T) {
		inst := New("a", ModeSave)
		inst.st.SetDir("work/proj")
		inst.filename = "  sub/out.txt  "
		got := inst.commitPaths()
		want := []string{"work/proj/sub/out.txt"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})
	t.Run("open-applies-os-display-root", func(t *testing.T) {
		inst := New("a", ModeOpen, WithDisplayRoot("/"))
		selectRows(inst, []fsbrowser.Entry{fileRow("home/test-user/file.go")}, "home/test-user/file.go")
		got := inst.commitPaths()
		want := []string{"/home/test-user/file.go"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})
	t.Run("save-applies-sandbox-display-root", func(t *testing.T) {
		inst := New("a", ModeSave, WithDisplayRoot("/sandbox/"))
		inst.st.SetDir("conf")
		inst.filename = "app.toml"
		got := inst.commitPaths()
		want := []string{"/sandbox/conf/app.toml"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})
	t.Run("display-root-on-fs-root-cwd", func(t *testing.T) {
		inst := New("a", ModeSave, WithDisplayRoot("/"))
		inst.st.SetDir(".")
		inst.filename = "x.txt"
		got := inst.commitPaths()
		want := []string{"/x.txt"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})
	t.Run("pickfolder-returns-cwd", func(t *testing.T) {
		inst := New("a", ModePickFolder)
		inst.st.SetDir("home/test-user/repo")
		got := inst.commitPaths()
		want := []string{"home/test-user/repo"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})
	t.Run("pickfolder-applies-display-root", func(t *testing.T) {
		inst := New("a", ModePickFolder, WithDisplayRoot("/"))
		inst.st.SetDir("home/test-user/repo")
		got := inst.commitPaths()
		want := []string{"/home/test-user/repo"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})
	t.Run("pickfolder-returns-the-one-selected-directory", func(t *testing.T) {
		inst := New("a", ModePickFolder, WithDisplayRoot("/"))
		inst.st.SetDir("home/test-user")
		rows := []fsbrowser.Entry{dirRow("home/test-user/repo"), dirRow("home/test-user/tmp")}
		selectRows(inst, rows, "home/test-user/repo")
		got := inst.commitPaths()
		want := []string{"/home/test-user/repo"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})
	t.Run("multiselect-returns-pick-order", func(t *testing.T) {
		inst := New("a", ModeOpen, WithMultiSelect(true))
		// Pick three files in a specific order — commitPaths must
		// preserve that order although the browser's selection is a set.
		rows := []fsbrowser.Entry{fileRow("d/x.go"), fileRow("d/y.go"), fileRow("d/z.go")}
		selectRows(inst, rows, "d/z.go")
		selectRows(inst, rows, "d/z.go", "d/x.go")
		selectRows(inst, rows, "d/z.go", "d/x.go", "d/y.go")
		got := inst.commitPaths()
		want := []string{"d/z.go", "d/x.go", "d/y.go"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})
	t.Run("multiselect-applies-display-root-per-path", func(t *testing.T) {
		inst := New("a", ModeOpen, WithMultiSelect(true), WithDisplayRoot("/"))
		rows := []fsbrowser.Entry{fileRow("home/a.go"), fileRow("home/b.go")}
		selectRows(inst, rows, "home/a.go", "home/b.go")
		got := inst.commitPaths()
		want := []string{"/home/a.go", "/home/b.go"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})
}

func TestRefreshStat(t *testing.T) {
	mtime := time.Date(2024, 3, 15, 14, 30, 0, 0, time.UTC)
	fsys := fstest.MapFS{
		"home/file.txt": &fstest.MapFile{
			Data:    []byte("hello world"),
			Mode:    0o644,
			ModTime: mtime,
		},
	}
	inst := New("a", ModeOpen, WithFsBackend(fsys))

	t.Run("empty-selection-clears-cache", func(t *testing.T) {
		inst.selected = ""
		inst.selectedInfo = nil
		inst.selectedStatErr = nil
		inst.selectedStatPath = "stale"
		inst.refreshStat()
		if inst.selectedInfo != nil {
			t.Error("expected nil info on empty selection")
		}
		if inst.selectedStatErr != nil {
			t.Error("expected nil err on empty selection")
		}
		if inst.selectedStatPath != "" {
			t.Errorf("expected empty cache key, got %q", inst.selectedStatPath)
		}
	})

	t.Run("populates-on-first-stat", func(t *testing.T) {
		inst.selected = "home/file.txt"
		inst.selectedStatPath = ""
		inst.refreshStat()
		if inst.selectedInfo == nil {
			t.Fatal("expected info populated")
		}
		if inst.selectedInfo.Size() != int64(len("hello world")) {
			t.Errorf("size: got %d, want %d", inst.selectedInfo.Size(), len("hello world"))
		}
		if !inst.selectedInfo.ModTime().Equal(mtime) {
			t.Errorf("mtime: got %v, want %v", inst.selectedInfo.ModTime(), mtime)
		}
		if inst.selectedStatPath != "home/file.txt" {
			t.Errorf("cache key: got %q", inst.selectedStatPath)
		}
	})

	t.Run("cache-hit-skips-restat", func(t *testing.T) {
		inst.selected = "home/file.txt"
		inst.selectedStatPath = "home/file.txt"
		inst.selectedInfo = nil // sentinel — refreshStat must NOT repopulate
		inst.refreshStat()
		if inst.selectedInfo != nil {
			t.Error("expected cache hit (nil sentinel preserved); got refresh")
		}
	})

	t.Run("error-on-missing-file", func(t *testing.T) {
		inst.selected = "no/such/path"
		inst.selectedStatPath = ""
		inst.refreshStat()
		if inst.selectedStatErr == nil {
			t.Error("expected stat error on missing path")
		}
		if inst.selectedInfo != nil {
			t.Error("expected nil info on stat error")
		}
		if inst.selectedStatPath != "no/such/path" {
			t.Errorf("expected cache key set even on error, got %q", inst.selectedStatPath)
		}
	})
}

func TestActionEString(t *testing.T) {
	tests := map[ActionE]string{
		ActionNone:       "none",
		ActionOpen:       "open",
		ActionSave:       "save",
		ActionCancel:     "cancel",
		ActionPickFolder: "pick-folder",
		ActionE(99):      "<invalid>",
	}
	for in, want := range tests {
		if got := in.String(); got != want {
			t.Errorf("ActionE(%d).String() = %q, want %q", in, got, want)
		}
	}
}

func TestNew_TitleDefaultsByMode(t *testing.T) {
	cases := map[ModeE]string{
		ModeOpen:       "Open",
		ModeSave:       "Save",
		ModePickFolder: "Pick folder",
	}
	for mode, want := range cases {
		inst := New("a", mode)
		if inst.title != want {
			t.Errorf("mode %d: got title %q, want %q", mode, inst.title, want)
		}
	}
}

func TestPrimaryButtonFor(t *testing.T) {
	cases := []struct {
		mode       ModeE
		wantLabel  string
		wantAction ActionE
	}{
		{ModeOpen, "Open", ActionOpen},
		{ModeSave, "Save", ActionSave},
		{ModePickFolder, "Pick This Folder", ActionPickFolder},
		// Unknown mode falls through to Open — keeps the footer
		// renderable even if a future mode lands without updating
		// the switch.
		{ModeE(99), "Open", ActionOpen},
	}
	for _, tc := range cases {
		gotLabel, gotAction := primaryButtonFor(tc.mode)
		if gotLabel != tc.wantLabel || gotAction != tc.wantAction {
			t.Errorf("mode %d: got (%q, %d), want (%q, %d)",
				tc.mode, gotLabel, gotAction, tc.wantLabel, tc.wantAction)
		}
	}
}

func TestWithGlobFilter(t *testing.T) {
	t.Run("matches-go-files", func(t *testing.T) {
		inst := New("a", ModeOpen, WithGlobFilter("*.go"))
		if !inst.fileFilter(testDirEntry{name: "main.go"}) {
			t.Error("expected main.go to pass *.go")
		}
		if inst.fileFilter(testDirEntry{name: "doc.md"}) {
			t.Error("expected doc.md to be rejected by *.go")
		}
	})
	t.Run("multiple-patterns-or-combined", func(t *testing.T) {
		inst := New("a", ModeOpen, WithGlobFilter("*.go", "*.md"))
		if !inst.fileFilter(testDirEntry{name: "main.go"}) {
			t.Error("expected main.go to pass go|md glob")
		}
		if !inst.fileFilter(testDirEntry{name: "README.md"}) {
			t.Error("expected README.md to pass go|md glob")
		}
		if inst.fileFilter(testDirEntry{name: "data.bin"}) {
			t.Error("expected data.bin to be rejected")
		}
	})
	t.Run("dirs-always-pass", func(t *testing.T) {
		inst := New("a", ModeOpen, WithGlobFilter("*.go"))
		if !inst.fileFilter(testDirEntry{name: "subdir", isDir: true}) {
			t.Error("expected dirs to bypass glob filter for navigation")
		}
	})
	t.Run("malformed-pattern-skipped-not-panicked", func(t *testing.T) {
		// path.Match errors on "[" (unclosed bracket). A typo in a host
		// config must not crash the dialog — the bad pattern silently
		// drops out and any remaining good patterns keep working.
		inst := New("a", ModeOpen, WithGlobFilter("[", "*.go"))
		if !inst.fileFilter(testDirEntry{name: "main.go"}) {
			t.Error("expected main.go to pass despite malformed sibling")
		}
		if inst.fileFilter(testDirEntry{name: "data.bin"}) {
			t.Error("expected data.bin to be rejected (malformed pattern doesn't open the gate)")
		}
	})
	t.Run("empty-patterns-disable-filter", func(t *testing.T) {
		inst := New("a", ModeOpen, WithGlobFilter("  ", ""))
		if inst.fileFilter != nil {
			t.Error("expected fileFilter to remain nil when all patterns whitespace/empty")
		}
		if inst.filterDesc != "" {
			t.Errorf("filterDesc: got %q, want empty", inst.filterDesc)
		}
	})
	t.Run("matches-basename-not-path", func(t *testing.T) {
		// path.Match("*.go", "vendor/x.go") is false — '*' doesn't cross
		// separators. The picker passes only the basename to the
		// predicate, so this test documents that contract.
		inst := New("a", ModeOpen, WithGlobFilter("*.go"))
		// The DirEntry name is just the leaf — no slash. Glob applies
		// to the leaf only.
		if !inst.fileFilter(testDirEntry{name: "x.go"}) {
			t.Error("expected leaf-only x.go to pass *.go")
		}
	})
}

func TestWithFilter(t *testing.T) {
	t.Run("custom-predicate", func(t *testing.T) {
		// Hide anything starting with "_", desc "no underscore".
		pred := func(de fs.DirEntry) bool {
			return !strings.HasPrefix(de.Name(), "_")
		}
		inst := New("a", ModeOpen, WithFilter(pred, "no underscore"))
		if !inst.fileFilter(testDirEntry{name: "main.go"}) {
			t.Error("expected main.go to pass")
		}
		if inst.fileFilter(testDirEntry{name: "_skip.go"}) {
			t.Error("expected _skip.go to be hidden")
		}
		if inst.filterDesc != "no underscore" {
			t.Errorf("filterDesc: got %q", inst.filterDesc)
		}
	})
	t.Run("nil-predicate-disables-filter", func(t *testing.T) {
		inst := New("a", ModeOpen,
			WithExtensionFilter(".go"), // set a filter first
			WithFilter(nil, ""))        // then clear it via WithFilter
		if inst.fileFilter != nil {
			t.Error("expected nil predicate to clear fileFilter")
		}
	})
	t.Run("last-option-wins", func(t *testing.T) {
		// Extension filter installed first, then overridden by a glob.
		inst := New("a", ModeOpen,
			WithExtensionFilter(".bin"),
			WithGlobFilter("*.go"))
		if !inst.fileFilter(testDirEntry{name: "main.go"}) {
			t.Error("expected glob to override ext filter")
		}
		if inst.fileFilter(testDirEntry{name: "data.bin"}) {
			t.Error("expected ext filter to be discarded")
		}
		if inst.filterDesc != "*.go" {
			t.Errorf("filterDesc: got %q, want *.go", inst.filterDesc)
		}
	})
}

func TestReconcilePicks(t *testing.T) {
	files := map[string]bool{"a.go": false, "b.go": false, "c.go": false, "sub": true}
	t.Run("a-toggled-out-pick-goes-and-comes-back-last", func(t *testing.T) {
		picks := reconcilePicks(nil, []string{"b.go"}, files)
		picks = reconcilePicks(picks, []string{"a.go", "b.go"}, files)
		picks = reconcilePicks(picks, []string{"a.go", "b.go", "c.go"}, files)
		if want := []string{"b.go", "a.go", "c.go"}; !reflect.DeepEqual(picks, want) {
			t.Fatalf("got %v, want %v", picks, want)
		}
		picks = reconcilePicks(picks, []string{"b.go", "c.go"}, files)
		picks = reconcilePicks(picks, []string{"a.go", "b.go", "c.go"}, files)
		if want := []string{"b.go", "c.go", "a.go"}; !reflect.DeepEqual(picks, want) {
			t.Errorf("got %v, want %v", picks, want)
		}
	})
	t.Run("a-replaced-selection-replaces-the-picks", func(t *testing.T) {
		picks := reconcilePicks([]string{"a.go"}, []string{"b.go"}, files)
		if want := []string{"b.go"}; !reflect.DeepEqual(picks, want) {
			t.Errorf("got %v, want %v", picks, want)
		}
	})
	t.Run("a-directory-is-never-a-pick", func(t *testing.T) {
		picks := reconcilePicks(nil, []string{"a.go", "sub"}, files)
		if want := []string{"a.go"}; !reflect.DeepEqual(picks, want) {
			t.Errorf("got %v, want %v", picks, want)
		}
	})
	t.Run("a-row-the-listing-dropped-stays-picked-but-is-not-made-one", func(t *testing.T) {
		picks := reconcilePicks([]string{"gone.go"}, []string{"gone.go", "unseen.go"}, files)
		if want := []string{"gone.go"}; !reflect.DeepEqual(picks, want) {
			t.Errorf("got %v, want %v", picks, want)
		}
	})
}

func TestSyncPicks(t *testing.T) {
	rows := []fsbrowser.Entry{dirRow("h/sub"), fileRow("h/a.go"), fileRow("h/b.go")}
	t.Run("the-stat-pane-follows-the-cursor-when-it-is-a-picked-file", func(t *testing.T) {
		inst := New("a", ModeOpen, WithMultiSelect(true))
		selectRows(inst, rows, "h/a.go", "h/b.go")
		if inst.selected != "h/b.go" {
			t.Errorf("selected: got %q, want h/b.go", inst.selected)
		}
		selectRows(inst, rows, "h/sub")
		if inst.selected != "" || inst.pickedDir != "h/sub" {
			t.Errorf("a selected directory: selected %q, pickedDir %q", inst.selected, inst.pickedDir)
		}
	})
	t.Run("navigation-clears-the-picks", func(t *testing.T) {
		inst := New("a", ModeOpen)
		selectRows(inst, rows, "h/a.go")
		inst.st.SetDir("h/sub") // the browser clears its selection on a directory change
		inst.syncPicks(nil)
		if len(inst.picked) != 0 || inst.selected != "" {
			t.Errorf("expected picks cleared, got picked %v, selected %q", inst.picked, inst.selected)
		}
	})
	t.Run("pickedDir-needs-exactly-one-directory", func(t *testing.T) {
		inst := New("a", ModeOpen, WithMultiSelect(true))
		selectRows(inst, rows, "h/sub", "h/a.go")
		if inst.pickedDir != "" {
			t.Errorf("pickedDir: got %q, want none for a mixed selection", inst.pickedDir)
		}
	})
}

func TestKeep(t *testing.T) {
	t.Run("pickfolder-lists-directories-only", func(t *testing.T) {
		inst := New("a", ModePickFolder)
		if inst.keep(fileRow("x.go")) {
			t.Error("expected files to hide in ModePickFolder")
		}
		if !inst.keep(dirRow("sub")) {
			t.Error("expected dirs to show in ModePickFolder")
		}
	})
	t.Run("filter-skipped-for-dirs", func(t *testing.T) {
		inst := New("a", ModeOpen, WithExtensionFilter(".go"))
		if !inst.keep(dirRow("subdir.d")) {
			t.Error("expected dirs to bypass extension filter for navigation")
		}
		if inst.keep(fileRow("notes.md")) {
			t.Error("expected notes.md rejected by the .go filter")
		}
		if !inst.keep(fileRow("main.go")) {
			t.Error("expected main.go to pass the .go filter")
		}
	})
	t.Run("no-filter-keeps-everything", func(t *testing.T) {
		inst := New("a", ModeSave)
		if !inst.keep(fileRow("anything.bin")) {
			t.Error("expected every file kept without a filter")
		}
	})
}

func TestEntryAsDirEntry(t *testing.T) {
	mtime := time.Date(2024, 3, 15, 14, 30, 0, 0, time.UTC)
	e := fsbrowser.Entry{Name: "link", Path: "d/link", Mode: fs.ModeSymlink | 0o777, Size: 7, ModTime: mtime}
	de := entryAsDirEntry{e: e}
	if de.Name() != "link" || de.IsDir() {
		t.Errorf("name %q, isDir %v", de.Name(), de.IsDir())
	}
	if de.Type() != fs.ModeSymlink {
		t.Errorf("Type: got %v, want the type bits alone", de.Type())
	}
	info, err := de.Info()
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if info.Size() != 7 || !info.ModTime().Equal(mtime) || info.Mode() != e.Mode {
		t.Errorf("info: size %d, mtime %v, mode %v", info.Size(), info.ModTime(), info.Mode())
	}
	bad := errors.New("no info")
	if _, err = (entryAsDirEntry{e: fsbrowser.Entry{Name: "x", InfoErr: bad}}).Info(); !errors.Is(err, bad) {
		t.Errorf("Info: got %v, want the listing's error", err)
	}
}

func TestWithShowHiddenFilesOption(t *testing.T) {
	t.Run("default-off", func(t *testing.T) {
		inst := New("a", ModeOpen)
		if inst.showHidden {
			t.Error("expected showHidden=false by default")
		}
	})
	t.Run("opted-on", func(t *testing.T) {
		inst := New("a", ModeOpen, WithShowHiddenFiles(true))
		if !inst.showHidden {
			t.Error("expected showHidden=true after option")
		}
	})
}

func TestColumnWidths(t *testing.T) {
	t.Run("resolver-is-keyed-to-the-dialog-and-loads", func(t *testing.T) {
		store := factsstore.NewInMemoryFactsStore()
		_, err := store.WriteColumnWidth(factsstore.ColumnWidthRow{
			AppId: AppId, Tier: factsstore.ColWidthTierColumn,
			ColumnKey: "k", Points: 123,
		})
		if err != nil {
			t.Fatalf("seed: %v", err)
		}
		res, err := NewColumnWidths(store)
		if err != nil || res == nil {
			t.Fatalf("NewColumnWidths: res %v, err %v", res, err)
		}
		if res.Len() != 1 {
			t.Errorf("Len: got %d, want the one stored override", res.Len())
		}
	})
	t.Run("option-and-setter", func(t *testing.T) {
		res, err := NewColumnWidths(factsstore.NewInMemoryFactsStore())
		if err != nil {
			t.Fatalf("NewColumnWidths: %v", err)
		}
		if inst := New("a", ModeOpen, WithColumnWidths(res)); inst.widths != res {
			t.Error("expected the option to set the resolver")
		}
		inst := New("b", ModeSave)
		inst.SetColumnWidths(res)
		if inst.widths != res {
			t.Error("expected the setter to set the resolver")
		}
	})
	t.Run("the-tag-is-the-mode-not-the-instance", func(t *testing.T) {
		a, b := New("req-1", ModeOpen), New("req-2", ModeOpen, WithMultiSelect(true))
		if a.widthTag() != b.widthTag() {
			t.Errorf("two open dialogs: %q vs %q", a.widthTag(), b.widthTag())
		}
		if New("s", ModeSave).widthTag() == a.widthTag() {
			t.Error("expected save and open to be different tables")
		}
	})
}
