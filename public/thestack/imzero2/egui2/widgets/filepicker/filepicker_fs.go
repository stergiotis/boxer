package filepicker

import (
	"io/fs"
	"path"
	"sort"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// readDirSorted returns dir's children from fsys, sorted via
// sortDirEntries (directories first, then case-insensitive name order).
//
// dir is an io/fs path: "." for the FS root, "home/test-user" for nested,
// no leading "/", forward slashes, no "..". The fs.ReadDir helper
// dispatches to fsys's ReadDirFS implementation when present and falls
// back to Open + ReadDir otherwise, so any [fs.FS] works — including
// [os.DirFS], [embed.FS], [testing/fstest.MapFS], or a remote backend.
func readDirSorted(fsys fs.FS, dir string) (entries []fs.DirEntry, err error) {
	entries, err = fs.ReadDir(fsys, dir)
	if err != nil {
		err = eh.Errorf("read dir %q: %w", dir, err)
		return
	}
	sortDirEntries(entries)
	return
}

// sortDirEntries places directories before files; within each group,
// sorts by case-insensitive name with a tiebreak on the original case
// so order is deterministic and stable across frames.
func sortDirEntries(es []fs.DirEntry) {
	sort.Slice(es, func(i, j int) bool {
		if es[i].IsDir() != es[j].IsDir() {
			return es[i].IsDir()
		}
		ai := strings.ToLower(es[i].Name())
		aj := strings.ToLower(es[j].Name())
		if ai != aj {
			return ai < aj
		}
		return es[i].Name() < es[j].Name()
	})
}

// normalizeExtensions lower-cases and strips leading dots from a list
// of user-supplied extensions. Empty entries are dropped. Returns nil
// for effectively-empty input so callers can treat nil as "no filter".
func normalizeExtensions(exts []string) (out []string) {
	if len(exts) == 0 {
		return
	}
	out = make([]string, 0, len(exts))
	for _, e := range exts {
		e = strings.ToLower(strings.TrimSpace(e))
		e = strings.TrimPrefix(e, ".")
		if e == "" {
			continue
		}
		out = append(out, e)
	}
	if len(out) == 0 {
		out = nil
	}
	return
}

// passesExtFilter reports whether de is shown given the active filter.
// Directories always pass. With nil/empty filter every file passes.
func passesExtFilter(de fs.DirEntry, filter []string) (ok bool) {
	if de.IsDir() || len(filter) == 0 {
		ok = true
		return
	}
	ext := strings.ToLower(strings.TrimPrefix(path.Ext(de.Name()), "."))
	for _, allowed := range filter {
		if ext == allowed {
			ok = true
			return
		}
	}
	return
}

// isHiddenName reports whether name follows the POSIX hidden-file
// convention (starts with a dot). ReadDir doesn't surface "." or ".."
// itself, so the simple prefix check is enough — no need to special-case
// the navigation entries.
func isHiddenName(name string) (ok bool) {
	ok = strings.HasPrefix(name, ".")
	return
}

// removeOrdered drops the first occurrence of victim from xs and
// returns the trimmed slice. Used by pickFile to keep the
// click-ordered companion of selectedSet consistent when the user
// toggles a file out. Allocations are cheap: the multi-select set
// is bounded by what fits on screen × the user's patience.
func removeOrdered(xs []string, victim string) (out []string) {
	for i, x := range xs {
		if x != victim {
			continue
		}
		out = append(xs[:i], xs[i+1:]...)
		return
	}
	out = xs
	return
}

// splitBreadcrumbs splits an io/fs path into segment names plus the
// path prefix at each segment, suitable for rendering a clickable
// breadcrumb bar.
//
// Example: "home/test-user" → (["home","test-user"], ["home","home/test-user"]).
// The FS root ("." or "") returns empty slices — the caller is at root.
func splitBreadcrumbs(cwd string) (segs, prefixes []string) {
	clean := path.Clean(cwd)
	if clean == "." || clean == "" {
		return
	}
	parts := strings.Split(clean, "/")
	segs = make([]string, 0, len(parts))
	prefixes = make([]string, 0, len(parts))
	cur := ""
	for _, p := range parts {
		if p == "" {
			continue
		}
		segs = append(segs, p)
		if cur == "" {
			cur = p
		} else {
			cur = cur + "/" + p
		}
		prefixes = append(prefixes, cur)
	}
	return
}
