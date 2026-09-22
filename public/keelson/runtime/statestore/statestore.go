// Package statestore is the typed surface of the two state kinds that are
// not persist values: saved workingsets (ADR-0148) and table column-width
// overrides (ADR-0151). It holds their row types, the interfaces a host
// hands to their consumers, and Memory, the in-process implementation a
// host uses when ClickHouse is down. The durable implementation is
// persist.StoreBackend, over the one state table every kind shares
// (ADR-0105 D3a and its Update of 2026-08-15).
//
// Both kinds lived on `boxer.facts` until that Update moved them: they are
// state — the newest row for a key wins and a delete reads as absent —
// which the append-only facts table could only answer through hand-written
// argMax SQL. The rule the tables now split on is *trail on `boxer.facts`,
// state on `boxer.persiststate`*.
//
// The package is deliberately light — no Arrow, no store — so the UI-layer
// consumers (colwidth, the widgets that resolve widths) can name the row
// types without taking on the storage stack.
package statestore

import (
	"sort"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// WorkingsetRow records one saved app workingset (ADR-0148 §SD6): the
// launch config that would reproduce the closing window's user-authored
// state, written at the closing edge exactly as a launch row records the
// opening edge.
//
// Identity is (AppId, Name): the durable app id plus a caller-chosen name
// (§SD3). v1 wires exactly one name, "default". Kind is the app's
// Manifest.LaunchKind, stored as its own column because the config bytes
// carry no kind marker — readers must not sniff them.
//
// RunId, TileKey and Reason are provenance, not identity: which run and
// which window wrote the record, and why it closed ("user-close" /
// "shutdown" / …). A later write for (AppId, Name) supersedes an earlier
// one without erasing it, and a delete reads back as not-found.
type WorkingsetRow struct {
	RunId   string
	AppId   app.AppIdT
	Name    string
	Kind    string
	Config  []byte
	TileKey uint64
	Reason  string
	Ts      time.Time
}

// SortWorkingsets orders rows by AppId then Name — the ListWorkingsets
// ordering (ADR-0148 §SD7). Every implementation calls it rather than
// trusting its own collation, so a caller comparing an in-memory answer
// with a ClickHouse one sees the same sequence.
func SortWorkingsets(rows []WorkingsetRow) {
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].AppId != rows[j].AppId {
			return rows[i].AppId < rows[j].AppId
		}
		return rows[i].Name < rows[j].Name
	})
}

// Column-width tier names (ADR-0151 §SD1), most specific first. They are
// stored as their own low-cardinality column rather than being inferred
// from whether Scope is empty, so a reader never has to reconstruct the
// tier from the shape of another field.
const (
	// ColWidthTierInstance scopes an override to one table in one app;
	// Scope carries the call site's stable table tag.
	ColWidthTierInstance = "instance"
	// ColWidthTierShape scopes an override to "the same logical table"
	// wherever it appears; Scope carries the shape hash over the sorted
	// column-key set. Read-only in v1 — nothing writes this tier yet
	// (§SD1's deliberate small cut).
	ColWidthTierShape = "shape"
	// ColWidthTierColumn scopes an override to a column anywhere in the
	// app; Scope is empty. This is the tier that lets a recurring column
	// keep its width across differently-shaped ad-hoc query results.
	ColWidthTierColumn = "column"
)

// ColumnWidthRow records one table column-width override (ADR-0151). One
// row per entry rather than one document per app: last-writer-wins lands
// at entry granularity, which removes the cross-window race the ADR's
// original document layout could only narrow.
//
// Identity is (AppId, Tier, Scope, ColumnKey). ColumnKey is the
// blake3short of (column name, type discriminator), so a type change
// invalidates the override by construction rather than by a rule someone
// has to remember. Points is the width, FontSize the font size it was
// captured under, so a resolver can rescale it. InstanceKey is provenance:
// the window that captured it.
type ColumnWidthRow struct {
	AppId       app.AppIdT
	InstanceKey uint64
	Tier        string
	Scope       string
	ColumnKey   string
	Points      float64
	FontSize    float64
	Ts          time.Time
}

// ColumnWidthKey is the identity tuple of an override, extracted so the
// implementations and the resolver agree on what "the same entry" means
// without each re-deriving it.
type ColumnWidthKey struct {
	Tier      string
	Scope     string
	ColumnKey string
}

// Key returns the row's identity within its app.
func (inst ColumnWidthRow) Key() (k ColumnWidthKey) {
	k = ColumnWidthKey{Tier: inst.Tier, Scope: inst.Scope, ColumnKey: inst.ColumnKey}
	return
}

// SortColumnWidths orders rows by (Tier, Scope, ColumnKey). Every
// implementation calls it so they agree on ordering without trusting
// ClickHouse's collation — the same reason SortWorkingsets exists.
func SortColumnWidths(rows []ColumnWidthRow) {
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Tier != rows[j].Tier {
			return rows[i].Tier < rows[j].Tier
		}
		if rows[i].Scope != rows[j].Scope {
			return rows[i].Scope < rows[j].Scope
		}
		return rows[i].ColumnKey < rows[j].ColumnKey
	})
}

// WorkingsetStoreI is the workingset half of the state store: what the
// window host saves and restores through, and what keelson('workingsets')
// lists (ADR-0148 §SD6/§SD7).
type WorkingsetStoreI interface {
	// WriteWorkingset records row as the newest version of (AppId, Name).
	// The earlier version stays in the trail.
	WriteWorkingset(row WorkingsetRow) (err error)
	// LatestWorkingset returns the live record for (appId, name). A key
	// never written and a deleted key both answer found=false with no
	// error. kind is read back as its own column, never from the bytes.
	LatestWorkingset(appId app.AppIdT, name string) (cfg []byte, kind string, found bool, err error)
	// ListWorkingsets returns the live record of every (AppId, Name) —
	// the set a restore would find, not the write trail — ordered by
	// [SortWorkingsets].
	ListWorkingsets() (rows []WorkingsetRow, err error)
	// DeleteWorkingset makes (appId, name) read as absent until the next
	// write.
	DeleteWorkingset(appId app.AppIdT, name string) (err error)
}

// ColumnWidthStoreI is the column-width half of the state store. It is
// exactly the shape colwidth.StoreI declares, so an implementation
// satisfies the resolver structurally.
type ColumnWidthStoreI interface {
	// ListColumnWidths returns the live override of every key belonging
	// to appId — the whole set a resolver loads at once, since resolution
	// walks three tiers per column and a per-key read would be one
	// round-trip per column per frame. Ordered by [SortColumnWidths]. A
	// cleared override is absent, not present-and-zero.
	ListColumnWidths(appId app.AppIdT) (rows []ColumnWidthRow, err error)
	// WriteColumnWidth records row as the newest version of its key.
	WriteColumnWidth(row ColumnWidthRow) (err error)
	// DeleteColumnWidth clears one override. Clearing a key never written
	// is not an error.
	DeleteColumnWidth(appId app.AppIdT, tier string, scope string, columnKey string) (err error)
}

// StoreI is both halves: what a host builds once and hands to the window
// host, the column-width capability and the introspection host.
type StoreI interface {
	WorkingsetStoreI
	ColumnWidthStoreI
}
