package stevedoredemo

import (
	"context"
	"encoding/json/v2"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/urfave/cli/v2"

	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/keelson/data/storeexec"
	"github.com/stergiotis/boxer/public/observability/eh"
	pkafka "github.com/stergiotis/boxer/public/streaming/persisted/kafka"
	"github.com/stergiotis/boxer/public/streaming/stevedore"
	"github.com/stergiotis/boxer/public/streaming/stevedore/lander"
	"github.com/stergiotis/boxer/public/streaming/stevedore/stevedorefacts"
)

func newLandCommand() *cli.Command {
	return &cli.Command{
		Name:  "land",
		Usage: "consume items from a topic, print each as a JSON line, dead-letter what does not decode",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "brokers", Required: true, Usage: "comma-separated seed brokers"},
			&cli.StringFlag{Name: "topic", Required: true, Usage: "the items topic"},
			&cli.StringFlag{Name: "group", Value: "stevedoredemo", Usage: "consumer group"},
			&cli.BoolFlag{Name: "reassemble", Usage: "reassemble bodies the pipeline split"},
			&cli.Int64Flag{Name: "reassemble-max-bytes", Value: 256 * 1024 * 1024, Usage: "bytes held across open sets"},
			&cli.DurationFlag{Name: "reassemble-max-age", Value: 10 * time.Minute, Usage: "how long a set waits for its last part"},
			&cli.StringFlag{Name: "dead-letters", Value: "log", Usage: "where dead letters go: log, or clickhouse (the facts table the CLICKHOUSE_* variables name)"},
			&cli.StringFlag{Name: "database", Usage: "with --dead-letters=clickhouse: the database holding the facts table; empty takes the store's default"},
		},
		Action: runLand,
	}
}

func runLand(c *cli.Context) (err error) {
	conn := pkafka.DefaultFranzConnectionDetails()
	conn.SeedBrokers = strings.Split(c.String("brokers"), ",")
	conn.ClientID = "stevedoredemo"
	conn.Logger = &log.Logger
	cons := pkafka.DefaultFranzConsumerDetails()
	err = cons.SetTopicSpec([]string{c.String("topic")}, true)
	if err != nil {
		return eh.Errorf("topic spec: %w", err)
	}
	readerOpts := pkafka.DefaultFranzReaderOrderedOpts()
	readerOpts.ConsumerGroup = c.String("group")
	readerOpts.Logger = &log.Logger
	reader, err := pkafka.NewFranzReaderOrdered(readerOpts, func() (opts []kgo.Opt, err error) {
		opts = append(opts, conn.FranzOpts()...)
		opts = append(opts, cons.FranzOpts()...)
		return
	})
	if err != nil {
		return eh.Errorf("reader: %w", err)
	}

	var dead lander.DeadLettersI
	switch c.String("dead-letters") {
	case "log":
		dead = logDeadLetters{}
	case "clickhouse":
		client := chclient.New(chclient.ConfigFromEnv(), nil)
		exec, xerr := storeexec.New(client, nil)
		if xerr != nil {
			return eh.Errorf("executor: %w", xerr)
		}
		cfg := stevedorefacts.StevedoreStoreConfig{}
		if db := c.String("database"); db != "" {
			cfg.Table = db + ".facts"
		}
		store, oerr := stevedorefacts.OpenStevedoreStore(c.Context, exec, nil, cfg)
		if oerr != nil {
			return eh.Errorf("open dead-letter store: %w", oerr)
		}
		defer store.Close()
		dead = &lander.StoreDeadLetters{Store: store}
	default:
		return eh.Errorf("--dead-letters is log or clickhouse")
	}

	l := lander.New(lander.Config{
		Reassemble:         c.Bool("reassemble"),
		ReassembleMaxBytes: c.Int64("reassemble-max-bytes"),
		ReassembleMaxAge:   c.Duration("reassemble-max-age"),
		Logger:             &log.Logger,
	}, reader, printSink{}, dead)
	ctx, cancel := context.WithCancel(c.Context)
	defer cancel()
	err = l.Run(ctx)
	log.Info().Uint64("landed", l.Landed()).Uint64("deadLettered", l.DeadLettered()).Msg("stevedoredemo land ends")
	return
}

// printSink is the example sink: one JSON line per item on stdout. A real
// sink writes rows keyed by the reference and the ordinal through a
// generated store; this one has no rows and nothing to flush.
type printSink struct{}

var _ stevedore.SinkI = printSink{}

type printedItem struct {
	Ref     string `json:"ref"`
	Origin  string `json:"origin,omitzero"`
	Ordinal uint64 `json:"ordinal"`
	Line    uint64 `json:"line,omitzero"`
	Offset  uint64 `json:"offset,omitzero"`
	Kind    string `json:"kind,omitzero"`
	Payload string `json:"payload"`
}

func (printSink) Land(_ context.Context, item stevedore.Item) error {
	b, err := json.Marshal(printedItem{
		Ref: "0x" + strconv.FormatUint(item.Ref.Value(), 16), Origin: item.Origin, Ordinal: item.Ordinal,
		Line: item.Line, Offset: item.Offset, Kind: item.PayloadKind, Payload: string(item.Payload),
	})
	if err != nil {
		return stevedore.Permanent(eh.Errorf("render item: %w", err))
	}
	_, err = os.Stdout.Write(append(b, '\n'))
	return err
}

func (printSink) Flush(context.Context) error { return nil }

// logDeadLetters is the example dead-letter store: one log line per row.
type logDeadLetters struct{}

var _ lander.DeadLettersI = logDeadLetters{}

func (logDeadLetters) Add(_ context.Context, row stevedorefacts.DeadLetter) error {
	log.Warn().Str("class", row.Class).Str("error", row.Error).Str("topic", row.Topic).
		Int32("partition", row.Partition).Int64("offset", row.Offset).Msg("dead letter")
	return nil
}

func (logDeadLetters) Flush(context.Context) error { return nil }
