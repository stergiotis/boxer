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
//   - Dropped AddHeaders and ExtractHeaders, which converted between
//     []kgo.RecordHeader and service.Message metadata. The pebble2impl
//     port treats *kgo.Record as the message envelope (see
//     EXPLANATION.md §"Why concrete types"); application code reads
//     kgo.Record.Headers directly without an intermediate metadata
//     hop.
//   - Dropped the kafkaHeaders metadata key constant, only used by
//     the above.
//   - Renamed return values per boxer coding standards. Lookup and
//     mutation logic preserved verbatim.

package kafka

import (
	"github.com/twmb/franz-go/pkg/kgo"
)

// GetHeaderValue retrieves the last header value matching the given
// key. Returns nil and ok=false if the key is not found. The returned
// slice references the original header data and must not be modified.
func GetHeaderValue(headers []kgo.RecordHeader, key string) (val []byte, ok bool) {
	for i := range headers {
		h := &headers[len(headers)-1-i]
		if h.Key == key {
			val = h.Value
			ok = true
			return
		}
	}
	return
}

// SetHeaderValue sets the last header value matching the given key. If
// the key is not found, a new header is appended to the end of the
// list. The returned slice may share backing storage with the input
// slice.
func SetHeaderValue(headers []kgo.RecordHeader, key string, value []byte) (out []kgo.RecordHeader) {
	for i := range headers {
		h := &headers[len(headers)-1-i]
		if h.Key == key {
			h.Value = value
			out = headers
			return
		}
	}
	out = append(headers, kgo.RecordHeader{
		Key:   key,
		Value: value,
	})
	return
}
