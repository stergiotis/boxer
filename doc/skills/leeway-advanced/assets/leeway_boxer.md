---
type: reference
audience: agent reading this skill asset
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.
Below is the public API surface of the library. Function bodies are stubbed (bodies replaced with `panic("stub")`) -- ignore this, it is an artifact of the export process. Your job is to write code that consumes this API.

--- FILE: base62/base62.go ---
```go
package base62

import (
	_ "fmt"
	_ "math/big"
)

type Base62Num string

func (inst Base62Num) String() string { panic("stub") }

func (inst Base62Num) Decode() (num uint64, valid bool) { panic("stub") }

func IsValid(encoded Base62Num) (valid bool) { panic("stub") }

func Decode(encoded Base62Num) (num uint64, valid bool) { panic("stub") }

func Encode(num uint64) (n Base62Num) { panic("stub") }


```

--- FILE: canonicaltypes/canonicaltypes_ast.go ---
```go
package canonicaltypes

import (
	_ "fmt"
	"io"
	_ "io"
	"iter"
	_ "iter"
	_ "strconv"
	_ "strings"

	_ "github.com/fxamacker/cbor/v2"
	_ "golang.org/x/exp/constraints"
)

func (inst StringAstNode) IsStringNode() bool { panic("stub") }

func (inst StringAstNode) IsValid() bool { panic("stub") }

func (inst StringAstNode) IsTemporalNode() bool { panic("stub") }

func (inst StringAstNode) IsMachineNumericNode() bool { panic("stub") }

func (inst StringAstNode) IsPrimitive() bool { panic("stub") }

func (inst StringAstNode) IsSignature() bool { panic("stub") }

func (inst StringAstNode) String() string { panic("stub") }

func (inst StringAstNode) IsScalar() bool { panic("stub") }

func (inst StringAstNode) IterateMembers() iter.Seq[PrimitiveAstNodeI] { panic("stub") }

func (inst StringAstNode) MarshalCBOR() (data []byte, err error) { panic("stub") }

func (inst StringAstNode) GenerateGoCode(w io.Writer) (err error) { panic("stub") }

func (inst TemporalTypeAstNode) IsStringNode() bool { panic("stub") }

func (inst TemporalTypeAstNode) IsTemporalNode() bool { panic("stub") }

func (inst TemporalTypeAstNode) IsMachineNumericNode() bool { panic("stub") }

func (inst TemporalTypeAstNode) IsPrimitive() bool { panic("stub") }

func (inst TemporalTypeAstNode) String() string { panic("stub") }

func (inst TemporalTypeAstNode) GenerateGoCode(w io.Writer) (err error) { panic("stub") }

func (inst TemporalTypeAstNode) IsValid() bool { panic("stub") }

func (inst TemporalTypeAstNode) IsScalar() bool { panic("stub") }

func (inst TemporalTypeAstNode) IsSignature() bool { panic("stub") }

func (inst TemporalTypeAstNode) IterateMembers() iter.Seq[PrimitiveAstNodeI] { panic("stub") }

func (inst TemporalTypeAstNode) MarshalCBOR() (data []byte, err error) { panic("stub") }

func (inst MachineNumericTypeAstNode) IsStringNode() bool { panic("stub") }

func (inst MachineNumericTypeAstNode) IsTemporalNode() bool { panic("stub") }

func (inst MachineNumericTypeAstNode) IsMachineNumericNode() bool { panic("stub") }

func (inst MachineNumericTypeAstNode) IsPrimitive() bool { panic("stub") }

func (inst MachineNumericTypeAstNode) IsValid() bool { panic("stub") }

func (inst MachineNumericTypeAstNode) IsSignature() bool { panic("stub") }

func (inst MachineNumericTypeAstNode) String() string { panic("stub") }

func (inst MachineNumericTypeAstNode) GenerateGoCode(w io.Writer) (err error) { panic("stub") }

func (inst MachineNumericTypeAstNode) IsScalar() bool { panic("stub") }

func (inst MachineNumericTypeAstNode) IterateMembers() iter.Seq[PrimitiveAstNodeI] { panic("stub") }

func (inst MachineNumericTypeAstNode) MarshalCBOR() (data []byte, err error) { panic("stub") }

func (inst GroupAstNode) IsStringNode() bool { panic("stub") }

func (inst GroupAstNode) IsTemporalNode() bool { panic("stub") }

func (inst GroupAstNode) IsMachineNumericNode() bool { panic("stub") }

func (inst GroupAstNode) IsPrimitive() bool { panic("stub") }

func (inst GroupAstNode) IsSignature() bool { panic("stub") }

func (inst GroupAstNode) IterateMembers() iter.Seq[PrimitiveAstNodeI] { panic("stub") }

func (inst GroupAstNode) IsValid() bool { panic("stub") }

func (inst GroupAstNode) String() string { panic("stub") }

// estimate size

// cache, note that ASTs are "immutable" (as far as easily possible in go *sigh*)

func (inst GroupAstNode) MarshalCBOR() (data []byte, err error) { panic("stub") }

func (inst Width) String() string { panic("stub") }

func (inst SignatureAstNode) IsSignature() bool { panic("stub") }

func (inst SignatureAstNode) IsPrimitive() bool { panic("stub") }

func (inst SignatureAstNode) IsValid() bool { panic("stub") }

func (inst SignatureAstNode) IterateMembers() iter.Seq[PrimitiveAstNodeI] { panic("stub") }

func (inst SignatureAstNode) IterateGroupMembers() iter.Seq[AstNodeI] { panic("stub") }

func (inst SignatureAstNode) String() string { panic("stub") }

// estimate size

// cache, note that ASTs are "immutable" (as far as easily possible in go *sigh*)

func (inst SignatureAstNode) MarshalCBOR() (data []byte, err error) { panic("stub") }

func NewGroupAstNode(members []PrimitiveAstNodeI) GroupAstNode { panic("stub") }


```

--- FILE: canonicaltypes/canonicaltypes_enums.go ---
```go
package canonicaltypes

const GroupSeparator = "-"
const SignatureSeparator = "_"

const (
	BaseTypeStringNone  BaseTypeStringE = 0
	BaseTypeStringUtf8  BaseTypeStringE = 's'
	BaseTypeStringBytes BaseTypeStringE = 'y'
	BaseTypeStringBool  BaseTypeStringE = 'b'
)

func (inst BaseTypeStringE) String() string { panic("stub") }

const (
	BaseTypeMachineNumericNone     BaseTypeMachineNumericE = 0
	BaseTypeMachineNumericUnsigned BaseTypeMachineNumericE = 'u'
	BaseTypeMachineNumericSigned   BaseTypeMachineNumericE = 'i'
	BaseTypeMachineNumericFloat    BaseTypeMachineNumericE = 'f'
)

func (inst BaseTypeMachineNumericE) String() string { panic("stub") }

const (
	BaseTypeTemporalNone          BaseTypeTemporalE = 0
	BaseTypeTemporalUtcDatetime   BaseTypeTemporalE = 'z'
	BaseTypeTemporalZonedDatetime BaseTypeTemporalE = 'd'
	BaseTypeTemporalZonedTime     BaseTypeTemporalE = 't'
)

func (inst BaseTypeTemporalE) String() string { panic("stub") }

const (
	ScalarModifierNone            ScalarModifierE = 0
	ScalarModifierHomogenousArray ScalarModifierE = 'h'
	ScalarModifierSet             ScalarModifierE = 'm'
)

func (inst ScalarModifierE) String() string { panic("stub") }

const (
	ByteOrderModifierNone         ByteOrderModifierE = 0
	ByteOrderModifierLittleEndian ByteOrderModifierE = 'l'
	ByteOrderModifierBigEndian    ByteOrderModifierE = 'n'
)

func (inst ByteOrderModifierE) String() string { panic("stub") }

const (
	WidthModifierNone  WidthModifierE = 0
	WidthModifierFixed WidthModifierE = 'x'
)

func (inst WidthModifierE) String() string { panic("stub") }


```

--- FILE: canonicaltypes/canonicaltypes_parser.go ---
```go
package canonicaltypes

import (
	_ "strconv"

	"github.com/antlr4-go/antlr"
	_ "github.com/antlr4-go/antlr/v4"
	_ "github.com/stergiotis/boxer/public/observability/eh"
	_ "github.com/stergiotis/boxer/public/observability/eh/eb"
	_ "github.com/stergiotis/boxer/public/parsing/antlr4utils"
	grammar2 "github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/grammar"
)

func NewParser() *Parser { panic("stub") }

func (inst *Parser) MustParseTypeOrGroupAst(typeOrGroup string) (ast AstNodeI) { panic("stub") }

func (inst *Parser) MustParsePrimitiveTypeAst(typeS string) (ast PrimitiveAstNodeI) { panic("stub") }

func (inst *Parser) ParsePrimitiveTypeAst(typeS string) (ast PrimitiveAstNodeI, err error) {
	panic("stub")
}

func (inst *Parser) ParsePrimitiveTypeOrGroupAst(typeOrGroup string) (ast AstNodeI, err error) {
	panic("stub")
}

func (inst *Parser) ParseSignature(signature string) (parser antlr.Recognizer, tree grammar2.ICanonicalTypeSignatureContext, err error) {
	panic("stub")
}

func (inst *Parser) ParseTypeOrGroup(typeOrGroup string) (parser antlr.Recognizer, tree grammar2.ISingleCanonicalTypeOrGroupContext, err error) {
	panic("stub")
}


```

--- FILE: canonicaltypes/canonicaltypes_types.go ---
```go
package canonicaltypes

import (
	"fmt"
	_ "fmt"
	"io"
	_ "io"
	"iter"
	_ "iter"

	_ "github.com/antlr4-go/antlr/v4"
	"github.com/fxamacker/cbor"
	_ "github.com/fxamacker/cbor/v2"
	"github.com/stergiotis/boxer/public/observability/eh"
	_ "github.com/stergiotis/boxer/public/observability/eh"
	_ "github.com/stergiotis/boxer/public/parsing/antlr4utils"
)

type BaseTypeStringE rune

type BaseTypeTemporalE rune

type BaseTypeMachineNumericE rune

type ScalarModifierE rune

type ByteOrderModifierE rune

type WidthModifierE rune

type Width uint32

type PrimitiveAstNodeI interface {
	IsStringNode() bool
	IsTemporalNode() bool
	IsMachineNumericNode() bool
	IsScalar() bool
	GenerateGoCode(w io.Writer) (err error)
	AstNodeI
}
type AstNodeI interface {
	cbor.Marshaler
	IsSignature() bool
	IsPrimitive() bool
	IsValid() bool
	IterateMembers() iter.Seq[PrimitiveAstNodeI]
	fmt.Stringer
}
type SignatureAstNode struct {
}

type GroupAstNode struct {
}

type StringAstNode struct {
	BaseType       BaseTypeStringE
	WidthModifier  WidthModifierE
	Width          Width
	ScalarModifier ScalarModifierE
}

type TemporalTypeAstNode struct {
	BaseType       BaseTypeTemporalE
	Width          Width
	ScalarModifier ScalarModifierE
}

type MachineNumericTypeAstNode struct {
	BaseType          BaseTypeMachineNumericE
	Width             Width
	ByteOrderModifier ByteOrderModifierE
	ScalarModifier    ScalarModifierE
}

var ErrInternalParserError = eh.Errorf("internal parser error")

type Parser struct {
}


```

--- FILE: canonicaltypes/canonicaltypes_utils.go ---
```go
package canonicaltypes

import (
	"iter"
	_ "iter"

	_ "github.com/rs/zerolog/log"
)

func GetScalarModifier(s PrimitiveAstNodeI) (mod ScalarModifierE, notSupported bool) { panic("stub") }

func DemoteToScalar(s PrimitiveAstNodeI) (out PrimitiveAstNodeI) { panic("stub") }

func PromoteScalars(in AstNodeI, scalarModifier ScalarModifierE) (out AstNodeI, modified int, unmodified int) {
	panic("stub")
}

func DemoteToScalars(in AstNodeI) (out AstNodeI, modified int, unmodified int) { panic("stub") }

func MergeGroup(l AstNodeI, r AstNodeI) (g GroupAstNode) { panic("stub") }

func CountMembers(t AstNodeI) (r int) { panic("stub") }

func CountMembersMulti(ts []AstNodeI) (r int) { panic("stub") }

func CountGroupTypeMembers(t AstNodeI) (r int) { panic("stub") }

func CountGroupTypeMembersMulti(ts []AstNodeI) (r int) { panic("stub") }

func CountNonScalarsMulti(ts []AstNodeI) (r int) { panic("stub") }

func CountNonScalars(t AstNodeI) (r int) { panic("stub") }

func IteratePrimitiveTypesMulti(ts []AstNodeI) iter.Seq2[int, PrimitiveAstNodeI] { panic("stub") }

func IterateGroupIndexedByOccurrence(t AstNodeI, uniqTypeIndex int) iter.Seq2[int, PrimitiveAstNodeI] {
	panic("stub")
}

func CastSliceOfPrimitiveAstNodes(s []PrimitiveAstNodeI) (o []AstNodeI) { panic("stub") }


```

--- FILE: canonicaltypes/codegen/canonicalTypes_go_codegen_dummy_test.gen.go ---
```go
// Code generated DO NOT EDIT
package codegen

import (
	"testing"
	_ "testing"
	_ "time"

	_ "github.com/stretchr/testify/require"
)

func TestGeneratedGoCodeOutput(t *testing.T) { panic("stub") }


```

--- FILE: canonicaltypes/codegen/canonicaltypes_go_abbrev_gen.go ---
```go
package codegen

import (
	_ "fmt"
	"io"
	_ "io"
	_ "strings"

	_ "github.com/rs/zerolog/log"
	_ "github.com/stergiotis/boxer/public/code/synthesis/golang"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/sample"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/canonicaltypes"
	_ "github.com/stoewer/go-strcase"
)

func GenerateGoAbbrev(packageName string, imp string, astPackage string, w io.Writer, accept func(ct canonicaltypes.PrimitiveAstNodeI) (keep bool)) (err error) {
	panic("stub")
}

// skipping fixed length string type


```

--- FILE: canonicaltypes/codegen/canonicaltypes_go_codegen.go ---
```go
package codegen

import (
	_ "fmt"

	"github.com/stergiotis/boxer/public/observability/eh"
	_ "github.com/stergiotis/boxer/public/observability/eh"
	_ "github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/observability/vcs"
	_ "github.com/stergiotis/boxer/public/observability/vcs"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/common"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/canonicaltypes"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/encodingaspects"
)

var CodeGeneratorName = "Leeway CT (" + vcs.ModuleInfo() + ")"

var ErrNotImplemented = eh.Errorf("go code generation not implemtented for given canonical type")

func GenerateGoCode(canonicalType canonicaltypes.PrimitiveAstNodeI, hints encodingaspects.AspectSet) (typeCode string, zeroValueLiteral string, imports []string, err error) {
	panic("stub")
}


```

--- FILE: canonicaltypes/ctabb/canonicaltypes_abbrevs.out.go ---
```go
// Code generated; Leeway CT (github.com/stergiotis/boxer/public/app) DO NOT EDIT.

package ctabb

import (
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/canonicaltypes"
)

var U8 = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'u', Width: 8, ByteOrderModifier: 0, ScalarModifier: 0}
var I8 = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'i', Width: 8, ByteOrderModifier: 0, ScalarModifier: 0}
var F8 = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'f', Width: 8, ByteOrderModifier: 0, ScalarModifier: 0}
var U16 = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'u', Width: 16, ByteOrderModifier: 0, ScalarModifier: 0}
var I16 = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'i', Width: 16, ByteOrderModifier: 0, ScalarModifier: 0}
var F16 = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'f', Width: 16, ByteOrderModifier: 0, ScalarModifier: 0}
var U32 = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'u', Width: 32, ByteOrderModifier: 0, ScalarModifier: 0}
var I32 = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'i', Width: 32, ByteOrderModifier: 0, ScalarModifier: 0}
var F32 = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'f', Width: 32, ByteOrderModifier: 0, ScalarModifier: 0}
var U64 = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'u', Width: 64, ByteOrderModifier: 0, ScalarModifier: 0}
var I64 = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'i', Width: 64, ByteOrderModifier: 0, ScalarModifier: 0}
var F64 = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'f', Width: 64, ByteOrderModifier: 0, ScalarModifier: 0}
var U8l = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'u', Width: 8, ByteOrderModifier: 'l', ScalarModifier: 0}
var I8l = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'i', Width: 8, ByteOrderModifier: 'l', ScalarModifier: 0}
var F8l = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'f', Width: 8, ByteOrderModifier: 'l', ScalarModifier: 0}
var U16l = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'u', Width: 16, ByteOrderModifier: 'l', ScalarModifier: 0}
var I16l = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'i', Width: 16, ByteOrderModifier: 'l', ScalarModifier: 0}
var F16l = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'f', Width: 16, ByteOrderModifier: 'l', ScalarModifier: 0}
var U32l = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'u', Width: 32, ByteOrderModifier: 'l', ScalarModifier: 0}
var I32l = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'i', Width: 32, ByteOrderModifier: 'l', ScalarModifier: 0}
var F32l = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'f', Width: 32, ByteOrderModifier: 'l', ScalarModifier: 0}
var U64l = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'u', Width: 64, ByteOrderModifier: 'l', ScalarModifier: 0}
var I64l = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'i', Width: 64, ByteOrderModifier: 'l', ScalarModifier: 0}
var F64l = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'f', Width: 64, ByteOrderModifier: 'l', ScalarModifier: 0}
var U8n = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'u', Width: 8, ByteOrderModifier: 'n', ScalarModifier: 0}
var I8n = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'i', Width: 8, ByteOrderModifier: 'n', ScalarModifier: 0}
var F8n = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'f', Width: 8, ByteOrderModifier: 'n', ScalarModifier: 0}
var U16n = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'u', Width: 16, ByteOrderModifier: 'n', ScalarModifier: 0}
var I16n = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'i', Width: 16, ByteOrderModifier: 'n', ScalarModifier: 0}
var F16n = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'f', Width: 16, ByteOrderModifier: 'n', ScalarModifier: 0}
var U32n = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'u', Width: 32, ByteOrderModifier: 'n', ScalarModifier: 0}
var I32n = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'i', Width: 32, ByteOrderModifier: 'n', ScalarModifier: 0}
var F32n = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'f', Width: 32, ByteOrderModifier: 'n', ScalarModifier: 0}
var U64n = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'u', Width: 64, ByteOrderModifier: 'n', ScalarModifier: 0}
var I64n = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'i', Width: 64, ByteOrderModifier: 'n', ScalarModifier: 0}
var F64n = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'f', Width: 64, ByteOrderModifier: 'n', ScalarModifier: 0}
var U8h = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'u', Width: 8, ByteOrderModifier: 0, ScalarModifier: 'h'}
var I8h = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'i', Width: 8, ByteOrderModifier: 0, ScalarModifier: 'h'}
var F8h = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'f', Width: 8, ByteOrderModifier: 0, ScalarModifier: 'h'}
var U16h = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'u', Width: 16, ByteOrderModifier: 0, ScalarModifier: 'h'}
var I16h = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'i', Width: 16, ByteOrderModifier: 0, ScalarModifier: 'h'}
var F16h = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'f', Width: 16, ByteOrderModifier: 0, ScalarModifier: 'h'}
var U32h = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'u', Width: 32, ByteOrderModifier: 0, ScalarModifier: 'h'}
var I32h = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'i', Width: 32, ByteOrderModifier: 0, ScalarModifier: 'h'}
var F32h = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'f', Width: 32, ByteOrderModifier: 0, ScalarModifier: 'h'}
var U64h = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'u', Width: 64, ByteOrderModifier: 0, ScalarModifier: 'h'}
var I64h = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'i', Width: 64, ByteOrderModifier: 0, ScalarModifier: 'h'}
var F64h = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'f', Width: 64, ByteOrderModifier: 0, ScalarModifier: 'h'}
var U8lh = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'u', Width: 8, ByteOrderModifier: 'l', ScalarModifier: 'h'}
var I8lh = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'i', Width: 8, ByteOrderModifier: 'l', ScalarModifier: 'h'}
var F8lh = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'f', Width: 8, ByteOrderModifier: 'l', ScalarModifier: 'h'}
var U16lh = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'u', Width: 16, ByteOrderModifier: 'l', ScalarModifier: 'h'}
var I16lh = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'i', Width: 16, ByteOrderModifier: 'l', ScalarModifier: 'h'}
var F16lh = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'f', Width: 16, ByteOrderModifier: 'l', ScalarModifier: 'h'}
var U32lh = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'u', Width: 32, ByteOrderModifier: 'l', ScalarModifier: 'h'}
var I32lh = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'i', Width: 32, ByteOrderModifier: 'l', ScalarModifier: 'h'}
var F32lh = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'f', Width: 32, ByteOrderModifier: 'l', ScalarModifier: 'h'}
var U64lh = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'u', Width: 64, ByteOrderModifier: 'l', ScalarModifier: 'h'}
var I64lh = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'i', Width: 64, ByteOrderModifier: 'l', ScalarModifier: 'h'}
var F64lh = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'f', Width: 64, ByteOrderModifier: 'l', ScalarModifier: 'h'}
var U8nh = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'u', Width: 8, ByteOrderModifier: 'n', ScalarModifier: 'h'}
var I8nh = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'i', Width: 8, ByteOrderModifier: 'n', ScalarModifier: 'h'}
var F8nh = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'f', Width: 8, ByteOrderModifier: 'n', ScalarModifier: 'h'}
var U16nh = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'u', Width: 16, ByteOrderModifier: 'n', ScalarModifier: 'h'}
var I16nh = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'i', Width: 16, ByteOrderModifier: 'n', ScalarModifier: 'h'}
var F16nh = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'f', Width: 16, ByteOrderModifier: 'n', ScalarModifier: 'h'}
var U32nh = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'u', Width: 32, ByteOrderModifier: 'n', ScalarModifier: 'h'}
var I32nh = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'i', Width: 32, ByteOrderModifier: 'n', ScalarModifier: 'h'}
var F32nh = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'f', Width: 32, ByteOrderModifier: 'n', ScalarModifier: 'h'}
var U64nh = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'u', Width: 64, ByteOrderModifier: 'n', ScalarModifier: 'h'}
var I64nh = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'i', Width: 64, ByteOrderModifier: 'n', ScalarModifier: 'h'}
var F64nh = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'f', Width: 64, ByteOrderModifier: 'n', ScalarModifier: 'h'}
var U8m = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'u', Width: 8, ByteOrderModifier: 0, ScalarModifier: 'm'}
var I8m = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'i', Width: 8, ByteOrderModifier: 0, ScalarModifier: 'm'}
var F8m = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'f', Width: 8, ByteOrderModifier: 0, ScalarModifier: 'm'}
var U16m = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'u', Width: 16, ByteOrderModifier: 0, ScalarModifier: 'm'}
var I16m = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'i', Width: 16, ByteOrderModifier: 0, ScalarModifier: 'm'}
var F16m = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'f', Width: 16, ByteOrderModifier: 0, ScalarModifier: 'm'}
var U32m = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'u', Width: 32, ByteOrderModifier: 0, ScalarModifier: 'm'}
var I32m = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'i', Width: 32, ByteOrderModifier: 0, ScalarModifier: 'm'}
var F32m = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'f', Width: 32, ByteOrderModifier: 0, ScalarModifier: 'm'}
var U64m = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'u', Width: 64, ByteOrderModifier: 0, ScalarModifier: 'm'}
var I64m = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'i', Width: 64, ByteOrderModifier: 0, ScalarModifier: 'm'}
var F64m = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'f', Width: 64, ByteOrderModifier: 0, ScalarModifier: 'm'}
var U8lm = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'u', Width: 8, ByteOrderModifier: 'l', ScalarModifier: 'm'}
var I8lm = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'i', Width: 8, ByteOrderModifier: 'l', ScalarModifier: 'm'}
var F8lm = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'f', Width: 8, ByteOrderModifier: 'l', ScalarModifier: 'm'}
var U16lm = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'u', Width: 16, ByteOrderModifier: 'l', ScalarModifier: 'm'}
var I16lm = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'i', Width: 16, ByteOrderModifier: 'l', ScalarModifier: 'm'}
var F16lm = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'f', Width: 16, ByteOrderModifier: 'l', ScalarModifier: 'm'}
var U32lm = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'u', Width: 32, ByteOrderModifier: 'l', ScalarModifier: 'm'}
var I32lm = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'i', Width: 32, ByteOrderModifier: 'l', ScalarModifier: 'm'}
var F32lm = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'f', Width: 32, ByteOrderModifier: 'l', ScalarModifier: 'm'}
var U64lm = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'u', Width: 64, ByteOrderModifier: 'l', ScalarModifier: 'm'}
var I64lm = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'i', Width: 64, ByteOrderModifier: 'l', ScalarModifier: 'm'}
var F64lm = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'f', Width: 64, ByteOrderModifier: 'l', ScalarModifier: 'm'}
var U8nm = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'u', Width: 8, ByteOrderModifier: 'n', ScalarModifier: 'm'}
var I8nm = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'i', Width: 8, ByteOrderModifier: 'n', ScalarModifier: 'm'}
var F8nm = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'f', Width: 8, ByteOrderModifier: 'n', ScalarModifier: 'm'}
var U16nm = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'u', Width: 16, ByteOrderModifier: 'n', ScalarModifier: 'm'}
var I16nm = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'i', Width: 16, ByteOrderModifier: 'n', ScalarModifier: 'm'}
var F16nm = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'f', Width: 16, ByteOrderModifier: 'n', ScalarModifier: 'm'}
var U32nm = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'u', Width: 32, ByteOrderModifier: 'n', ScalarModifier: 'm'}
var I32nm = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'i', Width: 32, ByteOrderModifier: 'n', ScalarModifier: 'm'}
var F32nm = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'f', Width: 32, ByteOrderModifier: 'n', ScalarModifier: 'm'}
var U64nm = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'u', Width: 64, ByteOrderModifier: 'n', ScalarModifier: 'm'}
var I64nm = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'i', Width: 64, ByteOrderModifier: 'n', ScalarModifier: 'm'}
var F64nm = canonicaltypes.MachineNumericTypeAstNode{BaseType: 'f', Width: 64, ByteOrderModifier: 'n', ScalarModifier: 'm'}
var B = canonicaltypes.StringAstNode{BaseType: 'b', WidthModifier: 0, Width: 0, ScalarModifier: 0}
var Y = canonicaltypes.StringAstNode{BaseType: 'y', WidthModifier: 0, Width: 0, ScalarModifier: 0}
var S = canonicaltypes.StringAstNode{BaseType: 's', WidthModifier: 0, Width: 0, ScalarModifier: 0}
var Bh = canonicaltypes.StringAstNode{BaseType: 'b', WidthModifier: 0, Width: 0, ScalarModifier: 'h'}
var Yh = canonicaltypes.StringAstNode{BaseType: 'y', WidthModifier: 0, Width: 0, ScalarModifier: 'h'}
var Sh = canonicaltypes.StringAstNode{BaseType: 's', WidthModifier: 0, Width: 0, ScalarModifier: 'h'}
var Bm = canonicaltypes.StringAstNode{BaseType: 'b', WidthModifier: 0, Width: 0, ScalarModifier: 'm'}
var Ym = canonicaltypes.StringAstNode{BaseType: 'y', WidthModifier: 0, Width: 0, ScalarModifier: 'm'}
var Sm = canonicaltypes.StringAstNode{BaseType: 's', WidthModifier: 0, Width: 0, ScalarModifier: 'm'}
var Z32 = canonicaltypes.TemporalTypeAstNode{BaseType: 'z', Width: 32, ScalarModifier: 0}
var D32 = canonicaltypes.TemporalTypeAstNode{BaseType: 'd', Width: 32, ScalarModifier: 0}
var T32 = canonicaltypes.TemporalTypeAstNode{BaseType: 't', Width: 32, ScalarModifier: 0}
var Z64 = canonicaltypes.TemporalTypeAstNode{BaseType: 'z', Width: 64, ScalarModifier: 0}
var D64 = canonicaltypes.TemporalTypeAstNode{BaseType: 'd', Width: 64, ScalarModifier: 0}
var T64 = canonicaltypes.TemporalTypeAstNode{BaseType: 't', Width: 64, ScalarModifier: 0}
var Z32h = canonicaltypes.TemporalTypeAstNode{BaseType: 'z', Width: 32, ScalarModifier: 'h'}
var D32h = canonicaltypes.TemporalTypeAstNode{BaseType: 'd', Width: 32, ScalarModifier: 'h'}
var T32h = canonicaltypes.TemporalTypeAstNode{BaseType: 't', Width: 32, ScalarModifier: 'h'}
var Z64h = canonicaltypes.TemporalTypeAstNode{BaseType: 'z', Width: 64, ScalarModifier: 'h'}
var D64h = canonicaltypes.TemporalTypeAstNode{BaseType: 'd', Width: 64, ScalarModifier: 'h'}
var T64h = canonicaltypes.TemporalTypeAstNode{BaseType: 't', Width: 64, ScalarModifier: 'h'}
var Z32m = canonicaltypes.TemporalTypeAstNode{BaseType: 'z', Width: 32, ScalarModifier: 'm'}
var D32m = canonicaltypes.TemporalTypeAstNode{BaseType: 'd', Width: 32, ScalarModifier: 'm'}
var T32m = canonicaltypes.TemporalTypeAstNode{BaseType: 't', Width: 32, ScalarModifier: 'm'}
var Z64m = canonicaltypes.TemporalTypeAstNode{BaseType: 'z', Width: 64, ScalarModifier: 'm'}
var D64m = canonicaltypes.TemporalTypeAstNode{BaseType: 'd', Width: 64, ScalarModifier: 'm'}
var T64m = canonicaltypes.TemporalTypeAstNode{BaseType: 't', Width: 64, ScalarModifier: 'm'}


```

--- FILE: canonicaltypes/grammar/canonicaltypesignature_lexer.out.go ---
```go
// Code generated from CanonicalTypeSignatureLexer.g4 by ANTLR 4.13.1. DO NOT EDIT.

package grammar

import (
	_ "fmt"
	_ "sync"
	_ "unicode"

	"github.com/antlr4-go/antlr"
	_ "github.com/antlr4-go/antlr/v4"
)

// Suppress unused import error

type CanonicalTypeSignatureLexer struct {
	*antlr.BaseLexer

	// TODO: EOF string
}

// CanonicalTypeSignatureLexerInit initializes any static state used to implement CanonicalTypeSignatureLexer. By default the
// static state used to implement the lexer is lazily initialized during the first call to
// NewCanonicalTypeSignatureLexer(). You can call this function if you wish to initialize the static state ahead
// of time.
func CanonicalTypeSignatureLexerInit() { panic("stub") }

// NewCanonicalTypeSignatureLexer produces a new lexer instance for the optional input antlr.CharStream.
func NewCanonicalTypeSignatureLexer(input antlr.CharStream) *CanonicalTypeSignatureLexer {
	panic("stub")
}

// TODO: l.EOF = antlr.TokenEOF

// CanonicalTypeSignatureLexer tokens.
const (
	CanonicalTypeSignatureLexerSEPARATOR        = 1
	CanonicalTypeSignatureLexerGROUP_SEPARATOR  = 2
	CanonicalTypeSignatureLexerUTF8_STRING      = 3
	CanonicalTypeSignatureLexerBYTE_STRING      = 4
	CanonicalTypeSignatureLexerBOOL             = 5
	CanonicalTypeSignatureLexerUNSIGNED         = 6
	CanonicalTypeSignatureLexerSIGNED           = 7
	CanonicalTypeSignatureLexerFLOAT            = 8
	CanonicalTypeSignatureLexerUTC_DATETIME     = 9
	CanonicalTypeSignatureLexerZONED_DATETIME   = 10
	CanonicalTypeSignatureLexerZONED_TIME       = 11
	CanonicalTypeSignatureLexerHOMOGENOUS_ARRAY = 12
	CanonicalTypeSignatureLexerSET              = 13
	CanonicalTypeSignatureLexerLITTLE_ENDIAN    = 14
	CanonicalTypeSignatureLexerBIG_ENDIAN       = 15
	CanonicalTypeSignatureLexerFIXED_MODIFIER   = 16
	CanonicalTypeSignatureLexerNUMBER           = 17
)


```

