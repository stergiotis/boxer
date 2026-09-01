package runtime

import (
	"fmt"
	"strings"

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
	err = eb.Build().Uint32("columnIndex", idx).Str("columnName", name).Stringer("effective", effective).Stringer("expected", expected).Errorf("unexpected data type for column %d %q: got %s (%s), want %s: %w", idx, name, effective.ID(), effective, expected, ErrUnexpectedArrowDataType) //boxer:lint disable=CS013 reason="the type pair and column name are composed to read side by side; the comment above says why, and the test asserts it"
	return
}

func ReleaseIfNotNil[T ReleasableI](a T) {
	if !generic.IsNil(a) {
		a.Release()
	}
}

var ErrColumnIndexOutOfRange = eh.Errorf("column index out of range")

// recordShapeI is the part of a record the index guard needs. Both RecordI
// variants — the non-generic one and the type-parameterised leeway_generic one —
// carry these two methods, so the guard is written once here rather than twice
// behind the build tags.
type recordShapeI interface {
	Schema() *arrow.Schema
	NumCols() int64
}

// outOfRangeColumnNames caps how many of the record's column names go into the
// message. Enough to recognise the record, short enough to stay readable in a
// GUI label or a CLI line.
const outOfRangeColumnNames = 8

// checkColumnIndexE guards the positional column lookup the generated read
// access performs.
//
// Read access binds by position, so a record narrower than the table it was
// generated for has no column at the index at all — arrow's Record.Column
// indexes a slice and panics rather than reporting it. That is not a corrupt
// program: a record reaches read access straight from whatever query a person
// typed, and `SELECT count() FROM facts11` is one column wide. It has to come
// back as an error so the callers that already handle unexpectedDataTypeE — the
// facts viewer's detail pane falls back to its generic renderer — handle this
// the same way instead of taking the process down.
//
// The record's own column names go into the message alongside the count, for
// the same reason unexpectedDataTypeE names the column it found: seeing
// "count()" in a record read access expected to be facts11-shaped is what makes
// the mismatch obvious, and a caller cannot read eb's fields back.
func checkColumnIndexE(rec recordShapeI, idx uint32) (err error) {
	n := rec.NumCols()
	if int64(idx) < n {
		return
	}
	var names []string
	if schema := rec.Schema(); schema != nil {
		for i := 0; i < schema.NumFields() && i < outOfRangeColumnNames; i++ {
			names = append(names, schema.Field(i).Name)
		}
	}
	// "1 column", "3 columns" — the message is read by people, and a trailing
	// "(s)" in a GUI label reads as an unfinished string.
	have := fmt.Sprintf("%d columns", n)
	if n == 1 {
		have = "1 column"
	}
	if len(names) > 0 {
		rendered := strings.Join(names, ", ")
		if int64(len(names)) < n {
			rendered += ", …"
		}
		have += " (" + rendered + ")"
	}
	err = eb.Build().Uint32("columnIndex", idx).Int64("numColumns", n).Strs("columnNames", names).Errorf("read access binds column %d but the record has only %s: %w", idx, have, ErrColumnIndexOutOfRange) //boxer:lint disable=CS013 reason="the record's own column list is what makes the mismatch obvious; the test asserts it"
	return
}
