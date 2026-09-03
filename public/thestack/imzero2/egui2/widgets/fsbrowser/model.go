package fsbrowser

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/fs/fsmatch"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/colwidth"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/regexedit"
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
	// WidthType discriminates this column for width persistence (ADR-0151
	// keys the column tier on name and render type); empty means "host".
	WidthType string
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
	// the defaults (one text line; the table's own auto-fit, capped at
	// ETABLE_AUTOFIT_CAP_PX; plain). MaxHeight is a ceiling — a listing
	// shorter than it renders at its own size — so feed it the pane's
	// measured height to bound the browser by its pane.
	RowHeight float32
	MaxHeight float32
	Striped   bool
	// HideBreadcrumb and HideFilter drop those rows when the host draws its
	// own location and filter chrome.
	HideBreadcrumb bool
	HideFilter     bool
	// Widths persists the columns' widths (ADR-0151): the widget resolves
	// them through the resolver before the table is emitted and reports the
	// table's settled widths back after, under WidthTag (ScopeKey when
	// empty). The host flushes the resolver once per frame. nil keeps the
	// defaults and persists nothing.
	Widths   *colwidth.Resolver
	WidthTag string
}

// Result is what one Render reports.
type Result struct {
	// Rows is what was shown: the sorted listing in list mode, one entry per
	// outline node in outline mode, and in either mode the search rows — the
	// subtree's matches, sorted — while a filter is set. Indices below refer
	// to it. The slice is the State's scratch and is valid until the next
	// Render; a host that keeps entries copies them.
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

	// filter, compiled: filterSrc is the trimmed text filterRe was built
	// from, filterLiteral whether that text failed to compile and matches
	// as a quoted literal instead (ADR-0164 §SD2's degradation). filterHl
	// is the filter box's highlight-job cache (regexedit); render-thread
	// confined like the text it colours.
	filterSrc     string
	filterRe      *regexp.Regexp
	filterLiteral bool
	filterHl      regexedit.Edit
	found         searchT

	// list-mode scratch and the key capture's id
	keyFrameID      uint64
	lastVisibleRows int
	rows            []Entry

	// outline mode
	tree     tree.State
	nodes    []Entry
	outlineT tree.Tree

	// width persistence (ADR-0151): per view, whether a report was seen and
	// the column signature the resolver last saw
	widthsSeenList    bool
	widthsSeenOutline bool
	widthSigList      string
	widthSigOutline   string
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

// Filter is the quick filter text; SetFilter replaces it. The text is one
// case-insensitive RE2 pattern matched anywhere in the io/fs path of every
// entry under the current directory, at any depth ("src/util/u.go", "/" the
// separator, no leading slash), so `^src/` keeps a subtree and `\.go$` an
// extension wherever it is. A pattern that does not compile matches as a
// literal substring instead (ADR-0164 §SD2), and [State.FilterLiteral]
// reports that it did. Where the pattern runs is [State.search]'s concern.
func (st *State) Filter() string     { return st.filter }
func (st *State) SetFilter(s string) { st.filter = s }

// FilterLiteral reports whether the filter text failed to compile as a
// regex and is matching as a quoted literal.
func (st *State) FilterLiteral() bool {
	st.matcher()
	return st.filterLiteral
}

// matcher returns the compiled filter, nil for an empty one, rebuilding
// only when the text changed since the previous call.
func (st *State) matcher() *regexp.Regexp {
	src := strings.TrimSpace(st.filter)
	if src == "" {
		st.filterSrc, st.filterRe, st.filterLiteral = "", nil, false
		return nil
	}
	if src == st.filterSrc && st.filterRe != nil {
		return st.filterRe
	}
	re, err := regexp.Compile("(?i)" + src)
	st.filterLiteral = err != nil
	if err != nil {
		re = regexp.MustCompile("(?i)" + regexp.QuoteMeta(src))
	}
	st.filterSrc, st.filterRe = src, re
	return re
}

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
	st.found = searchT{}
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
// for, the quick filter as a regex over the entry's path (see
// [State.Filter]), directories first, then the chosen order. A directory
// whose path does not match is out along with its subtree, in both modes.
func (st *State) view(l *listing, showHidden bool, dst []Entry) []Entry {
	dst = dst[:0]
	if l == nil {
		return dst
	}
	re := st.matcher()
	for _, e := range l.entries {
		if !showHidden && strings.HasPrefix(e.Name, ".") {
			continue
		}
		if re != nil && !re.MatchString(e.Path) {
			continue
		}
		dst = append(dst, e)
	}
	sortEntries(dst, st.sortBy, st.sortDesc, false)
	return dst
}

// searchLimit caps what a filter shows, and walkMaxDirs what a walk reads
// for it: a filter is for narrowing, and a pattern that matches thousands
// of paths is one the user refines rather than scrolls. walkReadsPerFrame
// bounds the walk's uncached directory reads per frame, so a plain tree the
// file system cannot search itself is walked across frames rather than
// within one.
const (
	searchLimit       = 2000
	walkMaxDirs       = 4096
	walkReadsPerFrame = 32
)

// searchT is what a non-empty filter shows: the entries under the current
// directory whose path matches, in the order found. The file system
// answers in one call when it can ([fsmatch.FS] — a snapshot store runs the
// pattern in ClickHouse); otherwise the cached listings are walked
// breadth-first, budgeted per frame, and done is false until the walk ends.
// key names what the rows are for; a different key starts over.
type searchT struct {
	key  string
	rows []Entry
	more bool
	done bool
	err  error

	// the walk, when there is one: directories still to read, how many
	// were, and the next row's ordinal.
	walking bool
	pending []string
	dirs    int
	next    int
}

// search advances the filter's search for the current directory and returns
// its rows sorted into dst. The file system's own match is tried first and
// answers in one call; the walk reads at most budget uncached directories
// per call and is resumed by the next one. Only called with a non-empty
// filter.
func (st *State) search(fsys fs.FS, showHidden bool, budget int, dst []Entry) (rows []Entry, s *searchT) {
	st.ensure()
	re := st.matcher()
	s = &st.found
	key := strings.Join([]string{st.cacheKey, st.Dir(), st.filterSrc, strconv.FormatBool(showHidden)}, "\x00")
	if s.key != key {
		*s = searchT{key: key, pending: []string{st.Dir()}}
	}
	if !s.done && !s.walking {
		if m, ok := fsys.(fsmatch.FS); ok {
			matches, more, err := m.MatchPaths(st.Dir(), re.String(), showHidden, searchLimit)
			if err == nil || !errors.Is(err, errors.ErrUnsupported) {
				s.done, s.more, s.err = true, more, err
				s.rows = s.rows[:0]
				for i, m := range matches {
					s.rows = append(s.rows, entryOfMatch(m, i))
				}
			}
		}
		if !s.done {
			s.walking = true
		}
	}
	for s.walking && !s.done && budget > 0 {
		dir := s.pending[0]
		s.pending = s.pending[1:]
		if _, cached := st.cache[dir]; !cached {
			budget--
		}
		l := st.read(fsys, dir)
		s.dirs++
		if l.err != nil {
			if s.dirs == 1 {
				s.err = l.err
			}
		} else {
			for _, e := range l.entries {
				if !showHidden && strings.HasPrefix(e.Name, ".") {
					continue
				}
				if e.IsDir {
					s.pending = append(s.pending, e.Path)
				}
				if re.MatchString(e.Path) {
					e.Ord = s.next
					s.next++
					s.rows = append(s.rows, e)
				}
			}
		}
		if len(s.pending) == 0 {
			s.done = true
		} else if s.dirs >= walkMaxDirs || len(s.rows) >= searchLimit {
			s.done, s.more = true, true
			s.pending = s.pending[:0]
		}
	}
	rows = append(dst[:0], s.rows...)
	if len(rows) > searchLimit {
		rows = rows[:searchLimit]
	}
	sortEntries(rows, st.sortBy, st.sortDesc, true)
	return
}

// entryOfMatch is an [Entry] for a match the file system answered. The
// ordinal is the match's index: unique across the answer, which is what the
// row's widget ids need, and stable while the answer is.
func entryOfMatch(m fsmatch.Match, ord int) Entry {
	e := Entry{Name: path.Base(m.Path), Path: m.Path, Ord: ord}
	if m.Info != nil {
		e.Mode = m.Info.Mode()
		e.IsDir = e.Mode.IsDir()
		e.IsSymlink = e.Mode&fs.ModeSymlink != 0
		e.Size = m.Info.Size()
		e.ModTime = m.Info.ModTime()
	}
	return e
}

// relTo is p as a search row shows it: relative to the directory searched.
func relTo(dir, p string) string {
	if dir == "." || dir == "" {
		return p
	}
	return strings.TrimPrefix(p, dir+"/")
}

// sortEntries orders a view: directories first, then the column, then the
// name — or the whole path, which is what search rows from several
// directories order by.
func sortEntries(es []Entry, by SortByE, desc bool, byPath bool) {
	slices.SortStableFunc(es, func(a, b Entry) int {
		// Directories first, whatever the column and the direction.
		if a.IsDir != b.IsDir {
			if a.IsDir {
				return -1
			}
			return 1
		}
		var c int
		switch by {
		case SortBySize:
			c = cmp.Compare(a.Size, b.Size)
		case SortByModTime:
			c = a.ModTime.Compare(b.ModTime)
		}
		if c == 0 {
			an, bn := a.Name, b.Name
			if byPath {
				an, bn = a.Path, b.Path
			}
			c = cmp.Compare(strings.ToLower(an), strings.ToLower(bn))
			if c == 0 {
				c = cmp.Compare(an, bn)
			}
		}
		if desc {
			c = -c
		}
		return c
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