--- FILE: canonicaltypes/grammar/canonicaltypesignature_parser.out.go ---
```go
// Code generated from CanonicalTypeSignatureParser.g4 by ANTLR 4.13.1. DO NOT EDIT.

package grammar // CanonicalTypeSignatureParser
import (
	_ "fmt"
	_ "strconv"
	_ "sync"

	"github.com/antlr4-go/antlr"
	_ "github.com/antlr4-go/antlr/v4"
)

// Suppress unused import errors

type CanonicalTypeSignatureParser struct {
	*antlr.BaseParser
}

// CanonicalTypeSignatureParserInit initializes any static state used to implement CanonicalTypeSignatureParser. By default the
// static state used to implement the parser is lazily initialized during the first call to
// NewCanonicalTypeSignatureParser(). You can call this function if you wish to initialize the static state ahead
// of time.
func CanonicalTypeSignatureParserInit() { panic("stub") }

// NewCanonicalTypeSignatureParser produces a new parser instance for the optional input antlr.TokenStream.
func NewCanonicalTypeSignatureParser(input antlr.TokenStream) *CanonicalTypeSignatureParser {
	panic("stub")
}

// CanonicalTypeSignatureParser tokens.
const (
	CanonicalTypeSignatureParserEOF              = antlr.TokenEOF
	CanonicalTypeSignatureParserSEPARATOR        = 1
	CanonicalTypeSignatureParserGROUP_SEPARATOR  = 2
	CanonicalTypeSignatureParserUTF8_STRING      = 3
	CanonicalTypeSignatureParserBYTE_STRING      = 4
	CanonicalTypeSignatureParserBOOL             = 5
	CanonicalTypeSignatureParserUNSIGNED         = 6
	CanonicalTypeSignatureParserSIGNED           = 7
	CanonicalTypeSignatureParserFLOAT            = 8
	CanonicalTypeSignatureParserUTC_DATETIME     = 9
	CanonicalTypeSignatureParserZONED_DATETIME   = 10
	CanonicalTypeSignatureParserZONED_TIME       = 11
	CanonicalTypeSignatureParserHOMOGENOUS_ARRAY = 12
	CanonicalTypeSignatureParserSET              = 13
	CanonicalTypeSignatureParserLITTLE_ENDIAN    = 14
	CanonicalTypeSignatureParserBIG_ENDIAN       = 15
	CanonicalTypeSignatureParserFIXED_MODIFIER   = 16
	CanonicalTypeSignatureParserNUMBER           = 17
)

// CanonicalTypeSignatureParser rules.
const (
	CanonicalTypeSignatureParserRULE_baseString                   = 0
	CanonicalTypeSignatureParserRULE_baseMachineNumeric           = 1
	CanonicalTypeSignatureParserRULE_baseTemporal                 = 2
	CanonicalTypeSignatureParserRULE_scalarModifier               = 3
	CanonicalTypeSignatureParserRULE_byteOrderModifier            = 4
	CanonicalTypeSignatureParserRULE_widthModifier                = 5
	CanonicalTypeSignatureParserRULE_canonicalType                = 6
	CanonicalTypeSignatureParserRULE_canonicalTypeSequence        = 7
	CanonicalTypeSignatureParserRULE_canonicalTypeGroup           = 8
	CanonicalTypeSignatureParserRULE_canonicalTypeOrGroup         = 9
	CanonicalTypeSignatureParserRULE_canonicalTypeOrGroupSequence = 10
	CanonicalTypeSignatureParserRULE_canonicalTypeSignature       = 11
	CanonicalTypeSignatureParserRULE_singleCanonicalType          = 12
	CanonicalTypeSignatureParserRULE_singleCanonicalTypeOrGroup   = 13
	CanonicalTypeSignatureParserRULE_singleCanonicalGroup         = 14
)

// IBaseStringContext is an interface to support dynamic dispatch.
type IBaseStringContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// Getter signatures
	UTF8_STRING() antlr.TerminalNode
	BYTE_STRING() antlr.TerminalNode
	BOOL() antlr.TerminalNode

	// IsBaseStringContext differentiates from other interfaces.
	IsBaseStringContext()
}

type BaseStringContext struct {
	antlr.BaseParserRuleContext
}

func NewEmptyBaseStringContext() *BaseStringContext { panic("stub") }

func InitEmptyBaseStringContext(p *BaseStringContext) { panic("stub") }

func (*BaseStringContext) IsBaseStringContext() { panic("stub") }

func NewBaseStringContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *BaseStringContext {
	panic("stub")
}

func (s *BaseStringContext) GetParser() antlr.Parser { panic("stub") }

func (s *BaseStringContext) UTF8_STRING() antlr.TerminalNode { panic("stub") }

func (s *BaseStringContext) BYTE_STRING() antlr.TerminalNode { panic("stub") }

func (s *BaseStringContext) BOOL() antlr.TerminalNode { panic("stub") }

func (s *BaseStringContext) GetRuleContext() antlr.RuleContext { panic("stub") }

func (s *BaseStringContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	panic("stub")
}

func (s *BaseStringContext) Accept(visitor antlr.ParseTreeVisitor) interface{} { panic("stub") }

func (p *CanonicalTypeSignatureParser) BaseString() (localctx IBaseStringContext) { panic("stub") }

// Trick to prevent compiler error if the label is not used

// IBaseMachineNumericContext is an interface to support dynamic dispatch.
type IBaseMachineNumericContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// Getter signatures
	UNSIGNED() antlr.TerminalNode
	SIGNED() antlr.TerminalNode
	FLOAT() antlr.TerminalNode

	// IsBaseMachineNumericContext differentiates from other interfaces.
	IsBaseMachineNumericContext()
}

type BaseMachineNumericContext struct {
	antlr.BaseParserRuleContext
}

func NewEmptyBaseMachineNumericContext() *BaseMachineNumericContext { panic("stub") }

func InitEmptyBaseMachineNumericContext(p *BaseMachineNumericContext) { panic("stub") }

func (*BaseMachineNumericContext) IsBaseMachineNumericContext() { panic("stub") }

func NewBaseMachineNumericContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *BaseMachineNumericContext {
	panic("stub")
}

func (s *BaseMachineNumericContext) GetParser() antlr.Parser { panic("stub") }

func (s *BaseMachineNumericContext) UNSIGNED() antlr.TerminalNode { panic("stub") }

func (s *BaseMachineNumericContext) SIGNED() antlr.TerminalNode { panic("stub") }

func (s *BaseMachineNumericContext) FLOAT() antlr.TerminalNode { panic("stub") }

func (s *BaseMachineNumericContext) GetRuleContext() antlr.RuleContext { panic("stub") }

func (s *BaseMachineNumericContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	panic("stub")
}

func (s *BaseMachineNumericContext) Accept(visitor antlr.ParseTreeVisitor) interface{} { panic("stub") }

func (p *CanonicalTypeSignatureParser) BaseMachineNumeric() (localctx IBaseMachineNumericContext) {
	panic("stub")
}

// Trick to prevent compiler error if the label is not used

// IBaseTemporalContext is an interface to support dynamic dispatch.
type IBaseTemporalContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// Getter signatures
	UTC_DATETIME() antlr.TerminalNode
	ZONED_DATETIME() antlr.TerminalNode
	ZONED_TIME() antlr.TerminalNode

	// IsBaseTemporalContext differentiates from other interfaces.
	IsBaseTemporalContext()
}

type BaseTemporalContext struct {
	antlr.BaseParserRuleContext
}

func NewEmptyBaseTemporalContext() *BaseTemporalContext { panic("stub") }

func InitEmptyBaseTemporalContext(p *BaseTemporalContext) { panic("stub") }

func (*BaseTemporalContext) IsBaseTemporalContext() { panic("stub") }

func NewBaseTemporalContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *BaseTemporalContext {
	panic("stub")
}

func (s *BaseTemporalContext) GetParser() antlr.Parser { panic("stub") }

func (s *BaseTemporalContext) UTC_DATETIME() antlr.TerminalNode { panic("stub") }

func (s *BaseTemporalContext) ZONED_DATETIME() antlr.TerminalNode { panic("stub") }

func (s *BaseTemporalContext) ZONED_TIME() antlr.TerminalNode { panic("stub") }

func (s *BaseTemporalContext) GetRuleContext() antlr.RuleContext { panic("stub") }

func (s *BaseTemporalContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	panic("stub")
}

func (s *BaseTemporalContext) Accept(visitor antlr.ParseTreeVisitor) interface{} { panic("stub") }

func (p *CanonicalTypeSignatureParser) BaseTemporal() (localctx IBaseTemporalContext) { panic("stub") }

// Trick to prevent compiler error if the label is not used

// IScalarModifierContext is an interface to support dynamic dispatch.
type IScalarModifierContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// Getter signatures
	HOMOGENOUS_ARRAY() antlr.TerminalNode
	SET() antlr.TerminalNode

	// IsScalarModifierContext differentiates from other interfaces.
	IsScalarModifierContext()
}

type ScalarModifierContext struct {
	antlr.BaseParserRuleContext
}

func NewEmptyScalarModifierContext() *ScalarModifierContext { panic("stub") }

func InitEmptyScalarModifierContext(p *ScalarModifierContext) { panic("stub") }

func (*ScalarModifierContext) IsScalarModifierContext() { panic("stub") }

func NewScalarModifierContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *ScalarModifierContext {
	panic("stub")
}

func (s *ScalarModifierContext) GetParser() antlr.Parser { panic("stub") }

func (s *ScalarModifierContext) HOMOGENOUS_ARRAY() antlr.TerminalNode { panic("stub") }

func (s *ScalarModifierContext) SET() antlr.TerminalNode { panic("stub") }

func (s *ScalarModifierContext) GetRuleContext() antlr.RuleContext { panic("stub") }

func (s *ScalarModifierContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	panic("stub")
}

func (s *ScalarModifierContext) Accept(visitor antlr.ParseTreeVisitor) interface{} { panic("stub") }

func (p *CanonicalTypeSignatureParser) ScalarModifier() (localctx IScalarModifierContext) {
	panic("stub")
}

// Trick to prevent compiler error if the label is not used

// IByteOrderModifierContext is an interface to support dynamic dispatch.
type IByteOrderModifierContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// Getter signatures
	BIG_ENDIAN() antlr.TerminalNode
	LITTLE_ENDIAN() antlr.TerminalNode

	// IsByteOrderModifierContext differentiates from other interfaces.
	IsByteOrderModifierContext()
}

type ByteOrderModifierContext struct {
	antlr.BaseParserRuleContext
}

func NewEmptyByteOrderModifierContext() *ByteOrderModifierContext { panic("stub") }

func InitEmptyByteOrderModifierContext(p *ByteOrderModifierContext) { panic("stub") }

func (*ByteOrderModifierContext) IsByteOrderModifierContext() { panic("stub") }

func NewByteOrderModifierContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *ByteOrderModifierContext {
	panic("stub")
}

func (s *ByteOrderModifierContext) GetParser() antlr.Parser { panic("stub") }

func (s *ByteOrderModifierContext) BIG_ENDIAN() antlr.TerminalNode { panic("stub") }

func (s *ByteOrderModifierContext) LITTLE_ENDIAN() antlr.TerminalNode { panic("stub") }

func (s *ByteOrderModifierContext) GetRuleContext() antlr.RuleContext { panic("stub") }

func (s *ByteOrderModifierContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	panic("stub")
}

func (s *ByteOrderModifierContext) Accept(visitor antlr.ParseTreeVisitor) interface{} { panic("stub") }

func (p *CanonicalTypeSignatureParser) ByteOrderModifier() (localctx IByteOrderModifierContext) {
	panic("stub")
}

// Trick to prevent compiler error if the label is not used

// IWidthModifierContext is an interface to support dynamic dispatch.
type IWidthModifierContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// Getter signatures
	FIXED_MODIFIER() antlr.TerminalNode
	NUMBER() antlr.TerminalNode

	// IsWidthModifierContext differentiates from other interfaces.
	IsWidthModifierContext()
}

type WidthModifierContext struct {
	antlr.BaseParserRuleContext
}

func NewEmptyWidthModifierContext() *WidthModifierContext { panic("stub") }

func InitEmptyWidthModifierContext(p *WidthModifierContext) { panic("stub") }

func (*WidthModifierContext) IsWidthModifierContext() { panic("stub") }

func NewWidthModifierContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *WidthModifierContext {
	panic("stub")
}

func (s *WidthModifierContext) GetParser() antlr.Parser { panic("stub") }

func (s *WidthModifierContext) FIXED_MODIFIER() antlr.TerminalNode { panic("stub") }

func (s *WidthModifierContext) NUMBER() antlr.TerminalNode { panic("stub") }

func (s *WidthModifierContext) GetRuleContext() antlr.RuleContext { panic("stub") }

func (s *WidthModifierContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	panic("stub")
}

func (s *WidthModifierContext) Accept(visitor antlr.ParseTreeVisitor) interface{} { panic("stub") }

func (p *CanonicalTypeSignatureParser) WidthModifier() (localctx IWidthModifierContext) {
	panic("stub")
}

// Recognition error - abort rule

// Recognition error - abort rule

// Trick to prevent compiler error if the label is not used

// ICanonicalTypeContext is an interface to support dynamic dispatch.
type ICanonicalTypeContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser
	// IsCanonicalTypeContext differentiates from other interfaces.
	IsCanonicalTypeContext()
}

type CanonicalTypeContext struct {
	antlr.BaseParserRuleContext
}

func NewEmptyCanonicalTypeContext() *CanonicalTypeContext { panic("stub") }

func InitEmptyCanonicalTypeContext(p *CanonicalTypeContext) { panic("stub") }

func (*CanonicalTypeContext) IsCanonicalTypeContext() { panic("stub") }

func NewCanonicalTypeContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *CanonicalTypeContext {
	panic("stub")
}

func (s *CanonicalTypeContext) GetParser() antlr.Parser { panic("stub") }

func (s *CanonicalTypeContext) CopyAll(ctx *CanonicalTypeContext) { panic("stub") }

func (s *CanonicalTypeContext) GetRuleContext() antlr.RuleContext { panic("stub") }

func (s *CanonicalTypeContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	panic("stub")
}

type CanonicalTypeTemporalContext struct {
	CanonicalTypeContext
}

func NewCanonicalTypeTemporalContext(parser antlr.Parser, ctx antlr.ParserRuleContext) *CanonicalTypeTemporalContext {
	panic("stub")
}

func (s *CanonicalTypeTemporalContext) GetRuleContext() antlr.RuleContext { panic("stub") }

func (s *CanonicalTypeTemporalContext) BaseTemporal() IBaseTemporalContext { panic("stub") }

func (s *CanonicalTypeTemporalContext) NUMBER() antlr.TerminalNode { panic("stub") }

func (s *CanonicalTypeTemporalContext) ScalarModifier() IScalarModifierContext { panic("stub") }

func (s *CanonicalTypeTemporalContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	panic("stub")
}

type CanonicalTypeMachineNumericContext struct {
	CanonicalTypeContext
}

func NewCanonicalTypeMachineNumericContext(parser antlr.Parser, ctx antlr.ParserRuleContext) *CanonicalTypeMachineNumericContext {
	panic("stub")
}

func (s *CanonicalTypeMachineNumericContext) GetRuleContext() antlr.RuleContext { panic("stub") }

func (s *CanonicalTypeMachineNumericContext) BaseMachineNumeric() IBaseMachineNumericContext {
	panic("stub")
}

func (s *CanonicalTypeMachineNumericContext) NUMBER() antlr.TerminalNode { panic("stub") }

func (s *CanonicalTypeMachineNumericContext) ByteOrderModifier() IByteOrderModifierContext {
	panic("stub")
}

func (s *CanonicalTypeMachineNumericContext) ScalarModifier() IScalarModifierContext { panic("stub") }

func (s *CanonicalTypeMachineNumericContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	panic("stub")
}

type CanonicalTypeStringContext struct {
	CanonicalTypeContext
}

func NewCanonicalTypeStringContext(parser antlr.Parser, ctx antlr.ParserRuleContext) *CanonicalTypeStringContext {
	panic("stub")
}

func (s *CanonicalTypeStringContext) GetRuleContext() antlr.RuleContext { panic("stub") }

func (s *CanonicalTypeStringContext) BaseString() IBaseStringContext { panic("stub") }

func (s *CanonicalTypeStringContext) WidthModifier() IWidthModifierContext { panic("stub") }

func (s *CanonicalTypeStringContext) ScalarModifier() IScalarModifierContext { panic("stub") }

func (s *CanonicalTypeStringContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	panic("stub")
}

func (p *CanonicalTypeSignatureParser) CanonicalType() (localctx ICanonicalTypeContext) {
	panic("stub")
}

// Recognition error - abort rule

// Recognition error - abort rule

// Trick to prevent compiler error if the label is not used

// ICanonicalTypeSequenceContext is an interface to support dynamic dispatch.
type ICanonicalTypeSequenceContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// Getter signatures
	AllCanonicalType() []ICanonicalTypeContext
	CanonicalType(i int) ICanonicalTypeContext
	AllSEPARATOR() []antlr.TerminalNode
	SEPARATOR(i int) antlr.TerminalNode

	// IsCanonicalTypeSequenceContext differentiates from other interfaces.
	IsCanonicalTypeSequenceContext()
}

type CanonicalTypeSequenceContext struct {
	antlr.BaseParserRuleContext
}

func NewEmptyCanonicalTypeSequenceContext() *CanonicalTypeSequenceContext { panic("stub") }

func InitEmptyCanonicalTypeSequenceContext(p *CanonicalTypeSequenceContext) { panic("stub") }

func (*CanonicalTypeSequenceContext) IsCanonicalTypeSequenceContext() { panic("stub") }

func NewCanonicalTypeSequenceContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *CanonicalTypeSequenceContext {
	panic("stub")
}

func (s *CanonicalTypeSequenceContext) GetParser() antlr.Parser { panic("stub") }

func (s *CanonicalTypeSequenceContext) AllCanonicalType() []ICanonicalTypeContext { panic("stub") }

func (s *CanonicalTypeSequenceContext) CanonicalType(i int) ICanonicalTypeContext { panic("stub") }

func (s *CanonicalTypeSequenceContext) AllSEPARATOR() []antlr.TerminalNode { panic("stub") }

func (s *CanonicalTypeSequenceContext) SEPARATOR(i int) antlr.TerminalNode { panic("stub") }

func (s *CanonicalTypeSequenceContext) GetRuleContext() antlr.RuleContext { panic("stub") }

func (s *CanonicalTypeSequenceContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	panic("stub")
}

func (s *CanonicalTypeSequenceContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	panic("stub")
}

func (p *CanonicalTypeSignatureParser) CanonicalTypeSequence() (localctx ICanonicalTypeSequenceContext) {
	panic("stub")
}

// Recognition error - abort rule

// Trick to prevent compiler error if the label is not used

// ICanonicalTypeGroupContext is an interface to support dynamic dispatch.
type ICanonicalTypeGroupContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// Getter signatures
	AllCanonicalType() []ICanonicalTypeContext
	CanonicalType(i int) ICanonicalTypeContext
	AllGROUP_SEPARATOR() []antlr.TerminalNode
	GROUP_SEPARATOR(i int) antlr.TerminalNode

	// IsCanonicalTypeGroupContext differentiates from other interfaces.
	IsCanonicalTypeGroupContext()
}

type CanonicalTypeGroupContext struct {
	antlr.BaseParserRuleContext
}

func NewEmptyCanonicalTypeGroupContext() *CanonicalTypeGroupContext { panic("stub") }

func InitEmptyCanonicalTypeGroupContext(p *CanonicalTypeGroupContext) { panic("stub") }

func (*CanonicalTypeGroupContext) IsCanonicalTypeGroupContext() { panic("stub") }

func NewCanonicalTypeGroupContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *CanonicalTypeGroupContext {
	panic("stub")
}

func (s *CanonicalTypeGroupContext) GetParser() antlr.Parser { panic("stub") }

func (s *CanonicalTypeGroupContext) AllCanonicalType() []ICanonicalTypeContext { panic("stub") }

func (s *CanonicalTypeGroupContext) CanonicalType(i int) ICanonicalTypeContext { panic("stub") }

func (s *CanonicalTypeGroupContext) AllGROUP_SEPARATOR() []antlr.TerminalNode { panic("stub") }

func (s *CanonicalTypeGroupContext) GROUP_SEPARATOR(i int) antlr.TerminalNode { panic("stub") }

func (s *CanonicalTypeGroupContext) GetRuleContext() antlr.RuleContext { panic("stub") }

func (s *CanonicalTypeGroupContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	panic("stub")
}

func (s *CanonicalTypeGroupContext) Accept(visitor antlr.ParseTreeVisitor) interface{} { panic("stub") }

func (p *CanonicalTypeSignatureParser) CanonicalTypeGroup() (localctx ICanonicalTypeGroupContext) {
	panic("stub")
}

// Recognition error - abort rule

// Trick to prevent compiler error if the label is not used

// ICanonicalTypeOrGroupContext is an interface to support dynamic dispatch.
type ICanonicalTypeOrGroupContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// Getter signatures
	CanonicalType() ICanonicalTypeContext
	CanonicalTypeGroup() ICanonicalTypeGroupContext

	// IsCanonicalTypeOrGroupContext differentiates from other interfaces.
	IsCanonicalTypeOrGroupContext()
}

type CanonicalTypeOrGroupContext struct {
	antlr.BaseParserRuleContext
}

func NewEmptyCanonicalTypeOrGroupContext() *CanonicalTypeOrGroupContext { panic("stub") }

func InitEmptyCanonicalTypeOrGroupContext(p *CanonicalTypeOrGroupContext) { panic("stub") }

func (*CanonicalTypeOrGroupContext) IsCanonicalTypeOrGroupContext() { panic("stub") }

func NewCanonicalTypeOrGroupContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *CanonicalTypeOrGroupContext {
	panic("stub")
}

func (s *CanonicalTypeOrGroupContext) GetParser() antlr.Parser { panic("stub") }

func (s *CanonicalTypeOrGroupContext) CanonicalType() ICanonicalTypeContext { panic("stub") }

func (s *CanonicalTypeOrGroupContext) CanonicalTypeGroup() ICanonicalTypeGroupContext { panic("stub") }

func (s *CanonicalTypeOrGroupContext) GetRuleContext() antlr.RuleContext { panic("stub") }

func (s *CanonicalTypeOrGroupContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	panic("stub")
}

func (s *CanonicalTypeOrGroupContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	panic("stub")
}

func (p *CanonicalTypeSignatureParser) CanonicalTypeOrGroup() (localctx ICanonicalTypeOrGroupContext) {
	panic("stub")
}

// Trick to prevent compiler error if the label is not used

// ICanonicalTypeOrGroupSequenceContext is an interface to support dynamic dispatch.
type ICanonicalTypeOrGroupSequenceContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// Getter signatures
	AllCanonicalTypeOrGroup() []ICanonicalTypeOrGroupContext
	CanonicalTypeOrGroup(i int) ICanonicalTypeOrGroupContext
	AllSEPARATOR() []antlr.TerminalNode
	SEPARATOR(i int) antlr.TerminalNode

	// IsCanonicalTypeOrGroupSequenceContext differentiates from other interfaces.
	IsCanonicalTypeOrGroupSequenceContext()
}

type CanonicalTypeOrGroupSequenceContext struct {
	antlr.BaseParserRuleContext
}

func NewEmptyCanonicalTypeOrGroupSequenceContext() *CanonicalTypeOrGroupSequenceContext {
	panic("stub")
}

func InitEmptyCanonicalTypeOrGroupSequenceContext(p *CanonicalTypeOrGroupSequenceContext) {
	panic("stub")
}

func (*CanonicalTypeOrGroupSequenceContext) IsCanonicalTypeOrGroupSequenceContext() { panic("stub") }

func NewCanonicalTypeOrGroupSequenceContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *CanonicalTypeOrGroupSequenceContext {
	panic("stub")
}

func (s *CanonicalTypeOrGroupSequenceContext) GetParser() antlr.Parser { panic("stub") }

func (s *CanonicalTypeOrGroupSequenceContext) AllCanonicalTypeOrGroup() []ICanonicalTypeOrGroupContext {
	panic("stub")
}

func (s *CanonicalTypeOrGroupSequenceContext) CanonicalTypeOrGroup(i int) ICanonicalTypeOrGroupContext {
	panic("stub")
}

func (s *CanonicalTypeOrGroupSequenceContext) AllSEPARATOR() []antlr.TerminalNode { panic("stub") }

func (s *CanonicalTypeOrGroupSequenceContext) SEPARATOR(i int) antlr.TerminalNode { panic("stub") }

func (s *CanonicalTypeOrGroupSequenceContext) GetRuleContext() antlr.RuleContext { panic("stub") }

func (s *CanonicalTypeOrGroupSequenceContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	panic("stub")
}

func (s *CanonicalTypeOrGroupSequenceContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	panic("stub")
}

func (p *CanonicalTypeSignatureParser) CanonicalTypeOrGroupSequence() (localctx ICanonicalTypeOrGroupSequenceContext) {
	panic("stub")
}

// Recognition error - abort rule

// Trick to prevent compiler error if the label is not used

// ICanonicalTypeSignatureContext is an interface to support dynamic dispatch.
type ICanonicalTypeSignatureContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// Getter signatures
	CanonicalTypeOrGroupSequence() ICanonicalTypeOrGroupSequenceContext
	EOF() antlr.TerminalNode

	// IsCanonicalTypeSignatureContext differentiates from other interfaces.
	IsCanonicalTypeSignatureContext()
}

type CanonicalTypeSignatureContext struct {
	antlr.BaseParserRuleContext
}

func NewEmptyCanonicalTypeSignatureContext() *CanonicalTypeSignatureContext { panic("stub") }

func InitEmptyCanonicalTypeSignatureContext(p *CanonicalTypeSignatureContext) { panic("stub") }

func (*CanonicalTypeSignatureContext) IsCanonicalTypeSignatureContext() { panic("stub") }

func NewCanonicalTypeSignatureContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *CanonicalTypeSignatureContext {
	panic("stub")
}

func (s *CanonicalTypeSignatureContext) GetParser() antlr.Parser { panic("stub") }

func (s *CanonicalTypeSignatureContext) CanonicalTypeOrGroupSequence() ICanonicalTypeOrGroupSequenceContext {
	panic("stub")
}

func (s *CanonicalTypeSignatureContext) EOF() antlr.TerminalNode { panic("stub") }

func (s *CanonicalTypeSignatureContext) GetRuleContext() antlr.RuleContext { panic("stub") }

func (s *CanonicalTypeSignatureContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	panic("stub")
}

func (s *CanonicalTypeSignatureContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	panic("stub")
}

func (p *CanonicalTypeSignatureParser) CanonicalTypeSignature() (localctx ICanonicalTypeSignatureContext) {
	panic("stub")
}

// Recognition error - abort rule

// Trick to prevent compiler error if the label is not used

// ISingleCanonicalTypeContext is an interface to support dynamic dispatch.
type ISingleCanonicalTypeContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// Getter signatures
	CanonicalType() ICanonicalTypeContext
	EOF() antlr.TerminalNode

	// IsSingleCanonicalTypeContext differentiates from other interfaces.
	IsSingleCanonicalTypeContext()
}

type SingleCanonicalTypeContext struct {
	antlr.BaseParserRuleContext
}

func NewEmptySingleCanonicalTypeContext() *SingleCanonicalTypeContext { panic("stub") }

func InitEmptySingleCanonicalTypeContext(p *SingleCanonicalTypeContext) { panic("stub") }

func (*SingleCanonicalTypeContext) IsSingleCanonicalTypeContext() { panic("stub") }

func NewSingleCanonicalTypeContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *SingleCanonicalTypeContext {
	panic("stub")
}

func (s *SingleCanonicalTypeContext) GetParser() antlr.Parser { panic("stub") }

func (s *SingleCanonicalTypeContext) CanonicalType() ICanonicalTypeContext { panic("stub") }

func (s *SingleCanonicalTypeContext) EOF() antlr.TerminalNode { panic("stub") }

func (s *SingleCanonicalTypeContext) GetRuleContext() antlr.RuleContext { panic("stub") }

func (s *SingleCanonicalTypeContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	panic("stub")
}

func (s *SingleCanonicalTypeContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	panic("stub")
}

func (p *CanonicalTypeSignatureParser) SingleCanonicalType() (localctx ISingleCanonicalTypeContext) {
	panic("stub")
}

// Recognition error - abort rule

// Trick to prevent compiler error if the label is not used

// ISingleCanonicalTypeOrGroupContext is an interface to support dynamic dispatch.
type ISingleCanonicalTypeOrGroupContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// Getter signatures
	CanonicalTypeOrGroup() ICanonicalTypeOrGroupContext
	EOF() antlr.TerminalNode

	// IsSingleCanonicalTypeOrGroupContext differentiates from other interfaces.
	IsSingleCanonicalTypeOrGroupContext()
}

type SingleCanonicalTypeOrGroupContext struct {
	antlr.BaseParserRuleContext
}

func NewEmptySingleCanonicalTypeOrGroupContext() *SingleCanonicalTypeOrGroupContext { panic("stub") }

func InitEmptySingleCanonicalTypeOrGroupContext(p *SingleCanonicalTypeOrGroupContext) { panic("stub") }

func (*SingleCanonicalTypeOrGroupContext) IsSingleCanonicalTypeOrGroupContext() { panic("stub") }

func NewSingleCanonicalTypeOrGroupContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *SingleCanonicalTypeOrGroupContext {
	panic("stub")
}

func (s *SingleCanonicalTypeOrGroupContext) GetParser() antlr.Parser { panic("stub") }

func (s *SingleCanonicalTypeOrGroupContext) CanonicalTypeOrGroup() ICanonicalTypeOrGroupContext {
	panic("stub")
}

func (s *SingleCanonicalTypeOrGroupContext) EOF() antlr.TerminalNode { panic("stub") }

func (s *SingleCanonicalTypeOrGroupContext) GetRuleContext() antlr.RuleContext { panic("stub") }

func (s *SingleCanonicalTypeOrGroupContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	panic("stub")
}

func (s *SingleCanonicalTypeOrGroupContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	panic("stub")
}

func (p *CanonicalTypeSignatureParser) SingleCanonicalTypeOrGroup() (localctx ISingleCanonicalTypeOrGroupContext) {
	panic("stub")
}

// Recognition error - abort rule

// Trick to prevent compiler error if the label is not used

// ISingleCanonicalGroupContext is an interface to support dynamic dispatch.
type ISingleCanonicalGroupContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// Getter signatures
	CanonicalTypeGroup() ICanonicalTypeGroupContext
	EOF() antlr.TerminalNode

	// IsSingleCanonicalGroupContext differentiates from other interfaces.
	IsSingleCanonicalGroupContext()
}

type SingleCanonicalGroupContext struct {
	antlr.BaseParserRuleContext
}

func NewEmptySingleCanonicalGroupContext() *SingleCanonicalGroupContext { panic("stub") }

func InitEmptySingleCanonicalGroupContext(p *SingleCanonicalGroupContext) { panic("stub") }

func (*SingleCanonicalGroupContext) IsSingleCanonicalGroupContext() { panic("stub") }

func NewSingleCanonicalGroupContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *SingleCanonicalGroupContext {
	panic("stub")
}

func (s *SingleCanonicalGroupContext) GetParser() antlr.Parser { panic("stub") }

func (s *SingleCanonicalGroupContext) CanonicalTypeGroup() ICanonicalTypeGroupContext { panic("stub") }

func (s *SingleCanonicalGroupContext) EOF() antlr.TerminalNode { panic("stub") }

func (s *SingleCanonicalGroupContext) GetRuleContext() antlr.RuleContext { panic("stub") }

func (s *SingleCanonicalGroupContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	panic("stub")
}

func (s *SingleCanonicalGroupContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	panic("stub")
}

func (p *CanonicalTypeSignatureParser) SingleCanonicalGroup() (localctx ISingleCanonicalGroupContext) {
	panic("stub")
}

// Recognition error - abort rule

// Trick to prevent compiler error if the label is not used


```

--- FILE: canonicaltypes/grammar/canonicaltypesignatureparser_base_visitor.out.go ---
```go
// Code generated from CanonicalTypeSignatureParser.g4 by ANTLR 4.13.1. DO NOT EDIT.

package grammar // CanonicalTypeSignatureParser
import (
	"github.com/antlr4-go/antlr"
	_ "github.com/antlr4-go/antlr/v4"
)

type BaseCanonicalTypeSignatureParserVisitor struct {
	*antlr.BaseParseTreeVisitor
}

func (v *BaseCanonicalTypeSignatureParserVisitor) VisitBaseString(ctx *BaseStringContext) interface{} {
	panic("stub")
}

func (v *BaseCanonicalTypeSignatureParserVisitor) VisitBaseMachineNumeric(ctx *BaseMachineNumericContext) interface{} {
	panic("stub")
}

func (v *BaseCanonicalTypeSignatureParserVisitor) VisitBaseTemporal(ctx *BaseTemporalContext) interface{} {
	panic("stub")
}

func (v *BaseCanonicalTypeSignatureParserVisitor) VisitScalarModifier(ctx *ScalarModifierContext) interface{} {
	panic("stub")
}

func (v *BaseCanonicalTypeSignatureParserVisitor) VisitByteOrderModifier(ctx *ByteOrderModifierContext) interface{} {
	panic("stub")
}

func (v *BaseCanonicalTypeSignatureParserVisitor) VisitWidthModifier(ctx *WidthModifierContext) interface{} {
	panic("stub")
}

func (v *BaseCanonicalTypeSignatureParserVisitor) VisitCanonicalTypeString(ctx *CanonicalTypeStringContext) interface{} {
	panic("stub")
}

func (v *BaseCanonicalTypeSignatureParserVisitor) VisitCanonicalTypeTemporal(ctx *CanonicalTypeTemporalContext) interface{} {
	panic("stub")
}

func (v *BaseCanonicalTypeSignatureParserVisitor) VisitCanonicalTypeMachineNumeric(ctx *CanonicalTypeMachineNumericContext) interface{} {
	panic("stub")
}

func (v *BaseCanonicalTypeSignatureParserVisitor) VisitCanonicalTypeSequence(ctx *CanonicalTypeSequenceContext) interface{} {
	panic("stub")
}

func (v *BaseCanonicalTypeSignatureParserVisitor) VisitCanonicalTypeGroup(ctx *CanonicalTypeGroupContext) interface{} {
	panic("stub")
}

func (v *BaseCanonicalTypeSignatureParserVisitor) VisitCanonicalTypeOrGroup(ctx *CanonicalTypeOrGroupContext) interface{} {
	panic("stub")
}

func (v *BaseCanonicalTypeSignatureParserVisitor) VisitCanonicalTypeOrGroupSequence(ctx *CanonicalTypeOrGroupSequenceContext) interface{} {
	panic("stub")
}

func (v *BaseCanonicalTypeSignatureParserVisitor) VisitCanonicalTypeSignature(ctx *CanonicalTypeSignatureContext) interface{} {
	panic("stub")
}

func (v *BaseCanonicalTypeSignatureParserVisitor) VisitSingleCanonicalType(ctx *SingleCanonicalTypeContext) interface{} {
	panic("stub")
}

func (v *BaseCanonicalTypeSignatureParserVisitor) VisitSingleCanonicalTypeOrGroup(ctx *SingleCanonicalTypeOrGroupContext) interface{} {
	panic("stub")
}

func (v *BaseCanonicalTypeSignatureParserVisitor) VisitSingleCanonicalGroup(ctx *SingleCanonicalGroupContext) interface{} {
	panic("stub")
}


```

--- FILE: canonicaltypes/grammar/canonicaltypesignatureparser_visitor.out.go ---
```go
// Code generated from CanonicalTypeSignatureParser.g4 by ANTLR 4.13.1. DO NOT EDIT.

package grammar // CanonicalTypeSignatureParser
import (
	"github.com/antlr4-go/antlr"
	_ "github.com/antlr4-go/antlr/v4"
)

// A complete Visitor for a parse tree produced by CanonicalTypeSignatureParser.
type CanonicalTypeSignatureParserVisitor interface {
	antlr.ParseTreeVisitor

	// Visit a parse tree produced by CanonicalTypeSignatureParser#baseString.
	VisitBaseString(ctx *BaseStringContext) interface{}

	// Visit a parse tree produced by CanonicalTypeSignatureParser#baseMachineNumeric.
	VisitBaseMachineNumeric(ctx *BaseMachineNumericContext) interface{}

	// Visit a parse tree produced by CanonicalTypeSignatureParser#baseTemporal.
	VisitBaseTemporal(ctx *BaseTemporalContext) interface{}

	// Visit a parse tree produced by CanonicalTypeSignatureParser#scalarModifier.
	VisitScalarModifier(ctx *ScalarModifierContext) interface{}

	// Visit a parse tree produced by CanonicalTypeSignatureParser#byteOrderModifier.
	VisitByteOrderModifier(ctx *ByteOrderModifierContext) interface{}

	// Visit a parse tree produced by CanonicalTypeSignatureParser#widthModifier.
	VisitWidthModifier(ctx *WidthModifierContext) interface{}

	// Visit a parse tree produced by CanonicalTypeSignatureParser#CanonicalTypeString.
	VisitCanonicalTypeString(ctx *CanonicalTypeStringContext) interface{}

	// Visit a parse tree produced by CanonicalTypeSignatureParser#CanonicalTypeTemporal.
	VisitCanonicalTypeTemporal(ctx *CanonicalTypeTemporalContext) interface{}

	// Visit a parse tree produced by CanonicalTypeSignatureParser#CanonicalTypeMachineNumeric.
	VisitCanonicalTypeMachineNumeric(ctx *CanonicalTypeMachineNumericContext) interface{}

	// Visit a parse tree produced by CanonicalTypeSignatureParser#canonicalTypeSequence.
	VisitCanonicalTypeSequence(ctx *CanonicalTypeSequenceContext) interface{}

	// Visit a parse tree produced by CanonicalTypeSignatureParser#canonicalTypeGroup.
	VisitCanonicalTypeGroup(ctx *CanonicalTypeGroupContext) interface{}

	// Visit a parse tree produced by CanonicalTypeSignatureParser#canonicalTypeOrGroup.
	VisitCanonicalTypeOrGroup(ctx *CanonicalTypeOrGroupContext) interface{}

	// Visit a parse tree produced by CanonicalTypeSignatureParser#canonicalTypeOrGroupSequence.
	VisitCanonicalTypeOrGroupSequence(ctx *CanonicalTypeOrGroupSequenceContext) interface{}

	// Visit a parse tree produced by CanonicalTypeSignatureParser#canonicalTypeSignature.
	VisitCanonicalTypeSignature(ctx *CanonicalTypeSignatureContext) interface{}

	// Visit a parse tree produced by CanonicalTypeSignatureParser#singleCanonicalType.
	VisitSingleCanonicalType(ctx *SingleCanonicalTypeContext) interface{}

	// Visit a parse tree produced by CanonicalTypeSignatureParser#singleCanonicalTypeOrGroup.
	VisitSingleCanonicalTypeOrGroup(ctx *SingleCanonicalTypeOrGroupContext) interface{}

	// Visit a parse tree produced by CanonicalTypeSignatureParser#singleCanonicalGroup.
	VisitSingleCanonicalGroup(ctx *SingleCanonicalGroupContext) interface{}
}


```

--- FILE: canonicaltypes/sample/ct_sample.go ---
```go
package sample

import (
	"math/rand/v2"
	_ "math/rand/v2"

	_ "github.com/rs/zerolog/log"
	canonicaltypes2 "github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	_ "github.com/stergiotis/boxer/public/statespace/mixedradix"
)

var SampleScalarModifier = []canonicaltypes2.ScalarModifierE{canonicaltypes2.ScalarModifierNone, canonicaltypes2.ScalarModifierHomogenousArray, canonicaltypes2.ScalarModifierSet}

var SampleMachineNumericTypeBaseType = []canonicaltypes2.BaseTypeMachineNumericE{canonicaltypes2.BaseTypeMachineNumericUnsigned, canonicaltypes2.BaseTypeMachineNumericSigned, canonicaltypes2.BaseTypeMachineNumericFloat}
var SampleMachineNumericTypeWidth = []canonicaltypes2.Width{8, 16, 32, 64}
var SampleMachineNumericTypeByteOrder = []canonicaltypes2.ByteOrderModifierE{canonicaltypes2.ByteOrderModifierNone, canonicaltypes2.ByteOrderModifierLittleEndian, canonicaltypes2.ByteOrderModifierBigEndian}

var SampleMachineNumericMaxExcl = sliceProd(sampleMachineNumericTypeRadixii)

var SampleStringTypeBaseType = []canonicaltypes2.BaseTypeStringE{canonicaltypes2.BaseTypeStringBool, canonicaltypes2.BaseTypeStringBytes, canonicaltypes2.BaseTypeStringUtf8}
var SampleStringTypeWidthModifier = []canonicaltypes2.WidthModifierE{canonicaltypes2.WidthModifierNone, canonicaltypes2.WidthModifierFixed}
var SampleStringTypeWidth = []canonicaltypes2.Width{0, 128, 145, 192}

var SampleStringTypeMaxExcl = sliceProd(sampleStringTypeRadixii)

var SampleTemporalTypeBaseType = []canonicaltypes2.BaseTypeTemporalE{canonicaltypes2.BaseTypeTemporalUtcDatetime, canonicaltypes2.BaseTypeTemporalZonedDatetime, canonicaltypes2.BaseTypeTemporalZonedTime}
var SampleTemporalTypeWidth = []canonicaltypes2.Width{32, 64}

var SampleTemporalTypeMaxExcl = sliceProd(sampleTemporalTypeRadixii)

var SampleTypeMaxExcl = sliceSum(sampleTypeU)

func GenerateSampleType(n uint64) (sample canonicaltypes2.PrimitiveAstNodeI) { panic("stub") }

func GenerateSamplePrimitiveType(rnd *rand.Rand, accept func(ct canonicaltypes2.PrimitiveAstNodeI) (ok bool, msg string)) (sample canonicaltypes2.PrimitiveAstNodeI) {
	panic("stub")
}

func GenerateSampleGroup(nMembers int, rnd *rand.Rand, accept func(ct canonicaltypes2.PrimitiveAstNodeI) (ok bool, msg string)) (sample canonicaltypes2.GroupAstNode) {
	panic("stub")
}

func GenerateSampleMachineNumericType(n uint64) (sample canonicaltypes2.MachineNumericTypeAstNode) {
	panic("stub")
}

func GenerateSampleStringType(n uint64) (sample canonicaltypes2.StringAstNode) { panic("stub") }

func GenerateSampleTemporalType(n uint64) (sample canonicaltypes2.TemporalTypeAstNode) { panic("stub") }


```

--- FILE: cli/lw_cli.go ---
```go
package cli

import (
	_ "fmt"
	"math/rand/v2"
	_ "math/rand/v2"
	_ "slices"

	_ "github.com/rs/zerolog/log"
	_ "github.com/stergiotis/boxer/public/config"
	_ "github.com/stergiotis/boxer/public/hmi/cli"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
	"github.com/urfave/cli"
	_ "github.com/urfave/cli/v2"
)

func BuildRndFlag() (flags []cli.Flag, f func(context *cli.Context) *rand.Rand) { panic("stub") }

func NewCliCommand() *cli.Command { panic("stub") }


```

--- FILE: cli/lw_cmd_ct.go ---
```go
package cli

import (
	_ "os"

	_ "github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/codegen"
	"github.com/urfave/cli"
	_ "github.com/urfave/cli/v2"
)

func NewCliCommandCanonicalTypes() *cli.Command { panic("stub") }


```

--- FILE: cli/lw_cmd_ddl.go ---
```go
package cli

import (
	_ "fmt"
	_ "math/rand/v2"
	_ "os"
	_ "slices"
	_ "strings"

	_ "github.com/rs/zerolog/log"
	_ "github.com/stergiotis/boxer/public/config"
	_ "github.com/stergiotis/boxer/public/hmi/cli"
	_ "github.com/stergiotis/boxer/public/observability/eh"
	_ "github.com/stergiotis/boxer/public/observability/eh/eb"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/common"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/ddl/arrow"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/ddl/golang"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/urfave/cli"
	_ "github.com/urfave/cli/v2"
)

func NewCliCommandDdl() *cli.Command { panic("stub") }


```

