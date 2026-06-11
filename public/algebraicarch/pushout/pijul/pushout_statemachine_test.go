// Stateful property harness for the pushout-native backend: a rapid
// state machine drives random verb sequences (record / resolve / push /
// pull / email-apply / unrecord) over three repos and checks, after
// every action, the structural invariants of every graggle, content
// conservation (every live node's bytes are traceable to an applied
// patch), and dependency closure of the applied set. After the sequence,
// pairwise syncing to a fixpoint must converge all repos to the same
// observable state — the system-level commutativity claim.
package pijul

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"pgregory.net/rapid"

	"github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/qc"
	t "github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/types"
)

var smPaths = []string{"alpha", "beta", "gamma", "delta"}

var smValues = []string{"", "1", "2", "x", `q"q`, "multi\nline", `back\slash`}

type repoMachine struct {
	tb    *testing.T
	ctx   context.Context
	root  string
	repos []*PushoutRepo
}

func (m *repoMachine) cleanup() {
	_ = os.RemoveAll(m.root)
}

func newRepoMachine(tb *testing.T, rt *rapid.T) *repoMachine {
	m := &repoMachine{tb: tb, ctx: context.Background()}
	b := NewPushoutBackend()
	// One root dir per property invocation, removed in cleanup() —
	// rapid's shrinker re-runs the property thousands of times, and
	// leaking three repo trees per run exhausts the tmpfs.
	root, err := os.MkdirTemp("", "pushout_sm_*")
	if err != nil {
		rt.Fatalf("mkdir: %v", err)
	}
	m.root = root
	for _, actor := range []string{"alice", "bob", "carol"} {
		dir, err := os.MkdirTemp(root, actor+"_*")
		if err != nil {
			rt.Fatalf("mkdir: %v", err)
		}
		repo := b.NewRepo(actor, dir).(*PushoutRepo)
		if _, err := repo.Init(m.ctx); err != nil {
			rt.Fatalf("init %s: %v", actor, err)
		}
		m.repos = append(m.repos, repo)
	}
	// Seed a converged base so early pulls have something to move.
	if _, _, err := m.repos[0].SetAndRecord(m.ctx, []KVLine{{Path: "alpha", Value: "base"}}, "alice", "base"); err != nil {
		rt.Fatalf("seed: %v", err)
	}
	for _, r := range m.repos[1:] {
		if _, _, err := r.Pull(m.ctx, m.repos[0]); err != nil {
			rt.Fatalf("seed pull: %v", err)
		}
	}
	return m
}

func (m *repoMachine) pick(rt *rapid.T, label string) *PushoutRepo {
	return m.repos[rapid.IntRange(0, len(m.repos)-1).Draw(rt, label)]
}

// record edits the repo's current state: on a clean repo it mutates a
// random cell (set / delete / create); on a conflicted repo it either
// resolves a random conflict to one of its sides (or a fresh value) or
// edits a clean cell. Creation attempts during conflict are expected to
// be rejected and count as a no-op.
func (m *repoMachine) record(rt *rapid.T) {
	repo := m.pick(rt, "repo")
	cells, _, _, err := repo.State(m.ctx)
	if err != nil {
		rt.Fatalf("state: %v", err)
	}
	edited := append([]KVLine(nil), cells...)
	path := rapid.SampledFrom(smPaths).Draw(rt, "path")
	value := rapid.SampledFrom(smValues).Draw(rt, "value")

	idx := -1
	for i := range edited {
		if edited[i].Path == path {
			idx = i
			break
		}
	}
	switch {
	case idx < 0:
		edited = append(edited, KVLine{Path: path, Value: value})
	case rapid.Bool().Draw(rt, "delete"):
		edited = append(edited[:idx], edited[idx+1:]...)
	case edited[idx].Conflict != nil:
		// Resolve: keep one existing side or pick the drawn value.
		sides := edited[idx].Conflict.AllValues()
		choice := rapid.IntRange(0, len(sides)).Draw(rt, "side")
		v := value
		if choice < len(sides) {
			v = sides[choice]
		}
		edited[idx] = KVLine{Path: path, Value: v}
	default:
		edited[idx] = KVLine{Path: path, Value: value, Conflict: nil, Credit: nil}
	}

	_, _, err = repo.SetAndRecord(m.ctx, edited, repo.actor, "sm edit "+path)
	if err != nil && !strings.Contains(err.Error(), "cannot create cell") {
		rt.Fatalf("record on %s: %v", repo.actor, err)
	}
}

func (m *repoMachine) push(rt *rapid.T) {
	src := m.pick(rt, "src")
	dst := m.pick(rt, "dst")
	if src == dst {
		return
	}
	if _, err := src.Push(m.ctx, dst); err != nil {
		rt.Fatalf("push %s→%s: %v", src.actor, dst.actor, err)
	}
}

func (m *repoMachine) pull(rt *rapid.T) {
	dst := m.pick(rt, "dst")
	src := m.pick(rt, "src")
	if src == dst {
		return
	}
	if _, _, err := dst.Pull(m.ctx, src); err != nil {
		rt.Fatalf("pull %s←%s: %v", dst.actor, src.actor, err)
	}
}

