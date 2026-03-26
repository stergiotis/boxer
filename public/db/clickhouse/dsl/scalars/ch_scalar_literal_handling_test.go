//go:build llm_generated_opus46

package scalars

import (
	"math"
	"testing"
)

func TestUnescapeString(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		// Basic strings
		{name: "empty", input: "''", want: ""},
		{name: "simple", input: "'hello'", want: "hello"},
		{name: "spaces", input: "'hello world'", want: "hello world"},

		// Backslash escapes
		{name: "escaped_backslash", input: `'a\\b'`, want: "a\\b"},
		{name: "escaped_quote", input: `'it\'s'`, want: "it's"},
		{name: "newline", input: `'line1\nline2'`, want: "line1\nline2"},
		{name: "tab", input: `'col1\tcol2'`, want: "col1\tcol2"},
		{name: "carriage_return", input: `'a\rb'`, want: "a\rb"},
		{name: "null_byte", input: `'a\0b'`, want: "a\x00b"},
		{name: "backspace", input: `'a\bb'`, want: "a\bb"},
		{name: "formfeed", input: `'a\fb'`, want: "a\fb"},
		{name: "bell", input: `'a\ab'`, want: "a\ab"},
		{name: "vertical_tab", input: `'a\vb'`, want: "a\vb"},

		// Hex escape
		{name: "hex_escape", input: `'\x41\x42'`, want: "AB"},
		{name: "hex_escape_ff", input: `'\xff'`, want: "\xff"},

		// Unicode escapes
		{name: "unicode_u", input: `'\u0041'`, want: "A"},
		{name: "unicode_u_emoji_bmp", input: `'\u00e9'`, want: "é"},
		{name: "unicode_U", input: `'\U0001F600'`, want: "😀"},

		// Doubled single-quote
		{name: "doubled_quote", input: "'''hello'''", want: "'hello'"},
		{name: "doubled_quote_middle", input: "'it''s a test'", want: "it's a test"},

		// Unknown escape (backslash dropped)
		{name: "unknown_escape", input: `'\q'`, want: "q"},

		// Mixed
		{name: "mixed_escapes", input: `'a\tb\nc\\d''e'`, want: "a\tb\nc\\d'e"},

		// Errors
		{name: "no_quotes", input: "hello", wantErr: true},
		{name: "single_char", input: "'", wantErr: true},
		{name: "double_quotes", input: `"hello"`, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := UnescapeString(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("UnescapeString(%q) expected error, got %q", tt.input, got)
				}
				return
			}
			if err != nil {
				t.Errorf("UnescapeString(%q) unexpected error: %v", tt.input, err)
				return
			}
			if got != tt.want {
				t.Errorf("UnescapeString(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestUnmarshal(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantType  LiteralTypeE
		wantStr   string
		wantInt   int64
		wantFloat float64
		wantBool  bool
		wantErr   bool
		checkNaN  bool
		checkInf  int // +1 or -1
	}{
		// NULL
		{name: "null_upper", input: "NULL", wantType: LiteralNull},
		{name: "null_lower", input: "null", wantType: LiteralNull},
		{name: "null_mixed", input: "Null", wantType: LiteralNull},

		// Booleans
		{name: "true", input: "true", wantType: LiteralBool, wantBool: true},
		{name: "false", input: "false", wantType: LiteralBool, wantBool: false},

		// Strings
		{name: "string_simple", input: "'hello'", wantType: LiteralString, wantStr: "hello"},
		{name: "string_empty", input: "''", wantType: LiteralString, wantStr: ""},
		{name: "string_escaped", input: `'a\tb'`, wantType: LiteralString, wantStr: "a\tb"},

		// Decimal integers
		{name: "decimal_zero", input: "0", wantType: LiteralInt, wantInt: 0},
		{name: "decimal", input: "42", wantType: LiteralInt, wantInt: 42},
		{name: "decimal_large", input: "1234567890", wantType: LiteralInt, wantInt: 1234567890},
		{name: "decimal_positive", input: "+42", wantType: LiteralInt, wantInt: 42},
		{name: "decimal_negative", input: "-42", wantType: LiteralInt, wantInt: -42},

		// Octal integers
		{name: "octal", input: "0777", wantType: LiteralInt, wantInt: 0777},
		{name: "octal_simple", input: "010", wantType: LiteralInt, wantInt: 8},
		{name: "octal_negative", input: "-010", wantType: LiteralInt, wantInt: -8},

		// Hexadecimal integers
		{name: "hex_lower", input: "0xff", wantType: LiteralInt, wantInt: 255},
		{name: "hex_upper", input: "0XFF", wantType: LiteralInt, wantInt: 255},
		{name: "hex_mixed", input: "0xDeadBeef", wantType: LiteralInt, wantInt: 0xDeadBeef},
		{name: "hex_negative", input: "-0xff", wantType: LiteralInt, wantInt: -255},

		// Floating-point
		{name: "float_dot", input: "3.14", wantType: LiteralFloat, wantFloat: 3.14},
		{name: "float_leading_dot", input: ".5", wantType: LiteralFloat, wantFloat: 0.5},
		{name: "float_trailing_dot", input: "3.", wantType: LiteralFloat, wantFloat: 3.0},
		{name: "float_exp", input: "1e10", wantType: LiteralFloat, wantFloat: 1e10},
		{name: "float_exp_neg", input: "1e-3", wantType: LiteralFloat, wantFloat: 1e-3},
		{name: "float_exp_pos", input: "1E+5", wantType: LiteralFloat, wantFloat: 1e5},
		{name: "float_full", input: "1.5e2", wantType: LiteralFloat, wantFloat: 150.0},
		{name: "float_negative", input: "-3.14", wantType: LiteralFloat, wantFloat: -3.14},
		{name: "float_positive", input: "+3.14", wantType: LiteralFloat, wantFloat: 3.14},

		// Hex floats
		{name: "hex_float", input: "0x1p10", wantType: LiteralFloat, wantFloat: 1024.0},

		// Special floats
		{name: "inf", input: "Inf", wantType: LiteralFloat, checkInf: 1},
		{name: "inf_upper", input: "INF", wantType: LiteralFloat, checkInf: 1},
		{name: "infinity", input: "infinity", wantType: LiteralFloat, checkInf: 1},
		{name: "neg_inf", input: "-Inf", wantType: LiteralFloat, checkInf: -1},
		{name: "pos_inf", input: "+Inf", wantType: LiteralFloat, checkInf: 1},
		{name: "nan", input: "NaN", wantType: LiteralFloat, checkNaN: true},
		{name: "nan_lower", input: "nan", wantType: LiteralFloat, checkNaN: true},

		// Whitespace trimming
		{name: "whitespace", input: "  42  ", wantType: LiteralInt, wantInt: 42},

		// Errors
		{name: "empty", input: "", wantErr: true},
		{name: "bare_sign", input: "+", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := UnmarshalScalarLiteral(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("UnmarshalScalarLiteral(%q) expected error, got %+v", tt.input, got)
				}
				return
			}
			if err != nil {
				t.Errorf("UnmarshalScalarLiteral(%q) unexpected error: %v", tt.input, err)
				return
			}
			if got.Type != tt.wantType {
				t.Errorf("UnmarshalScalarLiteral(%q).Type = %v, want %v", tt.input, got.Type, tt.wantType)
				return
			}
			switch tt.wantType {
			case LiteralString:
				if got.StringVal != tt.wantStr {
					t.Errorf("UnmarshalScalarLiteral(%q).StringVal = %q, want %q", tt.input, got.StringVal, tt.wantStr)
				}
			case LiteralInt:
				if got.IntVal != tt.wantInt {
					t.Errorf("UnmarshalScalarLiteral(%q).IntVal = %d, want %d", tt.input, got.IntVal, tt.wantInt)
				}
			case LiteralFloat:
				if tt.checkNaN {
					if !math.IsNaN(got.FloatVal) {
						t.Errorf("UnmarshalScalarLiteral(%q).FloatVal = %v, want NaN", tt.input, got.FloatVal)
					}
				} else if tt.checkInf != 0 {
					if !math.IsInf(got.FloatVal, tt.checkInf) {
						t.Errorf("UnmarshalScalarLiteral(%q).FloatVal = %v, want Inf(%d)", tt.input, got.FloatVal, tt.checkInf)
					}
				} else if got.FloatVal != tt.wantFloat {
					t.Errorf("UnmarshalScalarLiteral(%q).FloatVal = %v, want %v", tt.input, got.FloatVal, tt.wantFloat)
				}
			case LiteralBool:
				if got.BoolVal != tt.wantBool {
					t.Errorf("UnmarshalScalarLiteral(%q).BoolVal = %v, want %v", tt.input, got.BoolVal, tt.wantBool)
				}
			case LiteralNull:
				// No value to check
			}
		})
	}
}

