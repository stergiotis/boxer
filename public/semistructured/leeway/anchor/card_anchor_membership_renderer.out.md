---
type: reference
audience: contributor
status: draft
generated: true
generator: go test, TestMembershipRendererGeneration
---

> **Status: draft — pre-human-review.** Machine-generated demo artifact;
> regenerate via `go test`, do not edit.

# anchor — membership rendering: ids on the wire, names at read time

The batch carries membership ids; how an id displays is decided at read
time by the consumer's renderer (ADR-0072). The test reads the same batch
through the lens sink with the default renderer and with an anchor domain
formatter injected, and asserts the attribute names swap from the hex
column to the named column below. The formatter is a demo-local stand-in
for the registry-backed ref-to-name formatter play injects.

| ref id (wire) | default renderer | anchor formatter |
|---|---|---|
| 5 | `0x5` | `model:AeroQuad` |
| 999042 | `0xf3e82` | `customer:42` |
| 22 | `0x16` | `port:22` |
| 443 | `0x1bb` | `port:443` |
| 3735928559 | `0xdeadbeef` | `0xdeadbeef` |
