package pijul

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/observability/eh"

	"github.com/stergiotis/boxer/public/algebraicarch/pushout/envelope"
	"github.com/stergiotis/boxer/public/algebraicarch/pushout/exchange"
	"github.com/stergiotis/boxer/public/algebraicarch/pushout/exchange/inproc"
	"github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/patch"
	t "github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/types"
	"github.com/stergiotis/boxer/public/algebraicarch/pushout/repo"
	"github.com/stergiotis/boxer/public/algebraicarch/pushout/repo/filestore"
)

// ErrCellCreateWhileConflicted: creating a brand-new cell while the
// graggle is conflicted has no reliable anchor (no linear order) and is
// rejected; resolve the conflicts first.
var ErrCellCreateWhileConflicted = errors.New("cannot create a cell while the graggle is conflicted")

// pushoutBackend is the native realisation of [BackendI]: a thin
// KV-cell adapter over the domain-neutral engine (pushout/repo) with a
// filestore per actor under <repoDir>/.pushout/, jsonv1 envelopes, and
// the in-process exchange carrier for Push/Pull. All engine concerns —
// persistence, recovery, dependency gating, identity disambiguation,
// retention — live in the engine; this file only translates cells to
// changes and views back to cells.
type pushoutBackend struct {
	clock func() time.Time
}

var _ BackendI = (*pushoutBackend)(nil)

// NewPushoutBackend builds a native backend with the wall clock.
func NewPushoutBackend() (b *pushoutBackend) {
	b = &pushoutBackend{clock: time.Now}
	return
}

// NewPushoutBackendWithClock builds a native backend with an injected
// time source (deterministic tests; per ADR-0079 SD8 engine paths never
// read wall time directly).
func NewPushoutBackendWithClock(clock func() time.Time) (b *pushoutBackend) {
	b = &pushoutBackend{clock: clock}
	return
}

func (inst *pushoutBackend) Name() (n string) {
	n = "pushout-native"
	return
}

func (inst *pushoutBackend) NewRepo(actor string, path string) (r RepoI) {
	r = &PushoutRepo{actor: actor, path: path, clock: inst.clock}
	return
}

// Clone opens a fresh repo at destPath and pulls src's entire history
// over the in-process carrier — no file copying, no shared state.
func (inst *pushoutBackend) Clone(ctx context.Context, src RepoI, destPath string, destActor string) (dest RepoI, audit string, err error) {
	srcRepo, ok := src.(*PushoutRepo)
	if !ok {
		err = eh.Errorf("pushout-native cannot clone from a %T", src)
		return
	}
	if srcRepo.eng == nil {
		err = eh.Errorf("clone source %s is not initialised", srcRepo.path)
		return
	}
	d := &PushoutRepo{actor: destActor, path: destPath, clock: inst.clock}
	if _, err = d.Init(ctx); err != nil {
		return
	}
	stats, err := exchange.Pull(ctx, d.eng, inproc.New(srcRepo.eng))
	if err != nil {
		return
	}
	dest = d
	audit = fmt.Sprintf("[pushout-native] clone %s → %s (%d patches)", srcRepo.path, destPath, stats.Applied)
	return
}

// PushoutRepo is one actor's working copy: a KV adapter over a
// *repo.Repo engine. Advanced consumers (draft-diff previews, DOT
// visualisations) reach the engine's read API via Engine().View — the
// engine's internals are not exposed.
type PushoutRepo struct {
	actor string
	path  string
	clock func() time.Time
	eng   *repo.Repo
}

var _ RepoI = (*PushoutRepo)(nil)

func (inst *PushoutRepo) Path() (p string) {
	p = inst.path
	return
}

// Actor returns the actor name this working copy records as.
func (inst *PushoutRepo) Actor() (a string) {
	a = inst.actor
	return
}

// Engine exposes the underlying engine for read transactions and
// advanced verbs (Sweep). Nil before Init.
func (inst *PushoutRepo) Engine() (r *repo.Repo) {
	r = inst.eng
	return
}

// Init opens — or RECOVERS — the repo at <path>/.pushout. An existing
// store is recovered, not reset.
func (inst *PushoutRepo) Init(ctx context.Context) (audit string, err error) {
	st, err := filestore.Open(filepath.Join(inst.path, ".pushout"))
	if err != nil {
		return
	}
	reg, err := envelope.NewRegistry(envelope.JSONV1{})
	if err != nil {
		return
	}
	eng, err := repo.Open(ctx, repo.Options{
		Storage:  st,
		Codecs:   reg,
		Wire:     envelope.JSONV1Name,
		Producer: inst.actor,
		Clock:    inst.clock,
	})
	if err != nil {
		return
	}
	inst.eng = eng
	audit = fmt.Sprintf("[pushout-native] open %s", inst.path)
	return
}

