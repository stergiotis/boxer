package ebtest_test

import (
	"testing"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/observability/eh/eb/ebtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFieldsReadsScalars(t *testing.T) {
	err := eb.Build().
		Str("path", "/etc/hosts").
		Int("attempt", 3).
		Bool("retryable", true).
		Errorf("open: %w", eh.New("boom"))

	f := ebtest.Fields(t, err)
	assert.Equal(t, "/etc/hosts", f["path"])
	assert.EqualValues(t, 3, f["attempt"])
	assert.Equal(t, true, f["retryable"])
}

func TestFieldsReadsThroughAWrapChain(t *testing.T) {
	inner := eb.Build().Str("table", "facts").Errorf("scan failed")
	outer := eb.Build().Str("query", "SELECT 1").Errorf("run: %w", inner)

	f := ebtest.Fields(t, outer)
	assert.Equal(t, "SELECT 1", f["query"], "outer field")
	assert.Equal(t, "facts", f["table"], "inner field, reached through the wrap")
}

func TestFieldsPrefersTheOutermostOnAKeyCollision(t *testing.T) {
	inner := eb.Build().Str("stage", "inner").Errorf("boom")
	outer := eb.Build().Str("stage", "outer").Errorf("wrap: %w", inner)

	assert.Equal(t, "outer", ebtest.Fields(t, outer)["stage"])

	levels := ebtest.FieldsByLevel(t, outer)
	require.Len(t, levels, 2, "one map per payload-carrying error")
	assert.Equal(t, "outer", levels[0]["stage"])
	assert.Equal(t, "inner", levels[1]["stage"])
}

func TestFieldsSeesAnEmptyBuilderAsAPayload(t *testing.T) {
	// eb.Build() with no fields still writes an empty map, so the error is
	// payload-carrying and Fields must not report it as missing.
	err := eb.Build().Errorf("nothing to say")
	assert.Empty(t, ebtest.Fields(t, err))
}

func TestFieldsReachesAPayloadUnderAPlainWrap(t *testing.T) {
	// eh.Errorf carries no fields of its own; the payload below it must still
	// be found.
	inner := eb.Build().Str("id", "abc").Errorf("inner")
	outer := eh.Errorf("outer: %w", inner)

	assert.Equal(t, "abc", ebtest.Fields(t, outer)["id"])
}

func TestTextCarriesMessageAndFields(t *testing.T) {
	err := eb.Build().Str("clause", "ORDER BY").Int("limit", 5).Errorf("clause not supported")
	got := ebtest.Text(t, err)
	assert.Contains(t, got, "clause not supported", "the message")
	assert.Contains(t, got, "ORDER BY", "a field value")
	assert.Contains(t, got, "limit", "a field name")
}

func TestTextOnAnErrorWithoutAPayload(t *testing.T) {
	// eh.Errorf carries no fields; Text is still usable, unlike Fields.
	assert.Equal(t, "plain", ebtest.Text(t, eh.New("plain")))
}
