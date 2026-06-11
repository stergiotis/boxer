// Differential test: the same action script through the pushout-native
// and pijul-text backends must yield the same observable State. The two
// BackendI implementations are each other's reference; divergence means
// one of them mis-models patch semantics or the cell codec. Skipped when
// no pijul binary is installed.
package pijul

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestBackends_DifferentialStateEquivalence(tt *testing.T) {
	if _, err := exec.LookPath("pijul"); err != nil {
		tt.Skip("pijul binary not installed; skipping differential test")
	}
	ctx := context.Background()

	native := NewPushoutBackend().NewRepo("alice", tt.TempDir())
	text := NewPijulTextBackend(NewCliRunner(), "customer.txt").
		NewRepo("alice", filepath.Join(tt.TempDir(), "repo"))
	repos := []RepoI{native, text}

	for _, r := range repos {
		if _, err := r.Init(ctx); err != nil {
			tt.Fatalf("init %T: %v", r, err)
		}
	}

	script := [][]KVLine{
		{{Path: "name", Value: "Alice"}, {Path: "city", Value: "Bern"}},
		{{Path: "name", Value: "Alice"}, {Path: "city", Value: "Zurich"}, {Path: "tier", Value: "gold"}},
		{{Path: "name", Value: "Alice"}, {Path: "tier", Value: `2"`}},
	}
	for step, cells := range script {
		for _, r := range repos {
			if _, _, err := r.SetAndRecord(ctx, cells, "alice", "step"); err != nil {
				tt.Fatalf("step %d on %T: %v", step, r, err)
			}
		}
		var states [][]KVLine
		for _, r := range repos {
			cells, _, _, err := r.State(ctx)
			if err != nil {
				tt.Fatalf("state %T: %v", r, err)
			}
			states = append(states, cells)
		}
		if len(states[0]) != len(states[1]) {
			tt.Fatalf("step %d: cell count diverged: native=%+v text=%+v", step, states[0], states[1])
		}
		nativeByPath := map[string]string{}
		for _, c := range states[0] {
			nativeByPath[c.Path] = c.Value
		}
		for _, c := range states[1] {
			if v, ok := nativeByPath[c.Path]; !ok || v != c.Value {
				tt.Fatalf("step %d: path %s diverged: native=%q text=%q", step, c.Path, v, c.Value)
			}
		}
	}
}
