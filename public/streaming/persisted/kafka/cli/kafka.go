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
// kafka: kcat-style subcommand for the pebble CLI, layered over the
// streaming/persisted/kafka package. Three nested commands today
// (consume, produce, list); urfave/cli/v2 leaves room to grow.
//
// Examples:
//
//	./pebble.sh kafka consume -b localhost:9092 -t orders -G my-group
//	./pebble.sh kafka consume -b localhost:9092 -t events -e -c 10
//	echo 'hello' | ./pebble.sh kafka produce -b localhost:9092 -t events
//	./pebble.sh kafka list -b localhost:9092

package kafka

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
	cli "github.com/urfave/cli/v2"

	pkafka "github.com/stergiotis/boxer/public/streaming/persisted/kafka"
)

// NewCliCommand returns the top-level "kafka" command, suitable for
// registration in [src/go/app/app.go]'s Commands slice.
func NewCliCommand() *cli.Command {
	return &cli.Command{
		Name:  "kafka",
		Usage: "kcat-style operations over the streaming/persisted/kafka package",
		Subcommands: []*cli.Command{
			consumeCmd(),
			produceCmd(),
			listCmd(),
		},
	}
}

// commonFlags returns the flags every subcommand shares.
func commonFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:     "brokers",
			Aliases:  []string{"b"},
			Required: true,
			Usage:    "comma-separated list of broker addresses (host1:9092,host2:9092)",
			EnvVars:  []string{"PEBBLE_KAFKA_BROKERS"},
		},
		&cli.StringFlag{
			Name:    "client-id",
			Value:   "pebble",
			Usage:   "kafka client.id",
			EnvVars: []string{"PEBBLE_KAFKA_CLIENT_ID"},
		},
	}
}

func makeConnectionDetails(c *cli.Context) (d pkafka.FranzConnectionDetails) {
	d = pkafka.DefaultFranzConnectionDetails()
	d.SeedBrokers = strings.Split(c.String("brokers"), ",")
	d.ClientID = c.String("client-id")
	d.Logger = &log.Logger
	return
}

//------------------------------------------------------------------------------
// consume

func consumeCmd() *cli.Command {
	return &cli.Command{
		Name:    "consume",
		Aliases: []string{"C"},
		Usage:   "consume records from a topic and write them to stdout",
		Flags: append(commonFlags(),
			&cli.StringFlag{
				Name:     "topic",
				Aliases:  []string{"t"},
				Required: true,
				Usage:    "topic to consume from (supports the topic[:partition[:offset]] syntax)",
			},
			&cli.StringFlag{
				Name:    "group",
				Aliases: []string{"G"},
				Usage:   "consumer group; if empty, partitions of `topic` are consumed without group coordination",
			},
			&cli.StringFlag{
				Name:    "offset",
				Aliases: []string{"o"},
				Value:   "earliest",
				Usage:   "start offset: earliest, latest, committed, or a numeric offset",
			},
			&cli.StringFlag{
				Name:    "format",
				Aliases: []string{"f"},
				Value:   "%s\\n",
				Usage:   "output format: %t topic, %p partition, %o offset, %k key, %s value, %T timestamp-ms; \\n \\t escapes; %% literal",
			},
			&cli.IntFlag{
				Name:    "count",
				Aliases: []string{"c"},
				Value:   -1,
				Usage:   "exit after consuming N records (use -1 for unbounded)",
			},
			&cli.BoolFlag{
				Name:    "exit-on-eof",
				Aliases: []string{"e"},
				Usage:   "exit when all assigned partitions are idle for 3s (best-effort end-of-log signal)",
			},
		),
		Action: runConsume,
	}
}

