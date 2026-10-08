package play

import (
	"cmp"
	"slices"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/worldmap"
)

// The World pane as an agent reads and sets it (ADR-0270, update of
// 2026-10-05). The read is the last extraction: which column the pane took
// for countries and which for the value, what resolved and what did not,
// and each country with the row that set it. The command sets the value
// column and the projection. The pane has no pin: a country is selected
// through set_signal('selection') with the row get_world gives.

const (
	opGetWorld            = "get_world"
	opSetWorldOptions     = "set_world_options"
	worldPaneId           = "world"
	opsResWorld           = worldPaneId
	worldReadDefaultLimit = 50
	worldReadMaxLimit     = 300
	// worldUnmatchedSamples bounds the distinct country cells an extraction
	// keeps of those that resolved to nothing.
	worldUnmatchedSamples = 20
	// worldValueAutoName and worldValuePresenceName are the value settings
	// that name no column.
	worldValueAutoName     = "auto"
	worldValuePresenceName = "presence"
)

// WorldCountryReading is one country the map shades.
type WorldCountryReading struct {
	Country string   `desc:"the atlas's name for the country"`
	Iso     string   `json:",omitzero" desc:"its ISO 3166-1 alpha-3 code, or alpha-2 where it has no alpha-3"`
	Value   *float64 `json:",omitzero" desc:"the value it is shaded by; absent under presence, or where its cell was NULL"`
	Row     int64    `desc:"the result row that set it, the last of its rows; set_signal('selection', row) selects it"`
	Cell    string   `desc:"the country cell as the data spells it"`
}

// WorldReading is get_world's result.
type WorldReading struct {
	Drawn          PaneDraw              `desc:"which draw this is of, its status line, and why it drew nothing when it did not"`
	Value          string                `desc:"the value setting: auto (the first numeric column), presence, or a column's name"`
	Projection     string                `desc:"the projection drawn"`
	Projections    []string              `desc:"the projections set_world_options takes"`
	CountryColumn  string                `json:",omitzero" desc:"the column the pane resolved to countries"`
	ValueColumn    string                `json:",omitzero" desc:"the column the fill is shaded by; absent when the fill is presence"`
	NoSpread       bool                  `json:",omitzero" desc:"the value column holds one value across the matched countries, so the fill fell back to presence"`
	NumericColumns []string              `json:",omitzero" desc:"the columns value can name"`
	Min            *float64              `json:",omitzero" desc:"the legend's low end"`
	Max            *float64              `json:",omitzero" desc:"the legend's high end"`
	Countries      int32                 `json:",omitzero" desc:"countries at least one row resolved to"`
	UnmatchedRows  int64                 `json:",omitzero" desc:"rows whose country cell resolved to no country"`
	DuplicateRows  int64                 `json:",omitzero" desc:"rows past the first for a country; the last one wins"`
	Unmatched      []string              `json:",omitzero" desc:"up to 20 distinct country cells that resolved to nothing, for fixing the spelling in SQL"`
	List           []WorldCountryReading `json:",omitzero" desc:"the countries, highest value first (by name under presence)"`
	More           int32                 `json:",omitzero" desc:"countries past the ones listed; read on with offset"`
}

// GetWorldArgs is get_world's argument.
type GetWorldArgs struct {
	Offset int32 `json:",omitzero" desc:"countries to skip"`
	Limit  int32 `json:",omitzero" desc:"countries to list, 50 by default and at most 300"`
}

// SetWorldOptionsArgs is set_world_options' argument.
type SetWorldOptionsArgs struct {
	Value      *string `json:",omitzero" desc:"auto (the first numeric column), presence (membership only), or a numeric column's name"`
	Projection *string `json:",omitzero" desc:"one of the projections get_world lists"`
}

// worldOpsView is what get_world reads: the last extraction, shared, and the
// settings as they stand.
type worldOpsView struct {
	fold       *worldFold
	valueCol   int
	valueName  string
	projection worldmap.Projection
}

// opsView copies what get_world reads.
func (inst *WorldDriver) opsView() (v worldOpsView) {
	v = worldOpsView{fold: inst.fold, valueCol: inst.valueCol, projection: inst.widget.Opts.Projection}
	v.valueName = inst.valueSettingName()
	return
}

// valueSettingName is the value setting as set_world_options spells it.
func (inst *WorldDriver) valueSettingName() string {
	switch inst.valueCol {
	case worldValueAuto:
		return worldValueAutoName
	case worldValuePresence:
		return worldValuePresenceName
	}
	if s := inst.forSchema; s != nil && inst.valueCol < s.NumFields() {
		return s.Field(inst.valueCol).Name
	}
	return worldValueAutoName
}

func (inst *PlayApp) worldView() worldOpsView { return inst.worldDriver.opsView() }

func worldProjectionNames() (out []string) {
	for _, p := range worldmap.Projections {
		out = append(out, p.String())
	}
	return
}

func worldCountryIso(c *worldmap.Country) string {
	if c.A3 != "" {
		return c.A3
	}
	return c.A2
}

