---
type: reference
audience: agent reading this skill asset
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.
# Leeway Table Structure - Complete Type Definition Reference

## Overview
Leeway represents semi-structured tables with:
- **Plain value columns**: Entity identifiers, timestamps, routing, lifecycle, transactions, opaque columns
- **Tagged value sections**: Named groups with membership specifications (low/high cardinality, verbatim/parameterized refs)
- **Aspect sets**: Encoding hints, value semantics, use aspects
- **Co-arrays and streaming groups**: For related column semantics

---

## Core Table Descriptor Types

### TableDesc (In-Memory Representation)
**Location**: `public/semistructured/leeway/common/lw_types.go:111-122`

```
struct TableDesc {
  DictionaryEntry              TableDictionaryEntryDescDto
  
  // Plain value columns (parallel co-arrays)
  PlainValuesNames            []StylableName
  PlainValuesTypes            []canonicaltypes.PrimitiveAstNodeI
  PlainValuesEncodingHints    []encodingaspects.AspectSet
  PlainValuesItemTypes        []PlainItemTypeE
  PlainValuesValueSemantics   []valueaspects.AspectSet
  OpaqueStreamingGroup        Key
  
  // Tagged value sections
  TaggedValuesSections        []TaggedValuesSection
}
```

### TableDescDto (CBOR Serialization)
**Location**: `public/semistructured/leeway/common/lw_types.go:124-157`

Flattens PlainItemTypeE into separate arrays:
- EntityId{Names,Types,EncodingHints,ValueSemantics}
- EntityTimestamp{Names,Types,EncodingHints,ValueSemantics}
- EntityRouting{Names,Types,EncodingHints,ValueSemantics}
- EntityLifecycle{Names,Types,EncodingHints,ValueSemantics}
- Transaction{Names,Types,EncodingHints,ValueSemantics}
- OpaqueColumn{Names,Types,EncodingHints,ValueSemantics}
- TaggedValuesSections: []TaggedValuesSectionDto

### TableDictionaryEntryDescDto
**Location**: `public/semistructured/leeway/common/lw_types.go:107-110`

```
struct TableDictionaryEntryDescDto {
  Name     StylableName
  Comment  string
}
```

---

## Tagged Value Section Types

### TaggedValuesSection (In-Memory)
**Location**: `public/semistructured/leeway/common/lw_types.go:171-182`

```
struct TaggedValuesSection {
  Name               StylableName
  UseAspects         useaspects.AspectSet
  CoSectionGroup     Key
  StreamingGroup     Key
  
  // Value columns (co-arrays)
  ValueColumnNames   []StylableName
  ValueColumnTypes   []canonicaltypes.PrimitiveAstNodeI
  ValueEncodingHints []encodingaspects.AspectSet
  ValueSemantics     []valueaspects.AspectSet
  
  MembershipSpec     MembershipSpecE
}
```

**Validation**: `IsValid()` checks (line 12-20 lw_column.go):
- Length match: len(ValueColumnNames) == len(ValueColumnTypes) > 0
- Each column has valid type and non-empty name

### TaggedValuesSectionDto (CBOR)
**Location**: `public/semistructured/leeway/common/lw_types.go:159-169`

```
struct TaggedValuesSectionDto {
  Name                     StylableName
  UseAspects               useaspects.AspectSet
  CoSectionGroup           Key
  StreamingGroup           Key
  ValueColumnNames         []StylableName
  ValueColumnTypes         []string          // Serialized canonical types
  ValueColumnEncodingHints []encodingaspects.AspectSet
  ValueSemantics           []valueaspects.AspectSet
  MembershipSpec           MembershipSpecE
}
```

---

## Plain Item Types Enum
**Location**: `public/semistructured/leeway/common/lw_enums.go:379-419`

```
PlainItemTypeE (uint8):
  PlainItemTypeNone            = 0
  PlainItemTypeEntityId        = 1
  PlainItemTypeEntityTimestamp = 2
  PlainItemTypeEntityRouting   = 3
  PlainItemTypeEntityLifecycle = 4
  PlainItemTypeTransaction     = 5
  PlainItemTypeOpaque          = 6
  
AllPlainItemTypes = [all above values]
MaxPlainItemTypeExcl = 7 (count of AllMembershipSpecs)
```

