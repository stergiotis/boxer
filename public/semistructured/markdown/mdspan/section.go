package mdspan

import (
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// FindHeading resolves a heading path to an index into Headings.
//
// The path's last element names the heading; the elements before it name
// its nearest ancestors, innermost last, so ["Design", "Goals"] is a Goals
// directly under Design. A one-element path matches a heading at any depth.
// Each element matches a heading's text or slug, ignoring case and runs of
// space. ok is false when nothing matches; candidates lists every match's
// path when more than one does.
func (inst *Doc) FindHeading(path []string) (idx int, ok bool, candidates [][]string) {
	idx = -1
	if len(path) == 0 {
		return
	}
	want := make([]string, len(path))
	for i, p := range path {
		want[i] = normHeading(p)
	}
	for i := range inst.Headings {
		if !inst.matchesUpward(i, want) {
			continue
		}
		candidates = append(candidates, inst.HeadingPath(i))
		idx = i
	}
	switch len(candidates) {
	case 0:
		idx = -1
	case 1:
		ok = true
		candidates = nil
	default:
		idx = -1
	}
	return
}

// FindHeadingE is FindHeading with the failures as errors that say what to
// do next.
func (inst *Doc) FindHeadingE(path []string) (idx int, err error) {
	idx, ok, cands := inst.FindHeading(path)
	if ok {
		return
	}
	if len(cands) > 1 {
		paths := make([]string, 0, len(cands))
		for _, c := range cands {
			paths = append(paths, strings.Join(c, " > "))
		}
		err = eb.Build().Strs("candidates", paths).Errorf("heading path is ambiguous; name an ancestor too")
		return
	}
	err = eb.Build().Strs("path", path).Errorf("no heading matches the path")
	return
}

func (inst *Doc) matchesUpward(i int, want []string) bool {
	j := i
	for k := len(want) - 1; k >= 0; k-- {
		if j < 0 {
			return false
		}
		h := inst.Headings[j]
		if want[k] != normHeading(h.Text) && want[k] != normHeading(h.Slug) {
			return false
		}
		j = h.Parent
	}
	return true
}

func normHeading(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimLeft(strings.TrimSpace(s), "#")), " "))
}