func worldReading(sn *opsSnap, in GetWorldArgs) (out WorldReading, err error) {
	d, readable, err := paneDrawOf(sn, worldPaneId)
	if err != nil {
		return
	}
	v := &sn.paneViews.world
	out = WorldReading{Drawn: d, Value: v.valueName, Projection: v.projection.String(), Projections: worldProjectionNames()}
	f := v.fold
	if !readable || f == nil {
		return
	}
	limit := in.Limit
	switch {
	case limit < 0 || limit > worldReadMaxLimit:
		err = app.RefuseOperation("limit is at most " + strconv.Itoa(worldReadMaxLimit))
		return
	case limit == 0:
		limit = worldReadDefaultLimit
	}
	if in.Offset < 0 {
		err = app.RefuseOperation("offset is a count of countries to skip, 0 or more")
		return
	}
	out.CountryColumn, out.ValueColumn, out.NoSpread = f.countryCol, f.valueCol, f.degenerate
	out.NumericColumns = f.numeric
	out.Countries, out.UnmatchedRows, out.DuplicateRows = int32(f.matched), int64(f.unmatched), int64(f.dupes)
	for _, u := range f.unmatchedSeen {
		out.Unmatched = append(out.Unmatched, opsLabel(u))
	}
	shaded := f.valueOf != nil && !f.degenerate
	if shaded {
		out.Min, out.Max = finite(f.vmin), finite(f.vmax)
	}
	list := make([]WorldCountryReading, 0, len(f.rowOf))
	vals := make([]float64, 0, len(f.rowOf))
	for idx, row := range f.rowOf {
		if int(idx) < 0 || int(idx) >= len(f.atlas.Countries) {
			continue
		}
		c := &f.atlas.Countries[idx]
		r := WorldCountryReading{Country: c.Name, Iso: worldCountryIso(c), Row: row, Cell: opsLabel(f.cellOf[idx])}
		val, has := f.valueOf[idx]
		if shaded && has {
			r.Value = finite(val)
		}
		list = append(list, r)
		vals = append(vals, val)
	}
	order := make([]int, len(list))
	for i := range order {
		order[i] = i
	}
	slices.SortFunc(order, func(a, b int) int {
		ra, rb := &list[a], &list[b]
		if shaded && (ra.Value != nil) != (rb.Value != nil) {
			if ra.Value != nil {
				return -1
			}
			return 1
		}
		if shaded && ra.Value != nil {
			if c := cmp.Compare(vals[b], vals[a]); c != 0 {
				return c
			}
		}
		return strings.Compare(ra.Country, rb.Country)
	})
	start := min(int(in.Offset), len(order))
	end := min(start+int(limit), len(order))
	for _, i := range order[start:end] {
		out.List = append(out.List, list[i])
	}
	out.More = int32(len(order) - end)
	return
}

// setOptions is set_world_options on the driver. The value column is
// resolved by name against the schema of the last extraction, which is the
// result the pane is fed.
func (inst *WorldDriver) setOptions(in SetWorldOptionsArgs) (err error) {
	if in.Value == nil && in.Projection == nil {
		return noOptionsRefusal(worldPaneId, "value", "projection")
	}
	valueCol := inst.valueCol
	if in.Value != nil {
		switch name := strings.TrimSpace(*in.Value); name {
		case worldValueAutoName:
			valueCol = worldValueAuto
		case worldValuePresenceName, "none", "none (presence)":
			valueCol = worldValuePresence
		default:
			s := inst.forSchema
			if s == nil {
				return app.RefuseOperation("the World pane has drawn no map yet, so a column cannot be named; auto and presence can")
			}
			valueCol = -1
			var names []string
			for _, ci := range numericColumns(s) {
				names = append(names, s.Field(ci).Name)
				if valueCol < 0 && s.Field(ci).Name == name {
					valueCol = ci
				}
			}
			if valueCol < 0 {
				return app.RefuseOperation(strconv.Quote(name) + " is not a numeric column of the result the pane draws; value is auto, presence, or one of: " + strings.Join(names, ", "))
			}
		}
	}
	proj := inst.widget.Opts.Projection
	if in.Projection != nil {
		found := false
		for _, p := range worldmap.Projections {
			if strings.EqualFold(p.String(), strings.TrimSpace(*in.Projection)) {
				proj, found = p, true
				break
			}
		}
		if !found {
			return app.RefuseOperation("projection is one of: " + strings.Join(worldProjectionNames(), ", "))
		}
	}
	inst.valueCol = valueCol
	inst.widget.Opts.Projection = proj
	return nil
}

// requestOptions is a combo pick: through set_world_options when play's
// launcher routes it, directly otherwise.
func (inst *WorldDriver) requestOptions(in SetWorldOptionsArgs) {
	if inst.onOptions != nil {
		inst.onOptions(in)
		return
	}
	_ = inst.setOptions(in)
}

// worldOptionsDigest is the world resource: the value setting and the
// projection.
func worldOptionsDigest(p *PlayApp) string {
	d := p.worldDriver
	if d == nil {
		return ""
	}
	return "value=" + strconv.Itoa(d.valueCol) + "|proj=" + strconv.Itoa(int(d.widget.Opts.Projection))
}

func addWorldOps(s *appops.Set[*PlayLauncher, opsSnap]) {
	addPaneOps(s, paneOpsSpec[WorldReading, GetWorldArgs, SetWorldOptionsArgs]{
		pane:     worldPaneId,
		resource: "the World pane's settings: the value column and the projection",
		digest:   worldOptionsDigest,
		get:      opGetWorld,
		getSummary: "read what the World pane last drew: the country and value columns it took, the legend's range, " +
			"how many rows resolved to countries and which cells did not, and each country with its value and row",
		set:        opSetWorldOptions,
		setSummary: "set the World pane's value column (auto, presence or a numeric column) and its projection",
		gesture:    "the value and projection combos above the map",
		follows:    []string{"the pane shades with the new settings from its next frame; the result is not rerun"},
		read:       worldReading,
		apply:      func(p *PlayApp, in SetWorldOptionsArgs) error { return p.worldDriver.setOptions(in) },
	})
}