---

## Membership Specification Enum
**Location**: `public/semistructured/leeway/common/lw_enums.go:171-330`

Bitfield (uint8) for composable membership types:

```
MembershipSpecE (uint8) flags:
  MembershipSpecNone                                   = 0b0000_0000
  MembershipSpecHighCardRef                           = 0b0000_0001
  MembershipSpecHighCardVerbatim                      = 0b0000_0010
  MembershipSpecHighCardRefParametrized               = 0b0000_0100
  MembershipSpecLowCardRef                            = 0b0001_0000
  MembershipSpecLowCardVerbatim                       = 0b0010_0000
  MembershipSpecLowCardRefParametrized                = 0b0100_0000
  MembershipSpecMixedLowCardRefHighCardParameters     = 0b0000_1000
  MembershipSpecMixedLowCardVerbatimHighCardParameters = 0b1000_0000
  
AllMembershipSpecs = [all above]
```

**Methods**:
- Has*()/Add*()/Clear*() for each spec type
- `Count()`: bit population count
- `Iterate()`: iterate set flags
- `ContainsMixed()`: check for mixed cardinality specs

**String representations**:
- "high-card-ref", "high-card-verbatim", "high-card-ref-parametrized"
- "low-card-ref", "low-card-verbatim", "low-card-ref-parametrized"
- "low-card-ref-high-card-params"
- "low-card-verbatim-high-card-params"

---

## Column Role Enum
**Location**: `public/semistructured/leeway/common/lw_enums.go:17-74`

String enum (ColumnRoleE = string):

```
Base membership roles:
  ColumnRoleHighCardRef                     = "hr"
  ColumnRoleHighCardRefParametrized         = "hp"
  ColumnRoleHighCardVerbatim                = "hv"
  ColumnRoleLowCardRef                      = "lr"
  ColumnRoleLowCardRefParametrized          = "lp"
  ColumnRoleLowCardVerbatim                 = "lv"
  ColumnRoleMixedLowCardRef                 = "lmr"
  ColumnRoleMixedVerbatimHighCardParameters = "mvhp"
  ColumnRoleMixedRefHighCardParameters      = "mrhp"
  ColumnRoleMixedLowCardVerbatim            = "lmv"

Value/metadata roles:
  ColumnRoleValue                           = "val"
  ColumnRoleLength                          = "len"
  ColumnRoleCardinality                     = "card"
  ColumnRoleCusumLength                     = "cusumlen"
  ColumnRoleCusumCardinality                = "cusumcard"

Cardinality suffixes (composed):
  ColumnRoleHighCardRefCardinality          = "hrcard"
  ColumnRoleHighCardRefParametrizedCardinality = "hpcard"
  ColumnRoleHighCardVerbatimCardinality     = "hvcard"
  ColumnRoleLowCardRefCardinality           = "lrcard"
  ColumnRoleLowCardRefParametrizedCardinality = "lpcard"
  ColumnRoleLowCardVerbatimCardinality      = "lvcard"
  ColumnRoleMixedLowCardRefCardinality      = "lmrcard"
  ColumnRoleMixedLowCardVerbatimCardinality = "lmvcard"

Special:
  ColumnRoleUnspecific                      = ""
```

**Methods**:
- `String()`: returns string value
- `LongString()`: human-readable "high-card-ref", "low-card-verbatim", etc.
- `ParseColumnRole(string)`: parse from string

---

## Table Row Configuration Enum
**Location**: `public/semistructured/leeway/common/lw_enums.go:355-377`

```
TableRowConfigE (uint8):
  TableRowConfigMultiAttributesPerRow = 0
  
AllTableRowConfigs = [TableRowConfigMultiAttributesPerRow]
```

**Methods**:
- `IsValid()`: check if valid value
- `String()`: "multi-attributes-per-row"

---

## Implementation Status Enum
**Location**: `public/semistructured/leeway/common/lw_enums.go:331-353`

```
ImplementationStatusE (uint8):
  ImplementationStatusNotImplemented = 0
  ImplementationStatusPartial        = 127  (math.MaxUint8 >> 1)
  ImplementationStatusFull           = 255  (math.MaxUint8)
  
AllImplementationStatus = [all above]
```

