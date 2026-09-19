package fsbrowser

import (
	"context"
	"errors"
	"io/fs"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/stergiotis/boxer/public/fs/fsmatch"
	"github.com/stergiotis/boxer/public/keelson/runtime/bgjob"
	"github.com/stergiotis/boxer/public/keelson/runtime/task"
)

// The filter's search: the entries under the current directory whose path
// matches, at any depth. It runs as a background job (bgjob, and through it a
// keelson task when the host supplies [Input.Tasks]) because neither of its
// two forms belongs on the render thread — one query against a store, or a
// walk that stats every entry of every directory it reads. The render thread
// starts it, shows its progress and its Cancel in the filter row, copies the
// matches it has found so far each frame, and takes the result when it ends.
//
// The goroutine touches no State: it reads the file system itself rather than
// through the State's listing cache, and hands matches over through a feed
// under a lock. The file system is therefore read from two goroutines, which
// os.DirFS, a MapFS and a ladingview.Locked view all allow.

// searchLimit caps what a filter shows, and walkMaxDirs what a walk reads
// for it: a filter is for narrowing, and a pattern that matches thousands
// of paths is one the user refines rather than scrolls.
const (
	searchLimit = 2000
	walkMaxDirs = 4096
)

// searchDebounce is how long the filter text stands still before a search
// starts. Each search is a task on the bus when the host gives one, and a
// reader typing a pattern would otherwise start and cancel one per keystroke.
// A variable so a test does not wait for it.
var searchDebounce = 250 * time.Millisecond

// progressScale is the total a walk reports against. A walk does not know how
// many directories it will read, only how many it has read and how many it
// has found and not read yet, so its progress is a share of what is known —
// reported in thousandths of a constant total, because the job's estimator
// starts over whenever the total it is given changes.
const progressScale = 1000

// searchResult is what a finished search hands back.
type searchResult struct {
	rows []Entry
	// more: there were more matches than the limit shows. unread: the walk
	// stopped at its directory cap with directories still unread. They are
	// told apart because the reader's remedy differs — a narrower pattern
	// for the one, a deeper starting directory for the other.
	more   bool
	unread bool
}

// searchFeed carries matches from the search's goroutine to the render thread
// while the search runs. version moves with every append, so a frame that
// finds it unmoved copies nothing.
type searchFeed struct {
	mu      sync.Mutex
	rows    []Entry
	version uint64
}

func (inst *searchFeed) add(es ...Entry) {
	inst.mu.Lock()
	inst.rows = append(inst.rows, es...)
	inst.version++
	inst.mu.Unlock()
}

// take copies the rows into dst when they moved since seen.
func (inst *searchFeed) take(seen uint64, dst []Entry) (rows []Entry, version uint64, moved bool) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.version == seen {
		return dst, seen, false
	}
	return append(dst[:0], inst.rows...), inst.version, true
}

// searchT is the render thread's account of one search. key names what the
// rows are for; a different key is a different search.
type searchT struct {
	key    string
	rows   []Entry
	more   bool
	unread bool
	err    error
	// done: the rows are final — the job ended, failed, or was cancelled.
	done      bool
	cancelled bool
	// since is when the key was first seen (the debounce runs from it);
	// started whether the job was launched for it.
	since   time.Time
	started bool
	feed    *searchFeed
	seen    uint64
}

// search advances the filter's search for the current directory and returns
// its rows sorted into dst: it launches the job once the filter has stood
// still, copies the matches found so far while the job runs, and takes the
// result when it ends. Only called with a non-empty filter, once per frame.
func (st *State) search(fsys fs.FS, showHidden bool, keep func(Entry) bool, tasks task.TaskApiI, dst []Entry) (rows []Entry, s *searchT) {
	st.ensure()
	re := st.matcher()
	s = &st.found
	dir := st.Dir()
	key := strings.Join([]string{st.cacheKey, dir, st.filterSrc, strconv.FormatBool(showHidden)}, "\x00")
	if s.key != key {
		st.job.Invalidate()
		*s = searchT{key: key, since: time.Now()}
	}
	if !s.done && !s.started && time.Since(s.since) >= searchDebounce {
		feed := &searchFeed{}
		s.feed = feed
		s.started = st.job.StartReporting(tasks,
			bgjob.Spec{Kind: "fsbrowser-search", Title: "find " + st.filterSrc + " under " + dir, Tag: key},
			func(ctx context.Context, report bgjob.Reporter) (*searchResult, error) {
				return runSearch(ctx, fsys, dir, re, showHidden, keep, feed, report)
			})
	}
	if s.started && !s.done {
		if res, tag, ok := st.job.TakeResult(); ok && tag == key {
			s.rows, s.more, s.unread, s.done = res.rows, res.more, res.unread, true
		} else if snap := st.job.Snapshot(); snap.State == bgjob.StateFailed {
			s.err, s.done = snap.Err, true
		} else if snap.State == bgjob.StateIdle {
			// Ended without a result: cancelled, here or from the host's
			// task monitor. What was found stands, and says it is partial.
			s.rows, _, _ = s.feed.take(^uint64(0), s.rows)
			s.done, s.cancelled = true, true
		} else if got, version, moved := s.feed.take(s.seen, s.rows); moved {
			s.rows, s.seen = got, version
		}
	}
	rows = append(dst[:0], s.rows...)
	if len(rows) > searchLimit {
		rows = rows[:searchLimit]
	}
	sortEntries(rows, st.sortBy, st.sortDesc, true)
	return
}

