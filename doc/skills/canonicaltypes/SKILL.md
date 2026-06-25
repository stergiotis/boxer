---
name: canonicaltypes
description: "Use when generating, parsing, or manipulating Canonical Type Signatures (CT) — the compact string representation of data types (primitives, groups, signatures) for RPC/FFI interface descriptions."
---

This **SKILL.md** is designed for both human developers and LLM agents to understand, generate, and manipulate **Canonical Type Signatures (CT)**. It incorporates the latest networking extensions and structural fixes.

# SKILL: Canonical Type Signatures (CT)

## 1. Overview
Canonical Type Signatures (CT) are a technology-agnostic, compact string-based representation of data types. They are used to describe data structures (Primitives, Groups, and Signatures) in a way that is easily serializable and verifiable.

---

## 2. Grammar Specification

### Base Types (Runes)
| Category | Rune | Type | Example |
| :--- | :--- | :--- | :--- |
| **Numeric** | `u` / `i` / `f` | Unsigned / Signed / Float | `u64` |
| **String** | `s` / `y` / `b` | UTF-8 / Bytes / Boolean | `s`, `bx` |
| **Temporal** | `z` / `d` / `t` | UTC / Zoned Date / Zoned Time | `z64` |
| **Network** | `v` / `w` | IPv4 / IPv6 | `v`, `wc` |

### Modifiers (Suffixes)
Modifiers are appended to Base Types in a specific order: `[Base][Width][Modifier][Scalar]`.

1.  **Width**:
    *   **Numeric/Temporal**: Bits (e.g., `8, 16, 32, 64, 128`).
2.  **Byte Order (Numeric only)**:
    *   `l`: Little Endian.
    *   `n`: Big Endian (Network).
3.  **Width Modifier (String only)**:
    *   `x`: Fixed-width (requires a width value, e.g., `sx32`).
4.  **CIDR Modifier (Network only)**:
    *   `c`: CIDR — each value carries its own prefix length (e.g., `vc`, `wc`). Without it, `v`/`w` is a plain host address. There is no in-type width: prefix lengths are per-value data.
5.  **Scalar Modifier (All Primitives)**:
    *   `h`: Homogenous Array (List).
    *   `m`: Set (Unique collection).
    *   *None*: Single scalar value.

### Structural Separators
*   **`-` (Group Separator)**: Combines primitives into a single record (struct-like).
    *   *Example:* `u8-v` (An 8-bit int followed by an IPv4 address).
*   **`_` (Signature Separator)**: A higher-level logical grouping of groups or primitives.
    *   *Example:* `u8-v_s` (A group followed by a standalone string).

---

## 3. Programming Interface (Go)

### AST Node Types
*   **`MachineNumericTypeAstNode`**: `BaseType`, `Width`, `ByteOrderModifier`, `ScalarModifier`.
*   **`StringAstNode`**: `BaseType`, `WidthModifier`, `Width`, `ScalarModifier`.
*   **`TemporalTypeAstNode`**: `BaseType`, `Width`, `ScalarModifier`.
*   **`NetworkTypeAstNode`**: `BaseType` (`v`/`w`), `CIDRModifier` (`c` = per-value CIDR prefix), `ScalarModifier`.
*   **`GroupAstNode`**: A list of `PrimitiveAstNodeI`. (Uses `-`).
*   **`SignatureAstNode`**: A list of `AstNodeI`. (Uses `_`).

### Key Methods
*   **`String()`**: Generates the canonical string (e.g., `v32h`)
*   **`IsValid()`**: Performs semantic validation.
    *   *Network (`v`/`w`)*: `CIDRModifier` is empty (host address) or `c` (per-value CIDR prefix).
    *   *Boolean (`b`)*: `Width` must be $0$.
*   **`IterateMembers()`**: Recursively flattens the entire structure into a sequence of primitives.
*   **`IterateGroupMembers()`**: Shallow iteration. On a Signature, it returns the top-level Groups/Primitives without flattening.

---

## 4. Utility Functions

### Promotion & Demotion
Use these to convert between scalars and collections while **preserving hierarchy**:
*   **`PromoteScalars(node, modifier)`**: Wraps all scalars within a structure into arrays (`h`) or sets (`m`).
*   **`DemoteToScalars(node)`**: Strips all `h` and `m` modifiers from the internal primitives.

### Member Counting
*   **`CountMembers(node)`**: Total count of primitive fields.
*   **`CountNonScalars(node)`**: Count of fields that are arrays or sets.

---

## 5. Agent Instructions (LLM-Specific)

### Generation Rules
1.  **Network Types**: Use `v` for IPv4 and `w` for IPv6 (a plain host address). Append `c` when each value carries its own CIDR prefix length (e.g., `vc`, `wc`). Prefix lengths are per-value data, never part of the type.
2.  **Booleans**: Never assign a width to `b`.
3.  **Separators**: Use `-` for flat structures. Use `_` only when grouping logically distinct records.

### Transformation Logic
*   To create a "List of Objects", first define the Group `u32-s`, then use `PromoteScalars(group, 'h')` to get `u32h-sh`.
*   To check if two types are compatible, compare their `String()` outputs or use `Equals()`.

### Common Abbrev Reference (`ctabb`)
| Variable | Signature | Description |
| :--- | :--- | :--- |
| `ctabb.U64` | `u64` | 64-bit Unsigned Integer |
| `ctabb.V` | `v` | IPv4 Address |
| `ctabb.Vc` | `vc` | IPv4 CIDR (per-value prefix) |
| `ctabb.W` | `w` | IPv6 Address |
| `ctabb.S` | `s` | UTF-8 String |
| `ctabb.Sh` | `sh` | Array of UTF-8 Strings |

---

## 6. Invariants & Testing
*   **Roundtrip**: `Parser.Parse(node.String())` must produce a node equal to the original.
*   **Stability**: `node.String()` must be deterministic and contain no internal state labels (like `<none>`).
*   **Semantic Constraint**: Primitives are valid only if their modifiers make sense for the base type. Network types take no width — `v32` is no longer valid; use `v` (address) or `vc` (CIDR).