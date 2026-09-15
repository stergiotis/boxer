package watchbillpresence_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema/storegen"
	"github.com/stergiotis/boxer/public/keelson/runtime/vocab"
)

//go:generate sh -c "go test -tags=\"$(cat ../../../../../tags)\" -run TestGeneratePresenceStore ."

// TestGeneratePresenceStore (re)generates the facts-bound presence store in
// place; run it after changing the DTO or the vocabulary. The ids come from
// the runtime vocabulary the heartbeat writers use, so a scan filters on
// the same memberships the rows carry. The store is externally provisioned
// by construction: chstore owns boxer.facts (ADR-0184 §SD2).
func TestGeneratePresenceStore(t *testing.T) {
	ids, err := storegen.MembershipIds(vocab.NkRegistry)
	require.NoError(t, err)
	require.NoError(t, storegen.Input{
		PackageName:    "watchbillpresence",
		StoreName:      "Presence",
		ComponentPaths: []string{"./worker_dto.go"},
		OutDir:         ".",
		ImportPath:     "github.com/stergiotis/boxer/public/keelson/runtime/watchbill/watchbillpresence",
		Ids:            ids,
	}.Generate())
}
