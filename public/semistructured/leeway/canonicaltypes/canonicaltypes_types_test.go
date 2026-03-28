package canonicaltypes

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

var u8 = MachineNumericTypeAstNode{BaseType: 'u', Width: 8, ByteOrderModifier: 0, ScalarModifier: 0}
var s = StringAstNode{BaseType: 's', WidthModifier: 0, Width: 0, ScalarModifier: 0}
var i8 = MachineNumericTypeAstNode{BaseType: 'i', Width: 8, ByteOrderModifier: 0, ScalarModifier: 0}

func TestTypes(t *testing.T) {
	var a AstNodeI
	a = &GroupAstNode{}
	_, castable := (a).(PrimitiveAstNodeI)
	require.False(t, castable, "group ast node is a primitive type")
}

func TestIssue2_StructuralLoss(t *testing.T) {
	// 1. Create a Signature: "u8_s"
	// This uses the Signature separator "_"
	members := []AstNodeI{u8, s}
	sig := NewSignatureAstNode(members)

	if sig.String() != "u8_s" {
		t.Fatalf("setup failed: expected signature u8_s, got %s", sig.String())
	}

	// 2. Promote scalars to arrays ('h')
	// The expected result should logically be a Signature: "u8h_sh"
	out, _, _ := PromoteScalars(sig, ScalarModifierHomogenousArray)

	// 3. Check the type of 'out'
	_, isSignature := out.(SignatureAstNode)
	_, isGroup := out.(GroupAstNode)

	t.Logf("Output string: %s", out.String())

	if !isSignature && isGroup {
		t.Errorf("ISSUE 2 DETECTED: A Signature was demoted to a Group. Original: %T, Result: %T", sig, out)
		t.Errorf("Visual proof: Expected '_' separator, but got '%s'", out.String())
	}
}

func TestIssue4_RedundantIterators(t *testing.T) {
	// 1. Create a Signature containing a Group and a Primitive: (u8-i8)_s
	group := NewGroupAstNode([]PrimitiveAstNodeI{u8, i8})
	sig := NewSignatureAstNode([]AstNodeI{group, s})

	// 2. We use IterateGroupMembers.
	// Logically, for a Signature, this should yield its direct children:
	// [GroupAstNode, StringAstNode].
	yieldedTypes := []reflect.Type{}
	for member := range sig.IterateGroupMembers() {
		yieldedTypes = append(yieldedTypes, reflect.TypeOf(member))
	}

	t.Logf("Yielded types: %v", yieldedTypes)

	// If it yielded 3 items, it flattened the group into its primitives (Issue 4).
	if len(yieldedTypes) == 3 {
		t.Errorf("ISSUE 4 DETECTED: IterateGroupMembers flattened the hierarchy.")
		t.Errorf("It yielded primitives [u8, i8, s] instead of the Group and the String.")
	}

	// Verify if implementations are identical
	// (This is the "redundancy" part of the issue)
	primTypes := []reflect.Type{}
	for member := range sig.IterateMembers() {
		primTypes = append(primTypes, reflect.TypeOf(member))
	}

	if reflect.DeepEqual(yieldedTypes, primTypes) {
		t.Log("Note: IterateGroupMembers and IterateMembers have identical behavior (both flatten).")
	}
}