func TestEscapeString(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty", input: "", want: "''"},
		{name: "simple", input: "hello", want: "'hello'"},
		{name: "quote", input: "it's", want: `'it\'s'`},
		{name: "backslash", input: `a\b`, want: `'a\\b'`},
		{name: "newline", input: "a\nb", want: `'a\nb'`},
		{name: "tab", input: "a\tb", want: `'a\tb'`},
		{name: "null_byte", input: "a\x00b", want: `'a\0b'`},
		{name: "cr", input: "a\rb", want: `'a\rb'`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EscapeString(tt.input)
			if got != tt.want {
				t.Errorf("EscapeString(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestRoundTrip(t *testing.T) {
	// Verify that UnmarshalScalarLiteral(MarshalScalarLiteral(lit)) ≈ lit for various literals
	literals := []Literal{
		{Type: LiteralNull},
		{Type: LiteralBool, BoolVal: true},
		{Type: LiteralBool, BoolVal: false},
		{Type: LiteralString, StringVal: "hello"},
		{Type: LiteralString, StringVal: "it's a \"test\"\nwith\ttabs\\and\\backslashes"},
		{Type: LiteralString, StringVal: ""},
		{Type: LiteralString, StringVal: "\x00\x01\x02"},
		{Type: LiteralInt, IntVal: 0},
		{Type: LiteralInt, IntVal: 42},
		{Type: LiteralInt, IntVal: -100},
		{Type: LiteralFloat, FloatVal: 3.14},
		{Type: LiteralFloat, FloatVal: -0.001},
		{Type: LiteralFloat, FloatVal: 1e100},
		{Type: LiteralFloat, FloatVal: math.Inf(1)},
		{Type: LiteralFloat, FloatVal: math.Inf(-1)},
	}

	for _, lit := range literals {
		text, err := MarshalScalarLiteral(lit)
		if err != nil {
			t.Errorf("MarshalScalarLiteral(%+v) failed: %v", lit, err)
			continue
		}
		got, err := UnmarshalScalarLiteral(text)
		if err != nil {
			t.Errorf("UnmarshalScalarLiteral(MarshalScalarLiteral(%+v)) = UnmarshalScalarLiteral(%q) failed: %v", lit, text, err)
			continue
		}
		if got.Type != lit.Type {
			t.Errorf("roundtrip type mismatch: %v != %v (text=%q)", got.Type, lit.Type, text)
			continue
		}
		switch lit.Type {
		case LiteralString:
			if got.StringVal != lit.StringVal {
				t.Errorf("roundtrip string mismatch: %q != %q (text=%q)", got.StringVal, lit.StringVal, text)
			}
		case LiteralInt:
			if got.IntVal != lit.IntVal {
				t.Errorf("roundtrip int mismatch: %d != %d (text=%q)", got.IntVal, lit.IntVal, text)
			}
		case LiteralFloat:
			if math.IsInf(lit.FloatVal, 0) {
				if !math.IsInf(got.FloatVal, 0) || math.Signbit(got.FloatVal) != math.Signbit(lit.FloatVal) {
					t.Errorf("roundtrip inf mismatch: %v != %v (text=%q)", got.FloatVal, lit.FloatVal, text)
				}
			} else if got.FloatVal != lit.FloatVal {
				t.Errorf("roundtrip float mismatch: %v != %v (text=%q)", got.FloatVal, lit.FloatVal, text)
			}
		case LiteralBool:
			if got.BoolVal != lit.BoolVal {
				t.Errorf("roundtrip bool mismatch: %v != %v (text=%q)", got.BoolVal, lit.BoolVal, text)
			}
		}
	}
}

func TestLiteralTypeString(t *testing.T) {
	if LiteralString.String() != "String" {
		t.Errorf("LiteralString.String() = %q", LiteralString.String())
	}
	if LiteralUnknown.String() != "Unknown" {
		t.Errorf("LiteralUnknown.String() = %q", LiteralUnknown.String())
	}
}