--- FILE: cli/lw_cmd_dml.go ---
```go
package cli

import (
	_ "os"

	_ "github.com/rs/zerolog/log"
	_ "github.com/stergiotis/boxer/public/observability/eh"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/common"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/dml"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/gocodegen"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/urfave/cli"
	_ "github.com/urfave/cli/v2"
)

func NewCliCommandDml() *cli.Command { panic("stub") }


```

--- FILE: common/lw_codegen_utils.go ---
```go
package common

import _ "slices"

func IterateColumnPropsMultiIntermediatePairHolders(irhs ...*IntermediatePairHolder) IntermediateColumnIterator {
	panic("stub")
}

func NewIntermediatePairHolder(nEst int) *IntermediatePairHolder { panic("stub") }

func (inst *IntermediatePairHolder) Concat(other *IntermediatePairHolder) { panic("stub") }

func (inst *IntermediatePairHolder) Load(iter IntermediateColumnIterator) { panic("stub") }

func (inst *IntermediatePairHolder) Add(cc IntermediateColumnContext, cp *IntermediateColumnProps) {
	panic("stub")
}

func (inst *IntermediatePairHolder) Length() int { panic("stub") }

func (inst *IntermediatePairHolder) CountColumns() (nColumns int) { panic("stub") }

func (inst *IntermediatePairHolder) IterateColumnProps() IntermediateColumnIterator { panic("stub") }

func (inst *IntermediatePairHolder) DeriveSubHolder(filter func(cc IntermediateColumnContext) (keep bool)) (r *IntermediatePairHolder) {
	panic("stub")
}

func (inst *IntermediatePairHolder) Reset() { panic("stub") }


```

--- FILE: common/lw_column.go ---
```go
package common

import (
	_ "strings"

	_ "github.com/stergiotis/boxer/public/observability/eh"
	canonicaltypes2 "github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/encodingaspects"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/naming"
)

func (inst TaggedValuesSection) IsValid() bool { panic("stub") }

func (inst PhysicalColumnDesc) GetCanonicalType() (ct canonicaltypes2.PrimitiveAstNodeI, err error) {
	panic("stub")
}

func (inst PhysicalColumnDesc) GetEncodingHints() (hints encodingaspects.AspectSet, err error) {
	panic("stub")
}

func (inst PhysicalColumnDesc) GetTableRowConfig() (tableRowConfig TableRowConfigE, err error) {
	panic("stub")
}

func (inst PhysicalColumnDesc) GetPlainItemType() (plainItemType PlainItemTypeE, err error) {
	panic("stub")
}

func (inst PhysicalColumnDesc) GetSectionName() (name naming.StylableName, err error) { panic("stub") }

func (inst PhysicalColumnDesc) GetLeewayColumnName() (name naming.StylableName, err error) {
	panic("stub")
}

func (inst PhysicalColumnDesc) String() string { panic("stub") }

func (inst PhysicalColumnDesc) IsValid() bool { panic("stub") }


```

--- FILE: common/lw_compiletime_opts.go ---
```go
package common

const UseArrowDictionaryEncoding = false


```

--- FILE: common/lw_dto.go ---
```go
package common

import (
	"github.com/stergiotis/boxer/public/observability/eh"
	_ "github.com/stergiotis/boxer/public/observability/eh"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/encodingaspects"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/naming"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/valueaspects"
)

func (inst *TableDescDto) GetPlainItemCount(itemType PlainItemTypeE) (n int) { panic("stub") }

func (inst *TableDescDto) AddPlainItemNames(itemType PlainItemTypeE, names []naming.StylableName) {
	panic("stub")
}

func (inst *TableDescDto) AddPlainItemTypes(itemType PlainItemTypeE, types []string) { panic("stub") }

func (inst *TableDescDto) AddPlainItemEncodingHints(itemType PlainItemTypeE, hints []encodingaspects.AspectSet) {
	panic("stub")
}

func (inst *TableDescDto) AddPlainItemValueSemantics(itemType PlainItemTypeE, valueSemantics []valueaspects.AspectSet) {
	panic("stub")
}

func (inst *TableDescDto) GetPlainItemNames(itemType PlainItemTypeE) []naming.StylableName {
	panic("stub")
}

func (inst *TableDescDto) GetPlainItemTypes(itemType PlainItemTypeE) []string { panic("stub") }

func (inst *TableDescDto) GetPlainItemEncodingHints(itemType PlainItemTypeE) []encodingaspects.AspectSet {
	panic("stub")
}

func (inst *TableDescDto) GetPlainItemValueSemantics(itemType PlainItemTypeE) []valueaspects.AspectSet {
	panic("stub")
}

func NewTableDescDto() *TableDescDto { panic("stub") }

func (inst *TableDescDto) Reset() { panic("stub") }

var ErrInvalidType = eh.Errorf("table contains an invalid canonical type")

type TechnologyDto struct {
	Id   string
	Name string
}


```

--- FILE: common/lw_enums.go ---
```go
package common

import (
	_ "fmt"
	"iter"
	_ "iter"
	"math"
	_ "math"
	_ "math/bits"
	_ "slices"
	_ "strings"

	_ "github.com/rs/zerolog/log"
	_ "github.com/stergiotis/boxer/public/observability/eh/eb"
)

const InvalidEnumValueString = "<invalid>"

const (
	ColumnRoleUnspecific                      ColumnRoleE = ""
	ColumnRoleHighCardRef                     ColumnRoleE = "hr"
	ColumnRoleHighCardRefParametrized         ColumnRoleE = "hp"
	ColumnRoleHighCardVerbatim                ColumnRoleE = "hv"
	ColumnRoleLowCardRef                      ColumnRoleE = "lr"
	ColumnRoleLowCardRefParametrized          ColumnRoleE = "lp"
	ColumnRoleLowCardVerbatim                 ColumnRoleE = "lv"
	ColumnRoleMixedLowCardRef                 ColumnRoleE = "lmr"
	ColumnRoleMixedVerbatimHighCardParameters ColumnRoleE = "mvhp"
	ColumnRoleMixedRefHighCardParameters      ColumnRoleE = "mrhp"
	ColumnRoleMixedLowCardVerbatim            ColumnRoleE = "lmv"
	ColumnRoleValue                           ColumnRoleE = "val"
	ColumnRoleLength                          ColumnRoleE = "len"

	ColumnRoleHighCardRefCardinality             ColumnRoleE = ColumnRoleHighCardRef + ColumnRoleE("card")
	ColumnRoleHighCardRefParametrizedCardinality ColumnRoleE = ColumnRoleHighCardRefParametrized + ColumnRoleE("card")
	ColumnRoleHighCardVerbatimCardinality        ColumnRoleE = ColumnRoleHighCardVerbatim + ColumnRoleE("card")
	ColumnRoleLowCardRefCardinality              ColumnRoleE = ColumnRoleLowCardRef + ColumnRoleE("card")
	ColumnRoleLowCardRefParametrizedCardinality  ColumnRoleE = ColumnRoleLowCardRefParametrized + ColumnRoleE("card")
	ColumnRoleLowCardVerbatimCardinality         ColumnRoleE = ColumnRoleLowCardVerbatim + ColumnRoleE("card")
	ColumnRoleMixedLowCardRefCardinality         ColumnRoleE = ColumnRoleMixedLowCardRef + ColumnRoleE("card")
	ColumnRoleMixedLowCardVerbatimCardinality    ColumnRoleE = ColumnRoleMixedLowCardVerbatim + ColumnRoleE("card")

	ColumnRoleCardinality ColumnRoleE = "card"

	ColumnRoleCusumLength      ColumnRoleE = "cusumlen"
	ColumnRoleCusumCardinality ColumnRoleE = "cusumcard"
)

var AllColumnRoles = []ColumnRoleE{
	ColumnRoleUnspecific,
	ColumnRoleHighCardRef,
	ColumnRoleHighCardRefParametrized,
	ColumnRoleHighCardVerbatim,
	ColumnRoleLowCardRef,
	ColumnRoleLowCardRefParametrized,
	ColumnRoleLowCardVerbatim,
	ColumnRoleMixedLowCardRef,
	ColumnRoleMixedRefHighCardParameters,
	ColumnRoleMixedLowCardVerbatim,
	ColumnRoleValue,
	ColumnRoleLength,

	ColumnRoleHighCardRefCardinality,
	ColumnRoleHighCardRefParametrizedCardinality,
	ColumnRoleHighCardVerbatimCardinality,
	ColumnRoleLowCardRefCardinality,
	// NOTE: parametrization is high cardinality, ref is low-cardinality
	ColumnRoleLowCardRefParametrizedCardinality,
	ColumnRoleLowCardVerbatimCardinality,
	ColumnRoleMixedLowCardRefCardinality,
	ColumnRoleMixedLowCardVerbatimCardinality,

	ColumnRoleCardinality,
	ColumnRoleCusumLength,
	ColumnRoleCusumCardinality,
}

func ParseColumnRole(s string) (role ColumnRoleE, err error) { panic("stub") }

func (inst ColumnRoleE) String() string { panic("stub") }

func (inst ColumnRoleE) LongString() string { panic("stub") }

const (
	MembershipSpecNone                                   MembershipSpecE = 0b0000_0000
	MembershipSpecHighCardRef                            MembershipSpecE = 0b0000_0001
	MembershipSpecHighCardVerbatim                       MembershipSpecE = 0b0000_0010
	MembershipSpecHighCardRefParametrized                MembershipSpecE = 0b0000_0100
	MembershipSpecLowCardRef                             MembershipSpecE = 0b0001_0000
	MembershipSpecLowCardVerbatim                        MembershipSpecE = 0b0010_0000
	MembershipSpecLowCardRefParametrized                 MembershipSpecE = 0b0100_0000
	MembershipSpecMixedLowCardRefHighCardParameters      MembershipSpecE = 0b0000_1000
	MembershipSpecMixedLowCardVerbatimHighCardParameters MembershipSpecE = 0b1000_0000
)

var AllMembershipSpecs = []MembershipSpecE{
	MembershipSpecNone,
	MembershipSpecHighCardRef,
	MembershipSpecHighCardVerbatim,
	MembershipSpecHighCardRefParametrized,
	MembershipSpecLowCardRef,
	MembershipSpecLowCardVerbatim,
	MembershipSpecLowCardRefParametrized,
	MembershipSpecMixedLowCardRefHighCardParameters,
	MembershipSpecMixedLowCardVerbatimHighCardParameters,
}

// GetIndex returns -1 if .Count() > 1
func (inst MembershipSpecE) GetIndex() int { panic("stub") }

func (inst MembershipSpecE) ContainsMixed() (mixed bool) { panic("stub") }

func (inst MembershipSpecE) String() string { panic("stub") }

func (inst MembershipSpecE) HasHighCardRefOnly() bool { panic("stub") }

func (inst MembershipSpecE) HasLowCardRefOnly() bool { panic("stub") }

func (inst MembershipSpecE) HasHighCardVerbatim() bool { panic("stub") }

func (inst MembershipSpecE) HasLowCardVerbatim() bool { panic("stub") }

func (inst MembershipSpecE) HasHighCardRefParametrized() bool { panic("stub") }

func (inst MembershipSpecE) HasLowCardRefParametrized() bool { panic("stub") }

func (inst MembershipSpecE) HasMixedLowCardRefHighCardParameters() bool { panic("stub") }

func (inst MembershipSpecE) HasMixedLowCardVerbatimHighCardParameters() bool { panic("stub") }

func (inst MembershipSpecE) AddHighCardRefOnly() MembershipSpecE { panic("stub") }

func (inst MembershipSpecE) AddHighCardRefParametrized() MembershipSpecE { panic("stub") }

func (inst MembershipSpecE) AddHighCardVerbatim() MembershipSpecE { panic("stub") }

func (inst MembershipSpecE) AddLowCardRefOnly() MembershipSpecE { panic("stub") }

func (inst MembershipSpecE) AddLowCardRefParametrized() MembershipSpecE { panic("stub") }

func (inst MembershipSpecE) AddLowCardVerbatim() MembershipSpecE { panic("stub") }

func (inst MembershipSpecE) AddMixedLowCardRefHighCardParameters() MembershipSpecE { panic("stub") }

func (inst MembershipSpecE) AddMixedLowCardVerbatimHighCardParameters() MembershipSpecE {
	panic("stub")
}

func (inst MembershipSpecE) ClearHighCardRefOnly() MembershipSpecE { panic("stub") }

func (inst MembershipSpecE) ClearHighCardRefParametrized() MembershipSpecE { panic("stub") }

func (inst MembershipSpecE) ClearHighCardVerbatim() MembershipSpecE { panic("stub") }

func (inst MembershipSpecE) ClearLowCardRefOnly() MembershipSpecE { panic("stub") }

func (inst MembershipSpecE) ClearLowCardRefParametrized() MembershipSpecE { panic("stub") }

func (inst MembershipSpecE) ClearLowCardVerbatim() MembershipSpecE { panic("stub") }

func (inst MembershipSpecE) ClearMixedLowCardRefHighCardParameters() MembershipSpecE { panic("stub") }

func (inst MembershipSpecE) ClearMixedLowCardVerbatimHighCardParameters() MembershipSpecE {
	panic("stub")
}

func (inst MembershipSpecE) Count() int { panic("stub") }

func (inst MembershipSpecE) Iterate() iter.Seq[MembershipSpecE] { panic("stub") }

const (
	ImplementationStatusNotImplemented ImplementationStatusE = 0
	ImplementationStatusPartial        ImplementationStatusE = math.MaxUint8 >> 1
	ImplementationStatusFull           ImplementationStatusE = math.MaxUint8
)

var AllImplementationStatus = []ImplementationStatusE{
	ImplementationStatusNotImplemented,
	ImplementationStatusPartial,
	ImplementationStatusFull,
}

func (inst ImplementationStatusE) String() string { panic("stub") }

const (
	TableRowConfigMultiAttributesPerRow TableRowConfigE = 0
)

var AllTableRowConfigs = []TableRowConfigE{
	TableRowConfigMultiAttributesPerRow,
}

func (inst TableRowConfigE) IsValid() bool { panic("stub") }

func (inst TableRowConfigE) String() string { panic("stub") }

const (
	PlainItemTypeNone            PlainItemTypeE = 0
	PlainItemTypeEntityId        PlainItemTypeE = 1
	PlainItemTypeEntityTimestamp PlainItemTypeE = 2
	PlainItemTypeEntityRouting   PlainItemTypeE = 3
	PlainItemTypeEntityLifecycle PlainItemTypeE = 4
	PlainItemTypeTransaction     PlainItemTypeE = 5
	PlainItemTypeOpaque          PlainItemTypeE = 6
)

var AllPlainItemTypes = []PlainItemTypeE{
	PlainItemTypeNone,
	PlainItemTypeEntityId,
	PlainItemTypeEntityTimestamp,
	PlainItemTypeEntityRouting,
	PlainItemTypeEntityLifecycle,
	PlainItemTypeTransaction,
	PlainItemTypeOpaque,
}

var MaxPlainItemTypeExcl = PlainItemTypeE(len(AllMembershipSpecs))

func (inst PlainItemTypeE) String() string { panic("stub") }

var AllIntermediateColumnScopes = []IntermediateColumnScopeE{
	IntermediateColumnScopeEntity,
	IntermediateColumnScopeTransaction,
	IntermediateColumnScopeOpaque,
	IntermediateColumnScopeTagged,
}

const (
	IntermediateColumnScopeEntity      IntermediateColumnScopeE = "entity"
	IntermediateColumnScopeTransaction IntermediateColumnScopeE = "transaction"
	IntermediateColumnScopeOpaque      IntermediateColumnScopeE = "opaque"
	IntermediateColumnScopeTagged      IntermediateColumnScopeE = "tagged"
)

func (inst IntermediateColumnScopeE) String() string { panic("stub") }

func (inst IntermediateColumnScopeE) IsValid() bool { panic("stub") }

var AllIntermediateColumnsSubTypes = []IntermediateColumnSubTypeE{
	IntermediateColumnsSubTypeScalar,
	IntermediateColumnsSubTypeHomogenousArray,
	IntermediateColumnsSubTypeHomogenousArraySupport,
	IntermediateColumnsSubTypeSet,
	IntermediateColumnsSubTypeSetSupport,
	IntermediateColumnsSubTypeMembership,
	IntermediateColumnsSubTypeMembershipSupport,
}

const (
	IntermediateColumnsSubTypeScalar                 IntermediateColumnSubTypeE = "scalar"
	IntermediateColumnsSubTypeHomogenousArray        IntermediateColumnSubTypeE = "homogenous-array"
	IntermediateColumnsSubTypeHomogenousArraySupport IntermediateColumnSubTypeE = "homogenous-array-support"
	IntermediateColumnsSubTypeSet                    IntermediateColumnSubTypeE = "set"
	IntermediateColumnsSubTypeSetSupport             IntermediateColumnSubTypeE = "set-support"
	IntermediateColumnsSubTypeMembership             IntermediateColumnSubTypeE = "membership"
	IntermediateColumnsSubTypeMembershipSupport      IntermediateColumnSubTypeE = "membership-support"
)

func (inst IntermediateColumnSubTypeE) String() string { panic("stub") }

func (inst IntermediateColumnSubTypeE) IsValid() bool { panic("stub") }

func (inst PlainItemTypeE) GetIntermediateColumnScope() IntermediateColumnScopeE { panic("stub") }


```

--- FILE: common/lw_impl.go ---
```go
package common

import (
	_ "github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh"
	_ "github.com/stergiotis/boxer/public/observability/eh"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/canonicaltypes"
)

var ErrUnhandledRole = eh.Errorf("unhandled role")

func (inst ColumnRoleE) IsCardinalityRole() bool { panic("stub") }

func GetMembershipRoleByCardinalityRole(membershipCardinalityRole ColumnRoleE) (membershipRole ColumnRoleE, err error) {
	panic("stub")
}

func GetCardinalityRoleByMembershipRole(membershipRole ColumnRoleE) (cardinalityRole ColumnRoleE, err error) {
	panic("stub")
}

func GetSubTypeByScalarModifier(scalarModifier canonicaltypes.ScalarModifierE) (subType IntermediateColumnSubTypeE) {
	panic("stub")
}


```

--- FILE: common/lw_intermediate.go ---
```go
package common

import (
	"iter"
	_ "iter"
	_ "slices"

	"github.com/stergiotis/boxer/public/observability/eh"
	_ "github.com/stergiotis/boxer/public/observability/eh"
	_ "github.com/stergiotis/boxer/public/observability/eh/eb"
	canonicaltypes2 "github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/encodingaspects"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/naming"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/useaspects"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/valueaspects"
)

func ExtractScalarModifier(ct canonicaltypes2.PrimitiveAstNodeI) (scalarModifier canonicaltypes2.ScalarModifierE, err error) {
	panic("stub")
}

func NewIntermediateColumnsProps() *IntermediateColumnProps { panic("stub") }

func (inst *IntermediateColumnProps) Reset() { panic("stub") }

func (inst *IntermediateColumnProps) Reserve(n int) { panic("stub") }

func (inst *IntermediateColumnProps) Slice(beginIncl int, endExcl int) (sliced IntermediateColumnProps) {
	panic("stub")
}

func (inst *IntermediateColumnProps) Add(name naming.StylableName, role ColumnRoleE, ct canonicaltypes2.PrimitiveAstNodeI, hints encodingaspects.AspectSet, valueSemantics valueaspects.AspectSet) {
	panic("stub")
}

func (inst *IntermediateColumnProps) Length() int { panic("stub") }

func (inst *IntermediateColumnProps) IterateColumnIndex() iter.Seq[int] { panic("stub") }

func (inst *IntermediateColumnProps) IsEmpty() (empty bool) { panic("stub") }

func NewIntermediateTaggedValueDesc() *IntermediateTaggedValuesDesc { panic("stub") }

func (inst *IntermediateTaggedValuesDesc) Reset() { panic("stub") }

func (inst *IntermediateTaggedValuesDesc) LoadSection(sec *TaggedValuesSection, tech TechnologySpecificMembershipSetGenI) (err error) {
	panic("stub")
}

var ErrUnhandledMembershipSpec = eh.Errorf("unhandled membership specification")

func NewIntermediatePlainValueDesc() *IntermediatePlainValuesDesc { panic("stub") }

func (inst *IntermediatePlainValuesDesc) Reset() { panic("stub") }

func (inst *IntermediatePlainValuesDesc) Load(names []naming.StylableName, ctss []canonicaltypes2.AstNodeI, hintss []encodingaspects.AspectSet, ss []valueaspects.AspectSet, streamingGroup naming.Key) (err error) {
	panic("stub")
}

func (inst *IntermediatePlainValuesDesc) LoadSingle(name naming.StylableName, ct canonicaltypes2.PrimitiveAstNodeI, hints encodingaspects.AspectSet, vs valueaspects.AspectSet, streamingGroup naming.Key) (err error) {
	panic("stub")
}

func (inst *IntermediatePlainValuesDesc) Length() int { panic("stub") }

func NewIntermediateTableRepresentation() *IntermediateTableRepresentation { panic("stub") }

func (inst *IntermediateTableRepresentation) Reset() { panic("stub") }

func (inst *IntermediateTableRepresentation) LoadFromTable(table *TableDesc, tech TechnologySpecificMembershipSetGenI) (err error) {
	panic("stub")
}

func (inst *IntermediateTableRepresentation) IterateColumnProps() IntermediateColumnIterator {
	panic("stub")
}

func (inst *IntermediateTableRepresentation) Length() (nPlain int, nTagged int) { panic("stub") }

func (inst *IntermediateTableRepresentation) TotalLength() (nPlainPlusTagged int) { panic("stub") }

func (inst *IntermediateTaggedValuesDesc) Length() int { panic("stub") }

func (inst *IntermediateTaggedValuesDesc) IterateColumnProps(sectionName naming.StylableName, asp useaspects.AspectSet, indexOffset uint32) IntermediateColumnIterator {
	panic("stub")
}

func (inst *IntermediatePlainValuesDesc) IterateColumnProps(itemType PlainItemTypeE, indexOffset uint32) IntermediateColumnIterator {
	panic("stub")
}

func (inst IntermediateColumnContext) IsPlainColumn() bool { panic("stub") }

func (inst IntermediateColumnContext) IsTaggedColumn() bool { panic("stub") }


```

--- FILE: common/lw_intermediate_schema_table.go ---
```go
package common

import (
	"io"
	_ "io"

	"github.com/apache/arrow-go/v18/arrow"
	_ "github.com/apache/arrow-go/v18/arrow"
	_ "github.com/apache/arrow-go/v18/arrow/array"
	_ "github.com/apache/arrow-go/v18/arrow/ipc"
	_ "github.com/apache/arrow-go/v18/arrow/memory"
	_ "github.com/stergiotis/boxer/public/observability/eh"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/naming"
)

// FIXME make this an leeway table
var SchemaTableArrowSchema = arrow.NewSchema([]arrow.Field{
	{Name: "Id", Type: arrow.BinaryTypes.String, Nullable: false},
	{Name: "Scope", Type: &arrow.DictionaryType{ValueType: arrow.BinaryTypes.String, IndexType: arrow.PrimitiveTypes.Uint8}, Nullable: false},
	{Name: "ItemType", Type: &arrow.DictionaryType{ValueType: arrow.BinaryTypes.String, IndexType: arrow.PrimitiveTypes.Uint8}, Nullable: false},
	{Name: "SectionName", Type: &arrow.DictionaryType{ValueType: arrow.BinaryTypes.String, IndexType: arrow.PrimitiveTypes.Uint32}, Nullable: false},
	{Name: "LogicalColumnName", Type: arrow.BinaryTypes.String, Nullable: false},
	{Name: "ColumnRole", Type: &arrow.DictionaryType{ValueType: arrow.BinaryTypes.String, IndexType: arrow.PrimitiveTypes.Uint8}, Nullable: false},
	{Name: "SubType", Type: &arrow.DictionaryType{ValueType: arrow.BinaryTypes.String, IndexType: arrow.PrimitiveTypes.Uint8}, Nullable: false},
	{Name: "UseAspects", Type: arrow.ListOfNonNullable(arrow.BinaryTypes.String), Nullable: false},
	{Name: "CanonicalType", Type: arrow.BinaryTypes.String, Nullable: false},
	{Name: "EncodingHints", Type: arrow.ListOfNonNullable(arrow.BinaryTypes.String), Nullable: false},
}, nil)

func (inst *IntermediateTableRepresentation) LoadInArrowBuilder(id naming.StylableName, builder *array.RecordBuilder) (err error) {
	panic("stub")
}

func (inst *IntermediateTableRepresentation) ToSchemaTable(id naming.StylableName, out io.Writer) (err error) {
	panic("stub")
}


```

--- FILE: common/lw_key.go ---
```go
package common


```

--- FILE: common/lw_sample.go ---
```go
package common

import (
	_ "fmt"
	"math/rand/v2"
	_ "math/rand/v2"

	_ "github.com/rs/zerolog/log"
	_ "github.com/stergiotis/boxer/public/observability/eh"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/sample"
	encodingaspects2 "github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/canonicaltypes"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/valueaspects"
)

func GenerateSampleTableDesc(rnd *rand.Rand, acceptCanonicalType func(ct canonicaltypes.PrimitiveAstNodeI) (ok bool, msg string), acceptEncodingAspect func(asp encodingaspects2.AspectE) (ok bool, msg string)) (tbl TableDesc, err error) {
	panic("stub")
}

func GenerateSampleTableDescDto(rnd *rand.Rand, acceptCanonicalType func(ct canonicaltypes.PrimitiveAstNodeI) (ok bool, msg string), acceptEncodingAspect func(asp encodingaspects2.AspectE) (ok bool, msg string)) (dto TableDescDto, err error) {
	panic("stub")
}

func GenerateSampleEncodingAspectEx(nMembers int, r *rand.Rand, accept func(aspect encodingaspects2.AspectE) (ok bool, msg string)) (sample encodingaspects2.AspectSet) {
	panic("stub")
}

func GenerateSampleValueSemantics(nMembers int, rnd *rand.Rand) (valueSemantics valueaspects.AspectSet) {
	panic("stub")
}

func PopulateManipulator(manipulator *TableManipulator, rnd *rand.Rand, acceptCanonicalType func(ct canonicaltypes.PrimitiveAstNodeI) (ok bool, msg string), acceptEncodingAspect func(aspect encodingaspects2.AspectE) (ok bool, msg string)) (err error) {
	panic("stub")
}


```

--- FILE: common/lw_table.go ---
```go
package common

import (
	_ "slices"

	_ "github.com/stergiotis/boxer/public/containers/co"
	_ "github.com/stergiotis/boxer/public/observability/eh"
	_ "github.com/stergiotis/boxer/public/observability/eh/eb"
	canonicaltypes3 "github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/encodingaspects"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/naming"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/valueaspects"
)

func NewTableDesc() *TableDesc { panic("stub") }

func (inst *TableDesc) CountStructuredItemsByType(itemType PlainItemTypeE) (n int) { panic("stub") }

func (inst *TableDesc) Reset() { panic("stub") }

func (inst *TableDesc) AddPlainColumns(itemType PlainItemTypeE, names []naming.StylableName, canonicalTypes []string, encodingHints []encodingaspects.AspectSet, valueSemantics []valueaspects.AspectSet) (err error) {
	panic("stub")
}

func (inst *TableDesc) AddTaggedValuesSections(secs []TaggedValuesSectionDto) (err error) {
	panic("stub")
}

func (inst *TableDesc) LoadFrom(dto *TableDescDto) (err error) { panic("stub") }

func (inst *TableDesc) GetPlainItemNames(itemType PlainItemTypeE, in []naming.StylableName) (out []naming.StylableName) {
	panic("stub")
}

func (inst *TableDesc) GetPlainItemTypesStr(itemType PlainItemTypeE, in []string) (out []string, err error) {
	panic("stub")
}

func (inst *TableDesc) GetPlainItemTypes(itemType PlainItemTypeE, in []canonicaltypes3.AstNodeI) (out []canonicaltypes3.AstNodeI) {
	panic("stub")
}

func (inst *TableDesc) GetPlainItemEncodingHints(itemType PlainItemTypeE, in []encodingaspects.AspectSet) (out []encodingaspects.AspectSet, err error) {
	panic("stub")
}

func (inst *TableDesc) GetPlainItemValueSemantics(itemType PlainItemTypeE, in []valueaspects.AspectSet) (out []valueaspects.AspectSet, err error) {
	panic("stub")
}

func (inst *TableDesc) LoadTo(dto *TableDescDto) (err error) { panic("stub") }

// FIXME will render dto object invalid (destroy co-array structure)


```

--- FILE: common/lw_table_manipulator.go ---
```go
package common

import (
	_ "bytes"
	"iter"
	_ "iter"
	_ "slices"

	_ "github.com/rs/zerolog/log"
	_ "github.com/stergiotis/boxer/public/observability/eh"
	_ "github.com/stergiotis/boxer/public/observability/eh/eb"
	canonicaltypes2 "github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	encodingaspects2 "github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	useaspects2 "github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/naming"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/valueaspects"
	_ "golang.org/x/exp/maps"
)

func NewTableManipulator() (inst *TableManipulator, err error) { panic("stub") }

func (inst *TableManipulator) Reset() { panic("stub") }

func (inst *TableManipulator) BuildTableDesc() (tbl TableDesc, err error) { panic("stub") }

func (inst *TableManipulator) BuildTableDescDto() (dto TableDescDto, err error) { panic("stub") }

// reset to normalize representation ([]string{} vs []string(nil))

func (inst *TableManipulator) SetTableName(name naming.StylableName) *TableManipulator { panic("stub") }

func (inst *TableManipulator) SetTableComment(comment string) *TableManipulator { panic("stub") }

func (inst *TableManipulator) TaggedValueSection(sectionName naming.StylableName) TaggedValueSectionMerger {
	panic("stub")
}

func (inst TaggedValueSectionMerger) TaggedValueColumn(name naming.StylableName, canonicalType canonicaltypes2.PrimitiveAstNodeI) TaggedValueColumnMerger {
	panic("stub")
}

func (inst *TableManipulator) PlainValueColumn(itemType PlainItemTypeE, name naming.StylableName, canonicalType canonicaltypes2.PrimitiveAstNodeI) PlainValueColumnMerger {
	panic("stub")
}

func (inst *TableManipulator) AddPlainValueItem(itemType PlainItemTypeE, name naming.StylableName, ct canonicaltypes2.PrimitiveAstNodeI, hints encodingaspects2.AspectSet, valueSemantics valueaspects.AspectSet) *TableManipulator {
	panic("stub")
}

func (inst *TableManipulator) MergeTaggedValueSection(sectionName naming.StylableName, aspectSet useaspects2.AspectSet, membership MembershipSpecE, coSectionGroup naming.Key, streamingGroup naming.Key) *TableManipulator {
	panic("stub")
}

func (inst *TableManipulator) SetOpaqueColumnStreamingGroup(streamingGroup naming.Key) *TableManipulator {
	panic("stub")
}

func (inst *TableManipulator) MergeTaggedValueColumn(sectionName naming.StylableName, columnName naming.StylableName, ct canonicaltypes2.PrimitiveAstNodeI, hints encodingaspects2.AspectSet, valueSemantics valueaspects.AspectSet, aspectSet useaspects2.AspectSet, membership MembershipSpecE, coSectionGroup naming.Key, streamingGroup naming.Key) *TableManipulator {
	panic("stub")
}

func (inst *TableManipulator) LoadFromIntermediates(it iter.Seq2[IntermediateColumnContext, *IntermediateColumnProps]) (err error) {
	panic("stub")
}

// mixed, trigger on other column

func (inst *TableManipulator) MergeTable(tbl *TableDesc) (err error) { panic("stub") }

func (inst TaggedValueSectionMerger) SectionName(sectionName naming.StylableName) TaggedValueSectionMerger {
	panic("stub")
}

func (inst TaggedValueSectionMerger) AddSectionUseAspectSet(aspects useaspects2.AspectSet) TaggedValueSectionMerger {
	panic("stub")
}

func (inst TaggedValueSectionMerger) AddSectionUseAspects(aspects ...useaspects2.AspectE) TaggedValueSectionMerger {
	panic("stub")
}

func (inst TaggedValueSectionMerger) ResetSectionUseAspects() TaggedValueSectionMerger { panic("stub") }

func (inst TaggedValueSectionMerger) AddSectionMembership(memberships ...MembershipSpecE) TaggedValueSectionMerger {
	panic("stub")
}

func (inst TaggedValueSectionMerger) ClearSectionMembership(memberships ...MembershipSpecE) TaggedValueSectionMerger {
	panic("stub")
}

func (inst TaggedValueSectionMerger) ResetSectionMembership() TaggedValueSectionMerger { panic("stub") }

func (inst TaggedValueSectionMerger) SectionCoSectionGroup(coSectionGroup naming.Key) TaggedValueSectionMerger {
	panic("stub")
}

func (inst TaggedValueSectionMerger) SectionStreamingGroup(streamingGroup naming.Key) TaggedValueSectionMerger {
	panic("stub")
}

func (inst TaggedValueColumnMerger) Section() TaggedValueSectionMerger { panic("stub") }

func (inst TaggedValueColumnMerger) SetColumnName(columnName naming.StylableName) TaggedValueColumnMerger {
	panic("stub")
}

func (inst TaggedValueColumnMerger) SetColumnCanonicalType(ct canonicaltypes2.PrimitiveAstNodeI) TaggedValueColumnMerger {
	panic("stub")
}

func (inst TaggedValueColumnMerger) AddColumnEncodingHintSet(aspects encodingaspects2.AspectSet) TaggedValueColumnMerger {
	panic("stub")
}

func (inst TaggedValueColumnMerger) AddColumnEncodingHints(aspects ...encodingaspects2.AspectE) TaggedValueColumnMerger {
	panic("stub")
}

func (inst TaggedValueColumnMerger) AddColumnValueSemanticSet(semantics valueaspects.AspectSet) TaggedValueColumnMerger {
	panic("stub")
}

func (inst TaggedValueColumnMerger) AddColumnValueSemantics(semantics ...valueaspects.AspectE) TaggedValueColumnMerger {
	panic("stub")
}

func (inst PlainValueColumnMerger) SetColumnName(columnName naming.StylableName) PlainValueColumnMerger {
	panic("stub")
}

func (inst PlainValueColumnMerger) SetColumnCanonicalType(ct canonicaltypes2.PrimitiveAstNodeI) PlainValueColumnMerger {
	panic("stub")
}

func (inst PlainValueColumnMerger) AddColumnEncodingHintSet(aspects encodingaspects2.AspectSet) PlainValueColumnMerger {
	panic("stub")
}

func (inst PlainValueColumnMerger) AddColumnEncodingHints(aspects ...encodingaspects2.AspectE) PlainValueColumnMerger {
	panic("stub")
}

func (inst PlainValueColumnMerger) AddColumnValueSemanticSet(semantics valueaspects.AspectSet) PlainValueColumnMerger {
	panic("stub")
}

func (inst PlainValueColumnMerger) AddColumnValueSemantics(semantics ...valueaspects.AspectE) PlainValueColumnMerger {
	panic("stub")
}


```

--- FILE: common/lw_table_marshaller.go ---
```go
package common

import (
	"io"
	_ "io"

	_ "github.com/fxamacker/cbor/v2"
	_ "github.com/stergiotis/boxer/public/observability/eh"
)

func NewTableMarshaller() (inst *TableMarshaller, err error) { panic("stub") }

func (inst *TableMarshaller) EncodeTableCbor(w io.Writer, table *TableDesc) (err error) {
	panic("stub")
}

func (inst *TableMarshaller) EncodeDtoCbor(w io.Writer, dto *TableDescDto) (err error) { panic("stub") }

func (inst *TableMarshaller) DecodeTableCbor(r io.Reader, table *TableDesc) (err error) {
	panic("stub")
}

func (inst *TableMarshaller) DecodeDtoCbor(r io.Reader, dto *TableDescDto) (err error) { panic("stub") }


```

--- FILE: common/lw_table_normalizer.go ---
```go
package common

import (
	"math/rand/v2"
	_ "math/rand/v2"
	_ "slices"
	_ "strings"

	_ "github.com/stergiotis/boxer/public/containers/co"
	_ "github.com/stergiotis/boxer/public/observability/eh"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/naming"
)

type TableNormalizer struct {
}

func NewTableNormalizer(namingStyle naming.NamingStyleE) *TableNormalizer { panic("stub") }

func (inst *TableNormalizer) Equal(other *TableNormalizer) (same bool) { panic("stub") }

func (inst *TableNormalizer) Scramble(table *TableDesc, rnd *rand.Rand) { panic("stub") }

func (inst *TableNormalizer) Normalize(table *TableDesc) (nameChanges bool, reorderPlain bool, reorderTagged bool, err error) {
	panic("stub")
}

// Note: Names are normalized, therefor comparison is correct


```

--- FILE: common/lw_table_operations.go ---
```go
package common

import (
	_ "bytes"

	_ "github.com/rs/zerolog/log"
	_ "github.com/stergiotis/boxer/public/observability/eh"
	canonicaltypes2 "github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	encodingaspects2 "github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/useaspects"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/valueaspects"
)

type TableOperations struct {
}

func NewTableOperations() (inst *TableOperations, err error) { panic("stub") }

func (inst *TableOperations) MergeTables(tbl1, tbl2 *TableDesc) (out *TableDesc, err error) {
	panic("stub")
}

type CriteriaOperationTypeE uint8

const (
	CriteriaTypeWhitelist CriteriaOperationTypeE = 0
	CriteriaTypeBlacklist CriteriaOperationTypeE = 1
)

type TableSubsetPredicateI interface {
	ShouldKeepTagged(sectionName string, columnName string, ct canonicaltypes2.PrimitiveAstNodeI, hints encodingaspects2.AspectSet, valueSemantics valueaspects.AspectSet, aspectSet useaspects.AspectSet, membership MembershipSpecE) bool
	ShouldKeepPlain(sectionName string, columnName string, ct canonicaltypes2.PrimitiveAstNodeI, hints encodingaspects2.AspectSet, valueSemantics valueaspects.AspectSet, aspectSet useaspects.AspectSet, membership MembershipSpecE) bool
}

type TableSubsetSectionByNamePredicate struct {
	Type         CriteriaOperationTypeE
	SectionNames []string
}
type TableSubsetSectionByUseCriteriaPredicate struct {
	Type        CriteriaOperationTypeE
	UseCriteria useaspects.AspectSet
}
type TableSubsetCriteria struct {
	KeepSectionByName []string
}

func (inst *TableOperations) Subset(tbl *TableDesc, criteria TableSubsetCriteria) (out *TableDesc, err error) {
	panic("stub")
}

func (inst *TableOperations) MustCompare(tbl1, tbl2 *TableDesc) (r int) { panic("stub") }

func (inst *TableOperations) Compare(tbl1, tbl2 *TableDesc) (r int, err error) { panic("stub") }

func (inst *TableOperations) DeepCopy(tbl *TableDesc) (out TableDesc, err error) { panic("stub") }


```

