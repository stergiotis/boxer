package play

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsql"
)

// condTestClient is a Client with the selection-condition rewrite realised against a
// static schema, standing in for what installLeewayNameResolution builds from
// the live system.columns probe.
func condTestClient(t *testing.T) *Client {
	t.Helper()
	provider := passes.NewStaticSchemaProvider(map[string][]string{
		"tt":      {"a", "b", "c"},
		"collide": {"a", "c", "cond_1"},
	})
	client := NewClient(ClientConfig{URL: "http://example.invalid"}, nil)
	resolver := lwsql.NewResolver(provider)
	client.passBinding = &clientPassBinding{Resolver: resolver, SchemaProviderI: provider}
	client.conditionsPass = passes.ExposeSelectionConditions(passes.ExposeSelectionConditionsConfig{
		Schema: provider,
		Namer:  resolver,
	})
	return client
}

// TestExposeConditionsToggle is ADR-0121 §SD7's load-bearing claim: the rewrite is
// off until the top-bar toggle turns it on, and when on it reaches the SQL that
// actually ships.
func TestExposeConditionsToggle(t *testing.T) {
	client := condTestClient(t)
	const q = "SELECT a FROM tt WHERE c = 1"

	// Default off: the query ships exactly as written.
	require.False(t, client.ExposeConditions(), "the rewrite must default to off")
	residual, _ := client.buildResidual(q)
	require.Equal(t, q, residual, "an untoggled client must not rewrite")

	client.SetExposeConditions(true)
	require.True(t, client.ExposeConditions())
	residual, _ = client.buildResidual(q)
	require.Equal(t, "SELECT a, (c = 1) AS cond_1 FROM tt WHERE cond_1", residual)

	// And back off again — the toggle is not one-way.
	client.SetExposeConditions(false)
	residual, _ = client.buildResidual(q)
	require.Equal(t, q, residual)
}

// TestExposeConditionsToggleGroupsConjunction pins the SD1 granularity through the
// host: an OR-free predicate is one condition, the disjuncts of an OR are not.
func TestExposeConditionsToggleGroupsConjunction(t *testing.T) {
	client := condTestClient(t)
	client.SetExposeConditions(true)

	residual, _ := client.buildResidual("SELECT a FROM tt WHERE a = 1 AND b = 2")
	require.Equal(t, "SELECT a, (a = 1 AND b = 2) AS cond_1 FROM tt WHERE cond_1", residual)

	residual, _ = client.buildResidual("SELECT a FROM tt WHERE (a = 1 AND b = 2) OR c = 3")
	require.Equal(t, "SELECT a, (a = 1 AND b = 2) AS cond_1, (c = 3) AS cond_2 FROM tt WHERE cond_1 OR cond_2", residual)
}

// TestExposeConditionsRefusalShipsOriginal is the best-effort contract: a condition
// name colliding with a real column (§SD4) fails the pass, and the client sends
// the query as the user wrote it rather than failing the Run.
func TestExposeConditionsRefusalShipsOriginal(t *testing.T) {
	client := condTestClient(t)
	client.SetExposeConditions(true)

	const q = "SELECT a FROM collide WHERE c = 1"
	residual, _ := client.buildResidual(q)
	require.Equal(t, q, residual, "a refused rewrite must ship the original query")
}

// TestExposeConditionsUninstalledIsInert guards the nil case: a Client whose
// schema resolution was never installed (no endpoint) has no pass, so the
// toggle must do nothing rather than panic.
func TestExposeConditionsUninstalledIsInert(t *testing.T) {
	client := NewClient(ClientConfig{URL: "http://example.invalid"}, nil)
	client.SetExposeConditions(true)

	const q = "SELECT a FROM tt WHERE c = 1"
	residual, _ := client.buildResidual(q)
	require.Equal(t, q, residual)
}
