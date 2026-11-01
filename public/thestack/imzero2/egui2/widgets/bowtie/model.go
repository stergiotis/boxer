package bowtie

import (
	"slices"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// SideE is the half of the bow-tie a path sits on.
type SideE uint8

const (
	// SidePrevention is the left half: a threat, and the barriers that stop
	// it before the top event.
	SidePrevention SideE = iota
	// SideMitigation is the right half: a consequence, and the barriers that
	// limit it once the top event has happened.
	SideMitigation
)

// ParseSide reads the side vocabulary — `prevention` or `mitigation`, any
// case, surrounding space ignored.
func ParseSide(s string) (side SideE, ok bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "prevention":
		return SidePrevention, true
	case "mitigation":
		return SideMitigation, true
	}
	return
}

func (inst SideE) String() string {
	if inst == SideMitigation {
		return "mitigation"
	}
	return "prevention"
}

// StatusE is a barrier's test status. Every status is drawn as a colour, an
// icon and a label together, so none depends on colour alone.
type StatusE uint8

const (
	// StatusUntested is a barrier with no test in the window the data's
	// source applies — twelve months, in the label. It is the zero value, so
	// a barrier nobody has said anything about reads as untested rather than
	// as effective.
	StatusUntested StatusE = iota
	StatusEffective
	StatusDegraded
	StatusFailed
)

// statusTokens is the spelling ParseStatus accepts and Token returns.
var statusTokens = [...]string{
	StatusUntested:  "untested",
	StatusEffective: "effective",
	StatusDegraded:  "degraded",
	StatusFailed:    "failed",
}

// StatusTokens lists the status vocabulary in StatusE order, for a message
// that has to name it.
func StatusTokens() []string { return slices.Clone(statusTokens[:]) }

// ParseStatus reads the status vocabulary — `effective`, `degraded`,
// `failed`, `untested`, any case. An empty string is StatusUntested: a
// barrier with no recorded test.
func ParseStatus(s string) (st StatusE, ok bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return StatusUntested, true
	}
	for i, t := range statusTokens {
		if t == s {
			return StatusE(i), true
		}
	}
	return
}

// Token is the status's spelling in data.
func (inst StatusE) Token() string {
	if int(inst) < len(statusTokens) {
		return statusTokens[inst]
	}
	return statusTokens[StatusUntested]
}

// Label is the text drawn beside the status icon.
func (inst StatusE) Label() string {
	switch inst {
	case StatusEffective:
		return "Tested, effective"
	case StatusDegraded:
		return "Tested, degraded"
	case StatusFailed:
		return "Tested, failed"
	default:
		return "Not tested in 12 months"
	}
}

// Icon is the Phosphor glyph drawn before the label.
func (inst StatusE) Icon() string {
	switch inst {
	case StatusEffective:
		return icons.PhCheckCircle
	case StatusDegraded:
		return icons.PhWarningCircle
	case StatusFailed:
		return icons.PhXCircle
	default:
		return icons.PhQuestion
	}
}

// Tone is the semantic colour role the status is painted in.
func (inst StatusE) Tone() styletokens.Tone {
	switch inst {
	case StatusEffective:
		return styletokens.ToneSuccess
	case StatusDegraded:
		return styletokens.ToneWarning
	case StatusFailed:
		return styletokens.ToneError
	default:
		return styletokens.ToneNeutral
	}
}

// Barrier is one control on a path, in series with the others on it.
type Barrier struct {
	Name     string
	Status   StatusE
	Evidence string // the test behind the status — date, result; shown on hover
	// Source is the index of the Row BuildModel read the barrier from, for a
	// host to map a click back to its own data; a hand-built model may leave
	// it zero.
	Source int
}

// Path is a threat (prevention side) or a consequence (mitigation side) and
// its barriers, ordered from the top event outward on the mitigation side and
// from the threat inward on the prevention side — the order a path is walked
// in.
type Path struct {
	Name     string
	Barriers []Barrier
	// Source is the index of the first Row that named the path (see
	// Barrier.Source).
	Source int
}

