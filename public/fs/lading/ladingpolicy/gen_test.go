package ladingpolicy

//go:generate sh -c "go test -tags=\"$(cat ../../../../tags)\" -run TestGenerateLadingPolicyStore ."

import (
	"testing"

	"github.com/stergiotis/boxer/public/fs/lading/ladingvocab"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema/storegen"
	"github.com/stretchr/testify/require"
)

// TestGenerateLadingPolicyStore emits the mount-policy store. Run it after
// changing the DTO or the vocabulary:
//
//	go test -tags "$(cat tags)" -run TestGenerateLadingPolicyStore ./public/fs/lading/ladingpolicy/
//
// This is the one kind of the four that is facts-bound, so it goes through
// storegen rather than recordstore/gen directly: the table, the database and
// the row config are not parameters — they are what makes it facts-bound —
// and the store carries no EnsureTable, because `chstore` is `boxer.facts`'s
// sole DDL author (ADR-0184 §SD2).
//
// A mount's policy belongs there and its snapshots do not, for the reason
// ADR-0105 §D3a moved persist state the other way: the policy is runtime
// state — edited, outliving every snapshot taken under it, not retained by
// the store's TTL — while the snapshots are bulk data whose retention and
// indexes the store has to own.
func TestGenerateLadingPolicyStore(t *testing.T) {
	ids, err := storegen.MembershipIds(ladingvocab.NkRegistry)
	require.NoError(t, err)
	require.NoError(t, storegen.Input{
		PackageName:    "ladingpolicy",
		StoreName:      "Policy",
		ComponentPaths: []string{"./ladingmount_dto.go"},
		OutDir:         ".",
		ImportPath:     "github.com/stergiotis/boxer/public/fs/lading/ladingpolicy",
		Ids:            ids,
	}.Generate())
}
