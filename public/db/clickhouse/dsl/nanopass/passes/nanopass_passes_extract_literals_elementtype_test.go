package passes

import (
	"strings"
	"testing"
)

// TestExtractLiterals_MixedNumericListElementType guards a bug where a list
// whose elements inferred to different numeric types (1 is UInt64, -2 is
// Int64) fell back to element type String, binding the unquoted values
// [1, -2, 3] to an Array(String) parameter ClickHouse cannot parse.
func TestExtractLiterals_MixedNumericListElementType(t *testing.T) {
	config := NewExtractLiteralsConfig(100)
	config.SetUseSequentialNames(true)
	config.SetMinINListSize(3)
	pass := ExtractLiterals(config)
	for sql, want := range map[string]string{
		"SELECT 1 FROM t WHERE x IN (1, -2, 3)":  "Array(Int64)",
		"SELECT has(array(1, -2, 3), x) FROM t":  "Array(Int64)",
		"SELECT 1 FROM t WHERE x IN (1, 2.5, 3)": "Array(Float64)",
	} {
		got, err := pass.Run(sql)
		if err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		if !strings.Contains(got, want) || strings.Contains(got, "Array(String)") {
			t.Errorf("%s:\n got %s\nwant a %s slot", sql, got, want)
		}
	}
}

// TestExtractLiterals_IncompatibleListLeftAlone checks that a list with no
// common element type is not collapsed into an Array parameter.
func TestExtractLiterals_IncompatibleListLeftAlone(t *testing.T) {
	config := NewExtractLiteralsConfig(100)
	config.SetMinINListSize(3)
	pass := ExtractLiterals(config)
	sql := "SELECT 1 FROM t WHERE x IN (1, 'a', 3)"
	got, err := pass.Run(sql)
	if err != nil {
		t.Fatal(err)
	}
	if got != sql {
		t.Errorf("got %s, want it unchanged", got)
	}
}
