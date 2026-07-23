//go:build !leeway_generic

package runtime

import (
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
)

type RecordI interface {
	ReferenceCountingI
	Schema() *arrow.Schema

	NumRows() int64
	NumCols() int64

	Column(i int) arrow.Array
}

type ArrayDataI interface {
	arrow.ArrayData
	//ReferenceCountingI
	//DataType() arrow.DataType
	//Len() int
}

func LoadAccelFieldFromRecord[F, B IndexConstraintI](idx uint32, rec RecordI, dest *RandomAccessTwoLevelLookupAccel[F, B, int, int64]) (err error) {
	c := rec.Column(int(idx))
	if c.DataType().ID() != arrow.LIST {
		err = unexpectedDataTypeE(rec.Schema(), idx, c.DataType(), arrow.LIST)
		return
	}
	d := array.NewListData(c.Data())
	if d.ListValues().DataType().ID() != arrow.UINT64 {
		err = unexpectedDataTypeE(rec.Schema(), idx, d.ListValues().DataType(), arrow.UINT64)
		return
	}
	e := array.NewUint64Data(d.ListValues().Data())
	dest.LoadCardinalities(e.Values())
	dest.SetRanger(d)
	dest.SetReleaser(d)
	return
}
func LoadScalarValueFieldFromRecord[S any](idx uint32, expectedDatatype arrow.Type, rec RecordI, dest **S, ctor func(data arrow.ArrayData) *S) (err error) {
	c := rec.Column(int(idx))
	if c.DataType().ID() != expectedDatatype {
		if expectedDatatype == arrow.BINARY && c.DataType().ID() == arrow.STRING {
		} else {
			err = unexpectedDataTypeE(rec.Schema(), idx, c.DataType(), expectedDatatype)
			return
		}
	}
	*dest = ctor(c.Data())
	return
}
func LoadNonScalarValueFieldFromRecord[S any](idx uint32, expectedDatatype arrow.Type, rec RecordI, dest **array.List, destElementAccess **S, ctorElementAccess func(data arrow.ArrayData) *S) (err error) {
	c := rec.Column(int(idx))
	if c.DataType().ID() != arrow.LIST {
		err = unexpectedDataTypeE(rec.Schema(), idx, c.DataType(), arrow.LIST)
		return
	}
	d := array.NewListData(c.Data())
	if d.ListValues().DataType().ID() != expectedDatatype {
		if expectedDatatype == arrow.BINARY && d.ListValues().DataType().ID() == arrow.STRING {
		} else {
			err = unexpectedDataTypeE(rec.Schema(), idx, d.ListValues().DataType(), expectedDatatype)
			return
		}
	}
	*dest = d
	*destElementAccess = ctorElementAccess(d.ListValues().Data())
	return
}
