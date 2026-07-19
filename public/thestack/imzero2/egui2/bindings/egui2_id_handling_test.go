package bindings

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestWidgetIdStackBaseSalt pins the SetBaseSalt contract: the salt shifts
// every derived id, survives Reset (unlike a pushed scope), and zero keeps
// the unsalted behaviour byte-identical.
func TestWidgetIdStackBaseSalt(t *testing.T) {
	unsalted := NewWidgetIdStack()
	a := NewWidgetIdStack()
	a.SetBaseSalt(0x1111)
	b := NewWidgetIdStack()
	b.SetBaseSalt(0x2222)

	idPlain := unsalted.PrepareStr("topbar").Derive()
	idA := a.PrepareStr("topbar").Derive()
	idB := b.PrepareStr("topbar").Derive()
	assert.NotEqual(t, idPlain, idA)
	assert.NotEqual(t, idPlain, idB)
	assert.NotEqual(t, idA, idB, "same label under different salts must differ")
	assert.Equal(t, idPlain^0x1111, idA, "the salt is the empty stack's XOR base")

	// A zero salt is the legacy behaviour.
	z := NewWidgetIdStack()
	z.SetBaseSalt(0)
	assert.Equal(t, idPlain, z.PrepareStr("topbar").Derive())

	// Reset clears pushed scopes but keeps the base salt.
	scoped := a.PrepareStr("scope").DeriveStacked()
	assert.NotZero(t, scoped)
	a.Reset()
	assert.Equal(t, idA, a.PrepareStr("topbar").Derive(), "salt must survive Reset")

	// Scopes compose on top of the salt and pop back to it.
	base := b.PrepareStr("scope").DeriveStacked()
	inScope := b.PrepareStr("leaf").Derive()
	assert.Equal(t, base^unsalted.PrepareStr("leaf").Derive(), inScope)
	b.PopIdFromStackChecked(base)
	assert.Equal(t, idB, b.PrepareStr("topbar").Derive())
}
