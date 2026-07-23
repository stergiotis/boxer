package runtime

import (
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stergiotis/boxer/public/generic"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

var AccelEstimatedInitialLength = 128

var ErrUnexpectedArrowDataType = eh.Errorf("unexpected arrow data type")

// unexpectedDataTypeE reports a column whose Arrow type does not match what the
// generated read access expects at that position.
//
// The column name, the index and both types go into the message, not only into
// structured fields. eb fields reach log sinks; the consumers that surface this
// to a human — the facts viewer's detail pane, a CLI — render Error(), and eb
// exposes no way to read the fields back, so a caller cannot reconstruct them.
// Without them the message is a bare "unexpected data type" that names neither
// the column nor what was wrong with it.
//
// The name matters more than the index here. Read access binds by position, so
// the usual cause is a projection that is not a plain `SELECT *` — one extra
// expression before the table's columns shifts every one of them, and seeing
// which column landed in the slot is what makes that obvious.
func unexpectedDataTypeE(schema *arrow.Schema, idx uint32, effective arrow.DataType, expected arrow.Type) (err error) {
	name := "<unknown>"
	if schema != nil && int(idx) < schema.NumFields() {
		name = schema.Field(int(idx)).Name
	}
	// Both sides are named by their arrow.Type so they compare like with like —
	// DataType.String() renders "utf8" where Type.String() renders "STRING",
	// and the two side by side read as unrelated. The full type follows in
	// parentheses because it carries the element type of a list, which is
	// often the part that is actually wrong.
	err = eb.Build().Uint32("columnIndex", idx).Str("columnName", name).
		Stringer("effective", effective).Stringer("expected", expected).
		Errorf("unexpected data type for column %d %q: got %s (%s), want %s: %w",
			idx, name, effective.ID(), effective, expected, ErrUnexpectedArrowDataType)
	return
}

func ReleaseIfNotNil[T ReleasableI](a T) {
	if !generic.IsNil(a) {
		a.Release()
	}
}
