---
name: leeway-beginner
description: "Use to learn Leeway fundamentals — the backbone (plain values) vs payload (tagged values) architecture and basic columnar data modelling."
type: reference
audience: agent reading this skill
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.
# Leeway: A Structural-Shredding Columnar Protocol

## 1. Abstract
Leeway is a **schema-on-write** data representation protocol designed to bridge the gap between semi-structured data (JSON/Document) and high-performance columnar storage (DuckDB, Arrow, ClickHouse).

It decouples **Topology** (hierarchy/nesting) from **Content** (values). Instead of storing domain-specific objects, Leeway shreds data into type-specific **Sections**, managing structure via **Memberships** and **Cardinality Vectors**. This enables "Zero-Copy" schema evolution, vectorized processing of sparse data, and seamless transition between row-oriented transport (Streaming) and column-oriented storage (OLAP).

---

## 2. Core Architecture

Leeway partitions data into two orthogonal planes: the **Backbone** (Plain Values) and the **Payload** (Tagged Values).

### 2.1 Plain Values (The Backbone)
Attributes that define the existence, identity, and lifecycle of an entity. They map 1:1 to the logical row and are mandatory for structural integrity.

| Item Type | Description | Physical Type |
| :--- | :--- | :--- |
| **Entity** | Primary Identity (e.g., `blake3hash`). | `y` (Blob) |
| **Timestamp** | Primary event time. | `i64` (Epoch ns) |
| **Routing** | Partition keys, topics, offsets. | `s` (String) |
| **Lifecycle** | Row state (Active, Tombstone). | `u8` (Enum) |
| **Transaction** | MVCC/Consistency IDs. | `u64` |
| **Opaque** | **Data Mart / BI Projections.** Conventional, non-shredded columns (e.g., `total_revenue`) added to the table to support standard SQL tools that cannot query shredded sections. | Varies |

### 2.2 Tagged Value Sections (The Payload)
Data is shredded by **Canonical Type**, not by domain field.
*   **Section:** A physical container for values of a specific type (e.g., `float64`, `string`, `bool`).
*   **Tag (Membership):** The logical path or attribute name (e.g., `user.age`, `metrics.cpu`).
*   **Value:** The raw scalar or tensor data.

*> [Figure 1: The Shredding Prism – Illustrating how a JSON document splits into type-specific silos]*

### 2.3 The Canonical Type System
Types are defined by a machine-readable tuple notation (AST Nodes), strictly separating storage format from semantic meaning.

*   **Machine Numeric:** `(Base, Width, Endian, Structure)`
    *   `u,i,f`: Unsigned, Signed, Float.
    *   `8..64`: Bit width.
    *   `l,n`: Little/Network Endian.
    *   `h,m`: Homogenous Array / Set.
    *   *Ex:* `i,64,l,-` (int64), `f,32,n,h` (Float32 Vector).
*   **String-like:** `(Type, Format, Size, Structure)`
    *   `s`: UTF8 String.
    *   `y`: Byte Blob.
    *   `f`: Fixed width (e.g., for UUIDs).

---

## 3. Structural Semantics & Topology

Leeway replaces nested tree structures with flat, vectorized lookups using a Graph model.

### 3.1 Membership Specification
Defines how values associate with logical tags.
*   **Verbatim (Low Cardinality):** Static keys (e.g., `hostname`). Stored via Dictionary Encoding.
*   **Parametrized (High Cardinality):** Dynamic keys (e.g., `tags[0]`, `map["uuid-1"]`). The key is split into a static "Tag" (`tags/_`) and a dynamic "Parameter" stored in a parallel column.
*   **Ref:** The tag is an integer pointer to an external schema registry.

*> [Figure 2: The Zipper – Illustrating how High Cardinality keys are split into Schema (Low Card) and Data (High Card/Param)]*

### 3.2 Cardinality Vectors
Support columns that handle non-scalar data and aliasing.
*   **`value-card`:** Defines the number of scalars per logical value.
    *   *Usage:* Storing a 3x3 Matrix as 9 sequential floats. `value-card=9`.
*   **`membership-card`:** Defines the number of tags per logical value.
    *   *Usage:* **Multi-Membership (Aliasing)**. A single value `19.99` can be tagged as both `/price` and `/min_price`. `membership-card=2`.

---

## 4. Physical Layout & Naming

