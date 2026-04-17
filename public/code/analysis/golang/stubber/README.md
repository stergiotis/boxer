---
type: reference
audience: code-analysis tooling user
status: draft
---

> **Status: draft — pre-human-review.** Not yet verified against the current documentation standard. Do not cite as authoritative.

# stubber

## 1. Purpose
**`stubber`** is a CLI tool designed to generate a **redacted, compilable version** of Go source packages. 
Its primary goal is to strip away implementation details (proprietary logic) and private members while preserving the public API surface. 

This is useful for creating closed-source SDKs, distribution-safe libraries, or API documentation where the underlying code must remain hidden. 
It may also be helpful to make APIs available to LLMs.

## 2. How It Works
The tool processes a directory tree of Go packages (e.g., `./...`) using the following steps:

1.  **AST Analysis:** It parses the source code into an Abstract Syntax Tree (AST) using `go/parser`.
2.  **Filtering:**
    *   **Private Elements:** Removes all unexported (lowercase) constants, variables, types, and struct fields.
    *   **Dependency Pruning:** Recursively removes public functions or types if their signature depends on a private type (preventing compilation errors).
    *   **Sanitization:** Strips private keys from global composite literals.
3.  **Stubbing:** Replaces the bodies of all remaining public functions and methods with `panic("stub")`, keeping only the function signatures.
4.  **Import Management:**
    *   Converts imports that are only valid in the original implementation to side-effect imports (`_`).
5.  **Output Generation:**
    *   Merges or adds custom build tags (e.g., `//go:build apidoc`).
    *   Formats the code and organizes imports using `golang.org/x/tools/imports` (standard `goimports` behavior).
    *   Writes the resulting files to a specified output directory, mirroring the original structure.