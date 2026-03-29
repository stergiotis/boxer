//go:build llm_generated_opus46

package passes_test

import (
	"fmt"
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/marshalling"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- ReadSettings ---

func TestReadSettingsScalars(t *testing.T) {
	settings, err := passes.ReadSettings("SELECT 1 SETTINGS max_threads = 4, timeout = 30")
	require.NoError(t, err)
	assert.Equal(t, uint64(4), settings["max_threads"])
	assert.Equal(t, uint64(30), settings["timeout"])
}

func TestReadSettingsString(t *testing.T) {
	settings, err := passes.ReadSettings("SELECT 1 SETTINGS name = 'hello'")
	require.NoError(t, err)
	assert.Equal(t, "hello", settings["name"])
}

func TestReadSettingsNull(t *testing.T) {
	settings, err := passes.ReadSettings("SELECT 1 SETTINGS val = NULL")
	require.NoError(t, err)
	assert.Nil(t, settings["val"])
}

func TestReadSettingsFloat(t *testing.T) {
	settings, err := passes.ReadSettings("SELECT 1 SETTINGS rate = 0.5")
	require.NoError(t, err)
	assert.Equal(t, 0.5, settings["rate"])
}

func TestReadSettingsArray(t *testing.T) {
	settings, err := passes.ReadSettings("SELECT 1 SETTINGS ids = [1, 2, 3]")
	require.NoError(t, err)
	arr, ok := settings["ids"].([]uint64)
	require.True(t, ok)
	assert.Len(t, arr, 3)
	assert.Equal(t, uint64(1), arr[0])
	assert.Equal(t, uint64(2), arr[1])
	assert.Equal(t, uint64(3), arr[2])
}

func TestReadSettingsEmptyArray(t *testing.T) {
	settings, err := passes.ReadSettings("SELECT 1 SETTINGS ids = array()")
	require.NoError(t, err)
	arr, ok := settings["ids"].([]any)
	require.True(t, ok)
	assert.Empty(t, arr)
}

func TestReadSettingsTuple(t *testing.T) {
	settings, err := passes.ReadSettings("SELECT 1 SETTINGS bounds = tuple(1, 100)")
	require.NoError(t, err)
	tup, ok := settings["bounds"].(*marshalling.Tuple)
	require.True(t, ok)
	assert.Equal(t, 2, tup.Len())
	v0, found := tup.GetByIndex(0)
	assert.True(t, found)
	assert.Equal(t, uint64(1), v0)
	v1, found := tup.GetByIndex(1)
	assert.True(t, found)
	assert.Equal(t, uint64(100), v1)
}

func TestReadSettingsArrayFunction(t *testing.T) {
	settings, err := passes.ReadSettings("SELECT 1 SETTINGS ids = array(1, 2, 3)")
	require.NoError(t, err)
	arr, ok := settings["ids"].([]uint64)
	require.True(t, ok)
	assert.Len(t, arr, 3)
}

func TestReadSettingsTupleFunction(t *testing.T) {
	settings, err := passes.ReadSettings("SELECT 1 SETTINGS bounds = tuple(1, 100)")
	require.NoError(t, err)
	tup, ok := settings["bounds"].(*marshalling.Tuple)
	require.True(t, ok)
	assert.Equal(t, 2, tup.Len())
}

func TestReadSettingsNested(t *testing.T) {
	settings, err := passes.ReadSettings("SELECT 1 SETTINGS a = array(tuple(1, 2), tuple(3, 4))")
	require.NoError(t, err)
	arr, ok := settings["a"].([]any)
	require.True(t, ok)
	assert.Len(t, arr, 2)
	tup0, ok := arr[0].(*marshalling.Tuple)
	require.True(t, ok)
	v, _ := tup0.GetByIndex(0)
	assert.Equal(t, uint64(1), v)
}

func TestReadSettingsNoSettings(t *testing.T) {
	settings, err := passes.ReadSettings("SELECT 1")
	require.NoError(t, err)
	assert.Empty(t, settings)
}

// --- WriteSettings ---

func TestWriteSettingsAdd(t *testing.T) {
	pass := passes.WriteSettings(map[string]any{
		"max_threads": int64(4),
	})
	got, err := pass("SELECT 1")
	require.NoError(t, err)
	assert.Contains(t, got, "SETTINGS max_threads = 4")

	_, err = nanopass.Parse(got)
	require.NoError(t, err)
}

func TestWriteSettingsReplace(t *testing.T) {
	pass := passes.WriteSettings(map[string]any{
		"max_threads": int64(8),
	})
	got, err := pass("SELECT 1 SETTINGS max_threads = 4")
	require.NoError(t, err)
	assert.Contains(t, got, "max_threads = 8")
	assert.NotContains(t, got, "max_threads = 4")

	_, err = nanopass.Parse(got)
	require.NoError(t, err)
}

func TestWriteSettingsRemove(t *testing.T) {
	pass := passes.WriteSettings(map[string]any{})
	got, err := pass("SELECT 1 SETTINGS max_threads = 4")
	require.NoError(t, err)
	assert.NotContains(t, got, "SETTINGS")

	_, err = nanopass.Parse(got)
	require.NoError(t, err)
}

func TestWriteSettingsArray(t *testing.T) {
	pass := passes.WriteSettings(map[string]any{
		"ids": []any{int64(1), int64(2), int64(3)},
	})
	got, err := pass("SELECT 1")
	require.NoError(t, err)
	assert.Contains(t, got, "SETTINGS ids = array(1, 2, 3)")

	_, err = nanopass.Parse(got)
	require.NoError(t, err)
}

func TestWriteSettingsTuple(t *testing.T) {
	pass := passes.WriteSettings(map[string]any{
		"bounds": marshalling.NewUnnamedTuple(int64(0), int64(100)),
	})
	got, err := pass("SELECT 1")
	require.NoError(t, err)
	assert.Contains(t, got, "SETTINGS bounds = tuple(0, 100)")

	_, err = nanopass.Parse(got)
	require.NoError(t, err)
}

func TestWriteSettingsString(t *testing.T) {
	pass := passes.WriteSettings(map[string]any{
		"name": "hello world",
	})
	got, err := pass("SELECT 1")
	require.NoError(t, err)
	assert.Contains(t, got, "name = 'hello world'")

	_, err = nanopass.Parse(got)
	require.NoError(t, err)
}

func TestWriteSettingsStringEscape(t *testing.T) {
	pass := passes.WriteSettings(map[string]any{
		"name": "it's a test",
	})
	got, err := pass("SELECT 1")
	require.NoError(t, err)
	assert.Contains(t, got, "name = 'it\\'s a test'")
}

func TestWriteSettingsNull(t *testing.T) {
	pass := passes.WriteSettings(map[string]any{
		"val": nil,
	})
	got, err := pass("SELECT 1")
	require.NoError(t, err)
	assert.Contains(t, got, "val = NULL")

	_, err = nanopass.Parse(got)
	require.NoError(t, err)
}

func TestWriteSettingsBool(t *testing.T) {
	pass := passes.WriteSettings(map[string]any{
		"flag": true,
	})
	got, err := pass("SELECT 1")
	require.NoError(t, err)
	assert.Contains(t, got, "flag = true")
}

func TestWriteSettingsEmptyArray(t *testing.T) {
	pass := passes.WriteSettings(map[string]any{
		"ids": []any{},
	})
	got, err := pass("SELECT 1")
	require.NoError(t, err)
	assert.Contains(t, got, "ids = array()")

	_, err = nanopass.Parse(got)
	require.NoError(t, err)
}

func TestWriteSettingsNested(t *testing.T) {
	pass := passes.WriteSettings(map[string]any{
		"matrix": []any{
			[]int64{1, 2},
			[]int64{3, 4},
		},
	})
	got, err := pass("SELECT 1")
	require.NoError(t, err)
	assert.Contains(t, got, "matrix = array(array(1, 2), array(3, 4))")

	_, err = nanopass.Parse(got)
	require.NoError(t, err)
}

func TestWriteSettingsMultiple(t *testing.T) {
	pass := passes.WriteSettings(map[string]any{
		"a": int64(1),
		"b": "hello",
		"c": []uint8{1, 2},
	})
	got, err := pass("SELECT 1")
	require.NoError(t, err)
	// Keys are sorted alphabetically
	assert.Contains(t, got, "a = 1")
	assert.Contains(t, got, "b = 'hello'")
	assert.Contains(t, got, "c = array(1, 2)")

	_, err = nanopass.Parse(got)
	require.NoError(t, err)
}

// --- ModifySettings ---

func TestModifySettingsAddKey(t *testing.T) {
	pass := passes.ModifySettings(func(settings map[string]any) error {
		settings["new_key"] = int64(42)
		return nil
	})

	got, err := pass("SELECT 1 SETTINGS existing = 1")
	require.NoError(t, err)
	assert.Contains(t, got, "existing = 1")
	assert.Contains(t, got, "new_key = 42")

	_, err = nanopass.Parse(got)
	require.NoError(t, err)
}

func TestModifySettingsDeleteKey(t *testing.T) {
	pass := passes.ModifySettings(func(settings map[string]any) error {
		delete(settings, "remove_me")
		return nil
	})

	got, err := pass("SELECT 1 SETTINGS keep = 1, remove_me = 2")
	require.NoError(t, err)
	assert.Contains(t, got, "keep = 1")
	assert.NotContains(t, got, "remove_me")

	_, err = nanopass.Parse(got)
	require.NoError(t, err)
}

func TestModifySettingsUpdateValue(t *testing.T) {
	pass := passes.ModifySettings(func(settings map[string]any) error {
		if v, ok := settings["max_threads"]; ok {
			settings["max_threads"] = v.(uint64) * 2
		}
		return nil
	})

	got, err := pass("SELECT 1 SETTINGS max_threads = 4")
	require.NoError(t, err)
	assert.Contains(t, got, "max_threads = 8")

	_, err = nanopass.Parse(got)
	require.NoError(t, err)
}

func TestModifySettingsArrayManipulation(t *testing.T) {
	pass := passes.ModifySettings(func(settings map[string]any) error {
		arr := settings["ids"].([]uint64)
		arr = append(arr, uint64(4))
		settings["ids"] = arr
		return nil
	})

	got, err := pass("SELECT 1 SETTINGS ids = [1, 2, 3]")
	require.NoError(t, err)
	assert.Contains(t, got, "ids = array(1, 2, 3, 4)")

	_, err = nanopass.Parse(got)
	require.NoError(t, err)
}

func TestModifySettingsTupleManipulation(t *testing.T) {
	pass := passes.ModifySettings(func(settings map[string]any) error {
		tup := settings["bounds"].(*marshalling.Tuple)
		tup.SetByIndex(1, int64(200))
		return nil
	})

	got, err := pass("SELECT 1 SETTINGS bounds = tuple(0, 100)")
	require.NoError(t, err)
	assert.Contains(t, got, "bounds = tuple(0, 200)")

	_, err = nanopass.Parse(got)
	require.NoError(t, err)
}

func TestModifySettingsNoExisting(t *testing.T) {
	pass := passes.ModifySettings(func(settings map[string]any) error {
		settings["new_key"] = int64(1)
		return nil
	})

	got, err := pass("SELECT 1")
	require.NoError(t, err)
	assert.Contains(t, got, "SETTINGS new_key = 1")

	_, err = nanopass.Parse(got)
	require.NoError(t, err)
}

func TestModifySettingsClearAll(t *testing.T) {
	pass := passes.ModifySettings(func(settings map[string]any) error {
		for k := range settings {
			delete(settings, k)
		}
		return nil
	})

	got, err := pass("SELECT 1 SETTINGS a = 1, b = 2")
	require.NoError(t, err)
	assert.NotContains(t, got, "SETTINGS")

	_, err = nanopass.Parse(got)
	require.NoError(t, err)
}

// --- Round-trip ---

func TestSettingsRoundTrip(t *testing.T) {
	sqls := []string{
		"SELECT 1 SETTINGS a = 1",
		"SELECT 1 SETTINGS a = 'hello'",
		"SELECT 1 SETTINGS a = [1, 2, 3]",
		"SELECT 1 SETTINGS a = (1, 2)",
		"SELECT 1 SETTINGS a = []",
		"SELECT 1 SETTINGS a = 0.5",
		"SELECT 1 SETTINGS a = NULL",
	}
	for i, sql := range sqls {
		t.Run(fmt.Sprintf("roundtrip_%d", i), func(t *testing.T) {
			settings, err := passes.ReadSettings(sql)
			require.NoError(t, err)

			got, err := passes.WriteSettings(settings)(sql)
			require.NoError(t, err)

			// Read again and compare
			settings2, err := passes.ReadSettings(got)
			require.NoError(t, err)

			for k, v := range settings {
				assert.Equal(t, fmt.Sprintf("%v", v), fmt.Sprintf("%v", settings2[k]),
					"round-trip mismatch for key %q", k)
			}
		})
	}
}

// --- Edge cases ---

func TestReadSettingsRejectsInvalid(t *testing.T) {
	invalid := []string{"", "   ", "SELECT", ";;;"}
	for i, sql := range invalid {
		t.Run(fmt.Sprintf("invalid_%d", i), func(t *testing.T) {
			_, err := passes.ReadSettings(sql)
			assert.Error(t, err)
		})
	}
}

func TestWriteSettingsRejectsInvalid(t *testing.T) {
	pass := passes.WriteSettings(map[string]any{"a": int64(1)})
	invalid := []string{"", "   ", "SELECT", ";;;"}
	for i, sql := range invalid {
		t.Run(fmt.Sprintf("invalid_%d", i), func(t *testing.T) {
			_, err := pass(sql)
			assert.Error(t, err)
		})
	}
}

// --- Serialize ---

func TestSerializeSettingValue(t *testing.T) {
	tests := []struct {
		name     string
		val      any
		expected string
	}{
		{"int64", int64(42), "42"},
		{"uint64", uint64(42), "42"},
		{"float64", 3.14, "3.14"},
		{"string", "hello", "'hello'"},
		{"string_escape", "it's", "'it\\'s'"},
		{"nil", nil, "NULL"},
		{"bool_true", true, "true"},
		{"bool_false", false, "false"},
		{"empty_array", []any{}, "array()"},
		{"int_array", []any{int64(1), int64(2)}, "array(1, 2)"},
		{"string_array", []any{"a", "b"}, "array('a', 'b')"},
		{"nested_array", []any{[]any{int64(1)}, []any{int64(2)}}, "array(array(1), array(2))"},
		{"tuple", marshalling.NewUnnamedTuple(int64(1), int64(2)), "tuple(1, 2)"},
		{"empty_tuple", marshalling.NewUnnamedTuple(), "tuple()"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := marshalling.MarshalGoValueToSQL(tt.val)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)
		})
	}
}
