package ladingsql

import (
	"fmt"
	"strings"

	"github.com/stergiotis/boxer/public/fs/lading/ladingdata"
	"github.com/stergiotis/boxer/public/fs/lading/ladingmeta"
	"github.com/stergiotis/boxer/public/fs/lading/ladingschema"
	"github.com/stergiotis/boxer/public/identity/identifier"
)

// The store's own coordinates, as the defaults a Config leaves empty take.
var (
	defaultDatabase  = ladingschema.DatabaseName
	defaultMetaTable = ladingschema.TableNameMeta
	defaultDataTable = ladingschema.TableNameData
	defaultSnapTable = ladingschema.TableNameSnap
)

// The kind names the generated stores publish their artefacts under.
const (
	kindEntry    = "LadingEntry"
	kindSnapshot = "LadingSnapshot"
	kindBlock    = "LadingBlock"
)

// Physical backbone columns, resolved once from the descriptor rather than
// written out, so a rename in factsschema moves every expansion with it.
var (
	colID         = mustPlain("id")
	colNaturalKey = mustPlain("naturalKey")
	colTs         = mustPlain("ts")
	colExpiresAt  = mustPlain("expiresAt")
)

func mustPlain(name string) string {
	q, err := ladingschema.PhysicalPlainName(name)
	if err != nil {
		panic(err)
	}
	return q
}

// notExpired is the logical cutoff of §SD4, on the same column the TTL names.
//
// It is not belt-and-braces over the TTL: `TTL` reclaims space only at merge
// time, so a row routinely outlives its expiry on disk, and without this a
// query would return rows whose siblings a merge has already taken. With it,
// results and disk usage can only diverge in disk usage.
var notExpired = colExpiresAt + " > now64(9, 'UTC')"

// entriesSubquery renders `fs(m[, snap])`.
//
// Two levels, and the inner one earns its keep: the generated Projection is a
// single named-tuple expression, so evaluating it once and naming its elements
// above costs one pass over the leeway lanes rather than one per column.
func (inst Config) entriesSubquery(mount identifier.TaggedId, snap snapshotArg) string {
	art := ladingmeta.MetaComponentSQL.Kinds[kindEntry]
	inner := fmt.Sprintf("SELECT %s AS e, name, dir, depth, ext, %s AS expires_at FROM %s WHERE %s",
		art.Projection, colExpiresAt, inst.qualified(inst.MetaTable),
		inst.where(mount, snap, art.Presence))

	cols := []string{
		"e.Id AS mount",
		"e.NaturalKey AS path",
		"e.Ts AS snap",
		"expires_at",
		"e.NodeKind AS node_kind",
		"e.Content AS content",
		"e.Mode AS mode",
		"e.BlockSize AS block_size",
		"e.Blocks AS blocks",
		"e.Size AS size",
		"e.Mtime AS mtime",
		"e.LinkTarget AS link_target",
		"e.Err AS err",
		"e.ContentHash AS content_hash",
		"e.Text AS text",
		"name",
		"dir",
		"depth",
		"ext",
		// From the stored node kind rather than from the mode's type bits:
		// the kind is a LowCardinality symbol a query groups by directly,
		// where the mode is Go's own fs.FileMode encoding and reading it in
		// SQL would put that encoding in every query.
		"e.NodeKind = 'dir' AS is_dir",
		"e.NodeKind = 'symlink' AS is_symlink",
	}
	return "(SELECT " + strings.Join(cols, ", ") + " FROM (" + inner + "))"
}

