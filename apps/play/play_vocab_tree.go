package play

import (
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/tree"
)

// play_vocab_tree.go groups the Vocabulary tab's flat roster into the outline
// it draws — population, then family, then function — and produces the
// columnar (Labels, Parents, Keys) input tree.Render takes (ADR-0176 §SD1).
//
// It is split from the render for the reason every other tree adopter split
// theirs: the grouping is the part worth testing, and it is testable without a
// binding in sight.
//
// # Why a family level
//
// ADR-0174 §SD1 groups by where a function runs, because that predicts how a
// call fails. Within a section that leaves one flat run — 39 declared server
// functions, 21 client ones, and however many undeclared extras the endpoint
// happens to carry, which on a real endpoint has been in the hundreds. Family
// is the second grouping the model already carries: it names the declaring
// roster and the ADR a reader follows to find out what the family is for. It
// was computed and searched from the first version of the panel and never
// shown — vocabFamilyLabel's own doc comment called it "the panel's Family
// column" while no such column existed.
//
// Family does not replace the section: it subdivides one. A family sits
// entirely inside one population, so nothing about §SD1's failure-mode
// grouping changes — LW_ID_* still appears under both server and client, now
// as a family in each.
//
// # Every node carries a key
//
// The host rebuilds this whole outline every frame and the filter renumbers it
// on every keystroke, which is exactly the case tree.State's node indices do
// not survive (ADR-0176 §SD2). Absent keys, a family the reader collapsed
// reopens somewhere else one keystroke later, and it reads as a flaky widget
// rather than as a host bug.

// vocabNodeKindE is what one outline row stands for.
type vocabNodeKindE uint8

const (
	// vocabNodeSection is a population: server, client, play.
	vocabNodeSection vocabNodeKindE = iota
	// vocabNodeFamily is a declaring roster within one population.
	vocabNodeFamily
	// vocabNodeFunc is one callable name.
	vocabNodeFunc
)

// vocabNode is the per-node metadata the cells read back, indexed by the same
// node index the tree's own columns use. The tree carries the label and the
// parent; everything a cell needs beyond the label lives here.
type vocabNode struct {
	Kind  vocabNodeKindE
	Where vocabWhereE
	// Entry is the function this row shows. Meaningful for vocabNodeFunc only.
	Entry vocabEntry
	// Count is how many function rows sit beneath a section or family row.
	// Shown on the row itself so the size of a collapsed family is legible
	// without opening it.
	Count int
}

// vocabOutline is one frame's hierarchy: the widget's input and the metadata
// beside it, which share a node index.
type vocabOutline struct {
	Tree  tree.Tree
	Nodes []vocabNode
}

// Len is the node count.
func (inst vocabOutline) Len() int { return len(inst.Nodes) }

// buildVocabOutline groups entries into population, family and function,
// keeping only the ones accept admits.
//
// Every section is emitted even when the filter empties it, because the three
// populations are the panel's claim about what exists and a section that
// vanishes under a filter reads as "this build has none" — the same confusion
// ADR-0174's Alternatives rejected O1 over. An emptied FAMILY is dropped: a
// family is a fact about the roster, not about the build's shape, and a screen
// of empty ones would bury the matches.
//
// accept nil admits everything.
func buildVocabOutline(entries []vocabEntry, accept func(vocabEntry) bool) (out vocabOutline) {
	// Sections, families and the functions themselves; the slack covers the
	// section and family rows that are not in entries.
	n := len(entries) + 16
	out.Tree.Labels = make([]string, 0, n)
	out.Tree.Parents = make([]int32, 0, n)
	out.Tree.Keys = make([]string, 0, n)
	out.Nodes = make([]vocabNode, 0, n)

	add := func(label string, key string, parent int32, node vocabNode) int32 {
		out.Tree.Labels = append(out.Tree.Labels, label)
		out.Tree.Parents = append(out.Tree.Parents, parent)
		out.Tree.Keys = append(out.Tree.Keys, key)
		out.Nodes = append(out.Nodes, node)
		return int32(len(out.Nodes) - 1)
	}

	for _, where := range []vocabWhereE{vocabServer, vocabClient, vocabPlay} {
		sec := add(where.title(), vocabSectionKey(where), -1,
			vocabNode{Kind: vocabNodeSection, Where: where})
		// First-appearance order, so families follow the order the model
		// assembles the rosters in rather than an alphabetical one that would
		// separate the pack from the read-back family it is provisioned with.
		fams := make(map[string]int32, 8)
		for _, e := range entries {
			if e.Where != where {
				continue
			}
			if accept != nil && !accept(e) {
				continue
			}
			fam, ok := fams[e.Family]
			if !ok {
				fam = add(e.Family, vocabFamilyKey(where, e.Family), sec,
					vocabNode{Kind: vocabNodeFamily, Where: where})
				fams[e.Family] = fam
			}
			add(e.call(), vocabFuncKey(where, e.Name), fam,
				vocabNode{Kind: vocabNodeFunc, Where: where, Entry: e})
			out.Nodes[fam].Count++
			out.Nodes[sec].Count++
		}
	}
	return
}

// The three key spaces. Prefixed so a family named after a population — or a
// function named after its family — cannot collide with one, since a shared
// key IS a shared identity to tree.State and the widget documents duplicates
// as undetected rather than rejected.
func vocabSectionKey(where vocabWhereE) string { return "s|" + where.String() }

func vocabFamilyKey(where vocabWhereE, family string) string {
	return "f|" + where.String() + "|" + family
}

func vocabFuncKey(where vocabWhereE, name string) string {
	return "n|" + where.String() + "|" + name
}

// vocabExtraFamilies names the two families that hold what the endpoint
// carries and no roster declares (vocabExtras). They are the one population
// whose size this build does not bound — a live endpoint has reported
// hundreds — so the panel opens with them closed and every declared family
// open. See vocabTabState.seeded, which applies this once rather than every
// frame, so a reader who opens one keeps it open.
func vocabExtraFamilies() []string {
	return []string{vocabFamilyUndeclared, vocabFamilyWithdrawn}
}
