package fsbrowser

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"time"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/tree"
)

// ModeE selects how the current directory is shown.
type ModeE uint8

const (
	// ModeList is one directory as a sortable table, directories first.
	ModeList ModeE = iota
	// ModeOutline is the tree under the current directory, loaded on expand.
	ModeOutline
)

// SortByE is the column a listing is ordered by. Directories always sort
// before files whatever the column; the column orders within each group.
type SortByE uint8

const (
	SortByName SortByE = iota
	SortBySize
	SortByModTime
)

// Entry is one directory child as the browser shows it, read from the
// [fs.DirEntry] and its [fs.FileInfo] once per listing.
type Entry struct {
	// Name is the base name; Path the io/fs path from the root ("a/b/c").
	Name string
	Path string
	// IsDir and IsSymlink come off the entry's type bits — a symlink is
	// reported as recorded, not resolved, which is the Lstat reading an
	// [fs.ReadDirFS] gives.
	IsDir     bool
	IsSymlink bool
	Size      int64
	ModTime   time.Time
	Mode      fs.FileMode
	// InfoErr is the error [fs.DirEntry.Info] returned, when it did; the entry
	// still lists, with zero size and time, because a name that cannot be
	// stat'ed is still a name.
	InfoErr error
	// Ord is the entry's ordinal in its directory's listing, stable for as
	// long as the listing is cached. Widget ids key on it, so sorting and
	// filtering move rows without moving identities.
	Ord int
}

// Column is one host-supplied column beside the built-in three.
type Column struct {
	Header    string
	Width     float32
	Resizable bool
	// Cell draws the cell for e. Emit labels Selectable(false): the row's
	// click sense sits behind its cells, and a selectable label would take
	// the click.
	Cell func(e Entry)
}

// Input is one frame's rendering request.
type Input struct {
	Ids      *c.WidgetIdStack
	ScopeKey string
	// FS is the tree to browse. nil renders a placeholder.
	FS fs.FS
	// RootLabel names the root in the breadcrumb ("/" when empty).
	RootLabel string
	// CacheKey scopes the directory cache; a change drops it and the
	// selection and keeps the directory.
	CacheKey string
	State    *State
	Mode     ModeE
	Columns  []Column
	// ShowHidden includes dot-names.
	ShowHidden bool
	// RowHeight, MaxHeight and Striped are passed to the table; zero means
	// the defaults (one text line; fill; plain).
	RowHeight float32
	MaxHeight float32
	Striped   bool
	// HideBreadcrumb and HideFilter drop those rows when the host draws its
	// own location and filter chrome.
	HideBreadcrumb bool
	HideFilter     bool
}

// Result is what one Render reports.
type Result struct {
	// Rows is what was shown: the filtered, sorted listing in list mode, one
	// entry per outline node in outline mode. Indices below refer to it.
	Rows []Entry
	// Clicked is the row clicked this frame, -1 for none.
	Clicked int
	// Activated is the file row double-clicked or Enter-ed, -1 for none. A
	// directory activation is consumed by the widget (entered or toggled)
	// and reported as Navigated instead.
	Activated int
	// Navigated is true when the current directory changed this frame.
	Navigated bool
	// SelectionChanged is true when the selection or cursor moved.
	SelectionChanged bool
	Err              error
}

// listing is one cached directory read.
type listing struct {
	entries []Entry
	err     error
}

// State is the host-owned view state. Its zero value is a browser at the root
// with no filter, sorted by name, nothing selected.
type State struct {
	dir      string
	filter   string
	sortBy   SortByE
	sortDesc bool
	selected map[string]struct{}
	cursor   string

	cache    map[string]*listing
	cacheKey string

	// list-mode scratch and the key capture's id
	keyFrameID      uint64
	lastVisibleRows int
	rows            []Entry

	// outline mode
	tree      tree.State
	nodes     []Entry
	outlineT  tree.Tree
	loadedDir map[string]bool
}

func (st *State) ensure() {
	if st.selected == nil {
		st.selected = make(map[string]struct{}, 8)
	}
	if st.cache == nil {
		st.cache = make(map[string]*listing, 16)
	}
	if st.dir == "" {
		st.dir = "."
	}
}

// Dir is the current directory as an io/fs path; "." is the root.
func (st *State) Dir() string {
	if st.dir == "" {
		return "."
	}
	return st.dir
}

// SetDir changes the current directory and clears the selection and the
// cursor — selection is per directory, as in every file manager. The path is
// cleaned; "" and "/" mean the root. It is not checked against the tree: a
// directory that does not exist lists as an error row.
func (st *State) SetDir(dir string) {
	st.ensure()
	dir = path.Clean(strings.TrimPrefix(dir, "/"))
	if dir == "" || dir == "/" {
		dir = "."
	}
	if dir == st.dir {
		return
	}
	st.dir = dir
	st.clearSelection()
}

// Up moves to the parent directory; false at the root.
func (st *State) Up() bool {
	st.ensure()
	if st.dir == "." {
		return false
	}
	st.SetDir(path.Dir(st.dir))
	return true
}

// Filter is the quick filter text; SetFilter replaces it.
func (st *State) Filter() string     { return st.filter }
func (st *State) SetFilter(s string) { st.filter = s }