**String representations**:
- "not-implemented", "partially-implemented", "fully-implemented"

---

## Intermediate Representation Types

### IntermediateTableRepresentation (CBOR)
**Location**: `public/semistructured/leeway/common/lw_types.go:70-73`

```
struct IntermediateTableRepresentation {
  PlainValueDesc  []*IntermediatePlainValuesDesc
  TaggedValueDesc []*IntermediateTaggedValuesDesc
}
```

### IntermediatePlainValuesDesc
**Location**: `public/semistructured/leeway/common/lw_types.go:60-68`

```
struct IntermediatePlainValuesDesc {
  Scalar                          *IntermediateColumnProps
  NonScalarHomogenousArray        *IntermediateColumnProps
  NonScalarHomogenousArraySupport *IntermediateColumnProps
  NonScalarSet                    *IntermediateColumnProps
  NonScalarSetSupport             *IntermediateColumnProps
  
  StreamingGroup                  Key
  ItemType                        PlainItemTypeE
}
```

### IntermediateTaggedValuesDesc
**Location**: `public/semistructured/leeway/common/lw_types.go:47-59`

```
struct IntermediateTaggedValuesDesc {
  SectionName                     StylableName
  UseAspects                      useaspects.AspectSet
  
  Scalar                          *IntermediateColumnProps
  NonScalarHomogenousArray        *IntermediateColumnProps
  NonScalarHomogenousArraySupport *IntermediateColumnProps
  NonScalarSet                    *IntermediateColumnProps
  NonScalarSetSupport             *IntermediateColumnProps
  Membership                      *IntermediateColumnProps
  MembershipSupport               *IntermediateColumnProps
  
  CoSectionGroup                  Key
  StreamingGroup                  Key
}
```

### IntermediateColumnProps
**Location**: `public/semistructured/leeway/common/lw_types.go:39-46`

```
struct IntermediateColumnProps {
  Names              []StylableName  (cbor:"names")
  Roles              []ColumnRoleE   (cbor:"roles")
  CanonicalType      []canonicaltypes.PrimitiveAstNodeI (cbor:"canonicalType")
  EncodingHints      []encodingaspects.AspectSet (cbor:"encodingHints")
  ValueSemantics     []valueaspects.AspectSet (cbor:"valueSemantics")
}
```

**Co-array invariant**: all slices must have equal length

### IntermediateColumnContext
**Location**: `public/semistructured/leeway/common/lw_types.go:22-37`

```
struct IntermediateColumnContext {
  Scope              IntermediateColumnScopeE
  SubType            IntermediateColumnSubTypeE
  
  StreamingGroup     Key  // empty for plain sections
  SectionName        StylableName  // empty for plain sections
  UseAspects         useaspects.AspectSet
  CoSectionGroup     Key  // empty for plain sections
  IndexOffset        uint32
  
  PlainItemType      PlainItemTypeE
}
```

---

## Intermediate Column Scopes & Subtypes

### IntermediateColumnScopeE (String enum)
**Location**: `public/semistructured/leeway/common/lw_enums.go:421-452`

```
IntermediateColumnScopeE (string):
  IntermediateColumnScopeEntity      = "entity"
  IntermediateColumnScopeTransaction = "transaction"
  IntermediateColumnScopeOpaque      = "opaque"
  IntermediateColumnScopeTagged      = "tagged"
  
AllIntermediateColumnScopes = [all above]
```

### IntermediateColumnSubTypeE (String enum)
**Location**: `public/semistructured/leeway/common/lw_enums.go:454-494`

```
IntermediateColumnSubTypeE (string):
  IntermediateColumnsSubTypeScalar                 = "scalar"
  IntermediateColumnsSubTypeHomogenousArray        = "homogenous-array"
  IntermediateColumnsSubTypeHomogenousArraySupport = "homogenous-array-support"
  IntermediateColumnsSubTypeSet                    = "set"
  IntermediateColumnsSubTypeSetSupport             = "set-support"
  IntermediateColumnsSubTypeMembership             = "membership"
  IntermediateColumnsSubTypeMembershipSupport      = "membership-support"
  
AllIntermediateColumnsSubTypes = [all above]
```