--- FILE: common/lw_table_validator.go ---
```go
package common

import (
	_ "errors"

	_ "github.com/stergiotis/boxer/public/containers"
	_ "github.com/stergiotis/boxer/public/observability/eh"
	_ "github.com/stergiotis/boxer/public/observability/eh/eb"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/naming"
)

func NewTableValidator() *TableValidator { panic("stub") }

func (inst *TableValidator) Reset() { panic("stub") }

func (inst *TableValidator) ValidateSection(section TaggedValuesSection) (err error) { panic("stub") }

func (inst *TableValidator) ValidateTable(table *TableDesc) (err error) { panic("stub") }


```

--- FILE: common/lw_types.go ---
```go
package common

import (
	_ "bytes"
	_ "fmt"
	"iter"
	_ "iter"
	"strings"
	_ "strings"

	_ "github.com/fxamacker/cbor/v2"
	_ "github.com/stergiotis/boxer/public/containers"
	"github.com/stergiotis/boxer/public/observability/eh"
	_ "github.com/stergiotis/boxer/public/observability/eh"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/canonicaltypes"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/encodingaspects"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/naming"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/useaspects"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/valueaspects"
)

type IntermediateColumnScopeE string
type IntermediateColumnSubTypeE string

type IntermediateColumnContext struct {
	Scope         IntermediateColumnScopeE
	SubType       IntermediateColumnSubTypeE
	PlainItemType PlainItemTypeE
	IndexOffset   uint32

	// StreamingGroup empty for all plain sections but opaque
	StreamingGroup naming.Key

	// SectionName empty for plain sections
	SectionName naming.StylableName
	UseAspects  useaspects.AspectSet
	// CoSectionGroup empty for plain sections
	CoSectionGroup naming.Key
}

type IntermediateColumnProps struct {
	Names []naming.StylableName `cbor:"names"`
	Roles []ColumnRoleE         `cbor:"roles"`
	// original canonical type, for membership columns: scalar type
	CanonicalType  []canonicaltypes.PrimitiveAstNodeI `cbor:"canonicalType"`
	EncodingHints  []encodingaspects.AspectSet        `cbor:"encodingHints"`
	ValueSemantics []valueaspects.AspectSet           `cbor:"valueSemantics"`
}
type IntermediateTaggedValuesDesc struct {
	SectionName                     naming.StylableName      `cbor:"sectionName"`
	UseAspects                      useaspects.AspectSet     `cbor:"useAspects"`
	Scalar                          *IntermediateColumnProps `cbor:"scalar"`
	NonScalarHomogenousArray        *IntermediateColumnProps `cbor:"nonScalarHomogenousArray"`
	NonScalarHomogenousArraySupport *IntermediateColumnProps `cbor:"nonScalarHomogenousArraySupport"`
	NonScalarSet                    *IntermediateColumnProps `cbor:"nonScalarSet"`
	NonScalarSetSupport             *IntermediateColumnProps `cbor:"nonScalarSetSupport"`
	Membership                      *IntermediateColumnProps `cbor:"membership"`
	MembershipSupport               *IntermediateColumnProps `cbor:"membershipSupport"`
	CoSectionGroup                  naming.Key               `cbor:"coSectionGroup"`
	StreamingGroup                  naming.Key               `cbor:"streamingGroup"`
}
type IntermediatePlainValuesDesc struct {
	ItemType                        PlainItemTypeE           `cbor:"itemType"`
	Scalar                          *IntermediateColumnProps `cbor:"scalar"`
	NonScalarHomogenousArray        *IntermediateColumnProps `cbor:"nonScalarHomogenousArray"`
	NonScalarHomogenousArraySupport *IntermediateColumnProps `cbor:"nonScalarHomogenousArraySupport"`
	NonScalarSet                    *IntermediateColumnProps `cbor:"nonScalarSet"`
	NonScalarSetSupport             *IntermediateColumnProps `cbor:"nonScalarSetSupport"`
	StreamingGroup                  naming.Key               `cbor:"streamingGroup"`
}
type IntermediateColumnIterator = iter.Seq2[IntermediateColumnContext, *IntermediateColumnProps]
type IntermediateTableRepresentation struct {
	PlainValueDesc  []*IntermediatePlainValuesDesc  `cbor:"plainValueDesc"`
	TaggedValueDesc []*IntermediateTaggedValuesDesc `cbor:"taggedValueDesc"`
}

var ErrNotImplemented = eh.Errorf("not implemented")
var ErrNoBuilder = eh.Errorf("no builder to write code to")

type CodeBuilderHolderI interface {
	SetCodeBuilder(s *strings.Builder)
	GetCode() (code string, err error)
	ResetCodeBuilder()
}
type GeneratorHolderI interface {
	SetGenerator(generator TechnologySpecificGeneratorI)
}
type NamingConventionHolderI interface {
	SetNamingConvention(convention NamingConventionI)
}
type ColumnRoleE string

type TableRowConfigE uint8

type MembershipSpecE uint8

type PlainItemTypeE uint8

var ErrNoCodebuilder = eh.Errorf("no codebuilder set")

type TableDictionaryEntryDescDto struct {
	Name    naming.StylableName
	Comment string
}
type TableDesc struct {
	DictionaryEntry TableDictionaryEntryDescDto

	PlainValuesNames          []naming.StylableName
	PlainValuesTypes          []canonicaltypes.PrimitiveAstNodeI
	PlainValuesEncodingHints  []encodingaspects.AspectSet
	PlainValuesItemTypes      []PlainItemTypeE
	PlainValuesValueSemantics []valueaspects.AspectSet
	OpaqueStreamingGroup      naming.Key

	TaggedValuesSections []TaggedValuesSection
}

type TableDescDto struct {
	DictionaryEntry TableDictionaryEntryDescDto `cbor:"dictionaryEntry" json:"dictionaryEntry"`

	EntityIdNames                 [] /*i*/ naming.StylableName       `cbor:"entityIdNames" json:"entityIdNames"`
	EntityIdTypes                 [] /*i*/ string                    `cbor:"entityIdTypes" json:"entityIdTypes"`
	EntityIdEncodingHints         [] /*i*/ encodingaspects.AspectSet `cbor:"entityIdEncodingHints" json:"entityIdEncodingHints"`
	EntityIdValueSemantics        [] /*i*/ valueaspects.AspectSet    `cbor:"entityIdValueSemantics" json:"entityIdValueSemantics"`
	EntityTimestampNames          [] /*j*/ naming.StylableName       `cbor:"entityTimestampNames" json:"entityTimestampNames"`
	EntityTimestampTypes          [] /*j*/ string                    `cbor:"entityTimestampTypes" json:"entityTimestampTypes"`
	EntityTimestampEncodingHints  [] /*j*/ encodingaspects.AspectSet `cbor:"entityTimestampEncodingHints" json:"entityTimestampEncodingHints"`
	EntityTimestampValueSemantics [] /*i*/ valueaspects.AspectSet    `cbor:"entityTimestampValueSemantics" json:"entityTimestampValueSemantics"`
	EntityRoutingNames            [] /*k*/ naming.StylableName       `cbor:"entityRoutingNames" json:"entityRoutingNames"`
	EntityRoutingTypes            [] /*k*/ string                    `cbor:"entityRoutingTypes" json:"entityRoutingTypes"`
	EntityRoutingEncodingHints    [] /*k*/ encodingaspects.AspectSet `cbor:"entityRoutingEncodingHints" json:"entityRoutingEncodingHints"`
	EntityRoutingValueSemantics   [] /*i*/ valueaspects.AspectSet    `cbor:"entityRoutingValueSemantics" json:"entityRoutingValueSemantics"`
	EntityLifecycleNames          [] /*l*/ naming.StylableName       `cbor:"entityLifecycleNames" json:"entityLifecycleNames"`
	EntityLifecycleTypes          [] /*l*/ string                    `cbor:"entityLifecycleTypes" json:"entityLifecycleTypes"`
	EntityLifecycleEncodingHints  [] /*l*/ encodingaspects.AspectSet `cbor:"entityLifecycleEncodingHints" json:"entityLifecycleEncodingHints"`
	EntityLifecycleValueSemantics [] /*i*/ valueaspects.AspectSet    `cbor:"entityLifecycleValueSemantics" json:"entityLifecycleValueSemantics"`

	TaggedValuesSections []TaggedValuesSectionDto `cbor:"taggedValuesSections" json:"TaggedValuesSections"`

	TransactionNames          [] /*m*/ naming.StylableName       `cbor:"transactionNames" json:"transactionNames"`
	TransactionTypes          [] /*m*/ string                    `cbor:"transactionTypes" json:"transactionTypes"`
	TransactionEncodingHints  [] /*m*/ encodingaspects.AspectSet `cbor:"transactionEncodingHints" json:"transactionEncodingHints"`
	TransactionValueSemantics [] /*i*/ valueaspects.AspectSet    `cbor:"transactionValueSemantics" json:"transactionValueSemantics"`

	OpaqueColumnNames          [] /*n*/ naming.StylableName       `cbor:"opaqueColumnNames" json:"opaqueColumnNames"`
	OpaqueColumnTypes          [] /*n*/ string                    `cbor:"opaqueColumnTypes" json:"opaqueColumnTypes"`
	OpaqueColumnEncodingHints  [] /*n*/ encodingaspects.AspectSet `cbor:"opaqueColumnEncodingHints" json:"opaqueColumnEncodingHints"`
	OpaqueColumnValueSemantics [] /*i*/ valueaspects.AspectSet    `cbor:"opaqueColumnValueSemantics" json:"opaqueColumnValueSemantics"`
	OpaqueColumnStreamingGroup naming.Key                         `cbor:"opaqueColumnStreamingGroup" json:"opaqueColumnStreamingGroup"`
}

type TaggedValuesSectionDto struct {
	Name                     naming.StylableName                `cbor:"name" json:"name"`
	MembershipSpec           MembershipSpecE                    `cbor:"membershipSpec" json:"membershipSpec"`
	ValueColumnNames         [] /*i*/ naming.StylableName       `cbor:"valueColumnNames" json:"valueColumnNames"`
	ValueColumnTypes         [] /*i*/ string                    `cbor:"valueColumnTypes" json:"valueColumnTypes"`
	ValueColumnEncodingHints [] /*i*/ encodingaspects.AspectSet `cbor:"valueColumnEncodingHints" json:"valueColumnEncodingHints"`
	ValueSemantics           [] /*i*/ valueaspects.AspectSet    `cbor:"valueSemantics" json:"ValueSemantics"`
	UseAspects               useaspects.AspectSet               `cbor:"useAspects" json:"useAspects"`
	CoSectionGroup           naming.Key                         `cbor:"coSectionGroup" json:"coSectionGroup"`
	StreamingGroup           naming.Key                         `cbor:"streamingGroup" json:"streamingGroup"`
}

// TaggedValuesSection Note: If multiple, non-scalar columns are given they must have the same length and have co-array semantics
type TaggedValuesSection struct {
	Name               naming.StylableName
	MembershipSpec     MembershipSpecE
	ValueColumnNames   [] /*i*/ naming.StylableName
	ValueColumnTypes   [] /*i*/ canonicaltypes.PrimitiveAstNodeI
	ValueEncodingHints [] /*i*/ encodingaspects.AspectSet
	ValueSemantics     [] /*i*/ valueaspects.AspectSet
	UseAspects         useaspects.AspectSet
	CoSectionGroup     naming.Key
	StreamingGroup     naming.Key
}
type PhysicalColumnDesc struct {
	NameComponents             []string `cbor:"nameComponents"`
	NameComponentsExplanation  []string `cbor:"nameComponentsExplanation"`
	Comment                    string   `cbor:"comment"`
	GeneratingNamingConvention NamingConventionI
}

type TechnologySpecificMembershipSetGenI interface {
	ResolveMembership(s MembershipSpecE) (ct1 canonicaltypes.PrimitiveAstNodeI, hint1 encodingaspects.AspectSet, colRole1 ColumnRoleE, ct2 canonicaltypes.PrimitiveAstNodeI, hint2 encodingaspects.AspectSet, colRole2 ColumnRoleE, cardRole ColumnRoleE, err error)
}
type TechnologySpecificCodeGeneratorFwdI interface {
	GenerateColumnCode(idx int, phy PhysicalColumnDesc) (err error)
	GenerateType(canonicalType canonicaltypes.PrimitiveAstNodeI) (err error)
}
type TechnologySpecificCompatibilityI interface {
	CheckTypeCompatibility(canonicalType canonicaltypes.PrimitiveAstNodeI) (compatible bool, msg string)
	GetEncodingHintImplementationStatus(hint encodingaspects.AspectE) (status ImplementationStatusE, msg string)
}

type TechnologySpecificGeneratorI interface {
	CodeBuilderHolderI
	TechnologySpecificMembershipSetGenI
	TechnologySpecificCodeGeneratorFwdI
	TechnologySpecificCompatibilityI

	// GetTechnology stateless
	GetTechnology() (tech TechnologyDto)
}

var ErrInvalidMembershipSpec = eh.Errorf("invalid membership spec")

type ImplementationStatusE uint8

type NamingConventionFwdI interface {
	// MapIntermediateToPhysicalColumns mapping has to be 1:1 (i.e. len(cp.Names) == len(out))
	MapIntermediateToPhysicalColumns(cc IntermediateColumnContext, cp IntermediateColumnProps, in []PhysicalColumnDesc, tableRowConfig TableRowConfigE) (out []PhysicalColumnDesc, err error)
}
type NamingConventionBwdI interface {
	ExtractCanonicalType(column PhysicalColumnDesc) (ct canonicaltypes.PrimitiveAstNodeI, err error)
	ExtractEncodingHints(column PhysicalColumnDesc) (hints encodingaspects.AspectSet, err error)
	ExtractValueSemantics(column PhysicalColumnDesc) (semantics valueaspects.AspectSet, err error)
	ExtractTableRowConfig(column PhysicalColumnDesc) (tableRowConfig TableRowConfigE, err error)
	ExtractPlainItemType(column PhysicalColumnDesc) (plainItemType PlainItemTypeE, err error)
	ExtractSectionName(column PhysicalColumnDesc) (sectionName naming.StylableName, err error)
	ExtractLeewayColumnName(column PhysicalColumnDesc) (columName naming.StylableName, err error)
	ParseColumn(fullColumnName string) (column PhysicalColumnDesc, err error)

	DiscoverTableFromPhysicalColumns(phys []PhysicalColumnDesc) (table TableDesc, tableRowConfig TableRowConfigE, err error)
	DiscoverTableFromColumnNames(columnNames []string) (table TableDesc, tableRowConfig TableRowConfigE, err error)
}

type NamingConventionI interface {
	NamingConventionFwdI
	NamingConventionBwdI
}
type TableValidator struct {
}
type TableMarshaller struct {
}
type TableManipulator struct {
}

type TableManipulatorFluidI interface {
	//SetTableName(name naming.StylableName) TableManipulatorFluidI
	//SetTableComment(comment string) TableManipulatorFluidI
	TaggedValueSection(sectionName naming.StylableName) TaggedValueSectionMerger
	PlainValueColumn(itemType PlainItemTypeE, name naming.StylableName, canonicalType canonicaltypes.PrimitiveAstNodeI) PlainValueColumnMerger
	Reset()
}

type IntermediatePairHolder struct {
}

type TaggedValueSectionMerger struct {
}
type TaggedValueColumnMerger struct {
}
type PlainValueColumnMerger struct {
}


```

--- FILE: ddl/arrow/lw_ddl_arrow.go ---
```go
package arrow

import (
	_ "fmt"
	"strings"
	_ "strings"

	_ "github.com/stergiotis/boxer/public/observability/eh"
	_ "github.com/stergiotis/boxer/public/observability/eh/eb"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/common"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	encodingaspects2 "github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/canonicaltypes"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/common"
)

type TechnologySpecificCodeGenerator struct {
}

func (inst *TechnologySpecificCodeGenerator) GetEncodingHintImplementationStatus(hint encodingaspects2.AspectE) (status common.ImplementationStatusE, msg string) {
	panic("stub")
}

func (inst *TechnologySpecificCodeGenerator) CheckTypeCompatibility(canonicalType canonicaltypes.PrimitiveAstNodeI) (compatible bool, msg string) {
	panic("stub")
}

func (inst *TechnologySpecificCodeGenerator) ResolveMembership(s common.MembershipSpecE) (ct1 canonicaltypes.PrimitiveAstNodeI, hint1 encodingaspects2.AspectSet, colRole1 common.ColumnRoleE, ct2 canonicaltypes.PrimitiveAstNodeI, hint2 encodingaspects2.AspectSet, colRole2 common.ColumnRoleE, cardRole common.ColumnRoleE, err error) {
	panic("stub")
}

func (inst *TechnologySpecificCodeGenerator) GenerateType(canonicalType canonicaltypes.PrimitiveAstNodeI) (err error) {
	panic("stub")
}

func (inst *TechnologySpecificCodeGenerator) GenerateColumnCode(idx int, phy common.PhysicalColumnDesc) (err error) {
	panic("stub")
}

func (inst *TechnologySpecificCodeGenerator) ResetCodeBuilder() { panic("stub") }

func (inst *TechnologySpecificCodeGenerator) GetCode() (code string, err error) { panic("stub") }

func (inst *TechnologySpecificCodeGenerator) GetTechnology() (tech common.TechnologyDto) {
	panic("stub")
}

func NewTechnologySpecificCodeGenerator() (inst *TechnologySpecificCodeGenerator) { panic("stub") }

func (inst *TechnologySpecificCodeGenerator) SetCodeBuilder(s *strings.Builder) { panic("stub") }


```

--- FILE: ddl/clickhouse/lw_ddl_clickhouse.go ---
```go
package clickhouse

import (
	_ "fmt"
	"strings"
	_ "strings"

	_ "github.com/stergiotis/boxer/public/observability/eh"
	_ "github.com/stergiotis/boxer/public/observability/eh/eb"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/common"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	encodingaspects2 "github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/canonicaltypes"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/common"
)

type TechnologySpecificCodeGenerator struct {
}

func (inst *TechnologySpecificCodeGenerator) GetEncodingHintImplementationStatus(hint encodingaspects2.AspectE) (status common.ImplementationStatusE, msg string) {
	panic("stub")
}

func (inst *TechnologySpecificCodeGenerator) CheckTypeCompatibility(canonicalType canonicaltypes.PrimitiveAstNodeI) (compatible bool, msg string) {
	panic("stub")
}

func (inst *TechnologySpecificCodeGenerator) ResolveMembership(s common.MembershipSpecE) (ct1 canonicaltypes.PrimitiveAstNodeI, hint1 encodingaspects2.AspectSet, colRole1 common.ColumnRoleE, ct2 canonicaltypes.PrimitiveAstNodeI, hint2 encodingaspects2.AspectSet, colRole2 common.ColumnRoleE, cardRole common.ColumnRoleE, err error) {
	panic("stub")
}

func (inst *TechnologySpecificCodeGenerator) GenerateType(canonicalType canonicaltypes.PrimitiveAstNodeI) (err error) {
	panic("stub")
}

func (inst *TechnologySpecificCodeGenerator) GenerateColumnCode(idx int, phy common.PhysicalColumnDesc) (err error) {
	panic("stub")
}

// FIXME escaping

// FIXME escaping

func (inst *TechnologySpecificCodeGenerator) ResetCodeBuilder() { panic("stub") }

func (inst *TechnologySpecificCodeGenerator) GetCode() (code string, err error) { panic("stub") }

func (inst *TechnologySpecificCodeGenerator) GetTechnology() (tech common.TechnologyDto) {
	panic("stub")
}

func NewTechnologySpecificCodeGenerator() (inst *TechnologySpecificCodeGenerator) { panic("stub") }

func (inst *TechnologySpecificCodeGenerator) SetCodeBuilder(s *strings.Builder) { panic("stub") }

// 9 = nanosecond precision


```

--- FILE: ddl/clickhouse/lw_ddl_clickhouse_testutils.go ---
```go
package clickhouse

import (
	_ "os"

	_ "github.com/stergiotis/boxer/public/observability/eh"
)

func GetClickHouseBinaryPath() (path string, err error) { panic("stub") }


```

--- FILE: ddl/golang/lw_ddl_go.go ---
```go
package golang

import (
	_ "bytes"
	"io"
	_ "io"
	"strings"
	_ "strings"

	_ "github.com/ettle/strcase"
	_ "github.com/go-json-experiment/json"
	_ "github.com/go-json-experiment/json/jsontext"
	_ "github.com/stergiotis/boxer/public/observability/eh"
	_ "github.com/stergiotis/boxer/public/observability/eh/eb"
	canonicaltypes2 "github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/codegen"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/common"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	encodingaspects2 "github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/common"
)

type TechnologySpecificCodeGenerator struct {
}

func (inst *TechnologySpecificCodeGenerator) GetEncodingHintImplementationStatus(hint encodingaspects2.AspectE) (status common.ImplementationStatusE, msg string) {
	panic("stub")
}

func (inst *TechnologySpecificCodeGenerator) CheckTypeCompatibility(canonicalType canonicaltypes2.PrimitiveAstNodeI) (compatible bool, msg string) {
	panic("stub")
}

func (inst *TechnologySpecificCodeGenerator) ResolveMembership(s common.MembershipSpecE) (ct1 canonicaltypes2.PrimitiveAstNodeI, hint1 encodingaspects2.AspectSet, colRole1 common.ColumnRoleE, ct2 canonicaltypes2.PrimitiveAstNodeI, hint2 encodingaspects2.AspectSet, colRole2 common.ColumnRoleE, cardRole common.ColumnRoleE, err error) {
	panic("stub")
}

func (inst *TechnologySpecificCodeGenerator) GenerateType(canonicalType canonicaltypes2.PrimitiveAstNodeI) (err error) {
	panic("stub")
}

// TODO pass encoding aspects (only needed for imports, but you never know...)

type LeewayGoStructTag struct {
	ColumnNameComponents            []string `json:"columnNameComponents,omitempty"`
	ColumnNameComponentsExplanation []string `json:"columnNameComponentsExplanation,omitempty"`
	Comment                         string   `json:"comment,omitempty"`
}

func (inst LeewayGoStructTag) Marshall(w io.Writer) (err error) { panic("stub") }

func (inst *TechnologySpecificCodeGenerator) GenerateColumnCode(idx int, phy common.PhysicalColumnDesc) (err error) {
	panic("stub")
}

//name := strcase.ToPascal(phy.GetName())

// FIXME escaping

func (inst *TechnologySpecificCodeGenerator) ResetCodeBuilder() { panic("stub") }

func (inst *TechnologySpecificCodeGenerator) GetCode() (code string, err error) { panic("stub") }

func (inst *TechnologySpecificCodeGenerator) GetTechnology() (tech common.TechnologyDto) {
	panic("stub")
}

func NewTechnologySpecificCodeGenerator() (inst *TechnologySpecificCodeGenerator) { panic("stub") }

func (inst *TechnologySpecificCodeGenerator) SetCodeBuilder(s *strings.Builder) { panic("stub") }


```

--- FILE: ddl/lw_ddl_coverage.go ---
```go
package ddl

import (
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/sample"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/common"
)

type CoverageResult struct {
	NotCovered                 []string
	CoverageTypeMachineNumeric float64
	CoverageTypeTemporal       float64
	CoverageTypeString         float64
	CoverageTypeTotal          float64
}

func MeasureTechCoverage(techSpecificGen common.TechnologySpecificGeneratorI) (coverage CoverageResult) {
	panic("stub")
}


```

--- FILE: ddl/lw_ddl_gen_naming_human.go ---
```go
package ddl

import (
	_ "slices"
	_ "strings"

	_ "github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh"
	_ "github.com/stergiotis/boxer/public/observability/eh"
	_ "github.com/stergiotis/boxer/public/observability/eh/eb"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/base62"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/common"
	encodingaspects2 "github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/canonicaltypes"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/common"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/naming"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/valueaspects"
)

const IdPrefix string = "id"
const TimestampPrefix string = "ts"
const RoutingPrefix string = "ro"
const LifecyclePrefix string = "lc"
const TransactionPrefix string = "tx"
const OpaquePrefix string = "oq"
const TaggedValuePrefix string = "tv"

const SeparatorExplanation = "separator"

type HumanReadableNamingConvention struct {
}

var ColumnsComponentsExplanation13 = []string{
	componentPrefix,
	SeparatorExplanation,
	componentColumnName,
	SeparatorExplanation,
	componentCanonicalType,
	SeparatorExplanation,
	componentEncodingHints,
	SeparatorExplanation,
	componentValueSemantics,
	SeparatorExplanation,
	componentTableRowConfig,
	SeparatorExplanation,
	componentStreamingGroup,
}
var ColumnsComponentsExplanation21 = []string{
	componentPrefix,
	SeparatorExplanation,
	componentSectionName,
	SeparatorExplanation,
	componentColumnName,
	SeparatorExplanation,
	componentRole,
	SeparatorExplanation,
	componentCanonicalType,
	SeparatorExplanation,
	componentEncodingHints,
	SeparatorExplanation,
	componentUseAspects,
	SeparatorExplanation,
	componentValueSemantics,
	SeparatorExplanation,
	componentTableRowConfig,
	SeparatorExplanation,
	componentCoSectionGroup,
	SeparatorExplanation,
	componentStreamingGroup,
}

var ErrUnhandledIntermediateColumnContextType = eh.Errorf("unhandled intermediate column context type")

func init() { panic("stub") }

var ErrUnhandledNumberOfComponents = eh.Errorf("unhandled number of components")
var ErrNameComponentContainsSeparator = eh.Errorf("name component contains separator")
var ErrParseError = eh.Errorf("parse error")
var ErrInvalidColumns = eh.Errorf("invalid column name")
var ErrInvalidCanonicalType = eh.Errorf("invalid canonical type")
var ErrInvalidAspects = eh.Errorf("invalid useaspects")

func NewHumanReadableNamingConvention(separator string) (inst *HumanReadableNamingConvention, err error) {
	panic("stub")
}

func (inst *HumanReadableNamingConvention) ExtractValueSemantics(column common.PhysicalColumnDesc) (semantics valueaspects.AspectSet, err error) {
	panic("stub")
}

func (inst *HumanReadableNamingConvention) ExtractSectionName(column common.PhysicalColumnDesc) (sectionName naming.StylableName, err error) {
	panic("stub")
}

func (inst *HumanReadableNamingConvention) ExtractLeewayColumnName(column common.PhysicalColumnDesc) (columnName naming.StylableName, err error) {
	panic("stub")
}

func (inst *HumanReadableNamingConvention) ExtractPlainItemType(column common.PhysicalColumnDesc) (plainItemType common.PlainItemTypeE, err error) {
	panic("stub")
}

func (inst *HumanReadableNamingConvention) ExtractTableRowConfig(column common.PhysicalColumnDesc) (tableRowConfig common.TableRowConfigE, err error) {
	panic("stub")
}

func (inst *HumanReadableNamingConvention) ExtractEncodingHints(column common.PhysicalColumnDesc) (hints encodingaspects2.AspectSet, err error) {
	panic("stub")
}

func (inst *HumanReadableNamingConvention) ExtractCanonicalType(column common.PhysicalColumnDesc) (ct canonicaltypes.PrimitiveAstNodeI, err error) {
	panic("stub")
}

func (inst *HumanReadableNamingConvention) MapIntermediateToPhysicalColumns(cc common.IntermediateColumnContext, cp common.IntermediateColumnProps, in []common.PhysicalColumnDesc, tableRowConfig common.TableRowConfigE) (out []common.PhysicalColumnDesc, err error) {
	panic("stub")
}

func (inst *HumanReadableNamingConvention) ParseColumns(columnNames []string) (phys []common.PhysicalColumnDesc, err error) {
	panic("stub")
}

func (inst *HumanReadableNamingConvention) DiscoverTableFromColumnNames(columnNames []string) (table common.TableDesc, tableRowConfig common.TableRowConfigE, err error) {
	panic("stub")
}

func (inst *HumanReadableNamingConvention) DiscoverTableFromPhysicalColumns(phys []common.PhysicalColumnDesc) (table common.TableDesc, tableRowConfig common.TableRowConfigE, err error) {
	panic("stub")
}

// mixed, trigger on other

// support column

// tableRowConfig

func (inst *HumanReadableNamingConvention) ParseColumn(fullColumnName string) (column common.PhysicalColumnDesc, err error) {
	panic("stub")
}

// TODO use SplitN?


```

--- FILE: ddl/lw_ddl_generator.go ---
```go
package ddl

import (
	_ "github.com/rs/zerolog/log"
	_ "github.com/stergiotis/boxer/public/compiletimeflags"
	_ "github.com/stergiotis/boxer/public/observability/eh"
	_ "github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/observability/vcs"
	_ "github.com/stergiotis/boxer/public/observability/vcs"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/common"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/common"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/encodingaspects"
)

var CodeGeneratorName = "ET7 DDL (" + vcs.ModuleInfo() + ")"

type GeneratorDriver struct {
}

func NewGeneratorDriver() *GeneratorDriver { panic("stub") }

func (inst *GeneratorDriver) GenerateColumnsCode(iter common.IntermediateColumnIterator, tableRowConfig common.TableRowConfigE, conv common.NamingConventionI, tech common.TechnologySpecificGeneratorI, checkEncodingAspect func(hint encodingaspects.AspectE) (ok bool, msg string)) (err error) {
	panic("stub")
}

// collect encoding aspects

// check canonical type


```

--- FILE: ddl/lw_ddl_tech_common.go ---
```go
package ddl

import (
	_ "slices"

	_ "github.com/stergiotis/boxer/public/observability/eh/eb"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/common"
	encodingaspects2 "github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/canonicaltypes"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/common"
)

type CanonicalColumnarRepresentation struct {
}

func NewCanonicalColumnarRepresentation(aspectFilterFunc func(aspect encodingaspects2.AspectE) (keep bool, msg string)) *CanonicalColumnarRepresentation {
	panic("stub")
}

func FilterEncodingAspect(filterFunc func(aspect encodingaspects2.AspectE) (keep bool, msg string), a ...encodingaspects2.AspectE) []encodingaspects2.AspectE {
	panic("stub")
}

func EncodingAspectFilterFuncFromTechnology(tech common.TechnologySpecificGeneratorI, minImplementationStatusIncl common.ImplementationStatusE) func(aspect encodingaspects2.AspectE) (keep bool, msg string) {
	panic("stub")
}

func (inst *CanonicalColumnarRepresentation) ResolveMembership(s common.MembershipSpecE) (ct1 canonicaltypes.PrimitiveAstNodeI, hint1 encodingaspects2.AspectSet, colRole1 common.ColumnRoleE, ct2 canonicaltypes.PrimitiveAstNodeI, hint2 encodingaspects2.AspectSet, colRole2 common.ColumnRoleE, cardRole common.ColumnRoleE, err error) {
	panic("stub")
}

// NOTE: is high cardinality (parametrization is always high-card, even when the ref is low-card)


```

--- FILE: dml/example/cli.go ---
```go
package example

import (
	_ "bufio"
	_ "bytes"
	_ "errors"
	_ "hash"
	_ "io"
	_ "math"
	_ "os"
	_ "slices"
	_ "strconv"

	_ "github.com/apache/arrow-go/v18/arrow"
	_ "github.com/apache/arrow-go/v18/arrow/ipc"
	_ "github.com/apache/arrow-go/v18/arrow/memory"
	_ "github.com/apache/arrow-go/v18/parquet"
	_ "github.com/apache/arrow-go/v18/parquet/compress"
	_ "github.com/apache/arrow-go/v18/parquet/pqarrow"
	_ "github.com/go-json-experiment/json/jsontext"
	_ "github.com/rs/zerolog/log"
	_ "github.com/stergiotis/boxer/public/containers"
	_ "github.com/stergiotis/boxer/public/db/clickhouse/dsl"
	_ "github.com/stergiotis/boxer/public/observability/eh"
	_ "github.com/stergiotis/boxer/public/observability/eh/eb"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/base62"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/dml"
	"github.com/urfave/cli"
	_ "github.com/urfave/cli/v2"
	_ "lukechampine.com/blake3"
)

// dictionary key

//log.Info().Str("lc", string(lowCardPtr)).Str("hc", string(highCardPtr)).Str("token", string(kind)).Msg("got one")

func NewCliCommand() *cli.Command { panic("stub") }


```

