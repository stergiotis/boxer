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
	"crypto/tls"
	"crypto/x509"
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
	"github.com/twmb/franz-go/pkg/sasl"
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

// commonFlags returns the flags every subcommand shares: connection
// (brokers, client-id), SASL (mechanism + credentials), and TLS
// (enable + cert/key/ca paths).
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

		// SASL
		&cli.StringFlag{
			Name:    "sasl-mechanism",
			Usage:   "SASL mechanism: none (default), PLAIN, SCRAM-SHA-256, SCRAM-SHA-512, OAUTHBEARER",
			EnvVars: []string{"PEBBLE_KAFKA_SASL_MECHANISM"},
		},
		&cli.StringFlag{
			Name:    "sasl-username",
			Usage:   "SASL username (PLAIN, SCRAM-SHA-256, SCRAM-SHA-512)",
			EnvVars: []string{"PEBBLE_KAFKA_SASL_USERNAME"},
		},
		&cli.StringFlag{
			Name:    "sasl-password",
			Usage:   "SASL password (PLAIN, SCRAM-SHA-256, SCRAM-SHA-512); prefer the env var to avoid shell history",
			EnvVars: []string{"PEBBLE_KAFKA_SASL_PASSWORD"},
		},
		&cli.StringFlag{
			Name:    "sasl-token",
			Usage:   "static OAUTHBEARER token (only for --sasl-mechanism=OAUTHBEARER)",
			EnvVars: []string{"PEBBLE_KAFKA_SASL_TOKEN"},
		},

		// TLS
		&cli.BoolFlag{
			Name:    "tls",
			Usage:   "enable TLS (implicit if any --tls-* file flag is set)",
			EnvVars: []string{"PEBBLE_KAFKA_TLS"},
		},
		&cli.StringFlag{
			Name:    "tls-ca-file",
			Usage:   "path to a PEM-encoded CA bundle for verifying the broker certificate",
			EnvVars: []string{"PEBBLE_KAFKA_TLS_CA_FILE"},
		},
		&cli.StringFlag{
			Name:    "tls-cert-file",
			Usage:   "path to a PEM-encoded client certificate (for mTLS); requires --tls-key-file",
			EnvVars: []string{"PEBBLE_KAFKA_TLS_CERT_FILE"},
		},
		&cli.StringFlag{
			Name:    "tls-key-file",
			Usage:   "path to a PEM-encoded client key (for mTLS); requires --tls-cert-file",
			EnvVars: []string{"PEBBLE_KAFKA_TLS_KEY_FILE"},
		},
		&cli.BoolFlag{
			Name:    "tls-skip-verify",
			Usage:   "skip broker certificate verification (insecure; useful for self-signed dev clusters)",
			EnvVars: []string{"PEBBLE_KAFKA_TLS_SKIP_VERIFY"},
		},
	}
}

func makeConnectionDetails(c *cli.Context) (d pkafka.FranzConnectionDetails, err error) {
	d = pkafka.DefaultFranzConnectionDetails()
	d.SeedBrokers = strings.Split(c.String("brokers"), ",")
	d.ClientID = c.String("client-id")
	d.Logger = &log.Logger

	d.SASL, err = buildSASL(c)
	if err != nil {
		return
	}

	d.TLSEnabled, d.TLSConf, err = buildTLS(c)
	if err != nil {
		return
	}
	return
}

// parseSASLMechanism maps the user-supplied --sasl-mechanism string
// (case-insensitive) to a SASLMechanismE. Empty string and "none" both
// disable SASL.
func parseSASLMechanism(s string) (m pkafka.SASLMechanismE, err error) {
	switch strings.ToUpper(s) {
	case "", "NONE":
		m = pkafka.SASLMechanismNone
	case "PLAIN":
		m = pkafka.SASLMechanismPlain
	case "SCRAM-SHA-256":
		m = pkafka.SASLMechanismSCRAMSHA256
	case "SCRAM-SHA-512":
		m = pkafka.SASLMechanismSCRAMSHA512
	case "OAUTHBEARER":
		m = pkafka.SASLMechanismOAuthBearer
	default:
		err = fmt.Errorf("unsupported --sasl-mechanism %q (try PLAIN, SCRAM-SHA-256, SCRAM-SHA-512, OAUTHBEARER, or none)", s)
	}
	return
}

func buildSASL(c *cli.Context) (mechs []sasl.Mechanism, err error) {
	mech, err := parseSASLMechanism(c.String("sasl-mechanism"))
	if err != nil {
		return
	}
	if mech == pkafka.SASLMechanismNone {
		return
	}
	mechs, err = pkafka.SASLMechanisms([]pkafka.SASLConfig{{
		Mechanism: mech,
		Username:  c.String("sasl-username"),
		Password:  c.String("sasl-password"),
		Token:     c.String("sasl-token"),
	}})
	return
}

// buildTLS constructs a *tls.Config from the --tls-* flags. Returns
// enabled=false when no TLS-related flag is set so plaintext clusters
// require no extra ceremony.
func buildTLS(c *cli.Context) (enabled bool, cfg *tls.Config, err error) {
	caFile := c.String("tls-ca-file")
	certFile := c.String("tls-cert-file")
	keyFile := c.String("tls-key-file")
	skipVerify := c.Bool("tls-skip-verify")
	enabled = c.Bool("tls") || caFile != "" || certFile != "" || keyFile != "" || skipVerify
	if !enabled {
		return
	}

	cfg = &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: skipVerify,
	}

	if caFile != "" {
		var caPEM []byte
		caPEM, err = os.ReadFile(caFile)
		if err != nil {
			err = fmt.Errorf("read --tls-ca-file %q: %w", caFile, err)
			return
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caPEM) {
			err = fmt.Errorf("--tls-ca-file %q contains no PEM certificates", caFile)
			return
		}
		cfg.RootCAs = pool
	}

	if certFile != "" || keyFile != "" {
		if certFile == "" || keyFile == "" {
			err = fmt.Errorf("--tls-cert-file and --tls-key-file must be set together")
			return
		}
		var pair tls.Certificate
		pair, err = tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			err = fmt.Errorf("load TLS keypair: %w", err)
			return
		}
		cfg.Certificates = []tls.Certificate{pair}
	}
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
	connDetails, err := makeConnectionDetails(c)
	if err != nil {
		err = fmt.Errorf("connection: %w", err)
		return
	}

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
	connDetails, err := makeConnectionDetails(c)
	if err != nil {
		err = fmt.Errorf("connection: %w", err)
		return
	}
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
	connDetails, err := makeConnectionDetails(c)
	if err != nil {
		err = fmt.Errorf("connection: %w", err)
		return
	}

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
