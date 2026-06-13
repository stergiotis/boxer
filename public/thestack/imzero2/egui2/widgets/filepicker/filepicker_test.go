package filepicker

import (
	"errors"
	"io/fs"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
	"time"
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

func TestSortDirEntries(t *testing.T) {
	es := []fs.DirEntry{
		testDirEntry{name: "Zfile.txt"},
		testDirEntry{name: "afile.txt"},
		testDirEntry{name: "ZDir", isDir: true},
		testDirEntry{name: "adir", isDir: true},
		testDirEntry{name: "Bfile.txt"},
	}
	sortDirEntries(es)
	got := make([]string, len(es))
	for i, e := range es {
		got[i] = e.Name()
	}
	want := []string{"adir", "ZDir", "afile.txt", "Bfile.txt", "Zfile.txt"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
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

func TestSplitBreadcrumbs(t *testing.T) {
	tests := []struct {
		name         string
		path         string
		wantSegs     []string
		wantPrefixes []string
	}{
		{"root-dot", ".", nil, nil},
		{"empty", "", nil, nil},
		{"shallow", "home", []string{"home"}, []string{"home"}},
		{"deep", "home/spx/repo", []string{"home", "spx", "repo"}, []string{"home", "home/spx", "home/spx/repo"}},
		{"trailing-slash", "home/spx/", []string{"home", "spx"}, []string{"home", "home/spx"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSegs, gotPrefixes := splitBreadcrumbs(tt.path)
			if !reflect.DeepEqual(gotSegs, tt.wantSegs) {
				t.Errorf("segs: got %v, want %v", gotSegs, tt.wantSegs)
			}
			if !reflect.DeepEqual(gotPrefixes, tt.wantPrefixes) {
				t.Errorf("prefixes: got %v, want %v", gotPrefixes, tt.wantPrefixes)
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
		if inst.cwd != "." {
			t.Errorf("cwd: got %q, want \".\"", inst.cwd)
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
		if inst.cwd != "some/path" {
			t.Errorf("cwd: got %q, want some/path", inst.cwd)
		}
	})
	t.Run("idempotent-when-already-open", func(t *testing.T) {
		fsys := fstest.MapFS{}
		inst := New("a", ModeOpen, WithFsBackend(fsys))
		inst.Show()
		inst.cwd = "elsewhere"
		inst.Show() // second Show should not reset cwd
		if inst.cwd != "elsewhere" {
			t.Errorf("cwd: got %q, expected unchanged", inst.cwd)
		}
	})
}

func TestHide(t *testing.T) {
	fsys := fstest.MapFS{
		"a": {},
	}
	inst := New("a", ModeOpen, WithFsBackend(fsys))
	inst.Show()
	inst.selected = "a"
	inst.lastErr = errors.New("stale")
	inst.Hide()
	if inst.IsOpen() {
		t.Error("expected hidden")
	}
	if inst.selected != "" {
		t.Error("expected selected cleared")
	}
	if inst.lastErr != nil {
		t.Error("expected lastErr cleared")
	}
}

func TestCanCommit(t *testing.T) {
	t.Run("open-needs-selection", func(t *testing.T) {
		inst := New("a", ModeOpen)
		if inst.canCommit() {
			t.Error("expected canCommit=false without selection")
		}
		inst.pickFile("x/y")
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
		inst.pickFile("path/to/file.txt")
		got := inst.commitPaths()
		want := []string{"path/to/file.txt"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})
	t.Run("save-joins-cwd-and-filename", func(t *testing.T) {
		inst := New("a", ModeSave)
		inst.cwd = "work/proj"
		inst.filename = "out.txt"
		got := inst.commitPaths()
		want := []string{"work/proj/out.txt"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})
	t.Run("save-cleans-and-trims-filename", func(t *testing.T) {
		inst := New("a", ModeSave)
		inst.cwd = "work/proj"
		inst.filename = "  sub/out.txt  "
		got := inst.commitPaths()
		want := []string{"work/proj/sub/out.txt"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})
	t.Run("open-applies-os-display-root", func(t *testing.T) {
		inst := New("a", ModeOpen, WithDisplayRoot("/"))
		inst.pickFile("home/spx/file.go")
		got := inst.commitPaths()
		want := []string{"/home/spx/file.go"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})
	t.Run("save-applies-sandbox-display-root", func(t *testing.T) {
		inst := New("a", ModeSave, WithDisplayRoot("/sandbox/"))
		inst.cwd = "conf"
		inst.filename = "app.toml"
		got := inst.commitPaths()
		want := []string{"/sandbox/conf/app.toml"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})
	t.Run("display-root-on-fs-root-cwd", func(t *testing.T) {
		inst := New("a", ModeSave, WithDisplayRoot("/"))
		inst.cwd = "."
		inst.filename = "x.txt"
		got := inst.commitPaths()
		want := []string{"/x.txt"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})
	t.Run("pickfolder-returns-cwd", func(t *testing.T) {
		inst := New("a", ModePickFolder)
		inst.cwd = "home/spx/repo"
		got := inst.commitPaths()
		want := []string{"home/spx/repo"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})
	t.Run("pickfolder-applies-display-root", func(t *testing.T) {
		inst := New("a", ModePickFolder, WithDisplayRoot("/"))
		inst.cwd = "home/spx/repo"
		got := inst.commitPaths()
		want := []string{"/home/spx/repo"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})
	t.Run("multiselect-returns-click-order", func(t *testing.T) {
		inst := New("a", ModeOpen, WithMultiSelect(true))
		// Click three files in a specific order — commitPaths must
		// preserve that order regardless of internal map iteration.
		inst.pickFile("a/x.go")
		inst.pickFile("b/y.go")
		inst.pickFile("a/z.go")
		got := inst.commitPaths()
		want := []string{"a/x.go", "b/y.go", "a/z.go"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})
	t.Run("multiselect-applies-display-root-per-path", func(t *testing.T) {
		inst := New("a", ModeOpen, WithMultiSelect(true), WithDisplayRoot("/"))
		inst.pickFile("home/a.go")
		inst.pickFile("home/b.go")
		got := inst.commitPaths()
		want := []string{"/home/a.go", "/home/b.go"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})
}

func TestApplyPendingCwd(t *testing.T) {
	fsys := fstest.MapFS{
		"h/sub/b.txt": {},
	}
	inst := New("a", ModeOpen, WithMultiSelect(true), WithFsBackend(fsys))
	inst.Show()
	inst.cache["h/sub"] = cachedDir{entries: []fs.DirEntry{testDirEntry{name: "stale"}}}
	// Populate via pickFile so the multi-select set is exercised too —
	// clearSelection must wipe both the active preview and the set.
	inst.pickFile("h/a.txt")
	inst.pickFile("h/c.txt")
	inst.lastErr = errors.New("stale")
	inst.pendingCwd = "h/sub"
	inst.applyPendingCwd()
	if inst.cwd != "h/sub" {
		t.Errorf("cwd: got %q", inst.cwd)
	}
	if inst.pendingCwd != "" {
		t.Error("expected pendingCwd cleared")
	}
	if inst.selected != "" {
		t.Error("expected selected cleared on cwd change")
	}
	if len(inst.selectedSet) != 0 {
		t.Errorf("expected selectedSet cleared, got %d entries", len(inst.selectedSet))
	}
	if len(inst.selectedOrdered) != 0 {
		t.Errorf("expected selectedOrdered cleared, got %d entries", len(inst.selectedOrdered))
	}
	if inst.lastErr != nil {
		t.Error("expected lastErr cleared on cwd change")
	}
	if _, exists := inst.cache["h/sub"]; exists {
		t.Error("expected cache entry for new cwd cleared")
	}
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

func TestPickFile_SingleSelectReplaces(t *testing.T) {
	inst := New("a", ModeOpen)
	inst.pickFile("first.go")
	if got, want := len(inst.selectedSet), 1; got != want {
		t.Fatalf("set size: got %d, want %d", got, want)
	}
	inst.pickFile("second.go")
	if got, want := len(inst.selectedSet), 1; got != want {
		t.Fatalf("set size after second click: got %d, want %d (single-pick replaces)", got, want)
	}
	if _, in := inst.selectedSet["second.go"]; !in {
		t.Error("expected second.go in set after replacement")
	}
	if inst.selected != "second.go" {
		t.Errorf("active path: got %q, want second.go", inst.selected)
	}
	if !reflect.DeepEqual(inst.selectedOrdered, []string{"second.go"}) {
		t.Errorf("ordered: got %v", inst.selectedOrdered)
	}
}

func TestPickFile_MultiSelectToggles(t *testing.T) {
	inst := New("a", ModeOpen, WithMultiSelect(true))
	inst.pickFile("a.go")
	inst.pickFile("b.go")
	inst.pickFile("c.go")
	if got, want := len(inst.selectedSet), 3; got != want {
		t.Fatalf("set size after 3 picks: got %d, want %d", got, want)
	}
	// Clicking b.go again toggles it out; ordered slice keeps the
	// remaining click order.
	inst.pickFile("b.go")
	if _, in := inst.selectedSet["b.go"]; in {
		t.Error("expected b.go removed on re-click")
	}
	if !reflect.DeepEqual(inst.selectedOrdered, []string{"a.go", "c.go"}) {
		t.Errorf("ordered after toggle-off: got %v", inst.selectedOrdered)
	}
	// inst.selected tracks the *last click*, even when that click
	// removed the file from the set — drives the stat-pane preview.
	if inst.selected != "b.go" {
		t.Errorf("active path: got %q, want b.go (last clicked, even on toggle-off)", inst.selected)
	}
	// Re-click b.go a third time: comes back, lands at the end of
	// the order.
	inst.pickFile("b.go")
	if !reflect.DeepEqual(inst.selectedOrdered, []string{"a.go", "c.go", "b.go"}) {
		t.Errorf("ordered after re-add: got %v", inst.selectedOrdered)
	}
}

func TestPickFolderModeFiltersFiles(t *testing.T) {
	// readDirSorted itself doesn't filter — the filtering lives in
	// renderListing's per-entry branch. Test the filter predicate
	// directly through what renderListing checks.
	dir := testDirEntry{name: "child", isDir: true}
	file := testDirEntry{name: "a.txt"}

	// In ModePickFolder the listing skips non-dir entries. There is
	// no helper exposed beyond the inline check, so this test
	// documents the contract via the public surface: ext filter is
	// still applied, and dirs always pass.
	if !passesExtFilter(dir, nil) {
		t.Error("dir must always pass ext filter")
	}
	if !passesExtFilter(file, nil) {
		t.Error("file with empty filter must pass ext filter")
	}
}

func TestRemoveOrdered(t *testing.T) {
	tests := []struct {
		name   string
		in     []string
		victim string
		want   []string
	}{
		{"empty", nil, "x", nil},
		{"single-match", []string{"a"}, "a", []string{}},
		{"single-no-match", []string{"a"}, "z", []string{"a"}},
		{"head", []string{"a", "b", "c"}, "a", []string{"b", "c"}},
		{"middle", []string{"a", "b", "c"}, "b", []string{"a", "c"}},
		{"tail", []string{"a", "b", "c"}, "c", []string{"a", "b"}},
		{"missing-keeps-order", []string{"a", "b", "c"}, "z", []string{"a", "b", "c"}},
		// Only the first occurrence is removed — toggle semantics
		// never produce duplicates, so a second occurrence would be
		// a bug, but we document the policy.
		{"only-first", []string{"a", "b", "a"}, "a", []string{"b", "a"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := removeOrdered(append([]string(nil), tt.in...), tt.victim)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
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

func TestIsHiddenName(t *testing.T) {
	tests := map[string]bool{
		"":           false,
		"file.go":    false,
		".":          true,
		"..":         true,
		".git":       true,
		".cache":     true,
		"a.b":        false,
		"foo.bar":    false,
		".hiddendir": true,
	}
	for in, want := range tests {
		if got := isHiddenName(in); got != want {
			t.Errorf("isHiddenName(%q) = %v, want %v", in, got, want)
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

func TestEntryVisible(t *testing.T) {
	t.Run("hidden-hidden-by-default", func(t *testing.T) {
		inst := New("a", ModeOpen)
		if inst.entryVisible(testDirEntry{name: ".git", isDir: true}) {
			t.Error("expected .git to hide by default")
		}
		if inst.entryVisible(testDirEntry{name: ".bashrc"}) {
			t.Error("expected .bashrc to hide by default")
		}
	})
	t.Run("hidden-shown-when-toggle-on", func(t *testing.T) {
		inst := New("a", ModeOpen, WithShowHiddenFiles(true))
		if !inst.entryVisible(testDirEntry{name: ".git", isDir: true}) {
			t.Error("expected .git to show with showHidden=true")
		}
	})
	t.Run("pickfolder-hides-files", func(t *testing.T) {
		inst := New("a", ModePickFolder)
		if inst.entryVisible(testDirEntry{name: "x.go"}) {
			t.Error("expected files to hide in ModePickFolder")
		}
		if !inst.entryVisible(testDirEntry{name: "sub", isDir: true}) {
			t.Error("expected dirs to show in ModePickFolder")
		}
	})
	t.Run("pickfolder-still-hides-dot-dirs", func(t *testing.T) {
		inst := New("a", ModePickFolder)
		if inst.entryVisible(testDirEntry{name: ".git", isDir: true}) {
			t.Error("expected .git/ to hide in ModePickFolder by default")
		}
	})
	t.Run("filter-skipped-for-dirs", func(t *testing.T) {
		inst := New("a", ModeOpen, WithExtensionFilter(".go"))
		if !inst.entryVisible(testDirEntry{name: "subdir", isDir: true}) {
			t.Error("expected dirs to bypass extension filter for navigation")
		}
	})
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

func TestReadDirSorted_NotFound(t *testing.T) {
	fsys := fstest.MapFS{}
	_, err := readDirSorted(fsys, "this/path/does/not/exist/xyz123")
	if err == nil {
		t.Error("expected error on missing path")
	}
}

func TestReadDirSorted_OnMapFS(t *testing.T) {
	fsys := fstest.MapFS{
		"home/file1.txt":  {Data: []byte("one")},
		"home/file2.go":   {Data: []byte("two")},
		"home/sub/nested": {Data: []byte("three")},
	}
	es, err := readDirSorted(fsys, "home")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	got := make([]string, len(es))
	for i, e := range es {
		prefix := "f:"
		if e.IsDir() {
			prefix = "d:"
		}
		got[i] = prefix + e.Name()
	}
	want := []string{"d:sub", "f:file1.txt", "f:file2.go"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}
