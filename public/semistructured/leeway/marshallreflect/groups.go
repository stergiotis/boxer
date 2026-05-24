//go:build llm_generated_opus47

package marshallreflect

import (
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

		seenMemb := false
		for _, m := range g.Memberships {
			if m.LWMembership == f.LWMembership {
				seenMemb = true
				break
			}
		}
		if !seenMemb {
			g.Memberships = append(g.Memberships, f)
		}
	}
	return
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