// Model is one bow-tie: the top event, the threats that lead to it and the
// consequences that follow from it. It is the host's data and carries no UI
// state.
type Model struct {
	TopEvent     string
	Threats      []Path
	Consequences []Path
}

// Paths returns the paths of one side.
func (inst *Model) Paths(side SideE) []Path {
	if side == SideMitigation {
		return inst.Consequences
	}
	return inst.Threats
}

// Row is one element of a path, the shape a table of bow-tie data has: the
// path's barrier at Step, or — with no step and no barrier — the path alone.
type Row struct {
	TopEvent string
	Side     SideE
	Path     string
	Step     int64
	HasStep  bool
	Barrier  string
	Status   StatusE
	Evidence string
}

// BuildModel folds rows into a Model. Paths keep the order rows first name
// them, per side; barriers are ordered by Step, which need only be distinct
// on its path, not consecutive.
//
// It refuses rows that do not describe one bow-tie: two different top events,
// none at all, a row with no path, a barrier with no step or a step with no
// barrier, and two barriers at one step of one path — parallel barriers are
// not something the diagram can draw.
func BuildModel(rows []Row) (m Model, err error) {
	type pathKey struct {
		side SideE
		name string
	}
	type stepped struct {
		step int64
		b    Barrier
	}
	index := make(map[pathKey]int, 8)
	var steps [2][][]stepped
	for i, r := range rows {
		if top := strings.TrimSpace(r.TopEvent); top != "" {
			if m.TopEvent == "" {
				m.TopEvent = top
			} else if top != m.TopEvent {
				err = eb.Build().Int("row", i).Str("first", m.TopEvent).Str("other", top).
					Errorf("a bow-tie has one top event; the rows name two")
				return
			}
		}
		name := strings.TrimSpace(r.Path)
		if name == "" {
			err = eb.Build().Int("row", i).Errorf("a row names no path (threat or consequence)")
			return
		}
		barrier := strings.TrimSpace(r.Barrier)
		switch {
		case r.HasStep && barrier == "":
			err = eb.Build().Int("row", i).Str("path", name).Int64("step", r.Step).Errorf("a step names no barrier")
			return
		case !r.HasStep && barrier != "":
			err = eb.Build().Int("row", i).Str("path", name).Str("barrier", barrier).Errorf("a barrier has no step")
			return
		}
		k := pathKey{side: r.Side, name: name}
		pi, seen := index[k]
		if !seen {
			pi = len(steps[r.Side])
			index[k] = pi
			steps[r.Side] = append(steps[r.Side], nil)
			p := Path{Name: name, Source: i}
			if r.Side == SideMitigation {
				m.Consequences = append(m.Consequences, p)
			} else {
				m.Threats = append(m.Threats, p)
			}
		}
		if !r.HasStep {
			continue
		}
		for _, s := range steps[r.Side][pi] {
			if s.step == r.Step {
				err = eb.Build().Int("row", i).Str("path", name).Int64("step", r.Step).
					Str("barrier", barrier).Str("other", s.b.Name).Errorf("two barriers at one step of a path")
				return
			}
		}
		steps[r.Side][pi] = append(steps[r.Side][pi], stepped{step: r.Step,
			b: Barrier{Name: barrier, Status: r.Status, Evidence: strings.TrimSpace(r.Evidence), Source: i}})
	}
	if m.TopEvent == "" && len(rows) > 0 {
		err = eb.Build().Int("rows", len(rows)).Errorf("no row names a top event")
		return
	}
	for side := range steps {
		paths := m.Paths(SideE(side))
		for pi, ss := range steps[side] {
			slices.SortStableFunc(ss, func(a, b stepped) int {
				switch {
				case a.step < b.step:
					return -1
				case a.step > b.step:
					return 1
				}
				return 0
			})
			for _, s := range ss {
				paths[pi].Barriers = append(paths[pi].Barriers, s.b)
			}
		}
	}
	return
}
