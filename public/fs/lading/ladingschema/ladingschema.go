// Package ladingschema holds what the lading store's three tables are — their
// names, their leeway shape, their engine clauses and the ALTERs and
// materialised view that finish them (ADR-0198 §SD2, §SD4).
//
// The tables are `fsmeta`, `fsdata` and `fssnap` where the packages are
// `lading*`, and the two names answer different questions: `lading` is which
// subsystem owns them, `fs` is what they hold. The SQL surface reads against
// the tables — `fs(mount)` and `fsdata(mount)` — so it is the table names a
// query author sees, and they say filesystem because that is what is in them.
//
// It is a leaf on purpose. The gen-tests that emit the stores need the shape
// before the stores exist, and the provisioning that applies it needs the same
// values afterwards; a package either of them owned would be a cycle.
//
// # The tables carry the boxer.facts shape without being boxer.facts
//
// All three take `factsschema`'s TableDesc — the same 185 physical columns,
// so the same read-access scaffolding, the same bus codecs and the same
// vocabulary reach them — while partitioning, key, TTL, settings and skip
// indexes are the store's own, which is the combination ADR-0184's
// consequences named as missing. [TableDesc] is what makes that legal:
// `recordstore/gen` refuses a TableDesc whose own name disagrees with the
// table it is generated for, so the facts descriptor is copied and renamed
// rather than passed through (measured, ADR-0198 `## Updates` 2026-08-19).
//
// # What each table is
//
//   - `fsmeta` — one row per node per snapshot, keyed (mount, snapshot, path).
//   - `fsdata` — one row per block, the same key with the block ordinal on the
//     path. Its granularity is the profile's, because one block being one mark
//     is what makes a block read cost exactly one block.
//   - `fssnap` — the snapshot index: every root row, copied by a materialised
//     view. Derived, never written to directly.
package ladingschema

import (
	"fmt"
	"strings"
	"sync"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/storage/recordstore/gen"
)

// The ClickHouse coordinates. The database is factsschema's, so the store's
// tables sit beside the facts table whose shape they carry rather than in a
// deployment of their own.
const (
	DatabaseName  = factsschema.DatabaseName
	TableNameMeta = "fsmeta"
	TableNameData = "fsdata"
	TableNameSnap = "fssnap"
)

// TableRowConfig is factsschema's: several attributes per row. It is not a
// choice here — it is half of what "facts-shaped" means, the other half being
// [TableDesc].
const TableRowConfig = factsschema.TableRowConfig

// SharedRA binds the read-access scaffolding `factsschema` already carries
// instead of emitting a copy per store. The RA classes are a function of the
// table alone, and these tables are the facts table's shape, so a copy would
// be pure duplication — the ~206 KB ADR-0184 measured, once per store.
//
// Stylable stays "facts": it is the name the bound package was generated
// under, which is independent of the table a store binds it for.
var SharedRA = gen.Scaffold{
	ImportPath: "github.com/stergiotis/boxer/public/keelson/runtime/factsschema/ra",
	Package:    "ra",
	Stylable:   factsschema.TableName,
}

// Profile fixes the parameters a store chooses between; the schema is one
// (ADR-0198 §SD10). [ProfileCorpus] and [ProfileFleet] are the two the ADR
// names; a store may state its own.
type Profile struct {
	// Name is what a logbook or a policy record calls this profile.
	Name string
	// MetaGranularity and DataGranularity are the tables' index_granularity.
	// One block per mark on `fsdata` is what makes a block read cost exactly
	// one compressed block — verified at 1 MiB and at 256 KiB (M0 check 1).
	MetaGranularity uint32
	DataGranularity uint32
	// BlockSize is how many bytes one block holds before the cut.
	BlockSize uint32
	// PerBlockHash stores each block's own BLAKE3 digest, which is what
	// `BLAKE3(data) != hash` audits. Off where one-block files are the rule
	// and the file hash already covers them.
	PerBlockHash bool
}

// ProfileCorpus is few mounts of large files: fine granularity on the block
// table so one block is one mark, 1 MiB blocks, per-block digests.
var ProfileCorpus = Profile{
	Name:            "corpus",
	MetaGranularity: 1024,
	DataGranularity: 1,
	BlockSize:       1 << 20,
	PerBlockHash:    true,
}

// ProfileFleet is very many small trees: default granularities, no per-block
// digest — a one-block file's content hash is already its block's.
var ProfileFleet = Profile{
	Name:            "fleet",
	MetaGranularity: 8192,
	DataGranularity: 8192,
	BlockSize:       1 << 20,
	PerBlockHash:    false,
}

// TableDesc is the facts TableDesc under one of this store's table names.
//
// The rename is required, not cosmetic: `recordstore/gen` refuses an
// Input.TableName that disagrees with the descriptor's own name, and the
// facts descriptor calls itself "facts". Nothing else moves with it — the
// physical column names carry no table name, so the emitted columns are the
// facts table's, byte for byte.
func TableDesc(tableName string) (td common.TableDesc, err error) {
	manip, err := factsschema.GetSchemaInManipulator()
	if err != nil {
		err = eh.Errorf("facts schema: %w", err)
		return
	}
	td, err = manip.BuildTableDesc()
	if err != nil {
		err = eh.Errorf("build facts table desc: %w", err)
		return
	}
	td.DictionaryEntry.Name = naming.StylableName(tableName)
	return
}