--- FILE: dml/example/dml_json.out.go ---
```go
// Code generated; Leeway DML (github.com/stergiotis/boxer/public/semistructured/leeway/dml.test) DO NOT EDIT.

package example

import (
	_ "errors"
	_ "slices"

	"github.com/apache/arrow-go/v18/arrow"
	_ "github.com/apache/arrow-go/v18/arrow"
	_ "github.com/apache/arrow-go/v18/arrow/array"
	_ "github.com/apache/arrow-go/v18/arrow/ipc"
	_ "github.com/apache/arrow-go/v18/arrow/math"
	_ "github.com/apache/arrow-go/v18/arrow/memory"
	_ "github.com/stergiotis/boxer/public/observability/eh"
	_ "github.com/stergiotis/boxer/public/observability/eh/eb"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/dml/runtime"
	"modernc.org/memory"
)

///////////////////////////////////////////////////////////////////
// code generator
// gocodegen.GenerateArrowSchemaFactory
// ./public/semistructured/leeway/gocodegen/gocodegen_common.go:26

func CreateSchemaJson() (schema *arrow.Schema) { panic("stub") }

/* 000 */
/* 001 */
/* 002 */
/* 003 */
/* 004 */
/* 005 */
/* 006 */
/* 007 */
/* 008 */
/* 009 */
/* 010 */
/* 011 */
/* 012 */
/* 013 */
/* 014 */
/* 015 */
/* 016 */
/* 017 */
/* 018 */
/* 019 */
/* 020 */
/* 021 */
/* 022 */
/* 023 */
/* 024 */
/* 025 */
/* 026 */

///////////////////////////////////////////////////////////////////
// code generator
// dml.(*GoClassBuilder).ComposeEntityClassAndFactoryCode
// ./public/semistructured/leeway/dml/lw_dml_generator.go:1175

type InEntityJson struct {
}

func NewInEntityJson(allocator memory.Allocator, estimatedNumberOfRecords int) (inst *InEntityJson) {
	panic("stub")
}

///////////////////////////////////////////////////////////////////
// code generator
// dml.(*GoClassBuilder).ComposeEntityCode
// ./public/semistructured/leeway/dml/lw_dml_generator.go:1289

func (inst *InEntityJson) SetId(blake3hash0 []byte) *InEntityJson { panic("stub") }

func (inst *InEntityJson) CheckErrors() (err error) { panic("stub") }

func (inst *InEntityJson) GetSectionBool() *InEntityJsonSectionBool { panic("stub") }

func (inst *InEntityJson) GetSectionFloat64() *InEntityJsonSectionFloat64 { panic("stub") }

func (inst *InEntityJson) GetSectionInt64() *InEntityJsonSectionInt64 { panic("stub") }

func (inst *InEntityJson) GetSectionNull() *InEntityJsonSectionNull { panic("stub") }

func (inst *InEntityJson) GetSectionString() *InEntityJsonSectionString { panic("stub") }

func (inst *InEntityJson) GetSectionSymbol() *InEntityJsonSectionSymbol { panic("stub") }

func (inst *InEntityJson) GetSectionUndefined() *InEntityJsonSectionUndefined { panic("stub") }

func (inst *InEntityJson) BeginEntity() *InEntityJson { panic("stub") }

// FIXME check coSectionGroup consistency

func (inst *InEntityJson) CommitEntity() (err error) { panic("stub") }

func (inst *InEntityJson) RollbackEntity() (err error) { panic("stub") }

// arrow fields must all have one row

// FIXME find better way to truncate builder

// TransferRecords The returned Records must be Release()'d after use.
func (inst *InEntityJson) TransferRecords(recordsIn []arrow.Record) (recordsOut []arrow.Record, err error) {
	panic("stub")
}

func (inst *InEntityJson) GetSchema() (schema *arrow.Schema) { panic("stub") }

func (inst *InEntityJson) AppendError(err error) { panic("stub") }

type InEntityJsonSectionBool struct {
}

func NewInEntityJsonSectionBool(builder *array.RecordBuilder, parent *InEntityJson) (inst *InEntityJsonSectionBool) {
	panic("stub")
}

func (inst *InEntityJsonSectionBool) BeginAttribute(value1 bool) *InEntityJsonSectionBoolInAttr {
	panic("stub")
}

func (inst *InEntityJsonSectionBool) CheckErrors() (err error) { panic("stub") }

func (inst *InEntityJsonSectionBool) EndSection() *InEntityJson { panic("stub") }

func (inst *InEntityJsonSectionBool) AppendError(err error) { panic("stub") }

type InEntityJsonSectionBoolInAttr struct {
}

func NewInEntityJsonSectionBoolInAttr(builder *array.RecordBuilder, parent *InEntityJsonSectionBool) (inst *InEntityJsonSectionBoolInAttr) {
	panic("stub")
}

func (inst *InEntityJsonSectionBoolInAttr) AddMembershipMixedLowCardVerbatim(lmv2 []byte, mvhp3 []byte) *InEntityJsonSectionBoolInAttr {
	panic("stub")
}

func (inst *InEntityJsonSectionBoolInAttr) AddMembershipMixedLowCardVerbatimP(lmv2 []byte, mvhp3 []byte) {
	panic("stub")
}

func (inst *InEntityJsonSectionBoolInAttr) EndSection() *InEntityJson { panic("stub") }

func (inst *InEntityJsonSectionBoolInAttr) EndAttribute() *InEntityJsonSectionBool { panic("stub") }

func (inst *InEntityJsonSectionBoolInAttr) AppendError(err error) { panic("stub") }

type InEntityJsonSectionFloat64 struct {
}

func NewInEntityJsonSectionFloat64(builder *array.RecordBuilder, parent *InEntityJson) (inst *InEntityJsonSectionFloat64) {
	panic("stub")
}

func (inst *InEntityJsonSectionFloat64) BeginAttribute(value19 float64) *InEntityJsonSectionFloat64InAttr {
	panic("stub")
}

func (inst *InEntityJsonSectionFloat64) CheckErrors() (err error) { panic("stub") }

func (inst *InEntityJsonSectionFloat64) EndSection() *InEntityJson { panic("stub") }

func (inst *InEntityJsonSectionFloat64) AppendError(err error) { panic("stub") }

type InEntityJsonSectionFloat64InAttr struct {
}

func NewInEntityJsonSectionFloat64InAttr(builder *array.RecordBuilder, parent *InEntityJsonSectionFloat64) (inst *InEntityJsonSectionFloat64InAttr) {
	panic("stub")
}

func (inst *InEntityJsonSectionFloat64InAttr) AddMembershipMixedLowCardVerbatim(lmv20 []byte, mvhp21 []byte) *InEntityJsonSectionFloat64InAttr {
	panic("stub")
}

func (inst *InEntityJsonSectionFloat64InAttr) AddMembershipMixedLowCardVerbatimP(lmv20 []byte, mvhp21 []byte) {
	panic("stub")
}

func (inst *InEntityJsonSectionFloat64InAttr) EndSection() *InEntityJson { panic("stub") }

func (inst *InEntityJsonSectionFloat64InAttr) EndAttribute() *InEntityJsonSectionFloat64 {
	panic("stub")
}

func (inst *InEntityJsonSectionFloat64InAttr) AppendError(err error) { panic("stub") }

type InEntityJsonSectionInt64 struct {
}

func NewInEntityJsonSectionInt64(builder *array.RecordBuilder, parent *InEntityJson) (inst *InEntityJsonSectionInt64) {
	panic("stub")
}

func (inst *InEntityJsonSectionInt64) BeginAttribute(value23 int64) *InEntityJsonSectionInt64InAttr {
	panic("stub")
}

func (inst *InEntityJsonSectionInt64) CheckErrors() (err error) { panic("stub") }

func (inst *InEntityJsonSectionInt64) EndSection() *InEntityJson { panic("stub") }

func (inst *InEntityJsonSectionInt64) AppendError(err error) { panic("stub") }

type InEntityJsonSectionInt64InAttr struct {
}

func NewInEntityJsonSectionInt64InAttr(builder *array.RecordBuilder, parent *InEntityJsonSectionInt64) (inst *InEntityJsonSectionInt64InAttr) {
	panic("stub")
}

func (inst *InEntityJsonSectionInt64InAttr) AddMembershipMixedLowCardVerbatim(lmv24 []byte, mvhp25 []byte) *InEntityJsonSectionInt64InAttr {
	panic("stub")
}

func (inst *InEntityJsonSectionInt64InAttr) AddMembershipMixedLowCardVerbatimP(lmv24 []byte, mvhp25 []byte) {
	panic("stub")
}

func (inst *InEntityJsonSectionInt64InAttr) EndSection() *InEntityJson { panic("stub") }

func (inst *InEntityJsonSectionInt64InAttr) EndAttribute() *InEntityJsonSectionInt64 { panic("stub") }

func (inst *InEntityJsonSectionInt64InAttr) AppendError(err error) { panic("stub") }

type InEntityJsonSectionNull struct {
}

func NewInEntityJsonSectionNull(builder *array.RecordBuilder, parent *InEntityJson) (inst *InEntityJsonSectionNull) {
	panic("stub")
}

func (inst *InEntityJsonSectionNull) BeginAttribute() *InEntityJsonSectionNullInAttr { panic("stub") }

func (inst *InEntityJsonSectionNull) CheckErrors() (err error) { panic("stub") }

func (inst *InEntityJsonSectionNull) EndSection() *InEntityJson { panic("stub") }

func (inst *InEntityJsonSectionNull) AppendError(err error) { panic("stub") }

type InEntityJsonSectionNullInAttr struct {
}

func NewInEntityJsonSectionNullInAttr(builder *array.RecordBuilder, parent *InEntityJsonSectionNull) (inst *InEntityJsonSectionNullInAttr) {
	panic("stub")
}

func (inst *InEntityJsonSectionNullInAttr) AddMembershipMixedLowCardVerbatim(lmv8 []byte, mvhp9 []byte) *InEntityJsonSectionNullInAttr {
	panic("stub")
}

func (inst *InEntityJsonSectionNullInAttr) AddMembershipMixedLowCardVerbatimP(lmv8 []byte, mvhp9 []byte) {
	panic("stub")
}

func (inst *InEntityJsonSectionNullInAttr) EndSection() *InEntityJson { panic("stub") }

func (inst *InEntityJsonSectionNullInAttr) EndAttribute() *InEntityJsonSectionNull { panic("stub") }

func (inst *InEntityJsonSectionNullInAttr) AppendError(err error) { panic("stub") }

type InEntityJsonSectionString struct {
}

func NewInEntityJsonSectionString(builder *array.RecordBuilder, parent *InEntityJson) (inst *InEntityJsonSectionString) {
	panic("stub")
}

func (inst *InEntityJsonSectionString) BeginAttribute(value11 string) *InEntityJsonSectionStringInAttr {
	panic("stub")
}

func (inst *InEntityJsonSectionString) CheckErrors() (err error) { panic("stub") }

func (inst *InEntityJsonSectionString) EndSection() *InEntityJson { panic("stub") }

func (inst *InEntityJsonSectionString) AppendError(err error) { panic("stub") }

type InEntityJsonSectionStringInAttr struct {
}

func NewInEntityJsonSectionStringInAttr(builder *array.RecordBuilder, parent *InEntityJsonSectionString) (inst *InEntityJsonSectionStringInAttr) {
	panic("stub")
}

func (inst *InEntityJsonSectionStringInAttr) AddMembershipMixedLowCardVerbatim(lmv12 []byte, mvhp13 []byte) *InEntityJsonSectionStringInAttr {
	panic("stub")
}

func (inst *InEntityJsonSectionStringInAttr) AddMembershipMixedLowCardVerbatimP(lmv12 []byte, mvhp13 []byte) {
	panic("stub")
}

func (inst *InEntityJsonSectionStringInAttr) EndSection() *InEntityJson { panic("stub") }

func (inst *InEntityJsonSectionStringInAttr) EndAttribute() *InEntityJsonSectionString { panic("stub") }

func (inst *InEntityJsonSectionStringInAttr) AppendError(err error) { panic("stub") }

type InEntityJsonSectionSymbol struct {
}

func NewInEntityJsonSectionSymbol(builder *array.RecordBuilder, parent *InEntityJson) (inst *InEntityJsonSectionSymbol) {
	panic("stub")
}

func (inst *InEntityJsonSectionSymbol) BeginAttribute(value15 string) *InEntityJsonSectionSymbolInAttr {
	panic("stub")
}

func (inst *InEntityJsonSectionSymbol) CheckErrors() (err error) { panic("stub") }

func (inst *InEntityJsonSectionSymbol) EndSection() *InEntityJson { panic("stub") }

func (inst *InEntityJsonSectionSymbol) AppendError(err error) { panic("stub") }

type InEntityJsonSectionSymbolInAttr struct {
}

func NewInEntityJsonSectionSymbolInAttr(builder *array.RecordBuilder, parent *InEntityJsonSectionSymbol) (inst *InEntityJsonSectionSymbolInAttr) {
	panic("stub")
}

func (inst *InEntityJsonSectionSymbolInAttr) AddMembershipMixedLowCardVerbatim(lmv16 []byte, mvhp17 []byte) *InEntityJsonSectionSymbolInAttr {
	panic("stub")
}

func (inst *InEntityJsonSectionSymbolInAttr) AddMembershipMixedLowCardVerbatimP(lmv16 []byte, mvhp17 []byte) {
	panic("stub")
}

func (inst *InEntityJsonSectionSymbolInAttr) EndSection() *InEntityJson { panic("stub") }

func (inst *InEntityJsonSectionSymbolInAttr) EndAttribute() *InEntityJsonSectionSymbol { panic("stub") }

func (inst *InEntityJsonSectionSymbolInAttr) AppendError(err error) { panic("stub") }

type InEntityJsonSectionUndefined struct {
}

func NewInEntityJsonSectionUndefined(builder *array.RecordBuilder, parent *InEntityJson) (inst *InEntityJsonSectionUndefined) {
	panic("stub")
}

func (inst *InEntityJsonSectionUndefined) BeginAttribute() *InEntityJsonSectionUndefinedInAttr {
	panic("stub")
}

func (inst *InEntityJsonSectionUndefined) CheckErrors() (err error) { panic("stub") }

func (inst *InEntityJsonSectionUndefined) EndSection() *InEntityJson { panic("stub") }

func (inst *InEntityJsonSectionUndefined) AppendError(err error) { panic("stub") }

type InEntityJsonSectionUndefinedInAttr struct {
}

func NewInEntityJsonSectionUndefinedInAttr(builder *array.RecordBuilder, parent *InEntityJsonSectionUndefined) (inst *InEntityJsonSectionUndefinedInAttr) {
	panic("stub")
}

func (inst *InEntityJsonSectionUndefinedInAttr) AddMembershipMixedLowCardVerbatim(lmv5 []byte, mvhp6 []byte) *InEntityJsonSectionUndefinedInAttr {
	panic("stub")
}

func (inst *InEntityJsonSectionUndefinedInAttr) AddMembershipMixedLowCardVerbatimP(lmv5 []byte, mvhp6 []byte) {
	panic("stub")
}

func (inst *InEntityJsonSectionUndefinedInAttr) EndSection() *InEntityJson { panic("stub") }

func (inst *InEntityJsonSectionUndefinedInAttr) EndAttribute() *InEntityJsonSectionUndefined {
	panic("stub")
}

func (inst *InEntityJsonSectionUndefinedInAttr) AppendError(err error) { panic("stub") }


```

--- FILE: dml/example/dml_testtable.out.go ---
```go
// Code generated; Leeway DML (github.com/stergiotis/boxer/public/semistructured/leeway/dml.test) DO NOT EDIT.

package example

import (
	_ "errors"
	_ "slices"
	"time"
	_ "time"

	"github.com/apache/arrow-go/v18/arrow"
	_ "github.com/apache/arrow-go/v18/arrow"
	_ "github.com/apache/arrow-go/v18/arrow/array"
	_ "github.com/apache/arrow-go/v18/arrow/ipc"
	_ "github.com/apache/arrow-go/v18/arrow/math"
	_ "github.com/apache/arrow-go/v18/arrow/memory"
	_ "github.com/stergiotis/boxer/public/observability/eh"
	_ "github.com/stergiotis/boxer/public/observability/eh/eb"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/dml/runtime"
	"modernc.org/memory"
)

///////////////////////////////////////////////////////////////////
// code generator
// gocodegen.GenerateArrowSchemaFactory
// ./public/semistructured/leeway/gocodegen/gocodegen_common.go:26

func CreateSchemaTesttable() (schema *arrow.Schema) { panic("stub") }

/* 000 */
/* 001 */
/* 002 */
/* 003 */
/* 004 */
/* 005 */
/* 006 */
/* 007 */
/* 008 */
/* 009 */
/* 010 */
/* 011 */
/* 012 */
/* 013 */
/* 014 */
/* 015 */
/* 016 */
/* 017 */
/* 018 */
/* 019 */
/* 020 */

///////////////////////////////////////////////////////////////////
// code generator
// dml.(*GoClassBuilder).ComposeEntityClassAndFactoryCode
// ./public/semistructured/leeway/dml/lw_dml_generator.go:1175

type InEntityTesttable struct {
}

func NewInEntityTesttable(allocator memory.Allocator, estimatedNumberOfRecords int) (inst *InEntityTesttable) {
	panic("stub")
}

///////////////////////////////////////////////////////////////////
// code generator
// dml.(*GoClassBuilder).ComposeEntityCode
// ./public/semistructured/leeway/dml/lw_dml_generator.go:1289

func (inst *InEntityTesttable) SetId(id0 uint64) *InEntityTesttable { panic("stub") }

///////////////////////////////////////////////////////////////////
// code generator
// dml.(*GoClassBuilder).ComposeEntityCode
// ./public/semistructured/leeway/dml/lw_dml_generator.go:1289

func (inst *InEntityTesttable) SetTimestamp(ts1 time.Time) *InEntityTesttable { panic("stub") }

func (inst *InEntityTesttable) CheckErrors() (err error) { panic("stub") }

func (inst *InEntityTesttable) GetSectionBool() *InEntityTesttableSectionBool { panic("stub") }

func (inst *InEntityTesttable) GetSectionFloat64() *InEntityTesttableSectionFloat64 { panic("stub") }

func (inst *InEntityTesttable) GetSectionSpecial() *InEntityTesttableSectionSpecial { panic("stub") }

func (inst *InEntityTesttable) GetSectionString() *InEntityTesttableSectionString { panic("stub") }

func (inst *InEntityTesttable) BeginEntity() *InEntityTesttable { panic("stub") }

// FIXME check coSectionGroup consistency

func (inst *InEntityTesttable) CommitEntity() (err error) { panic("stub") }

func (inst *InEntityTesttable) RollbackEntity() (err error) { panic("stub") }

// arrow fields must all have one row

// FIXME find better way to truncate builder

// TransferRecords The returned Records must be Release()'d after use.
func (inst *InEntityTesttable) TransferRecords(recordsIn []arrow.Record) (recordsOut []arrow.Record, err error) {
	panic("stub")
}

func (inst *InEntityTesttable) GetSchema() (schema *arrow.Schema) { panic("stub") }

func (inst *InEntityTesttable) AppendError(err error) { panic("stub") }

type InEntityTesttableSectionBool struct {
}

func NewInEntityTesttableSectionBool(builder *array.RecordBuilder, parent *InEntityTesttable) (inst *InEntityTesttableSectionBool) {
	panic("stub")
}

func (inst *InEntityTesttableSectionBool) BeginAttribute(value2 bool) *InEntityTesttableSectionBoolInAttr {
	panic("stub")
}

func (inst *InEntityTesttableSectionBool) CheckErrors() (err error) { panic("stub") }

func (inst *InEntityTesttableSectionBool) EndSection() *InEntityTesttable { panic("stub") }

func (inst *InEntityTesttableSectionBool) AppendError(err error) { panic("stub") }

type InEntityTesttableSectionBoolInAttr struct {
}

func NewInEntityTesttableSectionBoolInAttr(builder *array.RecordBuilder, parent *InEntityTesttableSectionBool) (inst *InEntityTesttableSectionBoolInAttr) {
	panic("stub")
}

func (inst *InEntityTesttableSectionBoolInAttr) AddMembershipMixedLowCardVerbatim(lmv3 []byte, mvhp4 []byte) *InEntityTesttableSectionBoolInAttr {
	panic("stub")
}

func (inst *InEntityTesttableSectionBoolInAttr) AddMembershipMixedLowCardVerbatimP(lmv3 []byte, mvhp4 []byte) {
	panic("stub")
}

func (inst *InEntityTesttableSectionBoolInAttr) EndSection() *InEntityTesttable { panic("stub") }

func (inst *InEntityTesttableSectionBoolInAttr) EndAttribute() *InEntityTesttableSectionBool {
	panic("stub")
}

func (inst *InEntityTesttableSectionBoolInAttr) AppendError(err error) { panic("stub") }

type InEntityTesttableSectionFloat64 struct {
}

func NewInEntityTesttableSectionFloat64(builder *array.RecordBuilder, parent *InEntityTesttable) (inst *InEntityTesttableSectionFloat64) {
	panic("stub")
}

func (inst *InEntityTesttableSectionFloat64) BeginAttribute(value10 float64) *InEntityTesttableSectionFloat64InAttr {
	panic("stub")
}

func (inst *InEntityTesttableSectionFloat64) CheckErrors() (err error) { panic("stub") }

func (inst *InEntityTesttableSectionFloat64) EndSection() *InEntityTesttable { panic("stub") }

func (inst *InEntityTesttableSectionFloat64) AppendError(err error) { panic("stub") }

type InEntityTesttableSectionFloat64InAttr struct {
}

func NewInEntityTesttableSectionFloat64InAttr(builder *array.RecordBuilder, parent *InEntityTesttableSectionFloat64) (inst *InEntityTesttableSectionFloat64InAttr) {
	panic("stub")
}

func (inst *InEntityTesttableSectionFloat64InAttr) AddMembershipMixedLowCardVerbatim(lmv11 []byte, mvhp12 []byte) *InEntityTesttableSectionFloat64InAttr {
	panic("stub")
}

func (inst *InEntityTesttableSectionFloat64InAttr) AddMembershipMixedLowCardVerbatimP(lmv11 []byte, mvhp12 []byte) {
	panic("stub")
}

func (inst *InEntityTesttableSectionFloat64InAttr) EndSection() *InEntityTesttable { panic("stub") }

func (inst *InEntityTesttableSectionFloat64InAttr) EndAttribute() *InEntityTesttableSectionFloat64 {
	panic("stub")
}

func (inst *InEntityTesttableSectionFloat64InAttr) AppendError(err error) { panic("stub") }

type InEntityTesttableSectionSpecial struct {
}

func NewInEntityTesttableSectionSpecial(builder *array.RecordBuilder, parent *InEntityTesttable) (inst *InEntityTesttableSectionSpecial) {
	panic("stub")
}

func (inst *InEntityTesttableSectionSpecial) BeginAttribute(spc14 string) *InEntityTesttableSectionSpecialInAttr {
	panic("stub")
}

func (inst *InEntityTesttableSectionSpecial) CheckErrors() (err error) { panic("stub") }

func (inst *InEntityTesttableSectionSpecial) EndSection() *InEntityTesttable { panic("stub") }

func (inst *InEntityTesttableSectionSpecial) AppendError(err error) { panic("stub") }

type InEntityTesttableSectionSpecialInAttr struct {
}

func NewInEntityTesttableSectionSpecialInAttr(builder *array.RecordBuilder, parent *InEntityTesttableSectionSpecial) (inst *InEntityTesttableSectionSpecialInAttr) {
	panic("stub")
}

func (inst *InEntityTesttableSectionSpecialInAttr) AddToCoContainers(ary115 uint32, ary216 uint32) *InEntityTesttableSectionSpecialInAttr {
	panic("stub")
}

func (inst *InEntityTesttableSectionSpecialInAttr) AddMembershipMixedLowCardRef(lmr17 uint64, mrhp18 []byte) *InEntityTesttableSectionSpecialInAttr {
	panic("stub")
}

func (inst *InEntityTesttableSectionSpecialInAttr) AddMembershipMixedLowCardRefP(lmr17 uint64, mrhp18 []byte) {
	panic("stub")
}

func (inst *InEntityTesttableSectionSpecialInAttr) EndSection() *InEntityTesttable { panic("stub") }

func (inst *InEntityTesttableSectionSpecialInAttr) EndAttribute() *InEntityTesttableSectionSpecial {
	panic("stub")
}

func (inst *InEntityTesttableSectionSpecialInAttr) AppendError(err error) { panic("stub") }

type InEntityTesttableSectionString struct {
}

func NewInEntityTesttableSectionString(builder *array.RecordBuilder, parent *InEntityTesttable) (inst *InEntityTesttableSectionString) {
	panic("stub")
}

func (inst *InEntityTesttableSectionString) BeginAttribute(value6 string) *InEntityTesttableSectionStringInAttr {
	panic("stub")
}

func (inst *InEntityTesttableSectionString) CheckErrors() (err error) { panic("stub") }

func (inst *InEntityTesttableSectionString) EndSection() *InEntityTesttable { panic("stub") }

func (inst *InEntityTesttableSectionString) AppendError(err error) { panic("stub") }

type InEntityTesttableSectionStringInAttr struct {
}

func NewInEntityTesttableSectionStringInAttr(builder *array.RecordBuilder, parent *InEntityTesttableSectionString) (inst *InEntityTesttableSectionStringInAttr) {
	panic("stub")
}

func (inst *InEntityTesttableSectionStringInAttr) AddMembershipMixedLowCardVerbatim(lmv7 []byte, mvhp8 []byte) *InEntityTesttableSectionStringInAttr {
	panic("stub")
}

func (inst *InEntityTesttableSectionStringInAttr) AddMembershipMixedLowCardVerbatimP(lmv7 []byte, mvhp8 []byte) {
	panic("stub")
}

func (inst *InEntityTesttableSectionStringInAttr) EndSection() *InEntityTesttable { panic("stub") }

func (inst *InEntityTesttableSectionStringInAttr) EndAttribute() *InEntityTesttableSectionString {
	panic("stub")
}

func (inst *InEntityTesttableSectionStringInAttr) AppendError(err error) { panic("stub") }


```

--- FILE: dml/lw_dml_arrow_utils.go ---
```go
package dml

import (
	"github.com/apache/arrow-go/v18/arrow"
	_ "github.com/apache/arrow-go/v18/arrow"
	_ "github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/parquet/pqarrow"
	_ "github.com/apache/arrow-go/v18/parquet/pqarrow"
	_ "github.com/rs/zerolog/log"
	_ "github.com/stergiotis/boxer/public/observability/eh"
)

func WriteArrowRecords[E TransferRecordsI](ent E, records []arrow.Record, w *ipc.FileWriter, w2 *pqarrow.FileWriter) (recordsOut []arrow.Record, err error) {
	panic("stub")
}


```

--- FILE: dml/lw_dml_generator.go ---
```go
package dml

import (
	_ "fmt"
	_ "strconv"
	"strings"
	_ "strings"

	_ "github.com/ettle/strcase"
	_ "github.com/stergiotis/boxer/public/containers"
	"github.com/stergiotis/boxer/public/observability/eh"
	_ "github.com/stergiotis/boxer/public/observability/eh"
	_ "github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/observability/vcs"
	_ "github.com/stergiotis/boxer/public/observability/vcs"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/codegen"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/common"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/ddl/golang"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/dml/runtime"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/gocodegen"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/common"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/gocodegen"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/naming"
)

var CodeGeneratorName = "Leeway DML (" + vcs.ModuleInfo() + ")"

func NewGoClassBuilder() *GoClassBuilder { panic("stub") }

func (inst *GoClassBuilder) SetCodeBuilder(s *strings.Builder) { panic("stub") }

func (inst *GoClassBuilder) GetCode() (code string, err error) { panic("stub") }

func (inst *GoClassBuilder) ResetCodeBuilder() { panic("stub") }

func (inst *GoClassBuilder) ComposeNamingConventionDependentCode(tableName naming.StylableName, ir *common.IntermediateTableRepresentation, namingConvention common.NamingConventionI, tableRowConfig common.TableRowConfigE, clsNamer gocodegen.GoClassNamerI) (err error) {
	panic("stub")
}

var ErrUnhandledSubType = eh.Errorf("unhandled sub type")
var ErrUnhandledRole = eh.Errorf("unhandled column role")

// FIXME implement cast to uint64

func (inst *GoClassBuilder) ComposeAttributeClassAndFactoryCode(clsNamer gocodegen.GoClassNamerI, tableName naming.StylableName, sectionName naming.StylableName, sectionIdx int, totalSections int, sectionIRH *common.IntermediatePairHolder, tableRowConfig common.TableRowConfigE) (err error) {
	panic("stub")
}

func (inst *GoClassBuilder) ComposeSectionClassAndFactoryCode(clsNamer gocodegen.GoClassNamerI, tableName naming.StylableName, sectionName naming.StylableName, sectionIdx int, totalSections int, sectionIRH *common.IntermediatePairHolder, tableRowConfig common.TableRowConfigE) (err error) {
	panic("stub")
}

func GetMembershipAddFunctionName(role common.ColumnRoleE) (funcName string, err error) {
	panic("stub")
}

// mixed, trigger on other

func (inst *GoClassBuilder) ComposeAttributeCode(clsNamer gocodegen.GoClassNamerI, tableName naming.StylableName, sectionName naming.StylableName, sectionIdx int, totalSections int, sectionIRH *common.IntermediatePairHolder, tableRowConfig common.TableRowConfigE) (err error) {
	panic("stub")
}

// beginAttribute

// FIXME tableRowConfig

// AddToContainer/AddToCoContainers

// membership

// handleMembershipSupportColumns

// handleNonScalarSupportColumns

// completeAttribute

// EndSection

// EndAttribute

func (inst *GoClassBuilder) ComposeSectionCode(clsNamer gocodegen.GoClassNamerI, tableName naming.StylableName, sectionName naming.StylableName, sectionIdx int, totalSections int, sectionIRH *common.IntermediatePairHolder, tableRowConfig common.TableRowConfigE) (err error) {
	panic("stub")
}

// endAttribute

// BeginAttribute

// CheckErrors

// EndSection

// beginSection

// resetSection

func (inst *GoClassBuilder) ComposeEntityClassAndFactoryCode(clsNamer gocodegen.GoClassNamerI, tableName naming.StylableName, sectionNames []naming.StylableName, ir *common.IntermediateTableRepresentation, tableRowConfig common.TableRowConfigE, entityIRH *common.IntermediatePairHolder) (err error) {
	panic("stub")
}

func (inst *GoClassBuilder) ComposeEntityCode(clsNamer gocodegen.GoClassNamerI, tableName naming.StylableName, sectionNames []naming.StylableName, ir *common.IntermediateTableRepresentation, tableRowConfig common.TableRowConfigE, entityIRH *common.IntermediatePairHolder) (err error) {
	panic("stub")
}

//taggedIRH := entityIRH.DeriveSubHolder(deriveSubHolderSelectTaggedValues)

// setter

// appendPlainValues

// resetPlainValues

// reset sections

// beginSections

// resetSections

// CheckErrors

// section getter

// BeginEntity

// validateEntity

// CommitEntity

// RollbackEntity

// TransferRecords

// GetSchema

func (inst *GoClassBuilder) ComposeGoImports(ir *common.IntermediateTableRepresentation, tableRowConfig common.TableRowConfigE) (err error) {
	panic("stub")
}

func (inst *GoClassBuilder) PrepareCodeComposition() { panic("stub") }


```

--- FILE: dml/lw_dml_generator_hl.go ---
```go
package dml

import (
	_ "go/format"
	_ "strings"

	_ "github.com/stergiotis/boxer/public/code/synthesis/golang"
	_ "github.com/stergiotis/boxer/public/observability/eh"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/common"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/gocodegen"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	_ "github.com/stergiotis/boxer/public/unsafeperf"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/common"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/gocodegen"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/naming"
)

type GeneratorDriver struct {
}

func NewGoCodeGeneratorDriver(namingConvention common.NamingConventionI, tech common.TechnologySpecificGeneratorI) *GeneratorDriver {
	panic("stub")
}

func (inst *GeneratorDriver) GenerateGoClasses(packageName string, tableName naming.StylableName, tblDesc common.TableDesc, tableRowConfig common.TableRowConfigE, clsNamer gocodegen.GoClassNamerI) (sourceCode []byte, wellFormed bool, err error) {
	panic("stub")
}

// try formatting source code


```

--- FILE: dml/lw_dml_testutils.go ---
```go
package dml

import (
	_ "regexp"
	_ "slices"
	_ "strings"
	_ "testing"

	_ "github.com/stergiotis/boxer/public/unsafeperf"
	_ "github.com/stretchr/testify/require"
)


```

--- FILE: dml/lw_dml_types.go ---
```go
package dml

import (
	_ "strings"

	"github.com/apache/arrow-go/v18/arrow"
	_ "github.com/apache/arrow-go/v18/arrow"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/common"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/ddl/golang"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/common"
)

type TechnologySpecificBuilderI interface {
	common.CodeBuilderHolderI
}

type TransferRecordsI interface {
	TransferRecords(recordsIn []arrow.Record) (recordsOut []arrow.Record, err error)
}
type GoClassBuilder struct {
}


```

--- FILE: dml/runtime/lw_dml_rt_enum.go ---
```go
package runtime

const (
	EntityStateInitial     EntityStateE = 0
	EntityStateInEntity    EntityStateE = 1
	EntityStateInSection   EntityStateE = 2
	EntityStateInAttribute EntityStateE = 3
)
const InvalidEnumString = "<invalid>"

var EntityStateVariableNames = [4]string{
	"EntityStateInitial",
	"EntityStateInEntity",
	"EntityStateInSection",
	"EntityStateInAttribute",
}

func (inst EntityStateE) String() string { panic("stub") }


```

--- FILE: dml/runtime/lw_dml_runtime.go ---
```go
package runtime

import (
	"github.com/stergiotis/boxer/public/observability/eh"
	_ "github.com/stergiotis/boxer/public/observability/eh"
)

var ErrInvalidStateTransition = eh.Errorf("invalid state transition")


```

--- FILE: dml/runtime/lw_dml_types.go ---
```go
package runtime

import (
	_ "fmt"

	"github.com/apache/arrow-go/v18/arrow"
	_ "github.com/apache/arrow-go/v18/arrow"
)

type EntityStateE uint8

type InAttributeMembershipHighCardRefPI interface {
	AddMembershipHighCardRefP(highCardRef uint64)
}
type InAttributeMembershipHighCardRefParametrizedPI interface {
	AddMembershipHighCardRefParametrizedP(highCardRefParametrized []byte)
}
type InAttributeMembershipHighCardVerbatimPI interface {
	AddMembershipHighCardVerbatimP(highCardVerbatim []byte)
}
type InAttributeMembershipLowCardRefPI interface {
	AddMembershipLowCardRefP(lowCardRef uint64)
}
type InAttributeMembershipLowCardRefParametrizedPI interface {
	AddMembershipLowCardRefParametrizedP(lowCardRefParametrized []byte)
}
type InAttributeMembershipLowCardVerbatimPI interface {
	AddMembershipLowCardVerbatimP(lowCardVerbatim []byte)
}
type InAttributeMembershipMixedLowCardRefPI interface {
	AddMembershipMixedLowCardRefP(lowCardRef uint64, params []byte)
}
type InAttributeMembershipMixedLowCardVerbatimPI interface {
	AddMembershipMixedLowCardVerbatimP(lowCardVerbatim uint64, params []byte)
}

type InAttributeMembershipHighCardRefI[A any] interface {
	AddMembershipHighCardRef(highCardRef uint64) A
}
type InAttributeMembershipHighCardRefParametrizedI[A any] interface {
	AddMembershipHighCardRefParametrized(highCardRefParametrized []byte) A
}
type InAttributeMembershipHighCardVerbatimI[A any] interface {
	AddMembershipHighCardRef(highCardVerbatim []byte) A
}
type InAttributeMembershipLowCardRefI[A any] interface {
	AddMembershipLowCardRef(lowCardRef uint64) A
}
type InAttributeMembershipLowCardRefParametrizedI[A any] interface {
	AddMembershipLowCardRefParametrized(lowCardRefParametrized []byte) A
}
type InAttributeMembershipLowCardVerbatimI[A any] interface {
	AddMembershipLowCardVerbatim(lowCardVerbatim []byte) A
}
type InAttributeMembershipMixedLowCardRefI[A any] interface {
	AddMembershipMixedLowCardRef(lowCardRef uint64, params []byte) A
}
type InAttributeMembershipMixedLowCardVerbatimI[A any] interface {
	AddMembershipMixedLowCardVerbatim(lowCardVerbatim uint64, params []byte) A
}
type ErrorAppenderI interface {
	AppendError(err error)
}
type ErrorCheckerI interface {
	CheckErrors() (err error)
}
type ErrorHandlingI interface {
	ErrorAppenderI
	ErrorCheckerI
}

type InAttributeI[E any, S any, A any] interface {
	EndAttribute() S
	EndSection() E
}
type InSectionI[E any, S any] interface {
	ErrorHandlingI

	EndSection() E
}
type InEntity[E any] interface {
	ErrorHandlingI

	CommitEntity() error
	RollbackEntity() error

	TransferRecords(recordsIn []arrow.Record) (recordsOut []arrow.Record, err error)
	GetSchema() (schema *arrow.Schema)
}


```

--- FILE: encodingaspects/lw_encodinghints_encoder.go ---
```go
// Code generated by copy paste; DO NOT EDIT.
package encodingaspects

import (
	"iter"
	_ "iter"
	_ "math/bits"

	_ "github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh"
	_ "github.com/stergiotis/boxer/public/observability/eh"
	_ "github.com/stergiotis/boxer/public/observability/eh/eb"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/base62"
)

const EmptyAspectSet = AspectSet("0")

var ErrInvalidEncoding = eh.Errorf("encoding is wrong")
var ErrEmptySet = eh.Errorf("encoding contains empty set")

func EncodeAspects(aspects ...AspectE) (encoded AspectSet, err error) { panic("stub") }

func EncodeAspectsIgnoreInvalid(aspects ...AspectE) (encoded AspectSet) { panic("stub") }

func EncodeAspectsMustValidate(aspects ...AspectE) (encoded AspectSet) { panic("stub") }

func MaxEncodedAspect(encoded AspectSet) (aspect AspectE, err error) { panic("stub") }

func CountEncodedAspects(encoded AspectSet) (n int, err error) { panic("stub") }

func IterateAspects(encoded AspectSet) iter.Seq2[int, AspectE] { panic("stub") }

func UnionAspects(asp1 AspectSet, asp2 AspectSet) (res AspectSet, err error) { panic("stub") }

func UnionAspectsIgnoreInvalid(asp1 AspectSet, asp2 AspectSet) (res AspectSet) { panic("stub") }

func (inst AspectSet) String() string { panic("stub") }

func (inst AspectSet) IsValid() bool { panic("stub") }

func (inst AspectSet) IsEmptySet() bool { panic("stub") }

func (inst AspectSet) UnionAspectsIgnoreInvalid(asp2 AspectSet) (res AspectSet) { panic("stub") }

func (inst AspectSet) UnionAspects(asp2 AspectSet) (res AspectSet, err error) { panic("stub") }

func (inst AspectSet) IterateAspects() iter.Seq2[int, AspectE] { panic("stub") }

func (inst AspectSet) CountEncodedAspects() (n int, err error) { panic("stub") }

func (inst AspectSet) MaxEncodedAspect() (aspect AspectE, err error) { panic("stub") }


```

--- FILE: encodingaspects/lw_encodinghints_enum.go ---
```go
package encodingaspects

import (
	"slices"
	_ "slices"
)

const (
	AspectNone                          AspectE = 0
	AspectIntraRecordLowCardinality     AspectE = 1
	AspectInterRecordLowCardinality     AspectE = 2
	AspectUltraLightGeneralCompression  AspectE = 3
	AspectLightGeneralCompression       AspectE = 4
	AspectHeavyGeneralCompression       AspectE = 5
	AspectUltraHeavyGeneralCompression  AspectE = 6
	AspectDeltaEncoding                 AspectE = 7
	AspectDoubleDeltaEncoding           AspectE = 8
	AspectUltraLightSlowlyChangingFloat AspectE = 9
	AspectLightSlowlyChangingFloat      AspectE = 10
	AspectHeavySlowlyChangingFloat      AspectE = 11
	AspectUltraHeavySlowlyChangingFloat AspectE = 12
	AspectLightBiasSmallInteger         AspectE = 13
	AspectHeavyBiasSmallInteger         AspectE = 14
	AspectSparse                        AspectE = 15

	AspectJsonScalar AspectE = 16
	AspectJsonArray  AspectE = 17
	AspectJsonObject AspectE = 18
	AspectJson       AspectE = 19
	AspectCborScalar AspectE = 20
	AspectCborArray  AspectE = 21
	AspectCborMap    AspectE = 22
	AspectCbor       AspectE = 23
)

var MaxAspectExcl = slices.Max(AllAspects) + 1

var AllAspects = []AspectE{
	AspectNone,
	AspectIntraRecordLowCardinality,
	AspectInterRecordLowCardinality,
	AspectUltraLightGeneralCompression,
	AspectLightGeneralCompression,
	AspectHeavyGeneralCompression,
	AspectUltraHeavyGeneralCompression,
	AspectDeltaEncoding,
	AspectDoubleDeltaEncoding,
	AspectUltraLightSlowlyChangingFloat,
	AspectLightSlowlyChangingFloat,
	AspectHeavySlowlyChangingFloat,
	AspectUltraHeavySlowlyChangingFloat,
	AspectLightBiasSmallInteger,
	AspectHeavyBiasSmallInteger,
	AspectSparse,
	AspectJsonScalar,
	AspectJsonArray,
	AspectJsonObject,
	AspectJson,
	AspectCborScalar,
	AspectCborArray,
	AspectCborMap,
	AspectCbor,
}

const InvalidAspectEnumValueString = "<invalid AspectE>"

func (inst AspectE) IsValid() bool { panic("stub") }

func (inst AspectE) Value() uint8 { panic("stub") }

func (inst AspectE) String() string { panic("stub") }


```

--- FILE: encodingaspects/lw_encodinghints_types.go ---
```go
package encodingaspects

import _ "fmt"

type AspectSet string

type AspectE uint8


```

--- FILE: gocodegen/gocodegen_arrow.go ---
```go
package gocodegen

import (
	_ "fmt"

	_ "github.com/stergiotis/boxer/public/observability/eh"
	_ "github.com/stergiotis/boxer/public/observability/eh/eb"
	canonicaltypes2 "github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/common"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/encodingaspects"
)

func ArrowTypeToGoType(ct canonicaltypes2.PrimitiveAstNodeI, hints encodingaspects.AspectSet, useDictionaryEncoding bool) (prefix string, suffix string, err error) {
	panic("stub")
}

func GoTypeToArrowType(ct canonicaltypes2.PrimitiveAstNodeI, hints encodingaspects.AspectSet, useDictionaryEncoding bool) (prefix string, suffix string, err error) {
	panic("stub")
}

func CanonicalTypeToArrowBaseClassName(ct canonicaltypes2.PrimitiveAstNodeI, encodingHints encodingaspects.AspectSet, useDictionaryEncoding bool) (name string, mayError bool, err error) {
	panic("stub")
}


```

--- FILE: gocodegen/gocodegen_common.go ---
```go
package gocodegen

import (
	_ "fmt"
	_ "slices"
	"strings"
	_ "strings"

	_ "github.com/stergiotis/boxer/public/observability/eh/eb"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/common"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/ddl/arrow"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/common"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/naming"
)

func GenerateArrowSchemaFactory(b *strings.Builder, tableName naming.StylableName, ir *common.IntermediateTableRepresentation, namingConvention common.NamingConventionI, tableRowConfig common.TableRowConfigE, clsNamer GoClassNamerI) (err error) {
	panic("stub")
}

// schema factory

func ComposeCode(impl CodeComposerI, b *strings.Builder, tableName naming.StylableName, ir *common.IntermediateTableRepresentation, conv common.NamingConventionI, tableRowConfig common.TableRowConfigE, clsNamer GoClassNamerI) (err error) {
	panic("stub")
}


```