// blocksSubquery renders `fsdata(m[, snap])`.
//
// `path` and `seq` are decoded from the natural key rather than stored beside
// it: a block's key is `path ‖ 0x00 ‖ be32(seq)` (§SD11), which is what makes a
// file's blocks one contiguous range. The suffix is always five bytes, so the
// split is arithmetic rather than a search — and big-endian, so `reverse`
// before reinterpreting on a little-endian engine.
func (inst Config) blocksSubquery(mount identifier.TaggedId, snap snapshotArg) string {
	art := ladingdata.DataComponentSQL.Kinds[kindBlock]
	inner := fmt.Sprintf("SELECT %s AS b, %s AS nk, %s AS expires_at FROM %s WHERE %s",
		art.Projection, colNaturalKey, colExpiresAt, inst.qualified(inst.DataTable),
		inst.where(mount, snap, art.Presence))

	cols := []string{
		"b.Id AS mount",
		"substring(nk, 1, length(nk) - 5) AS path",
		"reinterpretAsUInt32(reverse(substring(nk, length(nk) - 3, 4))) AS seq",
		"b.Ts AS snap",
		"expires_at",
		"b.Data AS data",
		"b.Hash AS hash",
		"b.Line0 AS line0",
	}
	return "(SELECT " + strings.Join(cols, ", ") + " FROM (" + inner + "))"
}

// snapshotsSubquery renders `fssnap(m[, snap])`: one row per complete
// snapshot of the mount.
//
// It reads the index, so completeness is structural — a row is there only
// because the materialised view copied a root row, and a root row exists only
// because a walk finished. There is no `path` column: the grain is a snapshot,
// not a node.
func (inst Config) snapshotsSubquery(mount identifier.TaggedId, snap snapshotArg) string {
	art := ladingmeta.MetaComponentSQL.Kinds[kindSnapshot]
	// A pinned or '*' call still means "of these snapshots", so the same
	// pinning applies — but the index IS the set of complete snapshots, so a
	// bare fssnap(m) lists them all rather than resolving one.
	if snap.latest {
		snap = snapshotArg{all: true}
	}
	inner := fmt.Sprintf("SELECT %s AS s, %s AS expires_at FROM %s WHERE %s",
		art.Projection, colExpiresAt, inst.qualified(inst.SnapTable),
		inst.where(mount, snap, art.Presence))

	cols := []string{
		"s.Id AS mount",
		"s.Ts AS snap",
		"expires_at",
		"s.Entries AS snap_entries",
		"s.Bytes AS snap_bytes",
		"s.TtlClass AS ttl_class",
		"s.TextRule AS text_rule",
		"s.InlineMax AS inline_max",
	}
	return "(SELECT " + strings.Join(cols, ", ") + " FROM (" + inner + "))"
}

// where is the pinning every expansion carries: the mount, the snapshot, the
// cutoff, and the component's presence.
//
// Presence rather than the generated Filter: Filter adds the per-attribute
// arity validators the Go decode needs, and this projection reads by membership
// id — a row that carried the wrong arity would project a default, not a wrong
// value. Presence is the cheap half and it is the half that matters, because
// `has` over a membership lane prunes granules through a skip index where
// `countEqual` never does.
func (inst Config) where(mount identifier.TaggedId, snap snapshotArg, presence string) string {
	parts := []string{
		fmt.Sprintf("%s = %d", colID, mount.Value()),
		notExpired,
	}
	switch {
	case snap.all:
		parts = append(parts, fmt.Sprintf("%s IN (%s)", colTs, inst.completeSnapshots(mount)))
	case snap.latest:
		// max() over an empty set is the type's default rather than NULL, so a
		// mount with no complete snapshot resolves to the epoch and matches no
		// row — which is the answer, not an error.
		parts = append(parts, fmt.Sprintf("%s = (SELECT max(%s) FROM %s WHERE %s = %d AND %s)",
			colTs, colTs, inst.qualified(inst.SnapTable), colID, mount.Value(), notExpired))
	default:
		parts = append(parts, colTs+" = "+snap.expr)
	}
	parts = append(parts, "("+presence+")")
	return strings.Join(parts, " AND ")
}

// completeSnapshots is the set of a mount's complete snapshots.
//
// It reads the snapshot index, and that is the completeness rule rather than a
// performance choice: a row is in `fssnap` only because the materialised view
// copied a root row there, and a root row exists only because a walk finished
// (§SD6). A walk that died half way cannot be selected here however many rows
// it left behind.
func (inst Config) completeSnapshots(mount identifier.TaggedId) string {
	return fmt.Sprintf("SELECT %s FROM %s WHERE %s = %d AND %s",
		colTs, inst.qualified(inst.SnapTable), colID, mount.Value(), notExpired)
}
