---
type: reference
audience: agent reading this skill
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.
# Leeway - A columnar approach to semi structured data representation
Leeway is a way to represent semi structured data amenable to efficient columnar storage and processing technologies (e.g. DuckDB, ClickHouse, Arrow, data oriented programming). The approach aims to be fully code driven (i.e. having excellent tooling support).

## Key terms
The key terms used to describe the idea are:
* **Section** a group of columns that have to be interpreted as whole.
* **Plain Value** A scalar or non-scalar attribute of an entity. Plain values belong to one and only one plain item types.
* **Plain Item Types** Category to describe the purpose of a plain value. Fixed to be either entity, timestamp, routing, id, lifecycle, transaction or opaque. The plain item types group together all plain values of the same type, like a tagged value section does for tagged values.
* **Plain Value Specification** Schema describing a plain value column by a name, a canonical type, encoding hints and value semantics.
* **Tagged Value** A scalar or non-scalar attribute of an entity which can be tagged by associating the attribute value instance to belong to a set. This association is called membership. Each tagged value instance can belong to zero or many sets.
* **Tagged Value Section** A bundle of interrelated tagged value columns. The interrelation is, that all tagged value instances are co in the sense of a co-array to each other. Example: latitude, longitude. The membership data is contained within the section and applies to all tagged value columns within.
* **Tagged Value Section Specification** Schema describing a whole section of tagged values. Contains a name, a specification of possible membership types, use aspects, and a list of co-sections
* **Tagged Value Specification** Schema describing a tagged value column by a name, a canonical type, encoding hints, value semantics and a link to the containing tagged value section specification.
* **Membership Specification** Memberships describe tag like meta attribute associated with all co attributes instances in a tagged value section. The specification is a combination of two factors: low-/high cardinality and the value type of the membership itself. The value types are given and fixed: Verbatim, reference, parametrized reference, parameters.
* **Canonical Types** A succinct notation to describe canonical types in a technology neutral form. Defined are machine numeric types, stringlike types, temporal types.
* **Canonical Type Specification** A tuple describing the canonical type.
  * For machine numeric types: `(u|i|f,8|16|32|64,-|l|n,-|h|m)`. Meaning of the letters: u = unsigned, i = signed, f = float, 8 = bitwide, - = none, l = little endian, n = big endian/network, h = homogenous array, m = set. 
  * The specification for stringlike is `(s|y|b,-|f,-|*,-|h|m)` with s=utf8 string, y = byte blob, b = bit(string), f = fixed width,* = width in bytes if fixed, h = homogenous array, m = set.
* **Co-Sections**: Tagged value sections can be co to each other. This means, that they always contain the same number of attributes. This for example allows to shard memberships by defining a co-section without values and only memberships. Or it allows to have multiple representations for different purposes: e.g. Geo coordinates as lat/lng pairs in one section and h3 values in another without having to duplicated the memberships.
* **Streaminggroups**: While columnar databases have risen in the past few years the message oriented transport is still row based. By declaring which tagged values sections must be streamed together (as a subsetted table) we enable splitting and merging entities for eventg streaming.
* **Subsetting**: As long as all plain value sections are preserved tables can be sliced vertically at the (co)-section boundary. Opaque plain value columns are handled specially.
* **Leeway Datamarts**: In order to enable the composition of leeway informed tools with general purpose data tooling we may want to add read oriented "opaque" columns that are conventionally modeled.
* **Physical Columns**: By enforcing a name scheme that captures all information of a leeway schema in a structured form we can easily restore the leeway table definition from a given selection of physical columns (as long as it is properly "subsetting"). As an example: This allows leeway aware tools to rely on the canonical types prevents tedious mappings between type representations.
 
## Naming
All schemata are nominal and not structural when compared. 
All names are so called "stylable names" that can be written in camel case (lower/upper), snake case (lower/upper), spinal case (lower/upper).
The names are checked to not collide under all combinations of styles.