--- FILE: gocodegen/gocodegen_namer.go ---
```go
package gocodegen

import (
	"github.com/stergiotis/boxer/public/functional"
	_ "github.com/stergiotis/boxer/public/functional"
	_ "github.com/stergiotis/boxer/public/observability/eh"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/naming"
)

func NewDefaultGoClassNamer() *DefaultGoClassNamer { panic("stub") }

func (inst *DefaultGoClassNamer) PromiseToBeReferentialTransparent() (_ functional.InterfaceIsReferentialTransparentType) {
	panic("stub")
}

func NewMultiTablePerPackageGoClassNamer() *MultiTablePerPackageClassNamer { panic("stub") }

func NewClassNamesEntityOnly(classNamer GoClassNamerI, tableName naming.StylableName) (clsNames ClassNames, err error) {
	panic("stub")
}

func NewClassNames(classNamer GoClassNamerI, tableName naming.StylableName, sectionName naming.StylableName, sectionIndex int, totalSections int) (clsNames ClassNames, err error) {
	panic("stub")
}


```

--- FILE: gocodegen/gocodegen_namer_dml.go ---
```go
package gocodegen

import (
	"github.com/stergiotis/boxer/public/functional"
	_ "github.com/stergiotis/boxer/public/functional"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/naming"
)

func (inst *DefaultGoClassNamer) ComposeSchemaFactoryName(tableName naming.StylableName) (functionName string, err error) {
	panic("stub")
}

func (inst *DefaultGoClassNamer) ComposeEntityDmlClassName(tableName naming.StylableName) (fullClassName string, err error) {
	panic("stub")
}

func (inst *DefaultGoClassNamer) ComposeSectionDmlClassName(tableName naming.StylableName, sectionName naming.StylableName, sectionIndex int, sectionCount int) (fullClassName string, err error) {
	panic("stub")
}

func (inst *DefaultGoClassNamer) ComposeAttributeDmlClassName(tableName naming.StylableName, sectionName naming.StylableName, sectionIndex int, sectionCount int) (fullClassName string, err error) {
	panic("stub")
}

func (inst *MultiTablePerPackageClassNamer) ComposeSchemaFactoryName(tableName naming.StylableName) (functionName string, err error) {
	panic("stub")
}

func (inst *MultiTablePerPackageClassNamer) ComposeEntityDmlClassName(tableName naming.StylableName) (fullClassName string, err error) {
	panic("stub")
}

func (inst *MultiTablePerPackageClassNamer) ComposeSectionDmlClassName(tableName naming.StylableName, sectionName naming.StylableName, sectionIndex int, sectionCount int) (fullClassName string, err error) {
	panic("stub")
}

func (inst *MultiTablePerPackageClassNamer) ComposeAttributeDmlClassName(tableName naming.StylableName, sectionName naming.StylableName, sectionIndex int, sectionCount int) (fullClassName string, err error) {
	panic("stub")
}

func (inst *MultiTablePerPackageClassNamer) PromiseToBeReferentialTransparent() (_ functional.InterfaceIsReferentialTransparentType) {
	panic("stub")
}


```

--- FILE: gocodegen/gocodegen_namer_ra.go ---
```go
package gocodegen

import (
	_ "fmt"

	_ "github.com/stergiotis/boxer/public/observability/eh"
	_ "github.com/stergiotis/boxer/public/observability/eh/eb"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/common"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/common"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/naming"
	_ "golang.org/x/exp/constraints"
)

func (inst *DefaultGoClassNamer) ComposeEntityReadAccessClassName(tableName naming.StylableName) (className string, err error) {
	panic("stub")
}

func (inst *DefaultGoClassNamer) ComposeSectionReadAccessAttributeClassName(tableName naming.StylableName, itemType common.PlainItemTypeE, sectionName naming.StylableName) (className string, err error) {
	panic("stub")
}

func (inst *DefaultGoClassNamer) ComposeSectionReadAccessOuterClassName(tableName naming.StylableName, itemType common.PlainItemTypeE, sectionName naming.StylableName) (className string, err error) {
	panic("stub")
}

func (inst *DefaultGoClassNamer) ComposeSectionMembershipPackClassName(tableName naming.StylableName, sectionName naming.StylableName) (className string, err error) {
	panic("stub")
}

func (inst *DefaultGoClassNamer) ComposeSharedMembershipPackClassName(tableName naming.StylableName, membershipSpec common.MembershipSpecE, i int, total int) (className string, err error) {
	panic("stub")
}

func (inst *DefaultGoClassNamer) ComposeValueField(fieldNameIn string) (fieldNameOut string) {
	panic("stub")
}

func (inst *DefaultGoClassNamer) ComposeValueFieldElementAccessor(fieldNameIn string) (fieldNameOut string) {
	panic("stub")
}

func (inst *DefaultGoClassNamer) ComposeColumnIndexFieldName(fieldNameIn string) (fieldNameOut string) {
	panic("stub")
}

func (inst *DefaultGoClassNamer) ComposeAccelFieldName(fieldNameIn string) (fieldNameOut string) {
	panic("stub")
}

func (inst *MultiTablePerPackageClassNamer) ComposeSectionReadAccessAttributeClassName(tableName naming.StylableName, itemType common.PlainItemTypeE, sectionName naming.StylableName) (className string, err error) {
	panic("stub")
}

//className += naming.MustBeValidStylableName(subType.String()).Convert(naming.UpperCamelCase).String()

func (inst *MultiTablePerPackageClassNamer) ComposeSectionReadAccessOuterClassName(tableName naming.StylableName, itemType common.PlainItemTypeE, sectionName naming.StylableName) (className string, err error) {
	panic("stub")
}

func (inst *MultiTablePerPackageClassNamer) ComposeEntityReadAccessClassName(tableName naming.StylableName) (className string, err error) {
	panic("stub")
}

func (inst *MultiTablePerPackageClassNamer) ComposeSectionMembershipPackClassName(tableName naming.StylableName, sectionName naming.StylableName) (className string, err error) {
	panic("stub")
}

func (inst *MultiTablePerPackageClassNamer) ComposeSharedMembershipPackClassName(tableName naming.StylableName, membershipSpec common.MembershipSpecE, i int, total int) (className string, err error) {
	panic("stub")
}

func (inst *MultiTablePerPackageClassNamer) ComposeValueField(fieldNameIn string) (fieldNameOut string) {
	panic("stub")
}

func (inst *MultiTablePerPackageClassNamer) ComposeValueFieldElementAccessor(fieldNameIn string) (fieldNameOut string) {
	panic("stub")
}

func (inst *MultiTablePerPackageClassNamer) ComposeColumnIndexFieldName(fieldNameIn string) (fieldNameOut string) {
	panic("stub")
}

func (inst *MultiTablePerPackageClassNamer) ComposeAccelFieldName(fieldNameIn string) (fieldNameOut string) {
	panic("stub")
}


```

--- FILE: gocodegen/gocodegen_types.go ---
```go
package gocodegen

import (
	"github.com/stergiotis/boxer/public/functional"
	_ "github.com/stergiotis/boxer/public/functional"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/common"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/common"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/naming"
)

type GoClassNamerReadAccessI interface {
	ComposeEntityReadAccessClassName(tableName naming.StylableName) (className string, err error)
	ComposeSectionReadAccessOuterClassName(tableName naming.StylableName, itemType common.PlainItemTypeE, sectionName naming.StylableName) (className string, err error)
	ComposeSectionReadAccessAttributeClassName(tableName naming.StylableName, itemType common.PlainItemTypeE, sectionName naming.StylableName) (className string, err error)
	ComposeSectionMembershipPackClassName(tableName naming.StylableName, sectionName naming.StylableName) (className string, err error)
	ComposeSharedMembershipPackClassName(tableName naming.StylableName, membershipSpec common.MembershipSpecE, i int, total int) (className string, err error)

	ComposeValueField(fieldNameIn string) (fieldNameOut string)
	ComposeValueFieldElementAccessor(fieldNameIn string) (fieldNameOut string)
	ComposeColumnIndexFieldName(fieldNameIn string) (fieldNameOut string)
	ComposeAccelFieldName(fieldNameIn string) (fieldNameOut string)
}
type GoClassNamerDmlI interface {
	ComposeSchemaFactoryName(tableName naming.StylableName) (functionName string, err error)
	ComposeEntityDmlClassName(tableName naming.StylableName) (fullClassName string, err error)
	ComposeSectionDmlClassName(tableName naming.StylableName, sectionName naming.StylableName, sectionIndex int, sectionCount int) (fullClassName string, err error)
	ComposeAttributeDmlClassName(tableName naming.StylableName, sectionName naming.StylableName, sectionIndex int, sectionCount int) (fullClassName string, err error)
}

type GoClassNamerI interface {
	GoClassNamerReadAccessI
	GoClassNamerDmlI
	functional.PromiseReferentialTransparentI
}

type DefaultGoClassNamer struct {
}

type MultiTablePerPackageClassNamer struct {
}

type ClassNames struct {
	ReadAccessEntityClassName string
	InEntityClassName         string
	InSectionClassName        string
	InAttributeClassName      string
}

type CodeComposerI interface {
	PrepareCodeComposition()
	ComposeNamingConventionDependentCode(tableName naming.StylableName, ir *common.IntermediateTableRepresentation, namingConvention common.NamingConventionI, tableRowConfig common.TableRowConfigE, clsNamer GoClassNamerI) (err error)
	ComposeEntityClassAndFactoryCode(clsNamer GoClassNamerI, tableName naming.StylableName,
		sectionNames []naming.StylableName, ir *common.IntermediateTableRepresentation, tableRowConfig common.TableRowConfigE, entityIRH *common.IntermediatePairHolder) (err error)
	ComposeEntityCode(clsNamer GoClassNamerI, tableName naming.StylableName,
		sectionNames []naming.StylableName, ir *common.IntermediateTableRepresentation, tableRowConfig common.TableRowConfigE, entityIRH *common.IntermediatePairHolder) (err error)
	ComposeSectionClassAndFactoryCode(
		clsNamer GoClassNamerI, tableName naming.StylableName, sectionName naming.StylableName, sectionIdx int, totalSections int,
		sectionIRH *common.IntermediatePairHolder, tableRowConfig common.TableRowConfigE) (err error)
	ComposeSectionCode(
		clsNamer GoClassNamerI, tableName naming.StylableName, sectionName naming.StylableName, sectionIdx int, totalSections int,
		sectionIRH *common.IntermediatePairHolder, tableRowConfig common.TableRowConfigE) (err error)
	ComposeAttributeClassAndFactoryCode(
		clsNamer GoClassNamerI, tableName naming.StylableName, sectionName naming.StylableName, sectionIdx int, totalSections int,
		sectionIRH *common.IntermediatePairHolder, tableRowConfig common.TableRowConfigE) (err error)
	ComposeAttributeCode(
		clsNamer GoClassNamerI, tableName naming.StylableName, sectionName naming.StylableName, sectionIdx int, totalSections int,
		sectionIRH *common.IntermediatePairHolder, tableRowConfig common.TableRowConfigE) (err error)
}


```

--- FILE: gocodegen/gocodegen_utils.go ---
```go
package gocodegen

import (
	_ "fmt"
	"io"
	_ "io"
	_ "path/filepath"
	_ "runtime"
	_ "strings"
)

func PrettyCaller(skip int, rootFolder string, defaultFuncName string) (funcName string, filePath string, lineNumber int) {
	panic("stub")
}

func EmitGeneratingCodeLocation(w io.Writer) { panic("stub") }


```

--- FILE: mapping/lw_ddl_mapping_json.go ---
```go
package mapping

import (
	_ "github.com/stergiotis/boxer/public/observability/eh"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/common"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/common"
)

func LoadJsonMapping(manip common.TableManipulatorFluidI) { panic("stub") }

func LoadJsonMappingLossless(manip common.TableManipulatorFluidI) { panic("stub") }

func NewJsonMapping() (tbl common.TableDesc, err error) { panic("stub") }


```

--- FILE: naming/lw_naming_enums.go ---
```go
package naming

const InvalidEnumValueString = "<invalid>"
const (
	LowerCamelCase  NamingStyleE = 0
	UpperCamelCase  NamingStyleE = 1
	LowerSnakeCase  NamingStyleE = 2
	UpperSnakeCase  NamingStyleE = 3
	LowerSpinalCase NamingStyleE = 4
	UpperSpinalCase NamingStyleE = 5
)

var AllNamingStyles = []NamingStyleE{
	LowerCamelCase,
	UpperCamelCase,
	LowerSnakeCase,
	UpperSnakeCase,
	LowerSpinalCase,
	UpperSpinalCase,
}

func (inst NamingStyleE) String() string { panic("stub") }


```

--- FILE: naming/lw_naming_style.go ---
```go
package naming

import (
	_ "bytes"
	_ "errors"
	"iter"
	_ "iter"
	_ "strings"
	"unicode"
	_ "unicode"

	_ "github.com/ettle/strcase"
	_ "github.com/go-json-experiment/json"
	_ "github.com/rs/zerolog/log"
	_ "github.com/stergiotis/boxer/public/observability/eh"
	_ "github.com/stergiotis/boxer/public/observability/eh/eb"
)

const SnakeCaseSeparator = '_'
const SpinalCaseSeparator = '-'
const InvalidComponentRune = unicode.ReplacementChar
const (
	ShortestNamingStyle     = LowerCamelCase
	BestReadableNamingStyle = LowerSpinalCase
	DefaultNamingStyle      = BestReadableNamingStyle
)

func ConvertNameStyle[S ~string](name S, targetStyle NamingStyleE) (naming S) { panic("stub") }

func MustBeValidKey[S ~string](key S) (r Key) { panic("stub") }

func MustBeValidStylableName[S ~string](name S) (r StylableName) { panic("stub") }

func MakeKey[S ~string](key S) (r Key, err error) { panic("stub") }

func MakeStylableName[S ~string](name S) (r StylableName, err error) { panic("stub") }

func Compare[S ~string](a, b S) int { panic("stub") }

// fast path

func (inst StylableName) Compare(other StylableName) int { panic("stub") }

func ValidateStylableName[S ~string](name S) (err error) { panic("stub") }

func ValidateNameComponent[S ~string](component S) (err error) { panic("stub") }

func JoinComponents[S ~string](components ...S) (name StylableName, err error) { panic("stub") }

func (inst StylableName) IterateComponents() iter.Seq[StylableName] { panic("stub") }

func (inst StylableName) Validate() (err error) { panic("stub") }

func (inst StylableName) IsEmpty() (empty bool) { panic("stub") }

func (inst StylableName) IsValid() (valid bool) { panic("stub") }

func (inst StylableName) Convert(targetStyle NamingStyleE) StylableName { panic("stub") }

func (inst StylableName) IsUsingStyle(style NamingStyleE) bool { panic("stub") }

func (inst StylableName) String() string {
	panic(
		// NOTE: does _not_ enforce a style
		"stub")
}

func (inst StylableName) Bytes() []byte { panic("stub") }

func (inst Key) IsEmpty() (empty bool) { panic("stub") }

func (inst Key) String() string { panic("stub") }

func (inst Key) IsValid() (valid bool) { panic("stub") }

func (inst Key) Validate() (err error) { panic("stub") }

func ValidateKey[S ~string](s S) (err error) { panic("stub") }


```

--- FILE: naming/lw_naming_types.go ---
```go
package naming

import _ "fmt"

// StylableName a name that can be transformed to other naming styles without loosing is descriptive, referencing and uniqueness properties
type StylableName string

type Key string

type NamingStyleE uint8


```

--- FILE: readaccess/example/readaccess_testtable_dml.out.go ---
```go
// Code generated; Leeway DML (github.com/stergiotis/boxer/public/semistructured/leeway/readaccess.test) DO NOT EDIT.

package example

import (
	_ "errors"
	_ "slices"
	"time"
	_ "time"

	"github.com/apache/arrow-go/v18/arrow"
	_ "github.com/apache/arrow-go/v18/arrow"
	_ "github.com/apache/arrow-go/v18/arrow/array"
	_ "github.com/apache/arrow-go/v18/arrow/ipc"
	_ "github.com/apache/arrow-go/v18/arrow/math"
	_ "github.com/apache/arrow-go/v18/arrow/memory"
	_ "github.com/stergiotis/boxer/public/observability/eh"
	_ "github.com/stergiotis/boxer/public/observability/eh/eb"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/dml/runtime"
	"modernc.org/memory"
)

///////////////////////////////////////////////////////////////////
// code generator
// gocodegen.GenerateArrowSchemaFactory
// ./public/semistructured/leeway/gocodegen/gocodegen_common.go:26

func CreateSchemaTestTable() (schema *arrow.Schema) { panic("stub") }

/* 000 */
/* 001 */
/* 002 */
/* 003 */
/* 004 */
/* 005 */
/* 006 */
/* 007 */
/* 008 */
/* 009 */
/* 010 */
/* 011 */
/* 012 */
/* 013 */
/* 014 */
/* 015 */
/* 016 */
/* 017 */
/* 018 */
/* 019 */
/* 020 */

///////////////////////////////////////////////////////////////////
// code generator
// dml.(*GoClassBuilder).ComposeEntityClassAndFactoryCode
// ./public/semistructured/leeway/dml/lw_dml_generator.go:1175

type InEntityTestTable struct {
}

func NewInEntityTestTable(allocator memory.Allocator, estimatedNumberOfRecords int) (inst *InEntityTestTable) {
	panic("stub")
}

///////////////////////////////////////////////////////////////////
// code generator
// dml.(*GoClassBuilder).ComposeEntityCode
// ./public/semistructured/leeway/dml/lw_dml_generator.go:1289

func (inst *InEntityTestTable) SetId(id0 uint64) *InEntityTestTable { panic("stub") }

///////////////////////////////////////////////////////////////////
// code generator
// dml.(*GoClassBuilder).ComposeEntityCode
// ./public/semistructured/leeway/dml/lw_dml_generator.go:1289

func (inst *InEntityTestTable) SetTimestamp(ts1 time.Time, proc2 []time.Time) *InEntityTestTable {
	panic("stub")
}

func (inst *InEntityTestTable) CheckErrors() (err error) { panic("stub") }

func (inst *InEntityTestTable) GetSectionGeo() *InEntityTestTableSectionGeo { panic("stub") }

func (inst *InEntityTestTable) GetSectionText() *InEntityTestTableSectionText { panic("stub") }

func (inst *InEntityTestTable) BeginEntity() *InEntityTestTable { panic("stub") }

// FIXME check coSectionGroup consistency

func (inst *InEntityTestTable) CommitEntity() (err error) { panic("stub") }

func (inst *InEntityTestTable) RollbackEntity() (err error) { panic("stub") }

// arrow fields must all have one row

// FIXME find better way to truncate builder

// TransferRecords The returned Records must be Release()'d after use.
func (inst *InEntityTestTable) TransferRecords(recordsIn []arrow.Record) (recordsOut []arrow.Record, err error) {
	panic("stub")
}

func (inst *InEntityTestTable) GetSchema() (schema *arrow.Schema) { panic("stub") }

func (inst *InEntityTestTable) AppendError(err error) { panic("stub") }

type InEntityTestTableSectionGeo struct {
}

func NewInEntityTestTableSectionGeo(builder *array.RecordBuilder, parent *InEntityTestTable) (inst *InEntityTestTableSectionGeo) {
	panic("stub")
}

func (inst *InEntityTestTableSectionGeo) BeginAttribute(lat3 float32, lng4 float32, h3Res15 uint64, h3Res26 uint64) *InEntityTestTableSectionGeoInAttr {
	panic("stub")
}

func (inst *InEntityTestTableSectionGeo) CheckErrors() (err error) { panic("stub") }

func (inst *InEntityTestTableSectionGeo) EndSection() *InEntityTestTable { panic("stub") }

func (inst *InEntityTestTableSectionGeo) AppendError(err error) { panic("stub") }

type InEntityTestTableSectionGeoInAttr struct {
}

func NewInEntityTestTableSectionGeoInAttr(builder *array.RecordBuilder, parent *InEntityTestTableSectionGeo) (inst *InEntityTestTableSectionGeoInAttr) {
	panic("stub")
}

func (inst *InEntityTestTableSectionGeoInAttr) AddMembershipLowCardRef(lr7 uint64) *InEntityTestTableSectionGeoInAttr {
	panic("stub")
}

func (inst *InEntityTestTableSectionGeoInAttr) AddMembershipLowCardRefP(lr7 uint64) { panic("stub") }

func (inst *InEntityTestTableSectionGeoInAttr) AddMembershipMixedLowCardVerbatim(lmv8 []byte, mvhp9 []byte) *InEntityTestTableSectionGeoInAttr {
	panic("stub")
}

func (inst *InEntityTestTableSectionGeoInAttr) AddMembershipMixedLowCardVerbatimP(lmv8 []byte, mvhp9 []byte) {
	panic("stub")
}

func (inst *InEntityTestTableSectionGeoInAttr) EndSection() *InEntityTestTable { panic("stub") }

func (inst *InEntityTestTableSectionGeoInAttr) EndAttribute() *InEntityTestTableSectionGeo {
	panic("stub")
}

func (inst *InEntityTestTableSectionGeoInAttr) AppendError(err error) { panic("stub") }

type InEntityTestTableSectionText struct {
}

func NewInEntityTestTableSectionText(builder *array.RecordBuilder, parent *InEntityTestTable) (inst *InEntityTestTableSectionText) {
	panic("stub")
}

func (inst *InEntityTestTableSectionText) BeginAttribute(text12 string) *InEntityTestTableSectionTextInAttr {
	panic("stub")
}

func (inst *InEntityTestTableSectionText) CheckErrors() (err error) { panic("stub") }

func (inst *InEntityTestTableSectionText) EndSection() *InEntityTestTable { panic("stub") }

func (inst *InEntityTestTableSectionText) AppendError(err error) { panic("stub") }

type InEntityTestTableSectionTextInAttr struct {
}

func NewInEntityTestTableSectionTextInAttr(builder *array.RecordBuilder, parent *InEntityTestTableSectionText) (inst *InEntityTestTableSectionTextInAttr) {
	panic("stub")
}

func (inst *InEntityTestTableSectionTextInAttr) AddToCoContainers(wordLength13 uint32, words14 string) *InEntityTestTableSectionTextInAttr {
	panic("stub")
}

func (inst *InEntityTestTableSectionTextInAttr) AddMembershipLowCardRef(lr15 uint64) *InEntityTestTableSectionTextInAttr {
	panic("stub")
}

func (inst *InEntityTestTableSectionTextInAttr) AddMembershipLowCardRefP(lr15 uint64) { panic("stub") }

func (inst *InEntityTestTableSectionTextInAttr) AddMembershipMixedLowCardVerbatim(lmv16 []byte, mvhp17 []byte) *InEntityTestTableSectionTextInAttr {
	panic("stub")
}

func (inst *InEntityTestTableSectionTextInAttr) AddMembershipMixedLowCardVerbatimP(lmv16 []byte, mvhp17 []byte) {
	panic("stub")
}

func (inst *InEntityTestTableSectionTextInAttr) EndSection() *InEntityTestTable { panic("stub") }

func (inst *InEntityTestTableSectionTextInAttr) EndAttribute() *InEntityTestTableSectionText {
	panic("stub")
}

func (inst *InEntityTestTableSectionTextInAttr) AppendError(err error) { panic("stub") }


```

--- FILE: readaccess/example/readaccess_testtable_ra.out.go ---
```go
// Code generated; Leeway readaccess (github.com/stergiotis/boxer/public/semistructured/leeway/readaccess.test) DO NOT EDIT.

package example

import (
	"iter"
	"time"

	_ "github.com/apache/arrow-go/v18/arrow" ///////////////////////////////////////////////////////////////////
	// code generator
	// readaccess.(*GeneratorDriver).GenerateGoClasses
	// ./public/semistructured/leeway/readaccess/lw_ra_generator_hl.go:67

	_ "iter"
	_ "slices"
	_ "time"

	_ "github.com/apache/arrow-go/v18/arrow/array"
	_ "github.com/rs/zerolog/log"
	_ "github.com/stergiotis/boxer/public/observability/eh/eb"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/common"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/readaccess/fatruntime"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/readaccess/runtime"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/common"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/naming"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/readaccess/runtime"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/useaspects"
	///////////////////////////////////////////////////////////////////
	// code generator
	// readaccess.(*GeneratorDriver).GenerateGoClasses
	// ./public/semistructured/leeway/readaccess/lw_ra_generator_hl.go:101
)

///////////////////////////////////////////////////////////////////
// code generator
// readaccess.(*GoClassBuilder).composeMembershipPacks
// ./public/semistructured/leeway/readaccess/lw_ra_generator.go:238

type MembershipPackTestTableShared1 struct {
	ValueLowCardRef                                 *array.List
	ValueLowCardRefElements                         *array.Uint64
	AccelLowCardRef                                 *runtime.RandomAccessTwoLevelLookupAccel[runtime.MembershipLowCardRefIdx, runtime.AttributeIdx, int, int64]
	ColumnIndexLowCardRef                           uint32
	ColumnIndexLowCardRefAccel                      uint32
	ValueMixedLowCardVerbatim                       *array.List
	ValueMixedLowCardVerbatimElements               *array.Binary
	AccelMixedLowCardVerbatim                       *runtime.RandomAccessTwoLevelLookupAccel[runtime.MembershipMixedLowCardVerbatimIdx, runtime.AttributeIdx, int, int64]
	ColumnIndexMixedLowCardVerbatim                 uint32
	ColumnIndexMixedLowCardVerbatimAccel            uint32
	ValueMixedVerbatimHighCardParameters            *array.List
	ValueMixedVerbatimHighCardParametersElements    *array.Binary
	AccelMixedVerbatimHighCardParameters            *runtime.RandomAccessTwoLevelLookupAccel[runtime.MembershipMixedVerbatimHighCardParametersIdx, runtime.AttributeIdx, int, int64]
	ColumnIndexMixedVerbatimHighCardParameters      uint32
	ColumnIndexMixedVerbatimHighCardParametersAccel uint32
}

func NewMembershipPackTestTableShared1Geo() (inst *MembershipPackTestTableShared1) { panic("stub") }

func (inst *MembershipPackTestTableShared1) GetColumnIndices() (columnIndices []uint32) {
	panic("stub")
}

func (inst *MembershipPackTestTableShared1) GetColumnIndexFieldNames() (fieldNames []string) {
	panic("stub")
}

func (inst *MembershipPackTestTableShared1) SetColumnIndices(indices []uint32) (rest []uint32) {
	panic("stub")
}

func NewMembershipPackTestTableShared1Text() (inst *MembershipPackTestTableShared1) { panic("stub") }

func (inst *MembershipPackTestTableShared1) Release() { panic("stub") }

func (inst *MembershipPackTestTableShared1) Reset() { panic("stub") }

func (inst *MembershipPackTestTableShared1) LoadFromRecord(rec runtime.RecordI) (err error) {
	panic("stub")
}

func (inst *MembershipPackTestTableShared1) Len() (nEntities int) { panic("stub") }

func (inst *MembershipPackTestTableShared1) GetTotalNumberOfMemberItems(entityIdx runtime.EntityIdx) (nItems int64) {
	panic("stub")
}

func (inst *MembershipPackTestTableShared1) GetNumberOfMemberItemsLowCardRef(entityIdx runtime.EntityIdx) (nItems int64) {
	panic("stub")
}

func (inst *MembershipPackTestTableShared1) GetNumberOfMemberItemsMixedLowCardVerbatim(entityIdx runtime.EntityIdx) (nItems int64) {
	panic("stub")
}

func (inst *MembershipPackTestTableShared1) GetMembValueLowCardRef(entityIdx runtime.EntityIdx, attrIdx runtime.AttributeIdx) iter.Seq[uint64] {
	panic("stub")
}

func (inst *MembershipPackTestTableShared1) GetMembValueMixedLowCardVerbatim(entityIdx runtime.EntityIdx, attrIdx runtime.AttributeIdx) iter.Seq[[]byte] {
	panic("stub")
}

func (inst *MembershipPackTestTableShared1) GetMembValueMixedVerbatimHighCardParameters(entityIdx runtime.EntityIdx, attrIdx runtime.AttributeIdx) iter.Seq[[]byte] {
	panic("stub")
}

func (inst *MembershipPackTestTableShared1) GetMembValueLowCardVerbatimHighCardParams(entityIdx runtime.EntityIdx, attrIdx runtime.AttributeIdx) iter.Seq2[[]byte, []byte] {
	panic("stub")
}

func (inst *MembershipPackTestTableShared1) GetNumberOfMemberItemsByAttrLowCardRef(entityIdx runtime.EntityIdx, attrIdx runtime.AttributeIdx) (nItems int) {
	panic("stub")
}

func (inst *MembershipPackTestTableShared1) GetNumberOfMemberItemsByAttrLowCardVerbatimHighCardParams(entityIdx runtime.EntityIdx, attrIdx runtime.AttributeIdx) (nItems int) {
	panic("stub")
}

///////////////////////////////////////////////////////////////////
// code generator
// readaccess.(*GoClassBuilder).composeSectionAttributeClasses
// ./public/semistructured/leeway/readaccess/lw_ra_generator.go:849

type ReadAccessTestTablePlainEntityIdAttributes struct {
	ValueId       *array.Uint64
	ColumnIndexId uint32
}

type ReadAccessTestTablePlainEntityTimestampAttributes struct {
	ValueTs           *array.Timestamp
	ColumnIndexTs     uint32
	ValueProc         *array.List
	ColumnIndexProc   uint32
	ValueProcElements *array.Timestamp
}

type ReadAccessTestTableTaggedGeoAttributes struct {
	ValueLat            *array.List
	ColumnIndexLat      uint32
	ValueLatElements    *array.Float32
	ValueLng            *array.List
	ColumnIndexLng      uint32
	ValueLngElements    *array.Float32
	ValueH3Res1         *array.List
	ColumnIndexH3Res1   uint32
	ValueH3Res1Elements *array.Uint64
	ValueH3Res2         *array.List
	ColumnIndexH3Res2   uint32
	ValueH3Res2Elements *array.Uint64
}

type ReadAccessTestTableTaggedTextAttributes struct {
	ValueText                  *array.List
	ColumnIndexText            uint32
	ValueTextElements          *array.String
	ValueWordLength            *array.List
	ColumnIndexWordLength      uint32
	ValueWordLengthElements    *array.Uint32
	ValueWords                 *array.List
	ColumnIndexWords           uint32
	ValueWordsElements         *array.String
	AccelHomogenousArray       *runtime.RandomAccessTwoLevelLookupAccel[runtime.HomogenousArrayIdx, runtime.AttributeIdx, int, int64]
	ColumnIndexHomogenousArray uint32
}

func NewReadAccessTestTablePlainEntityIdAttributes() (inst *ReadAccessTestTablePlainEntityIdAttributes) {
	panic("stub")
}

func (inst *ReadAccessTestTablePlainEntityIdAttributes) GetColumnIndices() (columnIndices []uint32) {
	panic("stub")
}

func (inst *ReadAccessTestTablePlainEntityIdAttributes) GetColumnIndexFieldNames() (fieldNames []string) {
	panic("stub")
}

func (inst *ReadAccessTestTablePlainEntityIdAttributes) SetColumnIndices(indices []uint32) (rest []uint32) {
	panic("stub")
}

func NewReadAccessTestTablePlainEntityTimestampAttributes() (inst *ReadAccessTestTablePlainEntityTimestampAttributes) {
	panic("stub")
}

func (inst *ReadAccessTestTablePlainEntityTimestampAttributes) GetColumnIndices() (columnIndices []uint32) {
	panic("stub")
}

func (inst *ReadAccessTestTablePlainEntityTimestampAttributes) GetColumnIndexFieldNames() (fieldNames []string) {
	panic("stub")
}

func (inst *ReadAccessTestTablePlainEntityTimestampAttributes) SetColumnIndices(indices []uint32) (rest []uint32) {
	panic("stub")
}

func NewReadAccessTestTableTaggedGeoAttributes() (inst *ReadAccessTestTableTaggedGeoAttributes) {
	panic("stub")
}

func (inst *ReadAccessTestTableTaggedGeoAttributes) GetColumnIndices() (columnIndices []uint32) {
	panic("stub")
}

func (inst *ReadAccessTestTableTaggedGeoAttributes) GetColumnIndexFieldNames() (fieldNames []string) {
	panic("stub")
}

func (inst *ReadAccessTestTableTaggedGeoAttributes) SetColumnIndices(indices []uint32) (rest []uint32) {
	panic("stub")
}

func NewReadAccessTestTableTaggedTextAttributes() (inst *ReadAccessTestTableTaggedTextAttributes) {
	panic("stub")
}

func (inst *ReadAccessTestTableTaggedTextAttributes) GetColumnIndices() (columnIndices []uint32) {
	panic("stub")
}

func (inst *ReadAccessTestTableTaggedTextAttributes) GetColumnIndexFieldNames() (fieldNames []string) {
	panic("stub")
}

func (inst *ReadAccessTestTableTaggedTextAttributes) SetColumnIndices(indices []uint32) (rest []uint32) {
	panic("stub")
}

///////////////////////////////////////////////////////////////////
// code generator
// readaccess.(*GoClassBuilder).composeSectionAttributeClasses
// ./public/semistructured/leeway/readaccess/lw_ra_generator.go:1076

func (inst *ReadAccessTestTablePlainEntityIdAttributes) Reset() { panic("stub") }

func (inst *ReadAccessTestTablePlainEntityTimestampAttributes) Reset() { panic("stub") }

func (inst *ReadAccessTestTableTaggedGeoAttributes) Reset() { panic("stub") }

func (inst *ReadAccessTestTableTaggedTextAttributes) Reset() { panic("stub") }

///////////////////////////////////////////////////////////////////
// code generator
// readaccess.(*GoClassBuilder).composeSectionAttributeClasses
// ./public/semistructured/leeway/readaccess/lw_ra_generator.go:1155

func (inst *ReadAccessTestTablePlainEntityIdAttributes) Release() { panic("stub") }

func (inst *ReadAccessTestTablePlainEntityTimestampAttributes) Release() { panic("stub") }

func (inst *ReadAccessTestTableTaggedGeoAttributes) Release() { panic("stub") }

func (inst *ReadAccessTestTableTaggedTextAttributes) Release() { panic("stub") }

///////////////////////////////////////////////////////////////////
// code generator
// readaccess.(*GoClassBuilder).composeSectionAttributeClasses
// ./public/semistructured/leeway/readaccess/lw_ra_generator.go:1239

func (inst *ReadAccessTestTablePlainEntityIdAttributes) Len() (nEntities int) { panic("stub") }

func (inst *ReadAccessTestTablePlainEntityTimestampAttributes) Len() (nEntities int) { panic("stub") }

func (inst *ReadAccessTestTableTaggedGeoAttributes) Len() (nEntities int) { panic("stub") }

func (inst *ReadAccessTestTableTaggedTextAttributes) Len() (nEntities int) { panic("stub") }

///////////////////////////////////////////////////////////////////
// code generator
// readaccess.(*GoClassBuilder).composeSectionAttributeClasses
// ./public/semistructured/leeway/readaccess/lw_ra_generator.go:1293

func (inst *ReadAccessTestTablePlainEntityIdAttributes) LoadFromRecord(rec runtime.RecordI) (err error) {
	panic("stub")
}

func (inst *ReadAccessTestTablePlainEntityTimestampAttributes) LoadFromRecord(rec runtime.RecordI) (err error) {
	panic("stub")
}

func (inst *ReadAccessTestTableTaggedGeoAttributes) LoadFromRecord(rec runtime.RecordI) (err error) {
	panic("stub")
}

func (inst *ReadAccessTestTableTaggedTextAttributes) LoadFromRecord(rec runtime.RecordI) (err error) {
	panic("stub")
}

func (inst *ReadAccessTestTableTaggedGeoAttributes) GetAttrValueLat(entityIdx runtime.EntityIdx, attrIdx runtime.AttributeIdx) (scalarAttrValue float32) {
	panic("stub")
}

func (inst *ReadAccessTestTableTaggedGeoAttributes) GetAttrValueLng(entityIdx runtime.EntityIdx, attrIdx runtime.AttributeIdx) (scalarAttrValue float32) {
	panic("stub")
}

func (inst *ReadAccessTestTableTaggedGeoAttributes) GetAttrValueH3Res1(entityIdx runtime.EntityIdx, attrIdx runtime.AttributeIdx) (scalarAttrValue uint64) {
	panic("stub")
}

func (inst *ReadAccessTestTableTaggedGeoAttributes) GetAttrValueH3Res2(entityIdx runtime.EntityIdx, attrIdx runtime.AttributeIdx) (scalarAttrValue uint64) {
	panic("stub")
}

func (inst *ReadAccessTestTableTaggedTextAttributes) GetAttrValueText(entityIdx runtime.EntityIdx, attrIdx runtime.AttributeIdx) (scalarAttrValue string) {
	panic("stub")
}

func (inst *ReadAccessTestTableTaggedTextAttributes) GetAttrValueWordLength(entityIdx runtime.EntityIdx, attrIdx runtime.AttributeIdx) iter.Seq[uint32] {
	panic("stub")
}

func (inst *ReadAccessTestTableTaggedTextAttributes) GetAttrValueWords(entityIdx runtime.EntityIdx, attrIdx runtime.AttributeIdx) iter.Seq[string] {
	panic("stub")
}

func (inst *ReadAccessTestTablePlainEntityIdAttributes) GetAttrValueId(entityIdx runtime.EntityIdx) (scalarAttrValue uint64) {
	panic("stub")
}

func (inst *ReadAccessTestTablePlainEntityTimestampAttributes) GetAttrValueTs(entityIdx runtime.EntityIdx) (scalarAttrValue time.Time) {
	panic("stub")
}

func (inst *ReadAccessTestTablePlainEntityTimestampAttributes) GetAttrValueProc(entityIdx runtime.EntityIdx) iter.Seq[time.Time] {
	panic("stub")
}

///////////////////////////////////////////////////////////////////
// code generator
// readaccess.(*GoClassBuilder).composeSectionAttributeClasses
// ./public/semistructured/leeway/readaccess/lw_ra_generator.go:1569

func (inst *ReadAccessTestTableTaggedGeoAttributes) GetNumberOfAttributes(entityIdx runtime.EntityIdx) (nAttributes int64) {
	panic("stub")
}

func (inst *ReadAccessTestTableTaggedTextAttributes) GetNumberOfAttributes(entityIdx runtime.EntityIdx) (nAttributes int64) {
	panic("stub")
}

///////////////////////////////////////////////////////////////////
// code generator
// readaccess.(*GoClassBuilder).composeSectionClasses
// ./public/semistructured/leeway/readaccess/lw_ra_generator.go:1623

type ReadAccessTestTableTaggedGeo struct {
	Attributes  *ReadAccessTestTableTaggedGeoAttributes
	Memberships *MembershipPackTestTableShared1
}

type ReadAccessTestTableTaggedText struct {
	Attributes  *ReadAccessTestTableTaggedTextAttributes
	Memberships *MembershipPackTestTableShared1
}

func NewReadAccessTestTableTaggedGeo() (inst *ReadAccessTestTableTaggedGeo) { panic("stub") }

func NewReadAccessTestTableTaggedText() (inst *ReadAccessTestTableTaggedText) { panic("stub") }

func (inst *ReadAccessTestTableTaggedGeo) SetColumnIndices(indices []uint32) (restIndices []uint32) {
	panic("stub")
}

func (inst *ReadAccessTestTableTaggedText) SetColumnIndices(indices []uint32) (restIndices []uint32) {
	panic("stub")
}

func (inst *ReadAccessTestTableTaggedGeo) GetColumnIndices() (columnIndices []uint32) { panic("stub") }

func (inst *ReadAccessTestTableTaggedText) GetColumnIndices() (columnIndices []uint32) { panic("stub") }

func (inst *ReadAccessTestTableTaggedGeo) GetColumnIndexFieldNames() (fieldNames []string) {
	panic("stub")
}

func (inst *ReadAccessTestTableTaggedText) GetColumnIndexFieldNames() (fieldNames []string) {
	panic("stub")
}

func (inst *ReadAccessTestTableTaggedGeo) Release() { panic("stub") }

func (inst *ReadAccessTestTableTaggedText) Release() { panic("stub") }

func (inst *ReadAccessTestTableTaggedGeo) LoadFromRecord(rec runtime.RecordI) (err error) {
	panic("stub")
}

func (inst *ReadAccessTestTableTaggedText) LoadFromRecord(rec runtime.RecordI) (err error) {
	panic("stub")
}

func (inst *ReadAccessTestTableTaggedGeo) Len() (nEntities int) { panic("stub") }

func (inst *ReadAccessTestTableTaggedText) Len() (nEntities int) { panic("stub") }

func (inst *ReadAccessTestTableTaggedGeo) GetAttributes() *ReadAccessTestTableTaggedGeoAttributes {
	panic("stub")
}

func (inst *ReadAccessTestTableTaggedText) GetAttributes() *ReadAccessTestTableTaggedTextAttributes {
	panic("stub")
}

func (inst *ReadAccessTestTableTaggedGeo) GetMemberships() *MembershipPackTestTableShared1 {
	panic("stub")
}

func (inst *ReadAccessTestTableTaggedText) GetMemberships() *MembershipPackTestTableShared1 {
	panic("stub")
}

func (inst *ReadAccessTestTableTaggedGeo) GetSectionName() naming.StylableName { panic("stub") }

func (inst *ReadAccessTestTableTaggedText) GetSectionName() naming.StylableName { panic("stub") }

func (inst *ReadAccessTestTableTaggedGeo) GetSectionUseAspects() useaspects.AspectSet { panic("stub") }

func (inst *ReadAccessTestTableTaggedText) GetSectionUseAspects() useaspects.AspectSet { panic("stub") }

func (inst *ReadAccessTestTableTaggedGeo) GetSectionStreamingGroup() naming.Key { panic("stub") }

func (inst *ReadAccessTestTableTaggedText) GetSectionStreamingGroup() naming.Key { panic("stub") }

func (inst *ReadAccessTestTableTaggedGeo) GetSectionCoSectionGroup() naming.Key { panic("stub") }

func (inst *ReadAccessTestTableTaggedText) GetSectionCoSectionGroup() naming.Key { panic("stub") }

func (inst *ReadAccessTestTableTaggedGeo) GetSectionMembershipSpec() common.MembershipSpecE {
	panic("stub")
}

func (inst *ReadAccessTestTableTaggedText) GetSectionMembershipSpec() common.MembershipSpecE {
	panic("stub")
}

///////////////////////////////////////////////////////////////////
// code generator
// readaccess.(*GoClassBuilder).composeEntityClasses
// ./public/semistructured/leeway/readaccess/lw_ra_generator.go:1973

type ReadAccessTestTable struct {
	EntityId        *ReadAccessTestTablePlainEntityIdAttributes
	EntityTimestamp *ReadAccessTestTablePlainEntityTimestampAttributes
	Geo             *ReadAccessTestTableTaggedGeo
	Text            *ReadAccessTestTableTaggedText
}

func NewReadAccessTestTable() (inst *ReadAccessTestTable) { panic("stub") }

func (inst *ReadAccessTestTable) Release() { panic("stub") }

func (inst *ReadAccessTestTable) LoadFromRecord(rec runtime.RecordI) (err error) { panic("stub") }

func (inst *ReadAccessTestTable) SetColumnIndices(indices []uint32) (rest []uint32) { panic("stub") }

func (inst *ReadAccessTestTable) GetColumnIndices() (columnIndices []uint32) { panic("stub") }

func (inst *ReadAccessTestTable) GetColumnIndexFieldNames() (fieldNames []string) { panic("stub") }

func (inst *ReadAccessTestTable) GetNumberOfEntities() (nEntities int) { panic("stub") }


```

