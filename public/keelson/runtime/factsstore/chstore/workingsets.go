package chstore

import (
	"context"
	"encoding/hex"
	"fmt"
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
	sym.BeginAttribute(row.RunId).AddMembershipMixedLowCardRef(
		vocab.MembRuntimeRun.GetId().Value(), []byte(row.RunId)).EndAttribute()
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
// match and both reads are membership-keyed exactly as LatestState's are —
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

// DeleteWorkingset writes a tombstone row for (appId, name): the identity
// attributes of a workingset row plus a bool-section attribute marked with
// MembPersistTombstone — the same term DeleteState uses, reused rather than
// duplicated because the kind tag already disambiguates the row.
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
// query — composeLatestStateSql's shape with the workingset kind tag, the
// name in place of the persist key, and the config kind as a third
// projected column.
func composeLatestWorkingsetSql(table string, appId app.AppIdT, name string) (sql string) {
	const (
		symLR      = "`tv:symbol:lr:lr:u64:2q:0:0:0::data`"
		symLMR     = "`tv:symbol:lmr:lmr:u64:2q:0:0:0::data`"
		symValue   = "`tv:symbol:value:val:s:m:0:24:0::data`"
		symLRCard  = "`tv:symbol:lrcard:lrcard:u64:4gw:0:0:0::data`"
		blobValue  = "`tv:blobArray:value:val:yh:g:0:0:0::data`"
		blobLR     = "`tv:blobArray:lr:lr:u64:2q:0:0:0::data`"
		blobLRCard = "`tv:blobArray:lrcard:lrcard:u64:4gw:0:0:0::data`"
		boolLR     = "`tv:bool:lr:lr:u64:2q:0:0:0::data`"
		tsCol      = "`ts:ts:z64:2k:0:0:`"
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