## Example: Mapping JSON to a equivalent Leeway representation
Consider as a first illustrative example the mapping of json to a leeway table definition (using the leeway go sdk):
```go
func LoadJsonMapping(manip common.TableManipulatorFluidI) {
	manip.PlainValueColumn(common.PlainItemTypeEntityId, "blake3hash", ctabb.Y).
		AddColumnEncodingHints(enchint.AspectLightGeneralCompression)
	manip.TaggedValueSection("bool").
		AddSectionMembership(common.MembershipSpecMixedLowCardVerbatimHighCardParameters).
		TaggedValueColumn("value", ctabb.B)
	manip.TaggedValueSection("undefined").
		AddSectionMembership(common.MembershipSpecMixedLowCardVerbatimHighCardParameters)
	manip.TaggedValueSection("null").
		AddSectionMembership(common.MembershipSpecMixedLowCardVerbatimHighCardParameters)
	manip.TaggedValueSection("string").
		AddSectionMembership(common.MembershipSpecMixedLowCardVerbatimHighCardParameters).
		TaggedValueColumn("value", ctabb.S).
		AddColumnEncodingHints(enchint.AspectLightGeneralCompression)
	manip.TaggedValueSection("symbol").
		AddSectionMembership(common.MembershipSpecMixedLowCardVerbatimHighCardParameters).
		TaggedValueColumn("value", ctabb.S).
		AddColumnEncodingHints(enchint.AspectLightGeneralCompression,
			enchint.AspectInterRecordLowCardinality,
			enchint.AspectIntraRecordLowCardinality)
	manip.TaggedValueSection("float64").
		AddSectionMembership(common.MembershipSpecMixedLowCardVerbatimHighCardParameters).
		TaggedValueColumn("value", ctabb.F64)
	manip.TaggedValueSection("int64").
		AddSectionMembership(common.MembershipSpecMixedLowCardVerbatimHighCardParameters).
		TaggedValueColumn("value", ctabb.I64)
}
func LoadJsonMappingLossless(manip common.TableManipulatorFluidI) {
	LoadJsonMapping(manip)
	manip.TaggedValueSection("emptyObject").
		AddSectionMembership(common.MembershipSpecMixedLowCardVerbatimHighCardParameters)
	manip.TaggedValueSection("emptyArray").
		AddSectionMembership(common.MembershipSpecMixedLowCardVerbatimHighCardParameters)
}
```
Example data:
```json
// Entity ID: "hash-001"
{
  "hostname": "server-alpha",
  "tags": ["production", "eu-west"],
  "metrics": {
    "cpu": 45.5,
    "mem": 80,
    "error": null
  },
  "active": true
}
```
First we explode it into a list of path, value pairs (using / as key separator):
```json
[
  ["/hostname","server-alpha"],
  ["/tags/0","production"],
  ["/tags/1","eu-west"],
  ["/metrics/cpu", 45.5],
  ["/metrics/mem",80],
  ["/metrics/error",null],
  ["/active", true]
]
```
We then start separating the high- and low-cardinality part in the path by introducing a placeholder "_" for high-cardinality parameters:
```json
[
  ["/hostname",[],"server-alpha"],
  ["/tags/_",[0],"production"],
  ["/tags/_",[1],"eu-west"],
  ["/metrics/cpu",[], 45.5],
  ["/metrics/mem",[],80],
  ["/metrics/error",[],null],
  ["/active",[], true]
]
```
The outermost array contains an array of triples: Path-Low-Card, Path-High-Card-Parameters, Value.
Now we group the triples by the canonical type of the value. These correspond to the tagged value sections.
```json
{"string": [
  ["/hostname",[],"server-alpha"],
  ["/tags/_",[0],"production"],
  ["/tags/_",[1],"eu-west"]],
  "float64": [
    ["/metrics/cpu",[], 45.5]],
  "int64": [
    ["/metrics/mem",[],80]],
  "null": [
    ["/metrics/error",[]]],
  "bool": [
    ["/active",[], true]]
}
```
Note that "null" is fully defined by its section name and does not need the store the value itself (singleton).
Next we separate low-card  from high-card values. This is an optimization and needs known how that is not contained in a typical json schema. It can be inferred appreciatively from sample data. We only do this for "string" and name the low-card variant "symbol":
```json
{"string": [
  ["/hostname",[],"server-alpha"]],
  "symbol": [
    ["/tags/_",[0],"production"],
    ["/tags/_",[1],"eu-west"]],
  "float64": [
    ["/metrics/cpu",[], 45.5]],
  "int64": [
    ["/metrics/mem",[],80]],
  "null": [
    ["/metrics/error",[]]],
  "bool": [
    ["/active",[], true]]
}
```
Lets now transition to a more table like form. Note the nested arrays that bind together what belongs to  a single logical attribute:
```json
{
    "string": {"low-card-memberships": [["/hostname"]], "high-card-memberships": [[]], "values": [["server-alpha"]]},
    "symbol": {"low-card-memberships": [["/tags/_"],["/tags/_"]], "high-card-memberships": [[0],[1]], "values": [["production"],["eu-west"]]},
    "float64": {"low-card-memberships": [["/metrics/cpu"]], "high-card-memberships": [[]], "values": [[45.5]]},
    "int64": {"low-card-memberships": [["/metrics/mem"]], "high-card-memberships": [[]], "values": [[80]]},
    "null": {"low-card-memberships": [["/metrics/error"]], "high-card-memberships": [[]]},
    "bool": {"low-card-memberships": [["/active"]], "high-card-memberships": [[]], "values": [[true]]},
}
```
Now we need to map this to columns while allowing to generalize to
a) non-scalar values
b) multi memberships per attribute.
We therefore need a column "cardinality" per tagged section that give the number of values that belong to the attribute. Further we need a "membership cardinality" column for associating multiple memberships to a single (scalar/non-scalar) value.
Id did also include the `id` plain value section with the plain value column `blake3hash`.
```json
 {
    "id": {"blake3hash": {"values": ["hash-001"], "value-card": [1]}},
    "string": {"low-card-memberships": ["/hostname"], "high-card-memberships": [], "values": ["server-alpha"], "membership-card": [1], "value-card": [1]},
    "symbol": {"low-card-memberships": ["/tags/_","/tags/_"], "high-card-memberships": [0,1], "values": ["production","eu-west"], "membership-card": [1,1], "value-card": [1,1] },
    "float64": {"low-card-memberships": ["/metrics/cpu"], "high-card-memberships": [], "values": [45.5], "membership-card": [1], "value-card": [1] },
    "int64": {"low-card-memberships": ["/metrics/mem"], "high-card-memberships": [], "values": [80], "membership-card": [1], "value-card": [1] },
    "null": {"low-card-memberships": ["/metrics/error"], "high-card-memberships": [], "membership-card": [1]},
    "bool": {"low-card-memberships": ["/active"], "high-card-memberships": [], "values": [true], "membership-card": [1], "value-card": [1] },
}
```
The values column of  the symbol section would be mapped to a dictionary compressed column (i.e. low-cardinality in ClickHouse). This is a very important aspect highlighting that traditional treelike formats like JSON, CBOR, BSON, MsgPack are unable to capture.

