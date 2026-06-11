//go:build llm_generated_opus47

package pijul

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/stergiotis/boxer/public/observability/eh"

	"github.com/stergiotis/boxer/public/algebraicarch/pushout/envelope"
	"github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/algo"
	"github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/patch"
	"github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/store"
	t "github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/types"
)

// pushoutBackend is the native realisation of [BackendI]: per-actor
// graggles backed by the vendored pushout package, with on-disk patch
// envelopes used as the peer-to-peer transport. There is no `pijul`
// binary involved.
type pushoutBackend struct{}

var _ BackendI = (*pushoutBackend)(nil)

// NewPushoutBackend builds a native backend with no configuration.
func NewPushoutBackend() (b *pushoutBackend) {
	b = &pushoutBackend{}
	return
}

func (inst *pushoutBackend) Name() (n string) {
	n = "pushout-native"
	return
}

func (inst *pushoutBackend) NewRepo(actor string, path string) (repo RepoI) {
	repo = &PushoutRepo{
		actor:      actor,
		path:       path,
		MetaByHash: make(map[t.PatchHash]envelope.EnvelopeV1),
	}
	return
}

// Clone snapshots src's in-memory state under its lock and materialises
// the destination repo (graggle, applied list, envelope files) entirely
// from that snapshot. Copying the changes directory from disk instead
// would race with a concurrent SetAndRecord: memory could already hold a
// patch whose envelope file had not been copied yet, leaving the clone
// with an applied hash it can never ship. Both repos must come from this
// backend.
func (inst *pushoutBackend) Clone(ctx context.Context, src RepoI, destPath string, destActor string) (dest RepoI, audit string, err error) {
	srcRepo, ok := src.(*PushoutRepo)
	if !ok {
		err = eh.Errorf("pushout-native cannot clone from a %T", src)
		return
	}

	werr := os.MkdirAll(changesDir(destPath), 0755)
	if werr != nil {
		err = eh.Errorf("create dest repo dirs: %w", werr)
		return
	}

	srcRepo.Mu.Lock()
	if srcRepo.Graggle == nil {
		srcRepo.Mu.Unlock()
		err = eh.Errorf("clone source %s is not initialised", srcRepo.path)
		return
	}
	cloned := srcRepo.Graggle.Clone()
	clonedApplied := slices.Clone(srcRepo.appliedHash)
	clonedMeta := maps.Clone(srcRepo.MetaByHash)
	srcRepo.Mu.Unlock()

	if err = writeAppliedList(destPath, clonedApplied); err != nil {
		return
	}
	for _, env := range clonedMeta {
		if err = writeEnvelope(destPath, env); err != nil {
			return
		}
	}

	dest = &PushoutRepo{
		actor:       destActor,
		path:        destPath,
		Graggle:     cloned,
		appliedHash: clonedApplied,
		MetaByHash:  clonedMeta,
		writtenInit: true,
	}
	audit = fmt.Sprintf("[pushout-native] clone %s → %s", srcRepo.path, destPath)
	return
}

// PushoutRepo is one actor's working copy on the native backend. The
// graggle lives in memory; patch envelopes live on disk so peer
// actors can apply them. There is no rendered "tracked file" — the
// demo's record is derived directly from the live subgraph.
type PushoutRepo struct {
	Mu sync.Mutex

	actor       string
	path        string
	Graggle     *store.Graggle
	appliedHash []t.PatchHash
	MetaByHash  map[t.PatchHash]envelope.EnvelopeV1
	writtenInit bool
}

var _ RepoI = (*PushoutRepo)(nil)

func (inst *PushoutRepo) Path() (p string) {
	p = inst.path
	return
}

func (inst *PushoutRepo) Init(ctx context.Context) (audit string, err error) {
	inst.Mu.Lock()
	defer inst.Mu.Unlock()

	merr := os.MkdirAll(changesDir(inst.path), 0755)
	if merr != nil {
		err = eh.Errorf("create repo dirs: %w", merr)
		return
	}
	werr := os.WriteFile(appliedListPath(inst.path), nil, 0644)
	if werr != nil {
		err = eh.Errorf("touch applied.txt: %w", werr)
		return
	}
	inst.Graggle = store.New()
	inst.appliedHash = nil
	inst.MetaByHash = make(map[t.PatchHash]envelope.EnvelopeV1)
	inst.writtenInit = true
	audit = fmt.Sprintf("[pushout-native] init %s", inst.path)
	return
}