// MetaTableOptions is `fsmeta`'s engine clause.
//
// The key is the whole design: (mount, snapshot, path) makes Stat a point, a
// subtree a startsWith range and a directory a bloom-filtered lookup on the
// materialised `dir`. Partitioning by expiry *day* rather than by mount is
// what keeps the partition count independent of the mount count — a
// many-mount store partitioned by mount has one partition per mount — and
// what lets `ttl_only_drop_parts` drop whole parts, since every row of a
// partition then expires at the same instant.
//
// That last clause is load-bearing and is a constraint on policy, not just on
// DDL: retention classes must be whole days. A partition holding rows that
// expire at different times is only *partly* expired, and a partly expired
// part keeps its expired rows through every background merge under
// `ttl_only_drop_parts = 1` — only an explicit OPTIMIZE FINAL clears them
// (measured, ADR-0198 `## Updates` 2026-08-19).
func MetaTableOptions(p Profile) *clickhouse.TableOptions {
	return factsShapedOptions(p.MetaGranularity)
}

// DataTableOptions is `fsdata`'s. Same key and partitioning; the granularity
// is the profile's, and in the corpus profile it is 1 so that one block is one
// mark and one compressed block.
//
// The block ordinal is a suffix of the natural key, so no fourth key column is
// needed and a file's blocks are one contiguous range.
func DataTableOptions(p Profile) *clickhouse.TableOptions {
	return factsShapedOptions(p.DataGranularity)
}

// SnapTableOptions is `fssnap`'s: the snapshot index, one row per complete
// snapshot, so the path is not in the key.
func SnapTableOptions(p Profile) *clickhouse.TableOptions {
	o := factsShapedOptions(p.MetaGranularity)
	o.OrderBy = []clickhouse.ColumnRef{{Plain: "id"}, {Plain: "ts"}}
	return o
}

// factsShapedOptions is the clause set all three share: MergeTree, expiry-day
// partitioning, the (mount, snapshot, path) key and TTL on the same plain the
// partitioning uses.
//
// PARTITION BY and TTL are raw SQL, so they carry a physical column name; it
// is resolved from the descriptor rather than written out, so a rename in
// factsschema moves it instead of silently producing a clause over a column
// that no longer exists.
func factsShapedOptions(granularity uint32) *clickhouse.TableOptions {
	expiresAt := mustPhysicalPlainName("expiresAt")
	return &clickhouse.TableOptions{
		Mode:   clickhouse.CreateModeIfNotExists,
		Engine: "MergeTree()",
		OrderBy: []clickhouse.ColumnRef{
			{Plain: "id"}, {Plain: "ts"}, {Plain: "naturalKey"},
		},
		PartitionBy: "toYYYYMMDD(" + expiresAt + ")",
		TTL:         expiresAt,
		Settings: []string{
			fmt.Sprintf("index_granularity = %d", granularity),
			// Every row of a partition expires at once, so whole parts drop.
			"ttl_only_drop_parts = 1",
			// The facts shape uses LowCardinality on types ClickHouse warns
			// about; factsschema's own DDL carries the same setting.
			"allow_suspicious_low_cardinality_types = 1",
		},
	}
}

// composeCreateTable renders one CREATE TABLE from a descriptor and a clause
// set, the same way a generated store's embedded DDL is rendered — same IR,
// same naming convention, same composer — so a table provisioned through here
// and one provisioned by EnsureTable cannot disagree about their columns.
func composeCreateTable(qualifiedName string, td common.TableDesc, opts *clickhouse.TableOptions) (sql string, err error) {
	tech := clickhouse.NewTechnologySpecificCodeGenerator()
	ir := common.NewIntermediateTableRepresentation()
	err = ir.LoadFromTable(&td, tech)
	if err != nil {
		err = eh.Errorf("load table ir: %w", err)
		return
	}
	conv, err := ddl.NewHumanReadableNamingConvention(":")
	if err != nil {
		err = eh.Errorf("naming convention: %w", err)
		return
	}
	sql, err = clickhouse.ComposeCreateTable(qualifiedName, ir, TableRowConfig, conv, *opts)
	if err != nil {
		err = eh.Errorf("compose create table: %w", err)
	}
	return
}

// PhysicalPlainName is the quoted physical column name of one backbone plain
// — "id", "naturalKey", "ts" or "expiresAt" — under the facts shape.
//
// Raw SQL that has to name a backbone column (a partitioning expression, a
// TTL, a materialised column's definition, a scan predicate) goes through
// here rather than through a literal, so the physical naming stays one
// derivation from the descriptor. Exactly one column must match; zero or
// several is an error, because a clause over the wrong column would create
// cleanly and be wrong at read time.
func PhysicalPlainName(plain string) (quoted string, err error) {
	if hit, ok := plainNameCache.Load(plain); ok {
		return hit.(string), nil
	}
	quoted, err = resolvePhysicalPlainName(plain)
	if err != nil {
		return
	}
	plainNameCache.Store(plain, quoted)
	return
}

