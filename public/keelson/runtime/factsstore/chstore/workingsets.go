package chstore

import (
	"context"
	"encoding/hex"
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

// WriteWorkingset lands one boxer.facts row tagged KindWorkingset
// (ADR-0148 §SD6): the launch config that would reproduce the closing
// window, written at the closing edge as WriteLaunch records the opening
// edge. The row deliberately reuses the launch cohort's vocabulary — the
// record IS the app's LaunchKind DTO (§SD2) — so app / run identity ride
// MembRuntimeApp / MembRuntimeRun, the closing window's key reuses
// MembLifecycleTileKey, the kind rides MembLaunchConfigKind, the bytes
// MembLaunchConfig on the blob section, and the save provenance
// MembLifecycleStopReason. Only the kind tag and the set name are new.
func (inst *Store) WriteWorkingset(row factsstore.WorkingsetRow) (id uint64, err error) {
	id = inst.nextId.Add(1)
	ts := defaultTs(row.Ts)
	nk := naturalKeyForWorkingset(row.RunId, row.AppId, row.Name, row.TileKey)
	ent := dml.NewInEntityFacts(inst.allocator, 1)
	ent.BeginEntity().SetId(id, nk).SetTimestamp(ts)

	sym := ent.GetSectionSymbol()
	sym.BeginAttribute("workingset").AddMembershipLowCardRef(vocab.MembKindWorkingset.GetId().Value()).EndAttribute()
	sym.BeginAttribute(string(row.AppId)).AddMembershipMixedLowCardRef(
		vocab.MembRuntimeApp.GetId().Value(), []byte(row.AppId)).EndAttribute()
	inst.stampRun(sym, row.RunId)
	sym.BeginAttribute(row.Name).AddMembershipLowCardRef(vocab.MembWorkingsetName.GetId().Value()).EndAttribute()
	if row.Kind != "" {
		sym.BeginAttribute(row.Kind).AddMembershipLowCardRef(vocab.MembLaunchConfigKind.GetId().Value()).EndAttribute()
	}
	sym.EndSection()

	if row.Reason != "" {
		str := ent.GetSectionStringArray()
		str.BeginAttributeSingle(row.Reason).AddMembershipLowCardRef(vocab.MembLifecycleStopReason.GetId().Value()).EndAttribute()
		str.EndSection()
	}

	u64 := ent.GetSectionU64Array()
	u64.BeginAttributeSingle(row.TileKey).AddMembershipLowCardRef(vocab.MembLifecycleTileKey.GetId().Value()).EndAttribute()
	u64.EndSection()

	if len(row.Config) > 0 {
		blob := ent.GetSectionBlobArray()
		blob.BeginAttributeSingle(row.Config).AddMembershipLowCardRef(vocab.MembLaunchConfig.GetId().Value()).EndAttribute()
		blob.EndSection()
	}

	err = inst.commitAndShip(context.Background(), ent)
	return
}

// LatestWorkingset returns the most recent record for (appId, name). The
// match and both reads are membership-keyed exactly as RecentLogs' are —
// appId via the MembRuntimeApp mixed-membership, the name via the
// MembWorkingsetName low-card-ref, the kind via MembLaunchConfigKind, and
// the config via the MembLaunchConfig-tagged blob attribute — so nothing
// depends on the positional order of symbol attributes. The blob is
// hex-encoded over the wire for binary safety; the most recent row being a
// DeleteWorkingset tombstone reads back found=false.
//
// The kind comes off its own column: the facts wire carries no kind marker
// (ADR-0135 Update), so a reader that sniffed the bytes would be guessing.
func (inst *Store) LatestWorkingset(appId app.AppIdT, name string) (cfg []byte, kind string, found bool, err error) {
	ctx := context.Background()
	sql := composeLatestWorkingsetSql(inst.qualifiedTable(), appId, name)
	body, err := inst.cli.Query(ctx, sql)
	if err != nil {
		err = eh.Errorf("chstore: latest workingset query: %w", err)
		return
	}
	defer body.Close()
	buf := make([]byte, 131072)
	n, _ := body.Read(buf)
	raw := strings.TrimRight(string(buf[:n]), "\n")
	if raw == "" {
		return
	}
	parts := strings.Split(raw, "\t")
	if len(parts) != 3 {
		err = eh.Errorf("chstore: latest workingset: unexpected row shape: %q", raw)
		return
	}
	if parts[2] == "1" {
		return
	}
	cfg, err = hex.DecodeString(parts[0])
	if err != nil {
		err = eh.Errorf("chstore: latest workingset: hex decode: %w", err)
		return
	}
	kind = parts[1]
	found = true
	return
}

// ListWorkingsets returns the latest non-tombstoned record per (appId, name)
// — the set a restore would find (ADR-0148 §SD7). One round-trip: the trail
// is collapsed server-side, and the caller (the keelson('workingsets')
// provider) runs on an HTTP handler goroutine where that is affordable,
// unlike LatestWorkingset which sits on a window opener's thread.
//
// The result is bounded by (participating apps × names) and so carries no
// LIMIT: a cap here would silently truncate an answer whose whole point is
// completeness.
func (inst *Store) ListWorkingsets() (rows []factsstore.WorkingsetRow, err error) {
	ctx := context.Background()
	sql := composeListWorkingsetsSql(inst.qualifiedTable())
	body, qerr := inst.cli.Query(ctx, sql)
	if qerr != nil {
		err = eh.Errorf("chstore: list workingsets query: %w", qerr)
		return
	}
	defer body.Close()
	// io.ReadAll, not one body.Read into a fixed buffer: this read is
	// multi-row, and a single Read would truncate silently at whatever the
	// transport handed over first.
	raw, rerr := io.ReadAll(body)
	if rerr != nil {
		err = eh.Errorf("chstore: list workingsets read: %w", rerr)
		return
	}
	rows, err = parseListWorkingsetsRows(raw)
	if err != nil {
		err = eh.Errorf("chstore: list workingsets parse: %w", err)
		return
	}
	factsstore.SortWorkingsets(rows)
	return
}

// DeleteWorkingset writes a tombstone row for (appId, name): the identity
// attributes of a workingset row plus a bool-section attribute marked with
// MembPersistTombstone — the term the facts-bound persist tombstones used,
// reused rather than duplicated because the kind tag already disambiguates
// the row.
// LatestWorkingset treats the most-recent tombstone as found=false.
func (inst *Store) DeleteWorkingset(appId app.AppIdT, name string) (err error) {
	id := inst.nextId.Add(1)
	ts := time.Now().UTC()
	nk := naturalKeyFor("workingset-tomb", appId, []byte(name), nil)
	ent := dml.NewInEntityFacts(inst.allocator, 1)
	ent.BeginEntity().SetId(id, nk).SetTimestamp(ts)
	sym := ent.GetSectionSymbol()
	sym.BeginAttribute("workingset").AddMembershipLowCardRef(vocab.MembKindWorkingset.GetId().Value()).EndAttribute()
	sym.BeginAttribute(string(appId)).AddMembershipMixedLowCardRef(
		vocab.MembRuntimeApp.GetId().Value(), []byte(appId)).EndAttribute()
	sym.BeginAttribute(name).AddMembershipLowCardRef(vocab.MembWorkingsetName.GetId().Value()).EndAttribute()
	sym.EndSection()
	b := ent.GetSectionBool()
	b.BeginAttribute(true).AddMembershipLowCardRef(vocab.MembPersistTombstone.GetId().Value()).EndAttribute()
	b.EndSection()
	err = inst.commitAndShip(context.Background(), ent)
	return
}

// composeLatestWorkingsetSql builds the membership-keyed LatestWorkingset
// query — the shape the facts-bound persist read had, with the workingset
// kind tag, the name in place of the persist key, and the config kind as
// a third projected column.
func composeLatestWorkingsetSql(table string, appId app.AppIdT, name string) (sql string) {
	const (
		symLR      = "`tv:symbol:lr:lr:u64:1247:::0::data`"
		symLMR     = "`tv:symbol:lmr:lmr:u64:1247:::0::data`"
		symValue   = "`tv:symbol:value:val:s:124::I:0::data`"
		symLRCard  = "`tv:symbol:lrcard:lrcard:u64:4E:::0::data`"
		blobValue  = "`tv:blobArray:value:val:yh:4:::0::data`"
		blobLR     = "`tv:blobArray:lr:lr:u64:1247:::0::data`"
		blobLRCard = "`tv:blobArray:lrcard:lrcard:u64:4E:::0::data`"
		boolLR     = "`tv:bool:lr:lr:u64:1247:::0::data`"
		tsCol      = "`ts:ts:z64:47::0:`"
	)
	blobIdxInLr := fmt.Sprintf("indexOf(%s, %d)", blobLR, vocab.MembLaunchConfig.GetId().Value())
	configPick := fmt.Sprintf("hex(if(%s > 0, arrayElement(%s, indexOf(arrayCumSum(%s), %s)), ''))",
		blobIdxInLr, blobValue, blobLRCard, blobIdxInLr)
	kindPick := pickLcrString(symValue, symLR, symLRCard, vocab.MembLaunchConfigKind.GetId().Value())
	namePick := pickLcrString(symValue, symLR, symLRCard, vocab.MembWorkingsetName.GetId().Value())
	whereParts := []string{
		fmt.Sprintf("has(%s, %d)", symLR, vocab.MembKindWorkingset.GetId().Value()),
		fmt.Sprintf("has(%s, %d)", symLMR, vocab.MembRuntimeApp.GetId().Value()),
		appIdPredicate(appId),
		fmt.Sprintf("(%s) = %s", namePick, quoteSqlString(name)),
	}
	sql = fmt.Sprintf(`
SELECT
  %s AS cfg_hex,
  %s AS cfg_kind,
  has(%s, %d) AS is_tombstone
FROM %s
WHERE %s
ORDER BY %s DESC
LIMIT 1
FORMAT TabSeparated`,
		configPick,
		kindPick,
		boolLR, vocab.MembPersistTombstone.GetId().Value(),
		table,
		strings.Join(whereParts, " AND "),
		tsCol)
	return
}

// columnExprsWorkingset gathers the per-row projections the ListWorkingsets
// subquery emits. Order MUST match parseListWorkingsetsRows — the SELECT
// list below and the parser are one contract in two places.
type columnExprsWorkingset struct {
	appId   string
	name    string
	kind    string
	cfgHex  string
	tileKey string
	reason  string
	runId   string
	tsSec   string
	isTomb  string
	sortKey string
}

func buildWorkingsetColumnExprs() (e columnExprsWorkingset) {
	const (
		symLR      = "`tv:symbol:lr:lr:u64:1247:::0::data`"
		symLMR     = "`tv:symbol:lmr:lmr:u64:1247:::0::data`"
		symMRHP    = "`tv:symbol:mrhp:mrhp:y:4:::0::data`"
		symValue   = "`tv:symbol:value:val:s:124::I:0::data`"
		symLRCard  = "`tv:symbol:lrcard:lrcard:u64:4E:::0::data`"
		strLR      = "`tv:stringArray:lr:lr:u64:1247:::0::data`"
		strValue   = "`tv:stringArray:value:val:sh:4::8:0::data`"
		strLRCard  = "`tv:stringArray:lrcard:lrcard:u64:4E:::0::data`"
		u64LR      = "`tv:u64Array:lr:lr:u64:1247:::0::data`"
		u64Value   = "`tv:u64Array:value:val:u64h:4:::0::data`"
		u64LRCard  = "`tv:u64Array:lrcard:lrcard:u64:4E:::0::data`"
		blobLR     = "`tv:blobArray:lr:lr:u64:1247:::0::data`"
		blobValue  = "`tv:blobArray:value:val:yh:4:::0::data`"
		blobLRCard = "`tv:blobArray:lrcard:lrcard:u64:4E:::0::data`"
		boolLR     = "`tv:bool:lr:lr:u64:1247:::0::data`"
		idCol      = "`id:id:u64:47::0:`"
		tsCol      = "`ts:ts:z64:47::0:`"
	)
	e.appId = fmt.Sprintf("arrayFirst((p, m) -> m = %d, %s, %s)",
		vocab.MembRuntimeApp.GetId().Value(), symMRHP, symLMR)
	e.runId = fmt.Sprintf("arrayFirst((p, m) -> m = %d, %s, %s)",
		vocab.MembRuntimeRun.GetId().Value(), symMRHP, symLMR)
	e.name = pickLcrString(symValue, symLR, symLRCard, vocab.MembWorkingsetName.GetId().Value())
	e.kind = pickLcrString(symValue, symLR, symLRCard, vocab.MembLaunchConfigKind.GetId().Value())
	e.reason = pickLcrString(strValue, strLR, strLRCard, vocab.MembLifecycleStopReason.GetId().Value())
	e.tileKey = pickLcrNumeric(u64Value, u64LR, u64LRCard, vocab.MembLifecycleTileKey.GetId().Value(), "0")
	// The blob rides hex, as LatestWorkingset's does: TabSeparated is a text
	// transport and the config bytes are arbitrary.
	blobIdxInLr := fmt.Sprintf("indexOf(%s, %d)", blobLR, vocab.MembLaunchConfig.GetId().Value())
	e.cfgHex = fmt.Sprintf("hex(if(%s > 0, arrayElement(%s, indexOf(arrayCumSum(%s), %s)), ''))",
		blobIdxInLr, blobValue, blobLRCard, blobIdxInLr)
	e.tsSec = fmt.Sprintf("toUnixTimestamp(%s)", tsCol)
	e.isTomb = fmt.Sprintf("has(%s, %d)", boolLR, vocab.MembPersistTombstone.GetId().Value())
	// (ts, id) rather than ts alone: two saves can share a timestamp — the
	// ADR's last-writer-wins policy on one name only says the later write
	// wins, and the monotonic entity id is what makes "later" total.
	e.sortKey = fmt.Sprintf("tuple(%s, %s)", tsCol, idCol)
	return
}

// composeListWorkingsetsSql builds the latest-per-(app, name) read. Unlike
// this package's other composers it nests: the inner SELECT lifts each row's
// values out of the membership arrays, the outer one collapses the trail per
// key with argMax over the (ts, id) sort key. Flat SQL cannot express that —
// argMax needs the picks as plain columns.
//
// The tombstone test is a HAVING on the WINNING row, not a WHERE on the
// candidates: `WHERE NOT is_tomb` plus argMax would return the last
// surviving non-tombstone row and so resurrect a deleted record.
func composeListWorkingsetsSql(table string) (sql string) {
	e := buildWorkingsetColumnExprs()
	const (
		symLR  = "`tv:symbol:lr:lr:u64:1247:::0::data`"
		symLMR = "`tv:symbol:lmr:lmr:u64:1247:::0::data`"
	)
	whereParts := []string{
		fmt.Sprintf("has(%s, %d)", symLR, vocab.MembKindWorkingset.GetId().Value()),
		fmt.Sprintf("has(%s, %d)", symLMR, vocab.MembRuntimeApp.GetId().Value()),
	}
	// ORDER BY keeps the wire deterministic for anyone running this query by
	// hand; the Go caller re-sorts so the two backends agree regardless of
	// server collation.
	sql = fmt.Sprintf(`
SELECT
  app_id,
  ws_name,
  argMax(cfg_kind, sk) AS cfg_kind,
  argMax(cfg_hex, sk) AS cfg_hex,
  argMax(tile_key, sk) AS tile_key,
  argMax(reason, sk) AS reason,
  argMax(run_id, sk) AS run_id,
  argMax(ts_sec, sk) AS ts_sec
FROM (
  SELECT
    %s AS app_id,
    %s AS ws_name,
    %s AS cfg_kind,
    %s AS cfg_hex,
    %s AS tile_key,
    %s AS reason,
    %s AS run_id,
    %s AS ts_sec,
    %s AS is_tomb,
    %s AS sk
  FROM %s
  WHERE %s
)
GROUP BY app_id, ws_name
HAVING argMax(is_tomb, sk) = 0
ORDER BY app_id, ws_name
FORMAT TabSeparated`,
		e.appId, e.name, e.kind, e.cfgHex, e.tileKey, e.reason, e.runId,
		e.tsSec, e.isTomb, e.sortKey,
		table,
		strings.Join(whereParts, " AND "))
	return
}

// parseListWorkingsetsRows decodes the TabSeparated payload. The column
// order MUST match composeListWorkingsetsSql. String fields go through
// unescapeTabSeparated — a `reason` or a name carrying a tab or newline is
// not expected today, but the reader costs nothing and the alternative is a
// silently split row.
func parseListWorkingsetsRows(raw []byte) (rows []factsstore.WorkingsetRow, err error) {
	rows = []factsstore.WorkingsetRow{}
	if len(raw) == 0 {
		return
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) != 8 {
			err = eh.Errorf("chstore: list workingsets: expected 8 columns, got %d (line=%q)", len(parts), line)
			return
		}
		// cfg_hex needs no unescaping: hex digits carry no backslash.
		cfg, derr := hex.DecodeString(parts[3])
		if derr != nil {
			err = eh.Errorf("chstore: list workingsets: hex decode: %w", derr)
			return
		}
		tileKey, perr := strconv.ParseUint(parts[4], 10, 64)
		if perr != nil {
			err = eh.Errorf("chstore: list workingsets: parse tile_key %q: %w", parts[4], perr)
			return
		}
		tsSec, perr := strconv.ParseInt(parts[7], 10, 64)
		if perr != nil {
			err = eh.Errorf("chstore: list workingsets: parse ts %q: %w", parts[7], perr)
			return
		}
		rows = append(rows, factsstore.WorkingsetRow{
			AppId:   app.AppIdT(unescapeTabSeparated(parts[0])),
			Name:    unescapeTabSeparated(parts[1]),
			Kind:    unescapeTabSeparated(parts[2]),
			Config:  cfg,
			TileKey: tileKey,
			Reason:  unescapeTabSeparated(parts[5]),
			RunId:   unescapeTabSeparated(parts[6]),
			Ts:      time.Unix(tsSec, 0).UTC(),
		})
	}
	return
}

// naturalKeyForWorkingset seeds a stable per-save identifier. Distinct on
// (run_id, app_id, name, tile_key): a window closes once, so each save is
// its own row and the trail keeps the history the ADR calls for.
func naturalKeyForWorkingset(runId string, appId app.AppIdT, name string, tileKey uint64) (out []byte) {
	h := blake3.New(16, nil)
	_, _ = h.Write([]byte("workingset"))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(runId))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(appId))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(name))
	_, _ = h.Write([]byte{0})
	var tk [8]byte
	for i := 0; i < 8; i++ {
		tk[i] = byte(tileKey >> (8 * (7 - i)))
	}
	_, _ = h.Write(tk[:])
	out = h.Sum(nil)
	return
}
