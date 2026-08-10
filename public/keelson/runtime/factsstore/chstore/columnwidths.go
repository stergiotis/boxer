package chstore

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"lukechampine.com/blake3"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema/dml"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/vocab"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// Physical column names for the f64 section. Read off the live table
// rather than extrapolated from the u64 block — the value column's
// encoding segment is `gM`, not the `g` every other array section uses,
// and a hand-derived name would have compiled and then found nothing.
const (
	f64Value  = "`tv:f64Array:value:val:f64h:4A:::0::data`"
	f64LR     = "`tv:f64Array:lr:lr:u64:1247:::0::data`"
	f64LRCard = "`tv:f64Array:lrcard:lrcard:u64:4E:::0::data`"
)

// WriteColumnWidth lands one boxer.facts row tagged KindColumnWidth
// (ADR-0151). The identity attributes ride the symbol section as
// low-cardinality refs — tier has three values ever, and scope and column
// key are short hashes or call-site tags that repeat across every save —
// while the two measurements ride the f64 section as a pair.
//
// Scope is written even when empty (the column tier): an always-present
// attribute keeps the read uniform, and an absent one would read back as
// the empty string anyway, so writing it costs nothing and removes a
// special case.
func (inst *Store) WriteColumnWidth(row factsstore.ColumnWidthRow) (id uint64, err error) {
	id = inst.nextId.Add(1)
	ts := defaultTs(row.Ts)
	nk := naturalKeyForColumnWidth(row.AppId, row.Tier, row.Scope, row.ColumnKey)
	ent := dml.NewInEntityFacts(inst.allocator, 1)
	ent.BeginEntity().SetId(id, nk).SetTimestamp(ts)

	sym := ent.GetSectionSymbol()
	sym.BeginAttribute("columnWidth").AddMembershipLowCardRef(vocab.MembKindColumnWidth.GetId().Value()).EndAttribute()
	sym.BeginAttribute(string(row.AppId)).AddMembershipMixedLowCardRef(
		vocab.MembRuntimeApp.GetId().Value(), []byte(row.AppId)).EndAttribute()
	sym.BeginAttribute(row.Tier).AddMembershipLowCardRef(vocab.MembColWidthTier.GetId().Value()).EndAttribute()
	sym.BeginAttribute(row.Scope).AddMembershipLowCardRef(vocab.MembColWidthScope.GetId().Value()).EndAttribute()
	sym.BeginAttribute(row.ColumnKey).AddMembershipLowCardRef(vocab.MembColWidthColumnKey.GetId().Value()).EndAttribute()
	sym.EndSection()

	f64 := ent.GetSectionF64Array()
	f64.BeginAttributeSingle(row.Points).AddMembershipLowCardRef(vocab.MembColWidthPoints.GetId().Value()).EndAttribute()
	f64.BeginAttributeSingle(row.FontSize).AddMembershipLowCardRef(vocab.MembColWidthFontSize.GetId().Value()).EndAttribute()
	f64.EndSection()

	err = inst.commitAndShip(context.Background(), ent)
	return
}

// DeleteColumnWidth writes a tombstone for one override key: the identity
// attributes plus MembPersistTombstone on the bool section, the term
// DeleteState and DeleteWorkingset already share.
func (inst *Store) DeleteColumnWidth(appId app.AppIdT, tier string, scope string, columnKey string) (err error) {
	id := inst.nextId.Add(1)
	ts := time.Now().UTC()
	nk := naturalKeyForColumnWidth(appId, tier+"-tomb", scope, columnKey)
	ent := dml.NewInEntityFacts(inst.allocator, 1)
	ent.BeginEntity().SetId(id, nk).SetTimestamp(ts)
	sym := ent.GetSectionSymbol()
	sym.BeginAttribute("columnWidth").AddMembershipLowCardRef(vocab.MembKindColumnWidth.GetId().Value()).EndAttribute()
	sym.BeginAttribute(string(appId)).AddMembershipMixedLowCardRef(
		vocab.MembRuntimeApp.GetId().Value(), []byte(appId)).EndAttribute()
	sym.BeginAttribute(tier).AddMembershipLowCardRef(vocab.MembColWidthTier.GetId().Value()).EndAttribute()
	sym.BeginAttribute(scope).AddMembershipLowCardRef(vocab.MembColWidthScope.GetId().Value()).EndAttribute()
	sym.BeginAttribute(columnKey).AddMembershipLowCardRef(vocab.MembColWidthColumnKey.GetId().Value()).EndAttribute()
	sym.EndSection()
	b := ent.GetSectionBool()
	b.BeginAttribute(true).AddMembershipLowCardRef(vocab.MembPersistTombstone.GetId().Value()).EndAttribute()
	b.EndSection()
	err = inst.commitAndShip(context.Background(), ent)
	return
}

// ListColumnWidths returns the latest non-tombstoned override per key for
// appId. One round-trip for the app's whole override set: resolution walks
// three tiers per column on a render path, so a per-key read would be a
// query per column per frame.
//
// No LIMIT. The result is bounded by the app's distinct override keys, and
// a cap would silently drop overrides a user actually set — the same
// reasoning ListWorkingsets records.
func (inst *Store) ListColumnWidths(appId app.AppIdT) (rows []factsstore.ColumnWidthRow, err error) {
	ctx := context.Background()
	sql := composeListColumnWidthsSql(inst.qualifiedTable(), appId)
	body, qerr := inst.cli.Query(ctx, sql)
	if qerr != nil {
		err = eh.Errorf("chstore: list column widths query: %w", qerr)
		return
	}
	defer body.Close()
	raw, rerr := io.ReadAll(body)
	if rerr != nil {
		err = eh.Errorf("chstore: list column widths read: %w", rerr)
		return
	}
	rows, err = parseListColumnWidthsRows(appId, raw)
	if err != nil {
		err = eh.Errorf("chstore: list column widths parse: %w", err)
		return
	}
	factsstore.SortColumnWidths(rows)
	return
}

