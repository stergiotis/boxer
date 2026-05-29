package cli

import (
	"fmt"
	"os"

	"github.com/dgraph-io/badger/v4"
	badger2 "github.com/stergiotis/boxer/public/db/badger"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/urfave/cli/v2"
)

func NewCliCommandBadger() *cli.Command {
	return &cli.Command{
		Name: "badger",
		Subcommands: []*cli.Command{
			{
				Name: "dump",
				Flags: []cli.Flag{
					&cli.PathFlag{
						Name:     "path",
						Required: true,
					},
					&cli.IntFlag{
						Name:  "prefetchSize",
						Value: 1024,
					},
				},
				Action: func(context *cli.Context) error {
					storePath := context.Path("path")
					opts := badger.DefaultOptions(storePath).WithLogger(&badger2.ZerologLoggerAdapter{})
					kv, err := badger.Open(opts)
					if err != nil {
						return eb.Build().Str("storePath", storePath).Errorf("unable to open key value store database: %w", err)
					}
					prefetchSize := context.Int("prefetchSize")
					err = kv.View(func(txn *badger.Txn) error {
						iter := txn.NewIterator(badger.IteratorOptions{
							PrefetchSize:   prefetchSize,
							PrefetchValues: prefetchSize > 0,
							Reverse:        false,
							AllVersions:    false,
							InternalAccess: false,
							Prefix:         nil,
							SinceTs:        0,
						})
						defer iter.Close()
						for iter.Rewind(); iter.Valid(); iter.Next() {
							item := iter.Item()
							k := item.Key()
							err = item.Value(func(v []byte) error {
								_, e := fmt.Fprintf(os.Stdout, "%s = %s\n", k, v)
								return e
							})
							if err != nil {
								return err
							}
						}
						return nil
					})
					if err != nil {
						return eh.Errorf("unable to iterate badger database: %w", err)
					}
					return nil
				},
			},
		},
	}
}