// plainNameCache memoises the derivation, which is not cheap: it rebuilds the
// whole 185-column descriptor and re-runs the naming convention over it, and
// the read paths call it per query. The answer is a pure function of a
// descriptor that cannot change while the process runs.
var plainNameCache sync.Map

func resolvePhysicalPlainName(plain string) (quoted string, err error) {
	td, err := TableDesc(TableNameMeta)
	if err != nil {
		return
	}
	tech := clickhouse.NewTechnologySpecificCodeGenerator()
	ir := common.NewIntermediateTableRepresentation()
	err = ir.LoadFromTable(&td, tech)
	if err != nil {
		err = eh.Errorf("load table ir: %w", err)
		return
	}
	conv, err := ddl.NewHumanReadableNamingConvention(":")
	if err != nil {
		err = eh.Errorf("naming convention: %w", err)
		return
	}
	var matches []string
	for cc, cp := range ir.IterateColumnProps() {
		// Everything that is not a tagged section is a plain: the backbone
		// carries the entity scope, and opaque / transaction plains are the
		// same shape.
		if cc.Scope == common.IntermediateColumnScopeTagged {
			continue
		}
		var phys []common.PhysicalColumnDesc
		phys, err = conv.MapIntermediateToPhysicalColumns(cc, *cp, nil, TableRowConfig)
		if err != nil {
			err = eh.Errorf("render physical columns: %w", err)
			return
		}
		for i, name := range cp.Names {
			if string(name) != plain || i >= len(phys) {
				continue
			}
			matches = append(matches, `"`+phys[i].String()+`"`)
		}
	}
	switch len(matches) {
	case 1:
		quoted = matches[0]
	case 0:
		err = eb.Build().Str("plain", plain).
			Errorf("no plain column named %q in the facts shape", plain)
	default:
		err = eb.Build().Str("plain", plain).Str("matches", strings.Join(matches, ", ")).
			Errorf("plain %q resolves to %d physical columns", plain, len(matches))
	}
	return
}

// mustPhysicalPlainName panics on a failure [PhysicalPlainName] can only
// report for a schema that has changed shape underneath this package — a
// backbone plain removed or duplicated. Every caller is a clause builder with
// no error path, and a store whose partitioning names a column that is not
// there must not start.
func mustPhysicalPlainName(plain string) string {
	quoted, err := PhysicalPlainName(plain)
	if err != nil {
		panic(err)
	}
	return quoted
}

// The backbone columns raw SQL has to name, resolved once rather than per
// call. Every read path in the subsystem spells them from here, so the SQL
// surface and the `io/fs` adapter cannot drift on which physical column a
// predicate is over.
var (
	ColID         = mustPhysicalPlainName("id")
	ColNaturalKey = mustPhysicalPlainName("naturalKey")
	ColTs         = mustPhysicalPlainName("ts")
	ColExpiresAt  = mustPhysicalPlainName("expiresAt")
)

// NotExpired is the logical expiry cutoff of §SD4, on the same column the TTL
// names.
//
// It is not belt-and-braces over the TTL: `TTL` reclaims space only at merge
// time and `ttl_only_drop_parts = 1` leaves a partly expired part alone until
// an explicit OPTIMIZE FINAL, so a row routinely outlives its expiry on disk.
// A read path that omits this returns rows whose siblings a merge has already
// taken — and, worse, keeps serving a snapshot the rest of the store has
// stopped offering. Every read path ANDs it: the macros, the adapter, and
// therefore the SFTP head.
var NotExpired = ColExpiresAt + " > now64(9, 'UTC')"

// QuoteLiteral renders a ClickHouse single-quoted string literal, and
// UnquoteLiteral undoes it.
//
// They live here, in the leaf both the SQL surface and the `io/fs` adapter
// already import, because they are a pair: a private copy in each package let
// the two drift, and an unquote that did not undo everything the quote escapes
// turned a round-trip into a different value. A block's natural key carries a
// literal NUL, so the NUL escape is not decorative.
//
// The doubled-quote form is accepted on the way back because ClickHouse emits
// and accepts it too, and an argument this package did not write may use it.
func QuoteLiteral(s string) string {
	return "'" + literalEscaper.Replace(s) + "'"
}

func UnquoteLiteral(s string) string {
	return literalUnescaper.Replace(s)
}

var literalEscaper = strings.NewReplacer(
	`\`, `\\`,
	`'`, `\'`,
	"\x00", `\0`,
	"\b", `\b`,
	"\f", `\f`,
	"\n", `\n`,
	"\r", `\r`,
	"\t", `\t`,
)

// The inverse, longest-escape-first so `\\'` reads as a backslash then a
// quote rather than as an escaped quote.
var literalUnescaper = strings.NewReplacer(
	`\\`, `\`,
	`\'`, `'`,
	`\0`, "\x00",
	`\b`, "\b",
	`\f`, "\f",
	`\n`, "\n",
	`\r`, "\r",
	`\t`, "\t",
	`''`, `'`,
)