// emailApply ships only the LATEST patch of one repo to another —
// exactly the demo's "Email Patch" flow. Missing dependencies and
// duplicates are expected outcomes.
func (m *repoMachine) emailApply(rt *rapid.T) {
	src := m.pick(rt, "src")
	dst := m.pick(rt, "dst")
	if src == dst {
		return
	}
	env, _, err := src.ExportLatest(m.ctx)
	if err != nil {
		if strings.Contains(err.Error(), "no patches recorded") {
			return
		}
		rt.Fatalf("export from %s: %v", src.actor, err)
	}
	_, err = dst.Apply(m.ctx, env)
	if err != nil && !strings.Contains(err.Error(), "missing dependency") {
		rt.Fatalf("email-apply on %s: %v", dst.actor, err)
	}
}

// unrecord backs out a randomly chosen applied patch. Dependent-ordering
// rejections are expected; anything else must succeed.
func (m *repoMachine) unrecord(rt *rapid.T) {
	repo := m.pick(rt, "repo")
	repo.Mu.Lock()
	n := len(repo.appliedHash)
	var hash t.PatchHash
	if n > 0 {
		hash = repo.appliedHash[rapid.IntRange(0, n-1).Draw(rt, "idx")]
	}
	repo.Mu.Unlock()
	if n == 0 {
		return
	}
	_, err := repo.Unrecord(m.ctx, hash)
	if err != nil && !strings.Contains(err.Error(), "unrecord dependents first") {
		rt.Fatalf("unrecord on %s: %v", repo.actor, err)
	}
}

// check runs after every action: structural invariants, content
// conservation, and dependency closure on every repo.
func (m *repoMachine) check(rt *rapid.T) {
	for _, repo := range m.repos {
		repo.Mu.Lock()
		g := repo.Graggle
		if g == nil {
			repo.Mu.Unlock()
			continue
		}
		for _, e := range qc.CheckInvariants(g) {
			repo.Mu.Unlock()
			rt.Fatalf("invariant violated on %s: %v", repo.actor, e)
		}

		applied := make(map[t.PatchHash]struct{}, len(repo.appliedHash))
		knownContent := make(map[string]struct{})
		for _, h := range repo.appliedHash {
			applied[h] = struct{}{}
			env, ok := repo.MetaByHash[h]
			if !ok || env.Patch == nil {
				repo.Mu.Unlock()
				rt.Fatalf("%s: applied %s has no envelope", repo.actor, h)
			}
			for _, c := range env.Patch.Changes {
				knownContent[string(c.Content)] = struct{}{}
			}
			for _, dep := range env.Patch.Dependencies {
				if _, ok := applied[dep]; !ok {
					// Order within appliedHash is apply order, so a dep
					// must appear before its dependent.
					repo.Mu.Unlock()
					rt.Fatalf("%s: applied %s before its dependency %s", repo.actor, h, dep)
				}
			}
		}
		for id := range g.AllLiveNodes() {
			if id == t.RootNodeID {
				continue
			}
			if _, ok := knownContent[string(g.NodeContent(id))]; !ok {
				repo.Mu.Unlock()
				rt.Fatalf("%s: live node %v carries content not traceable to any applied patch: %q", repo.actor, id, g.NodeContent(id))
			}
		}
		repo.Mu.Unlock()
	}
}

// observable projects a repo's State onto comparable ground truth:
// path/value for clean cells, path/sides for conflicts. Credit is
// excluded — first-writer-wins provenance may legitimately differ
// between repos for hash-converged patches.
func (m *repoMachine) observable(repo *PushoutRepo) string {
	cells, _, _, err := repo.State(m.ctx)
	if err != nil {
		m.tb.Fatalf("state: %v", err)
	}
	var sb strings.Builder
	for _, c := range cells {
		if c.Conflict != nil {
			fmt.Fprintf(&sb, "%s ?%q\n", c.Path, c.Conflict.AllValues())
		} else {
			fmt.Fprintf(&sb, "%s =%q\n", c.Path, c.Value)
		}
	}
	return sb.String()
}

// converge pairwise-pulls until a fixpoint (bounded), then demands all
// repos agree on the observable state.
func (m *repoMachine) converge(rt *rapid.T) {
	for round := 0; round < 6; round++ {
		changed := false
		for _, dst := range m.repos {
			for _, src := range m.repos {
				if src == dst {
					continue
				}
				missing, err := src.missingOn(dst)
				if err != nil {
					rt.Fatalf("missingOn: %v", err)
				}
				if len(missing) == 0 {
					continue
				}
				changed = true
				if _, _, err := dst.Pull(m.ctx, src); err != nil {
					rt.Fatalf("converge pull %s←%s: %v", dst.actor, src.actor, err)
				}
			}
		}
		if !changed {
			break
		}
	}
	want := m.observable(m.repos[0])
	for _, repo := range m.repos[1:] {
		if got := m.observable(repo); got != want {
			rt.Fatalf("post-sync divergence:\n--- %s:\n%s--- %s:\n%s", m.repos[0].actor, want, repo.actor, got)
		}
	}
	m.check(rt)
}

func TestPushoutBackend_StateMachine(tt *testing.T) {
	rapid.Check(tt, func(rt *rapid.T) {
		m := newRepoMachine(tt, rt)
		defer m.cleanup()
		rt.Repeat(map[string]func(*rapid.T){
			"record":     m.record,
			"push":       m.push,
			"pull":       m.pull,
			"emailApply": m.emailApply,
			"unrecord":   m.unrecord,
			"":           m.check,
		})
		m.converge(rt)
	})
}
