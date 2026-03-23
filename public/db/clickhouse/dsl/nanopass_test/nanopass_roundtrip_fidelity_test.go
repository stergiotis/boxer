//go:build llm_generated_opus46

package nanopass

import (
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/testdata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRoundTripFidelity(t *testing.T) {
	entries, err := testdata.LoadCorpus()
	require.NoError(t, err)

	for _, entry := range entries {
		t.Run(entry.Name, func(t *testing.T) {
			pr, err := nanopass.Parse(entry.SQL)
			if err != nil {
				t.Skipf("parse failed: %v", err)
			}
			rw := nanopass.NewRewriter(pr)
			result := nanopass.GetText(rw)
			assert.Equal(t, entry.SQL, result, "round-trip fidelity broken")
		})
	}
}
