package runtime

import (
	"slices"

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
func LookupPhysicalColumnIndex(physicalColumnNames []string, name string) (index uint32, err error) {
	idx := slices.Index(physicalColumnNames, name)
	if idx < 0 {
		err = eb.Build().Strs("physicalColumnNames", physicalColumnNames).Str("name", name).Errorf("unable to find column index for given physical column name")
		return
	}
	index = uint32(idx)
	return
}
