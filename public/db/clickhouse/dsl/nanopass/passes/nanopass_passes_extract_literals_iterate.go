//go:build llm_generated_opus46

package passes

import (
	"fmt"
	"iter"
	"strings"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/scalars"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
)

// ExtractedParamInfo holds parsed information about an extracted parameter.
type ExtractedParamInfo struct {
	// FullName is the complete parameter name.
	FullName string

	// FunctionName is the context function/operator name (e.g. "eq", "like", "substring").
	FunctionName string

	// Metadata is the decoded ParamMetadata from the name suffix.
	Metadata ParamMetadata

	// LiteralSQL is the raw SQL value text (e.g. "'hello'", "42", "['a', 'b', 'c']").
	LiteralSQL string

	// Type is the canonical primitive type inferred from the literal value, or nil for composites.
	Type canonicaltypes.PrimitiveAstNodeI

	// CastType is the canonical type from an explicit cast encoded in the metadata.
	// Non-nil only when the original literal had a cast (e.g. 1::UInt64).
	// When re-injecting, wrap the value in a cast using this type.
	CastType canonicaltypes.PrimitiveAstNodeI
}

// Value deserializes the LiteralSQL into a Go value.
//
// Return types:
//   - scalars.Literal for scalar values (string, int, float, bool, null)
//   - []any for array values (elements are scalars.Literal or nested []any / *Tuple)
//   - *Tuple for tuple values
func (inst *ExtractedParamInfo) Value() (val any, err error) {
	return deserializeParamLiteral(inst.LiteralSQL)
}

// ScalarValue deserializes the LiteralSQL as a scalar literal.
// Returns an error if the value is an array or tuple.
func (inst *ExtractedParamInfo) ScalarValue() (val scalars.Literal, err error) {
	sql := strings.TrimSpace(inst.LiteralSQL)
	if len(sql) > 0 && (sql[0] == '[' || sql[0] == '(') {
		err = eh.Errorf("ScalarValue: value %q is a composite, not a scalar", sql)
		return
	}
	val, err = scalars.UnmarshalScalarLiteral(sql)
	if err != nil {
		err = eh.Errorf("ScalarValue: %w", err)
	}
	return
}

// HasCast returns true if the original literal had an explicit cast.
func (inst *ExtractedParamInfo) HasCast() bool {
	return inst.CastType != nil
}

// String returns a human-readable representation.
func (inst *ExtractedParamInfo) String() string {
	typeStr := "<composite>"
	if inst.Type != nil {
		typeStr = inst.Type.String()
	}
	castStr := ""
	if inst.CastType != nil {
		castStr = fmt.Sprintf(" cast=%s", inst.CastType.String())
	}
	return fmt.Sprintf("%s = %s (func=%s arg=%d type=%s%s)",
		inst.FullName, inst.LiteralSQL, inst.FunctionName, inst.Metadata.ArgIndex, typeStr, castStr)
}

// IterateExtractedParams parses the SET statements from ExtractLiterals output
// and yields ExtractedParamInfo for each parameter whose name matches the naming convention.
func IterateExtractedParams(extracted string, prefix string) iter.Seq2[int, ExtractedParamInfo] {
	if prefix == "" {
		prefix = ParamPrefixExtracted
	}

	return func(yield func(int, ExtractedParamInfo) bool) {
		sets, _ := ParseExtractedQuery(extracted, prefix)
		idx := 0
		for _, set := range sets {
			info, parseErr := parseSetStatementToInfo(set, prefix)
			if parseErr != nil {
				continue
			}
			if !yield(idx, info) {
				return
			}
			idx++
		}
	}
}

// IterateExtractedParamsFromSets parses pre-split SET lines and yields ExtractedParamInfo.
func IterateExtractedParamsFromSets(sets []string, prefix string) iter.Seq2[int, ExtractedParamInfo] {
	if prefix == "" {
		prefix = "param"
	}

	return func(yield func(int, ExtractedParamInfo) bool) {
		idx := 0
		for _, set := range sets {
			info, parseErr := parseSetStatementToInfo(set, prefix)
			if parseErr != nil {
				continue
			}
			if !yield(idx, info) {
				return
			}
			idx++
		}
	}
}

// CollectExtractedParams collects all extracted parameters into a slice.
func CollectExtractedParams(extracted string, prefix string) (params []ExtractedParamInfo) {
	for _, info := range IterateExtractedParams(extracted, prefix) {
		params = append(params, info)
	}
	return
}

// --- Parsing ---

func parseSetStatementToInfo(set string, prefix string) (info ExtractedParamInfo, err error) {
	line := set
	line = strings.TrimPrefix(line, "SET ")
	line = strings.TrimSpace(line)

	eqIdx := strings.Index(line, " = ")
	if eqIdx < 0 {
		err = eh.Errorf("invalid SET statement: no ' = ' found")
		return
	}

	name := line[:eqIdx]
	value := line[eqIdx+3:]

	info.FullName = name
	info.LiteralSQL = value

	// Parse the structured name
	contextName, meta, parseErr := ParseParamName(name, prefix)
	if parseErr != nil {
		err = eh.Errorf("parseSetStatementToInfo: %w", parseErr)
		return
	}

	info.FunctionName = contextName
	info.Metadata = meta

	// Infer scalar type from value
	info.Type = inferScalarType(value)

	// Reconstruct cast type from canonical string in metadata
	if meta.CastTypeCanonical != "" {
		info.CastType = parseCanonicalType(meta.CastTypeCanonical)
	}

	return
}