// Close checkpoints and releases the engine.
func (inst *PushoutRepo) Close(ctx context.Context) (err error) {
	if inst.eng == nil {
		return
	}
	err = inst.eng.Close(ctx)
	inst.eng = nil
	return
}

// Sweep delegates to the engine's retention sweep (durable purge).
func (inst *PushoutRepo) Sweep(ctx context.Context, now time.Time, horizon time.Duration) (report repo.SweepReport, err error) {
	if inst.eng == nil {
		err = eh.Errorf("repo not initialised")
		return
	}
	report, err = inst.eng.Sweep(ctx, now, horizon)
	return
}

// State derives the demo's KVLine slice from a read transaction: linear
// order when available, conflict grouping by cell path otherwise, plus
// the patch log.
func (inst *PushoutRepo) State(ctx context.Context) (cells []KVLine, log []PatchMetadata, audit string, err error) {
	if inst.eng == nil {
		audit = fmt.Sprintf("[pushout-native] state (uninit) %s", inst.path)
		return
	}
	err = inst.eng.View(ctx, func(v repo.ViewI) error {
		if order := v.LinearOrder(); order != nil {
			cells = cellsFromLinearOrder(v, order)
		} else {
			cells = cellsFromConflictedGraggle(v)
		}
		applied := v.Applied()
		log = make([]PatchMetadata, 0, len(applied))
		for _, h := range applied {
			log = append(log, metaToPatchMetadata(v, h))
		}
		return nil
	})
	if err != nil {
		return
	}
	audit = fmt.Sprintf("[pushout-native] state %s (%d cells, %d patches)", inst.path, len(cells), len(log))
	return
}

func cellsFromLinearOrder(v repo.ViewI, order []t.NodeID) (cells []KVLine) {
	g := v.Graph()
	for _, n := range order {
		if n == t.RootNodeID {
			continue
		}
		path, val, ok := splitKVLine(strings.TrimSuffix(string(g.NodeContent(n)), "\n"))
		if !ok {
			continue
		}
		cell := KVLine{Path: path, Value: val}
		meta := metaToPatchMetadata(v, n.Patch)
		if !meta.ID.Empty() {
			cell.Credit = &meta
		}
		cells = append(cells, cell)
	}
	return
}

// cellsFromConflictedGraggle handles the conflict case by grouping live
// nodes by cell path; every live node for a path becomes one side of
// the ConflictData. Ordering is alphabetical (no linear order exists).
func cellsFromConflictedGraggle(v repo.ViewI) (cells []KVLine) {
	g := v.Graph()
	byPath := make(map[string][]t.NodeID)
	var paths []string
	for n := range g.AllLiveNodes() {
		if n == t.RootNodeID {
			continue
		}
		p, _, ok := splitKVLine(strings.TrimSuffix(string(g.NodeContent(n)), "\n"))
		if !ok {
			continue
		}
		if _, exists := byPath[p]; !exists {
			paths = append(paths, p)
		}
		byPath[p] = append(byPath[p], n)
	}
	slices.Sort(paths)
	for _, p := range paths {
		nodes := byPath[p]
		switch len(nodes) {
		case 1:
			n := nodes[0]
			_, val, _ := splitKVLine(strings.TrimSuffix(string(g.NodeContent(n)), "\n"))
			cell := KVLine{Path: p, Value: val}
			meta := metaToPatchMetadata(v, n.Patch)
			if !meta.ID.Empty() {
				cell.Credit = &meta
			}
			cells = append(cells, cell)
		default:
			values := make([]string, 0, len(nodes))
			for _, n := range nodes {
				_, val, ok := splitKVLine(strings.TrimSuffix(string(g.NodeContent(n)), "\n"))
				if !ok {
					continue
				}
				values = append(values, val)
			}
			cd := &ConflictData{}
			if len(values) > 0 {
				cd.AliceValue = values[0]
			}
			if len(values) > 1 {
				cd.BobValue = values[1]
			}
			if len(values) > 2 {
				cd.OtherValues = append(cd.OtherValues, values[2:]...)
			}
			cells = append(cells, KVLine{Path: p, Conflict: cd})
		}
	}
	return
}

