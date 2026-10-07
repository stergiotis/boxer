package play

import (
	"regexp"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// Every operation the subset names is in play's catalog, and every pane
// operation of play's is offered or excluded on purpose (ADR-0288
// (proposed) §SD8).
func TestOperableSubsetCoversEveryPaneOperation(t *testing.T) {
	catalog := playOps.Catalog()
	names := make([]string, 0, len(catalog.Operations))
	for _, o := range catalog.Operations {
		names = append(names, o.Name)
	}
	listed := slices.Clone(operableCommon)
	for _, ops := range operablePanes {
		listed = append(listed, ops...)
	}
	for _, n := range slices.Concat(listed, operableExcluded) {
		assert.Contains(t, names, n, "the subset names an operation play does not declare")
	}
	panes := []string{detailPaneId}
	for pane := range operablePanes {
		panes = append(panes, pane)
	}
	for _, pane := range panes {
		re := regexp.MustCompile(`^(get|set|select)_` + pane + `(_|$)`)
		for _, n := range names {
			if re.MatchString(n) {
				assert.True(t, slices.Contains(listed, n) || slices.Contains(operableExcluded, n),
					"%s is a %s pane operation neither offered nor excluded", n, pane)
			}
		}
	}
}

func TestOperableOperationsAreTheSubset(t *testing.T) {
	ops := OperableOperations()
	var names []string
	for _, o := range ops {
		names = append(names, o.Spec.Name)
		assert.NotEqual(t, app.OperationEffectConsequential, o.Spec.Effect, "nothing consequential is offered")
		assert.NotEqual(t, app.OperationClassExternalRead, o.Spec.Class, "a view serves commands and queries")
	}
	assert.Subset(t, names, operableCommon)
	assert.NotContains(t, names, opSetSql)
	assert.NotContains(t, names, opPublishResult)
	assert.NotContains(t, names, opBindDataset)
	for _, o := range ops {
		if o.Spec.Name == opSetChartOptions {
			assert.Equal(t, chartPaneId, o.Pane)
		}
	}
	res := OperableResources()
	require.NotEmpty(t, res)
	var resNames []string
	for _, r := range res {
		resNames = append(resNames, r.Name)
	}
	assert.Contains(t, resNames, opsResParams)
	assert.Contains(t, resNames, opsResResult)
}