A columnar technology which has one-dimensional arrays as first class citizen can easily represent and index this structure (object keys correspond to columns, e.g. "null.low-card-memberships","null.value-card").
The 1:1 mapping is now completed.

Lets now assume that we want to "label" error messages in the document without introducing new path and break schema. We could easily do that in leeway by adding a membership "errormsg" as a low-cardinality verbatim membership:
```json
 {
    "string": {"low-card-memberships": ["/hostname"], "high-card-memberships": [], "values": ["server-alpha"], "membership-card": [1], "value-card": [1]},
    "symbol": {"low-card-memberships": ["/tags/_","/tags/_"], "high-card-memberships": [0,1], "values": ["production","eu-west"], "membership-card": [1,1], "value-card": [1,1] },
    "float64": {"low-card-memberships": ["/metrics/cpu"], "high-card-memberships": [], "values": [45.5], "membership-card": [1], "value-card": [1] },
    "int64": {"low-card-memberships": ["/metrics/mem"], "high-card-memberships": [], "values": [80], "membership-card": [1], "value-card": [1] },
    "null": {"low-card-memberships": ["/metrics/error","errormsg"], "high-card-memberships": [], "membership-card": [2]},
    "bool": {"low-card-memberships": ["/active"], "high-card-memberships": [], "values": [true], "membership-card": [1], "value-card": [1] },
}
```

