package chversion_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/chversion"
)

func TestParse(t *testing.T) {
	for in, want := range map[string]chversion.Version{
		"26.9.13.15": {26, 9, 13, 15},
		"26.8\n":     {26, 8, 0, 0},
		"ClickHouse local version 26.9.13.15 (official build).": {26, 9, 13, 15},
		"25.3.2.1-lts": {25, 3, 2, 1},
	} {
		got, err := chversion.Parse(in)
		require.NoError(t, err, in)
		require.Equal(t, want, got, in)
	}
	for _, in := range []string{"", "no digits", "26", "v."} {
		_, err := chversion.Parse(in)
		require.Error(t, err, in)
	}
}

func TestCompareAndString(t *testing.T) {
	a := chversion.Version{Major: 26, Minor: 8}
	require.Zero(t, a.Compare(chversion.FloorVersion()))
	require.Negative(t, chversion.Version{Major: 26, Minor: 7, Patch: 99}.Compare(a))
	require.Positive(t, chversion.Version{Major: 26, Minor: 8, Build: 1}.Compare(a))
	require.Positive(t, chversion.Version{Major: 27}.Compare(a))
	require.Equal(t, "26.8", a.String())
	require.Equal(t, "26.9.13.15", chversion.Version{26, 9, 13, 15}.String())
	require.Equal(t, "26.9.1", chversion.Version{26, 9, 1, 0}.String())
}

func TestCheck(t *testing.T) {
	require.NoError(t, chversion.Check("server", "26.8.1.1"))
	require.NoError(t, chversion.Check("server", "27.1"))
	err := chversion.Check("server", "26.7.3.4")
	require.ErrorContains(t, err, chversion.Floor)
	require.ErrorContains(t, err, "X-ClickHouse-Format")
	require.Error(t, chversion.Check("server", "garbage"))
}