// inferScalarType attempts to determine the canonical type from the SQL literal text.
// Returns nil for composite values (arrays, tuples).
func inferScalarType(sql string) canonicaltypes.PrimitiveAstNodeI {
	sql = strings.TrimSpace(sql)
	if len(sql) == 0 {
		return nil
	}

	if sql[0] == '[' || sql[0] == '(' {
		return nil
	}

	lit, err := scalars.UnmarshalScalarLiteral(sql)
	if err != nil {
		return nil
	}
	if lit.Null {
		return nil
	}
	return lit.Type
}

// parseCanonicalType parses a canonical type string (e.g. "u64", "u64h", "u8-s")
// back into a PrimitiveAstNodeI. Returns nil for group types (tuples) and
// unrecognized types — the canonical string is preserved in Metadata.CastTypeCanonical.
func parseCanonicalType(canonical string) canonicaltypes.PrimitiveAstNodeI {
	if canonical == "" {
		return nil
	}
	// Group types (tuples like "u8-s") cannot be represented as a single PrimitiveAstNodeI
	if strings.ContainsAny(canonical, "-_") {
		return nil
	}
	return ctabbFromString(canonical)
}

// ctabbFromString maps well-known canonical type strings to ctabb constants.
func ctabbFromString(s string) canonicaltypes.PrimitiveAstNodeI {
	switch s {
	// Scalar numeric
	case "u8":
		return ctabb.U8
	case "u16":
		return ctabb.U16
	case "u32":
		return ctabb.U32
	case "u64":
		return ctabb.U64
	case "i8":
		return ctabb.I8
	case "i16":
		return ctabb.I16
	case "i32":
		return ctabb.I32
	case "i64":
		return ctabb.I64
	case "f32":
		return ctabb.F32
	case "f64":
		return ctabb.F64

	// Scalar string-class
	case "s":
		return ctabb.S
	case "y":
		return ctabb.Y
	case "b":
		return ctabb.B

	// Homogenous arrays (h modifier)
	case "u8h":
		return ctabb.U8h
	case "u16h":
		return ctabb.U16h
	case "u32h":
		return ctabb.U32h
	case "u64h":
		return ctabb.U64h
	case "i8h":
		return ctabb.I8h
	case "i16h":
		return ctabb.I16h
	case "i32h":
		return ctabb.I32h
	case "i64h":
		return ctabb.I64h
	case "f32h":
		return ctabb.F32h
	case "f64h":
		return ctabb.F64h
	case "sh":
		return ctabb.Sh

	// Temporal
	case "z32":
		return ctabb.Z32
	case "z64":
		return ctabb.Z64

	default:
		return nil
	}
}

// --- Deserialization ---

func deserializeParamLiteral(sql string) (val any, err error) {
	sql = strings.TrimSpace(sql)
	if len(sql) == 0 {
		err = eh.Errorf("empty literal")
		return
	}

	// Array: [...]
	if sql[0] == '[' && sql[len(sql)-1] == ']' {
		return deserializeArrayLiteral(sql)
	}

	// Tuple: (...)
	if sql[0] == '(' && sql[len(sql)-1] == ')' {
		return deserializeTupleLiteral(sql)
	}

	// Scalar: delegate to scalars package
	lit, parseErr := scalars.UnmarshalScalarLiteral(sql)
	if parseErr != nil {
		err = eh.Errorf("deserializeParamLiteral: %w", parseErr)
		return
	}
	val = lit
	return
}

func deserializeArrayLiteral(sql string) (val any, err error) {
	inner := sql[1 : len(sql)-1]
	inner = strings.TrimSpace(inner)
	if inner == "" {
		val = make([]any, 0)
		return
	}

	elements, splitErr := splitTopLevelCommas(inner)
	if splitErr != nil {
		err = eh.Errorf("array literal: %w", splitErr)
		return
	}

	result := make([]any, 0, len(elements))
	for _, elem := range elements {
		elemVal, elemErr := deserializeParamLiteral(strings.TrimSpace(elem))
		if elemErr != nil {
			err = eh.Errorf("array element: %w", elemErr)
			return
		}
		result = append(result, elemVal)
	}
	val = result
	return
}

func deserializeTupleLiteral(sql string) (val any, err error) {
	inner := sql[1 : len(sql)-1]
	inner = strings.TrimSpace(inner)
	if inner == "" {
		val = NewUnnamedTuple()
		return
	}

	elements, splitErr := splitTopLevelCommas(inner)
	if splitErr != nil {
		err = eh.Errorf("tuple literal: %w", splitErr)
		return
	}

	values := make([]any, 0, len(elements))
	for _, elem := range elements {
		elemVal, elemErr := deserializeParamLiteral(strings.TrimSpace(elem))
		if elemErr != nil {
			err = eh.Errorf("tuple element: %w", elemErr)
			return
		}
		values = append(values, elemVal)
	}
	val = NewUnnamedTuple(values...)
	return
}

func splitTopLevelCommas(s string) (parts []string, err error) {
	parts = make([]string, 0, 8)
	depth := 0
	inString := false
	start := 0

	for i := 0; i < len(s); i++ {
		c := s[i]

		if inString {
			if c == '\\' && i+1 < len(s) {
				i++
				continue
			}
			if c == '\'' {
				inString = false
			}
			continue
		}

		switch c {
		case '\'':
			inString = true
		case '[', '(':
			depth++
		case ']', ')':
			depth--
			if depth < 0 {
				err = eh.Errorf("unbalanced brackets/parens")
				return
			}
		case ',':
			if depth == 0 {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}

	if inString {
		err = eh.Errorf("unterminated string literal")
		return
	}
	if depth != 0 {
		err = eh.Errorf("unbalanced brackets/parens")
		return
	}

	parts = append(parts, s[start:])
	return
}