--- FILE: readaccess/fatruntime/lw_ra_rt_fat.go ---
```go
package fatruntime

import (
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/common"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/common"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/naming"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/useaspects"
)

type SectionIntrospectionI interface {
	GetSectionName() naming.StylableName
	GetSectionUseAspects() useaspects.AspectSet
	GetSectionStreamingGroup() naming.Key
	GetSectionCoSectionGroup() naming.Key
	GetSectionMembershipSpec() common.MembershipSpecE
}


```

--- FILE: readaccess/lw_ra_generator.go ---
```go
package readaccess

import (
	_ "fmt"
	_ "slices"
	"strings"
	_ "strings"

	_ "github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/containers"
	_ "github.com/stergiotis/boxer/public/containers"
	_ "github.com/stergiotis/boxer/public/observability/eh"
	_ "github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/observability/vcs"
	_ "github.com/stergiotis/boxer/public/observability/vcs"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/codegen"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/common"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/ddl/arrow"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/ddl/golang"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/gocodegen"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/common"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/gocodegen"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/naming"
)

var CodeGeneratorName = "Leeway readaccess (" + vcs.ModuleInfo() + ")"

func NewGoClassBuilder(fatRuntime bool) *GoClassBuilder { panic("stub") }

func (inst *GoClassBuilder) SetCodeBuilder(s *strings.Builder) { panic("stub") }

func (inst *GoClassBuilder) GetCode() (code string, err error) { panic("stub") }

func (inst *GoClassBuilder) ResetCodeBuilder() { panic("stub") }

func (inst *GoClassBuilder) PrepareCodeComposition() { panic("stub") }

func (inst *GoClassBuilder) ComposeNamingConventionDependentCode(tableName naming.StylableName, ir *common.IntermediateTableRepresentation, namingConvention common.NamingConventionI, tableRowConfig common.TableRowConfigE, clsNamer gocodegen.GoClassNamerI) (err error) {
	panic("stub")
}

func (inst *GoClassBuilder) ComposeAttributeClassAndFactoryCode(clsNamer gocodegen.GoClassNamerI, tableName naming.StylableName, sectionName naming.StylableName, sectionIdx int, totalSections int, sectionIRH *common.IntermediatePairHolder, tableRowConfig common.TableRowConfigE) (err error) {
	panic("stub")
}

func (inst *GoClassBuilder) ComposeSectionClassAndFactoryCode(clsNamer gocodegen.GoClassNamerI, tableName naming.StylableName, sectionName naming.StylableName, sectionIdx int, totalSections int, sectionIRH *common.IntermediatePairHolder, tableRowConfig common.TableRowConfigE) (err error) {
	panic("stub")
}

func (inst *GoClassBuilder) ComposeAttributeCode(clsNamer gocodegen.GoClassNamerI, tableName naming.StylableName, sectionName naming.StylableName, sectionIdx int, totalSections int, sectionIRH *common.IntermediatePairHolder, tableRowConfig common.TableRowConfigE) (err error) {
	panic("stub")
}

func (inst *GoClassBuilder) ComposeSectionCode(clsNamer gocodegen.GoClassNamerI, tableName naming.StylableName, sectionName naming.StylableName, sectionIdx int, totalSections int, sectionIRH *common.IntermediatePairHolder, tableRowConfig common.TableRowConfigE) (err error) {
	panic("stub")
}

func ComposeMembershipPackInfo(tblDesc common.TableDesc, namer gocodegen.GoClassNamerReadAccessI) (membershipSpecs []common.MembershipSpecE, classNames []string, sectionToClassName []string, err error) {
	panic("stub")
}

// FIXME encoding hints vs demoted canonical type

// FIXME encoding hints vs demoted canonical type

// struct

// New

// .Release()

// .Reset()

// .LoadFromRecord(rec runtime.RecordI[C,D]) (err error)

// FIXME inconsistency in arrow: arrow.BOOLEAN but arrow.BooleanType{}

// .Len() (nEntities int)

// .GetTotalNumberOfMemberItems() (nItems int64)

// .GetNumberOfMemberItemsXXX() (nItems int64)

// .GetMembValueXXX(entityIdx runtime.EntityIdx,membIdx runtime.MemberIdx) (iter.Seq[XXX])

// .GetNumberOfMemberItemsByAttr(entityIdx runtime.EntityIdx,membIdx runtime.MemberIdx) (nItems int)

// FIXME name clashes with regular attributes possible?

// attribute classes: struct

// attribute class: factory

// .Reset()

// .Release()

// .Len()

// .LoadFromRecord(rec runtime.RecordI[C,D]) (err error)

// .GetAttrValueXXX

// .GetNumberOfAttributes(i runtime.EntityIdx) (nAttributes int)

// membership packs

// attribute classes

// struct

// factory

// .SetColumnIndices(indices []uint32) (restIndices []uint32)

// .GetColumnIndices() (columnIndices []uint32)

// .GetColumnIndexFieldNames() (fieldNames []string)

// .Release()

// .LoadFromRecord(rec runtime.RecordI[C,D]) (err error)

// .Len() (nEntities int)

// Getters for public Attributes to enable generic programming (interfaces)

// section introspection
// .GetSectionName() naming.StylableName

// .GetSectionUseAspects() useaspects.AspectSet

// .GetSectionStreamingGroup() naming.Key

// .GetSectionCoSectionGroup() naming.Key

// .GetSectionMembershipSpec() common.MembershipSpecE

// entity struct

// factory

// .Release()

// .LoadFromRecord(rec runtime.RecordI[C,D]) (err error)

// .SetColumnIndices(indices []uint32)

// .GetColumnIndices() (columnIndices []uint32)

// .GetColumnIndexFieldNames() (fieldNames []string)

// .GetNumberOfEntities()

func (inst *GoClassBuilder) ComposeEntityClassAndFactoryCode(clsNamer gocodegen.GoClassNamerI, tableName naming.StylableName, sectionNames []naming.StylableName, ir *common.IntermediateTableRepresentation, tableRowConfig common.TableRowConfigE, entityIRH *common.IntermediatePairHolder) (err error) {
	panic("stub")
}

func (inst *GoClassBuilder) ComposeEntityCode(clsNamer gocodegen.GoClassNamerI, tableName naming.StylableName, sectionNames []naming.StylableName, ir *common.IntermediateTableRepresentation, tableRowConfig common.TableRowConfigE, entityIRH *common.IntermediatePairHolder) (err error) {
	panic("stub")
}

func (inst *GoClassBuilder) ComposeGoImports(ir *common.IntermediateTableRepresentation, tableRowConfig common.TableRowConfigE, suppressedImports *containers.HashSet[string]) (err error) {
	panic("stub")
}


```

--- FILE: readaccess/lw_ra_generator_colidx.go ---
```go
package readaccess

import (
	_ "fmt"
	"io"
	_ "io"
	"iter"
	_ "iter"
)

type ColumnIndexCodeGenerator struct {
}

func NewColumnIndexCodeGenerator() *ColumnIndexCodeGenerator { panic("stub") }

func (inst *ColumnIndexCodeGenerator) IterateAll() iter.Seq2[uint32, string] { panic("stub") }

func (inst *ColumnIndexCodeGenerator) AddField(name string, columnIndex uint32) { panic("stub") }

func (inst *ColumnIndexCodeGenerator) GenerateInstInit(w io.Writer) (err error) { panic("stub") }

func (inst *ColumnIndexCodeGenerator) Length() int { panic("stub") }

func (inst *ColumnIndexCodeGenerator) GenerateCommon(w io.Writer, instClassType string) (err error) {
	panic("stub")
}

func (inst *ColumnIndexCodeGenerator) Reset() { panic("stub") }


```

--- FILE: readaccess/lw_ra_generator_generic.go ---
```go
//go:build leeway_generic

package readaccess


```

--- FILE: readaccess/lw_ra_generator_hl.go ---
```go
package readaccess

import (
	_ "fmt"
	_ "go/format"
	_ "strings"

	_ "github.com/stergiotis/boxer/public/code/synthesis/golang"
	_ "github.com/stergiotis/boxer/public/containers"
	_ "github.com/stergiotis/boxer/public/observability/eh"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/common"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/gocodegen"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	_ "github.com/stergiotis/boxer/public/unsafeperf"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/common"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/gocodegen"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/naming"
)

func NewGoCodeGeneratorDriver(namingConvention common.NamingConventionI, tech common.TechnologySpecificGeneratorI, fatRuntime bool) *GeneratorDriver {
	panic("stub")
}

func (inst *GeneratorDriver) GenerateGoClasses(packageName string, tableName naming.StylableName, tblDesc common.TableDesc, tableRowConfig common.TableRowConfigE, clsNamer gocodegen.GoClassNamerI) (sourceCode []byte, wellFormed bool, err error) {
	panic("stub")
}

// FIXME

// try formatting source code


```

--- FILE: readaccess/lw_ra_generator_nongeneric.go ---
```go
//go:build !leeway_generic

package readaccess


```

--- FILE: readaccess/lw_ra_types.go ---
```go
package readaccess

import (
	_ "strings"

	_ "github.com/stergiotis/boxer/public/semistructured/leeway/common"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/ddl/golang"
)

type GoClassBuilder struct {
}
type GeneratorDriver struct {
}


```

--- FILE: readaccess/runtime/lw_ra_generic.go ---
```go
//go:build leeway_generic

package runtime

import (
	"github.com/apache/arrow-go/v18/arrow"
	_ "github.com/apache/arrow-go/v18/arrow"
)

type RecordI[C ColumnI[D], D ArrayDataI] interface {
	ReferenceCountingI
	Schema() *arrow.Schema

	NumRows() int64
	NumCols() int64

	Column(i int) C
}

type ColumnI[D ArrayDataI] interface {
	ReferenceCountingI
	DataType() arrow.DataType
	Data() D
	Len() int
}
type ArrayDataI interface {
	arrow.ArrayData
	//ReferenceCountingI
	//DataType() arrow.DataType
	//Len() int
}

func LoadAccelFieldFromRecord[F, B IndexConstraintI, C ColumnI[D], D ArrayDataI](idx uint32, rec RecordI[C, D], dest *RandomAccessTwoLevelLookupAccel[F, B, int, int64]) (err error) {
	panic("stub")
}

func LoadScalarValueFieldFromRecord[S any, C ColumnI[D], D ArrayDataI](idx uint32, expectedDatatype arrow.Type, rec RecordI[C, D], dest **S, ctor func(data arrow.ArrayData) *S) (err error) {
	panic("stub")
}

func LoadNonScalarValueFieldFromRecord[S any, C ColumnI[D], D ArrayDataI](idx uint32, expectedDatatype arrow.Type, rec RecordI[C, D], dest **array.List, destElementAccess **S, ctorElementAccess func(data arrow.ArrayData) *S) (err error) {
	panic("stub")
}


```

--- FILE: readaccess/runtime/lw_ra_nongeneric.go ---
```go
//go:build !leeway_generic

package runtime

import (
	"github.com/apache/arrow-go/v18/arrow"
	_ "github.com/apache/arrow-go/v18/arrow"
	_ "github.com/apache/arrow-go/v18/arrow/array"
	_ "github.com/stergiotis/boxer/public/observability/eh/eb"
)

type RecordI interface {
	ReferenceCountingI
	Schema() *arrow.Schema

	NumRows() int64
	NumCols() int64

	Column(i int) arrow.Array
}

type ArrayDataI interface {
	arrow.ArrayData
	//ReferenceCountingI
	//DataType() arrow.DataType
	//Len() int
}

func LoadAccelFieldFromRecord[F, B IndexConstraintI](idx uint32, rec RecordI, dest *RandomAccessTwoLevelLookupAccel[F, B, int, int64]) (err error) {
	panic("stub")
}

func LoadScalarValueFieldFromRecord[S any](idx uint32, expectedDatatype arrow.Type, rec RecordI, dest **S, ctor func(data arrow.ArrayData) *S) (err error) {
	panic("stub")
}

func LoadNonScalarValueFieldFromRecord[S any](idx uint32, expectedDatatype arrow.Type, rec RecordI, dest **array.List, destElementAccess **S, ctorElementAccess func(data arrow.ArrayData) *S) (err error) {
	panic("stub")
}


```

--- FILE: readaccess/runtime/lw_ra_rt_impl.go ---
```go
package runtime

import (
	_ "github.com/stergiotis/boxer/public/generic"
	"github.com/stergiotis/boxer/public/observability/eh"
	_ "github.com/stergiotis/boxer/public/observability/eh"
)

var AccelEstimatedInitialLength = 128

var ErrUnexpectedArrowDataType = eh.Errorf("unexpected arrow data type")

func ReleaseIfNotNil[T ReleasableI](a T) { panic("stub") }


```

--- FILE: readaccess/runtime/lw_ra_rt_randomaccess1.go ---
```go
package runtime

import (
	"iter"
	_ "iter"
	_ "slices"
)

func NewRandomAccessLookupAccel[F IndexConstraintI, B IndexConstraintI](estLength int) *RandomAccessLookupAccel[F, B] {
	panic("stub")
}

func (inst *RandomAccessLookupAccel[F, B]) LookupForward(i B) (beginIncl F, endExcl F) { panic("stub") }

func (inst *RandomAccessLookupAccel[F, B]) LookupForwardRange(i B) (r Range[F]) { panic("stub") }

func (inst *RandomAccessLookupAccel[F, B]) LookupForwardIndexedRange(i B) (r IndexedRange[F, B]) {
	panic("stub")
}

func (inst *RandomAccessLookupAccel[F, B]) LookupBackward(i F) (index B) { panic("stub") }

func (inst *RandomAccessLookupAccel[F, B]) GetCardinality(i B) (card uint64) { panic("stub") }

func (inst *RandomAccessLookupAccel[F, B]) IterateAllFwdIndexedRange() iter.Seq[IndexedRange[F, B]] {
	panic("stub")
}

func (inst *RandomAccessLookupAccel[F, B]) IterateAllFwdRange() iter.Seq[Range[F]] { panic("stub") }

func (inst *RandomAccessLookupAccel[F, B]) LoadCardinalities(cards []uint64) { panic("stub") }

func (inst *RandomAccessLookupAccel[F, B]) Len() int { panic("stub") }

func (inst *RandomAccessLookupAccel[F, B]) Release() {
	panic(
		// nothing to do
		"stub")
}

func (inst *RandomAccessLookupAccel[F, B]) Reset() { panic("stub") }


```

--- FILE: readaccess/runtime/lw_ra_rt_randomaccess2.go ---
```go
package runtime

import (
	"iter"
	_ "iter"
)

func NewRandomAccessTwoLevelLookupAccel[F IndexConstraintI, B IndexConstraintI, I, I2 IndexConstraintI](estLength int) *RandomAccessTwoLevelLookupAccel[F, B, I, I2] {
	panic("stub")
}

func (inst *RandomAccessTwoLevelLookupAccel[F, B, I, I2]) SetCurrentEntityIdx(current I) {
	panic("stub")
}

func (inst *RandomAccessTwoLevelLookupAccel[F, B, I, I2]) SetReleaser(releaser ReleasableI) {
	panic("stub")
}

func (inst *RandomAccessTwoLevelLookupAccel[F, B, I, I2]) SetRanger(ranger ValueOffsetI[I, I2]) {
	panic("stub")
}

func (inst *RandomAccessTwoLevelLookupAccel[F, B, I, I2]) LoadCardinalities(cards []uint64) {
	panic("stub")
}

func (inst *RandomAccessTwoLevelLookupAccel[F, B, I, I2]) LookupForward(i B) (beginIncl F, endExcl F) {
	panic("stub")
}

func (inst *RandomAccessTwoLevelLookupAccel[F, B, I, I2]) LookupForwardRange(i B) (r Range[F]) {
	panic("stub")
}

func (inst *RandomAccessTwoLevelLookupAccel[F, B, I, I2]) LookupForwardIndexedRange(i B) (r IndexedRange[F, B]) {
	panic("stub")
}

func (inst *RandomAccessTwoLevelLookupAccel[F, B, I, I2]) LookupBackward(i F) (index B) {
	panic("stub")
}

func (inst *RandomAccessTwoLevelLookupAccel[F, B, I, I2]) GetCardinality(i B) (card uint64) {
	panic("stub")
}

func (inst *RandomAccessTwoLevelLookupAccel[F, B, I, I2]) IterateAllFwdIndexedRange() iter.Seq[IndexedRange[F, B]] {
	panic("stub")
}

func (inst *RandomAccessTwoLevelLookupAccel[F, B, I, I2]) IterateAllFwdRange() iter.Seq[Range[F]] {
	panic("stub")
}

func (inst *RandomAccessTwoLevelLookupAccel[F, B, I, I2]) Len() int { panic("stub") }

func (inst *RandomAccessTwoLevelLookupAccel[F, B, I, I2]) Release() { panic("stub") }

func (inst *RandomAccessTwoLevelLookupAccel[F, B, I, I2]) Reset() { panic("stub") }


```

--- FILE: readaccess/runtime/lw_ra_rt_range.go ---
```go
package runtime

func (inst Range[T]) ToRange() (r Range[T]) { panic("stub") }

func (inst IndexedRange[R, I]) ToRange() (r Range[R]) { panic("stub") }

func (inst Range[T]) IsEmpty() bool { panic("stub") }

func (inst IndexedRange[R, I]) IsEmpty() bool { panic("stub") }

func (inst Range[T]) CalcCardinality() (card uint64) { panic("stub") }

func (inst IndexedRange[R, I]) CalcCardinality() (card uint64) { panic("stub") }


```

--- FILE: readaccess/runtime/lw_ra_rt_types.go ---
```go
package runtime

import (
	"iter"
	_ "iter"

	"golang.org/x/exp/constraints"
	_ "golang.org/x/exp/constraints"
)

type ReferenceCountingI interface {
	ReleasableI
	Retain()
}

type ReleasableI interface {
	Release()
}

type (
	AttributeIdx                                 int
	HomogenousArrayIdx                           int
	SetIdx                                       int
	MembershipHighCardRefIdx                     int
	MembershipHighCardRefParameterizedIdx        int
	MembershipHighCardVerbatimIdx                int
	MembershipLowCardRefIdx                      int
	MembershipLowCardRefParameterizedIdx         int
	MembershipLowCardVerbatimIdx                 int
	MembershipMixedLowCardRefIdx                 int
	MembershipMixedRefHighCardParametersIdx      int
	MembershipMixedLowCardVerbatimIdx            int
	MembershipMixedVerbatimHighCardParametersIdx int

	EntityIdx int
)

type IndexConstraintI interface {
	constraints.Integer | constraints.Unsigned
}
type RandomAccessLookupAccel[F IndexConstraintI, B IndexConstraintI] struct {
}
type ValueOffsetI[I IndexConstraintI, I2 IndexConstraintI] interface {
	ValueOffsets(i I) (beginIncl I2, endExcl I2)
}
type RandomAccessTwoLevelLookupAccel[F IndexConstraintI, B IndexConstraintI, I IndexConstraintI, I2 IndexConstraintI] struct {
}
type RowIdx int
type RandomAccessLookupAccelI[F IndexConstraintI, B IndexConstraintI] interface {
	LookupForward(i B) (beginIncl F, endExcl F)
	LookupForwardRange(i B) (r Range[F])
	LookupForwardIndexedRange(i B) (r IndexedRange[F, B])
	LookupBackward(i F) (index B)
	GetCardinality(i B) (card uint64)
	IterateAllFwdIndexedRange() iter.Seq[IndexedRange[F, B]]
	IterateAllFwdRange() iter.Seq[Range[F]]
	LoadCardinalities(cards []uint64)
	Len() int
	ReleasableI
	Reset()
}

type Range[T IndexConstraintI] struct {
	BeginIncl T
	EndExcl   T
}
type IndexedRange[R IndexConstraintI, I IndexConstraintI] struct {
	BeginIncl R
	EndExcl   R
	Index     I
	Length    int
}
type RangeI[T IndexConstraintI] interface {
	ToRange() (r Range[T])
	IsEmpty() bool
	CalcCardinality() (card uint64)
}

type ColumnIndexHandlingI interface {
	SetColumnIndices(indices []uint32) (restIndices []uint32)
	GetColumnIndices() (columnIndices []uint32)
	GetColumnIndexFieldNames() (columnIndexFieldNames []string)
}
type SectionMethodsI interface {
	ColumnIndexHandlingI
	ReleasableI
	Reset()
	Len() (nEntities int)
}
type PlainSectionMethodsI interface {
	SectionMethodsI
}
type TaggedSectionMethodsI interface {
	SectionMethodsI
	GetNumberOfAttributes(entityIdx EntityIdx) (nAttributes int64)
}
type InAttributeMembershipHighCardRefI interface {
	GetMembValueHighCardRef(entityIdx EntityIdx, attrIdx AttributeIdx) iter.Seq[uint64]
}
type InAttributeMembershipHighCardRefParametrizedI interface {
	GetMembValueHighCardRefParametrized(entityIdx EntityIdx, attrIdx AttributeIdx) iter.Seq[[]byte]
}
type InAttributeMembershipHighCardVerbatimI interface {
	GetMembValueHighCardVerbatim(entityIdx EntityIdx, attrIdx AttributeIdx) iter.Seq[[]byte]
}

type InAttributeMembershipLowCardRefI interface {
	GetMembValueLowCardRef(entityIdx EntityIdx, attrIdx AttributeIdx) iter.Seq[uint64]
}
type InAttributeMembershipLowCardRefParametrizedI interface {
	GetMembValueLowCardRefParametrized(entityIdx EntityIdx, attrIdx AttributeIdx) iter.Seq[[]byte]
}
type InAttributeMembershipLowCardVerbatimI interface {
	GetMembValueLowCardVerbatim(entityIdx EntityIdx, attrIdx AttributeIdx) iter.Seq[[]byte]
}

type InAttributeMembershipMixedLowCardRefI interface {
	GetMembValueMixedLowCardRef(entityIdx EntityIdx, attrIdx AttributeIdx) iter.Seq[uint64]
}
type InAttributeMembershipMixedLowCardVerbatimI interface {
	GetMembValueMixedLowCardVerbatim(entityIdx EntityIdx, attrIdx AttributeIdx) iter.Seq[[]byte]
}
type InAttributeMembershipMixedVerbatimHighCardParametersI interface {
	GetMembValueMixedVerbatimHighCardParameters(entityIdx EntityIdx, attrIdx AttributeIdx) iter.Seq[[]byte]
}
type InAttributeMembershipMixedRefHighCardParametersI interface {
	GetMembValueMixedRefHighCardParameters(entityIdx EntityIdx, attrIdx AttributeIdx) iter.Seq[[]byte]
}
type InAttributeMembershipMixedValueLowCardRefHighCardParamsI interface {
	InAttributeMembershipMixedLowCardRefI
	InAttributeMembershipMixedRefHighCardParametersI
	GetMembValueLowCardRefHighCardParams(entityIdx EntityIdx, attrIdx AttributeIdx) iter.Seq2[uint64, []byte]
}
type InAttributeMembershipMixedValueLowCardVerbatimHighCardParamsI interface {
	InAttributeMembershipMixedLowCardRefI
	InAttributeMembershipMixedVerbatimHighCardParametersI
	GetMembValueLowCardVerbatimHighCardParams(entityIdx EntityIdx, attrIdx AttributeIdx) iter.Seq2[[]byte, []byte]
}


```

--- FILE: stopa/contract/lw_stop_a_contracts.go ---
```go
package contract

import (
	"github.com/stergiotis/boxer/public/identity/identifier"
	_ "github.com/stergiotis/boxer/public/identity/identifier"
	_ "github.com/stergiotis/boxer/public/observability/eh/eb"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/naming"
)

type ContractI interface {
	ValidateTagValue(tv identifier.TagValue) error
	ValidateNaturalKeyHumanReadable(tv identifier.TagValue, name naming.StylableName) error
	ValidateNaturalKeyMachineReadable(tv identifier.TagValue, m []byte) error
	ValidateMembershipVerbatimMachineReadable(m []byte) error
	ValidateMembershipVerbatimHumanReadable(name naming.StylableName) error
	ValidateMembershipParamsMachineReadable(m []byte) error
}

type VcsManagedContract struct {
}

func NewVcsManagedContract() *VcsManagedContract { panic("stub") }

func (inst *VcsManagedContract) ValidateTagValue(tv identifier.TagValue) error { panic("stub") }

func (inst *VcsManagedContract) ValidateNaturalKeyHumanReadable(tv identifier.TagValue, name naming.StylableName) error {
	panic("stub")
}

func (inst *VcsManagedContract) ValidateNaturalKeyMachineReadable(tv identifier.TagValue, m []byte) error {
	panic("stub")
}

func (inst *VcsManagedContract) ValidateMembershipVerbatimMachineReadable(m []byte) error {
	panic("stub")
}

func (inst *VcsManagedContract) ValidateMembershipVerbatimHumanReadable(name naming.StylableName) error {
	panic("stub")
}

func (inst *VcsManagedContract) ValidateMembershipParamsMachineReadable(m []byte) error {
	panic("stub")
}


```

--- FILE: stopa/naturalkey/lw_natuarlkey_enums.go ---
```go
package naturalkey

const (
	SerializationFormatCbor SerializationFormatE = 0
	SerializationFormatJson SerializationFormatE = 1
)

var AllSerializationFormats = []SerializationFormatE{SerializationFormatCbor, SerializationFormatJson}

const JsonSpecialValuePrefix = "3952d183f4183ad6:"


```

--- FILE: stopa/naturalkey/lw_naturalkey_encoder.go ---
```go
package naturalkey

import (
	_ "bytes"
	_ "errors"
	_ "slices"
	_ "strconv"
	_ "strings"
	"time"
	_ "time"

	_ "github.com/fxamacker/cbor/v2"
	_ "github.com/go-json-experiment/json"
	_ "github.com/go-json-experiment/json/jsontext"
	"github.com/stergiotis/boxer/public/identity/identifier"
	_ "github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/observability/eh"
	_ "github.com/stergiotis/boxer/public/observability/eh"
	_ "github.com/stergiotis/boxer/public/observability/eh/eb"
	_ "github.com/stergiotis/boxer/public/semistructured/cbor"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/naming"
)

func EncodeTaggedIdJson(id identifier.TaggedId) (r string) { panic("stub") }

func IsTaggedIdJson(s string) bool { panic("stub") }

func DecodeTaggedIdJson(s string) (id identifier.TaggedId) { panic("stub") }

func NewEncoder() *Encoder { panic("stub") }

func (inst *Encoder) Reset() { panic("stub") }

var ErrWrongState = eh.Errorf("wrong state")
var ErrInvalidArgument = eh.Errorf("invalid argument")

func (inst *Encoder) Begin() *Encoder { panic("stub") }

func (inst *Encoder) AddStr(v string) *Encoder { panic("stub") }

func (inst *Encoder) AddName(v naming.StylableName) *Encoder { panic("stub") }

func (inst *Encoder) AddKey(v naming.Key) *Encoder { panic("stub") }

func (inst *Encoder) AddBool(v bool) *Encoder { panic("stub") }

func (inst *Encoder) AddBytes(v []byte) *Encoder { panic("stub") }

func (inst *Encoder) AddTimeUTC(v time.Time) *Encoder { panic("stub") }

func (inst *Encoder) AddUint8(v uint8) *Encoder { panic("stub") }

func (inst *Encoder) AddUint16(v uint16) *Encoder { panic("stub") }

func (inst *Encoder) AddUint32(v uint32) *Encoder { panic("stub") }

func (inst *Encoder) AddId(v identifier.TaggedId) *Encoder { panic("stub") }

func (inst *Encoder) AddUint64(v uint64) *Encoder { panic("stub") }

func (inst *Encoder) AddInt8(v int8) *Encoder { panic("stub") }

func (inst *Encoder) AddInt16(v int16) *Encoder { panic("stub") }

func (inst *Encoder) AddInt32(v int32) *Encoder { panic("stub") }

func (inst *Encoder) AddInt64(v int64) *Encoder { panic("stub") }

func (inst *Encoder) EndAndResolve(resolver ResolverI, format SerializationFormatE) (id identifier.TaggedId, err error) {
	panic("stub")
}

func (inst *Encoder) EndAndGenerate(idGen identifier.IdGeneratorI, format SerializationFormatE) (id identifier.TaggedId, fresh bool, err error) {
	panic("stub")
}

func (inst *Encoder) EndAndGenerate2(idGen identifier.IdGeneratorI, format SerializationFormatE) (id identifier.TaggedId, key []byte, fresh bool, err error) {
	panic("stub")
}

func (inst *Encoder) End(format SerializationFormatE) (naturalKey []byte, err error) { panic("stub") }

// TODO encode on the fly...


```

--- FILE: stopa/naturalkey/lw_naturalkey_types.go ---
```go
package naturalkey

import (
	_ "bytes"

	_ "github.com/go-json-experiment/json/jsontext"
	"github.com/stergiotis/boxer/public/identity/identifier"
	_ "github.com/stergiotis/boxer/public/identity/identifier"
	_ "github.com/stergiotis/boxer/public/semistructured/cbor"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/naming"
)

type ResolverI interface {
	GetNaturalKeyId() (id identifier.TaggedId)
	ResolveNaturalKey(naturalKey []byte) (id identifier.TaggedId, err error)
	MustResolveNaturalKey(naturalKey []byte) (id identifier.TaggedId)
}
type ResolverHRI interface {
	GetNaturalKeyId() (id identifier.TaggedId)
	ResolveNaturalKeyHR(naturalKey naming.StylableName) (id identifier.TaggedId, err error)
	CanonicalizeNaturalKeyHR(naturalKey naming.StylableName) (canonicalized naming.StylableName)
	MustResolveNaturalKeyHR(naturalKey naming.StylableName) (id identifier.TaggedId)
}

type Encoder struct {
}
type SerializationFormatE uint8


```

--- FILE: stopa/registry/lw_registry_common.go ---
```go
package registry

import (
	_ "runtime"
	_ "runtime/debug"
	_ "strings"

	_ "github.com/stergiotis/boxer/public/unsafeperf"
)


```

--- FILE: stopa/registry/lw_registry_enums.go ---
```go
package registry

import (
	"iter"
	_ "iter"
	_ "math/bits"
	_ "strings"
)

const (
	MembershipValueNone       RegisteredValueFlagsE = 0b0000_0000
	MembershipValueFinal      RegisteredValueFlagsE = 0b0000_0001
	MembershipValueVirtual    RegisteredValueFlagsE = 0b0000_0010
	MembershipValueDeprecated RegisteredValueFlagsE = 0b0000_0100
)

var AllMembershipValues = []RegisteredValueFlagsE{
	MembershipValueNone,
	MembershipValueFinal,
	MembershipValueVirtual,
	MembershipValueDeprecated,
}

func (inst RegisteredValueFlagsE) Count() int { panic("stub") }

func (inst RegisteredValueFlagsE) Iterate() iter.Seq[RegisteredValueFlagsE] { panic("stub") }

func (inst RegisteredValueFlagsE) String() string { panic("stub") }

func (inst RegisteredValueFlagsE) HasVirtual() bool { panic("stub") }

func (inst RegisteredValueFlagsE) SetVirtual() RegisteredValueFlagsE { panic("stub") }

func (inst RegisteredValueFlagsE) ClearVirtual() RegisteredValueFlagsE { panic("stub") }

func (inst RegisteredValueFlagsE) HasFinal() bool { panic("stub") }

func (inst RegisteredValueFlagsE) SetFinal() RegisteredValueFlagsE { panic("stub") }

func (inst RegisteredValueFlagsE) ClearFinal() RegisteredValueFlagsE { panic("stub") }

func (inst RegisteredValueFlagsE) HasDeprecated() bool { panic("stub") }

func (inst RegisteredValueFlagsE) SetDeprecated() RegisteredValueFlagsE { panic("stub") }

func (inst RegisteredValueFlagsE) ClearDeprecated() RegisteredValueFlagsE { panic("stub") }


```

