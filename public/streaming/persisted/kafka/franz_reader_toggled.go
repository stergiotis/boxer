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
//   - Dropped the Benthos config-DSL surface entirely: the krtField*
//     constants, FranzReaderToggledConfigFields, and
//     NewFranzReaderToggledFromConfig. Upstream's "toggled" wrapper
//     existed primarily to expose a single YAML schema that switched
//     between the ordered and unordered readers via a nested
//     `unordered_processing.enabled` flag. Without YAML config, the
//     wrapper reduces to a constructor that picks the underlying
//     reader from a Go-struct field.
//   - Replaced the upstream return type service.BatchInput with the
//     package-local [ConsumerI]. Both [FranzReaderOrdered] and
//     [FranzReaderUnordered] satisfy ConsumerI directly; no extra
//     adapter is required.
//   - Refactored to boxer coding standards: named return values,
//     compile-time interface guarantee through the underlying
//     readers' var _ ConsumerI = (*Franz...)(nil) assertions.

package kafka

import (
	"github.com/twmb/franz-go/pkg/kgo"
)

// FranzReaderToggledOpts selects between an ordered and an unordered
// franz-go Kafka consumer at construction time. Set Unordered to
// false (the default) and populate OrderedOpts to use
// [FranzReaderOrdered]; set Unordered to true and populate
// UnorderedOpts to use [FranzReaderUnordered]. The opts for the
// inactive mode are ignored.
//
// This struct exists for callers that want a single configuration
// surface with a runtime-toggleable mode (typically driven by a
// command-line flag or an environment variable). Callers that always
// know which mode they want should construct
// [FranzReaderOrdered] / [FranzReaderUnordered] directly.
type FranzReaderToggledOpts struct {
	// Unordered selects the unordered reader when true.
	Unordered bool

	// OrderedOpts is used when Unordered is false.
	OrderedOpts FranzReaderOrderedOpts

	// UnorderedOpts is used when Unordered is true.
	UnorderedOpts FranzReaderUnorderedOpts
}

// DefaultFranzReaderToggledOpts returns toggled options whose
// per-mode sub-options match the upstream Connect defaults
// ([DefaultFranzReaderOrderedOpts], [DefaultFranzReaderUnorderedOpts]).
// The default mode is ordered (Unordered=false).
func DefaultFranzReaderToggledOpts() (opts FranzReaderToggledOpts) {
	opts = FranzReaderToggledOpts{
		OrderedOpts:   DefaultFranzReaderOrderedOpts(),
		UnorderedOpts: DefaultFranzReaderUnorderedOpts(),
	}
	return
}

// NewFranzReaderToggled constructs a [ConsumerI] backed by either
// [FranzReaderOrdered] or [FranzReaderUnordered], chosen by
// opts.Unordered. clientOpts is forwarded to the underlying
// constructor verbatim.
func NewFranzReaderToggled(opts FranzReaderToggledOpts, clientOpts func() (kgoOpts []kgo.Opt, err error)) (consumer ConsumerI, err error) {
	if opts.Unordered {
		consumer, err = NewFranzReaderUnordered(opts.UnorderedOpts, clientOpts)
		return
	}
	consumer, err = NewFranzReaderOrdered(opts.OrderedOpts, clientOpts)
	return
}