func metaToPatchMetadata(v repo.ViewI, h t.PatchHash) (m PatchMetadata) {
	info, ok := v.PatchInfo(h)
	if !ok {
		return
	}
	deps := make([]PatchID, 0, len(info.Patch.Dependencies))
	for _, d := range info.Patch.Dependencies {
		deps = append(deps, PatchID{Hex: hex.EncodeToString(d[:])})
	}
	m = PatchMetadata{
		ID:           PatchID{Hex: hex.EncodeToString(h[:])},
		Authors:      []string{info.Patch.Author},
		Timestamp:    info.Timestamp,
		Message:      info.Patch.Description,
		Dependencies: deps,
	}
	return
}

// SetAndRecord computes the diff (or, in conflict mode, the resolution
// plus clean-cell edits) inside a read transaction, then records
// through the engine. Creating a brand-new cell during a conflict is
// rejected (ErrCellCreateWhileConflicted).
func (inst *PushoutRepo) SetAndRecord(ctx context.Context, cells []KVLine, author string, message string) (id PatchID, audit string, err error) {
	if inst.eng == nil {
		err = eh.Errorf("repo not initialised")
		return
	}
	if err = validateCellPaths(cells); err != nil {
		return
	}
	var changes []patch.Change
	err = inst.eng.View(ctx, func(v repo.ViewI) error {
		if v.LinearOrder() == nil {
			var rerr error
			changes, rerr = changesForResolution(v, cells)
			return rerr
		}
		var derr error
		changes, derr = changesForLineDiff(v, cells)
		return derr
	})
	if err != nil {
		return
	}
	if len(changes) == 0 {
		audit = fmt.Sprintf("[pushout-native] record %s: no changes", inst.actor)
		return
	}

	h, err := inst.eng.Record(ctx, author, message, changes)
	if err != nil {
		if errors.Is(err, repo.ErrNoChanges) {
			err = nil
			audit = fmt.Sprintf("[pushout-native] record %s: no changes", inst.actor)
		}
		return
	}
	id = PatchID{Hex: hex.EncodeToString(h[:])}
	audit = fmt.Sprintf("[pushout-native] record %s by %s: %s", hex.EncodeToString(h[:8]), author, message)
	return
}

func changesForLineDiff(v repo.ViewI, cells []KVLine) (changes []patch.Change, err error) {
	g := v.Graph()
	var oldIDs []t.NodeID
	var oldContents [][]byte
	for _, n := range v.LinearOrder() {
		if n == t.RootNodeID {
			continue
		}
		oldIDs = append(oldIDs, n)
		oldContents = append(oldContents, g.NodeContent(n))
	}
	newLines := make([][]byte, 0, len(cells))
	for _, c := range cells {
		if c.Conflict != nil {
			// Should be impossible on the clean path; skip defensively.
			continue
		}
		newLines = append(newLines, []byte(formatCellLine(c)))
	}
	diff, derr := patch.LineDiff(oldIDs, oldContents, newLines)
	if derr != nil {
		err = derr
		return
	}
	changes = diff.Changes
	return
}

