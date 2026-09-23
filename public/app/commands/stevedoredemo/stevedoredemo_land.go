package stevedoredemo

import (
	"context"
	"encoding/json/v2"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/rs/zerolog/log"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/urfave/cli/v2"

	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/keelson/data/storeexec"
	"github.com/stergiotis/boxer/public/observability/eh"
	pkafka "github.com/stergiotis/boxer/public/streaming/persisted/kafka"
	"github.com/stergiotis/boxer/public/streaming/stevedore"
	"github.com/stergiotis/boxer/public/streaming/stevedore/deadletter"
	"github.com/stergiotis/boxer/public/streaming/stevedore/lander"
	"github.com/stergiotis/boxer/public/streaming/stevedore/stevedorefacts"
)

func newLandCommand() *cli.Command {
	return &cli.Command{
		Name:  "land",
		Usage: "consume items from a topic, print each as a JSON line, dead-letter what does not decode",
		Flags: append([]cli.Flag{
			&cli.StringFlag{Name: "brokers", Required: true, Usage: "comma-separated seed brokers"},
			&cli.StringFlag{Name: "topic", Required: true, Usage: "the items topic"},
			&cli.StringFlag{Name: "group", Value: "stevedoredemo", Usage: "consumer group"},
		}, deadLetterFlags()...),
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

	dead, closeDead, err := openDeadLetters(c)
	if err != nil {
		return
	}
	defer closeDead()

	l := lander.New(lander.Config{Logger: &log.Logger}, reader, printSink{}, dead)
	ctx, stop := signal.NotifyContext(c.Context, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	err = l.Run(ctx)
	log.Info().Uint64("landed", l.Landed()).Uint64("deadLettered", l.DeadLettered()).Msg("stevedoredemo land ends")
	return
}

// deadLetterFlags are the flags land and run share.
func deadLetterFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: "dead-letters", Value: "log", Usage: "where dead letters go: log, or clickhouse (the facts table the CLICKHOUSE_* variables name)"},
		&cli.StringFlag{Name: "database", Usage: "with --dead-letters=clickhouse: the database holding the facts table; empty takes the store's default"},
	}
}

// openDeadLetters builds the dead-letter store the flags name.
func openDeadLetters(c *cli.Context) (dead deadletter.StoreI, closeFn func(), err error) {
	closeFn = func() {}
	switch c.String("dead-letters") {
	case "log":
		dead = deadletter.Log{Logger: &log.Logger}
	case "clickhouse":
		client := chclient.New(chclient.ConfigFromEnv(), nil)
		exec, xerr := storeexec.New(client, nil)
		if xerr != nil {
			return nil, closeFn, eh.Errorf("executor: %w", xerr)
		}
		cfg := stevedorefacts.StevedoreStoreConfig{}
		if db := c.String("database"); db != "" {
			cfg.Table = db + ".facts"
		}
		store, oerr := stevedorefacts.OpenStevedoreStore(c.Context, exec, nil, cfg)
		if oerr != nil {
			return nil, closeFn, eh.Errorf("open dead-letter store: %w", oerr)
		}
		closeFn = store.Close
		dead = &deadletter.Store{Store: store}
	default:
		return nil, closeFn, eh.Errorf("--dead-letters is log or clickhouse")
	}
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
	Part    uint32 `json:"part,omitzero"`
	Last    bool   `json:"last,omitzero"`
	Line    uint64 `json:"line,omitzero"`
	Offset  uint64 `json:"offset,omitzero"`
	Kind    string `json:"kind,omitzero"`
	Payload string `json:"payload"`
}

func (printSink) Land(_ context.Context, item stevedore.Item) error {
	b, err := json.Marshal(printedItem{
		Ref: "0x" + strconv.FormatUint(item.Ref.Value(), 16), Origin: item.Origin, Ordinal: item.Ordinal,
		Part: item.Part, Last: item.Last, Line: item.Line, Offset: item.Offset, Kind: item.PayloadKind, Payload: string(item.Payload),
	})
	if err != nil {
		return stevedore.Permanent(eh.Errorf("render item: %w", err))
	}
	_, err = os.Stdout.Write(append(b, '\n'))
	return err
}

func (printSink) Flush(context.Context) error { return nil }
