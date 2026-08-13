package cli

import (
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stretchr/testify/require"
)

func TestComposeDdl_FullTable(t *testing.T) {
	sql, err := composeDdl(composeDdlInput{
		Table: "mymart",
		Plain: []string{
			"id u64 item:id",
			"created z64 item:ts enc:delta-encoding",
		},
		Tagged: []string{
			"symbol value s enc:light-general-compression",
		},
		Memberships:    []string{"symbol low-card-ref"},
		Engine:         "MergeTree()",
		OrderBy:        []string{"plain:id"},
		IfNotExists:    true,
		Settings:       []string{"allow_suspicious_low_cardinality_types=1"},
		TableRowConfig: common.TableRowConfigMultiAttributesPerRow,
	})
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(sql, "CREATE TABLE IF NOT EXISTS mymart (\n"), sql)
	// Machine-derived membership machinery is present without being authored.
	require.Contains(t, sql, `"tv:symbol:lr:lr:`)
	require.Contains(t, sql, `"tv:symbol:lrcard:lrcard:`)
	// The delta hint became a real codec through the generator.
	require.Contains(t, sql, "CODEC(")
	require.Contains(t, sql, "Delta")
	require.Contains(t, sql, `ORDER BY ("id:`)
	require.Contains(t, sql, "SETTINGS allow_suspicious_low_cardinality_types=1")
}

func TestComposeDdl_SpecTokensShareTheSeam(t *testing.T) {
	// The same misroutes the constructor family rejects are rejected here,
	// with the same error text (one parser, ADR-0181 §SD6).
	_, err := composeDdl(composeDdlInput{
		Table:  "t",
		Engine: "MergeTree()",
		Plain:  []string{"mycol u64 item:oq use:tlp-amber"},
	})
	require.ErrorContains(t, err, "tagged section")

	_, err = composeDdl(composeDdlInput{
		Table:  "t",
		Engine: "MergeTree()",
		Plain:  []string{"mycol u64"},
	})
	require.ErrorContains(t, err, "item:")

	_, err = composeDdl(composeDdlInput{
		Table:       "t",
		Engine:      "MergeTree()",
		Tagged:      []string{"sec v s"},
		Memberships: []string{"sec nope"},
	})
	require.ErrorContains(t, err, "unknown membership channel")
}

func TestComposeDdl_MixedChannelAllowedForDdl(t *testing.T) {
	// Mixed channels are constructible as *schema* (two lanes, both
	// machine-derived) even though the per-column authoring surface refuses
	// them (ADR-0181 §SD8).
	sql, err := composeDdl(composeDdlInput{
		Table:       "t",
		Engine:      "MergeTree()",
		Tagged:      []string{"sec v s"},
		Memberships: []string{"sec low-card-verbatim-high-card-params"},
	})
	require.NoError(t, err)
	require.Contains(t, sql, `"tv:sec:lmv:lmv:`)
}

func TestComposeDdl_OrderBySelectors(t *testing.T) {
	_, err := parseComposeColumnRef("plain:id")
	require.NoError(t, err)
	ref, err := parseComposeColumnRef("tvrole:symbol:lr")
	require.NoError(t, err)
	require.Equal(t, common.ColumnRoleLowCardRef, ref.Role)
	_, err = parseComposeColumnRef("symbol:value")
	require.ErrorContains(t, err, "unknown column selector")
}

func TestComposeDdl_RequiresTable(t *testing.T) {
	_, err := composeDdl(composeDdlInput{Engine: "MergeTree()"})
	require.ErrorContains(t, err, "--table is required")
}

func TestComposeDdl_SkipIndexes(t *testing.T) {
	sql, err := composeDdl(composeDdlInput{
		Table:       "t",
		Engine:      "MergeTree()",
		Tagged:      []string{"symbol value s"},
		Memberships: []string{"symbol low-card-ref"},
		OrderBy:     []string{"tvrole:symbol:lr"},
		SkipIndexes: true,
	})
	require.NoError(t, err)
	require.Contains(t, sql, "INDEX idx_section_symbol_role_lr")
	require.Contains(t, sql, "TYPE bloom_filter(0.01) GRANULARITY 4")
}

func TestComposeDdl_DuplicateSpecsRejected(t *testing.T) {
	_, err := composeDdl(composeDdlInput{
		Table:       "t",
		Engine:      "MergeTree()",
		Tagged:      []string{"sec reading u64", "sec reading f64"},
		Memberships: []string{"sec low-card-ref"},
	})
	require.ErrorContains(t, err, "duplicate tagged column spec")

	_, err = composeDdl(composeDdlInput{
		Table:  "t",
		Engine: "MergeTree()",
		Plain:  []string{"id u64 item:id", "id u32 item:id"},
	})
	require.ErrorContains(t, err, "duplicate plain column spec")
}

func TestComposeDdl_SectionCoherence(t *testing.T) {
	// Value lanes without a membership channel: the shape LwShapeCheck
	// rejects must not be emitted as durable DDL.
	_, err := composeDdl(composeDdlInput{
		Table:  "t",
		Engine: "MergeTree()",
		Tagged: []string{"symbol value s"},
	})
	require.ErrorContains(t, err, "no membership channel")

	// A typo'd section in a group flag must not mint a phantom section.
	_, err = composeDdl(composeDdlInput{
		Table:       "t",
		Engine:      "MergeTree()",
		Tagged:      []string{"symbol value s"},
		Memberships: []string{"symbol low-card-ref"},
		CoGroups:    []string{"sybmol g"},
	})
	require.ErrorContains(t, err, "unknown section")

	// A values-only co-section leaning on a membership-carrying partner is
	// legal (that sharing is what co-groups exist for).
	sql, err := composeDdl(composeDdlInput{
		Table:       "t",
		Engine:      "MergeTree()",
		Tagged:      []string{"a v s"},
		Memberships: []string{"b low-card-ref"},
		CoGroups:    []string{"a g", "b g"},
	})
	require.NoError(t, err)
	require.Contains(t, sql, `"tv:b:lr:`)
}

func TestComposeDdl_SkipIndexesZeroMatchIsLoud(t *testing.T) {
	_, err := composeDdl(composeDdlInput{
		Table:       "t",
		Engine:      "MergeTree()",
		Plain:       []string{"id u64 item:id"},
		SkipIndexes: true,
	})
	require.ErrorContains(t, err, "matched no lanes")
}
