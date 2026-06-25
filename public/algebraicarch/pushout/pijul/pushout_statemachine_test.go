// Stateful property harness for the pushout-native backend: a rapid
// state machine drives random verb sequences (record / resolve / push /
// pull / email-apply / unrecord / sweep / clone) over a fleet of repos
// and checks, after every action, the structural invariants of every
// graggle, content conservation (every live node's bytes are traceable
// to an applied patch), no-live-purge, and dependency closure of the
// applied set. After the sequence, pairwise syncing to a fixpoint must
// converge all repos to the same observable state — the system-level
// commutativity claim.
//
// Time is a deterministic fake: a shared clock that advances one minute
// per DeleteNode stamp, so retention sweeps purge real tombstones in
// milliseconds and rapid's replay/shrinking stays reproducible.
package pijul

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"pgregory.net/rapid"

	"github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/qc"
	t "github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/types"
	"github.com/stergiotis/boxer/public/algebraicarch/pushout/repo"
)

var smPaths = []string{"alpha", "beta", "gamma", "delta"}

var smValues = []string{"", "1", "2", "x", `q"q`, "multi\nline", `back\slash`}

const smMaxRepos = 5

// smClock is the deterministic time source shared by the whole fleet.
// Each tombstone stamp advances it by one minute; Peek reads without
// advancing (used as a sweep's "now"). Deterministic relative to the
// drawn action sequence — never wall time, which would break rapid's
// replay and shrinking.
type smClock struct {
	mu sync.Mutex
	t  time.Time
}

func newSMClock() *smClock {
	return &smClock{t: time.Unix(1_750_000_000, 0).UTC()}
}

func (c *smClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(time.Minute)
	return c.t
}

func (c *smClock) Peek() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

type repoMachine struct {
	tb      *testing.T
	ctx     context.Context
	root    string
	backend BackendI
	clock   *smClock
	repos   []*PushoutRepo
}

func (m *repoMachine) cleanup() {
	_ = os.RemoveAll(m.root)
}

func newRepoMachine(tb *testing.T, rt *rapid.T) *repoMachine {
	m := &repoMachine{tb: tb, ctx: context.Background(), clock: newSMClock()}
	m.backend = NewPushoutBackendWithClock(m.clock.Now)
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
		repo := m.backend.NewRepo(actor, dir).(*PushoutRepo)
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

	_, _, err = repo.SetAndRecord(m.ctx, edited, repo.Actor(), "sm edit "+path)
	if err != nil && !errors.Is(err, ErrCellCreateWhileConflicted) {
		rt.Fatalf("record on %s: %v", repo.Actor(), err)
	}
}

func (m *repoMachine) push(rt *rapid.T) {
	src := m.pick(rt, "src")
	dst := m.pick(rt, "dst")
	if src == dst {
		return
	}
	if _, err := src.Push(m.ctx, dst); err != nil {
		rt.Fatalf("push %s→%s: %v", src.Actor(), dst.Actor(), err)
	}
}

func (m *repoMachine) pull(rt *rapid.T) {
	dst := m.pick(rt, "dst")
	src := m.pick(rt, "src")
	if src == dst {
		return
	}
	if _, _, err := dst.Pull(m.ctx, src); err != nil {
		rt.Fatalf("pull %s←%s: %v", dst.Actor(), src.Actor(), err)
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
		rt.Fatalf("export from %s: %v", src.Actor(), err)
	}
	_, err = dst.Apply(m.ctx, env)
	if err != nil && !errors.Is(err, repo.ErrMissingDependency) {
		rt.Fatalf("email-apply on %s: %v", dst.Actor(), err)
	}
}

// unrecord backs out a randomly chosen applied patch. Dependent-ordering
// rejections and retention-horizon rejections (the patch tombstoned a
// node whose content a sweep has purged, and it is the last deleter)
// are expected; anything else must succeed.
func (m *repoMachine) unrecord(rt *rapid.T) {
	pr := m.pick(rt, "repo")
	applied, err := pr.Engine().Applied(m.ctx)
	if err != nil {
		rt.Fatalf("applied: %v", err)
	}
	if len(applied) == 0 {
		return
	}
	hash := applied[rapid.IntRange(0, len(applied)-1).Draw(rt, "idx")]
	_, err = pr.Unrecord(m.ctx, hash)
	if err != nil &&
		!errors.Is(err, repo.ErrDependentExists) &&
		!errors.Is(err, repo.ErrRetentionBlocked) {
		rt.Fatalf("unrecord on %s: %v", pr.Actor(), err)
	}
}

