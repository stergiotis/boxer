package marshallreflect_test

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	anchor "github.com/stergiotis/boxer/public/semistructured/leeway/anchor"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallreflect"
)

// badScalarOnU32Array binds a scalar-shaped field to anchor's u32Array
// section, whose container-shaped BeginAttribute() takes no arguments. The
// plan is valid (a plan does not know physical section shapes); the mismatch
// must surface in Validate's aggregated error. Previously scalar-section
// BeginAttribute arity went unchecked and the mismatch panicked mid-Marshal
// via mustCall (ADR-0113 review fallout).
type badScalarOnU32Array struct {
	_        struct{} `kind:"badScalarOnU32Array"`
	ID       uint64   `lw:",id"`
	Tracking []byte   `lw:",naturalKey"`
	Code     uint32   `lw:"code,u32Array"`
}

func TestValidate_ScalarArityAgainstContainerSection(t *testing.T) {
	err := marshallreflect.Validate[badScalarOnU32Array](anchor.NewInEntityTestTable(memory.NewGoAllocator(), 1))
	require.Error(t, err)
	require.ErrorContains(t, err, "BeginAttribute takes 0 arg(s), want 1")
}
