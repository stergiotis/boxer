package play

import (
	"strconv"
	"strings"
	"sync"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stergiotis/boxer/public/hmi/gloss"
)

// play_column_unit.go is what a named-column contract reads off a column's
// gloss declaration when the pane does arithmetic on the values rather than
// only showing them: the unit they are in. It reads the declaration in the
// column's NAME, the alias or the gloss(…) macro that writes one, and not the
// rules: a contract already asks for one AS per column, so the declaration has
// a place to go, and a pane's acceptance runs per frame without the buffer's
// directives to hand (ADR-0122's 2026-11-01 units update).

// declaredColumnCatalog is the catalog a column name is parsed against, built
// once: a consumer's extensions register in init, before the first frame, and
// this is asked for per column per frame.
var declaredColumnCatalog = sync.OnceValue(gloss.Default)

// columnUnit is the unit a column declares. The zero value declares none.
type columnUnit struct {
	// mediaType and unit are the gloss and its stored unit, which is what two
	// columns must share to be the same quantity.
	mediaType, unit string
	inst            gloss.InstanceI
}

// declared reports whether the column names a unit.
func (inst columnUnit) declared() bool { return inst.mediaType != "" }

// same reports whether two columns' values are in one unit.
func (inst columnUnit) same(o columnUnit) bool {
	return inst.mediaType == o.mediaType && inst.unit == o.unit
}

// String is the unit as a reason spells it.
func (inst columnUnit) String() string {
	if !inst.declared() {
		return "no unit"
	}
	return gloss.CompactMediaType(inst.mediaType, map[string]string{gloss.ParamUnit: inst.unit})
}

// format renders an amount through the gloss's inline face, or returns nil for
// a column with no unit: nil is what a widget reads as "no formatter".
func (inst columnUnit) format() func(float64) string {
	if inst.inst == nil {
		return nil
	}
	return func(v float64) string {
		return inst.inst.Inline(gloss.TextCell{S: strconv.FormatFloat(v, 'f', -1, 64), K: gloss.ValueKindNumeric}).Text
	}
}

// columnUnitOf reads a column's declared unit. A gloss that takes no `unit` —
// gloss/raw, gloss/bytes — says nothing about one, and the column has none. A
// declaration the catalog refuses, a misspelt currency, is a reason: the
// column plainly meant to say a unit. So is gloss/epoch, whose `unit` is a
// clock resolution: a moment is not an amount.
func columnUnitOf(field arrow.Field) (u columnUnit, reason string) {
	if !strings.ContainsRune(field.Name, '@') {
		return u, ""
	}
	d, declared := declaredColumnCatalog().ParseColumn(field.Name)
	if !declared {
		return u, ""
	}
	if d.Reason != "" {
		return u, "Column `" + d.Label + "` declares a gloss that does not bind: " + d.Reason
	}
	if d.MediaType == gloss.MediaTypeEpoch {
		return u, "Column `" + d.Label + "` is " + d.MediaType + ", a moment; this pane needs an amount."
	}
	unit, has := d.Params[gloss.ParamUnit]
	if !has {
		return u, ""
	}
	if ok, _ := d.Instance.Accepts(gloss.ValueKindNumeric); !ok {
		return u, ""
	}
	return columnUnit{mediaType: d.MediaType, unit: unit, inst: d.Instance}, ""
}