// StopSearch ends a search in flight and forgets its rows. The widget calls it
// when the filter is cleared; a host calls it when it stops rendering the
// browser — a dialog that closes — since a search nobody renders is otherwise
// left to run to its cap.
func (st *State) StopSearch() {
	if st.job != nil {
		st.job.Invalidate()
	}
	st.found = searchT{}
}

// cancelSearch is the reader's Cancel: the job ends, and the rows found so far
// stay as the answer to this filter until it changes.
func (st *State) cancelSearch() {
	if st.job != nil {
		st.job.Cancel()
	}
}

// searching reports whether the search's job is running, for the chrome.
func (st *State) searching() bool {
	return st.found.started && !st.found.done
}

// runSearch is the job. The file system answers in one call when it can
// ([fsmatch.FS] — a snapshot store runs the pattern in ClickHouse);
// [errors.ErrUnsupported] from it, or a file system without the seam, is
// walked. It runs on the job's goroutine and touches nothing of the State.
func runSearch(ctx context.Context, fsys fs.FS, dir string, re *regexp.Regexp, showHidden bool, keep func(Entry) bool, feed *searchFeed, report bgjob.Reporter) (res *searchResult, err error) {
	if fsys == nil {
		return nil, errors.New("fsbrowser: no file system")
	}
	if m, ok := fsys.(fsmatch.FS); ok {
		// One call, of a length the widget cannot see into.
		report(0, 0, "asking the store")
		matches, more, merr := m.MatchPaths(dir, re.String(), showHidden, searchLimit)
		if merr == nil {
			res = &searchResult{more: more}
			for i, m := range matches {
				if e := entryOfMatch(m, i); keep == nil || keep(e) {
					res.rows = append(res.rows, e)
				}
			}
			feed.add(res.rows...)
			return
		}
		if !errors.Is(merr, errors.ErrUnsupported) {
			return nil, merr
		}
	}
	return walkSearch(ctx, fsys, dir, re, showHidden, keep, feed, report)
}

// walkSearch reads the tree under dir breadth-first, to the caps, feeding
// matches as it finds them. An unreadable directory below the first is passed
// over, as it is when browsing; an unreadable first one is the search's error.
//
// Progress is the directories read over the directories known — read and
// found-but-unread. That share can fall while the walk is still discovering
// more than it reads, so what is reported is the most it has been: a bar that
// may wait, and does not go back.
func walkSearch(ctx context.Context, fsys fs.FS, dir string, re *regexp.Regexp, showHidden bool, keep func(Entry) bool, feed *searchFeed, report bgjob.Reporter) (res *searchResult, err error) {
	res = &searchResult{}
	pending := []string{dir}
	read, best := 0, uint64(0)
	var found []Entry
	for len(pending) > 0 {
		if err = ctx.Err(); err != nil {
			return
		}
		d := pending[0]
		pending = pending[1:]
		l := readListing(fsys, d)
		read++
		if l.err != nil {
			if read == 1 {
				return nil, l.err
			}
			continue
		}
		found = found[:0]
		for _, e := range l.entries {
			if !shown(e, showHidden, keep) {
				continue
			}
			if e.IsDir {
				pending = append(pending, e.Path)
			}
			if re.MatchString(e.Path) {
				e.Ord = len(res.rows) + len(found)
				found = append(found, e)
			}
		}
		if len(found) > 0 {
			res.rows = append(res.rows, found...)
			feed.add(found...)
		}
		if share := uint64(read) * progressScale / uint64(read+len(pending)); share > best {
			best = share
		}
		report(best, progressScale, walkNote(read, len(res.rows)))
		if len(res.rows) >= searchLimit {
			res.more = len(pending) > 0 || len(res.rows) > searchLimit
			return
		}
		if read >= walkMaxDirs && len(pending) > 0 {
			res.unread = true
			return
		}
	}
	return
}

func walkNote(dirs, matches int) string {
	return itoa(dirs) + " directories · " + itoa(matches) + " matches"
}
