// Package colwidth resolves table column widths from user-set overrides,
// per ADR-0151. It owns the four things the ADR's §SD5 names: tier
// resolution, capture detection, the per-table apply epoch, and the
// mapping to and from stored override rows.
//
// The model is that the highest-signal width is the one the user chose,
// so resolution runs override → app-supplied default → crate autofit, and
// a drag the user performs is captured back as an override. Overrides are
// keyed by what a column *is* — its name and render type — rather than
// where it sits, so they survive column reordering, a table moving to a
// different pane, and the same field recurring in a differently-shaped
// query result.
//
// Nothing here talks to egui. The package is pure Go over a store port so
// its state machine can be tested without a render loop; the binding that
// applies widths and reads them back is a separate milestone.
package colwidth

import (
	"sort"
	"strings"

	"lukechampine.com/blake3"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
)

// keyBytes is the length of a column key / shape hash before hex encoding.
// Sixteen bytes is the same width the facts natural keys use: far past any
// collision risk for the number of distinct columns one app renders, and
// short enough that the key stays a readable low-cardinality symbol value
// in the facts table.
const keyBytes = 16

// Column is a column's semantic identity as the call site knows it.
//
// Name is the rendered header text and Type a short discriminator for the
// render type — the leeway canonical type where the data has one, else an
// app-chosen format tag. Type participates in the key deliberately: when a
// column's type changes the old width is no longer meaningful, and keying
// on the pair invalidates it without needing a rule anyone has to
// remember.
type Column struct {
	Name string
	Type string
}

// Key returns the column's stable identity, hex-encoded.
//
// The two fields are length-prefixed rather than delimiter-joined so that
// no pair of (name, type) values can collide by moving the boundary — a
// column named "a" of type "b|c" and one named "a|b" of type "c" are
// different columns and must not share a width.
func (inst Column) Key() (key string) {
	h := blake3.New(keyBytes, nil)
	writeLenPrefixed(h, inst.Name)
	writeLenPrefixed(h, inst.Type)
	key = hexOf(h.Sum(nil))
	return
}

// ShapeHash identifies "the same logical table" — the sorted set of the
// columns' keys. Sorting makes it order-independent, so reordering columns
// does not change the shape; de-duplication makes a repeated column key
// contribute once, so the hash describes a set as the ADR says it does.
func ShapeHash(cols []Column) (hash string) {
	keys := make([]string, 0, len(cols))
	seen := make(map[string]struct{}, len(cols))
	for _, c := range cols {
		k := c.Key()
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := blake3.New(keyBytes, nil)
	for _, k := range keys {
		writeLenPrefixed(h, k)
	}
	hash = hexOf(h.Sum(nil))
	return
}

// StoreI is the resolver's view of durable storage. It is exactly the
// column-width subset of [factsstore.FactsStoreI], so a facts store
// satisfies it structurally and no adapter is needed; a test can supply a
// map instead.
type StoreI interface {
	ListColumnWidths(appId app.AppIdT) (rows []factsstore.ColumnWidthRow, err error)
	WriteColumnWidth(row factsstore.ColumnWidthRow) (id uint64, err error)
	DeleteColumnWidth(appId app.AppIdT, tier string, scope string, columnKey string) (err error)
}

func writeLenPrefixed(h *blake3.Hasher, s string) {
	var n [4]byte
	l := uint32(len(s))
	n[0], n[1], n[2], n[3] = byte(l>>24), byte(l>>16), byte(l>>8), byte(l)
	_, _ = h.Write(n[:])
	_, _ = h.Write([]byte(s))
}

const hexDigits = "0123456789abcdef"

func hexOf(b []byte) (s string) {
	var sb strings.Builder
	sb.Grow(len(b) * 2)
	for _, c := range b {
		sb.WriteByte(hexDigits[c>>4])
		sb.WriteByte(hexDigits[c&0x0f])
	}
	s = sb.String()
	return
}