// State derives the demo's KVLine slice directly from the live
// subgraph. For a linearly ordered graggle, walks LinearOrder. For a
// conflicted graggle, groups live nodes by their cell path: if a path
// has one live node, that's a clean cell; if it has two or more, the
// first two values are emitted as a Conflict cell.
//
// This bypasses the text round-trip used by [pijulTextRepo.Read] —
// provenance comes straight off NodeID.Patch, and the demo never has
// to re-parse pijul-style markers.
func (inst *PushoutRepo) State(ctx context.Context) (cells []KVLine, log []PatchMetadata, audit string, err error) {
	inst.Mu.Lock()
	defer inst.Mu.Unlock()
	if inst.Graggle == nil {
		// Pre-init state.
		audit = fmt.Sprintf("[pushout-native] state (uninit) %s", inst.path)
		return
	}
	inst.Graggle.ResolvePseudoEdges()

	order := algo.LinearOrder(inst.Graggle)
	if order != nil {
		cells = inst.cellsFromLinearOrder(order)
	} else {
		cells = inst.cellsFromConflictedGraggle()
	}

	log = make([]PatchMetadata, 0, len(inst.appliedHash))
	for _, h := range inst.appliedHash {
		log = append(log, inst.metaToPatchMetadata(h))
	}
	audit = fmt.Sprintf("[pushout-native] state %s (%d cells, %d patches)", inst.path, len(cells), len(log))
	return
}

func (inst *PushoutRepo) cellsFromLinearOrder(order []t.NodeID) (cells []KVLine) {
	for _, n := range order {
		if n == t.RootNodeID {
			continue
		}
		path, val, ok := splitKVLine(strings.TrimSuffix(string(inst.Graggle.NodeContent(n)), "\n"))
		if !ok {
			continue
		}
		cell := KVLine{Path: path, Value: val}
		meta := inst.metaToPatchMetadata(n.Patch)
		if !meta.ID.Empty() {
			cell.Credit = &meta
		}
		cells = append(cells, cell)
	}
	return
}