**Mapping from PlainItemTypeE to Scope**:
- PlainItemTypeEntityId/Timestamp/Routing/Lifecycle → IntermediateColumnScopeEntity
- PlainItemTypeTransaction → IntermediateColumnScopeTransaction
- PlainItemTypeOpaque → IntermediateColumnScopeOpaque
- (None) → IntermediateColumnScopeTagged

---

## Supporting Types

### StylableName
**Location**: `public/semistructured/leeway/naming/lw_naming_types.go:6`

```
type StylableName string
```

A name that can be transformed to different naming styles without losing descriptive/referencing/uniqueness properties.

### Key
**Location**: `public/semistructured/leeway/naming/lw_naming_types.go:10`

```
type Key string
```

For CoSectionGroup and StreamingGroup identifiers. Must be valid per `Key.Validate()`.

### Aspect Sets (String-based encoding)

#### encodingaspects.AspectSet
**Location**: `public/semistructured/leeway/encodingaspects/lw_encodinghints_types.go:5`

```
type AspectSet string
```

Encodes encoding hints (compression strategies, optimization hints). Has methods:
- `IsValid()`
- `UnionAspectsIgnoreInvalid()`
- `IterateAspects()`

#### useaspects.AspectSet
**Location**: `public/semistructured/leeway/useaspects/lw_useaspects_types.go:5`

```
type AspectSet string
```

Encodes use context/semantics.

#### valueaspects.AspectSet
**Location**: `public/semistructured/leeway/valueaspects/lw_valueaspects_types.go:5`

```
type AspectSet string
```

Encodes value semantics.

### PrimitiveAstNodeI
**Location**: `public/semistructured/leeway/canonicaltypes/canonicaltypes_types.go:42-50`

```
interface PrimitiveAstNodeI {
  IsStringNode() bool
  IsTemporalNode() bool
  IsMachineNumericNode() bool
  IsNetworkNode() bool
  IsScalar() bool
  GenerateGoCode(io.Writer) error
  AstNodeI  // cbor.Marshaler + IsSignature/IsPrimitive/IsValid/IterateMembers/fmt.Stringer
}
```

Represents canonical types as AST nodes (String, Temporal, MachineNumeric, Network types).

---

## Physical Column Descriptor (Bridge to Physical Schema)

### PhysicalColumnDesc
**Location**: `public/semistructured/leeway/common/lw_types.go:183-188`

```
struct PhysicalColumnDesc {
  GeneratingNamingConvention NamingConventionI
  Comment                    string
  NameComponents             []string
  NameComponentsExplanation  []string
}
```

**Methods** (lw_column.go:23-61):
- `GetCanonicalType()`: Extract from naming convention
- `GetEncodingHints()`
- `GetTableRowConfig()`
- `GetPlainItemType()`
- `GetSectionName()`
- `GetLeewayColumnName()`
- `String()`: Join NameComponents
- `IsValid()`: Check NameComponents length match + convention can extract type

---

## Naming Convention Interfaces

### NamingConventionI
**Location**: `public/semistructured/leeway/common/lw_types.go:238-241`

Combines forward and backward mapping:

```
interface NamingConventionI {
  // Forward: Leeway → Physical
  MapIntermediateToPhysicalColumns(
    IntermediateColumnContext,
    IntermediateColumnProps,
    []PhysicalColumnDesc,
    TableRowConfigE
  ) ([]PhysicalColumnDesc, error)
  
  // Backward: Physical → Leeway
  ExtractCanonicalType(PhysicalColumnDesc) (canonicaltypes.PrimitiveAstNodeI, error)
  ExtractEncodingHints(PhysicalColumnDesc) (encodingaspects.AspectSet, error)
  ExtractValueSemantics(PhysicalColumnDesc) (valueaspects.AspectSet, error)
  ExtractTableRowConfig(PhysicalColumnDesc) (TableRowConfigE, error)
  ExtractPlainItemType(PhysicalColumnDesc) (PlainItemTypeE, error)
  ExtractSectionName(PhysicalColumnDesc) (StylableName, error)
  ExtractLeewayColumnName(PhysicalColumnDesc) (StylableName, error)
  ParseColumn(string) (PhysicalColumnDesc, error)
  
  DiscoverTableFromPhysicalColumns([]PhysicalColumnDesc) (TableDesc, TableRowConfigE, error)
  DiscoverTableFromColumnNames([]string) (TableDesc, TableRowConfigE, error)
}
```

