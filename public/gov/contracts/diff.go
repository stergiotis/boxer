package contracts

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/config/env"
)

// Change is one difference between a baseline and the tree.
type Change struct {
	Kind KindE
	Item string
	// What says what happened: "added", "removed", "changed".
	What string
	// Additive is the verdict of the kind's rule (ADR-0388).
	Additive bool
	// Why is the rule's reason, one line.
	Why string
	// AckedBy is the migration note acknowledging a non-additive change;
	// empty when none does.
	AckedBy string
}

// Key is how a migration note names the change: `<kind>/<item>`.
func (inst Change) Key() string { return string(inst.Kind) + "/" + inst.Item }

// Diff compares the tree's snapshot against the baseline, kind by kind,
// sorted by kind and item.
func Diff(baseline Snapshot, tree Snapshot) (changes []Change) {
	retired := make(map[string]bool)
	for _, r := range env.RetiredVars() {
		retired[r.Name] = true
	}
	kinds := []KindE{KindDDL, KindMembership, KindLwSurface, KindEnv, KindIDL, KindVersion, KindCLI}
	for _, k := range kinds {
		old, cur := baseline[k], tree[k]
		items := make(map[string]bool, len(old)+len(cur))
		for i := range old {
			items[i] = true
		}
		for i := range cur {
			items[i] = true
		}
		names := make([]string, 0, len(items))
		for i := range items {
			names = append(names, i)
		}
		slices.Sort(names)
		for _, item := range names {
			ov, inOld := old[item]
			nv, inNew := cur[item]
			switch {
			case inOld && inNew && ov == nv:
				continue
			case !inOld:
				changes = append(changes, Change{Kind: k, Item: item, What: "added", Additive: true, Why: "new"})
			case !inNew:
				c := Change{Kind: k, Item: item, What: "removed", Why: "a consumer of it breaks"}
				if k == KindEnv && retired[item] {
					c.Additive, c.Why = true, "retired with a tombstone; a set value warns"
				}
				changes = append(changes, c)
			default:
				changes = append(changes, changed(k, item, ov, nv, tree))
			}
		}
	}
	changes = lwSurfaceVerdict(changes, baseline, tree)
	return
}

// changed classifies an item present on both sides with another content.
func changed(k KindE, item string, ov string, nv string, tree Snapshot) (c Change) {
	c = Change{Kind: k, Item: item, What: "changed"}
	switch k {
	case KindDDL, KindMembership:
		missing := linesMissing(ov, nv)
		if len(missing) == 0 {
			c.Additive, c.Why = true, "every line of the baseline is still there"
		} else {
			c.Why = "a line of the baseline is gone or changed: " + strings.TrimSpace(missing[0])
		}
	case KindVersion:
		c.Why = "a version moves only when an older reader cannot read the newer form (" + ov + " → " + nv + ")"
	case KindIDL:
		c.Why = "the IDL changed; a client built from the old one is refused"
	case KindLwSurface:
		// Settled across the kind by lwSurfaceVerdict.
		c.Why = ov + " → " + nv
	default:
		c.Why = ov + " → " + nv
	}
	return
}

// lwSurfaceVerdict applies the surface's rule: a higher generation with a
// roster that only grew is additive, digest included — an older build
// leaves a newer surface in place (ADR-0171, update of 2026-11-20). Bodies
// that moved without a generation bump, or a lower generation, are not.
func lwSurfaceVerdict(changes []Change, baseline Snapshot, tree Snapshot) (out []Change) {
	og, _ := strconv.Atoi(baseline[KindLwSurface]["generation"])
	ng, _ := strconv.Atoi(tree[KindLwSurface]["generation"])
	for _, c := range changes {
		if c.Kind == KindLwSurface && c.What == "changed" {
			switch {
			case ng > og:
				c.Additive, c.Why = true, "generation "+strconv.Itoa(og)+" → "+strconv.Itoa(ng)+"; an older build leaves the newer surface in place"
			case ng < og:
				c.Why = "the generation went down"
			default:
				c.Why = "the bodies moved without a generation bump"
			}
		}
		out = append(out, c)
	}
	return
}

// linesMissing lists the baseline's lines the new text lacks, compared as
// a multiset of trimmed lines with a trailing comma dropped — so a column
// appended after the last one, which adds a comma to its predecessor, is
// still read as an addition.
func linesMissing(old string, cur string) (missing []string) {
	have := make(map[string]int)
	for l := range strings.SplitSeq(cur, "\n") {
		have[normLine(l)]++
	}
	for l := range strings.SplitSeq(old, "\n") {
		n := normLine(l)
		if have[n] > 0 {
			have[n]--
			continue
		}
		missing = append(missing, l)
	}
	return
}

func normLine(l string) string {
	return strings.TrimSuffix(strings.TrimSpace(l), ",")
}

// ackPattern finds the acknowledgments a migration note makes: the key runs
// to a closing backtick or the end of the line, so a command-tree key, which
// holds spaces (`cli/boxer gov --root`), can be named too.
var ackPattern = regexp.MustCompile("contract:[ \\t]*([^`\\n]+)")

// Acks reads the acknowledgments the migration notes under dir make, key →
// the note's file name. A missing dir acknowledges nothing.
func Acks(dir string) (acks map[string]string, err error) {
	acks = make(map[string]string)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return acks, nil
	}
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		b, rerr := os.ReadFile(filepath.Join(dir, e.Name()))
		if rerr != nil {
			return nil, rerr
		}
		for _, m := range ackPattern.FindAllStringSubmatch(string(b), -1) {
			acks[strings.TrimSpace(m[1])] = e.Name()
		}
	}
	return
}

// Acknowledge marks each non-additive change a note names.
func Acknowledge(changes []Change, acks map[string]string) {
	for i := range changes {
		if !changes[i].Additive {
			changes[i].AckedBy = acks[changes[i].Key()]
		}
	}
}

// Unacknowledged counts the non-additive changes no note names.
func Unacknowledged(changes []Change) (n int) {
	for _, c := range changes {
		if !c.Additive && c.AckedBy == "" {
			n++
		}
	}
	return
}