// Sort is the current order; SetSort replaces it.
func (st *State) Sort() (by SortByE, desc bool) { return st.sortBy, st.sortDesc }
func (st *State) SetSort(by SortByE, desc bool) {
	st.sortBy, st.sortDesc = by, desc
}

// Selection is the selected paths, sorted.
func (st *State) Selection() (paths []string) {
	if len(st.selected) == 0 {
		return nil
	}
	paths = make([]string, 0, len(st.selected))
	for p := range st.selected {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return
}

// IsSelected reports whether p is selected.
func (st *State) IsSelected(p string) bool {
	_, ok := st.selected[p]
	return ok
}

// Select sets p's selection; SelectOnly makes p the whole selection and the
// cursor; ClearSelection empties it.
func (st *State) Select(p string, on bool) {
	st.ensure()
	if on {
		st.selected[p] = struct{}{}
	} else {
		delete(st.selected, p)
	}
}

func (st *State) SelectOnly(p string) {
	st.ensure()
	clear(st.selected)
	st.selected[p] = struct{}{}
	st.cursor = p
}

func (st *State) ClearSelection() { st.clearSelection() }

func (st *State) clearSelection() {
	st.ensure()
	clear(st.selected)
	st.cursor = ""
}

// Cursor is the keyboard cursor's path, "" for none.
func (st *State) Cursor() string     { return st.cursor }
func (st *State) SetCursor(p string) { st.cursor = p }

// Invalidate drops every cached listing, so the next frame re-reads what it
// shows. The selection and the directory stay.
func (st *State) Invalidate() {
	st.cache = nil
	st.loadedDir = nil
	st.ensure()
}

// rekey adopts a new cache key: the cache goes, the selection goes, the
// directory stays (ADR-0200 §SD3: the same path across two snapshots).
func (st *State) rekey(key string) (changed bool) {
	st.ensure()
	if key == st.cacheKey {
		return false
	}
	st.cacheKey = key
	st.cache = make(map[string]*listing, 16)
	st.loadedDir = nil
	st.clearSelection()
	return true
}

// read returns dir's listing, from the cache or from fsys.
func (st *State) read(fsys fs.FS, dir string) *listing {
	st.ensure()
	if l, ok := st.cache[dir]; ok {
		return l
	}
	l := readListing(fsys, dir)
	st.cache[dir] = l
	return l
}

// readListing reads one directory: every child with its info, in the order
// fs.ReadDir returns them (by name, which is what Ord then indexes).
func readListing(fsys fs.FS, dir string) (l *listing) {
	l = &listing{}
	if fsys == nil {
		l.err = errors.New("fsbrowser: no file system")
		return
	}
	des, err := fs.ReadDir(fsys, dir)
	if err != nil {
		l.err = err
		return
	}
	l.entries = make([]Entry, 0, len(des))
	for i, de := range des {
		e := Entry{
			Name:      de.Name(),
			Path:      joinPath(dir, de.Name()),
			IsDir:     de.IsDir(),
			IsSymlink: de.Type()&fs.ModeSymlink != 0,
			Mode:      de.Type(),
			Ord:       i,
		}
		if info, ierr := de.Info(); ierr != nil {
			e.InfoErr = ierr
		} else {
			e.Size = info.Size()
			e.ModTime = info.ModTime()
			e.Mode = info.Mode()
		}
		l.entries = append(l.entries, e)
	}
	return
}

func joinPath(dir, name string) string {
	if dir == "." || dir == "" {
		return name
	}
	return dir + "/" + name
}

// view filters and sorts a listing into dst: hidden names out unless asked
// for, the quick filter as a case-insensitive substring of the name,
// directories first, then the chosen order.
func (st *State) view(l *listing, showHidden bool, dst []Entry) []Entry {
	dst = dst[:0]
	if l == nil {
		return dst
	}
	needle := strings.ToLower(strings.TrimSpace(st.filter))
	for _, e := range l.entries {
		if !showHidden && strings.HasPrefix(e.Name, ".") {
			continue
		}
		if needle != "" && !strings.Contains(strings.ToLower(e.Name), needle) {
			continue
		}
		dst = append(dst, e)
	}
	sortEntries(dst, st.sortBy, st.sortDesc)
	return dst
}

func sortEntries(es []Entry, by SortByE, desc bool) {
	sort.SliceStable(es, func(i, j int) bool {
		a, b := es[i], es[j]
		if a.IsDir != b.IsDir {
			return a.IsDir
		}
		var less bool
		switch by {
		case SortBySize:
			if a.Size != b.Size {
				less = a.Size < b.Size
			} else {
				less = a.Name < b.Name
			}
		case SortByModTime:
			if !a.ModTime.Equal(b.ModTime) {
				less = a.ModTime.Before(b.ModTime)
			} else {
				less = a.Name < b.Name
			}
		default:
			less = strings.ToLower(a.Name) < strings.ToLower(b.Name) ||
				(strings.EqualFold(a.Name, b.Name) && a.Name < b.Name)
		}
		if desc {
			return !less && !(a.Name == b.Name && a.Size == b.Size && a.ModTime.Equal(b.ModTime))
		}
		return less
	})
}

// humanBytes renders a size the way a file manager does — IEC units, one
// decimal above a kibibyte.
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit && exp < 5; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// formatTime renders a modification time for a column: local date and time
// to the minute, empty for the zero time.
func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Local().Format("2006-01-02 15:04")
}
