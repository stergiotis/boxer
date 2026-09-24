package chrows

import (
	"bytes"
	"io"
	"reflect"

	"github.com/apache/arrow-go/v18/arrow"
)

// DecodeRows decodes rec into one T per row, T being a row struct (see
// [TagKey]). It binds and reads exactly as [Decode] does — the same
// refusals, the same copies — and differs only in where a value lands.
// A column struct is the shape to reach for first; a row struct is for a
// caller whose rows are the unit it works in.
func DecodeRows[T any](rec arrow.RecordBatch) (rows []T, err error) {
	p, err := planOf(reflect.TypeFor[T](), shapeRow)
	if err != nil {
		return
	}
	bs, err := p.bind(rec.Schema())
	if err != nil {
		return
	}
	return decodeRows(rows, bs, rec)
}

// DecodeRowsStream is [DecodeRows] over an Arrow IPC stream, every batch
// in turn.
func DecodeRowsStream[T any](r io.Reader) (rows []T, err error) {
	p, err := planOf(reflect.TypeFor[T](), shapeRow)
	if err != nil {
		return
	}
	err = eachBatch(r, p, func(bs []binding, rec arrow.RecordBatch) (err error) {
		rows, err = decodeRows(rows, bs, rec)
		return
	})
	return
}

// DecodeRowsBytes is [DecodeRowsStream] over a body already in memory.
func DecodeRowsBytes[T any](body []byte) (rows []T, err error) {
	return DecodeRowsStream[T](bytes.NewReader(body))
}

func decodeRows[T any](rows []T, bs []binding, rec arrow.RecordBatch) (out []T, err error) {
	n := int(rec.NumRows())
	base := len(rows)
	out = append(rows, make([]T, n)...)
	sv := reflect.ValueOf(out)
	for _, b := range bs {
		if b.col < 0 {
			continue
		}
		arr := rec.Column(b.col)
		if b.valid {
			for r := range n {
				sv.Index(base + r).Field(b.index).SetBool(arr.IsValid(r))
			}
			continue
		}
		if err = checkNulls(b, arr); err != nil {
			return rows, err
		}
		for r := range n {
			if err = setCell(sv.Index(base+r).Field(b.index), b, arr, r); err != nil {
				return rows, err
			}
		}
	}
	return
}