func runConsume(c *cli.Context) (err error) {
	connDetails := makeConnectionDetails(c)

	consDetails := pkafka.DefaultFranzConsumerDetails()
	if err = consDetails.SetTopicSpec([]string{c.String("topic")}, true); err != nil {
		err = fmt.Errorf("topic spec: %w", err)
		return
	}
	consDetails.StartOffset, err = parseOffset(c.String("offset"))
	if err != nil {
		return
	}

	readerOpts := pkafka.DefaultFranzReaderOrderedOpts()
	readerOpts.ConsumerGroup = c.String("group")
	readerOpts.Logger = &log.Logger

	clientOptsFn := func() (opts []kgo.Opt, err error) {
		opts = append(opts, connDetails.FranzOpts()...)
		opts = append(opts, consDetails.FranzOpts()...)
		return
	}

	reader, err := pkafka.NewFranzReaderOrdered(readerOpts, clientOptsFn)
	if err != nil {
		err = fmt.Errorf("reader: %w", err)
		return
	}

	if err = reader.Connect(c.Context); err != nil {
		err = fmt.Errorf("connect: %w", err)
		return
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		_ = reader.Close(closeCtx)
		cancel()
	}()

	out := bufio.NewWriter(os.Stdout)
	defer func() { _ = out.Flush() }()

	fmtFn := compileFormat(c.String("format"))
	count := c.Int("count")
	exitOnEOF := c.Bool("exit-on-eof")
	const eofIdleTimeout = 3 * time.Second
	seen := 0

	for {
		readCtx := c.Context
		var idleCancel context.CancelFunc
		if exitOnEOF {
			readCtx, idleCancel = context.WithTimeout(c.Context, eofIdleTimeout)
		}
		batch, readErr := reader.Read(readCtx)
		if idleCancel != nil {
			idleCancel()
		}
		if readErr != nil {
			if errors.Is(readErr, context.Canceled) && c.Context.Err() != nil {
				return
			}
			if errors.Is(readErr, context.DeadlineExceeded) && exitOnEOF {
				return
			}
			err = fmt.Errorf("read: %w", readErr)
			return
		}

		for r := range batch.Records.RecordsAll() {
			if err = fmtFn(out, r); err != nil {
				err = fmt.Errorf("format: %w", err)
				return
			}
			seen++
			if count > 0 && seen >= count {
				_ = batch.Ack(c.Context, nil)
				return
			}
		}
		if err = batch.Ack(c.Context, nil); err != nil {
			err = fmt.Errorf("ack: %w", err)
			return
		}
		_ = out.Flush()
	}
}

//------------------------------------------------------------------------------
// produce

func produceCmd() *cli.Command {
	return &cli.Command{
		Name:    "produce",
		Aliases: []string{"P"},
		Usage:   "read newline-delimited messages from stdin and produce them to a topic",
		Flags: append(commonFlags(),
			&cli.StringFlag{
				Name:     "topic",
				Aliases:  []string{"t"},
				Required: true,
				Usage:    "topic to produce to",
			},
			&cli.StringFlag{
				Name:    "key-delimiter",
				Aliases: []string{"K"},
				Usage:   "if set, split each input line at the first occurrence; left half is the record key, right half the value",
			},
		),
		Action: runProduce,
	}
}

func runProduce(c *cli.Context) (err error) {
	connDetails := makeConnectionDetails(c)
	prodOpts := pkafka.DefaultFranzProducerOpts()

	kgoOpts := append([]kgo.Opt{}, connDetails.FranzOpts()...)
	kgoOpts = append(kgoOpts, prodOpts.FranzOpts()...)

	client, err := pkafka.NewFranzClient(c.Context, kgoOpts...)
	if err != nil {
		err = fmt.Errorf("connect: %w", err)
		return
	}
	defer client.Close()

	writer, err := pkafka.NewFranzWriter(client, &log.Logger)
	if err != nil {
		err = fmt.Errorf("writer: %w", err)
		return
	}
	if err = writer.Connect(c.Context); err != nil {
		err = fmt.Errorf("writer connect: %w", err)
		return
	}
	defer func() {
		flushCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		_ = writer.Close(flushCtx)
		cancel()
	}()

	topic := c.String("topic")
	keyDelim := c.String("key-delimiter")

	scanner := bufio.NewScanner(os.Stdin)
	// Allow lines up to 64 MiB; brokers reject larger anyway.
	scanner.Buffer(make([]byte, 64*1024), 64*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		rec := &kgo.Record{Topic: topic}
		if keyDelim != "" {
			idx := strings.Index(string(line), keyDelim)
			if idx >= 0 {
				rec.Key = append([]byte{}, line[:idx]...)
				rec.Value = append([]byte{}, line[idx+len(keyDelim):]...)
			} else {
				rec.Value = append([]byte{}, line...)
			}
		} else {
			rec.Value = append([]byte{}, line...)
		}
		if err = writer.Write(c.Context, rec); err != nil {
			err = fmt.Errorf("write: %w", err)
			return
		}
		if c.Context.Err() != nil {
			return
		}
	}
	if scanErr := scanner.Err(); scanErr != nil {
		err = fmt.Errorf("read stdin: %w", scanErr)
	}
	return
}

//------------------------------------------------------------------------------
// list

func listCmd() *cli.Command {
	return &cli.Command{
		Name:    "list",
		Aliases: []string{"L"},
		Usage:   "fetch and print broker / topic metadata",
		Flags:   commonFlags(),
		Action:  runList,
	}
}

