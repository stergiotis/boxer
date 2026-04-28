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
// Modifications applied during port (see ./NOTICE):
//   - Dropped the Benthos config-DSL surface entirely:
//       * bytesFromStrField / BytesFromStrFieldAsInt32 (humanize-bytes
//         parser used by config defaults like "50MiB").
//       * The kfrField* string constants and FranzConsumerFields().
//       * FranzConsumerFieldLintRules (a Bloblang script for the
//         Benthos config validator).
//       * FranzConsumerDetailsFromConfig / FranzConsumerOptsFromConfig.
//     The github.com/dustin/go-humanize dependency disappears as a
//     result.
//   - Dropped TransactionIsolationLevel and startOffsetType string-typed
//     enums; they existed only as config-string-to-kgo-value mappings.
//     Callers populate IsolationLevel and StartOffset directly with
//     kgo.ReadCommitted() / kgo.ReadUncommitted() and
//     kgo.NewOffset().At*() values.
//   - Dropped FranzRecordToMessageV0 / FranzRecordToMessageV1; this
//     port exposes *kgo.Record directly per ADR-0005 / EXPLANATION.md
//     so the conversion to service.Message is unnecessary.
//   - Added DefaultFranzConsumerDetails(): returns a FranzConsumerDetails
//     pre-populated with the same defaults the upstream Connect config
//     applied at the YAML layer (50MiB fetch max, 5s fetch wait, 1m
//     session timeout, etc.).
//   - Added (*FranzConsumerDetails).SetTopicSpec(): pulled the
//     topic-spec parsing logic out of FranzConsumerDetailsFromConfig so
//     applications using the topic[:partition[:offset]] syntax get the
//     same semantics without re-implementing.
//   - Refactored to boxer coding standards: receiver `inst`, named
//     return values, pre-sized maps where the upper bound is known.

package kafka

import (
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
)

// FranzConsumerDetails describes the information required to create a
// franz-go Kafka consumer. The zero value is NOT usable in production:
// kgo rejects a zero session timeout and a zero fetch-max-bytes, and a
// zero StartOffset is an unusual concrete offset rather than "earliest"
// or "latest". Use [DefaultFranzConsumerDetails] as the starting point
// and override fields as needed.
type FranzConsumerDetails struct {
	RackID                 string
	InstanceID             string
	IsolationLevel         kgo.IsolationLevel
	SessionTimeout         time.Duration
	RebalanceTimeout       time.Duration
	HeartbeatInterval      time.Duration
	StartOffset            kgo.Offset
	Topics                 []string
	TopicPartitions        map[string]map[int32]kgo.Offset
	RegexPattern           bool
	ExcludeTopics          []string
	FetchMinBytes          int32
	FetchMaxBytes          int32
	FetchMaxPartitionBytes int32
	FetchMaxWait           time.Duration
}

// DefaultFranzConsumerDetails returns a FranzConsumerDetails populated
// with the defaults that the upstream Connect plugin applied at its
// YAML config layer. The caller assigns Topics (or calls SetTopicSpec)
// and any overrides before passing the value to [FranzConsumerDetails.FranzOpts].
//
// Defaults:
//
//	IsolationLevel         = kgo.ReadUncommitted()
//	StartOffset            = kgo.NewOffset().AtStart()  // "earliest"
//	SessionTimeout         = 1m
//	RebalanceTimeout       = 45s
//	HeartbeatInterval      = 3s
//	FetchMaxBytes          = 50 MiB
//	FetchMinBytes          = 1
//	FetchMaxPartitionBytes = 1 MiB
//	FetchMaxWait           = 5s
func DefaultFranzConsumerDetails() (d FranzConsumerDetails) {
	d = FranzConsumerDetails{
		IsolationLevel:         kgo.ReadUncommitted(),
		StartOffset:            kgo.NewOffset().AtStart(),
		SessionTimeout:         time.Minute,
		RebalanceTimeout:       45 * time.Second,
		HeartbeatInterval:      3 * time.Second,
		FetchMaxBytes:          50 * 1024 * 1024,
		FetchMinBytes:          1,
		FetchMaxPartitionBytes: 1 * 1024 * 1024,
		FetchMaxWait:           5 * time.Second,
	}
	return
}

// SetTopicSpec parses the topic specifications and populates inst's
// Topics and TopicPartitions fields. The accepted syntax is the same as
// the upstream Connect input — `topic`, `topic:partition`,
// `topic:start-end`, and (when allowExplicitOffsets is true)
// `topic:partition:offset` — see [ParseTopics] for the full grammar.
//
// When a topic entry omits an explicit offset, inst's current StartOffset
// (its EpochOffset()) is used as the default. Call this method after
// configuring StartOffset to avoid surprises.
func (inst *FranzConsumerDetails) SetTopicSpec(specs []string, allowExplicitOffsets bool) (err error) {
	var topicPartitionsInts map[string]map[int32]int64
	inst.Topics, topicPartitionsInts, err = ParseTopics(specs, inst.StartOffset.EpochOffset().Offset, allowExplicitOffsets)
	if err != nil {
		return
	}
	if len(topicPartitionsInts) == 0 {
		return
	}
	inst.TopicPartitions = make(map[string]map[int32]kgo.Offset, len(topicPartitionsInts))
	for topic, partitions := range topicPartitionsInts {
		partMap := make(map[int32]kgo.Offset, len(partitions))
		for part, offset := range partitions {
			partMap[part] = kgo.NewOffset().At(offset)
		}
		inst.TopicPartitions[topic] = partMap
	}
	return
}

// FranzOpts returns the kgo.Opt slice that establishes a consumer
// described by inst. The slice is freshly allocated and the caller may
// append additional options (consumer group name, custom rebalance
// callbacks, etc.) before passing it to [NewFranzClient] or
// kgo.NewClient. The connection-level options from
// [FranzConnectionDetails.FranzOpts] are NOT included; combine the
// two slices when building the full client option set.
func (inst *FranzConsumerDetails) FranzOpts() (opts []kgo.Opt) {
	opts = []kgo.Opt{
		kgo.Rack(inst.RackID),
		kgo.ConsumeTopics(inst.Topics...),
		kgo.ConsumePartitions(inst.TopicPartitions),
		kgo.ConsumeResetOffset(inst.StartOffset),
		kgo.FetchMaxBytes(inst.FetchMaxBytes),
		kgo.FetchMinBytes(inst.FetchMinBytes),
		kgo.FetchMaxPartitionBytes(inst.FetchMaxPartitionBytes),
		kgo.FetchMaxWait(inst.FetchMaxWait),
		kgo.SessionTimeout(inst.SessionTimeout),
		kgo.RebalanceTimeout(inst.RebalanceTimeout),
		kgo.HeartbeatInterval(inst.HeartbeatInterval),
		kgo.FetchIsolationLevel(inst.IsolationLevel),
	}
	if inst.RegexPattern {
		opts = append(opts, kgo.ConsumeRegex())
		if len(inst.ExcludeTopics) > 0 {
			opts = append(opts, kgo.ConsumeExcludeTopics(inst.ExcludeTopics...))
		}
	}
	if inst.InstanceID != "" {
		opts = append(opts, kgo.InstanceID(inst.InstanceID))
	}
	return
}