## Membership roles: primary vs secondary

In the labeling example above, `/metrics/error` and `errormsg` ride in the same `low-card-memberships` column with `membership-card: 2`. They are mechanically identical at the protocol layer but semantically different: `/metrics/error` *defines* what the attribute is, `errormsg` *annotates* it.

This distinction matters for downstream tooling — JSON Schema generation, data-contract field declarations, quality SQL, PII classification — but the protocol does not carry it. Per boxer ADR-0007 (`$(boxer-path)/doc/adr/0007-leeway-membership-role-classifier.md`; see also [pebble2impl ADR-0017](../../adr/0007-leeway-membership-role-classifier.md)), the role is decided by an application-supplied classifier that consumes value-level membership instances and returns a `(role, paramTreatment)` pair.

### Four-quadrant model

Per-membership classification is `MembershipRoleE × ParamTreatmentE`:

| Role | Param treatment | Example | Attribute key | Value shape |
|---|---|---|---|---|
| Primary | Identity | `/users/_/email` (UUID = identity) | `/users/abc-123/email` | scalar / values object |
| Primary | Index | `/embedding/_` (i = dim index) | `/embedding` | indexed list / vector |
| Secondary | Identity | `errormsg` with text param | label `{name, params}` |
| Secondary | Index | `errormsg` flag | label `name` |

Most real data uses three of the four cells. The classifier returns `ParamTreatmentNone` for non-parametrized memberships.

### Section uniformity hint

A section may be declared *uniform* in role: every membership in the section is primary, or every membership is secondary. The classifier honours the hint as a short-circuit. The hints ride two boxer use-aspects, `useaspects.AspectSectionMembershipsAllPrimary` and `useaspects.AspectSectionMembershipsAllSecondary`, accessed through `SectionDesc.UseAspects`.

The hint is *advisory*, not enforced. A classifier may override on a per-membership basis; validation tooling may cross-check. The protocol does not.

### Pattern: secondary co-section for annotation overlays

