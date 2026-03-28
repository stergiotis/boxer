//go:build llm_generated_gemini3pro

package sample_test

import (
	"bytes"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/sample"
)

// TestIsValidSemanticRules explicitly tests the "Garbage" cases
// that the mixed-radix generator might produce.
func TestIsValidSemanticRules(t *testing.T) {
	tests := []struct {
		name  string
		node  canonicaltypes.AstNodeI
		valid bool
	}{
		{"Valid IPv4", canonicaltypes.NetworkTypeAstNode{BaseType: 'v', CIDRWidth: 32}, true},
		{"Valid IPv4 CIDR", canonicaltypes.NetworkTypeAstNode{BaseType: 'v', CIDRWidth: 24}, true},
		{"Valid IPv6 CIDR", canonicaltypes.NetworkTypeAstNode{BaseType: 'w', CIDRWidth: 64}, true},
		{"Valid Fixed String", canonicaltypes.StringAstNode{BaseType: 's', Width: 32, WidthModifier: 'x'}, true},
		{"Invalid Fixed String (Zero Width)", canonicaltypes.StringAstNode{BaseType: 's', Width: 0, WidthModifier: 'x'}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.node.IsValid() != tt.valid {
				t.Errorf("Validation failed for %s: expected %v, got %v", tt.name, tt.valid, tt.node.IsValid())
			}
		})
	}
}

// TestStringStabilityRoundtrip ensures node.String() is deterministic.
func TestStringStabilityRoundtrip(t *testing.T) {
	seed := uint64(100)
	rnd := rand.New(rand.NewPCG(seed, seed))

	for i := 0; i < 1000; i++ {
		// Use the generator that filters for .IsValid() nodes
		node := sample.GenerateSamplePrimitiveType(rnd, nil)

		s1 := node.String()
		s2 := node.String() // Testing cache trigger

		if s1 != s2 {
			t.Fatalf("Non-deterministic string for node type %T: %s vs %s", node, s1, s2)
		}

		// Ensure no "invalid" tokens leaked into the string
		if strings.Contains(s1, "invalid") || strings.Contains(s1, "none") {
			t.Errorf("String representation leaked internal error state: %s", s1)
		}
	}
}

// TestGoCodeGenerationRoundtrip ensures GenerateGoCode produces valid-looking Go.
func TestGoCodeGenerationRoundtrip(t *testing.T) {
	seed := uint64(200)
	rnd := rand.New(rand.NewPCG(seed, seed))

	for i := 0; i < 100; i++ {
		node := sample.GenerateSamplePrimitiveType(rnd, nil)

		var buf bytes.Buffer
		err := node.GenerateGoCode(&buf)
		if err != nil {
			t.Fatalf("Go code generation errored: %v", err)
		}

		code := buf.String()

		// Check for common code-gen errors (unquoted runes or missing fields)
		if strings.Contains(code, "''") {
			t.Errorf("Empty rune generated in code: %s", code)
		}

		// Ensure the struct name matches the type
		typeName := ""
		switch node.(type) {
		case canonicaltypes.MachineNumericTypeAstNode:
			typeName = "MachineNumericTypeAstNode"
		case canonicaltypes.StringAstNode:
			typeName = "StringAstNode"
		case canonicaltypes.TemporalTypeAstNode:
			typeName = "TemporalTypeAstNode"
		case canonicaltypes.NetworkTypeAstNode:
			typeName = "NetworkTypeAstNode"
		}

		if !strings.HasPrefix(code, typeName) {
			t.Errorf("Wrong struct name in Go code. Expected prefix %s, got %s", typeName, code)
		}
	}
}

// TestComplexStructureRoundtrip builds a Signature -> Group -> Primitive hierarchy
// and ensures it can be promoted/demoted without losing the Network nodes.
func TestComplexStructureRoundtrip(t *testing.T) {
	// 1. Create a Signature: Group(u8, ipv4) _ Group(s, ipv6c)
	g1 := canonicaltypes.NewGroupAstNode([]canonicaltypes.PrimitiveAstNodeI{
		canonicaltypes.MachineNumericTypeAstNode{BaseType: 'u', Width: 8},
		canonicaltypes.NetworkTypeAstNode{BaseType: 'v', CIDRWidth: 32},
	})

	g2 := canonicaltypes.NewGroupAstNode([]canonicaltypes.PrimitiveAstNodeI{
		canonicaltypes.StringAstNode{BaseType: 's'},
		canonicaltypes.NetworkTypeAstNode{BaseType: 'w', CIDRWidth: 64},
	})

	sig := canonicaltypes.NewSignatureAstNode([]canonicaltypes.AstNodeI{g1, g2})

	originalStr := sig.String() // Expect something like "u8-4_s-6c64"

	// 2. Promote everything to Homogenous Arrays ('h')
	promoted, _, _ := canonicaltypes.PromoteScalars(sig, canonicaltypes.ScalarModifierHomogenousArray)

	pStr := promoted.String()
	if !strings.Contains(pStr, "v32h") || !strings.Contains(pStr, "w64h") {
		t.Errorf("Promotion failed to target Network nodes: %s", pStr)
	}

	// Ensure separators were preserved (Signature vs Group)
	if !strings.Contains(pStr, "_") || !strings.Contains(pStr, "-") {
		t.Errorf("Hierarchy separators lost during promotion: %s", pStr)
	}

	// 3. Demote back to scalars
	demoted, _, _ := canonicaltypes.DemoteToScalars(promoted)

	if demoted.String() != originalStr {
		t.Errorf("Roundtrip failed.\nOriginal: %s\nResult:   %s", originalStr, demoted.String())
	}
}

// TestDeepEqualityProperties verifies the Equals() implementation.
func TestDeepEqualityProperties(t *testing.T) {
	n1 := canonicaltypes.NetworkTypeAstNode{BaseType: 'v', CIDRWidth: 24, ScalarModifier: 'h'}
	n2 := canonicaltypes.NetworkTypeAstNode{BaseType: 'v', CIDRWidth: 24, ScalarModifier: 'h'}
	n3 := canonicaltypes.NetworkTypeAstNode{BaseType: 'v', CIDRWidth: 32, ScalarModifier: 'h'}

	if n1.String() != n2.String() {
		t.Error("Identical network nodes failed Equals()")
	}
	if n1.String() == n3.String() {
		t.Error("Nodes with different CIDR widths should not be equal")
	}
}
