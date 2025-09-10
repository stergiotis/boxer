package runtime

import (
	"github.com/stergiotis/boxer/public/generic"
	"github.com/stergiotis/boxer/public/observability/eh"
)

var ErrUnexpectedArrowDataType = eh.Errorf("unexpected arrow data type")

func ReleaseIfNotNil[T ReleasableI](a T) {
	if !generic.IsNil(a) {
		a.Release()
	}
}
