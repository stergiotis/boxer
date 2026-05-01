//go:build llm_generated_opus47

package pijul

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/stergiotis/boxer/public/observability/eh"

	"github.com/stergiotis/pebble2impl/src/go/public/algebraicarch/pushout/envelope"
	"github.com/stergiotis/pebble2impl/src/go/public/algebraicarch/pushout/graggle/algo"
	"github.com/stergiotis/pebble2impl/src/go/public/algebraicarch/pushout/graggle/patch"
	"github.com/stergiotis/pebble2impl/src/go/public/algebraicarch/pushout/graggle/store"
	t "github.com/stergiotis/pebble2impl/src/go/public/algebraicarch/pushout/graggle/types"
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
	repo = &pushoutRepo{
		actor:      actor,
		path:       path,
		metaByHash: make(map[t.PatchHash]envelope.EnvelopeV1),
	}
	return
}

// Clone copies src's on-disk envelopes to destPath/.pushout and produces
// a destination repo with a deep-cloned in-memory graggle. Both repos
// must come from this backend.
func (inst *pushoutBackend) Clone(ctx context.Context, src RepoI, destPath string, destActor string) (dest RepoI, audit string, err error) {
	srcRepo, ok := src.(*pushoutRepo)
	if !ok {
		err = eh.Errorf("pushout-native cannot clone from a %T", src)
		return
	}

	werr := os.MkdirAll(filepath.Join(destPath, ".pushout", "changes"), 0755)
	if werr != nil {
		err = eh.Errorf("create dest repo dirs: %w", werr)
		return
	}

	// Copy applied.txt + every envelope file.
	srcApplied, rerr := os.ReadFile(appliedListPath(srcRepo.path))
	if rerr != nil && !os.IsNotExist(rerr) {
		err = eh.Errorf("read src applied.txt: %w", rerr)
		return
	}
	werr = os.WriteFile(appliedListPath(destPath), srcApplied, 0644)
	if werr != nil {
		err = eh.Errorf("write dest applied.txt: %w", werr)
		return
	}
	srcChangesDir := changesDir(srcRepo.path)
	destChangesDir := changesDir(destPath)
	entries, derr := os.ReadDir(srcChangesDir)
	if derr != nil && !os.IsNotExist(derr) {
		err = eh.Errorf("read src changes dir: %w", derr)
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, rerr := os.ReadFile(filepath.Join(srcChangesDir, e.Name()))
		if rerr != nil {
			err = eh.Errorf("read envelope %s: %w", e.Name(), rerr)
			return
		}
		werr = os.WriteFile(filepath.Join(destChangesDir, e.Name()), data, 0644)
		if werr != nil {
			err = eh.Errorf("write envelope %s: %w", e.Name(), werr)
			return
		}
	}

	srcRepo.mu.Lock()
	cloned := srcRepo.g.Clone()
	clonedApplied := append([]t.PatchHash(nil), srcRepo.appliedHash...)
	clonedMeta := make(map[t.PatchHash]envelope.EnvelopeV1, len(srcRepo.metaByHash))
	for k, v := range srcRepo.metaByHash {
		clonedMeta[k] = v
	}
	srcRepo.mu.Unlock()

	dest = &pushoutRepo{
		actor:        destActor,
		path:         destPath,
		g:            cloned,
		appliedHash:  clonedApplied,
		metaByHash:   clonedMeta,
		writtenInit:  true,
	}
	audit = fmt.Sprintf("[pushout-native] clone %s → %s", srcRepo.path, destPath)
	return
}

// pushoutRepo is one actor's working copy on the native backend. The
// graggle lives in memory; patch envelopes live on disk so peer
// actors can apply them. There is no rendered "tracked file" — the
// demo's record is derived directly from the live subgraph.
type pushoutRepo struct {
	mu sync.Mutex

	actor       string
	path        string
	g           *store.Graggle
	appliedHash []t.PatchHash
	metaByHash  map[t.PatchHash]envelope.EnvelopeV1
	writtenInit bool
}

var _ RepoI = (*pushoutRepo)(nil)

func (inst *pushoutRepo) Path() (p string) {
	p = inst.path
	return
}

