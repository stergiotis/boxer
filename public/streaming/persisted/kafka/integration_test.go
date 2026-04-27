//go:build integration && llm_generated_opus47

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
// This file is not a derivative of upstream Connect's integration tests
// — the upstream suite (~1500 lines across 4 files) is heavily coupled
// to the Benthos service-framework, and a verbatim port would not
// exercise any pebble2impl-specific invariant. The shape of the
// testcontainers + topic-creation-retry plumbing is inspired by
// upstream's integration_test.go (the INVALID_PARTITIONS retry loop in
// particular), but the test logic itself targets this port's exported
// API directly.
//
// Run with the integration tag enabled:
//
//	go test -tags "$(cat tags | tr -d $'\n'),integration" \
//	  ./public/streaming/persisted/kafka/...
//
// Requires Docker — testcontainers-go spins up a redpandadata/redpanda
// container per top-level Test* function.

package kafka_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/modules/redpanda"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/stergiotis/boxer/public/streaming/persisted/kafka"
)

const redpandaImage = "redpandadata/redpanda:v23.3.10"

// startRedpanda spins up a single-broker Redpanda container scoped to
// the test's lifetime and returns its plaintext-listener address.
func startRedpanda(t *testing.T) (brokerAddr string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	container, err := redpanda.Run(ctx, redpandaImage)
	require.NoError(t, err, "start redpanda container")
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer stopCancel()
		_ = container.Terminate(stopCtx)
	})

	brokerAddr, err = container.KafkaSeedBroker(ctx)
	require.NoError(t, err, "resolve broker address")
	return
}

// createKafkaTopic creates a topic with the given partition count.
// Retries on INVALID_PARTITIONS — Redpanda's testcontainers image
// occasionally rejects the first CreateTopics request right after
// startup; a fresh client + short delay resolves it.
func createKafkaTopic(t *testing.T, brokerAddr, topic string, partitions int32) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var lastErr error
	for range 5 {
		cl, err := kgo.NewClient(kgo.SeedBrokers(brokerAddr))
		require.NoError(t, err, "kgo client for topic creation")
		adm := kadm.NewClient(cl)
		_, err = adm.CreateTopic(ctx, partitions, 1, nil, topic)
		cl.Close()
		if err == nil {
			return
		}
		if !errors.Is(err, kerr.InvalidPartitions) {
			require.NoError(t, err, "create topic %q", topic)
		}
		lastErr = err
		select {
		case <-time.After(time.Second):
		case <-ctx.Done():
			require.NoError(t, ctx.Err(), "create topic deadline")
		}
	}
	require.NoError(t, lastErr, "create topic %q (after retries)", topic)
}

// safeName converts t.Name() into a valid Kafka topic / consumer-group
// identifier — Kafka rejects '/' which appears in subtest names.
func safeName(t *testing.T) (s string) {
	s = strings.ReplaceAll(t.Name(), "/", "-")
	return
}

// TestIntegrationConnectivity validates the basic FranzConnectionDetails
// → NewFranzClient → Ping path against a live broker.
func TestIntegrationConnectivity(t *testing.T) {
	addr := startRedpanda(t)

	details := kafka.FranzConnectionDetails{
		SeedBrokers: []string{addr},
		ClientID:    "pebble2impl-conn-test",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cl, err := kafka.NewFranzClient(ctx, details.FranzOpts()...)
	require.NoError(t, err, "NewFranzClient")
	defer cl.Close()

	require.NoError(t, cl.Ping(ctx), "ping")
}

// TestIntegrationProduceConsume runs a produce → consume round-trip
// against a real broker, once for each reader mode.
func TestIntegrationProduceConsume(t *testing.T) {
	addr := startRedpanda(t)

	t.Run("ordered", func(t *testing.T) {
		runRoundTrip(t, addr, false)
	})
	t.Run("unordered", func(t *testing.T) {
		runRoundTrip(t, addr, true)
	})
}

// runRoundTrip writes numRecords records via FranzWriter, reads them
// back via FranzReaderToggled (mode chosen by `unordered`), and
// asserts every record arrives with the expected key/value.
func runRoundTrip(t *testing.T, brokerAddr string, unordered bool) {
	t.Helper()
	const numRecords = 50
	topic := fmt.Sprintf("rt-%s", safeName(t))
	group := fmt.Sprintf("grp-%s", safeName(t))

	createKafkaTopic(t, brokerAddr, topic, 4)

	connDetails := kafka.FranzConnectionDetails{
		SeedBrokers: []string{brokerAddr},
		ClientID:    "pebble2impl-rt",
	}

	// ---- Producer
	prodOpts := kafka.DefaultFranzProducerOpts()
	prodKgoOpts := append([]kgo.Opt{}, connDetails.FranzOpts()...)
	prodKgoOpts = append(prodKgoOpts, prodOpts.FranzOpts()...)

	prodCtx, prodCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer prodCancel()

	prodClient, err := kafka.NewFranzClient(prodCtx, prodKgoOpts...)
	require.NoError(t, err, "NewFranzClient (producer)")
	defer prodClient.Close()

	writer, err := kafka.NewFranzWriter(prodClient, nil)
	require.NoError(t, err, "NewFranzWriter")
	require.NoError(t, writer.Connect(prodCtx), "writer.Connect")

	records := make([]*kgo.Record, numRecords)
	for i := range records {
		records[i] = &kgo.Record{
			Topic: topic,
			Key:   fmt.Appendf(nil, "key-%d", i),
			Value: fmt.Appendf(nil, "value-%d", i),
		}
	}
	require.NoError(t, writer.Write(prodCtx, records...), "writer.Write")
	require.NoError(t, writer.Close(prodCtx), "writer.Close")

	// ---- Consumer
	consDetails := kafka.DefaultFranzConsumerDetails()
	consDetails.Topics = []string{topic}

	toggledOpts := kafka.DefaultFranzReaderToggledOpts()
	toggledOpts.Unordered = unordered
	if unordered {
		toggledOpts.UnorderedOpts.ConsumerGroup = group
	} else {
		toggledOpts.OrderedOpts.ConsumerGroup = group
	}

	clientOptsFn := func() (opts []kgo.Opt, err error) {
		opts = append(opts, connDetails.FranzOpts()...)
		opts = append(opts, consDetails.FranzOpts()...)
		return
	}

	reader, err := kafka.NewFranzReaderToggled(toggledOpts, clientOptsFn)
	require.NoError(t, err, "NewFranzReaderToggled")

	readCtx, readCancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer readCancel()

	require.NoError(t, reader.Connect(readCtx), "reader.Connect")

	seen := make(map[string]string, numRecords)
	for len(seen) < numRecords {
		batch, err := reader.Read(readCtx)
		require.NoError(t, err, "reader.Read after %d records seen", len(seen))
		for r := range batch.Records.RecordsAll() {
			seen[string(r.Key)] = string(r.Value)
		}
		require.NoError(t, batch.Ack(readCtx, nil), "batch.Ack")
	}

	closeCtx, closeCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer closeCancel()
	require.NoError(t, reader.Close(closeCtx), "reader.Close")

	assert.Len(t, seen, numRecords, "all records consumed")
	for i := range numRecords {
		key := fmt.Sprintf("key-%d", i)
		wantVal := fmt.Sprintf("value-%d", i)
		assert.Equal(t, wantVal, seen[key], "missing or wrong value for %s", key)
	}
}