Co-sections without value columns are valid Leeway: a co-section can be membership-only. Combined with the uniformity hint, this yields a clean pattern for layering annotations on an existing primary section without changing its value columns or data. A co-section group forms when two or more sections share a `SectionCoSectionGroup` key (here `"null"`, by convention the primary's name), so both the primary and the annotation section carry that one tag:

```go
// Existing primary section — opted into the "null" co-group with a single
// tag; its value columns and rows are otherwise unchanged.
manip.TaggedValueSection("null").
    SectionCoSectionGroup("null").
    AddSectionMembership(common.MembershipSpecMixedLowCardVerbatimHighCardParameters).
    AddSectionUseAspects(useaspects.AspectSectionMembershipsAllPrimary)

// New co-section — pure annotations, no value columns, same co-group key
manip.TaggedValueSection("null__labels").
    SectionCoSectionGroup("null").
    AddSectionMembership(common.MembershipSpecLowCardVerbatim).
    AddSectionUseAspects(useaspects.AspectSectionMembershipsAllSecondary)
```

The shared attribute index ties primary value to secondary annotations. Vertical subsetting drops or keeps the secondary co-group whole. Annotation teams (PII, governance, ML labels) add their co-section and opt the primary into the shared co-group key — the primary's value columns and rows stay untouched.

This pattern is *one* organizational option, not a requirement. Mixed-role sections work equally well; the classifier handles them because it acts at value grain.

### Consequences for canonical JSON

Under boxer ADR-0007 (and pebble2impl [ADR-0017](../../adr/0007-leeway-membership-role-classifier.md)) the canonical JSON layout is attribute-centric: primary memberships form attribute keys, secondary memberships ride in a per-attribute `labels` slot. Aliasing (multiple primary memberships on one value, e.g. `/price/current` ≡ `/stats/min`) collapses into one attribute object with an `aliases` field. JSON Schema / data-contract generation reads the classifier output to decide `properties` + `required` for primary versus a scoped extension slot for secondary.

The classifier interface lives in boxer at `github.com/stergiotis/boxer/public/semistructured/leeway/membershiprole`.

## The membership model: three orthogonal planes

The section above covers *meaning*; this places it in the full model and shows how the two catalogs that follow (Column roles, Membership types) relate to the in-memory **channel** a codec dispatches on. A membership decomposes into three independent planes — carriage ⟂ meaning ⟂ representation — with a single coupling (boxer ADR-0070 §Concept basis; carriage in boxer ADR-0072 (`$(boxer-path)/doc/adr/0072-leeway-membership-carriage.md`), role in boxer ADR-0073, which supersedes ADR-0007):

- **Carriage** — how the identity rides the wire: the *channel*.
- **Meaning** — *role* (primary defines an attribute / secondary annotates) and *param-treatment* (Identity / Index / None), decided by the classifier (see "Membership roles" above).
- **Representation** — identity → display string, rendered read-time by a `membership.Renderer` (ref id → hex/name, verbatim → bytes, params → text). Distinct from scalar *value* formatting, which stays driver-side.

The one cross-plane coupling: **a section carries a single channel**, so the reader resolves one accessor per section instead of probing every channel.

### Carriage: the channel is a product

A channel is `cardinality × identity × params`; the realized set is eight cells of that grid. The *identity* axis is `membership.IdentityEncoding`, the one vocabulary shared by the write-side channel and the read-side value:

| Channel (`mappingplan.MembershipChannel`) | card. | identity (`IdentityEncoding`) | params | Schema spec (`MembershipSpecE`) | Physical cols (`ColumnRoleE`) |
|---|---|---|---|---|---|
| `LowCardRef` *(default)* | Low | `IdentityRef` | — | `LowCardRef` | `lr` |
| `LowCardVerbatim` | Low | `IdentityVerbatim` | — | `LowCardVerbatim` | `lv` |
| `HighCardRef` | High | `IdentityRef` | — | `HighCardRef` | `hr` |
| `HighCardVerbatim` | High | `IdentityVerbatim` | — | `HighCardVerbatim` | `hv` |
| `MixedLowCardRef` | Low | `IdentityPerRowId` | ✓ | `MixedLowCardRefHighCardParameters` | `lmr` + `mrhp` |
| `MixedLowCardVerbatim` | Low | `IdentityPerRowName` | ✓ | `MixedLowCardVerbatimHighCardParameters` | `lmv` + `mvhp` |
| `LowCardRefParametrized` | Low | `IdentityPerRowBlob` | ✓ | `LowCardRefParametrized` | `lp` |
| `HighCardRefParametrized` | High | `IdentityPerRowBlob` | ✓ | `HighCardRefParametrized` | `hp` |

A repeating membership adds a `…card` cardinality column (e.g. `lrcard`). The carrier channels (mixed / parametrized) split their per-row data across the two physical columns shown — a value half and a params half — which the driver recombines. So the `MembershipSpecE` bitfield (a section's *accepted* kinds) and the `ColumnRoleE` strings (physical Arrow columns) catalogued below are two projections of one channel.

### One identity vocabulary, read and write

`mappingplan.ChannelIdentityE` aliases `membership.IdentityEncoding`, and the read-side `membership.MembershipValue.Kind` *is* an `IdentityEncoding` — value and channel speak one language. The read value is **the channel minus cardinality**: each High/Low pair collapses to one of five shapes.

| Read value `Kind` | from channels | live fields |
|---|---|---|
| `IdentityRef` | LowCardRef, HighCardRef | `Ref` |
| `IdentityVerbatim` | LowCard/HighCardVerbatim | `Verbatim` |
| `IdentityPerRowId` | MixedLowCardRef | `Ref`, `Params` |
| `IdentityPerRowName` | MixedLowCardVerbatim | `Verbatim`, `Params` |
| `IdentityPerRowBlob` | …RefParametrized | `Params` (`Ref` = 0) |

`IdentityEncoding.HasParams()` is true for exactly the three `PerRow…` encodings, so the classifier derives param-treatment from it rather than re-listing them; the zero value `IdentityNone` marks an empty slot.

> Two `AddMembership*` families are easy to conflate: the DML *write builder* `AddMembership<Channel>P` (channel-keyed, e.g. `AddMembershipLowCardRefP`) and the read-out sink's `AddMembership<Shape>` (the five shapes that populate `MembershipValue.Kind`).

## Column roles
Here is a complete list of all column roles needed in leeway tables:
```go
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
```
## Membership types
Here is a complete list of all membership types available:
```go
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
func (inst MembershipSpecE) String() string {
	if inst == MembershipSpecNone {
		return "none"
	}
	l := inst.Count()
	if l == 1 {
		switch inst {
		case MembershipSpecHighCardRef:
			return "high-card-ref"
		case MembershipSpecHighCardVerbatim:
			return "high-card-verbatim"
		case MembershipSpecHighCardRefParametrized:
			return "high-card-ref-parametrized"
		case MembershipSpecLowCardRef:
			return "low-card-ref"
		case MembershipSpecLowCardVerbatim:
			return "low-card-verbatim"
		case MembershipSpecLowCardRefParametrized:
			return "low-card-ref-parametrized"
		case MembershipSpecMixedLowCardRefHighCardParameters:
			return "low-card-ref-high-card-params"
		case MembershipSpecMixedLowCardVerbatimHighCardParameters:
			return "low-card-verbatim-high-card-params"
		default:
			break
		}
	}
	s := strings.Builder{}
	i := 0
	for m := range inst.Iterate() {
		if i > 0 {
			_, _ = s.WriteString(" | ")
		}
		_, _ = s.WriteString(m.String())
		i++
	}
	return s.String()
}
```
## Encoding aspects
Encoding hints do describe the data encoding techniques in a technology neutral form:
```go
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
```
## Value aspects
Value aspects give semantical information to leeway enable tooling what operations to support for a given leeway column:
```go
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
```

## Worked examples

### The "Tensor" Case (Homogenous N-Dimensional Arrays)

**Scenario:** Machine Learning data. We have a document containing a 1D embedding vector and a 2D transformation matrix. We map these to a `float64` section (canonical type `f,64,-,h` implies the values are interpreted as arrays).

**Input Data:**
```json
{
  "id": "model-v1",
  "embedding": [0.1, 0.5, 0.9],         // 1x3 Vector
  "layers": [                           // Array of Matrices? No, let's keep it simple: List of Vectors
     [1.0, 0.0],                        // Layer 0 weights
     [0.0, 1.0]                         // Layer 1 weights
  ]
}
```

**Leeway Representation (Section: `float64array`):**
*Here, "Values" is a flattened stream of scalars. `value-card` tells the engine how to chunk them into logical units (vectors).*

```json
{
  "float64array": {
    "low-card-memberships": [
      "/embedding",   // Row 0, Item 0
      "/layers/_",    // Row 0, Item 1 (Layer 0)
      "/layers/_"     // Row 0, Item 2 (Layer 1)
    ],
    "high-card-parameters": [
      [],             // Embedding has no dynamic parameter
      [0],            // Index 0 of layers
      [1]             // Index 1 of layers
    ],
    "values": [
      0.1, 0.5, 0.9,  // Chunk 1 (embedding)
      1.0, 0.0,       // Chunk 2 (layer 0)
      0.0, 1.0        // Chunk 3 (layer 1)
    ],
    "value-card": [
      3,              // 3 scalars make up the embedding
      2,              // 2 scalars make up layer 0
      2               // 2 scalars make up layer 1
    ],
    "membership-card": [
      1, 1, 1         // Each logical vector corresponds to 1 tag
    ]
  }
}
```

**Why this is difficult:**
1.  **Variable Widths:** The column contains vectors of size 3 and size 2 mixed together. `value-card` handles this "ragged" tensor structure perfectly.
2.  **Structural Mixing:** Top-level fields (`embedding`) and nested array items (`layers[i]`) coexist in the same column, differentiated only by the `low-card-membership` path and the `high-card-parameter`.

### The "Multi-Membership" Case (Aliasing & Projections)

**Scenario:** An E-commerce product where a specific price point serves multiple semantic roles. We want to query it as "current_price", but also index it as "lowest_price_ever" without duplicating the float value.

**Input Data (Conceptual):**
*   Entity: `prod-001`
*   Price: `19.99`
*   Semantics: This value is `/price/current`, but also `/stats/min`, and belongs to `/promo/flash_sale`.

**Leeway Representation (Section: `float64`):**

```json
{
  "float64": {
    "low-card-memberships": [
      "/price/current", 
      "/stats/min", 
      "/promo/flash_sale"
    ],
    "high-card-parameters": [
      [], [], []    // No dynamic parameters (e.g. array indices) needed here
    ],
    "values": [
      19.99
    ],
    "value-card": [
      1             // It is a single scalar
    ],
    "membership-card": [
      3             // CRITICAL: This 1 value is associated with 3 tags
    ]
  }
}
```

**Why this is difficult:**
*   **Column Alignment:** In a standard format (Parquet), you would likely have three columns: `price_current`, `stats_min`, `promo_flash_sale`. Two would duplicate the value `19.99`.
*   **Leeway Efficiency:** Leeway stores `19.99` once. The overhead is in the metadata (memberships). This is effectively a **Graph Edge** list (`Entity -> Value -> [Relation1, Relation2, Relation3]`).

### The "Sparse Heterogeneous List" Case

**Scenario:** A messy event log where an array contains mixed types (Polymorphism).

**Input Data:**
```json
{
  "events": [
    { "val": 100 },         // Index 0: Integer
    { "val": "error" },     // Index 1: String
    { "val": 101 },         // Index 2: Integer
    { "val": [1.1, 1.2] }   // Index 3: Float Array (Nested!)
  ]
}
```

**Leeway Representation (Split across Sections):**

**Section: `int64`**
```json
{
  "low-card-memberships": ["/events/_/val", "/events/_/val"],
  "high-card-parameters": [[0], [2]],  // Captures indices 0 and 2
  "values": [100, 101],
  "value-card": [1, 1]
}
```

**Section: `symbol` (String)**
```json
{
  "low-card-memberships": ["/events/_/val"],
  "high-card-parameters": [[1]],       // Captures index 1
  "values": ["error"],
  "value-card": [1]
}
```

**Section: `float64array` (Nested Array)**
```json
{
  "low-card-memberships": ["/events/_/val"],
  "high-card-parameters": [[3]],       // Captures index 3
  "values": [1.1, 1.2],
  "value-card": [2],                   // 2 scalars in this unit
  "membership-card": [1]
}
```

**Why this is difficult:**
*   **Reconstruction:** To rebuild the JSON `events` array, the reader must scan `int64`, `symbol`, and `float64array`.
*   **Synchronization:** The `high-card-parameters` (0, 1, 2, 3) are the only thing holding the order together.
*   **Topology:** Index 3 (`[1.1, 1.2]`) is logically a *nested* array inside the *events* array. Leeway flattens this. The `value-card: 2` on the `float64array` section implies the inner nesting, while the `high-card-parameter: [3]` implies the outer position.

## Go sdk
The go sdk for leeway contains the following capabilities:
* Canonical types
  * Parser: Parse the terse syntax (e.g. `u64lh`) into a go struct
  * Validator: Check validity of canonical type
  * Generator: Generating random or exhaustive examples
* Data-Definition-Language DDL:
  * Fluid API to define Leeway tables
  * Technology specific mappings to DDL code (e.g. SQL):
    * ClickHouse
    * Apache Arrow
  * Measurment of the coverage of the technology specific mapping process
  * Naming convention to embedd all schema information in the names of physical columns
* Generator support:
  * Intermediate format and various helpers to generate go and arrow-go code
* Data-Manipulation-Language DML:
  * Generator: Generate go code that provides a high-level API to insert/ingest data into a Leeway table
* Readaccess RA:
  * Generator: Generate go code that provides a high-level API to read and iterate attributes from a Leeway table