// changesForResolution turns the user's cell list into graph operations
// while the graggle is conflicted. Paths are classified by the conflict
// detector — a node is "conflicted" when it participates in an order,
// cycle, or orphan conflict; bare path multiplicity is NOT enough (two
// linearly ordered duplicate-key rows are clean and must not be
// collapsed).
//
// Per conflicted path with a chosen (non-Conflict) cell value: keep the
// matching sibling and delete the rest, or — when no sibling matches —
// delete all and add a new node carrying the chosen value, anchored via
// commonAnchors. Per clean single-node path: a value change becomes
// delete+insert anchored via the replaced node's neighbours; an absent
// cell becomes a delete. Multi-node clean paths are left untouched.
func changesForResolution(v repo.ViewI, cells []KVLine) (changes []patch.Change, err error) {
	g := v.Graph()
	cellByPath := make(map[string]KVLine, len(cells))
	for _, c := range cells {
		cellByPath[c.Path] = c
	}

	byPath := make(map[string][]t.NodeID)
	var paths []string
	for n := range g.AllLiveNodes() {
		if n == t.RootNodeID {
			continue
		}
		path, _, ok := splitKVLine(strings.TrimSuffix(string(g.NodeContent(n)), "\n"))
		if !ok {
			continue
		}
		if _, exists := byPath[path]; !exists {
			paths = append(paths, path)
		}
		byPath[path] = append(byPath[path], n)
	}
	sort.Strings(paths)

	// Reject creation attempts before emitting any change.
	for _, c := range cells {
		if c.Conflict != nil {
			continue
		}
		if _, exists := byPath[c.Path]; !exists {
			err = eh.Errorf("cell %q: %w", c.Path, ErrCellCreateWhileConflicted)
			return
		}
	}

	conflicted := make(map[t.NodeID]struct{})
	for _, ci := range v.Conflicts() {
		switch ci.Kind {
		case "order":
			// Nodes[0] is the common parent; the incomparable children
			// are the actual conflict participants.
			for _, n := range ci.Nodes[1:] {
				conflicted[n] = struct{}{}
			}
		case "cycle", "orphan":
			for _, n := range ci.Nodes {
				conflicted[n] = struct{}{}
			}
		}
	}

	nodeValue := func(n t.NodeID) (val string, ok bool) {
		_, val, ok = splitKVLine(strings.TrimSuffix(string(g.NodeContent(n)), "\n"))
		return
	}

	var newNodeIndex uint64
	for _, path := range paths {
		nodes := byPath[path]
		isConflicted := slices.ContainsFunc(nodes, func(n t.NodeID) bool {
			_, ok := conflicted[n]
			return ok
		})

		if !isConflicted {
			if len(nodes) != 1 {
				// Duplicate-key clean path: no per-row identity to edit by.
				continue
			}
			n := nodes[0]
			cell, present := cellByPath[path]
			switch {
			case !present:
				changes = append(changes, patch.Change{Kind: patch.ChangeKindDeleteNode, NodeID: n})
			case cell.Conflict != nil:
				// Stale conflict cell for a clean path; nothing to do.
			default:
				if val, ok := nodeValue(n); ok && val != cell.Value {
					upCtx, downCtx := commonAnchors(g, nodes)
					changes = append(changes,
						patch.Change{Kind: patch.ChangeKindDeleteNode, NodeID: n},
						patch.Change{
							Kind:        patch.ChangeKindNewNode,
							NodeID:      t.NodeID{Patch: t.PlaceholderHash, Index: newNodeIndex},
							Content:     []byte(formatCellLine(cell)),
							UpContext:   upCtx,
							DownContext: downCtx,
						})
					newNodeIndex++
				}
			}
			continue
		}

		cell, ok := cellByPath[path]
		if !ok || cell.Conflict != nil {
			// Partial resolution; this conflict stays unresolved
			// in this patch.
			continue
		}
		matched := false
		for _, n := range nodes {
			val, vok := nodeValue(n)
			if !vok {
				continue
			}
			if val == cell.Value && !matched {
				matched = true
				continue
			}
			changes = append(changes, patch.Change{Kind: patch.ChangeKindDeleteNode, NodeID: n})
		}
		if matched {
			continue
		}
		// The user's chosen value matches no existing side: delete
		// every sibling (already scheduled above) and add a new node
		// carrying the chosen value, anchored between a parent and a
		// downstream that the conflict siblings shared.
		upCtx, downCtx := commonAnchors(g, nodes)
		changes = append(changes, patch.Change{
			Kind:        patch.ChangeKindNewNode,
			NodeID:      t.NodeID{Patch: t.PlaceholderHash, Index: newNodeIndex},
			Content:     []byte(formatCellLine(cell)),
			UpContext:   upCtx,
			DownContext: downCtx,
		})
		newNodeIndex++
	}
	return
}

// commonAnchors picks an upContext + downContext for a new node that
// replaces a set of conflict-sibling live nodes. Conflict siblings in
// the demo's KV-row pattern share their parent and child anchors (all
// were inserted by `LineDiff` between the same two unchanged
// neighbours), so the first sibling's first live parent / child gives
// the right anchors. Returns nil slices if a sibling sits at a graph
// boundary.
func commonAnchors(g t.GraphReaderI, conflictNodes []t.NodeID) (upContext, downContext []t.NodeID) {
	if len(conflictNodes) == 0 {
		return
	}
	sample := conflictNodes[0]
	for p := range g.LiveParents(sample) {
		upContext = []t.NodeID{p}
		break
	}
	for ch := range g.LiveChildren(sample) {
		downContext = []t.NodeID{ch}
		break
	}
	return
}