--- FILE: stopa/registry/lw_registry_nk.go ---
```go
package registry

import (
	"iter"
	_ "iter"

	_ "github.com/rs/zerolog/log"
	_ "github.com/stergiotis/boxer/public/containers"
	"github.com/stergiotis/boxer/public/identity/identifier"
	_ "github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/observability/eh"
	_ "github.com/stergiotis/boxer/public/observability/eh"
	_ "github.com/stergiotis/boxer/public/observability/eh/eb"
	_ "github.com/stergiotis/boxer/public/observability/vcs"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/common"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/stopa/contract"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/stopa/naturalkey"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/common"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/naming"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/stopa/contract"
)

func NewNaturalKeyRegistry[C contract.ContractI](tagValue identifier.TagValue, estSize int, namingStyle naming.NamingStyleE, untaggedOffset identifier.UntaggedId, contr C) (inst *HumanReadableNaturalKeyRegistry[C], err error) {
	panic("stub")
}

func MustNewNaturalKeyRegistry[C contract.ContractI](tagValue identifier.TagValue, estSize int, namingStyle naming.NamingStyleE, untaggedOffset identifier.UntaggedId, contr C) (inst *HumanReadableNaturalKeyRegistry[C]) {
	panic("stub")
}

func (inst *HumanReadableNaturalKeyRegistry[C]) Length() int { panic("stub") }

func (inst *HumanReadableNaturalKeyRegistry[C]) IterateAll() iter.Seq2[naming.StylableName, RegisteredNaturalKey] {
	panic("stub")
}

func (inst *HumanReadableNaturalKeyRegistry[C]) IterateAllRoots() iter.Seq2[naming.StylableName, RegisteredNaturalKey] {
	panic("stub")
}

func (inst *HumanReadableNaturalKeyRegistry[C]) MustBegin(nk naming.StylableName) (r RegisteredNaturalKeyDml) {
	panic("stub")
}

var ErrNotFound = eh.Errorf("item is not contained in registry")

func (inst *HumanReadableNaturalKeyRegistry[C]) Lookup(nk naming.StylableName) (r RegisteredNaturalKey, err error) {
	panic("stub")
}

func (inst *HumanReadableNaturalKeyRegistry[C]) GetTagValue() identifier.TagValue { panic("stub") }

func (inst *HumanReadableNaturalKeyRegistry[C]) Begin(nk naming.StylableName) (r RegisteredNaturalKeyDml, err error) {
	panic("stub")
}

// needed to deduplicate before .End()

func (inst RegisteredNaturalKey) GetModuleInfo() string { panic("stub") }

func (inst RegisteredNaturalKey) GetNaturalKey() naming.StylableName { panic("stub") }

func (inst RegisteredNaturalKey) GetTagValue() identifier.TagValue { panic("stub") }

func (inst RegisteredNaturalKey) GetId() identifier.TaggedId { panic("stub") }

func (inst RegisteredNaturalKey) GetOrigin() string { panic("stub") }

func (inst RegisteredNaturalKey) IterateAllChildren() iter.Seq2[identifier.TaggedId, RegisteredNaturalKey] {
	panic("stub")
}

func (inst RegisteredNaturalKey) IterateAllParents() iter.Seq2[identifier.TaggedId, RegisteredNaturalKey] {
	panic("stub")
}

func (inst RegisteredNaturalKey) GetNumberOfRestrictions() (n int) { panic("stub") }

func (inst RegisteredNaturalKey) IterateRestrictionIndices() iter.Seq[int] { panic("stub") }

func (inst RegisteredNaturalKey) GetRestrictionCardinality(idx int) CardinalitySpecE { panic("stub") }

func (inst RegisteredNaturalKey) GetRestrictionSectionName(idx int) naming.StylableName {
	panic("stub")
}

func (inst RegisteredNaturalKey) GetRestrictionSectionMembership(idx int) common.MembershipSpecE {
	panic("stub")
}

func (inst RegisteredNaturalKey) GetFlags() RegisteredValueFlagsE { panic("stub") }

func (inst RegisteredNaturalKey) IsRoot() bool { panic("stub") }

func (inst RegisteredNaturalKey) IsLeaf() bool { panic("stub") }

func (inst RegisteredNaturalKey) GetParentsCount() int { panic("stub") }

func (inst RegisteredNaturalKey) GetChildrenCount() int { panic("stub") }

func (inst RegisteredNaturalKeyVirtual) GetNumberOfRestrictions() (n int) { panic("stub") }

func (inst RegisteredNaturalKeyVirtual) IterateRestrictionIndices() iter.Seq[int] { panic("stub") }

func (inst RegisteredNaturalKeyVirtual) GetRestrictionCardinality(idx int) CardinalitySpecE {
	panic("stub")
}

func (inst RegisteredNaturalKeyVirtual) GetRestrictionSectionName(idx int) naming.StylableName {
	panic("stub")
}

func (inst RegisteredNaturalKeyVirtual) GetRestrictionSectionMembership(idx int) common.MembershipSpecE {
	panic("stub")
}

func (inst RegisteredNaturalKeyVirtual) GetFlags() RegisteredValueFlagsE { panic("stub") }

func (inst RegisteredNaturalKeyVirtual) IsRoot() bool { panic("stub") }

func (inst RegisteredNaturalKeyVirtual) IsLeaf() bool { panic("stub") }

func (inst RegisteredNaturalKeyVirtual) GetParentsCount() int { panic("stub") }

func (inst RegisteredNaturalKeyVirtual) GetChildrenCount() int { panic("stub") }

func (inst RegisteredNaturalKeyVirtual) GetModuleInfo() string { panic("stub") }

func (inst RegisteredNaturalKeyVirtual) GetNaturalKey() naming.StylableName { panic("stub") }

func (inst RegisteredNaturalKeyVirtual) GetOrigin() string { panic("stub") }

func (inst RegisteredNaturalKeyVirtual) GetId() identifier.TaggedId { panic("stub") }

func (inst RegisteredNaturalKeyVirtual) GetTagValue() identifier.TagValue { panic("stub") }

func (inst RegisteredNaturalKeyVirtual) IterateAllChildren() iter.Seq2[identifier.TaggedId, RegisteredNaturalKey] {
	panic("stub")
}

func (inst RegisteredNaturalKeyVirtual) IterateAllParents() iter.Seq2[identifier.TaggedId, RegisteredNaturalKey] {
	panic("stub")
}

func (inst RegisteredNaturalKeyFinal) GetNumberOfRestrictions() (n int) { panic("stub") }

func (inst RegisteredNaturalKeyFinal) IterateRestrictionIndices() iter.Seq[int] { panic("stub") }

func (inst RegisteredNaturalKeyFinal) GetRestrictionCardinality(idx int) CardinalitySpecE {
	panic("stub")
}

func (inst RegisteredNaturalKeyFinal) GetRestrictionSectionName(idx int) naming.StylableName {
	panic("stub")
}

func (inst RegisteredNaturalKeyFinal) GetRestrictionSectionMembership(idx int) common.MembershipSpecE {
	panic("stub")
}

func (inst RegisteredNaturalKeyFinal) GetFlags() RegisteredValueFlagsE { panic("stub") }

func (inst RegisteredNaturalKeyFinal) IsRoot() bool { panic("stub") }

func (inst RegisteredNaturalKeyFinal) IsLeaf() bool { panic("stub") }

func (inst RegisteredNaturalKeyFinal) GetParentsCount() int { panic("stub") }

func (inst RegisteredNaturalKeyFinal) GetChildrenCount() int { panic("stub") }

func (inst RegisteredNaturalKeyFinal) GetModuleInfo() string { panic("stub") }

func (inst RegisteredNaturalKeyFinal) GetNaturalKey() naming.StylableName { panic("stub") }

func (inst RegisteredNaturalKeyFinal) GetOrigin() string { panic("stub") }

func (inst RegisteredNaturalKeyFinal) GetId() identifier.TaggedId { panic("stub") }

func (inst RegisteredNaturalKeyFinal) IterateAllChildren() iter.Seq2[identifier.TaggedId, RegisteredNaturalKey] {
	panic("stub")
}

func (inst RegisteredNaturalKeyFinal) IterateAllParents() iter.Seq2[identifier.TaggedId, RegisteredNaturalKey] {
	panic("stub")
}

func (inst RegisteredNaturalKeyFinal) GetTagValue() identifier.TagValue { panic("stub") }

func (inst RegisteredNaturalKeyConcrete) GetNumberOfRestrictions() (n int) { panic("stub") }

func (inst RegisteredNaturalKeyConcrete) IterateRestrictionIndices() iter.Seq[int] { panic("stub") }

func (inst RegisteredNaturalKeyConcrete) GetRestrictionCardinality(idx int) CardinalitySpecE {
	panic("stub")
}

func (inst RegisteredNaturalKeyConcrete) GetRestrictionSectionName(idx int) naming.StylableName {
	panic("stub")
}

func (inst RegisteredNaturalKeyConcrete) GetRestrictionSectionMembership(idx int) common.MembershipSpecE {
	panic("stub")
}

func (inst RegisteredNaturalKeyConcrete) GetFlags() RegisteredValueFlagsE { panic("stub") }

func (inst RegisteredNaturalKeyConcrete) IsRoot() bool { panic("stub") }

func (inst RegisteredNaturalKeyConcrete) IsLeaf() bool { panic("stub") }

func (inst RegisteredNaturalKeyConcrete) GetParentsCount() int { panic("stub") }

func (inst RegisteredNaturalKeyConcrete) GetChildrenCount() int { panic("stub") }

func (inst RegisteredNaturalKeyConcrete) GetModuleInfo() string { panic("stub") }

func (inst RegisteredNaturalKeyConcrete) GetNaturalKey() naming.StylableName { panic("stub") }

func (inst RegisteredNaturalKeyConcrete) GetOrigin() string { panic("stub") }

func (inst RegisteredNaturalKeyConcrete) IterateAllChildren() iter.Seq2[identifier.TaggedId, RegisteredNaturalKey] {
	panic("stub")
}

func (inst RegisteredNaturalKeyConcrete) IterateAllParents() iter.Seq2[identifier.TaggedId, RegisteredNaturalKey] {
	panic("stub")
}

func (inst RegisteredNaturalKeyConcrete) GetId() identifier.TaggedId { panic("stub") }

func (inst RegisteredNaturalKeyConcrete) GetTagValue() identifier.TagValue { panic("stub") }


```

--- FILE: stopa/registry/lw_registry_nk_dml1.go ---
```go
package registry

import (
	_ "github.com/rs/zerolog/log"
	_ "github.com/stergiotis/boxer/public/compiletimeflags"
	_ "github.com/stergiotis/boxer/public/observability/eh/eb"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/common"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/common"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/naming"
)

func (inst RegisteredNaturalKeyDml) MustAddParents(parents ...RegisteredNaturalKey) (r RegisteredNaturalKeyDml) {
	panic("stub")
}

func (inst RegisteredNaturalKeyDml) MustAddParentsVirtual(parents ...RegisteredNaturalKeyVirtual) (r RegisteredNaturalKeyDml) {
	panic("stub")
}

func (inst RegisteredNaturalKeyDml) AddParents(parents ...RegisteredNaturalKey) (r RegisteredNaturalKeyDml, err error) {
	panic("stub")
}

func (inst RegisteredNaturalKeyDml) AddParentsVirtual(parents ...RegisteredNaturalKeyVirtual) (r RegisteredNaturalKeyDml, err error) {
	panic("stub")
}

func (inst RegisteredNaturalKeyDml) MustAddRestriction(sectionName naming.StylableName, membershipSpec common.MembershipSpecE, cardinality CardinalitySpecE) RegisteredNaturalKeyDml {
	panic("stub")
}

func (inst RegisteredNaturalKeyDml) SetVirtual() RegisteredNaturalKeyVirtualDml { panic("stub") }

func (inst RegisteredNaturalKeyDml) SetFinal() RegisteredNaturalKeyFinalDml { panic("stub") }

func (inst RegisteredNaturalKeyDml) SetDeprecated() RegisteredNaturalKeyDml { panic("stub") }

func (inst RegisteredNaturalKeyDml) ClearDeprecated() RegisteredNaturalKeyDml { panic("stub") }

func (inst RegisteredNaturalKeyDml) End() RegisteredNaturalKey { panic("stub") }


```

--- FILE: stopa/registry/lw_registry_nk_dml2.go ---
```go
package registry

import (
	_ "github.com/rs/zerolog/log"
	_ "github.com/stergiotis/boxer/public/compiletimeflags"
	_ "github.com/stergiotis/boxer/public/observability/eh/eb"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/common"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/common"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/naming"
)

func (inst RegisteredNaturalKeyVirtualDml) MustAddParents(parents ...RegisteredNaturalKey) (r RegisteredNaturalKeyVirtualDml) {
	panic("stub")
}

func (inst RegisteredNaturalKeyVirtualDml) MustAddParentsVirtual(parents ...RegisteredNaturalKeyVirtual) (r RegisteredNaturalKeyVirtualDml) {
	panic("stub")
}

func (inst RegisteredNaturalKeyVirtualDml) AddParents(parents ...RegisteredNaturalKey) (r RegisteredNaturalKeyVirtualDml, err error) {
	panic("stub")
}

func (inst RegisteredNaturalKeyVirtualDml) AddParentsVirtual(parents ...RegisteredNaturalKeyVirtual) (r RegisteredNaturalKeyVirtualDml, err error) {
	panic("stub")
}

func (inst RegisteredNaturalKeyVirtualDml) MustAddRestriction(sectionName naming.StylableName, membershipSpec common.MembershipSpecE, cardinality CardinalitySpecE) RegisteredNaturalKeyVirtualDml {
	panic("stub")
}

func (inst RegisteredNaturalKeyVirtualDml) ClearVirtual() RegisteredNaturalKeyDml { panic("stub") }

func (inst RegisteredNaturalKeyVirtualDml) SetDeprecated() RegisteredNaturalKeyVirtualDml {
	panic("stub")
}

func (inst RegisteredNaturalKeyVirtualDml) ClearDeprecated() RegisteredNaturalKeyVirtualDml {
	panic("stub")
}

func (inst RegisteredNaturalKeyVirtualDml) End() RegisteredNaturalKeyVirtual { panic("stub") }


```

--- FILE: stopa/registry/lw_registry_nk_dml3.go ---
```go
package registry

import (
	_ "github.com/rs/zerolog/log"
	_ "github.com/stergiotis/boxer/public/compiletimeflags"
	_ "github.com/stergiotis/boxer/public/observability/eh/eb"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/common"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/common"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/naming"
)

func (inst RegisteredNaturalKeyFinalDml) MustAddParents(parents ...RegisteredNaturalKey) (r RegisteredNaturalKeyFinalDml) {
	panic("stub")
}

func (inst RegisteredNaturalKeyFinalDml) MustAddParentsVirtual(parents ...RegisteredNaturalKeyVirtual) (r RegisteredNaturalKeyFinalDml) {
	panic("stub")
}

func (inst RegisteredNaturalKeyFinalDml) AddParents(parents ...RegisteredNaturalKey) (r RegisteredNaturalKeyFinalDml, err error) {
	panic("stub")
}

func (inst RegisteredNaturalKeyFinalDml) AddParentsVirtual(parents ...RegisteredNaturalKeyVirtual) (r RegisteredNaturalKeyFinalDml, err error) {
	panic("stub")
}

func (inst RegisteredNaturalKeyFinalDml) MustAddRestriction(sectionName naming.StylableName, membershipSpec common.MembershipSpecE, cardinality CardinalitySpecE) RegisteredNaturalKeyFinalDml {
	panic("stub")
}

func (inst RegisteredNaturalKeyFinalDml) ClearFinal() RegisteredNaturalKeyDml { panic("stub") }

func (inst RegisteredNaturalKeyFinalDml) SetDeprecated() RegisteredNaturalKeyFinalDml { panic("stub") }

func (inst RegisteredNaturalKeyFinalDml) ClearDeprecated() RegisteredNaturalKeyFinalDml {
	panic("stub")
}

func (inst RegisteredNaturalKeyFinalDml) End() RegisteredNaturalKeyFinal { panic("stub") }


```

--- FILE: stopa/registry/lw_registry_tv.go ---
```go
package registry

import (
	_ "cmp"
	"iter"
	_ "iter"

	_ "github.com/rs/zerolog/log"
	_ "github.com/stergiotis/boxer/public/containers"
	"github.com/stergiotis/boxer/public/identity/identifier"
	_ "github.com/stergiotis/boxer/public/identity/identifier"
	_ "github.com/stergiotis/boxer/public/observability/eh"
	_ "github.com/stergiotis/boxer/public/observability/eh/eb"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/stopa/contract"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/stopa/naturalkey"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/naming"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/stopa/contract"
)

func NewTagValueRegistry[C contract.ContractI](offset identifier.TagValue, estSize int, namingStyle naming.NamingStyleE, contr C) (inst *MembershipTagValueRegistry[C], err error) {
	panic("stub")
}

func MustNewTagValueRegistry[C contract.ContractI](offset identifier.TagValue, namingStyle naming.NamingStyleE, estSize int, contr C) (inst *MembershipTagValueRegistry[C]) {
	panic("stub")
}

func (inst RegisteredTagValue) GetFlags() RegisteredValueFlagsE { panic("stub") }

func (inst RegisteredTagValue) GetTagValue() identifier.TagValue { panic("stub") }

func (inst RegisteredTagValue) GetOrigin() string { panic("stub") }

func (inst *MembershipTagValueRegistry[C]) IterateAll() iter.Seq2[identifier.IdTag, RegisteredTagValue] {
	panic("stub")
}

func (inst *MembershipTagValueRegistry[C]) GetRecordByTagValue(tv identifier.TagValue) (r RegisteredTagValue, has bool) {
	panic("stub")
}

func (inst *MembershipTagValueRegistry[C]) GetRecordByTag(tg identifier.IdTag) (r RegisteredTagValue, has bool) {
	panic("stub")
}

func (inst *MembershipTagValueRegistry[C]) HasRecordByTag(tg identifier.IdTag) (has bool) {
	panic("stub")
}

func (inst *MembershipTagValueRegistry[C]) HasRecordByTagValue(tv identifier.TagValue) (has bool) {
	panic("stub")
}

func (inst RegisteredTagValue) GetModuleInfo() string { panic("stub") }

func (inst RegisteredTagValue) GetNaturalKey() naming.StylableName { panic("stub") }

func (inst RegisteredTagValue) GetId() identifier.TaggedId { panic("stub") }

func (inst RegisteredTagValueDml) SetDeprecated() RegisteredTagValueDml { panic("stub") }

func (inst RegisteredTagValueDml) ClearDeprecated() RegisteredTagValueDml { panic("stub") }

func (inst *MembershipTagValueRegistry[C]) Length() int { panic("stub") }

func (inst *MembershipTagValueRegistry[C]) GetOffset() identifier.TagValue { panic("stub") }

func (inst *MembershipTagValueRegistry[C]) MustBegin(naturalKey naming.StylableName, tv identifier.TagValue) (r RegisteredTagValueDml) {
	panic("stub")
}

func (inst *MembershipTagValueRegistry[C]) Begin(nk naming.StylableName, tv identifier.TagValue) (r RegisteredTagValueDml, err error) {
	panic("stub")
}

func (inst RegisteredTagValueDml) End() RegisteredTagValue { panic("stub") }


```

--- FILE: stopa/registry/lw_registry_types.go ---
```go
package registry

import (
	_ "fmt"
	"iter"
	_ "iter"

	_ "github.com/stergiotis/boxer/public/containers"
	"github.com/stergiotis/boxer/public/identity/identifier"
	_ "github.com/stergiotis/boxer/public/identity/identifier"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/common"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/stopa/contract"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/stopa/naturalkey"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/common"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/naming"
	"github.com/stergiotis/pebble2impl/doc/skills/leeway-advanced/references/leeway/boxer/stopa/contract"
)

type RegisteredItemLineageI interface {
	GetModuleInfo() string
	GetOrigin() string
}
type RegisteredItemRestrictionsI interface {
	GetNumberOfRestrictions() (n int)
	IterateRestrictionIndices() iter.Seq[int]
	GetRestrictionCardinality(idx int) CardinalitySpecE
	GetRestrictionSectionName(idx int) naming.StylableName
	GetRestrictionSectionMembership(idx int) common.MembershipSpecE
}
type RegisteredItemIdentifierI interface {
	GetId() identifier.TaggedId
	GetTagValue() identifier.TagValue
	GetNaturalKey() naming.StylableName
}
type RegisteredItemI interface {
	RegisteredItemLineageI
	RegisteredItemRestrictionsI
	RegisteredItemIdentifierI
	IterateAllParents() iter.Seq2[identifier.TaggedId, RegisteredNaturalKey]
	IterateAllChildren() iter.Seq2[identifier.TaggedId, RegisteredNaturalKey]
	GetParentsCount() int
	GetChildrenCount() int
	IsRoot() bool
	IsLeaf() bool
}
type RegisteredItemDmlUseI[R1 any, R2 any] interface {
	MustAddParents(parents ...RegisteredNaturalKey) R1
	MustAddParentsVirtual(parents ...RegisteredNaturalKeyVirtual) R1
	AddParents(parents ...RegisteredNaturalKey) (R1, error)
	AddParentsVirtual(parents ...RegisteredNaturalKeyVirtual) (R1, error)

	MustAddRestriction(sectionName naming.StylableName, membershipSpec common.MembershipSpecE, card CardinalitySpecE) R1
	SetDeprecated() R1
	ClearDeprecated() R1

	End() R2
}

type CardinalitySpecE uint8

const (
	CardinalityZeroToOne  CardinalitySpecE = 0
	CardinalityExactlyOne CardinalitySpecE = 1
	CardinalityOneOrMore  CardinalitySpecE = 2
	CardinalityArbitrary  CardinalitySpecE = 3
)

type RegisteredNaturalKey struct {
}

type RegisteredNaturalKeyConcrete struct {
}

type RegisteredNaturalKeyVirtual struct {
}

type RegisteredNaturalKeyFinal struct {
}

type RegisteredNaturalKeyDml struct {
}

type RegisteredNaturalKeyVirtualDml struct {
}

type RegisteredNaturalKeyFinalDml struct {
}

type RegisteredTagValue struct {
}
type RegisteredTagValueDml struct {
}

type HumanReadableNaturalKeyRegistry[C contract.ContractI] struct {
}
type RegisteredValueFlagsE uint8

type MembershipTagValueRegistry[C contract.ContractI] struct {
}


```

--- FILE: useaspects/lw_useaspects_encoder.go ---
```go
// Code generated by copy paste; DO NOT EDIT.
package useaspects

import (
	"iter"
	_ "iter"
	_ "math/bits"

	_ "github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh"
	_ "github.com/stergiotis/boxer/public/observability/eh"
	_ "github.com/stergiotis/boxer/public/observability/eh/eb"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/base62"
)

const EmptyAspectSet = AspectSet("0")

var ErrInvalidEncoding = eh.Errorf("encoding is wrong")
var ErrEmptySet = eh.Errorf("encoding contains empty set")

func EncodeAspects(aspects ...AspectE) (encoded AspectSet, err error) { panic("stub") }

func EncodeAspectsIgnoreInvalid(aspects ...AspectE) (encoded AspectSet) { panic("stub") }

func EncodeAspectsMustValidate(aspects ...AspectE) (encoded AspectSet) { panic("stub") }

func MaxEncodedAspect(encoded AspectSet) (aspect AspectE, err error) { panic("stub") }

func CountEncodedAspects(encoded AspectSet) (n int, err error) { panic("stub") }

func IterateAspects(encoded AspectSet) iter.Seq2[int, AspectE] { panic("stub") }

func UnionAspects(asp1 AspectSet, asp2 AspectSet) (res AspectSet, err error) { panic("stub") }

func UnionAspectsIgnoreInvalid(asp1 AspectSet, asp2 AspectSet) (res AspectSet) { panic("stub") }

func (inst AspectSet) String() string { panic("stub") }

func (inst AspectSet) IsValid() bool { panic("stub") }

func (inst AspectSet) IsEmptySet() bool { panic("stub") }

func (inst AspectSet) UnionAspectsIgnoreInvalid(asp2 AspectSet) (res AspectSet) { panic("stub") }

func (inst AspectSet) UnionAspects(asp2 AspectSet) (res AspectSet, err error) { panic("stub") }

func (inst AspectSet) IterateAspects() iter.Seq2[int, AspectE] { panic("stub") }

func (inst AspectSet) CountEncodedAspects() (n int, err error) { panic("stub") }

func (inst AspectSet) MaxEncodedAspect() (aspect AspectE, err error) { panic("stub") }


```

--- FILE: useaspects/lw_useaspects_enum.go ---
```go
package useaspects

import (
	"slices"
	_ "slices"
)

const (
	AspectIndefinite         AspectE = 0
	AspectCompliance         AspectE = 1
	AspectRisk               AspectE = 2
	AspectPrivacy            AspectE = 3
	AspectProvenanceEntity   AspectE = 4 // see https://www.w3.org/TR/prov-overview/
	AspectProvenanceActivity AspectE = 5 // see https://www.w3.org/TR/prov-overview/
	AspectProvenanceAgent    AspectE = 6 // see https://www.w3.org/TR/prov-overview/
	AspectProvenanceRelation AspectE = 7 // see https://www.w3.org/TR/prov-overview/
	AspectLineage            AspectE = 8
	AspectCatalog            AspectE = 9
	AspectSecurity           AspectE = 10
	AspectAuthorization      AspectE = 11
	AspectAccess             AspectE = 12
	AspectAudit              AspectE = 13
	AspectQuality            AspectE = 14
	AspectPolicy             AspectE = 15
	AspectOwnership          AspectE = 16
	AspectMetrics            AspectE = 17
	AspectLog                AspectE = 18
	AspectCollaboration      AspectE = 19
	AspectInterop            AspectE = 20
	AspectEvolution          AspectE = 21
	AspectClassification     AspectE = 22
	AspectTaxonomy           AspectE = 23
	AspectUnit               AspectE = 24 // e.g. SI unit
	AspectProfile            AspectE = 25 // i.e. performance profiling data
	AspectSpatial            AspectE = 26
	AspectOrgUnit            AspectE = 27
	AspectOrgRole            AspectE = 28
	AspectOrgProcess         AspectE = 29
	AspectOrgFinance         AspectE = 30
	AspectBusinessAsset      AspectE = 31
	AspectBusinessPartner    AspectE = 32
	AspectBusinessActivity   AspectE = 33
	AspectBusinessChannel    AspectE = 34
	AspectWorkflow           AspectE = 35
	AspectLinking            AspectE = 36 // i.e. references, hyperlinks, graph edges, hyper edges ...
	AspectTesting            AspectE = 37
	AspectDevice             AspectE = 38
	AspectDocumentation      AspectE = 39
	AspectObservability      AspectE = 40

	AspectCodeSourceOfTruth                       AspectE = 41
	AspectDataSourceOfTruth                       AspectE = 42
	AspectExternalSourceOfTruth                   AspectE = 43
	AspectMiniDimension                           AspectE = 44
	AspectSlowlyChangingDimensionRetainOriginal   AspectE = 45 // i.e. type 0, see https://www.kimballgroup.com/data-warehouse-business-intelligence-resources/kimball-techniques/dimensional-modeling-techniques/type-0/
	AspectSlowlyChangingDimensionOverwrite        AspectE = 46 // i.e. type 1, see https://www.kimballgroup.com/data-warehouse-business-intelligence-resources/kimball-techniques/dimensional-modeling-techniques/type-1/
	AspectSlowlyChangingDimensionAddNewRecord     AspectE = 47 // i.e. type 2, add new row, see https://www.kimballgroup.com/data-warehouse-business-intelligence-resources/kimball-techniques/dimensional-modeling-techniques/type-2/
	AspectSlowlyChangingDimensionAddNewAttribute  AspectE = 48 // i.e. type 3, add new attribute, see https://www.kimballgroup.com/data-warehouse-business-intelligence-resources/kimball-techniques/dimensional-modeling-techniques/type-3/
	AspectSlowlyChangingDimensionAddMiniDimension AspectE = 49 // i.e. type 4, add mini dimension, see https://www.kimballgroup.com/data-warehouse-business-intelligence-resources/kimball-techniques/dimensional-modeling-techniques/type-4-mini-dimension/
	AspectSlowlyChangingDimensionType5            AspectE = 50 // i.e. type 5, add mini and type 1 outrigger, see https://www.kimballgroup.com/data-warehouse-business-intelligence-resources/kimball-techniques/dimensional-modeling-techniques/type-5/
	AspectSlowlyChangingDimensionType6            AspectE = 51 // i.e. type 6, add type 1 attributes to type 2 dimension, see https://www.kimballgroup.com/data-warehouse-business-intelligence-resources/kimball-techniques/dimensional-modeling-techniques/type-6/
	AspectSlowlyChangingDimensionType7            AspectE = 52 // i.e. type 7, dual type 1 and type 2 dimension, see https://www.kimballgroup.com/data-warehouse-business-intelligence-resources/kimball-techniques/dimensional-modeling-techniques/type-7/
	AspectQualityStaging                          AspectE = 53 // i.e. Bronze in medaillon architecture
	AspectQualityCore                             AspectE = 54 // i.e. Silver in medaillon architecture
	AspectQualitySemantical                       AspectE = 55 // i.e. Gold in medaillon architecture
)

var MaxAspectExcl = slices.Max(AllAspects) + 1

var AllAspects = []AspectE{
	AspectIndefinite,
	AspectCompliance,
	AspectRisk,
	AspectPrivacy,
	AspectProvenanceEntity,
	AspectProvenanceActivity,
	AspectProvenanceAgent,
	AspectProvenanceRelation,
	AspectLineage,
	AspectCatalog,
	AspectSecurity,
	AspectAuthorization,
	AspectAccess,
	AspectAudit,
	AspectQuality,
	AspectPolicy,
	AspectOwnership,
	AspectMetrics,
	AspectLog,
	AspectCollaboration,
	AspectInterop,
	AspectEvolution,
	AspectClassification,
	AspectTaxonomy,
	AspectUnit,
	AspectProfile,
	AspectSpatial,
	AspectOrgUnit,
	AspectOrgRole,
	AspectOrgProcess,
	AspectOrgFinance,
	AspectBusinessAsset,
	AspectBusinessPartner,
	AspectBusinessActivity,
	AspectBusinessChannel,
	AspectWorkflow,
	AspectLinking,
	AspectTesting,
	AspectDevice,
	AspectDocumentation,
	AspectObservability,
}

const InvalidAspectEnumValueString = "<invalid AspectE>"

func (inst AspectE) IsValid() bool { panic("stub") }

func (inst AspectE) String() string { panic("stub") }

func (inst AspectE) Value() uint8 { panic("stub") }


```

--- FILE: useaspects/lw_useaspects_types.go ---
```go
package useaspects

import _ "fmt"

type AspectSet string

type CanonicalEt7AspectCoder struct {
}

type AspectE uint8


```

--- FILE: valueaspects/lw_valueaspects_encoder.go ---
```go
// Code generated by copy paste; DO NOT EDIT.
package valueaspects

import (
	"iter"
	_ "iter"
	_ "math/bits"

	_ "github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh"
	_ "github.com/stergiotis/boxer/public/observability/eh"
	_ "github.com/stergiotis/boxer/public/observability/eh/eb"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/base62"
)

const EmptyAspectSet = AspectSet("0")

var ErrInvalidEncoding = eh.Errorf("encoding is wrong")
var ErrEmptySet = eh.Errorf("encoding contains empty set")

func EncodeAspects(aspects ...AspectE) (encoded AspectSet, err error) { panic("stub") }

func EncodeAspectsIgnoreInvalid(aspects ...AspectE) (encoded AspectSet) { panic("stub") }

func EncodeAspectsMustValidate(aspects ...AspectE) (encoded AspectSet) { panic("stub") }

func MaxEncodedAspect(encoded AspectSet) (aspect AspectE, err error) { panic("stub") }

func CountEncodedAspects(encoded AspectSet) (n int, err error) { panic("stub") }

func IterateAspects(encoded AspectSet) iter.Seq2[int, AspectE] { panic("stub") }

func UnionAspects(asp1 AspectSet, asp2 AspectSet) (res AspectSet, err error) { panic("stub") }

func UnionAspectsIgnoreInvalid(asp1 AspectSet, asp2 AspectSet) (res AspectSet) { panic("stub") }

func (inst AspectSet) String() string { panic("stub") }

func (inst AspectSet) IsValid() bool { panic("stub") }

func (inst AspectSet) IsEmptySet() bool { panic("stub") }

func (inst AspectSet) UnionAspectsIgnoreInvalid(asp2 AspectSet) (res AspectSet) { panic("stub") }

func (inst AspectSet) UnionAspects(asp2 AspectSet) (res AspectSet, err error) { panic("stub") }

func (inst AspectSet) IterateAspects() iter.Seq2[int, AspectE] { panic("stub") }

func (inst AspectSet) CountEncodedAspects() (n int, err error) { panic("stub") }

func (inst AspectSet) MaxEncodedAspect() (aspect AspectE, err error) { panic("stub") }


```

--- FILE: valueaspects/lw_valueaspects_enum.go ---
```go
package valueaspects

import (
	"slices"
	_ "slices"
)

const (
	AspectNone                             AspectE = 0
	AspectScaleOfMeasurementNominal        AspectE = 1
	AspectScaleOfMeasurementOrdinal        AspectE = 2
	AspectScaleOfMeasurementMetricInterval AspectE = 3
	AspectScaleOfMeasurementMetricRatio    AspectE = 4
	AspectVectorValue                      AspectE = 5
	AspectCanonicalizedValue               AspectE = 6
	AspectApplicationLevelEncryption       AspectE = 7
	AspectApplicationLevelCompression      AspectE = 8
	AspectHumanReadable                    AspectE = 9
	AspectMachineReadable                  AspectE = 10
	AspectUltraShortLifespan               AspectE = 11
	AspectShortLifespan                    AspectE = 12
	AspectMediumLifespan                   AspectE = 13
	AspectLongLifespan                     AspectE = 14
	AspectUltraLongLifespan                AspectE = 15
	AspectJsonScalar                       AspectE = 16
	AspectJsonArray                        AspectE = 17
	AspectJsonObject                       AspectE = 18
	AspectJson                             AspectE = 19
	AspectCborScalar                       AspectE = 20
	AspectCborArray                        AspectE = 21
	AspectCborMap                          AspectE = 22
	AspectCbor                             AspectE = 23
	AspectUrl                              AspectE = 24 // follow the WHATWG recommendation to forget URI and use URL (see https://url.spec.whatwg.org/#goals)
	AspectFeature                          AspectE = 25
	AspectFeatureOneHot                    AspectE = 26
	AspectFeatureScalingStandardN01        AspectE = 27
	AspectFeatureScalingMinMax01           AspectE = 28
	AspectFeatureScalingRobust01           AspectE = 29
	AspectFeatureBinarized                 AspectE = 30
	AspectFeatureOrdinal                   AspectE = 31
	AspectFeatureLabel                     AspectE = 32
	AspectMachineLearningEmbedding         AspectE = 33
	AspectIdNaturalKey                     AspectE = 34
	AspectIdSurrogateKey                   AspectE = 35
	AspectIdDurableSuperNaturalKey         AspectE = 36
	AspectIdContentAddressableKey          AspectE = 37
	AspectTextUnicodeNormalizedNfd         AspectE = 38 // Normalization Form Canonical Decomposition
	AspectTextUnicodeNormalizedNfc         AspectE = 39 // Normalization Form Canonical Composition
	AspectTextUnicodeNormalizedNfkd        AspectE = 40 // Normalization Form Compatibility Decomposition
	AspectTextUnicodeNormalizedNfkc        AspectE = 41 // Normalization Form Compatibility Composition
	AspectTextUnicodeCaseFolded            AspectE = 42 // Normalization Form Compatibility Composition
	AspectTextUnicodeCaseInsensitive       AspectE = 43
	AspectTextUnicodeLocaleSensitive       AspectE = 44
	AspectTextUnicodeMayBeBidi             AspectE = 45
	AspectHumanGenerated                   AspectE = 46
	AspectMachineGenerate                  AspectE = 47
	AspectBinaryCodedDecimal               AspectE = 48 // BCD see https://en.wikipedia.org/wiki/Binary-coded_decimal, note that there are many incompatible encodings
	AspectReflectedBinaryCode              AspectE = 49 // see https://en.wikipedia.org/wiki/Gray_code
	AspectTrinaryLogic                     AspectE = 50 // see https://en.wikipedia.org/wiki/Three-valued_logic
	AspectGraphVertex                      AspectE = 51
	AspectGraphEdge                        AspectE = 52
	AspectHyperGraphEdge                   AspectE = 53
	AspectAnonymized                       AspectE = 54
	AspectMandatory                        AspectE = 55
	AspectOptional                         AspectE = 56
	AspectEmulatedMembershipVerbatim       AspectE = 57
	AspectEmulatedMembershipRef            AspectE = 58
	AspectEmulatedMembershipParams         AspectE = 59
	AspectEmulatedMembershipRefWithParams  AspectE = 60
)

var MaxAspectExcl = slices.Max(AllAspects) + 1

var AllAspects = []AspectE{
	AspectNone,
	AspectScaleOfMeasurementNominal,
	AspectScaleOfMeasurementOrdinal,
	AspectScaleOfMeasurementMetricInterval,
	AspectScaleOfMeasurementMetricRatio,
	AspectVectorValue,
	AspectCanonicalizedValue,
	AspectApplicationLevelEncryption,
	AspectApplicationLevelCompression,
	AspectHumanReadable,
	AspectMachineReadable,
	AspectUltraShortLifespan,
	AspectShortLifespan,
	AspectMediumLifespan,
	AspectLongLifespan,
	AspectUltraLongLifespan,
	AspectJsonScalar,
	AspectJsonArray,
	AspectJsonObject,
	AspectJson,
	AspectCborScalar,
	AspectCborArray,
	AspectCborMap,
	AspectCbor,
	AspectUrl,
	AspectFeature,
	AspectFeatureOneHot,
	AspectFeatureScalingStandardN01,
	AspectFeatureScalingMinMax01,
	AspectFeatureScalingRobust01,
	AspectFeatureBinarized,
	AspectFeatureOrdinal,
	AspectFeatureLabel,
	AspectMachineLearningEmbedding,
	AspectIdNaturalKey,
	AspectIdSurrogateKey,
	AspectIdDurableSuperNaturalKey,
	AspectIdContentAddressableKey,
	AspectTextUnicodeNormalizedNfd,
	AspectTextUnicodeNormalizedNfc,
	AspectTextUnicodeNormalizedNfkd,
	AspectTextUnicodeNormalizedNfkc,
	AspectTextUnicodeCaseFolded,
	AspectTextUnicodeCaseInsensitive,
	AspectTextUnicodeLocaleSensitive,
	AspectTextUnicodeMayBeBidi,
	AspectHumanGenerated,
	AspectMachineGenerate,
	AspectBinaryCodedDecimal,
	AspectReflectedBinaryCode,
	AspectTrinaryLogic,
	AspectGraphVertex,
	AspectGraphEdge,
	AspectHyperGraphEdge,
	AspectAnonymized,
	AspectMandatory,
	AspectOptional,
	AspectEmulatedMembershipVerbatim,
	AspectEmulatedMembershipRef,
	AspectEmulatedMembershipParams,
	AspectEmulatedMembershipRefWithParams,
}

const InvalidAspectEnumValueString = "<invalid AspectE>"

func (inst AspectE) IsValid() bool { panic("stub") }

func (inst AspectE) String() string { panic("stub") }

func (inst AspectE) Value() uint8 { panic("stub") }


```

--- FILE: valueaspects/lw_valueaspects_types.go ---
```go
package valueaspects

import _ "fmt"

type AspectSet string

type AspectE uint8


```