Leeway enforces a **Self-Describing Naming Convention**. The physical column name encodes the full schema, including configuration and optimization hints, allowing reconstruction without a registry.

### 4.1 Column Roles (`ColumnRoleE`)
A Section is composed of multiple physical columns working in unison (Co-Arrays).

| Role | Enum | Description |
| :--- | :--- | :--- |
| **Value** | `val` | The actual data payload. |
| **Low-Card Verbatim** | `lmv` | The dictionary-encoded tag/path. |
| **High-Card Param** | `mvhp` | The dynamic parameter (index/key). |
| **Membership Card** | `lmvcard`| Support: Maps M memberships to 1 value. |
| **Value Card** | `valcard`| Support: Maps N scalars to 1 logical value. |

### 4.2 Naming Scheme & Base62 Encoding
Format: `[Prefix]:[Section]:[LogCol]:[Role]:[Type]:[Hints]:[Semantics]:[Config]:[Group]:`

To keep names concise and URL-safe, **Aspects** (Encoding Hints, Value Semantics, Use Aspects) are serialized as bitmasks and encoded using **Base62**.

**Example:** `"tv:bool:lmvcard:lmvcard:u64:4gw:0:0:0::"`

*   **tv:** Prefix (Tagged Value).
*   **bool:** Section Name.
*   **lmvcard:** Logical Name & Role (Membership Cardinality).
*   **u64:** Canonical Type (UInt64).
*   **4gw:** **Encoding Hints** (Base62 encoded bitmask). Decoding `4gw` reveals specific compression settings (e.g., Delta Encoding + ZSTD).
*   **0:** **Value Semantics** (Base62 encoded bitmask).
*   **0:** **Config** (TableRowConfig).
*   **0:** **Streaming Group**.

*> [Figure 3: Co-Array Alignment – Illustrating how Tag indices align physically with Value indices]*

---

## 5. Advanced Mechanics

### 5.1 Co-Sections
**Definition:** Two sections are "Co-Sections" if they share the exact same **Topology** (Cardinality).
*   **Constraint:** Defined via `CoSectionGroup`. If Attribute A exists in Section X, it *must* exist in Co-Section Y.
*   **Use Case:** Sharding metadata. Store heavy `Blob` data in Section A and lightweight `int64` metadata in Co-Section B. The membership overhead is paid only once.

*> [Figure 4: Multi-Membership & Co-Sections – Illustrating aliasing and shared topology]*

### 5.2 Streaming Groups
**Definition:** A subset of sections that must be transported together in row-oriented protocols (Kafka/Pulsar).
*   **Purpose:** Enables splitting "Fat Entities" (e.g., 500 columns) into smaller, coherent streams without breaking logical row integrity.

### 5.3 Vertical Subsetting
Leeway tables can be sliced vertically at Section/Co-Section boundaries. A valid Leeway subset must contain:
1.  All **Plain Value** columns.
2.  Complete **Co-Section Groups**.

---

## 6. The Go SDK Architecture

The SDK follows a **Definition $\to$ Synthesis $\to$ Runtime** pattern.

### 6.1 Definition Phase (`common`)
*   **`TableManipulator`:** Fluent API to construct schemas programmatically.
*   **`mapping`:** Utilities to infer Leeway schemas from JSON samples.
*   **`TableDesc`:** The immutable schema definition.

### 6.2 Synthesis Phase (Codegen)
*   **`dml.GoClassBuilder`:** Generates **Ingestion** structs (`InEntity`).
    *   Produces Arrow-compatible memory layouts.
    *   Manages transaction boundaries.
*   **`readaccess.GoClassBuilder`:** Generates **Query** structs (`ReadAccess`).
    *   Produces `MembershipPacks` and `Accelerators` (O(1) lookups).
*   **`ddl.GeneratorDriver`:** Generates storage definitions for Arrow, ClickHouse (SQL), and Go.

### 6.3 Runtime Phase
*   **Ingestion:** Uses generated `InEntity` structs to buffer data and `TransferRecords` to flush to Arrow batches.
*   **Access:** Uses generated `ReadAccess` structs to load Arrow batches and perform vectorized queries.

---

## 7. Comparative Analysis