// sweep purges tombstone content past a drawn retention horizon on one
// repo — retention is a session-local decision, so the fleet diverges
// in what it CAN unrecord while staying convergent in observable state.
// Oracles: every reported ID is a purged tombstone, the count matches,
// and an immediate identical sweep is a no-op.
func (m *repoMachine) sweep(rt *rapid.T) {
	pr := m.pick(rt, "repo")
	horizon := time.Duration(rapid.IntRange(0, 30).Draw(rt, "horizonMin")) * time.Minute
	now := m.clock.Peek()
	report, err := pr.Sweep(m.ctx, now, horizon)
	if err != nil {
		rt.Fatalf("sweep on %s: %v", pr.Actor(), err)
	}
	verr := pr.Engine().View(m.ctx, func(v repo.ViewI) error {
		g := v.Graph()
		for _, id := range report.Purged {
			if !g.IsDeleted(id) {
				rt.Fatalf("sweep on %s purged non-tombstone %v", pr.Actor(), id)
			}
			if g.NodeContentStatus(id) != t.NodeContentStatusPurged {
				rt.Fatalf("sweep on %s: %v not marked purged", pr.Actor(), id)
			}
		}
		return nil
	})
	if verr != nil {
		rt.Fatalf("view: %v", verr)
	}
	if again, err := pr.Sweep(m.ctx, now, horizon); err != nil || len(again.Purged) != 0 {
		rt.Fatalf("sweep on %s not idempotent: %d purged, %v", pr.Actor(), len(again.Purged), err)
	}
}

// reopen closes a repo and recovers it from storage in place — the
// crash/restart oracle: the recovered repo must be observably identical
// to the one that closed, including retention markers, under whatever
// random history (records, unrecords, sweeps, pulls) preceded it.
func (m *repoMachine) reopen(rt *rapid.T) {
	idx := rapid.IntRange(0, len(m.repos)-1).Draw(rt, "repo")
	old := m.repos[idx]
	before := m.observable(old)
	beforeRet := m.retentionFingerprint(old)
	if err := old.Close(m.ctx); err != nil {
		rt.Fatalf("close %s: %v", old.Actor(), err)
	}
	fresh := m.backend.NewRepo(old.Actor(), old.Path()).(*PushoutRepo)
	if _, err := fresh.Init(m.ctx); err != nil {
		rt.Fatalf("recover %s: %v", old.Actor(), err)
	}
	if got := m.observable(fresh); got != before {
		rt.Fatalf("recovery diverged for %s:\nbefore:\n%s\nafter:\n%s", old.Actor(), before, got)
	}
	// The pending retention horizon must survive recovery (ADR-0079): the
	// durable ledger seeds tombstoneAt so full replay cannot reset it.
	if got := m.retentionFingerprint(fresh); got != beforeRet {
		rt.Fatalf("retention horizon diverged across reopen for %s:\nbefore:\n%s\nafter:\n%s", old.Actor(), beforeRet, got)
	}
	m.repos[idx] = fresh
}

// clone forks a random repo mid-session. The clone must be observably
// identical to its source at birth, then joins the fleet (replacing a
// random member once the fleet is full) and participates in every
// subsequent verb including the final convergence.
func (m *repoMachine) clone(rt *rapid.T) {
	src := m.pick(rt, "src")
	dir, err := os.MkdirTemp(m.root, "clone_*")
	if err != nil {
		rt.Fatalf("mkdir: %v", err)
	}
	dest, _, err := m.backend.Clone(m.ctx, src, dir, fmt.Sprintf("clone%d", len(m.repos)))
	if err != nil {
		rt.Fatalf("clone of %s: %v", src.Actor(), err)
	}
	d := dest.(*PushoutRepo)
	if got, want := m.observable(d), m.observable(src); got != want {
		rt.Fatalf("clone not observably identical to source:\nclone: %s\nsrc:   %s", got, want)
	}
	dApplied, derr := d.Engine().Applied(m.ctx)
	sApplied, serr := src.Engine().Applied(m.ctx)
	if derr != nil || serr != nil || len(dApplied) != len(sApplied) {
		rt.Fatalf("clone applied-log diverges from source: %d vs %d (%v %v)", len(dApplied), len(sApplied), derr, serr)
	}
	if len(m.repos) < smMaxRepos {
		m.repos = append(m.repos, d)
	} else {
		m.repos[rapid.IntRange(0, len(m.repos)-1).Draw(rt, "replaceSlot")] = d
	}
}

