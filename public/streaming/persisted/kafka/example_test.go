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
// Testable Go examples for the broker-free surface of the
// streaming/persisted/kafka package. Each Example* function carries
// an // Output: (or // Unordered output:) block so `go test` both
// compiles and asserts the recipe — pkgsite renders these as
// inline documentation.

package kafka_test

import (
	"fmt"

	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/stergiotis/boxer/public/streaming/persisted/kafka"
)

func ExampleParseTopics() {
	topics, partitions, err := kafka.ParseTopics(
		[]string{"foo", "bar:0-2", "baz:3:100"},
		-1,   // defaultOffset
		true, // allowExplicitOffsets
	)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println("topics:", topics)
	fmt.Println("partitions:")
	for _, t := range []string{"bar", "baz"} {
		for p := int32(0); p < 4; p++ {
			off, ok := partitions[t][p]
			if !ok {
				continue
			}
			fmt.Printf("  %s:%d offset=%d\n", t, p, off)
		}
	}
	// Output:
	// topics: [foo]
	// partitions:
	//   bar:0 offset=-1
	//   bar:1 offset=-1
	//   bar:2 offset=-1
	//   baz:3 offset=100
}

func ExampleSASLMechanismE_String() {
	fmt.Println(kafka.SASLMechanismNone)
	fmt.Println(kafka.SASLMechanismPlain)
	fmt.Println(kafka.SASLMechanismOAuthBearer)
	fmt.Println(kafka.SASLMechanismSCRAMSHA256)
	fmt.Println(kafka.SASLMechanismSCRAMSHA512)
	// Output:
	// none
	// PLAIN
	// OAUTHBEARER
	// SCRAM-SHA-256
	// SCRAM-SHA-512
}

func ExampleSASLMechanisms() {
	mechs, err := kafka.SASLMechanisms([]kafka.SASLConfig{
		{Mechanism: kafka.SASLMechanismNone}, // skipped
		{Mechanism: kafka.SASLMechanismPlain, Username: "alice", Password: "s3cret"},
		{Mechanism: kafka.SASLMechanismSCRAMSHA512, Username: "alice", Password: "s3cret"},
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Printf("got %d franz-go mechanisms\n", len(mechs))
	for _, m := range mechs {
		fmt.Println("  -", m.Name())
	}
	// Output:
	// got 2 franz-go mechanisms
	//   - PLAIN
	//   - SCRAM-SHA-512
}

func ExampleGetHeaderValue() {
	headers := []kgo.RecordHeader{
		{Key: "trace-id", Value: []byte("abc-123")},
		{Key: "user-id", Value: []byte("alice")},
	}

	val, ok := kafka.GetHeaderValue(headers, "trace-id")
	fmt.Printf("trace-id: ok=%v value=%q\n", ok, val)

	val, ok = kafka.GetHeaderValue(headers, "missing")
	fmt.Printf("missing : ok=%v value=%q\n", ok, val)
	// Output:
	// trace-id: ok=true value="abc-123"
	// missing : ok=false value=""
}

func ExampleSetHeaderValue() {
	headers := []kgo.RecordHeader{
		{Key: "x", Value: []byte("1")},
	}
	headers = kafka.SetHeaderValue(headers, "y", []byte("2"))   // appends
	headers = kafka.SetHeaderValue(headers, "x", []byte("99"))  // updates
	for _, h := range headers {
		fmt.Printf("%s=%s\n", h.Key, h.Value)
	}
	// Output:
	// x=99
	// y=2
}

func ExampleDefaultFranzConsumerDetails() {
	d := kafka.DefaultFranzConsumerDetails()
	if err := d.SetTopicSpec([]string{"orders:0-2", "alerts"}, false); err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println("topics:", d.Topics)
	fmt.Println("partitions for orders:")
	for p := int32(0); p < 4; p++ {
		off, ok := d.TopicPartitions["orders"][p]
		if !ok {
			continue
		}
		fmt.Printf("  partition=%d start=%v\n", p, off.EpochOffset().Offset)
	}
	// Output:
	// topics: [alerts]
	// partitions for orders:
	//   partition=0 start=-2
	//   partition=1 start=-2
	//   partition=2 start=-2
}
