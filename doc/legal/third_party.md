---
type: reference
audience: contributor
status: draft
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Third-Party Attribution Index

This document is the aggregate index of third-party software distributed with or required by boxer. It is informational and does not modify any underlying license; the per-package NOTICE files and the upstream LICENSE files are authoritative.

## How this index is organised

Each entry below names a package, its license, the in-repo consumer (if scoped to one), and the upstream source. Sections are sorted alphabetically by SPDX identifier; within a section, entries are sorted alphabetically by package name.

When a package is consumed by a single in-repo location, the `Consumer` line names that location — the per-package NOTICE there is authoritative for modifications applied during port. When the package is consumed broadly (logger, error builder, etc.) the `Consumer` line says "repo-wide".

## Apache-2.0

### github.com/redpanda-data/connect (`internal/impl/kafka`)

- License: Apache License 2.0 (see [`licenses/Apache-2.0.txt`](../../licenses/Apache-2.0.txt)).
- Copyright: 2024 Redpanda Data, Inc.
- Upstream: https://github.com/redpanda-data/connect
- Pinned commit: `50aa034a668cc7d03d6acdcf63791fc36906a21c` (2026-04-24).
- Consumer: [`public/streaming/persisted/kafka/`](../../public/streaming/persisted/kafka).
- Use: source of the franz-go-based Kafka input/output, ported as a derivative work per [ADR-0005](../adr/0005-streaming-persisted-kafka-from-connect.md).
- Authoritative NOTICE: [`public/streaming/persisted/kafka/NOTICE`](../../public/streaming/persisted/kafka/NOTICE).

## BSD-3-Clause

### github.com/twmb/franz-go

- License: BSD-3-Clause.
- Copyright: 2020 Travis Bischel.
- Upstream: https://github.com/twmb/franz-go
- Consumer: [`public/streaming/persisted/kafka/`](../../public/streaming/persisted/kafka) — direct: `pkg/kgo`, `pkg/sasl/{plain,scram,oauth}`, `pkg/kadm` (consumer-lag observability via `consumer_lag.go`).
- Use: Kafka client library used by the ported input and output.

## MIT

### github.com/Jeffail/checkpoint

- License: MIT.
- Copyright: 2024 Ashley Jeffs.
- Upstream: https://github.com/Jeffail/checkpoint
- Consumer: [`public/streaming/persisted/kafka/`](../../public/streaming/persisted/kafka).
- Use: ordered-reader checkpointing primitive, retained from upstream Connect.

### github.com/Jeffail/shutdown

- License: MIT.
- Copyright: 2024 Ashley Jeffs.
- Upstream: https://github.com/Jeffail/shutdown
- Consumer: [`public/streaming/persisted/kafka/`](../../public/streaming/persisted/kafka).
- Use: lifecycle helper used by the upstream readers; conditional retention after Phase 5 if not replaced by `context` primitives.

### github.com/testcontainers/testcontainers-go

- License: MIT.
- Copyright: 2017-2019 Gianluca Arbezzano (and contributors).
- Upstream: https://github.com/testcontainers/testcontainers-go
- Consumer: [`public/streaming/persisted/kafka/integration_test.go`](../../public/streaming/persisted/kafka/integration_test.go) (test-only).
- Use: Docker-backed integration tests for the Kafka port; spins up a `redpandadata/redpanda` container per top-level test. Gated behind the `integration` build tag; not in the default build path.

## Maintenance notes

- When adding a new third-party dependency, add an entry here in the appropriate license section before the dep merges. The check is manual today (no `doclint` rule walks `go.mod`); future tooling may close that gap.
- When pinning a new upstream commit for a derived port (such as `redpanda-data/connect`), update both the per-package NOTICE and the matching entry above.
- License-section ordering is alphabetical by SPDX identifier; within a section, entries are alphabetical by package name. Insert new entries in their sorted slot rather than appending.
- This index is informational. The authoritative license text for each component lives in its own LICENSE file in its source repository, and (for derivative works in this repo) in the per-package NOTICE.
