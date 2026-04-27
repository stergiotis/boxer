//go:build llm_generated_opus47

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
// Validation- and option-builder unit tests. Broker-free; covers
// the constructors' error paths and the zero-value-duration
// regression for FranzConnectionDetails.FranzOpts (the bug caught
// by Phase 9 against Podman; see commit 58ba155f).

package kafka_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/stergiotis/boxer/public/streaming/persisted/kafka"
)

func TestNewFranzReaderOrderedValidation(t *testing.T) {
	stubClientOpts := func() ([]kgo.Opt, error) { return nil, nil }

	tests := []struct {
		name     string
		mutate   func(*kafka.FranzReaderOrderedOpts)
		errMatch string
	}{
		{
			name:     "PartitionBufferBytes zero",
			mutate:   func(o *kafka.FranzReaderOrderedOpts) { o.PartitionBufferBytes = 0 },
			errMatch: "PartitionBufferBytes must be > 0",
		},
		{
			name:     "PartitionBufferBytes negative",
			mutate:   func(o *kafka.FranzReaderOrderedOpts) { o.PartitionBufferBytes = -1 },
			errMatch: "PartitionBufferBytes must be > 0",
		},
		{
			name:     "MaxYieldBatchBytes zero",
			mutate:   func(o *kafka.FranzReaderOrderedOpts) { o.MaxYieldBatchBytes = 0 },
			errMatch: "MaxYieldBatchBytes must be > 0",
		},
		{
			name: "MaxYieldBatchBytes greater than PartitionBufferBytes",
			mutate: func(o *kafka.FranzReaderOrderedOpts) {
				o.PartitionBufferBytes = 1024
				o.MaxYieldBatchBytes = 2048
			},
			errMatch: "must be <= PartitionBufferBytes",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opts := kafka.DefaultFranzReaderOrderedOpts()
			tc.mutate(&opts)
			r, err := kafka.NewFranzReaderOrdered(opts, stubClientOpts)
			require.Error(t, err)
			require.Nil(t, r)
			assert.Contains(t, err.Error(), tc.errMatch)
		})
	}
}

func TestNewFranzReaderOrderedDefaultsValid(t *testing.T) {
	stubClientOpts := func() ([]kgo.Opt, error) { return nil, nil }
	r, err := kafka.NewFranzReaderOrdered(kafka.DefaultFranzReaderOrderedOpts(), stubClientOpts)
	require.NoError(t, err)
	require.NotNil(t, r)
}

func TestNewFranzReaderUnorderedValidation(t *testing.T) {
	stubClientOpts := func() ([]kgo.Opt, error) { return nil, nil }

	for _, tc := range []struct {
		name     string
		limit    int32
		errMatch string
	}{
		{name: "zero", limit: 0, errMatch: "CheckpointLimit must be > 0"},
		{name: "negative", limit: -1, errMatch: "CheckpointLimit must be > 0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := kafka.DefaultFranzReaderUnorderedOpts()
			opts.CheckpointLimit = tc.limit
			r, err := kafka.NewFranzReaderUnordered(opts, stubClientOpts)
			require.Error(t, err)
			require.Nil(t, r)
			assert.Contains(t, err.Error(), tc.errMatch)
		})
	}
}

func TestNewFranzReaderUnorderedDefaultsValid(t *testing.T) {
	stubClientOpts := func() ([]kgo.Opt, error) { return nil, nil }
	r, err := kafka.NewFranzReaderUnordered(kafka.DefaultFranzReaderUnorderedOpts(), stubClientOpts)
	require.NoError(t, err)
	require.NotNil(t, r)
}

func TestNewFranzWriterNilClient(t *testing.T) {
	w, err := kafka.NewFranzWriter(nil, nil)
	require.Error(t, err)
	require.Nil(t, w)
	assert.Contains(t, err.Error(), "client must not be nil")
}

func TestSASLMechanismsUnsupported(t *testing.T) {
	// A value outside the defined SASLMechanism* constants reaches the
	// default arm and reports ErrUnsupportedSASLMechanism with the
	// offending slice index.
	const bogus kafka.SASLMechanismE = 99
	mechs, err := kafka.SASLMechanisms([]kafka.SASLConfig{{Mechanism: bogus}})
	require.Error(t, err)
	require.Nil(t, mechs)
	assert.True(t, errors.Is(err, kafka.ErrUnsupportedSASLMechanism), "wraps ErrUnsupportedSASLMechanism")
	assert.Contains(t, err.Error(), "mechanism 0:")
}

// TestFranzConnectionDetailsZeroDurationsAreUsable is a regression
// test for the bug fixed in 58ba155f: FranzConnectionDetails{} (with
// only SeedBrokers populated) used to emit kgo.RequestTimeoutOverhead(0)
// which kgo rejects with "request timeout min overhead 0s is less
// than allowed 100ms". FranzOpts must skip zero-duration options so
// kgo's own defaults take over.
//
// This test does not Ping the client — kgo.NewClient validates option
// shape at construction without contacting a broker.
func TestFranzConnectionDetailsZeroDurationsAreUsable(t *testing.T) {
	d := kafka.FranzConnectionDetails{
		SeedBrokers: []string{"localhost:9092"},
		ClientID:    "regression",
	}
	opts := d.FranzOpts()

	// Sanity: all returned opts are non-nil; NewClient accepts the slice.
	for i, o := range opts {
		assert.NotNil(t, o, "opts[%d]", i)
	}

	cl, err := kgo.NewClient(opts...)
	require.NoError(t, err, "kgo.NewClient with zero-duration FranzConnectionDetails")
	defer cl.Close()
}

func TestFranzConnectionDetailsDefaultsAreUsable(t *testing.T) {
	d := kafka.DefaultFranzConnectionDetails()
	d.SeedBrokers = []string{"localhost:9092"}
	d.ClientID = "default"

	cl, err := kgo.NewClient(d.FranzOpts()...)
	require.NoError(t, err)
	defer cl.Close()
}

func TestFranzProducerOptsDefaultsAreUsable(t *testing.T) {
	d := kafka.DefaultFranzConnectionDetails()
	d.SeedBrokers = []string{"localhost:9092"}

	p := kafka.DefaultFranzProducerOpts()

	all := append([]kgo.Opt{}, d.FranzOpts()...)
	all = append(all, p.FranzOpts()...)

	cl, err := kgo.NewClient(all...)
	require.NoError(t, err)
	defer cl.Close()
}

func TestFranzConsumerDetailsDefaultsAreUsable(t *testing.T) {
	d := kafka.DefaultFranzConnectionDetails()
	d.SeedBrokers = []string{"localhost:9092"}

	c := kafka.DefaultFranzConsumerDetails()
	c.Topics = []string{"any"}

	all := append([]kgo.Opt{}, d.FranzOpts()...)
	all = append(all, c.FranzOpts()...)

	cl, err := kgo.NewClient(all...)
	require.NoError(t, err)
	defer cl.Close()
}