// composeListColumnWidthsSql builds the latest-per-key read. It nests for
// the same reason ListWorkingsets does: argMax needs the membership picks
// as plain columns, which flat SQL cannot give it.
//
// The tombstone test is a HAVING on the winning row, never a WHERE on the
// candidates. `WHERE NOT is_tomb` combined with argMax returns the newest
// surviving non-tombstone row, which resurrects an override the user
// explicitly cleared — the exact defect the workingsets read had to fix.
func composeListColumnWidthsSql(table string, appId app.AppIdT) (sql string) {
	const (
		symLR     = "`tv:symbol:lr:lr:u64:1247:::0::data`"
		symLMR    = "`tv:symbol:lmr:lmr:u64:1247:::0::data`"
		symValue  = "`tv:symbol:value:val:s:124::I:0::data`"
		symLRCard = "`tv:symbol:lrcard:lrcard:u64:4E:::0::data`"
		boolLR    = "`tv:bool:lr:lr:u64:1247:::0::data`"
		idCol     = "`id:id:u64:47::0:`"
		tsCol     = "`ts:ts:z64:47::0:`"
	)
	tier := pickLcrString(symValue, symLR, symLRCard, vocab.MembColWidthTier.GetId().Value())
	scope := pickLcrString(symValue, symLR, symLRCard, vocab.MembColWidthScope.GetId().Value())
	colKey := pickLcrString(symValue, symLR, symLRCard, vocab.MembColWidthColumnKey.GetId().Value())
	points := pickLcrNumeric(f64Value, f64LR, f64LRCard, vocab.MembColWidthPoints.GetId().Value(), "0")
	fontSize := pickLcrNumeric(f64Value, f64LR, f64LRCard, vocab.MembColWidthFontSize.GetId().Value(), "0")
	isTomb := fmt.Sprintf("has(%s, %d)", boolLR, vocab.MembPersistTombstone.GetId().Value())
	// (ts, id), not ts alone: two captures can share a timestamp, and the
	// monotonic entity id is what makes "later" a total order.
	sortKey := fmt.Sprintf("tuple(%s, %s)", tsCol, idCol)
	whereParts := []string{
		fmt.Sprintf("has(%s, %d)", symLR, vocab.MembKindColumnWidth.GetId().Value()),
		fmt.Sprintf("has(%s, %d)", symLMR, vocab.MembRuntimeApp.GetId().Value()),
		appIdPredicate(appId),
	}
	sql = fmt.Sprintf(`
SELECT
  tier,
  scope,
  col_key,
  argMax(points, sk) AS points,
  argMax(font_size, sk) AS font_size,
  argMax(ts_sec, sk) AS ts_sec
FROM (
  SELECT
    %s AS tier,
    %s AS scope,
    %s AS col_key,
    %s AS points,
    %s AS font_size,
    toUnixTimestamp(%s) AS ts_sec,
    %s AS is_tomb,
    %s AS sk
  FROM %s
  WHERE %s
)
GROUP BY tier, scope, col_key
HAVING argMax(is_tomb, sk) = 0
ORDER BY tier, scope, col_key
FORMAT TabSeparated`,
		tier, scope, colKey, points, fontSize, tsCol, isTomb, sortKey,
		table,
		strings.Join(whereParts, " AND "))
	return
}

// parseListColumnWidthsRows decodes the TabSeparated payload. Column order
// MUST match composeListColumnWidthsSql — the SELECT list and this parser
// are one contract kept in two places.
func parseListColumnWidthsRows(appId app.AppIdT, raw []byte) (rows []factsstore.ColumnWidthRow, err error) {
	rows = []factsstore.ColumnWidthRow{}
	if len(raw) == 0 {
		return
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) != 6 {
			err = eh.Errorf("chstore: list column widths: expected 6 columns, got %d (line=%q)", len(parts), line)
			return
		}
		points, perr := strconv.ParseFloat(parts[3], 64)
		if perr != nil {
			err = eh.Errorf("chstore: list column widths: parse points %q: %w", parts[3], perr)
			return
		}
		fontSize, perr := strconv.ParseFloat(parts[4], 64)
		if perr != nil {
			err = eh.Errorf("chstore: list column widths: parse font_size %q: %w", parts[4], perr)
			return
		}
		tsSec, perr := strconv.ParseInt(parts[5], 10, 64)
		if perr != nil {
			err = eh.Errorf("chstore: list column widths: parse ts %q: %w", parts[5], perr)
			return
		}
		rows = append(rows, factsstore.ColumnWidthRow{
			AppId:     appId,
			Tier:      unescapeTabSeparated(parts[0]),
			Scope:     unescapeTabSeparated(parts[1]),
			ColumnKey: unescapeTabSeparated(parts[2]),
			Points:    points,
			FontSize:  fontSize,
			Ts:        time.Unix(tsSec, 0).UTC(),
		})
	}
	return
}

// naturalKeyForColumnWidth seeds the domain-stable identifier: the entry's
// identity tuple, so repeated captures of one column share a natural key
// and the trail reads as one entity observed over time — the shape
// WriteState already uses for (app, key).
func naturalKeyForColumnWidth(appId app.AppIdT, tier string, scope string, columnKey string) (out []byte) {
	h := blake3.New(16, nil)
	_, _ = h.Write([]byte("columnWidth"))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(appId))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(tier))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(scope))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(columnKey))
	out = h.Sum(nil)
	return
}