// cellsFromConflictedGraggle handles the conflict case by grouping live
// nodes by cell path. The demo's typical conflict — two actors edit the
// same path with different values — produces exactly two live nodes for
// that path; both contents become the Alice/Bob sides.
//
// Order is alphabetical by path because LinearOrder is unavailable; the
// linear case (the common one) preserves the user's original cell
// ordering.
func (inst *PushoutRepo) cellsFromConflictedGraggle() (cells []KVLine) {
	byPath := make(map[string][]t.NodeID)
	var paths []string
	for n := range inst.Graggle.AllLiveNodes() {
		if n == t.RootNodeID {
			continue
		}
		p, _, ok := splitKVLine(strings.TrimSuffix(string(inst.Graggle.NodeContent(n)), "\n"))
		if !ok {
			continue
		}
		if _, exists := byPath[p]; !exists {
			paths = append(paths, p)
		}
		byPath[p] = append(byPath[p], n)
	}
	sort.Strings(paths)
	for _, p := range paths {
		nodes := byPath[p]
		switch len(nodes) {
		case 1:
			n := nodes[0]
			_, v, _ := splitKVLine(strings.TrimSuffix(string(inst.Graggle.NodeContent(n)), "\n"))
			cell := KVLine{Path: p, Value: v}
			meta := inst.metaToPatchMetadata(n.Patch)
			if !meta.ID.Empty() {
				cell.Credit = &meta
			}
			cells = append(cells, cell)
		default:
			values := make([]string, 0, len(nodes))
			for _, n := range nodes {
				_, v, ok := splitKVLine(strings.TrimSuffix(string(inst.Graggle.NodeContent(n)), "\n"))
				if !ok {
					continue
				}
				values = append(values, v)
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

func (inst *PushoutRepo) metaToPatchMetadata(h t.PatchHash) (m PatchMetadata) {
	env, ok := inst.MetaByHash[h]
	if !ok {
		return
	}
	deps := make([]PatchID, 0, len(env.Patch.Dependencies))
	for _, d := range env.Patch.Dependencies {
		deps = append(deps, PatchID{Hex: hex.EncodeToString(d[:])})
	}
	m = PatchMetadata{
		ID:           PatchID{Hex: hex.EncodeToString(h[:])},
		Authors:      []string{env.Patch.Author},
		Timestamp:    env.Timestamp,
		Message:      env.Patch.Description,
		Dependencies: deps,
	}
	return
}

// SetAndRecord computes the diff from the current live subgraph to the
// requested cell list, builds a patch via [patch.LineDiff] +
// [patch.NewPatch], applies it, and persists the resulting envelope.
//
// In conflict mode the patch carries both resolutions (keep one side,
// or an arbitrary replacement value, per conflicted path) and the
// user's edits/deletions of clean single-node paths — saving an
// unrelated edit while some other cell is conflicted must not silently
// drop it. Creating a brand-new cell during a conflict is rejected:
// without a linear order there is no reliable anchor for a new row.
//
// The patch applies to a clone of the graggle; the working state and the
// on-disk log advance only after every step (graph apply, envelope
// write, applied-log append) has succeeded.
func (inst *PushoutRepo) SetAndRecord(ctx context.Context, cells []KVLine, author string, message string) (id PatchID, audit string, err error) {
	inst.Mu.Lock()
	defer inst.Mu.Unlock()
	if inst.Graggle == nil {
		err = eh.Errorf("repo not initialised")
		return
	}
	if err = validateCellPaths(cells); err != nil {
		return
	}
	inst.Graggle.ResolvePseudoEdges()

	var changes []patch.Change
	if algo.HasConflicts(inst.Graggle) {
		changes, err = inst.changesForResolution(cells)
	} else {
		changes = inst.changesForLineDiff(cells)
	}
	if err != nil {
		return
	}
	if len(changes) == 0 {
		audit = fmt.Sprintf("[pushout-native] record %s: no changes", inst.actor)
		return
	}

	deps := patch.ComputeDependencies(changes)
	p := patch.NewPatch(author, message, deps, changes)
	next := inst.Graggle.Clone()
	if aerr := p.Apply(next); aerr != nil {
		err = eh.Errorf("apply new patch: %w", aerr)
		return
	}
	env := envelope.EnvelopeV1{Patch: p, Producer: inst.actor, Timestamp: time.Now()}
	if err = writeEnvelope(inst.path, env); err != nil {
		return
	}
	if err = appendApplied(inst.path, p.Hash); err != nil {
		return
	}
	if _, exists := inst.MetaByHash[p.Hash]; !exists {
		// First-writer-wins, matching writeEnvelope: identical changes
		// and deps recorded by another author share the hash; keep the
		// provenance that landed first.
		inst.MetaByHash[p.Hash] = env
	}
	inst.appliedHash = append(inst.appliedHash, p.Hash)
	inst.Graggle = next

	short := hex.EncodeToString(p.Hash[:8])
	id = PatchID{Hex: hex.EncodeToString(p.Hash[:])}
	audit = fmt.Sprintf("[pushout-native] record %s by %s: %s", short, author, message)
	return
}

func (inst *PushoutRepo) changesForLineDiff(cells []KVLine) (changes []patch.Change) {
	order := algo.LinearOrder(inst.Graggle)
	var oldIDs []t.NodeID
	var oldContents [][]byte
	for _, n := range order {
		if n == t.RootNodeID {
			continue
		}
		oldIDs = append(oldIDs, n)
		oldContents = append(oldContents, inst.Graggle.NodeContent(n))
	}
	newLines := make([][]byte, 0, len(cells))
	for _, c := range cells {
		if c.Conflict != nil {
			// Should be impossible on the clean path; skip defensively.
			continue
		}
		newLines = append(newLines, []byte(formatCellLine(c)))
	}
	diff := patch.LineDiff(oldIDs, oldContents, newLines)
	changes = diff.Changes
	return
}

// changesForResolution turns the user's cell list into graph operations
// while the graggle is conflicted. Paths are classified by the conflict
// detector — a node is "conflicted" when it participates in an order or
// cycle conflict; bare path multiplicity is NOT enough (two linearly
// ordered duplicate-key rows are clean and must not be collapsed).
//
// Per conflicted path with a chosen (non-Conflict) cell value:
//   - if one sibling's content matches cell.Value, keep it and delete
//     every other live sibling
//   - otherwise delete every sibling and add a new node carrying
//     cell.Value, anchored between a parent and a downstream the
//     conflict siblings shared
//
// Per clean path: a changed value becomes delete+insert anchored via
// the replaced node's own live neighbours; a path absent from the cell
// list is deleted. Multi-node clean paths (duplicate keys) are left
// untouched — there is no per-row identity in the KV cell model to
// know which row the user meant. A cell whose path exists nowhere in
// the graggle is rejected: creating new rows needs an anchor, and a
// conflicted graggle has no linear order to derive one from.
func (inst *PushoutRepo) changesForResolution(cells []KVLine) (changes []patch.Change, err error) {
	cellByPath := make(map[string]KVLine, len(cells))
	for _, c := range cells {
		cellByPath[c.Path] = c
	}

	byPath := make(map[string][]t.NodeID)
	var paths []string
	for n := range inst.Graggle.AllLiveNodes() {
		if n == t.RootNodeID {
			continue
		}
		path, _, ok := splitKVLine(strings.TrimSuffix(string(inst.Graggle.NodeContent(n)), "\n"))
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
			err = eh.Errorf("cannot create cell %q while the graggle is conflicted — resolve conflicts first", c.Path)
			return
		}
	}

	conflicted := make(map[t.NodeID]struct{})
	for _, ci := range algo.DetectConflicts(inst.Graggle) {
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

	nodeValue := func(n t.NodeID) (v string, ok bool) {
		_, v, ok = splitKVLine(strings.TrimSuffix(string(inst.Graggle.NodeContent(n)), "\n"))
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
				if v, ok := nodeValue(n); ok && v != cell.Value {
					upCtx, downCtx := commonAnchors(inst.Graggle, nodes)
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
			v, vok := nodeValue(n)
			if !vok {
				continue
			}
			if v == cell.Value && !matched {
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
		upCtx, downCtx := commonAnchors(inst.Graggle, nodes)
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
// boundary; the caller's [patch.NewPatch] tolerates empty contexts.
func commonAnchors(g *store.Graggle, conflictNodes []t.NodeID) (upContext, downContext []t.NodeID) {
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

// Unrecord undoes a previously-recorded patch on this repo: the
// patch's inverse is applied to the graggle (cell-level effects
// reverted), the hash is removed from the in-memory and persisted
// applied list. The patch envelope itself is *kept* in MetaByHash so
// the receiver can reapply it later — e.g. via Pull from a peer that
// still has it — making Unrecord round-trippable.
//
// Errors:
//   - patch not currently applied (no-op on already-removed hash)
//   - patch metadata missing from MetaByHash (corrupted state)
//   - an applied patch lists this hash among its Dependencies: the
//     caller must Unrecord dependents first. This is checked here, on
//     the declared dependency graph, BEFORE touching the graggle —
//     [patch.Patch.Unapply]'s structural pre-flights (foreign edges,
//     foreign tombstones) remain as defense in depth.
//
// Unrecord does not affect the patch graph as seen by Pull/Push: the
// hash is gone from appliedHash, so missingOn() will treat the patch
// as un-received again. A subsequent Pull from a peer that has it
// will reapply it cleanly.
func (inst *PushoutRepo) Unrecord(ctx context.Context, hash t.PatchHash) (audit string, err error) {
	inst.Mu.Lock()
	defer inst.Mu.Unlock()
	if inst.Graggle == nil {
		err = eh.Errorf("repo not initialised")
		return
	}

	idx := slices.Index(inst.appliedHash, hash)
	if idx < 0 {
		err = eh.Errorf("patch %s not currently applied", hash)
		return
	}
	env, ok := inst.MetaByHash[hash]
	if !ok || env.Patch == nil {
		err = eh.Errorf("patch %s metadata missing from MetaByHash", hash)
		return
	}
	for _, h := range inst.appliedHash {
		if h == hash {
			continue
		}
		dependent, ok := inst.MetaByHash[h]
		if !ok || dependent.Patch == nil {
			continue
		}
		if slices.Contains(dependent.Patch.Dependencies, hash) {
			err = eh.Errorf("patch %s is a dependency of applied patch %s — unrecord dependents first", hash, h)
			return
		}
	}

	// Unapply against a clone; swap in only after the on-disk applied
	// list has been rewritten, so a failure at any step leaves both the
	// working state and the log untouched.
	next := inst.Graggle.Clone()
	if uerr := env.Patch.Unapply(next); uerr != nil {
		err = eh.Errorf("unapply patch %s: %w", hash, uerr)
		return
	}
	newApplied := slices.Delete(slices.Clone(inst.appliedHash), idx, idx+1)
	if werr := writeAppliedList(inst.path, newApplied); werr != nil {
		err = eh.Errorf("rewrite applied list: %w", werr)
		return
	}
	inst.appliedHash = newApplied
	inst.Graggle = next

	short := hex.EncodeToString(hash[:8])
	audit = fmt.Sprintf("[pushout-native] unrecord %s", short)
	return
}

// Apply ingests a foreign envelope. The patch must depend only on
// patches that are currently APPLIED — merely having seen an envelope
// (MetaByHash) is not enough: after Unrecord a dependency is known but
// its nodes are gone from the graggle, and gating on the wrong set let a
// dependent walk into a half-applicable state. Pushout has no
// on-the-fly dependency fetch.
//
// The patch applies to a clone of the graggle; working state and the
// on-disk log advance only after every step succeeded, so a failed
// apply leaves no phantom nodes behind.
func (inst *PushoutRepo) Apply(ctx context.Context, env PatchEnvelope) (audit string, err error) {
	inst.Mu.Lock()
	defer inst.Mu.Unlock()
	if inst.Graggle == nil {
		err = eh.Errorf("repo not initialised")
		return
	}
	decoded, derr := envelope.Decode(env.Bytes)
	if derr != nil {
		err = eh.Errorf("decode envelope: %w", derr)
		return
	}
	if slices.Contains(inst.appliedHash, decoded.Patch.Hash) {
		audit = fmt.Sprintf("[pushout-native] apply %s: already present", PatchID{Hex: hex.EncodeToString(decoded.Patch.Hash[:])}.Short())
		return
	}
	if cached, exists := inst.MetaByHash[decoded.Patch.Hash]; exists {
		// Hash is cached in MetaByHash but absent from appliedHash:
		// this is the post-[Unrecord] re-apply path. Reuse the
		// cached envelope (identical content, by hash equality) and
		// fall through to the apply/persist sequence so the patch
		// re-enters the applied set.
		decoded = cached
	}
	for _, dep := range decoded.Patch.Dependencies {
		if !slices.Contains(inst.appliedHash, dep) {
			err = eh.Errorf("missing dependency %s — apply prerequisite patches first", PatchID{Hex: hex.EncodeToString(dep[:])}.Short())
			return
		}
	}
	next := inst.Graggle.Clone()
	if aerr := decoded.Patch.Apply(next); aerr != nil {
		err = eh.Errorf("apply patch %s: %w", decoded.Patch.Hash, aerr)
		return
	}
	if err = writeEnvelope(inst.path, decoded); err != nil {
		return
	}
	if err = appendApplied(inst.path, decoded.Patch.Hash); err != nil {
		return
	}
	if _, exists := inst.MetaByHash[decoded.Patch.Hash]; !exists {
		inst.MetaByHash[decoded.Patch.Hash] = decoded
	}
	inst.appliedHash = append(inst.appliedHash, decoded.Patch.Hash)
	inst.Graggle = next
	audit = fmt.Sprintf("[pushout-native] apply %s by %s", PatchID{Hex: hex.EncodeToString(decoded.Patch.Hash[:])}.Short(), decoded.Patch.Author)
	return
}

// Push ships every patch this repo has but dest doesn't, in apply
// order. Apply order respects topo order on dependencies.
func (inst *PushoutRepo) Push(ctx context.Context, dest RepoI) (audit string, err error) {
	other, ok := dest.(*PushoutRepo)
	if !ok {
		err = eh.Errorf("pushout-native Push requires pushout-native destination, got %T", dest)
		return
	}
	missing, err := inst.missingOn(other)
	if err != nil {
		return
	}
	var lines []string
	for _, h := range missing {
		env, rerr := readEnvelope(inst.path, h)
		if rerr != nil {
			err = rerr
			audit = strings.Join(lines, "\n")
			return
		}
		bytes, eerr := envelope.Encode(env)
		if eerr != nil {
			err = eerr
			audit = strings.Join(lines, "\n")
			return
		}
		applyAudit, aerr := other.Apply(ctx, PatchEnvelope{
			ID:       PatchID{Hex: hex.EncodeToString(h[:])},
			Producer: env.Producer,
			Bytes:    bytes,
		})
		if applyAudit != "" {
			lines = append(lines, applyAudit)
		}
		if aerr != nil {
			// Keep the audit of what DID land before the failure — a
			// partial push is real state the operator needs to see.
			err = aerr
			audit = strings.Join(lines, "\n")
			return
		}
	}
	lines = append(lines, fmt.Sprintf("[pushout-native] push %s → %s (%d patches)", inst.path, other.path, len(missing)))
	audit = strings.Join(lines, "\n")
	return
}

// Pull is the symmetric of Push: walks src's appliedHash for patches
// missing here. hadConflict is true iff the resulting graggle has any
// structural conflict.
func (inst *PushoutRepo) Pull(ctx context.Context, src RepoI) (audit string, hadConflict bool, err error) {
	other, ok := src.(*PushoutRepo)
	if !ok {
		err = eh.Errorf("pushout-native Pull requires pushout-native source, got %T", src)
		return
	}
	missing, merr := other.missingOn(inst)
	if merr != nil {
		err = merr
		return
	}
	var lines []string
	for _, h := range missing {
		env, rerr := readEnvelope(other.path, h)
		if rerr != nil {
			err = rerr
			audit = strings.Join(lines, "\n")
			return
		}
		bytes, eerr := envelope.Encode(env)
		if eerr != nil {
			err = eerr
			audit = strings.Join(lines, "\n")
			return
		}
		applyAudit, aerr := inst.Apply(ctx, PatchEnvelope{
			ID:       PatchID{Hex: hex.EncodeToString(h[:])},
			Producer: env.Producer,
			Bytes:    bytes,
		})
		if applyAudit != "" {
			lines = append(lines, applyAudit)
		}
		if aerr != nil {
			err = aerr
			audit = strings.Join(lines, "\n")
			return
		}
	}
	inst.Mu.Lock()
	if inst.Graggle != nil {
		inst.Graggle.ResolvePseudoEdges()
		hadConflict = algo.HasConflicts(inst.Graggle)
	}
	inst.Mu.Unlock()
	lines = append(lines, fmt.Sprintf("[pushout-native] pull %s ← %s (%d patches)", inst.path, other.path, len(missing)))
	audit = strings.Join(lines, "\n")
	return
}

// missingOn returns the hashes inst has but other doesn't, preserving
// inst's apply order so dependencies precede dependents.
func (inst *PushoutRepo) missingOn(other *PushoutRepo) (hashes []t.PatchHash, err error) {
	inst.Mu.Lock()
	// Clone, not alias: the slice is iterated after the lock is released,
	// and a concurrent mutation of the backing array would tear it.
	have := slices.Clone(inst.appliedHash)
	inst.Mu.Unlock()
	other.Mu.Lock()
	otherHas := make(map[t.PatchHash]struct{}, len(other.appliedHash))
	for _, h := range other.appliedHash {
		otherHas[h] = struct{}{}
	}
	other.Mu.Unlock()
	for _, h := range have {
		if _, ok := otherHas[h]; !ok {
			hashes = append(hashes, h)
		}
	}
	return
}

// ExportLatest returns the most recently recorded envelope as bytes
// for transmission.
func (inst *PushoutRepo) ExportLatest(ctx context.Context) (env PatchEnvelope, audit string, err error) {
	inst.Mu.Lock()
	defer inst.Mu.Unlock()
	if len(inst.appliedHash) == 0 {
		err = eh.Errorf("no patches recorded yet")
		return
	}
	h := inst.appliedHash[len(inst.appliedHash)-1]
	envV1, rerr := readEnvelope(inst.path, h)
	if rerr != nil {
		err = rerr
		return
	}
	bytes, eerr := envelope.Encode(envV1)
	if eerr != nil {
		err = eerr
		return
	}
	env = PatchEnvelope{
		ID:       PatchID{Hex: hex.EncodeToString(h[:])},
		Producer: envV1.Producer,
		Bytes:    bytes,
	}
	audit = fmt.Sprintf("[pushout-native] export-latest %s", env.ID.Short())
	return
}

// ---------------------------------------------------------------------------
// On-disk layout helpers
// ---------------------------------------------------------------------------

func appliedListPath(repoPath string) (p string) {
	p = filepath.Join(repoPath, ".pushout", "applied.txt")
	return
}

func changesDir(repoPath string) (p string) {
	p = filepath.Join(repoPath, ".pushout", "changes")
	return
}

// envelopeFilePath names envelope files by the FULL hex hash. A truncated
// name would let a prefix collision silently overwrite one envelope with
// another — readEnvelope would then hand back the wrong patch with no
// error naming the cause.
func envelopeFilePath(repoPath string, h t.PatchHash) (p string) {
	p = filepath.Join(changesDir(repoPath), hex.EncodeToString(h[:])+".json")
	return
}

// writeEnvelope persists an envelope, first-writer-wins: the file is
// content-addressed by the patch hash (which covers changes plus
// dependencies), so an existing file already carries the same patch.
// Envelope-level provenance (Producer, Timestamp, author) may differ
// between writers; whichever landed locally first is kept.
func writeEnvelope(repoPath string, env envelope.EnvelopeV1) (err error) {
	path := envelopeFilePath(repoPath, env.Patch.Hash)
	if _, serr := os.Stat(path); serr == nil {
		return
	}
	bytes, eerr := envelope.Encode(env)
	if eerr != nil {
		err = eerr
		return
	}
	werr := os.WriteFile(path, bytes, 0644)
	if werr != nil {
		err = eh.Errorf("write envelope %s: %w", env.Patch.Hash, werr)
		return
	}
	return
}

func readEnvelope(repoPath string, h t.PatchHash) (env envelope.EnvelopeV1, err error) {
	bytes, rerr := os.ReadFile(envelopeFilePath(repoPath, h))
	if rerr != nil {
		err = eh.Errorf("read envelope %s: %w", h, rerr)
		return
	}
	env, err = envelope.Decode(bytes)
	if err != nil {
		return
	}
	if env.Patch.Hash != h {
		err = eh.Errorf("envelope file for %s contains patch %s — store corrupted", h, env.Patch.Hash)
	}
	return
}

func appendApplied(repoPath string, h t.PatchHash) (err error) {
	f, oerr := os.OpenFile(appliedListPath(repoPath), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0644)
	if oerr != nil {
		err = eh.Errorf("open applied.txt: %w", oerr)
		return
	}
	_, werr := fmt.Fprintln(f, hex.EncodeToString(h[:]))
	cerr := f.Close()
	if werr != nil {
		err = eh.Errorf("write applied.txt: %w", werr)
		return
	}
	if cerr != nil {
		err = eh.Errorf("close applied.txt: %w", cerr)
	}
	return
}

// writeAppliedList replaces the persisted applied-list file with the
// given hashes, one per line. Used by [PushoutRepo.Unrecord] where the
// in-memory list shrinks and the on-disk file must shrink with it;
// [appendApplied] is the wrong shape for that path because it's
// append-only.
func writeAppliedList(repoPath string, hashes []t.PatchHash) (err error) {
	var buf bytes.Buffer
	for _, h := range hashes {
		fmt.Fprintln(&buf, hex.EncodeToString(h[:]))
	}
	if werr := os.WriteFile(appliedListPath(repoPath), buf.Bytes(), 0644); werr != nil {
		err = eh.Errorf("write applied.txt: %w", werr)
	}
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