// Unrecord delegates to the engine: dependent-gated, retention-gated,
// envelope kept for later re-apply (see repo.Repo.Unrecord).
func (inst *PushoutRepo) Unrecord(ctx context.Context, hash t.PatchHash) (audit string, err error) {
	if inst.eng == nil {
		err = eh.Errorf("repo not initialised")
		return
	}
	if err = inst.eng.Unrecord(ctx, hash); err != nil {
		return
	}
	audit = fmt.Sprintf("[pushout-native] unrecord %s", hex.EncodeToString(hash[:8]))
	return
}

// Apply ingests a foreign envelope (idempotently).
func (inst *PushoutRepo) Apply(ctx context.Context, env PatchEnvelope) (audit string, err error) {
	if inst.eng == nil {
		err = eh.Errorf("repo not initialised")
		return
	}
	h, applied, err := inst.eng.ApplyEnvelope(ctx, env.Bytes)
	if err != nil {
		return
	}
	if !applied {
		audit = fmt.Sprintf("[pushout-native] apply %s: already present", PatchID{Hex: hex.EncodeToString(h[:])}.Short())
		return
	}
	audit = fmt.Sprintf("[pushout-native] apply %s from %s", PatchID{Hex: hex.EncodeToString(h[:])}.Short(), env.Producer)
	return
}

// Push ships everything dest lacks, over the in-process carrier.
func (inst *PushoutRepo) Push(ctx context.Context, dest RepoI) (audit string, err error) {
	other, ok := dest.(*PushoutRepo)
	if !ok {
		err = eh.Errorf("pushout-native Push requires pushout-native destination, got %T", dest)
		return
	}
	if inst.eng == nil || other.eng == nil {
		err = eh.Errorf("repo not initialised")
		return
	}
	ep := inproc.New(other.eng)
	stats, err := exchange.Push(ctx, inst.eng, ep, ep)
	if err != nil {
		return
	}
	audit = fmt.Sprintf("[pushout-native] push %s → %s (%d patches)", inst.path, other.path, stats.Applied)
	return
}

// Pull fetches everything src has that this repo lacks. hadConflict
// reports whether the merged graggle lost its linear order.
func (inst *PushoutRepo) Pull(ctx context.Context, src RepoI) (audit string, hadConflict bool, err error) {
	other, ok := src.(*PushoutRepo)
	if !ok {
		err = eh.Errorf("pushout-native Pull requires pushout-native source, got %T", src)
		return
	}
	if inst.eng == nil || other.eng == nil {
		err = eh.Errorf("repo not initialised")
		return
	}
	stats, err := exchange.Pull(ctx, inst.eng, inproc.New(other.eng))
	if err != nil {
		return
	}
	verr := inst.eng.View(ctx, func(v repo.ViewI) error {
		hadConflict = v.LinearOrder() == nil
		return nil
	})
	if verr != nil {
		err = verr
		return
	}
	audit = fmt.Sprintf("[pushout-native] pull %s ← %s (%d patches)", inst.path, other.path, stats.Applied)
	return
}

// ExportLatest returns the most recently recorded envelope as bytes
// for transmission.
func (inst *PushoutRepo) ExportLatest(ctx context.Context) (env PatchEnvelope, audit string, err error) {
	if inst.eng == nil {
		err = eh.Errorf("repo not initialised")
		return
	}
	applied, err := inst.eng.Applied(ctx)
	if err != nil {
		return
	}
	if len(applied) == 0 {
		err = eh.Errorf("no patches recorded yet")
		return
	}
	h := applied[len(applied)-1]
	framed, err := inst.eng.EncodedEnvelope(ctx, h)
	if err != nil {
		return
	}
	info, err := inst.eng.PatchInfo(ctx, h)
	if err != nil {
		return
	}
	env = PatchEnvelope{
		ID:       PatchID{Hex: hex.EncodeToString(h[:])},
		Producer: info.Producer,
		Bytes:    framed,
	}
	audit = fmt.Sprintf("[pushout-native] export-latest %s", env.ID.Short())
	return
}

// formatCellLine renders one [KVLine] to a single line of pijul-style
// text — including the trailing newline. The line is what gets stored
// as a node's content; matching content across patches is what
// [patch.LineDiff] uses to detect "kept" lines.
//
// The value is strconv.Quote'd so that quotes, backslashes, and even
// newlines round-trip exactly through [splitKVLine]; the path is
// validated by [validateCellPaths] to be quote/space/newline-free, so
// the first space is always the separator.
func formatCellLine(c KVLine) (s string) {
	s = c.Path + " " + strconv.Quote(c.Value) + "\n"
	return
}
