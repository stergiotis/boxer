//go:build llm_generated_opus47

package marshallreflect

import (
	"sort"

	"github.com/stergiotis/boxer/public/semistructured/leeway/marshallgen"
)

// subColumn / sectionGroup mirror marshallgen's internal grouping. They
// are duplicated here (rather than exported from marshallgen) because
// they are pure value types and the duplication keeps marshallgen's
// public surface unchanged.

type subColumn struct {
	Name   string
	Fields []marshallgen.TaggedField
}

type sectionGroup struct {
	Section     string
	SubColumns  []subColumn
	Memberships []marshallgen.TaggedField
}

// computeGroups mirrors marshallgen.computeGroups: section order is
// DTO declaration order; within each section the fields are
// stable-partitioned scalar-first (shapeScalarBegin /
// shapeScalarBeginSingle) ahead of non-scalars per ADR-0008 D2.
// Memberships are rebuilt from the post-partition order so the two
// packages agree on first-seen-membership ordering.
func computeGroups(plan *marshallgen.Plan) (out []sectionGroup) {
	seen := map[string]int{}
	for _, f := range plan.Fields {
		gIdx, ok := seen[f.LWSection]
		if !ok {
			seen[f.LWSection] = len(out)
			gIdx = len(out)
			out = append(out, sectionGroup{Section: f.LWSection})
		}
		g := &out[gIdx]

		colName := f.LWColumn
		if colName == "" {
			colName = "value"
		}
		scIdx := -1
		for i := range g.SubColumns {
			if g.SubColumns[i].Name == colName {
				scIdx = i
				break
			}
		}
		if scIdx < 0 {
			g.SubColumns = append(g.SubColumns, subColumn{Name: colName})
			scIdx = len(g.SubColumns) - 1
		}
		g.SubColumns[scIdx].Fields = append(g.SubColumns[scIdx].Fields, f)
	}

	for gi := range out {
		g := &out[gi]
		for sci := range g.SubColumns {
			partitionScalarsFirst(g.SubColumns[sci].Fields)
		}
		rebuildMemberships(g)
	}
	return
}

func partitionScalarsFirst(fields []marshallgen.TaggedField) {
	sort.SliceStable(fields, func(i, j int) bool {
		return isScalarShape(fields[i]) && !isScalarShape(fields[j])
	})
}

func isScalarShape(f marshallgen.TaggedField) bool {
	switch classifyBegin(f) {
	case shapeScalarBegin, shapeScalarBeginSingle:
		return true
	default:
		return false
	}
}

func rebuildMemberships(g *sectionGroup) {
	g.Memberships = g.Memberships[:0]
	seen := map[string]bool{}
	for sci := range g.SubColumns {
		for _, f := range g.SubColumns[sci].Fields {
			if seen[f.LWMembership] {
				continue
			}
			seen[f.LWMembership] = true
			g.Memberships = append(g.Memberships, f)
		}
	}
}

// fieldBeginShape mirrors marshallgen's internal classifier — kept
// in sync with the matrix in EXPLANATION.md.
type fieldBeginShape int

const (
	shapeScalarBegin fieldBeginShape = iota
	shapeScalarBeginSingle
	shapeContainer
	shapeExplodeBegin
	shapeExplodeBeginSingle
)

func classifyBegin(f marshallgen.TaggedField) fieldBeginShape {
	isMulti := f.IsSlice || f.IsRoaring
	switch {
	case isMulti && f.Flags.Explode && f.Flags.Unit:
		return shapeExplodeBeginSingle
	case isMulti && f.Flags.Explode:
		return shapeExplodeBegin
	case isMulti:
		return shapeContainer
	case f.Flags.Unit:
		return shapeScalarBeginSingle
	default:
		return shapeScalarBegin
	}
}

func findPlainCol(plan *marshallgen.Plan, col string) *marshallgen.PlainCol {
	for i := range plan.PlainCols {
		if plan.PlainCols[i].Column == col {
			return &plan.PlainCols[i]
		}
	}
	return nil
}

func upperFirst(s string) string {
	if s == "" {
		return s
	}
	if s[0] >= 'a' && s[0] <= 'z' {
		return string(s[0]-32) + s[1:]
	}
	return s
}
