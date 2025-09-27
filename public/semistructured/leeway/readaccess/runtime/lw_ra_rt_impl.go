package runtime

import (
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/stergiotis/boxer/public/generic"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

var ErrUnexpectedArrowDataType = eh.Errorf("unexpected arrow data type")

func ReleaseIfNotNil[T ReleasableI](a T) {
	if !generic.IsNil(a) {
		a.Release()
	}
}
func LoadAccelFieldFromRecord[F, B IndexConstraintI](idx int, rec arrow.Record, dest *RandomAccessTwoLevelLookupAccel[F, B, int, int64]) (err error) {
	c := rec.Column(int(idx))
	if c.DataType().ID() != arrow.LIST {
		err = eb.Build().Int("columnIndex", idx).Stringer("effective", c.DataType()).Stringer("expected", arrow.LIST).Errorf("unexpected data type: %w", ErrUnexpectedArrowDataType)
		return
	}
	d := array.NewListData(c.Data())
	if d.ListValues().DataType().ID() != arrow.UINT64 {
		err = eb.Build().Int("columnIndex", idx).Stringer("effective", c.DataType()).Stringer("expected", arrow.UINT64).Errorf("unexpected data type: %w", ErrUnexpectedArrowDataType)
		return
	}
	e := array.NewUint64Data(d.ListValues().Data())
	dest.LoadCardinalities(e.Values())
	dest.SetRanger(d)
	//.releasable = append(.releasable, d, e)
	return
}
func LoadScalarValueFieldFromRecord[S any](idx int, expectedDatatype arrow.Type, rec arrow.Record, dest **S, ctor func(data arrow.ArrayData) *S) (err error) {
	c := rec.Column(idx)
	if c.DataType().ID() != expectedDatatype {
		err = eb.Build().Int("columnIndex", idx).Stringer("effective", c.DataType()).Stringer("expected", expectedDatatype).Errorf("unexpected data type: %w", ErrUnexpectedArrowDataType)
		return
	}
	*dest = ctor(c.Data())
	return
}
func LoadNonScalarValueFieldFromRecord[S any](idx int, expectedDatatype arrow.Type, rec arrow.Record, dest **array.List, destElementAccess **S, ctorElementAccess func(data arrow.ArrayData) *S) (err error) {
	c := rec.Column(idx)
	if c.DataType().ID() != arrow.LIST {
		err = eb.Build().Int("columnIndex", idx).Stringer("effective", c.DataType()).Stringer("expected", arrow.LIST).Errorf("unexpected data type: %w", ErrUnexpectedArrowDataType)
		return
	}
	d := array.NewListData(c.Data())
	if d.ListValues().DataType().ID() != expectedDatatype {
		err = eb.Build().Int("columnIndex", idx).Stringer("effective", c.DataType()).Stringer("expected", expectedDatatype).Errorf("unexpected data type: %w", ErrUnexpectedArrowDataType)
		return
	}
	*dest = d
	*destElementAccess = ctorElementAccess(d.ListValues().Data())
	return
}