func runList(c *cli.Context) (err error) {
	connDetails := makeConnectionDetails(c)

	client, err := pkafka.NewFranzClient(c.Context, connDetails.FranzOpts()...)
	if err != nil {
		err = fmt.Errorf("connect: %w", err)
		return
	}
	defer client.Close()

	adm := kadm.NewClient(client)
	md, err := adm.Metadata(c.Context)
	if err != nil {
		err = fmt.Errorf("metadata: %w", err)
		return
	}

	out := bufio.NewWriter(os.Stdout)
	defer func() { _ = out.Flush() }()

	fmt.Fprintf(out, "Cluster: %s\n", md.Cluster)
	if md.Controller != -1 {
		fmt.Fprintf(out, "Controller: %d\n", md.Controller)
	}
	fmt.Fprintf(out, "\nBrokers (%d):\n", len(md.Brokers))
	for _, b := range md.Brokers {
		rack := "(none)"
		if b.Rack != nil {
			rack = *b.Rack
		}
		fmt.Fprintf(out, "  id=%d host=%s:%d rack=%s\n", b.NodeID, b.Host, b.Port, rack)
	}

	fmt.Fprintf(out, "\nTopics (%d):\n", len(md.Topics))
	topicNames := make([]string, 0, len(md.Topics))
	for name := range md.Topics {
		topicNames = append(topicNames, name)
	}
	sort.Strings(topicNames)
	for _, name := range topicNames {
		t := md.Topics[name]
		fmt.Fprintf(out, "  %s (%d partitions, internal=%v)\n", t.Topic, len(t.Partitions), t.IsInternal)
		partIDs := make([]int32, 0, len(t.Partitions))
		for p := range t.Partitions {
			partIDs = append(partIDs, p)
		}
		sort.Slice(partIDs, func(i, j int) bool { return partIDs[i] < partIDs[j] })
		for _, p := range partIDs {
			pi := t.Partitions[p]
			fmt.Fprintf(out, "    partition=%d leader=%d replicas=%v isr=%v\n", pi.Partition, pi.Leader, pi.Replicas, pi.ISR)
		}
	}
	return
}

//------------------------------------------------------------------------------
// helpers

// parseOffset converts the --offset flag value to a kgo.Offset.
func parseOffset(s string) (off kgo.Offset, err error) {
	switch s {
	case "earliest":
		off = kgo.NewOffset().AtStart()
	case "latest":
		off = kgo.NewOffset().AtEnd()
	case "committed":
		off = kgo.NewOffset().AtCommitted()
	default:
		var n int64
		n, err = strconv.ParseInt(s, 10, 64)
		if err != nil {
			err = fmt.Errorf("invalid offset %q: %w", s, err)
			return
		}
		off = kgo.NewOffset().At(n)
	}
	return
}

// formatter writes one record's formatted output to w.
type formatter func(w io.Writer, r *kgo.Record) (err error)

// compileFormat returns a formatter that interprets a kcat-style format
// string. Verbs: %t topic, %p partition, %o offset, %k key, %s value,
// %T timestamp (millis), %% literal %. Escape sequences \n \t \\ are
// recognised so callers can pass `-f "%t/%p:%o %s\n"` from a shell
// without losing the newline.
func compileFormat(format string) (fn formatter) {
	fn = func(w io.Writer, r *kgo.Record) (err error) {
		i := 0
		for i < len(format) {
			ch := format[i]
			if ch == '%' && i+1 < len(format) {
				if err = writeVerb(w, format[i+1], r); err != nil {
					return
				}
				i += 2
				continue
			}
			if ch == '\\' && i+1 < len(format) {
				if err = writeEscape(w, format[i+1]); err != nil {
					return
				}
				i += 2
				continue
			}
			if _, err = w.Write([]byte{ch}); err != nil {
				return
			}
			i++
		}
		return
	}
	return
}

func writeVerb(w io.Writer, verb byte, r *kgo.Record) (err error) {
	switch verb {
	case 't':
		_, err = io.WriteString(w, r.Topic)
	case 'p':
		_, err = fmt.Fprintf(w, "%d", r.Partition)
	case 'o':
		_, err = fmt.Fprintf(w, "%d", r.Offset)
	case 'k':
		_, err = w.Write(r.Key)
	case 's':
		_, err = w.Write(r.Value)
	case 'T':
		_, err = fmt.Fprintf(w, "%d", r.Timestamp.UnixMilli())
	case '%':
		_, err = w.Write([]byte{'%'})
	default:
		_, err = fmt.Fprintf(w, "%%%c", verb)
	}
	return
}

func writeEscape(w io.Writer, esc byte) (err error) {
	switch esc {
	case 'n':
		_, err = w.Write([]byte{'\n'})
	case 't':
		_, err = w.Write([]byte{'\t'})
	case '\\':
		_, err = w.Write([]byte{'\\'})
	default:
		_, err = w.Write([]byte{'\\', esc})
	}
	return
}
