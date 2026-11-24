package eh_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

var errTextBase = errors.New("the cell is closed")

func TestTextRendersTheMessageAndEveryFact(t *testing.T) {
	inner := eb.Build().Str("path", "/var/a b").Int("kid", 42).Errorf("open the store: %w", errTextBase)
	outer := eb.Build().Str("cell", "c1").Int("kid", 7).Strs("tags", []string{"x", "y z"}).Errorf("start: %w", inner)

	assert.Equal(t, `start: open the store: the cell is closed [cell=c1 kid=7 path="/var/a b" tags=[x,"y z"]]`, eh.Text(outer),
		"keys sorted, the outer kid wins, a value with a space quoted")
}

func TestTextOfAnErrorWithoutFacts(t *testing.T) {
	assert.Equal(t, "the cell is closed", eh.Text(errTextBase))
	assert.Equal(t, "wrap: the cell is closed", eh.Text(eh.Errorf("wrap: %w", errTextBase)))
	assert.Empty(t, eh.Text(nil))
}

func TestTextWalksJoinedErrors(t *testing.T) {
	a := eb.Build().Str("a", "1").Errorf("first")
	b := eb.Build().Bool("b", true).Str("empty", "").Errorf("second")
	assert.Equal(t, "first\nsecond [a=1 b=true empty=\"\"]", eh.Text(errors.Join(a, b)))
}
