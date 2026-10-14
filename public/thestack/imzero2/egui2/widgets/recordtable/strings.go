package recordtable

import (
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// StringRecord holds rows of text as a record with one string column per
// header, for a host whose rows are already formatted. A row shorter than the
// headers is padded with empty cells. Sorting such a record compares text, so
// a column of formatted numbers or sizes ("9", "10", "1.2 GiB") does not sort
// as numbers; a host that wants that keeps the typed record instead, or marks
// the column NoSort. The caller releases the record.
func StringRecord(headers []string, rows [][]string) (rec arrow.RecordBatch) {
	fields := make([]arrow.Field, len(headers))
	for i, h := range headers {
		fields[i] = arrow.Field{Name: h, Type: arrow.BinaryTypes.String}
	}
	mem := memory.DefaultAllocator
	arrs := make([]arrow.Array, len(headers))
	for ci := range headers {
		b := array.NewStringBuilder(mem)
		for _, row := range rows {
			if ci < len(row) {
				b.Append(row[ci])
			} else {
				b.Append("")
			}
		}
		arrs[ci] = b.NewArray()
		b.Release()
	}
	rec = array.NewRecordBatch(arrow.NewSchema(fields, nil), arrs, int64(len(rows)))
	for _, a := range arrs {
		a.Release()
	}
	return
}
