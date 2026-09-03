// Package fsmatch is the seam through which a file system runs a path
// filter itself.
//
// io/fs has no way to ask "every entry under this directory whose path
// matches this pattern" in one call; a caller walks, one ReadDir per
// directory. Over a snapshot store that is one query per directory, when
// the store could answer the whole question with one `match()` over its
// path column. A file system that can do that implements [FS]; a consumer
// that wants it type-asserts, and walks when the assertion fails or the
// call answers [errors.ErrUnsupported].
//
// The pattern is RE2 — what Go's regexp and ClickHouse's match() both
// take — so a pattern means the same thing whichever side runs it. The
// consumer is the authority on what it sends: a case fold is `(?i)` in the
// pattern, a literal is quote-meta'd before it arrives.
package fsmatch

import "io/fs"

// Match is one entry a [FS] answered: its io/fs path from the file
// system's root, and what Lstat would say about it — a symlink is
// reported as recorded, not resolved.
type Match struct {
	Path string
	Info fs.FileInfo
}

// FS is a file system that can run a path pattern itself.
type FS interface {
	// MatchPaths lists the entries under dir — recursively, dir itself
	// excluded — whose path matches the RE2 pattern, in path order. With
	// hidden false, an entry with a dot-segment below dir is left out.
	// limit caps the answer; more reports that the cap cut it. limit 0
	// is no cap.
	MatchPaths(dir, pattern string, hidden bool, limit int) (matches []Match, more bool, err error)
}