// check runs after every action: structural invariants, content
// conservation, and dependency closure on every repo — all through the
// engine's read transaction.
func (m *repoMachine) check(rt *rapid.T) {
	for _, pr := range m.repos {
		eng := pr.Engine()
		if eng == nil {
			continue
		}
		err := eng.View(m.ctx, func(v repo.ViewI) error {
			g := v.Graph()
			for _, e := range qc.CheckInvariants(g.(t.InspectableI)) {
				rt.Fatalf("invariant violated on %s: %v", pr.Actor(), e)
			}
			appliedSet := make(map[t.PatchHash]struct{})
			knownContent := make(map[string]struct{})
			for _, h := range v.Applied() {
				info, ok := v.PatchInfo(h)
				if !ok || info.Patch == nil {
					rt.Fatalf("%s: applied %s has no envelope", pr.Actor(), h)
				}
				for _, c := range info.Patch.Changes {
					knownContent[string(c.Content)] = struct{}{}
				}
				for _, dep := range info.Patch.Dependencies {
					if _, ok := appliedSet[dep]; !ok {
						// Apply order: a dep must precede its dependent.
						rt.Fatalf("%s: applied %s before its dependency %s", pr.Actor(), h, dep)
					}
				}
				appliedSet[h] = struct{}{}
			}
			for id := range g.AllLiveNodes() {
				if id == t.RootNodeID {
					continue
				}
				if _, ok := knownContent[string(g.NodeContent(id))]; !ok {
					rt.Fatalf("%s: live node %v carries content not traceable to any applied patch: %q", pr.Actor(), id, g.NodeContent(id))
				}
				if g.NodeContentStatus(id) == t.NodeContentStatusPurged {
					rt.Fatalf("%s: LIVE node %v has purged content — sweep must only touch tombstones, and a purged tombstone must never resurrect", pr.Actor(), id)
				}
			}
			return nil
		})
		if err != nil {
			rt.Fatalf("view on %s: %v", pr.Actor(), err)
		}
	}
}

// observable projects a repo's State onto comparable ground truth:
// path/value for clean cells, path/sides for conflicts. Credit is
// excluded — first-writer-wins provenance may legitimately differ
// between repos for hash-converged patches.
// retentionFingerprint renders a repo's durable retention horizon
// (replica-local, so compared only across the SAME repo's reopen, never
// across the fleet — unlike observable).
func (m *repoMachine) retentionFingerprint(pr *PushoutRepo) string {
	entries, err := pr.Engine().RetentionStamps(m.ctx)
	if err != nil {
		m.tb.Fatalf("retention stamps: %v", err)
	}
	var sb strings.Builder
	for _, e := range entries {
		fmt.Fprintf(&sb, "%v=%d\n", e.Node, e.UnixNano)
	}
	return sb.String()
}

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
				srcApplied, aerr := src.Engine().Applied(m.ctx)
				if aerr != nil {
					rt.Fatalf("applied: %v", aerr)
				}
				dstApplied, aerr := dst.Engine().Applied(m.ctx)
				if aerr != nil {
					rt.Fatalf("applied: %v", aerr)
				}
				dstHas := make(map[t.PatchHash]struct{}, len(dstApplied))
				for _, h := range dstApplied {
					dstHas[h] = struct{}{}
				}
				missing := 0
				for _, h := range srcApplied {
					if _, ok := dstHas[h]; !ok {
						missing++
					}
				}
				if missing == 0 {
					continue
				}
				changed = true
				if _, _, err := dst.Pull(m.ctx, src); err != nil {
					rt.Fatalf("converge pull %s←%s: %v", dst.Actor(), src.Actor(), err)
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
			rt.Fatalf("post-sync divergence:\n--- %s:\n%s--- %s:\n%s", m.repos[0].Actor(), want, repo.Actor(), got)
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
			"sweep":      m.sweep,
			"clone":      m.clone,
			"reopen":     m.reopen,
			"":           m.check,
		})
		m.converge(rt)
	})
}