---

## Table Manipulation (Fluent API)

### TableManipulator
**Location**: `public/semistructured/leeway/common/lw_types.go:253-262`

```
struct TableManipulator {
  marshaller                *TableMarshaller
  buffer                    *bytes.Buffer
  validator                 *TableValidator
  sectionNameToIndex        map[string]int
  table                     *TableDesc
  plainValueItemNameToIndex []map[string]int  // Lookup by PlainItemTypeE
  upsertedCount             int
  receivedInvalidAspects    bool
}
```

**Fluent API** (TableManipulatorFluidI):
- `TaggedValueSection(StylableName) TaggedValueSectionMerger`
- `PlainValueColumn(PlainItemTypeE, StylableName, canonicaltypes.PrimitiveAstNodeI) PlainValueColumnMerger`
- `Reset()`

**Builders**:
- `SetTableName(StylableName) *TableManipulator`
- `SetTableComment(string) *TableManipulator`
- `AddPlainValueItem(...) *TableManipulator`
- `MergeTaggedValueSection(...) *TableManipulator`
- `MergeTaggedValueColumn(...) *TableManipulator`
- `MergeTable(*TableDesc) error`
- `LoadFromIntermediates(iter.Seq2[IntermediateColumnContext, *IntermediateColumnProps]) error`
- `BuildTableDesc() (TableDesc, error)`
- `BuildTableDescDto() (TableDescDto, error)`

---

## Validation (Constraints)

### TableValidator
**Location**: `public/semistructured/leeway/common/lw_types.go:242-247`

```
struct TableValidator {
  duplicatedNames  *containers.HashSet[string]
  usedNamingStyles []uint32
  possibleNames    []string
  errors           []error
}
```

**Validation Rules** (lw_table_validator.go:21-171):

1. **Names** (validateNames):
   - StylableName.Validate() passes
   - Each name matches at least one supported naming style
   - No duplicates across all naming style variants
   - All names follow same naming style (consistency)

2. **Names vs Types** (validateNamesTypes):
   - len(names) == len(types)
   - validateNames(names) passes
   - Each type.IsValid() == true

3. **Plain Columns** (validateTable):
   - validateNamesTypes(PlainValuesNames, PlainValuesTypes)

4. **Tagged Sections** (validateSection):
   - SectionName.Validate() passes
   - UseAspects.IsValid()
   - validateNamesTypes(ValueColumnNames, ValueColumnTypes)
   - Each ValueEncodingHints[i].IsValid()
   - StreamingGroup.Validate() (if non-empty)
   - CoSectionGroup.Validate() (if non-empty)

5. **Section Names** (validateNames):
   - All section names validate and follow naming styles
   - No duplicates

6. **OpaqueStreamingGroup** (if non-empty):
   - OpaqueStreamingGroup.Validate()

---

## Key Design Invariants

1. **Co-arrays**: PlainValuesNames, PlainValuesTypes, PlainValuesEncodingHints, PlainValuesItemTypes, PlainValuesValueSemantics must always have equal length

2. **Co-arrays in sections**: ValueColumnNames, ValueColumnTypes, ValueEncodingHints, ValueSemantics must always have equal length

3. **Membership specification**: Bitfield allows composing multiple membership types (up to 8 distinct specs)

4. **Aspect sets**: String-based encoding for extensibility; must pass IsValid() checks

5. **Naming consistency**: All column and section names must follow the same naming style variant

6. **Type validity**: All canonical types must pass IsValid() checks before serialization

7. **Intermediate→Physical mapping**: 1:1 correspondence required (len(intermediate.Names) == len(output physical columns))

8. **Scalar modifiers**: Extracted from base canonical type nodes (String, MachineNumeric, Temporal)

---

## Serialization Formats

- **CBOR**: Via TableMarshaller with cbor.EncMode/DecMode
- **Arrow**: Via IntermediateTableRepresentation.ToSchemaTable() for schema metadata
- **JSON**: TableDescDto supports json tags (alternate to cbor)