| Feature | **Leeway** | **JSON / Document** | **Parquet / Arrow** | **Relational (SQL)** |
| :--- | :--- | :--- | :--- | :--- |
| **Structure** | **Graph** (Shredded) | **Tree** (Nested) | **Tree** (Nested Levels) | **Flat** (Rigid) |
| **Schema** | Physical (On-Write) | Implicit (In-Data) | Header / Registry | DDL (Pre-defined) |
| **Map Keys** | **Parameters** (Data) | Keys (Parsing req.) | **Explosion** (Schema break) | Not Supported / JSONB |
| **Polymorphism** | **Native** (Type Routing) | Native | Union Types (Complex) | Impossible |
| **Storage** | Sparse Vectors | Dense Blobs | Dense / Sparse | Dense |
| **Aliasing** | **Multi-Membership** | Duplication | Duplication | Duplication |

---

## 8. End-to-End Walkthrough

**Input:**
```json
{"hostname": "server-a", "tags": ["prod"], "metrics": {"cpu": 45.5}, "active": true}
```

**Logical Shredding:**
1.  `/hostname` $\to$ `string` Section (Tag: `/hostname`)
2.  `/tags/0` $\to$ `symbol` Section (Tag: `/tags/_`, Param: `0`)
3.  `/metrics/cpu` $\to$ `float64` Section (Tag: `/metrics/cpu`)
4.  `/active` $\to$ `bool` Section (Tag: `/active`)

**Physical Table (ClickHouse Arrays):**

| Section | Column Name (Base62 decoded for readability) | Value | Notes |
| :--- | :--- | :--- | :--- |
| **Backbone** | `id:blake3...` | `["hash-1"]` | Entity ID |
| **String** | `tv:string:val...` | `["server-a"]` | Value |
| | `tv:string:lmv...` | `["/hostname"]` | Tag |
| **Symbol** | `tv:symbol:val...` | `["prod"]` | Value |
| | `tv:symbol:lmv...` | `["/tags/_"]` | Tag (Schema) |
| | `tv:symbol:mvhp...` | `["0"]` | Param (Data) |
| **Float64** | `tv:float64:val...` | `[45.5]` | Value |
| | `tv:float64:lmv...` | `["/metrics/cpu"]` | Tag |

---

## 9. Knowledge Check: Leeway Concepts

### 9.1 Questions

**Q1. In a standard Leeway schema, where does the integer value `42` for the attribute `user.profile.age` end up residing?**
A) In the `user` table, under the `profile_age` column.
B) In the `int64` section, grouped with all other integers from the document.
C) In a JSONB blob column named `properties`.
D) In the `age` section, which is a dedicated column for user ages.

**Q2. How does Leeway prevent "Map Key Explosion" when handling dynamic keys like UUIDs?**
A) It creates a new column for every unique UUID.
B) It stores the entire object as an Opaque JSON blob.
C) It treats the keys as "High Cardinality Parameters," storing them in a data column (`mvhp`) while keeping the schema tag static.
D) It hashes the UUIDs and drops the original strings.

**Q3. You have a large 1024-dimension float vector. You want to tag it as both "ImageEmbedding" and "SearchContext" without duplicating the heavy data. How does Leeway handle this?**
A) Using **Multi-Membership**: The value is stored once, and the `membership-card` column maps it to two tags.
B) Using **Co-Sections**: You create two sections that share the value.
C) You must insert the value twice.
D) Using Virtual Columns.

**Q4. A section is defined with the canonical type `(f, 64, -, h)` (Homogenous Array). If a single logical row contains a 3x3 matrix (9 floats), what does the `value-card` column contain?**
A) `3` (Rows)
B) `1` (One Object)
C) `9` (Scalar Units)
D) `0`

**Q5. Where does a tool look to find the schema definition (Types, Hints, Semantics) for a physical column?**
A) It queries the `system.leeway_registry` table.
B) It parses the column name itself, decoding the Base62 segments.
C) It looks for a companion `.proto` file.
D) It cannot determine the schema; this is an internal column.

**Q6. What is the primary purpose of an "Opaque" column?**
A) To store binary blobs that cannot be shredded.
B) To provide a conventional, read-oriented view (Data Mart) for BI tools.
C) To encrypt sensitive data.
D) To store the original JSON for debugging only.

**Q7. Section A is defined as a "Co-Section" of Section B. What does this imply?**
A) They contain the exact same values.
B) They share the same **Topology** (Cardinality).
C) They must be stored on the same disk.
D) Section A is a backup of Section B.