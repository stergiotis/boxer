//go:build llm_generated_opus47

// Copyright 2024 Redpanda Data, Inc.
// Copyright 2026 Panos Stergiotis
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// Modifications: see ./NOTICE for the per-package modification record;
// the design rationale is in EXPLANATION.md alongside this file.

// Package kafka exposes a franz-go-based Kafka consumer and producer for
// pebble2impl's persisted-streaming subsystem.
//
// The package is a derivative work of the franz-go-flavoured plugins in
// github.com/redpanda-data/connect/internal/impl/kafka, pinned at commit
// 50aa034a668cc7d03d6acdcf63791fc36906a21c (2026-04-24). The Benthos
// public/service framework is intentionally absent: configuration is plain
// Go, the message envelope is [github.com/twmb/franz-go/pkg/kgo.Record]
// directly, and lifecycle plumbing follows boxer coding standards. See
// [doc/adr/0005-streaming-persisted-kafka-from-connect.md] for the
// derivation decision and EXPLANATION.md for the interface contract.
//
// Per Apache-2.0 §4.b every modified source file in this package carries
// a header referencing ./NOTICE. THIRD_PARTY_NOTICES.md §1.4 (at the
// repo root) reproduces the Apache-2.0 license text verbatim and
// satisfies §4.a; the package NOTICE satisfies §4.d.
package kafka
