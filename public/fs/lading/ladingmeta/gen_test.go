package ladingmeta

//go:generate sh -c "go test -tags=\"$(cat ../../../../tags)\" -run TestGenerateLadingMetaStore ."

import (
	"testing"

	"github.com/stergiotis/boxer/public/fs/lading/ladingschema"
	"github.com/stergiotis/boxer/public/fs/lading/ladingvocab"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema/storegen"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallgen"
	"github.com/stergiotis/boxer/public/storage/recordstore/gen"
	"github.com/stretchr/testify/require"
)

// TestGenerateLadingMetaStore emits the entry store over `boxer.fsmeta`. Run
// it after changing a DTO or the vocabulary:
//
//	go test -tags "$(cat tags)" -run TestGenerateLadingMetaStore ./public/fs/lading/ladingmeta/
//
// Three things here are not defaults and are the point of ADR-0198 §SD2:
//
//   - The table is the `boxer.facts` shape but is NOT `boxer.facts` — the
//     descriptor is renamed by [ladingschema.TableDesc], because the generator
//     refuses one whose own name disagrees with the table.
//   - SharedRA binds `factsschema/ra` rather than re-emitting it. It is the
//     same generator over the same descriptor, so the classes are identical.
//   - ExternallyProvisioned is left false, unlike a facts-bound store: this
//     store owns its table, so it gets EnsureTable and the DDL of
//     [ladingschema.MetaTableOptions] — expiry-day partitioning, the
//     (mount, snapshot, path) key, TTL, and the profile's granularity.
//
// The ids come from the vocabulary rather than from declaration order, which
// is what lets the two kinds co-reside on the shared `symbol` and `u64Array`
// sections: the root row is an entry *and* a snapshot.
//
// The profile is the corpus one. It only reaches `index_granularity` here, and
// a store on the fleet profile regenerates with the other — the schema is one
// either way (§SD10).
func TestGenerateLadingMetaStore(t *testing.T) {
	td, err := ladingschema.TableDesc(ladingschema.TableNameMeta)
	require.NoError(t, err)
	ids, err := storegen.MembershipIds(ladingvocab.NkRegistry)
	require.NoError(t, err)
	require.NoError(t, gen.Input{
		PackageName: "ladingmeta",
		StoreName:   "Meta",
		TableName:   ladingschema.TableNameMeta,
		Database:    ladingschema.DatabaseName,
		Table:       td,
		RowConfig:   ladingschema.TableRowConfig,
		ComponentPaths: []string{
			"./ladingentry_dto.go",
			"./ladingsnapshot_dto.go",
		},
		OutDir:     ".",
		ImportPath: "github.com/stergiotis/boxer/public/fs/lading/ladingmeta",
		Wrapper:    marshallgen.FixedIdsWrapper{Ids: ids},
		SharedRA:   &ladingschema.SharedRA,
		DDL:        ladingschema.MetaTableOptions(ladingschema.ProfileCorpus),
	}.Generate())
}
