---
type: how-to
audience: engineer with a specific task
status: draft
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# How to tidy a module inside a vendored workspace, offline

This recipe brings a module's `go.mod` and `go.sum` in line with a vendored Go
workspace it is a member of, with no network and no module cache. It does not
add dependencies the workspace does not vendor. The design and its limits are
in [ADR-0384](../adr/0384-offline-go-mod-tidy-against-a-vendored-workspace.md).

## When to use this recipe

A vendored workspace is a `go.work` that `use`s some modules' source trees, with
the `go work vendor` output beside it. This is the shape a downstream airgap
bundle carries boxer in (see [airgapped-build.md](./airgapped-build.md)). Other
modules at the site join it with `go work use`.

When the workspace is re-packed with newer versions, a member pinned to the old
ones stops building, and so does every other build in that workspace:

```text
go: inconsistent vendoring in <bundle>:
	github.com/stergiotis/boxer@v0.0.13: is explicitly required in go.mod, but not marked as explicit in vendor/modules.txt
```

Every `require` of a member has to name a version that `vendor/modules.txt`
marks explicit. `go get`, `go mod tidy` and `go work sync` cannot fix that
offline, because each needs the module cache. `vendortidy` computes the same
requirements from the sources in the workspace instead.

## Prerequisites

- The workspace's offline environment: `GOWORK` pointing at its `go.work`,
  `GOFLAGS=-mod=vendor`, `GOPROXY=off`, `GOTOOLCHAIN=local`. A bundle's env file
  sets these.
- The member is in the workspace: `go work use <member-dir>`.
- boxer's source is in the workspace, at a version that has
  `code analysis golang vendortidy`.

## Steps

1. **Build the command without the drifted member.** Go checks vendor
   consistency for the whole workspace, so while any member is drifted nothing
   in it builds, the command included. Build from a work file that leaves the
   member out. It has to sit next to `vendor/`, because Go looks for the vendor
   tree beside the work file:

   ```bash
   cd <bundle>
   cp go.work vendortidy.work
   go work edit -dropuse=./<member> vendortidy.work
   GOWORK=$PWD/vendortidy.work go build -o vendortidy github.com/stergiotis/boxer/public/app
   rm vendortidy.work
   ```

   The binary holds all of boxer's commands. A cold build compiles boxer's
   whole dependency set, which takes minutes on a small machine. Build it once
   and keep it until the next re-pack.

2. **See the drift.** From the member's directory, with the normal environment:

   ```bash
   cd <member-dir>
   <bundle>/vendortidy code analysis golang vendortidy --check
   ```

   Each line is one change: `add`, `drop`, `upgrade`, `DOWNGRADE` (the
   workspace builds an older version than the member asked for), `mark`
   (direct/indirect), and `go` for a raised `go` line. `go.sum lacks` lists
   missing checksums. The command exits non-zero when anything differs.

3. **Write it.**

   ```bash
   <bundle>/vendortidy code analysis golang vendortidy
   ```

   This rewrites `go.mod` and `go.sum` in place and keeps every other directive.

## Verification

```bash
<bundle>/vendortidy code analysis golang vendortidy --check   # "<module> matches the vendored workspace"
go build ./... && go test ./...                               # offline, against vendor/
```

Back on a connected host, `GOWORK=off go build ./...` works without changes.
`GOWORK=off go mod tidy --diff` leaves `go.mod` alone and may only remove
`go.sum` lines.

## What it cannot do offline

- **Add a module the workspace does not vendor.** A member cannot even
  `require` one. An import nothing provides stops the run as `unresolved
  import`. Vendor it on a connected host and re-pack, or `go work use` its
  source.
- **Prune `go.sum` exactly.** Checksums that only dependency tests need cannot
  be judged without those tests, so old lines are kept and the next connected
  `go mod tidy` may drop a few.
- **Decide some requirements.** A `kept` line is an existing requirement on a
  module that provides no vendored package. Tidy keeps such a line when a
  dependency's tests need its version, which cannot be checked offline. It is
  kept and never added.

## Troubleshooting

- **Symptom:** `not a module used by the workspace`.
  **Cause:** the directory is not in `go.work`, or `GOWORK` names another work
  file, for example one left set from step 1.
  **Fix:** `go work use <member-dir>`, or pass `--work <bundle>/go.work`.
- **Symptom:** `no workspace: set GOWORK or pass --work`.
  **Cause:** the workspace environment is not loaded.
  **Fix:** source the bundle's env file, or pass `--work`.
- **Symptom:** `not required: <module> is used through the workspace and has no
  version in vendor/modules.txt`.
  **Cause:** a used module that no workspace module requires at a version, so
  there is no version to pin.
  **Fix:** none needed. It builds from source through the workspace.
- **Symptom:** `replace <module> => … go.mod needs the same replace`.
  **Cause:** the workspace builds that module through a `replace`.
  **Fix:** add the same `replace` to the member's `go.mod`.
