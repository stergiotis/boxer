//go:build leeway_generic

package runtime

import "github.com/apache/arrow-go/v18/arrow"

type RecordI[C ColumnI[D], D ArrayDataI] interface {
	ReferenceCountingI
	Schema() *arrow.Schema

	NumRows() int64
	NumCols() int64

	Column(i int) C
}

type ColumnI[D ArrayDataI] interface {
	ReferenceCountingI
	DataType() arrow.DataType
	Data() D
	Len() int
}
type ArrayDataI interface {
	arrow.ArrayData
	//ReferenceCountingI
	//DataType() arrow.DataType
	//Len() int
}
func LoadAccelFieldFromRecord[F, B IndexConstraintI, C ColumnI[D], D ArrayDataI](idx uint32, rec RecordI[C, D], dest *RandomAccessTwoLevelLookupAccel[F, B, int, int64]) (err error) {
	c := rec.Column(int(idx))
	if c.DataType().ID() != arrow.LIST {
		err = eb.Build().Uint32("columnIndex", idx).Stringer("effective", c.DataType()).Stringer("expected", arrow.LIST).Errorf("unexpected data type: %w", ErrUnexpectedArrowDataType)
		return
	}
	d := array.NewListData(c.Data())
	if d.ListValues().DataType().ID() != arrow.UINT64 {
		err = eb.Build().Uint32("columnIndex", idx).Stringer("effective", c.DataType()).Stringer("expected", arrow.UINT64).Errorf("unexpected data type: %w", ErrUnexpectedArrowDataType)
		return
	}
	e := array.NewUint64Data(d.ListValues().Data())
	dest.LoadCardinalities(e.Values())
	dest.SetRanger(d)
	dest.SetReleaser(d)
	return
}
func LoadScalarValueFieldFromRecord[S any, C ColumnI[D], D ArrayDataI](idx uint32, expectedDatatype arrow.Type, rec RecordI[C, D], dest **S, ctor func(data arrow.ArrayData) *S) (err error) {
	c := rec.Column(int(idx))
	if c.DataType().ID() != expectedDatatype {
		if expectedDatatype == arrow.BINARY && c.DataType().ID() == arrow.STRING {
		} else {
			err = eb.Build().Uint32("columnIndex", idx).Stringer("effective", c.DataType()).Stringer("expected", expectedDatatype).Errorf("unexpected data type: %w", ErrUnexpectedArrowDataType)
			return
		}
	}
	*dest = ctor(c.Data())
	return
}
func LoadNonScalarValueFieldFromRecord[S any, C ColumnI[D], D ArrayDataI](idx uint32, expectedDatatype arrow.Type, rec RecordI[C, D], dest **array.List, destElementAccess **S, ctorElementAccess func(data arrow.ArrayData) *S) (err error) {
	c := rec.Column(int(idx))
	if c.DataType().ID() != arrow.LIST {
		err = eb.Build().Uint32("columnIndex", idx).Stringer("effective", c.DataType()).Stringer("expected", arrow.LIST).Errorf("unexpected data type: %w", ErrUnexpectedArrowDataType)
		return
	}
	d := array.NewListData(c.Data())
	if d.ListValues().DataType().ID() != expectedDatatype {
		if expectedDatatype == arrow.BINARY && d.ListValues().DataType().ID() == arrow.STRING {
		} else {
			err = eb.Build().Uint32("columnIndex", idx).Stringer("effective", c.DataType()).Stringer("expected", expectedDatatype).Errorf("unexpected data type: %w", ErrUnexpectedArrowDataType)
			return
		}
	}
	*dest = d
	*destElementAccess = ctorElementAccess(d.ListValues().Data())
	return
}
