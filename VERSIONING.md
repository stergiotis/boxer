---
type: reference
audience: consumers pinning a version of this module
status: draft
---

> **Status: draft — pre-human-review.** The policy proposed by
> [ADR-0389](./doc/adr/0389-stability-tiers-per-package-and-what-a-version-number-promises.md);
> nothing here binds until that ADR is accepted.

# Versioning

What a version number of `github.com/stergiotis/boxer` promises, and what it
does not. The promise is narrow on purpose: most of the tree is outside it.

## When the promise starts

At the first tag cut after the stability tiers exist. Earlier tags, and every
untagged commit, promise nothing; a pseudo-version pins a commit, not a
contract. `scripts/ci/apidiff.sh` can compare against any ref; against a base
that declares no Stable package it reports and exits zero, since no promise was
made there.

## What a version is

`v0.MINOR.PATCH` while the module is pre-1.0.

- **MINOR** moves for any incompatible change to something this page covers.
- **PATCH** moves for everything else, compatible additions included.
- A tag is cut only from a commit that passed the release script
  (`scripts/ci/release.sh`).
- A broken tag is retracted with a `retract` directive in `go.mod` and a new
  tag is cut. A tag is never moved or deleted.

## What is covered

- **The Go API of Stable packages.** A package's tier is its
  `Stability` declaration in `package_props.go`;
  [doc/SURFACE.md](./doc/SURFACE.md) lists every package by tier.
  `public/packageprops` is Stable by ADR-0389's statement, since it cannot
  declare itself.
- **Stored tables and wire formats** that have a contract snapshot (the
  `contracts/` directory, checked by `scripts/ci/contracts.sh`).
- **Registered environment-variable names** (the `public/config/env`
  registry, [doc/env-vars.md](./doc/env-vars.md)). Removing or renaming one is
  incompatible; adding one is not.
- **The `boxer` command tree:** command names and flag names. Output formats
  are covered only where a contract snapshot covers them.
- **The Go↔Rust pairing rule:** the Go bindings and the Rust host are built
  from one tree. A version promises nothing about mixing the halves of two
  versions.

## What is not covered

- Experimental, Internal and unclassified packages, including every package
  without a declaration.
- The Go API of tool packages (the `boxer` command tree is covered above),
  demos, examples, test support that is not tiered Stable, and generated
  grammars.
- App packages under `apps/`, unless a declaration tiers one.
- Behaviour that no test or contract pins: log text, timing, the layout of a
  window.

## Deprecation

An incompatible change to a Stable package is preceded by one tagged version in
which the old form still works and carries a `// Deprecated:` comment naming
its replacement. The change itself then lands in a later MINOR.

This needs a carve-out from two house rules
([CODINGSTANDARDS](./CODINGSTANDARDS.md) "No Aliases" and "No compatibility
shims"): a forwarding type alias or wrapper marked `// Deprecated:` may live
in a Stable package for exactly one MINOR, and is deleted in the next.
Experimental and Internal packages break without a window, as before.

## Toolchain floor

The `go` directive in `go.mod` is every consumer's floor. It moves only

- for a standard-library vulnerability that this module's code reaches, or
- for a language or library feature the tree adopts by ADR.

Each move is listed in the release notes as a build-requirement change.

## Release notes

`boxer gov releasenotes --from <previous tag> --to <new tag>` compiles them:
breaking commits with their `Breaks:` paragraphs, removed or renamed packages,
the `go` directive and requirement changes, changed contract snapshots and
migration notes. `scripts/ci/apidiff.sh <previous tag>` lists the API
differences of Stable packages and fails on one no breaking commit owns.
[doc/releases/unreleased.md](./doc/releases/unreleased.md) is the notes for the
latest tag..`main`, regenerated at release time.

## Support

Only `main` and the latest tag are supported. A fix lands on `main` and ships
in the next tag, a PATCH when nothing incompatible landed since the latest one;
older tags receive no fixes, and there are no backport branches
([SECURITY.md](./SECURITY.md)).
