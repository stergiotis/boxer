package kafka

// FuzzParseTopics feeds newline-separated topic specs to ParseTopics and
// checks:
//
//	total      — ParseTopics never panics on any spec, offset, or flag.
//	bounded    — an accepted spec names at most MaxTopicPartitions
//	             partitions in total, whatever its length.
//	consistent — every returned topic name is non-empty, trimmed, and free
//	             of the ',' and ':' separators; every partition map is
//	             non-empty with partitions >= 0; without explicit offsets
//	             every offset is the default.
//	stable     — the canonical form (plain topics, then one
//	             `topic:partition:offset` entry per mapped partition)
//	             re-parses to the same topics and partition map.
//
// Run e.g.:
//
//	go test -run xxx -fuzz FuzzParseTopics -fuzztime 60s ./public/streaming/persisted/kafka/

import (
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func FuzzParseTopics(f *testing.F) {
	f.Add("foo", int64(-1), false)
	f.Add(" foo, bar \nbaz ", int64(-1), false)
	f.Add("foo:5-7\nbar:0-4", int64(-2), false)
	f.Add("foo:4-6:3\nfoo:5:7", int64(-1), true)
	f.Add("foo:4-6:3,foo:5:-1", int64(-1), true)
	f.Add("t:0-2147483647", int64(0), false)
	f.Add("t:0-65535,t:65535", int64(0), false)
	f.Add("t:7-5", int64(0), false)
	f.Add(" :3", int64(0), true)
	f.Add("a:+1-+2:9", int64(0), true)

	f.Fuzz(func(t *testing.T, spec string, defaultOffset int64, allowOffsets bool) {
		topics, tps, err := ParseTopics(strings.Split(spec, "\n"), defaultOffset, allowOffsets)
		if err != nil {
			return // rejection is fine; panics and unbounded work are not
		}

		checkName := func(name string) {
			if name == "" || name != strings.TrimSpace(name) || strings.ContainsAny(name, ",:") {
				t.Fatalf("malformed topic name %q from %q", name, spec)
			}
		}
		for _, topic := range topics {
			checkName(topic)
		}
		total := 0
		for topic, parts := range tps {
			checkName(topic)
			if len(parts) == 0 {
				t.Fatalf("topic %q has an empty partition map from %q", topic, spec)
			}
			total += len(parts)
			for p, offset := range parts {
				if p < 0 {
					t.Fatalf("negative partition %d for %q from %q", p, topic, spec)
				}
				if !allowOffsets && offset != defaultOffset {
					t.Fatalf("offset %d without explicit offsets from %q", offset, spec)
				}
			}
		}
		if total > MaxTopicPartitions {
			t.Fatalf("%d partitions exceed MaxTopicPartitions from %q", total, spec)
		}

		canonical := slices.Clone(topics)
		for _, topic := range slices.Sorted(maps.Keys(tps)) {
			for _, p := range slices.Sorted(maps.Keys(tps[topic])) {
				canonical = append(canonical, topic+":"+strconv.Itoa(int(p))+":"+strconv.FormatInt(tps[topic][p], 10))
			}
		}
		topics2, tps2, err := ParseTopics(canonical, defaultOffset, true)
		if err != nil {
			t.Fatalf("canonical form of %q does not re-parse: %v\ncanonical: %q", spec, err, canonical)
		}
		if !slices.Equal(topics, topics2) || !reflect.DeepEqual(tps, tps2) {
			t.Fatalf("canonical form of %q re-parses differently:\ncanonical: %q\ntopics %q -> %q\npartitions %v -> %v", spec, canonical, topics, topics2, tps, tps2)
		}
	})
}
