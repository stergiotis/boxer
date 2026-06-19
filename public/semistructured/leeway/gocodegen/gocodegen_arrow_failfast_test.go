package gocodegen

import (
	"testing"

	"github.com/stretchr/testify/require"

	ct "github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
)

// TestArrowConversions_ZonedTemporalFailFast pins the fail-fast for zoned
// temporal types. ZonedDatetime was silently mapped to a UTC Timestamp (zone
// dropped); now every Arrow⇄Go conversion errors on it, matching ZonedTime and
// the canonicaltypes codegen. UtcDatetime — the only supported temporal — stays
// fine.
func TestArrowConversions_ZonedTemporalFailFast(t *testing.T) {
	zonedDT := ct.TemporalTypeAstNode{BaseType: ct.BaseTypeTemporalZonedDatetime, Width: 64}
	zonedT := ct.TemporalTypeAstNode{BaseType: ct.BaseTypeTemporalZonedTime, Width: 64}
	utc := ct.TemporalTypeAstNode{BaseType: ct.BaseTypeTemporalUtcDatetime, Width: 64}
	hints := encodingaspects.EmptyAspectSet

	for _, zoned := range []ct.TemporalTypeAstNode{zonedDT, zonedT} {
		_, _, err := ArrowTypeToGoType(zoned, hints, false)
		require.Error(t, err, "ArrowTypeToGoType %s", zoned.BaseType)
		_, _, err = GoTypeToArrowType(zoned, hints, false)
		require.Error(t, err, "GoTypeToArrowType %s", zoned.BaseType)
		_, _, err = CanonicalTypeToArrowBaseClassName(zoned, hints, false)
		require.Error(t, err, "CanonicalTypeToArrowBaseClassName %s", zoned.BaseType)
	}

	// UtcDatetime still converts.
	name, _, err := CanonicalTypeToArrowBaseClassName(utc, hints, false)
	require.NoError(t, err)
	require.Equal(t, "Timestamp", name)
}
