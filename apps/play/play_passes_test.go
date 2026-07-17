package play

import (
	"testing"

	"github.com/stergiotis/boxer/public/keelson/data/passreg"
	passregdefaults "github.com/stergiotis/boxer/public/keelson/data/passreg/defaults"
)

// TestRegisterPassesOrdering pins the host set's apply order:
// canonicalisation first (50), then the standard entries (macro expansion
// 100, late-bound column resolution 200) consuming canonical shapes.
// TestExecuteArrowStreamCanonicalizesViaHostSet proves the behavioural
// consequence through the client.
func TestRegisterPassesOrdering(t *testing.T) {
	reg := passreg.NewRegistry()
	if err := passregdefaults.RegisterStandard(reg); err != nil {
		t.Fatalf("RegisterStandard: %v", err)
	}
	if err := RegisterPasses(reg); err != nil {
		t.Fatalf("RegisterPasses: %v", err)
	}

	rows := reg.Catalog()
	got := make([]string, 0, len(rows))
	for _, r := range rows {
		if r.Stage == passreg.StagePreExecute {
			got = append(got, r.Name)
		}
	}
	want := []string{"CanonicalizeFull", "ExpandLwIdMacros", "ResolveColumnNames"}
	if len(got) != len(want) {
		t.Fatalf("pre-execute catalog = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("pre-execute catalog order = %v, want %v", got, want)
		}
	}

	for _, r := range rows {
		if r.Name != "CanonicalizeFull" {
			continue
		}
		if r.LateBound {
			t.Error("CanonicalizeFull must be a concrete entry, not a factory")
		}
		if !r.Properties.Idempotent {
			t.Error("CanonicalizeFull catalog row must carry the Idempotent property")
		}
	}
}
