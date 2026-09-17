package cardgrid

import (
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"strings"
	"testing"
)

func TestModelValidate(t *testing.T) {
	ok := Model{
		Count: 2, Slots: SlotsHero | SlotsTitle | SlotsFacts | SlotsTags,
		Title:   []string{"a", "b"},
		FactOff: []int32{0, 2, 3}, FactLabel: []string{"k", "l", "m"}, FactValue: []string{"1", "2", "3"},
		TagOff: []int32{0, 0, 1}, Tag: []string{"x"},
	}
	if err := ok.Validate(); err != nil {
		t.Fatalf("a well-formed model: %v", err)
	}
	if lo, hi, _ := ok.facts(1); lo != 2 || hi != 3 {
		t.Errorf("facts(1) = [%d, %d)", lo, hi)
	}
	if tags := ok.tags(0); len(tags) != 0 {
		t.Errorf("tags(0) = %v", tags)
	}

	bad := []struct {
		name string
		mut  func(m *Model)
		want string
	}{
		{"short slice", func(m *Model) { m.Title = m.Title[:1] }, "Title has 1 entries"},
		{"undeclared slice", func(m *Model) { m.Body = []string{"x", "y"} }, "slot is not declared"},
		{"fact offsets", func(m *Model) { m.FactOff = []int32{0, 3} }, "FactOff has 2 entries"},
		{"fact span", func(m *Model) { m.FactOff = []int32{0, 2, 2} }, "FactOff spans"},
		{"fact monotone", func(m *Model) { m.FactOff = []int32{0, 4, 3} }, "not monotone"},
		{"fact values", func(m *Model) { m.FactValue = m.FactValue[:2] }, "fact values"},
		{"tone length", func(m *Model) { m.Tone = make([]color.Color, 1) }, "Tone has 1 entries"},
	}
	for _, tc := range bad {
		m := ok
		m.Title = append([]string(nil), ok.Title...)
		tc.mut(&m)
		err := m.Validate()
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: got %v, want an error containing %q", tc.name, err, tc.want)
		}
	}
}

func TestStateSelection(t *testing.T) {
	var st State
	if st.Selected() != -1 {
		t.Fatalf("the zero State selects %d", st.Selected())
	}
	st.SetSelected(0)
	if st.Selected() != 0 {
		t.Fatalf("SetSelected(0) reads back %d", st.Selected())
	}
	st.SetSelected(-5)
	if st.Selected() != -1 {
		t.Fatalf("a negative ordinal should clear, got %d", st.Selected())
	}
}
