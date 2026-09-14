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
//   - Refactored to boxer coding standards: named return values, eh.Errorf
//     instead of errors.New / fmt.Errorf. Parsing logic, accepted syntax,
//     and error messages preserved verbatim from upstream.
//   - Rejects partition specs upstream accepted: ranges expanding past
//     MaxTopicPartitions, reversed ranges, and an empty topic name.

package kafka

import (
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// MaxTopicPartitions bounds how many partitions one [ParseTopics] call may
// expand its ranges into, summed over every entry. A range is expanded into
// one map entry per partition, so without a bound `topic:0-2147483647` asks
// for 2^31 of them. The value is far above the partition count a consumer
// names explicitly — a whole topic is consumed by naming the topic without
// partitions — and keeps the worst case at a few MiB.
const MaxTopicPartitions = 1 << 16

// parsePartitions parses a single partition spec or a contiguous range
// (for example "5" or "5-10") and returns the corresponding partition
// list. An empty expression, a malformed or reversed range, or a range
// longer than budget yields an error.
func parsePartitions(expr string, budget int) (parts []int32, err error) {
	if expr == "" {
		err = eh.Errorf("empty partition expression")
		return
	}

	rangeExpr := strings.Split(expr, "-")
	if len(rangeExpr) > 2 {
		err = eb.Build().Str("partition", expr).Errorf("partition is invalid; only one range can be specified")
		return
	}

	if len(rangeExpr) == 1 {
		var partition int64
		partition, err = strconv.ParseInt(expr, 10, 32)
		if err != nil {
			err = eh.Errorf("parsing partition number: %w", err)
			return
		}
		if budget < 1 {
			err = eb.Build().Str("partition", expr).Int("limit", MaxTopicPartitions).Errorf("topic spec exceeds the partition limit")
			return
		}
		parts = []int32{int32(partition)}
		return
	}

	var start, end int64
	start, err = strconv.ParseInt(rangeExpr[0], 10, 32)
	if err != nil {
		err = eh.Errorf("parsing start of range: %w", err)
		return
	}
	end, err = strconv.ParseInt(rangeExpr[1], 10, 32)
	if err != nil {
		err = eh.Errorf("parsing end of range: %w", err)
		return
	}

	if start > end {
		err = eb.Build().Str("partition", expr).Errorf("partition range is invalid; start of range is after its end")
		return
	}
	if end-start+1 > int64(budget) {
		err = eb.Build().Str("partition", expr).Int("limit", MaxTopicPartitions).Errorf("topic spec exceeds the partition limit")
		return
	}

	parts = make([]int32, 0, end-start+1)
	for i := start; i <= end; i++ {
		parts = append(parts, int32(i))
	}
	return
}

// ParseTopics parses topic specifications of the forms `topic`,
// `topic:partition`, `topic:partitionRange`, and `topic:partition:offset`,
// the last of which is rejected unless allowExplicitOffsets is true.
// Comma-separated and whitespace-padded entries are accepted.
//
// The partitions named across all entries may total at most
// [MaxTopicPartitions]; a spec asking for more is rejected before any range
// is expanded.
//
// When a partition appears multiple times across the input, an explicit
// non-default offset always wins; ties at the default offset preserve
// the first-seen mapping.
func ParseTopics(sourceTopics []string, defaultOffset int64, allowExplicitOffsets bool) (topics []string, topicPartitions map[string]map[int32]int64, err error) {
	// expanded counts partitions before de-duplication, so overlapping
	// entries spend the budget too; that keeps the work bounded as well as
	// the result.
	expanded := 0
	for _, t := range sourceTopics {
		for splitTopic := range strings.SplitSeq(t, ",") {
			trimmed := strings.TrimSpace(splitTopic)
			if trimmed == "" {
				continue
			}

			splitByColon := strings.Split(trimmed, ":")
			if len(splitByColon) == 1 {
				topics = append(topics, trimmed)
				continue
			}

			if len(splitByColon) > 3 {
				err = eb.Build().Str("topic", trimmed).Errorf("topic is invalid; only one partition and an optional offset should be specified")
				return
			}
			if len(splitByColon) == 3 && !allowExplicitOffsets {
				err = eb.Build().Str("topic", trimmed).Errorf("topic is invalid; explicit offsets are not supported by this input")
				return
			}

			topic := strings.TrimSpace(splitByColon[0])
			if topic == "" {
				err = eb.Build().Str("topic", trimmed).Errorf("topic is invalid; empty topic name")
				return
			}

			var parts []int32
			parts, err = parsePartitions(splitByColon[1], MaxTopicPartitions-expanded)
			if err != nil {
				return
			}
			expanded += len(parts)

			offset := defaultOffset
			if len(splitByColon) == 3 {
				offset, err = strconv.ParseInt(splitByColon[2], 10, 64)
				if err != nil {
					return
				}
			}

			if topicPartitions == nil {
				topicPartitions = map[string]map[int32]int64{}
			}

			partMap, exists := topicPartitions[topic]
			if !exists {
				partMap = map[int32]int64{}
				topicPartitions[topic] = partMap
			}

			for _, p := range parts {
				_, alreadyMapped := partMap[p]
				if offset == defaultOffset && alreadyMapped {
					continue
				}
				partMap[p] = offset
			}
		}
	}
	return
}