func (inst *pushoutRepo) Init(ctx context.Context) (audit string, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()

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
	inst.g = store.New()
	inst.appliedHash = nil
	inst.metaByHash = make(map[t.PatchHash]envelope.EnvelopeV1)
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
func (inst *pushoutRepo) State(ctx context.Context) (cells []KVLine, log []PatchMetadata, audit string, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.g == nil {
		// Pre-init state.
		audit = fmt.Sprintf("[pushout-native] state (uninit) %s", inst.path)
		return
	}
	inst.g.ResolvePseudoEdges()

	order := algo.LinearOrder(inst.g)
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

func (inst *pushoutRepo) cellsFromLinearOrder(order []t.NodeID) (cells []KVLine) {
	for _, n := range order {
		if n == t.RootNodeID {
			continue
		}
		path, val, ok := splitKVLine(strings.TrimSuffix(string(inst.g.NodeContent(n)), "\n"))
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
func (inst *pushoutRepo) cellsFromConflictedGraggle() (cells []KVLine) {
	byPath := make(map[string][]t.NodeID)
	var paths []string
	for n := range inst.g.AllLiveNodes() {
		if n == t.RootNodeID {
			continue
		}
		p, _, ok := splitKVLine(strings.TrimSuffix(string(inst.g.NodeContent(n)), "\n"))
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
			_, v, _ := splitKVLine(strings.TrimSuffix(string(inst.g.NodeContent(n)), "\n"))
			cell := KVLine{Path: p, Value: v}
			meta := inst.metaToPatchMetadata(n.Patch)
			if !meta.ID.Empty() {
				cell.Credit = &meta
			}
			cells = append(cells, cell)
		default:
			_, va, _ := splitKVLine(strings.TrimSuffix(string(inst.g.NodeContent(nodes[0])), "\n"))
			_, vb, _ := splitKVLine(strings.TrimSuffix(string(inst.g.NodeContent(nodes[1])), "\n"))
			cells = append(cells, KVLine{
				Path:     p,
				Conflict: &ConflictData{AliceValue: va, BobValue: vb},
			})
		}
	}
	return
}

func (inst *pushoutRepo) metaToPatchMetadata(h t.PatchHash) (m PatchMetadata) {
	env, ok := inst.metaByHash[h]
	if !ok {
		return
	}
	m = PatchMetadata{
		ID:        PatchID{Hex: hex.EncodeToString(h[:])},
		Authors:   []string{env.Patch.Author},
		Timestamp: env.Timestamp,
		Message:   env.Patch.Description,
	}
	return
}

// SetAndRecord computes the diff from the current live subgraph to the
// requested cell list, builds a patch via [patch.LineDiff] +
// [patch.NewPatch], applies it, and persists the resulting envelope.
//
// Conflict resolution is supported only when the user picks one of the
// two existing sides ("Keep Alice" / "Keep Bob"). An arbitrary new
// value entered into a conflicted row is rejected; the user must
// resolve via the side-buttons. Multi-row conflicts are resolved one
// click at a time, each click producing a delete-the-rejected-side
// patch.
func (inst *pushoutRepo) SetAndRecord(ctx context.Context, cells []KVLine, author string, message string) (id PatchID, audit string, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.g == nil {
		err = eh.Errorf("repo not initialised")
		return
	}
	inst.g.ResolvePseudoEdges()

	var changes []patch.Change
	if algo.HasConflicts(inst.g) {
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
	aerr := tolerantApply(p, inst.g)
	if aerr != nil {
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
	inst.metaByHash[p.Hash] = env
	inst.appliedHash = append(inst.appliedHash, p.Hash)

	short := hex.EncodeToString(p.Hash[:8])
	id = PatchID{Hex: hex.EncodeToString(p.Hash[:])}
	audit = fmt.Sprintf("[pushout-native] record %s by %s: %s", short, author, message)
	return
}

func (inst *pushoutRepo) changesForLineDiff(cells []KVLine) (changes []patch.Change) {
	order := algo.LinearOrder(inst.g)
	var oldIDs []t.NodeID
	var oldContents [][]byte
	for _, n := range order {
		if n == t.RootNodeID {
			continue
		}
		oldIDs = append(oldIDs, n)
		oldContents = append(oldContents, inst.g.NodeContent(n))
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

func (inst *pushoutRepo) changesForResolution(cells []KVLine) (changes []patch.Change, err error) {
	cellByPath := make(map[string]KVLine, len(cells))
	for _, c := range cells {
		cellByPath[c.Path] = c
	}
	for _, conf := range algo.DetectConflicts(inst.g) {
		if conf.Kind != "order" || len(conf.Nodes) != 3 {
			continue
		}
		sideA, sideB := conf.Nodes[1], conf.Nodes[2]
		pathA, valA, okA := splitKVLine(strings.TrimSuffix(string(inst.g.NodeContent(sideA)), "\n"))
		_, valB, okB := splitKVLine(strings.TrimSuffix(string(inst.g.NodeContent(sideB)), "\n"))
		if !okA || !okB {
			continue
		}
		cell, ok := cellByPath[pathA]
		if !ok || cell.Conflict != nil {
			// Partial resolution; this conflict stays unresolved
			// in this patch.
			continue
		}
		switch cell.Value {
		case valA:
			changes = append(changes, patch.Change{Kind: patch.ChangeDeleteNode, NodeID: sideB})
		case valB:
			changes = append(changes, patch.Change{Kind: patch.ChangeDeleteNode, NodeID: sideA})
		default:
			err = eh.Errorf("pushout-native: arbitrary conflict resolution (value %q matches neither side) is not supported; use Keep-A or Keep-B", cell.Value)
			return
		}
	}
	return
}

// Apply ingests a foreign envelope. The patch must depend only on
// patches we have already applied; otherwise we reject — pushout has
// no on-the-fly dependency fetch.
func (inst *pushoutRepo) Apply(ctx context.Context, env PatchEnvelope) (audit string, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.g == nil {
		err = eh.Errorf("repo not initialised")
		return
	}
	decoded, derr := envelope.Decode(env.Bytes)
	if derr != nil {
		err = eh.Errorf("decode envelope: %w", derr)
		return
	}
	if _, exists := inst.metaByHash[decoded.Patch.Hash]; exists {
		audit = fmt.Sprintf("[pushout-native] apply %s: already present", PatchID{Hex: hex.EncodeToString(decoded.Patch.Hash[:])}.Short())
		return
	}
	for _, dep := range decoded.Patch.Dependencies {
		if _, ok := inst.metaByHash[dep]; !ok {
			err = eh.Errorf("missing dependency %s — apply prerequisite patches first", PatchID{Hex: hex.EncodeToString(dep[:])}.Short())
			return
		}
	}
	if aerr := tolerantApply(decoded.Patch, inst.g); aerr != nil {
		err = eh.Errorf("apply patch %s: %w", decoded.Patch.Hash, aerr)
		return
	}
	if err = writeEnvelope(inst.path, decoded); err != nil {
		return
	}
	if err = appendApplied(inst.path, decoded.Patch.Hash); err != nil {
		return
	}
	inst.metaByHash[decoded.Patch.Hash] = decoded
	inst.appliedHash = append(inst.appliedHash, decoded.Patch.Hash)
	audit = fmt.Sprintf("[pushout-native] apply %s by %s", PatchID{Hex: hex.EncodeToString(decoded.Patch.Hash[:])}.Short(), decoded.Patch.Author)
	return
}

// Push ships every patch this repo has but dest doesn't, in apply
// order. Apply order respects topo order on dependencies.
func (inst *pushoutRepo) Push(ctx context.Context, dest RepoI) (audit string, err error) {
	other, ok := dest.(*pushoutRepo)
	if !ok {
		err = eh.Errorf("pushout-native Push requires pushout-native destination, got %T", dest)
		return
	}
	missing, err := inst.missingOn(other)
	if err != nil {
		return
	}
	for _, h := range missing {
		env, rerr := readEnvelope(inst.path, h)
		if rerr != nil {
			err = rerr
			return
		}
		bytes, eerr := envelope.Encode(env)
		if eerr != nil {
			err = eerr
			return
		}
		_, aerr := other.Apply(ctx, PatchEnvelope{
			ID:       PatchID{Hex: hex.EncodeToString(h[:])},
			Producer: env.Producer,
			Bytes:    bytes,
		})
		if aerr != nil {
			err = aerr
			return
		}
	}
	audit = fmt.Sprintf("[pushout-native] push %s → %s (%d patches)", inst.path, other.path, len(missing))
	return
}

// Pull is the symmetric of Push: walks src's appliedHash for patches
// missing here. hadConflict is true iff the resulting graggle has any
// structural conflict.
func (inst *pushoutRepo) Pull(ctx context.Context, src RepoI) (audit string, hadConflict bool, err error) {
	other, ok := src.(*pushoutRepo)
	if !ok {
		err = eh.Errorf("pushout-native Pull requires pushout-native source, got %T", src)
		return
	}
	missing, merr := other.missingOn(inst)
	if merr != nil {
		err = merr
		return
	}
	for _, h := range missing {
		env, rerr := readEnvelope(other.path, h)
		if rerr != nil {
			err = rerr
			return
		}
		bytes, eerr := envelope.Encode(env)
		if eerr != nil {
			err = eerr
			return
		}
		_, aerr := inst.Apply(ctx, PatchEnvelope{
			ID:       PatchID{Hex: hex.EncodeToString(h[:])},
			Producer: env.Producer,
			Bytes:    bytes,
		})
		if aerr != nil {
			err = aerr
			return
		}
	}
	inst.mu.Lock()
	if inst.g != nil {
		inst.g.ResolvePseudoEdges()
		hadConflict = algo.HasConflicts(inst.g)
	}
	inst.mu.Unlock()
	audit = fmt.Sprintf("[pushout-native] pull %s ← %s (%d patches)", inst.path, other.path, len(missing))
	return
}

// missingOn returns the hashes inst has but other doesn't, preserving
// inst's apply order so dependencies precede dependents.
func (inst *pushoutRepo) missingOn(other *pushoutRepo) (hashes []t.PatchHash, err error) {
	inst.mu.Lock()
	have := inst.appliedHash
	inst.mu.Unlock()
	other.mu.Lock()
	otherHas := make(map[t.PatchHash]struct{}, len(other.appliedHash))
	for _, h := range other.appliedHash {
		otherHas[h] = struct{}{}
	}
	other.mu.Unlock()
	for _, h := range have {
		if _, ok := otherHas[h]; !ok {
			hashes = append(hashes, h)
		}
	}
	return
}

// ExportLatest returns the most recently recorded envelope as bytes
// for transmission.
func (inst *pushoutRepo) ExportLatest(ctx context.Context) (env PatchEnvelope, audit string, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
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

func envelopeFilePath(repoPath string, h t.PatchHash) (p string) {
	short := hex.EncodeToString(h[:8])
	p = filepath.Join(changesDir(repoPath), short+".json")
	return
}

func writeEnvelope(repoPath string, env envelope.EnvelopeV1) (err error) {
	bytes, eerr := envelope.Encode(env)
	if eerr != nil {
		err = eerr
		return
	}
	werr := os.WriteFile(envelopeFilePath(repoPath, env.Patch.Hash), bytes, 0644)
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
	return
}

func appendApplied(repoPath string, h t.PatchHash) (err error) {
	f, oerr := os.OpenFile(appliedListPath(repoPath), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0644)
	if oerr != nil {
		err = eh.Errorf("open applied.txt: %w", oerr)
		return
	}
	defer f.Close()
	_, werr := fmt.Fprintln(f, hex.EncodeToString(h[:]))
	if werr != nil {
		err = eh.Errorf("write applied.txt: %w", werr)
	}
	return
}

// formatCellLine renders one [KVLine] to a single line of pijul-style
// text — including the trailing newline. The line is what gets stored
// as a node's content; matching content across patches is what
// [patch.LineDiff] uses to detect "kept" lines.
func formatCellLine(c KVLine) (s string) {
	s = fmt.Sprintf("%s \"%s\"\n", c.Path, c.Value)
	return
}

// tolerantApply mirrors [patch.Patch.Apply] with one relaxation:
// DeleteNode on an already-deleted node becomes a no-op. The vendored
// store rejects double-deletion as an error, but the merge model
// requires idempotent deletion — two actors can independently delete
// the same node and both patches must remain applicable in either
// order. AddNode/AddEdge do not have this issue because their
// identities are patch-scoped.
func tolerantApply(p *patch.Patch, g *store.Graggle) (err error) {
	for _, c := range p.Changes {
		switch c.Kind {
		case patch.ChangeNewNode:
			if aerr := g.AddNode(c.NodeID, c.Content, p.Hash, c.UpContext, c.DownContext); aerr != nil {
				err = eh.Errorf("apply NewNode %v: %w", c.NodeID, aerr)
				return
			}
		case patch.ChangeNewEdge:
			if aerr := g.AddEdge(c.Src, c.Dest, p.Hash); aerr != nil {
				err = eh.Errorf("apply NewEdge %v->%v: %w", c.Src, c.Dest, aerr)
				return
			}
		}
	}
	for _, c := range p.Changes {
		if c.Kind != patch.ChangeDeleteNode {
			continue
		}
		if g.IsDeleted(c.NodeID) {
			continue
		}
		if derr := g.DeleteNode(c.NodeID); derr != nil {
			err = eh.Errorf("apply DeleteNode %v: %w", c.NodeID, derr)
			return
		}
	}
	g.ResolvePseudoEdges()
	return
}
