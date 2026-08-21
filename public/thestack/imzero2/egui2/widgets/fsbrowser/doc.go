// Package fsbrowser renders any [io/fs.FS] as a file browser: one directory at
// a time as a sortable list, or the tree below the current directory as an
// outline, with a breadcrumb above, a quick filter, a selection, a keyboard
// cursor, and host-supplied columns beside name, size and modification time
// (ADR-0200 §SD2).
//
// # What the host owns
//
// The host owns the [State] — current directory, filter, sort, selection,
// cursor, the directory cache and the outline's expansion — and the [io/fs.FS]
// the browser reads. The widget reads directories on demand through
// [io/fs.ReadDir] and caches each listing under the host's [Input.CacheKey];
// a host whose tree cannot change under it (a lading snapshot) keeps the key
// fixed and never pays a read twice, a host over a live tree calls
// [State.Invalidate] when it wants a re-read. A change of CacheKey drops the
// cache and the selection and keeps the directory, so two snapshots of one
// mount are browsed at the same path — synchronized browsing is the default
// the location model gives for free.
//
// # What the widget decides, and what it reports
//
// A click selects (ctrl toggles, shift extends from the cursor), a double
// click or Enter *activates*: a directory is entered (list mode) or toggled
// (outline mode), a file is reported in [Result.Activated] for the host to
// preview or open. Backspace goes up a directory; the arrows, Home, End, Page
// Up and Page Down move the cursor and the selection with it. Nothing here
// writes: the browser has no notion of copy, move, rename or delete, which is
// what makes it safe over a read-only store and over a capability grant.
//
// # Modes
//
// [ModeList] shows the current directory's children in an etable — directories
// first, then files — and is the mode a file-transfer client opens in.
// [ModeOutline] shows the tree under the current directory through
// widgets/tree (ADR-0176): children load when a node is opened and never
// before, an unread directory shows a disclosure control because it might
// have children, and an empty one turns into a leaf once read. Both modes
// share the State, the columns and the cursor.
package fsbrowser
